package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Stage 36.2.1 / 36.2.2 / 36.2.5 - the PIM task engine.
//
// A task is not an approval. The approval engine answers "may this saved
// document proceed?", once, from someone with the authority. A task says
// "someone must go and improve this product", is owned by a named person, has
// a due date, and exists precisely because the product is not ready yet. The
// two are kept apart on purpose: a task can never move a document's approval
// state, and completing a task is not an approval decision.
//
// Everything here is stored as ordinary documents, so tasks inherit RBAC, soft
// delete, the audit trail, the generic list view and CSV export without a
// parallel admin surface being built for them.
// ---------------------------------------------------------------------------

// PIMTaskOpenStatuses are the states that still represent outstanding work.
// Used by the inbox's default filter, by the overdue report and - importantly -
// by the workflow engine's "has this stage's work finished?" test, so all three
// agree on what "open" means instead of each hardcoding a list.
var PIMTaskOpenStatuses = []string{"Open", "In Progress", "Blocked"}

// pimTaskTransitions is the task state machine, as data.
//
// Done and Cancelled are deliberately terminal. Re-opening a completed task
// would be actively unsafe here: a Done task may already have satisfied its
// workflow stage's exit condition and advanced the run past it, and there is no
// honest way to un-advance a run whose later stages have themselves created
// work. The correct response to "that wasn't actually finished" is a new
// follow-up task, which is one click in the inbox and leaves the history
// truthful. CreatePIMFollowUpTask exists for exactly this.
var pimTaskTransitions = map[string][]string{
	"Open":        {"In Progress", "Blocked", "Done", "Cancelled"},
	"In Progress": {"Open", "Blocked", "Done", "Cancelled"},
	"Blocked":     {"Open", "In Progress", "Cancelled"},
	"Done":        {},
	"Cancelled":   {},
}

type PIMTaskComment struct {
	At      string `json:"at"`
	Author  string `json:"author"`
	Comment string `json:"comment"`
}

// PIMTaskRequest is the one shape every task-creating path goes through - the
// API, a template instantiation, a workflow stage and the report-row assign
// button. One constructor means a task created by a workflow cannot end up
// with fields a hand-created task would have been refused for.
type PIMTaskRequest struct {
	Title        string `json:"title"`
	TaskType     string `json:"task_type"`
	ScopeType    string `json:"scope_type"`
	ScopeRef     string `json:"scope_ref"`
	ItemCode     string `json:"item_code"`
	Assignee     string `json:"assignee"`
	AssigneeRole string `json:"assignee_role"`
	DueDate      string `json:"due_date"`
	Priority     string `json:"priority"`
	Instructions string `json:"instructions"`

	// Set only by the workflow engine and template instantiation; not accepted
	// from an HTTP caller (see handlers_pim_tasks.go, which builds its own
	// request rather than decoding straight into this struct). A hand-created
	// task claiming to belong to a workflow run would be advanced by that run's
	// exit test without the workflow ever having asked for it.
	Template    string `json:"-"`
	WorkflowRun string `json:"-"`
	Stage       string `json:"-"`
}

type PIMTaskRow struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	TaskType     string           `json:"task_type"`
	ScopeType    string           `json:"scope_type"`
	ScopeRef     string           `json:"scope_ref"`
	ItemCode     string           `json:"item_code"`
	ItemName     string           `json:"item_name"`
	Assignee     string           `json:"assignee"`
	AssigneeRole string           `json:"assignee_role"`
	DueDate      string           `json:"due_date"`
	Priority     string           `json:"priority"`
	Instructions string           `json:"instructions"`
	Status       string           `json:"status"`
	Template     string           `json:"template,omitempty"`
	WorkflowRun  string           `json:"workflow_run,omitempty"`
	Stage        string           `json:"stage,omitempty"`
	CompletedAt  string           `json:"completed_at,omitempty"`
	CompletedBy  string           `json:"completed_by,omitempty"`
	Comments     []PIMTaskComment `json:"comments"`
	CreatedAt    time.Time        `json:"created_at"`
	Overdue      bool             `json:"overdue"`
	DaysToDue    *int             `json:"days_to_due,omitempty"`
}

// PIMTaskFilter drives the inbox. Every field is optional; an empty filter
// returns every task the tenant has, newest first.
type PIMTaskFilter struct {
	// TaskID narrows to one task. It exists so the single-task read reuses this
	// one query - the item-name join, the overdue arithmetic and the comment
	// decoding - instead of a second assembler that could disagree with the
	// list about what a task looks like.
	TaskID      string
	Assignee    string
	Status      string
	OnlyOpen    bool
	OnlyOverdue bool
	ItemCode    string
	WorkflowRun string
	Priority    string
	TaskType    string
	Limit       int
	Offset      int
}

