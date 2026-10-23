package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Stage 26.12.10 (Per-order/customer notification templates): distinct from
// alerting.go's incident-level Slack/Teams ops alerts (an operator-facing,
// single-webhook-per-deployment mechanism) - this is customer-facing,
// per-tenant, per-channel (Email/SMS/WhatsApp), and template-driven.
// Scope decision made with the user (2026-07-25): real generic webhook
// dispatch. This repo has no email/SMS/WhatsApp provider SDK and adding
// one would violate the no-new-dependency principle, so - same "code-
// complete, credentials-pending" shape as the courier API (26.12.4) and PIM
// channel connectors - a tenant-configured webhook URL per channel is the
// dispatch target; a real provider send happens on the other end of that
// webhook (Zapier/Make/the tenant's own service), which is also who
// resolves actual customer contact details - this process never holds or
// forwards raw customer PII beyond order/event identifiers, the same
// minimal-payload precedent alerting.go's own file header documents for ops
// alerts.

var notificationHTTPClient = newOutboundClient(5*time.Second, false) // 134.2: a client-typed address

type notificationWebhookPayload struct {
	// DeliveryID is the NotificationLog id: the provider echoes it back on
	// /api/public/v1/notifications/{id}/delivery-status (Stage 73.2.13).
	DeliveryID string            `json:"delivery_id,omitempty"`
	Event      string            `json:"event"`
	Channel    string            `json:"channel"`
	OrderID    string            `json:"order_id"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body"`
	Extra      map[string]string `json:"extra,omitempty"`
}

// renderTemplate does flat {{key}} substitution - deliberately not
// text/template (which would let an admin-authored template string execute
// arbitrary template actions); a plain placeholder swap is enough for this
// scope and keeps the substitution surface inert.
func renderTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

