package server

import (
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Stage 36.2 - the HTTP surface for the PIM task & workflow engine.
//
// Authoring a template or a workflow definition stays on the generic document
// API (/api/v1/doc/PIMTaskTemplate, /PIMWorkflowDefinition), which already has
// the form, the RBAC, the audit trail and the CSV import. What lives here is
// only what the generic API cannot express: the task state machine, template
// instantiation against a group, and the workflow run lifecycle.
//
// The actor recorded everywhere below is Resolved-Username, not
// Resolved-User-ID. Task assignees are usernames (validated against
// users.username), so using the same identifier for the assignee, the comment
// author, completed_by and the audit actor keeps them all in one namespace -
// otherwise "who closed this?" and "who is it assigned to?" would be answered
// in two different alphabets.
// ---------------------------------------------------------------------------

// pimTaskGuard resolves the caller and checks one permission on PIMTask. Every
// handler in this file starts with it, so a permission cannot be forgotten on a
// route added later.
func pimTaskGuard(w http.ResponseWriter, r *http.Request, doctype, action string) (tenantID, actor string, ok bool) {
	tenantID = r.Header.Get("Resolved-Tenant-ID")
	actor = r.Header.Get("Resolved-Username")
	role := r.Header.Get("Resolved-Role")
	allowed, err := checkPermission(tenantID, role, doctype, action)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return "", "", false
	}
	if !allowed {
		writeAPIError(w, r, "GLOBAL-0011", "")
		return "", "", false
	}
	return tenantID, actor, true
}

func pimRequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return false
	}
	return true
}

func pimDecodeBody(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 36.2.1 / 36.2.7 - tasks.
// ---------------------------------------------------------------------------

// handlePIMTasks lists (GET) or creates (POST) tasks.
//
// GET accepts assignee=me, which the server resolves from the session rather
// than trusting a username the browser supplied. That is what makes the My Work
// inbox a link anyone can bookmark and still see only their own work.
func handlePIMTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tenantID, actor, ok := pimTaskGuard(w, r, "PIMTask", "read")
		if !ok {
			return
		}
		q := r.URL.Query()
		atoi := func(key string) int {
			n, _ := strconv.Atoi(q.Get(key))
			return n
		}
		assignee := q.Get("assignee")
		if assignee == "me" {
			assignee = actor
		}
		filter := engines.PIMTaskFilter{
			TaskID:      q.Get("task_id"),
			Assignee:    assignee,
			Status:      q.Get("status"),
			OnlyOpen:    q.Get("only_open") == "1" || q.Get("only_open") == "true",
			OnlyOverdue: q.Get("only_overdue") == "1" || q.Get("only_overdue") == "true",
			ItemCode:    q.Get("item_code"),
			WorkflowRun: q.Get("workflow_run"),
			Priority:    q.Get("priority"),
			TaskType:    q.Get("task_type"),
			Limit:       atoi("limit"),
			Offset:      atoi("offset"),
		}
		result, err := engines.ListPIMTasks(tenantID, filter)
		if err != nil {
			writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(result)

	case http.MethodPost:
		tenantID, actor, ok := pimTaskGuard(w, r, "PIMTask", "create")
		if !ok {
			return
		}
		// Decoded into a local struct rather than straight into
		// engines.PIMTaskRequest: the workflow_run/stage/template fields on
		// that struct are the engine's own, and a caller who could set
		// workflow_run would have their hand-made task counted by a run's
		// stage-completion test.
		var req struct {
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
		}
		if !pimDecodeBody(w, r, &req) {
			return
		}
		taskID, err := engines.CreatePIMTask(tenantID, actor, engines.PIMTaskRequest{
			Title: req.Title, TaskType: req.TaskType, ScopeType: req.ScopeType,
			ScopeRef: req.ScopeRef, ItemCode: req.ItemCode, Assignee: req.Assignee,
			AssigneeRole: req.AssigneeRole, DueDate: req.DueDate, Priority: req.Priority,
			Instructions: req.Instructions,
		})
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"task_id": taskID, "status": "Open"})

	default:
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handlePIMTaskAction is the single-task write surface: status, assign,
// due-date, comment and follow-up, dispatched on {action} in the path.
//
// One handler rather than five because they share the guard, the id lookup and
// the error envelope, and the difference between them is a single engine call.
func handlePIMTaskAction(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, actor, ok := pimTaskGuard(w, r, "PIMTask", "update")
	if !ok {
		return
	}
	taskID := r.PathValue("id")
	var req struct {
		Status       string `json:"status"`
		Assignee     string `json:"assignee"`
		AssigneeRole string `json:"assignee_role"`
		DueDate      string `json:"due_date"`
		Comment      string `json:"comment"`
		Note         string `json:"note"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}

	var err error
	response := map[string]interface{}{"task_id": taskID}
	switch r.PathValue("action") {
	case "status":
		err = engines.SetPIMTaskStatus(tenantID, actor, taskID, req.Status)
		response["status"] = req.Status
	case "assign":
		err = engines.ReassignPIMTask(tenantID, actor, taskID, req.Assignee, req.AssigneeRole)
		response["assignee"] = req.Assignee
	case "due-date":
		err = engines.SetPIMTaskDueDate(tenantID, actor, taskID, req.DueDate)
		response["due_date"] = req.DueDate
	case "comment":
		err = engines.AddPIMTaskComment(tenantID, actor, taskID, req.Comment)
	case "follow-up":
		// Creating a task, so it is gated on create rather than update - a role
		// allowed to progress its own work is not automatically allowed to
		// manufacture new work for other people.
		if _, _, createOK := pimTaskGuard(w, r, "PIMTask", "create"); !createOK {
			return
		}
		var newID string
		newID, err = engines.CreatePIMFollowUpTask(tenantID, actor, taskID, req.Note)
		response["follow_up_task_id"] = newID
	default:
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "Unknown task action")
		return
	}
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	response["ok"] = true
	_ = json.NewEncoder(w).Encode(response)
}

// handlePIMTaskBulk is 36.2.5. Like the OMS console's bulk bar it answers 200
// with a per-task breakdown even when some tasks refused: a selection is
// expected to be partially applicable (a Done task cannot be reassigned), and a
// blanket 4xx would hide the ones that did work.
func handlePIMTaskBulk(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, actor, ok := pimTaskGuard(w, r, "PIMTask", "update")
	if !ok {
		return
	}
	var req struct {
		Action  string   `json:"action"`
		TaskIDs []string `json:"task_ids"`
		GroupID string   `json:"group_id"`
		Value   string   `json:"value"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}
	taskIDs, err := engines.ResolvePIMTaskTargets(tenantID, req.GroupID, req.TaskIDs)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	result, err := engines.BulkPIMTaskAction(tenantID, actor, req.Action, taskIDs, req.Value)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handlePIMAssignableUsers backs the assignee picker. Gated on update rights
// over PIMTask - the same right that lets you reassign one - so it grants no
// visibility to a role that could not already put someone's name on a task.
func handlePIMAssignableUsers(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMTask", "update")
	if !ok {
		return
	}
	users, err := engines.ListPIMAssignableUsers(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"users": users})
}

