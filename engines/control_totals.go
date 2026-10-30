package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 73.2.19: report control totals - detail = summary = ledger, with a
// stated cut-off.
//
// A material report is only trustworthy when three independently computed
// figures agree:
//
//	detail   the sum of the very rows the report returned to the user
//	summary  one aggregate query over the same source documents
//	ledger   the GL balance of the control account(s) at the cut-off
//
// Summary and ledger are read inside one REPEATABLE READ snapshot, so they
// share an exact cut-off; detail comes from the report run a moment earlier,
// so heavy concurrent activity can show a small detail/summary gap that a
// re-run clears (the note says so). A difference is never hidden or forced to
// zero: it is shown with what is known to explain it, because an unexplained
// difference is exactly what a control total exists to surface.
//
// LedgerTotal is nil when no GL account carries the report's figure (Payables
// Ageing ages approved purchase orders - commitments, not liabilities); the
// note then says what the ledger does hold instead.

// ReportControlTotals is the strip shown above a report's rows.
type ReportControlTotals struct {
	CutOff          string   `json:"cut_off"`
	Basis           string   `json:"basis"`
	DetailTotal     float64  `json:"detail_total"`
	SummaryTotal    float64  `json:"summary_total"`
	LedgerTotal     *float64 `json:"ledger_total"`
	LedgerAccounts  []string `json:"ledger_accounts"`
	DetailVsSummary float64  `json:"detail_vs_summary"`
	SummaryVsLedger *float64 `json:"summary_vs_ledger"`
	Tied            bool     `json:"tied"`
	Notes           []string `json:"notes"`
	// AmountsHidden is set for a role that may not see the report's
	// sensitive columns: the tie result stays visible, the money does not.
	AmountsHidden bool `json:"amounts_hidden"`
}

// ReportControlTotalsFunc computes a report's control totals from the rows
// its own Run just returned (unmasked) and the same params.
type ReportControlTotalsFunc func(tenantID string, params map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error)

// controlTotalTolerance absorbs float rounding of rupee figures summed from
// paise; anything at or above one paisa is a real difference.
const controlTotalTolerance = 0.005

var reportControlTotals = map[string]ReportControlTotalsFunc{}

// RegisterReportControlTotals attaches control totals to an already
// registered report (called from init, after the report's own RegisterReport).
func RegisterReportControlTotals(reportID string, fn ReportControlTotalsFunc) {
	reportControlTotals[reportID] = fn
}

// HasReportControlTotals reports whether a report carries control totals.
func HasReportControlTotals(reportID string) bool {
	_, ok := reportControlTotals[reportID]
	return ok
}

// RunReportWithControlTotals is RunReportContext plus the report's control
// totals (nil when it has none). The rows returned are masked for role
// exactly as RunReportContext masks them; the control totals are computed
// from the unmasked run and hide their amounts when the rows were masked.
func RunReportWithControlTotals(ctx context.Context, tenantID, reportID, role, userID string, params map[string]string) (*ReportDefinition, []map[string]interface{}, bool, *ReportControlTotals, error) {
	fn, ok := reportControlTotals[reportID]
	if !ok {
		def, rows, masked, err := RunReportContext(ctx, tenantID, reportID, role, userID, params)
		return def, rows, masked, nil, err
	}
	// The caller's own rights first (140.12.10) - the unmasked run below is
	// a full-visibility role and would skip them.
	if d, known := reportRegistry[reportID]; known {
		if !ReportAllowedForRole(d, role) {
			return nil, nil, false, nil, reportRefused(d)
		}
		if access, limited := reportStoreScope(tenantID, role, userID); limited {
			// A store-limited user gets their stores' rows, without the
			// whole-ledger totals they could not tie to.
			if err := checkReportStoreParams(d, params, access); err != nil {
				return nil, nil, false, nil, err
			}
			def, rows, masked, err := RunReportContext(ctx, tenantID, reportID, role, userID, params)
			return def, rows, masked, nil, err
		}
	}
	// Run unmasked (a full-visibility role), total, then mask for the caller.
	// The caller's real role rides along so the sensitive-read event (49.11.1)
	// names who saw the answer, not the role that computed it.
	def, rawRows, _, err := RunReportContext(context.WithValue(ctx, reportReadRoleKey{}, role), tenantID, reportID, RoleSuperAdmin, userID, params)
	if err != nil {
		return nil, nil, false, nil, err
	}
	totals, terr := fn(tenantID, params, rawRows)
	if terr != nil {
		totals = &ReportControlTotals{Notes: []string{"Control totals could not be computed: " + terr.Error()}}
	} else if totals != nil && !totals.Tied {
		// 49.11.1: the books disagreeing is an INVARIANT event, flagged once
		// a day per report rather than on every open.
		LogInvariantEvent(tenantID, userID, "control_totals:"+reportID, "FLAGGED",
			fmt.Sprintf("detail %.2f vs summary %.2f (diff %.2f)%s", totals.DetailTotal, totals.SummaryTotal, totals.DetailVsSummary, ledgerDiffText(totals)))
	}
	masked := reportMasked(def.Columns, role)
	rows := maskSensitiveColumns(def.Columns, rawRows, role)
	if masked && totals != nil {
		totals.hideAmounts()
	}
	return def, rows, masked, totals, nil
}

