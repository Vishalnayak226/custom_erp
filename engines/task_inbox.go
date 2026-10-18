package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 73.3.3 - one task inbox.
//
// Pending approvals (approval.go), the OMS exception queues
// (oms_exception_queues.go) and PIM "My work" (pim_tasks.go) were three
// places to look. This is a read-only aggregation of those three sources -
// no new state, no new action: every row links back to the screen where the
// existing engine acts on it, so maker-checker, location scope and the PIM
// state machine stay exactly where they are.
//
// What makes a row the viewer's:
//   - an approval whose amount slab names the viewer's role (or any, for a
//     Super Admin), in their location, that they did not submit themselves;
//   - an OMS queue with work in it, for a role that may read sales orders;
//   - an open PIM task assigned to the viewer, or to their role and nobody yet.

// TaskInboxItem is one row of the inbox.
type TaskInboxItem struct {
	Source       string    `json:"source"` // Approval, Order desk, Catalog
	Title        string    `json:"title"`
	Reason       string    `json:"reason"`
	DocType      string    `json:"doctype,omitempty"`
	DocID        string    `json:"doc_id,omitempty"`
	View         string    `json:"view"`
	Queue        string    `json:"queue,omitempty"`
	DueDate      string    `json:"due_date,omitempty"`
	WaitingSince time.Time `json:"waiting_since"`
	Overdue      bool      `json:"overdue"`
	Count        int       `json:"count,omitempty"`
	Priority     string    `json:"priority,omitempty"`
}

// TaskInboxScope says which sources the viewer may see; the HTTP layer fills
// it from the same RBAC and entitlement checks the source screens use.
type TaskInboxScope struct {
	UserID, Username, Role, Location string
	OMS, PIM                         bool
}

// TaskInbox is the whole inbox plus per-source counts and any source that
// could not be read (shown, never silently dropped).
type TaskInbox struct {
	Items  []TaskInboxItem   `json:"items"`
	Counts map[string]int    `json:"counts"`
	Errors map[string]string `json:"errors"`
}

// ListTaskInbox assembles the viewer's inbox, overdue first, then oldest.
func ListTaskInbox(tenantID string, scope TaskInboxScope) (*TaskInbox, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	inbox := &TaskInbox{Items: []TaskInboxItem{}, Counts: map[string]int{}, Errors: map[string]string{}}

	// 1. Approvals routed to this role.
	if pending, err := ListPendingApprovals(tenantID, scope.Role, scope.Location); err != nil {
		inbox.Errors["approvals"] = err.Error()
	} else {
		for _, doc := range pending {
			doctype, _ := doc["doctype"].(string)
			id, _ := doc["id"].(string)
			var createdBy string
			var waiting time.Time
			_ = db.DB.QueryRow(fmt.Sprintf(
				`SELECT COALESCE(created_by, ''), updated_at AT TIME ZONE current_setting('TimeZone') FROM %s.documents WHERE id = $1 AND doctype = $2`, schema),
				id, doctype).Scan(&createdBy, &waiting)
			if createdBy != "" && createdBy == scope.UserID {
				continue // maker-checker: someone else decides this one
			}
			amount := DocumentBaseAmount(doc)
			required, err := RequiredApproverRoleForAmount(tenantID, doctype, amount)
			if err != nil || (!IsSuperAdmin(scope.Role) && required != scope.Role) {
				continue
			}
			code := id
			for _, key := range []string{"code", "name", "po_number", "invoice_number"} {
				if v, _ := doc[key].(string); strings.TrimSpace(v) != "" {
					code = v
					break
				}
			}
			reason := fmt.Sprintf("Waiting for %s approval", required)
			if required == "" {
				reason = "Waiting for approval (no rule names a role, so an administrator decides)"
			}
			if amount > 0 {
				reason = fmt.Sprintf("%s - amount %.2f", reason, amount)
			}
			if createdBy != "" {
				reason += ", submitted by " + createdBy
			}
			inbox.Items = append(inbox.Items, TaskInboxItem{
				Source: "Approval", Title: doctype + " " + code, Reason: reason,
				DocType: doctype, DocID: id, View: "approvals", WaitingSince: waiting,
			})
			inbox.Counts["approvals"]++
		}
	}

	// 2. OMS exception queues with work in them.
	if scope.OMS {
		for _, def := range OMSExceptionQueueDefs {
			rows, err := OMSExceptionQueue(tenantID, def.ID)
			if err != nil {
				inbox.Errors["oms:"+def.ID] = err.Error()
				continue
			}
			if len(rows) == 0 {
				continue
			}
			oldest, _ := rows[0]["since"].(time.Time)
			first, _ := rows[0]["reason"].(string)
			inbox.Items = append(inbox.Items, TaskInboxItem{
				Source: "Order desk", Title: fmt.Sprintf("%s: %d", def.Label, len(rows)),
				Reason: "Oldest: " + first + " - " + fmt.Sprint(rows[0]["next_action"]),
				View:   "oms", Queue: def.ID, WaitingSince: oldest, Count: len(rows),
				Overdue: time.Since(oldest) > 24*time.Hour,
			})
			inbox.Counts["oms"] += len(rows)
		}
	}

	// 3. PIM "My work": open tasks for this user, or for the role with no
	// named assignee yet.
	if scope.PIM {
		seen := map[string]bool{}
		add := func(filter PIMTaskFilter, unassignedOnly bool) {
			res, err := ListPIMTasks(tenantID, filter)
			if err != nil {
				inbox.Errors["pim"] = err.Error()
				return
			}
			for _, task := range res.Tasks {
				if seen[task.ID] || (unassignedOnly && (task.Assignee != "" || task.AssigneeRole != scope.Role)) {
					continue
				}
				seen[task.ID] = true
				title := task.Title
				if task.ItemCode != "" {
					title += " - " + task.ItemCode
				}
				reason := task.TaskType + " task, " + task.Status
				if task.Assignee == "" {
					reason += ", for anyone in " + task.AssigneeRole
				}
				inbox.Items = append(inbox.Items, TaskInboxItem{
					Source: "Catalog", Title: title, Reason: reason, DocType: "PIMTask", DocID: task.ID,
					View: "pim", DueDate: task.DueDate, WaitingSince: task.CreatedAt, Overdue: task.Overdue,
					Priority: task.Priority,
				})
				inbox.Counts["pim"]++
			}
		}
		// PIM stores an assignee as a username; fall back to the id, which
		// equals the username for every user created on screen.
		who := scope.Username
		if who == "" {
			who = scope.UserID
		}
		add(PIMTaskFilter{Assignee: who, OnlyOpen: true, Limit: 200}, false)
		add(PIMTaskFilter{OnlyOpen: true, Limit: 200}, true)
		inbox.addImportWork(schema, who, scope.UserID)
	}

	sort.SliceStable(inbox.Items, func(i, j int) bool {
		a, b := inbox.Items[i], inbox.Items[j]
		if a.Overdue != b.Overdue {
			return a.Overdue
		}
		return a.WaitingSince.Before(b.WaitingSince)
	})
	return inbox, nil
}

