package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 62 (user, 2026-10-15): a GRN receives against one or more POs of the
// same vendor, carries the PO price and the vendor's invoice price on every
// line, and the difference between the two is settled by a Debit Note
// (invoice above PO) or a Vendor Credit Note (invoice below PO) raised and
// posted automatically - "never on hold".
//
// Prices here are per unit and ex-GST (the taxable rate a GST invoice prints
// per line). The PO rate is always read from the PO by the server, never
// taken from the screen. Stock and 2100 GRN Suspense are booked at the
// invoice rate, so the vendor's bill matches its GRN; the notes then move the
// difference to 5150, so net payable and net purchase cost both land on the
// agreed PO value.

// grnPOLineRate is one SKU's agreed ex-GST unit price on a PO.
type grnPOLineRate struct {
	Rate    float64
	GSTRate float64
}

// poRatesBySKU prices a PO through PreviewPurchaseOrder - the same function the
// PO screen, its print and GRN costing already use - so "PO rate" means one
// thing everywhere. Several lines of one SKU are blended by quantity.
func poRatesBySKU(tenantID, poID string) (map[string]grnPOLineRate, map[string]interface{}, error) {
	data, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		return nil, nil, err
	}
	preview, err := PreviewPurchaseOrder(tenantID, data)
	if err != nil {
		return nil, data, err
	}
	type acc struct{ taxable, gst float64; qty int }
	sums := map[string]*acc{}
	for _, l := range preview.Lines {
		if l.Error != "" || l.SKU == "" || l.Qty <= 0 {
			continue
		}
		a := sums[l.SKU]
		if a == nil {
			a = &acc{}
			sums[l.SKU] = a
		}
		a.taxable += l.Taxable
		a.gst = l.GSTRate
		a.qty += l.Qty
	}
	out := map[string]grnPOLineRate{}
	for sku, a := range sums {
		out[sku] = grnPOLineRate{Rate: round2(a.taxable / float64(a.qty)), GSTRate: a.gst}
	}
	return out, data, nil
}

// GRNPOIDs lists the POs a GRN receives against, in first-seen order: the
// header po_id, then po_ids, then any line's own po_id. Every existing reader
// of a GRN's single po_id keeps working, because the header still carries the
// first one.
func GRNPOIDs(data map[string]interface{}) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	add(strField(data, "po_id"))
	for _, id := range strings.Split(strField(data, "po_ids"), ",") {
		add(id)
	}
	for _, l := range grnLineMaps(data) {
		add(strField(l, "po_id"))
	}
	return out
}

// grnLineMaps reads received_items as generic maps, so every key a line
// carries (batch, serials, weight...) survives a rewrite untouched.
func grnLineMaps(data map[string]interface{}) []map[string]interface{} {
	raw, _ := data["received_items"].(string)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var lines []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		return nil
	}
	return lines
}

// grnLineAcceptedQty mirrors grnReceivedLine.derivedAcceptedQty: an explicit
// accepted_qty wins, else received less rejected and damaged.
func grnLineAcceptedQty(l map[string]interface{}) float64 {
	if v, ok := l["accepted_qty"]; ok && v != nil && fmt.Sprintf("%v", v) != "" {
		return numFromInterface(v)
	}
	q := numFromInterface(l["qty"]) - numFromInterface(l["rejected_qty"]) - numFromInterface(l["damaged_qty"])
	if q < 0 {
		return 0
	}
	return q
}