type PIMTaskListResult struct {
	Tasks      []PIMTaskRow   `json:"tasks"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	StatusTally map[string]int `json:"status_tally"`
}

// ---------------------------------------------------------------------------
// Validation - attached to ValidateDocument via ValidateParityFoundationDocument,
// so a task written through the generic document API, a CSV import or this
// engine is subject to the same rules. Without that a task could be created
// through /api/v1/doc/PIMTask with a status the state machine has never heard
// of, and the inbox would then show a row no action could ever move.
// ---------------------------------------------------------------------------

func ValidatePIMTaskDocument(tenantID string, payload map[string]interface{}) error {
	status := pimString(payload["status"])
	if status != "" {
		if _, known := pimTaskTransitions[status]; !known {
			return &ValidationError{Code: "META-0199", SubFor: "Status",
				Message: fmt.Sprintf("task status %q is not one of Open, In Progress, Blocked, Done or Cancelled", status)}
		}
	}
	scopeType := pimString(payload["scope_type"])
	scopeRef := pimString(payload["scope_ref"])
	if scopeType != "" && scopeRef == "" {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Scope Reference",
			Message: fmt.Sprintf("a %s-scoped task needs the %s it refers to", scopeType, strings.ToLower(scopeType))}
	}
	if due := pimString(payload["due_date"]); due != "" {
		if _, err := time.Parse("2006-01-02", due); err != nil {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Due Date",
				Message: "due date must be a calendar date in YYYY-MM-DD form"}
		}
	}
	var comments []PIMTaskComment
	if err := decodeProductGroupJSON(payload["comments"], &comments); err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Comments",
			Message: "comments must be a JSON array of {at, author, comment} rows"}
	}
	// An assignee that is not a real, active user produces a task nobody will
	// ever see in their inbox - a silent black hole, which is worse than a
	// refused save. Blank stays legal: an unassigned task is a real state (it
	// is what a queue looks like before anyone picks it up).
	if assignee := pimString(payload["assignee"]); assignee != "" && db.DB != nil {
		ok, err := pimUserExists(tenantID, assignee)
		if err != nil {
			return err
		}
		if !ok {
			return &ValidationError{Code: "META-0198", SubFor: "Assignee",
				Message: fmt.Sprintf("no active user named %q to assign this task to", assignee)}
		}
	}
	return nil
}

func ValidatePIMTaskTemplateDocument(_ string, payload map[string]interface{}) error {
	pattern := pimString(payload["title_pattern"])
	if pattern != "" {
		if err := validatePIMTitlePattern(pattern); err != nil {
			return err
		}
	}
	if days := pimString(payload["due_in_days"]); days != "" {
		value, err := strconv.ParseFloat(days, 64)
		if err != nil || value < 0 || value > 3650 {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Due In (days)",
				Message: "due in days must be a whole number of days from 0 to 3650"}
		}
	}
	return nil
}

// validatePIMTitlePattern refuses a placeholder the renderer does not know.
// A template is authored once and instantiated hundreds of times; a typo like
// {item-code} would otherwise be discovered as literal text sitting in a
// hundred task titles, by which point they are already in people's inboxes.
func validatePIMTitlePattern(pattern string) error {
	known := map[string]bool{"item_code": true, "item_name": true, "family": true, "status": true}
	rest := pattern
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			return nil
		}
		close := strings.Index(rest[open:], "}")
		if close < 0 {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Title Pattern",
				Message: "title pattern has an unclosed { placeholder"}
		}
		name := rest[open+1 : open+close]
		if !known[name] {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Title Pattern",
				Message: fmt.Sprintf("unknown placeholder {%s} - use item_code, item_name, family or status", name)}
		}
		rest = rest[open+close+1:]
	}
}

// ---------------------------------------------------------------------------
// Small shared helpers.
// ---------------------------------------------------------------------------

func pimString(raw interface{}) string {
	if raw == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if s == "<nil>" {
		return ""
	}
	return s
}

func pimUserExists(tenantID, username string) (bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.users WHERE username = $1 AND status = 'Active')`, schema),
		username).Scan(&exists)
	return exists, err
}

