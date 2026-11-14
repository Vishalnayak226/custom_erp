package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 170.23: the EPFO ECR text file for one payroll month, built from the
// Posted payslips whose period ends in that month. Fields are separated by #~#
// as the EPFO portal's ECR text upload expects. EPF wages are the earned basic;
// EPS and EDLI wages are capped at the Rs 15,000 wage ceiling; EPS is 8.33% of
// the capped wages and the employer's balance of the PF goes to the EPF diff.
// A CA must confirm the figures before the file is uploaded.

const ecrWageCeiling = 15000.0

// ECRRow is one member line, also the JSON preview.
type ECRRow struct {
	EmployeeID string   `json:"employee_id"`
	UAN        string   `json:"uan"`
	Name       string   `json:"name"`
	Gross      int64    `json:"gross_wages"`
	EPFWages   int64    `json:"epf_wages"`
	EPSWages   int64    `json:"eps_wages"`
	EDLIWages  int64    `json:"edli_wages"`
	EE         int64    `json:"ee_epf"`
	EPS        int64    `json:"eps"`
	ERDiff     int64    `json:"er_diff"`
	NCPDays    int64    `json:"ncp_days"`
	Warnings   []string `json:"warnings,omitempty"`
}

// PFECRRows works out the month's rows. month is YYYY-MM.
func PFECRRows(tenantID, month string) ([]ECRRow, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, &ValidationError{Code: "HRPAYR-1741", Message: "the month must look like 2026-04"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from := t.Format("2006-01-02")
	to := t.AddDate(0, 1, -1).Format("2006-01-02")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT p.data, COALESCE(e.data->>'uan', ''), COALESCE(e.data->>'name', '')
		FROM %s.documents p LEFT JOIN %s.documents e ON e.doctype = 'Employee' AND e.id = p.data->>'employee_id'
		WHERE p.doctype = 'Payslip' AND p.status = 'Posted' AND p.deleted_at IS NULL
		  AND p.data->>'period_to' BETWEEN $1 AND $2 ORDER BY p.data->>'employee_id'`, schema, schema), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ECRRow
	for rows.Next() {
		var raw, uan, name string
		var d map[string]interface{}
		if rows.Scan(&raw, &uan, &name) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		pf := numFromInterface(d["pf_deduction"])
		if pf <= 0 {
			continue // no PF on this payslip: not a member line
		}
		r := ECRRow{EmployeeID: strField(d, "employee_id"), UAN: strings.TrimSpace(uan), Name: name}
		if r.UAN == "" {
			r.Warnings = append(r.Warnings, "UAN missing")
		}
		epfWages := 0.0
		if sc, err := CalculateSalaryComponentsFor(tenantID, r.EmployeeID, strField(d, "period_to")); err == nil {
			epfWages = sc.Basic
			if pd := numFromInterface(d["period_days"]); pd > 0 {
				epfWages = sc.Basic * numFromInterface(d["paid_days"]) / pd
			}
		}
		if epfWages <= 0 {
			epfWages = pf / 0.12 // structure gone: back out from a 12% contribution
			r.Warnings = append(r.Warnings, "wages estimated from the PF amount")
		}
		capped := math.Min(epfWages, ecrWageCeiling)
		r.Gross = int64(math.Round(numFromInterface(d["gross_pay"])))
		r.EPFWages, r.EPSWages, r.EDLIWages = int64(math.Round(epfWages)), int64(math.Round(capped)), int64(math.Round(capped))
		r.EE = int64(math.Round(pf))
		r.EPS = int64(math.Round(capped * 8.33 / 100))
		r.ERDiff = r.EE - r.EPS // employer's PF equals the employee's, so the EPF share is what EPS leaves
		r.NCPDays = int64(math.Round(numFromInterface(d["lop_days"])))
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, &ValidationError{Code: "HRPAYR-1742", Message: "No posted payslip with a PF deduction ends in " + month + "."}
	}
	return out, nil
}

// PFECRText renders the rows as the ECR text file.
func PFECRText(rows []ECRRow) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s#~#%s#~#%d#~#%d#~#%d#~#%d#~#%d#~#%d#~#%d#~#%d#~#0\n",
			r.UAN, r.Name, r.Gross, r.EPFWages, r.EPSWages, r.EDLIWages, r.EE, r.EPS, r.ERDiff, r.NCPDays)
	}
	return b.String()
}
