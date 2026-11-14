package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stage 170.24 (benefits) and 170.23 (LWF): the extra earnings, deductions and
// employer-paid shares a payslip carries beyond PF / ESI / PT / TDS / loan.
// Both are empty for an employee with no enrolment and no LWF row, so a payslip
// is then exactly what it was before.

// PayslipBenefitLine is one enrolled benefit's amount on one payslip.
type PayslipBenefitLine struct {
	Plan   string  `json:"plan"`
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
}

// PayslipExtras is the benefits and LWF part of a payslip.
type PayslipExtras struct {
	BenefitEarnings, BenefitDeductions, BenefitEmployer float64
	LWFEmployee, LWFEmployer                            float64
	Lines                                               []PayslipBenefitLine
}

func (x PayslipExtras) Deductions() float64 { return x.BenefitDeductions + x.LWFEmployee }

// payslipExtras works out the benefits (Active enrolments overlapping the
// period) and, behind hr.lwf_enabled, the LWF for the period's month.
func payslipExtras(tenantID, employeeID, periodFrom, periodTo string, basic float64) (PayslipExtras, error) {
	var out PayslipExtras
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT e.data, p.id, p.data FROM %s.documents e
		JOIN %s.documents p ON p.doctype = 'BenefitPlan' AND p.id = e.data->>'plan_id' AND p.deleted_at IS NULL
		WHERE e.doctype = 'BenefitEnrolment' AND e.deleted_at IS NULL AND e.data->>'employee_id' = $1
		  AND COALESCE(e.data->>'status', 'Active') = 'Active'
		  AND e.data->>'start_date' <= $3 AND (COALESCE(e.data->>'end_date', '') = '' OR e.data->>'end_date' >= $2)
		  AND COALESCE(p.data->>'status', 'Active') = 'Active'
		ORDER BY e.id`, schema, schema), employeeID, periodFrom, periodTo)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var er, pid, pr string
		var e, p map[string]interface{}
		if rows.Scan(&er, &pid, &pr) != nil || json.Unmarshal([]byte(er), &e) != nil || json.Unmarshal([]byte(pr), &p) != nil {
			continue
		}
		v := numFromInterface(p["value"])
		if s, ok := e["value_override"]; ok && s != nil && fmt.Sprint(s) != "" {
			v = numFromInterface(s)
		}
		amt := v
		if strField(p, "calc_method") == "Percent of basic" {
			amt = round2(basic * v / 100)
		}
		if amt <= 0 {
			continue
		}
		typ := strField(p, "benefit_type")
		switch typ {
		case "Earning":
			out.BenefitEarnings += amt
		case "Deduction":
			out.BenefitDeductions += amt
		case "Employer-paid":
			out.BenefitEmployer += amt
		default:
			continue
		}
		out.Lines = append(out.Lines, PayslipBenefitLine{Plan: pid, Name: strField(p, "name"), Type: typ, Amount: amt})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.BenefitEarnings, out.BenefitDeductions, out.BenefitEmployer = round2(out.BenefitEarnings), round2(out.BenefitDeductions), round2(out.BenefitEmployer)
	out.LWFEmployee, out.LWFEmployer = lwfFor(tenantID, schema, employeeID, periodTo)
	return out, nil
}

// lwfFor returns the employee and employer LWF due in periodTo's month: the
// employee's work state's rate row, when its months list includes the month.
func lwfFor(tenantID, schema, employeeID, periodTo string) (emp, er float64) {
	if !GetSettingBool(tenantID, "hr.lwf_enabled") {
		return 0, 0
	}
	t, err := time.Parse("2006-01-02", periodTo)
	if err != nil {
		return 0, 0
	}
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Employee' AND id = $1 AND deleted_at IS NULL`, schema), employeeID).Scan(&raw) != nil {
		return 0, 0
	}
	var e map[string]interface{}
	if json.Unmarshal([]byte(raw), &e) != nil {
		return 0, 0
	}
	state := strings.TrimSpace(strField(e, "work_state"))
	if state == "" {
		return 0, 0
	}
	var rate string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'LWFRate' AND deleted_at IS NULL
		AND lower(data->>'state') = lower($1) AND COALESCE(data->>'status', 'Active') = 'Active' ORDER BY id LIMIT 1`, schema), state).Scan(&rate) != nil {
		return 0, 0
	}
	var r map[string]interface{}
	if json.Unmarshal([]byte(rate), &r) != nil {
		return 0, 0
	}
	for _, m := range strings.Split(strField(r, "months"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(m)); err == nil && n == int(t.Month()) {
			return round2(numFromInterface(r["employee_amount"])), round2(numFromInterface(r["employer_amount"]))
		}
	}
	return 0, 0
}

// ---- validation hooks (called from ValidateDocument) ------------------------

func validateBenefitEnrolment(tenantID string, payload map[string]interface{}) error {
	start, end := strField(payload, "start_date"), strField(payload, "end_date")
	if end != "" && start != "" && end < start {
		return &ValidationError{Code: "HRPAYR-1743", Message: "The end date is before the start date."}
	}
	return nil
}

func validateBenefitPlan(payload map[string]interface{}) error {
	if v, ok := payload["value"]; ok && numFromInterface(v) < 0 {
		return &ValidationError{Code: "HRPAYR-1744", Message: "The amount or percent cannot be negative."}
	}
	if strField(payload, "calc_method") == "Percent of basic" && numFromInterface(payload["value"]) > 100 {
		return &ValidationError{Code: "HRPAYR-1744", Message: "A percent cannot be more than 100."}
	}
	return nil
}

func validateTaxSlab(payload map[string]interface{}) error {
	to, from, rate := numFromInterface(payload["to_amount"]), numFromInterface(payload["from_amount"]), numFromInterface(payload["rate_percent"])
	if rate < 0 || rate > 100 || (to > 0 && to <= from) || from < 0 {
		return &ValidationError{Code: "HRPAYR-1745", Message: "The slab is not valid: rate must be 0-100 and the upper limit above the lower."}
	}
	return nil
}
