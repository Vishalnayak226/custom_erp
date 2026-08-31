package server

import (
	"custom_erp/engines"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// Stage 36.6: DAM depth. Media upload/list/thumbnail/deactivate already
// exist (Stage 15.2/26.4.4, handlers_procurement_pim2.go); these handlers
// cover what's new - on-demand transform renditions, bulk zip up/down, and
// the catalog-wide search the browse UI reads.

// handlePIMMediaTransformPresets publishes exactly what GetMediaTransform
// implements, so the browse UI can never offer a preset the engine refuses.
func handlePIMMediaTransformPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	_ = json.NewEncoder(w).Encode(engines.ListPIMMediaTransformPresets())
}

// handlePIMMediaTransform streams a generated rendition, generating and
// caching it on first request - same authenticated-route posture as
// handlePIMMediaFile/handlePIMMediaThumbnail (private storage, not the
// unauthenticated static file server).
func handlePIMMediaTransform(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	mediaID := r.PathValue("id")
	preset := r.PathValue("preset")
	path, fileType, err := engines.GetMediaTransform(tenantID, mediaID, preset)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "stored transform missing")
		return
	}
	w.Header().Set("Content-Type", fileType)
	_, _ = w.Write(fileBytes)
}

// handlePIMMediaBulkUpload (36.6.2/36.6.4) accepts a zip of images and
// auto-associates each one to a product by filename convention
// ("ITEMCODE__role.ext") - same bigger-body-limit pattern
// handlePIMMediaUpload already uses, since a batch of images is larger than
// the global 2MB JSON body cap.
func handlePIMMediaBulkUpload(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Zip file is mandatory under multipart FormFile 'file'")
		return
	}
	defer file.Close()
	zipBytes, err := io.ReadAll(file)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Failed to read uploaded file")
		return
	}
	outcomes, err := engines.BulkUploadMediaZip(tenantID, zipBytes, userID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(outcomes)
}

// handlePIMMediaBulkDownload (36.6.2) zips every Active media for a
// comma-separated item selection or a product group (reusing
// ResolvePIMBulkTargetIDs, the same mutually-exclusive-selection rule
// every other bulk action in PIM already enforces).
func handlePIMMediaBulkDownload(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	for _, doctype := range []string{"ProductMedia", "Item"} {
		allowed, err := checkPermission(tenantID, role, doctype, "read")
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			writeAPIError(w, r, "GLOBAL-0011", "")
			return
		}
	}
	var itemCodes []string
	if raw := strings.TrimSpace(r.URL.Query().Get("item")); raw != "" {
		for _, code := range strings.Split(raw, ",") {
			if code = strings.TrimSpace(code); code != "" {
				itemCodes = append(itemCodes, code)
			}
		}
	}
	resolved, err := engines.ResolvePIMBulkTargetIDs(tenantID, "Item", r.URL.Query().Get("group_id"), itemCodes)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(resolved) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "No items to download - send item (comma-separated) or group_id")
		return
	}
	zipBytes, err := engines.BulkDownloadMediaZip(tenantID, resolved)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=pim_media_bulk.zip")
	_, _ = w.Write(zipBytes)
}

// handlePIMMediaSearch (36.6.3) is the catalog-wide, filterable read the
// browse UI lists against - ListMediaForItem stays scoped to one item for
// the Workbench gallery.
func handlePIMMediaSearch(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	filters := engines.PIMMediaSearchFilters{
		Item:      r.URL.Query().Get("item"),
		MediaRole: r.URL.Query().Get("role"),
		Tag:       r.URL.Query().Get("tag"),
		FileType:  r.URL.Query().Get("file_type"),
	}
	results, err := engines.SearchMedia(tenantID, filters)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}
