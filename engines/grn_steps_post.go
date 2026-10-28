package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Stage 94 steps ④ and ⑤, the quick path, and Correct after posting.
//
// Money, D1: stock is booked at the GRN price (PostGRNReceiptPart costs a
// step GRN at grn_price), so GRN Suspense (2100) carries what we owe. The
// vendor bill is the vendor's invoice total; the note is invoice total minus
// our total:
//   - Debit note (they billed more): applied to the vendor bill as a
//     settlement line, so the bill's open balance is our total. It books no
//     GL of its own - the over-billed part was never booked.
//   - Vendor credit note (they billed less): we still owe our total, so the
//     extra is owed on the GRN. Its GST part moves input credit to our GST
//     (Dr 1500 / Cr 2100); its taxable part is already in 2100.
// Paying the bill then clears 2100 and 1500 to exactly our figures.

// GRNPostInput is step ④ for one child: the MRP per line (by line number).
// A line left out keeps the MRP already saved on it, else the item's current
// MRP - "pre-filled when one exists".
type GRNPostInput struct {
	MRP    map[string]float64 `json:"mrp"`
	Reason string             `json:"reason"`
}

// PostGRNChildStep sets the MRPs of one child and posts it (94.5): stock at
// the GRN price, batches carrying the MRP (D2), barcodes, GL. When the last
// child posts, the invoice-vs-GRN note is raised and the vendor bill made.
func PostGRNChildStep(tenantID, userID, storeCode, grnID, childKey string, in GRNPostInput) (*GRNStepView, error) {
	g, err := loadStepGRN(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	if err := g.childPostable(childKey); err != nil {
		g.tx.Rollback()
		return nil, err
	}
	location := strField(g.data, "location")
	var mine []map[string]interface{}
	var missing, pending []string
	now := time.Now()
	for _, l := range g.lines {
		if strField(l, "child") != childKey {
			continue
		}
		no := grnLineNo(l)
		sku := strField(l, "sku")
		if strField(l, "excess_status") == "Pending" {
			pending = append(pending, fmt.Sprintf("line %d (%s)", no, sku))
		}
		item, _, _ := fetchDocData(tenantID, "Item", sku)
		current := numFromInterface(item["mrp"])
		mrp, given := in.MRP[strconv.Itoa(no)]
		if !given {
			mrp = grnLineNum(l, "mrp", current)
		}
		if mrp < 0 || math.IsNaN(mrp) || math.IsInf(mrp, 0) {
			g.tx.Rollback()
			return nil, &ValidationError{Code: "GOODSR-0945", SubFor: "MRP", Message: fmt.Sprintf("line %d (%s): the MRP must be zero or more", no, sku)}
		}
		if mrp <= 0 && grnLineNeedsMRP(tenantID, sku) {
			missing = append(missing, fmt.Sprintf("line %d (%s)", no, sku))
		}
		if !lineNumberSet(l, "old_mrp") {
			l["old_mrp"] = current
		}
		if mrp > 0 {
			l["mrp"] = WholeRupeeMRP(mrp) // Stage 94.15
		}
		// D2: a per-SKU-per-day batch already priced at another MRP by an
		// earlier receipt today gets a lot of its own, so each lot has one MRP.
		if b := strField(l, "batch_no"); b != "" && l["batch_auto"] == true && mrp > 0 {
			if existing, _ := GetBatch(tenantID, sku, b); existing != nil && existing.GRN != grnID && existing.MRP > 0 && math.Abs(existing.MRP-mrp) >= 0.005 {
				if fresh, err := GenerateGRNBatchNumber(tenantID, location, now); err == nil {
					l["batch_no"] = fresh
				}
			}
		}
		mine = append(mine, l)
	}
	if len(pending) > 0 {
		g.tx.Rollback()
		return nil, &ValidationError{Code: "GOODSR-0946", Message: "waiting: " + strings.Join(pending, ", ")}
	}
	if len(missing) > 0 {
		g.tx.Rollback()
		return nil, &ValidationError{Code: "GOODSR-0945", SubFor: "MRP", Message: "enter the MRP for " + strings.Join(missing, ", ")}
	}
	if err := g.save(tenantID, userID, "MRP", in.Reason); err != nil {
		return nil, err
	}

	// Stage 135.21: the child's stock posts in the transaction that marks it
	// posted, under the GRN's row lock, and the child is checked again there.
	// The MRPs above commit on their own (kept if the posting fails), so two
	// posts of one child at once both passed the check above, and each posted
	// its stock, QC split and cost - and a second last child, a second bill.
	g, err = loadStepGRN(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	if err := g.childPostable(childKey); err != nil {
		g.tx.Rollback()
		return nil, err
	}
	location = strField(g.data, "location")
	mine = nil
	for _, l := range g.lines {
		if strField(l, "child") == childKey {
			mine = append(mine, l)
		}
	}

	// Post the child's stock through the one receipt posting every GRN uses.
	items := make([]interface{}, 0, len(mine))
	for _, l := range mine {
		c := map[string]interface{}{}
		for k, v := range l {
			c[k] = v
		}
		c["qty"] = grnGRNQty(l)
		c["accepted_qty"] = grnStepAccepted(l)
		items = append(items, c)
	}
	_, afterReceipt, err := postGRNReceiptPartTx(g.tx, tenantID, g.schema, location, items, userID, grnID, childKey)
	if err != nil {
		g.tx.Rollback()
		return nil, &ValidationError{Code: "GOODSR-0944", Message: fmt.Sprintf("child %s could not be posted to stock at %s (%v) - nothing was posted; check the location is active and post again", childKey, location, err)}
	}

	// Mark the child posted; the last one posts the GRN.
	stamp := now.UTC().Format(time.RFC3339)
	children := grnChildren(g.data)
	allPosted := true
	for i := range children {
		if children[i].Key == childKey {
			children[i].Status = GRNStepPosted
			children[i].PostedAt = stamp
			children[i].PostedBy = userID
		}
		if children[i].Status != GRNStepPosted {
			allPosted = false
		}
	}
	setGRNChildren(g.data, children)
	for _, l := range g.lines {
		if strField(l, "child") == childKey {
			l["posted_at"] = stamp
			l["posted_by"] = userID
		}
	}
	if allPosted {
		g.data["grn_step"] = GRNStepPosted
		g.data["status"] = "Approved"
		g.data["posted_at"] = stamp
		g.data["payment_status"] = "Due"
	}
	if err := g.save(tenantID, userID, GRNStepPosted, fmt.Sprintf("Child %s posted", childKey)); err != nil {
		return nil, err
	}
	afterReceipt()
	EnsureReceiptBarcodes(tenantID, grnID, items)
	for _, l := range mine {
		if mrp := numFromInterface(l["mrp"]); mrp > 0 {
			setItemLatestMRP(tenantID, strField(l, "sku"), mrp) // D2: Item MRP shows the latest
		}
	}
	if tx, err := db.DB.Begin(); err == nil {
		_ = db.SetSearchPath(tx, g.schema)
		_ = PublishEvent(tx, g.schema, "inventory.stock_changed", map[string]interface{}{"grn_id": grnID, "location": location, "child": childKey})
		_ = tx.Commit()
	}
	LogAuditEvent(tenantID, userID, "GRN_CHILD_POSTED", "SUCCESS", fmt.Sprintf("GRN %s child %s posted (%d line(s))", grnID, childKey, len(mine)))
	if allPosted {
		MarkASNReceivedIfComplete(tenantID, grnID)
		MatchCrossDockPlansForGRN(tenantID, grnID) // Stage 130.13 (80.7 g)
		finalizeStepGRN(tenantID, userID, storeCode, grnID)
	}
	return GetGRNStepView(tenantID, grnID)
}

// childPostable refuses posting childKey unless the GRN is at the MRP step and
// the child is on it, not yet posted.
func (g *stepGRN) childPostable(childKey string) error {
	if g.step() != GRNStepPriced {
		if g.step() == GRNStepPosted {
			return &ValidationError{Code: "GOODSR-0944", Message: "this goods receipt is already posted"}
		}
		return &ValidationError{Code: "GOODSR-0944", Message: "run the price check first (step 3)"}
	}
	for _, c := range grnChildren(g.data) {
		if c.Key == childKey {
			if c.Status == GRNStepPosted {
				return &ValidationError{Code: "GOODSR-0944", Message: fmt.Sprintf("child %s is already posted", childKey)}
			}
			return nil
		}
	}
	return &ValidationError{Code: "GLOBAL-0002", SubFor: "Child", Message: fmt.Sprintf("there is no child %s on this receipt", childKey)}
}

// setItemLatestMRP keeps Item.mrp at the latest received MRP (D2).
func setItemLatestMRP(tenantID, sku string, mrp float64) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{mrp}', to_jsonb($1::numeric)), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Item' AND id = $2 AND deleted_at IS NULL`, schema), round2(mrp), sku); err != nil {
		LogSystemError(tenantID, "", "WARN", "setItemLatestMRP", fmt.Sprintf("item %s MRP not updated to %.2f: %v", sku, mrp, err), "")
	}
}

// grnNoteLine is one line of the invoice-vs-GRN note: where the difference
// comes from, line by line (D1).
type grnNoteLine struct {
	LineNo       int     `json:"line_no,omitempty"`
	SKU          string  `json:"sku,omitempty"`
	Source       string  `json:"source"` // price | qty | price and qty | round-off
	InvoiceQty   float64 `json:"invoice_qty,omitempty"`
	InvoiceRate  float64 `json:"invoice_rate,omitempty"`
	GRNQty       float64 `json:"grn_qty,omitempty"`
	GRNPrice     float64 `json:"grn_price,omitempty"`
	GSTRate      float64 `json:"gst_rate,omitempty"`
	Taxable      float64 `json:"taxable"`
	GST          float64 `json:"gst"`
	Qty          float64 `json:"qty,omitempty"`
	PORate       float64 `json:"po_rate,omitempty"`
	InvoiceValue float64 `json:"invoice_value,omitempty"`
	OurValue     float64 `json:"our_value,omitempty"`
}

// grnNoteLines splits invoice total - our total into its lines (positive
// means the vendor billed more than we owe).
func grnNoteLines(data map[string]interface{}, lines []map[string]interface{}) (out []grnNoteLine, taxable, gst float64) {
	for _, l := range lines {
		iq, ip := grnInvoiceQty(l), grnInvoicePrice(l)
		aq, gp := grnStepAccepted(l), grnGRNPrice(l)
		rate := numFromInterface(l["gst_rate"])
		iv, ig := grnInvoiceLineAmount(l)
		ov, og := grnOurLineAmount(l)
		t := round2(iv - ov)
		if math.Abs(t) < 0.005 {
			continue
		}
		src := []string{}
		if math.Abs(ip-gp) >= 0.005 {
			src = append(src, "price")
		}
		if math.Abs(iq-aq) > 1e-9 {
			src = append(src, "qty")
		}
		n := grnNoteLine{LineNo: grnLineNo(l), SKU: strField(l, "sku"), Source: strings.Join(src, " and "),
			InvoiceQty: iq, InvoiceRate: round2(ip), GRNQty: aq, GRNPrice: round2(gp), GSTRate: rate,
			Taxable: t, GST: round2(ig - og), Qty: aq, PORate: round2(gp), InvoiceValue: round2(iv), OurValue: round2(ov)}
		out = append(out, n)
		taxable += n.Taxable
		gst += n.GST
	}
	if ro := numFromInterface(data["invoice_unexplained"]); math.Abs(ro) >= 0.005 {
		out = append(out, grnNoteLine{Source: "round-off", Taxable: round2(ro)})
		taxable += ro
	}
	return out, round2(taxable), round2(gst)
}

// finalizeStepGRN runs once the last child has posted: the invoice-vs-GRN
// note (D1) and the vendor bill made from what was keyed at Receive (D9).
// Like Stage 62, nothing here can undo the receipt: a step that fails is
// logged and reported on the GRN, and can be finished by hand.
func finalizeStepGRN(tenantID, userID, storeCode, grnID string) {
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return
	}
	schema, _ := db.GetTenantSchema(tenantID)
	lines := grnLineMaps(data)
	grnStampTotals(data, lines)
	var warnings []string
	patch := map[string]interface{}{}

	billID, billGST, err := ensureStepGRNVendorBill(tenantID, userID, grnID, data)
	if err != nil {
		warnings = append(warnings, err.Error())
	} else {
		patch["vendor_bill_id"] = billID
	}

	diff := numFromInterface(data["note_difference"])
	if math.Abs(diff) >= 0.005 {
		noteLines, taxable, gst := grnNoteLines(data, lines)
		doctype, series := "DebitNote", "DN"
		if diff < 0 {
			doctype, series = "VendorCreditNote", "VCN"
			taxable, gst = -taxable, -gst
			for i := range noteLines {
				noteLines[i].Taxable, noteLines[i].GST = -noteLines[i].Taxable, -noteLines[i].GST
			}
		}
		noteID, nerr := raiseStepGRNNote(tenantID, userID, storeCode, grnID, data, doctype, series, noteLines, taxable, gst, math.Abs(diff))
		if nerr != nil {
			warnings = append(warnings, nerr.Error())
		}
		if noteID != "" {
			if doctype == "DebitNote" {
				patch["debit_note_id"] = noteID
				if billID != "" {
					if aerr := applyDebitNoteToBill(tenantID, userID, billID, noteID, math.Abs(diff), numFromInterface(data["our_gst"])); aerr != nil {
						warnings = append(warnings, aerr.Error())
					}
				}
			} else {
				patch["vendor_credit_note_id"] = noteID
				patch["vcn_total"] = round2(math.Abs(diff))
				// Input credit moves from the bill's GST to ours.
				if billID != "" {
					if gerr := postVCNGSTLeg(tenantID, noteID, numFromInterface(data["our_gst"])-billGST); gerr != nil {
						warnings = append(warnings, gerr.Error())
					}
				}
			}
		}
	}
	if len(warnings) > 0 {
		patch["posting_warning"] = strings.Join(warnings, "; ")
		for _, w := range warnings {
			LogSystemError(tenantID, "", "ERROR", "finalizeStepGRN", fmt.Sprintf("GRN %s: %s", grnID, w), "")
		}
	}
	if b, err := json.Marshal(patch); err == nil && len(patch) > 0 {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`, schema), string(b), grnID); err != nil {
			LogSystemError(tenantID, "", "ERROR", "finalizeStepGRN", fmt.Sprintf("GRN %s: note / bill not linked: %v", grnID, err), "")
		}
	}
	SyncGRNLineRows(tenantID, grnID)
}