// DispatchNotification is this item's trigger point, called from a fixed,
// documented set of order/return lifecycle events (engines/orders.go's
// CreateSalesOrder/CancelOrder, engines/returns.go's request/approve/
// reject/refund functions). Deliberately NOT wired into
// engines/marketplace.go's HandoverManifest/RecordDeliveryEvent (Order
// Shipped/Delivered) in this pass - that file is being actively edited by a
// concurrent 26.12.4 session; wiring those two trigger points is a small,
// safe follow-up once that lands, same file-collision-avoidance precedent
// this sprint's own build notes already use elsewhere. For every Active
// NotificationTemplate matching event, resolves that channel's
// NotificationChannelConfig and POSTs a JSON payload to its webhook_url
// (fire-and-forget, mirroring alerting.go's SendOpsAlert - never adds
// latency to the caller). An unconfigured channel or missing template never
// blocks the caller either; both cases still write a NotificationLog row so
// the gap is visible (Stage 26.12.7's exception-queue report reads the
// underlying integration_event_outbox instead, but this log is its own
// direct audit trail for "did we even try to notify").
func DispatchNotification(tenantID, event, orderID string, extra map[string]string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		log.Printf("[NOTIFY] failed to resolve tenant schema for %s: %v", tenantID, err)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data->>'channel', COALESCE(data->>'subject', ''), data->>'body_template' FROM %s.documents
		 WHERE doctype = 'NotificationTemplate' AND status = 'Active' AND deleted_at IS NULL AND data->>'event' = $1`, schema), event)
	if err != nil {
		log.Printf("[NOTIFY] failed to query templates for event %s: %v", event, err)
		return
	}
	type tmplRow struct{ id, channel, subject, body string }
	var templates []tmplRow
	for rows.Next() {
		var t tmplRow
		if errScan := rows.Scan(&t.id, &t.channel, &t.subject, &t.body); errScan == nil {
			templates = append(templates, t)
		}
	}
	rows.Close()

	if len(templates) == 0 {
		writeNotificationLog(schema, event, "", orderID, "", "Skipped-NoTemplate", "no Active NotificationTemplate configured for this event")
		return
	}

	vars := map[string]string{"order_id": orderID, "event": event}
	for k, v := range extra {
		vars[k] = v
	}

	for _, t := range templates {
		subject := renderTemplate(t.subject, vars)
		body := renderTemplate(t.body, vars)

		webhookURL, errCfg := activeChannelWebhook(schema, t.channel)
		if errCfg != nil || webhookURL == "" {
			log.Printf("[NOTIFY] (no NotificationChannelConfig for channel %s, not sent) [%s] order=%s", t.channel, event, orderID)
			writeNotificationLog(schema, event, t.channel, orderID, t.id, "Skipped-NoConfig", "no Active NotificationChannelConfig for this channel")
			continue
		}

		// 49.7.6 / 47.0.5-47.11.6 Gate 0: this was the one real outbound HTTP
		// call site in the codebase with neither the sandbox check
		// (engines/webhook.go's deliverWebhook, engines/password_reset.go's
		// sendPasswordResetEmail) nor the server-wide ExternalSideEffectsEnabled()
		// switch every other external call site is expected to honour - found
		// while closing 49.7.6. A tenant's own configured NotificationChannelConfig
		// (a real Zapier/Make webhook URL) would fire on every order/return event
		// regardless of sandbox flag or dev/test posture. Logged distinctly so the
		// NotificationLog audit trail says what actually happened rather than
		// claiming "Sent" for a call that was suppressed.
		if isSandbox, _ := IsSandboxSchema(schema); isSandbox {
			log.Printf("[NOTIFY] sandbox tenant schema %s - simulating dispatch of %q to %s (no real HTTP call made)", schema, event, t.channel)
			writeNotificationLog(schema, event, t.channel, orderID, t.id, "Skipped-Sandbox", "sandbox tenant - no real webhook call made")
			continue
		}
		if !ExternalSideEffectsEnabled() {
			log.Printf("[NOTIFY] external side effects are OFF - simulating dispatch of %q to %s (no real HTTP call made)", event, t.channel)
			writeNotificationLog(schema, event, t.channel, orderID, t.id, "Skipped-SideEffectsOff", "external side effects disabled - no real webhook call made")
			continue
		}

		// Stage 73.2.13: no more fire-and-forget "Sent". The log row and its
		// delivery job commit together; the job runner makes the attempts and
		// the provider's own answer sets the final state.
		payload := notificationWebhookPayload{Event: event, Channel: t.channel, OrderID: orderID, Subject: subject, Body: body, Extra: extra}
		if err := queueNotificationDelivery(schema, t.id, webhookURL, payload); err != nil {
			log.Printf("[NOTIFY] could not queue %q to %s: %v", event, t.channel, err)
			writeNotificationLog(schema, event, t.channel, orderID, t.id, "Failed", "could not be queued for delivery - nothing was sent")
		}
	}
}

func activeChannelWebhook(schema, channel string) (string, error) {
	var url string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'webhook_url' FROM %s.documents WHERE doctype = 'NotificationChannelConfig' AND status = 'Active' AND deleted_at IS NULL AND data->>'channel' = $1 LIMIT 1`, schema),
		channel).Scan(&url)
	if err != nil {
		return "", err
	}
	return url, nil
}

// Stage 73.2.13 - a notification's delivery state comes from evidence.
//
//	Queued    - the log row and its delivery job committed together; no
//	            provider has seen it yet.
//	Retrying  - an attempt failed in a way worth retrying (network, timeout,
//	            HTTP 408/429/5xx); the job runner backs off and tries again.
//	Accepted  - the provider answered 2xx: it took the message, but has not
//	            yet said it reached the customer.
//	Delivered - the provider said so, in its answer or by a later callback.
//	Failed    - the provider refused it, said it failed, or every attempt
//	            ran out. Each step is appended to the row's history.
//
// The send rides the async job runner (Stage 38.6), not
// integration_event_outbox: that table's payload is shown on Integration
// Logs and fanned out to webhook subscribers, and a rendered body can hold a
// one-time code (Loyalty Redemption OTP). async_jobs keeps its payload
// internal and is itself a transactional outbox - claimed with SKIP LOCKED,
// attempts, exponential backoff and a dead-letter state.
const notificationDeliveryJobType = "notification_delivery"

