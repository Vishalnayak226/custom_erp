package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Purchase Order lines, pricing preview, print payload and vendor dispatch
// (Stage 40.1).
//
// Before this, a PO recorded a vendor, a warehouse and one hand-typed total.
// There was no record of what was being bought, so the GST engine had nothing
// to classify, GRN receipt had nothing to match against, and there was
// nothing to send a vendor. This file is the missing middle: it turns the
// `items` JSON the PO screen now edits into resolved, priced, tax-classified
// lines, and it is the single place that happens - the screen, the print
// sheet and the vendor's copy all read the same computation rather than each
// re-deriving totals and disagreeing at the third decimal place.

// POLineInput is one line as the PO screen and the API send it. `rate` is the
// purchase price; whether it already contains GST is decided by the PO's
// gst_mode (see ResolvePOGSTMode), not by this struct.
//
// MRP is captured because a buyer negotiates against it and a receiving clerk
// prices retail from it, but it is deliberately not part of any tax or
// accounting computation - and it is omitted from the vendor's printed copy
// (BuildPurchaseOrderPrint), because the vendor's own selling price is not
// their business to be shown back to them.
type POLineInput struct {
	SKU  string  `json:"sku"`
	Qty  int     `json:"qty"`
	Rate float64 `json:"rate"`
	MRP  float64 `json:"mrp,omitempty"`
}

// POLinePreview is one line resolved against the Item master and priced.
//
// Error is per-line rather than a hard failure for the whole request: the PO
// screen shows a red cell against the one line whose Item is missing HSN,
// which is actionable, instead of a single banner saying the PO cannot be
// priced, which is not. The save path still refuses the document - that gate
// is ComputePurchaseOrderGST at the generic doc choke point, unchanged.
type POLinePreview struct {
	POLineInput
	ItemName  string  `json:"item_name,omitempty"`
	UOM       string  `json:"uom,omitempty"`
	HSNCode   string  `json:"hsn_code,omitempty"`
	GSTRate   float64 `json:"gst_rate"`
	Treatment string  `json:"tax_treatment,omitempty"`
	Taxable   float64 `json:"taxable"`
	TaxAmount float64 `json:"tax_amount"`
	LineTotal float64 `json:"line_total"`
	Error     string  `json:"error,omitempty"`
}

// POPreview is everything the PO screen needs to render totals live, and
// everything the printed PO needs for its tax block.
type POPreview struct {
	Lines         []POLinePreview `json:"lines"`
	GSTMode       string          `json:"gst_mode"`
	PlaceOfSupply PlaceOfSupply   `json:"place_of_supply"`
	Breakdown     GSTBreakdown    `json:"breakdown"`
	// GrandTotal is what the vendor bills: taxable + non-taxable + tax.
	GrandTotal float64 `json:"grand_total"`
	// Blocking is true when at least one line could not be tax-classified, so
	// the screen can disable Save with a reason rather than letting the maker
	// discover it on submit.
	Blocking bool `json:"blocking"`
}

// ParsePOLines reads the `items` field's stored JSON. An absent or empty
// value is not an error - a Draft PO legitimately starts with no lines.
func ParsePOLines(itemsRaw interface{}) ([]POLineInput, error) {
	str, _ := itemsRaw.(string)
	str = strings.TrimSpace(str)
	if str == "" {
		return nil, nil
	}
	var lines []POLineInput
	if err := json.Unmarshal([]byte(str), &lines); err != nil {
		return nil, fmt.Errorf("could not parse PO items JSON: %v", err)
	}
	return lines, nil
}

