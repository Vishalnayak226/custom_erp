package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// Ask 176 item 176.1: treasury cash position.
//
// One table for "how much cash do we have and what is about to move": the
// balance of every cash and bank account as of a day (the 1100 / 1101 / 1102
// cash-family accounts plus each active bank account's ledger account), the
// open customer invoices and supplier bills due in the next four weeks and
// after, and the running position those leave. Due amounts read the same
// open statuses and outstanding amount as the Cash Flow Forecast
// (budgeting.go); an invoice already overdue, or with no due date, falls in
// "Now" rather than being left out.

const cashPositionReportID = "cash-position"

// cashPositionBuckets are the report's time columns, in order.
var cashPositionBuckets = []string{"now", "w1", "w2", "w3", "w4", "later"}

func init() {
	money := func(key, label string) ReportColumn {
		return ReportColumn{Key: key, Label: label, Sensitive: true, FinanceSees: true}
	}
	RegisterReport(ReportDefinition{
		ID: cashPositionReportID, Label: "Cash position", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "section", Label: "Section"}, {Key: "item", Label: "Item"},
			money("now", "Now"), money("w1", "Week 1"), money("w2", "Week 2"), money("w3", "Week 3"),
			money("w4", "Week 4"), money("later", "Later"), money("total", "Total"),
		},
		Params: []ReportParam{{Key: "as_of", Label: "As of", Type: "date"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return CashPosition(tenantID, params["as_of"])
		},
	})
}

// CashPosition builds the report as of asOf (YYYY-MM-DD; blank is today).
func CashPosition(tenantID, asOf string) ([]map[string]interface{}, error) {
	if asOf == "" {
		asOf = BusinessToday()
	}
	day, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "As of", Message: "as_of must be YYYY-MM-DD"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	out := []map[string]interface{}{}
	accounts, err := db.DB.Query(fmt.Sprintf(`
		WITH codes AS (
		     SELECT unnest(ARRAY['1100', '1101', '1102']) AS code
		     UNION
		     SELECT data->>'gl_account_code' FROM %[1]s.documents
		      WHERE doctype = 'BankAccount' AND deleted_at IS NULL AND status = 'Active'
		        AND COALESCE(data->>'gl_account_code', '') <> '')
		SELECT c.code, COALESCE(a.account_name, ''),
		       COALESCE((SELECT string_agg(DISTINCT b.data->>'bank_name', ', ') FROM %[1]s.documents b
		                  WHERE b.doctype = 'BankAccount' AND b.deleted_at IS NULL AND b.status = 'Active'
		                    AND b.data->>'gl_account_code' = c.code), ''),
		       COALESCE((SELECT SUM(p.debit) - SUM(p.credit) FROM %[1]s.gl_postings p
		                  WHERE p.account_code = c.code AND p.created_at < public.erp_day_start($1::date + 1)), 0)::bigint
		  FROM codes c LEFT JOIN %[1]s.gl_accounts a ON a.account_code = c.code
		 ORDER BY c.code`, schema), asOf)
	if err != nil {
		return nil, err
	}
	var cashPaise int64
	for accounts.Next() {
		var code, name, banks string
		var balance int64
		if err := accounts.Scan(&code, &name, &banks, &balance); err != nil {
			accounts.Close()
			return nil, err
		}
		item := code
		if name != "" {
			item += " " + name
		}
		if banks != "" {
			item += " (" + banks + ")"
		}
		cashPaise += balance
		out = append(out, cashPositionRow("Cash and bank", item, map[string]int64{"now": balance}))
	}
	accounts.Close()
	if err := accounts.Err(); err != nil {
		return nil, err
	}
	out = append(out, cashPositionRow("Cash and bank", "Total cash and bank", map[string]int64{"now": cashPaise}))

	in, err := cashPositionDue(schema, "SalesInvoice", "total_amount", "'Approved'", day)
	if err != nil {
		return nil, err
	}
	pay, err := cashPositionDue(schema, "VendorInvoice", "invoice_amount", "'Matched', 'MismatchHold', 'Pending Approval', 'Approved'", day)
	if err != nil {
		return nil, err
	}
	out = append(out, cashPositionRow("Receivables due", "Customer invoices", in))
	out = append(out, cashPositionRow("Payables due", "Supplier bills", pay))

	running := cashPaise
	position := map[string]interface{}{"section": "Position", "item": "Cash after receipts and payments"}
	for _, b := range cashPositionBuckets {
		running += in[b] - pay[b]
		position[b] = PaiseToRupees(running)
	}
	position["total"] = PaiseToRupees(running)
	return append(out, position), nil
}

// cashPositionDue buckets one doctype's open amounts by due date: overdue,
// due today or with no due date in "now", then four 7-day weeks, then later.
// Only documents created by the end of asOf count.
func cashPositionDue(schema, doctype, amountField, openStatuses string, asOf time.Time) (map[string]int64, error) {
	// amountField and openStatuses are this file's own literals, as in
	// openInvoiceDueTotalsByDate.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'due_date', ''),
		       COALESCE(NULLIF(data->>'outstanding_amount', '')::numeric, NULLIF(data->>'%s', '')::numeric, 0)::float8
		  FROM %s.documents
		 WHERE doctype = $1 AND status IN (%s) AND deleted_at IS NULL
		   AND created_at < public.erp_day_start($2::date + 1)`, amountField, schema, openStatuses),
		doctype, asOf.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]int64{}
	for rows.Next() {
		var due string
		var amount float64
		if err := rows.Scan(&due, &amount); err != nil {
			return nil, err
		}
		totals[cashPositionBucket(asOf, due)] += RupeesToPaise(amount)
	}
	return totals, rows.Err()
}

// cashPositionBucket is the column a due date falls in, as of asOf.
func cashPositionBucket(asOf time.Time, due string) string {
	d, err := time.Parse("2006-01-02", due)
	if err != nil || !d.After(asOf) {
		return "now"
	}
	days := int(d.Sub(asOf).Hours() / 24)
	if days > 28 {
		return "later"
	}
	return cashPositionBuckets[1+(days-1)/7]
}

// cashPositionRow is one report row; buckets missing from amounts are 0.
func cashPositionRow(section, item string, amounts map[string]int64) map[string]interface{} {
	row := map[string]interface{}{"section": section, "item": item}
	var total int64
	for _, b := range cashPositionBuckets {
		row[b] = PaiseToRupees(amounts[b])
		total += amounts[b]
	}
	row["total"] = PaiseToRupees(total)
	return row
}
