package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 140.12 reports: the Day Book and the Z-report (140.12.13) - only the
// cash book and bank book existed - and the customer receipt register
// (140.12.5).

func init() {
	RegisterReport(ReportDefinition{
		ID: "day-book", Label: "Day Book", Category: "Finance", StoreKey: "-",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{
			{Key: "date", Label: "Date"}, {Key: "voucher_type", Label: "Voucher type"}, {Key: "voucher", Label: "Voucher"},
			{Key: "account", Label: "Account"}, {Key: "account_name", Label: "Account name"},
			{Key: "debit", Label: "Debit", Sensitive: true}, {Key: "credit", Label: "Credit", Sensitive: true}, {Key: "posted_by", Label: "By"},
		},
		Run: dayBookReport,
	})
	RegisterReport(ReportDefinition{
		ID: "z-report", Label: "Z-report (till close)", Category: "Sales", StoreKey: "store",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}, {Key: "store", Label: "Store", Type: "text"}},
		Columns: []ReportColumn{
			{Key: "closed_at", Label: "Closed"}, {Key: "store", Label: "Store"}, {Key: "till", Label: "Till"}, {Key: "cashier", Label: "Cashier"},
			{Key: "bills", Label: "Bills"}, {Key: "first_bill", Label: "First bill"}, {Key: "last_bill", Label: "Last bill"},
			{Key: "opening", Label: "Opening float"}, {Key: "cash_sales", Label: "Cash sales"}, {Key: "cash_refunds", Label: "Cash refunds"},
			{Key: "advances_in", Label: "Advances in"}, {Key: "advances_out", Label: "Advances refunded"},
			{Key: "paid_in", Label: "Paid in"}, {Key: "paid_out", Label: "Paid out"}, {Key: "safe_drops", Label: "Safe drops"},
			{Key: "expected", Label: "Expected cash"}, {Key: "counted", Label: "Counted cash"}, {Key: "variance", Label: "Variance"},
			{Key: "tenders", Label: "Tenders"}, {Key: "card_declared", Label: "Card declared"}, {Key: "card_difference", Label: "Card difference"},
			{Key: "upi_declared", Label: "UPI declared"}, {Key: "upi_difference", Label: "UPI difference"}, {Key: "status", Label: "Status"},
		},
		Run: zReport,
	})
	RegisterReport(ReportDefinition{
		ID: "customer-receipts", Label: "Customer receipts", Category: "Finance", StoreKey: "store",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}, {Key: "customer", Label: "Customer", Type: "text"}},
		Columns: []ReportColumn{
			{Key: "date", Label: "Date"}, {Key: "number", Label: "Receipt"}, {Key: "customer", Label: "Customer"}, {Key: "store", Label: "Store"},
			{Key: "mode", Label: "Mode"}, {Key: "reference", Label: "Reference"}, {Key: "amount", Label: "Amount"}, {Key: "tds", Label: "TDS"},
			{Key: "allocated", Label: "To invoices"}, {Key: "invoices", Label: "Invoices"}, {Key: "on_account", Label: "On account"}, {Key: "advance", Label: "Advance no."},
		},
		Run: customerReceiptsReport,
	})
}