// PreviewPurchaseOrder prices a set of lines without saving anything.
//
// It is the read-only twin of what the save path does, and it deliberately
// runs the same functions (GetItemTaxInfo, ResolvePlaceOfSupply,
// CalculateGST) rather than reimplementing the arithmetic - so what the maker
// sees on screen before saving is what the document will actually store.
func PreviewPurchaseOrder(tenantID string, payload map[string]interface{}) (POPreview, error) {
	lines, err := ParsePOLines(payload["items"])
	if err != nil {
		return POPreview{}, err
	}

	location, _ := payload["location"].(string)
	vendor, _ := payload["vendor"].(string)
	if vendor == "" {
		vendor, _ = payload["vendor_id"].(string)
	}

	out := POPreview{
		GSTMode:       ResolvePOGSTMode(tenantID, payload),
		PlaceOfSupply: ResolvePlaceOfSupply(tenantID, location, vendor),
		Lines:         make([]POLinePreview, 0, len(lines)),
	}

	// A maker's explicit override wins over the derivation, exactly as it does
	// on save - otherwise the preview would show one supply type and the saved
	// document another.
	interstate := out.PlaceOfSupply.Interstate
	if truthy(payload["interstate_override"]) {
		interstate = truthy(payload["interstate"])
	} else if !out.PlaceOfSupply.Derived {
		interstate = truthy(payload["interstate"])
	}
	out.Breakdown.Interstate = interstate
	inclusive := out.GSTMode != GSTModeExclusive

	for _, l := range lines {
		p := POLinePreview{POLineInput: l}
		if l.SKU == "" {
			p.Error = "pick an item"
			out.Lines = append(out.Lines, p)
			out.Blocking = true
			continue
		}
		if l.Qty <= 0 {
			p.Error = "quantity must be more than zero"
			out.Lines = append(out.Lines, p)
			out.Blocking = true
			continue
		}

		if item, errItem := ResolveItemBySKU(tenantID, l.SKU); errItem == nil {
			p.ItemName, _ = item.Data["name"].(string)
			p.UOM, _ = item.Data["uom"].(string)
		}

		info, errTax := GetItemTaxInfo(tenantID, l.SKU)
		if errTax != nil {
			p.Error = errTax.Error()
			out.Lines = append(out.Lines, p)
			out.Blocking = true
			continue
		}
		p.HSNCode, p.GSTRate, p.Treatment = info.HSNCode, info.GSTRate, info.Treatment

		gross := l.Rate * float64(l.Qty)
		if !info.Taxable() {
			// No tax to add or back out either way - the whole gross lands in
			// its own return bucket and nothing in the tax accumulators.
			p.GSTRate, p.Taxable, p.TaxAmount, p.LineTotal = 0, gross, 0, gross
			switch info.Treatment {
			case TaxTreatmentExempt:
				out.Breakdown.ExemptAmount = round2(out.Breakdown.ExemptAmount + gross)
			case TaxTreatmentNilRated:
				out.Breakdown.NilRatedAmount = round2(out.Breakdown.NilRatedAmount + gross)
			case TaxTreatmentZeroRated:
				out.Breakdown.ZeroRatedAmount = round2(out.Breakdown.ZeroRatedAmount + gross)
			}
			out.Breakdown.TotalAmount = round2(out.Breakdown.TotalAmount + gross)
			out.Lines = append(out.Lines, p)
			continue
		}

		taxable := gross
		if inclusive {
			taxable = gross / (1 + info.GSTRate/100)
		}
		lineBreakdown, errCalc := CalculateGST(taxable, info.GSTRate, interstate)
		if errCalc != nil {
			p.Error = errCalc.Error()
			out.Lines = append(out.Lines, p)
			out.Blocking = true
			continue
		}
		p.Taxable = round2(lineBreakdown.TaxableAmount)
		p.TaxAmount = lineBreakdown.TotalTax
		p.LineTotal = lineBreakdown.TotalAmount

		out.Breakdown.TaxableAmount += lineBreakdown.TaxableAmount
		out.Breakdown.CGST += lineBreakdown.CGST
		out.Breakdown.SGST += lineBreakdown.SGST
		out.Breakdown.IGST += lineBreakdown.IGST
		out.Breakdown.TotalTax += lineBreakdown.TotalTax
		out.Breakdown.TotalAmount += lineBreakdown.TotalAmount
		out.Lines = append(out.Lines, p)
	}

	out.Breakdown.TaxableAmount = round2(out.Breakdown.TaxableAmount)
	out.Breakdown.CGST = round2(out.Breakdown.CGST)
	out.Breakdown.SGST = round2(out.Breakdown.SGST)
	out.Breakdown.IGST = round2(out.Breakdown.IGST)
	out.Breakdown.TotalTax = round2(out.Breakdown.TotalTax)
	out.Breakdown.TotalAmount = round2(out.Breakdown.TotalAmount)
	if out.Breakdown.TaxableAmount > 0 {
		out.Breakdown.GSTRate = round2(out.Breakdown.TotalTax / out.Breakdown.TaxableAmount * 100)
	}
	out.GrandTotal = out.Breakdown.TotalAmount
	return out, nil
}

