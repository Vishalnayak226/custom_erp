package engines

import (
	"bytes"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder with image.Decode
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Media Library / DAM (Stage 15.2, PIM Blueprint V2 §8). Genuinely new
// infrastructure - no file-upload code existed anywhere in this codebase
// before this. Files are stored on local disk under mediaStoreDir (NOT
// under public/, which is served unauthenticated via http.FileServer - see
// internal/server/routes.go) and served back only through an authenticated
// handler (GetMediaFile + handlePIMMediaFile in
// internal/server/handlers_procurement_pim2.go) - the pragmatic in-house
// equivalent of "private storage + signed URL" for a single-binary app
// with no CDN/object-storage/signing infra. Content-addressed by SHA-256
// checksum for free duplicate detection and to make "never overwrite
// original, mark inactive instead" trivial: identical bytes always resolve
// to the same stored file.

// mediaStoreDir is a var (not const), same reason connector_shopify.go's
// shopifyGraphQLURL is: so tests can point storage at a scratch directory
// instead of this relative path under the working directory, which on this
// machine sits under Documents\ - Windows Controlled Folder Access blocks a
// freshly-built (test) binary from creating a new directory there (see the
// CLAUDE.md note on that exact error). Production is unaffected either way;
// the deployed server's working directory is not under Documents\.
var mediaStoreDir = "media_store"

var allowedMediaExtensions = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".pdf":  "application/pdf",
}

type ProductMediaAsset struct {
	ID           string `json:"id"`
	Item         string `json:"item"`
	MediaRole    string `json:"media_role"`
	FileType     string `json:"file_type"`
	Checksum     string `json:"checksum"`
	VersionNo    int    `json:"version_no"`
	Status       string `json:"status"`
	AltText      string `json:"alt_text,omitempty"`
	ExpiryDate   string `json:"expiry_date,omitempty"`
	HasThumbnail bool   `json:"has_thumbnail,omitempty"`
	Tags         string `json:"tags,omitempty"`
}

// generateThumbnail decodes a jpg/png upload and produces a nearest-
// neighbor-downsampled JPEG no larger than maxDim on its longest side,
// always stdlib image/jpeg+image/png (no new dependency). webp/gif/pdf
// are skipped (ok=false, not an error) - golang.org/x/image's webp decoder
// and a GIF-frame-aware resize are both real scope beyond what a single
// static thumbnail needs here, stated as a limitation rather than
// approximated badly.
//
// maxDim is passed in rather than read from config here (Stage 30.7, the
// "pim.thumbnail_max_dim" setting, default still 200) so this stays a pure
// image helper with no tenant/DB dependency - the caller resolves it.
func generateThumbnail(fileBytes []byte, fileType string, maxDim int) (thumbBytes []byte, ok bool) {
	if fileType != "image/jpeg" && fileType != "image/png" {
		return nil, false
	}
	src, _, err := image.Decode(bytes.NewReader(fileBytes))
	if err != nil {
		return nil, false
	}
	return encodeJPEG(resizeToMaxDim(src, maxDim))
}

