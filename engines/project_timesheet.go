package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Ask 170 item 170.31: timesheets cost the project.
//
// A Timesheet entry is one employee, project, task, date and number of hours.
// It is approved through the maker-checker engine. On approval the hours times
// the employee's cost rate (Employee.cost_rate, per hour) post to the project:
// Dr 5450 Project Labour Cost (carrying the project) / Cr 5460 Project Labour
// Absorbed (no project, so the project's P&L shows the cost, not a net zero).
// The posting key Timesheet:<id>:COST makes it once per entry however often the
// approval is replayed, and the rate and cost are stamped on the entry in the
// same transaction. An approved entry is fixed.

const (
	TimesheetDoctype            = "Timesheet"
	PostingEventProjectLabour   = "project_labour_cost"
	timesheetCodeInvalid        = "FIN-1720"
	timesheetCodeNoRate         = "FIN-1721"
	timesheetMaxHoursPerEntry   = 24.0
	timesheetDefaultCostAccount = "5450"
	timesheetDefaultAbsorbed    = "5460"
)

func init() {
	postingEvents = append(postingEvents, PostingEventProjectLabour)
	RegisterApprovalSubmitCheck(TimesheetDoctype, func(tenantID, docID string, data map[string]interface{}) error {
		_, err := timesheetCostRate(tenantID, data)
		return err
	})
	RegisterApprovalDecisionHook(TimesheetDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := CostTimesheet(tenantID, docID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "timesheet", fmt.Sprintf("timesheet %s approved but not costed: %v", docID, err), "")
		}
	})
}

// validateTimesheetSave runs on every Timesheet save.
func validateTimesheetSave(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	if docID != "" && priorData != nil && (priorStatus == "Approved" || strField(priorData, "cost_posted") != "") {
		return &ValidationError{Code: timesheetCodeInvalid, Message: "An approved timesheet is costed and cannot be changed."}
	}
	h := numFromInterface(payload["hours"])
	if h <= 0 || h > timesheetMaxHoursPerEntry {
		return &ValidationError{Code: timesheetCodeInvalid, Message: "Hours must be above 0 and at most 24."}
	}
	if _, err := time.Parse("2006-01-02", strField(payload, "work_date")); err != nil {
		return &ValidationError{Code: timesheetCodeInvalid, Message: "Enter the date worked."}
	}
	if strField(payload, "employee_id") == "" || strField(payload, "project") == "" {
		return &ValidationError{Code: timesheetCodeInvalid, Message: "A timesheet needs an employee and a project."}
	}
	if err := validateProjectReference(tenantID, strField(payload, "project")); err != nil {
		return &ValidationError{Code: timesheetCodeInvalid, Message: err.Error()}
	}
	return nil
}

// timesheetCostRate is the employee's hourly cost rate, or FIN-1721.
func timesheetCostRate(tenantID string, ts map[string]interface{}) (float64, error) {
	emp, _, err := fetchDocData(tenantID, "Employee", strField(ts, "employee_id"))
	if err != nil || emp == nil {
		return 0, &ValidationError{Code: timesheetCodeInvalid, Message: "The employee on the timesheet was not found."}
	}
	rate := numFromInterface(emp["cost_rate"])
	if rate <= 0 {
		return 0, &ValidationError{Code: timesheetCodeNoRate, Message: "The employee has no cost rate. Set one on the employee first."}
	}
	return rate, nil
}

// CostTimesheet posts an approved timesheet's cost to its project, once.
func CostTimesheet(tenantID, id string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data::text, status FROM %s.documents WHERE doctype = 'Timesheet' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), id).Scan(&raw, &status); err != nil {
		return fmt.Errorf("timesheet not found: %v", err)
	}
	if status != "Approved" {
		return &ValidationError{Code: timesheetCodeInvalid, Message: "Only an approved timesheet is costed."}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	if strField(data, "cost_posted") != "" {
		return nil // already costed: replay posts nothing
	}
	rate, err := timesheetCostRate(tenantID, data)
	if err != nil {
		return err
	}
	paise := int64(math.Round(numFromInterface(data["hours"]) * rate * 100))
	if paise <= 0 {
		return &ValidationError{Code: timesheetCodeInvalid, Message: "The timesheet cost is zero."}
	}
	workDate := strField(data, "work_date")
	on, _ := time.Parse("2006-01-02", workDate)
	res := ResolvePostingAccounts(tenantID, PostingEventProjectLabour, PostingContext{On: on}, timesheetDefaultCostAccount, timesheetDefaultAbsorbed)
	project := strField(data, "project")
	if err := PostDoubleEntryTx(tx, tenantID, schema, TimesheetDoctype, id,
		map[string]int64{res.Debit: paise}, map[string]int64{res.Credit: paise}, workDate, fmt.Sprintf("Timesheet:%s:COST", id),
		PostingOptions{Project: project, Lines: []PostingLine{
			{Account: res.Debit, Debit: paise},
			{Account: res.Credit, Credit: paise, NoProject: true},
		}}); err != nil {
		return fmt.Errorf("failed to post timesheet cost: %v", err)
	}
	data["cost_rate"] = rate
	data["cost_amount"] = PaiseToRupees(paise)
	data["cost_posted"] = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Timesheet' AND id = $2`, schema), b, id); err != nil {
		return err
	}
	return tx.Commit()
}