// PIMAssignableUser is the minimum a task assigner needs to know about a
// colleague: their username (which is what the assignee field stores) and their
// role (which is how you tell two similar names apart).
type PIMAssignableUser struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ListPIMAssignableUsers exists because /api/v1/admin/users is Super Admin
// gated, and a Store Manager who may create and reassign tasks must still be
// able to see who they can hand work to - otherwise the assignee field is a
// free-text box you have to spell correctly from memory, and every typo becomes
// a task in nobody's inbox.
//
// Deliberately narrow: active users only, and only username and role. No email,
// no id, no location, no MFA state, nothing that would make this a way around
// the admin user list.
func ListPIMAssignableUsers(tenantID string) ([]PIMAssignableUser, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT username, COALESCE(role, '') FROM %s.users WHERE status = 'Active' ORDER BY username`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMAssignableUser{}
	for rows.Next() {
		var user PIMAssignableUser
		if err := rows.Scan(&user.Username, &user.Role); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

// pimTaskDocument reads one task and returns its canonical id alongside the
// stored data, so every mutation below works from what is actually in the
// database rather than from what the caller believed was there.
func pimTaskDocument(tenantID, taskID string) (string, map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var id, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMTask' AND id = $1 AND deleted_at IS NULL`, schema), taskID).Scan(&id, &raw, &status)
	if err != nil {
		return "", nil, fmt.Errorf("task %q not found", taskID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", nil, fmt.Errorf("task %q has invalid stored data: %w", taskID, err)
	}
	// documents.status is the authority - it is what the list view, RBAC and
	// every report read. If a historical write left data.status disagreeing
	// with it, believe the column.
	data["status"] = status
	return id, data, nil
}

func writePIMDocument(tenantID, doctype, docID string, data map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	status := pimString(data["status"])
	if status == "" {
		status = "Active"
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP, version = version + 1
		WHERE id = $3 AND doctype = $4 AND deleted_at IS NULL`, schema),
		payload, status, docID, doctype)
	return err
}

// insertPIMDocument is the single insert path for all four Stage 36.2
// doctypes.
//
// created_by is 'system' rather than the actor: documents.created_by carries a
// foreign key to users, and an engine-internal or test caller has no users row,
// so writing the actor there would make task creation fail for exactly the
// callers that need it most. The actor is recorded where it is actually read -
// in the audit log, in the run's activity log, and in the task's own assignee/
// completed_by fields. Same choice SaveOMSView and CreateSalesOrder make.
func insertPIMDocument(tenantID, doctype, docID string, data map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	status := pimString(data["status"])
	if status == "" {
		status = "Active"
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by, version) VALUES ($1, $2, $3, $4, 'system', 1)`, schema),
		docID, doctype, payload, status)
	return err
}

// ---------------------------------------------------------------------------
// 36.2.1 - create, list, progress and comment on a task.
// ---------------------------------------------------------------------------

// CreatePIMTask is the only way a task comes into existence. Validation runs
// through ValidatePIMTaskDocument, which is the same function ValidateDocument
// calls for a generic API save - so there is one definition of a legal task,
// not one per entry point.
func CreatePIMTask(tenantID, actor string, req PIMTaskRequest) (string, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return "", fmt.Errorf("a task needs a title")
	}
	if req.ScopeType == "" {
		// The overwhelmingly common case is a task about one product, and
		// requiring the caller to say so twice (item_code and scope_type) is
		// friction with no information in it.
		if req.ItemCode != "" {
			req.ScopeType = "Product"
			req.ScopeRef = req.ItemCode
		} else {
			return "", fmt.Errorf("a task needs a scope - a product, a product group or an attribute set")
		}
	}
	if req.ScopeRef == "" {
		req.ScopeRef = req.ItemCode
	}
	if req.Priority == "" {
		req.Priority = "Normal"
	}
	if req.TaskType == "" {
		req.TaskType = "Other"
	}

	taskID := NewDocID("PTSK")
	data := map[string]interface{}{
		"code": taskID, "title": req.Title, "task_type": req.TaskType,
		"scope_type": req.ScopeType, "scope_ref": req.ScopeRef, "item_code": req.ItemCode,
		"assignee": req.Assignee, "assignee_role": req.AssigneeRole, "due_date": req.DueDate,
		"priority": req.Priority, "instructions": req.Instructions,
		"comments": "[]", "status": "Open",
	}
	if req.Template != "" {
		data["template"] = req.Template
	}
	if req.WorkflowRun != "" {
		data["workflow_run"] = req.WorkflowRun
		data["stage"] = req.Stage
	}
	if err := ValidatePIMTaskDocument(tenantID, data); err != nil {
		return "", err
	}
	// A task pointing at a product that does not exist is unactionable, and
	// the Link fieldtype only enforces this on the generic API path.
	if req.ItemCode != "" && db.DB != nil {
		exists, err := verifyDocumentExists(tenantID, "Item", req.ItemCode)
		if err != nil {
			return "", err
		}
		if !exists {
			return "", fmt.Errorf("item %q does not exist", req.ItemCode)
		}
	}
	if err := insertPIMDocument(tenantID, "PIMTask", taskID, data); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, actor, "PIM_TASK_CREATED", "Success",
		fmt.Sprintf("task %s (%s) on %s assigned to %s", taskID, req.Title, req.ScopeRef, orNone(req.Assignee)))
	return taskID, nil
}

