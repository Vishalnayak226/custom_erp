package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"custom_erp/db"
)

// Stage 148.1: cost of goods follows the stock in hand.
//
// item_cost.avg_unit_cost_paise is now the moving average of what is in hand,
// company-wide, instead of everything ever received. Every receipt blends with
// the units held at that moment, read live under the item_cost row lock, so the
// many stock-out paths still need no hook: a sale, transfer or count of unnamed
// units leaves the rate alone. cumulative_* keep their meaning (lifetime
// received) and only decide whether an item has a cost at all.
//
// In hand = on_hand + in_transit across every store, so a transfer is
// cost-neutral, less GRN units set aside as rejected or damaged (they were never
// valued, see stockInHandForCost).
//
// Lock order everywhere: inventory_availability, then SerialNumber, then
// item_cost, one SKU at a time in SKU order.

const accountCOGSVariance = "5195"

const (
	costSourceOwn      = "Own cost"
	costSourceAverage  = "Average in hand"
	costSourceStandard = "Standard cost"
	costSourceNone     = "No cost yet"
)

func ratOf(v float64) *big.Rat {
	r := new(big.Rat)
	if r.SetFloat64(v) == nil {
		return new(big.Rat)
	}
	return r
}

// roundRatPaise rounds half up. A rate is never negative.
func roundRatPaise(r *big.Rat) int64 {
	if r.Sign() <= 0 {
		return 0
	}
	n := new(big.Int).Mul(r.Num(), big.NewInt(2))
	n.Add(n, r.Denom())
	return new(big.Int).Quo(n, new(big.Int).Mul(r.Denom(), big.NewInt(2))).Int64()
}

// blendRatePaise is the average after receiving qty worth value on top of held
// units at rate. Nothing held (or less than nothing): the receipt's own rate.
func blendRatePaise(held float64, rate, value int64, qty float64) int64 {
	if qty <= 0 {
		return max(rate, 0)
	}
	num := new(big.Rat).SetInt64(value)
	den := ratOf(qty)
	if held > 0 {
		h := ratOf(held)
		num.Add(num, new(big.Rat).Mul(h, new(big.Rat).SetInt64(rate)))
		den.Add(den, h)
	}
	return roundRatPaise(num.Quo(num, den))
}

// rateAfterTakingOutPaise is the average of the heldAfter units left once qtyOut
// units worth value left the pool at their own cost. Nothing left: rate stands.
func rateAfterTakingOutPaise(heldAfter float64, rate int64, qtyOut float64, value int64) int64 {
	if heldAfter <= 0 {
		return max(rate, 0)
	}
	h := ratOf(heldAfter)
	pool := new(big.Rat).Add(h, ratOf(qtyOut))
	pool.Mul(pool, new(big.Rat).SetInt64(rate))
	pool.Sub(pool, new(big.Rat).SetInt64(value))
	return roundRatPaise(pool.Quo(pool, h))
}

// rateAfterChargePaise is the average once share paise (either sign) is added to
// the value of held units.
func rateAfterChargePaise(held float64, rate, share int64) int64 {
	if held <= 0 {
		return max(rate, 0)
	}
	h := ratOf(held)
	value := new(big.Rat).Mul(h, new(big.Rat).SetInt64(rate))
	value.Add(value, new(big.Rat).SetInt64(share))
	return roundRatPaise(value.Quo(value, h))
}

// sharePaise is amount x part / whole, rounded half away from zero.
func sharePaise(amount int64, part, whole float64) int64 {
	if whole <= 0 || part <= 0 {
		return 0
	}
	sign := int64(1)
	if amount < 0 {
		sign, amount = -1, -amount
	}
	r := new(big.Rat).Mul(new(big.Rat).SetInt64(amount), ratOf(part))
	return sign * roundRatPaise(r.Quo(r, ratOf(whole)))
}

// lockItemCostTx takes the item's cost row lock, creating an empty row if it has
// none, and reads its lifetime qty and current rate.
func lockItemCostTx(tx *sql.Tx, schema, sku string) (cumQty float64, rate int64, err error) {
	if _, err = tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise)
		VALUES ($1, 0, 0, 0) ON CONFLICT (item_code) DO NOTHING`, schema), sku); err != nil {
		return 0, 0, err
	}
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT cumulative_qty_received, avg_unit_cost_paise FROM %s.item_cost WHERE item_code = $1 FOR UPDATE`, schema),
		sku).Scan(&cumQty, &rate)
	return cumQty, rate, err
}

// stockInHandForCost is the units the average is spread over. Call it AFTER
// lockItemCostTx, as a statement of its own: under READ COMMITTED it then sees a
// receipt that committed while this one waited for the lock.
//
// Units a GRN set aside (rejected or damaged at receipt, no Dr 1200) sit inside
// on_hand but were never valued, so they stay out of the pool until they leave.
// They are the GRN set-asides still standing, capped at what the two buckets
// hold: a customer return landing in a bucket WAS valued and stays in.
func stockInHandBeforeMemo(q queryRower, schema, sku string) (float64, error) {
	var held, setAside float64
	if err := q.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(on_hand + COALESCE(in_transit, 0)), 0)::float8,
		       COALESCE(SUM(COALESCE(qc_hold, 0) + COALESCE(damaged, 0)), 0)::float8
		FROM %s.inventory_availability WHERE sku = $1`, schema), sku).Scan(&held, &setAside); err != nil {
		return 0, err
	}
	if setAside <= 0 {
		return held, nil
	}
	var standing float64
	if err := q.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM((data->>'qty')::numeric), 0)::float8 FROM %s.documents
		WHERE doctype = 'StockLedgerEntry' AND data @> jsonb_build_object('item_id', $1::text)
		  AND ((data->>'voucher_type' = 'GRN' AND data->>'to_status' IN ('QC-Hold', 'Damaged'))
		    OR (data->>'voucher_type' = 'PurchaseReturn' AND data->>'from_status' IN ('QC-Hold', 'Damaged')))`, schema),
		sku).Scan(&standing); err != nil {
		return 0, err
	}
	if standing > 0 {
		held -= math.Min(standing, setAside)
	}
	return held, nil
}

