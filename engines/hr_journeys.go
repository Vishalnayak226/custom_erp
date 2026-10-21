package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 55.14: the guided onboarding, appraisal, training and grievance
// screens (public/view-hr.js) are built on the doctypes' own fields and the
// generic document API. Two things those cannot give honestly, and this file
// does:
//
//  1. Ticking an onboarding/offboarding step with WHO ticked it and WHEN,
//     stamped here from the session - a done_by the browser writes is only a
//     claim. The checklist's status follows its steps (all done = Completed).
//  2. The statement of who can read a grievance, derived from the very policy
//     the server enforces (sensitive_fields.go's field rules, scope_policy.go's
//     self scope, the Grievance approval rule), so the label a user reads
//     cannot drift from what actually happens to the record.

const onboardingDoctype = "OnboardingChecklist"

// OnboardingProgress is a checklist after a step changed.
type OnboardingProgress struct {
	ID     string                   `json:"id"`
	Items  []map[string]interface{} `json:"items"`
	Done   int                      `json:"done"`
	Total  int                      `json:"total"`
	Status string                   `json:"status"`
}

// ParseOnboardingSteps reads OnboardingChecklist.items: JSON text in a Data
// field ([{task, done}], the doctype's seeded convention), or an array
// already decoded. Every key of a step is kept, so a step written by the
// generic form or an import loses nothing when one of its siblings is ticked.
func ParseOnboardingSteps(raw interface{}) ([]map[string]interface{}, error) {
	var list []interface{}
	switch v := raw.(type) {
	case nil:
		return []map[string]interface{}{}, nil
	case []interface{}:
		list = v
	case string:
		if strings.TrimSpace(v) == "" {
			return []map[string]interface{}{}, nil
		}
		if err := json.Unmarshal([]byte(v), &list); err != nil {
			return nil, &ValidationError{Code: "HR-0532", Message: "this checklist's steps are not a step list ([{task, done}])"}
		}
	default:
		return nil, &ValidationError{Code: "HR-0532", Message: "this checklist's steps are not a step list ([{task, done}])"}
	}
	steps := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		step, ok := item.(map[string]interface{})
		if !ok {
			return nil, &ValidationError{Code: "HR-0532", Message: "this checklist's steps are not a step list ([{task, done}])"}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func onboardingStepDone(step map[string]interface{}) bool {
	switch v := step["done"].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	return false
}

// SetOnboardingStep ticks (done=true) or unticks step index of a checklist.
// actor is the username recorded as done_by. The session must be able to read
// the checklist under the doctype's own scope rule (an employee's own, or
// every one for a role with HR's self-scope exemption); the caller checks the
// role's update permission on the doctype, as for any document write.
func SetOnboardingStep(tenantID, checklistID string, index int, done bool, actor string, session SessionScope) (*OnboardingProgress, error) {
	session, err := ResolveSessionScope(tenantID, onboardingDoctype, session)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var dataStr string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema), onboardingDoctype, checklistID).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "HR-0531", Message: "no onboarding or offboarding checklist has this id"}
	}
	if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	if constraints, known := ScopeConstraints(onboardingDoctype, session); known {
		if failed := DocumentMatchesScope(constraints, data); failed != "" {
			// The same answer the document API gives for a record out of scope.
			return nil, &ValidationError{Code: "GLOBAL-0011", Message: "this checklist is not yours to change"}
		}
	}
	steps, err := ParseOnboardingSteps(data["items"])
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(steps) {
		return nil, &ValidationError{Code: "HR-0533", Message: fmt.Sprintf("this checklist has %d steps - step %d does not exist", len(steps), index+1)}
	}
	step := steps[index]
	step["done"] = done
	if done {
		step["done_by"] = actor
		step["done_at"] = time.Now().UTC().Format(time.RFC3339)
	} else {
		delete(step, "done_by")
		delete(step, "done_at")
	}
	doneCount := 0
	for _, s := range steps {
		if onboardingStepDone(s) {
			doneCount++
		}
	}
	status := "In Progress"
	if len(steps) > 0 && doneCount == len(steps) {
		status = "Completed"
	}
	// Written back as JSON text: the field is a Data field, and every other
	// writer (the generic form, imports) stores it that way.
	itemsJSON, err := json.Marshal(steps)
	if err != nil {
		return nil, err
	}
	data["items"] = string(itemsJSON)
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP, version = version + 1
		WHERE doctype = $3 AND id = $4`, schema), marshaled, status, onboardingDoctype, checklistID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	action := "Undone"
	if done {
		action = "Done"
	}
	task := fmt.Sprintf("%v", step["task"])
	LogAuditEvent(tenantID, session.UserID, "ONBOARDING_STEP", action, fmt.Sprintf("%s step %d (%s)", checklistID, index+1, task))
	return &OnboardingProgress{ID: checklistID, Items: steps, Done: doneCount, Total: len(steps), Status: status}, nil
}

// GrievanceAccess is who can read a grievance, from the enforced policy.
type GrievanceAccess struct {
	// ContentReaders read what a grievance says and how it was resolved
	// (description, resolution_notes).
	ContentReaders []string `json:"content_readers"`
	// CaseListRoles list every employee's grievances - number, employee,
	// category, status - without being content readers.
	CaseListRoles []string `json:"case_list_roles"`
	// ReviewerRole is the Grievance approval rule's role ("" if none): who
	// decides a case besides Super Admin.
	ReviewerRole string             `json:"reviewer_role"`
	You          GrievanceAccessYou `json:"you"`
}

// GrievanceAccessYou is what this session itself can do.
type GrievanceAccessYou struct {
	Role        string `json:"role"`
	AllCases    bool   `json:"all_cases"`
	ReadContent bool   `json:"read_content"`
	WriteNotes  bool   `json:"write_notes"`
	Decide      bool   `json:"decide"`
}

// GetGrievanceAccess answers "who can read a grievance" for this tenant and
// role. Tenant field_permissions rows can only narrow the code policy, so the
// session's own answer (You) reads the merged permissions, while the role
// lists name the policy's own grants.
func GetGrievanceAccess(tenantID, role string) (*GrievanceAccess, error) {
	out := &GrievanceAccess{ContentReaders: []string{RoleSuperAdmin, RoleAdmin}, CaseListRoles: []string{}}
	if rule, ok := sensitiveFields["Grievance"]["description"]; ok {
		for _, r := range rule.ReadRoles {
			if r != anySensitiveWriter && !HasFullAccess(r) {
				out.ContentReaders = append(out.ContentReaders, r)
			}
		}
	}
	for r, doctypes := range selfScopeExemptions {
		if doctypes["Grievance"] && !HasFullAccess(r) && !containsRole(out.ContentReaders, r) {
			out.CaseListRoles = append(out.CaseListRoles, r)
		}
	}
	sort.Strings(out.CaseListRoles)
	reviewer, _, err := LowestApproverRoleForDoctype(tenantID, "Grievance")
	if err != nil {
		return nil, err
	}
	out.ReviewerRole = CanonicalRole(reviewer)

	perms, err := fieldPermissions(tenantID, role, "Grievance")
	if err != nil {
		return nil, err
	}
	readable := func(field string) bool { p, ok := perms[field]; return !ok || p.AllowRead }
	writable := func(field string) bool { p, ok := perms[field]; return !ok || p.AllowWrite }
	session := SessionScope{Role: role}
	out.You = GrievanceAccessYou{
		Role:        role,
		AllCases:    session.HasGlobalScope(ScopeSelf, "Grievance"),
		ReadContent: readable("description") && readable("resolution_notes"),
		WriteNotes:  writable("resolution_notes"),
		// DecideApproval's own rule; a grievance carries no location, so its
		// location check never applies.
		Decide: HasFullAccess(role) || (reviewer != "" && role == reviewer),
	}
	return out, nil
}

func containsRole(list []string, role string) bool {
	for _, r := range list {
		if normalizeRoleKey(r) == normalizeRoleKey(role) {
			return true
		}
	}
	return false
}