// lineNumberSet reports whether a line key holds a usable number (a blank
// string from the screen counts as unset).
func lineNumberSet(l map[string]interface{}, key string) bool {
	v, ok := l[key]
	if !ok || v == nil {
		return false
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	return true
}

// PrepareGRNPricing runs on a new GRN before it is validated and written:
//   - resolves every line's PO (a line without one takes the header PO),
//     sets the header po_id/po_ids, and refuses POs of different vendors;
//   - stamps each line's PO rate and GST rate from its PO, and defaults the
//     invoice rate to the PO rate;
//   - records the totals the GRN screen and print show: PO value, invoice
//     value + GST, and the difference to the vendor invoice amount typed on
//     the header (a control figure - it never blocks the receipt).
//
// Edits are left alone: a posted GRN's prices drove its stock cost and notes.
func PrepareGRNPricing(tenantID string, isCreate bool, payload map[string]interface{}) error {
	if !isCreate {
		return nil
	}
	lines := grnLineMaps(payload)
	headerPO := strings.TrimSpace(strField(payload, "po_id"))
	if headerPO == "" {
		for _, id := range strings.Split(strField(payload, "po_ids"), ",") {
			if id = strings.TrimSpace(id); id != "" {
				headerPO = id
				break
			}
		}
	}
	for _, l := range lines {
		if strings.TrimSpace(strField(l, "po_id")) == "" && headerPO != "" {
			l["po_id"] = headerPO
		}
	}
	if headerPO != "" {
		payload["po_id"] = headerPO
	}
	pos := GRNPOIDs(payload)
	if len(pos) == 0 {
		return nil
	}
	payload["po_id"] = pos[0]
	payload["po_ids"] = strings.Join(pos, ",")

	rates := map[string]map[string]grnPOLineRate{}
	vendor := ""
	for _, po := range pos {
		r, poData, err := poRatesBySKU(tenantID, po)
		if err == sql.ErrNoRows || poData == nil {
			// The Link check (META-0198) names a missing PO better than this could.
			continue
		}
		if err != nil {
			return err
		}
		rates[po] = r
		v := firstNonEmpty(str(poData, "vendor"), str(poData, "vendor_id"))
		if vendor == "" {
			vendor = v
		} else if v != "" && v != vendor {
			return &ValidationError{Code: "GOODSR-0318", SubFor: "Purchase Orders", Message: fmt.Sprintf(
				"PO %s is for vendor %s, but this receipt is for vendor %s - one goods receipt can only take POs of the same vendor", po, v, vendor)}
		}
	}
	if vendor != "" && strings.TrimSpace(strField(payload, "vendor")) == "" {
		payload["vendor"] = vendor
	}

	var poValue, invValue, invGST float64
	for _, l := range lines {
		sku := strField(l, "sku")
		r, ok := rates[strField(l, "po_id")][sku]
		if ok {
			l["po_rate"] = r.Rate
			l["gst_rate"] = r.GSTRate
		}
		if lineNumberSet(l, "invoice_rate") {
			ir := numFromInterface(l["invoice_rate"])
			if ir < 0 || math.IsNaN(ir) || math.IsInf(ir, 0) {
				return &ValidationError{Code: "GOODSR-0319", SubFor: "Invoice Rate", Message: fmt.Sprintf(
					"invoice rate for SKU %q must be zero or more", sku)}
			}
			l["invoice_rate"] = round2(ir)
		} else if ok {
			l["invoice_rate"] = r.Rate
		} else {
			continue
		}
		acc := grnLineAcceptedQty(l)
		ir := numFromInterface(l["invoice_rate"])
		gst := numFromInterface(l["gst_rate"])
		if ok {
			poValue += r.Rate * acc
		}
		invValue += ir * acc
		invGST += ir * acc * gst / 100
	}
	if encoded, err := json.Marshal(lines); err == nil && lines != nil {
		payload["received_items"] = string(encoded)
	}
	payload["po_value"] = round2(poValue)
	payload["invoice_value"] = round2(invValue)
	payload["invoice_gst"] = round2(invGST)
	payload["invoice_total"] = round2(invValue + invGST)
	if typed := numFromInterface(payload["vendor_invoice_amount"]); typed > 0 {
		payload["vendor_invoice_amount"] = round2(typed)
		payload["invoice_amount_difference"] = round2(typed - (invValue + invGST))
	}
	return nil
}

// grnLineUnitCostsPaise is the ex-GST unit cost of each SKU on a priced GRN
// (Stage 62): accepted-qty-weighted across lines, so the same SKU received
// from two POs at two prices costs at their blend. ok=false for a GRN written
// before Stage 62 (no invoice_rate), which keeps costing from its PO.
func grnLineUnitCostsPaise(tenantID, grnID string) map[string]int64 {
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return nil
	}
	type acc struct{ value, qty float64 }
	sums := map[string]*acc{}
	for _, l := range grnLineMaps(data) {
		if !lineNumberSet(l, "invoice_rate") {
			continue
		}
		sku := strField(l, "sku")
		q := grnLineAcceptedQty(l)
		if sku == "" || q <= 0 {
			continue
		}
		a := sums[sku]
		if a == nil {
			a = &acc{}
			sums[sku] = a
		}
		a.value += numFromInterface(l["invoice_rate"]) * q
		a.qty += q
	}
	out := map[string]int64{}
	for sku, a := range sums {
		out[sku] = RupeesToPaise(a.value / a.qty)
	}
	return out
}

