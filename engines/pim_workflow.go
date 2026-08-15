package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Stage 36.2.3 / 36.2.4 / 36.2.5 - the PIM workflow engine.
//
// A workflow is an ordered list of stages a product travels while it is being
// made ready. Each stage optionally instantiates a task template, so the work
// lands in a named person's inbox, and each stage has an entry and an exit
// condition drawn from a CLOSED vocabulary (pimWorkflowConditions below).
//
// The single most important design decision here: this is a declarative,
// table-driven engine and NOT a scripting runtime. A workflow definition is
// authored by a category manager in an ordinary form. A form that accepts an
// expression language is a remote code execution surface wearing a lab coat,
// and it also guarantees that the next person to open the definition cannot
// tell what it does. So conditions are a fixed set of named checks, each taking
// at most one operand, each implemented in Go here, each individually testable.
// If a condition someone needs is missing, the answer is to add it to the map -
// a five-line change, reviewed - not to let the tenant write code.
//
// Parallel branches are expressed by giving two or more stages the same
// parallel_group. Stages in a group are entered together and every one of them
// must satisfy its exit before the run moves past the group. A blank
// parallel_group means the stage is a group of one, i.e. ordinary sequential
// behaviour, which is what almost every workflow will use.
// ---------------------------------------------------------------------------

type PIMWorkflowStage struct {
	StageCode      string `json:"stage_code"`
	Label          string `json:"label"`
	Sequence       int    `json:"sequence"`
	ParallelGroup  string `json:"parallel_group"`
	Assignee       string `json:"assignee"`
	AssigneeRole   string `json:"assignee_role"`
	TaskTemplate   string `json:"task_template"`
	DueInDays      int    `json:"due_in_days"`
	EntryCondition string `json:"entry_condition"`
	EntryValue     string `json:"entry_value"`
	ExitCondition  string `json:"exit_condition"`
	ExitValue      string `json:"exit_value"`
}

