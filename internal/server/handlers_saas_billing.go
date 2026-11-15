package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"custom_erp/engines"
)

// 50.12 (ask 193, ADR-009): the platform's SaaS subscription over HTTP.
// Engine: engines/saas_subscription.go, saas_package_transition.go, saas_metering.go.

// handleSaaSSubscriptionWebhook takes Razorpay's subscription.* webhooks for
// the platform's own account (public). The signature is checked against the
// billing client's Razorpay webhook secret; a repeat of an event id is
// answered, not redone.
func handleSaaSSubscriptionWebhook(w http.ResponseWriter, r *http.Request) {
	billing := engines.SaaSBillingTenant()
	body, err := io.ReadAll(io.LimitReader(r.Body, engines.InboundWebhookMaxBody))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Unreadable body")
		return
	}
	if !engines.VerifyWebhookHMACAny(body, r.Header.Get("X-Razorpay-Signature"), "", "hex", engines.GetChannelWebhookSecrets(billing, engines.RazorpayCredentialCode)...) {
		refuseWebhookSignature(w, r, "razorpay-subscription")
		return
	}
	result, err := engines.HandleSaaSSubscriptionWebhook(engines.SaaSEventID(r.Header.Get("X-Razorpay-Event-Id"), body), body)
	if err != nil {
		engines.LogSystemError(billing, r.Header.Get("Resolved-Correlation-ID"), "WARN", r.URL.Path, fmt.Sprintf("saas subscription webhook: %v", err), "")
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "result": result})
}

// handlePackageImpact previews a package change: modules gained and lost and
// the open work left frozen. Nothing changes.
func handlePackageImpact(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	var req struct {
		TenantID string   `json:"tenant_id"`
		Packages []string `json:"packages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TenantID == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'tenant_id' is required")
		return
	}
	imp, err := engines.ImpactOfPackages(req.TenantID, req.Packages)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(imp)
}

// handleMySubscription is the client's own view: where the subscription
// stands, in plain words, and this month's usage and cost.
func handleMySubscription(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	sub, err := engines.GetSaaSSubscription(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	st, err := engines.SaaSUsageStatement(tenantID, engines.SaaSUsagePeriod(time.Now()))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	invoices, err := engines.ListSaaSInvoices(tenantID, 50)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"subscription": sub, "usage": st, "invoices": invoices})
}

func handleSaaSPlans(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		var p engines.SaaSPlan
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid plan")
			return
		}
		if err := engines.UpsertSaaSPlan(p); err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		return
	}
	plan, err := engines.GetSaaSPlan(r.URL.Query().Get("plan_key"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(plan)
}

func handleSaaSSubscribe(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	var req struct {
		TenantID string `json:"tenant_id"`
		PlanKey  string `json:"plan_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TenantID == "" || req.PlanKey == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'tenant_id' and 'plan_key' are required")
		return
	}
	sub, err := engines.StartSaaSSubscription(r.Context(), req.TenantID, req.PlanKey, credentialActor(r))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(sub)
}

func handleSaaSReconcile(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	drift, err := engines.ReconcileSaaSSubscriptions(r.Context(), r.URL.Query().Get("provider") == "1")
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"drift": drift})
}

func handleSaaSCosts(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformOperator(w, r) {
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = engines.SaaSUsagePeriod(time.Now())
	}
	costs, err := engines.SaaSTenantCosts(period)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(costs)
}
