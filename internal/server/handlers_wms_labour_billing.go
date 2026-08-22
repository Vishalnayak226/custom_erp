package server

import (
	"custom_erp/engines"
	"encoding/json"
	"io"
	"net/http"
)

// Stage 42.6's configurable masters use the generic document endpoints. This
// file contains only the operational verbs whose inputs must go through the
// calculation/capture engine rather than letting a generic document write
// alter an immutable billable event.

func handleLaborPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	start, end := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if start == "" || end == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'start' and 'end' are required")
		return
	}
	rows, err := engines.GetLaborPlan(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("location_code"), start, end)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
}

func handleCapturedCharges(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	switch r.Method {
	case http.MethodGet:
		rows, err := engines.ListCapturedCharges(tenantID, r.URL.Query().Get("owner_id"), r.URL.Query().Get("start"), r.URL.Query().Get("end"), r.URL.Query().Get("status"))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
	case http.MethodPost:
		var req struct {
			EventKey     string  `json:"event_key"`
			ChargeCode   string  `json:"charge_code"`
			OwnerID      string  `json:"owner_id"`
			LocationCode string  `json:"location_code"`
			Quantity     float64 `json:"quantity"`
			OccurredOn   string  `json:"occurred_on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EventKey == "" || req.ChargeCode == "" || req.OwnerID == "" || req.OccurredOn == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "event_key, charge_code, owner_id and occurred_on are required")
			return
		}
		id, err := engines.CaptureManualCharge(tenantID, req.EventKey, req.ChargeCode, req.OwnerID, req.LocationCode, req.Quantity, req.OccurredOn, userID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "status": "Captured"})
	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleCapturedChargeInvoice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OwnerID string `json:"owner_id"`
		Start   string `json:"start"`
		End     string `json:"end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OwnerID == "" || req.Start == "" || req.End == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "owner_id, start and end are required")
		return
	}
	result, err := engines.GenerateInvoiceFromCapturedCharges(r.Header.Get("Resolved-Tenant-ID"), req.OwnerID, req.Start, req.End, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleStorageBalanceSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		SnapshotDate string `json:"snapshot_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid snapshot request")
		return
	}
	count, err := engines.CaptureStorageBalanceSnapshot(r.Header.Get("Resolved-Tenant-ID"), req.SnapshotDate, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"captured": count})
}

func handleStorageBillingV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	start, end := r.URL.Query().Get("start"), r.URL.Query().Get("end")
	if start == "" || end == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Query parameters 'start' and 'end' are required")
		return
	}
	rows, err := engines.GetStorageBillingV2(r.Header.Get("Resolved-Tenant-ID"), r.URL.Query().Get("owner_id"), start, end)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
}

func handleCaptureStorageCharges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		OwnerID string `json:"owner_id"`
		Start   string `json:"start"`
		End     string `json:"end"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Start == "" || req.End == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "start and end are required")
		return
	}
	count, err := engines.CaptureStorageCharges(r.Header.Get("Resolved-Tenant-ID"), req.OwnerID, req.Start, req.End, r.Header.Get("Resolved-User-ID"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"captured": count})
}
