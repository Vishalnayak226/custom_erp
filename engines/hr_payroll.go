package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 26.8 (HR/Payroll Maturity Sprint) - extends Stage 13.13a's HR
// Foundation (Employee/Attendance/Leave, access-link sync, payroll export)
// rather than replacing it. 26.8.7 (full KRA/KPI appraisal cycles,
// training, grievance handling) stays out of scope per the checklist's own
// [P2 - tier/scope decision] note.

// ============================================================
// 26.8.2: Salary structure + statutory deduction calc
// ============================================================

// SalaryComponents is one employee's computed pay breakdown for a period -
// PF/ESI/PT/TDS-on-salary as a real payroll *processing* engine, a
// deliberate sibling to Stage 13.13a's payroll *export* (GetPayrollExport
// stays read-only/unchanged), the same pattern Stage 20.28's
// PayVendorInvoiceWithTDS used alongside the plain PayVendorInvoice.
type SalaryComponents struct {
	Basic           float64 `json:"basic"`
	HRA             float64 `json:"hra"`
	OtherAllowances float64 `json:"other_allowances"`
	Gross           float64 `json:"gross"`
	PF              float64 `json:"pf"`
	ESI             float64 `json:"esi"`
	PT              float64 `json:"pt"`
}

// esiWageCeilingFor mirrors India's statutory ESI applicability ceiling - an
// employee whose gross exceeds it owes no ESI contribution, same threshold-
// gated shape as CalculateTDS's own section threshold. Stage 30.7 moved it to
// the "hr.esi_wage_ceiling" setting (default still 21000) so a statutory
// revision is an admin edit rather than a code change and redeploy.
func esiWageCeilingFor(tenantID string) float64 {
	return GetSettingFloat(tenantID, "hr.esi_wage_ceiling")
}

// CalculateSalaryComponents reads the employee's latest Active
// SalaryStructure (HRPAYR-0154 if none is configured) and computes gross
// pay plus the PF/ESI/PT statutory deductions.
func CalculateSalaryComponents(tenantID, employeeID string) (*SalaryComponents, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'SalaryStructure' AND data->>'employee_id' = $1 AND status = 'Active'
		ORDER BY data->>'effective_from' DESC LIMIT 1`, schema), employeeID).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "HRPAYR-0154", Message: fmt.Sprintf("no active salary structure is configured for employee %s", employeeID)}
	}
	if err != nil {
		return nil, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return nil, err
	}

	sc := &SalaryComponents{
		Basic:           numFromInterface(d["basic"]),
		HRA:             numFromInterface(d["hra"]),
		OtherAllowances: numFromInterface(d["other_allowances"]),
	}
	sc.Gross = sc.Basic + sc.HRA + sc.OtherAllowances
	pfPercent := numFromInterface(d["pf_percent"])
	sc.PF = round2(sc.Basic * pfPercent / 100)
	if sc.Gross <= esiWageCeilingFor(tenantID) {
		esiPercent := numFromInterface(d["esi_percent"])
		sc.ESI = round2(sc.Gross * esiPercent / 100)
	}
	sc.PT = numFromInterface(d["pt_amount"])
	return sc, nil
}

// ============================================================
// 26.8.3: Payslip generation + payroll-to-GL posting
// ============================================================

// RunPayroll computes one employee's payslip for a period: pulls the
// existing Stage 13.13a attendance/leave export (HRPAYR-0150 if no
// attendance exists for the period at all), the statutory deductions
// above, a flat-rate TDS-on-salary estimate (reusing engines/tds.go's
// existing CalculateTDS against a tenant-configured "SALARY" TDSSection,
// rather than building a second, progressive-slab tax engine), and any
// Active EmployeeLoan deduction - then writes a Draft Payslip document.
func RunPayroll(tenantID, employeeID, periodFrom, periodTo, actorUserID string) (string, error) {
	export, err := GetPayrollExport(tenantID, periodFrom, periodTo)
	if err != nil {
		return "", err
	}
	found := false
	for _, e := range export {
		if e.EmployeeID == employeeID {
			found = true
			break
		}
	}
	if !found {
		return "", &ValidationError{Code: "HRPAYR-0150", Message: fmt.Sprintf("no attendance is recorded for employee %s between %s and %s - regularize attendance before running payroll", employeeID, periodFrom, periodTo)}
	}

	sc, err := CalculateSalaryComponents(tenantID, employeeID)
	if err != nil {
		return "", err
	}

	tdsAmount := 0.0
	if _, netAfterTDS, terr := CalculateTDS(tenantID, "SALARY", sc.Gross); terr == nil {
		tdsAmount = sc.Gross - netAfterTDS
	}

	loanDeduction, err := activeLoanDeductionForEmployee(tenantID, employeeID)
	if err != nil {
		return "", err
	}

	netPay := sc.Gross - sc.PF - sc.ESI - sc.PT - tdsAmount - loanDeduction
	if netPay < 0 {
		netPay = 0
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	code, err := GenerateSequence(tenantID, "Payslip", "", "")
	if err != nil {
		code = fmt.Sprintf("PAYSLIP-%s-%d", employeeID, time.Now().Unix())
	}
	payload := map[string]interface{}{
		"code": code, "employee_id": employeeID,
		"period_from": periodFrom, "period_to": periodTo,
		"gross_pay": sc.Gross, "pf_deduction": sc.PF, "esi_deduction": sc.ESI,
		"pt_deduction": sc.PT, "tds_deduction": tdsAmount, "loan_deduction": loanDeduction,
		"net_pay": netPay,
	}
	marshaled, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'Payslip', $2, 'Draft', $3)`, schema), code, marshaled, actorUserID); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, actorUserID, "PAYROLL_RUN", "SUCCESS", fmt.Sprintf("Payslip %s generated for employee %s, period %s to %s, net pay %v", code, employeeID, periodFrom, periodTo, netPay))
	return code, nil
}