func init() {
	RegisterJobHandler(notificationDeliveryJobType, deliverNotification)
}

const notificationMaxResponseBytes = 64 * 1024

func notificationHistoryEntry(status, source, detail string) map[string]interface{} {
	return map[string]interface{}{
		"at": time.Now().UTC().Format(time.RFC3339), "status": status, "source": source, "detail": detail,
	}
}

// queueNotificationDelivery writes the Queued log row and its delivery job in
// one transaction: a row never claims a send that was never queued, and a
// queued send always has its row. The job's idempotency key is the log id,
// so the same notification can never be queued twice.
func queueNotificationDelivery(schema, templateID, webhookURL string, payload notificationWebhookPayload) error {
	logID := NewDocID("NL")
	payload.DeliveryID = logID
	doc := map[string]interface{}{
		"code": logID, "event": payload.Event, "channel": payload.Channel, "order_id": payload.OrderID,
		"template_id": templateID, "dispatch_status": "Queued", "attempts": 0,
		"response_detail": "queued for delivery - no provider response yet",
		"history":         []interface{}{notificationHistoryEntry("Queued", "system", "queued for delivery")},
	}
	marshaled, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var jobPayload map[string]interface{}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payloadJSON, &jobPayload); err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'NotificationLog', $2, 'Queued', 'system')`, schema),
		logID, marshaled); err != nil {
		return err
	}
	jobID, err := enqueueJobWith(tx, schema, notificationDeliveryJobType,
		map[string]interface{}{"log_id": logID, "url": webhookURL, "payload": jobPayload}, logID, "")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{job_id}', to_jsonb($2::text)) WHERE id = $1`, schema),
		logID, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

// notificationProviderResult is what one attempt learned from the provider.
type notificationProviderResult struct {
	HTTPStatus  int
	ProviderRef string
	State       string // Accepted, Delivered or Failed
	Detail      string
}

// errNotificationRetryable marks an attempt worth repeating.
type errNotificationRetryable struct{ msg string }

func (e *errNotificationRetryable) Error() string { return e.msg }

// normalizeProviderDeliveryState maps a provider's own word to ours. Anything
// not clearly delivered or failed ("sent", "queued", "accepted") stays
// Accepted: the provider has it, and nobody has said it arrived.
func normalizeProviderDeliveryState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "delivered", "read", "seen":
		return "Delivered"
	case "failed", "undelivered", "undeliverable", "bounced", "rejected", "blocked", "expired":
		return "Failed"
	}
	return "Accepted"
}