// ensureStepGRNVendorBill makes the vendor bill from the Receive figures and
// matches it - or returns the one already made for this GRN.
func ensureStepGRNVendorBill(tenantID, userID, grnID string, grn map[string]interface{}) (string, float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND data->>'grn_id' = $1 ORDER BY created_at LIMIT 1`, schema), grnID).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return "", 0, err
	}
	if id == "" {
		id = NewDocID("VI")
		total := numFromInterface(grn["vendor_invoice_amount"])
		doc := map[string]interface{}{
			"code": id, "invoice_number": strField(grn, "vendor_invoice_no"), "vendor_id": strField(grn, "vendor"),
			"po_id": strField(grn, "po_id"), "grn_id": grnID, "invoice_amount": total,
			"invoice_date": strField(grn, "vendor_invoice_date"), "financial_year": documentFinancialYear(time.Now()),
			"status": "Draft", "source_doctype": "GRN", "source_doc_id": grnID,
		}
		// Stage 130.16 (74.3.2): due by the PO's payment term.
		if due := POPaymentDueDate(tenantID, strField(grn, "po_id"), strField(grn, "vendor_invoice_date")); due != "" {
			doc["due_date"] = due
		}
		issueNumberInto(doc, tenantID, "VendorInvoice", id, "", strField(grn, "location"))
		b, _ := json.Marshal(doc)
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, 'Draft', $3)`, schema), id, b, userID); err != nil {
			return "", 0, fmt.Errorf("the vendor bill could not be made: %v", err)
		}
		LogAuditEvent(tenantID, userID, "CREATE_VENDOR_INVOICE_FROM_GRN", "SUCCESS", fmt.Sprintf("Vendor bill %s made from GRN %s for %.2f", id, grnID, total))
	}
	bill, status, err := fetchDocData(tenantID, "VendorInvoice", id)
	if err != nil {
		return id, 0, err
	}
	if status == "Draft" || status == "MismatchHold" {
		m, err := MatchVendorInvoice(tenantID, id, "", "", 0)
		if err != nil {
			return id, 0, fmt.Errorf("vendor bill %s was made but could not be matched: %v", id, err)
		}
		if !m.Matched {
			return id, 0, fmt.Errorf("vendor bill %s is on hold: %s", id, strings.Join(m.Reasons, " "))
		}
		bill, _, _ = fetchDocData(tenantID, "VendorInvoice", id)
	}
	return id, numFromInterface(bill["gst_amount"]), nil
}

