package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Ask 170 items 17, 20 and 26: positions and reporting lines, the disciplinary
// case's readers, and the manager's team scope.

const (
	// SettingManagerTeamScope (170.26): on, a manager sees the leave, attendance
	// and appraisals of their direct reports (Employee.manager) and approves
	// their leave only; off (the default), the scope is exactly as it was.
	SettingManagerTeamScope = "hr.manager_team_scope"
	// SettingExitClearanceRequired (170.19): on, an open offboarding checklist
	// holds the leaver's payslip posting.
	SettingExitClearanceRequired = "hr.exit_clearance_required"
	// SettingAnalyticsMinGroup (170.27): a group smaller than this is shown in
	// the combined "Other" row.
	SettingAnalyticsMinGroup = "hr.analytics_min_group"
	// SettingCertificateReminderDays (170.21): days before a certificate runs
	// out that its holder's HR is told, once. 0 = off.
	SettingCertificateReminderDays = "hr.certificate_reminder_days"

	DisciplinaryCaseDoctype = "DisciplinaryCase"
)

// teamScopeDoctypes are the records a manager sees for the team (170.26).
var teamScopeDoctypes = map[string]bool{"Leave": true, "Attendance": true, "Appraisal": true}

func init() {
	RegisterSetting(SettingDefinition{Key: SettingManagerTeamScope, Module: "HR", Label: "Managers see their own team",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a manager sees the leave, attendance and appraisals of the people who report to them (the Manager on the employee) and approves their leave only. Off: as before."})
	RegisterSetting(SettingDefinition{Key: SettingExitClearanceRequired, Module: "HR", Label: "Exit clearance before final pay",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a leaver's payslip cannot be posted while their offboarding checklist is open."})
	RegisterSetting(SettingDefinition{Key: SettingAnalyticsMinGroup, Module: "HR", Label: "Smallest group shown in people analytics",
		Type: SettingTypeInt, Default: "5", Unit: "people", Min: settingBound(1), Max: settingBound(100),
		Description: "A group of fewer people than this is shown in one combined Other row."})
	RegisterSetting(SettingDefinition{Key: SettingCertificateReminderDays, Module: "HR", Label: "Tell HR before a certificate expires",
		Type: SettingTypeInt, Default: "30", Unit: "days", Min: settingBound(0), Max: settingBound(365),
		Description: "Days before a training certificate's Valid until that HR is told, once. 0 = off."})
}

// --- scope helpers (170.20, 170.26) ----------------------------------------

func (c ScopeConstraint) candidates() []string {
	var out []string
	if c.Value != "" {
		out = append(out, c.Value)
	}
	for _, a := range c.Alt {
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

// Matches reports whether a document's scope value satisfies the constraint.
func (c ScopeConstraint) Matches(value string) bool {
	cands := c.candidates()
	if c.Contains {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			for _, cand := range cands {
				if part != "" && part == cand {
					return true
				}
			}
		}
		return false
	}
	for _, cand := range cands {
		if value == cand {
			return true
		}
	}
	return false
}

// NeedsMultiValue is true for a constraint a plain "= value" cannot express.
func (c ScopeConstraint) NeedsMultiValue() bool { return c.Contains || len(c.Alt) > 0 }

// SQLCondition renders the constraint for a list query: expr is the
// NULLIF(data->>'field',”) expression, firstArg the next $ number. The values
// are bound parameters, never interpolated.
func (c ScopeConstraint) SQLCondition(expr string, firstArg int) (string, []interface{}) {
	cands := c.candidates()
	if len(cands) == 0 {
		return "FALSE", nil
	}
	args := make([]interface{}, 0, len(cands))
	parts := make([]string, 0, len(cands))
	holders := make([]string, 0, len(cands))
	for i, v := range cands {
		args = append(args, v)
		n := firstArg + i
		holders = append(holders, "$"+strconv.Itoa(n))
		parts = append(parts, fmt.Sprintf("position(',' || $%d || ',' in ',' || replace(%s, ' ', '') || ',') > 0", n, expr))
	}
	var cond string
	if c.Contains {
		cond = "(" + strings.Join(parts, " OR ") + ")"
	} else {
		cond = expr + " IN (" + strings.Join(holders, ", ") + ")"
	}
	if c.AllowMissing {
		cond = "(" + cond + " OR " + expr + " IS NULL)"
	}
	return cond, args
}

// teamModeApplies: the team scope is on for this tenant, and the session is
// neither HR/admin nor holding the self scope globally by its role template.
func teamModeApplies(tenantID, doctype string, s SessionScope) bool {
	if !teamScopeDoctypes[doctype] || HasFullAccess(s.Role) || roleTemplateGrantsGlobalScope(s.Role, ScopeSelf) {
		return false
	}
	return GetSettingBool(tenantID, SettingManagerTeamScope)
}

// hrEmployeeByRef finds an Employee by document id or by its code.
func hrEmployeeByRef(schema, ref string) (string, map[string]interface{}, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, nil
	}
	var id, raw string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'Employee' AND deleted_at IS NULL
		AND (id = $1 OR data->>'code' = $1) ORDER BY (id = $1) DESC LIMIT 1`, schema), ref).Scan(&id, &raw)
	if err == sql.ErrNoRows {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return "", nil, err
	}
	return id, d, nil
}

// DirectReportCodes are the codes (and ids) of the Employees whose Manager is
// the given employee.
func DirectReportCodes(tenantID, ownCode string) ([]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	ownID, own, err := hrEmployeeByRef(schema, ownCode)
	if err != nil || own == nil {
		return nil, err
	}
	refs := []string{ownID}
	if code := strField(own, "code"); code != "" && code != ownID {
		refs = append(refs, code)
	}
	args := make([]interface{}, len(refs))
	holders := make([]string, len(refs))
	for i, r := range refs {
		args[i] = r
		holders[i] = "$" + strconv.Itoa(i+1)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', '') FROM %s.documents
		WHERE doctype = 'Employee' AND deleted_at IS NULL AND data->>'manager' IN (%s)`, schema, strings.Join(holders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, code string
		if err := rows.Scan(&id, &code); err != nil {
			return nil, err
		}
		out = append(out, id)
		if code != "" && code != id {
			out = append(out, code)
		}
	}
	return out, rows.Err()
}

// ApprovalTeamRefusal (170.26): with the team scope on, a manager decides only
// their own team's leave. HR and admins decide any.
func ApprovalTeamRefusal(tenantID, doctype string, data map[string]interface{}, actorUserID, actorRole string) error {
	base := SessionScope{Role: actorRole, UserID: actorUserID}
	if doctype != "Leave" || !teamModeApplies(tenantID, doctype, base) {
		return nil
	}
	session, err := ResolveSessionScope(tenantID, doctype, base)
	if err != nil {
		return err
	}
	constraints, _ := ScopeConstraints(doctype, session)
	if DocumentMatchesScope(constraints, data) != "" {
		return &ValidationError{Code: "HR-1730", Message: "this leave is not from your team"}
	}
	return nil
}

// --- validation hook (one call from ValidateTransactionalRules) ------------

// validateHRPeople runs the ask-170 HR rules for the doctypes it owns.
func validateHRPeople(tenantID, doctype, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	switch doctype {
	case "Position":
		return validatePositionSave(tenantID, docID, payload)
	case "Employee":
		return validateEmployeeManager(tenantID, docID, payload)
	case DisciplinaryCaseDoctype:
		return prepareDisciplinaryCase(tenantID, payload)
	case "Applicant":
		return validateApplicantSave(priorStatus, payload)
	case "HolidayCalendar":
		return validateHolidayCalendar(payload)
	case "Leave":
		return applyCalendarToLeave(tenantID, payload)
	case "Attendance":
		return applyCalendarToAttendance(tenantID, payload)
	}
	return nil
}

func validatePositionSave(tenantID, docID string, payload map[string]interface{}) error {
	up := strings.TrimSpace(strField(payload, "reports_to"))
	if up == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	self := map[string]bool{docID: true, strField(payload, "code"): true}
	delete(self, "")
	ref := up
	for i := 0; i < 50 && ref != ""; i++ {
		if self[ref] {
			return &ValidationError{Code: "HR-1700", Message: "a position cannot report to itself, directly or through others"}
		}
		var id, code, next string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'reports_to', '') FROM %s.documents
			WHERE doctype = 'Position' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), ref).Scan(&id, &code, &next)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if self[id] || (code != "" && self[code]) {
			return &ValidationError{Code: "HR-1700", Message: "a position cannot report to itself, directly or through others"}
		}
		ref = strings.TrimSpace(next)
	}
	return nil
}

func validateEmployeeManager(tenantID, docID string, payload map[string]interface{}) error {
	mgr := strings.TrimSpace(strField(payload, "manager"))
	if mgr == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	self := map[string]bool{docID: true, strField(payload, "code"): true}
	delete(self, "")
	ref := mgr
	for i := 0; i < 50 && ref != ""; i++ {
		id, doc, err := hrEmployeeByRef(schema, ref)
		if err != nil {
			return err
		}
		if self[ref] || self[id] || (doc != nil && self[strField(doc, "code")]) {
			return &ValidationError{Code: "HR-1701", Message: "a person cannot be their own manager, directly or through others"}
		}
		if doc == nil {
			return nil
		}
		ref = strings.TrimSpace(strField(doc, "manager"))
	}
	return nil
}

// prepareDisciplinaryCase keeps `readers`: the employee, the investigator and
// the people named, each as code and id, so the self scope reads them in.
func prepareDisciplinaryCase(tenantID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	refs := []string{strField(payload, "employee_id"), strField(payload, "investigator")}
	for _, p := range strings.Split(strField(payload, "named_people"), ",") {
		refs = append(refs, p)
	}
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(strings.ReplaceAll(v, ",", ""))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		add(ref)
		if id, doc, err := hrEmployeeByRef(schema, ref); err == nil && doc != nil {
			add(id)
			add(strField(doc, "code"))
		}
	}
	payload["readers"] = strings.Join(out, ",")
	return nil
}

var applicantStages = []string{"Applied", "Screening", "Interview", "Offer", "Hired", "Rejected"}

func validateApplicantSave(priorStatus string, payload map[string]interface{}) error {
	to := strings.TrimSpace(strField(payload, "status"))
	if to == "" {
		return nil
	}
	known := false
	for _, s := range applicantStages {
		if s == to {
			known = true
		}
	}
	if !known {
		return &ValidationError{Code: "HR-1702", Message: "the stage is not one of Applied, Screening, Interview, Offer, Hired, Rejected"}
	}
	if priorStatus == to {
		return nil
	}
	if priorStatus == "Hired" || priorStatus == "Rejected" {
		return &ValidationError{Code: "HR-1702", Message: "a hired or rejected applicant cannot change stage"}
	}
	if to == "Hired" {
		return &ValidationError{Code: "HR-1703", Message: "an applicant is Hired by accepting their offer"}
	}
	return nil
}
