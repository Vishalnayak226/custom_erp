package engines

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"custom_erp/db"
)

// Review of the 2026-11-04 batch: money that came out wrong on a mixed path, and
// limits a public or a trusted caller could step past. One test per finding.

// The goods receipt screen posts by steps, and that path makes the vendor bill at
// post. A memo receipt must not: the sale raises the bill, so one here is owed twice.
func TestStage14816StepReceiptRaisesNoBill(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	s := m.stepReceipt(5, 80, 80, 5, 420)
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.documents SET data = data || '{"memo_stock":"Yes"}' WHERE id = $1`, s.grn); err != nil {
		t.Fatal(err)
	}
	v := m.postStep(s)
	if v.VendorBillID != "" || v.NetPayable != 0 || !v.MemoStock {
		t.Fatalf("posted memo receipt: bill %q due %v memo %v, want no bill, nothing due, memo", v.VendorBillID, v.NetPayable, v.MemoStock)
	}
	if v.PaymentStatus != "" {
		t.Errorf("posted memo receipt says %q, want no payment status", v.PaymentStatus)
	}
	grnData, grnStatus, err := fetchDocData(m.tenant, "GRN", s.grn)
	if err != nil {
		t.Fatal(err)
	}
	if tab := GRNInboxTab(grnData, grnStatus); tab != "done" {
		t.Errorf("posted memo receipt on tab %q, want done", tab)
	}
	var bills int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM `+m.schema+`.documents WHERE doctype = 'VendorInvoice' AND data->>'grn_id' = $1`, s.grn).Scan(&bills); err != nil {
		t.Fatal(err)
	}
	if bills != 0 {
		t.Fatalf("%d vendor bills on the memo receipt, want none", bills)
	}
	if n, open := memoLotCount(t, m, s.sku); n != 1 || open != 5 {
		t.Fatalf("memo lots %d open %v, want 1 lot of 5", n, open)
	}
	for _, acct := range []string{"1200", "2100"} {
		if got := m.gl(s.grn, acct); got != 0 {
			t.Errorf("memo receipt posted %d paise to %s", got, acct)
		}
	}

	refused := func(door string, err error) {
		t.Helper()
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Code != memoBillCode {
			t.Errorf("%s on a memo receipt: got %v, want %s", door, err, memoBillCode)
		}
	}
	_, err = PayGRNStep(m.tenant, "system", s.grn, GRNPayInput{Amount: 10, Mode: "Bank", Reference: "M14816"})
	refused("pay", err)
	_, err = CorrectGRNStep(m.tenant, "system", "", s.grn, GRNCorrectionInput{Apply: true, Reason: "typed wrong"})
	refused("correct", err)
	_, err = CreateLandedCostVoucher(m.tenant, s.grn, []map[string]interface{}{{"amount": 10.0}}, "system")
	refused("landed cost", err)
	refused("landed cost form", ValidateLandedCostVoucherDocument(m.tenant, map[string]interface{}{"grn_reference": s.grn}))
	typed := m.id("VI")
	m.doc(typed, "VendorInvoice", "Draft", map[string]interface{}{"code": typed, "invoice_number": typed, "grn_id": s.grn, "po_id": s.po, "invoice_amount": 420.0})
	_, err = MatchVendorInvoiceAs(m.tenant, "system", typed, "", "", 0)
	refused("a bill typed against it", err)

	cart, _ := m.mustSell(s.sku, 2, nil)
	ids, amounts := memoBills(t, m, cart)
	if len(ids) != 1 || len(amounts) != 1 || amounts[0] != 160 {
		t.Fatalf("memo bills %v %v, want one bill of 160", ids, amounts)
	}
}

// Units written off leave no mark on a memo lot. They must not reach a vendor bill
// once own stock arrives and the "never more than on hand" cap stops hiding them.
func TestStage14816LostMemoUnitsAreNotBilled(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	sku := m.item(nil)
	memoReceive(t, m, vendor, sku, 10, 80)
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.inventory_availability SET on_hand = on_hand - 5 WHERE sku = $1`, sku); err != nil {
		t.Fatal(err)
	}
	m.receive(sku, 20, 100)
	if _, open := memoLotCount(t, m, sku); open != 5 {
		t.Fatalf("open memo units %v, want 5 (10 received, 5 written off)", open)
	}
	cart, _ := m.mustSell(sku, 8, nil)
	if _, amounts := memoBills(t, m, cart); len(amounts) != 1 || amounts[0] != 400 {
		t.Fatalf("vendor bill %v, want one of 400 (5 memo units at 80; the other 3 sold are own stock)", amounts)
	}
}

// A sale that runs into the same gap draws what is really there and no more.
func TestStage14816SaleAfterAWriteOffBillsOnlyWhatIsHeld(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	sku := m.item(nil)
	memoReceive(t, m, vendor, sku, 10, 80)
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.inventory_availability SET on_hand = on_hand - 6 WHERE sku = $1`, sku); err != nil {
		t.Fatal(err)
	}
	cart, _ := m.mustSell(sku, 3, nil)
	if _, amounts := memoBills(t, m, cart); len(amounts) != 1 || amounts[0] != 240 {
		t.Fatalf("vendor bill %v, want 240 (3 at 80)", amounts)
	}
	if _, open := memoLotCount(t, m, sku); open != 1 {
		t.Fatalf("open memo units %v, want 1 (4 held, 3 sold)", open)
	}
}

