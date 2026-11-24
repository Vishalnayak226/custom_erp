package server

import (
	"custom_erp/engines"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Silent printing endpoints (Stage 31.1), used by the local print agent
// (PrintBridge, which speaks the QZ Tray protocol).
//
// The browser cannot hold the signing key, so these endpoints are what
// public/print-agent.js needs to talk to the agent:
//
//	GET  /api/v1/print/agent/certificate  - public cert, sent in the handshake
//	POST /api/v1/print/agent/sign         - signs one request hash
//	GET  /api/v1/print/agent/printers     - configured Printer Masters
//	POST /api/v1/print/agent/payload      - builds what to actually print
//	POST /api/v1/print/agent/log          - records the outcome
//	GET  /api/v1/print/agent/log          - print audit trail
//
// Each is also served under /api/v1/print/qz/ (the path before ask 263), so a
// page loaded before a deploy keeps printing.
//
// All of them sit behind apiMiddleware and the existing "stickers" module
// gate - the Printer doctype is already mapped to that key
// (db/migrations/migrations_stage14a_modules.sql), so no new module key is introduced.
// A new key would 403 every existing tenant until their plan was edited, the
// trap called out in db/migrations/migrations_stage30_7_pos_offers.sql.

func handleAgentCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	cert, err := engines.AgentCertificatePEM()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError,
			"Print signing is not configured on this server: "+err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"certificate": cert})
}

func handleAgentSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var req struct {
		Request string `json:"request"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	sig, err := engines.AgentSignRequest(req.Request)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"signature": sig,
		"algorithm": engines.AgentSignAlgorithm,
	})
}

func handleAgentPrinters(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	printers, err := engines.ListAgentPrinters(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(printers)
}

// handleAgentPrintPayload turns "print this thing" into the exact data array the
// print agent expects, choosing raw commands or a rasterised document based on the
// target printer's configured language.
//
// Deliberately one endpoint rather than one per document type: the browser
// then has a single call to make, and adding a new printable document later
// means one new case here instead of a new route, handler and client method.
func handleAgentPrintPayload(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}

	var req struct {
		JobType        string                         `json:"job_type"`
		DocumentRef    string                         `json:"document_ref"`
		PrinterCode    string                         `json:"printer_code"`
		Copies         int                            `json:"copies"`
		SKUs           []string                       `json:"skus"`
		Reprint        string                         `json:"reprint_reason"`
		DataBase64     string                         `json:"data_base64"`
		DocFormat      string                         `json:"doc_format"`
		SourceDoctype  string                         `json:"source_doctype"`
		SourceDocID    string                         `json:"source_doc_id"`
		CopiesOverride map[string]int                 `json:"copies_override"`
		Lines          []engines.StickerLineSelection `json:"lines"`
		RequestKey     string                         `json:"request_key"` // Stage 55.19
		// Stage 98.4: which computer is printing, for which store and kind of
		// print, so its own chosen printer is used.
		Computer  string `json:"computer"`
		Location  string `json:"location"`
		PrintKind string `json:"print_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if req.Copies < 1 {
		req.Copies = 1
	}

	printer, err := resolveAgentPrinter(tenantID, req.PrinterCode, req.JobType, req.PrintKind, req.Computer, req.Location)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}

	var payload *engines.AgentPrintPayload

	switch strings.TrimSpace(req.JobType) {
	case "Shipping Label":
		if req.DocumentRef == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'document_ref' is required for a shipping label")
			return
		}
		payload, err = engines.BuildShippingLabelPayload(tenantID, req.DocumentRef, printer.Language)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}

	case "Sticker":
		// Routed through PrintStickers/PrintStickersForDocument so the
		// existing SKU validation, DEVICE-0298 printer check and
		// sticker_print_log audit trail all still run - the agent changes how a
		// sticker reaches the printer, not the rules about what may be
		// printed.
		var labels []engines.StickerLabel
		var sErr error
		if req.SourceDoctype != "" && !stickerSourceReadable(w, r, req.SourceDoctype) {
			return
		}
		if req.SourceDoctype != "" && req.SourceDocID != "" {
			// Stage 52: bulk print from a GRN/Transfer Order - the selection
			// (if any) narrows the document's lines, which is what makes
			// "print the whole document" and "print one line" the same
			// request shape. Resolved by the same stickerSelections helper
			// the browser-fallback handler uses.
			labels, sErr = engines.PrintStickersForDocument(tenantID, req.SourceDoctype, req.SourceDocID, printer.Code, userID, req.Reprint, stickerSelections(req.Lines, req.SKUs, req.CopiesOverride), engines.StickerPrintOptions{RequestKey: req.RequestKey})
		} else {
			if len(req.SKUs) == 0 {
				writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'skus' is required for a sticker print")
				return
			}
			labels, sErr = engines.PrintStickers(tenantID, req.SKUs, printer.Code, userID, req.Reprint, req.Copies, engines.StickerPrintOptions{RequestKey: req.RequestKey})
		}
		if sErr != nil {
			writeEngineError(w, r, sErr, http.StatusUnprocessableEntity)
			return
		}
		payload = engines.BuildStickerPayload(labels, req.Copies, printer)
		if payload == nil {
			// Not a raw-command printer: let the browser fall back to the
			// existing @media print sheet, which already renders these.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fallback": "browser",
				"labels":   labels,
				"printer":  printer,
			})
			return
		}

	case "Document":
		if req.DataBase64 == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'data_base64' is required for a document print")
			return
		}
		// Strip a data: URI prefix if the caller pasted one - the agent's base64
		// flavor wants the payload bare, and this is an easy mistake to make
		// when the PDF came from a marketplace download link.
		data := req.DataBase64
		if i := strings.Index(data, ";base64,"); i != -1 {
			data = data[i+len(";base64,"):]
		}
		data = strings.Join(strings.Fields(data), "")
		if _, dErr := base64.StdEncoding.DecodeString(data); dErr != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'data_base64' is not valid base64")
			return
		}
		payload = engines.BuildPassThroughPayload(data, req.DocFormat, printer)

	case "Receipt":
		// Stage 31.1.9. document_ref is the cart number; the sale itself is
		// re-read from its POSCart document, so a reprint prints what was
		// actually rung up rather than whatever the browser still has in
		// memory.
		if req.DocumentRef == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'document_ref' is required for a receipt")
			return
		}
		payload, err = engines.BuildReceiptPayload(tenantID, req.DocumentRef, printer)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}

	case "Invoice":
		if req.DocumentRef == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'document_ref' is required for an invoice")
			return
		}
		payload, err = engines.BuildInvoicePayload(tenantID, req.DocumentRef, printer)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}

	case "Purchase Order":
		// Stage 40.10. document_ref is the PO's id or code; re-read and
		// re-priced server-side via BuildPurchaseOrderPrint exactly like the
		// browser fallback sheet, so a silent print cannot show a vendor a
		// total the app itself would compute differently.
		if req.DocumentRef == "" {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'document_ref' is required for a purchase order")
			return
		}
		payload, err = engines.BuildPurchaseOrderAgentPayload(tenantID, req.DocumentRef, printer)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}

	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity,
			"Unknown job_type - expected 'Shipping Label', 'Sticker', 'Receipt', 'Invoice', 'Purchase Order' or 'Document'")
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"printer": printer,
		"format":  payload.Format,
		"items":   payload.Items,
		"copies":  req.Copies,
	})
}

