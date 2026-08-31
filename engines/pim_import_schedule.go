package engines

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stage 36.3.1/36.3.2/36.3.4: PIMImportSchedule drives a PIMImportTemplate
// either by a periodic scan of a configured directory, or by an inbound
// webhook token minted for it - the two delivery mechanisms this stage
// actually builds (see the migration's own comment for why a native SFTP
// client is deliberately not a third).

// ValidatePIMImportScheduleDocument runs at ValidateDocument's shared exit.
func ValidatePIMImportScheduleDocument(tenantID string, payload map[string]interface{}) error {
	template := strings.TrimSpace(pimString(payload["template"]))
	if template == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Template", Message: "an import schedule needs a template"}
	}
	if db.DB != nil {
		if _, _, _, err := fetchPIMImportTemplate(tenantID, template); err != nil {
			return &ValidationError{Code: "META-0198", SubFor: "Template", Message: err.Error()}
		}
	}

	sourceType := strings.TrimSpace(pimString(payload["source_type"]))
	switch sourceType {
	case "Drop Directory":
		if strings.TrimSpace(pimString(payload["source_path"])) == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Source Path", Message: "a Drop Directory schedule needs a directory path"}
		}
		frequency := strings.TrimSpace(pimString(payload["frequency"]))
		if frequency != "Daily" && frequency != "Weekly" && frequency != "Monthly" {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Frequency", Message: "frequency must be Daily, Weekly or Monthly"}
		}
		if err := validateISODate("Next Run Date", pimString(payload["next_run_date"]), true); err != nil {
			return err
		}
	case "API Hook":
		// source_path/frequency/next_run_date are Drop-Directory-only - left
		// set on an API Hook schedule, they would describe a scan that never
		// runs, which is confusing rather than merely unused.
		for label, value := range map[string]string{
			"Drop Directory Path": pimString(payload["source_path"]),
			"Frequency":           pimString(payload["frequency"]),
			"Next Run Date":       pimString(payload["next_run_date"]),
		} {
			if strings.TrimSpace(value) != "" {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: label, Message: fmt.Sprintf("%s only applies to a Drop Directory schedule", label)}
			}
		}
	default:
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Source Type", Message: "source_type must be 'Drop Directory' or 'API Hook'"}
	}
	return nil
}

func fetchPIMImportScheduleData(tenantID, scheduleID string) (canonicalID string, data map[string]interface{}, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PIMImportSchedule' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), scheduleID).Scan(&canonicalID, &raw)
	if err != nil {
		return "", nil, fmt.Errorf("import schedule %q not found", scheduleID)
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", nil, fmt.Errorf("import schedule %q has invalid stored data: %w", scheduleID, err)
	}
	return canonicalID, data, nil
}

// PIMImportScheduleInfo is the list-facing view of a schedule - deliberately
// never carries hook_token_hash, the same "the digest itself is not the
// interesting leak, but it still has no business on a list screen" posture
// Stage 38.2's api_credentials list response takes.
type PIMImportScheduleInfo struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Template      string `json:"template"`
	SourceType    string `json:"source_type"`
	SourcePath    string `json:"source_path,omitempty"`
	Frequency     string `json:"frequency,omitempty"`
	NextRunDate   string `json:"next_run_date,omitempty"`
	HasHookToken  bool   `json:"has_hook_token"`
	LastRunAt     string `json:"last_run_at,omitempty"`
	LastRunStatus string `json:"last_run_status,omitempty"`
	Status        string `json:"status"`
}

