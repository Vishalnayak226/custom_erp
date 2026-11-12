package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
)

// s13524Net is what the step GRN, its vendor bill and its note leave on one
// account (debit - credit), in paise.
func s13524Net(schema, account string, docIDs ...string) (net int64) {
	for _, id := range docIDs {
		if id == "" {
			continue
		}
		dr, cr := s13521aGL(schema, id, account)
		net += dr - cr
	}
	return net
}

// s13524PostedStepGRN is a step GRN of our 2100.00 (2000 + 5% GST) against
// the vendor's invoice total, both children posted.
func s13524PostedStepGRN(t *testing.T, invoiceTotal float64) (tenantID, schema, grnID string) {
	t.Helper()
	tenantID, schema, insert := stage135Tenant(t)
	if !grnLineTablesReady(db.DB, schema) {
		t.Skip("needs the Stage 94 GRN line tables")
	}
	grnID = "GRN-13524"
	v := s13521aStepGRN(t, tenantID, insert, grnID, "INV-13524", invoiceTotal)
	for _, c := range v.Children {
		if _, err := PostGRNChildStep(tenantID, "system", "", grnID, c.Key, GRNPostInput{}); err != nil {
			t.Fatalf("post child %s: %v", c.Key, err)
		}
	}
	if v, err := GetGRNStepView(tenantID, grnID); err != nil || v.NetPayable != 2100 {
		t.Fatalf("posted GRN: %v, due %v (want 2100)", err, v.NetPayable)
	}
	return tenantID, schema, grnID
}

// s13524PaidRight checks a step GRN paid in full: our 2100.00 left the bank
// (no more), GRN Suspense is clear and the input credit is our GST.
func s13524PaidRight(t *testing.T, tenantID, schema, grnID string) {
	t.Helper()
	v, err := GetGRNStepView(tenantID, grnID)
	if err != nil || v.PaymentStatus != "Paid" || v.NetPayable != 0 {
		t.Fatalf("after paying all: %v, %s, due %v - want Paid, 0", err, v.PaymentStatus, v.NetPayable)
	}
	note := firstNonEmpty(v.VendorCreditNoteID, v.DebitNoteID)
	if paid := -s13524Net(schema, "1100", grnID, v.VendorBillID, note); paid != 210000 {
		t.Errorf("paid from cash / bank: %d paise, want 210000 (our total, to the paisa)", paid)
	}
	if left := s13524Net(schema, "2100", grnID, v.VendorBillID, note); left != 0 {
		t.Errorf("GRN Suspense left on the receipt: %d paise, want 0", left)
	}
	if gst := s13524Net(schema, "1500", grnID, v.VendorBillID, note); gst != 10000 {
		t.Errorf("input GST: %d paise, want 10000 (our GST)", gst)
	}
}

// Stage 135.24: a step GRN's vendor bill is paid at its exact amount, not
// the whole rupee - so with a credit note (billed 2099.50) or a debit note
// (billed 2100.50) the receipt still pays exactly our 2100.00.
func TestStage13524StepBillPaysToThePaisa(t *testing.T) {
	t.Run("credit note, part then the rest", func(t *testing.T) {
		tenantID, schema, grnID := s13524PostedStepGRN(t, 2099.5)
		v, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 100, Mode: "UPI", Reference: "UTR13524A"})
		if err != nil {
			t.Fatal(err)
		}
		if v.NetPayable != 2000 {
			t.Fatalf("due after paying 100 of 2100: %v, want 2000", v.NetPayable)
		}
		if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Mode: "Bank", Reference: "UTR13524B"}); err != nil {
			t.Fatal(err)
		}
		s13524PaidRight(t, tenantID, schema, grnID)
	})
	t.Run("credit note, all at once", func(t *testing.T) {
		tenantID, schema, grnID := s13524PostedStepGRN(t, 2099.5)
		if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Mode: "Bank", Reference: "UTR13524C"}); err != nil {
			t.Fatal(err)
		}
		s13524PaidRight(t, tenantID, schema, grnID)
	})
	t.Run("debit note, all at once", func(t *testing.T) {
		tenantID, schema, grnID := s13524PostedStepGRN(t, 2100.5)
		if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Mode: "Bank", Reference: "UTR13524D"}); err != nil {
			t.Fatal(err)
		}
		s13524PaidRight(t, tenantID, schema, grnID)
	})
	t.Run("the bill paid from Supplier bills takes its exact amount", func(t *testing.T) {
		tenantID, schema, grnID := s13524PostedStepGRN(t, 2099.5)
		v, _ := GetGRNStepView(tenantID, grnID)
		if _, _, err := PayVendorInvoice(tenantID, v.VendorBillID, "system", "Admin", ""); err != nil {
			t.Fatal(err)
		}
		if _, cr := s13521aGL(schema, v.VendorBillID, "1100"); cr != 209950 {
			t.Errorf("the 2099.50 bill paid in full: %d paise, want 209950", cr)
		}
		if v, _ = GetGRNStepView(tenantID, grnID); v.NetPayable != 0.5 {
			t.Errorf("due on the GRN after its bill: %v, want the 0.50 credit-note extra", v.NetPayable)
		}
	})
}

