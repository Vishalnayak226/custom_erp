package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Ask 170 item 170.32: project milestones and budget.
//
// A ProjectMilestone has a name, an amount (or a percent of the project's
// contract value), a due date and a status Planned -> Completed / Invoiced.
// CompleteProjectMilestone is the only way to complete one: a billable
// milestone raises a Draft SalesInvoice for the project's customer (carrying
// the project and the milestone), which then goes through the normal invoice
// posting. A replay finds the milestone done and returns its invoice, so one
// milestone never bills twice.
//
// A ProjectBudget lists amounts by cost type; the budget-vs-actual report sets
// them beside what the ledger holds for the project.

const (
	ProjectMilestoneDoctype = "ProjectMilestone"
	ProjectBudgetDoctype    = "ProjectBudget"
	projectCodeMilestone    = "FIN-1722"
	projectCodeBudget       = "FIN-1723"
)

func init() {
	RegisterReport(ReportDefinition{
		ID: "project-budget-vs-actual", Label: "Project budget vs actual", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "cost_type", Label: "Cost type"}, {Key: "budget", Label: "Budget", Sensitive: true},
			{Key: "actual", Label: "Actual", Sensitive: true}, {Key: "variance", Label: "Left", Sensitive: true},
		},
		Params: []ReportParam{{Key: "project", Label: "Project", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return ProjectBudgetVsActual(tenantID, params["project"])
		},
	})
}

// validateProjectMilestoneSave: a milestone is priced, belongs to a live
// project, and its status moves only by the complete action.
func validateProjectMilestoneSave(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	if priorData != nil && priorStatus != "" && priorStatus != "Planned" {
		return &ValidationError{Code: projectCodeMilestone, Message: "A completed milestone cannot be changed."}
	}
	if numFromInterface(payload["amount"]) <= 0 && numFromInterface(payload["percent"]) <= 0 {
		return &ValidationError{Code: projectCodeMilestone, Message: "Enter an amount or a percent."}
	}
	if p := numFromInterface(payload["percent"]); p < 0 || p > 100 {
		return &ValidationError{Code: projectCodeMilestone, Message: "Percent must be between 0 and 100."}
	}
	if _, _, err := fetchDocData(tenantID, "Project", strField(payload, "project")); err != nil {
		return &ValidationError{Code: projectCodeMilestone, Message: "Pick an existing project."}
	}
	return nil
}

// validateProjectBudgetSave: every line has a cost type and an amount.
func validateProjectBudgetSave(payload map[string]interface{}) error {
	rows, _ := payload["lines"].([]interface{})
	if len(rows) == 0 {
		return &ValidationError{Code: projectCodeBudget, Message: "Add at least one budget line."}
	}
	seen := map[string]bool{}
	for _, r := range rows {
		m, _ := r.(map[string]interface{})
		t := budgetCostType(strField(m, "cost_type"))
		if t == "" || numFromInterface(m["amount"]) < 0 {
			return &ValidationError{Code: projectCodeBudget, Message: "Each line needs a cost type (Labour, Materials, Expenses or Other) and an amount."}
		}
		if seen[t] {
			return &ValidationError{Code: projectCodeBudget, Message: "A cost type appears twice in the budget."}
		}
		seen[t] = true
	}
	return nil
}

func budgetCostType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "labour", "labor":
		return "Labour"
	case "materials", "material":
		return "Materials"
	case "expenses", "expense":
		return "Expenses"
	case "other":
		return "Other"
	}
	return ""
}

// milestoneAmount is the milestone's value in rupees: its own amount, else its
// percent of the project's contract value.
func milestoneAmount(ms, project map[string]interface{}) float64 {
	if a := numFromInterface(ms["amount"]); a > 0 {
		return a
	}
	return math.Round(numFromInterface(project["contract_value"])*numFromInterface(ms["percent"])) / 100
}

