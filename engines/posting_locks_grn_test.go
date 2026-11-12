package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Stage 135.21 (part A): stock and costing postings that read a document's
// status outside the transaction that posts. Each test fires the same call
// from eight goroutines at once and checks the effect happened exactly once.

// s13521aTogether runs f from n goroutines released together.
func s13521aTogether(n int, f func(i int) error) []error {
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = f(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func s13521aOK(errs []error) (ok int) {
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	return
}

func s13521aLines(rows ...map[string]interface{}) string {
	raw, _ := json.Marshal(rows)
	return string(raw)
}

// s13521aLedger counts and sums the stock ledger lines of a voucher and item.
func s13521aLedger(schema, voucher, sku string) (lines int, sum float64) {
	_ = db.DB.QueryRow("SELECT COUNT(*), COALESCE(SUM((data->>'qty')::numeric), 0) FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1 AND data->>'item_id' = $2",
		voucher, sku).Scan(&lines, &sum)
	return
}

func s13521aItemCost(schema, sku string) (qty float64, value int64) {
	_ = db.DB.QueryRow("SELECT cumulative_qty_received, cumulative_value_received_paise FROM "+schema+".item_cost WHERE item_code = $1", sku).Scan(&qty, &value)
	return
}

func s13521aGL(schema, docID, account string) (dr, cr int64) {
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit), 0), COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", docID, account).Scan(&dr, &cr)
	return
}

// s13521aStepGRN receives a step GRN of 10 x TEE at 100 (bought before at
// 100: the Same child) and 5 x CAP at 200 (never bought: the New child),
// invoice total as given, and takes it through Verify and Price check.
func s13521aStepGRN(t *testing.T, tenantID string, insert func(id, doctype, status string, data map[string]interface{}), grnID, invoiceNo string, invoiceTotal float64) *GRNStepView {
	t.Helper()
	const loc = "WH-13521A"
	insert("TEE", "Item", "Active", map[string]interface{}{"code": "TEE", "name": "Tee", "hsn_code": "6109", "gst_rate": 5.0, "mrp": 299.0})
	insert("CAP", "Item", "Active", map[string]interface{}{"code": "CAP", "name": "Cap", "hsn_code": "6505", "gst_rate": 5.0, "mrp": 499.0})
	insert("VEND-13521A", "Vendor", "Active", map[string]interface{}{"code": "VEND-13521A", "name": "Vendor 135.21"})
	insert("PO-13521A", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-13521A", "vendor": "VEND-13521A", "gst_mode": GSTModeExclusive, "total_amount": 2000, "grand_total": 2100,
		"items": s13521aLines(map[string]interface{}{"sku": "TEE", "qty": 10, "rate": 100.0}, map[string]interface{}{"sku": "CAP", "qty": 5, "rate": 200.0})})
	insert("GRN-13521A-OLD", "GRN", "Approved", map[string]interface{}{"code": "GRN-13521A-OLD", "vendor": "VEND-13521A", "location": loc,
		"received_items": s13521aLines(map[string]interface{}{"sku": "TEE", "qty": 1, "invoice_rate": 100.0})})
	payload := map[string]interface{}{
		"flow": "steps", "po_ids": "PO-13521A", "location": loc,
		"vendor_invoice_no": invoiceNo, "vendor_invoice_date": "2026-10-27", "vendor_invoice_amount": invoiceTotal,
		"received_items": s13521aLines(
			map[string]interface{}{"sku": "TEE", "qty": 10, "invoice_rate": 100},
			map[string]interface{}{"sku": "CAP", "qty": 5, "invoice_rate": 200},
		),
	}
	if err := PrepareGRNPricing(tenantID, true, payload); err != nil {
		t.Fatalf("PrepareGRNPricing: %v", err)
	}
	if err := PrepareGRNSteps(tenantID, "system", true, payload); err != nil {
		t.Fatalf("PrepareGRNSteps: %v", err)
	}
	if err := PrepareGRNBatchNumbers(tenantID, true, payload); err != nil {
		t.Fatalf("PrepareGRNBatchNumbers: %v", err)
	}
	payload["code"] = grnID
	insert(grnID, "GRN", strField(payload, "status"), payload)
	SyncGRNLineRows(tenantID, grnID)
	if _, err := VerifyGRNStep(tenantID, "system", "Super Admin", grnID, GRNVerifyInput{Reason: "test"}); err != nil {
		t.Fatalf("verify: %v", err)
	}
	v, err := PriceCheckGRNStep(tenantID, "system", grnID, GRNPriceCheckInput{Reason: "test"})
	if err != nil {
		t.Fatalf("price check: %v", err)
	}
	if len(v.Children) != 2 {
		t.Fatalf("want two children (New, Same), got %+v", v.Children)
	}
	return v
}