// s13526FailSaves makes every save of one doctype's documents fail in the
// test client's schema until the returned func drops it - a failure after
// the bill / challan part of a posting, before its own document commits.
func s13526FailSaves(t *testing.T, schema, doctype string) func() {
	t.Helper()
	stmts := []string{
		`CREATE OR REPLACE FUNCTION ` + schema + `.s13526_fail() RETURNS trigger AS $f$ BEGIN RAISE EXCEPTION 's13526: the save fails'; END $f$ LANGUAGE plpgsql`,
		`CREATE TRIGGER s13526_fail BEFORE UPDATE ON ` + schema + `.documents FOR EACH ROW WHEN (NEW.doctype = '` + doctype + `') EXECUTE FUNCTION ` + schema + `.s13526_fail()`,
	}
	for _, s := range stmts {
		if _, err := db.DB.Exec(s); err != nil {
			t.Fatalf("fail trigger: %v", err)
		}
	}
	return func() {
		if _, err := db.DB.Exec(`DROP TRIGGER IF EXISTS s13526_fail ON ` + schema + `.documents`); err != nil {
			t.Fatalf("drop fail trigger: %v", err)
		}
	}
}

// Stage 135.26: a step GRN's payment pays its bill part in the GRN's own
// transaction - when the GRN cannot be saved, the bill is not paid either,
// and the retry pays it once.
func TestStage13526GRNPaymentCommitsWithItsBill(t *testing.T) {
	tenantID, schema, grnID := s13524PostedStepGRN(t, 2100)
	v, _ := GetGRNStepView(tenantID, grnID)
	restore := s13526FailSaves(t, schema, "GRN")
	if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 100, Mode: "UPI", Reference: "UTR13526"}); err == nil {
		restore()
		t.Fatal("the payment must fail when the GRN cannot be saved")
	}
	restore()
	bill, _, _ := fetchDocData(tenantID, "VendorInvoice", v.VendorBillID)
	if _, cr := s13521aGL(schema, v.VendorBillID, "1100"); cr != 0 || vendorInvoiceHasPayments(bill) {
		t.Fatalf("after a failed GRN payment the bill shows %d paise paid (payments recorded: %v), want nothing", cr, vendorInvoiceHasPayments(bill))
	}
	if _, err := PayGRNStep(tenantID, "system", grnID, GRNPayInput{Amount: 100, Mode: "UPI", Reference: "UTR13526"}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if v, _ = GetGRNStepView(tenantID, grnID); len(v.Payments) != 1 || v.NetPayable != 2000 {
		t.Fatalf("after the retry: %d payment(s), due %v - want 1, 2000", len(v.Payments), v.NetPayable)
	}
	if _, cr := s13521aGL(schema, v.VendorBillID, "1100"); cr != 10000 {
		t.Fatalf("paid on the bill after the retry: %d paise, want 10000", cr)
	}
}

