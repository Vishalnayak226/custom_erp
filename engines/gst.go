package engines

import "fmt"

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
