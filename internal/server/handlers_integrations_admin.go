package server

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"net/http"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 9 integrations (Unicommerce, Pine Labs, CleverTap), integration
// event log query/retry, SaaS tenant provisioning, feature flags, module
// governance/entitlements, version endpoints, patch-proposal decisions, and
// the 3rd-party extension hook/token admin screens.

func handleUnicommerceCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure Unicommerce credentials"})
		return
	}
	var req struct {
		StoreCode string `json:"store_code"`
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
		BaseURL   string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.StoreCode == "" || req.APIKey == "" || req.APISecret == "" || req.BaseURL == "" {
		http.Error(w, "Fields 'store_code', 'api_key', 'api_secret', and 'base_url' are required", http.StatusBadRequest)
		return
	}
	if err := engines.SaveUnicommerceCredential(tenantID, req.StoreCode, req.APIKey, req.APISecret, req.BaseURL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "store_code": req.StoreCode})
}

func handleGetUnicommerceCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	creds, err := engines.GetUnicommerceCredentials(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(creds)
}

func handleUnicommerceOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ChannelOrderID string `json:"channel_order_id"`
		StoreCode      string `json:"store_code"`
		Items          []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ChannelOrderID == "" || req.StoreCode == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'channel_order_id', 'store_code', and 'items' are required", http.StatusBadRequest)
		return
	}
	var items []map[string]interface{}
	for _, item := range req.Items {
		items = append(items, map[string]interface{}{
			"sku": item.Sku,
			"qty": item.Qty,
		})
	}
	orderID, err := engines.ImportUnicommerceOrder(tenantID, req.ChannelOrderID, req.StoreCode, items)
	if err != nil {
		if err.Error() == "ORDER_ALREADY_IMPORTED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "details": "Order already imported (idempotency check)"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "imported", "order_id": orderID})
}

func handleListUnicommerceOrders(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	orders, err := engines.ListUnicommerceOrders(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if orders == nil {
		orders = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(orders)
}

func handleListUnicommerceInventorySyncs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	syncs, err := engines.ListUnicommerceInventorySyncs(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if syncs == nil {
		syncs = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(syncs)
}

// =========================================================================
// Stage 9.1: Pine Labs Plutus Integration Handlers
// =========================================================================

func handlePineLabsCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure Pine Labs credentials"})
		return
	}
	var req struct {
		TerminalID string `json:"terminal_id"`
		APIKey     string `json:"api_key"`
		MerchantID string `json:"merchant_id"`
		BaseURL    string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TerminalID == "" || req.APIKey == "" || req.MerchantID == "" || req.BaseURL == "" {
		http.Error(w, "Fields 'terminal_id', 'api_key', 'merchant_id', and 'base_url' are required", http.StatusBadRequest)
		return
	}
	if err := engines.SavePineLabsCredential(tenantID, req.TerminalID, req.APIKey, req.MerchantID, req.BaseURL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "terminal_id": req.TerminalID})
}

func handleGetPineLabsCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	creds, err := engines.GetPineLabsCredentials(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(creds)
}

func handlePineLabsTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TransactionID string  `json:"transaction_id"`
		TerminalID    string  `json:"terminal_id"`
		CartNumber    string  `json:"cart_number"`
		Amount        float64 `json:"amount"`
		PaymentMode   string  `json:"payment_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TransactionID == "" || req.TerminalID == "" || req.CartNumber == "" || req.Amount <= 0 {
		http.Error(w, "Fields 'transaction_id', 'terminal_id', 'cart_number', and positive 'amount' are required", http.StatusBadRequest)
		return
	}
	paymentMode := req.PaymentMode
	if paymentMode == "" {
		paymentMode = "Card"
	}
	if err := engines.RecordPineLabsTransaction(tenantID, req.TransactionID, req.TerminalID, req.CartNumber, req.Amount, paymentMode); err != nil {
		if err.Error() == "TRANSACTION_ALREADY_RECORDED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "details": "Transaction already recorded"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "recorded", "transaction_id": req.TransactionID})
}

func handlePineLabsReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can run Pine Labs reconciliation"})
		return
	}
	result, err := engines.ReconcilePineLabsTransactions(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleListPineLabsTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	txns, err := engines.ListPineLabsTransactions(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if txns == nil {
		txns = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(txns)
}

// =========================================================================
// Stage 9.1: CleverTap Integration Handlers
// =========================================================================

func handleCleverTapCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure CleverTap credentials"})
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
		Passcode  string `json:"passcode"`
		Region    string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.AccountID == "" || req.Passcode == "" {
		http.Error(w, "Fields 'account_id' and 'passcode' are required", http.StatusBadRequest)
		return
	}
	region := req.Region
	if region == "" {
		region = "in1"
	}
	if err := engines.SaveCleverTapCredential(tenantID, req.AccountID, req.Passcode, region); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "account_id": req.AccountID})
}

func handleGetCleverTapCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	creds, err := engines.GetCleverTapCredentials(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(creds)
}

func handleListCleverTapLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	logs, err := engines.ListCleverTapEventLogs(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(logs)
}

func handleGetIntegrationLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	logs, err := engines.GetIntegrationLogs(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleRetryIntegrationEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EventID string `json:"event_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid retry payload", http.StatusBadRequest)
		return
	}

	if req.EventID == "" {
		http.Error(w, "Field 'event_id' is required", http.StatusBadRequest)
		return
	}

	err := engines.RetryIntegrationEvent(tenantID, req.EventID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "queued_for_retry",
		"event_id": req.EventID,
	})
}

