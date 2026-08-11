package engines

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// round2 (24.9) rounds to 2 decimal places (paise) using stdlib math only -
// not shopspring/decimal, per this stage's scoping note against adding a
// new dependency for it.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// The four GST treatments an Item can be sold under (Stage 26.6.11), stored
// in Item.tax_treatment. Only Taxable carries a rate; the other three are all
// charged at 0% but are NOT interchangeable, because the returns report them
// in different places - GSTR-1's nil/exempt/non-GST table gives nil-rated and
// exempt their own columns, and GSTR-3B puts zero-rated in 3.1(b) while
// exempt and nil-rated go to 3.1(c).
const (
	// TaxTreatmentTaxable is the ordinary case and the default for any item
	// that does not say otherwise - see NormalizeTaxTreatment.
	TaxTreatmentTaxable = "Taxable"
	// TaxTreatmentExempt is exempt by notification (unbranded grain, fresh
	// produce, ...): the goods are taxable in principle but relieved by a
	// specific exemption.
	TaxTreatmentExempt = "Exempt"
	// TaxTreatmentNilRated is a tariff rate that is genuinely 0% (e.g. salt,
	// certain cereals) rather than an exemption granted on top of a rate.
	TaxTreatmentNilRated = "Nil-Rated"
	// TaxTreatmentZeroRated is exports and SEZ supplies. This MVP models the
	// LUT/bond route only - supplied without payment of tax - not the
	// export-with-payment-and-refund route, which needs a refund claim
	// workflow that does not exist here.
	TaxTreatmentZeroRated = "Zero-Rated"
)

// taxTreatmentAliases maps the spellings a human or a CSV import can
// plausibly produce onto the canonical values above. Keys are lowercased with
// spaces/underscores/hyphens stripped, so "Nil Rated", "nil_rated" and
// "NILRATED" all land on the same treatment - the generic ValidateDocument
// Select check enforces the exact catalog value on the doctype form, but
// BulkImportCSV and the API accept free text, and silently mis-bucketing a
// tenant's exempt turnover is worse than being lenient about a hyphen.
var taxTreatmentAliases = map[string]string{
	"taxable":   TaxTreatmentTaxable,
	"exempt":    TaxTreatmentExempt,
	"exempted":  TaxTreatmentExempt,
	"nilrated":  TaxTreatmentNilRated,
	"nil":       TaxTreatmentNilRated,
	"zerorated": TaxTreatmentZeroRated,
	"zero":      TaxTreatmentZeroRated,
	"export":    TaxTreatmentZeroRated,
	"exports":   TaxTreatmentZeroRated,
}

// NormalizeTaxTreatment resolves a stored/entered tax_treatment to one of the
// four canonical values. A blank or absent value is TaxTreatmentTaxable: every
// Item that existed before Stage 26.6.11 predates the field, and reading those
// as taxable is what keeps 30.1.2's "gst_rate must be > 0" rule applying to
// them unchanged. ok is false only for a non-empty value that matches nothing,
// which callers must reject rather than guess at - quietly defaulting a typo'd
// "Exmept" to Taxable would charge tax on exempt goods.
func NormalizeTaxTreatment(raw string) (treatment string, ok bool) {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	if cleaned == "" {
		return TaxTreatmentTaxable, true
	}
	cleaned = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(cleaned)
	if canonical, found := taxTreatmentAliases[cleaned]; found {
		return canonical, true
	}
	return "", false
}

// IsTaxableTreatment reports whether a canonical treatment charges GST at all.
// The three non-taxable treatments are all 0%; what differs is only which
// return bucket their turnover lands in.
func IsTaxableTreatment(treatment string) bool {
	return treatment == TaxTreatmentTaxable
}