// readStepGRNBilling values a step GRN for its vendor bill: the vendor's
// invoice as keyed at Receive - invoice qty x invoice price, and the typed
// total - so the bill made from it matches exactly.
func readStepGRNBilling(grn map[string]interface{}, lines []map[string]interface{}) *grnBilling {
	rate := map[string]float64{}
	qty := map[string]float64{}
	var net float64
	for _, l := range lines {
		sku := strField(l, "sku")
		v, _ := grnInvoiceLineAmount(l)
		net += v
		rate[sku] += v
		qty[sku] += grnInvoiceQty(l)
	}
	for sku, v := range rate {
		if qty[sku] > 0 {
			rate[sku] = v / qty[sku]
		}
	}
	if net <= 0 {
		return nil
	}
	gross := numFromInterface(grn["vendor_invoice_amount"])
	if gross <= 0 {
		iv, ig := grnInvoiceLineTotals(lines)
		gross = iv + ig
	}
	return &grnBilling{TaxRatio: gross / net, AcceptedNet: net, RateBySku: rate, Priced: true}
}

// raiseStepGRNNote raises the invoice-vs-GRN note, Posted (its GL, where it
// has any, is booked by the caller against the bill). Idempotent per GRN.
func raiseStepGRNNote(tenantID, userID, storeCode, grnID string, grn map[string]interface{}, doctype, series string, lines []grnNoteLine, taxable, gst, total float64) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if id, _, err := existingGRNNote(schema, doctype, grnID); err != nil || id != "" {
		return id, err
	}
	label := map[string]string{"DebitNote": "debit note", "VendorCreditNote": "vendor credit note"}[doctype]
	store := numberingStore(tenantID, storeCode, strField(grn, "location"))
	id, err := GenerateSequence(tenantID, series, store, documentFinancialYear(time.Now()))
	if err != nil {
		return "", fmt.Errorf("could not number the %s for GRN %s: %w", label, grnID, err)
	}
	encoded, _ := json.Marshal(lines)
	direction := "more"
	if doctype == "VendorCreditNote" {
		direction = "less"
	}
	note := map[string]interface{}{
		"code": id, "note_number": id, "vendor_id": strField(grn, "vendor"), "reference_po": strField(grn, "po_id"),
		"grn_id": grnID, "vendor_invoice_no": strField(grn, "vendor_invoice_no"),
		"amount": round2(taxable), "gst_amount": round2(gst), "total_with_gst": round2(total),
		"reason": fmt.Sprintf("Invoice %s total %.2f vs goods receipt %s total %.2f: the vendor billed %.2f %s than received and verified",
			strField(grn, "vendor_invoice_no"), numFromInterface(grn["vendor_invoice_amount"]), firstNonEmpty(strField(grn, "code"), grnID),
			numFromInterface(grn["our_total"]), round2(total), direction),
		"note_lines": string(encoded), "status": "Posted", "basis": "invoice_total_vs_grn",
		"source_doctype": "GRN", "source_doc_id": grnID,
	}
	b, _ := json.Marshal(note)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Posted', $4)`, schema), id, doctype, b, userID); err != nil {
		return "", fmt.Errorf("could not raise the %s for GRN %s: %v", label, grnID, err)
	}
	LogAuditEvent(tenantID, userID, "CREATE_GRN_INVOICE_NOTE", "SUCCESS", fmt.Sprintf("Raised %s %s for %.2f on GRN %s", label, id, total, grnID))
	return id, nil
}

