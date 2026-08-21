package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// Stage 26.6: P&L, Balance Sheet, Cash Flow, GL drill-down/customer-ledger/
// tax-ledger, and the statutory audit export - all new ReportDefinition
// entries off the existing gl_postings/gl_accounts tables, following the
// same "register a function" pattern Trial Balance/Ageing already use
// (engines/report_registry.go). No new routes/handlers/frontend: the
// generic report catalog surfaces every one of these for free, and the
// statutory export gets async CSV export for free via the existing
// CreateReportExportJob/StartReportExportWorker machinery (engines/report_export.go).

// Date-range convention for every gl_postings report in this file (and in
// gl_cost_center.go / reports.go / report_definitions.go, which follow it):
// the inclusive [startDate, endDate] day range is expressed as the half-open
// timestamp range `created_at >= $start::date AND created_at < ($end::date + 1)`
// rather than the more obvious `created_at::date BETWEEN $start AND $end`.
// The two are exactly equivalent for a `timestamp` column, but the cast form
// wraps the indexed column in a function call, so the planner cannot use it as
// a range seek - it has to read every posting for the account and discard the
// out-of-range ones with a filter. The half-open form lets the date land in
// the Index Cond of idx_gl_postings_account_created
// (db/migrations_stage29_gl_postings_reporting_index.sql). Measured on a 1M-row
// gl_postings: P&L for one quarter went 87ms/2480 buffers -> 5ms/162 buffers,
// identical results. Keep new gl_postings date filters in this form.

// GetProfitAndLoss sums Revenue and Expense account activity between
// [startDate, endDate] (inclusive, "YYYY-MM-DD"). Revenue's natural balance
// is credit, Expense's is debit - both are reported as positive numbers.
func GetProfitAndLoss(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT a.account_code, a.account_name, a.account_type,
		       CASE WHEN a.account_type = 'Revenue' THEN COALESCE(SUM(p.credit), 0) - COALESCE(SUM(p.debit), 0)
		            ELSE COALESCE(SUM(p.debit), 0) - COALESCE(SUM(p.credit), 0) END AS amount
		FROM %s.gl_accounts a
		LEFT JOIN %s.gl_postings p ON a.account_code = p.account_code AND p.created_at >= $1::date AND p.created_at < ($2::date + 1)
		WHERE a.account_type IN ('Revenue', 'Expense')
		GROUP BY a.account_code, a.account_name, a.account_type
		ORDER BY a.account_type, a.account_code`, schema, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	totalRevenue, totalExpense := 0.0, 0.0
	for rows.Next() {
		var code, name, atype string
		var amountPaise int64
		if err := rows.Scan(&code, &name, &atype, &amountPaise); err != nil {
			return nil, err
		}
		// gl_postings.debit/credit are paise (Stage 45); every report in
		// this file converts to rupees at its own scan, right where the raw
		// SUM comes back, so the rest of the function is unchanged.
		amount := PaiseToRupees(amountPaise)
		if atype == "Revenue" {
			totalRevenue += amount
		} else {
			totalExpense += amount
		}
		out = append(out, map[string]interface{}{
			"account_code": code, "account_name": name, "account_type": atype, "amount": amount,
		})
	}
	out = append(out, map[string]interface{}{
		"account_code": "", "account_name": "Net Profit", "account_type": "Summary",
		"amount": totalRevenue - totalExpense,
	})
	return out, nil
}

// GetBalanceSheet reports Asset/Liability/Equity account balances as of a
// single date (all gl_postings up to and including asOfDate), each at its
// natural balance shown as a positive number. This lists account balances
// exactly as they stand in gl_postings - it does not perform a year-end
// closing entry (no such mechanism exists in this system), so Revenue/
// Expense activity for the current open period is not rolled into Equity.
// A known simplification, the same spirit as this system's other reports
// (e.g. GetPayablesAgeingReport's own documented approximation).
func GetBalanceSheet(tenantID, asOfDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if asOfDate == "" {
		return nil, fmt.Errorf("as_of date is required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT a.account_code, a.account_name, a.account_type,
		       CASE WHEN a.account_type = 'Asset' THEN COALESCE(SUM(p.debit), 0) - COALESCE(SUM(p.credit), 0)
		            ELSE COALESCE(SUM(p.credit), 0) - COALESCE(SUM(p.debit), 0) END AS amount
		FROM %s.gl_accounts a
		LEFT JOIN %s.gl_postings p ON a.account_code = p.account_code AND p.created_at < ($1::date + 1)
		WHERE a.account_type IN ('Asset', 'Liability', 'Equity')
		GROUP BY a.account_code, a.account_name, a.account_type
		ORDER BY a.account_type, a.account_code`, schema, schema), asOfDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	totals := map[string]float64{}
	for rows.Next() {
		var code, name, atype string
		var amountPaise int64
		if err := rows.Scan(&code, &name, &atype, &amountPaise); err != nil {
			return nil, err
		}
		amount := PaiseToRupees(amountPaise)
		totals[atype] += amount
		out = append(out, map[string]interface{}{
			"account_code": code, "account_name": name, "account_type": atype, "amount": amount,
		})
	}
	for _, atype := range []string{"Asset", "Liability", "Equity"} {
		out = append(out, map[string]interface{}{
			"account_code": "", "account_name": fmt.Sprintf("Total %s", atype), "account_type": "Summary",
			"amount": totals[atype],
		})
	}
	return out, nil
}