type PIMWorkflowDef struct {
	ID          string             `json:"id"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Stages      []PIMWorkflowStage `json:"stages"`
}

type PIMWorkflowActivity struct {
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Event  string `json:"event"`
	Detail string `json:"detail,omitempty"`
}

type PIMWorkflowRunView struct {
	ID            string                `json:"id"`
	Workflow      string                `json:"workflow"`
	WorkflowName  string                `json:"workflow_name"`
	ItemCode      string                `json:"item_code"`
	ItemName      string                `json:"item_name"`
	CurrentStage  string                `json:"current_stage"`
	CurrentGroup  string                `json:"current_group"`
	Status        string                `json:"status"`
	StartedAt     string                `json:"started_at"`
	CompletedAt   string                `json:"completed_at,omitempty"`
	BlockedReason string                `json:"blocked_reason,omitempty"`
	Activity      []PIMWorkflowActivity `json:"activity"`
	OpenTasks     int                   `json:"open_tasks"`
	TotalTasks    int                   `json:"total_tasks"`
	StageProgress string                `json:"stage_progress"`
}

// ---------------------------------------------------------------------------
// The condition vocabulary.
//
// Each entry says what the condition means and whether it needs an operand.
// Exported through ListPIMWorkflowConditions so the definition form can offer a
// dropdown of exactly what the engine implements - a screen that lets someone
// type a condition name the engine has never heard of produces a workflow that
// silently never advances, which is the worst possible failure for this feature.
// ---------------------------------------------------------------------------

type pimWorkflowCondition struct {
	Description  string
	NeedsValue   bool
	ValueHint    string
	evaluate     func(tenantID, itemCode, runID, stageCode, value string) (bool, string, error)
}

// PIMWorkflowConditionInfo is the form-facing description of one condition.
type PIMWorkflowConditionInfo struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	NeedsValue  bool   `json:"needs_value"`
	ValueHint   string `json:"value_hint,omitempty"`
}

var pimWorkflowConditions = map[string]pimWorkflowCondition{
	"always": {
		Description: "No condition - always satisfied.",
		evaluate: func(_, _, _, _, _ string) (bool, string, error) {
			return true, "", nil
		},
	},
	"tasks_complete": {
		Description: "Every task this stage created has been closed. This is the default exit condition, and it is what makes a stage with no tasks pass straight through.",
		evaluate: func(tenantID, _, runID, stageCode, _ string) (bool, string, error) {
			open, err := pimOpenTaskCountForStage(tenantID, runID, stageCode)
			if err != nil {
				return false, "", err
			}
			if open > 0 {
				return false, fmt.Sprintf("%d task(s) still open on this stage", open), nil
			}
			return true, "", nil
		},
	},
	"completeness_at_least": {
		Description: "The product's completeness score has reached a threshold.",
		NeedsValue:  true, ValueHint: "a percentage, 0-100",
		evaluate: func(tenantID, itemCode, _, _, value string) (bool, string, error) {
			threshold, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				return false, "", fmt.Errorf("completeness_at_least needs a number, got %q", value)
			}
			result, err := CalculateCompleteness(tenantID, itemCode, "en", "")
			if err != nil {
				return false, "", err
			}
			if result.Score < threshold {
				return false, fmt.Sprintf("completeness is %.1f%%, needs %.1f%%", result.Score, threshold), nil
			}
			return true, "", nil
		},
	},
	"attribute_present": {
		Description: "A named product attribute has a non-empty value.",
		NeedsValue:  true, ValueHint: "an attribute code",
		evaluate: func(tenantID, itemCode, _, _, value string) (bool, string, error) {
			attribute := strings.TrimSpace(value)
			if attribute == "" {
				return false, "", fmt.Errorf("attribute_present needs an attribute code")
			}
			resolved, err := ResolveAttributeValue(tenantID, itemCode, attribute, "en", "")
			if err != nil {
				return false, "", err
			}
			if strings.TrimSpace(resolved) == "" {
				return false, fmt.Sprintf("attribute %q is empty", attribute), nil
			}
			return true, "", nil
		},
	},
	"has_main_image": {
		Description: "The product has an active Main Image in the DAM.",
		evaluate: func(tenantID, itemCode, _, _, _ string) (bool, string, error) {
			count, err := pimCountDocuments(tenantID, `doctype = 'ProductMedia'
				AND data->>'item' = $1 AND data->>'media_role' = 'Main Image' AND status = 'Active'`, itemCode)
			if err != nil {
				return false, "", err
			}
			if count == 0 {
				return false, "no active Main Image", nil
			}
			return true, "", nil
		},
	},
	"content_approved": {
		Description: "The product has approved marketing content in the default locale.",
		evaluate: func(tenantID, itemCode, _, _, _ string) (bool, string, error) {
			count, err := pimCountDocuments(tenantID, `doctype = 'ProductContent'
				AND data->>'product_id' = $1 AND data->>'language' = 'en' AND status = 'Approved'`, itemCode)
			if err != nil {
				return false, "", err
			}
			if count == 0 {
				return false, "no approved content in 'en'", nil
			}
			return true, "", nil
		},
	},
	"item_status": {
		Description: "The product itself is in a given status.",
		NeedsValue:  true, ValueHint: "Active or Inactive",
		evaluate: func(tenantID, itemCode, _, _, value string) (bool, string, error) {
			want := strings.TrimSpace(value)
			schema, err := db.GetTenantSchema(tenantID)
			if err != nil {
				return false, "", err
			}
			var status string
			if err := db.DB.QueryRow(fmt.Sprintf(
				`SELECT status FROM %s.documents WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL`, schema),
				itemCode).Scan(&status); err != nil {
				return false, "", fmt.Errorf("item %q not found", itemCode)
			}
			if status != want {
				return false, fmt.Sprintf("item is %s, needs %s", status, want), nil
			}
			return true, "", nil
		},
	},
}

// ListPIMWorkflowConditions returns the vocabulary in a stable order, for the
// definition form's condition dropdown.
func ListPIMWorkflowConditions() []PIMWorkflowConditionInfo {
	keys := make([]string, 0, len(pimWorkflowConditions))
	for key := range pimWorkflowConditions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]PIMWorkflowConditionInfo, 0, len(keys))
	for _, key := range keys {
		condition := pimWorkflowConditions[key]
		out = append(out, PIMWorkflowConditionInfo{
			Key: key, Description: condition.Description,
			NeedsValue: condition.NeedsValue, ValueHint: condition.ValueHint,
		})
	}
	return out
}

func pimCountDocuments(tenantID, where string, args ...interface{}) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var count int
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE deleted_at IS NULL AND (%s)`, schema, where), args...).Scan(&count)
	return count, err
}

func pimOpenTaskCountForStage(tenantID, runID, stageCode string) (int, error) {
	return pimCountDocuments(tenantID, `doctype = 'PIMTask'
		AND COALESCE(data->>'workflow_run', '') = $1
		AND COALESCE(data->>'stage', '') = $2
		AND status IN ('Open', 'In Progress', 'Blocked')`, runID, stageCode)
}

// evaluatePIMCondition runs one condition and returns whether it holds plus,
// when it does not, a human sentence saying why. That sentence is what ends up
// in the run's blocked_reason and on the screen - "waiting" with no reason is
// the single most common complaint about workflow tools.
func evaluatePIMCondition(tenantID, itemCode, runID, stageCode, name, value string) (bool, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "always"
	}
	condition, known := pimWorkflowConditions[name]
	if !known {
		return false, "", fmt.Errorf("unknown workflow condition %q", name)
	}
	return condition.evaluate(tenantID, itemCode, runID, stageCode, value)
}

// ---------------------------------------------------------------------------
// Definition loading and validation.
// ---------------------------------------------------------------------------

