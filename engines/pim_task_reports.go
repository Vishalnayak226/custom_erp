package engines

import (
	"custom_erp/db"
	"fmt"
)

// ---------------------------------------------------------------------------
// Stage 36.2 - the task and workflow engine's registered reports.
//
// These go through RegisterReport rather than being three more bespoke
// endpoints, which means they arrive with the report catalog's existing
// screen, parameter form, CSV export, scheduling, column-level redaction and
// permission handling already attached. That is the whole reason the report
// framework exists, and a task module that grew its own reporting surface
// beside it would be the third way of doing the same thing.
// ---------------------------------------------------------------------------

func init() {
	RegisterReport(ReportDefinition{
		ID: "pim-task-workload", Label: "PIM Task Workload by Assignee", Category: "PIM",
		Columns: []ReportColumn{
			{Key: "assignee", Label: "Assignee"},
			{Key: "open_tasks", Label: "Open"},
			{Key: "in_progress", Label: "In Progress"},
			{Key: "blocked", Label: "Blocked"},
			{Key: "overdue", Label: "Overdue"},
			{Key: "due_this_week", Label: "Due This Week"},
		},
		Run: runPIMTaskWorkloadReport,
	})

	RegisterReport(ReportDefinition{
		ID: "pim-task-overdue", Label: "PIM Overdue Tasks", Category: "PIM",
		Columns: []ReportColumn{
			{Key: "task_id", Label: "Task"},
			{Key: "title", Label: "Title"},
			{Key: "item_code", Label: "Item"},
			{Key: "assignee", Label: "Assignee"},
			{Key: "due_date", Label: "Due"},
			{Key: "days_overdue", Label: "Days Overdue"},
			{Key: "priority", Label: "Priority"},
			{Key: "status", Label: "Status"},
		},
		Params: []ReportParam{
			{Key: "assignee", Label: "Assignee (blank = everyone)", Type: "text"},
		},
		Run: runPIMOverdueTaskReport,
	})

	// The counterpart to the task reports: a run that has stopped moving is
	// invisible in a task list, because a blocked run has no open tasks - that
	// is precisely why it is stuck. Without this report the failure mode is a
	// product that quietly sits in stage 2 forever and nobody notices.
	RegisterReport(ReportDefinition{
		ID: "pim-workflow-stalled", Label: "PIM Stalled Workflow Runs", Category: "PIM",
		Columns: []ReportColumn{
			{Key: "run_id", Label: "Run"},
			{Key: "workflow", Label: "Workflow"},
			{Key: "item_code", Label: "Item"},
			{Key: "current_stage", Label: "Stage"},
			{Key: "status", Label: "Status"},
			{Key: "blocked_reason", Label: "Why It Is Waiting"},
			{Key: "age_days", Label: "Age (days)"},
			{Key: "open_tasks", Label: "Open Tasks"},
		},
		Run: runPIMStalledWorkflowReport,
	})
}

func runPIMTaskWorkloadReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Aggregated in SQL rather than by reading every task into Go: this is the
	// report a manager opens on a catalogue with tens of thousands of tasks.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(data->>'assignee', ''), '(unassigned)') AS assignee,
		       COUNT(*) FILTER (WHERE status = 'Open')::int,
		       COUNT(*) FILTER (WHERE status = 'In Progress')::int,
		       COUNT(*) FILTER (WHERE status = 'Blocked')::int,
		       COUNT(*) FILTER (WHERE NULLIF(data->>'due_date','') IS NOT NULL
		                          AND (data->>'due_date')::date < CURRENT_DATE)::int,
		       COUNT(*) FILTER (WHERE NULLIF(data->>'due_date','') IS NOT NULL
		                          AND (data->>'due_date')::date BETWEEN CURRENT_DATE AND CURRENT_DATE + 7)::int
		  FROM %s.documents
		 WHERE doctype = 'PIMTask' AND deleted_at IS NULL
		   AND status IN ('Open', 'In Progress', 'Blocked')
		 GROUP BY 1
		 ORDER BY 5 DESC, 2 DESC, 1`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var assignee string
		var open, inProgress, blocked, overdue, dueSoon int
		if err := rows.Scan(&assignee, &open, &inProgress, &blocked, &overdue, &dueSoon); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"assignee": assignee, "open_tasks": open, "in_progress": inProgress,
			"blocked": blocked, "overdue": overdue, "due_this_week": dueSoon,
		})
	}
	return out, rows.Err()
}

func runPIMOverdueTaskReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT id,
		       COALESCE(data->>'title', ''),
		       COALESCE(data->>'item_code', ''),
		       COALESCE(NULLIF(data->>'assignee', ''), '(unassigned)'),
		       COALESCE(data->>'due_date', ''),
		       (CURRENT_DATE - (data->>'due_date')::date)::int,
		       COALESCE(data->>'priority', 'Normal'),
		       status
		  FROM %s.documents
		 WHERE doctype = 'PIMTask' AND deleted_at IS NULL
		   AND status IN ('Open', 'In Progress', 'Blocked')
		   AND NULLIF(data->>'due_date', '') IS NOT NULL
		   AND (data->>'due_date')::date < CURRENT_DATE
		   AND ($1 = '' OR COALESCE(data->>'assignee', '') = $1)
		 ORDER BY (data->>'due_date')::date ASC, id`, schema)
	rows, err := db.DB.Query(query, params["assignee"])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, title, itemCode, assignee, dueDate, priority, status string
		var daysOverdue int
		if err := rows.Scan(&id, &title, &itemCode, &assignee, &dueDate, &daysOverdue, &priority, &status); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"task_id": id, "title": title, "item_code": itemCode, "assignee": assignee,
			"due_date": dueDate, "days_overdue": daysOverdue, "priority": priority, "status": status,
		})
	}
	return out, rows.Err()
}

func runPIMStalledWorkflowReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// "Stalled" is deliberately two different shapes of stuck: a run carrying
	// a blocked_reason (it tried to advance and could not), and a paused run
	// (someone stopped it and may have forgotten). Both need the same nudge.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT r.id,
		       COALESCE(w.data->>'name', COALESCE(r.data->>'workflow', '')),
		       COALESCE(r.data->>'item_code', ''),
		       COALESCE(r.data->>'current_stage', ''),
		       r.status,
		       COALESCE(r.data->>'blocked_reason', ''),
		       EXTRACT(DAY FROM CURRENT_TIMESTAMP - r.created_at)::int,
		       (SELECT COUNT(*) FROM %s.documents t
		         WHERE t.doctype = 'PIMTask' AND t.deleted_at IS NULL
		           AND COALESCE(t.data->>'workflow_run','') = r.id
		           AND t.status IN ('Open','In Progress','Blocked'))::int
		  FROM %s.documents r
		  LEFT JOIN %s.documents w
		         ON w.doctype = 'PIMWorkflowDefinition' AND w.id = r.data->>'workflow' AND w.deleted_at IS NULL
		 WHERE r.doctype = 'PIMWorkflowRun' AND r.deleted_at IS NULL
		   AND (r.status = 'Paused'
		        OR (r.status = 'Running' AND COALESCE(r.data->>'blocked_reason', '') <> ''))
		 ORDER BY r.created_at ASC`, schema, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, workflow, itemCode, stage, status, reason string
		var ageDays, openTasks int
		if err := rows.Scan(&id, &workflow, &itemCode, &stage, &status, &reason, &ageDays, &openTasks); err != nil {
			return nil, err
		}
		if reason == "" && status == "Paused" {
			reason = "paused by an operator"
		}
		out = append(out, map[string]interface{}{
			"run_id": id, "workflow": workflow, "item_code": itemCode, "current_stage": stage,
			"status": status, "blocked_reason": reason, "age_days": ageDays, "open_tasks": openTasks,
		})
	}
	return out, rows.Err()
}