// applyDebitNoteToBill takes the debit note off the vendor bill as a
// settlement line with no GL, leaving our total open: the bill's GST part
// drops to our GST and its goods part to our value.
func applyDebitNoteToBill(tenantID, userID, billID, noteID string, noteTotal, ourGST float64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema), billID).Scan(&dataStr, &status); err != nil {
		return err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	entries, err := readSettlementEntries(data, "payments")
	if err != nil {
		return err
	}
	if _, done := findSettlementByReference(entries, noteID); done {
		return nil
	}
	if status != "Matched" {
		return fmt.Errorf("debit note %s could not be taken off vendor bill %s (bill is %s) - take it off by hand", noteID, billID, status)
	}
	position := documentFXPosition(tenantID, data, "invoice_amount")
	open, outstanding := vendorOpenLegs(data, position)
	notePaise := RupeesToPaise(noteTotal)
	if notePaise > outstanding {
		notePaise = outstanding
	}
	gstLeg := open["1500"] - RupeesToPaise(ourGST)
	if gstLeg > notePaise {
		gstLeg = notePaise
	}
	goodsLeg := notePaise - gstLeg
	if goodsLeg > open["2100"] {
		goodsLeg = open["2100"]
		gstLeg = notePaise - goodsLeg
	}
	legs := map[string]int64{}
	if goodsLeg != 0 {
		legs["2100"] = goodsLeg
	}
	if gstLeg != 0 {
		legs["1500"] = gstLeg
	}
	remaining := noteVendorPayment(data, entries, "Debit note", noteID, "", userID, BusinessToday(), legs, position, 0)
	newStatus := "Matched"
	if remaining <= 0 {
		newStatus = "Paid"
	}
	data["status"] = newStatus
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $3`, schema), b, newStatus, billID); err != nil {
		return err
	}
	return tx.Commit()
}

// postVCNGSTLeg moves input credit by the GST difference a vendor credit
// note carries (our GST - the bill's GST).
func postVCNGSTLeg(tenantID, noteID string, gstDelta float64) error {
	p := RupeesToPaise(gstDelta)
	if p == 0 {
		return nil
	}
	debits, credits := map[string]int64{"1500": p}, map[string]int64{"2100": p}
	if p < 0 {
		debits, credits = map[string]int64{"2100": -p}, map[string]int64{"1500": -p}
	}
	if err := PostDoubleEntry(tenantID, "VendorCreditNote", noteID, debits, credits, "", fmt.Sprintf("VendorCreditNote:%s:GRN_GST", noteID)); err != nil {
		return fmt.Errorf("vendor credit note %s: its GST of %.2f could not be booked: %v", noteID, gstDelta, err)
	}
	return nil
}

// --- the quick path (94.10) ------------------------------------------------------

// PostAllGRNStep runs every step at once when everything matches.
func PostAllGRNStep(tenantID, userID, role, storeCode, grnID string) (*GRNStepView, error) {
	v, err := GetGRNStepView(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	if !v.QuickPath {
		return nil, &ValidationError{Code: "GOODSR-0951", Message: "Post all needs everything to match: " + strings.Join(v.QuickPathReasons, "; ")}
	}
	if v.Step == GRNStepReceived {
		if _, err := VerifyGRNStep(tenantID, userID, role, grnID, GRNVerifyInput{Reason: "Post all"}); err != nil {
			return nil, err
		}
	}
	v, err = PriceCheckGRNStep(tenantID, userID, grnID, GRNPriceCheckInput{Reason: "Post all"})
	if err != nil {
		return nil, err
	}
	for _, c := range v.Children {
		if c.Status == GRNStepPosted {
			continue
		}
		if v, err = PostGRNChildStep(tenantID, userID, storeCode, grnID, c.Key, GRNPostInput{Reason: "Post all"}); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// --- ⑤ Pay (94.7) ----------------------------------------------------------------

// GRNPayment is one payment made from the GRN.
type GRNPayment struct {
	Reference  string  `json:"reference"`
	Mode       string  `json:"mode"`
	Amount     float64 `json:"amount"`
	BillPart   float64 `json:"bill_part"`
	ExtraPart  float64 `json:"extra_part,omitempty"`
	TDS        float64 `json:"tds,omitempty"`
	Date       string  `json:"date"`
	By         string  `json:"by"`
	RecordedAt string  `json:"recorded_at"`
}

func grnPayments(data map[string]interface{}) []GRNPayment {
	out := []GRNPayment{}
	if raw := strField(data, "grn_payments"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// grnNetPayable is what is still owed on a posted step GRN: the bill's open
// balance (invoice total less any debit note, less payments) plus a vendor
// credit note's extra not yet paid.
func grnNetPayable(tenantID string, data map[string]interface{}) float64 {
	billID := strField(data, "vendor_bill_id")
	if billID == "" {
		return 0
	}
	bill, status, err := fetchDocData(tenantID, "VendorInvoice", billID)
	if err != nil {
		return 0
	}
	open := 0.0
	if status != "Paid" {
		open = vendorInvoiceOpenAmount(bill)
	}
	extra := numFromInterface(data["vcn_total"])
	for _, p := range grnPayments(data) {
		extra -= p.ExtraPart
	}
	return round2(open + math.Max(0, extra))
}

// GRNPayInput is Pay now: amount (blank = all that is due), mode, reference
// (UTR / cheque no; any unique text for cash), date, and whether to withhold
// TDS at the vendor's section.
type GRNPayInput struct {
	Amount    float64 `json:"amount"`
	Mode      string  `json:"mode"`
	Reference string  `json:"reference"`
	Date      string  `json:"date"`
	WithTDS   bool    `json:"with_tds"`
}

var grnPayModes = map[string]string{"cash": "Cash", "bank": "Bank", "upi": "UPI", "cheque": "Cheque"}

// PayGRNStep pays the vendor from a posted step GRN (D9): the bill first,
// through the existing part-payment engine, then any credit-note extra.
// Payments post to Cash / Bank (1100), as every vendor payment does.
func PayGRNStep(tenantID, userID, grnID string, in GRNPayInput) (*GRNStepView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Stage 135.21: a GRN's payments go one at a time, under its row lock -
	// what is due is checked, paid and recorded on the GRN while it is held.
	// Read outside it, two payments at once both passed the check against
	// the same balance (a credit-note extra could be paid twice), and the
	// later save dropped the earlier payment from the GRN. The bill part pays
	// in the bill's own transaction, keyed by the reference, so a retry after
	// a failure here does not pay it twice.
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var dataStr string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'GRN' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema), grnID).Scan(&dataStr); err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	if !IsGRNStepFlow(data) || strField(data, "grn_step") != GRNStepPosted {
		return nil, &ValidationError{Code: "GOODSR-0944", Message: "post the goods receipt before paying the vendor"}
	}
	billID := strField(data, "vendor_bill_id")
	if billID == "" {
		return nil, &ValidationError{Code: "GOODSR-0950", Message: "this receipt has no vendor bill yet - " + firstNonEmpty(strField(data, "posting_warning"), "record it under Supplier bills")}
	}
	mode := grnPayModes[strings.ToLower(strings.TrimSpace(in.Mode))]
	if mode == "" {
		return nil, &ValidationError{Code: "GOODSR-0950", SubFor: "Mode", Message: "choose how it is paid: Cash, Bank, UPI or Cheque"}
	}
	ref := strings.TrimSpace(in.Reference)
	if ref == "" {
		return nil, &ValidationError{Code: "GOODSR-0950", SubFor: "Reference", Message: "enter the UTR, cheque number or a reference for this payment"}
	}
	if !settlementReferencePattern.MatchString(ref) {
		return nil, &ValidationError{Code: "FIN-0520", SubFor: "Reference", Message: fmt.Sprintf("reference %q is not valid: use up to 64 letters, digits, '.', '_', '/' or '-'", ref)}
	}
	for _, p := range grnPayments(data) {
		if p.Reference == ref {
			tx.Rollback()
			return GetGRNStepView(tenantID, grnID) // a retry of a payment already made
		}
	}
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = BusinessToday()
	}
	net := grnNetPayable(tenantID, data)
	if net <= 0.004 {
		return nil, &ValidationError{Code: "GOODSR-0950", Message: "nothing is due on this receipt"}
	}
	amount := in.Amount
	if amount <= 0 {
		amount = net
	}
	if math.IsNaN(amount) || amount > net+0.005 {
		return nil, &ValidationError{Code: "GOODSR-0950", SubFor: "Amount", Message: fmt.Sprintf("%.2f is more than the %.2f due", amount, net)}
	}
	bill, billStatus, err := fetchDocData(tenantID, "VendorInvoice", billID)
	if err != nil {
		return nil, err
	}
	billOpen := 0.0
	if billStatus != "Paid" {
		billOpen = vendorInvoiceOpenAmount(bill)
	}
	pay := GRNPayment{Reference: ref, Mode: mode, Amount: round2(amount), Date: date, By: userID, RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	pay.BillPart = round2(math.Min(amount, billOpen))
	pay.ExtraPart = round2(amount - pay.BillPart)
	if pay.BillPart > 0 {
		if in.WithTDS {
			section := ""
			if vd, _, err := fetchDocData(tenantID, "Vendor", strField(data, "vendor")); err == nil {
				section = strField(vd, "tds_section")
			}
			if section == "" {
				return nil, &ValidationError{Code: "GOODSR-0950", SubFor: "TDS", Message: "this vendor has no TDS section - set it on the vendor, or pay without TDS"}
			}
			if math.Abs(pay.BillPart-billOpen) > 0.005 {
				return nil, &ValidationError{Code: "FIN-0521", Message: "TDS is withheld on the whole bill - pay all that is due on the bill, or pay without TDS"}
			}
			tds, err := payStepBillWithTDS(tenantID, userID, billID, section, ref, date)
			if err != nil {
				return nil, err
			}
			pay.TDS = tds
		} else if _, err := PayVendorInvoiceAmount(tenantID, billID, userID, pay.BillPart, ref, SettlementOptions{SettlementDate: date}); err != nil {
			return nil, err
		}
	}
	if pay.ExtraPart > 0 {
		p := RupeesToPaise(pay.ExtraPart)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "GRN", grnID, map[string]int64{"2100": p}, map[string]int64{"1100": p}, date,
			fmt.Sprintf("GRN:%s:PAY:%s", grnID, ref)); err != nil {
			return nil, fmt.Errorf("the credit-note part of the payment (%.2f) could not be booked: %v", pay.ExtraPart, err)
		}
	}
	payments := append(grnPayments(data), pay)
	paid := 0.0
	for _, p := range payments {
		paid += p.Amount + p.TDS
	}
	encoded, _ := json.Marshal(payments)
	data["grn_payments"] = string(encoded)
	data["paid_amount"] = round2(paid)
	remaining := grnNetPayable(tenantID, data)
	data["payment_status"] = "Part paid"
	if remaining <= 0.004 {
		data["payment_status"] = "Paid"
	}
	patch := map[string]interface{}{"grn_payments": data["grn_payments"], "paid_amount": data["paid_amount"], "payment_status": data["payment_status"]}
	b, _ := json.Marshal(patch)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'GRN' AND id = $2`, schema), string(b), grnID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "GRN_VENDOR_PAYMENT", "SUCCESS", fmt.Sprintf("GRN %s: paid %.2f by %s (%s), TDS %.2f", grnID, pay.Amount, mode, ref, pay.TDS))
	return GetGRNStepView(tenantID, grnID)
}