func (c *ReportControlTotals) hideAmounts() {
	c.AmountsHidden = true
	c.DetailTotal, c.SummaryTotal, c.DetailVsSummary = 0, 0, 0
	if c.LedgerTotal != nil {
		zero := 0.0
		c.LedgerTotal = &zero
	}
	if c.SummaryVsLedger != nil {
		zero := 0.0
		c.SummaryVsLedger = &zero
	}
}

// finish fills the differences and the tie verdict.
func (c *ReportControlTotals) finish() *ReportControlTotals {
	c.DetailTotal = roundRupees(c.DetailTotal)
	c.SummaryTotal = roundRupees(c.SummaryTotal)
	c.DetailVsSummary = roundRupees(c.DetailTotal - c.SummaryTotal)
	c.Tied = math.Abs(c.DetailVsSummary) < controlTotalTolerance
	if c.LedgerTotal != nil {
		ledger := roundRupees(*c.LedgerTotal)
		c.LedgerTotal = &ledger
		diff := roundRupees(c.SummaryTotal - ledger)
		c.SummaryVsLedger = &diff
		c.Tied = c.Tied && math.Abs(diff) < controlTotalTolerance
	}
	if c.Notes == nil {
		c.Notes = []string{}
	}
	if c.LedgerAccounts == nil {
		c.LedgerAccounts = []string{}
	}
	return c
}

func roundRupees(v float64) float64 { return math.Round(v*100) / 100 }

// ledgerDiffText is the ledger leg of a FLAGGED control-total event, when
// the report has one.
func ledgerDiffText(c *ReportControlTotals) string {
	if c.LedgerTotal == nil || c.SummaryVsLedger == nil {
		return ""
	}
	return fmt.Sprintf("; summary vs ledger %.2f (diff %.2f)", *c.LedgerTotal, *c.SummaryVsLedger)
}

func sumRowField(rows []map[string]interface{}, key string) float64 {
	total := 0.0
	for _, r := range rows {
		switch v := r[key].(type) {
		case float64:
			total += v
		case int64:
			total += float64(v)
		case int:
			total += float64(v)
		}
	}
	return total
}

// controlSnapshot opens the read-only REPEATABLE READ snapshot that summary
// and ledger share, and captures its cut-off from the database clock.
func controlSnapshot(schema string) (*sql.Tx, time.Time, error) {
	tx, err := db.DB.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := db.SetSearchPath(tx, schema); err != nil {
		tx.Rollback()
		return nil, time.Time{}, err
	}
	var cut time.Time
	if err := tx.QueryRow(`SELECT LOCALTIMESTAMP`).Scan(&cut); err != nil {
		tx.Rollback()
		return nil, time.Time{}, err
	}
	return tx, cut, nil
}

