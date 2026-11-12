package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 148.30: sales incentives. Module sales_incentives, off by default.
//
// A share of each sold line's margin (net of discount, before tax, less cost)
// goes to the salesman, by a slab chosen from the line's own margin %. It is
// accrued once when a bill posts (Dr incentive expense, Cr accrued incentives),
// split between salesmen by %, clawed back in the same split on a return or a
// cancelled bill, and paid out by the payslip (the payslip debits the accrued
// account for it). A clawback after payout is an unpaid negative row that nets
// against the next payout; it is never lost. A rule change never touches an
// accrual that exists, because the rate is frozen on the row.
//
// The sale paths call AccrueSaleIncentivesLogged / ClawbackSaleIncentivesLogged
// after their own commit: a failure there is recorded as a system error and
// never blocks the sale.

const (
	ModuleSalesIncentives = "sales_incentives"

	SettingIncentiveExpenseAccount = "sales_incentives.expense_account"
	SettingIncentiveAccruedAccount = "sales_incentives.accrued_account"

	incentiveSplitCode    = "SALESP-0740" // the split is not 100%
	incentiveSalesmanCode = "SALESP-0741" // salesman not on file
	incentiveOffCode      = "SALESP-0742" // module off
	incentiveNoProfile    = "SALESP-0743" // the login is not a salesman
	incentivePayoutCode   = "HR-0600"     // entries changed since the payslip was made
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingIncentiveExpenseAccount, Module: "Sales",
		Label: "Incentive expense account", Type: SettingTypeString, Default: "5500",
		Description: "Account debited when an incentive is accrued.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingIncentiveAccruedAccount, Module: "Sales",
		Label: "Accrued incentives account", Type: SettingTypeString, Default: "2400",
		Description: "Account credited when an incentive is accrued; the payslip clears it.",
	})
}

func incentivesOn(tenantID string) bool {
	on, err := IsModuleEnabled(tenantID, ModuleSalesIncentives)
	return err == nil && on
}

// RequireSalesIncentives refuses while the module is off.
func RequireSalesIncentives(tenantID string) error {
	if !incentivesOn(tenantID) {
		return &ValidationError{Code: incentiveOffCode, Message: "Sales incentives are off."}
	}
	return nil
}

type incLine struct {
	SKU       string
	Qty       int
	NetPaise  int64 // before tax, after discount
	CostPaise int64
}

type incShare struct {
	Salesman string
	Pct      float64
}

type incRule struct {
	Category  string
	MinMargin float64
	Pct       float64
}

