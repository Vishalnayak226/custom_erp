package engines

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 148.8: what happens to a charge after the receipt: the carrier's bill, the
// vendor's own bill and a return.

// chargeEntry is an entry read for change.
type chargeEntry struct {
	ID, Status string
	Data       map[string]interface{}
	Lines      []map[string]interface{}
}

func chargeEntryFromRow(id, status string, raw []byte) (*chargeEntry, error) {
	e := &chargeEntry{ID: id, Status: status}
	if err := json.Unmarshal(raw, &e.Data); err != nil {
		return nil, err
	}
	e.Lines = jsonRows(e.Data["lines"])
	return e, nil
}

func (e *chargeEntry) paise(field string) int64 { return int64(numFromInterface(e.Data[field])) }

func (e *chargeEntry) setOpen(open int64) {
	e.Data["open_paise"], e.Data["open_amount"] = open, PaiseToRupees(open)
}

func (e *chargeEntry) saveTx(tx *sql.Tx, schema string) error {
	lines := make([]interface{}, len(e.Lines))
	for i, l := range e.Lines {
		lines[i] = l
	}
	e.Data["lines"] = lines
	e.Data["note"] = chargeEntryNote(e.Lines)
	e.Data["status"] = e.Status
	raw, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = $3 AND id = $4`, schema),
		raw, e.Status, chargeEntryDoctype, e.ID)
	return err
}

func (e *chargeEntry) intoCost() bool { return strField(e.Data, "into_cost") != "No" }

func (e *chargeEntry) thirdParty() bool { return strField(e.Data, "payee") == chargePayeeThirdParty }

// grnChargeEntries reads a GRN's posted entries, locked when forUpdate.
func grnChargeEntries(q queryRower, schema, grnID string, forUpdate bool) ([]*chargeEntry, error) {
	lock := ""
	if forUpdate {
		lock = "FOR UPDATE"
	}
	rows, err := q.Query(fmt.Sprintf(`SELECT id, status, data FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND data->>'grn_id' = $2 ORDER BY id %s`, schema, lock), chargeEntryDoctype, grnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*chargeEntry
	for rows.Next() {
		var id, status string
		var raw []byte
		if err := rows.Scan(&id, &status, &raw); err != nil {
			return nil, err
		}
		e, err := chargeEntryFromRow(id, status, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- The carrier's bill ----------------------------------------------------

type ChargeBillInput struct {
	EntryIDs      []string `json:"entry_ids"`
	VendorID      string   `json:"vendor_id"`
	InvoiceNumber string   `json:"invoice_number"`
	InvoiceDate   string   `json:"invoice_date"`
	InvoiceAmount float64  `json:"invoice_amount"`
	GSTAmount     *float64 `json:"gst_amount"`
}

type ChargeBillResult struct {
	InvoiceID  string  `json:"invoice_id"`
	Replayed   bool    `json:"replayed"`
	Difference float64 `json:"difference"`
}

func chargeBillRefused(code, sub, format string, args ...interface{}) error {
	return &ValidationError{Code: code, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// BillChargeEntries books a third party's bill against the estimates it settles.
// The bill is an ordinary VendorInvoice that pays through the ordinary path, but
// it carries no grn_id or po_id: those mark the goods bill, and everything that
// reads a GRN's bills (three-way match, the return's GST) means the goods bill.
//
// Each entry's clearing goes to nought. The bill is split over the entries by what
// each estimated, and the difference goes through addCostAdjustmentLegsTx: the
// share of units still here into their cost, of units sold to COGS variance.
func BillChargeEntries(tenantID, userID string, in ChargeBillInput) (*ChargeBillResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	number := strings.TrimSpace(in.InvoiceNumber)
	vendor := strings.TrimSpace(in.VendorID)
	ids := append([]string(nil), in.EntryIDs...)
	sort.Strings(ids)
	switch {
	case len(ids) == 0:
		return nil, chargeBillRefused("FIN-0896", "Charge entries", "choose the entries this bill settles")
	case vendor == "":
		return nil, chargeBillRefused("FIN-0896", "Vendor", "say whose bill this is")
	case number == "":
		return nil, chargeBillRefused("FIN-0896", "Invoice number", "the bill needs its number")
	case in.InvoiceAmount <= 0:
		return nil, chargeBillRefused("FIN-0896", "Amount", "the bill needs an amount above nought")
	}
	if in.GSTAmount != nil && (*in.GSTAmount < 0 || *in.GSTAmount >= in.InvoiceAmount) {
		return nil, chargeBillRefused("FIN-0896", "GST", "the GST must be less than the bill")
	}
	vendorDoc, _, err := fetchDocData(tenantID, "Vendor", vendor)
	if err != nil {
		return nil, chargeBillRefused("FIN-0896", "Vendor", "vendor %s is not on file", vendor)
	}
	sum := sha256.Sum256([]byte(vendor + "|" + strings.ToLower(number)))
	billID := "VI-CHG-" + hex.EncodeToString(sum[:])[:20]

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	entries := make([]*chargeEntry, 0, len(ids))
	onThisBill := 0
	for _, id := range ids {
		var status string
		var raw []byte
		err := tx.QueryRow(fmt.Sprintf(`SELECT status, data FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema),
			chargeEntryDoctype, id).Scan(&status, &raw)
		if err == sql.ErrNoRows {
			return nil, chargeBillRefused("FIN-0895", "Charge entries", "charge entry %s is not on file", id)
		}
		if err != nil {
			return nil, err
		}
		e, err := chargeEntryFromRow(id, status, raw)
		if err != nil {
			return nil, err
		}
		switch bill := strField(e.Data, "bill_id"); {
		case bill == billID:
			onThisBill++
		case bill != "":
			return nil, chargeBillRefused("FIN-0897", "Charge entries", "charge entry %s is already on bill %s", id, bill)
		}
		entries = append(entries, e)
	}

	// Already booked? The same bill again is the same bill, nothing new.
	var billRaw []byte
	err = tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 AND deleted_at IS NULL`, schema), billID).Scan(&billRaw)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		var bill map[string]interface{}
		if err := json.Unmarshal(billRaw, &bill); err != nil {
			return nil, err
		}
		if onThisBill == len(ids) && len(jsonStrings(bill["charge_entries"])) == len(ids) && math.Abs(numFromInterface(bill["invoice_amount"])-in.InvoiceAmount) < 0.005 {
			return &ChargeBillResult{InvoiceID: billID, Replayed: true}, nil
		}
		return nil, chargeBillRefused("FIN-0899", "Invoice number", "bill %s of this vendor is already booked for other entries or another amount", number)
	}
	var other string
	err = tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND status <> 'Cancelled' AND data->>'vendor_id' = $1 AND lower(trim(data->>'invoice_number')) = lower($2) LIMIT 1`, schema),
		vendor, number).Scan(&other)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		return nil, chargeBillRefused("FIN-0899", "Invoice number", "bill %s of this vendor is already booked as %s", number, other)
	}

	var weightSum, gstWeighted, openSum int64
	var location string
	weights := make([]int64, len(entries))
	for i, e := range entries {
		billable := e.thirdParty() && (e.Status == chargeStatusEstimated || (e.Status == chargeStatusReversed && e.paise("amount_paise") > 0))
		if !billable {
			return nil, chargeBillRefused("FIN-0895", "Charge entries", "charge entry %s is %s and is not waiting for a third party's bill", e.ID, e.Status)
		}
		if carrier := strField(e.Data, "vendor_id"); carrier != "" && carrier != vendor && carrier != strField(vendorDoc, "code") {
			return nil, chargeBillRefused("FIN-0898", "Vendor", "charge entry %s is for carrier %s, not %s", e.ID, carrier, vendor)
		}
		weights[i] = e.paise("amount_paise")
		weightSum += weights[i]
		gstWeighted += sharePaise(weights[i], numFromInterface(e.Data["gst_rate"]), 1)
		openSum += e.paise("open_paise")
		location = firstNonEmpty(location, strField(e.Data, "location"))
	}
	gst := 0.0
	if in.GSTAmount != nil {
		gst = round2(*in.GSTAmount)
	} else if weightSum > 0 && gstWeighted > 0 {
		rate := float64(gstWeighted) / float64(weightSum)
		gst = round2(in.InvoiceAmount * rate / (100 + rate))
	}
	taxable := RupeesToPaise(round2(in.InvoiceAmount - gst))

	billDate := firstNonEmpty(strings.TrimSpace(in.InvoiceDate), BusinessToday())
	doc := map[string]interface{}{"code": billID, "invoice_number": number, "vendor_id": vendor, "invoice_date": billDate,
		"invoice_amount": in.InvoiceAmount, "taxable_amount": PaiseToRupees(taxable), "gst_amount": gst, "charge_entries": ids,
		"financial_year": documentFinancialYear(time.Now()), "source_doctype": chargeEntryDoctype, "payment_basis_exact": true, "status": "Matched"}
	if due := VendorBillDueDate(tenantID, "", vendor, billDate, billDate); due != "" {
		doc["due_date"] = due
	}
	issueNumberInto(doc, tenantID, "VendorInvoice", billID, "", location)
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, 'Matched', $3)`, schema), billID, raw, userID); err != nil {
		return nil, fmt.Errorf("the bill could not be booked: %w", err)
	}

	shares := spreadPaise(taxable, weights)
	for i, e := range entries {
		if err := trueUpChargeEntryTx(tx, tenantID, schema, e, billID, shares[i]); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "CHARGE_ENTRY_BILLED", "SUCCESS", fmt.Sprintf("Bill %s of %s for %.2f trued up %d charge entries", number, vendor, in.InvoiceAmount, len(ids)))
	return &ChargeBillResult{InvoiceID: billID, Difference: PaiseToRupees(taxable - openSum)}, nil
}

// trueUpChargeEntryTx moves one entry from clearing to the bill. Units already
// returned are out of it: the difference lands on what is left of the receipt.
func trueUpChargeEntryTx(tx *sql.Tx, tenantID, schema string, e *chargeEntry, billID string, billed int64) error {
	open := e.paise("open_paise")
	credit := strField(e.Data, "credit_account")
	legs := map[string]int64{credit: open, "2100": -billed}
	diff := billed - open
	if diff != 0 {
		weights := make([]int64, len(e.Lines))
		for i, l := range e.Lines {
			weights[i] = int64(numFromInterface(l["share"]) - numFromInterface(l["returned_paise"]))
		}
		spread := spreadPaise(diff, weights)
		given := false
		for i, l := range e.Lines {
			if spread[i] == 0 {
				continue
			}
			given = true
			l["share"] = numFromInterface(l["share"]) + float64(spread[i])
			if e.intoCost() {
				left := numFromInterface(l["qty"]) - numFromInterface(l["returned_qty"])
				if _, _, err := addCostAdjustmentLegsTx(tx, tenantID, schema, strField(l, "sku"), strField(e.Data, "grn_id"), left, spread[i], legs); err != nil {
					return fmt.Errorf("charge entry %s on %s: %w", e.ID, strField(l, "sku"), err)
				}
			}
		}
		switch {
		case !e.intoCost():
			legs[strField(e.Data, "gl_account")] += diff
		case !given: // every unit went back: nothing left to carry it
			legs[accountCOGSVariance] += diff
		}
	}
	if err := postSignedLegsTx(tx, tenantID, schema, chargeEntryDoctype, e.ID, legs, fmt.Sprintf("%s:%s:TRUEUP", chargeEntryDoctype, e.ID)); err != nil {
		return fmt.Errorf("charge entry %s journal: %w", e.ID, err)
	}
	e.Data["bill_id"], e.Data["billed_paise"] = billID, billed
	e.setOpen(0)
	if e.Status != chargeStatusReversed {
		e.Status = chargeStatusTruedUp
	}
	return e.saveTx(tx, schema)
}

func jsonStrings(v interface{}) []string {
	var out []string
	if list, ok := v.([]interface{}); ok {
		for _, s := range list {
			if text, ok := s.(string); ok {
				out = append(out, text)
			}
		}
	}
	return out
}

// ---- The vendor's own bill -------------------------------------------------

// vendorBillCharge is what a GRN's vendor-paid charges add to a bill. Net, GST
// and Gross are what is still to be billed; AllGross is the whole of it.
type vendorBillCharge struct {
	Net, GST, Gross, AllGross int64
}

func vendorChargeForBillTx(q queryRower, schema, grnID, invoiceID string) (vendorBillCharge, error) {
	var out vendorBillCharge
	if grnID == "" {
		return out, nil
	}
	entries, err := grnChargeEntries(q, schema, grnID, false)
	if err != nil || len(entries) == 0 {
		return out, err
	}
	for _, e := range entries {
		if e.Status == chargeStatusSkipped || e.thirdParty() {
			continue
		}
		net := e.paise("amount_paise") - e.paise("returned_paise")
		out.Net += net
		out.GST += sharePaise(net, numFromInterface(e.Data["gst_rate"]), 100)
	}
	out.Gross = out.Net + out.GST
	out.AllGross = out.Gross
	if out.Gross == 0 {
		return out, nil
	}
	var otherNet, otherGST float64
	if err := q.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(NULLIF(data->>'charge_net', '')::numeric), 0)::float8, COALESCE(SUM(NULLIF(data->>'charge_gst', '')::numeric), 0)::float8
		FROM %s.documents WHERE doctype = 'VendorInvoice' AND id <> $1 AND deleted_at IS NULL AND status IN (%s) AND data->>'grn_id' = $2`, schema, billableVendorInvoiceStatuses),
		invoiceID, grnID).Scan(&otherNet, &otherGST); err != nil {
		return out, err
	}
	out.Net = max(out.Net-RupeesToPaise(otherNet), 0)
	out.GST = max(out.GST-RupeesToPaise(otherGST), 0)
	out.Gross = out.Net + out.GST
	return out, nil
}

