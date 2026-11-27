package engines

import (
	"bytes"
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Copy server-disk photos to the client's image drive (Stage 75.4.3).
//
// Photos saved before a drive was set stay on the server's disk and are read
// from there (95.3). The admin can move them: a preview counts what is on the
// disk and on the drive, then a background job copies each photo to the key
// the drive would have given it, reads it back, compares SHA-256 with the
// disk file, and only then switches the ProductMedia record to the drive key.
// A photo that fails or does not match stays on the disk, and is listed with
// its reason. The disk file is never deleted. A re-run skips what is already
// on the drive; one run at a time per client.

const imageDriveCopyJobType = "image_drive_copy"

// imageDriveCopyMaxListed bounds the failure list kept in the job result.
const imageDriveCopyMaxListed = 100

func init() {
	RegisterJobHandler(imageDriveCopyJobType, runImageDriveCopyJob)
	SetJobMaxRuntime(imageDriveCopyJobType, 2*time.Hour)
}

// ImageDriveCopyFailure is a photo that stayed on the disk.
type ImageDriveCopyFailure struct {
	MediaID string `json:"media_id"`
	Item    string `json:"item"`
	Reason  string `json:"reason"`
}

// ImageDriveCopyPreview is what the admin sees before starting, and the last
// run's outcome.
type ImageDriveCopyPreview struct {
	DriveOn     bool                    `json:"drive_on"`
	OnDisk      int                     `json:"on_disk"`
	DiskBytes   int64                   `json:"disk_bytes"`
	OnDrive     int                     `json:"on_drive"`
	Running     bool                    `json:"running"`
	LastJobID   string                  `json:"last_job_id,omitempty"`
	LastStatus  string                  `json:"last_status,omitempty"`
	LastPercent int                     `json:"last_percent"`
	Copied      int                     `json:"copied"`
	Skipped     int                     `json:"skipped"`
	Failed      int                     `json:"failed"`
	Failures    []ImageDriveCopyFailure `json:"failures,omitempty"`
}

const imageDriveCopyCandidates = `doctype = 'ProductMedia' AND status = 'Active' AND COALESCE(data->>'storage', '') <> 'drive'
	AND COALESCE(data->>'file_type', '') LIKE 'image/%'`

// diskFilePath is where a disk-stored photo lives.
func diskFilePath(stored string) string {
	if key := mediaKeyForStoredPath(stored); key != "" {
		return filepath.Join(mediaStoreDir, filepath.FromSlash(key))
	}
	return stored
}

// PreviewImageDriveCopy counts the photos on the disk (and their size) and
// on the drive, and reports the last run.
func PreviewImageDriveCopy(tenantID string) (ImageDriveCopyPreview, error) {
	var p ImageDriveCopyPreview
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return p, err
	}
	s, err := loadImageDrive(tenantID)
	if err != nil {
		return p, err
	}
	p.DriveOn = s != nil && s.Enabled
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'file_path', '') FROM %s.documents WHERE %s`, schema, imageDriveCopyCandidates))
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var stored string
		if err := rows.Scan(&stored); err != nil {
			rows.Close()
			return p, err
		}
		p.OnDisk++
		if stored != "" {
			if st, err := os.Stat(diskFilePath(stored)); err == nil {
				p.DiskBytes += st.Size()
			}
		}
	}
	rows.Close()
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ProductMedia' AND status = 'Active' AND data->>'storage' = 'drive'`, schema)).Scan(&p.OnDrive); err != nil {
		return p, err
	}
	var result sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, status, progress_pct, result::text FROM %s.async_jobs WHERE job_type = $1 ORDER BY created_at DESC LIMIT 1`, schema),
		imageDriveCopyJobType).Scan(&p.LastJobID, &p.LastStatus, &p.LastPercent, &result)
	if err != nil && err != sql.ErrNoRows {
		return p, err
	}
	p.Running = p.LastStatus == "Pending" || p.LastStatus == "Leased"
	if result.Valid {
		var res struct {
			Copied   int                     `json:"copied"`
			Skipped  int                     `json:"skipped"`
			Failed   int                     `json:"failed"`
			Failures []ImageDriveCopyFailure `json:"failures"`
		}
		if json.Unmarshal([]byte(result.String), &res) == nil {
			p.Copied, p.Skipped, p.Failed, p.Failures = res.Copied, res.Skipped, res.Failed, res.Failures
		}
	}
	return p, nil
}

// StartImageDriveCopy queues the copy. Only with the drive switched on, and
// never a second run while one is queued or running for this client.
func StartImageDriveCopy(tenantID, userID string) (string, error) {
	s, err := loadImageDrive(tenantID)
	if err != nil {
		return "", err
	}
	if s == nil || !s.Enabled {
		return "", &ValidationError{Code: "PIM-9360", Message: "tick Store new images on this drive and save first"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var active string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.async_jobs WHERE job_type = $1 AND status IN ('Pending', 'Leased') LIMIT 1`, schema), imageDriveCopyJobType).Scan(&active)
	if err == nil {
		return "", &ValidationError{Code: "PIM-9361", Message: "a copy started earlier is still going"}
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	return EnqueueJobContext(WithJobActor(context.Background(), userID), tenantID, imageDriveCopyJobType,
		map[string]interface{}{"tenant_id": tenantID}, fmt.Sprintf("%s:%d", imageDriveCopyJobType, time.Now().UnixNano()))
}

