package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"custom_erp/engines"
)

// Ask 170.35 / 170.36: the GSTR-1 JSON download and the GSTR-2B import,
// match and decision routes. Files only; nothing here calls the GST portal.

// handleGSTR1JSON returns the month's GSTR-1 for one GSTIN. With download=1
// it is sent as a file named for the return, holding only the GSTN schema;
// without it the answer also carries the reconciliation with the return summary.
func handleGSTR1JSON(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	q := r.URL.Query()
	res, err := engines.BuildGSTR1(tenantID, q.Get("gstin"), q.Get("month"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if q.Get("download") == "1" {
		w.Header().Set("Content-Disposition", attachmentDisposition(fmt.Sprintf("GSTR1_%s_%s.json", fmt.Sprint(res.File["gstin"]), fmt.Sprint(res.File["fp"]))))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res.File)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleGSTR2BImport takes the 2B JSON as the request body (or a multipart
// "file") and the month as ?month=YYYY-MM.
func handleGSTR2BImport(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	var raw []byte
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, _, ferr := r.FormFile("file")
		if ferr != nil {
			writeAPIErrorGeneric(w, r, http.StatusBadRequest, "choose the GSTR-2B JSON file")
			return
		}
		defer f.Close()
		raw, err = io.ReadAll(f)
	} else {
		raw, err = io.ReadAll(r.Body)
	}
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "the file could not be read")
		return
	}
	res, err := engines.ImportGSTR2B(tenantID, r.URL.Query().Get("month"), raw, userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handleGSTR2BReconcile matches the month again after bills were entered.
func handleGSTR2BReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	summary, err := engines.ReconcileGSTR2B(tenantID, r.URL.Query().Get("month"), userID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"summary": summary})
}

// handleGSTR2BDecision records Accepted / Rejected / Query raised on a line.
func handleGSTR2BDecision(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-User-ID")
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeAPIErrorGeneric(w, r, http.StatusBadRequest, "invalid request")
		return
	}
	if err := engines.SetGSTR2BDecision(tenantID, r.PathValue("id"), req.Decision, req.Note, userID); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