// PostPayslipToGL posts a Draft payslip's amounts to the GL (Salary Expense
// debited; PF/ESI/PT/TDS payable, Staff Loan receivable reduction, and
// Salary Payable credited - a balanced multi-credit entry exactly like
// PayVendorInvoiceWithTDS's own shape) and marks it Posted. Requires the
// employee to have bank details on file (HRPAYR-0155) - a payslip can be
// computed without them, but not actually disbursed.
func PostPayslipToGL(tenantID, payslipID, actorUserID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'Payslip' AND id = $1`, schema), payslipID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("payslip not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("only a Draft payslip can be posted (current status: %s)", status)
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return err
	}
	employeeID, _ := d["employee_id"].(string)

	var bankAccount string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'bank_account_no', '') FROM %s.documents WHERE doctype = 'Employee' AND id = $1`, schema), employeeID).Scan(&bankAccount)
	if bankAccount == "" {
		return &ValidationError{Code: "HRPAYR-0155", Message: fmt.Sprintf("employee %s has no bank account on file - add bank details before disbursing salary", employeeID)}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	periodTo, _ := d["period_to"].(string)
	if perr := rejectIfCurrentPeriodClosed(tx, schema, "Payslip", payslipID, periodTo); perr != nil {
		return &ValidationError{Code: "HRPAYR-0153", Message: "payroll period is locked - changes are not allowed"}
	}

	gross := int(numFromInterface(d["gross_pay"]))
	pf := int(numFromInterface(d["pf_deduction"]))
	esi := int(numFromInterface(d["esi_deduction"]))
	pt := int(numFromInterface(d["pt_deduction"]))
	tds := int(numFromInterface(d["tds_deduction"]))
	loanDed := int(numFromInterface(d["loan_deduction"]))
	netPay := int(numFromInterface(d["net_pay"]))

	credits := map[string]int{"2400": netPay}
	if pf > 0 {
		credits["2401"] = pf
	}
	if esi > 0 {
		credits["2402"] = esi
	}
	if pt > 0 {
		credits["2403"] = pt
	}
	if tds > 0 {
		credits["2300"] = tds
	}
	if loanDed > 0 {
		credits["1600"] = loanDed
	}
	debits := map[string]int{"5500": gross}
	if err := PostDoubleEntry(tenantID, "Payslip", payslipID, debits, credits, periodTo, fmt.Sprintf("Payslip:%s:POST", payslipID)); err != nil {
		return fmt.Errorf("GL posting failed, payslip not marked Posted: %v", err)
	}

	if loanDed > 0 {
		if err := applyLoanDeduction(tx, schema, employeeID); err != nil {
			return err
		}
	}

	d["status"] = "Posted"
	marshaled, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Payslip' AND id = $2`, schema), marshaled, payslipID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "PAYSLIP_POSTED", "SUCCESS", fmt.Sprintf("Payslip %s posted to GL, net pay %d", payslipID, netPay))
	return nil
}

