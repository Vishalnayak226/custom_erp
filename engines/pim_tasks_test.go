package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Stage 36.2 - task and workflow engine tests.
//
// Every assertion here is scoped to its own uniquely-named fixtures. This suite
// shares one database with every other package test, so a count over "all
// PIMTask rows" would be a claim about other tests' data as much as its own -
// the same lesson 35.3.7's sweeper test recorded.
// ---------------------------------------------------------------------------

// pimTestFixture creates an Item to hang tasks off and returns a cleanup that
// removes every document this test made, whatever the test's outcome.
func pimTestFixture(t *testing.T, prefix string) (string, string, func()) {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("resolve default tenant schema: %v", err)
	}
	itemID := prefix + "-ITEM"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype IN ('PIMTask','PIMWorkflowRun') AND (data->>'item_code' = $1 OR data->>'scope_ref' = $1)", itemID)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE '" + prefix + "%'")
	}
	cleanup()

	encoded, _ := json.Marshal(map[string]interface{}{"code": itemID, "name": prefix + " fixture", "family": ""})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		itemID, encoded); err != nil {
		t.Fatalf("insert fixture item: %v", err)
	}
	return schema, itemID, cleanup
}

func pimInsertDoc(t *testing.T, schema, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')",
		id, doctype, encoded, status); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func TestPIMTaskLifecycle(t *testing.T) {
	_, itemID, cleanup := pimTestFixture(t, "PIMTASK-LIFE")
	defer cleanup()

	taskID, err := CreatePIMTask("default", "tester", PIMTaskRequest{
		Title: "Enrich the fixture", ItemCode: itemID, TaskType: "Enrichment",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// A task created with only an item_code must infer Product scope rather
	// than making the caller say the same thing twice.
	listed, err := ListPIMTasks("default", PIMTaskFilter{TaskID: taskID})
	if err != nil {
		t.Fatalf("list task: %v", err)
	}
	if len(listed.Tasks) != 1 {
		t.Fatalf("expected exactly the one task, got %d", len(listed.Tasks))
	}
	task := listed.Tasks[0]
	if task.ScopeType != "Product" || task.ScopeRef != itemID {
		t.Errorf("scope = %s/%s, want Product/%s", task.ScopeType, task.ScopeRef, itemID)
	}
	if task.Status != "Open" || task.Priority != "Normal" {
		t.Errorf("new task = %s/%s, want Open/Normal", task.Status, task.Priority)
	}

	// The state machine: Open -> In Progress -> Done is legal.
	for _, next := range []string{"In Progress", "Done"} {
		if err := SetPIMTaskStatus("default", "tester", taskID, next); err != nil {
			t.Fatalf("move task to %s: %v", next, err)
		}
	}

	// Done is terminal, and the refusal must say what to do instead - a bare
	// "not allowed" would leave the operator with no next step.
	err = SetPIMTaskStatus("default", "tester", taskID, "Open")
	if err == nil {
		t.Fatal("a Done task was re-opened; Done must be terminal")
	}
	if !strings.Contains(err.Error(), "follow-up") {
		t.Errorf("refusal %q does not point at the follow-up path", err)
	}

	// completed_at / completed_by are what the audit question "who closed
	// this?" is answered from, so they must actually be written.
	listed, _ = ListPIMTasks("default", PIMTaskFilter{TaskID: taskID})
	if listed.Tasks[0].CompletedBy != "tester" || listed.Tasks[0].CompletedAt == "" {
		t.Errorf("completion not stamped: by=%q at=%q", listed.Tasks[0].CompletedBy, listed.Tasks[0].CompletedAt)
	}

	followUpID, err := CreatePIMFollowUpTask("default", "tester", taskID, "the images are still wrong")
	if err != nil {
		t.Fatalf("create follow-up: %v", err)
	}
	followUps, _ := ListPIMTasks("default", PIMTaskFilter{TaskID: followUpID})
	if len(followUps.Tasks) != 1 || followUps.Tasks[0].ItemCode != itemID {
		t.Fatalf("follow-up did not inherit the original's product")
	}
	if !strings.Contains(followUps.Tasks[0].Instructions, taskID) {
		t.Errorf("follow-up instructions %q do not reference the original task", followUps.Tasks[0].Instructions)
	}

	// Comments are append-only and carry their author.
	if err := AddPIMTaskComment("default", "tester", followUpID, "picked this up"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if err := AddPIMTaskComment("default", "other", followUpID, "handing over"); err != nil {
		t.Fatalf("add second comment: %v", err)
	}
	followUps, _ = ListPIMTasks("default", PIMTaskFilter{TaskID: followUpID})
	comments := followUps.Tasks[0].Comments
	if len(comments) != 2 || comments[0].Author != "tester" || comments[1].Comment != "handing over" {
		t.Errorf("comment thread = %#v, want both comments in order", comments)
	}
	if err := AddPIMTaskComment("default", "tester", followUpID, "   "); err == nil {
		t.Error("an empty comment was accepted")
	}
}

func TestPIMTaskValidationRefusals(t *testing.T) {
	db.InitDB(testConnStr())

	if err := ValidatePIMTaskDocument("default", map[string]interface{}{"status": "Nearly Done"}); err == nil {
		t.Error("an unknown task status was accepted")
	}
	if err := ValidatePIMTaskDocument("default", map[string]interface{}{"due_date": "31/12/2026"}); err == nil {
		t.Error("a non-ISO due date was accepted")
	}
	if err := ValidatePIMTaskDocument("default", map[string]interface{}{"scope_type": "Product Group"}); err == nil {
		t.Error("a scoped task with no scope reference was accepted")
	}
	// An assignee nobody can be is the silent failure this check exists for:
	// the task would sit in an inbox that no session ever opens.
	if err := ValidatePIMTaskDocument("default", map[string]interface{}{
		"status": "Open", "assignee": "definitely-not-a-real-user-36-2",
	}); err == nil {
		t.Error("a task assigned to a non-existent user was accepted")
	}

	// Template title patterns: an unknown placeholder must be caught at author
	// time, not discovered as literal text in a hundred generated titles.
	if err := ValidatePIMTaskTemplateDocument("default", map[string]interface{}{"title_pattern": "Fix {item-code}"}); err == nil {
		t.Error("an unknown title placeholder was accepted")
	}
	if err := ValidatePIMTaskTemplateDocument("default", map[string]interface{}{"title_pattern": "Fix {item_code"}); err == nil {
		t.Error("an unclosed title placeholder was accepted")
	}
	if err := ValidatePIMTaskTemplateDocument("default", map[string]interface{}{"title_pattern": "Enrich {item_name} ({family})"}); err != nil {
		t.Errorf("a valid title pattern was refused: %v", err)
	}
	if err := ValidatePIMTaskTemplateDocument("default", map[string]interface{}{"due_in_days": "not a number"}); err == nil {
		t.Error("a non-numeric due_in_days was accepted")
	}
}

func TestPIMTaskTemplateInstantiationSkipsDuplicates(t *testing.T) {
	schema, itemID, cleanup := pimTestFixture(t, "PIMTASK-TPL")
	defer cleanup()

	const templateID = "PIMTASK-TPL-TEMPLATE"
	const groupID = "PIMTASK-TPL-GROUP"
	pimInsertDoc(t, schema, templateID, "PIMTaskTemplate", "Active", map[string]interface{}{
		"code": templateID, "name": "Enrichment sweep", "task_type": "Enrichment",
		"title_pattern": "Enrich {item_code}", "due_in_days": 7, "priority": "High", "status": "Active",
	})
	pimInsertDoc(t, schema, groupID, "PIMProductGroup", "Active", map[string]interface{}{
		"code": groupID, "name": "Template fixture group", "group_type": "Static",
		"members": fmt.Sprintf(`[{"item_code":%q}]`, itemID), "status": "Active",
	})

	first, err := InstantiatePIMTaskTemplate("default", "tester", templateID, groupID)
	if err != nil {
		t.Fatalf("instantiate template: %v", err)
	}
	if first.CreatedCount != 1 || first.SkippedCount != 0 {
		t.Fatalf("first run created %d skipped %d, want 1/0", first.CreatedCount, first.SkippedCount)
	}

	created, _ := ListPIMTasks("default", PIMTaskFilter{TaskID: first.Created[0]})
	task := created.Tasks[0]
	if task.Title != "Enrich "+itemID {
		t.Errorf("title pattern not rendered: %q", task.Title)
	}
	if task.Priority != "High" || task.ScopeType != "Product Group" || task.ScopeRef != groupID {
		t.Errorf("template fields not carried through: %#v", task)
	}
	// due_in_days is relative, so a template reused next month still produces a
	// future due date rather than one in the past.
	wantDue := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	if task.DueDate != wantDue {
		t.Errorf("due date = %q, want %q", task.DueDate, wantDue)
	}

	// Re-running is normal (a dynamic group picks up new products), so the
	// second run must skip rather than duplicate.
	second, err := InstantiatePIMTaskTemplate("default", "tester", templateID, groupID)
	if err != nil {
		t.Fatalf("re-instantiate template: %v", err)
	}
	if second.CreatedCount != 0 || second.SkippedCount != 1 {
		t.Fatalf("second run created %d skipped %d, want 0/1", second.CreatedCount, second.SkippedCount)
	}

	// Once the first task is closed the product is eligible again - the skip is
	// "already has OPEN work", not "has ever had work".
	if err := SetPIMTaskStatus("default", "tester", first.Created[0], "Done"); err != nil {
		t.Fatalf("close first task: %v", err)
	}
	third, err := InstantiatePIMTaskTemplate("default", "tester", templateID, groupID)
	if err != nil {
		t.Fatalf("third instantiation: %v", err)
	}
	if third.CreatedCount != 1 {
		t.Errorf("a closed task should free the product for a new one; created %d", third.CreatedCount)
	}
}

func TestPIMWorkflowDefinitionValidation(t *testing.T) {
	db.InitDB(testConnStr())

	base := func(stages string) map[string]interface{} {
		return map[string]interface{}{"code": "WF", "name": "WF", "stages": stages, "status": "Active"}
	}
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(`[]`)); err == nil {
		t.Error("a workflow with no stages was accepted")
	}
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(
		`[{"stage_code":"a","label":"A","sequence":1},{"stage_code":"a","label":"A2","sequence":2}]`)); err == nil {
		t.Error("duplicate stage codes were accepted - a run tracks position by stage code")
	}
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(
		`[{"stage_code":"a","label":"A","sequence":1},{"stage_code":"b","label":"B","sequence":1}]`)); err == nil {
		t.Error("two sequential stages sharing a sequence were accepted")
	}
	// The same clash inside one parallel group is the legitimate way to say
	// "these run together".
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(
		`[{"stage_code":"a","label":"A","sequence":1,"parallel_group":"g"},{"stage_code":"b","label":"B","sequence":1,"parallel_group":"g"}]`)); err != nil {
		t.Errorf("a legitimate parallel group was refused: %v", err)
	}
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(
		`[{"stage_code":"a","label":"A","sequence":1,"exit_condition":"vibes_are_good"}]`)); err == nil {
		t.Error("an unknown exit condition was accepted - the run would never advance")
	}
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(
		`[{"stage_code":"a","label":"A","sequence":1,"exit_condition":"completeness_at_least"}]`)); err == nil {
		t.Error("a value-taking condition was accepted with no value")
	}
	if err := ValidatePIMWorkflowDefinitionDocument("default", base(
		`[{"stage_code":"a","label":"A","sequence":1,"exit_condition":"completeness_at_least","exit_value":"80"}]`)); err != nil {
		t.Errorf("a valid condition was refused: %v", err)
	}
	// The condition vocabulary the form offers must be exactly what the engine
	// implements, or a screen can offer a condition that never evaluates.
	for _, info := range ListPIMWorkflowConditions() {
		if _, known := pimWorkflowConditions[info.Key]; !known {
			t.Errorf("published condition %q has no implementation", info.Key)
		}
	}
}