// rawJSONField reads a document field that may hold a JSON array directly or
// as a JSON string.
func rawJSONField(d map[string]json.RawMessage, key string, out interface{}) {
	raw, ok := d[key]
	if !ok {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	_ = json.Unmarshal(raw, out)
}

func rawStringField(d map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := d[key]; ok && json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

// costBySKU totals what the sale booked as cost, per SKU.
func incentiveCosts(dataStr string) map[string][2]int64 { // sku -> qty, value
	out := map[string][2]int64{}
	for _, c := range readSaleCosts(dataStr) {
		v := out[c.SKU]
		v[0] += int64(c.Qty)
		v[1] += c.ValuePaise
		out[c.SKU] = v
	}
	return out
}

func lineCost(tenantID string, costs map[string][2]int64, sku string, qty int) int64 {
	if v, ok := costs[sku]; ok && v[0] > 0 {
		return int64(math.Round(float64(v[1]) * float64(qty) / float64(v[0])))
	}
	return int64(qty) * ResolveCOGSUnitCostPaise(tenantID, sku, itemStandardCostRupees(tenantID, sku))
}

// loadIncentiveSale reads the lines and the salesmen of a posted sale.
func loadIncentiveSale(tenantID, schema, docType, id string) ([]incLine, []incShare, error) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), docType, id).Scan(&dataStr); err != nil {
		return nil, nil, fmt.Errorf("%s %s not found: %v", docType, id, err)
	}
	var d map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dataStr), &d); err != nil {
		return nil, nil, err
	}
	costs := incentiveCosts(dataStr)
	var lines []incLine
	switch docType {
	case "SalesInvoice":
		for _, l := range salesInvoiceLineItems(dataStr) {
			if l.Sku == "" || l.Qty <= 0 {
				continue
			}
			var net int64
			if l.Taxable > 0 {
				net = RupeesToPaise(l.Taxable)
			} else if l.Rate > 0 {
				net = RupeesToPaise(l.Rate * float64(l.Qty))
			} else {
				net = RupeesToPaise(l.paidUnitPrice() * float64(l.Qty) / (1 + l.GSTRate/100))
			}
			lines = append(lines, incLine{SKU: l.Sku, Qty: l.Qty, NetPaise: net, CostPaise: lineCost(tenantID, costs, l.Sku, l.Qty)})
		}
	case "POSCart":
		var alloc []SaleLineAllocation
		rawJSONField(d, "allocation", &alloc)
		var gst GSTBreakdown
		rawJSONField(d, "gst_breakdown", &gst)
		ratio := 1.0
		if gst.TotalAmount > 0 && gst.TaxableAmount > 0 {
			ratio = gst.TaxableAmount / gst.TotalAmount
		}
		for _, a := range alloc {
			if a.SKU == "" || a.Qty <= 0 {
				continue
			}
			lines = append(lines, incLine{SKU: a.SKU, Qty: a.Qty, NetPaise: int64(math.Round(float64(a.NetPaise) * ratio)), CostPaise: lineCost(tenantID, costs, a.SKU, a.Qty)})
		}
	default:
		return nil, nil, fmt.Errorf("%s does not carry incentives", docType)
	}

	var split []struct {
		Salesman string  `json:"salesman_id"`
		Pct      float64 `json:"pct"`
	}
	rawJSONField(d, "salesman_split", &split)
	var shares []incShare
	for _, s := range split {
		if strings.TrimSpace(s.Salesman) != "" {
			shares = append(shares, incShare{strings.TrimSpace(s.Salesman), s.Pct})
		}
	}
	if len(shares) == 0 {
		if one := rawStringField(d, "salesman_id"); one != "" {
			shares = []incShare{{one, 100}}
		}
	}
	return lines, shares, nil
}

func validateIncentiveShares(schema string, shares []incShare) error {
	var sum float64
	for _, s := range shares {
		if s.Pct <= 0 {
			return &ValidationError{Code: incentiveSplitCode, Message: "The salesman split needs a % above zero for each salesman."}
		}
		sum += s.Pct
		var n int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Salesman' AND id = $1 AND deleted_at IS NULL`, schema), s.Salesman).Scan(&n)
		if n == 0 {
			return &ValidationError{Code: incentiveSalesmanCode, Message: fmt.Sprintf("Salesman %s is not on file.", s.Salesman)}
		}
	}
	if math.Abs(sum-100) > 0.001 {
		return &ValidationError{Code: incentiveSplitCode, Message: fmt.Sprintf("The salesman split adds up to %.2f%%, not 100%%.", sum)}
	}
	return nil
}