// GRNRateDifferenceLine is one line of an automatic rate-difference note.
type GRNRateDifferenceLine struct {
	SKU         string  `json:"sku"`
	POID        string  `json:"po_id"`
	Qty         float64 `json:"qty"`
	PORate      float64 `json:"po_rate"`
	InvoiceRate float64 `json:"invoice_rate"`
	GSTRate     float64 `json:"gst_rate"`
	Taxable     float64 `json:"taxable"`
	GST         float64 `json:"gst"`
}

// grnRateDifferences splits a GRN's priced lines into those billed above the
// PO price (debit) and below it (credit), each with its taxable and GST
// difference on the accepted quantity. Rejected and damaged units are not
// owed at all - they go back on a Purchase Return - so they carry no price
// difference either.
func grnRateDifferences(data map[string]interface{}) (debit, credit []GRNRateDifferenceLine) {
	for _, l := range grnLineMaps(data) {
		if !lineNumberSet(l, "po_rate") || !lineNumberSet(l, "invoice_rate") {
			continue
		}
		q := grnLineAcceptedQty(l)
		po, inv := numFromInterface(l["po_rate"]), numFromInterface(l["invoice_rate"])
		if q <= 0 || math.Abs(inv-po) < 0.005 {
			continue
		}
		gstRate := numFromInterface(l["gst_rate"])
		taxable := round2(math.Abs(inv-po) * q)
		d := GRNRateDifferenceLine{
			SKU: strField(l, "sku"), POID: strField(l, "po_id"), Qty: q,
			PORate: po, InvoiceRate: inv, GSTRate: gstRate,
			Taxable: taxable, GST: round2(taxable * gstRate / 100),
		}
		if inv > po {
			debit = append(debit, d)
		} else {
			credit = append(credit, d)
		}
	}
	return debit, credit
}

// GRNNotesResult says which notes a GRN raised, and any that could not post.
type GRNNotesResult struct {
	DebitNoteID        string `json:"debit_note_id,omitempty"`
	VendorCreditNoteID string `json:"vendor_credit_note_id,omitempty"`
	Warning            string `json:"warning,omitempty"`
}

// PostGRNRateDifferenceNotes raises and posts the GRN's rate-difference notes.
// Called once the receipt's stock has posted. Idempotent per GRN and note
// type, and never fails the receipt: the goods are in, and a note that could
// not post is reported (and logged) so it can be posted from Debit / Credit
// Notes, rather than holding anything.
func PostGRNRateDifferenceNotes(tenantID, grnID, userID, storeCode string) GRNNotesResult {
	var res GRNNotesResult
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		res.Warning = fmt.Sprintf("could not read GRN %s for its price-difference notes: %v", grnID, err)
		return res
	}
	debit, credit := grnRateDifferences(data)
	if len(debit) == 0 && len(credit) == 0 {
		return res
	}
	if strings.TrimSpace(storeCode) == "" {
		storeCode = "HQ"
	}
	var warnings []string
	raise := func(doctype, series string, lines []GRNRateDifferenceLine) string {
		id, werr := raiseGRNRateNote(tenantID, grnID, userID, storeCode, doctype, series, data, lines)
		if werr != nil {
			warnings = append(warnings, werr.Error())
			LogSystemError(tenantID, "", "ERROR", "PostGRNRateDifferenceNotes", werr.Error(), "")
		}
		return id
	}
	if len(debit) > 0 {
		res.DebitNoteID = raise("DebitNote", "DN", debit)
	}
	if len(credit) > 0 {
		res.VendorCreditNoteID = raise("VendorCreditNote", "VCN", credit)
	}
	res.Warning = strings.Join(warnings, "; ")

	schema, err := db.GetTenantSchema(tenantID)
	if err == nil && (res.DebitNoteID != "" || res.VendorCreditNoteID != "") {
		patch := map[string]interface{}{}
		if res.DebitNoteID != "" {
			patch["debit_note_id"] = res.DebitNoteID
		}
		if res.VendorCreditNoteID != "" {
			patch["vendor_credit_note_id"] = res.VendorCreditNoteID
		}
		if b, merr := json.Marshal(patch); merr == nil {
			if _, uerr := db.DB.Exec(fmt.Sprintf(
				`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`, schema),
				string(b), grnID); uerr != nil {
				LogSystemError(tenantID, "", "ERROR", "PostGRNRateDifferenceNotes",
					fmt.Sprintf("GRN %s: notes raised but not linked on the GRN: %v", grnID, uerr), "")
			}
		}
	}
	return res
}