// ValidatePIMWorkflowDefinitionDocument runs at ValidateDocument's shared exit,
// so a definition saved through the generic form or a CSV import is checked the
// same way. Every check here exists because the failure it prevents is silent:
// a workflow with a duplicate stage code, an unknown condition or no stages at
// all saves happily and then simply never advances, and the person waiting on
// it has no way to tell why.
func ValidatePIMWorkflowDefinitionDocument(tenantID string, payload map[string]interface{}) error {
	stages, err := decodePIMWorkflowStages(payload["stages"])
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Stages", Message: err.Error()}
	}
	if len(stages) == 0 {
		return &ValidationError{Code: "GLOBAL-0001", SubFor: "Stages",
			Message: "a workflow needs at least one stage"}
	}
	seenCode := map[string]bool{}
	seenSequence := map[int]string{}
	for i, stage := range stages {
		if strings.TrimSpace(stage.StageCode) == "" {
			return &ValidationError{Code: "GLOBAL-0001", SubFor: "Stages",
				Message: fmt.Sprintf("stage %d has no stage code", i+1)}
		}
		if seenCode[stage.StageCode] {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Stages",
				Message: fmt.Sprintf("stage code %q appears more than once - a run tracks its position by stage code, so they must be unique", stage.StageCode)}
		}
		seenCode[stage.StageCode] = true

		// Two stages sharing a sequence but not a parallel group is almost
		// always a typo for "these run in parallel", and the resulting order
		// would otherwise depend on map iteration. Refuse it and say which
		// field expresses the intent.
		if other, clash := seenSequence[stage.Sequence]; clash && stage.ParallelGroup == "" {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Stages",
				Message: fmt.Sprintf("stages %q and %q share sequence %d - give them the same Parallel Group if they are meant to run together", other, stage.StageCode, stage.Sequence)}
		}
		seenSequence[stage.Sequence] = stage.StageCode

		for label, pair := range map[string][2]string{
			"entry": {stage.EntryCondition, stage.EntryValue},
			"exit":  {stage.ExitCondition, stage.ExitValue},
		} {
			name := strings.TrimSpace(pair[0])
			if name == "" {
				continue
			}
			condition, known := pimWorkflowConditions[name]
			if !known {
				return &ValidationError{Code: "GLOBAL-0002", SubFor: "Stages",
					Message: fmt.Sprintf("stage %q has an unknown %s condition %q", stage.StageCode, label, name)}
			}
			if condition.NeedsValue && strings.TrimSpace(pair[1]) == "" {
				return &ValidationError{Code: "GLOBAL-0001", SubFor: "Stages",
					Message: fmt.Sprintf("stage %q: %s condition %q needs a value (%s)", stage.StageCode, label, name, condition.ValueHint)}
			}
		}

		// A stage naming a template that does not exist creates no tasks and
		// therefore passes tasks_complete instantly - the workflow appears to
		// work while doing nothing at all.
		if template := strings.TrimSpace(stage.TaskTemplate); template != "" && db.DB != nil {
			if _, lookupErr := fetchPIMTaskTemplate(tenantID, template); lookupErr != nil {
				return &ValidationError{Code: "META-0198", SubFor: "Stages",
					Message: fmt.Sprintf("stage %q names task template %q: %v", stage.StageCode, template, lookupErr)}
			}
		}
		if assignee := strings.TrimSpace(stage.Assignee); assignee != "" && db.DB != nil {
			ok, lookupErr := pimUserExists(tenantID, assignee)
			if lookupErr != nil {
				return lookupErr
			}
			if !ok {
				return &ValidationError{Code: "META-0198", SubFor: "Stages",
					Message: fmt.Sprintf("stage %q is assigned to %q, who is not an active user", stage.StageCode, assignee)}
			}
		}
	}
	return nil
}

func decodePIMWorkflowStages(raw interface{}) ([]PIMWorkflowStage, error) {
	var rows []map[string]interface{}
	if err := decodeProductGroupJSON(raw, &rows); err != nil {
		return nil, fmt.Errorf("stages must be a JSON array of stage rows: %w", err)
	}
	stages := make([]PIMWorkflowStage, 0, len(rows))
	for _, row := range rows {
		sequence := 0
		if value := pimString(row["sequence"]); value != "" {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("stage %q has a non-numeric sequence %q", pimString(row["stage_code"]), value)
			}
			sequence = int(parsed)
		}
		dueInDays := 0
		if value := pimString(row["due_in_days"]); value != "" {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("stage %q has a non-numeric due_in_days %q", pimString(row["stage_code"]), value)
			}
			dueInDays = int(parsed)
		}
		stages = append(stages, PIMWorkflowStage{
			StageCode: pimString(row["stage_code"]), Label: pimString(row["label"]),
			Sequence: sequence, ParallelGroup: pimString(row["parallel_group"]),
			Assignee: pimString(row["assignee"]), AssigneeRole: pimString(row["assignee_role"]),
			TaskTemplate: pimString(row["task_template"]), DueInDays: dueInDays,
			EntryCondition: pimString(row["entry_condition"]), EntryValue: pimString(row["entry_value"]),
			ExitCondition: pimString(row["exit_condition"]), ExitValue: pimString(row["exit_value"]),
		})
	}
	return stages, nil
}