// payStepBillWithTDS is PayVendorInvoiceWithTDS on a bill a debit note has
// already reduced: TDS on the open goods value, the rest paid in cash/bank.
func payStepBillWithTDS(tenantID, userID, billID, section, ref, date string) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'VendorInvoice' AND id = $1 FOR UPDATE`, schema), billID).Scan(&dataStr, &status); err != nil {
		return 0, err
	}
	if status != "Matched" {
		return 0, &ValidationError{Code: "VENDOR-0092", Message: fmt.Sprintf("vendor bill %s is %s, not Matched", billID, status)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	entries, err := readSettlementEntries(data, "payments")
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.Kind != "Debit note" {
			return 0, &ValidationError{Code: "FIN-0521", Message: "this bill is already part-paid; TDS is withheld on a whole bill, so pay the rest without TDS"}
		}
	}
	position := documentFXPosition(tenantID, data, "invoice_amount")
	if position.Foreign {
		return 0, &ValidationError{Code: "FIN-0841", Message: "TDS cannot be worked out on a bill in a foreign currency"}
	}
	open, total := vendorOpenLegs(data, position)
	if total <= 0 {
		return 0, nothingOutstandingError("vendor invoice", billID)
	}
	tdsF, _, err := CalculateTDS(tenantID, section, PaiseToRupees(open["2100"]))
	if err != nil {
		return 0, err
	}
	tdsP := RupeesToPaise(tdsF)
	key := fmt.Sprintf("VendorInvoice:%s:PAY_TDS:%s", billID, ref)
	if err := PostDoubleEntryTx(tx, tenantID, schema, "VendorInvoice", billID, open, map[string]int64{"1100": total - tdsP, "2300": tdsP}, date, key); err != nil {
		return 0, fmt.Errorf("GL posting failed, bill not paid: %v", err)
	}
	noteVendorPayment(data, entries, settlementKindPaymentTDS, ref, key, userID, date, open, position, 0)
	data["status"] = "Paid"
	data["tds_section"] = section
	data["tds_amount"] = tdsF
	data["net_paid"] = PaiseToRupees(total - tdsP)
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'VendorInvoice' AND id = $2`, schema), b, billID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	LogAuditEvent(tenantID, userID, "PAY_VENDOR_INVOICE_TDS", "SUCCESS", fmt.Sprintf("Paid vendor bill %s with TDS %.2f (section %s)", billID, tdsF, section))
	return tdsF, nil
}