// ---------------------------------------------------------------------------
// 36.2.2 - task templates.
// ---------------------------------------------------------------------------

func handlePIMTaskTemplates(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMTaskTemplate", "read")
	if !ok {
		return
	}
	templates, err := engines.ListPIMTaskTemplates(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"templates": templates})
}

// handlePIMTaskTemplateInstantiate runs a template over a product group,
// creating one task per product. Requires create on PIMTask as well as read on
// the template: this is the endpoint that actually manufactures work.
func handlePIMTaskTemplateInstantiate(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMTaskTemplate", "read")
	if !ok {
		return
	}
	_, actor, ok := pimTaskGuard(w, r, "PIMTask", "create")
	if !ok {
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.GroupID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'group_id' is required - a template is instantiated against a product group")
		return
	}
	result, err := engines.InstantiatePIMTaskTemplate(tenantID, actor, r.PathValue("code"), req.GroupID)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// ---------------------------------------------------------------------------
// 36.2.3 / 36.2.4 - workflow definitions and runs.
// ---------------------------------------------------------------------------

// handlePIMWorkflows lists the active definitions and, in the same response,
// the condition vocabulary the engine implements. One call, because the
// definition form needs both and a screen that renders a condition dropdown
// from a second, later request can render an empty one.
func handlePIMWorkflows(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMWorkflowDefinition", "read")
	if !ok {
		return
	}
	definitions, err := engines.ListPIMWorkflowDefinitions(tenantID)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"workflows":  definitions,
		"conditions": engines.ListPIMWorkflowConditions(),
	})
}

// handlePIMWorkflowStart begins a run for one product, or - when the body
// carries group_id instead of item_code - one run per product in a group
// (36.2.5).
func handlePIMWorkflowStart(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMWorkflowRun", "create")
	if !ok {
		return
	}
	// Starting a run creates tasks, so it needs the same right as creating one
	// directly. Without this a role could manufacture work for other people by
	// going through a workflow instead of the task endpoint.
	_, actor, ok := pimTaskGuard(w, r, "PIMTask", "create")
	if !ok {
		return
	}
	var req struct {
		ItemCode string `json:"item_code"`
		GroupID  string `json:"group_id"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}
	code := r.PathValue("code")
	itemCode := strings.TrimSpace(req.ItemCode)
	groupID := strings.TrimSpace(req.GroupID)
	switch {
	case itemCode != "" && groupID != "":
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity,
			"Send either 'item_code' or 'group_id', not both")
	case groupID != "":
		result, err := engines.StartPIMWorkflowForGroup(tenantID, actor, code, groupID)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	case itemCode != "":
		runID, err := engines.StartPIMWorkflowRun(tenantID, actor, code, itemCode)
		if err != nil {
			writeEngineError(w, r, err, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"run_id": runID, "item_code": itemCode})
	default:
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity,
			"Field 'item_code' or 'group_id' is required")
	}
}

func handlePIMWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodGet) {
		return
	}
	tenantID, _, ok := pimTaskGuard(w, r, "PIMWorkflowRun", "read")
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	runs, err := engines.ListPIMWorkflowRuns(tenantID, q.Get("status"), q.Get("item_code"), limit)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"runs": runs})
}

// handlePIMWorkflowRunAction is 36.2.4: pause, resume, cancel and advance.
func handlePIMWorkflowRunAction(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, actor, ok := pimTaskGuard(w, r, "PIMWorkflowRun", "update")
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}
	message, err := engines.SetPIMWorkflowRunState(tenantID, actor, r.PathValue("id"), req.Action)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": message})
}

// handlePIMWorkflowRunBulk is 36.2.5's other half - the same four actions over
// a selection of runs.
func handlePIMWorkflowRunBulk(w http.ResponseWriter, r *http.Request) {
	if !pimRequireMethod(w, r, http.MethodPost) {
		return
	}
	tenantID, actor, ok := pimTaskGuard(w, r, "PIMWorkflowRun", "update")
	if !ok {
		return
	}
	var req struct {
		Action string   `json:"action"`
		RunIDs []string `json:"run_ids"`
	}
	if !pimDecodeBody(w, r, &req) {
		return
	}
	result, err := engines.BulkPIMWorkflowRunAction(tenantID, actor, req.Action, req.RunIDs)
	if err != nil {
		writeEngineError(w, r, err, http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
