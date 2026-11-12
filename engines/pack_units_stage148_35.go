package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Stage 148.35: buy in packs, stock in pieces (setting inventory.pack_units, off).
//
// An item may carry a purchase unit and how many stock units are in one
// (purchase_unit, purchase_unit_factor), per item, never global. A PO or GRN line
// may then be keyed in packs (pack_qty, and the bill's price per pack) and is
// converted once, at the document's save, to the stock quantity and a rate per
// stock unit. Everything after that - GRN posting, stock, journal, three-way
// match, landed cost, the moving average, a PO return - reads those stock-unit
// figures exactly as before, so none of it needed a change. The line keeps the
// factor it used (pack_factor): changing the item's factor later never touches
// a document already written. Off, the pack keys are dropped and nothing differs.
const (
	SettingPackUnits = "inventory.pack_units"

	packYes = "Yes"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingPackUnits, Module: "Inventory", Label: "Buy in packs, stock in pieces",
		Type: SettingTypeBool, Default: "false",
		Description: "On: an item can have a purchase unit (a pack of 12), and the PO and GRN lines can be keyed in packs. Stock, cost and the bill count pieces. An item's stock unit locks after its first movement. Off: quantities are in stock units only."})
	// The Item boxes appear only while the setting is on (the meta filter in doctype.go).
	if settingGatedFields["Item"] == nil {
		settingGatedFields["Item"] = map[string]string{}
	}
	for _, f := range []string{"uom", "purchase_unit", "purchase_unit_factor", "allow_fraction"} {
		settingGatedFields["Item"][f] = SettingPackUnits
	}
}