func loadIncentiveRules(schema string) []incRule {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'category',''), COALESCE(NULLIF(data->>'min_margin_pct','')::numeric,0), COALESCE(NULLIF(data->>'pct','')::numeric,0)
		FROM %s.documents WHERE doctype = 'IncentiveRule' AND deleted_at IS NULL AND COALESCE(status,'Active') = 'Active'`, schema))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []incRule
	for rows.Next() {
		var r incRule
		if rows.Scan(&r.Category, &r.MinMargin, &r.Pct) == nil && r.Pct > 0 {
			out = append(out, r)
		}
	}
	return out
}

// pickIncentiveRule: a rule of the line's own category first, else a rule for
// all categories; the highest margin slab the line reaches.
func pickIncentiveRule(rules []incRule, category string, marginPct float64) (incRule, bool) {
	best := func(match func(incRule) bool) (incRule, bool) {
		var c []incRule
		for _, r := range rules {
			if match(r) && r.MinMargin <= marginPct {
				c = append(c, r)
			}
		}
		if len(c) == 0 {
			return incRule{}, false
		}
		sort.Slice(c, func(i, j int) bool { return c[i].MinMargin > c[j].MinMargin })
		return c[0], true
	}
	if r, ok := best(func(r incRule) bool { return category != "" && r.Category == category }); ok {
		return r, true
	}
	return best(func(r incRule) bool { return r.Category == "" })
}

func itemCategory(tenantID, sku string) string {
	if it, err := ResolveItemBySKU(tenantID, sku); err == nil && it != nil {
		return strings.TrimSpace(fmt.Sprint(it.Data["category"]))
	}
	return ""
}

// splitPaise cuts total between shares; the last share takes the remainder so
// the parts always add up.
func splitPaise(total int64, shares []incShare) []int64 {
	out := make([]int64, len(shares))
	var used int64
	for i, s := range shares {
		if i == len(shares)-1 {
			out[i] = total - used
			break
		}
		out[i] = int64(math.Round(float64(total) * s.Pct / 100))
		used += out[i]
	}
	return out
}

func incentiveAccounts(tenantID string) (expense, accrued string) {
	expense = strings.TrimSpace(GetSettingString(tenantID, SettingIncentiveExpenseAccount))
	accrued = strings.TrimSpace(GetSettingString(tenantID, SettingIncentiveAccruedAccount))
	if expense == "" {
		expense = "5500"
	}
	if accrued == "" {
		accrued = "2400"
	}
	return
}

// AccrueSaleIncentives books the incentive of one posted bill (an invoice or
// a POS cart). Once per bill: a replay finds the rows and does nothing.
func AccrueSaleIncentives(tenantID, docType, id string) error {
	if !incentivesOn(tenantID) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var already int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.sales_incentive_entries WHERE source_doctype = $1 AND source_id = $2 AND kind = 'Accrual'`, schema), docType, id).Scan(&already)
	if already > 0 {
		return nil
	}
	lines, shares, err := loadIncentiveSale(tenantID, schema, docType, id)
	if err != nil {
		return err
	}
	if len(shares) == 0 || len(lines) == 0 {
		return nil
	}
	rules := loadIncentiveRules(schema)
	if len(rules) == 0 {
		return nil
	}
	if err := validateIncentiveShares(schema, shares); err != nil {
		return err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	var total int64
	today := BusinessToday()
	for i, l := range lines {
		margin := l.NetPaise - l.CostPaise
		if margin <= 0 || l.NetPaise <= 0 {
			continue
		}
		cat := itemCategory(tenantID, l.SKU)
		rule, ok := pickIncentiveRule(rules, cat, float64(margin)*100/float64(l.NetPaise))
		if !ok {
			continue
		}
		amount := int64(math.Round(float64(margin) * rule.Pct / 100))
		if amount <= 0 {
			continue
		}
		for k, part := range splitPaise(amount, shares) {
			if part == 0 {
				continue
			}
			res, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.sales_incentive_entries
				(kind, source_doctype, source_id, line_no, salesman_id, sku, category, qty, margin_paise, pct, split_pct, amount_paise, entry_date)
				VALUES ('Accrual', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
				ON CONFLICT (source_doctype, source_id, line_no, salesman_id) WHERE kind = 'Accrual' DO NOTHING`, schema),
				docType, id, i, shares[k].Salesman, l.SKU, cat, l.Qty, margin, rule.Pct, shares[k].Pct, part, today)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				total += part
			}
		}
	}
	if total > 0 {
		expense, accrued := incentiveAccounts(tenantID)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "SalesIncentive", id,
			map[string]int64{expense: total}, map[string]int64{accrued: total}, "",
			fmt.Sprintf("SalesIncentive:%s:%s:ACCRUE", docType, id)); err != nil {
			return fmt.Errorf("incentive posting failed: %v", err)
		}
	}
	return tx.Commit()
}

// AccrueSaleIncentivesLogged is the sale paths' call: never an error back.
func AccrueSaleIncentivesLogged(tenantID, docType, id string) {
	if !incentivesOn(tenantID) {
		return
	}
	if err := AccrueSaleIncentives(tenantID, docType, id); err != nil {
		LogSystemError(tenantID, "", "ERROR", "sales_incentives", fmt.Sprintf("incentive not accrued for %s %s: %v", docType, id, err), "")
	}
}

// ClawbackSaleIncentives takes back, in the same split, the incentive of the
// units returned (qtyBySKU) or, with none given, of everything left on the
// bill. ref names the return or cancellation; the same ref twice does nothing.
func ClawbackSaleIncentives(tenantID, sourceID, ref string, qtyBySKU map[string]int) error {
	if !incentivesOn(tenantID) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	rows, err := tx.Query(fmt.Sprintf(`SELECT a.id, a.line_no, a.sku, a.qty, a.amount_paise, a.salesman_id,
			a.amount_paise + COALESCE((SELECT SUM(c.amount_paise) FROM %s.sales_incentive_entries c WHERE c.kind = 'Clawback' AND c.accrual_id = a.id), 0)
		FROM %s.sales_incentive_entries a WHERE a.kind = 'Accrual' AND a.source_id = $1 ORDER BY a.line_no, a.id FOR UPDATE`, schema, schema), sourceID)
	if err != nil {
		return err
	}
	type acc struct {
		id, amount, left int64
		line             int
		sku, salesman    string
		qty              float64
	}
	var accs []acc
	for rows.Next() {
		var a acc
		if err := rows.Scan(&a.id, &a.line, &a.sku, &a.qty, &a.amount, &a.salesman, &a.left); err != nil {
			rows.Close()
			return err
		}
		accs = append(accs, a)
	}
	rows.Close()
	if len(accs) == 0 {
		return nil
	}
	want := map[string]float64{}
	for k, v := range qtyBySKU {
		want[k] = float64(v)
	}
	// What each line still has to give back, across its split shares.
	lineAmount, lineLeft := map[int]int64{}, map[int]int64{}
	for _, a := range accs {
		lineAmount[a.line] += a.amount
		lineLeft[a.line] += a.left
	}
	consumed := map[int]float64{}
	var total int64
	for _, a := range accs {
		claw := a.left
		if len(qtyBySKU) > 0 {
			q, seen := consumed[a.line]
			if !seen {
				remaining := a.qty
				if lineAmount[a.line] > 0 {
					// Whole units left: paise rounding must not leave 0.99 of a unit.
					remaining = math.Min(a.qty, math.Ceil(a.qty*float64(lineLeft[a.line])/float64(lineAmount[a.line])-0.01))
				}
				q = math.Max(0, math.Min(want[a.sku], remaining))
				want[a.sku] -= q
				consumed[a.line] = q
			}
			if q <= 0 || a.qty <= 0 {
				continue
			}
			claw = int64(math.Round(float64(a.amount) * q / a.qty))
			if claw > a.left {
				claw = a.left
			}
		}
		if claw <= 0 {
			continue
		}
		res, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.sales_incentive_entries
			(kind, source_doctype, source_id, line_no, ref, salesman_id, sku, qty, amount_paise, accrual_id, entry_date)
			SELECT 'Clawback', source_doctype, source_id, line_no, $2, salesman_id, sku, qty, $3, id, $4 FROM %s.sales_incentive_entries WHERE id = $1
			ON CONFLICT (accrual_id, ref) WHERE kind = 'Clawback' DO NOTHING`, schema, schema), a.id, ref, -claw, BusinessToday())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			total += claw
		}
	}
	if total > 0 {
		expense, accrued := incentiveAccounts(tenantID)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "SalesIncentive", sourceID,
			map[string]int64{accrued: total}, map[string]int64{expense: total}, "",
			fmt.Sprintf("SalesIncentive:%s:CLAW:%s", sourceID, ref)); err != nil {
			return fmt.Errorf("incentive clawback posting failed: %v", err)
		}
	}
	return tx.Commit()
}

