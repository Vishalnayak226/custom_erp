package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Control totals for the catalog's money reports (Stage 75.3.5, = 73.2.19
// pulled forward): the report's own rows, an independent sum of the same
// documents, and the ledger - over the same dates and store the report was
// run for.

func init() {
	RegisterReportControlTotals("cat-sales-register", catSalesRegisterControls)
	RegisterReportControlTotals("cat-stock-valuation-summary", catStockValuationControls)
	for _, id := range []string{"cat-gst-document-tax-detail", "cat-gstr-1-hsn-summary", "cat-gstr-1-b2b-b2c-summary"} {
		RegisterReportControlTotals(id, catGSTControls("cgst_amount", "sgst_amount", "igst_amount"))
	}
	RegisterReportControlTotals("cat-hsn-tax-summary", catGSTControls("gst_amount"))
	// AP / AR ageing: the existing subledger ties (payables open items vs 2100,
	// approved invoices vs 1300), with this report's own rows as the detail.
	RegisterReportControlTotals("cat-ap-aging", catDetailOver(payablesAgeingControlTotals, "outstanding_amount"))
	RegisterReportControlTotals("cat-ar-aging", catDetailOver(receivablesAgeingControlTotals, "outstanding_amount"))
}

func catDetailOver(fn ReportControlTotalsFunc, key string) ReportControlTotalsFunc {
	return func(tenantID string, p map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
		c, err := fn(tenantID, p, rows)
		if err != nil || c == nil {
			return c, err
		}
		c.DetailTotal = sumRowField(rows, key)
		return c.finish(), nil
	}
}

// catWindow is the report's date and store filter as SQL on documents d.
func catWindow(p map[string]string, storeField string, args *[]interface{}) string {
	var conds []string
	add := func(cond string, v interface{}) {
		*args = append(*args, v)
		conds = append(conds, fmt.Sprintf(cond, len(*args)))
	}
	if v := strings.TrimSpace(p["from"]); v != "" {
		add("d.created_at >= $%d::date", v)
	}
	if v := strings.TrimSpace(p["to"]); v != "" {
		add("d.created_at < $%d::date + 1", v)
	}
	if v := strings.TrimSpace(p["location"]); v != "" && storeField != "" {
		add("d.data->>'"+storeField+"' = ANY($%d)", pqStringArray(strings.Split(v, ",")))
	}
	if len(conds) == 0 {
		return "TRUE"
	}
	return strings.Join(conds, " AND ")
}

func catSalesRegisterControls(tenantID string, p map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	args := []interface{}{cut}
	window := catWindow(p, "location", &args)
	sales := fmt.Sprintf(`(d.doctype = 'POSCart' AND d.status = 'Paid') OR (d.doctype = 'SalesInvoice' AND d.status NOT IN ('Draft', 'Cancelled'))`)
	var summary float64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(CASE WHEN d.doctype = 'POSCart' THEN COALESCE(NULLIF(d.data->>'amount_paid', '')::numeric, 0)
		ELSE COALESCE(NULLIF(d.data->>'total_amount', '')::numeric, 0) END), 0)::float8
		FROM %s.documents d WHERE d.deleted_at IS NULL AND d.created_at <= $1 AND (%s) AND %s`, schema, sales, window), args...).Scan(&summary); err != nil {
		return nil, err
	}
	var ledgerPaise int64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(p.credit), 0) - COALESCE(SUM(p.debit), 0) FROM %[1]s.gl_postings p
		JOIN %[1]s.documents d ON d.id = p.document_id AND d.doctype = p.document_type
		WHERE p.document_type IN ('POSCart', 'SalesInvoice') AND p.account_code = ANY($%[2]d) AND p.created_at <= $1 AND d.deleted_at IS NULL AND (%[3]s) AND %[4]s`,
		schema, len(args)+1, sales, window), append(args, "{"+strings.Join(salesLedgerAccounts, ",")+"}")...).Scan(&ledgerPaise); err != nil {
		return nil, err
	}
	ledger := PaiseToRupees(ledgerPaise)
	c := &ReportControlTotals{CutOff: cutOffLabel(cut), Basis: "Paid till bills (amount paid) and issued sales invoices (invoice total), for the dates and stores asked",
		DetailTotal: sumRowField(rows, "bill_net_amount"), SummaryTotal: summary,
		LedgerTotal: &ledger, LedgerAccounts: []string{"4100-4112 Sales revenue", "2200-2202 Output GST"}}
	c.Notes = append(c.Notes, "Ledger is the revenue and output GST posted for these same bills and invoices. A difference is a sale whose posting failed or an invoice not yet posted - check POS Sale Reconciliation.")
	return c.finish(), nil
}