func runImageDriveCopyJob(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
	tenantID, _ := job.Payload["tenant_id"].(string)
	// 297.3: rows done and the total, so the Tasks bar shows the time left.
	res, err := copyPhotosToImageDrive(ctx, tenantID, func(done, total int) { _ = ReportJobProgress(schema, job.ID, int64(done), int64(total)) })
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"copied": res.Copied, "skipped": res.Skipped, "failed": res.Failed, "failures": res.Failures}, nil
}

type imageDriveCopyResult struct {
	Copied, Skipped, Failed int
	Failures                []ImageDriveCopyFailure
}

// copyPhotosToImageDrive does the work of one run. A photo's own failure is
// recorded and the run goes on; only a failure of the run itself (no drive,
// the database) returns an error, and a retry resumes where it stopped.
func copyPhotosToImageDrive(ctx context.Context, tenantID string, progress func(done, total int)) (imageDriveCopyResult, error) {
	var res imageDriveCopyResult
	drive, err := activeImageDrive(tenantID)
	if err != nil {
		return res, err
	}
	if drive == nil {
		return res, &ValidationError{Code: "PIM-9360", Message: "tick Store new images on this drive and save first"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE %s ORDER BY id`, schema, imageDriveCopyCandidates))
	if err != nil {
		return res, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return res, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		outcome, item, reason := copyOnePhoto(tenantID, schema, drive, id)
		switch outcome {
		case "copied":
			res.Copied++
		case "skipped":
			res.Skipped++
		default:
			res.Failed++
			if len(res.Failures) < imageDriveCopyMaxListed {
				res.Failures = append(res.Failures, ImageDriveCopyFailure{MediaID: id, Item: item, Reason: reason})
			}
		}
		if progress != nil {
			progress(i+1, len(ids))
		}
	}
	return res, nil
}

func mimeExtension(fileType string) string {
	switch fileType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ".jpg"
}

// copyOnePhoto: outcome is "copied", "skipped" or "failed" (with its reason).
func copyOnePhoto(tenantID, schema string, drive *s3Client, mediaID string) (outcome, item, reason string) {
	fail := func(format string, a ...interface{}) (string, string, string) {
		return "failed", item, fmt.Sprintf(format, a...)
	}
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ProductMedia' AND id = $1 AND status = 'Active'`, schema), mediaID).Scan(&raw); err != nil {
		return "skipped", "", ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return fail("the media record could not be read")
	}
	item = strField(data, "item")
	if mediaOnDrive(data) {
		return "skipped", item, ""
	}
	stored := strField(data, "file_path")
	if stored == "" {
		return fail("the record has no stored file")
	}
	var body []byte
	var err error
	if key := mediaKeyForStoredPath(stored); key != "" {
		body, err = serverMedia.Read(key)
	} else {
		body, err = os.ReadFile(stored)
	}
	if err != nil {
		return fail("the file is missing from the server's disk")
	}
	want := sha256.Sum256(body)
	if sum := strField(data, "checksum"); sum != "" && sum != checksumOf(body) {
		return fail("the file on the disk does not match its recorded checksum")
	}
	fileType := strField(data, "file_type")
	ext := strings.ToLower(filepath.Ext(stored))
	if ext == "" {
		ext = mimeExtension(fileType)
	}
	ownerDoctype := strField(data, "owner_doctype")
	if ownerDoctype == "" {
		ownerDoctype = "Item"
	}

	unlock := lockOwnerPhotos(tenantID, item)
	defer unlock()
	objectKey, err := drive.ownerPhotoKey(tenantID, ownerDoctype, item, ext)
	if err != nil {
		return fail("could not work out the drive address: %v", err)
	}
	if err := drive.put(objectKey, body, fileType); err != nil {
		return fail("the drive refused the photo: %v", err)
	}
	back, err := drive.get(objectKey)
	if err != nil {
		return fail("the photo could not be read back from the drive: %v", err)
	}
	if got := sha256.Sum256(back); !bytes.Equal(got[:], want[:]) {
		return fail("the photo read back from the drive differs from the disk file")
	}
	hasThumb := "No"
	if strField(data, "has_thumbnail") == "Yes" {
		thumb, terr := serverMedia.Read(thumbnailKey(strField(data, "checksum")))
		if terr != nil {
			var ok bool
			if thumb, ok = generateThumbnail(body, fileType, GetSettingInt(tenantID, "pim.thumbnail_max_dim")); !ok {
				thumb = nil
			}
		}
		if thumb != nil && drive.put(mediaDerivedKey(objectKey, "thumb"), thumb, "image/jpeg") == nil {
			hasThumb = "Yes"
		}
	}
	// Switch the record, only if it is still the one read above.
	patch, _ := json.Marshal(map[string]interface{}{"storage": "drive", "object_key": objectKey, "drive_bucket": drive.bucket(),
		"file_path": "", "disk_path": stored, "has_thumbnail": hasThumb, "owner_doctype": ownerDoctype})
	r, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'ProductMedia' AND id = $1 AND COALESCE(data->>'storage', '') <> 'drive' AND data->>'file_path' = $3`, schema), mediaID, string(patch), stored)
	if err != nil {
		return fail("the record could not be switched: %v", err)
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return "skipped", item, ""
	}
	return "copied", item, ""
}