// writeItemCostTx stores a new rate and moves the lifetime figures (never below zero).
func writeItemCostTx(tx *sql.Tx, schema, sku string, rate int64, addQty float64, addValue int64) error {
	_, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.item_cost SET avg_unit_cost_paise = $2::bigint,
			cumulative_qty_received = GREATEST(cumulative_qty_received + $3::numeric, 0),
			cumulative_value_received_paise = GREATEST(cumulative_value_received_paise + $4::bigint, 0),
			updated_at = CURRENT_TIMESTAMP
		WHERE item_code = $1`, schema), sku, rate, addQty, addValue)
	return err
}

// blendIntoCostTx puts qty worth value into the average. stockIn is how many of
// the units now counted as in hand belong to this receipt (and any later line of
// it), since the stock posts first. A real receipt also grows the lifetime
// figures; goods coming back from a customer do not. Stock with no cost on record
// must not dilute a first real rate, so it counts as nothing held.
func blendIntoCostTx(tx *sql.Tx, schema, sku string, qty float64, value int64, stockIn float64, addToLifetime bool) error {
	if qty <= 0 || value < 0 {
		return nil
	}
	cum, rate, err := lockItemCostTx(tx, schema, sku)
	if err != nil {
		return err
	}
	if err := memoTrimLotsTx(tx, schema, sku, stockIn); err != nil { // 148.16
		return err
	}
	if !addToLifetime && cum <= 0 {
		// 148.16: goods that came back from a memo sale carry the memo rate, and the
		// SKU has no cost record yet because the memo receipt booked none.
		if value <= 0 || !memoSoldSKU(tx, schema, sku) {
			return nil
		}
		addToLifetime = true
	}
	held, err := stockInHandForCost(tx, schema, sku)
	if err != nil {
		return err
	}
	held -= stockIn
	if cum <= 0 {
		held = 0
	}
	var addQty float64
	var addValue int64
	if addToLifetime {
		addQty, addValue = qty, value
	}
	return writeItemCostTx(tx, schema, sku, blendRatePaise(held, rate, value, qty), addQty, addValue)
}

// takeOutOfCostTx removes units worth value at their own cost (a purchase return,
// after its stock has left), which moves the rate of what stays.
func takeOutOfCostTx(tx *sql.Tx, schema, sku string, qty float64, value int64) error {
	if qty <= 0 || value <= 0 {
		return nil
	}
	_, rate, err := lockItemCostTx(tx, schema, sku)
	if err != nil {
		return err
	}
	held, err := stockInHandForCost(tx, schema, sku)
	if err != nil {
		return err
	}
	return writeItemCostTx(tx, schema, sku, rateAfterTakingOutPaise(held, rate, qty, value), -qty, -value)
}

// correctReceiptCost is a correction to a posted GRN, whose stock has already
// moved: qty units and value paise (either sign) are what it booked to stock, and
// the pieces of that GRN still here move by priceChange each.
func correctReceiptCost(tenantID, sku, grnID string, qty float64, value, priceChange int64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := correctReceiptCostTx(tx, schema, sku, grnID, qty, value, priceChange); err != nil {
		return err
	}
	return tx.Commit()
}

// correctReceiptCostTx is correctReceiptCost in the caller's transaction (162.9).
func correctReceiptCostTx(tx *sql.Tx, schema, sku, grnID string, qty float64, value, priceChange int64) error {
	cum, rate, err := lockItemCostTx(tx, schema, sku)
	if err != nil {
		return err
	}
	held, err := stockInHandForCost(tx, schema, sku)
	if err != nil {
		return err
	}
	switch {
	case qty > 0 && cum <= 0:
		rate = blendRatePaise(0, rate, value, qty)
	case qty > 0:
		rate = blendRatePaise(held-qty, rate, value, qty)
	case cum > 0:
		rate = rateAfterTakingOutPaise(held, rate, -qty, -value)
	}
	if err := writeItemCostTx(tx, schema, sku, rate, qty, value); err != nil {
		return err
	}
	return raisePieceCostsTx(tx, schema, sku, grnID, priceChange)
}

// --- A bill or late charge that differs from what the GRN booked. ---

// grnCostBase is what one GRN booked for one SKU.
type grnCostBase struct {
	sku   string
	qty   float64
	value int64
}

// grnCostBases is the GRN's accepted lines at the unit cost its receipt booked,
// one row per SKU in SKU order, so two lines of a SKU cannot each claim the units
// on hand. A line with no resolvable cost was never booked and is left out.
func grnCostBases(tenantID, schema, grnID string, lines []map[string]interface{}) []grnCostBase {
	poID := grnPurchaseOrderID(schema, grnID)
	grnCosts := grnLineUnitCostsPaise(tenantID, grnID)
	bySKU := map[string]*grnCostBase{}
	for _, m := range lines {
		sku, _ := m["sku"].(string)
		accepted := grnLineAcceptedQty(m)
		if sku == "" || accepted <= 0 {
			continue
		}
		unitCostPaise, ok := grnCosts[sku]
		if !ok {
			unitCostPaise, ok = resolvePOBaseUnitCostPaise(tenantID, poID, sku)
		}
		if !ok {
			continue
		}
		b := bySKU[sku]
		if b == nil {
			b = &grnCostBase{sku: sku}
			bySKU[sku] = b
		}
		b.qty += accepted
		b.value += unitCostPaise * int64(accepted)
	}
	out := make([]grnCostBase, 0, len(bySKU))
	for _, b := range bySKU {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sku < out[j].sku })
	return out
}

// receiptUnitsInHand is how many of a receipt's units are still here. For bulk
// stock nobody can tell which unit left, so it is min(in hand, received); pieces
// that carry this GRN's name are counted.
func receiptUnitsInHand(tx *sql.Tx, schema, sku, grnID string, received, held float64) (float64, error) {
	units := math.Min(held, received)
	if grnID == "" {
		return units, nil
	}
	var serials, serialsHere int
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status IN ('%s', '%s')) FROM %s.documents
		WHERE doctype = 'SerialNumber' AND data @> jsonb_build_object('item', $1::text, 'receipt_grn', $2::text)`,
		SerialInStock, SerialAllocated, schema), sku, grnID).Scan(&serials, &serialsHere); err != nil {
		return 0, err
	}
	if serials > 0 {
		return math.Min(units, float64(serialsHere)), nil
	}
	var batches int
	var batchHere float64
	if err := tx.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(l.qty), 0)::float8 FROM %[1]s.documents b
		CROSS JOIN LATERAL (SELECT COALESCE(SUM((e.data->>'qty')::numeric), 0) AS qty FROM %[1]s.documents e
			WHERE e.doctype = 'StockLedgerEntry'
			  AND e.data @> jsonb_build_object('item_id', b.data->>'item', 'batch_no', b.data->>'batch_no')) l
		WHERE b.doctype = 'Batch' AND b.data @> jsonb_build_object('item', $1::text, 'grn', $2::text)
		  AND b.data ? 'receipt_cost_paise'`, schema), sku, grnID).Scan(&batches, &batchHere); err != nil {
		return 0, err
	}
	if batches > 0 {
		return math.Max(0, math.Min(units, batchHere)), nil
	}
	return units, nil
}

// raisePieceCostsTx moves the own cost of this GRN's pieces still here by their
// share of the charge.
func raisePieceCostsTx(tx *sql.Tx, schema, sku, grnID string, perUnit int64) error {
	if grnID == "" || perUnit == 0 {
		return nil
	}
	raise := `jsonb_set(data, '{receipt_cost_paise}', to_jsonb(GREATEST((data->>'receipt_cost_paise')::bigint + $1::bigint, 0)))`
	if _, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = %s, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SerialNumber' AND data @> jsonb_build_object('item', $2::text, 'receipt_grn', $3::text)
		  AND data ? 'receipt_cost_paise' AND status IN ('%s', '%s')`, schema, raise, SerialInStock, SerialAllocated),
		perUnit, sku, grnID); err != nil {
		return err
	}
	_, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = %s, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Batch' AND data @> jsonb_build_object('item', $2::text, 'grn', $3::text)
		  AND data ? 'receipt_cost_paise'`, schema, raise), perUnit, sku, grnID)
	return err
}

// applyCostAdjustmentTx moves a receipt's cost by amount (either sign): the share
// of the units still here goes into the rate, the share of the units already sold
// is returned for COGS variance. A cut cannot take stock below nothing; what it
// cannot take back from stock falls to the variance too. received <= 0 means the
// whole stock in hand carries it.
func applyCostAdjustmentTx(tx *sql.Tx, schema, sku, grnID string, received float64, amount int64) (onHand, sold int64, err error) {
	if amount == 0 {
		return 0, 0, nil
	}
	cum, rate, err := lockItemCostTx(tx, schema, sku)
	if err != nil {
		return 0, 0, err
	}
	held, err := stockInHandForCost(tx, schema, sku)
	if err != nil {
		return 0, 0, err
	}
	if held <= 0 || cum <= 0 {
		return 0, amount, writeItemCostTx(tx, schema, sku, rate, 0, amount)
	}
	if received <= 0 {
		received = held
	}
	units, err := receiptUnitsInHand(tx, schema, sku, grnID, received, held)
	if err != nil {
		return 0, 0, err
	}
	onHand = sharePaise(amount, units, received)
	if floor := -sharePaise(rate, held, 1); onHand < floor {
		onHand = floor
	}
	if err := writeItemCostTx(tx, schema, sku, rateAfterChargePaise(held, rate, onHand), 0, amount); err != nil {
		return 0, 0, err
	}
	if err := raisePieceCostsTx(tx, schema, sku, grnID, sharePaise(amount, 1, received)); err != nil {
		return 0, 0, err
	}
	return onHand, amount - onHand, nil
}

// addCostAdjustmentLegsTx is applyCostAdjustmentTx plus its GL legs, added to
// legs as signed nets. An item expensed at receipt never entered stock, so its
// difference goes to the same expense account.
func addCostAdjustmentLegsTx(tx *sql.Tx, tenantID, schema, sku, grnID string, received float64, amount int64, legs map[string]int64) (onHand, sold int64, err error) {
	if policy, _, known := ItemPolicyForSKU(tenantID, sku); known && !policy.Valuated() {
		legs[policy.ExpenseAccount] += amount
		return 0, 0, nil
	}
	onHand, sold, err = applyCostAdjustmentTx(tx, schema, sku, grnID, received, amount)
	if err != nil {
		return 0, 0, err
	}
	legs["1200"] += onHand
	legs[accountCOGSVariance] += sold
	return onHand, sold, nil
}

// postSignedLegsTx posts signed nets (+ debit, - credit). All zero posts nothing.
func postSignedLegsTx(tx *sql.Tx, tenantID, schema, docType, docID string, legs map[string]int64, key string) error {
	debits, credits := map[string]int64{}, map[string]int64{}
	for account, net := range legs {
		switch {
		case net > 0:
			debits[account] = net
		case net < 0:
			credits[account] = -net
		}
	}
	if len(debits) == 0 && len(credits) == 0 {
		return nil
	}
	return PostDoubleEntryTx(tx, tenantID, schema, docType, docID, debits, credits, "", key)
}

// billLine is one line of what the vendor billed (VendorInvoice.bill_lines).
type billLine struct {
	SKU     string  `json:"sku"`
	Qty     float64 `json:"qty"`
	Rate    float64 `json:"rate"`
	WeightG float64 `json:"weight_g"`
}

// vendorBillLines reads a bill's own lines. None means the whole-bill rule.
func vendorBillLines(invData map[string]interface{}) []billLine {
	raw, _ := invData["bill_lines"].(string)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var parsed, out []billLine
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return nil
	}
	for _, l := range parsed {
		if l.SKU = strings.TrimSpace(l.SKU); l.SKU != "" && l.Qty > 0 {
			out = append(out, l)
		}
	}
	return out
}

// lineWeightGrams reads a GRN line's actual weight in grams. A unit that is not a
// weight is no check at all.
func lineWeightGrams(m map[string]interface{}) (float64, bool) {
	weight := numFromInterface(m["actual_weight"])
	if weight <= 0 {
		return 0, false
	}
	switch strings.ToLower(strField(m, "weight_uom")) {
	case "", "g", "gm", "gms", "gram", "grams":
		return weight, true
	case "kg", "kgs", "kilogram", "kilograms":
		return weight * 1000, true
	case "mg", "milligram", "milligrams":
		return weight / 1000, true
	}
	return 0, false
}

// billLineReasons holds a bill line by line, in place of the whole-bill amount
// test: over-billing only (a line under the GRN rate is the vendor's gift, the
// difference still books, and a line for fewer units is a part bill). More units
// than the GRN has left to bill holds it. The price tolerance is the item type's,
// else the match's; the weight tolerance is the vendor's, in grams, and only a
// vendor who has one is checked.
func billLineReasons(tx *sql.Tx, tenantID, schema, invoiceID string, invData map[string]interface{}, grnID string, lines []billLine, taxable, tolerancePercent float64) []string {
	grnData, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return []string{fmt.Sprintf("GRN %s could not be read to check this bill's lines.", grnID)}
	}
	grnLines := grnLineMaps(grnData)
	returned, err := postedPurchaseReturnAcceptedQty(tx, schema, grnID)
	if err != nil {
		return []string{fmt.Sprintf("GRN %s could not be read to check this bill's lines.", grnID)}
	}
	others, err := billsElsewhereTx(tx, tenantID, schema, invoiceID, grnID, "", 1)
	if err != nil {
		return []string{fmt.Sprintf("GRN %s could not be read to check this bill's lines.", grnID)}
	}
	unitPaise, billable := map[string]int64{}, map[string]float64{}
	for _, b := range grnCostBases(tenantID, schema, grnID, grnLines) {
		unitPaise[b.sku] = sharePaise(b.value, 1, b.qty)
		billable[b.sku] = others.openUnits(b.sku, b.qty-returned[b.sku])
	}
	grams, weighed := map[string]float64{}, map[string]bool{}
	for _, m := range grnLines {
		if g, ok := lineWeightGrams(m); ok {
			grams[strField(m, "sku")] += g
			weighed[strField(m, "sku")] = true
		}
	}
	var weightTolerance float64
	if vendorID := strField(invData, "vendor_id"); vendorID != "" {
		if vendor, _, verr := fetchDocData(tenantID, "Vendor", vendorID); verr == nil {
			weightTolerance = numFromInterface(vendor["bill_weight_tolerance_g"])
		}
	}
	billedWeight, billedQty := map[string]float64{}, map[string]float64{}
	var reasons []string
	var total float64
	for _, l := range lines {
		total += l.Qty * l.Rate
		billedWeight[l.SKU] += l.WeightG
		billedQty[l.SKU] += l.Qty
		unit, known := unitPaise[l.SKU]
		if !known {
			reasons = append(reasons, fmt.Sprintf("%s is on this bill but has no value on GRN %s.", l.SKU, grnID))
			continue
		}
		tol := tolerancePercent
		if item, ierr := ResolveItemBySKU(tenantID, l.SKU); ierr == nil {
			if rules := ItemRules(tenantID, item.Data); rules.BillPriceToleranceSet {
				tol = rules.BillPriceTolerancePercent
			}
		}
		grnRate := PaiseToRupees(unit)
		if l.Rate > grnRate*(1+tol/100)+0.005 {
			reasons = append(reasons, fmt.Sprintf("%s billed at %.2f, GRN rate %.2f: over by more than %.2f%%.", l.SKU, l.Rate, grnRate, tol))
		}
	}
	for _, sku := range costKeys(billedQty) {
		if _, known := unitPaise[sku]; known && billedQty[sku] > billable[sku]+0.0005 {
			reasons = append(reasons, fmt.Sprintf("%s billed for %g, GRN %s has %g to bill.", sku, billedQty[sku], grnID, billable[sku]))
		}
	}
	for _, sku := range costKeys(billedWeight) {
		if billedWeight[sku] > 0 && weightTolerance > 0 && weighed[sku] && billedWeight[sku] > grams[sku]+weightTolerance {
			reasons = append(reasons, fmt.Sprintf("%s billed %.3f g, received %.3f g: over by more than %.3f g.", sku, billedWeight[sku], grams[sku], weightTolerance))
		}
	}
	if math.Abs(total-taxable) > 1 {
		reasons = append(reasons, fmt.Sprintf("This bill's lines add up to %.2f, its value before tax is %.2f.", total, taxable))
	}
	return reasons
}

func costKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// billsElsewhere is what the other standing bills on a GRN have settled.
type billsElsewhere struct {
	accounted int64              // what they billed less what they booked as a difference
	units     map[string]float64 // units they settled line by line
	whole     bool               // one lists no lines, so it took every unit
}

func (o billsElsewhere) openUnits(sku string, left float64) float64 {
	if o.whole {
		return 0
	}
	return math.Max(left-o.units[sku], 0)
}

// storedBillCost is what a bill has booked so far, per SKU.
func storedBillCost(invData map[string]interface{}) map[string]int64 {
	stored := map[string]int64{}
	if m, ok := invData["bill_cost_adjust"].(map[string]interface{}); ok {
		for sku, v := range m {
			stored[sku] = int64(math.Round(numFromInterface(v)))
		}
	}
	return stored
}

// billValuePaise is what a bill puts through 2100: its value before tax without
// the charge booked at the receipt. A bill never matched has no value before tax
// and clears whole; a foreign one clears at what the ledger carries.
func billValuePaise(tenantID string, d map[string]interface{}) int64 {
	if p := documentFXPosition(tenantID, d, "invoice_amount"); p.Foreign {
		return int64(p.CarryingAmount) * 100
	}
	value := d["taxable_amount"]
	if value == nil {
		value = d["invoice_amount"]
	}
	return RupeesToPaise(numFromInterface(value)) - RupeesToPaise(numFromInterface(d["charge_net"]))
}

// billsElsewhereTx reads the other bills on a GRN. What each billed less what it
// booked is the part of the receipt it accounts for, so a bill from before a
// second bill could settle counts the same as one after. A note's bill booked its
// difference with the tax in it (see stepNoteBillTx).
func billsElsewhereTx(tx *sql.Tx, tenantID, schema, invoiceID, grnID, noteBill string, noteRatio float64) (billsElsewhere, error) {
	out := billsElsewhere{units: map[string]float64{}}
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND id <> $1
		AND status IN (%s) AND data->>'grn_id' = $2`, schema, billableVendorInvoiceStatuses), invoiceID, grnID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return out, err
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return out, err
		}
		var booked int64
		for _, v := range storedBillCost(d) {
			booked += v
		}
		if id == noteBill {
			booked = sharePaise(booked, 1, noteRatio)
		}
		out.accounted += billValuePaise(tenantID, d) - booked
		if m, ok := d["bill_cost_units"].(map[string]interface{}); ok {
			for sku, v := range m {
				out.units[sku] += numFromInterface(v)
			}
		} else if lines := vendorBillLines(d); len(lines) > 0 {
			for _, l := range lines {
				out.units[l.SKU] += l.Qty
			}
		} else {
			out.whole = true
		}
	}
	return out, rows.Err()
}