func handleProvisionTenant(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can provision new tenants"})
		return
	}

	var req struct {
		TenantID   string `json:"tenant_id"`
		SchemaName string `json:"schema_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid provisioning payload", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" || req.SchemaName == "" {
		http.Error(w, "Fields 'tenant_id' and 'schema_name' are required", http.StatusBadRequest)
		return
	}

	adminPassword, err := engines.ProvisionTenantSchema(req.TenantID, req.SchemaName, currentAppVersion())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":              "provisioned",
		"tenant_id":           req.TenantID,
		"schema_name":         req.SchemaName,
		"admin_username":      "admin",
		"admin_password":      adminPassword,
		"admin_password_note": "Shown once - store it securely now. It is not persisted in plaintext anywhere and cannot be retrieved again.",
	})
}

func handleSetFeatureFlag(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can modify feature flags"})
		return
	}

	var req struct {
		FeatureName string `json:"feature_name"`
		Enabled     bool   `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid feature-flag payload", http.StatusBadRequest)
		return
	}

	if req.FeatureName == "" {
		http.Error(w, "Field 'feature_name' is required", http.StatusBadRequest)
		return
	}

	err := engines.SetFeatureFlag(tenantID, req.FeatureName, req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "updated",
		"feature_name": req.FeatureName,
		"enabled":      req.Enabled,
	})
}

// handleListModules returns the global module catalog (tenant-independent) -
// what modules exist at all, regardless of any tenant's entitlements.
func handleListModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view the module catalog"})
		return
	}

	modules, err := engines.ListModules()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(modules)
}

// handleGetModuleEntitlements returns the module catalog joined with the
// resolved tenant's current enabled/disabled state for each module.
func handleGetModuleEntitlements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view module entitlements"})
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = r.Header.Get("Resolved-Tenant-ID")
	}

	entitlements, err := engines.ListModuleEntitlements(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(entitlements)
}

// handleSetModuleEntitlement enables or disables one module for a tenant.
// Core modules are rejected server-side by engines.SetModuleEntitlement -
// this handler just surfaces that error as a 400 rather than re-checking it.
func handleSetModuleEntitlement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can modify module entitlements"})
		return
	}

	var req struct {
		TenantID  string `json:"tenant_id"`
		ModuleKey string `json:"module_key"`
		Enabled   bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid module-entitlement payload", http.StatusBadRequest)
		return
	}
	if req.ModuleKey == "" {
		http.Error(w, "Field 'module_key' is required", http.StatusBadRequest)
		return
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = r.Header.Get("Resolved-Tenant-ID")
	}

	grantedBy := r.Header.Get("Resolved-Username")
	if err := engines.SetModuleEntitlement(tenantID, req.ModuleKey, req.Enabled, grantedBy); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "updated",
		"tenant_id":  tenantID,
		"module_key": req.ModuleKey,
		"enabled":    req.Enabled,
	})
}

// handleVersion (Stage 14.6) reports the running binary's build identity.
// Public (see publicRoutes) - an ops tool or client should be able to check
// what's running without authenticating first, same as any /version
// endpoint elsewhere.
func handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"version":    currentAppVersion(),
		"git_commit": gitCommit,
		"build_time": buildTime,
	})
}

