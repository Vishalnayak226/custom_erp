package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Stage 42.4.1/42.4.2 - Wave/WaveTemplate: promotes 26.5.6's free-text
// FulfillmentTask.wave_id tag into a real, governed object with a lifecycle
// (Planned -> Released -> In Progress -> Complete -> Closed) and a
// rule-based auto-creation path. AssignTasksToWave/GenerateWavePickList
// (engines/wms_picking.go) are unchanged in shape - a Wave's id IS the
// wave_id value those two already tag/read, so every pre-42.4 wave (a bare
// string nobody registered) keeps working exactly as before. What's new is
// that GenerateWavePickList now refuses to run against a *registered* Wave
// that isn't yet Released/In Progress (waveDispatchGate below) - "release is
// the point tasks become dispatchable," from the plan text, realised at the
// one place picking actually starts in this codebase, since there is no
// separate GetNextTask-style dispatch step for FulfillmentTask picks.

// WaveInfo is one Wave document, flattened.
type WaveInfo struct {
	DocID        string `json:"doc_id"`
	WaveTemplate string `json:"wave_template,omitempty"`
	LocationCode string `json:"location_code"`
	Status       string `json:"status"`
	CreatedVia   string `json:"created_via"`
	TaskCount    int    `json:"task_count"`
	Notes        string `json:"notes,omitempty"`
}

// CreateWave (42.4.1) registers a new Wave, always starting Planned, and
// tags the caller-chosen FulfillmentTask ids into it via the existing
// AssignTasksToWave - so a Wave document and the wave_id its tasks carry are
// never two different things to keep in sync.
func CreateWave(tenantID, locationCode, waveTemplateID string, taskIDs []string, createdVia, userID string) (string, int, error) {
	if strings.TrimSpace(locationCode) == "" {
		return "", 0, errors.New("location_code is required")
	}
	if createdVia != "Manual" && createdVia != "Auto" {
		createdVia = "Manual"
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	waveID := NewDocID("WAVE")
	data := map[string]interface{}{
		"code": waveID, "wave_template": waveTemplateID, "location_code": locationCode,
		"status": "Planned", "created_via": createdVia, "task_count": len(taskIDs),
	}
	payload, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'Wave', $2, 'Planned', $3)`, schema), waveID, payload, userID); err != nil {
		return "", 0, err
	}
	tagged := 0
	if len(taskIDs) > 0 {
		tagged, err = AssignTasksToWave(tenantID, waveID, taskIDs, userID)
		if err != nil {
			return waveID, 0, err
		}
		if tagged != len(taskIDs) {
			_, _ = db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = data || jsonb_build_object('task_count', $1::int) WHERE doctype = 'Wave' AND id = $2`, schema),
				tagged, waveID)
		}
	}
	LogAuditEvent(tenantID, userID, "WMS_WAVE_CREATE", "SUCCESS", fmt.Sprintf("Created wave %s (%s) at %s with %d task(s)", waveID, createdVia, locationCode, tagged))
	return waveID, tagged, nil
}

