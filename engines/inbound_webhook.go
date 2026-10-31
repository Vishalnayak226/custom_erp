package engines

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Stage 134.3 (49.8.3): what every inbound webhook shares, whatever the
// platform's own signature format.
//
//   - VerifyWebhookHMACAny: the signature checked against each secret the
//     sender may be using - the current one, and the previous one for a week
//     after a rotation, so a secret can be changed without dropping events.
//   - ClaimInboundWebhook: the replay store. A delivery is claimed by its
//     platform's own delivery id (or, where the platform sends none, the
//     body's hash); a second claim is a repeat, answered OK and not processed
//     again. A claim whose processing failed is released, so the platform's
//     own retry is processed.
//   - InboundWebhookMaxBody caps what a handler reads.
//
// Refusals answer the same generic "Invalid webhook signature" whatever the
// reason, so a caller learns nothing about which check failed.

const InboundWebhookMaxBody = 1 << 20

// inboundWebhookRetention is how long a claim is kept: longer than any
// platform retries (Shopify: 48 hours).
const inboundWebhookRetention = 30 * 24 * time.Hour

// webhookSecretOverlap is how long the previous secret is still accepted.
const webhookSecretOverlap = 7 * 24 * time.Hour

// VerifyWebhookHMACAny checks signature (after prefix, e.g. "sha256=") as the
// HMAC-SHA256 of payload under any of secrets, constant-time.
func VerifyWebhookHMACAny(payload []byte, signature, prefix, encoding string, secrets ...string) bool {
	signature = strings.TrimSpace(signature)
	if prefix != "" {
		if !strings.HasPrefix(signature, prefix) {
			return false
		}
		signature = signature[len(prefix):]
	}
	if signature == "" {
		return false
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		sum := mac.Sum(nil)
		var want string
		if encoding == "base64" {
			want = base64.StdEncoding.EncodeToString(sum)
		} else {
			want = hex.EncodeToString(sum)
			signature = strings.ToLower(signature)
		}
		if hmac.Equal([]byte(signature), []byte(want)) {
			return true
		}
	}
	return false
}

// GetChannelWebhookSecrets is the channel's current webhook secret and, for a
// week after it was changed, the previous one.
func GetChannelWebhookSecrets(tenantID, channelCode string) []string {
	fields, err := getChannelCredential(tenantID, channelCode)
	if err != nil {
		return nil
	}
	out := []string{}
	if s := fields["webhook_secret"]; s != "" {
		out = append(out, s)
	}
	if prev := fields["webhook_secret_previous"]; prev != "" {
		if at, err := time.Parse(time.RFC3339, fields["webhook_secret_rotated_at"]); err == nil && time.Since(at) < webhookSecretOverlap {
			out = append(out, prev)
		}
	}
	return out
}

// keepWebhookSecretOverlap is SaveChannelCredential's half of the rotation:
// a changed webhook_secret moves the old one to webhook_secret_previous; a
// save that does not change it keeps an overlap still running.
func keepWebhookSecretOverlap(old, fields map[string]string) {
	if old == nil {
		return
	}
	next, ok := fields["webhook_secret"]
	switch {
	case ok && next != "" && old["webhook_secret"] != "" && next != old["webhook_secret"]:
		fields["webhook_secret_previous"] = old["webhook_secret"]
		fields["webhook_secret_rotated_at"] = time.Now().UTC().Format(time.RFC3339)
	case old["webhook_secret_previous"] != "" && fields["webhook_secret_previous"] == "":
		fields["webhook_secret_previous"] = old["webhook_secret_previous"]
		fields["webhook_secret_rotated_at"] = old["webhook_secret_rotated_at"]
	}
}

var inboundWebhookPruned sync.Map // schema -> time.Time of the last prune

// ClaimInboundWebhook records one delivery. duplicate is true when the same
// delivery was already claimed: answer OK and do nothing. eventID "" uses the
// body's hash.
func ClaimInboundWebhook(tenantID, source, eventID string, body []byte) (duplicate bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	id := inboundEventID(eventID, body)
	if last, ok := inboundWebhookPruned.Load(schema); !ok || time.Since(last.(time.Time)) > time.Hour {
		inboundWebhookPruned.Store(schema, time.Now())
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inbound_webhook_receipts WHERE received_at < $1`, schema), time.Now().Add(-inboundWebhookRetention))
	}
	res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.inbound_webhook_receipts (source, event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, schema), source, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 0, nil
}

// ReleaseInboundWebhook drops a claim whose processing failed.
func ReleaseInboundWebhook(tenantID, source, eventID string, body []byte) {
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inbound_webhook_receipts WHERE source = $1 AND event_id = $2`, schema), source, inboundEventID(eventID, body))
	}
}

func inboundEventID(eventID string, body []byte) string {
	if eventID = strings.TrimSpace(eventID); eventID != "" && len(eventID) <= 200 {
		return eventID
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// --- Stage 49.11.4: the events behind the webhook detections ---------------
//
// A refused signature and a repeated delivery are each one signed
// WEBHOOK_INBOUND audit row (status SIGNATURE_REFUSED / REPLAYED) naming the
// source - never the body or a header - with the request's address and
// correlation id (LogAuditEventCtx). The detection sweep counts them per
// client (security_detection.go: webhook_signature_surge,
// webhook_replay_surge). Rows are capped per client and outcome per window on
// each server, so a flood of forged calls cannot grow the audit log without
// bound: past the cap the alert has long since fired.

const (
	InboundWebhookAction    = "WEBHOOK_INBOUND"
	WebhookSignatureRefused = "SIGNATURE_REFUSED"
	WebhookReplayed         = "REPLAYED"
	webhookEventWindow      = 15 * time.Minute
	webhookEventRowCap      = 100
)

var webhookEventCounts = struct {
	sync.Mutex
	window time.Time
	n      map[string]int
}{n: map[string]int{}}

// NoteInboundWebhookEvent records one refused signature or repeated delivery
// for source (e.g. "shopify:web"). outcome is WebhookSignatureRefused or
// WebhookReplayed.
func NoteInboundWebhookEvent(ctx context.Context, tenantID, source, outcome string) {
	if outcome != WebhookSignatureRefused && outcome != WebhookReplayed {
		return
	}
	if !webhookEventAllowed(tenantID, outcome, time.Now()) {
		return
	}
	src := boundPlain(source, 120)
	if src == "" {
		src = "unknown"
	}
	LogAuditEventCtx(ctx, tenantID, "system", InboundWebhookAction, outcome, "source="+src)
}

// webhookEventAllowed counts one event for tenant and outcome in the current
// window and says whether it is still under webhookEventRowCap. The map is
// emptied when the window turns, so it holds at most one window's keys.
func webhookEventAllowed(tenantID, outcome string, now time.Time) bool {
	window := now.Truncate(webhookEventWindow)
	webhookEventCounts.Lock()
	defer webhookEventCounts.Unlock()
	if !webhookEventCounts.window.Equal(window) {
		webhookEventCounts.window = window
		webhookEventCounts.n = map[string]int{}
	}
	key := tenantID + "|" + outcome
	if webhookEventCounts.n[key] >= webhookEventRowCap {
		return false
	}
	webhookEventCounts.n[key]++
	return true
}
