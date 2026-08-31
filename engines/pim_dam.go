package engines

import (
	"archive/zip"
	"bytes"
	"custom_erp/db"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stage 36.6: DAM depth - transformations beyond the 26.4.4 thumbnail, bulk
// zip up/download with filename-based auto-association, and tagging/
// search/filter (the browse UI's data layer). Everything here reads/writes
// the same ProductMedia rows and media_store files Stage 15.2/26.4.4 already
// established - no new table, no second storage mechanism.

// --- 36.6.1: asset transformations ---------------------------------------

// pimMediaTransformPresets is a closed vocabulary, the same reasoning as
// 36.2.3's workflow conditions and 36.5.2's transform functions: an
// endpoint that resized to whatever dimensions a caller asked for would be
// a free image-processing amplifier against this server. JPEG/PNG only,
// same as generateThumbnail - Go's stdlib has no WebP encoder, and a
// dependency or a hand-rolled encoder is real scope this stage does not
// take on (stated limit, not a guess).
type pimMediaTransformPreset struct {
	maxDim int
	square bool
}

var pimMediaTransformPresets = map[string]pimMediaTransformPreset{
	"small":  {maxDim: 400},
	"medium": {maxDim: 800},
	"large":  {maxDim: 1600},
	"square": {maxDim: 600, square: true},
}

// ListPIMMediaTransformPresets publishes exactly what GetMediaTransform
// implements, so a caller can never ask for a preset the engine will refuse.
func ListPIMMediaTransformPresets() []string {
	names := make([]string, 0, len(pimMediaTransformPresets))
	for name := range pimMediaTransformPresets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func mediaTransformStorePath(checksum, preset string) string {
	return filepath.Join(mediaStoreDir, checksum+"_transform_"+preset+".jpg")
}

// centerCropSquareRGBA crops the longer side down to the shorter one,
// centered, before the resize step - "square" is a crop-then-resize
// preset, not a stretch-to-fit one, so a product photo's subject stays
// centered rather than being squashed.
func centerCropSquareRGBA(src image.Image) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	side := w
	if h < w {
		side = h
	}
	x0 := bounds.Min.X + (w-side)/2
	y0 := bounds.Min.Y + (h-side)/2
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(image.Rect(x0, y0, x0+side, y0+side))
	}
	// Fallback for a decoded type that doesn't implement SubImage (none of
	// the stdlib jpeg/png decoders' concrete return types are missing it in
	// practice, but the interface itself doesn't guarantee it).
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			dst.Set(x, y, src.At(x0+x, y0+y))
		}
	}
	return dst
}

