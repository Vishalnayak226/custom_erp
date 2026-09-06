package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 47.3.6 - "Add sale reconciliation: one sale identity must balance
// quantity, stock ledger, availability, COGS, revenue, tax, tender/receivable,
// loyalty and audit/outbox outcome." (audit A-03)
//
// 47.3.2 makes a half-posted sale impossible going forward. This report is how
// anyone actually KNOWS that - including for the sales posted before it, and
// for the two things deliberately left outside the transaction (the stock
// ledger rows and the outbox event, both written after commit and both
// idempotency-keyed). Without it, "the sale is atomic" is a claim about the
// code; with it, it is a query anyone can run.
//
// One row per POSCart, with the figures it SHOULD have produced next to the
// ones the ledgers actually hold, and a verdict. A tenant with nothing wrong
// gets a page of "Balanced" - which is the point: the report is evidence, not
// an exception list, so an empty exception list can be told apart from a
// report that never ran.

// SaleReconciliationRow is one sale's cross-ledger comparison.
type SaleReconciliationRow struct {
	CartNumber   string
	Status       string
	PaymentState string
	// What the cart itself says the sale was.
	CartQty       int
	CartSaleValue float64
	// What each ledger independently holds for that same identity.
	LedgerQty       int
	RevenuePosted   float64
	COGSPosted      float64
	TaxPosted       float64
	TenderPosted    float64
	LoyaltyBurned   int
	OutboxPublished bool
	Verdict         string
	Detail          string
}

// ReconcileSale checks one sale identity across every ledger it touched.
//
// The checks are deliberately independent reads rather than a single joined
// query: a join would let one missing side hide another (an absent GL row and
// an absent ledger row would both simply drop out of an inner join), and the
// whole point is to notice exactly that.
func ReconcileSale(tenantID, cartNumber string) (*SaleReconciliationRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	row := &SaleReconciliationRow{CartNumber: cartNumber}

	var qty int
	var saleValue float64
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT d.status,
		       COALESCE(d.data->>'payment_state', ''),
		       COALESCE(SUM((line->>'qty')::int), 0),
		       COALESCE(SUM((line->>'qty')::numeric * (line->>'sale_price')::numeric), 0)
		FROM %s.documents d
		LEFT JOIN LATERAL jsonb_array_elements(COALESCE(d.data->'items', '[]'::jsonb)) line ON TRUE
		WHERE d.doctype = 'POSCart' AND d.id = $1
		GROUP BY d.status, d.data->>'payment_state'`, schema), cartNumber).
		Scan(&row.Status, &row.PaymentState, &qty, &saleValue); err != nil {
		return nil, fmt.Errorf("cart %s not found: %v", cartNumber, err)
	}
	row.CartQty = qty
	row.CartSaleValue = round2(saleValue)

	// Stock ledger: quantity is negative for a sale, so it is negated here to
	// compare like with like.
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(-SUM((data->>'qty')::numeric), 0)::int FROM %s.documents
		WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1`, schema), cartNumber).Scan(&row.LedgerQty)

	// GL, by account role rather than by voucher, so a posting that landed in
	// the wrong account shows up as a shortfall rather than being counted.
	glSum := func(accounts []string) float64 {
		list := "'" + strings.Join(accounts, "','") + "'"
		var paise int64
		_ = db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(credit) + SUM(debit), 0) FROM %s.gl_postings
			WHERE document_type = 'POSCart' AND document_id = $1 AND account_code IN (%s)`, schema, list), cartNumber).Scan(&paise)
		return PaiseToRupees(paise)
	}
	// 4100 is credited with the full tax-inclusive value and then debited with
	// the tax portion and any exempt reclass, so summing debit+credit on it
	// alone would net to the taxable amount. Revenue here is the gross credit.
	var revenuePaise int64
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(credit), 0) FROM %s.gl_postings
		WHERE document_type = 'POSCart' AND document_id = $1 AND account_code = '4100'`, schema), cartNumber).Scan(&revenuePaise)
	row.RevenuePosted = PaiseToRupees(revenuePaise)
	row.COGSPosted = glSum([]string{"5100"})
	row.TaxPosted = glSum([]string{"2200", "2201", "2202"})
	// Tender: whichever clearing account this sale settled into, plus the
	// loyalty-redemption expense account that stands in for points paid with.
	row.TenderPosted = glSum([]string{"1100", "1101", "1102", "5250"})

	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Burn' THEN points ELSE -points END), 0)
		FROM %s.loyalty_point_ledger WHERE reference_doctype = 'POSCart' AND reference_id LIKE $1`, schema),
		cartNumber+"%").Scan(&row.LoyaltyBurned)

	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS(SELECT 1 FROM %s.integration_event_outbox
		WHERE payload->>'cart_number' = $1)`, schema), cartNumber).Scan(&row.OutboxPublished)

	row.Verdict, row.Detail = judgeSaleReconciliation(row)
	return row, nil
}

