package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 170.23: salary TDS by income-tax slab. The slabs, standard deduction,
// rebate and cess are editable rows (TaxSlab / TaxRegimeRule) per regime and
// financial year; the FY 2025-26 figures are seeded as ordinary rows and must be
// confirmed by a CA before use. Behind setting hr.tds_by_slab (default OFF) the
// payslip's TDS is the projected annual tax less the TDS already deducted this
// financial year, spread over the months left - the flat "SALARY" section
// estimate stays the behaviour while the setting is off.
//
// Out of scope (stated in the admin guide): surcharge, HRA / 80C proof
// handling beyond one declared yearly deductions figure, previous-employer
// income, and arrears relief (section 89).

func init() {
	RegisterSetting(SettingDefinition{
		Key: "hr.tds_by_slab", Module: "HR & Payroll",
		Label: "Salary TDS by tax slab", Type: SettingTypeBool, Default: "false",
		Description: "When on, payroll works out monthly TDS from the income-tax slabs (projected yearly tax less TDS already deducted, spread over the months left). When off, the flat TDS estimate is used. Check the slab rows with your CA first.",
	})
	RegisterSetting(SettingDefinition{
		Key: "hr.lwf_enabled", Module: "HR & Payroll",
		Label: "Labour welfare fund on payslips", Type: SettingTypeBool, Default: "false",
		Description: "When on, payroll deducts LWF (and books the employer share) in the months listed on the employee's work-state rate row. Enter the state rates first.",
	})
}

// TaxSlabRow is one band of a regime's slab table.
type TaxSlabRow struct {
	From, To float64 // To == 0 means no upper limit
	Rate     float64
}

// TaxRegimeParams are the per-regime, per-year figures around the slabs.
type TaxRegimeParams struct {
	StandardDeduction, RebateLimit, RebateMax, Cess float64
	MarginalRelief                                  bool
}

// TaxComputation is the working behind an annual tax figure.
type TaxComputation struct {
	Regime, FinYear string
	Gross           float64 `json:"gross"`
	StandardDed     float64 `json:"standard_deduction"`
	OtherDeductions float64 `json:"other_deductions"`
	Taxable         float64 `json:"taxable_income"`
	Tax             float64 `json:"tax"`
}

// FinYearOf returns the financial-year label ("2025-26") and its start year for a date.
func FinYearOf(date string) (string, int, error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", 0, fmt.Errorf("date %q is not YYYY-MM-DD", date)
	}
	y := t.Year()
	if t.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100), y, nil
}

// monthsLeftInFY counts the payroll months from the month of date to March, inclusive.
func monthsLeftInFY(date string) int {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 1
	}
	return (int(time.March)-int(t.Month())+12)%12 + 1
}

// ComputeAnnualTax applies slabs, rebate, marginal relief and cess to a taxable income.
func ComputeAnnualTax(slabs []TaxSlabRow, p TaxRegimeParams, taxable float64) float64 {
	if taxable <= 0 {
		return 0
	}
	tax := 0.0
	for _, s := range slabs {
		upper := s.To
		if upper <= 0 || upper > taxable {
			upper = taxable
		}
		if upper > s.From {
			tax += (upper - s.From) * s.Rate / 100
		}
	}
	if p.RebateLimit > 0 {
		if taxable <= p.RebateLimit {
			rebate := p.RebateMax
			if rebate > tax {
				rebate = tax
			}
			tax -= rebate
		} else if p.MarginalRelief && tax > taxable-p.RebateLimit {
			tax = taxable - p.RebateLimit
		}
	}
	tax += tax * p.Cess / 100
	return round2(tax)
}