// 135.21: eight posts of each of a step GRN's two children at once - each
// child's stock, QC split and cost post once, and the last child raises one
// vendor bill and one invoice-vs-GRN note.
func TestStage13521aGRNChildPostsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	if !grnLineTablesReady(db.DB, schema) {
		t.Skip("needs the Stage 94 GRN line tables")
	}
	const grnID, loc = "GRN-13521A", "WH-13521A"
	// Invoice 2100.50 against our 2100.00: a debit note of 0.50.
	v := s13521aStepGRN(t, tenantID, insert, grnID, "INV-13521A", 2100.5)
	errs := s13521aTogether(8, func(i int) error {
		_, err := PostGRNChildStep(tenantID, "system", "", grnID, v.Children[i%2].Key, GRNPostInput{})
		return err
	})
	if ok := s13521aOK(errs); ok != 2 {
		t.Fatalf("eight posts of two children: %d succeeded, want 2 (%v)", ok, errs)
	}
	for _, err := range errs {
		if err != nil && !strings.Contains(err.Error(), "already posted") {
			t.Fatalf("a losing post must be refused as already posted, got %v", err)
		}
	}
	for sku, want := range map[string]float64{"TEE": 10, "CAP": 5} {
		if onHand, _, _ := stage135Stock(t, schema, sku, loc); float64(onHand) != want {
			t.Fatalf("%s on hand %d, want %v (posted once)", sku, onHand, want)
		}
		if n, sum := s13521aLedger(schema, grnID, sku); n != 1 || sum != want {
			t.Fatalf("%s ledger: %d line(s) summing %v, want 1 of %v", sku, n, sum, want)
		}
		if qty, _ := s13521aItemCost(schema, sku); qty != want {
			t.Fatalf("%s item cost took %v received, want %v", sku, qty, want)
		}
	}
	if dr, _ := s13521aGL(schema, grnID, "1200"); dr != 200000 {
		t.Fatalf("Dr 1200 on the GRN = %d, want 200000", dr)
	}
	var bills, notes int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'VendorInvoice' AND data->>'grn_id' = $1", grnID).Scan(&bills)
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype IN ('DebitNote', 'VendorCreditNote') AND data->>'grn_id' = $1", grnID).Scan(&notes)
	if bills != 1 || notes != 1 {
		t.Fatalf("%d vendor bill(s) and %d note(s), want 1 and 1", bills, notes)
	}
	if after, err := GetGRNStepView(tenantID, grnID); err != nil || after.Step != GRNStepPosted {
		t.Fatalf("GRN after both children: %v %+v", err, after)
	}
	stage135BookAgrees(t, tenantID, loc)
}

