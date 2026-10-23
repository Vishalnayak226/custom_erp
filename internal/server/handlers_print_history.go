package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
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
		// Stage 130.16 (74.1.4).
		Copies     int      `json:"copies"`
		CopyLabels []string `json:"copy_labels"`
		TemplateID string   `json:"template_id"`
		ReasonCode string   `json:"reason_code"`
		ApprovalID string   `json:"approval_id"`
		Channel    string   `json:"channel"`
		Check      bool     `json:"check"` // reprint control's check before printing; nothing is logged
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	mayReprint, _ := checkPermission(tenantID, r.Header.Get("Resolved-Role"), engines.DocumentReprintDoctype, "create")
	err := engines.RecordDocumentPrint(tenantID, firstNonBlank(r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-User-ID")), engines.DocumentPrint{
		Txn: txn, Doctype: doctype, DocID: r.PathValue("id"), Number: req.Number, Title: req.Title, Store: req.Store,
		Action: strings.TrimSpace(req.Action), ReprintReason: req.ReprintReason, Printer: req.Printer,
		Copies: req.Copies, CopyLabels: strings.Join(req.CopyLabels, " / "), TemplateID: req.TemplateID, ReasonCode: req.ReasonCode,
		ApprovalID: req.ApprovalID, Channel: req.Channel, MayReprint: mayReprint, CheckOnly: req.Check,
	})
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"recorded": true})
}

// handleRequestReprint (Stage 130.16, 74.1.4) sends a reprint to its
// approver when reprint control asks for one.
func handleRequestReprint(w http.ResponseWriter, r *http.Request) {
	txn := r.PathValue("txn")
	doctype := printTxnDoctype(r, txn)
	if doctype == "" {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown print transaction")
		return
	}
	if !requireDocPermission(w, r, doctype, "read", "You do not have permission to view this document") ||
		!requireDocPermission(w, r, engines.ReprintRequestDoctype, "create", "You do not have permission to ask for a reprint") {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	id, err := engines.RequestReprint(r.Header.Get("Resolved-Tenant-ID"), txn, r.PathValue("id"), req.Reason,
		firstNonBlank(r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-User-ID")), r.Header.Get("Resolved-User-ID"), r.Header.Get("Resolved-Role"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "Pending Approval"})
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

// Stage 130.16 (74.3.4): a print template's versions - list, approve or
// reject a waiting change, restore an old one. Changing templates is what
// these need (PrintTemplate update).
func handlePrintTemplateVersions(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PrintTemplate", "read", "You do not have permission to view print templates") {
		return
	}
	rows, err := engines.ListPrintTemplateVersions(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func handlePrintTemplateVersionAction(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PrintTemplate", "update", "You do not have permission to change print templates") {
		return
	}
	var req struct {
		Action string `json:"action"` // approve, reject, restore
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	version, convErr := strconv.Atoi(r.PathValue("version"))
	if convErr != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Unknown version")
		return
	}
	tenantID, templateID, userID := r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), firstNonBlank(r.Header.Get("Resolved-Username"), r.Header.Get("Resolved-User-ID"))
	var err error
	out := map[string]interface{}{"ok": true}
	switch req.Action {
	case "approve", "reject":
		err = engines.DecidePrintTemplateVersion(tenantID, templateID, version, req.Action == "approve", userID, req.Note)
	case "restore":
		var n int
		n, err = engines.RestorePrintTemplateVersion(tenantID, templateID, version, userID)
		out["version"] = n
	default:
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Unknown action")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
