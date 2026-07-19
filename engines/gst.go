package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// GSTBreakdown is the result of splitting a taxable amount at a given GST
// rate into its Indian GST components. Intra-state sales split the rate
// evenly into CGST+SGST (both flow to the seller's home state); inter-state
// sales charge the full rate as IGST instead - never both.
type GSTBreakdown struct {
	TaxableAmount float64 `json:"taxable_amount"`
	GSTRate       float64 `json:"gst_rate"`
	Interstate    bool    `json:"interstate"`
	CGST          float64 `json:"cgst"`
	SGST          float64 `json:"sgst"`
	IGST          float64 `json:"igst"`
	TotalTax      float64 `json:"total_tax"`
	TotalAmount   float64 `json:"total_amount"`
}

// CalculateGST computes the CGST/SGST/IGST split for a taxable amount at
// gstRate (a percentage, e.g. 18 for 18%). The rate itself is expected to
// have already been resolved from an item's HSN classification (Item.gst_rate,
// Stage 13.10) - this function is the calculation step, not the HSN lookup,
// since HSN-to-rate mapping is a business/accounting classification decision
// per item, not something this function can reliably auto-derive.
func CalculateGST(taxableAmount, gstRate float64, interstate bool) (GSTBreakdown, error) {
	if taxableAmount < 0 {
		return GSTBreakdown{}, fmt.Errorf("taxable_amount cannot be negative")
	}
	if gstRate < 0 {
		return GSTBreakdown{}, fmt.Errorf("gst_rate cannot be negative")
	}

	totalTax := taxableAmount * gstRate / 100

	result := GSTBreakdown{
		TaxableAmount: taxableAmount,
		GSTRate:       gstRate,
		Interstate:    interstate,
		TotalTax:      totalTax,
		TotalAmount:   taxableAmount + totalTax,
	}
	if interstate {
		result.IGST = totalTax
	} else {
		result.CGST = totalTax / 2
		result.SGST = totalTax / 2
	}
	return result, nil
}

// GetItemGSTInfo resolves an Item's HSN classification and GST rate
// (Stage 13.10 fields). Returns an error if either is unset - the gate
// Stage 17.5 uses to block a PurchaseOrder/checkout line from posting with
// incomplete tax classification, rather than silently defaulting to 0%.
func GetItemGSTInfo(tenantID, sku string) (hsnCode string, gstRate float64, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL`, schema), sku).Scan(&dataStr)
	if err != nil {
		return "", 0, fmt.Errorf("item '%s' not found", sku)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", 0, err
	}
	hsnCode, _ = data["hsn_code"].(string)
	if hsnCode == "" {
		return "", 0, fmt.Errorf("item '%s' is missing hsn_code - required before it can be sold or purchased", sku)
	}
	if rate, ok := data["gst_rate"].(float64); ok && rate > 0 {
		gstRate = rate
	} else {
		return "", 0, fmt.Errorf("item '%s' is missing a positive gst_rate - required before it can be sold or purchased", sku)
	}
	return hsnCode, gstRate, nil
}

// GSTLineInput is one taxable line (an Item sku, quantity, and its
// tax-inclusive unit rate) going into ComputeGSTForLines.
type GSTLineInput struct {
	Sku      string
	Qty      int
	UnitRate float64
}

// ComputeGSTForLines validates every line's Item has hsn_code/gst_rate set
// and returns the aggregate CGST/SGST/IGST breakdown across all lines.
// UnitRate is treated as tax-inclusive (this codebase's existing
// sale_price/rate fields are MRP-style prices, not tax-exclusive base
// prices), so each line's taxable amount is backed out of its gross total
// rather than added on top of it.
func ComputeGSTForLines(tenantID string, lines []GSTLineInput, interstate bool) (GSTBreakdown, error) {
	total := GSTBreakdown{Interstate: interstate}
	for _, line := range lines {
		if line.Qty <= 0 {
			continue
		}
		_, gstRate, err := GetItemGSTInfo(tenantID, line.Sku)
		if err != nil {
			return GSTBreakdown{}, err
		}
		gross := line.UnitRate * float64(line.Qty)
		taxable := gross / (1 + gstRate/100)
		lineBreakdown, err := CalculateGST(taxable, gstRate, interstate)
		if err != nil {
			return GSTBreakdown{}, err
		}
		total.TaxableAmount += lineBreakdown.TaxableAmount
		total.CGST += lineBreakdown.CGST
		total.SGST += lineBreakdown.SGST
		total.IGST += lineBreakdown.IGST
		total.TotalTax += lineBreakdown.TotalTax
		total.TotalAmount += lineBreakdown.TotalAmount
	}
	return total, nil
}

// ComputePurchaseOrderGST is the PurchaseOrder-side half of Stage 17.5's
// enforcement. `items` is the doctype's existing mandatory "PO Items JSON"
// field (db/migrations_phase3.sql) - a JSON-encoded string, same convention
// as GRN's "received_items" - expected to hold objects with sku/qty/rate
// (rate = tax-inclusive unit price, this codebase's existing convention).
// A missing or empty items list is not itself an error here (that's
// ValidateDocument's mandatory-field job); this only gates on HSN/rate once
// there's something to gate.
func ComputePurchaseOrderGST(tenantID string, payload map[string]interface{}) (GSTBreakdown, error) {
	itemsStr, _ := payload["items"].(string)
	if itemsStr == "" {
		return GSTBreakdown{}, nil
	}
	var rawItems []struct {
		Sku  string  `json:"sku"`
		Qty  int     `json:"qty"`
		Rate float64 `json:"rate"`
	}
	if err := json.Unmarshal([]byte(itemsStr), &rawItems); err != nil {
		return GSTBreakdown{}, fmt.Errorf("could not parse items JSON: %v", err)
	}
	if len(rawItems) == 0 {
		return GSTBreakdown{}, nil
	}
	interstate, _ := payload["interstate"].(bool)
	lines := make([]GSTLineInput, len(rawItems))
	for i, it := range rawItems {
		lines[i] = GSTLineInput{Sku: it.Sku, Qty: it.Qty, UnitRate: it.Rate}
	}
	return ComputeGSTForLines(tenantID, lines, interstate)
}