func loadTaxTables(tenantID, regime, finYear string) ([]TaxSlabRow, TaxRegimeParams, error) {
	var params TaxRegimeParams
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, params, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype, data FROM %s.documents
		WHERE doctype IN ('TaxSlab', 'TaxRegimeRule') AND deleted_at IS NULL
		  AND data->>'regime' = $1 AND data->>'fin_year' = $2 AND COALESCE(data->>'status', 'Active') <> 'Inactive'`, schema), regime, finYear)
	if err != nil {
		return nil, params, err
	}
	defer rows.Close()
	var slabs []TaxSlabRow
	haveRule := false
	for rows.Next() {
		var dt, raw string
		if rows.Scan(&dt, &raw) != nil {
			continue
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		if dt == "TaxSlab" {
			slabs = append(slabs, TaxSlabRow{From: numFromInterface(d["from_amount"]), To: numFromInterface(d["to_amount"]), Rate: numFromInterface(d["rate_percent"])})
		} else {
			haveRule = true
			params = TaxRegimeParams{StandardDeduction: numFromInterface(d["standard_deduction"]), RebateLimit: numFromInterface(d["rebate_income_limit"]),
				RebateMax: numFromInterface(d["rebate_max"]), Cess: numFromInterface(d["cess_percent"]), MarginalRelief: strField(d, "marginal_relief") == "Yes"}
		}
	}
	if len(slabs) == 0 || !haveRule {
		return nil, params, &ValidationError{Code: "HRPAYR-1740", Message: fmt.Sprintf("no tax slabs and regime rule are set up for the %s regime, FY %s", regime, finYear)}
	}
	sort.Slice(slabs, func(i, j int) bool { return slabs[i].From < slabs[j].From })
	return slabs, params, nil
}

// employeeTaxProfile reads the employee's regime (default New), declared yearly deductions.
func employeeTaxProfile(tenantID, employeeID string) (regime string, declared float64) {
	regime = "New"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Employee' AND id = $1 AND deleted_at IS NULL`, schema), employeeID).Scan(&raw) != nil {
		return
	}
	var d map[string]interface{}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return
	}
	if strField(d, "tax_regime") == "Old" {
		regime = "Old"
	}
	declared = numFromInterface(d["tax_declared_deductions"])
	return
}

// AnnualTaxOn works out the tax for a year's gross and professional tax.
func AnnualTaxOn(tenantID, regime, finYear string, gross, pt, declared float64) (TaxComputation, error) {
	slabs, params, err := loadTaxTables(tenantID, regime, finYear)
	if err != nil {
		return TaxComputation{}, err
	}
	tc := TaxComputation{Regime: regime, FinYear: finYear, Gross: round2(gross), StandardDed: params.StandardDeduction}
	if regime == "Old" {
		tc.OtherDeductions = round2(declared + pt) // section 16(iii) tax on employment and declared 80C etc.
	}
	tc.Taxable = round2(gross - tc.StandardDed - tc.OtherDeductions)
	if tc.Taxable < 0 {
		tc.Taxable = 0
	}
	tc.Tax = ComputeAnnualTax(slabs, params, tc.Taxable)
	return tc, nil
}