// CompleteProjectMilestone completes a milestone and, when billable, raises its
// Draft invoice. Returns the invoice id ("" when not billable).
func CompleteProjectMilestone(tenantID, id string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var raw, status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data::text, status FROM %s.documents WHERE doctype = 'ProjectMilestone' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), id).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return "", &ValidationError{Code: projectCodeMilestone, Message: "Milestone not found."}
	}
	if err != nil {
		return "", err
	}
	var ms map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &ms); err != nil {
		return "", err
	}
	if status != "Planned" { // replay: the milestone is done, hand back its invoice
		return strField(ms, "invoice_id"), nil
	}
	proj, _, err := fetchDocData(tenantID, "Project", strField(ms, "project"))
	if err != nil || proj == nil {
		return "", &ValidationError{Code: projectCodeMilestone, Message: "The project was not found."}
	}
	if strField(proj, "status") != "" && strField(proj, "status") != "Active" {
		return "", &ValidationError{Code: projectCodeMilestone, Message: "The project is not Active."}
	}
	amount := milestoneAmount(ms, proj)
	if amount <= 0 {
		return "", &ValidationError{Code: projectCodeMilestone, Message: "The milestone has no value. Set an amount, or a percent and the project's contract value."}
	}
	invoiceID := ""
	newStatus := "Completed"
	if strings.EqualFold(strField(ms, "billable"), "Yes") {
		customer := strField(proj, "customer")
		if customer == "" {
			return "", &ValidationError{Code: projectCodeMilestone, Message: "A billable milestone needs a customer on the project."}
		}
		invoiceID = NewDocID("INV")
		inv := map[string]interface{}{
			"id": invoiceID, "code": invoiceID,
			"invoice_number": IssueDocumentNumber(tenantID, "SalesInvoice", invoiceID, ""),
			"customer":       customer, "total_amount": amount, "project": strField(ms, "project"),
			"project_milestone_id": id, "status": "Draft",
		}
		b, _ := json.Marshal(inv)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')`, schema), invoiceID, b); err != nil {
			return "", err
		}
		newStatus = "Invoiced"
		ms["invoice_id"] = invoiceID
	}
	ms["status"] = newStatus
	ms["completed_at"] = BusinessToday()
	b, _ := json.Marshal(ms)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProjectMilestone' AND id = $3`, schema), b, newStatus, id); err != nil {
		return "", err
	}
	return invoiceID, tx.Commit()
}

// ProjectBudgetVsActual lists the budget and the ledger's actual cost per cost
// type for a project. Actual is the net debit on the project's expense rows.
func ProjectBudgetVsActual(tenantID, project string) ([]map[string]interface{}, error) {
	if strings.TrimSpace(project) == "" {
		return nil, &ValidationError{Code: projectCodeBudget, Message: "Pick a project."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	budget := map[string]float64{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE doctype = 'ProjectBudget' AND deleted_at IS NULL AND status = 'Active' AND data->>'project' = $1`, schema), project)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		lines, _ := d["lines"].([]interface{})
		for _, l := range lines {
			m, _ := l.(map[string]interface{})
			if t := budgetCostType(strField(m, "cost_type")); t != "" {
				budget[t] += numFromInterface(m["amount"])
			}
		}
	}
	rows.Close()
	actual := map[string]float64{}
	ar, err := db.DB.Query(fmt.Sprintf(`
		SELECT CASE WHEN g.account_code = '5450' THEN 'Labour'
		            WHEN g.account_code = '5400' THEN 'Expenses'
		            WHEN g.account_code LIKE '51%%' THEN 'Materials'
		            ELSE 'Other' END,
		       SUM(g.debit - g.credit)
		  FROM %s.gl_postings g JOIN %s.gl_accounts a ON a.account_code = g.account_code
		 WHERE g.project = $1 AND a.account_type = 'Expense'
		 GROUP BY 1`, schema, schema), project)
	if err != nil {
		return nil, err
	}
	for ar.Next() {
		var t string
		var v float64
		if ar.Scan(&t, &v) == nil {
			actual[t] = v / 100 // gl_postings hold paise
		}
	}
	ar.Close()
	out := []map[string]interface{}{}
	for _, t := range []string{"Labour", "Materials", "Expenses", "Other"} {
		if budget[t] == 0 && actual[t] == 0 {
			continue
		}
		out = append(out, map[string]interface{}{"cost_type": t, "budget": budget[t], "actual": actual[t], "variance": budget[t] - actual[t]})
	}
	return out, nil
}