func packErr(code, sub, format string, args ...interface{}) error {
	return &ValidationError{Code: code, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// packOffer is what an item offers a PO line: its purchase unit and factor, and
// only while the setting is on.
func packOffer(tenantID string, item map[string]interface{}) (string, float64) {
	if !GetSettingBool(tenantID, SettingPackUnits) {
		return "", 0
	}
	unit := strField(item, "purchase_unit")
	f := rebateNum(item, "purchase_unit_factor")
	if unit == "" || f <= 0 {
		return "", 0
	}
	return unit, f
}

// itemHasStockMovement reports whether any stock ledger row exists for the item.
func itemHasStockMovement(tenantID, code string) (bool, error) {
	if code == "" {
		return false, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}
	var has bool
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents
		WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND deleted_at IS NULL)`, schema), code).Scan(&has)
	return has, err
}

// PrepareItemPackUnits is an Item save's half: the purchase unit needs a factor,
// and the stock unit does not change once the item has moved (an item that moved
// before the setting was on is locked too).
func PrepareItemPackUnits(tenantID, id string, payload map[string]interface{}) error {
	packUnits := GetSettingBool(tenantID, SettingPackUnits)
	unit := strField(payload, "purchase_unit")
	factor := rebateNum(payload, "purchase_unit_factor")
	if packUnits && unit != "" && (factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0)) {
		return packErr("INVENT-1400", "purchase_unit_factor", "Enter how many stock units are in one %s.", unit)
	}
	if unit == "" && factor > 0 {
		return packErr("INVENT-1400", "purchase_unit", "Enter the purchase unit for this factor.")
	}
	if unit != "" && strings.EqualFold(unit, strField(payload, "uom")) {
		return packErr("INVENT-1402", "purchase_unit", "The purchase unit is the stock unit; leave it blank.")
	}
	if id == "" {
		return nil
	}
	old, _, err := fetchDocData(tenantID, "Item", id)
	if err != nil || old == nil {
		return nil
	}
	if _, sent := payload["uom"]; !sent || strings.EqualFold(strField(payload, "uom"), strField(old, "uom")) {
		return nil
	}
	moved, err := itemHasStockMovement(tenantID, strField(old, "code"))
	if err != nil {
		return err
	}
	if moved {
		return packErr("INVENT-1401", "uom", "This item has stock movements, so its stock unit cannot change.")
	}
	return nil
}

// packLineFactor is the factor a line converts with: (a GRN line) its PO line's,
// else the one it already carries, else the item's own.
func packLineFactor(line map[string]interface{}, fromPO, itemFactor float64) float64 {
	if fromPO > 0 {
		return fromPO
	}
	if f := rebateNum(line, "pack_factor"); f > 0 {
		return f
	}
	return itemFactor
}

// packPieceRate is a pack's price per piece, with enough places that packs x
// price comes back whole once the line is rounded to paise (144 for Rs 1,000).
func packPieceRate(packRate, factor float64) float64 {
	return math.Round(packRate/factor*1e10) / 1e10
}

var packKeysPO = []string{"pack_qty", "pack_rate", "pack_factor", "pack_unit"}
var packKeysGRN = []string{"pack_qty", "rejected_pack_qty", "damaged_pack_qty", "invoice_pack_rate", "pack_factor", "pack_unit"}

func dropKeys(m map[string]interface{}, keys []string) {
	for _, k := range keys {
		delete(m, k)
	}
}

func hasAnyKey(m map[string]interface{}, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func trimFloat(f float64) string { return fmt.Sprintf("%g", math.Round(f*1e4)/1e4) }

// packStockQty is packs x factor, which must come out in whole pieces unless the
// item allows fractions (and then only where the document can carry them).
func packStockQty(sku string, packs, factor float64, item map[string]interface{}, wholeOnly bool) (float64, error) {
	q := math.Round(packs*factor*1e6) / 1e6
	if math.Abs(q-math.Round(q)) < 1e-6 {
		return math.Round(q), nil
	}
	if wholeOnly {
		return 0, packErr("PURCHA-5132", "pack_qty", "%s: a PO takes whole pieces, and %s is a part-piece. Order whole pieces.", sku, trimFloat(q))
	}
	if !strings.EqualFold(strField(item, "allow_fraction"), packYes) {
		return 0, packErr("PURCHA-5131", "pack_qty", "%s does not allow part-pieces, and %s is not a whole number.", sku, trimFloat(q))
	}
	return q, nil
}

// StampPOPacks converts a PO's pack-keyed lines (called from StampPOPricing, the
// PO's one save choke point). Lines without pack keys, and every PO with the
// setting off, are left as they were. It reports whether any line changed.
func StampPOPacks(tenantID string, lines []map[string]interface{}) (bool, error) {
	on := GetSettingBool(tenantID, SettingPackUnits)
	changed := false
	for _, l := range lines {
		if !hasAnyKey(l, packKeysPO) {
			continue
		}
		if !on {
			dropKeys(l, packKeysPO)
			changed = true
			continue
		}
		if !lineNumberSet(l, "pack_qty") {
			continue
		}
		packs := rebateNum(l, "pack_qty")
		if packs <= 0 {
			return false, packErr("PURCHA-5133", "pack_qty", "Pack quantity must be more than zero.")
		}
		sku := strings.TrimSpace(pimString(l["sku"]))
		item, err := ResolveItemBySKU(tenantID, sku)
		if err != nil {
			continue // the PO's own item check names it
		}
		unit, itemFactor := packOffer(tenantID, item.Data)
		factor := packLineFactor(l, 0, itemFactor)
		if factor <= 0 {
			return false, packErr("PURCHA-5130", "pack_qty", "%s has no purchase unit. Set one on the item, or key pieces.", sku)
		}
		q, err := packStockQty(sku, packs, factor, item.Data, true)
		if err != nil {
			return false, err
		}
		l["pack_factor"], l["qty"] = factor, q
		if strField(l, "pack_unit") == "" {
			l["pack_unit"] = unit
		}
		if lineNumberSet(l, "pack_rate") {
			pr := rebateNum(l, "pack_rate")
			if pr < 0 || math.IsNaN(pr) || math.IsInf(pr, 0) {
				return false, packErr("PURCHA-5133", "pack_rate", "Price per pack must be zero or more.")
			}
			l["rate"] = packPieceRate(pr, factor)
		}
		changed = true
	}
	return changed, nil
}

// poPackFactors maps sku to the pack factor the PO's own line used, for a GRN
// line to follow (a past PO keeps its factor whatever the item says now).
func poPackFactors(tenantID, poID string) map[string]float64 {
	out := map[string]float64{}
	data, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil || data == nil {
		return out
	}
	lines, _ := ParsePOLines(data["items"])
	for _, l := range lines {
		if l.PackFactor > 0 && out[l.SKU] == 0 {
			out[l.SKU] = l.PackFactor
		}
	}
	return out
}

// stampGRNExactPrice keeps a pack line's price a piece before rounding: 144 to a
// pack at Rs 1,000 shows Rs 6.94, yet 50 packs must bill Rs 50,000. It is worked
// here, never taken from the caller, from the vendor's pack price or else the PO's.
func stampGRNExactPrice(l map[string]interface{}, po grnPOLineRate) {
	delete(l, "exact_unit_price")
	delete(l, grnStockRoundSettled)
	factor := po.PackFactor
	if factor <= 0 {
		factor = rebateNum(l, "pack_factor")
	}
	exact := po.Exact
	if pr := rebateNum(l, "invoice_pack_rate"); pr > 0 && factor > 0 {
		exact = pr / factor
	}
	if exact > 0 {
		l["exact_unit_price"] = exact
	} else if po.Inclusive {
		l[grnStockRoundSettled] = true // 148.51
	}
}

// grnStockRoundSettled marks an inclusive line with no pack, received from
// 148.51 on, whose rounding the bill settles. A GRN without the mark keeps the
// books it has, so a re-match never moves them.
const grnStockRoundSettled = "stock_round_settled"

// grnLineSettlesRound says the bill settles this line's rounding to the paisa.
func grnLineSettlesRound(l map[string]interface{}) bool {
	settled, _ := l[grnStockRoundSettled].(bool)
	return settled || numFromInterface(l["exact_unit_price"]) > 0
}

// grnExactPrice is a line's exact price a piece while it still shows that price,
// once rounded. A price typed since then stands as typed.
func grnExactPrice(l map[string]interface{}, shown float64) float64 {
	if exact := numFromInterface(l["exact_unit_price"]); exact > 0 && math.Abs(round2(exact)-shown) < 0.005 {
		return exact
	}
	return shown
}

// grnStockRoundGapPaise is how far a pack line's stock went in off the line's value.
// Stock costs whole paise a piece: 144 to a pack at Rs 1,000 books 6.94 x 7,200, or
// Rs 49,968, on a line worth Rs 50,000. The bill settles it, as on any receipt.
func grnStockRoundGapPaise(l map[string]interface{}) int64 {
	if !grnLineSettlesRound(l) {
		return 0
	}
	ours, _ := grnOurLineAmount(l)
	return RupeesToPaise(grnGRNPrice(l))*int64(grnStepAccepted(l)) - RupeesToPaise(ours)
}

// PrepareGRNPacks converts a GRN's pack-keyed lines to stock units, before the
// GRN is priced: received / rejected / damaged packs become pieces and the
// vendor's price per pack becomes the invoice rate per piece.
func PrepareGRNPacks(tenantID string, payload map[string]interface{}) error {
	lines := grnLineMaps(payload)
	if len(lines) == 0 {
		return nil
	}
	on := GetSettingBool(tenantID, SettingPackUnits)
	changed := false
	poFactors := map[string]map[string]float64{}
	for _, l := range lines {
		if !hasAnyKey(l, packKeysGRN) {
			continue
		}
		changed = true
		if !on {
			dropKeys(l, packKeysGRN)
			continue
		}
		if !lineNumberSet(l, "pack_qty") && !lineNumberSet(l, "invoice_pack_rate") {
			continue
		}
		if lineNumberSet(l, "pack_qty") && rebateNum(l, "pack_qty") <= 0 {
			return packErr("PURCHA-5133", "pack_qty", "Pack quantity must be more than zero.")
		}
		sku := strField(l, "sku")
		item, err := ResolveItemBySKU(tenantID, sku)
		if err != nil {
			continue
		}
		unit, itemFactor := packOffer(tenantID, item.Data)
		po := firstNonEmpty(strField(l, "po_id"), strField(payload, "po_id"))
		if _, ok := poFactors[po]; !ok {
			poFactors[po] = poPackFactors(tenantID, po)
		}
		factor := packLineFactor(l, poFactors[po][sku], itemFactor)
		if factor <= 0 {
			return packErr("PURCHA-5130", "pack_qty", "%s has no purchase unit. Set one on the item, or key pieces.", sku)
		}
		l["pack_factor"] = factor
		if strField(l, "pack_unit") == "" {
			l["pack_unit"] = unit
		}
		for _, k := range [][2]string{{"pack_qty", "qty"}, {"rejected_pack_qty", "rejected_qty"}, {"damaged_pack_qty", "damaged_qty"}} {
			if !lineNumberSet(l, k[0]) {
				continue
			}
			p := rebateNum(l, k[0])
			if p < 0 {
				return packErr("PURCHA-5133", k[0], "Pack quantity cannot be negative.")
			}
			q, err := packStockQty(sku, p, factor, item.Data, false)
			if err != nil {
				return err
			}
			l[k[1]] = q
		}
		if lineNumberSet(l, "pack_qty") {
			delete(l, "accepted_qty") // derived again from the converted counts
		}
		if lineNumberSet(l, "invoice_pack_rate") {
			pr := rebateNum(l, "invoice_pack_rate")
			if pr < 0 || math.IsNaN(pr) || math.IsInf(pr, 0) {
				return packErr("PURCHA-5133", "invoice_pack_rate", "Price per pack must be zero or more.")
			}
			per := packPieceRate(pr, factor)
			l["invoice_rate"], l["invoice_rate_incl"] = per, per
		}
	}
	if changed {
		if encoded, err := json.Marshal(lines); err == nil {
			payload["received_items"] = string(encoded)
		}
	}
	return nil
}
