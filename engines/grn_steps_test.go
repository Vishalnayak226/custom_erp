package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

// TestStage94GRNSteps walks one receipt through every step - Receive,
// Verify (excess Return, the invoice-total check), Price check into child
// GRNs, MRP and posting per child, the invoice-vs-GRN debit note applied to
// the vendor bill, Correct after posting, and Pay now - checking the stock,
// the GL, the batch MRP and the line rows at each step. A second receipt
// covers Keep (the approval engine) and the quick path.
func TestStage94GRNSteps(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if !grnLineTablesReady(db.DB, schema) {
		t.Skip("needs migrations_stage94_grn_steps.sql - run erp-server -migrate on this database")
	}
	const (
		skuA, skuB, skuD = "TEST94-ITEM-A", "TEST94-ITEM-B", "TEST94-ITEM-D"
		vend             = "TEST94-VEND"
		po1, po2         = "TEST94-PO-1", "TEST94-PO-2"
		oldGRN           = "TEST94-OLD-GRN"
		location         = "TEST94-LOC"
	)
	preserveSeries(t, schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(rows ...map[string]interface{}) string {
		raw, _ := json.Marshal(rows)
		return string(raw)
	}
	var grnIDs []string
	cleanup := func() {
		ids := append([]string{skuA, skuB, skuD, vend, po1, po2, oldGRN}, grnIDs...)
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype IN ('StockLedgerEntry') AND (data->>'voucher_id' = $1 OR data->>'voucher_id' LIKE $1 || '-C%')", id)
			db.DB.Exec("DELETE FROM "+schema+".grn_line WHERE grn_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".grn_line_change WHERE grn_id = $1", id)
		}
		for _, id := range grnIDs {
			rows, _ := db.DB.Query("SELECT id FROM "+schema+".documents WHERE doctype IN ('DebitNote','VendorCreditNote','VendorInvoice','GRNExcessApproval') AND (data->>'source_doc_id' = $1 OR data->>'grn_id' = $1)", id)
			var linked []string
			for rows != nil && rows.Next() {
				var x string
				rows.Scan(&x)
				linked = append(linked, x)
			}
			if rows != nil {
				rows.Close()
			}
			for _, x := range linked {
				db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", x)
				db.DB.Exec("DELETE FROM "+schema+".approval_log WHERE document_id = $1", x)
				db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", x)
			}
		}
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		for _, sku := range []string{skuA, skuB, skuD} {
			db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", sku)
			db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype IN ('Batch','StockLedgerEntry') AND (data->>'item' = $1 OR data->>'item_id' = $1)", sku)
		}
	}
	cleanup()
	defer cleanup()

	insert(skuA, "Item", "Active", map[string]interface{}{"code": skuA, "name": "Stage 94 Kurta", "hsn_code": "6204", "gst_rate": 5.0, "mrp": 199.0})
	insert(skuB, "Item", "Active", map[string]interface{}{"code": skuB, "name": "Stage 94 Dupatta", "hsn_code": "6204", "gst_rate": 5.0})
	insert(skuD, "Item", "Active", map[string]interface{}{"code": skuD, "name": "Stage 94 Scarf", "hsn_code": "6204", "gst_rate": 5.0, "mrp": 99.0})
	insert(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": "Stage 94 Vendor"})
	insert(po1, "PurchaseOrder", "Approved", map[string]interface{}{"code": po1, "vendor": vend, "gst_mode": GSTModeExclusive, "total_amount": 2200, "grand_total": 2310,
		"items": lines(map[string]interface{}{"sku": skuA, "qty": 10, "rate": 100.0}, map[string]interface{}{"sku": skuB, "qty": 5, "rate": 200.0}, map[string]interface{}{"sku": skuD, "qty": 4, "rate": 50.0})})
	insert(po2, "PurchaseOrder", "Approved", map[string]interface{}{"code": po2, "vendor": vend, "gst_mode": GSTModeExclusive, "total_amount": 500, "grand_total": 525,
		"items": lines(map[string]interface{}{"sku": skuA, "qty": 5, "rate": 100.0})})
	// The previous purchases: A at 100, D at 50 (a receipt from before Stage 94).
	insert(oldGRN, "GRN", "Approved", map[string]interface{}{"code": oldGRN, "vendor": vend, "location": location,
		"received_items": lines(map[string]interface{}{"sku": skuA, "qty": 1, "invoice_rate": 100.0}, map[string]interface{}{"sku": skuD, "qty": 1, "invoice_rate": 50.0})})

	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}
	glSum := func(docID, account string) (dr, cr int64) {
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", docID, account).Scan(&dr, &cr)
		return
	}
	stock := func(sku, column string) float64 {
		var v float64
		db.DB.QueryRow("SELECT COALESCE(SUM("+column+"),0)::float8 FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&v)
		return v
	}
	receive := func(id string, payload map[string]interface{}) {
		t.Helper()
		if err := PrepareGRNPricing(tenantID, true, payload); err != nil {
			t.Fatalf("PrepareGRNPricing: %v", err)
		}
		if err := PrepareGRNSteps(tenantID, "system", true, payload); err != nil {
			t.Fatalf("PrepareGRNSteps: %v", err)
		}
		if err := PrepareGRNBatchNumbers(tenantID, true, payload); err != nil {
			t.Fatalf("PrepareGRNBatchNumbers: %v", err)
		}
		if err := validateGRNRules(tenantID, "", payload); err != nil {
			t.Fatalf("validateGRNRules must let a receipt in steps take the excess: %v", err)
		}
		payload["code"] = id
		grnIDs = append(grnIDs, id)
		insert(id, "GRN", strField(payload, "status"), payload)
		SyncGRNLineRows(tenantID, id)
	}

	// ① Receive. A arrives 11 of 10 ordered; B (never bought) is billed at
	// 210 against the PO's 200; D's price went up from 50 to 55.
	// Invoice lines: 1100 + 1050 + 220 = 2370 + 5% = 2488.50; typed 2489.
	const grnID = "TEST94-GRN-1"
	payload := map[string]interface{}{
		"flow": "steps", "po_ids": po1, "location": location,
		"vendor_invoice_no": "INV-94-1", "vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": 2489,
		"received_items": lines(
			map[string]interface{}{"sku": skuA, "qty": 11, "invoice_rate": 100},
			map[string]interface{}{"sku": skuB, "qty": 5, "invoice_rate": 210},
			map[string]interface{}{"sku": skuD, "qty": 4, "invoice_rate": 55},
		),
	}
	t.Run("receive needs the invoice number, date and total", func(t *testing.T) {
		p := map[string]interface{}{"flow": "steps", "po_id": po1, "received_items": lines(map[string]interface{}{"sku": skuA, "qty": 1})}
		if got := code(PrepareGRNSteps(tenantID, "system", true, p)); got != "GOODSR-0940" {
			t.Fatalf("want GOODSR-0940, got %q", got)
		}
	})
	receive(grnID, payload)
	t.Run("a received GRN posts nothing", func(t *testing.T) {
		v, err := GetGRNStepView(tenantID, grnID)
		if err != nil {
			t.Fatalf("view: %v", err)
		}
		if v.Step != GRNStepReceived || v.Status != "Pending" || len(v.Lines) != 3 || v.Lines[0].LineNo != 1 {
			t.Fatalf("step %s status %s lines %d", v.Step, v.Status, len(v.Lines))
		}
		if v.InvoiceLinesTotal != 2488.5 || v.Unexplained != 0.5 {
			t.Fatalf("invoice lines %v unexplained %v", v.InvoiceLinesTotal, v.Unexplained)
		}
		if stock(skuA, "on_hand") != 0 {
			t.Fatalf("stock moved at Receive")
		}
		var rows int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".grn_line WHERE grn_id = $1 AND step = 'Received'", grnID).Scan(&rows)
		if rows != 3 {
			t.Fatalf("grn_line rows = %d, want 3", rows)
		}
		if v.QuickPath {
			t.Fatalf("this receipt does not match everywhere, quick path must be off")
		}
	})
	t.Run("the same vendor invoice cannot be received twice", func(t *testing.T) {
		dup := map[string]interface{}{"flow": "steps", "po_id": po1, "vendor": vend, "vendor_invoice_no": " inv-94-1 ", "vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": 10,
			"received_items": lines(map[string]interface{}{"sku": skuA, "qty": 1})}
		if got := code(PrepareGRNSteps(tenantID, "system", true, dup)); got != "GOODSR-0941" {
			t.Fatalf("want GOODSR-0941, got %q", got)
		}
	})

	// ② Verify.
	price := func(v float64) *float64 { return &v }
	t.Run("an unexplained invoice total is refused, naming the line", func(t *testing.T) {
		if _, err := SaveGRNReceive(tenantID, "system", grnID, GRNReceiveEdit{InvoiceAmount: price(2594)}); err != nil {
			t.Fatalf("edit total: %v", err)
		}
		_, err := VerifyGRNStep(tenantID, "system", "Super Admin", grnID, GRNVerifyInput{Lines: []GRNVerifyLine{{LineNo: 1, ExcessAction: GRNExcessReturn}}})
		if code(err) != "GOODSR-0942" || !strings.Contains(err.Error(), "unit") {
			t.Fatalf("want GOODSR-0942 naming a line off by whole units, got %v", err)
		}
		if _, err := SaveGRNReceive(tenantID, "system", grnID, GRNReceiveEdit{InvoiceAmount: price(2489), Reason: "typo"}); err != nil {
			t.Fatalf("edit total back: %v", err)
		}
	})
	t.Run("goods beyond the PO need Keep or Return", func(t *testing.T) {
		_, err := VerifyGRNStep(tenantID, "system", "Super Admin", grnID, GRNVerifyInput{})
		if code(err) != "GOODSR-0943" || !strings.Contains(err.Error(), skuA) {
			t.Fatalf("want GOODSR-0943 for %s, got %v", skuA, err)
		}
	})
	v, err := VerifyGRNStep(tenantID, "system", "Super Admin", grnID, GRNVerifyInput{Lines: []GRNVerifyLine{
		{LineNo: 1, ExcessAction: GRNExcessReturn},
		{LineNo: 2, GRNPrice: price(200)},
	}, Reason: "called the vendor"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Run("verify returns the excess and works out the note", func(t *testing.T) {
		a := v.Lines[0]
		if a.ExcessQty != 1 || a.ExcessReturned != 1 || a.AcceptedQty != 10 || a.RejectedQty != 0 {
			t.Fatalf("line A excess %v returned %v accepted %v own rejected %v", a.ExcessQty, a.ExcessReturned, a.AcceptedQty, a.RejectedQty)
		}
		// Ours: 10x100 + 5x200 + 4x55 = 2220 + 111 GST = 2331; invoice 2489.
		if v.OurTotal != 2331 || v.Difference != 158 || v.NoteKind != "DebitNote" || v.NoteAmount != 158 {
			t.Fatalf("our %v difference %v note %s %v", v.OurTotal, v.Difference, v.NoteKind, v.NoteAmount)
		}
		var changes int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".grn_line_change WHERE grn_id = $1 AND field = 'grn_price' AND new_value = '200'", grnID).Scan(&changes)
		if changes != 1 {
			t.Fatalf("the GRN price change must be logged once, got %d", changes)
		}
	})

	// ③ Price check: A same as before (100), B new, D changed (50 -> 55).
	v, err = PriceCheckGRNStep(tenantID, "system", grnID, GRNPriceCheckInput{Names: map[string]string{GRNGroupChanged: "Repriced"}})
	if err != nil {
		t.Fatalf("price check: %v", err)
	}
	t.Run("lines sort into New, Same and Changed children", func(t *testing.T) {
		got := map[string]string{}
		for _, l := range v.Lines {
			got[l.SKU] = l.Child
		}
		if got[skuA] != "B" || got[skuB] != "A" || got[skuD] != "C" {
			t.Fatalf("children %v", got)
		}
		if len(v.Children) != 3 || v.Children[2].Name != "Repriced" || v.Lines[2].PrevPrice == nil || *v.Lines[2].PrevPrice != 50 || *v.Lines[2].PriceChangePct != 10 {
			t.Fatalf("children %+v / D prev %v", v.Children, v.Lines[2].PrevPrice)
		}
		moved, err := PriceCheckGRNStep(tenantID, "system", grnID, GRNPriceCheckInput{Moves: map[string]string{skuD: GRNGroupSame}})
		if err != nil || moved.Lines[2].Child != "B" {
			t.Fatalf("move D to Same: %v %+v", err, moved.Lines[2])
		}
		back, err := PriceCheckGRNStep(tenantID, "system", grnID, GRNPriceCheckInput{Regroup: true})
		if err != nil || back.Lines[2].Child != "C" {
			t.Fatalf("regroup: %v %+v", err, back.Lines[2])
		}
	})

	// ④ MRP and posting, one child at a time.
	v, err = PostGRNChildStep(tenantID, "system", "", grnID, "B", GRNPostInput{})
	if err != nil {
		t.Fatalf("post child B: %v", err)
	}
	t.Run("a child posts on its own, at the GRN price, with the MRP on its batch", func(t *testing.T) {
		if stock(skuA, "available") != 10 || stock(skuA, "qc_hold") != 1 || stock(skuB, "on_hand") != 0 {
			t.Fatalf("A available %v qc %v, B on hand %v", stock(skuA, "available"), stock(skuA, "qc_hold"), stock(skuB, "on_hand"))
		}
		if dr, _ := glSum(grnID, "1200"); dr != 100000 {
			t.Fatalf("Dr 1200 after child B = %d, want 100000", dr)
		}
		b, _ := GetBatch(tenantID, skuA, v.Lines[0].BatchNo)
		if v.Lines[0].BatchNo == "" || b == nil || b.MRP != 199 {
			t.Fatalf("batch %q MRP %+v", v.Lines[0].BatchNo, b)
		}
		if v.Step != GRNStepPriced || v.Status != "Pending" {
			t.Fatalf("one child posted must leave the GRN open, got %s %s", v.Step, v.Status)
		}
	})
	t.Run("a line needs its MRP", func(t *testing.T) {
		if _, err := PostGRNChildStep(tenantID, "system", "", grnID, "A", GRNPostInput{}); code(err) != "GOODSR-0945" {
			t.Fatalf("want GOODSR-0945, got %v", err)
		}
	})
	if _, err = PostGRNChildStep(tenantID, "system", "", grnID, "A", GRNPostInput{MRP: map[string]float64{"2": 449}}); err != nil {
		t.Fatalf("post child A: %v", err)
	}
	v, err = PostGRNChildStep(tenantID, "system", "", grnID, "C", GRNPostInput{MRP: map[string]float64{"3": 119}})
	if err != nil {
		t.Fatalf("post child C: %v", err)
	}
	grn, _, _ := fetchDocData(tenantID, "GRN", grnID)
	t.Run("the last child posts the GRN, raises the note and makes the bill", func(t *testing.T) {
		if v.Step != GRNStepPosted || v.Status != "Approved" || strField(grn, "posting_warning") != "" {
			t.Fatalf("step %s status %s warning %q", v.Step, v.Status, strField(grn, "posting_warning"))
		}
		if dr, _ := glSum(grnID, "1200"); dr != 222000 {
			t.Fatalf("Dr 1200 = %d, want 222000 (stock at the GRN price)", dr)
		}
		dn, st, err := fetchDocData(tenantID, "DebitNote", v.DebitNoteID)
		if err != nil || st != "Posted" || dn["total_with_gst"] != 158.0 || dn["amount"] != 150.5 || dn["gst_amount"] != 7.5 {
			t.Fatalf("debit note %s %v %v", st, dn, err)
		}
		if dr, cr := glSum(v.DebitNoteID, "2100"); dr != 0 || cr != 0 {
			t.Fatalf("the debit note is settled through the bill and books no GL, got %d/%d", dr, cr)
		}
		bill, st, _ := fetchDocData(tenantID, "VendorInvoice", v.VendorBillID)
		if st != "Matched" || bill["invoice_amount"] != 2489.0 || v.NetPayable != 2331 {
			t.Fatalf("bill %s amount %v net payable %v", st, bill["invoice_amount"], v.NetPayable)
		}
		if item, _, _ := fetchDocData(tenantID, "Item", skuB); item["mrp"] != 449.0 {
			t.Fatalf("item B MRP = %v, want the latest 449", item["mrp"])
		}
		if tab := GRNInboxTab(grn, "Approved"); tab != "pay" {
			t.Fatalf("a posted, unpaid GRN sits on Ready to pay, got %s", tab)
		}
	})

	// Correct after posting: D was really 52.
	t.Run("correct previews, refuses what cannot be reversed, then applies the difference", func(t *testing.T) {
		eff, err := CorrectGRNStep(tenantID, "system", "", grnID, GRNCorrectionInput{Lines: []GRNCorrectionLine{{LineNo: 3, GRNPrice: price(52)}}})
		if err != nil || eff.Applied || eff.ValueDelta != -12 || eff.NoteKind != "DebitNote" || eff.NoteAmount != 12.6 {
			t.Fatalf("preview %+v %v", eff, err)
		}
		over, _ := CorrectGRNStep(tenantID, "system", "", grnID, GRNCorrectionInput{Lines: []GRNCorrectionLine{{LineNo: 1, GRNQty: price(0)}}})
		if len(over.Refusals) != 0 {
			t.Fatalf("10 of A are in stock, taking them off is allowed: %v", over.Refusals)
		}
		eff, err = CorrectGRNStep(tenantID, "system", "", grnID, GRNCorrectionInput{Lines: []GRNCorrectionLine{{LineNo: 3, GRNPrice: price(52)}}, Reason: "vendor confirmed 52", Apply: true})
		if err != nil || !eff.Applied || eff.NoteID == "" {
			t.Fatalf("apply %+v %v", eff, err)
		}
		if dr, cr := glSum(grnID, "1200"); dr-cr != 222000-1200 {
			t.Fatalf("1200 net %d, want 220800", dr-cr)
		}
		v, _ = GetGRNStepView(tenantID, grnID)
		if v.NetPayable != 2318.4 || v.OurTotal != 2318.4 {
			t.Fatalf("after the correction we owe %v (our total %v), want 2318.40", v.NetPayable, v.OurTotal)
		}
	})

	// ⑤ Pay now: a part payment, then the rest.
	t.Run("pay part, then the rest; 2100 and 1500 clear to our figures", func(t *testing.T) {
		if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 5000, Mode: "UPI", Reference: "UTR94A"}); code(err) != "GOODSR-0950" {
			t.Fatalf("paying more than is due must be refused, got %v", err)
		}
		v, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 1000, Mode: "UPI", Reference: "UTR94A"})
		if err != nil || v.PaymentStatus != "Part paid" || v.NetPayable != 1318.4 {
			t.Fatalf("part payment %v %s %v", err, v.PaymentStatus, v.NetPayable)
		}
		again, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 1000, Mode: "UPI", Reference: "UTR94A"})
		if err != nil || len(again.Payments) != 1 {
			t.Fatalf("a retried reference must not pay twice: %v %d", err, len(again.Payments))
		}
		v, err = PayGRNStep(tenantID, "system", grnID, GRNPayInput{Mode: "Cash", Reference: "CASH94B"})
		if err != nil || v.PaymentStatus != "Paid" || v.NetPayable != 0 || v.VendorBillStatus != "Paid" {
			t.Fatalf("pay the rest %v %s %v %s", err, v.PaymentStatus, v.NetPayable, v.VendorBillStatus)
		}
		_, billCr := glSum(v.VendorBillID, "1100")
		_, grnCr := glSum(grnID, "2100")
		grnDr, _ := glSum(grnID, "2100")
		billDr2100, _ := glSum(v.VendorBillID, "2100")
		billDr1500, _ := glSum(v.VendorBillID, "1500")
		if billCr != 231840 || billDr1500 != 11040 || grnCr-grnDr-billDr2100 != 0 {
			t.Fatalf("paid %d, input GST %d, 2100 left %d (want 231840, 11040, 0)", billCr, billDr1500, grnCr-grnDr-billDr2100)
		}
	})

	// Keep: a second receipt, 6 of PO2's 5, kept beyond the PO.
	const grn2 = "TEST94-GRN-2"
	receive(grn2, map[string]interface{}{
		"flow": "steps", "po_ids": po2, "location": location,
		"vendor_invoice_no": "INV-94-2", "vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": 630,
		"received_items": lines(map[string]interface{}{"sku": skuA, "qty": 6, "invoice_rate": 100}),
	})
	t.Run("keep goes for approval and holds its child until approved", func(t *testing.T) {
		v, err := VerifyGRNStep(tenantID, "system", "Super Admin", grn2, GRNVerifyInput{Lines: []GRNVerifyLine{{LineNo: 1, ExcessAction: GRNExcessKeep}}, ExcessReason: "fast seller"})
		if err != nil || !v.ExcessPending || v.ExcessApprovalID == "" {
			t.Fatalf("keep %v pending %v id %q", err, v.ExcessPending, v.ExcessApprovalID)
		}
		if _, st, _ := fetchDocData(tenantID, "GRNExcessApproval", v.ExcessApprovalID); st != "Pending Approval" {
			t.Fatalf("excess approval is %s", st)
		}
		if _, err := PriceCheckGRNStep(tenantID, "system", grn2, GRNPriceCheckInput{}); err != nil {
			t.Fatalf("price check: %v", err)
		}
		if _, err := PostGRNChildStep(tenantID, "system", "", grn2, "B", GRNPostInput{}); code(err) != "GOODSR-0946" {
			t.Fatalf("want GOODSR-0946 while the approval is open, got %v", err)
		}
		if err := DecideApproval(tenantID, "GRNExcessApproval", v.ExcessApprovalID, "approver94", "Super Admin", "", "Approved", "ok"); err != nil {
			t.Fatalf("approve: %v", err)
		}
		v, err = GetGRNStepView(tenantID, grn2)
		if err != nil || v.ExcessPending || v.Lines[0].ExcessStatus != "Approved" {
			t.Fatalf("after approval %v %+v", err, v.Lines[0])
		}
		if v, err = PostGRNChildStep(tenantID, "system", "", grn2, "B", GRNPostInput{MRP: map[string]float64{"1": 219}}); err != nil || v.Step != GRNStepPosted {
			t.Fatalf("post after approval: %v", err)
		}
		if math.Abs(stock(skuA, "available")-16) > 1e-9 {
			t.Fatalf("A available %v, want 16 (10 + all 6 kept)", stock(skuA, "available"))
		}
	})

	// 94.6 (D2): A is now on the shelf at two MRPs - 10 at 199 (GRN 1) and
	// 6 at 219 (GRN 2).
	t.Run("the till sells at the MRP on the shelf and asks when there are two", func(t *testing.T) {
		if got := BatchMRPChoices(tenantID, skuA, location); len(got) != 2 || got[0] != 219 || got[1] != 199 {
			t.Fatalf("MRPs on the shelf = %v, want [219 199] (newest first)", got)
		}
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: location, Lines: []QuoteLineRequest{{Sku: skuA, Qty: 1}}})
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		if l := q.Lines[0]; len(l.MRPChoices) != 2 || l.MRP != 199 || l.UnitPrice != 199 {
			t.Fatalf("no choice yet: sells at the lowest MRP and asks, got %+v", l)
		}
		q, err = ResolvePOSQuote(tenantID, QuoteRequest{Location: location, Lines: []QuoteLineRequest{{Sku: skuA, Qty: 1, MRP: 219}}})
		if err != nil || q.Lines[0].UnitPrice != 219 || len(q.Lines[0].MRPChoices) != 0 {
			t.Fatalf("picked 219: %v %+v", err, q.Lines[0])
		}
		if q, _ = ResolvePOSQuote(tenantID, QuoteRequest{Location: location, Lines: []QuoteLineRequest{{Sku: skuA, Qty: 1, MRP: 500}}}); q.Lines[0].UnitPrice != 199 || len(q.Lines[0].MRPChoices) != 2 {
			t.Fatalf("an MRP not on the shelf is not taken on trust: %+v", q.Lines[0])
		}
		// The older stock sells first: once only 5 are left, they are GRN 2's.
		if _, err := PostInventoryLedger(tenantID, location, []interface{}{map[string]interface{}{"sku": skuA, "qty": -11.0}}, false); err != nil {
			t.Fatalf("sell 11: %v", err)
		}
		if got := BatchMRPChoices(tenantID, skuA, location); len(got) != 1 || got[0] != 219 {
			t.Fatalf("with 5 left, only the newest MRP is on the shelf, got %v", got)
		}
	})

	t.Run("the quick path runs every step when everything matches", func(t *testing.T) {
		const grn3 = "TEST94-GRN-3"
		db.DB.Exec("UPDATE "+schema+".documents SET status = 'Approved' WHERE id = $1", po2)
		// D again at 55 now matches its last price; MRP 119 is on the item.
		insert("TEST94-PO-3", "PurchaseOrder", "Approved", map[string]interface{}{"code": "TEST94-PO-3", "vendor": vend, "gst_mode": GSTModeExclusive, "total_amount": 110, "grand_total": 115.5,
			"items": lines(map[string]interface{}{"sku": skuD, "qty": 2, "rate": 52.0})})
		grnIDs = append(grnIDs, "TEST94-PO-3")
		receive(grn3, map[string]interface{}{
			"flow": "steps", "po_ids": "TEST94-PO-3", "location": location,
			"vendor_invoice_no": "INV-94-3", "vendor_invoice_date": "2026-10-18", "vendor_invoice_amount": 109.2,
			"received_items": lines(map[string]interface{}{"sku": skuD, "qty": 2, "invoice_rate": 52}),
		})
		v, err := GetGRNStepView(tenantID, grn3)
		if err != nil || !v.QuickPath {
			t.Fatalf("quick path should be on: %v %v", err, v.QuickPathReasons)
		}
		if v, err = PostAllGRNStep(tenantID, "system", "Super Admin", "", grn3); err != nil || v.Step != GRNStepPosted || v.DebitNoteID != "" || v.VendorCreditNoteID != "" {
			t.Fatalf("post all: %v %+v", err, v)
		}
	})

	t.Run("a step GRN's set-aside goods go back without a second note", func(t *testing.T) {
		if !stepGRNSettlesSetAside(tenantID, grnID) || stepGRNSettlesSetAside(tenantID, oldGRN) {
			t.Fatalf("only a step GRN settles its set-aside goods on its own note")
		}
	})
}