// vendorChargeGrossOnGRN is everything the vendor was to bill for charges on this
// receipt, returns or not: what a typed invoice total holds on top of the goods.
func vendorChargeGrossOnGRN(q queryRower, schema, grnID string) (float64, error) {
	if grnID == "" {
		return 0, nil
	}
	entries, err := grnChargeEntries(q, schema, grnID, false)
	if err != nil {
		return 0, err
	}
	var gross int64
	for _, e := range entries {
		if e.Status == chargeStatusSkipped || e.thirdParty() {
			continue
		}
		net := e.paise("amount_paise")
		gross += net + sharePaise(net, numFromInterface(e.Data["gst_rate"]), 100)
	}
	return PaiseToRupees(gross), nil
}

func minPaise(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// stampStepGRNCharges tells a step GRN how much of the typed invoice total is the
// vendor's charges, so the round-off and the note do not read it as a difference.
// Once the lines have posted it is the entries; before that, what they will be.
func stampStepGRNCharges(q queryRower, tenantID, schema, grnID string, data map[string]interface{}, lines []map[string]interface{}) {
	on, err := tradeChargeCodesOnFile(q, schema)
	if err != nil || !on {
		delete(data, "vendor_charge_gross")
		return
	}
	posted := true
	for _, l := range lines {
		if grnStepAccepted(l) > 0 && strField(l, "posted_at") == "" {
			posted = false
		}
	}
	var gross float64
	if posted {
		if gross, err = vendorChargeGrossOnGRN(q, schema, grnID); err != nil {
			return
		}
	} else {
		all := grnChargeDocLines(tenantID, lines, func(map[string]interface{}) bool { return true })
		plans, err := planReceiptCharges(q, tenantID, schema, grnID, data, "", all, nil)
		if err != nil {
			return
		}
		var paise int64
		for _, p := range plans {
			if p.Skip == "" && !p.ask.code.thirdParty() {
				paise += p.Total + sharePaise(p.Total, p.ask.code.GSTRate, 100)
			}
		}
		gross = PaiseToRupees(paise)
	}
	if gross > 0 {
		data["vendor_charge_gross"] = gross
	} else {
		delete(data, "vendor_charge_gross")
	}
}

// grnInvoiceCheckOf checks the lines against the goods part of the typed total.
func grnInvoiceCheckOf(lines []map[string]interface{}, data map[string]interface{}) *GRNInvoiceCheck {
	charge := numFromInterface(data["vendor_charge_gross"])
	c := grnInvoiceCheck(lines, numFromInterface(data["vendor_invoice_amount"])-charge)
	if c != nil && charge > 0 {
		c.Message += fmt.Sprintf(" That is after taking out %.2f of charges.", charge)
	}
	return c
}

// billedChargeGSTPaiseQ is the charge GST the GRN's bills claimed.
func billedChargeGSTPaiseQ(q queryRower, schema, grnID string) (int64, error) {
	var gst float64
	err := q.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(NULLIF(data->>'charge_gst', '')::numeric), 0)::float8
		FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND status IN (%s) AND data->>'grn_id' = $1`, schema, billableVendorInvoiceStatuses), grnID).Scan(&gst)
	return RupeesToPaise(gst), err
}

// grnChargePerUnit is what the charges added to each unit of a receipt: part of a
// piece's own cost. The pieces are registered after the charge has posted.
func grnChargePerUnit(schema, grnID string) map[string]int64 {
	entries, err := grnChargeEntries(db.DB, schema, grnID, false)
	if err != nil {
		return nil
	}
	shares, qtys := map[string]int64{}, map[string]float64{}
	for _, e := range entries {
		if e.Status == chargeStatusSkipped || !e.intoCost() {
			continue
		}
		for _, l := range e.Lines {
			if int64(numFromInterface(l["share"])) > 0 {
				shares[strField(l, "sku")] += int64(numFromInterface(l["share"]))
				qtys[strField(l, "sku")] = math.Max(qtys[strField(l, "sku")], numFromInterface(l["qty"]))
			}
		}
	}
	out := map[string]int64{}
	for sku, share := range shares {
		if qtys[sku] > 0 {
			out[sku] = sharePaise(share, 1, qtys[sku])
		}
	}
	return out
}

// ---- A return from the receipt ---------------------------------------------

// reverseReceiptChargesTx takes back the receipt's charges on the units returned,
// entry by entry, pro rata to what each line still carries, so the returns of a
// line always sum to its share. It gives back the value that leaves stock per SKU
// (it goes out of cost with the goods) and what the vendor owes back, with GST.
//
// A return to a vendor who was paid the charge credits it through the debit note.
// A carrier's open estimate shrinks. Once the carrier has billed it is paid and
// gone: that freight is a loss, to COGS variance.
func reverseReceiptChargesTx(tx *sql.Tx, tenantID, schema, returnID, grnID string, lines []PurchaseReturnLine) (map[string]int64, int64, int64, error) {
	if grnID == "" {
		return nil, 0, 0, nil
	}
	if on, err := tradeChargeCodesOnFile(tx, schema); err != nil || !on {
		return nil, 0, 0, err
	}
	entries, err := grnChargeEntries(tx, schema, grnID, true)
	if err != nil || len(entries) == 0 {
		return nil, 0, 0, err
	}
	returning := map[string]float64{}
	for _, l := range lines {
		if l.StockBucket == PurchaseReturnBucketAccepted {
			returning[l.SKU] += l.Qty
		}
	}
	chargeBack := map[string]int64{}
	var vendorBack, vendorBackGST int64
	left := map[string]map[string]float64{} // per code: one return is taken once across a code's parts
	for _, e := range entries {
		if e.Status == chargeStatusSkipped || containsString(jsonStrings(e.Data["returns"]), returnID) {
			continue
		}
		key := strField(e.Data, "code_id")
		if left[key] == nil {
			left[key] = map[string]float64{}
			for sku, qty := range returning {
				left[key][sku] = qty
			}
		}
		legs := map[string]int64{}
		var back int64
		took := false
		for _, l := range e.Lines {
			sku := strField(l, "sku")
			qty, done := numFromInterface(l["qty"]), numFromInterface(l["returned_qty"])
			take := math.Min(left[key][sku], qty-done)
			if take <= 0 {
				continue
			}
			took = true
			left[key][sku] -= take
			rest := int64(numFromInterface(l["share"]) - numFromInterface(l["returned_paise"]))
			amount := sharePaise(rest, take, qty-done)
			l["returned_qty"] = done + take
			l["returned_paise"] = numFromInterface(l["returned_paise"]) + float64(amount)
			back += amount
			switch policy, _, known := ItemPolicyForSKU(tenantID, sku); {
			case !e.intoCost():
				legs[strField(e.Data, "gl_account")] -= amount
			case known && !policy.Valuated():
				legs[policy.ExpenseAccount] -= amount
			default:
				legs["1200"] -= amount
				chargeBack[sku] += amount
			}
		}
		if !took {
			continue
		}
		if e.thirdParty() {
			open := e.paise("open_paise")
			fromOpen := minPaise(open, back)
			legs[strField(e.Data, "credit_account")] += fromOpen
			e.setOpen(open - fromOpen)
			switch beyond := back - fromOpen; {
			case beyond == 0:
			case e.intoCost():
				legs[accountCOGSVariance] += beyond
			default: // already expensed and paid for: it stays where it is
				legs[strField(e.Data, "gl_account")] += beyond
			}
		} else {
			legs["5150"] += back
			vendorBack += back
			vendorBackGST += sharePaise(back, numFromInterface(e.Data["gst_rate"]), 100)
		}
		if err := postSignedLegsTx(tx, tenantID, schema, chargeEntryDoctype, e.ID, legs, fmt.Sprintf("%s:%s:RETURN:%s", chargeEntryDoctype, e.ID, returnID)); err != nil {
			return nil, 0, 0, fmt.Errorf("charge entry %s journal: %w", e.ID, err)
		}
		e.Data["returned_paise"] = e.paise("returned_paise") + back
		e.Data["returns"] = append(jsonStrings(e.Data["returns"]), returnID)
		all := true
		for _, l := range e.Lines {
			if numFromInterface(l["share"]) > 0 && numFromInterface(l["returned_qty"]) < numFromInterface(l["qty"]) {
				all = false
			}
		}
		if all {
			e.Status = chargeStatusReversed
		}
		if err := e.saveTx(tx, schema); err != nil {
			return nil, 0, 0, err
		}
	}
	return chargeBack, vendorBack, vendorBackGST, nil
}

// ---- Why a line has what it has --------------------------------------------

type ChargeLineCharge struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Paise int64  `json:"paise"`
}

type ChargeLineView struct {
	SKU     string             `json:"sku"`
	Qty     float64            `json:"qty"`
	Charges []ChargeLineCharge `json:"charges"`
	Reasons []string           `json:"reasons"`
}

// GRNChargeLines is each received line's charges, or why it has none.
func GRNChargeLines(tenantID, grnID string) ([]ChargeLineView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	grn, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return nil, err
	}
	bySKU := map[string]*ChargeLineView{}
	var skus []string
	for _, l := range grnLineMaps(grn) {
		sku := strField(l, "sku")
		if sku == "" {
			continue
		}
		v := bySKU[sku]
		if v == nil {
			v = &ChargeLineView{SKU: sku, Charges: []ChargeLineCharge{}, Reasons: []string{}}
			bySKU[sku] = v
			skus = append(skus, sku)
		}
		v.Qty += grnLineAcceptedQty(l)
	}
	sort.Strings(skus)
	entries, err := grnChargeEntries(db.DB, schema, grnID, false)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		code := strField(e.Data, "charge_code")
		for _, l := range e.Lines {
			sku := strField(l, "sku")
			v := bySKU[sku]
			if v == nil {
				continue
			}
			seen[sku] = true
			if share := int64(numFromInterface(l["share"])); share > 0 {
				v.Charges = append(v.Charges, ChargeLineCharge{Code: code, Label: strField(e.Data, "charge_label"), Paise: share})
			} else if why := strField(l, "reason"); why != "" {
				v.Reasons = append(v.Reasons, code+": "+why)
			}
		}
	}
	rules, err := loadTradeChargeRules(db.DB, schema)
	if err != nil {
		return nil, err
	}
	_, pos, err := chargePOsOf(db.DB, schema, grn)
	if err != nil {
		return nil, err
	}
	vendorGroup := ""
	if len(rules) > 0 {
		vendorGroup = partyGroup(tenantID, "Vendor", "vendor_group", chargeVendorOf(grn, pos))
	}
	out := make([]ChargeLineView, 0, len(skus))
	for _, sku := range skus {
		v := bySKU[sku]
		if !seen[sku] {
			switch {
			case len(entries) > 0:
				v.Reasons = append(v.Reasons, "Not received into stock")
			case len(rules) > 0:
				v.Reasons = append(v.Reasons, chargeNoRuleReason(vendorGroup, itemGroupForSKU(tenantID, sku)))
			default:
				v.Reasons = append(v.Reasons, "No charge on this receipt")
			}
		}
		out = append(out, *v)
	}
	return out, nil
}