// stepBillBases is what a GRN received in steps expects of its bill: the invoice
// keyed at Receive. Stock went in at our verified price; the gap between the two
// is the note's (Stage 94 D1), so only what a bill differs from the invoice is a
// difference in cost.
func stepBillBases(lines []map[string]interface{}) []grnCostBase {
	bySKU := map[string]*grnCostBase{}
	for _, l := range lines {
		sku, qty := strField(l, "sku"), grnInvoiceQty(l)
		if sku == "" || qty <= 0 {
			continue
		}
		b := bySKU[sku]
		if b == nil {
			b = &grnCostBase{sku: sku}
			bySKU[sku] = b
		}
		value, _ := grnInvoiceLineAmount(l)
		b.qty += qty
		b.value += RupeesToPaise(value) + grnStockRoundGapPaise(l) // 148.35
	}
	out := make([]grnCostBase, 0, len(bySKU))
	for _, sku := range costKeys(bySKU) {
		out = append(out, *bySKU[sku])
	}
	return out
}

// stepNoteBillTx is the bill a step GRN's note lands on, and the tax ratio its
// difference moves at: the note puts that bill's input credit at our figure, so
// the whole gross of what is left runs through 2100, not the value before tax.
func stepNoteBillTx(tx *sql.Tx, tenantID, schema, grnID string, grnData map[string]interface{}, lines []map[string]interface{}) (string, float64, error) {
	stampStepGRNCharges(tx, tenantID, schema, grnID, grnData, lines)
	grnStampTotals(grnData, lines)
	billing := readStepGRNBilling(grnData, lines)
	if billing == nil || billing.TaxRatio <= 0 || math.Abs(numFromInterface(grnData["note_difference"])) < 0.005 {
		return "", 1, nil
	}
	id := strField(grnData, "vendor_bill_id")
	if id == "" {
		// Not linked yet: the one ensureStepGRNVendorBill takes.
		err := tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND data->>'grn_id' = $1 ORDER BY created_at LIMIT 1`, schema), grnID).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return "", 1, err
		}
	}
	return id, billing.TaxRatio, nil
}

const billSkipStepNotPosted = "GRN not posted yet"

// billAwaitedStepGRN is a bill matched before its GRN finished posting, so it
// settled nothing: the posting matches it again, at the tolerance it passed at.
func billAwaitedStepGRN(bill map[string]interface{}, status string) (bool, float64) {
	details, _ := bill["match_details"].(map[string]interface{})
	switch strField(details, "cost_difference_skipped") {
	case billSkipStepNotPosted, "GRN received in steps":
		if status == "Matched" && !vendorInvoiceHasPayments(bill) {
			return true, numFromInterface(details["tolerance_percent"])
		}
	}
	return false, 0
}

// billCostTargets is, per SKU, how far the bill's value before tax is from what
// the GRN has left to account for, and the units a bill by lines settled. A
// difference under a rupee is round-off. Empty with a reason when the bill does
// not settle here: a held bill, a foreign currency bill (realised FX books at
// payment), no GRN (nothing was booked to differ from), a receipt with no rates,
// or a step GRN still being posted.
func billCostTargets(tx *sql.Tx, tenantID, schema, invoiceID string, invData map[string]interface{}, grnID string, matched, rateMissing bool) (target map[string]int64, units, received map[string]float64, skipped string, err error) {
	target, units, received = map[string]int64{}, map[string]float64{}, map[string]float64{}
	if grnID == "" {
		return target, units, received, "No GRN", nil
	}
	grnData, _, ferr := fetchDocData(tenantID, "GRN", grnID)
	if ferr != nil {
		return target, units, received, "GRN not readable", nil
	}
	lines := grnLineMaps(grnData)
	bases := grnCostBases(tenantID, schema, grnID, lines)
	for _, b := range bases {
		received[b.sku] = b.qty
	}
	switch {
	case !matched:
		return target, units, received, "Bill on hold", nil
	case rateMissing:
		return target, units, received, "No rates on the PO", nil
	case documentFXPosition(tenantID, invData, "invoice_amount").Foreign:
		return target, units, received, "Foreign currency bill", nil
	}
	noteBill, noteRatio := "", 1.0
	roundOff := int64(100) // paise under which a difference is left as round-off
	if IsGRNStepFlow(grnData) {
		if strField(grnData, "grn_step") != GRNStepPosted {
			return target, units, received, billSkipStepNotPosted, nil
		}
		bases = stepBillBases(lines)
		for _, l := range lines {
			if grnLineSettlesRound(l) { // 148.51: this rounding is ours, so it clears to the paisa
				roundOff = 1
			}
		}
		if noteBill, noteRatio, err = stepNoteBillTx(tx, tenantID, schema, grnID, grnData, lines); err != nil {
			return nil, nil, nil, "", err
		}
	}
	returned, err := postedPurchaseReturnAcceptedQty(tx, schema, grnID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	others, err := billsElsewhereTx(tx, tenantID, schema, invoiceID, grnID, noteBill, noteRatio)
	if err != nil {
		return nil, nil, nil, "", err
	}
	left := map[string]float64{}
	var booked int64
	for i := range bases {
		b := &bases[i]
		if r := returned[b.sku]; r > 0 {
			b.value -= sharePaise(b.value, math.Min(r, b.qty), b.qty)
		}
		left[b.sku] = b.qty - math.Min(returned[b.sku], b.qty)
		booked += b.value
	}
	if billLines := vendorBillLines(invData); len(billLines) > 0 {
		// Only what the bill lists settles: those units at the bill's rate against
		// the same units at the GRN's. An item or unit it leaves out is still to bill;
		// one another bill already settled is a difference whole.
		billed, billedQty := map[string]int64{}, map[string]float64{}
		for _, l := range billLines {
			billed[l.SKU] += RupeesToPaise(l.Rate * l.Qty)
			billedQty[l.SKU] += l.Qty
		}
		for _, b := range bases {
			if billedQty[b.sku] <= 0 || left[b.sku] <= 0 {
				continue
			}
			units[b.sku] = math.Min(billedQty[b.sku], others.openUnits(b.sku, left[b.sku]))
			if d := billed[b.sku] - sharePaise(b.value, units[b.sku], left[b.sku]); d >= roundOff || d <= -roundOff {
				target[b.sku] = d
			}
		}
	} else {
		// Stage 148.8: a charge on the bill was booked when the receipt posted.
		diff := billValuePaise(tenantID, invData) + others.accounted - booked
		if diff > -roundOff && diff < roundOff || booked <= 0 {
			return target, units, received, "", nil
		}
		// Across what is still to bill, so a second bill's difference lands on the
		// items it is for.
		weight, open := map[string]int64{}, int64(0)
		for _, b := range bases {
			weight[b.sku] = sharePaise(b.value, others.openUnits(b.sku, left[b.sku]), left[b.sku])
			open += weight[b.sku]
		}
		if open <= 0 {
			for _, b := range bases {
				weight[b.sku] = b.value
			}
			open = booked
		}
		var spread int64
		for i, b := range bases {
			share := diff - spread
			if i < len(bases)-1 {
				share = sharePaise(diff, float64(weight[b.sku]), float64(open))
			}
			spread += share
			target[b.sku] = share
		}
	}
	if noteBill == invoiceID {
		for sku, d := range target {
			target[sku] = sharePaise(d, noteRatio, 1)
		}
	}
	return target, units, received, "", nil
}

// settleBillCostDifferenceTx books a matched bill's difference from the GRN at
// bill time: the share of units still here into their cost, the share already
// sold to COGS variance, the whole against 2100, which the GRN credited at the
// booked value and the payment will debit at the billed one. What the bill has
// booked so far is kept on it, so a re-match only moves the change and a hold
// takes it all back.
func settleBillCostDifferenceTx(tx *sql.Tx, tenantID, schema, invoiceID string, invData, details map[string]interface{}, grnID string, matched, rateMissing bool) error {
	if grnID != "" {
		// One bill at a time on a GRN: each reads what the others settled.
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":bill-cost:"+grnID); err != nil {
			return err
		}
	}
	target, units, received, skipped, err := billCostTargets(tx, tenantID, schema, invoiceID, invData, grnID, matched, rateMissing)
	if err != nil {
		return err
	}
	stored := storedBillCost(invData)
	// A matched bill that does not settle here keeps what it booked; only a hold
	// takes it back.
	if matched && skipped != "" {
		target = stored
	}
	all := map[string]bool{}
	for sku := range target {
		all[sku] = true
	}
	for sku := range stored {
		all[sku] = true
	}
	legs := map[string]int64{}
	var moved, totalTarget int64
	onHand := int64(math.Round(numFromInterface(invData["bill_cost_on_hand"])))
	sold := int64(math.Round(numFromInterface(invData["bill_cost_sold"])))
	for _, sku := range costKeys(all) {
		totalTarget += target[sku]
		delta := target[sku] - stored[sku]
		if delta == 0 {
			continue
		}
		h, s, aerr := addCostAdjustmentLegsTx(tx, tenantID, schema, sku, grnID, received[sku], delta, legs)
		if aerr != nil {
			return fmt.Errorf("cost of %s: %w", sku, aerr)
		}
		moved += delta
		onHand += h
		sold += s
	}
	legs["2100"] -= moved
	posts := int(numFromInterface(invData["bill_cost_posts"])) + 1
	if err := postSignedLegsTx(tx, tenantID, schema, "VendorInvoice", invoiceID, legs,
		fmt.Sprintf("VendorInvoice:%s:BILL_DIFF:%d", invoiceID, posts)); err != nil {
		return fmt.Errorf("bill difference posting: %w", err)
	}
	// The units a bill by lines settled are its own, so a re-match reads the same.
	if !matched || (skipped == "" && len(units) == 0) {
		delete(invData, "bill_cost_units")
	} else if skipped == "" {
		invData["bill_cost_units"] = units
	}
	if len(target) == 0 && len(stored) == 0 {
		if skipped != "" {
			details["cost_difference_skipped"] = skipped
		}
		return nil
	}
	invData["bill_cost_adjust"] = target
	invData["bill_cost_posts"] = posts
	invData["bill_cost_on_hand"] = onHand
	invData["bill_cost_sold"] = sold
	if skipped != "" {
		details["cost_difference_skipped"] = skipped
	}
	details["cost_difference"] = PaiseToRupees(totalTarget)
	details["on_hand_share"] = PaiseToRupees(onHand)
	details["sold_share"] = PaiseToRupees(sold)
	return nil
}

// settleOverriddenBillTx books a held bill's difference when a checker's override
// pays it, in the payment's transaction: the payment debits 2100 at the billed
// value, so the gap has to reach cost now. The bill's own record makes a bill that
// was matched first move nothing here. A bill never matched has no value before
// tax to compare, and says so.
func settleOverriddenBillTx(tx *sql.Tx, tenantID, schema, invoiceID string, data map[string]interface{}) error {
	details, _ := data["match_details"].(map[string]interface{})
	if details == nil {
		details = map[string]interface{}{}
	}
	delete(details, "cost_difference_skipped")
	data["match_details"] = details
	if data["taxable_amount"] == nil {
		details["cost_difference_skipped"] = "Never matched"
		return nil
	}
	// An amount corrected after the last match: what this payment clears is what settles.
	amount, gst := numFromInterface(data["invoice_amount"]), numFromInterface(data["gst_amount"])
	if math.Abs(numFromInterface(data["taxable_amount"])+gst-amount) > 0.005 {
		data["taxable_amount"] = round2(amount - gst)
	}
	rateMissing, _ := details["rate_data_missing"].(bool)
	return settleBillCostDifferenceTx(tx, tenantID, schema, invoiceID, data, details, strField(data, "grn_id"), true, rateMissing)
}

// ReverseVendorBillDifference takes back what a bill booked when it is cancelled
// or deleted, the reverse of the same posting and key shape. A bill with money or
// a decision on it is refused; one that booked nothing posts nothing, and a retry
// finds nothing left to take back.
func ReverseVendorBillDifference(tenantID, invoiceID, userID string) error {
	// Stage 148.19: a bill without a PO takes back its own journals.
	if handled, err := reverseExpenseBill(tenantID, invoiceID); handled || err != nil {
		return err
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
	var raw, status string
	err = tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), invoiceID).Scan(&raw, &status)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	if vendorInvoiceHasPayments(data) || status == "Paid" || status == "Approved" || status == "Pending Approval" {
		return &ValidationError{Code: "FIN-0910", Message: "This bill has a payment or an approval on it."}
	}
	if len(storedBillCost(data)) == 0 && data["bill_cost_units"] == nil {
		return nil
	}
	details, _ := data["match_details"].(map[string]interface{})
	if details == nil {
		details = map[string]interface{}{}
	}
	if err := settleBillCostDifferenceTx(tx, tenantID, schema, invoiceID, data, details, strField(data, "grn_id"), false, false); err != nil {
		return err
	}
	details["cost_difference_skipped"] = "Bill cancelled"
	data["match_details"] = details
	updated, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema), updated, invoiceID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "REVERSE_VENDOR_BILL_DIFFERENCE", "SUCCESS", "Vendor bill "+invoiceID)
	return nil
}

// --- What a sale booked, kept on the sale so a return reverses that figure. ---

// pieceList reads a line's serial_nos: a list, or a code or comma-separated codes
// typed into one cell. It never fails the line it rides on.
type pieceList []string

func (p *pieceList) UnmarshalJSON(raw []byte) error {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		*p = list
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		for _, s := range strings.Split(text, ",") {
			if s = strings.TrimSpace(s); s != "" {
				*p = append(*p, s)
			}
		}
	}
	return nil
}

type soldPiece struct {
	SerialNo  string `json:"serial_no"`
	CostPaise int64  `json:"cost_paise"`
}

// saleCostLine is one sold line's cost, and where the rate came from.
type saleCostLine struct {
	SKU        string      `json:"sku"`
	Qty        int         `json:"qty"`
	ValuePaise int64       `json:"value_paise"`
	Source     string      `json:"source"`
	BatchNo    string      `json:"batch_no,omitempty"`
	Pieces     []soldPiece `json:"pieces,omitempty"`
	// Parts is a bundle's components: one record for the sold line, so a return of
	// the line reads one figure and a component sold on the same bill stays apart.
	Parts     []saleCostLine `json:"parts,omitempty"`
	serialNos []string
	unitPaise int64
}

func newSaleCostLine(tenantID, sku string, qty int, unitPaise int64, serialNos []string, batchNo string) saleCostLine {
	source := costSourceNone
	if unitPaise > 0 {
		source = costSourceStandard
		if _, hasCost, err := GetItemUnitCost(tenantID, sku); err == nil && hasCost {
			source = costSourceAverage
		}
	}
	return saleCostLine{SKU: sku, Qty: qty, ValuePaise: unitPaise * int64(qty), Source: source,
		BatchNo: strings.TrimSpace(batchNo), serialNos: serialNos, unitPaise: unitPaise}
}

// markStockLineBatch names the lot on a sale's stock lines, so the ledger can say
// what is left of it.
func markStockLineBatch(stock []interface{}, batchNo string) {
	batchNo = strings.TrimSpace(batchNo)
	if batchNo == "" {
		return
	}
	for _, line := range stock {
		if m, ok := line.(map[string]interface{}); ok {
			m["batch_no"] = batchNo
		}
	}
}

// grnPieceCostResolver is what a GRN booked per unit: the cost a piece or batch
// from it carries. An item expensed at receipt never entered stock, so none.
func grnPieceCostResolver(tenantID, schema, grnID string) func(sku string) int64 {
	poID := grnPurchaseOrderID(schema, grnID)
	grnCosts := grnLineUnitCostsPaise(tenantID, grnID)
	charges := grnChargePerUnit(schema, grnID) // Stage 148.8: what the receipt's charges added per unit
	return func(sku string) int64 {
		if policy, _, known := ItemPolicyForSKU(tenantID, sku); known && !policy.Valuated() {
			return 0
		}
		if unit, ok := grnCosts[sku]; ok {
			return unit + charges[sku]
		}
		unit, _ := resolvePOBaseUnitCostPaise(tenantID, poID, sku)
		return unit + charges[sku]
	}
}

// pieceItemCode is the item a scanned piece code belongs to. A code two items
// share names neither.
func pieceItemCode(tenantID, code string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT DISTINCT data->>'item' FROM %s.documents WHERE doctype = 'SerialNumber' AND deleted_at IS NULL
		  AND data @> jsonb_build_object('serial_no', $1::text) LIMIT 2`, schema), strings.TrimSpace(code))
	if err != nil {
		return ""
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var sku string
		if rows.Scan(&sku) == nil {
			items = append(items, sku)
		}
	}
	if len(items) != 1 {
		return ""
	}
	return items[0]
}