// GSTBreakdown is the result of splitting a taxable amount at a given GST
// rate into its Indian GST components. Intra-state sales split the rate
// evenly into CGST+SGST (both flow to the seller's home state); inter-state
// sales charge the full rate as IGST instead - never both.
//
// Stage 26.6.11 added the three non-taxable buckets. They are deliberately
// NOT part of TaxableAmount: "taxable value" is a defined term on the returns
// (GSTR-3B 3.1(a)), and folding exempt turnover into it would overstate it.
// TotalAmount does include them, because it is what the customer actually
// pays. They are `omitempty` so an all-taxable cart's response - every cart
// before this stage - is byte-identical to what it was.
type GSTBreakdown struct {
	TaxableAmount   float64 `json:"taxable_amount"`
	GSTRate         float64 `json:"gst_rate"`
	Interstate      bool    `json:"interstate"`
	CGST            float64 `json:"cgst"`
	SGST            float64 `json:"sgst"`
	IGST            float64 `json:"igst"`
	TotalTax        float64 `json:"total_tax"`
	TotalAmount     float64 `json:"total_amount"`
	ExemptAmount    float64 `json:"exempt_amount,omitempty"`
	NilRatedAmount  float64 `json:"nil_rated_amount,omitempty"`
	ZeroRatedAmount float64 `json:"zero_rated_amount,omitempty"`
}

// NonTaxableAmount is the total turnover in this breakdown that carried no
// GST, across all three non-taxable treatments.
func (b GSTBreakdown) NonTaxableAmount() float64 {
	return round2(b.ExemptAmount + b.NilRatedAmount + b.ZeroRatedAmount)
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

	// 24.9: round to 2 decimal places (paise) before it goes anywhere else -
	// plain float64 division otherwise produces results like
	// 18.018000000000003 instead of 18.02 for fractional-rupee amounts.
	totalTax := round2(taxableAmount * gstRate / 100)

	result := GSTBreakdown{
		TaxableAmount: taxableAmount,
		GSTRate:       gstRate,
		Interstate:    interstate,
		TotalTax:      totalTax,
		TotalAmount:   round2(taxableAmount + totalTax),
	}
	if interstate {
		result.IGST = totalTax
	} else {
		result.CGST = round2(totalTax / 2)
		result.SGST = round2(totalTax / 2)
	}
	return result, nil
}

// ItemTaxInfo is an Item's complete tax classification as a transaction needs
// it: what it is classified as (HSN), how it is treated (Stage 26.6.11), and
// the rate that follows from that treatment.
type ItemTaxInfo struct {
	HSNCode   string
	GSTRate   float64
	Treatment string
}

// Taxable reports whether this item charges GST.
func (i ItemTaxInfo) Taxable() bool { return IsTaxableTreatment(i.Treatment) }

// GetItemTaxInfo resolves an Item's tax classification (Stage 13.10's HSN and
// rate, plus Stage 26.6.11's treatment). It is the single place a transaction
// turns an Item into tax facts - every checkout line, PurchaseOrder line and
// GST computation runs through it - so the "is this item transactable?" gate
// lives here once rather than at each call site.
//
// Returns an error when the classification is incomplete: no HSN (any
// treatment), no positive rate on a Taxable item, or a tax_treatment nobody
// recognizes. That is the gate Stage 17.5 uses to block a line from posting
// with incomplete tax classification rather than silently defaulting to 0%.
//
// Stage 30.1.1: the SKU is resolved through the shared ResolveItemBySKU
// (code -> barcode -> id) rather than the internal id alone, so a scanned
// barcode or a typed item Code reaches the same record the POS typeahead
// offered.
func GetItemTaxInfo(tenantID, sku string) (ItemTaxInfo, error) {
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil {
		if errors.Is(err, ErrItemNotFound) {
			return ItemTaxInfo{}, fmt.Errorf("item '%s' not found", sku)
		}
		return ItemTaxInfo{}, err
	}
	data := item.Data

	info := ItemTaxInfo{}
	info.HSNCode, _ = data["hsn_code"].(string)
	if info.HSNCode == "" {
		// ADMINC-0034 (Stage 25.5): "Tax configuration missing" - an Item
		// reaching checkout/PO with no HSN classified is exactly this
		// scenario, not a per-field format error (MASTER-0043 covers the
		// format-is-wrong case at master-save time; this is the
		// not-set-at-all case caught later, at transaction time).
		//
		// Stage 26.6.11 kept this ahead of the treatment check: HSN is
		// required on the invoice whatever the rate is, and GSTR-1 reports
		// even the nil/exempt table HSN-wise, so an exempt item is no more
		// sellable without one than a taxable item is.
		return ItemTaxInfo{}, &ValidationError{Code: "ADMINC-0034", Message: fmt.Sprintf("item '%s' is missing hsn_code - required before it can be sold or purchased", sku)}
	}

	rawTreatment, _ := data["tax_treatment"].(string)
	treatment, ok := NormalizeTaxTreatment(rawTreatment)
	if !ok {
		return ItemTaxInfo{}, &ValidationError{Code: "ADMINC-0034", Message: fmt.Sprintf("item '%s' has an unrecognized tax_treatment %q - expected one of %s, %s, %s or %s", sku, rawTreatment, TaxTreatmentTaxable, TaxTreatmentExempt, TaxTreatmentNilRated, TaxTreatmentZeroRated)}
	}
	info.Treatment = treatment

	if !info.Taxable() {
		// Rate stays 0 whatever the item happens to have stored.
		// validateItemMasterRules refuses to save a non-taxable item with a
		// positive rate, so this only differs from the stored value for a row
		// written before that rule existed - and there the declared treatment
		// is the more recent, more deliberate statement of intent.
		return info, nil
	}

	if rate, ok := data["gst_rate"].(float64); ok && rate > 0 {
		info.GSTRate = rate
		return info, nil
	}
	return ItemTaxInfo{}, &ValidationError{Code: "ADMINC-0034", Message: fmt.Sprintf("item '%s' is missing a positive gst_rate - required before it can be sold or purchased. If it is genuinely untaxed, set its Tax Treatment to %s, %s or %s instead of a 0 rate", sku, TaxTreatmentExempt, TaxTreatmentNilRated, TaxTreatmentZeroRated)}
}

