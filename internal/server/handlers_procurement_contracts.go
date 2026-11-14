package server

import (
	"encoding/json"
	"net/http"

	"custom_erp/engines"
)

// Stage 168: blanket / rate contracts and service entry sheets. The role that
// may activate, close or accept is decided in the engine (the role that
// approves a purchase order of that value); these handlers only carry it.

func procurementActor(w http.ResponseWriter, r *http.Request) (tenantID, userID, role string, ok bool) {
	if refuseProcurementExtension(w, r) {
		return "", "", "", false
	}
	return r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"), true
}

func handleActivatePurchaseContract(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, role, ok := procurementActor(w, r)
	if !ok {
		return
	}
	if err := engines.ActivatePurchaseContract(tenantID, r.PathValue("id"), userID, role); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Active"})
}

func handleClosePurchaseContract(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, role, ok := procurementActor(w, r)
	if !ok {
		return
	}
	if err := engines.ClosePurchaseContract(tenantID, r.PathValue("id"), userID, role); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Closed"})
}

func handlePurchaseContractSummary(w http.ResponseWriter, r *http.Request) {
	if refuseProcurementExtension(w, r) {
		return
	}
	s, err := engines.PurchaseContractSummary(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(s)
}

func handleAcceptServiceEntrySheet(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, role, ok := procurementActor(w, r)
	if !ok {
		return
	}
	if err := engines.AcceptServiceEntrySheet(tenantID, r.PathValue("id"), userID, role); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "Accepted"})
}

func handleServiceBalance(w http.ResponseWriter, r *http.Request) {
	if refuseProcurementExtension(w, r) {
		return
	}
	lines, err := engines.ServiceBalanceForPO(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"lines": lines})
}

func refuseProcurementExtension(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Resolved-Purpose") == "extension" {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Extension tokens cannot use contracts or service entry")
		return true
	}
	return false
}