// resizeToMaxDim nearest-neighbor-downsamples src so its longest side is no
// larger than maxDim, preserving aspect ratio - the exact resize math
// generateThumbnail always used, now shared with Stage 36.6.1's transform
// presets so there is one resize implementation, not two that could drift.
// Never upscales past maxDim < 1's floor of 1px; a source already at or
// under maxDim on both sides is returned unchanged in size (scale stays
// 1.0), matching generateThumbnail's original behaviour exactly.
func resizeToMaxDim(src image.Image, maxDim int) *image.RGBA {
	if maxDim < 1 {
		maxDim = 1
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	scale := 1.0
	if w >= h && w > maxDim {
		scale = float64(maxDim) / float64(w)
	} else if h > w && h > maxDim {
		scale = float64(maxDim) / float64(h)
	}
	newW, newH := int(float64(w)*scale), int(float64(h)*scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := bounds.Min.Y + y*h/newH
		for x := 0; x < newW; x++ {
			srcX := bounds.Min.X + x*w/newW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func encodeJPEG(img image.Image) (jpegBytes []byte, ok bool) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// thumbnailStorePath is the deterministic on-disk location for a media
// asset's thumbnail, derived from its checksum the same content-addressed
// way the original file is - never a second row/id to keep in sync.
func thumbnailStorePath(checksum string) string {
	return filepath.Join(mediaStoreDir, checksum+"_thumb.jpg")
}

// nextMediaVersion (Stage 26.4.4) returns the next version number for an
// item+role - real per-role incrementing version history (counting every
// prior version, including ones since deactivated), replacing the old
// hardcoded version_no=1 every upload used to get.
func nextMediaVersion(tenantID, itemCode, mediaRole string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 1, err
	}
	var maxVersion int
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(MAX(NULLIF(data->>'version_no', '')::int), 0) FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND data->>'media_role' = $2`, schema), itemCode, mediaRole).Scan(&maxVersion)
	if err != nil {
		return 1, err
	}
	return maxVersion + 1, nil
}

// validateMediaFile checks the extension allowlist AND sniffs actual file
// content (http.DetectContentType) so a renamed executable ("virus.exe" ->
// "virus.jpg") is still rejected - "don't rely only on filename," matching
// this codebase's own existing convention (see engines/stickers.go's
// barcode-validation notes) and the blueprint's explicit "no executable
// uploads" rule.
func validateMediaFile(filename string, fileBytes []byte) (fileType string, err error) {
	ext := strings.ToLower(filepath.Ext(filename))
	expectedMIME, ok := allowedMediaExtensions[ext]
	if !ok {
		return "", &ValidationError{Code: "GLOBAL-0008", Message: fmt.Sprintf("file type %q is not allowed - allowed types: jpg, jpeg, png, webp, gif, pdf", ext)}
	}

	sniffLen := 512
	if len(fileBytes) < sniffLen {
		sniffLen = len(fileBytes)
	}
	detected := http.DetectContentType(fileBytes[:sniffLen])
	if expectedMIME == "application/pdf" {
		if !strings.HasPrefix(detected, "application/pdf") {
			return "", &ValidationError{Code: "GLOBAL-0008", Message: fmt.Sprintf("file content does not match a PDF (detected %q) - upload rejected", detected)}
		}
	} else if !strings.HasPrefix(detected, "image/") {
		return "", &ValidationError{Code: "GLOBAL-0008", Message: fmt.Sprintf("file content does not match an image (detected %q) - upload rejected", detected)}
	}
	return expectedMIME, nil
}

func checksumOf(fileBytes []byte) string {
	sum := sha256.Sum256(fileBytes)
	return hex.EncodeToString(sum[:])
}

// findExistingMediaByChecksum returns the id of an Active ProductMedia
// already storing these exact bytes for this item+role, if any. Scoped by
// role (not just item+checksum) so re-using the same image for a different
// role (e.g. Main Image and Gallery) creates a distinct record rather than
// silently reusing one - only a true re-upload of the same bytes+role is
// treated as a no-op duplicate.
func findExistingMediaByChecksum(tenantID, itemCode, mediaRole, checksum string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND data->>'media_role' = $2 AND data->>'checksum' = $3 AND status = 'Active'
		LIMIT 1`, schema), itemCode, mediaRole, checksum).Scan(&id)
	if err != nil {
		return "", nil // not found - not an error
	}
	return id, nil
}

// demoteExistingMainImage marks any currently-Active "Main Image" for this
// item Inactive - "only one active primary image per object," enforced by
// demotion rather than deletion (never overwrite/delete original media).
func demoteExistingMainImage(tenantID, itemCode string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND data->>'media_role' = 'Main Image' AND status = 'Active'`, schema), itemCode)
	if err != nil {
		return err
	}
	type pending struct {
		id   string
		data map[string]interface{}
	}
	var toUpdate []pending
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		toUpdate = append(toUpdate, pending{id, data})
	}
	rows.Close()

	for _, u := range toUpdate {
		u.data["status"] = "Inactive"
		marshaled, err := json.Marshal(u.data)
		if err != nil {
			continue
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Inactive', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProductMedia' AND id = $2`, schema), marshaled, u.id); err != nil {
			return err
		}
	}
	return nil
}