// cashFlowActivityByDocType heuristically classifies which activity
// section a gl_postings.document_type belongs to for the cash flow
// statement below - this system has no dedicated activity-type field, so
// anything not listed here defaults to "Operating".
var cashFlowActivityByDocType = map[string]string{
	"Asset": "Investing",
}

func cashFlowActivity(docType string) string {
	if a, ok := cashFlowActivityByDocType[docType]; ok {
		return a
	}
	return "Operating"
}

// GetCashFlowStatement summarises cash movement on the Cash/Bank clearing
// account (1100) between [startDate, endDate], bucketed into Operating/
// Investing/Financing by document_type. A heuristic categorisation, not a
// full indirect-method statement. BankStatementLine (Stage 20c bank
// reconciliation) is a second, narrower source of the same underlying cash
// fact once a tenant has actually uploaded bank statements; gl_postings is
// used here since it is always populated regardless of that.
func GetCashFlowStatement(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT document_type, COALESCE(SUM(debit), 0) AS cash_in, COALESCE(SUM(credit), 0) AS cash_out
		FROM %s.gl_postings WHERE account_code = '1100' AND created_at >= $1::date AND created_at < ($2::date + 1)
		GROUP BY document_type ORDER BY document_type`, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type flow struct{ in, out float64 }
	byActivity := map[string]*flow{}
	order := []string{"Operating", "Investing", "Financing"}
	for _, a := range order {
		byActivity[a] = &flow{}
	}
	for rows.Next() {
		var docType string
		var inPaise, outPaise int64
		if err := rows.Scan(&docType, &inPaise, &outPaise); err != nil {
			return nil, err
		}
		in, out := PaiseToRupees(inPaise), PaiseToRupees(outPaise)
		act := cashFlowActivity(docType)
		f, ok := byActivity[act]
		if !ok {
			f = &flow{}
			byActivity[act] = f
			order = append(order, act)
		}
		f.in += in
		f.out += out
	}

	var out []map[string]interface{}
	netChange := 0.0
	for _, act := range order {
		f := byActivity[act]
		net := f.in - f.out
		netChange += net
		out = append(out, map[string]interface{}{
			"activity": act, "cash_in": f.in, "cash_out": f.out, "net_change": net,
		})
	}
	out = append(out, map[string]interface{}{
		"activity": "Net Change in Cash", "cash_in": nil, "cash_out": nil, "net_change": netChange,
	})
	return out, nil
}

// GetCustomerLedgerReport mirrors GetVendorLedgerReport (engines/reports.go)
// for the receivables side - SalesInvoice grouped/filterable by customer.
func GetCustomerLedgerReport(tenantID, customerFilter string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Stage 30.2.3: every text projection is COALESCE'd. Found live - this
	// report 503'd outright because one real SalesInvoice had no
	// invoice_number, and a JSONB `->>` on an absent key returns NULL, which
	// cannot scan into a plain Go string. A report must not disappear because
	// one row is incomplete; the incomplete value renders as blank instead.
	query := fmt.Sprintf(`
		SELECT id, COALESCE(data->>'customer', '') AS customer,
		       COALESCE(data->>'invoice_number', '') AS invoice_number,
		       COALESCE((data->>'total_amount')::numeric, 0) AS total_amount,
		       COALESCE(status, '') AS status, created_at
		FROM %s.documents WHERE doctype = 'SalesInvoice'`, schema)
	var args []interface{}
	if customerFilter != "" {
		query += " AND data->>'customer' = $1"
		args = append(args, customerFilter)
	}
	query += " ORDER BY data->>'customer', created_at DESC"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var id, customer, invoiceNumber, status string
		var totalAmount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &customer, &invoiceNumber, &totalAmount, &status, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"id": id, "customer": customer, "invoice_number": invoiceNumber,
			"total_amount": totalAmount, "status": status, "created_at": createdAt,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// taxLedgerAccountCodes are the GST accounts (Stage 17.5's output accounts
// plus the pre-existing input credit account) this report lists postings
// against - fixed, developer-known codes, not user input.
var taxLedgerAccountCodes = "'1500', '2200', '2201', '2202'"

// GetTaxLedgerReport lists every gl_postings entry against a GST account,
// optionally filtered to [startDate, endDate].
func GetTaxLedgerReport(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`
		SELECT p.account_code, a.account_name, p.document_type, p.document_id, p.debit, p.credit, p.created_at
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE p.account_code IN (%s)`, schema, schema, taxLedgerAccountCodes)
	var args []interface{}
	if startDate != "" && endDate != "" {
		query += " AND p.created_at >= $1::date AND p.created_at < ($2::date + 1)"
		args = append(args, startDate, endDate)
	}
	query += " ORDER BY p.created_at ASC"

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var accountCode, accountName, docType, docID string
		var debitPaise, creditPaise int64
		var createdAt time.Time
		if err := rows.Scan(&accountCode, &accountName, &docType, &docID, &debitPaise, &creditPaise, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"account_code": accountCode, "account_name": accountName, "document_type": docType,
			"document_id": docID, "debit": PaiseToRupees(debitPaise), "credit": PaiseToRupees(creditPaise), "created_at": createdAt,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// GetStatutoryGLExport returns every gl_postings row (with its account's
// name/type) for [startDate, endDate] - a structured full-GL export for a
// closed accounting period. Registered as a normal report below, which
// gets async CSV export "for free" via the existing
// CreateReportExportJob/StartReportExportWorker machinery.
func GetStatutoryGLExport(tenantID, startDate, endDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if startDate == "" || endDate == "" {
		return nil, fmt.Errorf("start and end are required")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.posting_id, p.account_code, a.account_name, a.account_type,
		       p.document_type, p.document_id, p.debit, p.credit, p.created_at
		FROM %s.gl_postings p JOIN %s.gl_accounts a ON a.account_code = p.account_code
		WHERE p.created_at >= $1::date AND p.created_at < ($2::date + 1)
		ORDER BY p.created_at ASC`, schema, schema), startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var postingID, accountCode, accountName, accountType, docType, docID string
		var debitPaise, creditPaise int64
		var createdAt time.Time
		if err := rows.Scan(&postingID, &accountCode, &accountName, &accountType, &docType, &docID, &debitPaise, &creditPaise, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"posting_id": postingID, "account_code": accountCode, "account_name": accountName,
			"account_type": accountType, "document_type": docType, "document_id": docID,
			"debit": PaiseToRupees(debitPaise), "credit": PaiseToRupees(creditPaise), "created_at": createdAt,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "profit-and-loss", Label: "Profit & Loss", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "account_code", Label: "Account Code"}, {Key: "account_name", Label: "Account Name"},
			{Key: "account_type", Label: "Type"}, {Key: "amount", Label: "Amount", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetProfitAndLoss(tenantID, params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "balance-sheet", Label: "Balance Sheet", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "account_code", Label: "Account Code"}, {Key: "account_name", Label: "Account Name"},
			{Key: "account_type", Label: "Type"}, {Key: "amount", Label: "Amount", Sensitive: true},
		},
		Params: []ReportParam{{Key: "as_of", Label: "As Of", Type: "date", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetBalanceSheet(tenantID, params["as_of"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "cash-flow-statement", Label: "Cash Flow Statement", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "activity", Label: "Activity"}, {Key: "cash_in", Label: "Cash In", Sensitive: true},
			{Key: "cash_out", Label: "Cash Out", Sensitive: true}, {Key: "net_change", Label: "Net Change", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From", Type: "date", Required: true},
			{Key: "end", Label: "To", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetCashFlowStatement(tenantID, params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "gl-drilldown", Label: "GL Drill-down (by Account)", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "document_type", Label: "Document Type"}, {Key: "document_id", Label: "Document ID"},
			{Key: "debit", Label: "Debit", Sensitive: true}, {Key: "credit", Label: "Credit", Sensitive: true},
			{Key: "balance", Label: "Running Balance", Sensitive: true}, {Key: "created_at", Label: "Date"},
		},
		Params: []ReportParam{{Key: "account_code", Label: "Account Code", Type: "text", Required: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return getGLAccountBook(tenantID, params["account_code"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "customer-ledger", Label: "Customer Ledger", Category: "Sales",
		Columns: []ReportColumn{
			{Key: "id", Label: "Invoice ID"}, {Key: "customer", Label: "Customer"}, {Key: "invoice_number", Label: "Invoice Number"},
			{Key: "total_amount", Label: "Total Amount", Sensitive: true}, {Key: "status", Label: "Status"},
			{Key: "created_at", Label: "Date"},
		},
		Params: []ReportParam{{Key: "customer", Label: "Customer (optional)", Type: "text"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetCustomerLedgerReport(tenantID, params["customer"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "tax-ledger", Label: "Tax Ledger", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "account_code", Label: "Account Code"}, {Key: "account_name", Label: "Account Name"},
			{Key: "document_type", Label: "Document Type"}, {Key: "document_id", Label: "Document ID"},
			{Key: "debit", Label: "Debit", Sensitive: true}, {Key: "credit", Label: "Credit", Sensitive: true},
			{Key: "created_at", Label: "Date"},
		},
		Params: []ReportParam{
			{Key: "start", Label: "From (optional)", Type: "date"},
			{Key: "end", Label: "To (optional)", Type: "date"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetTaxLedgerReport(tenantID, params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "statutory-gl-export", Label: "Statutory Audit Export (Full GL)", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "posting_id", Label: "Posting ID"}, {Key: "account_code", Label: "Account Code"},
			{Key: "account_name", Label: "Account Name"}, {Key: "account_type", Label: "Type"},
			{Key: "document_type", Label: "Document Type"}, {Key: "document_id", Label: "Document ID"},
			{Key: "debit", Label: "Debit", Sensitive: true}, {Key: "credit", Label: "Credit", Sensitive: true},
			{Key: "created_at", Label: "Date"},
		},
		Params: []ReportParam{
			{Key: "start", Label: "Period Start", Type: "date", Required: true},
			{Key: "end", Label: "Period End", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetStatutoryGLExport(tenantID, params["start"], params["end"])
		},
	})
}