func refusePiece(format string, args ...interface{}) error {
	return &ValidationError{Code: "FIN-0850", Message: fmt.Sprintf(format, args...)}
}

// namedCostTx prices the units this line names. A piece stamped at its receipt
// costs that; an older one costs the line's average. An offline sale has already
// left the store, so what cannot be named falls back to the average.
func (l *saleCostLine) namedCostTx(tx *sql.Tx, schema string, offline bool, customerID string) (own int64, units int, stamped bool, err error) {
	if len(l.serialNos) > l.Qty {
		if !offline {
			return 0, 0, false, refusePiece("%d pieces named for %d of %s", len(l.serialNos), l.Qty, l.SKU)
		}
		l.serialNos = l.serialNos[:l.Qty]
	}
	for _, no := range l.serialNos {
		no = strings.TrimSpace(no)
		docID, piece, lerr := lockSerialTx(tx, schema, l.SKU, no)
		if _, invalid := lerr.(*ValidationError); invalid {
			if offline {
				continue
			}
			return 0, 0, false, refusePiece("piece %s of %s is not registered", no, l.SKU)
		} else if lerr != nil {
			return 0, 0, false, lerr
		}
		if !piece.Allocatable() {
			if offline {
				continue
			}
			return 0, 0, false, refusePiece("piece %s of %s is %s, it cannot be sold", no, l.SKU, piece.Status)
		}
		var heldFields map[string]interface{} // Stage 148.41: another customer's hold stops the sale; the holder's sale ends it
		if piece.HoldCustomer != "" {
			if herr := pieceHoldRefusal(piece, customerID); herr != nil && !offline {
				return 0, 0, false, herr
			}
			heldFields = pieceHoldClear()
		}
		cost := l.unitPaise
		if piece.ReceiptCostPaise > 0 {
			cost, stamped = piece.ReceiptCostPaise, true
		}
		own += cost
		units++
		l.Pieces = append(l.Pieces, soldPiece{SerialNo: no, CostPaise: cost})
		if err := patchSerialTx(tx, schema, docID, SerialShipped, heldFields); err != nil {
			return 0, 0, false, err
		}
	}
	if units > 0 || l.BatchNo == "" {
		return own, units, stamped, nil
	}
	var dataStr string
	berr := tx.QueryRow(fmt.Sprintf(`
		SELECT data FROM %s.documents WHERE doctype = 'Batch' AND deleted_at IS NULL
		  AND data @> jsonb_build_object('item', $1::text, 'batch_no', $2::text) LIMIT 1`, schema),
		l.SKU, l.BatchNo).Scan(&dataStr)
	if berr == sql.ErrNoRows {
		if offline {
			return 0, 0, false, nil
		}
		return 0, 0, false, refusePiece("batch %s of %s is not registered", l.BatchNo, l.SKU)
	} else if berr != nil {
		return 0, 0, false, berr
	}
	if cost := batchFromData("", dataStr, "").ReceiptCostPaise; cost > 0 {
		return cost * int64(l.Qty), l.Qty, true, nil
	}
	return 0, 0, false, nil
}