// handleGetTenantVersion (Stage 14.6) surfaces what version was recorded
// against a tenant's schema at last provision/promotion time, alongside the
// live binary's own version, so a mismatch is visible from the API without
// SSH'ing into an instance. app_version here is a point-in-time compat/audit
// record, not live per-request dispatch - one running process can only ever
// serve one binary version, regardless of which tenant is asking.
func handleGetTenantVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view tenant version records"})
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = r.Header.Get("Resolved-Tenant-ID")
	}
	if tenantID == "" || tenantID == "default" {
		tenantID = "default"
	}

	var recordedVersion, pinnedVersion sql.NullString
	err := db.DB.QueryRow(`SELECT app_version, pinned_version FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&recordedVersion, &pinnedVersion)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unknown tenant_id"})
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	liveVersion := currentAppVersion()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tenant_id":        tenantID,
		"recorded_version": recordedVersion.String,
		"pinned_version":   pinnedVersion.String,
		"live_version":     liveVersion,
		"mismatch":         recordedVersion.Valid && recordedVersion.String != liveVersion,
	})
}

// handleListPatchProposals (Stage 14.13-14.16) lists patch-intake proposals,
// optionally filtered by status (defaults to "pending" - the queue an admin
// actually needs to act on; pass ?status=all to see everything).
func handleListPatchProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view patch proposals"})
		return
	}

	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	if status == "all" {
		status = ""
	}

	proposals, err := engines.ListPatchProposals(status)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if proposals == nil {
		proposals = []engines.PatchProposal{}
	}
	_ = json.NewEncoder(w).Encode(proposals)
}

// decidePatchProposalRequest is the shared payload shape for both
// approve/reject - kept as two distinct routes (rather than one route with
// a decision field) so each action's intent is explicit in the URL, the
// same convention handleSetFeatureFlag/handleSetModuleEntitlement don't
// need but approval/rejection - a one-way decision - benefits from.
type decidePatchProposalRequest struct {
	ProposalID int    `json:"proposal_id"`
	Notes      string `json:"notes"`
}

func handleApprovePatchProposal(w http.ResponseWriter, r *http.Request) {
	decidePatchProposal(w, r, "approved")
}

func handleRejectPatchProposal(w http.ResponseWriter, r *http.Request) {
	decidePatchProposal(w, r, "rejected")
}

// decidePatchProposal records a human decision only - it never takes any
// further action itself. See engines/patchintake.go's package doc comment
// for why that boundary is deliberate: applying a real fix (a
// module-entitlement toggle, a code change promoted via promote.ps1) stays
// a separate, manual step using the tools already built in Phases A/C.
func decidePatchProposal(w http.ResponseWriter, r *http.Request, decision string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can decide patch proposals"})
		return
	}

	var req decidePatchProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid patch-proposal decision payload", http.StatusBadRequest)
		return
	}
	if req.ProposalID == 0 {
		http.Error(w, "Field 'proposal_id' is required", http.StatusBadRequest)
		return
	}

	decidedBy := r.Header.Get("Resolved-Username")
	if err := engines.DecidePatchProposal(req.ProposalID, decision, decidedBy, req.Notes); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "updated",
		"proposal_id": req.ProposalID,
		"decision":    decision,
	})
}

// handleCreateExtensionHook registers a new hook and returns its secret -
// shown exactly once, same "generated + never retrievable again" pattern
// tenant admin passwords already use (engines.ProvisionTenantSchema).
func handleCreateExtensionHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can register extension hooks"})
		return
	}

	var req struct {
		HookPoint string `json:"hook_point"`
		Doctype   string `json:"doctype"`
		TargetURL string `json:"target_url"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid extension-hook payload", http.StatusBadRequest)
		return
	}
	if req.Doctype == "" {
		http.Error(w, "Field 'doctype' is required (use '*' to match every doctype)", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	createdBy := r.Header.Get("Resolved-Username")
	id, secret, err := engines.RegisterExtensionHook(tenantID, req.HookPoint, req.Doctype, req.TargetURL, req.TimeoutMs, createdBy)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":          id,
		"secret":      secret,
		"secret_note": "Shown once - store it securely now. It is not persisted in plaintext anywhere and cannot be retrieved again.",
	})
}

func handleListExtensionHooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view extension hooks"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	hooks, err := engines.ListExtensionHooks(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if hooks == nil {
		hooks = []engines.ExtensionHook{}
	}
	_ = json.NewEncoder(w).Encode(hooks)
}

func handleDeleteExtensionHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can delete extension hooks"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	hookID := r.PathValue("id")
	if err := engines.DeleteExtensionHook(tenantID, hookID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": hookID})
}

func handleGetExtensionHookLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view extension hook logs"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	hookID := r.PathValue("id")
	logEntries, err := engines.GetExtensionHookLog(tenantID, hookID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if logEntries == nil {
		logEntries = []engines.ExtensionHookLogEntry{}
	}
	_ = json.NewEncoder(w).Encode(logEntries)
}

// handleIssueExtensionToken issues a tenant-and-doctype-scoped token for a
// 3rd-party developer's extension code to call back into this API with -
// the only credential it ever receives, alongside the inbound hook payload
// itself. Never the core repo, never a full session, never another tenant.
func handleIssueExtensionToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can issue extension tokens"})
		return
	}

	var req struct {
		ScopeDoctype string `json:"scope_doctype"`
		TTLMinutes   int    `json:"ttl_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid extension-token payload", http.StatusBadRequest)
		return
	}
	if req.ScopeDoctype == "" {
		http.Error(w, "Field 'scope_doctype' is required", http.StatusBadRequest)
		return
	}
	ttl := time.Duration(req.TTLMinutes) * time.Minute
	if req.TTLMinutes <= 0 || ttl > 24*time.Hour {
		ttl = time.Hour
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	token := engines.SignExtensionToken(tenantID, req.ScopeDoctype, ttl)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":              token,
		"scope_doctype":      req.ScopeDoctype,
		"expires_in_minutes": int(ttl.Minutes()),
	})
}