// resolveAgentPrinter picks the printer for a job, first match wins: an explicit
// printer_code; this computer's own printer for this kind of print (98.4,
// chosen once); an Active printer whose "Default For" is the kind at the
// job's own store (98.4.1 - with 100 stores, store A must never print on
// store B's printer); then one anywhere in the company, as before 98.4.
// Falling back to the role is what makes one-click printing possible - each
// bench is configured once and never asks again.
func resolveAgentPrinter(tenantID, printerCode, jobType, kind, computer, location string) (engines.AgentPrinter, error) {
	printers, err := engines.ListAgentPrinters(tenantID)
	if err != nil {
		return engines.AgentPrinter{}, err
	}

	if printerCode != "" {
		for _, p := range printers {
			if p.Code == printerCode || p.ID == printerCode {
				return p, nil
			}
		}
		return engines.AgentPrinter{}, &engines.ValidationError{
			Code:    "DEVICE-0298",
			Message: fmt.Sprintf("printer %q is not configured or is inactive", printerCode),
		}
	}

	kind = engines.PrintKindForJob(jobType, kind)
	if p, ok := engines.ChosenPrinter(tenantID, computer, kind); ok {
		return p, nil
	}
	roles := engines.PrintRolesForKind(kind)
	location = strings.TrimSpace(location)
	if location != "" {
		for _, p := range printers {
			if p.Location == location && containsStr(roles, p.PrintRole) {
				return p, nil
			}
		}
	}
	for _, p := range printers {
		if containsStr(roles, p.PrintRole) {
			return p, nil
		}
	}
	return engines.AgentPrinter{}, &engines.ValidationError{
		Code: "DEVICE-0298",
		Message: fmt.Sprintf("no printer is chosen for %q on this computer and no active printer is set as the default for it - "+
			"print once to choose one, or set 'Default For' on a Printer record", kind),
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// handleAgentPrinterChoices (Stage 98.4): GET lists a computer's printer per
// kind of print; POST remembers one ("ask one time ... then never ask"). Any
// signed-in user may save their own computer's choice - it is the print they
// are making - which makes the Printer record for that OS printer if needed.
func handleAgentPrinterChoices(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		choices, err := engines.PrinterChoices(tenantID, q.Get("computer"))
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		out := map[string]interface{}{"choices": choices, "kinds": engines.PrintKinds}
		// ?kind= also answers where a print of that kind would go now - this
		// computer's choice, else the store's default, else nowhere yet.
		if kind := q.Get("kind"); kind != "" {
			if p, err := resolveAgentPrinter(tenantID, "", "", kind, q.Get("computer"), q.Get("location")); err == nil {
				out["printer"] = p
			}
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	var body struct {
		Computer string `json:"computer"`
		Location string `json:"location"`
		Kind     string `json:"print_kind"`
		Printer  string `json:"os_printer_name"`
		// Legacy: the key a page loaded before ask 263 sends.
		LegacyPrinter string `json:"qz_printer_name"`
		Language      string `json:"printer_language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	if body.Printer == "" {
		body.Printer = body.LegacyPrinter
	}
	p, err := engines.ChoosePrinter(tenantID, r.Header.Get("Resolved-User-ID"), body.Computer, body.Location, body.Kind, body.Printer, body.Language)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"printer": p})
}

func handleAgentPrintLog(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		entries, err := engines.GetPrintJobLog(tenantID, limit)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(entries)

	case http.MethodPost:
		var req struct {
			JobType       string `json:"job_type"`
			DocumentRef   string `json:"document_ref"`
			PrinterCode   string `json:"printer_code"`
			OSPrinterName string `json:"os_printer_name"`
			// Legacy: the key a page loaded before ask 263 sends.
			LegacyPrinterName string `json:"qz_printer_name"`
			Format            string `json:"print_format"`
			Copies            int    `json:"copies"`
			Status            string `json:"status"`
			ErrorDetail       string `json:"error_detail"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
			return
		}
		if req.OSPrinterName == "" {
			req.OSPrinterName = req.LegacyPrinterName
		}
		if err := engines.LogPrintJob(tenantID, req.JobType, req.DocumentRef, req.PrinterCode,
			req.OSPrinterName, req.Format, req.Copies, userID, req.Status, req.ErrorDetail); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}