// TestStage94DesignReusePerVendor is 94.13 (D10): Inherit from existing
// looks only at the PO vendor's designs; a design with no vendor still matches.
func TestStage94DesignReusePerVendor(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	ids := []string{"TEST94-DES-V1", "TEST94-DES-V2", "TEST94-DES-NOV"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	ins := func(id, name, vendor string) {
		data := map[string]interface{}{"code": id, "name": name, "design_attributes": "Fabric:Cotton"}
		if vendor != "" {
			data["vendor"] = vendor
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductFamily', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	attrs := map[string]string{"Fabric": "Cotton"}
	ins("TEST94-DES-V1", "Stage 94 Test Tee", "TEST94-V1")
	ins("TEST94-DES-V2", "Stage 94 Test Tee", "TEST94-V2")
	if got, _ := FindMatchingDesignForVendor(tenantID, "stage 94 test tee", "TEST94-V2", attrs); got != "TEST94-DES-V2" {
		t.Fatalf("vendor 2's PO must reuse vendor 2's design, got %q", got)
	}
	if got, _ := FindMatchingDesignForVendor(tenantID, "Stage 94 Test Tee", "TEST94-V3", attrs); got != "" {
		t.Fatalf("another vendor's design of the same name is a different article, got %q", got)
	}
	ins("TEST94-DES-NOV", "Stage 94 Test Tee", "")
	if got, _ := FindMatchingDesignForVendor(tenantID, "Stage 94 Test Tee", "TEST94-V3", attrs); got != "TEST94-DES-NOV" {
		t.Fatalf("a design with no vendor still matches, got %q", got)
	}
	if got, _ := FindMatchingDesign(tenantID, "Stage 94 Test Tee", attrs); got != "TEST94-DES-V1" {
		t.Fatalf("no vendor searches every design (oldest first), got %q", got)
	}
}