func generateMediaID(itemCode, mediaRole, checksum string) string {
	short := checksum
	if len(short) > 12 {
		short = short[:12]
	}
	roleSlug := strings.ToLower(strings.ReplaceAll(mediaRole, " ", "-"))
	return itemCode + "::" + roleSlug + "::" + short
}

// SaveMediaFile validates, dedups, stores, and registers an uploaded file
// as a ProductMedia document. Returns the existing asset (no new file
// written, no new document) if this exact checksum is already Active for
// this item+role. altText/expiryDate are optional (Stage 26.4.4); pass ""
// for either to leave them unset.
func SaveMediaFile(tenantID string, fileBytes []byte, filename, itemCode, mediaRole, uploadedBy, altText, expiryDate string) (*ProductMediaAsset, error) {
	if itemCode == "" {
		return nil, fmt.Errorf("item is required")
	}
	if mediaRole == "" {
		return nil, fmt.Errorf("media_role is required")
	}
	if len(fileBytes) == 0 {
		return nil, fmt.Errorf("uploaded file is empty")
	}
	fileType, err := validateMediaFile(filename, fileBytes)
	if err != nil {
		return nil, err
	}

	checksum := checksumOf(fileBytes)
	if existingID, err := findExistingMediaByChecksum(tenantID, itemCode, mediaRole, checksum); err == nil && existingID != "" {
		return &ProductMediaAsset{ID: existingID, Item: itemCode, MediaRole: mediaRole, Checksum: checksum, FileType: fileType, Status: "Active"}, nil
	}

	if err := os.MkdirAll(mediaStoreDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to prepare media storage: %v", err)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	storedPath := filepath.Join(mediaStoreDir, checksum+ext)
	if _, statErr := os.Stat(storedPath); os.IsNotExist(statErr) {
		if err := os.WriteFile(storedPath, fileBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to store file: %v", err)
		}
	}

	hasThumbnail := false
	if thumbBytes, ok := generateThumbnail(fileBytes, fileType, GetSettingInt(tenantID, "pim.thumbnail_max_dim")); ok {
		thumbPath := thumbnailStorePath(checksum)
		if _, statErr := os.Stat(thumbPath); os.IsNotExist(statErr) {
			if err := os.WriteFile(thumbPath, thumbBytes, 0644); err == nil {
				hasThumbnail = true
			}
		} else {
			hasThumbnail = true
		}
	}

	if mediaRole == "Main Image" {
		if err := demoteExistingMainImage(tenantID, itemCode); err != nil {
			return nil, fmt.Errorf("failed to demote prior main image: %v", err)
		}
	}

	versionNo, err := nextMediaVersion(tenantID, itemCode, mediaRole)
	if err != nil {
		return nil, err
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	mediaID := generateMediaID(itemCode, mediaRole, checksum)
	hasThumbFlag := "No"
	if hasThumbnail {
		hasThumbFlag = "Yes"
	}
	data := map[string]interface{}{
		"id":            mediaID,
		"code":          mediaID,
		"item":          itemCode,
		"media_role":    mediaRole,
		"file_path":     storedPath,
		"file_type":     fileType,
		"checksum":      checksum,
		"version_no":    versionNo,
		"sort_order":    0,
		"status":        "Active",
		"alt_text":      altText,
		"expiry_date":   expiryDate,
		"has_thumbnail": hasThumbFlag,
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ProductMedia', $2, 'Active', $3)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = EXCLUDED.status, updated_at = CURRENT_TIMESTAMP`, schema),
		mediaID, marshaled, uploadedBy)
	if err != nil {
		return nil, err
	}

	return &ProductMediaAsset{
		ID: mediaID, Item: itemCode, MediaRole: mediaRole,
		FileType: fileType, Checksum: checksum, VersionNo: versionNo, Status: "Active",
		AltText: altText, ExpiryDate: expiryDate, HasThumbnail: hasThumbnail,
	}, nil
}

// UpdateMediaMetadata (Stage 26.4.4; tags added Stage 36.6.3) corrects alt
// text/expiry date/tags after upload without re-uploading the file - all
// three are metadata-only fields; editing them never touches the stored
// bytes, checksum, or version. tags is a comma-separated list, the same
// shape ProductContent.tags already uses, and is a *string so a caller that
// only edits alt text/expiry (the existing single-item Workbench gallery,
// which has no tags input) can pass nil and leave whatever tags are already
// set untouched, rather than every such edit silently wiping them.
func UpdateMediaMetadata(tenantID, mediaID, altText, expiryDate string, tags *string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductMedia' AND id = $1`, schema), mediaID).Scan(&dataStr); err != nil {
		return fmt.Errorf("media not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	data["alt_text"] = altText
	data["expiry_date"] = expiryDate
	if tags != nil {
		data["tags"] = *tags
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProductMedia' AND id = $2`, schema), marshaled, mediaID)
	return err
}

// GetMediaThumbnail resolves a ProductMedia id to its generated thumbnail's
// file path, if one exists (Stage 26.4.4) - same Active-only rule as
// GetMediaFile.
func GetMediaThumbnail(tenantID, mediaID string) (path string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ProductMedia' AND id = $1`, schema), mediaID).Scan(&dataStr, &status)
	if err != nil {
		return "", fmt.Errorf("media not found: %v", err)
	}
	if status != "Active" {
		return "", fmt.Errorf("media is not active")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", err
	}
	hasThumb, _ := data["has_thumbnail"].(string)
	checksum, _ := data["checksum"].(string)
	if hasThumb != "Yes" || checksum == "" {
		return "", fmt.Errorf("no thumbnail available for this media")
	}
	return thumbnailStorePath(checksum), nil
}

// GetMediaFile resolves a ProductMedia id to its stored file path and MIME
// type, for the authenticated download handler. Only returns Active media -
// a deactivated asset is no longer servable by id even if the caller knows
// it.
func GetMediaFile(tenantID, mediaID string) (path, fileType string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'ProductMedia' AND id = $1`, schema), mediaID).Scan(&dataStr, &status)
	if err != nil {
		return "", "", fmt.Errorf("media not found: %v", err)
	}
	if status != "Active" {
		return "", "", fmt.Errorf("media is not active")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", "", err
	}
	path, _ = data["file_path"].(string)
	fileType, _ = data["file_type"].(string)
	if path == "" {
		return "", "", fmt.Errorf("media record has no stored file")
	}
	return path, fileType, nil
}

// ListMediaForItem returns all Active ProductMedia for an item, sorted by
// sort_order then id, for the Workbench media gallery.
func ListMediaForItem(tenantID, itemCode string) ([]ProductMediaAsset, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'media_role', ''), COALESCE(data->>'file_type', ''), COALESCE(data->>'checksum', ''), status,
			COALESCE(NULLIF(data->>'version_no', '')::int, 1), COALESCE(data->>'alt_text', ''), COALESCE(data->>'expiry_date', ''), COALESCE(data->>'has_thumbnail', '') = 'Yes'
		FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1 AND status = 'Active'
		ORDER BY id`, schema), itemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProductMediaAsset
	for rows.Next() {
		var m ProductMediaAsset
		if err := rows.Scan(&m.ID, &m.MediaRole, &m.FileType, &m.Checksum, &m.Status, &m.VersionNo, &m.AltText, &m.ExpiryDate, &m.HasThumbnail); err != nil {
			return nil, err
		}
		m.Item = itemCode
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeactivateMedia marks a ProductMedia Inactive (never hard-deletes,
// preserving version history).
func DeactivateMedia(tenantID, mediaID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductMedia' AND id = $1`, schema), mediaID).Scan(&dataStr); err != nil {
		return fmt.Errorf("media not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	data["status"] = "Inactive"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Inactive', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProductMedia' AND id = $2`, schema), marshaled, mediaID)
	return err
}