func catStockValuationControls(tenantID string, p map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	args := []interface{}{}
	storeCond := "TRUE"
	if v := strings.TrimSpace(p["location"]); v != "" {
		args = append(args, pqStringArray(strings.Split(v, ",")))
		storeCond = "ia.location_code = ANY($1)"
	}
	var summaryPaise float64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(ia.on_hand * COALESCE(NULLIF(ic.avg_unit_cost_paise, 0)::numeric,
			COALESCE(NULLIF(i.data->>'standard_cost', '')::numeric, 0) * 100)), 0)::float8
		FROM %[1]s.inventory_availability ia LEFT JOIN %[1]s.item_cost ic ON ic.item_code = ia.sku
		LEFT JOIN %[1]s.documents i ON i.doctype = 'Item' AND i.id = ia.sku
		WHERE (ia.on_hand <> 0 OR ia.available <> 0 OR ia.in_transit <> 0) AND %[2]s`, schema, storeCond), args...).Scan(&summaryPaise); err != nil {
		return nil, err
	}
	c := &ReportControlTotals{CutOff: cutOffLabel(cut), Basis: "On-hand quantity x moving average cost (standard cost where the item was never received)",
		DetailTotal: sumRowField(rows, "stock_value_at_purchase_price"), SummaryTotal: summaryPaise / 100}
	if len(args) == 0 {
		ledger, err := glDebitBalanceAt(tx, schema, []string{"1200"}, cut)
		if err != nil {
			return nil, err
		}
		c.LedgerTotal, c.LedgerAccounts = &ledger, []string{"1200 Inventory Control"}
		c.Notes = append(c.Notes, "Ledger is the inventory control account. A difference is stock valued at standard cost (never received, so never posted) or a movement whose posting failed - check Stock Book vs Ledger.")
	} else {
		c.Notes = append(c.Notes, "Run for selected stores: the ledger holds inventory for the whole company, so it is compared only when no store is chosen.")
	}
	return c.finish(), nil
}

// catGSTControls: the report's tax columns, the bills' and invoices' own
// stored tax, and the output GST accounts' movement in the window.
func catGSTControls(keys ...string) ReportControlTotalsFunc {
	return func(tenantID string, p map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
		schema, err := db.GetTenantSchema(tenantID)
		if err != nil {
			return nil, err
		}
		tx, cut, err := controlSnapshot(schema)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		detail := 0.0
		for _, k := range keys {
			detail += sumRowField(rows, k)
		}
		args := []interface{}{cut}
		window := catWindow(p, "location", &args)
		var summary float64
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(CASE WHEN d.doctype = 'POSCart' THEN COALESCE(NULLIF(d.data->'gst_breakdown'->>'total_tax', '')::numeric, 0)
			ELSE COALESCE(NULLIF(d.data->>'total_tax', '')::numeric, 0) END), 0)::float8
			FROM %s.documents d WHERE d.deleted_at IS NULL AND d.created_at <= $1
			AND ((d.doctype = 'POSCart' AND d.status = 'Paid') OR (d.doctype = 'SalesInvoice' AND d.status NOT IN ('Draft', 'Cancelled'))) AND %s`,
			schema, window), args...).Scan(&summary); err != nil {
			return nil, err
		}
		c := &ReportControlTotals{CutOff: cutOffLabel(cut), Basis: "Output GST of paid till bills and issued sales invoices, for the dates and stores asked",
			DetailTotal: detail, SummaryTotal: summary}
		if strings.TrimSpace(p["location"]) == "" {
			gargs := []interface{}{cut}
			gwin := "TRUE"
			if v := strings.TrimSpace(p["from"]); v != "" {
				gargs = append(gargs, v)
				gwin = fmt.Sprintf("created_at >= $%d::date", len(gargs))
			}
			if v := strings.TrimSpace(p["to"]); v != "" {
				gargs = append(gargs, v)
				gwin += fmt.Sprintf(" AND created_at < $%d::date + 1", len(gargs))
			}
			var ledgerPaise int64
			if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit), 0) - COALESCE(SUM(debit), 0) FROM %s.gl_postings
				WHERE account_code IN ('2200', '2201', '2202') AND document_type IN ('POSCart', 'SalesInvoice') AND created_at <= $1 AND %s`, schema, gwin),
				gargs...).Scan(&ledgerPaise); err != nil {
				return nil, err
			}
			ledger := PaiseToRupees(ledgerPaise)
			c.LedgerTotal, c.LedgerAccounts = &ledger, []string{"2200-2202 Output GST (sales postings)"}
		} else {
			c.Notes = append(c.Notes, "Run for selected stores: the ledger is compared only when no store is chosen.")
		}
		c.Notes = append(c.Notes, "Returns and credit notes are not in these reports (the ERP's credit notes carry no tax split); the ledger figure is sales postings only, so they do not distort it.")
		return c.finish(), nil
	}
}
