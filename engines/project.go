package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
)

// Stage 37.7: Projects & job costing. Pre-build audit found no Project/Job
// concept anywhere, and confirmed the codebase's own conventions point to
// "Project" as a 4th whole-posting dimension (the CostCenter/Department
// (26.6.8)/Entity (37.2.1) precedent), not a WIP/running-cost mechanism -
// every cost-incurring doctype here posts immediately on approval/payment,
// so there is no pre-GL accumulation stage to attach a running ledger to
// (unlike item_cost, Stage 37.3, which exists because inventory valuation
// genuinely needs one). Timesheets/labour-hours-to-project costing would
// need a new Timesheet doctype plus employee hourly-rate data HR does not
// have today - a real, separate, larger feature, explicitly out of scope.

// validateProjectReferenceInSchema/validateProjectReference mirror
// validateCostCenterReferenceInSchema's exact shape (engines/gl_cost_center.go).
func validateProjectReferenceInSchema(schema, project string) error {
	if project == "" {
		return nil
	}
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Project' AND id = $1 AND deleted_at IS NULL`, schema),
		project).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("project '%s' is not a registered Project", project)
	}
	if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("project '%s' is not Active", project)
	}
	return nil
}

func validateProjectReference(tenantID, project string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return validateProjectReferenceInSchema(schema, project)
}

// GetProjectPL is GetCostCenterPL's exact shape (engines/gl_cost_center.go)
// for the Project dimension - job profitability, revenue minus expense,
// grouped by project.
func GetProjectPL(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(p.project, 'Unassigned') AS project, a.account_type,
		       CASE WHEN a.account_type = 'Revenue' THEN COALESCE(SUM(p.credit), 0) - COALESCE(SUM(p.debit), 0)
		            ELSE COALESCE(SUM(p.debit), 0) - COALESCE(SUM(p.credit), 0) END AS amount
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE a.account_type IN ('Revenue', 'Expense') AND p.created_at >= $1::date AND p.created_at < ($2::date + 1)
		GROUP BY COALESCE(p.project, 'Unassigned'), a.account_type
		ORDER BY project, a.account_type`, schema, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[string]float64{}
	var out []map[string]interface{}
	for rows.Next() {
		var project, accountType string
		var amountPaise int64
		if err := rows.Scan(&project, &accountType, &amountPaise); err != nil {
			return nil, err
		}
		amount := PaiseToRupees(amountPaise)
		if accountType == "Revenue" {
			totals[project] += amount
		} else {
			totals[project] -= amount
		}
		out = append(out, map[string]interface{}{
			"project": project, "account_type": accountType, "amount": amount,
		})
	}
	for project, net := range totals {
		out = append(out, map[string]interface{}{
			"project": project, "account_type": "Net Profit", "amount": net,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "project-pl", Label: "Project P&L (Job Costing)", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "project", Label: "Project"}, {Key: "account_type", Label: "Type"},
			{Key: "amount", Label: "Amount", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetProjectPL(tenantID, params["start"], params["end"])
		},
		DrillDown: func(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
			return dimensionPLDrillDown(tenantID, "project", rowKey, params["start"], params["end"])
		},
	})
}
