package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"custom_erp/engines"
)

// 185.3 (ask 185): Razorpay payment links for a sales invoice
// (engines/finance_payment_links.go).

// handleSalesInvoicePaymentLink makes, or gives back, the invoice's link.
func handleSalesInvoicePaymentLink(w http.ResponseWriter, r *http.Request) {
	link, err := engines.CreateInvoicePaymentLink(r.Context(), r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(link)
}

// razorpayWebhookURL is the address to paste into Razorpay's webhook
// settings: the one the administrator is using, naming this client.
func razorpayWebhookURL(r *http.Request) string {
	return publicCallbackURL(r, "/api/v1/integration/razorpay/webhook", url.Values{"tenant_id": {r.Header.Get("Resolved-Tenant-ID")}})
}

func handleRazorpayCredentials(w http.ResponseWriter, r *http.Request) {
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only an administrator can save the Razorpay keys")
		return
	}
	var req struct {
		KeyID         string `json:"key_id"`
		KeySecret     string `json:"key_secret"`
		WebhookSecret string `json:"webhook_secret"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request body")
		return
	}
	if err := engines.SaveRazorpayCredential(r.Header.Get("Resolved-Tenant-ID"), req.KeyID, req.KeySecret, req.WebhookSecret, credentialActor(r), req.ExpiresAt); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "webhook_url": razorpayWebhookURL(r)})
}

func handleGetRazorpayCredentials(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.GetRazorpayCredentials(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	for _, row := range rows {
		row["webhook_url"] = razorpayWebhookURL(r)
	}
	_ = json.NewEncoder(w).Encode(rows)
}

// handleRazorpayWebhook takes Razorpay's webhooks for the client the address
// names (public). Without the client's webhook secret every call is refused;
// a forged or altered body fails the HMAC.
func handleRazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	body, err := io.ReadAll(io.LimitReader(r.Body, engines.InboundWebhookMaxBody))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Unreadable body")
		return
	}
	source, eventID := "razorpay", r.Header.Get("X-Razorpay-Event-Id")
	if !engines.VerifyWebhookHMACAny(body, r.Header.Get("X-Razorpay-Signature"), "", "hex", engines.GetChannelWebhookSecrets(tenantID, engines.RazorpayCredentialCode)...) {
		refuseWebhookSignature(w, r, source)
		return
	}
	// Razorpay sends a retry with the same event id: answered, not redone.
	if dup, err := claimInboundWebhook(r, tenantID, source, eventID, body); err == nil && dup {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "result": "duplicate"})
		return
	}
	result, err := engines.HandleRazorpayWebhook(tenantID, body)
	if err != nil {
		engines.ReleaseInboundWebhook(tenantID, source, eventID, body) // Razorpay's retry is processed
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "WARN", r.URL.Path, fmt.Sprintf("razorpay webhook: %v", err), "")
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "result": result})
}