// existingGRNNote keeps the notes idempotent: one of each type per GRN.
func existingGRNNote(schema, doctype, grnID string) (string, string, error) {
	var id, status string
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, status FROM %s.documents
		 WHERE doctype = $1 AND deleted_at IS NULL AND status <> 'Cancelled'
		   AND data->>'source_doctype' = 'GRN' AND data->>'source_doc_id' = $2
		 ORDER BY created_at, id LIMIT 1`, schema), doctype, grnID).Scan(&id, &status)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return id, status, err
}

func raiseGRNRateNote(tenantID, grnID, userID, storeCode, doctype, series string, grn map[string]interface{}, lines []GRNRateDifferenceLine) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	label := map[string]string{"DebitNote": "debit note", "VendorCreditNote": "vendor credit note"}[doctype]
	id, status, err := existingGRNNote(schema, doctype, grnID)
	if err != nil {
		return "", err
	}
	if id == "" {
		var taxable, gst float64
		var skus []string
		for _, l := range lines {
			taxable += l.Taxable
			gst += l.GST
			skus = append(skus, l.SKU)
		}
		taxable, gst = round2(taxable), round2(gst)
		if taxable <= 0 {
			return "", nil
		}
		if id, err = GenerateSequence(tenantID, series, storeCode, documentFinancialYear(time.Now())); err != nil {
			return "", fmt.Errorf("could not number the %s for GRN %s: %w", label, grnID, err)
		}
		direction := "above"
		if doctype == "VendorCreditNote" {
			direction = "below"
		}
		encodedLines, _ := json.Marshal(lines)
		note := map[string]interface{}{
			"code": id, "note_number": id, "vendor_id": strField(grn, "vendor"),
			"reference_po": strField(grn, "po_id"), "grn_id": grnID,
			"vendor_invoice_no": strField(grn, "vendor_invoice_no"),
			"amount":            taxable, "gst_amount": gst, "total_with_gst": round2(taxable + gst),
			"reason": fmt.Sprintf("Price difference on GRN %s: vendor invoice priced %s the PO for %s | GST %.2f; total with GST %.2f",
				firstNonEmpty(strField(grn, "code"), grnID), direction, strings.Join(skus, ", "), gst, round2(taxable+gst)),
			"note_lines":     string(encodedLines),
			"status":         "Draft",
			"source_doctype": "GRN", "source_doc_id": grnID,
		}
		b, err := json.Marshal(note)
		if err != nil {
			return "", err
		}
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
			id, doctype, b, userID); err != nil {
			return "", fmt.Errorf("could not raise the %s for GRN %s: %v", label, grnID, err)
		}
		LogAuditEvent(tenantID, userID, "CREATE_GRN_RATE_NOTE", "SUCCESS",
			fmt.Sprintf("Raised %s %s for %.2f + GST %.2f on GRN %s", label, id, taxable, gst, grnID))
		status = "Draft"
	}
	if status != "Posted" {
		if doctype == "DebitNote" {
			_, err = PostDebitNote(tenantID, id, userID)
		} else {
			_, err = PostVendorCreditNote(tenantID, id, userID)
		}
		if err != nil {
			return id, fmt.Errorf("%s %s was raised for GRN %s but could not be posted (%v) - post it from Debit / Credit Notes", label, id, grnID, err)
		}
	}
	// PostDebitNote books the taxable leg only (a Purchase Return posts its own
	// GST reversal), so the input credit claimed on the overcharge is reversed
	// here. Keyed, so a repeat call never books it twice.
	if doctype == "DebitNote" {
		var gst float64
		for _, l := range lines {
			gst += l.GST
		}
		if gst = round2(gst); gst > 0 {
			if err := PostDoubleEntry(tenantID, "DebitNote", id,
				map[string]int64{"2100": RupeesToPaise(gst)}, map[string]int64{"1500": RupeesToPaise(gst)},
				"", fmt.Sprintf("DebitNote:%s:GST", id)); err != nil {
				return id, fmt.Errorf("debit note %s posted, but its GST reversal of %.2f could not be booked: %v", id, gst, err)
			}
		}
	}
	return id, nil
}

// PostVendorCreditNote posts a Draft VendorCreditNote: the mirror of
// PostDebitNote. The vendor billed below the agreed PO price, so what we owe
// them rises back to that price (Cr 2100) and the purchase cost carried in
// 5150 rises with it (Dr 5150); the GST on the difference is input credit
// (Dr 1500 / Cr 2100). A note with no gst_amount posts the taxable leg only.
func PostVendorCreditNote(tenantID, noteID, userID string) (amount int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, err
	}
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'VendorCreditNote' AND id = $1 FOR UPDATE`, schema),
		noteID).Scan(&dataStr, &status); err != nil {
		return 0, fmt.Errorf("vendor credit note not found: %v", err)
	}
	if status == "Posted" {
		return 0, fmt.Errorf("vendor credit note is already Posted")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	amountF := numFromInterface(data["amount"])
	if amountF <= 0 {
		return 0, fmt.Errorf("amount must be positive to post")
	}
	if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorCreditNote", noteID,
		map[string]int64{"5150": RupeesToPaise(amountF)}, map[string]int64{"2100": RupeesToPaise(amountF)},
		"", fmt.Sprintf("VendorCreditNote:%s:POST", noteID)); err != nil {
		return 0, fmt.Errorf("GL posting failed, note not marked Posted: %v", err)
	}
	if gst := numFromInterface(data["gst_amount"]); gst > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorCreditNote", noteID,
			map[string]int64{"1500": RupeesToPaise(gst)}, map[string]int64{"2100": RupeesToPaise(gst)},
			"", fmt.Sprintf("VendorCreditNote:%s:GST", noteID)); err != nil {
			return 0, fmt.Errorf("GL posting failed, note not marked Posted: %v", err)
		}
	}
	data["status"] = "Posted"
	b, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorCreditNote' AND id = $2`, schema),
		b, noteID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "POST_VENDOR_CREDIT_NOTE", "SUCCESS", fmt.Sprintf("Posted vendor credit note %s amount=%.2f", noteID, amountF))
	return int(amountF), nil
}