// ListPIMImportSchedules backs the Import Schedules screen.
func ListPIMImportSchedules(tenantID string) ([]PIMImportScheduleInfo, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL ORDER BY COALESCE(data->>'name', id)`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMImportScheduleInfo{}
	for rows.Next() {
		var id, raw, status string
		if sErr := rows.Scan(&id, &raw, &status); sErr != nil {
			return nil, sErr
		}
		var data map[string]interface{}
		if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
			continue
		}
		out = append(out, PIMImportScheduleInfo{
			ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
			Template: pimString(data["template"]), SourceType: pimString(data["source_type"]),
			SourcePath: pimString(data["source_path"]), Frequency: pimString(data["frequency"]),
			NextRunDate: pimString(data["next_run_date"]), HasHookToken: pimString(data["hook_token_hash"]) != "",
			LastRunAt: pimString(data["last_run_at"]), LastRunStatus: pimString(data["last_run_status"]),
			Status: status,
		})
	}
	return out, rows.Err()
}

// RotatePIMImportHookToken mints a fresh 256-bit token for an API Hook
// schedule, stores only its SHA-256 digest, and returns the raw token
// exactly once - the same crypto/rand + digest-only shape Stage 38.2a's API
// keys use. Rotating immediately invalidates whatever token was minted
// before it, the same as a key rotation does.
func RotatePIMImportHookToken(tenantID, scheduleID string) (rawToken string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	canonicalID, data, err := fetchPIMImportScheduleData(tenantID, scheduleID)
	if err != nil {
		return "", err
	}
	if pimString(data["source_type"]) != "API Hook" {
		return "", fmt.Errorf("import schedule %q is not an API Hook schedule", scheduleID)
	}

	buf := make([]byte, 32)
	if _, rErr := rand.Read(buf); rErr != nil {
		return "", rErr
	}
	rawToken = hex.EncodeToString(buf)
	digest := sha256.Sum256([]byte(rawToken))

	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{hook_token_hash}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'PIMImportSchedule' AND id = $2`, schema), hex.EncodeToString(digest[:]), canonicalID)
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

// ResolvePIMImportScheduleByHookToken is the API hook handler's one lookup:
// hash the caller-supplied token and find the (unique, Active) schedule
// whose stored digest matches, scoped to the caller's own tenant. A caller
// naming the wrong tenant sees exactly the same "not found" a wrong token
// does - which tenant a given token belongs to is not information this
// endpoint leaks either way.
func ResolvePIMImportScheduleByHookToken(tenantID, rawToken string) (scheduleID, template string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256([]byte(rawToken))

	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data->>'template', COALESCE(data->>'hook_token_hash', '')
		FROM %s.documents WHERE doctype = 'PIMImportSchedule' AND status = 'Active'
		AND data->>'source_type' = 'API Hook' AND deleted_at IS NULL`, schema))
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, tmpl, storedHex string
		if sErr := rows.Scan(&id, &tmpl, &storedHex); sErr != nil {
			continue
		}
		if storedHex == "" {
			continue
		}
		stored, decErr := hex.DecodeString(storedHex)
		if decErr != nil {
			continue
		}
		if subtle.ConstantTimeCompare(stored, digest[:]) == 1 {
			return id, tmpl, nil
		}
	}
	return "", "", fmt.Errorf("no active API Hook schedule matches this token")
}

// StartPIMImportScheduleWorker polls every tenant schema for Active,
// Drop-Directory PIMImportSchedules whose next_run_date has arrived - same
// ticker/schema-fanout shape as StartScheduledReportWorker/
// StartWaveAutoCreationWorker rather than a third timer mechanism.
func StartPIMImportScheduleWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[PIM_IMPORT_SCHEDULE] failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processPIMImportSchedules(schema)
				}
			}
		}
	}()
}

func processPIMImportSchedules(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'PIMImportSchedule' AND status = 'Active'
		 AND data->>'source_type' = 'Drop Directory'`, schema))
	if err != nil {
		log.Printf("[PIM_IMPORT_SCHEDULE] query failed for %s: %v", schema, err)
		return
	}
	type due struct {
		id   string
		data map[string]interface{}
	}
	var candidates []due
	today := time.Now().Format("2006-01-02")
	for rows.Next() {
		var id, raw string
		if sErr := rows.Scan(&id, &raw); sErr != nil {
			continue
		}
		var data map[string]interface{}
		if uErr := json.Unmarshal([]byte(raw), &data); uErr != nil {
			continue
		}
		if nextRun := pimString(data["next_run_date"]); nextRun != "" && nextRun <= today {
			candidates = append(candidates, due{id: id, data: data})
		}
	}
	rows.Close()
	if len(candidates) == 0 {
		return
	}

	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return
	}
	for _, c := range candidates {
		runPIMImportScheduleDropDirectory(tenantID, c.id, c.data)
	}
}

