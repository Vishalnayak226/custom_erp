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

// QZ Tray silent printing endpoints (Stage 31.1).
//
// The browser cannot hold the signing key, so these three endpoints are what
// public/qz-print.js needs to talk to a local QZ Tray instance:
//
//	GET  /api/v1/print/qz/certificate  - public cert, sent in the handshake
//	POST /api/v1/print/qz/sign         - signs one request hash
//	GET  /api/v1/print/qz/printers     - configured Printer Masters
//	POST /api/v1/print/qz/payload      - builds what to actually print
//	POST /api/v1/print/qz/log          - records the outcome
//	GET  /api/v1/print/qz/log          - print audit trail
//
// All of them sit behind apiMiddleware and the existing "stickers" module
// gate - the Printer doctype is already mapped to that key
// (db/migrations_stage14a_modules.sql), so no new module key is introduced.
// A new key would 403 every existing tenant until their plan was edited, the
// trap called out in db/migrations_stage30_7_pos_offers.sql.

func handleQZCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	cert, err := engines.QZCertificatePEM()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError,
			"Print signing is not configured on this server: "+err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"certificate": cert})
}

func handleQZSign(w http.ResponseWriter, r *http.Request) {
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
	sig, err := engines.QZSignRequest(req.Request)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"signature": sig,
		"algorithm": engines.QZSignAlgorithm,
	})
}

func handleQZPrinters(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	printers, err := engines.ListQZPrinters(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(printers)
}

// handleQZPrintPayload turns "print this thing" into the exact data array QZ
// Tray expects, choosing raw commands or a rasterised document based on the
// target printer's configured language.
//
// Deliberately one endpoint rather than one per document type: the browser
// then has a single call to make, and adding a new printable document later
// means one new case here instead of a new route, handler and client method.
func handleQZPrintPayload(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}

	var req struct {
		JobType     string   `json:"job_type"`
		DocumentRef string   `json:"document_ref"`
		PrinterCode string   `json:"printer_code"`
		Copies      int      `json:"copies"`
		SKUs        []string `json:"skus"`
		Reprint     string   `json:"reprint_reason"`
		DataBase64  string   `json:"data_base64"`
		DocFormat   string   `json:"doc_format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
		return
	}
	if req.Copies < 1 {
		req.Copies = 1
	}

	printer, err := resolveQZPrinter(tenantID, req.PrinterCode, req.JobType)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}

	var payload *engines.QZPrintPayload

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
		if len(req.SKUs) == 0 {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'skus' is required for a sticker print")
			return
		}
		// Routed through PrintStickers so the existing SKU validation,
		// DEVICE-0298 printer check and sticker_print_log audit trail all
		// still run - QZ changes how a sticker reaches the printer, not the
		// rules about what may be printed.
		labels, sErr := engines.PrintStickers(tenantID, req.SKUs, printer.Code, userID, req.Reprint, req.Copies)
		if sErr != nil {
			writeEngineError(w, r, sErr, http.StatusUnprocessableEntity)
			return
		}
		payload = engines.BuildStickerPayload(labels, req.Copies, printer.Language)
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
		// Strip a data: URI prefix if the caller pasted one - QZ's base64
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

	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity,
			"Unknown job_type - expected 'Shipping Label', 'Sticker', 'Receipt', 'Invoice' or 'Document'")
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"printer": printer,
		"format":  payload.Format,
		"items":   payload.Items,
		"copies":  req.Copies,
	})
}

// resolveQZPrinter picks the printer for a job: an explicit printer_code when
// the caller passed one, otherwise the Active printer whose print_role
// matches the job type. Falling back to the role is what makes one-click
// printing possible - the operator configures each bench once and then never
// chooses a printer again.
func resolveQZPrinter(tenantID, printerCode, jobType string) (engines.QZPrinter, error) {
	printers, err := engines.ListQZPrinters(tenantID)
	if err != nil {
		return engines.QZPrinter{}, err
	}

	if printerCode != "" {
		for _, p := range printers {
			if p.Code == printerCode || p.ID == printerCode {
				return p, nil
			}
		}
		return engines.QZPrinter{}, &engines.ValidationError{
			Code:    "DEVICE-0298",
			Message: fmt.Sprintf("printer %q is not configured or is inactive", printerCode),
		}
	}

	role := strings.TrimSpace(jobType)
	if role == "Document" {
		role = "Shipping Label" // marketplace documents are labels by default
	}
	for _, p := range printers {
		if p.PrintRole == role {
			return p, nil
		}
	}
	return engines.QZPrinter{}, &engines.ValidationError{
		Code: "DEVICE-0298",
		Message: fmt.Sprintf("no active printer is set as the default for %q - set 'Default For' on a Printer record, "+
			"or pick a printer for this job", role),
	}
}

func handleQZPrintLog(w http.ResponseWriter, r *http.Request) {
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
			QZPrinterName string `json:"qz_printer_name"`
			Format        string `json:"print_format"`
			Copies        int    `json:"copies"`
			Status        string `json:"status"`
			ErrorDetail   string `json:"error_detail"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid request payload")
			return
		}
		if err := engines.LogPrintJob(tenantID, req.JobType, req.DocumentRef, req.PrinterCode,
			req.QZPrinterName, req.Format, req.Copies, userID, req.Status, req.ErrorDetail); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}