// addImportWork (Stage 55.11) adds the viewer's own import follow-ups: a
// schedule they own whose last run failed, and an import they ran that
// still has failed rows to fix and resume.
func (inbox *TaskInbox) addImportWork(schema, username, userID string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'name', id), COALESCE(data->>'last_run_status', ''), COALESCE(data->>'last_run_at', ''),
		       updated_at AT TIME ZONE current_setting('TimeZone')
		FROM %s.documents
		WHERE doctype = 'PIMImportSchedule' AND deleted_at IS NULL AND status = 'Active'
		  AND COALESCE(data->>'last_run_status', '') LIKE 'Failed%%'
		  AND (data->>'owner' = $1 OR (COALESCE(data->>'owner', '') = '' AND created_by = $2))`, schema), username, userID)
	if err == nil {
		for rows.Next() {
			var id, name, status, lastRun string
			var at time.Time
			if rows.Scan(&id, &name, &status, &lastRun, &at) != nil {
				continue
			}
			inbox.Items = append(inbox.Items, TaskInboxItem{
				Source: "Catalog", Title: "Import schedule " + name, Reason: "Last run " + status,
				DocType: "PIMImportSchedule", DocID: id, View: "pim", WaitingSince: at, Overdue: true,
			})
			inbox.Counts["pim"]++
		}
		rows.Close()
	}
	rows, err = db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'source_doctype', data->>'doctype_name', ''), jsonb_array_length(COALESCE(data->'open_failed_rows', '[]'::jsonb)),
		       created_at AT TIME ZONE current_setting('TimeZone')
		FROM %s.documents
		WHERE doctype = 'ImportJob' AND deleted_at IS NULL AND created_by = $1 AND data->>'resumable' = 'true'
		ORDER BY created_at DESC LIMIT 50`, schema), userID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, doctype string
		var open int
		var at time.Time
		if rows.Scan(&id, &doctype, &open, &at) != nil {
			continue
		}
		inbox.Items = append(inbox.Items, TaskInboxItem{
			Source: "Catalog", Title: fmt.Sprintf("Import %s: %d failed row(s)", id, open),
			Reason: "Fix the failed rows and resume - the rows that imported are not re-run", DocType: "ImportJob", DocID: id,
			View: "import-job", WaitingSince: at,
		})
		inbox.Counts["pim"]++
	}
}