// dayBookReport is every GL line of the period, voucher by voucher.
func dayBookReport(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"])
	if from == "" {
		from = BusinessToday()
	}
	if to == "" {
		to = from
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT g.created_at, g.document_type, g.document_id, g.account_code, COALESCE(a.account_name, ''), g.debit, g.credit, COALESCE(g.posted_by, '')
		FROM %s.gl_postings g LEFT JOIN %s.gl_accounts a ON a.account_code = g.account_code
		WHERE g.created_at >= public.erp_day_start($1::date) AND g.created_at < public.erp_day_start($2::date + 1)
		ORDER BY g.created_at, g.document_type, g.document_id, g.debit DESC, g.account_code
		LIMIT 20000`, schema, schema), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var at time.Time
		var docType, docID, account, name, by string
		var debit, credit int64
		if err := rows.Scan(&at, &docType, &docID, &account, &name, &debit, &credit, &by); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"date": at.Format("2006-01-02 15:04"), "voucher_type": docType, "voucher": docID,
			"account": account, "account_name": name, "debit": PaiseToRupees(debit), "credit": PaiseToRupees(credit), "posted_by": by})
	}
	return out, rows.Err()
}

// zReport is each till closed in the period: its whole drawer, the counted
// cash and the variance, the tenders and the bill range.
func zReport(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"])
	if from == "" {
		from = BusinessToday()
	}
	if to == "" {
		to = from
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, updated_at, data FROM %s.documents
		WHERE doctype = 'POSSession' AND deleted_at IS NULL AND status IN ('Closed', 'Pending Approval')
		  AND updated_at >= public.erp_day_start($1::date) AND updated_at < public.erp_day_start($2::date + 1)
		  AND ($3 = '' OR data->>'location' = $3)
		ORDER BY updated_at`, schema), from, to, strings.TrimSpace(p["store"]))
	if err != nil {
		return nil, err
	}
	type till struct {
		id   string
		at   time.Time
		data map[string]interface{}
	}
	var tills []till
	for rows.Next() {
		var t till
		var raw []byte
		if err := rows.Scan(&t.id, &t.at, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(raw, &t.data)
		tills = append(tills, t)
	}
	rows.Close()
	out := []map[string]interface{}{}
	for _, t := range tills {
		pos, err := SessionCashPosition(tenantID, t.id)
		if err != nil {
			return nil, err
		}
		var bills int
		var first, last string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COALESCE(MIN(data->>'bill_number'), ''), COALESCE(MAX(data->>'bill_number'), '')
			FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'pos_session' = $1`, schema), t.id).Scan(&bills, &first, &last)
		modes := make([]string, 0, len(pos.Tenders))
		for k := range pos.Tenders {
			modes = append(modes, k)
		}
		sort.Strings(modes)
		parts := make([]string, 0, len(modes))
		for _, k := range modes {
			parts = append(parts, fmt.Sprintf("%s %.2f", k, pos.Tenders[k]))
		}
		out = append(out, map[string]interface{}{
			"closed_at": t.at.Format("2006-01-02 15:04"), "store": pos.Location, "till": t.id, "cashier": pos.Cashier,
			"bills": bills, "first_bill": first, "last_bill": last,
			"opening": pos.Opening, "cash_sales": pos.CashSales, "cash_refunds": pos.CashRefunds,
			"advances_in": pos.AdvancesIn, "advances_out": pos.AdvancesOut, "paid_in": pos.PaidIn, "paid_out": pos.PaidOut, "safe_drops": pos.SafeDrops,
			"expected": numFromInterface(t.data["expected_cash"]), "counted": numFromInterface(t.data["closing_counted_cash"]),
			"variance": numFromInterface(t.data["variance"]), "tenders": strings.Join(parts, " · "), "status": pos.Status,
			"card_declared": t.data["declared_card"], "card_difference": t.data["card_difference"],
			"upi_declared": t.data["declared_upi"], "upi_difference": t.data["upi_difference"],
		})
	}
	return out, nil
}

// customerReceiptsReport is the receipt register.
func customerReceiptsReport(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	list, err := ListCustomerReceipts(tenantID, p["customer"], 500)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"])
	out := []map[string]interface{}{}
	for _, r := range list {
		if (from != "" && r.Date < from) || (to != "" && r.Date > to) {
			continue
		}
		refs := make([]string, 0, len(r.Invoices))
		for _, l := range r.Invoices {
			refs = append(refs, fmt.Sprintf("%s %.2f", l.Ref, l.Amount))
		}
		out = append(out, map[string]interface{}{"date": r.Date, "number": r.Number, "customer": r.Customer, "store": r.Location,
			"mode": r.Mode, "reference": r.Reference, "amount": r.Amount, "tds": r.TDS, "allocated": r.Allocated,
			"invoices": strings.Join(refs, ", "), "on_account": r.OnAccountAmt, "advance": r.OnAccount})
	}
	return out, nil
}