// --- Correct after posting (94.8, D8) ----------------------------------------------

type GRNCorrectionLine struct {
	LineNo   int      `json:"line_no"`
	GRNQty   *float64 `json:"grn_qty"`
	GRNPrice *float64 `json:"grn_price"`
	MRP      *float64 `json:"mrp"`
}

type GRNCorrectionInput struct {
	Lines  []GRNCorrectionLine `json:"lines"`
	Reason string              `json:"reason"`
	Apply  bool                `json:"apply"`
}

// GRNCorrectionEffect is what a correction does, shown before it is applied.
type GRNCorrectionEffect struct {
	Lines      []GRNCorrectionEffectLine `json:"lines"`
	StockDelta float64                   `json:"stock_delta"`
	ValueDelta float64                   `json:"value_delta"` // stock value, before GST
	TotalDelta float64                   `json:"total_delta"` // what we owe, incl. GST
	NoteKind   string                    `json:"note_kind,omitempty"`
	NoteAmount float64                   `json:"note_amount"`
	Refusals   []string                  `json:"refusals,omitempty"`
	Applied    bool                      `json:"applied"`
	NoteID     string                    `json:"note_id,omitempty"`
}

type GRNCorrectionEffectLine struct {
	LineNo     int     `json:"line_no"`
	SKU        string  `json:"sku"`
	Field      string  `json:"field"`
	Old        float64 `json:"old"`
	New        float64 `json:"new"`
	StockDelta float64 `json:"stock_delta,omitempty"`
	ValueDelta float64 `json:"value_delta,omitempty"`
}

