package server

import (
	"encoding/json"
	"io"
	"net/http"

	"custom_erp/engines"
)

// Stage 134.10 (120.6): a client's own brand (engines/branding.go).
//
// GET /api/v1/branding and GET /api/v1/branding/logo are public: the sign-in
// page on the client's own address shows them before anyone signs in. They
// carry only what that page shows anyway - the company name, the colour, the
// logo - and nothing when branding is off.

func handleBranding(w http.ResponseWriter, r *http.Request) {
	b := engines.GetBranding(r.Header.Get("Resolved-Tenant-ID"))
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(b)
}

func handleBrandingLogo(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if !engines.GetBranding(tenantID).Enabled {
		http.NotFound(w, r)
		return
	}
	content, contentType, err := engines.BrandLogo(tenantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "public, max-age=86400") // the URL carries ?v=<version>
	_, _ = w.Write(content)
}

// POST /api/v1/admin/branding/logo (multipart "file"), DELETE removes it.
func handleBrandingLogoAdmin(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if !engines.HasFullAccess(r.Header.Get("Resolved-Role")) {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return
	}
	if r.Method == http.MethodDelete {
		if err := engines.DeleteBrandLogo(tenantID); err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "BRANDING_LOGO", "REMOVED", "")
		_ = json.NewEncoder(w).Encode(engines.GetBranding(tenantID))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a PNG, JPEG or WebP logo of at most 300 KB.")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose a PNG, JPEG or WebP logo of at most 300 KB.")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The logo could not be read.")
		return
	}
	if err := engines.SaveBrandLogo(tenantID, content); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "BRANDING_LOGO", "UPLOADED", "")
	_ = json.NewEncoder(w).Encode(engines.GetBranding(tenantID))
}