// Stage 135.26: job work takes the same item back in the order's own
// transaction - when the order cannot be saved, the goods stay out on the
// challan, and the retry brings them back once.
func TestStage13526JobWorkReturnCommitsWithItsOrder(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	const pol, loc, order = "JW26-POL", "JW26-LOC", "JW-13526"
	insert(pol, "Item", "Active", map[string]interface{}{"code": pol, "name": pol, "cost_price": 40.0, "valuation": "VALUATED"})
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "JW store 135.26", "type": "Store"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": pol, "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert(order, "SubcontractOrder", "Draft", map[string]interface{}{"code": order, "vendor_id": "JW-VENDOR", "location": loc, "due_date": "2099-01-01",
		"lines": s13521aLines(map[string]interface{}{"sku": pol, "qty": 10})})
	if st, err := SendJobWork(tenantID, order, "system", RoleStoreManager, ChallanDispatch{VehicleNo: "KA01"}); err != nil || st != "Sent" {
		t.Fatalf("send: %s %v", st, err)
	}
	read := func() (available, withParty int) {
		_ = db.DB.QueryRow("SELECT available, COALESCE(with_party, 0) FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", pol, loc).Scan(&available, &withParty)
		return
	}

	restore := s13526FailSaves(t, schema, "SubcontractOrder")
	if _, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, "K26", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 4}}); err == nil {
		restore()
		t.Fatal("the receipt must fail when the order cannot be saved")
	}
	restore()
	if a, w := read(); a != 0 || w != 10 {
		t.Fatalf("after a failed receipt: %d available, %d out - want 0, 10 (nothing back)", a, w)
	}
	if _, err := ReceiveJobWork(tenantID, order, "system", RoleStoreManager, "K26", []JobWorkReceiveLine{{LineNo: 1, AcceptedQty: 4}}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if a, w := read(); a != 4 || w != 6 {
		t.Fatalf("after the retry: %d available, %d out - want 4, 6", a, w)
	}
	stage135BookAgrees(t, tenantID, loc)
}

// Stage 135.25: an order one settlement has taken is refused by another,
// naming the first - its sale is cleared once - and settlements naming the
// same orders at once settle them once.
func TestStage13525OrderSettledOnce(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)
	for _, id := range []string{"ORD-25A", "ORD-25B", "ORD-25C", "ORD-25D"} {
		insert(id, "POSCart", "Shipped", map[string]interface{}{"status": "Shipped"})
	}
	if err := ProcessMarketplaceSettlement(tenantID, "Shopify", "SETT-25-1", 10000, 1000, 9000, []string{"ORD-25A"}); err != nil {
		t.Fatal(err)
	}
	err := ProcessMarketplaceSettlement(tenantID, "Shopify", "SETT-25-2", 20000, 2000, 18000, []string{"ORD-25B", "ORD-25A"})
	if err == nil || !strings.Contains(err.Error(), "ORD-25A is already settled under settlement SETT-25-1") {
		t.Fatalf("a second settlement taking ORD-25A: %v, want it refused naming SETT-25-1", err)
	}
	if status, _ := s13521bDoc(t, schema, "ORD-25B"); status != "Shipped" {
		t.Fatalf("ORD-25B on the refused settlement is %s, want Shipped (nothing of it saved)", status)
	}
	if cr := s13521bInt(t, "SELECT COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_type = 'MarketplaceSettlement' AND document_id = 'SETT-25-2'"); cr != 0 {
		t.Fatalf("the refused settlement posted %d paise", cr)
	}

	// Eight settlements of their own naming the same two orders (in either
	// order) at once: one takes them.
	errs := s13521bRace(8, func(i int) error {
		orders := []string{"ORD-25C", "ORD-25D"}
		if i%2 == 1 {
			orders = []string{"ORD-25D", "ORD-25C"}
		}
		return ProcessMarketplaceSettlement(tenantID, "Shopify", fmt.Sprintf("SETT-25-R%d", i), 5000, 500, 4500, orders)
	})
	s13521bOneWinner(t, "settle the same orders", errs, "is already settled under settlement")
	if cr := s13521bInt(t, "SELECT COALESCE(SUM(credit), 0) FROM "+schema+".gl_postings WHERE document_type = 'MarketplaceSettlement' AND document_id LIKE 'SETT-25-R%' AND account_code = '1300'"); cr != 500000 {
		t.Fatalf("receivable cleared %d paise for ORD-25C / D, want 500000 (once)", cr)
	}
}