// glDebitBalanceAt is debit minus credit over accounts for every posting up
// to and including cut (paise -> rupees). A liability's balance is its
// negative.
func glDebitBalanceAt(tx *sql.Tx, schema string, accounts []string, cut time.Time) (float64, error) {
	var paise int64
	err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(debit), 0) - COALESCE(SUM(credit), 0) FROM %s.gl_postings
		WHERE account_code = ANY($1) AND created_at <= $2`, schema), "{"+strings.Join(accounts, ",")+"}", cut).Scan(&paise)
	return PaiseToRupees(paise), err
}

func cutOffLabel(cut time.Time) string {
	return "as at " + cut.Format("2006-01-02 15:04:05") + " (server time): every document and GL posting up to this moment"
}

// --- Inventory Valuation: detail = summary = GL 1200 Inventory Control. ---

func inventoryValuationControlTotals(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var summaryPaise int64
	var uncosted int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(v.qty * v.cost), 0), COUNT(*) FILTER (WHERE v.cost = 0)
		FROM (SELECT ia.sku, SUM(ia.on_hand) AS qty, COALESCE(MAX(ic.avg_unit_cost_paise), 0) AS cost
		      FROM %s.inventory_availability ia LEFT JOIN %s.item_cost ic ON ic.item_code = ia.sku
		      GROUP BY ia.sku HAVING SUM(ia.on_hand) <> 0) v`, schema, schema)).Scan(&summaryPaise, &uncosted); err != nil {
		return nil, err
	}
	ledger, err := glDebitBalanceAt(tx, schema, []string{"1200"}, cut)
	if err != nil {
		return nil, err
	}
	c := &ReportControlTotals{
		CutOff: cutOffLabel(cut), Basis: "On-hand quantity x moving average cost, every SKU with stock",
		DetailTotal: sumRowField(rows, "total_value"), SummaryTotal: PaiseToRupees(summaryPaise),
		LedgerTotal: &ledger, LedgerAccounts: []string{"1200 Inventory Control"},
	}
	if uncosted > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d SKU(s) with stock have no cost on record and are valued at zero - see Non-Valuated Stock; their GL value (from the GRN) is in the ledger figure.", uncosted))
	}
	c.Notes = append(c.Notes, "A remaining difference is stock moved without a GL entry (e.g. cycle-count variances, which post no GL) or GL posted to 1200 without stock (a journal) - review the GL Journal for account 1200.")
	return c.finish(), nil
}

// --- Receivables Ageing: detail = summary = GL 1300 Accounts Receivable. ---

func receivablesAgeingControlTotals(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var summary float64
	var foreign int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(%s), 0)::float8,
		       COUNT(*) FILTER (WHERE COALESCE(data->>'currency', '') NOT IN ('', 'INR'))
		FROM %s.documents WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND created_at <= $1`, salesInvoiceOutstandingSQL, schema), cut).Scan(&summary, &foreign); err != nil {
		return nil, err
	}
	ledger, err := glDebitBalanceAt(tx, schema, []string{"1300"}, cut)
	if err != nil {
		return nil, err
	}
	c := &ReportControlTotals{
		CutOff: cutOffLabel(cut), Basis: "Approved (posted, not yet fully settled) customer invoices at their outstanding balance - invoice total less receipts and applied credit notes",
		DetailTotal: sumRowField(rows, "amount"), SummaryTotal: summary,
		LedgerTotal: &ledger, LedgerAccounts: []string{"1300 Accounts Receivable"},
	}
	if foreign > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d open invoice(s) are in a foreign currency: the ageing shows their invoice amount, the ledger carries them in rupees at the booking (or revalued) rate.", foreign))
	}
	return c.finish(), nil
}

// --- Payables Ageing: detail = summary = GL 2100 GRN Suspense. ---
//
// Stage 73 / 55.10: the report ages the received-not-paid sub-ledger, built
// from the 2100 postings themselves (engines/payables_subledger.go), so the
// ledger tie is exact. The notes name what a reader will find in it that is
// not "a goods receipt waiting for its bill to be paid".

func payablesAgeingControlTotals(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := payablesOpenItems(tx, schema, cut)
	if err != nil {
		return nil, err
	}
	var summaryPaise int64
	unlinked, overCleared, billed := 0, 0, 0
	for _, it := range items {
		summaryPaise += it.Outstanding
		switch {
		case it.GRNID == "":
			unlinked++
		case it.Outstanding < 0:
			overCleared++
		}
		if len(it.Bills) > 0 && it.Outstanding > 0 {
			billed++
		}
	}
	debitBalance, err := glDebitBalanceAt(tx, schema, []string{"2100"}, cut)
	if err != nil {
		return nil, err
	}
	ledger := -debitBalance // 2100 is a liability: what is owed is its credit balance
	c := &ReportControlTotals{
		CutOff: cutOffLabel(cut), Basis: "Goods received and not yet paid (GRN Suspense 2100) - each receipt's value less what bill payments, debit and credit notes and return GST reversals have cleared for it - aged from the bill's due date, else the receipt date",
		DetailTotal: sumRowField(rows, "amount"), SummaryTotal: PaiseToRupees(summaryPaise),
		LedgerTotal: &ledger, LedgerAccounts: []string{"2100 GRN Suspense"},
		Notes: []string{"A receipt is carried at its taxable cost; the GST on its bill goes to 1500 GST Input Credit when paid, so an item here is the goods value still owed, not the bill total."},
	}
	if billed > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d open receipt(s) already have a vendor bill - pay the bill (in part or in full) from Vendor Invoices to clear them.", billed))
	}
	if unlinked > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d item(s) on 2100 are not tied to a goods receipt (a fixed asset bought on credit, a journal, or a debit note without a GRN) and are aged from their posting date.", unlinked))
	}
	if overCleared > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d receipt(s) are cleared by more than they were received at and show as a negative amount - usually a bill whose taxable value differs from the receipt's cost, or a debit note / return GST reversal on it. Review them with the drill-down.", overCleared))
	}
	return c.finish(), nil
}

// --- Open Purchase Commitments: detail = summary; no ledger carries PO commitments. ---

func poCommitmentsControlTotals(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var summary float64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(COALESCE((data->>'total_amount')::numeric, 0)), 0)::float8
		FROM %s.documents WHERE doctype = 'PurchaseOrder' AND status = 'Approved' AND created_at <= $1`, schema), cut).Scan(&summary); err != nil {
		return nil, err
	}
	grnSuspense, err := glDebitBalanceAt(tx, schema, []string{"2100"}, cut)
	if err != nil {
		return nil, err
	}
	c := &ReportControlTotals{
		CutOff: cutOffLabel(cut), Basis: "Approved purchase orders not yet closed (commitments) at order total",
		DetailTotal: sumRowField(rows, "amount"), SummaryTotal: summary,
		Notes: []string{fmt.Sprintf("No GL account carries purchase-order commitments, so there is no ledger tie for this report. Goods received but not yet paid sit in 2100 GRN Suspense: %.2f at this cut-off - Payables Ageing ages them and ties to it.", -grnSuspense)},
	}
	return c.finish(), nil
}

