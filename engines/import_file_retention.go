package engines

import (
	"custom_erp/db"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stage 47.18.3: bounded retention for raw import files. Two places keep
// the bytes a client uploaded:
//
//   - a scheduled import (pim_import_schedule.go) moves each CSV it has
//     processed into the drop folder's imported/ or failed/ subfolder, where
//     it would otherwise sit forever;
//   - an ImportJob document keeps the failed rows' original cells and the
//     error file so the job can be resumed (Stage 55.11).
//
// Both are swept hourly on the leader after platform.import_file_retention_days
// (30 by default; 0 keeps everything - a hold, evidenced by the settings
// audit line that names the key and value). A job keeps its counts, control
// totals and source lineage: only the raw rows go, and the job says when.
// Nothing is swept while the client is under legal hold, the same rule as
// every other retention sweep (retentionDelete).

const importFileRetentionSetting = "platform.import_file_retention_days"

// ImportFileSweepResult counts what one sweep removed.
type ImportFileSweepResult struct {
	Files int64 // CSVs deleted from drop folders
	Jobs  int64 // ImportJob documents whose raw rows were stripped
}

// SweepImportFileRetention removes raw import files older than the
// tenant's retention. A retention of 0 (a hold) and a legal hold both sweep
// nothing.
func SweepImportFileRetention(tenantID string) (ImportFileSweepResult, error) {
	var out ImportFileSweepResult
	days := GetSettingInt(tenantID, importFileRetentionSetting)
	if days <= 0 {
		return out, nil // a hold
	}
	if held, err := tenantLegalHold(tenantID); err != nil || held {
		return out, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	// Drop folders: every schedule's imported/ and failed/ subfolders.
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'source_path', '') FROM %s.documents
		WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL`, schema))
	if err != nil {
		return out, err
	}
	var folders []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && strings.TrimSpace(p) != "" {
			folders = append(folders, strings.TrimSpace(p))
		}
	}
	rows.Close()
	for _, folder := range folders {
		out.Files += sweepImportDropFolder(folder, cutoff)
	}

	// Import jobs: the raw rows and error file go; the job stays.
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = (data - 'open_failed_rows' - 'source_header' - 'error_csv')
		           || jsonb_build_object('raw_rows_expired_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), 'resumable', false),
		    updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'ImportJob' AND created_at < $1
		  AND (data ? 'open_failed_rows' OR data ? 'source_header' OR COALESCE(data->>'error_csv', '') <> '')`, schema), cutoff)
	if err != nil {
		return out, err
	}
	out.Jobs, _ = res.RowsAffected()
	return out, nil
}

// sweepImportDropFolder deletes the CSVs in folder's imported/ and failed/
// subfolders last modified before cutoff - only those two subfolders, only
// .csv files, never the drop folder itself where new files arrive.
func sweepImportDropFolder(folder string, cutoff time.Time) int64 {
	var n int64
	for _, sub := range []string{"imported", "failed"} {
		dir := filepath.Join(folder, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // no such subfolder yet, or unreadable: nothing to sweep
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".csv") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.ModTime().Before(cutoff) {
				continue
			}
			if os.Remove(filepath.Join(dir, entry.Name())) == nil {
				n++
			}
		}
	}
	return n
}