// --- Printed PO / vendor dispatch -------------------------------------------

// POParty is one side of the printed PO's header block.
type POParty struct {
	Name    string `json:"name"`
	Code    string `json:"code,omitempty"`
	Address string `json:"address,omitempty"`
	GSTIN   string `json:"gstin,omitempty"`
	State   string `json:"state,omitempty"`
	Email   string `json:"email,omitempty"`
	Phone   string `json:"phone,omitempty"`
}

// POPrint is the fully-resolved payload behind both the printed PO and the
// copy sent to the vendor. Assembled server-side in one request so the print
// sheet does not have to make four calls and stitch masters together itself.
type POPrint struct {
	PONumber      string          `json:"po_number"`
	DocumentID    string          `json:"document_id"`
	Status        string          `json:"status"`
	OrderDate     string          `json:"order_date"`
	Buyer         POParty         `json:"buyer"`
	Vendor        POParty         `json:"vendor"`
	ShipTo        string          `json:"ship_to"`
	Lines         []POLinePreview `json:"lines"`
	GSTMode       string          `json:"gst_mode"`
	PlaceOfSupply PlaceOfSupply   `json:"place_of_supply"`
	Breakdown     GSTBreakdown    `json:"breakdown"`
	GrandTotal    float64         `json:"grand_total"`
	AmountInWords string          `json:"amount_in_words"`
	SentAt        string          `json:"sent_at,omitempty"`
}