// CorrectGRNStep previews (Apply false) or applies a correction to a posted
// step GRN: only the difference is reversed and re-posted - stock, value and
// an adjusting note. It refuses only what cannot be reversed: stock already
// sold (a Purchase Return is the way), or a bill paid beyond the new amount.
func CorrectGRNStep(tenantID, userID, storeCode, grnID string, in GRNCorrectionInput) (*GRNCorrectionEffect, error) {
	g, err := loadStepGRN(tenantID, grnID)
	if err != nil {
		return nil, err
	}
	defer g.tx.Rollback()
	if g.step() != GRNStepPosted {
		return nil, &ValidationError{Code: "GOODSR-0944", Message: "this receipt is not posted yet - go back to the step and change it there"}
	}
	if in.Apply && strings.TrimSpace(in.Reason) == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Reason", Message: "say why the posted receipt is being corrected"}
	}
	location := strField(g.data, "location")
	eff := &GRNCorrectionEffect{}
	_, oldGST := grnOurTotals(g.lines)
	oldTotal := numFromInterface(g.data["our_total"])
	type stockMove struct {
		sku, batch string
		qty        float64
		pricePaise int64
	}
	var moves []stockMove
	var valueDelta float64
	var mrpChanges []map[string]interface{}
	for _, c := range in.Lines {
		l := g.line(c.LineNo)
		if l == nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Lines", Message: fmt.Sprintf("line %d is not on this receipt", c.LineNo)}
		}
		sku := strField(l, "sku")
		oldQty, oldPrice, oldAcc := grnGRNQty(l), grnGRNPrice(l), grnStepAccepted(l)
		newQty, newPrice := oldQty, oldPrice
		for _, v := range []*float64{c.GRNQty, c.GRNPrice, c.MRP} {
			if v != nil && (*v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return nil, &ValidationError{Code: "GOODSR-0952", Message: fmt.Sprintf("line %d: values must be zero or more", c.LineNo)}
			}
		}
		if c.GRNQty != nil {
			newQty = round2(*c.GRNQty)
		}
		if c.GRNPrice != nil {
			newPrice = round2(*c.GRNPrice)
		}
		newAcc := math.Max(0, newQty-numFromInterface(l["rejected_qty"])-numFromInterface(l["damaged_qty"]))
		dq := newAcc - oldAcc
		dv := round2(newAcc*newPrice - oldAcc*oldPrice)
		avail := grnAvailableQty(g.tx, g.schema, sku, location)
		if dq < -1e-9 && avail+1e-9 < -dq {
			eff.Refusals = append(eff.Refusals, fmt.Sprintf("line %d (%s): only %v of the %v to take off are still in stock - the rest is sold; raise a Purchase Return for what is left", c.LineNo, sku, avail, -dq))
		}
		if math.Abs(newPrice-oldPrice) >= 0.005 && avail+1e-9 < oldAcc+math.Min(dq, 0) {
			eff.Refusals = append(eff.Refusals, fmt.Sprintf("line %d (%s): some of it is sold at the old cost, so its price cannot be changed here - settle the difference with a debit or credit note", c.LineNo, sku))
		}
		if math.Abs(newQty-oldQty) > 1e-9 {
			eff.Lines = append(eff.Lines, GRNCorrectionEffectLine{LineNo: c.LineNo, SKU: sku, Field: "grn_qty", Old: oldQty, New: newQty, StockDelta: dq, ValueDelta: round2(dq * newPrice)})
		}
		if math.Abs(newPrice-oldPrice) >= 0.005 {
			eff.Lines = append(eff.Lines, GRNCorrectionEffectLine{LineNo: c.LineNo, SKU: sku, Field: "grn_price", Old: oldPrice, New: newPrice, ValueDelta: round2(oldAcc * (newPrice - oldPrice))})
		}
		if c.MRP != nil && math.Abs(WholeRupeeMRP(*c.MRP)-numFromInterface(l["mrp"])) >= 0.005 {
			newMRP := WholeRupeeMRP(*c.MRP) // Stage 94.15
			eff.Lines = append(eff.Lines, GRNCorrectionEffectLine{LineNo: c.LineNo, SKU: sku, Field: "mrp", Old: numFromInterface(l["mrp"]), New: newMRP})
			mrpChanges = append(mrpChanges, map[string]interface{}{"sku": sku, "batch": strField(l, "batch_no"), "mrp": newMRP})
			l["mrp"] = newMRP
		}
		eff.StockDelta += dq
		valueDelta += dv
		if math.Abs(dq) > 1e-9 {
			moves = append(moves, stockMove{sku: sku, batch: strField(l, "batch_no"), qty: dq, pricePaise: RupeesToPaise(newPrice)})
		}
		l["grn_qty"] = newQty
		l["grn_price"] = newPrice
		l["qty"] = newQty
		l["accepted_qty"] = newAcc
	}
	eff.ValueDelta = round2(valueDelta)
	grnStampTotals(g.data, g.lines)
	newTotal := numFromInterface(g.data["our_total"])
	_, newGST := grnOurTotals(g.lines)
	eff.TotalDelta = round2(newTotal - oldTotal)
	if eff.TotalDelta < -0.004 {
		eff.NoteKind, eff.NoteAmount = "DebitNote", -eff.TotalDelta
	} else if eff.TotalDelta > 0.004 {
		eff.NoteKind, eff.NoteAmount = "VendorCreditNote", eff.TotalDelta
	}
	paid := numFromInterface(g.data["paid_amount"])
	if newTotal+0.005 < paid {
		eff.Refusals = append(eff.Refusals, fmt.Sprintf("%.2f is already paid, more than the corrected total %.2f - recover the difference with a debit note to the vendor instead", paid, newTotal))
	}
	if !in.Apply || len(eff.Refusals) > 0 || len(eff.Lines) == 0 {
		return eff, nil
	}

	// Apply: the GRN first, so a failure below leaves a logged, visible state.
	n := len(grnCorrections(g.data)) + 1
	corr := map[string]interface{}{"n": n, "reason": strings.TrimSpace(in.Reason), "by": userID, "at": time.Now().UTC().Format(time.RFC3339),
		"value_delta": eff.ValueDelta, "total_delta": eff.TotalDelta}
	g.data["corrections"] = appendJSONList(strField(g.data, "corrections"), corr)
	if eff.NoteKind == "VendorCreditNote" {
		g.data["vcn_total"] = round2(numFromInterface(g.data["vcn_total"]) + eff.NoteAmount)
	}
	if err := g.save(tenantID, userID, "Correct", in.Reason); err != nil {
		return nil, err
	}
	voucher := fmt.Sprintf("%s-C%d", grnID, n)
	for _, m := range moves {
		line := map[string]interface{}{"sku": m.sku, "qty": m.qty}
		if m.batch != "" {
			line["batch_no"] = m.batch
		}
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{line}, false, "GRNCorrection", voucher, userID); err != nil {
			return nil, fmt.Errorf("the correction was saved but its stock change for %s could not post (%v) - adjust the stock by hand", m.sku, err)
		}
		if m.qty > 0 {
			_ = RecordStockReceiptCost(tenantID, m.sku, m.qty, m.pricePaise)
		}
	}
	if p := RupeesToPaise(eff.ValueDelta); p != 0 {
		debits, credits := map[string]int64{"1200": p}, map[string]int64{"2100": p}
		if p < 0 {
			debits, credits = map[string]int64{"2100": -p}, map[string]int64{"1200": -p}
		}
		if err := PostDoubleEntry(tenantID, "GRN", grnID, debits, credits, "", fmt.Sprintf("GRN:%s:CORR:%d", grnID, n)); err != nil {
			return nil, fmt.Errorf("the correction was saved but its stock value change could not be booked: %v", err)
		}
	}
	for _, m := range mrpChanges {
		sku, batch, mrp := m["sku"].(string), m["batch"].(string), m["mrp"].(float64)
		setItemLatestMRP(tenantID, sku, mrp)
		if batch != "" {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, '{mrp}', to_jsonb($1::numeric)), updated_at = CURRENT_TIMESTAMP
				WHERE doctype = 'Batch' AND data->>'item' = $2 AND data->>'batch_no' = $3`, g.schema), mrp, sku, batch)
		}
	}
	if eff.NoteKind != "" {
		data, _, _ := fetchDocData(tenantID, "GRN", grnID)
		noteLines := []grnNoteLine{}
		for _, el := range eff.Lines {
			if el.Field == "mrp" {
				continue
			}
			noteLines = append(noteLines, grnNoteLine{LineNo: el.LineNo, SKU: el.SKU, Source: "correction of " + strings.ReplaceAll(el.Field, "_", " "), Taxable: -el.ValueDelta})
		}
		gstDelta := round2(newGST - oldGST)
		series := map[string]string{"DebitNote": "DN", "VendorCreditNote": "VCN"}[eff.NoteKind]
		noteID, err := raiseCorrectionNote(tenantID, userID, storeCode, grnID, data, eff.NoteKind, series, n, noteLines, math.Abs(eff.ValueDelta), math.Abs(gstDelta), eff.NoteAmount, in.Reason)
		if err != nil {
			return nil, err
		}
		eff.NoteID = noteID
		if billID := strField(data, "vendor_bill_id"); billID != "" {
			if eff.NoteKind == "DebitNote" {
				if err := applyDebitNoteToBill(tenantID, userID, billID, noteID, eff.NoteAmount, newGST); err != nil {
					return nil, err
				}
			} else if err := postVCNGSTLeg(tenantID, noteID, gstDelta); err != nil {
				return nil, err
			}
		}
	}
	eff.Applied = true
	LogAuditEvent(tenantID, userID, "GRN_CORRECTED", "SUCCESS", fmt.Sprintf("GRN %s correction %d: stock %+.2f, value %+.2f, total %+.2f (%s)", grnID, n, eff.StockDelta, eff.ValueDelta, eff.TotalDelta, in.Reason))
	SyncGRNLineRows(tenantID, grnID)
	return eff, nil
}

func grnAvailableQty(q queryRower, schema, sku, location string) float64 {
	var v sql.NullFloat64
	_ = q.QueryRow(fmt.Sprintf(`SELECT SUM(available)::float8 FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location).Scan(&v)
	return v.Float64
}