// GetItemGSTInfo is the two-value accessor onto GetItemTaxInfo kept for
// callers that only need the HSN and the rate. A non-taxable item resolves
// successfully here with gstRate 0 - which is correct for anything that only
// wants to price a line, but means a caller deciding how to REPORT the line
// must use GetItemTaxInfo and read Treatment, since 0 no longer implies
// "unclassified".
func GetItemGSTInfo(tenantID, sku string) (hsnCode string, gstRate float64, err error) {
	info, err := GetItemTaxInfo(tenantID, sku)
	if err != nil {
		return "", 0, err
	}
	return info.HSNCode, info.GSTRate, nil
}

// GSTLineInput is one taxable line (an Item sku, quantity, and its
// tax-inclusive unit rate) going into ComputeGSTForLines.
type GSTLineInput struct {
	Sku      string
	Qty      int
	UnitRate float64
}

// ComputeGSTForLines validates every line's Item is fully tax-classified and
// returns the aggregate CGST/SGST/IGST breakdown across all lines.
// UnitRate is treated as tax-inclusive (this codebase's existing
// sale_price/rate fields are MRP-style prices, not tax-exclusive base
// prices), so each line's taxable amount is backed out of its gross total
// rather than added on top of it.
//
// Stage 26.6.11: a line whose Item is Exempt/Nil-Rated/Zero-Rated contributes
// its full gross to the matching bucket and nothing to the tax accumulators.
// Backing tax out of it would be wrong twice over - there is no tax in the
// price to back out, and the value would land in TaxableAmount, which is a
// filed figure on GSTR-3B 3.1(a).
func ComputeGSTForLines(tenantID string, lines []GSTLineInput, interstate bool) (GSTBreakdown, error) {
	return ComputeGSTForLinesMode(tenantID, lines, interstate, GSTModeInclusive)
}

// GST treatment of a line rate (Stage 40.1). Which one applies is a business
// convention, not a property of the tax: a retail sale price is quoted with
// GST already in it, a vendor's purchase price almost never is.
const (
	// GSTModeInclusive - the rate already contains GST, so the taxable value
	// is backed out of the gross. This codebase's original and only behavior
	// before Stage 40.1, and still the default for every sale-side caller.
	GSTModeInclusive = "Inclusive"
	// GSTModeExclusive - the rate is the taxable value and GST is added on
	// top. The default for purchase orders.
	GSTModeExclusive = "Exclusive"
)