// sendNotificationHTTP makes one attempt. A network error, timeout, 408, 429
// or 5xx is retryable; any other non-2xx is a refusal that retrying will not
// change. Kept apart from deliverNotification's guards so a test can point it
// at an httptest server, the sendWebhookHTTP precedent.
func sendNotificationHTTP(ctx context.Context, rawURL string, payload notificationWebhookPayload) (notificationProviderResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return notificationProviderResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return notificationProviderResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notification-Delivery", payload.DeliveryID)
	resp, err := notificationHTTPClient.Do(req)
	if err != nil {
		return notificationProviderResult{}, &errNotificationRetryable{msg: "provider not reached: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, notificationMaxResponseBytes))

	result := notificationProviderResult{HTTPStatus: resp.StatusCode}
	var answer map[string]interface{}
	if json.Unmarshal(raw, &answer) == nil {
		for _, key := range []string{"provider_ref", "message_id", "id", "sid"} {
			if v, ok := answer[key]; ok && fmt.Sprint(v) != "" {
				result.ProviderRef = fmt.Sprint(v)
				break
			}
		}
		for _, key := range []string{"delivery_status", "status"} {
			if v, ok := answer[key].(string); ok && v != "" {
				result.Detail = "provider status: " + v
				result.State = normalizeProviderDeliveryState(v)
				break
			}
		}
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if result.State == "" {
			result.State = "Accepted"
		}
		if result.Detail == "" {
			result.Detail = fmt.Sprintf("provider accepted it (HTTP %d); delivery not yet confirmed", resp.StatusCode)
		}
		return result, nil
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return result, &errNotificationRetryable{msg: fmt.Sprintf("provider answered HTTP %d", resp.StatusCode)}
	}
	result.State = "Failed"
	result.Detail = fmt.Sprintf("provider refused it (HTTP %d)", resp.StatusCode)
	return result, nil
}

// notificationLogState reads a log row's current dispatch status.
func notificationLogState(schema, logID string) (string, error) {
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'dispatch_status', status) FROM %s.documents WHERE id = $1 AND doctype = 'NotificationLog'`, schema),
		logID).Scan(&status)
	return status, err
}

// recordNotificationState moves a log row to status, merging fields and
// appending one history entry, in one statement.
func recordNotificationState(schema, logID, status, source, detail string, fields map[string]interface{}) error {
	patch := map[string]interface{}{"dispatch_status": status, "response_detail": detail}
	for k, v := range fields {
		patch[k] = v
	}
	if status == "Delivered" || status == "Failed" {
		patch["final_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	entryJSON, err := json.Marshal([]interface{}{notificationHistoryEntry(status, source, detail)})
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents
		SET data = (data || $2::jsonb) || jsonb_build_object('history', COALESCE(data->'history', '[]'::jsonb) || $3::jsonb),
		    status = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND doctype = 'NotificationLog'`, schema),
		logID, patchJSON, entryJSON, status)
	return err
}

// deliverNotification is the notification_delivery job handler. It never
// sends a message the provider already has (Accepted or Delivered), so a
// replayed or reclaimed job cannot notify a customer twice.
func deliverNotification(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logID, _ := job.Payload["log_id"].(string)
	rawURL, _ := job.Payload["url"].(string)
	state, err := notificationLogState(schema, logID)
	if err != nil {
		return nil, fmt.Errorf("notification log %s: %w", logID, err)
	}
	switch state {
	case "Accepted", "Delivered", "Sent":
		return map[string]interface{}{"skipped": "provider already has it (" + state + ")"}, nil
	}

	attempt := job.Attempts + 1
	attemptFields := map[string]interface{}{"attempts": attempt, "last_attempt_at": time.Now().UTC().Format(time.RFC3339)}
	if isSandbox, _ := IsSandboxSchema(schema); isSandbox {
		return map[string]interface{}{"simulated": true}, recordNotificationState(schema, logID, "Skipped-Sandbox", "system", "sandbox tenant - no real webhook call made", nil)
	}
	if !ExternalSideEffectsEnabled() {
		return map[string]interface{}{"simulated": true}, recordNotificationState(schema, logID, "Skipped-SideEffectsOff", "system", "external side effects disabled - no real webhook call made", nil)
	}
	if err := validateWebhookURL(rawURL); err != nil {
		detail := "channel webhook URL not allowed: " + err.Error()
		return map[string]interface{}{"state": "Failed"}, recordNotificationState(schema, logID, "Failed", "system", detail, attemptFields)
	}

	var payload notificationWebhookPayload
	if raw, err := json.Marshal(job.Payload["payload"]); err == nil {
		_ = json.Unmarshal(raw, &payload)
	}
	payload.DeliveryID = logID
	result, sendErr := sendNotificationHTTP(ctx, rawURL, payload)
	return applyNotificationAttempt(schema, logID, attempt, job.MaxAttempts, attemptFields, result, sendErr)
}