func TestPIMWorkflowRunAdvancesAndBlocks(t *testing.T) {
	schema, itemID, cleanup := pimTestFixture(t, "PIMWF-RUN")
	defer cleanup()

	const templateID = "PIMWF-RUN-TEMPLATE"
	const workflowID = "PIMWF-RUN-WORKFLOW"
	pimInsertDoc(t, schema, templateID, "PIMTaskTemplate", "Active", map[string]interface{}{
		"code": templateID, "name": "Stage work", "task_type": "Enrichment",
		"title_pattern": "Do {item_code}", "status": "Active",
	})
	// Three stages: one that creates work, one that waits on a condition the
	// fixture cannot satisfy, and one after it. The middle stage is what proves
	// a run stops with a reason rather than silently skipping ahead.
	pimInsertDoc(t, schema, workflowID, "PIMWorkflowDefinition", "Active", map[string]interface{}{
		"code": workflowID, "name": "Fixture workflow", "status": "Active",
		"stages": fmt.Sprintf(`[
			{"stage_code":"enrich","label":"Enrich","sequence":1,"task_template":%q},
			{"stage_code":"imagery","label":"Imagery","sequence":2,"entry_condition":"has_main_image"},
			{"stage_code":"final","label":"Final","sequence":3}
		]`, templateID),
	})

	runID, err := StartPIMWorkflowRun("default", "tester", workflowID, itemID)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}

	// Starting must immediately enter stage 1 and create its task - a workflow
	// whose first stage appears only after some later trigger looks broken.
	runs, err := ListPIMWorkflowRuns("default", "", itemID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one run for the fixture item, got %d", len(runs))
	}
	if runs[0].CurrentStage != "enrich" {
		t.Fatalf("run is at %q, want the first stage", runs[0].CurrentStage)
	}
	if runs[0].OpenTasks != 1 {
		t.Fatalf("stage 1 created %d open task(s), want 1", runs[0].OpenTasks)
	}

	// A second live run over the same (workflow, product) is refused by the
	// partial unique index rather than left to a read-then-write race.
	if _, err := StartPIMWorkflowRun("default", "tester", workflowID, itemID); err == nil {
		t.Error("a duplicate live run was allowed")
	}

	// While the stage's task is open the run must not move.
	message, err := AdvancePIMWorkflowRun("default", "tester", runID)
	if err != nil {
		t.Fatalf("advance with open task: %v", err)
	}
	if !strings.Contains(message, "not finished") {
		t.Errorf("advance said %q, want an explanation that the stage is unfinished", message)
	}

	stageTasks, _ := ListPIMTasks("default", PIMTaskFilter{WorkflowRun: runID, OnlyOpen: true})
	if len(stageTasks.Tasks) != 1 {
		t.Fatalf("expected one open stage task, got %d", len(stageTasks.Tasks))
	}
	// Completing the stage's last task is what advances the run - not a
	// sweeper, and not a second operator action.
	if err := SetPIMTaskStatus("default", "tester", stageTasks.Tasks[0].ID, "Done"); err != nil {
		t.Fatalf("complete stage task: %v", err)
	}

	runs, _ = ListPIMWorkflowRuns("default", "", itemID, 10)
	// The fixture item has no Main Image, so the run must be held at stage 1
	// with imagery's entry condition named as the reason - never stranded
	// between two stages, and never silently skipped past.
	if runs[0].CurrentStage != "enrich" {
		t.Errorf("run left stage 1 despite a failing entry condition; now at %q", runs[0].CurrentStage)
	}
	if !strings.Contains(runs[0].BlockedReason, "imagery") || !strings.Contains(runs[0].BlockedReason, "Main Image") {
		t.Errorf("blocked reason %q does not name the stage and the missing thing", runs[0].BlockedReason)
	}

	// Satisfy the condition and the same idempotent advance carries on. The
	// stage after imagery has no template, so a single advance should walk
	// through it and finish the run rather than needing a press per stage.
	pimInsertDoc(t, schema, "PIMWF-RUN-MEDIA", "ProductMedia", "Active", map[string]interface{}{
		"item": itemID, "media_role": "Main Image", "checksum": "PIMWF-RUN-FIXTURE",
	})
	if _, err := AdvancePIMWorkflowRun("default", "tester", runID); err != nil {
		t.Fatalf("advance after satisfying the condition: %v", err)
	}
	runs, _ = ListPIMWorkflowRuns("default", "", itemID, 10)
	if runs[0].Status != "Completed" {
		t.Errorf("run status = %q with stage %q, want Completed", runs[0].Status, runs[0].CurrentStage)
	}
	if runs[0].CompletedAt == "" {
		t.Error("a completed run has no completed_at")
	}

	// The activity log is the run's own history and must have recorded each
	// transition, not just the final state.
	events := map[string]bool{}
	for _, entry := range runs[0].Activity {
		events[entry.Event] = true
	}
	for _, want := range []string{"started", "advanced", "blocked", "completed"} {
		if !events[want] {
			t.Errorf("activity log is missing a %q entry: %#v", want, runs[0].Activity)
		}
	}
}

