package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Item 273.4 - a sale below cost at the till.
//
// A cashier can key a price or a bill discount that sells an item for less
// than it cost, and nothing said so until a margin report weeks later.
// pos.below_cost_sale decides what happens: Off (the default, as before), Warn
// (the cashier confirms) or Needs approval (the sale waits for the lowest
// POSCart approval slab's approver - the queue a large discount already goes
// to).
//
// A line is below cost when one unit - after the line's own price and its
// share of the cashier's bill discount, with GST taken out - is under the
// item's cost (ResolveQuoteUnitCostPaise: moving average, then standard cost).
// Offers and vouchers are left out: a promotion was set up on purpose. A line
// with no known cost is never flagged. Only the flag reaches the till, never
// the cost (47.2.2), and the cart keeps the lines (SKU, qty, price sold at)
// for the "Sold below cost" report, which shows the booked cost to managers.

const (
	SettingBelowCostSale = "pos.below_cost_sale"

	BelowCostOff      = "off"
	BelowCostWarn     = "warn"
	BelowCostApproval = "approval"
	// BelowCostOffline is what an offline sale's cart records: it was sold
	// offline and is checked when POS > Offline Sales validates it.
	BelowCostOffline = "offline"

	// BelowCostWarnCode asks the cashier to confirm; BelowCostNoApproverCode
	// refuses a sale that needs approval when no POSCart approval slab exists.
	BelowCostWarnCode       = "POSOFF-1372"
	BelowCostNoApproverCode = "POSOFF-1373"
)

func init() {
	RegisterSetting(SettingDefinition{Key: SettingBelowCostSale, Module: "Point of Sale",
		Label: "A sale below cost", Type: SettingTypeSelect, Default: BelowCostOff,
		Options: []SettingOption{{Value: BelowCostOff, Label: "Goes through"},
			{Value: BelowCostWarn, Label: "Asks the cashier to confirm"},
			{Value: BelowCostApproval, Label: "Waits for a manager's approval"}},
		Description: "An item is sold below cost when its price after the cashier's price change and bill discount, without GST, is under what it cost. Offers and vouchers do not count. Approval goes to the approver of the lowest POSCart rule (Settings > Approval Rules); with no rule, the sale is refused."})
	RegisterReport(ReportDefinition{ID: "sold-below-cost", Label: "Sold below cost", Category: "Sales",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "date", Label: "Date"}, {Key: "bill", Label: "Bill", Type: "doclink"}, {Key: "store", Label: "Store"},
			{Key: "cashier", Label: "Cashier"}, {Key: "sku", Label: "Item"}, {Key: "qty", Label: "Qty"},
			{Key: "sold_at", Label: "Sold at (ex GST)"}, {Key: "unit_cost", Label: "Cost", Sensitive: true},
			{Key: "loss", Label: "Loss", Sensitive: true, Total: true}, {Key: "cleared", Label: "Cleared by"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return SoldBelowCost(tenantID, params["from"], params["to"])
		}})
}

// BelowCostRule is the tenant's pos.below_cost_sale; anything unknown is Off.
func BelowCostRule(tenantID string) string {
	switch v := strings.TrimSpace(GetSettingString(tenantID, SettingBelowCostSale)); v {
	case BelowCostWarn, BelowCostApproval:
		return v
	}
	return BelowCostOff
}

// markBelowCostLines flags each line sold below its cost. With the rule Off it
// reads nothing, so the till's quote costs no more than before.
func markBelowCostLines(tenantID string, result *QuoteResult) {
	if BelowCostRule(tenantID) == BelowCostOff {
		return
	}
	for i := range result.Lines {
		l := &result.Lines[i]
		l.BelowCost = false
		if l.Qty <= 0 || l.NotStocked || l.PromotionRef != "" || l.PriceSource == PriceSourceItemTypeFree || l.PriceSource == PriceSourceGWP {
			continue
		}
		cost, ok := ResolveQuoteUnitCostPaise(tenantID, l.Sku)
		if !ok || cost <= 0 {
			continue
		}
		l.BelowCost = RupeesToPaise(belowCostUnitExGST(tenantID, *l)) < cost
	}
}

// belowCostUnitExGST is one unit's price after the line's own price and its
// share of the bill discount, GST taken out at the item's rate.
func belowCostUnitExGST(tenantID string, l QuoteLine) float64 {
	unit := (l.LineTotal - l.BillShare) / float64(l.Qty)
	if info, err := GetItemTaxInfo(tenantID, l.Sku); err == nil && info.Taxable() && info.GSTRate > 0 {
		unit /= 1 + info.GSTRate/100
	}
	return math.Round(unit*100) / 100
}