// priorPayslipTotals sums the employee's earlier payslips in the financial year
// (Draft and Posted, not the period being run).
func priorPayslipTotals(tenantID, employeeID, finYear string, startYear int, periodFrom string) (gross, pt, tds float64) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE doctype = 'Payslip' AND deleted_at IS NULL AND status <> 'Cancelled' AND data->>'employee_id' = $1
		  AND data->>'period_to' >= $2 AND data->>'period_to' < $3`, schema), employeeID, fmt.Sprintf("%d-04-01", startYear), periodFrom)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var d map[string]interface{}
		if rows.Scan(&raw) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		gross += numFromInterface(d["gross_pay"])
		pt += numFromInterface(d["pt_deduction"])
		tds += numFromInterface(d["tds_deduction"])
	}
	return
}

// salaryTDSBySlab returns the month's TDS under the slab method. used is false
// when the setting is off, so the caller keeps its flat estimate.
func salaryTDSBySlab(tenantID, employeeID string, gross, pt float64, periodFrom, periodTo string) (tds float64, regime string, used bool, err error) {
	if !GetSettingBool(tenantID, "hr.tds_by_slab") {
		return 0, "", false, nil
	}
	fy, startYear, err := FinYearOf(periodTo)
	if err != nil {
		return 0, "", true, err
	}
	regime, declared := employeeTaxProfile(tenantID, employeeID)
	rem := monthsLeftInFY(periodTo)
	pg, ppt, ptds := priorPayslipTotals(tenantID, employeeID, fy, startYear, periodFrom)
	tc, err := AnnualTaxOn(tenantID, regime, fy, pg+gross*float64(rem), ppt+pt*float64(rem), declared)
	if err != nil {
		return 0, regime, true, err
	}
	left := tc.Tax - ptds
	if left < 0 {
		left = 0
	}
	return round2(left / float64(rem)), regime, true, nil
}

// ---- Form 24Q (salary TDS return data) ------------------------------------

// Form24QCSV builds the deductee-wise quarterly salary TDS data as CSV. Annexure
// I lists each month's payment and TDS per employee; annexure II (Q4 only) lists
// the full-year salary and tax computation per employee. The FVU text format the
// TIN-NSDL utility needs is not produced: this is the data to key into it.
func Form24QCSV(tenantID, finYear, quarter, annexure string) (string, error) {
	var sy int
	if _, err := fmt.Sscanf(finYear, "%d-", &sy); err != nil || sy < 2000 || len(finYear) != 7 {
		return "", &ValidationError{Code: "HRPAYR-1741", Message: "financial year must look like 2025-26"}
	}
	months := map[string][2]string{
		"Q1": {fmt.Sprintf("%d-04-01", sy), fmt.Sprintf("%d-06-30", sy)},
		"Q2": {fmt.Sprintf("%d-07-01", sy), fmt.Sprintf("%d-09-30", sy)},
		"Q3": {fmt.Sprintf("%d-10-01", sy), fmt.Sprintf("%d-12-31", sy)},
		"Q4": {fmt.Sprintf("%d-01-01", sy+1), fmt.Sprintf("%d-03-31", sy+1)},
	}
	rng, ok := months[quarter]
	if !ok {
		return "", &ValidationError{Code: "HRPAYR-1741", Message: "quarter must be Q1, Q2, Q3 or Q4"}
	}
	if annexure == "" {
		annexure = "I"
	}
	if annexure != "I" && annexure != "II" {
		return "", &ValidationError{Code: "HRPAYR-1741", Message: "annexure must be I or II"}
	}
	if annexure == "II" && quarter != "Q4" {
		return "", &ValidationError{Code: "HRPAYR-1741", Message: "annexure II is part of the Q4 return only"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	from, to := rng[0], rng[1]
	if annexure == "II" {
		from, to = fmt.Sprintf("%d-04-01", sy), fmt.Sprintf("%d-03-31", sy+1)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT p.id, p.data, COALESCE(e.data->>'pan', ''), COALESCE(e.data->>'name', '')
		FROM %s.documents p LEFT JOIN %s.documents e ON e.doctype = 'Employee' AND e.id = p.data->>'employee_id'
		WHERE p.doctype = 'Payslip' AND p.status = 'Posted' AND p.deleted_at IS NULL
		  AND p.data->>'period_to' BETWEEN $1 AND $2 ORDER BY p.data->>'employee_id', p.data->>'period_to'`, schema, schema), from, to)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	csvq := func(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }
	var b strings.Builder
	type annual struct {
		name, pan      string
		gross, pt, tds float64
	}
	totals := map[string]*annual{}
	var order []string
	if annexure == "I" {
		b.WriteString("Employee,Deductee PAN,Deductee name,Section,Month,Payment date,Amount paid,TDS deducted,TDS deposited,Note\n")
	}
	for rows.Next() {
		var id, raw, pan, name string
		var d map[string]interface{}
		if rows.Scan(&id, &raw, &pan, &name) != nil || json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		emp, pt := strField(d, "employee_id"), strField(d, "period_to")
		gross, tds := numFromInterface(d["gross_pay"]), numFromInterface(d["tds_deduction"])
		if annexure == "I" {
			note := ""
			if pan == "" {
				note = "PAN missing - deductee is reported as PANNOTAVBL"
				pan = "PANNOTAVBL"
			}
			fmt.Fprintf(&b, "%s,%s,%s,192,%s,%s,%.2f,%.2f,%.2f,%s\n", csvq(emp), csvq(pan), csvq(name), pt[:7], pt, gross, tds, tds, csvq(note))
			continue
		}
		a := totals[emp]
		if a == nil {
			a = &annual{name: name, pan: pan}
			totals[emp], order = a, append(order, emp)
		}
		a.gross += gross
		a.pt += numFromInterface(d["pt_deduction"])
		a.tds += tds
	}
	if annexure == "I" {
		return b.String(), rows.Err()
	}
	b.WriteString("Employee,Deductee PAN,Deductee name,Regime,Gross salary,Standard deduction,Other deductions,Taxable income,Tax on income,TDS deducted,Difference\n")
	for _, emp := range order {
		a := totals[emp]
		regime, declared := employeeTaxProfile(tenantID, emp)
		tc, terr := AnnualTaxOn(tenantID, regime, finYear, a.gross, a.pt, declared)
		pan := a.pan
		if pan == "" {
			pan = "PANNOTAVBL"
		}
		if terr != nil {
			fmt.Fprintf(&b, "%s,%s,%s,%s,%.2f,,,,,%.2f,%s\n", csvq(emp), csvq(pan), csvq(a.name), regime, a.gross, a.tds, csvq("tax slabs missing for this year"))
			continue
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f\n", csvq(emp), csvq(pan), csvq(a.name), regime, tc.Gross, tc.StandardDed, tc.OtherDeductions, tc.Taxable, tc.Tax, a.tds, round2(tc.Tax-a.tds))
	}
	return b.String(), nil
}
