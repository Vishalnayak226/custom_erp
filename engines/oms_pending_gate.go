package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 88.3.1 (D2, D12): the Pending gate. A marketplace or website order
// whose checks pass waits at Pending - its stock reserved, nothing picked -
// until someone accepts or rejects it on OMS ▸ Pending Orders. Accept
// releases it to Pick; Reject cancels it with a reason. Either answer goes
// back to the channel through the async job runner (Stage 38.6), so a push
// that fails is retried with backoff and, once out of attempts, stays on the
// order as Failed with a Retry.
//
// The gate adds no order status. Pending is the existing Reserved - the one
// state an order already waited in for its release - plus acceptance_status
// on the order, which says whether the channel has had its answer. Orders
// keyed in by staff are accepted as they are created (SalesOrderInput.
// SkipPending), and "oms.accept_channel_orders" accepts channel orders on
// import for a business that does not want the step.

const (
	AcceptancePending  = "Pending"
	AcceptanceAccepted = "Accepted"
	AcceptanceRejected = "Rejected"

	// The answers as the channel is told them (ConnectorStatusUpdate.Status).
	OrderDecisionAccepted = "Accepted"
	OrderDecisionRejected = "Rejected"

	channelDecisionJobType = "oms_channel_decision_push"
)

// ErrOrderDecisionPushUnsupported is a connector's answer when its channel
// has no accept / reject call - Amazon accepts every order itself and
// Flipkart's status call is its dispatch. The answer is then given in the
// channel's own seller portal, and the order says so instead of retrying.
var ErrOrderDecisionPushUnsupported = errors.New("this channel takes accept / reject in its own seller portal, not through its connector")

func init() {
	RegisterJobHandler(channelDecisionJobType, pushChannelDecision)
}

// AcceptPendingOrder releases an order waiting at Pending to Pick and queues
// the answer to its channel. Accepting an order the channel already heard
// "Accepted" for (a released order edited back to Reserved) releases it
// again without telling the channel twice. Returns the push status.
func AcceptPendingOrder(tenantID, orderID, userID string) (string, error) {
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return "", err
	}
	if status := strField(order, "order_status"); status != "Reserved" {
		return "", fmt.Errorf("order %s is %s - only an order waiting at Pending (its stock reserved) can be accepted", orderID, status)
	}
	if _, _, err := ReleaseOrderToFulfillment(tenantID, orderID, userID); err != nil {
		return "", err
	}
	already := strField(order, "acceptance_status") == AcceptanceAccepted
	if err := mergeSalesOrderData(schema, orderID, map[string]interface{}{
		"acceptance_status": AcceptanceAccepted, "accepted_by": userID, "accepted_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return "", err
	}
	push := ""
	if !already {
		push, _ = queueChannelDecision(tenantID, schema, orderID, order, OrderDecisionAccepted)
	}
	LogAuditEvent(tenantID, userID, "ORDER_ACCEPTED", "SUCCESS", fmt.Sprintf("order=%s channel_push=%s", orderID, push))
	return push, nil
}

// RejectPendingOrder cancels an order under the cancellation rules (reason
// code, stage gate) and queues "Rejected" to its channel.
func RejectPendingOrder(tenantID, orderID, reasonCode, userID string) (string, error) {
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return "", err
	}
	if err := CancelOrder(tenantID, orderID, reasonCode, userID); err != nil {
		return "", err
	}
	if err := mergeSalesOrderData(schema, orderID, map[string]interface{}{
		"acceptance_status": AcceptanceRejected, "rejected_by": userID, "rejected_at": time.Now().UTC().Format(time.RFC3339),
		"rejection_reason_code": reasonCode,
	}); err != nil {
		return "", err
	}
	push, _ := queueChannelDecision(tenantID, schema, orderID, order, OrderDecisionRejected)
	LogAuditEvent(tenantID, userID, "ORDER_REJECTED", "SUCCESS", fmt.Sprintf("order=%s reason=%s channel_push=%s", orderID, reasonCode, push))
	return push, nil
}

// RetryChannelDecisionPush queues the order's last answer again - for a push
// that failed every attempt, or a channel connected after the answer was given.
func RetryChannelDecisionPush(tenantID, orderID, userID string) error {
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	push, _ := order["channel_push"].(map[string]interface{})
	decision, _ := push["decision"].(string)
	status, _ := push["status"].(string)
	if decision == "" {
		return fmt.Errorf("order %s has no answer for its channel to retry", orderID)
	}
	if status != "Failed" && status != "Not connected" {
		return fmt.Errorf("order %s: the channel push is %s - only a Failed or Not connected push is retried", orderID, status)
	}
	got, why := queueChannelDecision(tenantID, schema, orderID, order, decision, fmt.Sprintf("retry-%d", time.Now().UnixNano()))
	if got == "" {
		// 140.12.17: an own-channel order stuck before this change - nothing
		// waits for the answer, so the push is closed instead of retried.
		_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"channel_push": map[string]interface{}{
			"decision": decision, "status": "Not needed", "error": "", "at": time.Now().UTC().Format(time.RFC3339)}})
		LogAuditEvent(tenantID, userID, "ORDER_CHANNEL_PUSH_RETRY", "SUCCESS", fmt.Sprintf("order=%s decision=%s channel_push=Not needed", orderID, decision))
		return nil
	}
	LogAuditEvent(tenantID, userID, "ORDER_CHANNEL_PUSH_RETRY", "SUCCESS", fmt.Sprintf("order=%s decision=%s channel_push=%s", orderID, decision, got))
	if got != "Queued" {
		return fmt.Errorf("still %s: %s", strings.ToLower(got), why)
	}
	return nil
}