// 135.21: payments of one GRN at once are all recorded (none dropped by a
// later save), and the credit-note extra is paid once.
func TestStage13521aGRNPayments(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	if !grnLineTablesReady(db.DB, schema) {
		t.Skip("needs the Stage 94 GRN line tables")
	}
	const grnID = "GRN-13521A-PAY"
	// Invoice 2099.50 against our 2100.00: a vendor credit note of 0.50,
	// owed on the GRN beside the bill.
	v := s13521aStepGRN(t, tenantID, insert, grnID, "INV-13521A-PAY", 2099.5)
	for _, c := range v.Children {
		if _, err := PostGRNChildStep(tenantID, "system", "", grnID, c.Key, GRNPostInput{}); err != nil {
			t.Fatalf("post child %s: %v", c.Key, err)
		}
	}
	v, err := GetGRNStepView(tenantID, grnID)
	if err != nil || v.VendorCreditNoteID == "" || v.NetPayable != 2100 {
		t.Fatalf("posted GRN: %v, credit note %q, due %v (want 2100)", err, v.VendorCreditNoteID, v.NetPayable)
	}

	// Eight part payments with their own references, at once: all eight land.
	errs := s13521aTogether(8, func(i int) error {
		_, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 100, Mode: "UPI", Reference: fmt.Sprintf("UTR13521A%d", i)})
		return err
	})
	if ok := s13521aOK(errs); ok != 8 {
		t.Fatalf("eight part payments of 100: %d succeeded, want 8 (%v)", ok, errs)
	}
	if v, _ = GetGRNStepView(tenantID, grnID); len(v.Payments) != 8 || v.PaidAmount != 800 {
		t.Fatalf("after eight payments of 100: %d recorded, paid %v - want 8, 800", len(v.Payments), v.PaidAmount)
	}
	if _, cr := s13521aGL(schema, v.VendorBillID, "1100"); cr != 80000 {
		t.Fatalf("paid on the bill: %d paise, want 80000", cr)
	}

	// The rest of the bill, then the 0.50 extra paid by eight at once: once.
	bill, _, _ := fetchDocData(tenantID, "VendorInvoice", v.VendorBillID)
	if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: vendorInvoiceOpenAmount(bill), Mode: "Bank", Reference: "UTR13521A-BILL"}); err != nil {
		t.Fatalf("pay the rest of the bill: %v", err)
	}
	errs = s13521aTogether(8, func(i int) error {
		_, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Mode: "Cash", Reference: fmt.Sprintf("CASH13521A%d", i)})
		return err
	})
	if ok := s13521aOK(errs); ok != 1 {
		t.Fatalf("eight payments of the 0.50 extra at once: %d succeeded, want 1 (%v)", ok, errs)
	}
	if _, cr := s13521aGL(schema, grnID, "1100"); cr != 50 {
		t.Fatalf("the extra paid from cash/bank on the GRN: %d paise, want 50", cr)
	}
	if v, _ = GetGRNStepView(tenantID, grnID); v.PaymentStatus != "Paid" || v.NetPayable != 0 || len(v.Payments) != 10 {
		t.Fatalf("after paying all: %s, due %v, %d payments - want Paid, 0, 10", v.PaymentStatus, v.NetPayable, len(v.Payments))
	}
}

// 135.21: eight applies of one landed cost voucher at once raise the item's
// cost once.
func TestStage13521aLandedCostAppliesOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const sku, poID, grnID, loc = "LC-ITEM", "LC-PO", "LC-GRN", "LC-LOC"
	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Landed", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
	insert(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "gst_mode": GSTModeExclusive, "items": s13521aLines(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0})})
	insert(grnID, "GRN", "Approved", map[string]interface{}{"code": grnID, "po_id": poID, "location": loc, "received_items": s13521aLines(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10})})
	if _, err := PostGRNReceiptWithQC(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0}}, "system", grnID); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	voucherID, err := CreateLandedCostVoucher(tenantID, grnID, []map[string]interface{}{{"charge_type": "Freight", "amount": 200.0}}, "system")
	if err != nil {
		t.Fatalf("voucher: %v", err)
	}
	errs := s13521aTogether(8, func(int) error { return ApplyLandedCostVoucher(tenantID, voucherID, "system") })
	if ok := s13521aOK(errs); ok != 1 {
		t.Fatalf("eight applies at once: %d succeeded, want 1 (%v)", ok, errs)
	}
	for _, err := range errs {
		if err != nil && !strings.Contains(err.Error(), "already been applied") {
			t.Fatalf("a losing apply must be refused as already applied, got %v", err)
		}
	}
	if _, value := s13521aItemCost(schema, sku); value != 120000 {
		t.Fatalf("item value %d paise, want 120000 (1000 + 200 once)", value)
	}
	if got, _, _ := GetItemUnitCost(tenantID, sku); got != 12000 {
		t.Fatalf("unit cost %d paise, want 12000", got)
	}
	if dr, _ := s13521aGL(schema, voucherID, "1200"); dr != 20000 {
		t.Fatalf("Dr 1200 on the voucher = %d, want 20000", dr)
	}
}