// CreatePIMFollowUpTask is the honest alternative to re-opening a Done task
// (see pimTaskTransitions). It copies the original's scope and assignment and
// links back to it in the instructions, so the inbox shows a new piece of work
// and the history still says the first one was completed.
func CreatePIMFollowUpTask(tenantID, actor, taskID, note string) (string, error) {
	_, data, err := pimTaskDocument(tenantID, taskID)
	if err != nil {
		return "", err
	}
	instructions := fmt.Sprintf("Follow-up to %s (%s).", taskID, pimString(data["title"]))
	if strings.TrimSpace(note) != "" {
		instructions += " " + strings.TrimSpace(note)
	}
	return CreatePIMTask(tenantID, actor, PIMTaskRequest{
		Title:        "Follow-up: " + pimString(data["title"]),
		TaskType:     pimString(data["task_type"]),
		ScopeType:    pimString(data["scope_type"]),
		ScopeRef:     pimString(data["scope_ref"]),
		ItemCode:     pimString(data["item_code"]),
		Assignee:     pimString(data["assignee"]),
		AssigneeRole: pimString(data["assignee_role"]),
		Priority:     pimString(data["priority"]),
		Instructions: instructions,
	})
}

// SetPIMTaskStatus moves a task through the state machine above and, when the
// task belongs to a workflow run, asks that run whether it can now advance.
// Advancing is deliberately part of *this* call rather than a separate cron:
// the moment a stage's last task is finished is exactly when the next stage's
// work should appear in someone's inbox, and a workflow whose progress depends
// on a sweeper running feels broken to the person waiting on it.
func SetPIMTaskStatus(tenantID, actor, taskID, newStatus string) error {
	id, data, err := pimTaskDocument(tenantID, taskID)
	if err != nil {
		return err
	}
	current := pimString(data["status"])
	if current == newStatus {
		return nil
	}
	allowed, known := pimTaskTransitions[current]
	if !known {
		return fmt.Errorf("task %s is in unknown status %q", taskID, current)
	}
	if !containsString(allowed, newStatus) {
		if len(allowed) == 0 {
			return fmt.Errorf("task %s is %s and cannot be re-opened - create a follow-up task instead", taskID, current)
		}
		return fmt.Errorf("a %s task cannot move to %s (allowed: %s)", current, newStatus, strings.Join(allowed, ", "))
	}
	data["status"] = newStatus
	if newStatus == "Done" {
		data["completed_at"] = time.Now().UTC().Format(time.RFC3339)
		data["completed_by"] = actor
	}
	if err := writePIMDocument(tenantID, "PIMTask", id, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "PIM_TASK_STATUS", "Success",
		fmt.Sprintf("task %s moved %s -> %s", taskID, current, newStatus))

	if runID := pimString(data["workflow_run"]); runID != "" {
		// A failure to advance must not undo a legitimately completed task -
		// the task really is done, and the run can be advanced again by hand
		// or by the next completion. Recorded, not swallowed.
		if _, advErr := AdvancePIMWorkflowRun(tenantID, actor, runID); advErr != nil {
			LogSystemError(tenantID, runID, "Warning", "pim_workflow",
				fmt.Sprintf("run %s did not advance after task %s completed: %v", runID, taskID, advErr), "")
		}
	}
	return nil
}

// ReassignPIMTask changes the owner. Kept separate from a generic field update
// because it is the single most common action in the inbox and deserves its own
// audit event - "who was this handed to, and when" is a question people ask.
func ReassignPIMTask(tenantID, actor, taskID, assignee, assigneeRole string) error {
	id, data, err := pimTaskDocument(tenantID, taskID)
	if err != nil {
		return err
	}
	if status := pimString(data["status"]); status == "Done" || status == "Cancelled" {
		return fmt.Errorf("task %s is %s - there is nothing left to assign", taskID, status)
	}
	previous := pimString(data["assignee"])
	data["assignee"] = strings.TrimSpace(assignee)
	if strings.TrimSpace(assigneeRole) != "" {
		data["assignee_role"] = strings.TrimSpace(assigneeRole)
	}
	if err := ValidatePIMTaskDocument(tenantID, data); err != nil {
		return err
	}
	if err := writePIMDocument(tenantID, "PIMTask", id, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "PIM_TASK_ASSIGNED", "Success",
		fmt.Sprintf("task %s reassigned %s -> %s", taskID, orNone(previous), orNone(assignee)))
	return nil
}

