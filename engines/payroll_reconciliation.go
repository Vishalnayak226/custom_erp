package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Stage 73.2.17: payroll reconciliation - gross -> deductions -> net -> bank
// -> GL, as one control report.
//
// The payslips whose pay period ends in [start, end] are the source. Each
// component total is set beside what the GL holds for those same payslips
// (PostPayslipToGL: Dr 5500 gross; Cr 2401 PF, 2402 ESI, 2403 PT, 2300 TDS,
// 1600 loan recovery, 2400 net). Then the money: what has cleared Salary
// Payable (2400) since the period began - salary paid, today recorded by a
// journal or payment that debits 2400 - and how much of that payment's bank
// leg is matched to a bank statement line (73.2.16). What is still in 2400
// is what employees are still owed.
//
// Every step drills to the payslips or GL lines behind it. A row is Tied when
// its two figures agree to the paisa, Difference when they do not, and Info
// when it is a balance to read rather than a pair to agree.

type payrollSlip struct {
	id, employee, status, periodTo     string
	gross, pf, esi, pt, tds, loan, net float64
}

var payrollComponentAccounts = []struct{ key, label, field, account string }{
	{"gross", "Gross pay (Salary Expense 5500)", "gross", "5500"},
	{"pf", "PF deducted (PF Payable 2401)", "pf", "2401"},
	{"esi", "ESI deducted (ESI Payable 2402)", "esi", "2402"},
	{"pt", "Professional tax deducted (PT Payable 2403)", "pt", "2403"},
	{"tds", "TDS deducted (TDS Payable 2300)", "tds", "2300"},
	{"loan", "Loan recovered (Staff Loans 1600)", "loan", "1600"},
	{"net", "Net pay (Salary Payable 2400)", "net", "2400"},
}