func getWave(schema, waveID string) (*WaveInfo, error) {
	var dataStr, status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, COALESCE(status, '') FROM %s.documents WHERE doctype = 'Wave' AND id = $1`, schema), waveID).Scan(&dataStr, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	info := &WaveInfo{DocID: waveID, Status: status}
	info.WaveTemplate, _ = data["wave_template"].(string)
	info.LocationCode, _ = data["location_code"].(string)
	info.CreatedVia, _ = data["created_via"].(string)
	info.TaskCount = int(numFromInterface(data["task_count"]))
	info.Notes, _ = data["notes"].(string)
	return info, nil
}

// waveDispatchGate (42.4.2) is called from GenerateWavePickList
// (engines/wms_picking.go) before it builds a pick list. A waveID that
// doesn't resolve to a registered Wave document is left alone entirely -
// every wave created before this Stage, or ever created by a caller that
// skips the Wave object, dispatches exactly as it always did.
func waveDispatchGate(tenantID, waveID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	wave, err := getWave(schema, waveID)
	if err != nil || wave == nil {
		return nil
	}
	if wave.Status != "Released" && wave.Status != "In Progress" {
		return fmt.Errorf("wave %s is %s - it must be Released before it can be picked", waveID, wave.Status)
	}
	if wave.Status == "Released" {
		// First pick list pull against a Released wave is what "in progress"
		// means operationally - best-effort, never fails the pick list itself.
		if terr := TransitionWaveStatus(tenantID, waveID, "In Progress", "system"); terr != nil {
			LogSystemError(tenantID, "", "WARN", "waveDispatchGate", fmt.Sprintf("failed to auto-advance wave %s to In Progress: %v", waveID, terr), "")
		}
	}
	return nil
}

// TransitionWaveStatus (42.4.2) is the one choke point that legally advances
// a Wave - Planned -> Released -> In Progress -> Complete -> Closed, no
// skipping, no going back, mirroring validateWaveMasterRules'
// (master_data_validation.go) own guard against the generic doc-save path
// being used to do the same thing without these checks. Moving to Complete
// additionally requires every FulfillmentTask this wave tagged to have
// reached a terminal status (Packed/Dispatched/Rejected) - a Wave claiming
// to be Complete while it still has open picking work would be a false
// signal to the cockpit and to 42.4.9's Bill of Lading assembly.
func TransitionWaveStatus(tenantID, waveID, newStatus, userID string) error {
	rank, ok := waveStatusOrder[newStatus]
	if !ok {
		return fmt.Errorf("status must be one of Planned, Released, In Progress, Complete or Closed")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	wave, err := getWave(schema, waveID)
	if err != nil {
		return err
	}
	if wave == nil {
		return fmt.Errorf("wave %s not found", waveID)
	}
	priorRank, ok := waveStatusOrder[wave.Status]
	if !ok || rank != priorRank+1 {
		return fmt.Errorf("a wave can only move forward one step at a time (%s -> %s is not valid)", wave.Status, newStatus)
	}
	if newStatus == "Complete" {
		var openCount int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'FulfillmentTask' AND data->>'wave_id' = $1 AND status NOT IN ('Packed', 'Dispatched', 'Rejected')`, schema),
			waveID).Scan(&openCount); err != nil {
			return err
		}
		if openCount > 0 {
			return fmt.Errorf("wave %s still has %d task(s) not yet Packed/Dispatched/Rejected", waveID, openCount)
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('status', $1::text), status = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Wave' AND id = $2`, schema),
		newStatus, waveID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_WAVE_STATUS_CHANGE", "SUCCESS", fmt.Sprintf("Wave %s: %s -> %s", waveID, wave.Status, newStatus))
	return nil
}

// WaveMonitorRow is one wave's status for the cockpit's wave-monitor panel.
type WaveMonitorRow struct {
	WaveID     string `json:"wave_id"`
	Status     string `json:"status"`
	CreatedVia string `json:"created_via"`
	TaskCount  int    `json:"task_count"`
	OpenTasks  int    `json:"open_tasks"`
	AgeMins    int    `json:"age_mins"`
}

