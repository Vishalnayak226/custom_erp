package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 170.25: a salary revision (employee, new amounts, effective date, reason)
// goes through the maker-checker approval engine. Once Approved it is the salary
// for every payroll month on or after the month of its effective date: the
// engine reads it when it works out a payslip, and the SalaryStructure row is
// never edited. Months already run are not re-paid: SalaryRevisionArrears lists
// what they would have been, for HR to settle by hand.

var salaryRevisionFields = []string{"basic", "hra", "other_allowances", "pf_percent", "esi_percent", "pt_amount"}

// applyApprovedRevision overlays the latest Approved revision effective by asOf
// (a YYYY-MM-DD date, normally the payroll period's end) onto a structure.
func applyApprovedRevision(tenantID, employeeID, asOf string, structure map[string]interface{}) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || asOf == "" {
		return
	}
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE doctype = 'SalaryRevision' AND status = 'Approved' AND deleted_at IS NULL
		  AND data->>'employee_id' = $1 AND data->>'effective_date' <= $2
		ORDER BY data->>'effective_date' DESC, created_at DESC LIMIT 1`, schema), employeeID, asOf).Scan(&raw) != nil {
		return
	}
	var rev map[string]interface{}
	if json.Unmarshal([]byte(raw), &rev) != nil {
		return
	}
	for _, f := range salaryRevisionFields {
		if v, ok := rev[f]; ok && v != nil && fmt.Sprint(v) != "" {
			structure[f] = v
		}
	}
}

// SalaryArrearsRow is one already-run payslip the revision would have changed.
type SalaryArrearsRow struct {
	PayslipID  string  `json:"payslip_id"`
	PeriodFrom string  `json:"period_from"`
	PeriodTo   string  `json:"period_to"`
	Status     string  `json:"status"`
	OldGross   float64 `json:"old_gross"`
	NewGross   float64 `json:"new_gross"`
	Arrears    float64 `json:"arrears"`
}

// SalaryRevisionArrears lists, for an Approved revision, the payslips of its
// employee for months on or after its effective date that were worked out on
// the old amounts. It lists only; nothing is paid or posted.
func SalaryRevisionArrears(tenantID, revisionID string) ([]SalaryArrearsRow, float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, 0, err
	}
	var raw, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'SalaryRevision' AND id = $1 AND deleted_at IS NULL`, schema), revisionID).Scan(&raw, &status); err != nil {
		return nil, 0, &ValidationError{Code: "HRPAYR-1748", Message: "That salary revision was not found."}
	}
	if status != "Approved" {
		return nil, 0, &ValidationError{Code: "HRPAYR-1748", Message: "Arrears are listed only for an approved salary revision."}
	}
	var rev map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rev); err != nil {
		return nil, 0, err
	}
	emp, eff := strField(rev, "employee_id"), strField(rev, "effective_date")
	// A payslip for the month holding the effective date counts: the revision applies to that whole month.
	monthStart := eff
	if t, err := time.Parse("2006-01-02", eff); err == nil {
		monthStart = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, data FROM %s.documents
		WHERE doctype = 'Payslip' AND deleted_at IS NULL AND status <> 'Cancelled' AND data->>'employee_id' = $1
		  AND data->>'period_to' >= $2 ORDER BY data->>'period_to'`, schema), emp, monthStart)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SalaryArrearsRow
	total := 0.0
	for rows.Next() {
		var id, st, pr string
		var d map[string]interface{}
		if rows.Scan(&id, &st, &pr) != nil || json.Unmarshal([]byte(pr), &d) != nil {
			continue
		}
		to := strField(d, "period_to")
		old := numFromInterface(d["full_month_gross"])
		if old == 0 {
			old = numFromInterface(d["gross_pay"])
		}
		sc, err := CalculateSalaryComponentsFor(tenantID, emp, to)
		if err != nil {
			continue
		}
		ratio := 1.0
		if pd := numFromInterface(d["period_days"]); pd > 0 {
			ratio = numFromInterface(d["paid_days"]) / pd
		}
		diff := round2((sc.Gross - old) * ratio)
		if diff > -0.005 && diff < 0.005 {
			continue
		}
		out = append(out, SalaryArrearsRow{PayslipID: id, PeriodFrom: strField(d, "period_from"), PeriodTo: to, Status: st, OldGross: old, NewGross: sc.Gross, Arrears: diff})
		total += diff
	}
	return out, round2(total), rows.Err()
}

func validateSalaryRevision(tenantID string, payload map[string]interface{}) error {
	if t := strField(payload, "effective_date"); t != "" {
		if _, err := time.Parse("2006-01-02", t); err != nil {
			return &ValidationError{Code: "HRPAYR-1746", Message: "The effective date must be a date."}
		}
	}
	any := false
	for _, f := range salaryRevisionFields {
		if v, ok := payload[f]; ok && v != nil && fmt.Sprint(v) != "" {
			any = true
			if numFromInterface(v) < 0 {
				return &ValidationError{Code: "HRPAYR-1746", Message: "An amount cannot be negative."}
			}
		}
	}
	if !any {
		return &ValidationError{Code: "HRPAYR-1746", Message: "Enter at least one new amount."}
	}
	if emp := strField(payload, "employee_id"); emp != "" {
		if _, err := CalculateSalaryComponentsFor(tenantID, emp, ""); err != nil {
			return &ValidationError{Code: "HRPAYR-1747", Message: "This employee has no active salary structure to revise."}
		}
	}
	return nil
}

// 170.25 / 170.23 / 170.24: the new pay fields are payroll-grade like the rest
// (HR / Super Admin only), so a revision or a payslip's benefits are never
// readable by a Cashier or Store Manager.
func init() {
	const why = "Individual compensation"
	rev := map[string]sensitiveFieldRule{}
	for _, f := range append([]string{"reason", "effective_date"}, salaryRevisionFields...) {
		rev[f] = payrollRule(why)
	}
	sensitiveFields["SalaryRevision"] = rev
	for _, f := range []string{"lwf_deduction", "lwf_employer", "benefit_earnings", "benefit_deductions", "benefit_employer", "benefit_lines"} {
		sensitiveFields["Payslip"][f] = payrollRule(why)
	}
	sensitiveFields["BenefitEnrolment"] = map[string]sensitiveFieldRule{"value_override": payrollRule(why)}
	sensitiveFields["BenefitPlan"] = map[string]sensitiveFieldRule{"value": payrollRule(why)}
	sensitiveFields["Employee"]["tax_declared_deductions"] = payrollRule("Individual tax declaration")
}
