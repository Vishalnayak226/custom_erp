package server

import (
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Stage 95: Design and Combination ID images - the client's image drive
// (Configuration -> Integrations, admin only), the PO's Images page, and the
// image strip on the Design and Item screens. The engines live in
// engines/image_drive.go and engines/owner_images.go.

// --- image drive settings --------------------------------------------------

func handleGetImageDrive(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	v, err := engines.GetImageDrive(r.Header.Get("Resolved-Tenant-ID"))
	if err != nil {
		writeEngineError(w, r, err, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func decodeImageDrive(w http.ResponseWriter, r *http.Request) (engines.ImageDriveSettings, bool) {
	var in engines.ImageDriveSettings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid image drive settings")
		return in, false
	}
	return in, true
}

func handleSaveImageDrive(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	in, ok := decodeImageDrive(w, r)
	if !ok {
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	v, err := engines.SaveImageDrive(tenantID, in)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	state := "off"
	if v.Enabled {
		state = "on"
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-Username"), "IMAGE_DRIVE", "SAVED",
		fmt.Sprintf("Image drive %s/%s saved, new images to the drive: %s", v.Endpoint, v.Bucket, state))
	_ = json.NewEncoder(w).Encode(v)
}

func handleTestImageDrive(w http.ResponseWriter, r *http.Request) {
	if !requireHRAdmin(w, r, r.Header.Get("Resolved-Role")) {
		return
	}
	in, ok := decodeImageDrive(w, r)
	if !ok {
		return
	}
	if err := engines.TestImageDrive(r.Header.Get("Resolved-Tenant-ID"), in); err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- uploads -----------------------------------------------------------------

// readImageUpload reads the multipart "file" (10 MB, as the PIM upload).
func readImageUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeAPIError(w, r, "GLOBAL-0007", "")
		return nil, "", false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Choose an image file to upload")
		return nil, "", false
	}
	defer file.Close()
	b, err := io.ReadAll(file)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Failed to read uploaded file")
		return nil, "", false
	}
	return b, header.Filename, true
}

// writeImageError: a coded engine error (drive, not found) keeps its code;
// anything else is the engine's own sentence, 422.
func writeImageError(w http.ResponseWriter, r *http.Request, err error) {
	if verr, ok := err.(*engines.ValidationError); ok && verr.Code != "" {
		status := http.StatusUnprocessableEntity
		if verr.Code == "GLOBAL-0004" {
			status = http.StatusNotFound
		}
		writeEngineError(w, r, err, status)
		return
	}
	writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
}

func decodeMediaID(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		MediaID string `json:"media_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.MediaID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "media_id is required")
		return "", false
	}
	return strings.TrimSpace(req.MediaID), true
}

// --- the PO's Images page ----------------------------------------------------
// Covered by the PO's own permission: whoever makes POs (and so their
// designs and combinations - handleGetDesignMatrix) adds their images.

func handleGetPOImages(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "read", "You do not have permission to view purchase orders") {
		return
	}
	p, err := engines.GetPOImages(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeImageError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(p)
}

func handleUploadPOImage(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "create", "You do not have permission to add images to purchase orders") {
		return
	}
	b, name, ok := readImageUpload(w, r)
	if !ok {
		return
	}
	res, err := engines.SavePOImage(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), r.Header.Get("Resolved-User-ID"),
		b, name, r.FormValue("target_doctype"), r.FormValue("target_code"), r.FormValue("main") == "1")
	if err != nil {
		writeImageError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func handlePOImageMain(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "create", "You do not have permission to change images on purchase orders") {
		return
	}
	mediaID, ok := decodeMediaID(w, r)
	if !ok {
		return
	}
	if err := engines.SetPOImageMain(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), mediaID); err != nil {
		writeImageError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handlePOImageRemove(w http.ResponseWriter, r *http.Request) {
	if !requireDocPermission(w, r, "PurchaseOrder", "create", "You do not have permission to change images on purchase orders") {
		return
	}
	mediaID, ok := decodeMediaID(w, r)
	if !ok {
		return
	}
	if err := engines.RemovePOImage(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"), mediaID); err != nil {
		writeImageError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- the Design / Item screens -----------------------------------------------
// Covered by the owner's own permission (read to see, update to change).

func ownerDoctypeAllowed(w http.ResponseWriter, r *http.Request, doctype, action string) bool {
	return requireDocPermission(w, r, doctype, action, "You do not have permission to change this record's images")
}

func handleListOwnerImages(w http.ResponseWriter, r *http.Request) {
	doctype, err := engines.ImageOwnerDoctype(r.URL.Query().Get("doctype"))
	if err != nil {
		writeImageError(w, r, err)
		return
	}
	if !ownerDoctypeAllowed(w, r, doctype, "read") {
		return
	}
	list, err := engines.ListOwnerImages(r.Header.Get("Resolved-Tenant-ID"), strings.TrimSpace(r.URL.Query().Get("code")))
	if err != nil {
		writeImageError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleUploadOwnerImage(w http.ResponseWriter, r *http.Request) {
	b, name, ok := readImageUpload(w, r)
	if !ok {
		return
	}
	doctype, err := engines.ImageOwnerDoctype(r.FormValue("doctype"))
	if err != nil {
		writeImageError(w, r, err)
		return
	}
	if !ownerDoctypeAllowed(w, r, doctype, "update") {
		return
	}
	img, err := engines.SaveOwnerImage(r.Header.Get("Resolved-Tenant-ID"), doctype, r.FormValue("code"), r.Header.Get("Resolved-User-ID"), b, name, r.FormValue("main") == "1")
	if err != nil {
		writeImageError(w, r, err)
		return
	}
	_ = json.NewEncoder(w).Encode(img)
}

func handleOwnerImageAction(set bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaID, ok := decodeMediaID(w, r)
		if !ok {
			return
		}
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		doctype, _, err := engines.MediaOwner(tenantID, mediaID)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
			return
		}
		if !ownerDoctypeAllowed(w, r, doctype, "update") {
			return
		}
		if set {
			err = engines.SetMainImage(tenantID, mediaID)
		} else {
			err = engines.RemoveOwnerImage(tenantID, mediaID)
		}
		if err != nil {
			writeImageError(w, r, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// --- reading an image --------------------------------------------------------
// Any signed-in user of the tenant may see a product image (a cashier and a
// picker both need them); only images are served here - PIM's certificates
// and QC documents stay behind the PIM routes.

func handleImageThumbnail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	b, err := engines.ReadMediaThumbnail(tenantID, r.PathValue("id"))
	if err != nil {
		// No thumbnail (WebP / GIF): the image itself.
		handleImageFile(w, r)
		return
	}
	writeMediaBytes(w, "image/jpeg", b)
}

func handleImageFile(w http.ResponseWriter, r *http.Request) {
	b, fileType, _, err := engines.ReadMediaFile(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("id"))
	if err != nil {
		writeMediaReadError(w, r, err)
		return
	}
	if !strings.HasPrefix(fileType, "image/") {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "not an image")
		return
	}
	writeMediaBytes(w, fileType, b)
}
