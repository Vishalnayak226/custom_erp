package engines

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Stage 38.4: webhook subscriptions with HMAC signing, retry/backoff and
// DLQ - extends outbox.go/notifications.go per the backlog item's own
// framing. WebhookSubscription is a plain registered doctype (the
// ScheduledReport precedent) - no dedicated create/list/delete handler.
// dispatchWebhooksForEvent is called from processOutbox (engines/outbox.go)
// once an event is already durably committed to integration_event_outbox,
// so a subscription match is only ever enqueued for an event that actually
// happened - no transactional coupling needed with whatever caller
// originally called PublishEvent. Delivery itself rides Stage 38.6's async
// job runner: its own retry/backoff/DeadLettered handling IS this item's
// DLQ, not a second mechanism.

const webhookDeliveryJobType = "webhook_delivery"

func init() {
	RegisterJobHandler(webhookDeliveryJobType, deliverWebhook)
}

// ValidateWebhookSubscriptionDocument is this doctype's only validation
// seam (the Budget/ReorderPointConfig precedent: a pure generic-document
// doctype, no dedicated create function). Refuses an unsafe URL at create
// time rather than only discovering it at delivery time.
func ValidateWebhookSubscriptionDocument(tenantID string, payload map[string]interface{}) error {
	rawURL, _ := payload["url"].(string)
	if err := validateWebhookURL(rawURL); err != nil {
		return err
	}
	secret, _ := payload["secret"].(string)
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("a webhook subscription needs a signing secret")
	}
	pattern, _ := payload["event_pattern"].(string)
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("a webhook subscription needs an event_pattern")
	}
	return nil
}

// validateWebhookURL is the one SSRF guard, called both at document-create
// time (a convenience check) and again immediately before every delivery
// attempt (the real guarantee - DNS can change between the two, and a
// create-time-only check would be a classic TOCTOU bypass). https only,
// and every resolved IP must be a genuine public address - no loopback,
// private, link-local, unspecified or multicast target, which is what
// would let a webhook URL be used to probe this server's own internal
// network or cloud metadata endpoint.
func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("webhook url %q is not a valid URL", rawURL)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("webhook url must use https, got %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("webhook host %q does not resolve", host)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("webhook host %q resolves to a non-public address (%s) - refused", host, ip)
		}
	}
	return nil
}

// matchEventPattern supports an exact match or a trailing-"*" prefix
// wildcard ("inventory.*" matches "inventory.stock_changed") - deliberately
// no full glob/regex vocabulary, matching this codebase's stated preference
// for the simplest mechanism that covers the real cases (report_registry.go
// makes the identical call against a full query builder).
func matchEventPattern(pattern, eventName string) bool {
	if pattern == eventName {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(eventName, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// dispatchWebhooksForEvent enqueues one webhook_delivery job per Active
// WebhookSubscription whose event_pattern matches eventName. Called from
// processOutbox once eventID's own outbox row is already committed.
// idempotency_key is eventID+subscriptionID, so a re-run of the same outbox
// event (processOutbox retries a Failed row) never double-enqueues delivery
// to the same subscription.
func dispatchWebhooksForEvent(schema, eventID, eventName string, payload map[string]interface{}) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'url', ''), COALESCE(data->>'secret', ''), COALESCE(data->>'event_pattern', '')
		FROM %s.documents WHERE doctype = 'WebhookSubscription' AND status = 'Active' AND deleted_at IS NULL`, schema))
	if err != nil {
		log.Printf("[WEBHOOK] failed to list subscriptions for %s: %v", schema, err)
		return
	}
	type sub struct{ id, url, secret, pattern string }
	var subs []sub
	for rows.Next() {
		var s sub
		if err := rows.Scan(&s.id, &s.url, &s.secret, &s.pattern); err == nil {
			subs = append(subs, s)
		}
	}
	rows.Close()

	for _, s := range subs {
		if !matchEventPattern(s.pattern, eventName) {
			continue
		}
		jobPayload := map[string]interface{}{
			"url": s.url, "secret": s.secret, "event_name": eventName, "event_payload": payload,
		}
		if _, err := enqueueJobInSchema(schema, webhookDeliveryJobType, jobPayload, eventID+"-"+s.id); err != nil {
			log.Printf("[WEBHOOK] failed to enqueue delivery for subscription %s: %v", s.id, err)
		}
	}
}

var webhookHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	// Redirects are never followed - a URL that validated as public at
	// enqueue time could otherwise 3xx to an internal address and reach it
	// anyway. http.ErrUseLastResponse returns the redirect response as-is.
	CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
}

const webhookMaxResponseBodyBytes = 64 * 1024

// deliverWebhook is the webhook_delivery job handler (Stage 38.6's runner).
// A sandbox tenant (Stage 38.7) never makes a real outbound call - "all
// external side effects off" for a sandbox is enforced right here, the one
// real outbound HTTP call this whole feature makes.
func deliverWebhook(schema string, job Job) (map[string]interface{}, error) {
	rawURL, _ := job.Payload["url"].(string)
	secret, _ := job.Payload["secret"].(string)
	eventName, _ := job.Payload["event_name"].(string)
	eventPayload, _ := job.Payload["event_payload"].(map[string]interface{})

	if isSandbox, _ := IsSandboxSchema(schema); isSandbox {
		log.Printf("[WEBHOOK] sandbox tenant %s - simulating delivery of %q to %s (no real HTTP call made)", schema, eventName, rawURL)
		return map[string]interface{}{"simulated": true, "reason": "sandbox tenant"}, nil
	}

	// Stage 47.0.5/47.11.6 Gate 0: this server-wide switch is the sandbox
	// check's twin for every other tenant - a dev/test boot must not deliver
	// a real webhook just because a regression/abuse test or a developer
	// happened to trigger one.
	if !ExternalSideEffectsEnabled() {
		log.Printf("[WEBHOOK] external side effects are OFF - simulating delivery of %q to %s (no real HTTP call made)", eventName, rawURL)
		return map[string]interface{}{"simulated": true, "reason": "external side effects disabled"}, nil
	}

	if err := validateWebhookURL(rawURL); err != nil {
		return nil, err
	}
	return sendWebhookHTTP(rawURL, secret, eventName, eventPayload, job.ID)
}

// sendWebhookHTTP is the actual HMAC-sign-and-POST mechanism, kept separate
// from deliverWebhook's sandbox/SSRF guards above it so it can be tested
// directly against an httptest.Server - validateWebhookURL correctly
// refuses 127.0.0.1 (loopback), which is exactly what a local test server
// binds to.
func sendWebhookHTTP(rawURL, secret, eventName string, eventPayload map[string]interface{}, jobID string) (map[string]interface{}, error) {
	bodyBytes, err := json.Marshal(map[string]interface{}{"event": eventName, "payload": eventPayload})
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(bodyBytes)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	req.Header.Set("X-Webhook-Event", eventName)
	req.Header.Set("X-Webhook-Delivery", jobID)

	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, webhookMaxResponseBodyBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webhook endpoint returned HTTP %d", resp.StatusCode)
	}
	return map[string]interface{}{"status_code": resp.StatusCode}, nil
}