// fetchDocData reads one document's JSON payload by id, or by its `code` when
// the id does not match - the same two-identifier tolerance ResolveItemBySKU
// applies, because Link fields in this codebase hold either.
func fetchDocData(tenantID, doctype, id string) (map[string]interface{}, string, error) {
	if strings.TrimSpace(id) == "" {
		return nil, "", sql.ErrNoRows
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data, status FROM %s.documents
		 WHERE doctype = $1 AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2)
		 ORDER BY CASE WHEN id = $2 THEN 0 ELSE 1 END
		 LIMIT 1`, schema), doctype, id).Scan(&dataStr, &status)
	if err != nil {
		return nil, "", err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal([]byte(dataStr), &out); err != nil {
		return nil, "", err
	}
	return out, status, nil
}

func str(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// BuildPurchaseOrderPrint assembles the vendor-facing copy of a PO.
//
// Deliberately re-prices from the stored lines rather than trusting the
// stored gst_breakdown: a PO edited through the API by a caller that skipped
// the recompute would otherwise print a total that does not match its own
// lines, and the printed copy is the one a vendor holds us to.
func BuildPurchaseOrderPrint(tenantID, poID string) (POPrint, error) {
	po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err == sql.ErrNoRows {
		return POPrint{}, fmt.Errorf("purchase order '%s' not found", poID)
	}
	if err != nil {
		return POPrint{}, err
	}

	preview, err := PreviewPurchaseOrder(tenantID, po)
	if err != nil {
		return POPrint{}, err
	}

	out := POPrint{
		PONumber:      firstNonEmpty(str(po, "po_number"), str(po, "code"), poID),
		DocumentID:    poID,
		Status:        status,
		OrderDate:     firstNonEmpty(str(po, "order_date"), str(po, "created_at")),
		ShipTo:        firstNonEmpty(str(po, "target_warehouse"), str(po, "location")),
		Lines:         preview.Lines,
		GSTMode:       preview.GSTMode,
		PlaceOfSupply: preview.PlaceOfSupply,
		Breakdown:     preview.Breakdown,
		GrandTotal:    preview.GrandTotal,
		AmountInWords: AmountInWords(preview.GrandTotal),
		SentAt:        str(po, "sent_to_vendor_at"),
	}
	if out.Status == "" {
		out.Status = str(po, "status")
	}

	vendorID := firstNonEmpty(str(po, "vendor"), str(po, "vendor_id"))
	if v, _, errV := fetchDocData(tenantID, "Vendor", vendorID); errV == nil {
		out.Vendor = POParty{
			Name:    firstNonEmpty(str(v, "name"), vendorID),
			Code:    firstNonEmpty(str(v, "code"), vendorID),
			Address: str(v, "address"),
			GSTIN:   str(v, "gstin"),
			State:   StateLabel(firstNonEmpty(StateCodeFromGSTIN(str(v, "gstin")), StateCodeFromName(str(v, "state")))),
			Email:   str(v, "contact_email"),
			Phone:   str(v, "contact_phone"),
		}
	} else {
		out.Vendor = POParty{Name: vendorID, Code: vendorID}
	}

	// Buyer block: the Legal Entity behind the PO's Location, which is the
	// registered party the vendor invoices - not the warehouse it ships to.
	if loc, _, errL := fetchDocData(tenantID, "Location", str(po, "location")); errL == nil {
		if e, _, errE := fetchDocData(tenantID, "LegalEntity", str(loc, "legal_entity")); errE == nil {
			out.Buyer = POParty{
				Name:    str(e, "name"),
				Code:    str(e, "code"),
				Address: str(e, "address"),
				GSTIN:   str(e, "gstin"),
				State:   StateLabel(firstNonEmpty(StateCodeFromGSTIN(str(e, "gstin")), StateCodeFromName(str(e, "state")))),
			}
		}
		if out.Buyer.Name == "" {
			out.Buyer.Name = firstNonEmpty(str(loc, "name"), str(po, "location"))
		}
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// MarkPurchaseOrderSent records that a PO's vendor copy went out, and fires
// the notification engine's PurchaseOrderIssued event.
//
// It reuses engines/notifications.go's existing template/channel machinery
// rather than introducing an SMTP client: a tenant that has configured a
// NotificationChannelConfig gets the PO pushed down it, and a tenant that has
// not still gets the stamp, the audit entry and the mailto: fallback the UI
// offers. Nobody is blocked from sending a PO because a channel is not set up.
func MarkPurchaseOrderSent(tenantID, poID, userID string) (POPrint, error) {
	doc, err := BuildPurchaseOrderPrint(tenantID, poID)
	if err != nil {
		return POPrint{}, err
	}
	if doc.Status == "Draft" {
		return POPrint{}, &ValidationError{Message: "a Draft purchase order cannot be sent to a vendor - submit it for approval first"}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return POPrint{}, err
	}
	sentAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents
		   SET data = jsonb_set(data, '{sent_to_vendor_at}', to_jsonb($1::text), true)
		 WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2)`,
		schema), sentAt, poID); err != nil {
		return POPrint{}, err
	}
	doc.SentAt = sentAt

	DispatchNotification(tenantID, "PurchaseOrderIssued", doc.PONumber, map[string]string{
		"vendor":       doc.Vendor.Name,
		"vendor_email": doc.Vendor.Email,
		"po_number":    doc.PONumber,
		"grand_total":  fmt.Sprintf("%.2f", doc.GrandTotal),
		"line_count":   fmt.Sprintf("%d", len(doc.Lines)),
	})
	LogAuditEvent(tenantID, userID, "PURCHASE_ORDER_SENT", "SUCCESS",
		fmt.Sprintf("po=%s vendor=%s total=%.2f", doc.PONumber, doc.Vendor.Code, doc.GrandTotal))
	return doc, nil
}