func FetchPIMWorkflowDefinition(tenantID, code string) (*PIMWorkflowDef, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMWorkflowDefinition' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, id LIMIT 1`, schema), code).Scan(&id, &raw, &status)
	if err != nil {
		return nil, fmt.Errorf("workflow %q not found", code)
	}
	if status != "Active" {
		return nil, fmt.Errorf("workflow %q is not active", code)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("workflow %q has invalid stored data: %w", code, err)
	}
	stages, err := decodePIMWorkflowStages(data["stages"])
	if err != nil {
		return nil, fmt.Errorf("workflow %q: %w", code, err)
	}
	return &PIMWorkflowDef{
		ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
		Description: pimString(data["description"]), Stages: stages,
	}, nil
}

// ListPIMWorkflowDefinitions backs the "start a workflow" picker.
func ListPIMWorkflowDefinitions(tenantID string) ([]PIMWorkflowDef, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'PIMWorkflowDefinition' AND deleted_at IS NULL AND status = 'Active'
		ORDER BY COALESCE(data->>'name', id)`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMWorkflowDef{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		stages, _ := decodePIMWorkflowStages(data["stages"])
		out = append(out, PIMWorkflowDef{
			ID: id, Code: pimString(data["code"]), Name: pimString(data["name"]),
			Description: pimString(data["description"]), Stages: stages,
		})
	}
	return out, rows.Err()
}

// pimStageGroups collapses the stage list into the ordered groups the run
// actually travels. A blank parallel_group makes the stage a group of one, so
// the common sequential case needs no special handling anywhere else.
type pimStageGroup struct {
	Key    string
	Stages []PIMWorkflowStage
	Order  int
}

func pimStageGroups(def *PIMWorkflowDef) []pimStageGroup {
	byKey := map[string]*pimStageGroup{}
	order := []string{}
	for _, stage := range def.Stages {
		key := strings.TrimSpace(stage.ParallelGroup)
		if key == "" {
			key = "@" + stage.StageCode
		}
		group, exists := byKey[key]
		if !exists {
			group = &pimStageGroup{Key: key, Order: stage.Sequence}
			byKey[key] = group
			order = append(order, key)
		}
		if stage.Sequence < group.Order {
			group.Order = stage.Sequence
		}
		group.Stages = append(group.Stages, stage)
	}
	groups := make([]pimStageGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, *byKey[key])
	}
	// Sorted by the group's lowest sequence, with the authoring order as the
	// tie-break so two groups that forgot to set a sequence still travel in the
	// order they were written rather than in map order.
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Order < groups[j].Order })
	for i := range groups {
		sort.SliceStable(groups[i].Stages, func(a, b int) bool {
			return groups[i].Stages[a].Sequence < groups[i].Stages[b].Sequence
		})
	}
	return groups
}

// ---------------------------------------------------------------------------
// Runs: start, advance, pause, resume, cancel.
// ---------------------------------------------------------------------------

func pimRunDocument(tenantID, runID string) (string, map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, err
	}
	var id, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMWorkflowRun' AND id = $1 AND deleted_at IS NULL`, schema), runID).Scan(&id, &raw, &status)
	if err != nil {
		return "", nil, fmt.Errorf("workflow run %q not found", runID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", nil, fmt.Errorf("workflow run %q has invalid stored data: %w", runID, err)
	}
	data["status"] = status
	return id, data, nil
}

// appendPIMRunActivity is the run's own history. Kept on the run because every
// question it answers - which stage, when, who paused it, why it stalled - is
// asked about one run at a time, and a join against the global audit log to
// render a screen that is opened constantly would be wasteful. The coarse
// events still reach LogAuditEvent; this is the detail beneath them.
func appendPIMRunActivity(data map[string]interface{}, actor, event, detail string) {
	var activity []PIMWorkflowActivity
	_ = decodeProductGroupJSON(data["activity"], &activity)
	activity = append(activity, PIMWorkflowActivity{
		At: time.Now().UTC().Format(time.RFC3339), Actor: orNone(actor), Event: event, Detail: detail,
	})
	encoded, err := json.Marshal(activity)
	if err != nil {
		return
	}
	data["activity"] = string(encoded)
}