// readPricedGRNBilling values a Stage 62 GRN for vendor-invoice matching: what
// the vendor may bill is the accepted quantity at the GRN's own invoice rates
// plus GST at each line's rate, less accepted stock already returned. nil for
// a GRN with any unpriced line (written before Stage 62), which keeps the
// PO-rate basis it was always matched on.
func readPricedGRNBilling(tx queryRower, schema, grnID string, grnData map[string]interface{}) *grnBilling {
	lines := grnLineMaps(grnData)
	if len(lines) == 0 {
		return nil
	}
	type acc struct{ value, qty, gstRate float64 }
	bySKU := map[string]*acc{}
	var net, gross float64
	for _, l := range lines {
		if !lineNumberSet(l, "invoice_rate") {
			return nil
		}
		sku := strField(l, "sku")
		q := grnLineAcceptedQty(l)
		ir, gst := numFromInterface(l["invoice_rate"]), numFromInterface(l["gst_rate"])
		net += ir * q
		gross += ir * q * (1 + gst/100)
		a := bySKU[sku]
		if a == nil {
			a = &acc{}
			bySKU[sku] = a
		}
		a.value += ir * q
		a.qty += q
		a.gstRate = gst
	}
	rate := map[string]float64{}
	for sku, a := range bySKU {
		if a.qty > 0 {
			rate[sku] = a.value / a.qty
		}
	}
	returned, err := postedPurchaseReturnAcceptedQty(tx, schema, grnID)
	if err != nil {
		return nil
	}
	for sku, q := range returned {
		if a, ok := bySKU[sku]; ok {
			net -= rate[sku] * q
			gross -= rate[sku] * q * (1 + a.gstRate/100)
		}
	}
	if net < 0 {
		net = 0
	}
	ratio := 1.0
	if net > 0 && gross > 0 {
		ratio = gross / net
	}
	return &grnBilling{TaxRatio: ratio, AcceptedNet: net, RateBySku: rate, Priced: true}
}