// SetPIMTaskDueDate is the third field the inbox lets someone change in place.
func SetPIMTaskDueDate(tenantID, actor, taskID, dueDate string) error {
	id, data, err := pimTaskDocument(tenantID, taskID)
	if err != nil {
		return err
	}
	previous := pimString(data["due_date"])
	data["due_date"] = strings.TrimSpace(dueDate)
	if err := ValidatePIMTaskDocument(tenantID, data); err != nil {
		return err
	}
	if err := writePIMDocument(tenantID, "PIMTask", id, data); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "PIM_TASK_DUE_DATE", "Success",
		fmt.Sprintf("task %s due date %s -> %s", taskID, orNone(previous), orNone(dueDate)))
	return nil
}

// AddPIMTaskComment appends to the task's own thread. Comments are append-only
// by construction - there is no edit or delete path - because the thread is the
// record of why a task took three weeks, and a record that can be rewritten
// answers nothing.
func AddPIMTaskComment(tenantID, actor, taskID, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return fmt.Errorf("a comment needs some text")
	}
	id, data, err := pimTaskDocument(tenantID, taskID)
	if err != nil {
		return err
	}
	var comments []PIMTaskComment
	if err := decodeProductGroupJSON(data["comments"], &comments); err != nil {
		return fmt.Errorf("task %s has an unreadable comment thread: %w", taskID, err)
	}
	comments = append(comments, PIMTaskComment{
		At: time.Now().UTC().Format(time.RFC3339), Author: orNone(actor), Comment: comment,
	})
	encoded, err := json.Marshal(comments)
	if err != nil {
		return err
	}
	data["comments"] = string(encoded)
	return writePIMDocument(tenantID, "PIMTask", id, data)
}