// StartPIMWorkflowRun begins one product's journey through a workflow, then
// immediately tries to advance it - so the first stage's tasks exist the moment
// the run is created rather than after some later trigger.
func StartPIMWorkflowRun(tenantID, actor, workflowCode, itemCode string) (string, error) {
	def, err := FetchPIMWorkflowDefinition(tenantID, workflowCode)
	if err != nil {
		return "", err
	}
	if len(def.Stages) == 0 {
		return "", fmt.Errorf("workflow %q has no stages", def.Code)
	}
	if db.DB != nil {
		exists, existsErr := verifyDocumentExists(tenantID, "Item", itemCode)
		if existsErr != nil {
			return "", existsErr
		}
		if !exists {
			return "", fmt.Errorf("item %q does not exist", itemCode)
		}
	}
	runID := NewDocID("PWFR")
	data := map[string]interface{}{
		"code": runID, "workflow": def.ID, "item_code": itemCode,
		"current_stage": "", "current_group": "",
		"started_at": time.Now().UTC().Format(time.RFC3339),
		"activity":   "[]", "status": "Running",
	}
	appendPIMRunActivity(data, actor, "started", fmt.Sprintf("workflow %s on %s", def.Code, itemCode))
	if err := insertPIMDocument(tenantID, "PIMWorkflowRun", runID, data); err != nil {
		// The partial unique index on (workflow, item_code) for live runs is
		// what makes a duplicate impossible, so translate its violation into
		// the sentence the operator actually needs.
		if strings.Contains(strings.ToLower(err.Error()), "idx_documents_pim_workflow_run_live_unique") {
			return "", fmt.Errorf("%s is already running workflow %s - pause or cancel that run first", itemCode, def.Code)
		}
		return "", err
	}
	LogAuditEvent(tenantID, actor, "PIM_WORKFLOW_STARTED", "Success",
		fmt.Sprintf("run %s: workflow %s on item %s", runID, def.Code, itemCode))
	if _, err := AdvancePIMWorkflowRun(tenantID, actor, runID); err != nil {
		// The run exists and is recoverable by hand; returning an error here
		// would leave the caller believing nothing was created.
		LogSystemError(tenantID, runID, "Warning", "pim_workflow",
			fmt.Sprintf("run %s created but did not enter its first stage: %v", runID, err), "")
	}
	return runID, nil
}

// AdvancePIMWorkflowRun is the whole engine in one function, and it is
// deliberately idempotent: calling it on a run that cannot move yet changes
// nothing except the recorded reason it cannot move. That is what lets it be
// called from three places - run creation, every task completion, and an
// operator pressing Advance - without any of them needing to know whether the
// others already did.
//
// Returns the sentence describing what happened, for the caller to show.
func AdvancePIMWorkflowRun(tenantID, actor, runID string) (string, error) {
	id, data, err := pimRunDocument(tenantID, runID)
	if err != nil {
		return "", err
	}
	status := pimString(data["status"])
	switch status {
	case "Paused":
		return "", fmt.Errorf("run %s is paused - resume it before advancing", runID)
	case "Completed", "Cancelled":
		return "", fmt.Errorf("run %s is %s", runID, strings.ToLower(status))
	case "Running":
	default:
		return "", fmt.Errorf("run %s is in unknown status %q", runID, status)
	}

	def, err := FetchPIMWorkflowDefinition(tenantID, pimString(data["workflow"]))
	if err != nil {
		return "", err
	}
	itemCode := pimString(data["item_code"])
	groups := pimStageGroups(def)
	if len(groups) == 0 {
		return "", fmt.Errorf("workflow %s has no stages", def.Code)
	}

	currentKey := pimString(data["current_group"])
	currentIndex := -1
	for i, group := range groups {
		if group.Key == currentKey {
			currentIndex = i
			break
		}
	}
	// A run whose stored group no longer exists in the definition (someone
	// edited the workflow while it was mid-flight) is stopped rather than
	// silently restarted from the beginning - restarting would re-create every
	// task the product has already been through.
	if currentKey != "" && currentIndex < 0 {
		reason := fmt.Sprintf("stage group %q no longer exists in workflow %s - edit the run or cancel it", currentKey, def.Code)
		data["blocked_reason"] = reason
		appendPIMRunActivity(data, actor, "blocked", reason)
		if err := writePIMDocument(tenantID, "PIMWorkflowRun", id, data); err != nil {
			return "", err
		}
		return reason, nil
	}

	if currentIndex >= 0 {
		// Still in a group: check every stage's exit before moving on.
		for _, stage := range groups[currentIndex].Stages {
			exitName := stage.ExitCondition
			if strings.TrimSpace(exitName) == "" {
				exitName = "tasks_complete"
			}
			satisfied, why, evalErr := evaluatePIMCondition(tenantID, itemCode, runID, stage.StageCode, exitName, stage.ExitValue)
			if evalErr != nil {
				return "", evalErr
			}
			if !satisfied {
				reason := fmt.Sprintf("stage %s is not finished: %s", stage.StageCode, why)
				if pimString(data["blocked_reason"]) != reason {
					data["blocked_reason"] = reason
					if err := writePIMDocument(tenantID, "PIMWorkflowRun", id, data); err != nil {
						return "", err
					}
				}
				return reason, nil
			}
		}
	}

	nextIndex := currentIndex + 1
	if nextIndex >= len(groups) {
		data["status"] = "Completed"
		data["completed_at"] = time.Now().UTC().Format(time.RFC3339)
		data["current_stage"] = ""
		data["current_group"] = ""
		data["blocked_reason"] = ""
		appendPIMRunActivity(data, actor, "completed", fmt.Sprintf("workflow %s finished for %s", def.Code, itemCode))
		if err := writePIMDocument(tenantID, "PIMWorkflowRun", id, data); err != nil {
			return "", err
		}
		LogAuditEvent(tenantID, actor, "PIM_WORKFLOW_COMPLETED", "Success",
			fmt.Sprintf("run %s: workflow %s on item %s", runID, def.Code, itemCode))
		return fmt.Sprintf("run %s completed", runID), nil
	}

	next := groups[nextIndex]
	// Entry conditions are checked BEFORE leaving the current group, so a run
	// held up by the next stage's entry stays where it is with an explanation,
	// rather than being stranded between two stages.
	for _, stage := range next.Stages {
		satisfied, why, evalErr := evaluatePIMCondition(tenantID, itemCode, runID, stage.StageCode, stage.EntryCondition, stage.EntryValue)
		if evalErr != nil {
			return "", evalErr
		}
		if !satisfied {
			reason := fmt.Sprintf("cannot enter stage %s: %s", stage.StageCode, why)
			data["blocked_reason"] = reason
			appendPIMRunActivity(data, actor, "blocked", reason)
			if err := writePIMDocument(tenantID, "PIMWorkflowRun", id, data); err != nil {
				return "", err
			}
			return reason, nil
		}
	}

	stageCodes := make([]string, 0, len(next.Stages))
	created := 0
	for _, stage := range next.Stages {
		stageCodes = append(stageCodes, stage.StageCode)
		count, taskErr := createPIMStageTasks(tenantID, actor, runID, itemCode, stage)
		if taskErr != nil {
			return "", taskErr
		}
		created += count
	}
	data["current_group"] = next.Key
	data["current_stage"] = strings.Join(stageCodes, ", ")
	data["blocked_reason"] = ""
	detail := fmt.Sprintf("entered %s", strings.Join(stageCodes, ", "))
	if created > 0 {
		detail += fmt.Sprintf(", created %d task(s)", created)
	}
	appendPIMRunActivity(data, actor, "advanced", detail)
	if err := writePIMDocument(tenantID, "PIMWorkflowRun", id, data); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, actor, "PIM_WORKFLOW_ADVANCED", "Success",
		fmt.Sprintf("run %s %s", runID, detail))

	// A stage with no tasks and a satisfied exit should not need a second
	// press of Advance to move on, so recurse once the group is entered. The
	// recursion terminates because every call either completes the run,
	// returns a blocked reason, or moves strictly forward through a finite
	// group list.
	if created == 0 {
		if onward, onwardErr := AdvancePIMWorkflowRun(tenantID, actor, runID); onwardErr == nil && onward != "" {
			return onward, nil
		}
	}
	return detail, nil
}