// Two lines of one SKU on a memo receipt are one lot of both quantities.
func TestStage14816TwoLinesOfOneSKUAreOneLot(t *testing.T) {
	m := newMC1481(t)
	memoModule(t, m, true)
	vendor := memoVendor(m)
	sku := m.item(nil)
	lines := []mc1481Line{{sku: sku, qty: 3, rate: 80}, {sku: sku, qty: 4, rate: 80}}
	grnID, _ := m.grnDocs(m.loc, lines)
	if _, err := db.DB.Exec(`UPDATE `+m.schema+`.documents SET data = data || $2::jsonb WHERE id = $1`, grnID,
		`{"memo_stock":"Yes","vendor":"`+vendor+`"}`); err != nil {
		t.Fatal(err)
	}
	if err := m.postGRN(m.loc, grnID, lines); err != nil {
		t.Fatalf("memo receipt: %v", err)
	}
	if n, open := memoLotCount(t, m, sku); n != 1 || open != 7 {
		t.Fatalf("lots %d open %v, want one lot of 7", n, open)
	}
}

// A stock item's revenue never leaves the sales account, whatever the line names.
func TestStage14822StockLineStaysOnItsOwnAccount(t *testing.T) {
	tenantID, schema, insert := stage14822Setup(t)
	if !freeTextLineIsStock(tenantID, "TEE14822") {
		t.Fatal("the fixture item must read as stock")
	}
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "TEE14822", "qty": 1, "rate": 100.0, "amount": 100.0, "income_account": "4950"}})
	insert("SI-14822-STOCK", "SalesInvoice", "Draft", "manager1", map[string]interface{}{"customer": "C14822", "location": "S14822", "items": string(items), "total_amount": 100.0, "status": "Draft"})
	if _, err := PostSalesInvoice(tenantID, "SI-14822-STOCK", "manager1"); err != nil {
		t.Fatal(err)
	}
	if _, c := stage140GL(t, schema, "SalesInvoice", "SI-14822-STOCK", "4950"); c != 0 {
		t.Fatalf("a stock line moved %d paise to the account it named", c)
	}
	if _, c := stage140GL(t, schema, "SalesInvoice", "SI-14822-STOCK", "4100"); c != 10000 {
		t.Fatalf("a stock line must stay on 4100, got %d", c)
	}
}

// A replacement goes out free: the customer's price list does not fill it in.
func TestStage14826ReplacementOrderIsNotPriced(t *testing.T) {
	f := newB2BFx(t)
	f.version("PLV-WS1", "WS", "2020-01-01", "Approved", wsRows)
	f.on()
	repl := []SalesOrderLineInput{{SKU: "ITEM-B1", Qty: 2}}
	if _, _, err := applyB2BOrderPricing(f.tenantID, "Replacement", "CUST-WS", repl); err != nil {
		t.Fatal(err)
	}
	if repl[0].UnitPrice != 0 {
		t.Fatalf("a replacement line was priced at %v", repl[0].UnitPrice)
	}
	manual := []SalesOrderLineInput{{SKU: "ITEM-B1", Qty: 2}}
	if _, _, err := applyB2BOrderPricing(f.tenantID, "Manual", "CUST-WS", manual); err != nil {
		t.Fatal(err)
	}
	if manual[0].UnitPrice != 400 {
		t.Fatalf("an ordinary order still takes the list: got %v", manual[0].UnitPrice)
	}
}

// Memo stock goes back to its vendor without a debit note, so no purchase return is drafted on it.
func TestStage14828ReturnToVendorSkipsAMemoReceipt(t *testing.T) {
	f := stage1484Setup(t)
	f.grn(t, "GRN-14828M", "V1")
	if _, err := db.DB.Exec(`UPDATE ` + f.schema + `.documents SET data = data || '{"memo_stock":"Yes"}' WHERE id = 'GRN-14828M'`); err != nil {
		t.Fatal(err)
	}
	stage14828Rule(t, f, "RDR-M", map[string]interface{}{"disposition": "Damaged", "action": "Return to vendor"})
	stage14828Sale(t, f, "CART-14828-M")
	returnID, _ := stage14828QC(t, f, "CART-14828-M", "SHOE1484", "Damaged")
	if n := stage14828Count(t, f, "PurchaseReturn", "data->>'return_request_id' = '"+returnID+"'"); n != 0 {
		t.Fatalf("a memo receipt must not get a purchase return, got %d", n)
	}
}