// ComputeGSTForLinesMode is ComputeGSTForLines with the inclusive/exclusive
// convention made explicit.
//
// Split out rather than adding a parameter to ComputeGSTForLines because every
// existing sale-side call site (POS checkout, sales invoice, returns) is
// correct as-is and must keep its behavior byte-identical - so the old name
// keeps the old meaning and only the PO path opts into the new one.
//
// Note what does NOT change with the mode: exempt/nil/zero-rated lines carry
// no tax under either convention, so their gross is their gross either way.
func ComputeGSTForLinesMode(tenantID string, lines []GSTLineInput, interstate bool, mode string) (GSTBreakdown, error) {
	inclusive := mode != GSTModeExclusive
	total := GSTBreakdown{Interstate: interstate}
	for _, line := range lines {
		if line.Qty <= 0 {
			continue
		}
		info, err := GetItemTaxInfo(tenantID, line.Sku)
		if err != nil {
			return GSTBreakdown{}, err
		}
		gross := line.UnitRate * float64(line.Qty)

		if !info.Taxable() {
			switch info.Treatment {
			case TaxTreatmentExempt:
				total.ExemptAmount = round2(total.ExemptAmount + gross)
			case TaxTreatmentNilRated:
				total.NilRatedAmount = round2(total.NilRatedAmount + gross)
			case TaxTreatmentZeroRated:
				total.ZeroRatedAmount = round2(total.ZeroRatedAmount + gross)
			}
			total.TotalAmount = round2(total.TotalAmount + gross)
			continue
		}

		gstRate := info.GSTRate
		// Inclusive: back the tax out of the gross. Exclusive: the gross IS
		// the taxable value and CalculateGST adds tax on top of it.
		taxable := gross
		if inclusive {
			taxable = gross / (1 + gstRate/100)
		}
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
	// Stage 30.5.10: the aggregate used to report gst_rate: 0 while charging
	// the correct tax, because GSTRate is a per-line input that nothing ever
	// set on the rolled-up total - so a checkout response said "0%" on a sale
	// that had just been charged 5%. A cart can legitimately mix rates, so the
	// figure reported is the blended effective rate actually applied, which is
	// exactly the line rate when every line shares one.
	//
	// Stage 26.6.11: exempt/nil/zero-rated turnover is in neither term, so a
	// mixed cart reports the rate its taxable half was actually charged rather
	// than a rate diluted by goods that were never in scope. A wholly
	// non-taxable cart leaves this 0, which is the true effective rate.
	if total.TaxableAmount > 0 {
		total.GSTRate = round2(total.TotalTax / total.TaxableAmount * 100)
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
	// Stage 40.1: `interstate` is normally derived from the two parties'
	// states by ApplyPlaceOfSupply (which runs just before this at the same
	// choke point) and only read back here. truthy() rather than a bool
	// assertion because a Check field also arrives as "true"/"1" from a CSV
	// import or a form post, and a string there used to silently mean
	// intra-state.
	interstate := truthy(payload["interstate"])
	lines := make([]GSTLineInput, len(rawItems))
	for i, it := range rawItems {
		lines[i] = GSTLineInput{Sku: it.Sku, Qty: it.Qty, UnitRate: it.Rate}
	}
	return ComputeGSTForLinesMode(tenantID, lines, interstate, ResolvePOGSTMode(tenantID, payload))
}

// ResolvePOGSTMode decides whether a PO's line rates already include GST:
// the PO's own gst_mode if it sets one, otherwise the tenant default
// (procurement.po_gst_mode), which ships as Exclusive.
//
// An unrecognised value resolves to the tenant default rather than silently
// picking a convention, so a typo in an API payload cannot quietly change what
// a vendor is paid.
func ResolvePOGSTMode(tenantID string, payload map[string]interface{}) string {
	mode, _ := payload["gst_mode"].(string)
	switch strings.TrimSpace(mode) {
	case GSTModeExclusive:
		return GSTModeExclusive
	case GSTModeInclusive:
		return GSTModeInclusive
	}
	if GetSettingString(tenantID, "procurement.po_gst_mode") == GSTModeInclusive {
		return GSTModeInclusive
	}
	return GSTModeExclusive
}
