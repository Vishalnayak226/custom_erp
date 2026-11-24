package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 26.5.13 (WMS Enterprise Maturity Sprint P2 follow-up): labor
// standards/productivity dashboard. Go-ahead given 2026-07-27 for all five
// P2 bundles previously deferred pending a real warehouse-scale pilot.
//
// logTaskCompletionTx is called from PutawayToBin (wms.go), PackTransferOrder
// (transfer_orders.go), and PostCycleCountAdjustment (wms.go) - the three
// existing WMS actions that already carry a userID. Picking is a
// documented, deliberate gap: engines/fulfillment_pickpack.go's
// ScanPickItem has no per-user tracking today, and adding it would mean
// changing that function's signature across every caller.
//
// 252.1: the row is written in the action's own transaction. It began as
// instrumentation whose errors were swallowed, but the 3PL handling charge
// (wms_3pl_billing.go) and the labour reports count these rows, so a dropped
// one under-billed with no trace; now the action and its row commit together.
// The row's created_by must be a real user (FK to users). An ID with no user
// row (a service caller, an import) cannot be stored: a savepoint drops only
// the row, the action still commits, and System Errors records the miss.

func logTaskCompletionTx(tx *sql.Tx, tenantID, schema, taskType, userID, locationCode, referenceID string, qty float64) error {
	if userID == "" {
		return nil
	}
	id := NewDocIDCompact("TCL")
	docData := map[string]interface{}{
		"id": id, "code": id,
		"task_type": taskType, "user_id": userID, "qty": qty,
	}
	if locationCode != "" {
		docData["location_code"] = locationCode
	}
	if referenceID != "" {
		docData["reference_id"] = referenceID
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return err
	}
	if _, err := tx.Exec("SAVEPOINT task_completion_log"); err != nil {
		return fmt.Errorf("task completion log: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'TaskCompletionLog', $2, 'Active', $3)`, schema),
		id, marshaled, userID); err != nil {
		if _, rbErr := tx.Exec("ROLLBACK TO SAVEPOINT task_completion_log"); rbErr != nil {
			return fmt.Errorf("task completion log: %w", err)
		}
		LogSystemError(tenantID, "", "WARN", "TaskCompletionLog", fmt.Sprintf(
			"%s %s by %s: no task completion row (labour and 3PL handling counts miss it): %v",
			taskType, referenceID, userID, err), "")
		return nil
	}
	if _, err := tx.Exec("RELEASE SAVEPOINT task_completion_log"); err != nil {
		return fmt.Errorf("task completion log: %w", err)
	}
	return nil
}

// LaborProductivity (26.5.13) aggregates TaskCompletionLog per user per
// task type - tasks/hour is computed against the wall-clock span between
// this user's first and last logged task of that type in the window
// (a simple, honest approximation - not a shift-roster-aware standard-time
// engine, since no per-task-type standard-time target exists anywhere else
// in this codebase to compare against yet).
type LaborProductivity struct {
	UserID        string  `json:"user_id"`
	TaskType      string  `json:"task_type"`
	TaskCount     int     `json:"task_count"`
	FirstTaskAt   string  `json:"first_task_at"`
	LastTaskAt    string  `json:"last_task_at"`
	TasksPerHour  float64 `json:"tasks_per_hour"`
}

func GetLaborProductivity(tenantID, start, end string) ([]LaborProductivity, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'user_id', ''), COALESCE(data->>'task_type', ''), COUNT(*), MIN(created_at), MAX(created_at)
		FROM %s.documents
		WHERE doctype = 'TaskCompletionLog'
		  AND ($1 = '' OR created_at >= public.erp_day_start($1::date))
		  AND ($2 = '' OR created_at < public.erp_day_start($2::date + 1))
		GROUP BY data->>'user_id', data->>'task_type'
		ORDER BY data->>'user_id', data->>'task_type'`, schema), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LaborProductivity{}
	for rows.Next() {
		var p LaborProductivity
		var first, last time.Time
		if err := rows.Scan(&p.UserID, &p.TaskType, &p.TaskCount, &first, &last); err != nil {
			continue
		}
		p.FirstTaskAt = first.Format("2006-01-02 15:04")
		p.LastTaskAt = last.Format("2006-01-02 15:04")
		spanHours := last.Sub(first).Hours()
		if spanHours > 0 {
			p.TasksPerHour = roundTo2(float64(p.TaskCount) / spanHours)
		} else {
			p.TasksPerHour = float64(p.TaskCount) // all logged within the same minute - report the raw count rather than divide by ~0
		}
		out = append(out, p)
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "labor-productivity", Label: "Labor Productivity", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "user_id", Label: "User"}, {Key: "task_type", Label: "Task Type"},
			{Key: "task_count", Label: "Tasks"}, {Key: "tasks_per_hour", Label: "Tasks/Hour"},
			{Key: "first_task_at", Label: "First Task"}, {Key: "last_task_at", Label: "Last Task"},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From (optional)", Type: "date"},
			{Key: "end", Label: "To (optional)", Type: "date"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetLaborProductivity(tenantID, params["start"], params["end"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