// GetMediaTransform resolves a ProductMedia id + preset to a generated
// rendition's file path, generating and caching it on first request - the
// same lazy generate-once-then-serve-from-disk shape the existing thumbnail
// already uses, just keyed by preset as well as checksum so presets can
// never collide with each other or with the thumbnail.
func GetMediaTransform(tenantID, mediaID, preset string) (path, fileType string, err error) {
	spec, ok := pimMediaTransformPresets[preset]
	if !ok {
		return "", "", fmt.Errorf("unknown transform preset %q - see GET /api/v1/pim/media/transform-presets", preset)
	}
	sourcePath, sourceType, err := GetMediaFile(tenantID, mediaID)
	if err != nil {
		return "", "", err
	}
	if sourceType != "image/jpeg" && sourceType != "image/png" {
		return "", "", fmt.Errorf("transformations are only available for JPEG/PNG originals (this asset is %s)", sourceType)
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	var checksum string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'checksum','') FROM %s.documents WHERE doctype = 'ProductMedia' AND id = $1`, schema), mediaID).Scan(&checksum); err != nil || checksum == "" {
		return "", "", fmt.Errorf("media %q has no checksum on record", mediaID)
	}

	cachePath := mediaTransformStorePath(checksum, preset)
	if _, statErr := os.Stat(cachePath); statErr == nil {
		return cachePath, "image/jpeg", nil
	}

	fileBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read stored original: %v", err)
	}
	src, _, decErr := image.Decode(bytes.NewReader(fileBytes))
	if decErr != nil {
		return "", "", fmt.Errorf("failed to decode stored original: %v", decErr)
	}
	if spec.square {
		src = centerCropSquareRGBA(src)
	}
	rendered := resizeToMaxDim(src, spec.maxDim)
	jpegBytes, ok := encodeJPEG(rendered)
	if !ok {
		return "", "", fmt.Errorf("failed to encode transformed image")
	}
	if err := os.MkdirAll(mediaStoreDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to prepare media storage: %v", err)
	}
	if err := os.WriteFile(cachePath, jpegBytes, 0644); err != nil {
		return "", "", fmt.Errorf("failed to cache transformed image: %v", err)
	}
	return cachePath, "image/jpeg", nil
}

// --- 36.6.2 / 36.6.4: bulk zip up/download, filename auto-association ----

// pimMediaRoles is the closed set of ProductMedia.media_role values (the
// Select field's own option list, db/migration.sql section 15.2) - the
// single place both the filename slug map and its reverse below are built
// from, so the two can never drift out of sync with each other.
var pimMediaRoles = []string{"Main Image", "Gallery", "Variant Image", "Lifestyle", "Certificate", "Internal QC", "Video/Other"}

func mediaRoleSlug(role string) string {
	slug := strings.ToLower(role)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "/", "-")
	return slug
}

var pimMediaRoleBySlug = func() map[string]string {
	m := make(map[string]string, len(pimMediaRoles))
	for _, role := range pimMediaRoles {
		m[mediaRoleSlug(role)] = role
	}
	return m
}()

// parseMediaFilename reads the bulk-upload/download naming convention:
// "<ItemCode>__<role-slug>.<ext>", role optional (defaults to Gallery, the
// role an asset with no stated purpose is safest treated as - never Main
// Image, which SaveMediaFile treats specially by demoting whatever else
// currently holds that slot). This is 36.6.4's whole mechanism: an
// operator names files by convention once, and every upload that follows
// it associates itself with the right product with no per-file picking.
func parseMediaFilename(filename string) (itemCode, mediaRole string) {
	// Base() first, THEN strip the extension - stripping the extension from
	// a bare-dot name like ".jpg" leaves "", and Base("") returns "." (its
	// documented empty-path behaviour), which would otherwise read back as
	// a literal item code "." instead of "no item code at all".
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.SplitN(base, "__", 2)
	itemCode = strings.TrimSpace(parts[0])
	mediaRole = "Gallery"
	if len(parts) == 2 {
		if role, ok := pimMediaRoleBySlug[strings.ToLower(strings.TrimSpace(parts[1]))]; ok {
			mediaRole = role
		}
	}
	return itemCode, mediaRole
}

// BulkMediaUploadOutcome is one zip entry's result - the same per-item
// "report exactly which succeeded/failed and why" shape 36.2.5's bulk
// actions and 36.3.5's variant preflight already use, rather than one bad
// file aborting the whole batch.
type BulkMediaUploadOutcome struct {
	Filename string `json:"filename"`
	ItemCode string `json:"item_code"`
	MediaRole string `json:"media_role"`
	MediaID  string `json:"media_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

// BulkUploadMediaZip unpacks a zip of images and saves each one through the
// exact same SaveMediaFile a single upload uses - every guard a manual
// upload already passes (extension allowlist, content sniffing, checksum
// dedup, Main Image demotion) applies here too, because it is the same
// function, not a parallel bulk-only path.
func BulkUploadMediaZip(tenantID string, zipBytes []byte, uploadedBy string) ([]BulkMediaUploadOutcome, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip file: %v", err)
	}
	var outcomes []BulkMediaUploadOutcome
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(entry.Name)
		itemCode, mediaRole := parseMediaFilename(name)
		outcome := BulkMediaUploadOutcome{Filename: name, ItemCode: itemCode, MediaRole: mediaRole}
		if itemCode == "" {
			outcome.Error = `filename does not start with an item code (expected "ITEMCODE__role.ext" or "ITEMCODE.ext")`
			outcomes = append(outcomes, outcome)
			continue
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			outcome.Error = openErr.Error()
			outcomes = append(outcomes, outcome)
			continue
		}
		fileBytes, readErr := readAllLimited(rc, 10<<20)
		rc.Close()
		if readErr != nil {
			outcome.Error = readErr.Error()
			outcomes = append(outcomes, outcome)
			continue
		}
		asset, saveErr := SaveMediaFile(tenantID, fileBytes, name, itemCode, mediaRole, uploadedBy, "", "")
		if saveErr != nil {
			outcome.Error = saveErr.Error()
			outcomes = append(outcomes, outcome)
			continue
		}
		outcome.MediaID = asset.ID
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// readAllLimited reads at most limit+1 bytes, refusing anything over limit -
// a zip entry's declared size is untrusted (a zip bomb lies about it), so
// this bounds the decompressed read itself rather than trusting the header.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds the %d MB per-entry limit", limit/(1<<20))
	}
	return data, nil
}