// createPIMStageTasks instantiates a stage's task template against this run's
// one product. Reuses CreatePIMTask so a workflow-created task passes exactly
// the same validation a hand-created one does.
func createPIMStageTasks(tenantID, actor, runID, itemCode string, stage PIMWorkflowStage) (int, error) {
	if strings.TrimSpace(stage.TaskTemplate) == "" {
		return 0, nil
	}
	template, err := fetchPIMTaskTemplate(tenantID, stage.TaskTemplate)
	if err != nil {
		return 0, fmt.Errorf("stage %s: %w", stage.StageCode, err)
	}
	// The stage's own assignee wins over the template's default: the template
	// says what the work is, the stage says who does it at this point in this
	// particular workflow.
	assignee := strings.TrimSpace(stage.Assignee)
	if assignee == "" {
		assignee = template.DefaultAssignee
	}
	assigneeRole := strings.TrimSpace(stage.AssigneeRole)
	if assigneeRole == "" {
		assigneeRole = template.DefaultRole
	}
	dueInDays := stage.DueInDays
	if dueInDays <= 0 {
		dueInDays = template.DueInDays
	}
	dueDate := ""
	if dueInDays > 0 {
		dueDate = time.Now().AddDate(0, 0, dueInDays).Format("2006-01-02")
	}

	member := ProductGroupMember{ItemCode: itemCode}
	if name, family, status, lookupErr := pimItemSummary(tenantID, itemCode); lookupErr == nil {
		member.Name, member.Family, member.Status = name, family, status
	}
	_, err = CreatePIMTask(tenantID, actor, PIMTaskRequest{
		Title:        renderPIMTitlePattern(template.TitlePattern, member),
		TaskType:     template.TaskType,
		ScopeType:    "Product",
		ScopeRef:     itemCode,
		ItemCode:     itemCode,
		Assignee:     assignee,
		AssigneeRole: assigneeRole,
		DueDate:      dueDate,
		Priority:     template.Priority,
		Instructions: template.Instructions,
		Template:     template.Code,
		WorkflowRun:  runID,
		Stage:        stage.StageCode,
	})
	if err != nil {
		return 0, fmt.Errorf("stage %s: %w", stage.StageCode, err)
	}
	return 1, nil
}