// ClawbackSaleIncentivesLogged is the return and cancel paths' call.
func ClawbackSaleIncentivesLogged(tenantID, sourceID, ref string, qtyBySKU map[string]int) {
	if !incentivesOn(tenantID) {
		return
	}
	if err := ClawbackSaleIncentives(tenantID, sourceID, ref, qtyBySKU); err != nil {
		LogSystemError(tenantID, "", "ERROR", "sales_incentives", fmt.Sprintf("incentive not clawed back for %s (%s): %v", sourceID, ref, err), "")
	}
}

// ClawbackReturnIncentives claws back what a processed refund gave back: the
// refund-eligible, non-exchange units of its return request.
func ClawbackReturnIncentives(tenantID, originalOrderID, refundRequestID, returnRequestID string) {
	if !incentivesOn(tenantID) || originalOrderID == "" {
		return
	}
	qty := map[string]int{}
	if returnRequestID != "" {
		if _, rr, err := fetchReturnRequest(tenantID, returnRequestID); err == nil {
			raw, _ := json.Marshal(rr["items"])
			var items []returnItemRecord
			_ = json.Unmarshal(raw, &items)
			for _, it := range items {
				if rule, ok := returnDispositionRule[it.Disposition]; ok && rule.RefundEligible && it.ExchangeSKU == "" && it.NextAction != returnActionCredit && it.Qty > 0 {
					qty[it.SKU] += it.Qty
				}
			}
		}
	}
	if len(qty) == 0 {
		return
	}
	for _, src := range []string{originalOrderID, salesInvoiceForOrder(tenantID, originalOrderID)} {
		ClawbackSaleIncentivesLogged(tenantID, src, "RET:"+refundRequestID, qty)
	}
}

