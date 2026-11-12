package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 148.19: bills with no PO. The bill is made here, never by a plain save.

// expenseBillAllowed is the grant the document route asks for on a vendor bill.
func expenseBillAllowed(w http.ResponseWriter, r *http.Request, action string) bool {
	allowed, err := checkPermission(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role"), "VendorInvoice", action)
	if err != nil || !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return false
	}
	return true
}

func handleCreateExpenseBill(w http.ResponseWriter, r *http.Request) {
	if !expenseBillAllowed(w, r, "create") {
		return
	}
	var req engines.ExpenseBillInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'vendor_id', 'invoice_number' and 'lines' are required")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	scope, err := engines.ResolveSessionScope(tenantID, "VendorInvoice", engines.SessionScope{Role: r.Header.Get("Resolved-Role"), UserID: r.Header.Get("Resolved-User-ID"), LocationCode: r.Header.Get("Resolved-Location")})
	if err != nil {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	if named := engines.LocationOutOfScope("VendorInvoice", scope, map[string]interface{}{"location": req.Location}); named != "" {
		writeAPIErrorDetail(w, r, "GLOBAL-0011", "", engines.LocationOutOfScopeMessage(named, scope.LocationCode))
		return
	}
	res, err := engines.CreateExpenseBill(tenantID, r.Header.Get("Resolved-User-ID"), req)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleLinkExpenseBill(w http.ResponseWriter, r *http.Request) {
	if !expenseBillAllowed(w, r, "update") {
		return
	}
	var req struct {
		POID string `json:"po_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.POID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'po_id' is required")
		return
	}
	if err := engines.LinkExpenseBillToPO(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.PathValue("id"), strings.TrimSpace(req.POID)); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "linked"})
}

func handleListExpenseBills(w http.ResponseWriter, r *http.Request) {
	rows, err := engines.ListExpenseBills(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The bills could not be listed")
		return
	}
	tenant := r.Header.Get("Resolved-Tenant-ID")
	accounts, _ := engines.ListExpenseAccounts(tenant)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"bills": rows, "accounts": accounts, "enabled": engines.GetSettingBool(tenant, engines.SettingExpenseBills)})
}