// The restocking fee comes off a credit note as it comes off a refund.
func TestStage14828CreditLineTakesTheFee(t *testing.T) {
	f := stage1484Setup(t)
	if _, err := db.DB.Exec("INSERT INTO " + f.schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('4170', 'Restocking fees', 'Revenue') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	f.insert("TC-RESTOCK", "TradeChargeCode", "Active", "system", map[string]interface{}{"code": "RESTOCK", "charge_name": "Restocking fee", "used_for": "Selling", "gl_account": "4170", "status": "Active"})
	stage14828Rule(t, f, "RDR-CF", map[string]interface{}{"disposition": "Damaged", "action": "Credit", "charge_code": "RESTOCK", "fee_percent": 10.0})
	stage14828Sale(t, f, "CART-14828-CF")
	returnID, _ := stage14828QC(t, f, "CART-14828-CF", "SHOE1484", "Damaged")
	if n := stage14828Count(t, f, "CreditNote", "data->>'return_request_id' = '"+returnID+"' AND (data->>'amount')::float8 = 90"); n != 1 {
		t.Fatalf("want one credit note of 90 (100 less a 10%% fee), got %d", n)
	}
}

// Trust covers a date or a cut. A trusted vendor cannot raise what we buy without the buyer.
func TestStage14818TrustedVendorQtyRiseWaits(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-Q")
	tok := f.link("VL-Q")
	f.vendorSet(map[string]interface{}{"vendor_link_trusted": "Yes"})
	res, err := SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 1, Qty: qty(25)}})
	if err != nil || res.Applied != 0 || res.Pending != 1 {
		t.Fatalf("a rise must wait for the buyer: %+v %v", res, err)
	}
	if got := numFromInterface(f.poLines("VL-Q")[0]["qty"]); got != 10 {
		t.Fatalf("the PO line moved to %v before the buyer saw it", got)
	}
	res, err = SubmitVendorProposal(f.tenantID, tok, []VendorProposalLine{{LineNo: 2, Qty: qty(3)}})
	if err != nil || res.Applied != 1 {
		t.Fatalf("a cut from a trusted vendor applies at once: %+v %v", res, err)
	}
}

// The file cap holds when a link's uploads arrive together.
func TestStage14818FileCapHoldsUnderConcurrentUploads(t *testing.T) {
	f := newVLFixture(t)
	f.openPO("VL-F")
	tok := f.link("VL-F")
	pdf := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 100)...)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = SubmitVendorInvoice(f.tenantID, tok, "bill.pdf", pdf, "", "", nil)
		}()
	}
	wg.Wait()
	var n int
	if err := db.DB.QueryRow(`SELECT count(*) FROM ` + f.schema + `.vendor_po_files WHERE po_id = 'VL-F'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != vendorFilesPerLink {
		t.Fatalf("%d files stored, want exactly %d", n, vendorFilesPerLink)
	}
}

// A hold is checked against who the invoice sells to, by id, by name or by the id in the name field.
func TestStage14841InvoiceCustomerForAHold(t *testing.T) {
	tenantID, _, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("C-HN", "Customer", "Active", map[string]interface{}{"name": "Cust Named", "mobile": "99HN"})
	for _, c := range []struct {
		data map[string]interface{}
		want string
	}{
		{map[string]interface{}{"customer_id": "C-X", "customer": "Cust Named"}, "C-X"},
		{map[string]interface{}{"customer": "Cust Named"}, "C-HN"},
		{map[string]interface{}{"customer": "C-HN"}, "C-HN"},
		{map[string]interface{}{}, ""},
	} {
		if got := invoiceHoldCustomer(tenantID, c.data); got != c.want {
			t.Errorf("%v: got %q want %q", c.data, got, c.want)
		}
	}
}

// A set-off voucher made on the record form is issued a number from its series, and
// the form does not ask for what the server fills in on every save.
func TestStage14823VoucherNumberAndForm(t *testing.T) {
	f := newSetOffFixture(t)
	payload := map[string]interface{}{"pair_id": "any", "amount": 10.0}
	if err := PrepareDocumentNumber(f.tenantID, "HQ", SetOffVoucherDoctype, true, payload); err != nil {
		t.Fatal(err)
	}
	id, _ := payload["id"].(string)
	if !strings.HasPrefix(id, "SOV") || payload["code"] != id {
		t.Fatalf("want a voucher numbered from the SOV series, got id %q code %v", id, payload["code"])
	}
	for _, k := range []string{"customer_id", "vendor_id", "customer_balance", "vendor_balance", "posted_at", "reversed_on", "reversal_date"} {
		if !serverSetFields[SetOffVoucherDoctype][k] {
			t.Errorf("%s is set by the server and must not be asked for on the form", k)
		}
	}
	for _, k := range []string{"pair_id", "amount", "voucher_date"} {
		if serverSetFields[SetOffVoucherDoctype][k] {
			t.Errorf("%s is the user's to fill", k)
		}
	}
}