// ClawbackCreditNoteIncentives claws back the units a posted return credit note
// gave back. A note not raised from a return (a price correction) takes nothing.
func ClawbackCreditNoteIncentives(tenantID, noteID string, note map[string]interface{}) {
	if !incentivesOn(tenantID) || strField(note, "return_request_id") == "" {
		return
	}
	qty := map[string]int{}
	for _, l := range creditNoteLineMaps(note) {
		if sku, n := strField(l, "sku"), int(numFromInterface(l["qty"])); sku != "" && n > 0 {
			qty[sku] += n
		}
	}
	if len(qty) == 0 {
		return
	}
	for _, src := range []string{strField(note, "sales_invoice_id"), strField(note, "reference_cart")} {
		if src != "" {
			ClawbackSaleIncentivesLogged(tenantID, src, "CRN:"+noteID, qty)
		}
	}
}

// IncentiveUnpaidPaise is what a salesman is owed and not yet paid out; negative
// when returns after payout are carried.
func IncentiveUnpaidPaise(tenantID, salesmanID string) int64 {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	var v int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise),0) FROM %s.sales_incentive_entries WHERE salesman_id = $1 AND paid_payslip = ''`, schema), salesmanID).Scan(&v)
	return v
}

func employeeSalesmen(schema, employeeID string) []string {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'salesman_id' FROM %s.documents WHERE doctype = 'IncentiveProfile' AND deleted_at IS NULL
		AND COALESCE(status,'Active') = 'Active' AND data->>'employee_id' = $1`, schema), employeeID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// IncentivePayForEmployee is what the next payslip pays this employee in