// BelowCostLines is what the cart keeps of the flagged lines: SKU, quantity
// and the price sold at without GST - never the cost.
func BelowCostLines(tenantID string, q *QuoteResult) []map[string]interface{} {
	var out []map[string]interface{}
	for _, l := range q.Lines {
		if l.BelowCost {
			out = append(out, map[string]interface{}{"sku": l.Sku, "qty": l.Qty, "sold_at": belowCostUnitExGST(tenantID, l)})
		}
	}
	return out
}

// BelowCostMessage names the lines for the cashier, without any cost.
func BelowCostMessage(lines []map[string]interface{}) string {
	skus := make([]string, 0, len(lines))
	for _, l := range lines {
		skus = append(skus, fmt.Sprint(l["sku"]))
	}
	return "Sold below cost: " + strings.Join(skus, ", ")
}

// SoldBelowCostRow is one line of the "Sold below cost" report.
type SoldBelowCostRow struct {
	Date        string  `json:"date"`
	Bill        string  `json:"bill"`
	BillDoctype string  `json:"bill_doctype"`
	BillID      string  `json:"bill_id"`
	Store       string  `json:"store"`
	Cashier     string  `json:"cashier"`
	SKU         string  `json:"sku"`
	Qty         int     `json:"qty"`
	SoldAt      float64 `json:"sold_at"`
	UnitCost    float64 `json:"unit_cost"`
	Loss        float64 `json:"loss"`
	Cleared     string  `json:"cleared"`
}

// SoldBelowCost lists the paid bills' lines the guard caught, newest first,
// with the cost the sale booked (sale_costs) and the loss against it.
func SoldBelowCost(tenantID, from, to string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where, args := "", []interface{}{}
	if from != "" {
		args = append(args, from)
		where += fmt.Sprintf(" AND created_at >= public.erp_day_start($%d::date)", len(args))
	}
	if to != "" {
		args = append(args, to)
		where += fmt.Sprintf(" AND created_at < public.erp_day_start($%d::date + 1)", len(args))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, to_char(created_at, 'YYYY-MM-DD'), COALESCE(data->>'bill_number', ''), COALESCE(data->>'location', ''),
		       COALESCE(created_by, ''), COALESCE(data->>'below_cost_rule', ''),
		       data->'below_cost_lines', COALESCE(data->'sale_costs', '[]'::jsonb)
		FROM %s.documents
		WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL
		  AND jsonb_typeof(data->'below_cost_lines') = 'array'%s
		ORDER BY created_at DESC, id`, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SoldBelowCostRow
	for rows.Next() {
		var id, day, bill, store, cashier, rule string
		var linesRaw, costsRaw []byte
		if err := rows.Scan(&id, &day, &bill, &store, &cashier, &rule, &linesRaw, &costsRaw); err != nil {
			return nil, err
		}
		var lines []struct {
			SKU    string  `json:"sku"`
			Qty    int     `json:"qty"`
			SoldAt float64 `json:"sold_at"`
		}
		_ = json.Unmarshal(linesRaw, &lines)
		var costs []saleCostLine
		_ = json.Unmarshal(costsRaw, &costs)
		booked := map[string][2]int64{} // sku -> value paise, qty
		for _, c := range costs {
			b := booked[c.SKU]
			booked[c.SKU] = [2]int64{b[0] + c.ValuePaise, b[1] + int64(c.Qty)}
		}
		if bill == "" {
			bill = id
		}
		cleared := "Cashier"
		switch rule {
		case BelowCostApproval:
			cleared = "Manager"
		case BelowCostOffline:
			cleared = "Offline check"
		}
		for _, l := range lines {
			r := SoldBelowCostRow{Date: day, Bill: bill, BillDoctype: "POSCart", BillID: id, Store: store, Cashier: cashier,
				SKU: l.SKU, Qty: l.Qty, SoldAt: l.SoldAt, Cleared: cleared}
			if b := booked[l.SKU]; b[1] > 0 {
				r.UnitCost = round2(PaiseToRupees(b[0]) / float64(b[1]))
				if loss := round2((r.UnitCost - l.SoldAt) * float64(l.Qty)); loss > 0 {
					r.Loss = loss
				}
			}
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return structsToRows(out)
}