// --- Sales Register: detail = summary = GL sales credits on POS sales. ---

// salesLedgerAccounts are where a POS sale's value lands: revenue (taxable,
// exempt, nil, zero-rated) and output GST.
var salesLedgerAccounts = []string{"4100", "4110", "4111", "4112", "2200", "2201", "2202"}

func salesRegisterControlTotals(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var summary float64
	var unreadable int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(t.total), 0)::float8, COUNT(*) FILTER (WHERE t.total IS NULL)
		FROM (SELECT d.id, (SELECT SUM(COALESCE((i->>'qty')::numeric, 0) * COALESCE((i->>'sale_price')::numeric, 0))
		                    FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d.data->'items') = 'array' THEN d.data->'items' ELSE '[]'::jsonb END) i) AS total
		      FROM %s.documents d
		      WHERE d.doctype = 'POSCart' AND d.status IN ('Paid', 'Settled') AND d.created_at <= $1) t`, schema), cut).Scan(&summary, &unreadable); err != nil {
		return nil, err
	}
	var ledgerPaise int64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(p.credit), 0) - COALESCE(SUM(p.debit), 0)
		FROM %s.gl_postings p
		JOIN %s.documents d ON d.doctype = 'POSCart' AND d.id = p.document_id AND d.status IN ('Paid', 'Settled')
		WHERE p.document_type = 'POSCart' AND p.account_code = ANY($1) AND p.created_at <= $2`, schema, schema),
		"{"+strings.Join(salesLedgerAccounts, ",")+"}", cut).Scan(&ledgerPaise); err != nil {
		return nil, err
	}
	ledger := PaiseToRupees(ledgerPaise)
	c := &ReportControlTotals{
		CutOff: cutOffLabel(cut), Basis: "Paid and settled POS sales: quantity x line price as rung up",
		DetailTotal: sumRowField(rows, "sale_total"), SummaryTotal: summary,
		LedgerTotal: &ledger, LedgerAccounts: []string{"4100-4112 Sales revenue", "2200-2202 Output GST"},
	}
	if unreadable > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d sale(s) have no readable lines and count as zero here (see the Data Issue column).", unreadable))
	}
	c.Notes = append(c.Notes, "Ledger is the revenue and output GST posted for these same sales. A difference is bill-level discounts and offers (the register shows the line price before them) or a sale whose GL posting failed - check POS Sale Reconciliation.")
	return c.finish(), nil
}

// --- GST Return Summary: drill rows = summary = GL 2200-2202 in the window. ---