// ListPIMTasks is the read behind the My Work inbox (36.2.7), a product's own
// task panel, and the workflow engine's stage-completion test.
//
// Filtering happens in SQL rather than in Go: the inbox is the one screen a
// category manager keeps open all day, and "fetch every task the tenant has
// ever created and filter in the browser" is the exact mistake the OMS console
// was built to undo.
func ListPIMTasks(tenantID string, filter PIMTaskFilter) (*PIMTaskListResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"t.doctype = 'PIMTask'", "t.deleted_at IS NULL"}
	args := []interface{}{}
	add := func(clause string, value interface{}) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if filter.TaskID != "" {
		add("t.id = $%d", filter.TaskID)
	}
	if filter.Assignee != "" {
		add("COALESCE(t.data->>'assignee', '') = $%d", filter.Assignee)
	}
	if filter.Status != "" {
		add("t.status = $%d", filter.Status)
	}
	if filter.OnlyOpen {
		where = append(where, "t.status IN ('Open', 'In Progress', 'Blocked')")
	}
	if filter.OnlyOverdue {
		// Compared as a date in SQL so "overdue" means the same thing to the
		// database as it does to the row-level Overdue flag computed below.
		where = append(where, "NULLIF(t.data->>'due_date', '') IS NOT NULL",
			"(t.data->>'due_date')::date < CURRENT_DATE",
			"t.status IN ('Open', 'In Progress', 'Blocked')")
	}
	if filter.ItemCode != "" {
		add("COALESCE(t.data->>'item_code', '') = $%d", filter.ItemCode)
	}
	if filter.WorkflowRun != "" {
		add("COALESCE(t.data->>'workflow_run', '') = $%d", filter.WorkflowRun)
	}
	if filter.Priority != "" {
		add("COALESCE(t.data->>'priority', '') = $%d", filter.Priority)
	}
	if filter.TaskType != "" {
		add("COALESCE(t.data->>'task_type', '') = $%d", filter.TaskType)
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents t WHERE %s`, schema, clause), args...).Scan(&total); err != nil {
		return nil, err
	}

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	// Ordering: overdue first, then by due date, then High priority, then
	// newest. An inbox sorted only by creation date buries the thing that is
	// already late, which is the one row that needed to be at the top.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT t.id, t.data, t.status, t.created_at,
		       COALESCE(i.data->>'name', '') AS item_name
		  FROM %s.documents t
		  LEFT JOIN %s.documents i
		         ON i.doctype = 'Item' AND i.id = t.data->>'item_code' AND i.deleted_at IS NULL
		 WHERE %s
		 ORDER BY
		   CASE WHEN NULLIF(t.data->>'due_date','') IS NOT NULL
		         AND (t.data->>'due_date')::date < CURRENT_DATE
		         AND t.status IN ('Open','In Progress','Blocked') THEN 0 ELSE 1 END,
		   NULLIF(t.data->>'due_date','')::date NULLS LAST,
		   CASE COALESCE(t.data->>'priority','Normal') WHEN 'High' THEN 0 WHEN 'Normal' THEN 1 ELSE 2 END,
		   t.created_at DESC
		 LIMIT %d OFFSET %d`, schema, schema, clause, limit, offset), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &PIMTaskListResult{Tasks: []PIMTaskRow{}, Total: total, Limit: limit, Offset: offset}
	today := time.Now().Truncate(24 * time.Hour)
	for rows.Next() {
		var id, raw, status, itemName string
		var createdAt time.Time
		if err := rows.Scan(&id, &raw, &status, &createdAt, &itemName); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		task := PIMTaskRow{
			ID: id, Title: pimString(data["title"]), TaskType: pimString(data["task_type"]),
			ScopeType: pimString(data["scope_type"]), ScopeRef: pimString(data["scope_ref"]),
			ItemCode: pimString(data["item_code"]), ItemName: itemName,
			Assignee: pimString(data["assignee"]), AssigneeRole: pimString(data["assignee_role"]),
			DueDate: pimString(data["due_date"]), Priority: pimString(data["priority"]),
			Instructions: pimString(data["instructions"]), Status: status,
			Template: pimString(data["template"]), WorkflowRun: pimString(data["workflow_run"]),
			Stage: pimString(data["stage"]), CompletedAt: pimString(data["completed_at"]),
			CompletedBy: pimString(data["completed_by"]), CreatedAt: createdAt,
			Comments: []PIMTaskComment{},
		}
		_ = decodeProductGroupJSON(data["comments"], &task.Comments)
		if task.Comments == nil {
			task.Comments = []PIMTaskComment{}
		}
		if task.DueDate != "" {
			if due, parseErr := time.Parse("2006-01-02", task.DueDate); parseErr == nil {
				days := int(due.Sub(today).Hours() / 24)
				task.DaysToDue = &days
				task.Overdue = days < 0 && containsString(PIMTaskOpenStatuses, status)
			}
		}
		result.Tasks = append(result.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The tally is over the whole filtered set, not the current page - a badge
	// reading "3 open" when the page happens to show 3 of 80 is a lie the
	// screen tells every time someone pages forward.
	tally, err := pimTaskStatusTally(schema, clause, args)
	if err != nil {
		return nil, err
	}
	result.StatusTally = tally
	return result, nil
}

func pimTaskStatusTally(schema, clause string, args []interface{}) (map[string]int, error) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT t.status, COUNT(*) FROM %s.documents t WHERE %s GROUP BY t.status`, schema, clause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tally := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		tally[status] = count
	}
	return tally, rows.Err()
}

// ---------------------------------------------------------------------------
// 36.2.2 - task templates, instantiated against a product group.
// ---------------------------------------------------------------------------

type PIMTaskTemplate struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	TaskType        string `json:"task_type"`
	TitlePattern    string `json:"title_pattern"`
	DefaultAssignee string `json:"default_assignee"`
	DefaultRole     string `json:"default_role"`
	DueInDays       int    `json:"due_in_days"`
	Priority        string `json:"priority"`
	Instructions    string `json:"instructions"`
}

type PIMTaskInstantiation struct {
	TemplateCode string   `json:"template_code"`
	GroupID      string   `json:"group_id,omitempty"`
	Created      []string `json:"created_task_ids"`
	Skipped      []string `json:"skipped_items"`
	CreatedCount int      `json:"created_count"`
	SkippedCount int      `json:"skipped_count"`
}

func fetchPIMTaskTemplate(tenantID, code string) (*PIMTaskTemplate, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMTaskTemplate' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), code).Scan(&id, &raw, &status)
	if err != nil {
		return nil, fmt.Errorf("task template %q not found", code)
	}
	if status != "Active" {
		return nil, fmt.Errorf("task template %q is not active", code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("task template %q has invalid stored data: %w", code, err)
	}
	days := 0
	if raw := pimString(data["due_in_days"]); raw != "" {
		if parsed, convErr := strconv.ParseFloat(raw, 64); convErr == nil {
			days = int(parsed)
		}
	}
	return &PIMTaskTemplate{
		ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
		TaskType: pimString(data["task_type"]), TitlePattern: pimString(data["title_pattern"]),
		DefaultAssignee: pimString(data["default_assignee"]), DefaultRole: pimString(data["default_role"]),
		DueInDays: days, Priority: pimString(data["priority"]), Instructions: pimString(data["instructions"]),
	}, nil
}

// renderPIMTitlePattern is deliberately a fixed placeholder substitution and
// not a template language. Four known keys, replaced literally; anything else
// was refused at save time by validatePIMTitlePattern.
func renderPIMTitlePattern(pattern string, item ProductGroupMember) string {
	replacer := strings.NewReplacer(
		"{item_code}", item.ItemCode,
		"{item_name}", item.Name,
		"{family}", item.Family,
		"{status}", item.Status,
	)
	rendered := strings.TrimSpace(replacer.Replace(pattern))
	if rendered == "" {
		return item.ItemCode
	}
	return rendered
}

// InstantiatePIMTaskTemplate creates one task per product in a group.
//
// It skips a product that already has an open task from this template rather
// than creating a duplicate. Running an instantiation twice is a normal thing
// to do - the group is dynamic, so re-running it is how newly-failing products
// get picked up - and a template that produced a second identical task every
// time it was run would make the inbox useless within a week.
func InstantiatePIMTaskTemplate(tenantID, actor, templateCode, groupID string) (*PIMTaskInstantiation, error) {
	template, err := fetchPIMTaskTemplate(tenantID, templateCode)
	if err != nil {
		return nil, err
	}
	resolved, err := ResolvePIMProductGroup(tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if len(resolved.Members) == 0 {
		return nil, fmt.Errorf("product group %q currently resolves to no products", groupID)
	}
	existing, err := pimItemsWithOpenTemplateTask(tenantID, template.Code)
	if err != nil {
		return nil, err
	}

	dueDate := ""
	if template.DueInDays > 0 {
		dueDate = time.Now().AddDate(0, 0, template.DueInDays).Format("2006-01-02")
	}
	out := &PIMTaskInstantiation{
		TemplateCode: template.Code, GroupID: resolved.GroupID,
		Created: []string{}, Skipped: []string{},
	}
	for _, member := range resolved.Members {
		if existing[member.ItemCode] {
			out.Skipped = append(out.Skipped, member.ItemCode)
			continue
		}
		taskID, createErr := CreatePIMTask(tenantID, actor, PIMTaskRequest{
			Title:        renderPIMTitlePattern(template.TitlePattern, member),
			TaskType:     template.TaskType,
			ScopeType:    "Product Group",
			ScopeRef:     resolved.GroupID,
			ItemCode:     member.ItemCode,
			Assignee:     template.DefaultAssignee,
			AssigneeRole: template.DefaultRole,
			DueDate:      dueDate,
			Priority:     template.Priority,
			Instructions: template.Instructions,
			Template:     template.Code,
		})
		if createErr != nil {
			// One bad product must not abandon the other ninety-nine. The
			// failure is reported in Skipped so the operator can see it.
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s (%v)", member.ItemCode, createErr))
			continue
		}
		out.Created = append(out.Created, taskID)
	}
	out.CreatedCount = len(out.Created)
	out.SkippedCount = len(out.Skipped)
	LogAuditEvent(tenantID, actor, "PIM_TASK_TEMPLATE_RUN", "Success",
		fmt.Sprintf("template %s over group %s created %d task(s), skipped %d",
			template.Code, resolved.GroupID, out.CreatedCount, out.SkippedCount))
	return out, nil
}

func pimItemsWithOpenTemplateTask(tenantID, templateCode string) (map[string]bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'item_code', '') FROM %s.documents
		 WHERE doctype = 'PIMTask' AND deleted_at IS NULL
		   AND COALESCE(data->>'template', '') = $1
		   AND status IN ('Open', 'In Progress', 'Blocked')`, schema), templateCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		if code != "" {
			out[code] = true
		}
	}
	return out, rows.Err()
}