func pimItemSummary(tenantID, itemCode string) (string, string, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", "", err
	}
	var name, family, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name',''), COALESCE(data->>'family',''), status
		FROM %s.documents WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL`, schema),
		itemCode).Scan(&name, &family, &status)
	return name, family, status, err
}

// SetPIMWorkflowRunState implements 36.2.4's pause / resume / cancel, plus the
// operator-driven advance, behind one guarded transition table.
func SetPIMWorkflowRunState(tenantID, actor, runID, action string) (string, error) {
	if action == "advance" {
		return AdvancePIMWorkflowRun(tenantID, actor, runID)
	}
	id, data, err := pimRunDocument(tenantID, runID)
	if err != nil {
		return "", err
	}
	current := pimString(data["status"])
	var target string
	switch action {
	case "pause":
		if current != "Running" {
			return "", fmt.Errorf("only a running workflow can be paused (run %s is %s)", runID, current)
		}
		target = "Paused"
	case "resume":
		if current != "Paused" {
			return "", fmt.Errorf("only a paused workflow can be resumed (run %s is %s)", runID, current)
		}
		target = "Running"
	case "cancel":
		if current == "Completed" || current == "Cancelled" {
			return "", fmt.Errorf("run %s is already %s", runID, strings.ToLower(current))
		}
		target = "Cancelled"
	default:
		return "", fmt.Errorf("unknown workflow action %q - use pause, resume, cancel or advance", action)
	}

	data["status"] = target
	if target == "Cancelled" {
		data["completed_at"] = time.Now().UTC().Format(time.RFC3339)
		data["current_stage"] = ""
		data["current_group"] = ""
	}
	appendPIMRunActivity(data, actor, strings.ToLower(action), "")
	if err := writePIMDocument(tenantID, "PIMWorkflowRun", id, data); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, actor, "PIM_WORKFLOW_"+strings.ToUpper(action), "Success",
		fmt.Sprintf("run %s -> %s", runID, target))

	if target == "Cancelled" {
		// Leaving a cancelled run's tasks open would put work in people's
		// inboxes for a product nobody is progressing any more - the single
		// most common way a task list loses its credibility.
		cancelled, cancelErr := cancelPIMRunTasks(tenantID, actor, runID)
		if cancelErr != nil {
			LogSystemError(tenantID, runID, "Warning", "pim_workflow",
				fmt.Sprintf("run %s cancelled but its open tasks were not: %v", runID, cancelErr), "")
		} else if cancelled > 0 {
			return fmt.Sprintf("run %s cancelled, %d open task(s) cancelled with it", runID, cancelled), nil
		}
	}
	if target == "Running" {
		// Resuming should immediately pick up anything that became true while
		// the run was paused.
		if onward, advErr := AdvancePIMWorkflowRun(tenantID, actor, runID); advErr == nil && onward != "" {
			return fmt.Sprintf("run %s resumed - %s", runID, onward), nil
		}
	}
	return fmt.Sprintf("run %s is now %s", runID, target), nil
}

func cancelPIMRunTasks(tenantID, actor, runID string) (int, error) {
	tasks, err := ListPIMTasks(tenantID, PIMTaskFilter{WorkflowRun: runID, OnlyOpen: true, Limit: 500})
	if err != nil {
		return 0, err
	}
	cancelled := 0
	for _, task := range tasks.Tasks {
		if err := SetPIMTaskStatus(tenantID, actor, task.ID, "Cancelled"); err == nil {
			cancelled++
		}
	}
	return cancelled, nil
}

// ---------------------------------------------------------------------------
// Reads for the screen.
// ---------------------------------------------------------------------------

func ListPIMWorkflowRuns(tenantID, status, itemCode string, limit int) ([]PIMWorkflowRunView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where := []string{"r.doctype = 'PIMWorkflowRun'", "r.deleted_at IS NULL"}
	args := []interface{}{}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("r.status = $%d", len(args)))
	}
	if itemCode != "" {
		args = append(args, itemCode)
		where = append(where, fmt.Sprintf("COALESCE(r.data->>'item_code','') = $%d", len(args)))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT r.id, r.data, r.status,
		       COALESCE(i.data->>'name', ''), COALESCE(w.data->>'name', ''),
		       (SELECT COUNT(*) FROM %s.documents t
		         WHERE t.doctype = 'PIMTask' AND t.deleted_at IS NULL
		           AND COALESCE(t.data->>'workflow_run','') = r.id),
		       (SELECT COUNT(*) FROM %s.documents t
		         WHERE t.doctype = 'PIMTask' AND t.deleted_at IS NULL
		           AND COALESCE(t.data->>'workflow_run','') = r.id
		           AND t.status IN ('Open','In Progress','Blocked'))
		  FROM %s.documents r
		  LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.id = r.data->>'item_code' AND i.deleted_at IS NULL
		  LEFT JOIN %s.documents w ON w.doctype = 'PIMWorkflowDefinition' AND w.id = r.data->>'workflow' AND w.deleted_at IS NULL
		 WHERE %s
		 ORDER BY r.created_at DESC
		 LIMIT %d`, schema, schema, schema, schema, schema, strings.Join(where, " AND "), limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PIMWorkflowRunView{}
	for rows.Next() {
		var id, raw, runStatus, itemName, workflowName string
		var totalTasks, openTasks int
		if err := rows.Scan(&id, &raw, &runStatus, &itemName, &workflowName, &totalTasks, &openTasks); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		view := PIMWorkflowRunView{
			ID: id, Workflow: pimString(data["workflow"]), WorkflowName: workflowName,
			ItemCode: pimString(data["item_code"]), ItemName: itemName,
			CurrentStage: pimString(data["current_stage"]), CurrentGroup: pimString(data["current_group"]),
			Status: runStatus, StartedAt: pimString(data["started_at"]),
			CompletedAt: pimString(data["completed_at"]), BlockedReason: pimString(data["blocked_reason"]),
			Activity: []PIMWorkflowActivity{}, OpenTasks: openTasks, TotalTasks: totalTasks,
		}
		_ = decodeProductGroupJSON(data["activity"], &view.Activity)
		if view.Activity == nil {
			view.Activity = []PIMWorkflowActivity{}
		}
		view.StageProgress = pimStageProgressLabel(tenantID, view)
		out = append(out, view)
	}
	return out, rows.Err()
}