// settleNamedPiecesTx runs after the sale's stock has moved. Lines naming a piece
// or batch cost what it cost, and the difference from the average comes back to
// the caller's COGS; the rate of what stays moves so stock in hand keeps its
// value. Unnamed units touch nothing here, as they never did.
func settleNamedPiecesTx(tx *sql.Tx, schema string, lines []saleCostLine, offline bool, customerID string) (int64, error) {
	order := make([]int, len(lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return lines[order[a]].SKU < lines[order[b]].SKU })
	var delta int64
	for _, i := range order {
		l := &lines[i]
		if len(l.serialNos) == 0 && l.BatchNo == "" {
			continue
		}
		own, units, stamped, err := l.namedCostTx(tx, schema, offline, customerID)
		if err != nil {
			return 0, err
		}
		if !stamped {
			continue
		}
		d := own - l.unitPaise*int64(units)
		l.ValuePaise += d
		l.Source = costSourceOwn
		delta += d
		_, rate, err := lockItemCostTx(tx, schema, l.SKU)
		if err != nil {
			return 0, err
		}
		held, err := stockInHandForCost(tx, schema, l.SKU)
		if err != nil {
			return 0, err
		}
		if err := writeItemCostTx(tx, schema, l.SKU, rateAfterTakingOutPaise(held, rate, float64(units), own), 0, 0); err != nil {
			return 0, err
		}
	}
	return delta, nil
}

