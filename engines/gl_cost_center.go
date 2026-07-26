package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
)

// Stage 26.6.8: intercompany/cost-center/profit-center postings and
// reports - extends Stage 17.9's Department/CostCenter masters (zero
// validation function, never referenced in postings before this) into
// finance postings via PostDoubleEntry's new additive PostingOptions
// (engines/finance.go).

// validateCostCenterReferenceInSchema/validateDepartmentReferenceInSchema
// mirror ValidateLocationReference's exact validate-against-active-master
// pattern (engines/location_masters.go) - a transaction may only reference
// a code that exists in the master and is Active. Empty is always valid
// (both fields are optional). Schema-level (not tenantID-facing) since
// PostDoubleEntry/createJournalVoucherInSchema already have the schema and
// would otherwise pay a redundant db.GetTenantSchema round trip.
func validateCostCenterReferenceInSchema(schema, costCenter string) error {
	if costCenter == "" {
		return nil
	}
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'CostCenter' AND id = $1 AND deleted_at IS NULL`, schema),
		costCenter).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("cost center '%s' is not a registered CostCenter", costCenter)
	}
	if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("cost center '%s' is not Active", costCenter)
	}
	return nil
}

func validateDepartmentReferenceInSchema(schema, department string) error {
	if department == "" {
		return nil
	}
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Department' AND id = $1 AND deleted_at IS NULL`, schema),
		department).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("department '%s' is not a registered Department", department)
	}
	if err != nil {
		return err
	}
	if status != "Active" {
		return fmt.Errorf("department '%s' is not Active", department)
	}
	return nil
}

// validateCostCenterReference/validateDepartmentReference are the
// tenantID-facing forms, for callers (e.g. PostDoubleEntry) that don't
// already have the schema resolved.
func validateCostCenterReference(tenantID, costCenter string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return validateCostCenterReferenceInSchema(schema, costCenter)
}

func validateDepartmentReference(tenantID, department string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return validateDepartmentReferenceInSchema(schema, department)
}

// GetCostCenterPL groups Revenue/Expense gl_postings by cost_center (rows
// with no cost_center tagged land under "Unassigned") between [startDate,
// endDate] - the cost-center-wise counterpart to GetProfitAndLoss
// (engines/finance_reports_stage26.go), reading the column Stage 26.6.8
// adds where present.
func GetCostCenterPL(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(p.cost_center, 'Unassigned') AS cost_center, a.account_type,
		       CASE WHEN a.account_type = 'Revenue' THEN COALESCE(SUM(p.credit), 0) - COALESCE(SUM(p.debit), 0)
		            ELSE COALESCE(SUM(p.debit), 0) - COALESCE(SUM(p.credit), 0) END AS amount
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE a.account_type IN ('Revenue', 'Expense') AND p.created_at::date BETWEEN $1 AND $2
		GROUP BY COALESCE(p.cost_center, 'Unassigned'), a.account_type
		ORDER BY cost_center, a.account_type`, schema, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[string]float64{}
	var out []map[string]interface{}
	for rows.Next() {
		var costCenter, accountType string
		var amount float64
		if err := rows.Scan(&costCenter, &accountType, &amount); err != nil {
			return nil, err
		}
		if accountType == "Revenue" {
			totals[costCenter] += amount
		} else {
			totals[costCenter] -= amount
		}
		out = append(out, map[string]interface{}{
			"cost_center": costCenter, "account_type": accountType, "amount": amount,
		})
	}
	for costCenter, net := range totals {
		out = append(out, map[string]interface{}{
			"cost_center": costCenter, "account_type": "Net Profit", "amount": net,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "cost-center-pl", Label: "Cost Center P&L", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "cost_center", Label: "Cost Center"}, {Key: "account_type", Label: "Type"},
			{Key: "amount", Label: "Amount", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetCostCenterPL(tenantID, params["start"], params["end"])
		},
	})
}
