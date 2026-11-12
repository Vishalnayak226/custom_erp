package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
)

// Stage 146.5.4: the wide product sheet. Preview writes nothing; import
// writes all three parts or none. The caller needs the same create / update
// right the neighbour imports ask, on each of the three doctypes the sheet
// can touch.

func handleProductSheet(commit bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		role := r.Header.Get("Resolved-Role")
		for _, dt := range []string{"Item", "ProductAttributeValue", "ProductContent"} {
			if !requireCreateAndUpdate(w, r, tenantID, role, dt) {
				return
			}
		}
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			writeAPIError(w, r, "GLOBAL-0007", "")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "CSV file is mandatory under multipart FormFile 'file'")
			return
		}
		defer file.Close()
		res, err := engines.ImportProductSheet(r.Context(), tenantID, engines.NamedUpload(file, header.Filename), r.Header.Get("Resolved-User-ID"), role, commit)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

// requireCreateAndUpdate: the sheet both creates and updates, so the caller needs both rights.
func requireCreateAndUpdate(w http.ResponseWriter, r *http.Request, tenantID, role, doctype string) bool {
	for _, action := range []string{"create", "update"} {
		allowed, err := checkPermission(tenantID, role, doctype, action)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return false
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return false
		}
	}
	return true
}
