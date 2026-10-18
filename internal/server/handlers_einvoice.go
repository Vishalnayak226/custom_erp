package server

import (
	"context"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Stage 75.2.9: e-invoice (IRN) - Tax Integration Settings (admin) and the IRN
// Exception Workbench (the tax role). Engines: engines/einvoice*.go.

func einvoiceRecordID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown e-invoice record")
		return 0, false
	}
	return id, true
}

// GET /api/v1/einvoice/profiles
func handleListEInvoiceProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := engines.ListIRPProfiles(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"environment": engines.IRPEnvironment(), "profiles": profiles})
}

// POST /api/v1/einvoice/profiles - create or change a GSTIN's set-up. The
// credentials, when sent, are stored encrypted and never returned.
func handleSaveEInvoiceProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		engines.IRPProfile
		Credentials map[string]string `json:"credentials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	creds := map[string]string{}
	for k, v := range req.Credentials {
		if v = strings.TrimSpace(v); v != "" {
			creds[k] = v
		}
	}
	p, err := engines.SaveIRPProfile(r.Header.Get("Resolved-Tenant-ID"), req.IRPProfile, creds, r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

// POST /api/v1/einvoice/profiles/{gstin}/test - Health + sign-in.
func handleTestEInvoiceProfile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p, err := engines.TestIRPConnection(ctx, r.Header.Get("Resolved-Tenant-ID"), r.PathValue("gstin"), r.Header.Get("Resolved-Username"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

// GET /api/v1/einvoice/documents?tab=&gstin=&from=&to=&source=&q=&min_age=&limit=&offset=
func handleListEInvoiceDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := engines.EInvoiceFilter{Tab: q.Get("tab"), GSTIN: q.Get("gstin"), From: q.Get("from"), To: q.Get("to"), Source: q.Get("source"), Search: strings.TrimSpace(q.Get("q"))}
	f.MinAgeMinutes, _ = strconv.Atoi(q.Get("min_age"))
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	rows, total, err := engines.ListEInvoiceRecords(tenantID, f)
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	counts := map[string]int{}
	for tab := range engines.EInvoiceWorkbenchTabs {
		if _, n, err := engines.ListEInvoiceRecords(tenantID, engines.EInvoiceFilter{Tab: tab, GSTIN: f.GSTIN, Limit: 1}); err == nil {
			counts[tab] = n
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows, "total": total, "counts": counts})
}

// GET /api/v1/einvoice/documents/{id}
func handleGetEInvoiceDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := einvoiceRecordID(w, r)
	if !ok {
		return
	}
	d, err := engines.GetEInvoiceRecordDetail(r.Header.Get("Resolved-Tenant-ID"), id)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown e-invoice record")
		return
	}
	_ = json.NewEncoder(w).Encode(d)
}

// POST /api/v1/einvoice/documents/{id}/{action} - validate, submit, check,
// cancel {code, reason}, manual-import {irn, ack_no, ack_date, signed_qr, reason}.
func handleEInvoiceDocumentAction(w http.ResponseWriter, r *http.Request) {
	id, ok := einvoiceRecordID(w, r)
	if !ok {
		return
	}
	tenantID, user, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-Role")
	var req struct {
		Code     string `json:"code"`
		Reason   string `json:"reason"`
		IRN      string `json:"irn"`
		AckNo    string `json:"ack_no"`
		AckDate  string `json:"ack_date"`
		SignedQR string `json:"signed_qr"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
			return
		}
	}
	var rec engines.IRPRecord
	var err error
	switch r.PathValue("action") {
	case "validate":
		rec, err = engines.RevalidateEInvoice(tenantID, id, user)
	case "submit", "check":
		rec, err = engines.SubmitEInvoice(tenantID, id, user)
	case "cancel":
		rec, err = engines.RequestIRNCancellation(tenantID, id, req.Code, req.Reason, user, role)
	case "manual-import":
		rec, err = engines.RequestIRNManualImport(tenantID, id, req.IRN, req.AckNo, req.AckDate, req.SignedQR, req.Reason, user, role)
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown e-invoice action")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(rec)
}

// GET /api/v1/einvoice/badges/{doctype}[?id=] - the IRN state of a list's
// invoices or credit notes, one call per screen; with ?id=, that document's,
// with its signed QR, for its print.
func handleEInvoiceBadges(w http.ResponseWriter, r *http.Request) {
	badges, err := engines.EInvoiceBadges(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("doctype"), strings.TrimSpace(r.URL.Query().Get("id")))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(badges)
}

// GET /api/v1/einvoice/reconciliation
func handleEInvoiceReconciliation(w http.ResponseWriter, r *http.Request) {
	sums, err := engines.ReconcileEInvoices(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(sums)
}

// POST /api/v1/einvoice/run - the workbench's "Check now": one worker pass for
// this tenant (registers newly posted documents, works the due queue). It
// sends nothing that was not already queued.
func handleRunEInvoiceWorker(w http.ResponseWriter, r *http.Request) {
	if err := engines.RunEInvoiceWorkerOnce(r.Header.Get("Resolved-Tenant-ID")); err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "done"})
}
