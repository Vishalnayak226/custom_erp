package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Stage 130.16 (74.4, the e-way bill side): the E-Way Bills workbench. The
// list refreshes the queue (decides new documents, marks lapsed bills
// Expired, reconciles with the provider); each action works on one
// document's bill and needs update rights on that document.

var ewbDoctypeAllowed = map[string]bool{engines.DeliveryChallanDoctype: true, "SalesInvoice": true, "TransferOrder": true}

func handleListEWBs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if err := engines.RefreshEWBQueue(tenantID); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	rows, counts, err := engines.ListEWBs(tenantID, r.URL.Query().Get("tab"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	role := r.Header.Get("Resolved-Role")
	visible := []engines.EWBRecord{}
	readable := map[string]bool{}
	for _, rec := range rows {
		ok, seen := readable[rec.Doctype]
		if !seen {
			allowed, err := checkPermission(tenantID, role, rec.Doctype, "read")
			ok = err == nil && allowed
			readable[rec.Doctype] = ok
		}
		if ok {
			visible = append(visible, rec)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": visible, "counts": counts})
}

func handleEWBAction(w http.ResponseWriter, r *http.Request) {
	doctype, id, action := r.PathValue("doctype"), r.PathValue("id"), r.PathValue("action")
	if !ewbDoctypeAllowed[doctype] {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "That record does not carry an e-way bill")
		return
	}
	if !requireDocPermission(w, r, doctype, "update", "You do not have permission to change this document's e-way bill") {
		return
	}
	var req struct {
		engines.EWBPartB
		EWBNo       string `json:"ewb_no"`
		ValidUpto   string `json:"valid_upto"`
		RemainingKm int    `json:"remaining_km"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	var rec *engines.EWBRecord
	var err error
	switch action {
	case "decide":
		rec, err = engines.EnsureEWBRecord(tenantID, doctype, id)
	case "generate":
		rec, err = engines.GenerateEWB(tenantID, doctype, id, userID, req.EWBPartB)
	case "record":
		until, perr := time.ParseInLocation("2006-01-02T15:04", strings.TrimSpace(req.ValidUpto), time.Local)
		if perr != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Enter the e-way bill's valid-until date and time")
			return
		}
		rec, err = engines.RecordPortalEWB(tenantID, doctype, id, userID, req.EWBNo, until, req.EWBPartB)
	case "vehicle":
		rec, err = engines.UpdateEWBVehicle(tenantID, doctype, id, userID, req.EWBPartB)
	case "extend":
		rec, err = engines.ExtendEWB(tenantID, doctype, id, userID, req.RemainingKm, req.Reason)
	case "cancel":
		rec, err = engines.CancelEWB(tenantID, doctype, id, userID, req.Reason)
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown e-way bill action")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rec)
}
