package server

import (
	"net/http"

	"custom_erp/engines"
)

// Stage 49.11.4: every inbound webhook refusal and repeat goes through these
// two, so the detection sweep sees them (engines.NoteInboundWebhookEvent).
// The caller still learns nothing about which check failed (134.3).

// refuseWebhookSignature answers a delivery whose signature did not verify
// and records it for source.
func refuseWebhookSignature(w http.ResponseWriter, r *http.Request, source string) {
	engines.NoteInboundWebhookEvent(r.Context(), r.Header.Get("Resolved-Tenant-ID"), source, engines.WebhookSignatureRefused)
	writeAPIErrorGeneric(w, r, http.StatusUnauthorized, "Invalid webhook signature")
}

// claimInboundWebhook is engines.ClaimInboundWebhook plus the record of a
// repeated delivery.
func claimInboundWebhook(r *http.Request, tenantID, source, eventID string, body []byte) (bool, error) {
	dup, err := engines.ClaimInboundWebhook(tenantID, source, eventID, body)
	if err == nil && dup {
		engines.NoteInboundWebhookEvent(r.Context(), tenantID, source, engines.WebhookReplayed)
	}
	return dup, err
}