// GetWaveMonitor (42.4.2) lists every non-Closed wave at a location, newest
// first, with a live open-task count alongside the task_count captured at
// creation - the pair a warehouse manager needs to see "is this wave stuck".
func GetWaveMonitor(tenantID, locationCode string) ([]WaveMonitorRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT w.id, w.status, COALESCE(w.data->>'created_via', ''), COALESCE((w.data->>'task_count')::int, 0),
		       COALESCE((SELECT COUNT(*) FROM %s.documents ft WHERE ft.doctype = 'FulfillmentTask' AND ft.data->>'wave_id' = w.id AND ft.status NOT IN ('Packed', 'Dispatched', 'Rejected')), 0),
		       COALESCE(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - w.created_at)) / 60, 0)
		FROM %s.documents w
		WHERE w.doctype = 'Wave' AND w.data->>'location_code' = $1 AND w.status != 'Closed'
		ORDER BY w.created_at DESC`, schema, schema), locationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WaveMonitorRow{}
	for rows.Next() {
		var r WaveMonitorRow
		var ageMins float64
		if err := rows.Scan(&r.WaveID, &r.Status, &r.CreatedVia, &r.TaskCount, &r.OpenTasks, &ageMins); err != nil {
			return nil, err
		}
		r.AgeMins = int(ageMins)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 42.4.1 - rule-based wave creation, manual "run now" and the daily scheduler.
// ---------------------------------------------------------------------------

// RunWaveTemplateAutoCreate (42.4.1) resolves waveTemplateID's criteria
// against every still-unwaved, open FulfillmentTask at its location and, if
// any match, creates a Wave from them - the same mechanism whether it is
// triggered by a manager's "Run Now" button or by the daily scheduler below.
// Matching is deliberately best-effort per criterion (see the migration's
// own header on why carrier/order_type are not matched): a FulfillmentTask
// whose order_id does not resolve to a real SalesOrder simply cannot fail a
// channel/service_level criterion it has no data for, and is treated as a
// non-match only when the template actually specifies one.
func RunWaveTemplateAutoCreate(tenantID, waveTemplateID, userID string) (string, int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	var dataStr string
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'WaveTemplate' AND id = $1`, schema), waveTemplateID).Scan(&dataStr, &status); err != nil {
		if err == sql.ErrNoRows {
			return "", 0, fmt.Errorf("wave template %s not found", waveTemplateID)
		}
		return "", 0, err
	}
	if status != "Active" {
		return "", 0, fmt.Errorf("wave template %s is not Active", waveTemplateID)
	}
	var tmpl map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &tmpl)
	locationCode := strField(tmpl, "location_code")
	channel := strField(tmpl, "channel")
	serviceLevel := strField(tmpl, "service_level")
	cutoff := strField(tmpl, "cutoff_time")

	query := fmt.Sprintf(`
		SELECT ft.id
		FROM %s.documents ft
		LEFT JOIN %s.documents so ON so.doctype = 'SalesOrder' AND so.id = ft.data->>'order_id'
		WHERE ft.doctype = 'FulfillmentTask' AND ft.status = 'Pending'
		  AND COALESCE(ft.data->>'wave_id', '') = ''
		  AND ($1 = '' OR ft.data->>'location_code' = $1)
		  AND ($2 = '' OR COALESCE(so.data->>'channel', '') = $2)
		  AND ($3 = '' OR $3 = 'Any' OR COALESCE(so.data->>'priority', 'Normal') = $3)`, schema, schema)
	args := []interface{}{locationCode, channel, serviceLevel}
	if cutoff != "" {
		if cutoffT, perr := time.Parse("15:04", cutoff); perr == nil {
			today := time.Now().Format("2006-01-02")
			deadline := today + " " + cutoffT.Format("15:04:00")
			query += fmt.Sprintf(" AND ft.created_at <= $%d::timestamp", len(args)+1)
			args = append(args, deadline)
		}
	}
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return "", 0, err
	}
	var taskIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", 0, err
		}
		taskIDs = append(taskIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	if len(taskIDs) == 0 {
		return "", 0, nil // nothing due - not an error, just nothing to wave right now
	}
	waveLocation := locationCode
	if waveLocation == "" {
		waveLocation = "ALL"
	}
	waveID, tagged, err := CreateWave(tenantID, waveLocation, waveTemplateID, taskIDs, "Auto", userID)
	if err != nil {
		return "", 0, err
	}
	return waveID, tagged, nil
}

// StartWaveAutoCreationWorker (42.4.1, open decision 5) polls every tenant
// schema once per interval for Active WaveTemplates whose run_daily_at has
// arrived today and haven't already run today - same ticker/schema-fanout
// shape as StartScheduledReportWorker (engines/scheduled_reports.go), the
// scheduler precedent the plan named. A template with a blank run_daily_at
// is manual-only and is never touched by this worker.
func StartWaveAutoCreationWorker(ctx context.Context, interval time.Duration) {
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
					log.Printf("[WAVE_AUTO_CREATE] failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					processWaveTemplateAutoRun(schema)
				}
			}
		}
	}()
}

func processWaveTemplateAutoRun(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data->>'run_daily_at', COALESCE(data->>'last_auto_run_date', '')
		 FROM %s.documents WHERE doctype = 'WaveTemplate' AND status = 'Active' AND COALESCE(data->>'run_daily_at', '') != ''`, schema))
	if err != nil {
		log.Printf("[WAVE_AUTO_CREATE] query failed for %s: %v", schema, err)
		return
	}
	type due struct{ id, runAt string }
	var candidates []due
	now := time.Now()
	today := now.Format("2006-01-02")
	for rows.Next() {
		var id, runAt, lastRun string
		if err := rows.Scan(&id, &runAt, &lastRun); err != nil {
			continue
		}
		if lastRun == today {
			continue
		}
		runT, perr := time.Parse("15:04", runAt)
		if perr != nil {
			continue
		}
		if now.Hour() < runT.Hour() || (now.Hour() == runT.Hour() && now.Minute() < runT.Minute()) {
			continue
		}
		candidates = append(candidates, due{id: id, runAt: runAt})
	}
	rows.Close()

	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return
	}
	for _, c := range candidates {
		if _, _, err := RunWaveTemplateAutoCreate(tenantID, c.id, "system"); err != nil {
			LogSystemError(tenantID, "", "WARN", "processWaveTemplateAutoRun",
				fmt.Sprintf("auto wave creation failed for template %s: %v", c.id, err), "")
		}
		_, _ = db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = data || jsonb_build_object('last_auto_run_date', $1::text) WHERE doctype = 'WaveTemplate' AND id = $2`, schema),
			today, c.id)
	}
}