func grnCorrections(data map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	if raw := strField(data, "corrections"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func appendJSONList(raw string, item map[string]interface{}) string {
	var list []map[string]interface{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &list)
	}
	list = append(list, item)
	b, _ := json.Marshal(list)
	return string(b)
}

// raiseCorrectionNote raises the adjusting note a correction makes, keyed to
// the correction so a retry never raises two.
func raiseCorrectionNote(tenantID, userID, storeCode, grnID string, grn map[string]interface{}, doctype, series string, n int, lines []grnNoteLine, taxable, gst, total float64, reason string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var existing string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND data->>'source_doc_id' = $2 AND data->>'correction' = $3 LIMIT 1`, schema),
		doctype, grnID, strconv.Itoa(n)).Scan(&existing)
	if existing != "" {
		return existing, nil
	}
	id, err := GenerateSequence(tenantID, series, numberingStore(tenantID, storeCode, strField(grn, "location")), documentFinancialYear(time.Now()))
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(lines)
	note := map[string]interface{}{
		"code": id, "note_number": id, "vendor_id": strField(grn, "vendor"), "reference_po": strField(grn, "po_id"),
		"grn_id": grnID, "vendor_invoice_no": strField(grn, "vendor_invoice_no"),
		"amount": round2(taxable), "gst_amount": round2(gst), "total_with_gst": round2(total),
		"reason":     fmt.Sprintf("Correction %d of goods receipt %s: %s", n, firstNonEmpty(strField(grn, "code"), grnID), strings.TrimSpace(reason)),
		"note_lines": string(encoded), "status": "Posted", "basis": "grn_correction", "correction": strconv.Itoa(n),
		"source_doctype": "GRN", "source_doc_id": grnID,
	}
	b, _ := json.Marshal(note)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Posted', $4)`, schema), id, doctype, b, userID); err != nil {
		return "", err
	}
	return id, nil
}

// --- purchase returns of a step GRN's set-aside goods -----------------------------

// stepGRNSettlesSetAside: a step GRN's rejected and damaged units were left
// off what we pay - its invoice-vs-GRN note already claimed them - so a
// Purchase Return sending them back raises no second note for them.
func stepGRNSettlesSetAside(tenantID, grnID string) bool {
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	return err == nil && IsGRNStepFlow(data)
}