// ============================================================
// 26.8.4: Loans/advances against salary
// ============================================================

// DisburseEmployeeLoan moves a Draft EmployeeLoan to Active: posts Dr Staff
// Loans Receivable / Cr Cash-Bank, and initializes outstanding_balance to
// the principal so PostPayslipToGL's later deductions have something to
// reduce.
func DisburseEmployeeLoan(tenantID, loanID, actorUserID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'EmployeeLoan' AND id = $1`, schema), loanID).Scan(&dataStr, &status); err != nil {
		return fmt.Errorf("loan not found: %v", err)
	}
	if status == "Active" || status == "Closed" {
		return fmt.Errorf("loan %s has already been disbursed (status: %s)", loanID, status)
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return err
	}
	principal := int(numFromInterface(d["principal_amount"]))
	if principal <= 0 {
		return fmt.Errorf("principal_amount must be positive")
	}

	debits := map[string]int{"1600": principal}
	credits := map[string]int{"1100": principal}
	if err := PostDoubleEntry(tenantID, "EmployeeLoan", loanID, debits, credits, "", fmt.Sprintf("EmployeeLoan:%s:DISBURSE", loanID)); err != nil {
		return fmt.Errorf("GL posting failed, loan not disbursed: %v", err)
	}

	d["status"] = "Active"
	d["outstanding_balance"] = float64(principal)
	marshaled, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Active', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'EmployeeLoan' AND id = $2`, schema), marshaled, loanID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actorUserID, "EMPLOYEE_LOAN_DISBURSED", "SUCCESS", fmt.Sprintf("Loan %s disbursed, principal %d", loanID, principal))
	return nil
}

// activeLoanDeductionForEmployee sums this month's deduction across every
// Active loan the employee has, each capped at its own remaining
// outstanding_balance so a loan's final installment never over-deducts.
func activeLoanDeductionForEmployee(tenantID, employeeID string) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'EmployeeLoan' AND data->>'employee_id' = $1 AND status = 'Active'`, schema), employeeID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := 0.0
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
			continue
		}
		monthly := numFromInterface(d["monthly_deduction"])
		outstanding := numFromInterface(d["outstanding_balance"])
		if monthly > outstanding {
			monthly = outstanding
		}
		total += monthly
	}
	return total, nil
}

// applyLoanDeduction reduces every Active loan's outstanding_balance by its
// own share of the deducted total (same per-loan cap logic as the function
// above, so the two stay consistent), closing a loan that reaches zero.
// Runs inside PostPayslipToGL's own transaction so a GL-posting failure
// never leaves a loan balance updated with nothing posted behind it.
func applyLoanDeduction(tx *sql.Tx, schema, employeeID string) error {
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, data FROM %s.documents
		WHERE doctype = 'EmployeeLoan' AND data->>'employee_id' = $1 AND status = 'Active' FOR UPDATE`, schema), employeeID)
	if err != nil {
		return err
	}
	type loanRow struct {
		id   string
		data map[string]interface{}
	}
	var loans []loanRow
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			rows.Close()
			return err
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
			continue
		}
		loans = append(loans, loanRow{id: id, data: d})
	}
	rows.Close()

	for _, l := range loans {
		monthly := numFromInterface(l.data["monthly_deduction"])
		outstanding := numFromInterface(l.data["outstanding_balance"])
		deduct := monthly
		if deduct > outstanding {
			deduct = outstanding
		}
		if deduct <= 0 {
			continue
		}
		newBalance := outstanding - deduct
		l.data["outstanding_balance"] = newBalance
		newStatus := "Active"
		if newBalance <= 0.0001 {
			newStatus = "Closed"
		}
		l.data["status"] = newStatus
		marshaled, err := json.Marshal(l.data)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'EmployeeLoan' AND id = $3`, schema), marshaled, newStatus, l.id); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================
// 26.8.5: Employee self-service (leave + expense-claim submission)
// ============================================================

// GetMyEmployeeRecord resolves the Employee document linked to the
// currently logged-in user (Employee.user_id), so a self-service screen can
// prefill/scope requests without the user needing to know their own
// employee code. Returns ("", nil) if the user has no linked Employee.
func GetMyEmployeeRecord(tenantID, userID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var id, dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'Employee' AND data->>'user_id' = $1 LIMIT 1`, schema), userID).Scan(&id, &dataStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return nil, err
	}
	d["id"] = id
	return d, nil
}