func loadPayrollSlips(schema, start, end string) ([]payrollSlip, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status FROM %s.documents
		WHERE doctype = 'Payslip' AND COALESCE(data->>'period_to', '') BETWEEN $1 AND $2
		ORDER BY data->>'period_to', id`, schema), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []payrollSlip
	for rows.Next() {
		var id, dataStr, status string
		if err := rows.Scan(&id, &dataStr, &status); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &d) != nil {
			continue
		}
		s := payrollSlip{id: id, status: status}
		s.employee, _ = d["employee_id"].(string)
		s.periodTo, _ = d["period_to"].(string)
		s.gross, s.pf, s.esi = numFromInterface(d["gross_pay"]), numFromInterface(d["pf_deduction"]), numFromInterface(d["esi_deduction"])
		s.pt, s.tds, s.loan = numFromInterface(d["pt_deduction"]), numFromInterface(d["tds_deduction"]), numFromInterface(d["loan_deduction"])
		s.net = numFromInterface(d["net_pay"])
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s payrollSlip) component(field string) float64 {
	switch field {
	case "gross":
		return s.gross
	case "pf":
		return s.pf
	case "esi":
		return s.esi
	case "pt":
		return s.pt
	case "tds":
		return s.tds
	case "loan":
		return s.loan
	}
	return s.net
}

// GetPayrollReconciliation is the report body.
func GetPayrollReconciliation(tenantID, start, end string) ([]map[string]interface{}, error) {
	if start == "" || end == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	slips, err := loadPayrollSlips(schema, start, end)
	if err != nil {
		return nil, err
	}
	var posted []string
	draftCount, arithmetic := 0, 0
	draftNet := 0.0
	for _, s := range slips {
		if s.status == "Posted" {
			posted = append(posted, s.id)
		} else {
			draftCount++
			draftNet += s.net
		}
		// net = gross - deductions, floored at zero by RunPayroll.
		if math.Abs(math.Max(s.gross-s.pf-s.esi-s.pt-s.tds-s.loan, 0)-s.net) >= 0.01 {
			arithmetic++
		}
	}
	postedArg := "{" + joinQuoted(posted) + "}"

	glFor := func(account string, debitSide bool) (float64, error) {
		var paise int64
		col := "credit - debit"
		if debitSide {
			col = "debit - credit"
		}
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(%s), 0) FROM %s.gl_postings
			WHERE document_type = 'Payslip' AND document_id = ANY($1) AND account_code = $2`, col, schema), postedArg, account).Scan(&paise)
		return PaiseToRupees(paise), err
	}

	row := func(step, label string, source float64, gl *float64, status, note string, count int) map[string]interface{} {
		r := map[string]interface{}{"step": step, "label": label, "payslips": count, "source_total": roundRupees(source), "status": status, "note": note}
		if gl != nil {
			r["gl_total"] = roundRupees(*gl)
			r["difference"] = roundRupees(source - *gl)
		} else {
			r["gl_total"] = nil
			r["difference"] = nil
		}
		return r
	}
	var out []map[string]interface{}
	for _, c := range payrollComponentAccounts {
		source := 0.0
		for _, s := range slips {
			if s.status == "Posted" {
				source += s.component(c.field)
			}
		}
		// Gross is the one debit (5500); every deduction and net pay is a
		// credit, including the loan recovery that reduces receivable 1600.
		gl, err := glFor(c.account, c.key == "gross")
		if err != nil {
			return nil, err
		}
		status := "Tied"
		if math.Abs(source-gl) >= 0.005 {
			status = "Difference"
		}
		out = append(out, row(c.key, c.label, source, &gl, status, "posted payslips vs the GL lines they posted", len(posted)))
	}

	// Gross - deductions = net, across the posted payslips.
	var g, d, n float64
	for _, s := range slips {
		if s.status == "Posted" {
			g += s.gross
			d += s.pf + s.esi + s.pt + s.tds + s.loan
			n += s.net
		}
	}
	derived := g - d
	status := "Tied"
	if math.Abs(derived-n) >= 0.005 {
		status = "Difference"
	}
	note := "gross minus every deduction equals net pay"
	if arithmetic > 0 {
		note = fmt.Sprintf("%d payslip(s) do not satisfy gross - deductions = net (net is floored at zero when deductions exceed gross)", arithmetic)
	}
	out = append(out, row("arithmetic", "Gross - deductions = net", derived, &n, status, note, len(posted)))

	// The money: Salary Payable cleared since the period began, and how much
	// of each clearing document's bank leg is matched to a statement line.
	var paidPaise, bankLegPaise, bankMatchedPaise, balancePaise int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(debit), 0) FROM %s.gl_postings
		WHERE account_code = '2400' AND debit > 0 AND created_at >= $1::date`, schema), start).Scan(&paidPaise); err != nil {
		return nil, err
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(b.credit), 0), COALESCE(SUM(b.credit) FILTER (WHERE b.matched_statement_line_id IS NOT NULL), 0)
		FROM %s.gl_postings b
		WHERE b.credit > 0 AND b.account_code IN (`+bankGLAccountsSQL(schema)+`)
		  AND (b.document_type, b.document_id) IN (SELECT p.document_type, p.document_id FROM %s.gl_postings p
		       WHERE p.account_code = '2400' AND p.debit > 0 AND p.created_at >= $1::date)`, schema, schema), start).Scan(&bankLegPaise, &bankMatchedPaise); err != nil {
		return nil, err
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit), 0) - COALESCE(SUM(debit), 0) FROM %s.gl_postings WHERE account_code = '2400'`, schema)).Scan(&balancePaise); err != nil {
		return nil, err
	}
	paid := PaiseToRupees(paidPaise)
	bankLeg, bankMatched := PaiseToRupees(bankLegPaise), PaiseToRupees(bankMatchedPaise)
	out = append(out, row("paid", "Salary paid (2400 cleared since period start)", n, &paid, "Info",
		"net pay of this period's payslips vs payments that cleared Salary Payable since the period began - a later payment or an earlier period's arrears moves this", len(posted)))
	payStatus := "Tied"
	if math.Abs(bankLeg-bankMatched) >= 0.005 {
		payStatus = "Difference"
	}
	out = append(out, row("bank", "Payments confirmed on the bank statement", bankLeg, &bankMatched, payStatus,
		"bank side of those salary payments vs the part matched to a bank statement line (Bank Reconciliation)", 0))
	owed := PaiseToRupees(balancePaise)
	out = append(out, row("owed", "Still owed to employees (2400 balance now)", owed, nil, "Info",
		"Salary Payable at this moment, all periods", 0))
	draftStatus := "Tied"
	if draftCount > 0 {
		draftStatus = "Difference"
	}
	out = append(out, row("unposted", "Payslips not yet posted to the GL", draftNet, nil, draftStatus,
		fmt.Sprintf("%d payslip(s) in the period are still %s - they are in no figure above", draftCount, "Draft"), draftCount))
	return out, nil
}

// bankGLAccountsSQL lists every BankAccount's GL code plus 1100 Cash/Bank.
func bankGLAccountsSQL(schema string) string {
	return fmt.Sprintf(`SELECT '1100' UNION SELECT COALESCE(data->>'gl_account_code', '') FROM %s.documents WHERE doctype = 'BankAccount'`, schema)
}

func joinQuoted(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		b, _ := json.Marshal(id)
		out += string(b)
	}
	return out
}

// payrollReconciliationDrillDown lists what is behind one step.
func payrollReconciliationDrillDown(tenantID, step, start, end string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	switch step {
	case "paid", "bank":
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT p.created_at, p.document_type, p.document_id, p.account_code, p.debit, p.credit, COALESCE(p.matched_statement_line_id, ''), COALESCE(p.posted_by, '')
			FROM %s.gl_postings p
			WHERE (p.document_type, p.document_id) IN (SELECT q.document_type, q.document_id FROM %s.gl_postings q
			       WHERE q.account_code = '2400' AND q.debit > 0 AND q.created_at >= $1::date)
			ORDER BY p.created_at, p.document_id`, schema, schema), start)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var at time.Time
			var docType, docID, account, matched, by string
			var debit, credit int64
			if err := rows.Scan(&at, &docType, &docID, &account, &debit, &credit, &matched, &by); err != nil {
				return nil, err
			}
			out = append(out, map[string]interface{}{
				"posted_at": at, "document_type": docType, "document_id": docID, "account_code": account,
				"debit": PaiseToRupees(debit), "credit": PaiseToRupees(credit), "statement_line": matched, "posted_by": by,
			})
		}
		return out, rows.Err()
	}
	slips, err := loadPayrollSlips(schema, start, end)
	if err != nil {
		return nil, err
	}
	for _, s := range slips {
		if step == "unposted" && s.status == "Posted" {
			continue
		}
		if step != "unposted" && s.status != "Posted" {
			continue
		}
		r := map[string]interface{}{
			"payslip": s.id, "employee_id": s.employee, "period_to": s.periodTo, "status": s.status,
			"gross_pay": s.gross, "pf": s.pf, "esi": s.esi, "pt": s.pt, "tds": s.tds, "loan": s.loan, "net_pay": s.net,
		}
		if step == "arithmetic" {
			if math.Abs(math.Max(s.gross-s.pf-s.esi-s.pt-s.tds-s.loan, 0)-s.net) < 0.01 {
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "payroll-reconciliation", Label: "Payroll Reconciliation (gross to bank to GL)", Category: "HR",
		Columns: []ReportColumn{
			{Key: "step", Label: "Step"}, {Key: "label", Label: "What is compared"},
			{Key: "payslips", Label: "Payslips"},
			{Key: "source_total", Label: "Source total", Sensitive: true},
			{Key: "gl_total", Label: "Compared with", Sensitive: true},
			{Key: "difference", Label: "Difference", Sensitive: true},
			{Key: "status", Label: "Status"}, {Key: "note", Label: "Note"},
		},
		Params: []ReportParam{
			{Key: "start", Label: "Pay periods ending from", Type: "date", Required: true},
			{Key: "end", Label: "Pay periods ending to", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetPayrollReconciliation(tenantID, params["start"], params["end"])
		},
		DrillDown: func(tenantID, rowKey string, params map[string]string) ([]map[string]interface{}, error) {
			return payrollReconciliationDrillDown(tenantID, rowKey, params["start"], params["end"])
		},
	})
}