func saleCostsJSON(lines []saleCostLine) string {
	if lines == nil {
		lines = []saleCostLine{}
	}
	raw, _ := json.Marshal(lines)
	return string(raw)
}

// readSaleCosts reads what a sale booked. A sale from before this stage has none.
func readSaleCosts(dataStr string) []saleCostLine {
	var d struct {
		SaleCosts []saleCostLine `json:"sale_costs"`
	}
	_ = json.Unmarshal([]byte(dataStr), &d)
	return d.SaleCosts
}

// useBookedSaleCosts makes a return's cost basis what the sale itself booked, not
// today's rate, which would post a different figure than the sale took out of
// stock and move the average.
func useBookedSaleCosts(prices map[string]originalLinePrice, dataStr string) {
	type sum struct {
		qty      int
		value    int64
		pieces   []soldPiece
		batch    string
		oneBatch bool
	}
	sums := map[string]*sum{}
	for _, l := range readSaleCosts(dataStr) {
		// A bundle's record is kept but not returned against: what comes back today
		// is the kit's own code, which holds no cost, so the GL would run ahead of the
		// stock value. Its components coming back is a decision for later (148.1).
		if len(l.Parts) > 0 {
			continue
		}
		s := sums[l.SKU]
		if s == nil {
			s = &sum{batch: l.BatchNo, oneBatch: true}
			sums[l.SKU] = s
		}
		s.qty += l.Qty
		s.value += l.ValuePaise
		s.pieces = append(s.pieces, l.Pieces...)
		s.oneBatch = s.oneBatch && s.batch == l.BatchNo
	}
	for sku, s := range sums {
		p, ok := prices[sku]
		if !ok || s.qty <= 0 {
			continue
		}
		p.CostPrice = PaiseToRupees(sharePaise(s.value, 1, float64(s.qty)))
		p.SoldQty, p.Pieces = s.qty, s.pieces
		if s.oneBatch {
			p.BatchNo = s.batch
		}
		prices[sku] = p
	}
}