func gstReturnControlTotals(tenantID string, params map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	start, end := params["start"], params["end"]
	tx, _, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Detail: the individual tax lines the drill-down lists, summed line by
	// line; ledger: the same three accounts' net movement in the window.
	var detailPaise, ledgerPaise int64
	var lines int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(credit - debit), 0), COUNT(*) FROM %s.gl_postings
		WHERE account_code IN ('2200', '2201', '2202') AND created_at >= public.erp_day_start($1::date) AND created_at < public.erp_day_start($2::date + 1)`, schema),
		start, end).Scan(&detailPaise, &lines); err != nil {
		return nil, err
	}
	for _, account := range []string{"2200", "2201", "2202"} {
		var net int64
		if err := tx.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(credit), 0) - COALESCE(SUM(debit), 0) FROM %s.gl_postings
			WHERE account_code = $1 AND created_at >= public.erp_day_start($2::date) AND created_at < public.erp_day_start($3::date + 1)`, schema),
			account, start, end).Scan(&net); err != nil {
			return nil, err
		}
		ledgerPaise += net
	}
	ledger := PaiseToRupees(ledgerPaise)
	c := &ReportControlTotals{
		CutOff:      fmt.Sprintf("%s 00:00 to %s 23:59:59 (server time), by GL posting date", start, end),
		Basis:       fmt.Sprintf("Output GST (CGST + SGST + IGST): %d tax line(s) in the window", lines),
		DetailTotal: PaiseToRupees(detailPaise), SummaryTotal: sumRowField(rows, "total_tax_liability"),
		LedgerTotal: &ledger, LedgerAccounts: []string{"2200 CGST", "2201 SGST", "2202 IGST"},
		Notes: []string{"Detail is the tax lines View Details lists; summary is the Total Tax Liability column."},
	}
	return c.finish(), nil
}

// --- Loyalty Points Liability: detail = summary = GL 2630 when accruing. ---
//
// 73.2.12 made the treatment a policy (Loyalty -> Account for loyalty points).
// With "liability when earned" points' value sits in 2630 Loyalty Points
// Liability and the report ties to it; with the default (expensed when
// redeemed) no account carries the liability, and the strip says so.

func pointsLiabilityControlTotals(tenantID string, _ map[string]string, rows []map[string]interface{}) (*ReportControlTotals, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, cut, err := controlSnapshot(schema)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	valuePaise := loyaltyPointValuePaise(tenantID)
	var positivePoints, negativePoints int64
	var negativeCustomers int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(bal) FILTER (WHERE bal > 0), 0), COALESCE(SUM(bal) FILTER (WHERE bal < 0), 0), COUNT(*) FILTER (WHERE bal < 0)
		FROM (SELECT customer_id, SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END) AS bal
		      FROM %s.loyalty_point_ledger WHERE created_at <= $1 GROUP BY customer_id) t`, schema), cut).Scan(&positivePoints, &negativePoints, &negativeCustomers); err != nil {
		return nil, err
	}
	c := &ReportControlTotals{
		CutOff:       cutOffLabel(cut),
		Basis:        fmt.Sprintf("Unredeemed points of customers with a positive balance, at %.2f a point", PaiseToRupees(valuePaise)),
		DetailTotal:  sumRowField(rows, "total_liability_value"),
		SummaryTotal: PaiseToRupees(positivePoints * valuePaise),
	}
	if negativeCustomers > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("%d customer(s) have a negative balance (%d points, %.2f) - points spent before a return took them back. The report leaves them out; the ledger nets them in.",
			negativeCustomers, -negativePoints, PaiseToRupees(-negativePoints*valuePaise)))
	}
	if !loyaltyAccruesForSchema(schema) {
		c.Notes = append(c.Notes, "Points are expensed when redeemed (Loyalty -> Account for loyalty points), so no GL account carries this liability and there is no ledger tie. Choose \"liability when earned\" to book it to 2630.")
		return c.finish(), nil
	}
	ledgerDebit, err := glDebitBalanceAt(tx, schema, []string{accountLoyaltyLiability}, cut)
	if err != nil {
		return nil, err
	}
	// 2630 is a liability: its balance is credit minus debit. The report's
	// basis excludes negative balances, so compare the ledger with them added
	// back - an otherwise tied book still shows equal.
	ledger := -ledgerDebit - PaiseToRupees(negativePoints*valuePaise)
	c.LedgerTotal = &ledger
	c.LedgerAccounts = []string{"2630 Loyalty Points Liability"}
	c.Notes = append(c.Notes, "Turning on \"liability when earned\" does not book the balance outstanding at that moment - journal it to 2630 once, or the ledger stays short by exactly that amount. A change of point value moves the report but not the ledger.")
	return c.finish(), nil
}

func init() {
	RegisterReportControlTotals("points-liability", pointsLiabilityControlTotals)
	RegisterReportControlTotals("inventory-valuation", inventoryValuationControlTotals)
	RegisterReportControlTotals("receivables-ageing", receivablesAgeingControlTotals)
	RegisterReportControlTotals("payables-ageing", payablesAgeingControlTotals)
	RegisterReportControlTotals("po-commitments", poCommitmentsControlTotals)
	RegisterReportControlTotals("sales-register", salesRegisterControlTotals)
	RegisterReportControlTotals("gst-return-summary", gstReturnControlTotals)
}
