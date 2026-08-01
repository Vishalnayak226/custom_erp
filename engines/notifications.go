package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"fmt"
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

var notificationHTTPClient = &http.Client{Timeout: 5 * time.Second}

type notificationWebhookPayload struct {
	Event   string            `json:"event"`
	Channel string            `json:"channel"`
	OrderID string            `json:"order_id"`
	Subject string            `json:"subject,omitempty"`
	Body    string            `json:"body"`
	Extra   map[string]string `json:"extra,omitempty"`
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

		writeNotificationLog(schema, event, t.channel, orderID, t.id, "Sent", "dispatch attempted (fire-and-forget, delivery not confirmed)")
		payload := notificationWebhookPayload{Event: event, Channel: t.channel, OrderID: orderID, Subject: subject, Body: body, Extra: extra}
		go postNotificationWebhook(webhookURL, payload)
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

func postNotificationWebhook(webhookURL string, payload notificationWebhookPayload) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[NOTIFY] failed to marshal payload: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[NOTIFY] failed to build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := notificationHTTPClient.Do(req)
	if err != nil {
		log.Printf("[NOTIFY] webhook delivery failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[NOTIFY] webhook returned HTTP %d", resp.StatusCode)
	}
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