// returnedPieces picks the sold pieces a return line brings back: the one it
// names, or all of them when it brings back the whole line. Otherwise nobody can
// say which piece came back and it comes back at the line's mean.
func (p originalLinePrice) returnedPieces(it ReturnItemInput) (cost float64, serials []string) {
	cost = p.CostPrice
	if no := strings.TrimSpace(it.SerialNo); no != "" {
		for _, piece := range p.Pieces {
			if piece.SerialNo == no && it.Qty == 1 {
				return PaiseToRupees(piece.CostPaise), []string{no}
			}
		}
		return cost, nil
	}
	if len(p.Pieces) == it.Qty && p.SoldQty == it.Qty && it.Qty > 0 {
		var total int64
		for _, piece := range p.Pieces {
			total += piece.CostPaise
			serials = append(serials, piece.SerialNo)
		}
		return PaiseToRupees(sharePaise(total, 1, float64(it.Qty))), serials
	}
	return cost, nil
}

// returnCostsToAverageTx runs after a QC has put the returned units back: their
// booked cost (the figure the COGS reversal debits to 1200) blends into the
// average, and a named piece goes back in stock. A line with no booked cost posts
// nothing and enters at the rate, as an RTO always has.
func returnCostsToAverageTx(tx *sql.Tx, schema, location string, items []returnItemRecord) error {
	type sum struct {
		qty   float64
		value int64
	}
	sums := map[string]*sum{}
	for _, it := range items {
		rule, ok := returnDispositionRule[it.Disposition]
		if !ok || !rule.ReceivesStock {
			continue
		}
		for _, no := range it.SerialNos {
			status := SerialReturned
			if rule.Bucket == "available" {
				status = SerialInStock
			}
			docID, piece, err := lockSerialTx(tx, schema, it.SKU, no)
			if err != nil {
				if _, invalid := err.(*ValidationError); invalid {
					continue
				}
				return err
			}
			if piece.Status != SerialShipped {
				continue
			}
			if err := patchSerialTx(tx, schema, docID, status, map[string]interface{}{"location_code": location}); err != nil {
				return err
			}
		}
		if it.OriginalCostPrice <= 0 {
			continue
		}
		s := sums[it.SKU]
		if s == nil {
			s = &sum{}
			sums[it.SKU] = s
		}
		s.qty += float64(it.Qty)
		s.value += RupeesToPaise(it.OriginalCostPrice * float64(it.Qty))
	}
	for _, sku := range costKeys(sums) {
		if err := blendIntoCostTx(tx, schema, sku, sums[sku].qty, sums[sku].value, sums[sku].qty, false); err != nil {
			return fmt.Errorf("item cost for %s: %w", sku, err)
		}
	}
	return nil
}