// ListPIMTaskTemplates backs the picker on the My Work screen and the workflow
// definition form.
func ListPIMTaskTemplates(tenantID string) ([]PIMTaskTemplate, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PIMTaskTemplate' AND deleted_at IS NULL AND status = 'Active'
		ORDER BY COALESCE(data->>'name', id)`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMTaskTemplate{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		days := 0
		if value := pimString(data["due_in_days"]); value != "" {
			if parsed, convErr := strconv.ParseFloat(value, 64); convErr == nil {
				days = int(parsed)
			}
		}
		out = append(out, PIMTaskTemplate{
			ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
			TaskType: pimString(data["task_type"]), TitlePattern: pimString(data["title_pattern"]),
			DefaultAssignee: pimString(data["default_assignee"]), DefaultRole: pimString(data["default_role"]),
			DueInDays: days, Priority: pimString(data["priority"]), Instructions: pimString(data["instructions"]),
		})
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 36.2.5 - bulk task actions.
// ---------------------------------------------------------------------------

type PIMBulkTaskOutcome struct {
	TaskID string `json:"task_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

type PIMBulkTaskResult struct {
	Action    string               `json:"action"`
	Requested int                  `json:"requested"`
	Succeeded int                  `json:"succeeded"`
	Failed    int                  `json:"failed"`
	Outcomes  []PIMBulkTaskOutcome `json:"outcomes"`
}

// BulkPIMTaskAction loops the single-task engines rather than issuing one wide
// UPDATE. That is the same choice BulkDecideApproval and the OMS console's bulk
// bar make, and for the same reason: every per-task guard - the state machine,
// the assignee-exists check, the workflow advance that a completion triggers -
// still applies to every row. A partially-applicable selection therefore
// reports exactly which tasks refused and why, instead of silently doing
// something different to some of them.
func BulkPIMTaskAction(tenantID, actor, action string, taskIDs []string, value string) (*PIMBulkTaskResult, error) {
	if len(taskIDs) == 0 {
		return nil, fmt.Errorf("select at least one task")
	}
	if len(taskIDs) > 500 {
		return nil, fmt.Errorf("bulk task actions are capped at 500 tasks per request (%d selected)", len(taskIDs))
	}
	result := &PIMBulkTaskResult{Action: action, Requested: len(taskIDs), Outcomes: []PIMBulkTaskOutcome{}}
	for _, taskID := range taskIDs {
		var err error
		switch action {
		case "assign":
			err = ReassignPIMTask(tenantID, actor, taskID, value, "")
		case "status":
			err = SetPIMTaskStatus(tenantID, actor, taskID, value)
		case "due_date":
			err = SetPIMTaskDueDate(tenantID, actor, taskID, value)
		case "comment":
			err = AddPIMTaskComment(tenantID, actor, taskID, value)
		default:
			return nil, fmt.Errorf("unknown bulk task action %q - use assign, status, due_date or comment", action)
		}
		outcome := PIMBulkTaskOutcome{TaskID: taskID, OK: err == nil}
		if err != nil {
			outcome.Error = err.Error()
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	LogAuditEvent(tenantID, actor, "PIM_TASK_BULK", "Success",
		fmt.Sprintf("bulk %s over %d task(s): %d ok, %d refused", action, result.Requested, result.Succeeded, result.Failed))
	return result, nil
}

// ResolvePIMTaskTargets lets the bulk endpoint accept a product group in place
// of an explicit task list - "reassign every open task on the Winter Launch
// group". It reuses ResolvePIMProductGroupItemCodes, the same seam 36.1.3's
// bulk edit and export consume, so a group means the same thing everywhere.
func ResolvePIMTaskTargets(tenantID, groupID string, explicitIDs []string) ([]string, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return explicitIDs, nil
	}
	if len(explicitIDs) > 0 {
		// Same refusal as ResolvePIMBulkTargetIDs, for the same reason:
		// merging them leaves "what did I just change?" ambiguous at the exact
		// moment the answer matters.
		return nil, fmt.Errorf("send either a product group or an explicit task selection, not both")
	}
	itemCodes, err := ResolvePIMProductGroupItemCodes(tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if len(itemCodes) == 0 {
		return nil, fmt.Errorf("product group %q currently resolves to no products", groupID)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	placeholders := make([]string, 0, len(itemCodes))
	args := make([]interface{}, 0, len(itemCodes))
	for i, code := range itemCodes {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, code)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'PIMTask' AND deleted_at IS NULL
		  AND status IN ('Open', 'In Progress', 'Blocked')
		  AND COALESCE(data->>'item_code', '') IN (%s)
		ORDER BY id`, schema, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no open tasks exist on the products in group %q", groupID)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Shared small helpers used by both this file and pim_workflow.go.
// ---------------------------------------------------------------------------

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func orNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(unassigned)"
	}
	return value
}