func TestPIMWorkflowPauseResumeCancel(t *testing.T) {
	schema, itemID, cleanup := pimTestFixture(t, "PIMWF-STATE")
	defer cleanup()

	const templateID = "PIMWF-STATE-TEMPLATE"
	const workflowID = "PIMWF-STATE-WORKFLOW"
	pimInsertDoc(t, schema, templateID, "PIMTaskTemplate", "Active", map[string]interface{}{
		"code": templateID, "name": "Stage work", "title_pattern": "Do {item_code}", "status": "Active",
	})
	pimInsertDoc(t, schema, workflowID, "PIMWorkflowDefinition", "Active", map[string]interface{}{
		"code": workflowID, "name": "State fixture", "status": "Active",
		"stages": fmt.Sprintf(`[{"stage_code":"one","label":"One","sequence":1,"task_template":%q}]`, templateID),
	})

	runID, err := StartPIMWorkflowRun("default", "tester", workflowID, itemID)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if _, err := SetPIMWorkflowRunState("default", "tester", runID, "pause"); err != nil {
		t.Fatalf("pause run: %v", err)
	}
	// A paused run must refuse to advance - that is the whole point of pausing.
	if _, err := AdvancePIMWorkflowRun("default", "tester", runID); err == nil {
		t.Error("a paused run advanced")
	}
	if _, err := SetPIMWorkflowRunState("default", "tester", runID, "pause"); err == nil {
		t.Error("an already-paused run was paused again")
	}
	if _, err := SetPIMWorkflowRunState("default", "tester", runID, "resume"); err != nil {
		t.Fatalf("resume run: %v", err)
	}

	// Cancelling must take the run's open tasks with it, or work stays in
	// people's inboxes for a product nobody is progressing.
	before, _ := ListPIMTasks("default", PIMTaskFilter{WorkflowRun: runID, OnlyOpen: true})
	if len(before.Tasks) == 0 {
		t.Fatal("fixture produced no open task to cancel")
	}
	if _, err := SetPIMWorkflowRunState("default", "tester", runID, "cancel"); err != nil {
		t.Fatalf("cancel run: %v", err)
	}
	after, _ := ListPIMTasks("default", PIMTaskFilter{WorkflowRun: runID, OnlyOpen: true})
	if len(after.Tasks) != 0 {
		t.Errorf("%d task(s) still open after the run was cancelled", len(after.Tasks))
	}
	if _, err := SetPIMWorkflowRunState("default", "tester", runID, "resume"); err == nil {
		t.Error("a cancelled run was resumed")
	}
}