// saleCostsBackTx takes a cancelled sale's stock back at the figures it booked.
// A sale from before this stage booked none: its units enter at the rate.
func saleCostsBackTx(tx *sql.Tx, schema, location, saleData string) error {
	var items []returnItemRecord
	for _, l := range readSaleCosts(saleData) {
		if l.Qty <= 0 {
			continue
		}
		record := returnItemRecord{SKU: l.SKU, Qty: l.Qty, Disposition: "Sellable",
			OriginalCostPrice: PaiseToRupees(sharePaise(l.ValuePaise, 1, float64(l.Qty)))}
		for _, piece := range l.Pieces {
			record.SerialNos = append(record.SerialNos, piece.SerialNo)
		}
		items = append(items, record)
	}
	return returnCostsToAverageTx(tx, schema, location, items)
}

// creditNotePieceBackTx brings one sold piece back on a credit note that names it
// (serial_no, the sale in sales_invoice_id, the store in return_location): stock,
// ledger, COGS reversal and average at the cost the sale booked. A note that names
// no piece gives money back and never touches cost.
func creditNotePieceBackTx(tx *sql.Tx, tenantID, schema, noteID, userID string, data map[string]interface{}) error {
	no := strField(data, "serial_no")
	if no == "" {
		return nil
	}
	refuse := func(format string, args ...interface{}) error {
		return &ValidationError{Code: "FIN-0851", Message: fmt.Sprintf(format, args...)}
	}
	saleID, location := strField(data, "sales_invoice_id"), strField(data, "return_location")
	if saleID == "" || location == "" {
		return refuse("a credit note naming piece %s also needs its sale and the store it comes back to", no)
	}
	var saleStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype IN ('SalesInvoice', 'POSCart') AND id = $1`, schema), saleID).Scan(&saleStr); err != nil {
		return refuse("sale %s not found for piece %s", saleID, no)
	}
	var sku string
	var cost int64
	dispatched, err := dispatchedSaleCosts(schema, saleOrderOf(saleStr))
	if err != nil {
		return err
	}
	for _, l := range append(readSaleCosts(saleStr), dispatched...) {
		for _, piece := range l.Pieces {
			if piece.SerialNo == no {
				sku, cost = l.SKU, piece.CostPaise
			}
		}
	}
	if sku == "" {
		return refuse("piece %s was not sold on %s", no, saleID)
	}
	docID, piece, err := lockSerialTx(tx, schema, sku, no)
	if err != nil {
		return err
	}
	if piece.Status != SerialShipped {
		return refuse("piece %s of %s is %s, not sold", no, sku, piece.Status)
	}
	if err := applyReturnedStockToBucket(tx, schema, sku, location, 1, "available"); err != nil {
		return err
	}
	if err := insertStockLedgerEntry(tx, schema, StockLedgerEntry{
		ItemID: sku, WarehouseID: location, Qty: 1, VoucherType: "CreditNote", VoucherID: noteID,
		IdempotencyKey: fmt.Sprintf("CreditNote:%s:STOCK", noteID), ToStatus: "available", SerialNo: no, UserID: userID,
	}); err != nil {
		return err
	}
	if cost > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "CreditNote", noteID,
			map[string]int64{"1200": cost}, map[string]int64{"5100": cost}, "",
			fmt.Sprintf("CreditNote:%s:COGS_REVERSAL", noteID)); err != nil {
			return err
		}
		if err := blendIntoCostTx(tx, schema, sku, 1, cost, 1, false); err != nil {
			return err
		}
	}
	return patchSerialTx(tx, schema, docID, SerialInStock, map[string]interface{}{"location_code": location})
}

// --- Stock value. ---

type pieceTotal struct {
	units int64
	value int64
}

// covers says the pieces ARE this item's stock, so they are its value. Rounding
// aside the sum equals on hand x rate by construction (the rate moves with every
// named piece). A wider gap means units left unnamed, and the books hold on hand x
// rate, so the report stays on that.
func (p pieceTotal) covers(qty, rate int64) bool {
	gap := p.value - qty*rate
	return p.units == qty && qty > 0 && gap <= qty && gap >= -qty
}

// pieceCostTotals sums the own cost of pieces in hand per item, for items whose
// every piece in hand carries one.
func pieceCostTotals(q queryRower, schema string) (map[string]pieceTotal, error) {
	out := map[string]pieceTotal{}
	read := func(query string, args ...interface{}) error {
		rows, err := q.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sku string
			var t pieceTotal
			if err := rows.Scan(&sku, &t.units, &t.value); err != nil {
				return err
			}
			out[sku] = t
		}
		return rows.Err()
	}
	if err := read(fmt.Sprintf(`
		SELECT b.item, COALESCE(ROUND(SUM(l.qty)), 0)::bigint, COALESCE(ROUND(SUM(l.qty * b.cost)), 0)::bigint
		FROM (SELECT data->>'item' AS item, data->>'batch_no' AS batch_no, (data->>'receipt_cost_paise')::bigint AS cost
		      FROM %[1]s.documents WHERE doctype = 'Batch' AND deleted_at IS NULL AND data ? 'receipt_cost_paise') b
		CROSS JOIN LATERAL (SELECT COALESCE(SUM((e.data->>'qty')::numeric), 0) AS qty FROM %[1]s.documents e
			WHERE e.doctype = 'StockLedgerEntry' AND e.data @> jsonb_build_object('item_id', b.item, 'batch_no', b.batch_no)) l
		GROUP BY b.item`, schema)); err != nil {
		return nil, err
	}
	if err := read(fmt.Sprintf(`
		SELECT data->>'item', COUNT(*), COALESCE(SUM((data->>'receipt_cost_paise')::bigint), 0)::bigint
		FROM %s.documents WHERE doctype = 'SerialNumber' AND deleted_at IS NULL AND status IN ($1, $2)
		GROUP BY data->>'item'
		HAVING COUNT(*) = COUNT(*) FILTER (WHERE data ? 'receipt_cost_paise')`, schema), SerialInStock, SerialAllocated); err != nil {
		return nil, err
	}
	return out, nil
}

// ownCostAdjustmentPaise is how far the sum of own costs sits from on hand x rate,
// so a total taken as on hand x rate ties with the report's rows.
func ownCostAdjustmentPaise(q queryRower, schema string) (int64, error) {
	totals, err := pieceCostTotals(q, schema)
	if err != nil || len(totals) == 0 {
		return 0, err
	}
	rows, err := q.Query(fmt.Sprintf(`
		SELECT ia.sku, SUM(ia.on_hand)::bigint, COALESCE(MAX(ic.avg_unit_cost_paise), 0)
		FROM %s.inventory_availability ia LEFT JOIN %s.item_cost ic ON ic.item_code = ia.sku
		GROUP BY ia.sku HAVING SUM(ia.on_hand) <> 0`, schema, schema))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var adjust int64
	for rows.Next() {
		var sku string
		var qty, rate int64
		if err := rows.Scan(&sku, &qty, &rate); err != nil {
			return 0, err
		}
		if t, ok := totals[sku]; ok && t.covers(qty, rate) {
			adjust += t.value - qty*rate
		}
	}
	return adjust, rows.Err()
}
