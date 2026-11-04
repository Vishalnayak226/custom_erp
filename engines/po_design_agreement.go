package engines

import (
	"encoding/json"
	"strings"
)

// DesignRowPrice answers for one row of the PO's design grid: the agreement's
// price, or why there is none, in the words a PO line uses.
type DesignRowPrice struct {
	Rate float64 `json:"rate,omitempty"`
	Note string  `json:"note,omitempty"`
}

const designRowNoPrice = "agreement sets no price"

// designRowItemAttrs reads an existing item as StampPOPricing does.
func designRowItemAttrs(tenantID, sku string) (poItemAttrs, bool) {
	a := poItemAttrs{sku: sku}
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil {
		return a, false
	}
	a.design = firstNonEmpty(strField(item.Data, "parent_product_code"), strField(item.Data, "family"))
	a.category = strField(item.Data, "category")
	return a, true
}

// PriceDesignRows (Stage 148.6) prices the design grid's rows before their items
// exist. A row carries the SKU it already has, or the design and category its new
// item will be given; those are what StampPOPricing reads when the PO saves, so the
// rate given here is the one the save keeps. It only reads.
func PriceDesignRows(tenantID string, payload map[string]interface{}) []DesignRowPrice {
	asked, _ := payload["design_rows"].([]interface{})
	rows := loadVendorAgreementRows(tenantID, strings.TrimSpace(firstNonEmpty(str(payload, "vendor"), str(payload, "vendor_id"))))
	asOf := strings.TrimSpace(firstNonEmpty(str(payload, "order_date"), str(payload, "pricing_date")))
	if len(asOf) > 10 {
		asOf = asOf[:10]
	}
	if asOf == "" {
		asOf = BusinessToday()
	}

	type counted struct {
		attrs poItemAttrs
		qty   float64
	}
	var all []counted
	attrsOfRow := make([]poItemAttrs, len(asked))
	qtys := make([]float64, len(asked))
	for i, raw := range asked {
		row, _ := raw.(map[string]interface{})
		sku := strings.TrimSpace(pimString(row["sku"]))
		a, known := poItemAttrs{}, false
		if sku != "" {
			a, known = designRowItemAttrs(tenantID, sku)
		}
		if !known {
			a = poItemAttrs{sku: sku, design: strings.TrimSpace(pimString(row["design"])), category: strings.TrimSpace(pimString(row["category"]))}
		}
		attrsOfRow[i], qtys[i] = a, numFromInterface(row["qty"])
		all = append(all, counted{a, qtys[i]})
	}
	// Lines already on the PO count towards a break read across it.
	var lines []map[string]interface{}
	if text, _ := payload["items"].(string); len(rows) > 0 && json.Unmarshal([]byte(text), &lines) == nil {
		for _, l := range lines {
			sku := strings.TrimSpace(pimString(l["sku"]))
			if sku == "" {
				continue
			}
			a, _ := designRowItemAttrs(tenantID, sku)
			all = append(all, counted{a, numFromInterface(l["qty"])})
		}
	}

	out := make([]DesignRowPrice, len(asked))
	for i, a := range attrsOfRow {
		qty := qtys[i]
		rule, why := resolveAgreementRule(rows, a, asOf, func(r vendorAgreementRow) float64 {
			if !r.CountAcross {
				return qty
			}
			sum := 0.0
			for _, c := range all {
				if c.attrs.matches(r) {
					sum += c.qty
				}
			}
			return sum
		})
		switch {
		case rule == nil:
			// A vendor with no agreement at all gets no note, as a plain line gets none.
			if len(rows) > 0 {
				out[i].Note = why
			}
		case rule.Price > 0:
			out[i].Rate = rule.Price
		default:
			out[i].Note = designRowNoPrice
		}
	}
	return out
}
