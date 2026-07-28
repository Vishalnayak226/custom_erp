package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// Stage 26.5.13 (WMS Enterprise Maturity Sprint P2 follow-up): labor
// standards/productivity dashboard. Go-ahead given 2026-07-27 for all five
// P2 bundles previously deferred pending a real warehouse-scale pilot.
//
// logTaskCompletion is called from PutawayToBin (wms.go), PackTransferOrder
// (transfer_orders.go), and PostCycleCountAdjustment (wms.go) - the three
// existing WMS actions that already carry a userID. Picking is a
// documented, deliberate gap: engines/fulfillment_pickpack.go's
// ScanPickItem has no per-user tracking today, and adding it would mean
// changing that function's signature across every caller. Errors are
// swallowed the same way WriteStockLedgerEntry's own callers already treat
// it - this is instrumentation, never a reason to fail the real action.
var taskCompletionLogSeq uint64

func logTaskCompletion(tenantID, taskType, userID, locationCode, referenceID string, qty float64) {
	if userID == "" {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	id := fmt.Sprintf("TCL%d%d", time.Now().UnixNano(), atomic.AddUint64(&taskCompletionLogSeq, 1)%1000)
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
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'TaskCompletionLog', $2, 'Active', $3)`, schema),
		id, marshaled, userID)
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
		SELECT data->>'user_id', data->>'task_type', COUNT(*), MIN(created_at), MAX(created_at)
		FROM %s.documents
		WHERE doctype = 'TaskCompletionLog'
		  AND ($1 = '' OR created_at >= $1::date)
		  AND ($2 = '' OR created_at < ($2::date + interval '1 day'))
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
