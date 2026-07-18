package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Media Library / DAM (Stage 15.2, PIM Blueprint V2 §8). Genuinely new
// infrastructure - no file-upload code existed anywhere in this codebase
// before this. Files are stored on local disk under mediaStoreDir (NOT
// under public/, which is served unauthenticated via http.FileServer - see
// main.go:510) and served back only through an authenticated handler
// (GetMediaFile + handlePIMMediaFile in main.go) - the pragmatic in-house
// equivalent of "private storage + signed URL" for a single-binary app
// with no CDN/object-storage/signing infra. Content-addressed by SHA-256
// checksum for free duplicate detection and to make "never overwrite
// original, mark inactive instead" trivial: identical bytes always resolve
// to the same stored file.

const mediaStoreDir = "media_store"

var allowedMediaExtensions = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".pdf":  "application/pdf",
}

type ProductMediaAsset struct {
	ID        string `json:"id"`
	Item      string `json:"item"`
	MediaRole string `json:"media_role"`
	FileType  string `json:"file_type"`
	Checksum  string `json:"checksum"`
	VersionNo int    `json:"version_no"`
	Status    string `json:"status"`
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
		return "", fmt.Errorf("file type %q is not allowed - allowed types: jpg, jpeg, png, webp, gif, pdf", ext)
	}

	sniffLen := 512
	if len(fileBytes) < sniffLen {
		sniffLen = len(fileBytes)
	}
	detected := http.DetectContentType(fileBytes[:sniffLen])
	if expectedMIME == "application/pdf" {
		if !strings.HasPrefix(detected, "application/pdf") {
			return "", fmt.Errorf("file content does not match a PDF (detected %q) - upload rejected", detected)
		}
	} else if !strings.HasPrefix(detected, "image/") {
		return "", fmt.Errorf("file content does not match an image (detected %q) - upload rejected", detected)
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
// this item+role.
func SaveMediaFile(tenantID string, fileBytes []byte, filename, itemCode, mediaRole, uploadedBy string) (*ProductMediaAsset, error) {
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

	if mediaRole == "Main Image" {
		if err := demoteExistingMainImage(tenantID, itemCode); err != nil {
			return nil, fmt.Errorf("failed to demote prior main image: %v", err)
		}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	mediaID := generateMediaID(itemCode, mediaRole, checksum)
	data := map[string]interface{}{
		"id":         mediaID,
		"code":       mediaID,
		"item":       itemCode,
		"media_role": mediaRole,
		"file_path":  storedPath,
		"file_type":  fileType,
		"checksum":   checksum,
		"version_no": 1,
		"sort_order": 0,
		"status":     "Active",
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
		FileType: fileType, Checksum: checksum, VersionNo: 1, Status: "Active",
	}, nil
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
		SELECT id, COALESCE(data->>'media_role', ''), COALESCE(data->>'file_type', ''), COALESCE(data->>'checksum', ''), status
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
		if err := rows.Scan(&m.ID, &m.MediaRole, &m.FileType, &m.Checksum, &m.Status); err != nil {
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
