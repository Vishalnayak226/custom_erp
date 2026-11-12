package engines

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.28: a return's disposition can carry a restocking fee and draft the
// next document (credit note, replacement order, return to vendor, scrap).
// No rule on file = every return behaves exactly as before.

func stage14828Rule(t *testing.T, f stage1484Fixture, id string, data map[string]interface{}) {
	t.Helper()
	data["status"] = "Active"
	f.insert(id, "ReturnDispositionRule", "Active", "system", data)
}

// stage14828Sale checks out a cart and returns a one-unit return of sku through QC.
func stage14828Sale(t *testing.T, f stage1484Fixture, cart string) {
	t.Helper()
	f.cart(t, cart, "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, cart, ""); err != nil {
		t.Fatalf("checkout: %v", err)
	}
}

func stage14828QC(t *testing.T, f stage1484Fixture, cart, sku, disposition string) (returnID, refundID string) {
	t.Helper()
	returnID, err := CreateReturnRequest(f.tenantID, "Customer Return", "S1484", cart, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	if err := ApproveReturnRequest(f.tenantID, returnID, "approver"); err != nil {
		t.Fatal(err)
	}
	if err := ReceiveReturnRequest(f.tenantID, returnID, "store"); err != nil {
		t.Fatal(err)
	}
	_, refundID, err = ApplyReturnQC(f.tenantID, returnID, map[string]string{sku: disposition}, "qc")
	if err != nil {
		t.Fatalf("QC: %v", err)
	}
	return returnID, refundID
}

func stage14828Count(t *testing.T, f stage1484Fixture, doctype, where string) int {
	t.Helper()
	var n int
	q := "SELECT count(*) FROM " + f.schema + ".documents WHERE doctype = $1 AND deleted_at IS NULL"
	if where != "" {
		q += " AND " + where
	}
	if err := db.DB.QueryRow(q, doctype).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStage14828RestockingFeeComesOffTheRefund(t *testing.T) {
	f := stage1484Setup(t)
	if _, err := db.DB.Exec("INSERT INTO " + f.schema + ".gl_accounts (account_code, account_name, account_type) VALUES ('4170', 'Restocking fees', 'Revenue') ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	f.insert("TC-RESTOCK", "TradeChargeCode", "Active", "system", map[string]interface{}{"code": "RESTOCK", "charge_name": "Restocking fee", "used_for": "Selling", "gl_account": "4170", "status": "Active"})
	stage14828Rule(t, f, "RDR-1", map[string]interface{}{"disposition": "Sellable", "charge_code": "RESTOCK", "fee_percent": 10.0})
	stage14828Sale(t, f, "CART-14828-A")
	returnID, refundID := stage14828QC(t, f, "CART-14828-A", "SHOE1484", "Sellable")
	_, rf, _ := fetchRefundRequest(f.tenantID, refundID)
	if numFromInterface(rf["amount"]) != 90 || numFromInterface(rf["restocking_fee"]) != 10 {
		t.Fatalf("a 10%% fee on 100 leaves 90 to refund: %v", rf)
	}
	_ = ApproveRefundRequest(f.tenantID, refundID, "manager")
	if err := ProcessRefundRequest(f.tenantID, refundID, "manager", "Cash"); err != nil {
		t.Fatalf("process: %v", err)
	}
	got := stage1484Posted(t, f.schema, "RefundRequest", refundID)
	if got["D:4100"] != 10000 || got["C:1100"] != 9000 || got["C:4170"] != 1000 {
		t.Fatalf("revenue goes back whole, cash 90, the fee to its account: %v", got)
	}
	_, rr, _ := fetchReturnRequest(f.tenantID, returnID)
	if rr["status"] != "Closed" {
		t.Fatalf("return not closed: %v", rr["status"])
	}
}

func TestStage14828NoRuleNoFee(t *testing.T) {
	f := stage1484Setup(t)
	stage14828Rule(t, f, "RDR-2", map[string]interface{}{"disposition": "Damaged", "fee_percent": 50.0})
	stage14828Sale(t, f, "CART-14828-B")
	_, refundID := stage14828QC(t, f, "CART-14828-B", "SHOE1484", "Sellable")
	_, rf, _ := fetchRefundRequest(f.tenantID, refundID)
	if numFromInterface(rf["amount"]) != 100 || rf["restocking_fee"] != nil {
		t.Fatalf("no rule for Sellable: full refund, no fee: %v", rf)
	}
}

func TestStage14828CreditDispositionDraftsOneCreditNote(t *testing.T) {
	f := stage1484Setup(t)
	stage14828Rule(t, f, "RDR-3", map[string]interface{}{"disposition": "Damaged", "action": "Credit"})
	stage14828Sale(t, f, "CART-14828-C")
	returnID, refundID := stage14828QC(t, f, "CART-14828-C", "SHOE1484", "Damaged")
	if refundID != "" {
		t.Fatalf("a credit line is credited, not refunded: %s", refundID)
	}
	if n := stage14828Count(t, f, "CreditNote", "data->>'return_request_id' = '"+returnID+"' AND status = 'Draft'"); n != 1 {
		t.Fatalf("want one draft credit note, got %d", n)
	}
	// Drafting again does not draft again.
	if _, err := DraftReturnNextDocuments(f.tenantID, returnID, "qc"); err != nil {
		t.Fatal(err)
	}
	if n := stage14828Count(t, f, "CreditNote", "data->>'return_request_id' = '"+returnID+"'"); n != 1 {
		t.Fatalf("a return is never drafted twice, got %d notes", n)
	}
}

func TestStage14828ScrapGoesToQuarantineAndDraftsAnAdjustment(t *testing.T) {
	f := stage1484Setup(t)
	stage14828Rule(t, f, "RDR-4", map[string]interface{}{"disposition": "Sellable", "action": "Scrap"})
	stage14828Sale(t, f, "CART-14828-D")
	returnID, _ := stage14828QC(t, f, "CART-14828-D", "SHOE1484", "Sellable")
	if n := stage14828Count(t, f, "InventoryAdjustment", "data->>'return_request_id' = '"+returnID+"' AND status = 'Draft'"); n != 1 {
		t.Fatalf("want one draft adjustment, got %d", n)
	}
	var qcHold int
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(qc_hold),0) FROM " + f.schema + ".inventory_availability WHERE sku = 'SHOE1484' AND location_code = 'S1484'").Scan(&qcHold)
	if qcHold != 1 {
		t.Fatalf("scrapped unit sits in quarantine (qc_hold), got %d", qcHold)
	}
}

func TestStage14828ReturnToVendorDraftsAPurchaseReturn(t *testing.T) {
	f := stage1484Setup(t)
	f.grn(t, "GRN-14828", "V1")
	stage14828Rule(t, f, "RDR-5", map[string]interface{}{"disposition": "Damaged", "action": "Return to vendor"})
	stage14828Sale(t, f, "CART-14828-E")
	returnID, _ := stage14828QC(t, f, "CART-14828-E", "SHOE1484", "Damaged")
	if n := stage14828Count(t, f, "PurchaseReturn", "data->>'return_request_id' = '"+returnID+"' AND data->>'grn_id' = 'GRN-14828' AND status = 'Draft'"); n != 1 {
		t.Fatalf("want one draft purchase return on the GRN, got %d", n)
	}
}

func TestStage14828ReplaceOnATillBillIsLeftToAPerson(t *testing.T) {
	f := stage1484Setup(t)
	stage14828Rule(t, f, "RDR-6", map[string]interface{}{"disposition": "Damaged", "action": "Replace"})
	stage14828Sale(t, f, "CART-14828-F")
	returnID, _ := stage14828QC(t, f, "CART-14828-F", "SHOE1484", "Damaged")
	if n := stage14828Count(t, f, "SalesOrder", "data->>'return_request_id' = '"+returnID+"'"); n != 0 {
		t.Fatalf("a till bill has no delivery address: nothing drafted, got %d", n)
	}
	_, rr, _ := fetchReturnRequest(f.tenantID, returnID)
	if !strings.Contains(fmt.Sprint(rr["drafts"]), "no delivery address") {
		t.Fatalf("the line says why it was not drafted: %v", rr["drafts"])
	}
}

func TestStage14828ReplaceDraftsAnOrderAndNeverTwice(t *testing.T) {
	f := stage1484Setup(t)
	f.insert("SO-14828", "SalesOrder", "Pending", "system", map[string]interface{}{"code": "SO-14828", "customer_name": "Asha Rao", "shipping_address": "12 MG Road", "shipping_state": "Karnataka", "shipping_pincode": "560001", "status": "Pending"})
	f.insert("RR-14828-REPL", "ReturnRequest", "QC Complete", "system", map[string]interface{}{
		"code": "RR-14828-REPL", "status": "QC Complete", "original_order_id": "SO-14828", "return_location": "S1484",
		"items": []map[string]interface{}{{"sku": "SHOE1484", "qty": 1, "disposition": "Damaged", "next_action": "Replace"}},
	})
	for i := 0; i < 2; i++ {
		if _, err := DraftReturnNextDocuments(f.tenantID, "RR-14828-REPL", "qc"); err != nil {
			t.Fatal(err)
		}
	}
	if n := stage14828Count(t, f, "SalesOrder", "data->>'return_request_id' = 'RR-14828-REPL'"); n != 1 {
		t.Fatalf("want exactly one replacement order, got %d", n)
	}
}

func TestStage14828ReturnNeedsAReasonWhenSettingIsOn(t *testing.T) {
	f := stage1484Setup(t)
	if _, _, err := ValidateReturnReason(f.tenantID, "", ""); err != nil {
		t.Fatalf("setting off: no reason needed: %v", err)
	}
	if err := SetSetting(f.tenantID, "sales.return_reason_required", "true", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateReturnReason(f.tenantID, "", ""); err == nil || !strings.Contains(err.Error(), "needs a reason") {
		t.Fatalf("setting on: refused without a reason: %v", err)
	}
	if _, text, err := ValidateReturnReason(f.tenantID, "", "Too small"); err != nil || text != "Too small" {
		t.Fatalf("typed reason accepted: %q %v", text, err)
	}
	stage14828Sale(t, f, "CART-14828-R")
	returnID, _ := stage14828QC(t, f, "CART-14828-R", "SHOE1484", "Sellable")
	_, rr, _ := fetchReturnRequest(f.tenantID, returnID)
	if rr["rma_no"] == nil || rr["rma_no"] != rr["code"] {
		t.Fatalf("the return carries its RMA no.: %v vs %v", rr["rma_no"], rr["code"])
	}
}

func TestStage14828CreditNoteOnStockNeedsAPostedReceipt(t *testing.T) {
	f := stage1484Setup(t)
	note := func(id string, extra map[string]interface{}) {
		d := map[string]interface{}{"code": id, "note_number": id, "amount": 100, "reason": "Return", "status": "Draft", "items": []map[string]interface{}{{"sku": "TEE1484", "qty": 1}}}
		for k, v := range extra {
			d[k] = v
		}
		f.insert(id, "CreditNote", "Draft", "system", d)
	}
	note("CN-14828-OFF", nil)
	if _, err := PostCreditNote(f.tenantID, "CN-14828-OFF", "acct"); err != nil {
		t.Fatalf("setting off: posts as today: %v", err)
	}
	if err := SetSetting(f.tenantID, "sales.credit_note_needs_receipt", "true", "admin"); err != nil {
		t.Fatalf("setting must exist: %v", err)
	}
	note("CN-14828-NO", nil)
	if _, err := PostCreditNote(f.tenantID, "CN-14828-NO", "acct"); !hasCode(err, "SALESR-1316") {
		t.Fatalf("stock note with no return receipt must be refused: %v", err)
	}
	// A note with no stock lines (a price adjustment) is not asked for a receipt.
	note("CN-14828-PRICE", map[string]interface{}{"items": []map[string]interface{}{}})
	if _, err := PostCreditNote(f.tenantID, "CN-14828-PRICE", "acct"); err != nil {
		t.Fatalf("no stock lines: %v", err)
	}
	f.insert("RR-14828-REC", "ReturnRequest", "Received", "system", map[string]interface{}{"code": "RR-14828-REC", "status": "Received"})
	f.insert("RR-14828-QC", "ReturnRequest", "QC Complete", "system", map[string]interface{}{"code": "RR-14828-QC", "status": "QC Complete"})
	note("CN-14828-REC", map[string]interface{}{"return_request_id": "RR-14828-REC"})
	if _, err := PostCreditNote(f.tenantID, "CN-14828-REC", "acct"); !hasCode(err, "SALESR-1316") {
		t.Fatalf("goods received but not checked in yet: refused: %v", err)
	}
	note("CN-14828-QC", map[string]interface{}{"return_request_id": "RR-14828-QC"})
	if _, err := PostCreditNote(f.tenantID, "CN-14828-QC", "acct"); err != nil {
		t.Fatalf("receipt posted: %v", err)
	}
}

func hasCode(err error, code string) bool {
	var ve *ValidationError
	return errors.As(err, &ve) && ve.Code == code
}

// A scrapped return's write-off draws from quarantine (qc_hold), never from
// sellable stock, and books the unit's cost like any write-off.
func TestStage14828ScrapPostingTakesTheUnitOutOfQuarantine(t *testing.T) {
	f := stage1484Setup(t)
	stage14828Rule(t, f, "RDR-7", map[string]interface{}{"disposition": "Sellable", "action": "Scrap"})
	stage14828Sale(t, f, "CART-14828-G")
	// A second sellable unit that must stay untouched.
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".inventory_availability SET available = available + 4, on_hand = on_hand + 4 WHERE sku = 'SHOE1484' AND location_code = 'S1484'"); err != nil {
		t.Fatal(err)
	}
	returnID, _ := stage14828QC(t, f, "CART-14828-G", "SHOE1484", "Sellable")
	stock := func() (onHand, avail, qc int) {
		_ = db.DB.QueryRow("SELECT on_hand, available, COALESCE(qc_hold,0) FROM "+f.schema+".inventory_availability WHERE sku = 'SHOE1484' AND location_code = 'S1484'").Scan(&onHand, &avail, &qc)
		return
	}
	var adjID string
	if err := db.DB.QueryRow("SELECT id FROM "+f.schema+".documents WHERE doctype = 'InventoryAdjustment' AND data->>'return_request_id' = $1", returnID).Scan(&adjID); err != nil {
		t.Fatalf("draft adjustment: %v", err)
	}
	h0, a0, q0 := stock()
	if q0 != 1 {
		t.Fatalf("setup: one unit in quarantine, got %d", q0)
	}
	if err := PostInventoryAdjustment(f.tenantID, adjID, "tester"); err != nil {
		t.Fatalf("post: %v", err)
	}
	h1, a1, q1 := stock()
	if q1 != q0-1 || a1 != a0 || h1 != h0-1 {
		t.Fatalf("want qc_hold %d->%d, available unchanged at %d, on_hand %d->%d; got qc %d avail %d on_hand %d", q0, q0-1, a0, h0, h0-1, q1, a1, h1)
	}
	var loss float64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(debit),0)::float8 FROM "+f.schema+".gl_postings WHERE document_type = 'InventoryAdjustment' AND document_id = $1", adjID).Scan(&loss)
	if loss != 6000 {
		t.Fatalf("the write-off books the unit's cost (60 rupees = 6000 paise), got %v", loss)
	}
	var moved float64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM((data->>'qty')::float8),0) FROM "+f.schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = 'InventoryAdjustment' AND data->>'voucher_id' = (SELECT COALESCE(data->>'code', id) FROM "+f.schema+".documents WHERE id = $1)", adjID).Scan(&moved)
	if moved != -1 {
		t.Fatalf("the stock ledger shows -1, got %v", moved)
	}
	// Posting again changes nothing.
	_ = PostInventoryAdjustment(f.tenantID, adjID, "tester")
	if h2, a2, q2 := stock(); h2 != h1 || a2 != a1 || q2 != q1 {
		t.Fatalf("a second post moved stock")
	}
}