// paise, for entries up to the period end; nothing while the module is off or
// the net is not above zero (a negative net is carried to a later payout).
func IncentivePayForEmployee(tenantID, employeeID, periodTo string) int64 {
	if !incentivesOn(tenantID) {
		return 0
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0
	}
	people := employeeSalesmen(schema, employeeID)
	if len(people) == 0 {
		return 0
	}
	var v int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise),0) FROM %s.sales_incentive_entries
		WHERE salesman_id = ANY($1) AND paid_payslip = '' AND entry_date <= $2::date`, schema), pqStringArray(people), periodTo).Scan(&v)
	if v < 0 {
		return 0
	}
	return v
}

// payoutIncentivesTx marks the entries a payslip pays, inside the payslip's own
// transaction, and refuses if they no longer add up to what the payslip says.
func payoutIncentivesTx(tx *sql.Tx, schema, employeeID, payslipID, periodTo string, expected int64) error {
	people := employeeSalesmen(schema, employeeID)
	rows, err := tx.Query(fmt.Sprintf(`UPDATE %s.sales_incentive_entries SET paid_payslip = $1
		WHERE salesman_id = ANY($2) AND paid_payslip = '' AND entry_date <= $3::date RETURNING amount_paise`, schema), payslipID, pqStringArray(people), periodTo)
	if err != nil {
		return err
	}
	defer rows.Close()
	var sum int64
	for rows.Next() {
		var v int64
		_ = rows.Scan(&v)
		sum += v
	}
	if sum != expected {
		return &ValidationError{Code: incentivePayoutCode, Message: "The incentive changed since this payslip was made - run payroll for the employee again."}
	}
	return nil
}

// IncentiveEarning is one row of a salesman's own statement.
type IncentiveEarning struct {
	Date     string  `json:"date"`
	Kind     string  `json:"kind"`
	Bill     string  `json:"bill"`
	SKU      string  `json:"sku"`
	Amount   float64 `json:"amount"`
	Paid     bool    `json:"paid"`
	Salesman string  `json:"-"`
}

// IncentiveStatement is what the store app shows a signed-in salesman: only
// their own rows, found by the login on their IncentiveProfile.
type IncentiveStatement struct {
	Linked  bool               `json:"linked"`
	Unpaid  float64            `json:"unpaid"`
	Paid    float64            `json:"paid"`
	Entries []IncentiveEarning `json:"entries"`
}

func IncentiveStatementForUser(tenantID, username string) (*IncentiveStatement, error) {
	if err := RequireSalesIncentives(tenantID); err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data->>'salesman_id' FROM %s.documents WHERE doctype = 'IncentiveProfile' AND deleted_at IS NULL
		AND COALESCE(status,'Active') = 'Active' AND lower(data->>'user') = lower($1) AND $1 <> ''`, schema), strings.TrimSpace(username))
	if err != nil {
		return nil, err
	}
	var mine []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil && s != "" {
			mine = append(mine, s)
		}
	}
	rows.Close()
	st := &IncentiveStatement{Entries: []IncentiveEarning{}}
	if len(mine) == 0 {
		return st, nil
	}
	st.Linked = true
	arr := pqStringArray(mine)
	var unpaid, paid int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(amount_paise) FILTER (WHERE paid_payslip = ''),0), COALESCE(SUM(amount_paise) FILTER (WHERE paid_payslip <> ''),0)
		FROM %s.sales_incentive_entries WHERE salesman_id = ANY($1)`, schema), arr).Scan(&unpaid, &paid)
	st.Unpaid, st.Paid = PaiseToRupees(unpaid), PaiseToRupees(paid)
	er, err := db.DB.Query(fmt.Sprintf(`SELECT to_char(entry_date,'YYYY-MM-DD'), kind, source_id, sku, amount_paise, paid_payslip <> ''
		FROM %s.sales_incentive_entries WHERE salesman_id = ANY($1) ORDER BY id DESC LIMIT 100`, schema), arr)
	if err != nil {
		return nil, err
	}
	defer er.Close()
	for er.Next() {
		var e IncentiveEarning
		var p int64
		if er.Scan(&e.Date, &e.Kind, &e.Bill, &e.SKU, &p, &e.Paid) == nil {
			e.Amount = PaiseToRupees(p)
			st.Entries = append(st.Entries, e)
		}
	}
	return st, nil
}