// applyNotificationAttempt records one attempt's outcome. A retryable failure
// returns its error so the job runner backs off; on the last attempt the row
// says Failed and the job dead-letters (Replay on Background Jobs resends).
func applyNotificationAttempt(schema, logID string, attempt, maxAttempts int, fields map[string]interface{}, result notificationProviderResult, sendErr error) (map[string]interface{}, error) {
	if result.HTTPStatus != 0 {
		fields["provider_http_status"] = result.HTTPStatus
	}
	if result.ProviderRef != "" {
		fields["provider_ref"] = result.ProviderRef
	}
	if sendErr != nil {
		if _, retryable := sendErr.(*errNotificationRetryable); retryable && attempt < maxAttempts {
			detail := fmt.Sprintf("attempt %d of %d failed: %s - will retry", attempt, maxAttempts, sendErr.Error())
			if err := recordNotificationState(schema, logID, "Retrying", "system", detail, fields); err != nil {
				return nil, err
			}
			return nil, sendErr
		}
		detail := fmt.Sprintf("gave up after %d attempt(s): %s", attempt, sendErr.Error())
		if err := recordNotificationState(schema, logID, "Failed", "system", detail, fields); err != nil {
			return nil, err
		}
		return nil, sendErr
	}
	if err := recordNotificationState(schema, logID, result.State, "provider", result.Detail, fields); err != nil {
		return nil, err
	}
	return map[string]interface{}{"state": result.State, "http_status": result.HTTPStatus}, nil
}

// RecordNotificationDeliveryStatus takes the provider's later answer - its
// delivery receipt - through the public API (scope notifications:write).
// Delivered is final. Failed can still become Delivered: a provider that
// finally delivers after we gave up is better evidence than our give-up. A
// row that was never sent (Skipped-*) cannot be confirmed. Repeating the
// same answer is a no-op, so a provider retrying its callback is safe.
func RecordNotificationDeliveryStatus(tenantID, logID, rawStatus, providerRef, detail, source string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	target := normalizeProviderDeliveryState(rawStatus)
	if target != "Delivered" && target != "Failed" {
		return nil, &ValidationError{Code: "NOTIFI-0461", SubFor: "status", Message: "status must be delivered or failed"}
	}
	current, err := notificationLogState(schema, logID)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "NOTIFI-0462", SubFor: "id", Message: fmt.Sprintf("notification %s not found", logID)}
	}
	if err != nil {
		return nil, err
	}
	if current == target {
		return map[string]interface{}{"id": logID, "dispatch_status": current, "changed": false}, nil
	}
	if current == "Delivered" || strings.HasPrefix(current, "Skipped") {
		return nil, &ValidationError{Code: "NOTIFI-0463", SubFor: "status",
			Message: fmt.Sprintf("notification %s is %s and cannot be changed to %s", logID, current, target)}
	}
	detail = strings.TrimSpace(detail)
	if len(detail) > 500 {
		detail = detail[:500]
	}
	if detail == "" {
		detail = "provider reported " + strings.ToLower(target)
	}
	fields := map[string]interface{}{}
	if ref := strings.TrimSpace(providerRef); ref != "" {
		fields["provider_ref"] = ref
	}
	if err := recordNotificationState(schema, logID, target, source, detail, fields); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": logID, "dispatch_status": target, "changed": true}, nil
}

func writeNotificationLog(schema, event, channel, orderID, templateID, dispatchStatus, detail string) {
	logID := NewDocID("NL")
	doc := map[string]interface{}{
		"code": logID, "event": event, "channel": channel, "order_id": orderID,
		"template_id": templateID, "dispatch_status": dispatchStatus, "response_detail": detail,
	}
	marshaled, err := json.Marshal(doc)
	if err != nil {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'NotificationLog', $2, $3, 'system')`, schema),
		logID, marshaled, dispatchStatus); err != nil {
		log.Printf("[NOTIFY] failed to write NotificationLog: %v", err)
	}
}