func TestPIMBulkTaskActionReportsPerTaskOutcomes(t *testing.T) {
	_, itemID, cleanup := pimTestFixture(t, "PIMTASK-BULK")
	defer cleanup()

	open, err := CreatePIMTask("default", "tester", PIMTaskRequest{Title: "Open one", ItemCode: itemID})
	if err != nil {
		t.Fatalf("create open task: %v", err)
	}
	closed, err := CreatePIMTask("default", "tester", PIMTaskRequest{Title: "Closed one", ItemCode: itemID})
	if err != nil {
		t.Fatalf("create second task: %v", err)
	}
	if err := SetPIMTaskStatus("default", "tester", closed, "Done"); err != nil {
		t.Fatalf("close second task: %v", err)
	}

	// A mixed selection is the normal case, and it must report exactly which
	// rows refused rather than failing or silently succeeding as a whole.
	result, err := BulkPIMTaskAction("default", "tester", "status", []string{open, closed}, "In Progress")
	if err != nil {
		t.Fatalf("bulk status: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 1 {
		t.Fatalf("bulk result = %d ok / %d failed, want 1/1", result.Succeeded, result.Failed)
	}
	for _, outcome := range result.Outcomes {
		if outcome.TaskID == closed && (outcome.OK || outcome.Error == "") {
			t.Errorf("the Done task should have refused with a reason: %#v", outcome)
		}
		if outcome.TaskID == open && !outcome.OK {
			t.Errorf("the open task should have succeeded: %#v", outcome)
		}
	}

	if _, err := BulkPIMTaskAction("default", "tester", "teleport", []string{open}, ""); err == nil {
		t.Error("an unknown bulk action was accepted")
	}
	if _, err := BulkPIMTaskAction("default", "tester", "status", nil, "Done"); err == nil {
		t.Error("an empty selection was accepted")
	}
	// A group and an explicit selection together leave "what did I just
	// change?" ambiguous, so they are refused rather than merged.
	if _, err := ResolvePIMTaskTargets("default", "some-group", []string{open}); err == nil {
		t.Error("a group plus an explicit selection was accepted")
	}
}