// 135.21: job work receipts and charges at once, each case on an order of its
// own (gold out to become rings, polish out to come back as itself).
func TestStage13521aJobWorkPostsOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const gold, ring, pol = "JW-GOLD", "JW-RING", "JW-POL"
	for sku, cost := range map[string]float64{gold: 100, ring: 0, pol: 40} {
		insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "cost_price": cost, "valuation": "VALUATED"})
	}
	type stock struct{ onHand, available, withParty int }
	read := func(loc, sku string) (s stock) {
		_ = db.DB.QueryRow("SELECT on_hand, available, COALESCE(with_party, 0) FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&s.onHand, &s.available, &s.withParty)
		return
	}
	// sent sends 50 gold and 10 polish on a new order from a store of its own.
	sent := func(t *testing.T, tag string) (order, loc string) {
		t.Helper()
		order, loc = "JW-13521A-"+tag, "JW-LOC-"+tag
		insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "JW store " + tag, "type": "Store"})
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": gold, "qty": 100.0}, map[string]interface{}{"sku": pol, "qty": 10.0}}, false); err != nil {
			t.Fatal(err)
		}
		lines := s13521aLines(map[string]interface{}{"sku": gold, "qty": 50, "output_sku": ring, "output_per_unit": 0.2}, map[string]interface{}{"sku": pol, "qty": 10})
		insert(order, "SubcontractOrder", "Draft", map[string]interface{}{"code": order, "vendor_id": "JW-VENDOR", "location": loc, "due_date": "2099-01-01", "lines": lines})
		if st, err := SendJobWork(tenantID, order, "system", RoleStoreManager, ChallanDispatch{VehicleNo: "KA01"}); err != nil || st != "Sent" {
			t.Fatalf("send: %s %v", st, err)
		}
		return order, loc
	}
	accepted := func(order string) []int {
		data, _, _ := fetchSubcontractOrder(tenantID, order)
		bal, _ := jobWorkState(data)
		var out []int
		for _, b := range bal {
			out = append(out, b.Accepted)
		}
		return out
	}

	t.Run("one receipt key eight times at once moves stock once", func(t *testing.T) {
		order, loc := sent(t, "KEY")
		for _, err := range s13521aTogether(8, func(int) error {
			_, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, "K1", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 10}, {LineNo: 2, AcceptedQty: 4}})
			return err
		}) {
			if err != nil {
				t.Fatalf("a replay of receipt K1 must succeed and do nothing, got %v", err)
			}
		}
		if g, r, p := read(loc, gold), read(loc, ring), read(loc, pol); g.onHand != 90 || g.withParty != 40 || r.onHand != 2 || p.available != 4 || p.withParty != 6 {
			t.Fatalf("after receipt K1 (x8): gold %+v ring %+v polish %+v - want gold 90 / 40 out, 2 rings, polish 4 back / 6 out", g, r, p)
		}
		stage135BookAgrees(t, tenantID, loc)
	})

	t.Run("receipts with their own keys take only what is out, and each reaches the ledger", func(t *testing.T) {
		order, loc := sent(t, "OWN")
		costBefore, _ := s13521aItemCost(schema, ring)
		errs := s13521aTogether(8, func(i int) error {
			_, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, fmt.Sprintf("P%d", i), []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 20}})
			return err
		})
		if ok := s13521aOK(errs); ok != 2 {
			t.Fatalf("eight receipts of 20 against 50 out: %d succeeded, want 2 (%v)", ok, errs)
		}
		if g, r := read(loc, gold), read(loc, ring); g.onHand != 60 || g.withParty != 10 || r.onHand != 8 {
			t.Fatalf("after two receipts: gold %+v ring %+v - want gold 60 / 10 out, 8 rings", g, r)
		}
		if got := accepted(order); got[0] != 40 {
			t.Fatalf("order balance accepted %v, want 40 on line 1 (no receipt dropped)", got)
		}
		if n, sum := s13521aLedger(schema, order, ring); n != 2 || sum != 8 {
			t.Fatalf("ring ledger: %d line(s) summing %v, want 2 summing 8 (one per receipt)", n, sum)
		}
		if n, sum := s13521aLedger(schema, order, gold); n != 2 || sum != -40 {
			t.Fatalf("gold ledger: %d line(s) summing %v, want 2 summing -40", n, sum)
		}
		if qty, _ := s13521aItemCost(schema, ring); qty-costBefore != 8 {
			t.Fatalf("ring cost took %v more received, want 8", qty-costBefore)
		}
		stage135BookAgrees(t, tenantID, loc)
	})

	t.Run("two receipts one after the other each reach the ledger", func(t *testing.T) {
		order, loc := sent(t, "SEQ")
		for _, key := range []string{"S1", "S2"} {
			if _, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, key, []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 10}}); err != nil {
				t.Fatalf("receipt %s: %v", key, err)
			}
		}
		if n, sum := s13521aLedger(schema, order, ring); n != 2 || sum != 4 {
			t.Fatalf("ring ledger: %d line(s) summing %v, want 2 summing 4", n, sum)
		}
		stage135BookAgrees(t, tenantID, loc)
	})

	t.Run("a receipt with no key is not a replay of the one before", func(t *testing.T) {
		order, loc := sent(t, "NOKEY")
		for i := 0; i < 2; i++ {
			if _, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, "", []JobWorkReceiveLine{{LineNo: 2, AcceptedQty: 2}}); err != nil {
				t.Fatalf("keyless receipt %d: %v", i+1, err)
			}
		}
		if p := read(loc, pol); p.available != 4 || p.withParty != 6 {
			t.Fatalf("polish after two keyless receipts of 2: %+v, want 4 back / 6 out", p)
		}
		if got := accepted(order); got[1] != 4 {
			t.Fatalf("order balance accepted %v, want 4 on line 2", got)
		}
	})

	t.Run("eight bills at once are all booked, one bill eight times once", func(t *testing.T) {
		order, _ := sent(t, "BILL")
		if _, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, "R1", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 10}}); err != nil {
			t.Fatalf("receive: %v", err)
		}
		if ok := s13521aOK(s13521aTogether(8, func(i int) error {
			return PostJobWorkCharge(tenantID, order, "system", fmt.Sprintf("B%d", i), 100)
		})); ok != 8 {
			t.Fatalf("eight bills at once: %d booked, want 8", ok)
		}
		if ok := s13521aOK(s13521aTogether(8, func(int) error { return PostJobWorkCharge(tenantID, order, "system", "BX", 50) })); ok != 1 {
			t.Fatalf("one bill eight times: %d booked, want 1", ok)
		}
		data, _, _ := fetchSubcontractOrder(tenantID, order)
		if got := numFromInterface(data["processing_charges"]); got != 850 {
			t.Fatalf("processing charges on the order %v, want 850", got)
		}
		var charged int64
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit), 0) FROM "+schema+".gl_postings WHERE document_id = $1 AND idempotency_key LIKE 'JobWorkCharge:%'", order).Scan(&charged)
		if charged != 85000 {
			t.Fatalf("charges booked %d paise, want 85000", charged)
		}
	})

	t.Run("an order sent the old way receives once", func(t *testing.T) {
		const order, loc = "JW-13521A-OLD", "JW-LOC-OLD"
		insert(order, "SubcontractOrder", "Sent", map[string]interface{}{"code": order, "received_item_id": ring, "location": loc, "status": "Sent"})
		if ok := s13521aOK(s13521aTogether(8, func(int) error { return ReceiveFromSubcontractor(tenantID, order, "system", 5) })); ok != 1 {
			t.Fatalf("eight receipts of an old-way order: %d succeeded, want 1", ok)
		}
		if r := read(loc, ring); r.onHand != 5 {
			t.Fatalf("rings after the old-way receipt of 5: %d, want 5", r.onHand)
		}
		stage135BookAgrees(t, tenantID, loc)
	})
}