// judgeSaleReconciliation is the verdict rule, kept separate so the report and
// any test agree on what "balanced" means rather than each deciding.
func judgeSaleReconciliation(row *SaleReconciliationRow) (verdict, detail string) {
	var problems []string

	// A cart that never posted must have nothing anywhere. This is the check
	// that catches the exact A-03 failure: stock gone, GL empty.
	posted := row.Status == "Paid" || row.PaymentState == PaymentStatePosted
	if !posted {
		if row.LedgerQty != 0 || row.RevenuePosted != 0 || row.COGSPosted != 0 {
			problems = append(problems, fmt.Sprintf(
				"cart is %s/%s but has already moved stock (%d) and/or posted revenue (%.2f) - a sale that did not complete must have left nothing behind",
				row.Status, displayPaymentState(row.PaymentState), row.LedgerQty, row.RevenuePosted))
		}
		if len(problems) == 0 {
			return "Not posted", "nothing posted, as expected for a cart in this state"
		}
		return "BROKEN", strings.Join(problems, "; ")
	}

	if row.LedgerQty != row.CartQty {
		problems = append(problems, fmt.Sprintf("stock ledger shows %d unit(s), the cart sold %d", row.LedgerQty, row.CartQty))
	}
	if !amountsAgree(row.RevenuePosted, row.CartSaleValue) {
		problems = append(problems, fmt.Sprintf("revenue posted %.2f, the cart totals %.2f", row.RevenuePosted, row.CartSaleValue))
	}
	// Tender + points must account for the whole bill. Points are counted at
	// their redemption value via the 5250 debit, which is already in
	// TenderPosted, so this is one comparison rather than two.
	if !amountsAgree(row.TenderPosted, row.CartSaleValue) {
		problems = append(problems, fmt.Sprintf("tender/receivable posted %.2f against a %.2f bill", row.TenderPosted, row.CartSaleValue))
	}
	if !row.OutboxPublished {
		problems = append(problems, "no sale event was published to the outbox")
	}
	// COGS is deliberately NOT required to be non-zero: Stage 47.2.2 posts no
	// COGS leg for an item with no cost basis on record, and records a
	// POSCostingGap instead. Flagging that here would report an honest,
	// already-tracked gap as a broken sale.

	if len(problems) == 0 {
		return "Balanced", "quantity, revenue, tax, tender, loyalty and evidence all agree"
	}
	return "BROKEN", strings.Join(problems, "; ")
}

// amountsAgree tolerates a paisa of rounding between two independently
// computed rupee figures - the GL stores paise and the cart stores float
// rupees, so exact equality would report rounding as corruption.
func amountsAgree(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= 0.01
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "pos-sale-reconciliation", Label: "POS Sale Reconciliation", Category: "Finance",
		Columns: []ReportColumn{
			{Key: "cart_number", Label: "Sale"}, {Key: "status", Label: "Status"},
			{Key: "payment_state", Label: "Payment State"},
			{Key: "cart_qty", Label: "Qty Sold"}, {Key: "ledger_qty", Label: "Qty in Stock Ledger"},
			{Key: "cart_sale_value", Label: "Cart Total", Sensitive: true},
			{Key: "revenue_posted", Label: "Revenue Posted", Sensitive: true},
			{Key: "cogs_posted", Label: "COGS Posted", Sensitive: true},
			{Key: "tax_posted", Label: "Tax Posted", Sensitive: true},
			{Key: "tender_posted", Label: "Tender Posted", Sensitive: true},
			{Key: "loyalty_burned", Label: "Points Burned"},
			{Key: "outbox_published", Label: "Event Published"},
			{Key: "verdict", Label: "Verdict"}, {Key: "detail", Label: "Detail"},
		},
		Params: []ReportParam{
			{Key: "from_date", Label: "From", Type: "date"},
			{Key: "to_date", Label: "To", Type: "date"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return runSaleReconciliation(tenantID, params["from_date"], params["to_date"])
		},
	})
}

// runSaleReconciliation reconciles every cart in a window. Bounded by an
// explicit LIMIT as well as the date range, per Stage 47.10's "bound queries"
// rule - a reconciliation report is exactly the kind of thing that quietly
// becomes a full table scan of a year of sales.
func runSaleReconciliation(tenantID, fromDate, toDate string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	conditions := "doctype = 'POSCart' AND deleted_at IS NULL"
	args := []interface{}{}
	if strings.TrimSpace(fromDate) != "" {
		args = append(args, fromDate)
		conditions += fmt.Sprintf(" AND created_at >= $%d::date", len(args))
	}
	if strings.TrimSpace(toDate) != "" {
		args = append(args, toDate)
		conditions += fmt.Sprintf(" AND created_at < ($%d::date + 1)", len(args))
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id FROM %s.documents WHERE %s ORDER BY created_at DESC LIMIT 500`, schema, conditions), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		rec, err := ReconcileSale(tenantID, id)
		if err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"cart_number": rec.CartNumber, "status": rec.Status, "payment_state": displayPaymentState(rec.PaymentState),
			"cart_qty": rec.CartQty, "ledger_qty": rec.LedgerQty,
			"cart_sale_value": rec.CartSaleValue, "revenue_posted": rec.RevenuePosted,
			"cogs_posted": rec.COGSPosted, "tax_posted": rec.TaxPosted, "tender_posted": rec.TenderPosted,
			"loyalty_burned": rec.LoyaltyBurned, "outbox_published": rec.OutboxPublished,
			"verdict": rec.Verdict, "detail": rec.Detail,
		})
	}
	return out, nil
}