// ownOrderChannels are order sources that belong to the business itself. Set
// up as a Channel with a connector (a Shopify store named "Website"), the
// connector's own rules apply instead.
var ownOrderChannels = map[string]bool{"website": true, "own website": true, "web": true, "phone": true, "whatsapp": true, "store": true, "walk-in": true, "email": true}

// queueChannelDecision decides whether the answer can go to the order's
// channel, queues it when it can, and records the outcome on the order as
// channel_push. An order with no channel reference (staff-keyed) needs none.
func queueChannelDecision(tenantID, schema, orderID string, order map[string]interface{}, decision string, retryKey ...string) (string, string) {
	channel, ref := strField(order, "channel"), strField(order, "channel_order_id")
	if channel == "" || strings.EqualFold(channel, "Manual") || ref == "" {
		return "", ""
	}
	status, detail := "Queued", ""
	cfg, cfgErr := LoadChannelRuntimeConfig(tenantID, channel)
	if cfgErr != nil && ownOrderChannels[strings.ToLower(strings.TrimSpace(channel))] {
		// 140.12.17: the business's own website, phone or chat - no
		// marketplace is waiting for the answer, so like Manual it needs no
		// push (it was "Not connected" and retried for ever).
		return "", ""
	}
	if cfgErr != nil {
		status, detail = "Not connected", fmt.Sprintf("no active channel %q is set up (Setup ▸ PIM ▸ Channel) - add it and its credentials, then Retry", channel)
	} else if connector, err := ResolveOmnichannelConnector(cfg.Platform); err != nil || !connector.Descriptor().PushStatus {
		status, detail = "Not supported", "this channel's connector cannot send order answers - answer it in the channel's seller portal"
	} else if ok, _ := HasChannelCredential(tenantID, channel); !ok {
		status, detail = "Not connected", "the channel has no credentials yet - retry once they are saved"
	} else {
		key := orderID + ":" + decision
		if len(retryKey) > 0 {
			key += ":" + retryKey[0]
		}
		if _, err := EnqueueJob(tenantID, channelDecisionJobType, map[string]interface{}{
			"tenant_id": tenantID, "order_id": orderID, "decision": decision, "channel": channel, "channel_order_id": ref,
		}, key); err != nil {
			status, detail = "Failed", err.Error()
		}
	}
	_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"channel_push": map[string]interface{}{
		"decision": decision, "status": status, "error": detail, "at": time.Now().UTC().Format(time.RFC3339),
	}})
	return status, detail
}

// pushChannelDecision is the job handler. A sandbox tenant or a server with
// external side effects off records the push as Simulated (the webhook
// delivery rule); an error is retried by the runner, and the order shows the
// latest attempt either way.
func pushChannelDecision(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	str := func(k string) string { v, _ := job.Payload[k].(string); return v }
	tenantID, orderID, decision := str("tenant_id"), str("order_id"), str("decision")
	record := func(status, detail string) {
		_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"channel_push": map[string]interface{}{
			"decision": decision, "status": status, "error": detail, "at": time.Now().UTC().Format(time.RFC3339), "attempts": job.Attempts + 1,
		}})
	}
	ok, reason := OutsideCallsAllowed(schema) // 49.8.7a
	if sandbox, _ := IsSandboxSchema(schema); sandbox || !ok {
		record("Simulated", firstNonEmpty(reason, "a sandbox client")+" - nothing was sent")
		return map[string]interface{}{"simulated": true}, nil
	}
	err := PushChannelOrderStatus(ctx, tenantID, str("channel"), ConnectorStatusUpdate{ChannelOrderID: str("channel_order_id"), Status: decision})
	if errors.Is(err, ErrOrderDecisionPushUnsupported) {
		record("Not supported", err.Error())
		return map[string]interface{}{"supported": false}, nil
	}
	if err != nil {
		record("Failed", err.Error())
		return nil, err
	}
	record("Sent", "")
	return map[string]interface{}{"sent": true}, nil
}

// mergeSalesOrderData adds top-level fields to an order's data, leaving every
// other field as it is.
func mergeSalesOrderData(schema, orderID string, fields map[string]interface{}) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SalesOrder' AND id = $2`, schema),
		string(encoded), orderID)
	return err
}

// acceptImportedChannelOrder is "oms.accept_channel_orders": a business that
// does not want the Pending step has its channel orders accepted on import.
func acceptImportedChannelOrder(tenantID, orderID string) {
	if GetSettingBool(tenantID, "oms.accept_channel_orders") {
		_, _ = AcceptPendingOrder(tenantID, orderID, "system")
	}
}

func isOrderDecision(status string) bool {
	return status == OrderDecisionAccepted || status == OrderDecisionRejected
}