// pimStageProgressLabel renders "2 of 4" so a list of runs is scannable
// without opening each one. A definition that cannot be loaded (deleted,
// deactivated) degrades to the stage name rather than failing the whole list -
// one broken definition must not take the screen down.
func pimStageProgressLabel(tenantID string, view PIMWorkflowRunView) string {
	if view.Status == "Completed" {
		return "done"
	}
	def, err := FetchPIMWorkflowDefinition(tenantID, view.Workflow)
	if err != nil {
		return view.CurrentStage
	}
	groups := pimStageGroups(def)
	for i, group := range groups {
		if group.Key == view.CurrentGroup {
			return fmt.Sprintf("%d of %d", i+1, len(groups))
		}
	}
	if view.CurrentGroup == "" {
		return fmt.Sprintf("0 of %d", len(groups))
	}
	return view.CurrentStage
}

// ---------------------------------------------------------------------------
// 36.2.5 - bulk workflow actions.
// ---------------------------------------------------------------------------

type PIMWorkflowBulkOutcome struct {
	Target  string `json:"target"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type PIMWorkflowBulkResult struct {
	Action    string                   `json:"action"`
	Requested int                      `json:"requested"`
	Succeeded int                      `json:"succeeded"`
	Failed    int                      `json:"failed"`
	Outcomes  []PIMWorkflowBulkOutcome `json:"outcomes"`
}

// StartPIMWorkflowForGroup starts one run per product in a product group. The
// group is resolved through the same seam bulk edit and export use, so "the
// Winter Launch group" means the same set of products to all three.
func StartPIMWorkflowForGroup(tenantID, actor, workflowCode, groupID string) (*PIMWorkflowBulkResult, error) {
	itemCodes, err := ResolvePIMProductGroupItemCodes(tenantID, groupID)
	if err != nil {
		return nil, err
	}
	if len(itemCodes) == 0 {
		return nil, fmt.Errorf("product group %q currently resolves to no products", groupID)
	}
	if len(itemCodes) > 500 {
		return nil, fmt.Errorf("starting a workflow is capped at 500 products per request (%d resolved)", len(itemCodes))
	}
	result := &PIMWorkflowBulkResult{Action: "start", Requested: len(itemCodes), Outcomes: []PIMWorkflowBulkOutcome{}}
	for _, itemCode := range itemCodes {
		runID, startErr := StartPIMWorkflowRun(tenantID, actor, workflowCode, itemCode)
		outcome := PIMWorkflowBulkOutcome{Target: itemCode, OK: startErr == nil, Message: runID}
		if startErr != nil {
			outcome.Error = startErr.Error()
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	LogAuditEvent(tenantID, actor, "PIM_WORKFLOW_BULK_START", "Success",
		fmt.Sprintf("workflow %s over group %s: %d started, %d refused", workflowCode, groupID, result.Succeeded, result.Failed))
	return result, nil
}

// BulkPIMWorkflowRunAction applies pause/resume/cancel/advance across a
// selection of runs, looping the single-run engine so every per-run guard still
// applies and a partially-applicable selection reports exactly which runs
// refused and why.
func BulkPIMWorkflowRunAction(tenantID, actor, action string, runIDs []string) (*PIMWorkflowBulkResult, error) {
	if len(runIDs) == 0 {
		return nil, fmt.Errorf("select at least one workflow run")
	}
	if len(runIDs) > 500 {
		return nil, fmt.Errorf("bulk workflow actions are capped at 500 runs per request (%d selected)", len(runIDs))
	}
	result := &PIMWorkflowBulkResult{Action: action, Requested: len(runIDs), Outcomes: []PIMWorkflowBulkOutcome{}}
	for _, runID := range runIDs {
		message, err := SetPIMWorkflowRunState(tenantID, actor, runID, action)
		outcome := PIMWorkflowBulkOutcome{Target: runID, OK: err == nil, Message: message}
		if err != nil {
			outcome.Error = err.Error()
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	LogAuditEvent(tenantID, actor, "PIM_WORKFLOW_BULK", "Success",
		fmt.Sprintf("bulk %s over %d run(s): %d ok, %d refused", action, result.Requested, result.Succeeded, result.Failed))
	return result, nil
}