// runPIMImportScheduleDropDirectory processes every file currently sitting
// in the schedule's source_path, moving each into an imported/ or failed/
// subfolder afterwards - never deleted, so a run is always reviewable after
// the fact. One ImportJob per file, through the existing RecordImportJob,
// so a scheduled run is auditable the identical way a manual upload is.
func runPIMImportScheduleDropDirectory(tenantID, scheduleID string, data map[string]interface{}) {
	template := pimString(data["template"])
	_, targetDoctype, _, tErr := fetchPIMImportTemplate(tenantID, template)
	if tErr != nil {
		LogSystemError(tenantID, "", "WARN", "runPIMImportScheduleDropDirectory",
			fmt.Sprintf("schedule %s: template %q: %v", scheduleID, template, tErr), "")
		return
	}
	sourcePath := pimString(data["source_path"])
	entries, err := os.ReadDir(sourcePath)
	overallStatus := "Completed"
	if err != nil {
		overallStatus = "Failed: " + err.Error()
		LogSystemError(tenantID, "", "WARN", "runPIMImportScheduleDropDirectory",
			fmt.Sprintf("schedule %s: cannot read %q: %v", scheduleID, sourcePath, err), "")
	} else {
		importedDir := filepath.Join(sourcePath, "imported")
		failedDir := filepath.Join(sourcePath, "failed")
		processedAny := false
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".csv") {
				continue
			}
			processedAny = true
			filePath := filepath.Join(sourcePath, entry.Name())
			f, openErr := os.Open(filePath)
			if openErr != nil {
				LogSystemError(tenantID, "", "WARN", "runPIMImportScheduleDropDirectory",
					fmt.Sprintf("schedule %s: cannot open %q: %v", scheduleID, filePath, openErr), "")
				continue
			}
			// "" role: a scheduled worker run has no operator session/role to
			// check field-write restrictions against, same as the hook path.
			result, runErr := RunPIMImportTemplate(tenantID, template, f, "system", "", false)
			f.Close()

			destDir := importedDir
			if runErr != nil || result == nil || result.SuccessRows == 0 {
				destDir = failedDir
			}
			if mkErr := os.MkdirAll(destDir, 0o755); mkErr == nil {
				_ = os.Rename(filePath, filepath.Join(destDir, entry.Name()))
			}

			if runErr != nil {
				LogSystemError(tenantID, "", "WARN", "runPIMImportScheduleDropDirectory",
					fmt.Sprintf("schedule %s: import of %q failed: %v", scheduleID, entry.Name(), runErr), "")
				continue
			}
			if _, jobErr := RecordImportJob(tenantID, targetDoctype, result, "system"); jobErr != nil {
				LogSystemError(tenantID, "", "WARN", "runPIMImportScheduleDropDirectory",
					fmt.Sprintf("schedule %s: could not record import job for %q: %v", scheduleID, entry.Name(), jobErr), "")
			}
		}
		if !processedAny {
			overallStatus = "Completed: no files waiting"
		}
	}

	nextRun := pimString(data["next_run_date"])
	frequency := pimString(data["frequency"])
	data["next_run_date"] = advanceScheduleDate(nextRun, frequency)
	data["last_run_at"] = time.Now().Format(time.RFC3339)
	data["last_run_status"] = overallStatus
	marshaled, mErr := json.Marshal(data)
	if mErr != nil {
		return
	}
	schema, sErr := db.GetTenantSchema(tenantID)
	if sErr != nil {
		return
	}
	if _, uErr := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PIMImportSchedule' AND id = $2`, schema),
		marshaled, scheduleID); uErr != nil {
		log.Printf("[PIM_IMPORT_SCHEDULE] update failed for %s: %v", scheduleID, uErr)
	}
}