// BulkDownloadMediaZip zips every Active ProductMedia for the given items,
// named by the same "<ItemCode>__<role-slug>.<ext>" convention
// BulkUploadMediaZip reads - round-trippable: what this produces can be fed
// straight back into a bulk upload elsewhere.
func BulkDownloadMediaZip(tenantID string, itemCodes []string) ([]byte, error) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	seenNames := map[string]int{}
	for _, itemCode := range itemCodes {
		assets, err := ListMediaForItem(tenantID, itemCode)
		if err != nil {
			return nil, err
		}
		for _, asset := range assets {
			path, fileType, fErr := GetMediaFile(tenantID, asset.ID)
			if fErr != nil {
				continue
			}
			fileBytes, rErr := os.ReadFile(path)
			if rErr != nil {
				continue
			}
			ext := extensionForMediaFileType(fileType)
			name := fmt.Sprintf("%s__%s%s", itemCode, mediaRoleSlug(asset.MediaRole), ext)
			if n := seenNames[name]; n > 0 {
				name = fmt.Sprintf("%s__%s-%d%s", itemCode, mediaRoleSlug(asset.MediaRole), n, ext)
			}
			seenNames[name]++
			zEntry, wErr := writer.Create(name)
			if wErr != nil {
				return nil, wErr
			}
			if _, wErr := zEntry.Write(fileBytes); wErr != nil {
				return nil, wErr
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func extensionForMediaFileType(fileType string) string {
	for ext, mime := range allowedMediaExtensions {
		if mime == fileType {
			return ext
		}
	}
	return ""
}

// --- 36.6.3: tagging / search / filter (the browse UI's data layer) ------

// PIMMediaSearchFilters are all optional and AND together - the browse
// screen's filter bar maps one-to-one onto these.
type PIMMediaSearchFilters struct {
	Item      string
	MediaRole string
	Tag       string
	FileType  string
}

// SearchMedia is the catalog-wide counterpart to ListMediaForItem (which is
// scoped to one item for the Workbench gallery) - the browse UI's one read,
// Active-only like every other media read in this file.
func SearchMedia(tenantID string, filters PIMMediaSearchFilters) ([]ProductMediaAsset, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT id, COALESCE(data->>'item',''), COALESCE(data->>'media_role',''), COALESCE(data->>'file_type',''),
			COALESCE(data->>'checksum',''), status, COALESCE(NULLIF(data->>'version_no','')::int, 1),
			COALESCE(data->>'alt_text',''), COALESCE(data->>'expiry_date',''), COALESCE(data->>'has_thumbnail','') = 'Yes',
			COALESCE(data->>'tags','')
		FROM %s.documents WHERE doctype = 'ProductMedia' AND status = 'Active'`, schema)
	var args []interface{}
	if filters.Item != "" {
		args = append(args, filters.Item)
		query += fmt.Sprintf(" AND data->>'item' = $%d", len(args))
	}
	if filters.MediaRole != "" {
		args = append(args, filters.MediaRole)
		query += fmt.Sprintf(" AND data->>'media_role' = $%d", len(args))
	}
	if filters.FileType != "" {
		args = append(args, filters.FileType)
		query += fmt.Sprintf(" AND data->>'file_type' = $%d", len(args))
	}
	if filters.Tag != "" {
		args = append(args, "%"+strings.ToLower(filters.Tag)+"%")
		query += fmt.Sprintf(" AND LOWER(COALESCE(data->>'tags','')) LIKE $%d", len(args))
	}
	query += " ORDER BY id"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductMediaAsset
	for rows.Next() {
		var m ProductMediaAsset
		if err := rows.Scan(&m.ID, &m.Item, &m.MediaRole, &m.FileType, &m.Checksum, &m.Status,
			&m.VersionNo, &m.AltText, &m.ExpiryDate, &m.HasThumbnail, &m.Tags); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if out == nil {
		out = []ProductMediaAsset{}
	}
	return out, rows.Err()
}
