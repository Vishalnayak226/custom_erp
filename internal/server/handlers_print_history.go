package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
)

// Stage 127.6 (user, 2026-10-21: "I need reprint setion for all soort of
// preint and if will limit client to client if i give less moduel access to
// them"): every document print is recorded, and Setup > Printing > Reprint
// lists them - decided (user, 2026-10-21): only for the modules the client
// has, and only the documents the person may read.

// handlePrintDocumentLog records one Print / PDF / Send / Reprint of a
// document. Reading the document is what printing it needs.
func handlePrintDocumentLog(w http.ResponseWriter, r *http.Request) {
	txn := r.PathValue("txn")
	doctype := printTxnDoctype(r, txn)
	if doctype == "" {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown print transaction")
		return
	}
	if !requireDocPermission(w, r, doctype, "read", "You do not have permission to view this document") {
		return
	}
	var req struct {
		Action        string `json:"action"`
		Number        string `json:"number"`
		Title         string `json:"title"`
		Store         string `json:"store"`
		ReprintReason string `json:"reprint_reason"`
		Printer       string `json:"printer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	err := engines.RecordDocumentPrint(r.Header.Get("Resolved-Tenant-ID"), firstNonBlank(r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-User-ID")), engines.DocumentPrint{
		Txn: txn, Doctype: doctype, DocID: r.PathValue("id"), Number: req.Number, Title: req.Title, Store: req.Store,
		Action: strings.TrimSpace(req.Action), ReprintReason: req.ReprintReason, Printer: req.Printer,
	})
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"recorded": true})
}

// handleDocumentPrintHistory lists document prints, newest first: ?doctype= one
// transaction, ?q= a number or text.
func handleDocumentPrintHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	inModules, err := engines.EnabledModuleDoctypes(tenantID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	rows, err := engines.ListDocumentPrints(tenantID, engines.DocumentPrintFilter{Doctype: q.Get("doctype"), Query: q.Get("q"), Limit: 500})
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "Could not load the print history")
		return
	}
	readable := map[string]bool{}
	out := []engines.DocumentPrint{}
	for _, p := range rows {
		ok, seen := readable[p.Doctype]
		if !seen {
			allowed, err := checkPermission(tenantID, role, p.Doctype, "read")
			ok = err == nil && allowed && inModules[p.Doctype]
			readable[p.Doctype] = ok
		}
		if ok {
			out = append(out, p)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
