package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 148.31 / 148.32: summary invoices and drop-ship (module wholesale).
// Role comes from each route's capability row. These are head-office actions:
// a session bound to one store is refused unless its role has full access, so a store user cannot bill or
// confirm for the whole company.

func wholesaleHeadOffice(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Resolved-Location") != "" && !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "This is a head-office action. A store session cannot run it.")
		return false
	}
	return true
}

func handlePreviewSummaryInvoices(w http.ResponseWriter, r *http.Request) {
	if !wholesaleHeadOffice(w, r) {
		return
	}
	q := r.URL.Query()
	res, err := engines.PreviewSummaryInvoices(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(q.Get("from")), strings.TrimSpace(q.Get("to")), strings.TrimSpace(q.Get("customer_id")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handleCreateSummaryInvoices(w http.ResponseWriter, r *http.Request) {
	if !wholesaleHeadOffice(w, r) {
		return
	}
	var req struct {
		From       string   `json:"from"`
		To         string   `json:"to"`
		CustomerID string   `json:"customer_id"`
		Keys       []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Fields 'from' and 'to' are required")
		return
	}
	ids, err := engines.CreateSummaryInvoices(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), strings.TrimSpace(req.From), strings.TrimSpace(req.To), strings.TrimSpace(req.CustomerID), req.Keys)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"invoices": ids})
}

func handleOpenDropShipLines(w http.ResponseWriter, r *http.Request) {
	if !wholesaleHeadOffice(w, r) {
		return
	}
	rows, err := engines.OpenDropShipLines(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(r.URL.Query().Get("vendor_id")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"lines": rows})
}

func handleConfirmDropShip(w http.ResponseWriter, r *http.Request) {
	if !wholesaleHeadOffice(w, r) {
		return
	}
	var req struct {
		LineIDs  []string `json:"line_ids"`
		All      bool     `json:"all"`
		VendorID string   `json:"vendor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Send 'line_ids' or 'all'")
		return
	}
	res, err := engines.ConfirmDropShip(r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID"), req.LineIDs, req.All, strings.TrimSpace(req.VendorID))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}
