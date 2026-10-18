package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 73.2.5 - hold and resume. One till resumes a held bill, a second is
// refused; a bill can be held again after resuming; a paid bill cannot be
// held; discarding needs a reason; the resumed bill posts under its number
// exactly once. Own throwaway tenant.
func TestStage73HoldAndResumeBill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "MUG", "name": "Mug", "sale_price": 500.0, "hsn_code": "6912", "tax_treatment": "Exempt"})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('MUG', 'Item', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := PostInventoryLedger(tenantID, "STORE-H", []interface{}{map[string]interface{}{"sku": "MUG", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	const cart = "POS-STORE-H-1"
	if _, err := HoldPOSBill(tenantID, "cashier-a", "system", HeldBill{CartNumber: cart, Location: "STORE-H"}); code(err) != "POSOFF-0609" {
		t.Errorf("an empty bill cannot be held: %v", err)
	}
	held, err := HoldPOSBill(tenantID, "cashier-a", "system", HeldBill{CartNumber: cart, Location: "STORE-H", CustomerID: "CUST-H",
		Lines: []HeldBillLine{{SKU: "MUG", Name: "Mug", Qty: 2}}, Total: 1000, Note: "Mr Rao, back in 5"})
	if err != nil || held.Status != "Held" || held.LineCount != 1 {
		t.Fatalf("hold: %v %+v", err, held)
	}
	if _, err := HoldPOSBill(tenantID, "cashier-a", "system", HeldBill{CartNumber: cart, Location: "STORE-H", Lines: []HeldBillLine{{SKU: "MUG", Qty: 1}}}); code(err) != "POSOFF-0609" {
		t.Errorf("holding the same bill twice: %v", err)
	}
	list, err := ListHeldPOSBills(tenantID, "STORE-H")
	if err != nil || len(list) != 1 || list[0].Note != "Mr Rao, back in 5" || list[0].Lines[0].Qty != 2 {
		t.Fatalf("list: %v %+v", err, list)
	}
	if other, _ := ListHeldPOSBills(tenantID, "STORE-OTHER"); len(other) != 0 {
		t.Errorf("held bills are listed per store")
	}
	if _, err := ResumePOSBill(tenantID, held.ID, "STORE-OTHER", "cashier-b"); code(err) != "POSOFF-0610" {
		t.Errorf("another store cannot resume it: %v", err)
	}
	back, err := ResumePOSBill(tenantID, held.ID, "STORE-H", "cashier-b")
	if err != nil || back.CartNumber != cart || back.CustomerID != "CUST-H" {
		t.Fatalf("resume: %v %+v", err, back)
	}
	if _, err := ResumePOSBill(tenantID, held.ID, "STORE-H", "cashier-c"); code(err) != "POSOFF-0610" {
		t.Errorf("a second till cannot resume the same bill: %v", err)
	}
	if again, err := HoldPOSBill(tenantID, "cashier-b", "system", HeldBill{CartNumber: cart, Location: "STORE-H", Lines: []HeldBillLine{{SKU: "MUG", Qty: 2}}}); err != nil || again.Status != "Held" {
		t.Errorf("a resumed bill can be held again: %v", err)
	}
	if err := DiscardPOSBill(tenantID, held.ID, "STORE-H", "cashier-b", ""); code(err) != "POSOFF-0609" {
		t.Errorf("discarding needs a reason: %v", err)
	}
	if _, err := ResumePOSBill(tenantID, held.ID, "STORE-H", "cashier-b"); err != nil {
		t.Fatal(err)
	}
	// The resumed bill posts under its number; it cannot then be held.
	cartData, _ := json.Marshal(map[string]interface{}{"location": "STORE-H", "payment_mode": "Cash", "items": []map[string]interface{}{{"sku": "MUG", "qty": 2, "sale_price": 500.0}}})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'system')", cart, cartData); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := HoldPOSBill(tenantID, "cashier-b", "system", HeldBill{CartNumber: cart, Location: "STORE-H", Lines: []HeldBillLine{{SKU: "MUG", Qty: 2}}}); code(err) != "POSOFF-0609" {
		t.Errorf("a paid bill cannot be held: %v", err)
	}
	if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
		t.Fatal(err)
	}
	var sold int
	_ = db.DB.QueryRow(`SELECT available FROM ` + schemaName + `.inventory_availability WHERE sku = 'MUG' AND location_code = 'STORE-H'`).Scan(&sold)
	if sold != 8 {
		t.Errorf("the bill posted once: available %d, want 8", sold)
	}
}

// Stage 73.2.6 - exchange with net settlement. A shirt (900 after the offer)
// comes back: its tax and points reverse and its money becomes an exchange
// credit. Like for like, the credit pays the new bill and the points are
// re-earned; cheaper, the rest is paid back from the till; dearer, the
// customer pays the difference. 2620 nets to zero; a credit is used once.
// Own throwaway tenant.
func TestStage73ExchangeAtTill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	gl := func(docType, docID, account string) int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit - credit), 0) FROM `+schemaName+`.gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3`, docType, docID, account).Scan(&v)
		return v
	}
	const loc, cust = "STORE-EX", "CUST-EX"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store EX", "type": "Store", "sellable": "Yes"})
	for code, price := range map[string]float64{"SHIRT": 1000, "SOCKS": 500, "COAT": 2000} {
		insert(code, "Item", "Active", map[string]interface{}{"code": code, "name": code, "sale_price": price, "hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5.0})
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": code, "qty": 10.0}}, false); err != nil {
			t.Fatal(err)
		}
	}
	insert("OFF-EX", "Offer", "Active", map[string]interface{}{"name": "Ten off", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10.0})
	sell := func(cart, credit string, lines []QuoteLineRequest) {
		t.Helper()
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, Lines: lines})
		if err != nil {
			t.Fatalf("quote %s: %v", cart, err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": cust,
			"items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown, "offer_discount": q.OfferDiscount, "exchange_credit_id": credit})
		if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
			t.Fatalf("finalize %s: %v", cart, err)
		}
	}
	exchange := func(origin string) *ExchangeCredit {
		t.Helper()
		c, err := StartTillExchange(tenantID, "cashier-x", ExchangeReturnRequest{OriginalOrderID: origin, Location: loc, Lines: []ReturnItemInput{{SKU: "SHIRT", Qty: 1}}})
		if err != nil {
			t.Fatalf("exchange %s: %v", origin, err)
		}
		return c
	}
	balance := func() int { b, _ := GetLoyaltyBalance(tenantID, cust); return b }

	sell("CART-EX-1", "", []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}}) // 900 cash, 9 points
	if balance() != 9 {
		t.Fatalf("earned on the shirt: %d, want 9", balance())
	}
	credit := exchange("CART-EX-1")
	if credit.Amount != 900 || credit.Status != "Open" {
		t.Fatalf("credit %+v, want 900 open", credit)
	}
	if balance() != 0 {
		t.Errorf("the returned shirt's points are reversed: balance %d", balance())
	}
	var taxBack int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM ` + schemaName + `.gl_postings WHERE document_type = 'ReturnRequest' AND account_code IN ('2200','2201')`).Scan(&taxBack)
	if taxBack == 0 {
		t.Errorf("the returned shirt's GST is reversed")
	}
	// Like for like: two socks, 900 after the offer, paid wholly by the credit.
	sell("CART-EX-2", credit.ID, []QuoteLineRequest{{Sku: "SOCKS", Qty: 2}})
	if got := gl("POSCart", "CART-EX-2", "1100"); got != 0 {
		t.Errorf("nothing more is taken in cash: 1100 net %d", got)
	}
	if got := gl("POSCart", "CART-EX-2", "2620"); got != 90000 {
		t.Errorf("the credit is the tender: 2620 debit %d, want 90000", got)
	}
	if balance() != 9 {
		t.Errorf("points re-earned on the new bill: balance %d, want 9", balance())
	}
	if rec, err := ReconcileSale(tenantID, "CART-EX-2"); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("the exchange sale reconciles: %v %+v", err, rec)
	}
	cartData, _ := json.Marshal(map[string]interface{}{"location": loc, "payment_mode": "Cash", "exchange_credit_id": credit.ID,
		"items": []map[string]interface{}{{"sku": "SOCKS", "qty": 1, "sale_price": 500.0}}})
	_, _ = db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('CART-EX-REUSE', 'POSCart', $1, 'Processing', 'system')", cartData)
	if _, _, err := FinalizePOSCheckout(tenantID, "CART-EX-REUSE", ""); err == nil {
		t.Errorf("a used credit cannot pay a second bill")
	}

	// Cheaper: one pair of socks (450): 450 is paid back from the till.
	sell("CART-EX-3", "", []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}})
	credit2 := exchange("CART-EX-3")
	sell("CART-EX-4", credit2.ID, []QuoteLineRequest{{Sku: "SOCKS", Qty: 1}})
	if got := gl("ExchangeCredit", credit2.ID, "1100"); got != -45000 {
		t.Errorf("the rest of the credit goes back in cash: 1100 %d, want -45000", got)
	}
	// Dearer: a coat (1800 after the offer): the customer pays 900 more.
	sell("CART-EX-5", "", []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}})
	credit3 := exchange("CART-EX-5")
	sell("CART-EX-6", credit3.ID, []QuoteLineRequest{{Sku: "COAT", Qty: 1}})
	if got := gl("POSCart", "CART-EX-6", "1100"); got != 90000 {
		t.Errorf("the difference is paid in cash: 1100 %d, want 90000", got)
	}
	var clearing int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit - credit), 0) FROM ` + schemaName + `.gl_postings WHERE account_code = '2620'`).Scan(&clearing)
	if clearing != 0 {
		t.Errorf("2620 Exchange Credit Clearing nets to zero after every exchange completes: %d", clearing)
	}
}

// Stage 73.2.10 - issued vouchers at the till: priced in the quote after the
// offers, posted net, spent with the sale and only then; a used-up, expired,
// another customer's or online-only voucher is refused with its reason; a
// voucher used up between quote and checkout stops the sale. Own tenant.
func TestStage73VouchersAtTill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	const loc = "STORE-V"
	insert("SHIRT", "Item", "Active", map[string]interface{}{"code": "SHIRT", "name": "Shirt", "sale_price": 1000.0, "hsn_code": "6109", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "SHIRT", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert("V1", "Voucher", "Active", map[string]interface{}{"code": "FLAT100", "discount_type": "Flat", "discount_value": 100.0, "max_uses": 1.0, "status": "Active"})
	insert("V2", "Voucher", "Active", map[string]interface{}{"code": "VIP10", "discount_type": "Percentage", "discount_value": 10.0, "customer_id": "CUST-VIP", "status": "Active"})
	insert("V3", "Voucher", "Active", map[string]interface{}{"code": "WEBONLY", "discount_type": "Flat", "discount_value": 50.0, "channel": "Online", "status": "Active"})
	insert("V4", "Voucher", "Active", map[string]interface{}{"code": "ONCE", "discount_type": "Flat", "discount_value": 30.0, "max_uses": 1.0, "status": "Active"})
	quote := func(cust string, codes ...string) *QuoteResult {
		t.Helper()
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, CouponCodes: codes, Lines: []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}}})
		if err != nil {
			t.Fatalf("quote: %v", err)
		}
		return q
	}
	sell := func(cart string, q *QuoteResult, cust string) error {
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": cust,
			"items": POSCartItemsFromQuote(q), "offer_discount": q.OfferDiscount, "applied_offers": q.AppliedOffers})
		_, _, err := FinalizePOSCheckout(tenantID, cart, "")
		return err
	}
	q := quote("", "flat100")
	if q.Total != 900 || len(q.AppliedOffers) != 1 || q.AppliedOffers[0].OfferType != OfferTypeVoucher || len(q.UnmatchedCodes) != 0 {
		t.Fatalf("a flat 100 voucher on a 1000 bill: total %.2f, %+v unmatched %v", q.Total, q.AppliedOffers, q.UnmatchedCodes)
	}
	if err := sell("CART-V1", q, ""); err != nil {
		t.Fatalf("sell: %v", err)
	}
	var revenue int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(credit), 0) FROM ` + schemaName + `.gl_postings WHERE document_id = 'CART-V1' AND account_code = '4100'`).Scan(&revenue)
	if revenue != 90000 {
		t.Errorf("the voucher reaches the books: revenue %d, want 90000", revenue)
	}
	var status string
	_ = db.DB.QueryRow(`SELECT status FROM ` + schemaName + `.documents WHERE id = 'V1'`).Scan(&status)
	if status != "Exhausted" {
		t.Errorf("a one-use voucher is spent by its sale: %q", status)
	}
	if q2 := quote("", "FLAT100"); q2.Total != 1000 || q2.CouponMessages["FLAT100"] == "" {
		t.Errorf("a used-up voucher gives nothing and says why: %.2f %v", q2.Total, q2.CouponMessages)
	}
	if q3 := quote("CUST-OTHER", "VIP10"); q3.Total != 1000 || q3.CouponMessages["VIP10"] == "" {
		t.Errorf("another customer's voucher is refused with a reason: %v", q3.CouponMessages)
	}
	if q4 := quote("CUST-VIP", "VIP10"); q4.Total != 900 {
		t.Errorf("the customer's own 10%% voucher: %.2f, want 900", q4.Total)
	}
	if q5 := quote("", "WEBONLY"); q5.Total != 1000 || q5.CouponMessages["WEBONLY"] == "" {
		t.Errorf("an online-only voucher is refused at the till: %v", q5.CouponMessages)
	}
	// Quoted, then used up elsewhere before the sale completes.
	q6 := quote("", "ONCE")
	if _, err := RedeemVoucher(tenantID, "ONCE", "", "ELSEWHERE", 500); err != nil {
		t.Fatal(err)
	}
	if err := sell("CART-V2", q6, ""); err == nil {
		t.Errorf("a voucher used up since the quote stops the sale")
	} else if ve, ok := err.(*ValidationError); !ok || ve.Code != "POSOFF-0613" {
		t.Errorf("used-up voucher at checkout: %v, want POSOFF-0613", err)
	}
	var posted int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.gl_postings WHERE document_id = 'CART-V2'`).Scan(&posted)
	if posted != 0 {
		t.Errorf("a refused sale posts nothing: %d rows", posted)
	}
}

// Stage 73.2.7 - the till's cash over a day: a cash sale, a cash refund at
// the store, Paid In, Paid Out and a Safe Drop reconcile to the count; a count
// off by more than the tolerance waits for a manager (approved -> Closed,
// rejected -> Open for a recount); a handover closes one session against the
// count and opens the next with it as float. Own throwaway tenant.
func TestStage73TillCashMovements(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	const loc = "STORE-CM"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store CM", "type": "Store", "sellable": "Yes"})
	insert("MUG", "Item", "Active", map[string]interface{}{"code": "MUG", "name": "Mug", "sale_price": 500.0, "hsn_code": "6912", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "MUG", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert("CM-FLOAT", "ReasonCode", "Active", map[string]interface{}{"code": "CM-FLOAT", "category": "Cash Movement", "status": "Active"})
	insert("CM-WRONG", "ReasonCode", "Active", map[string]interface{}{"code": "CM-WRONG", "category": "Return", "status": "Active"})

	sess, err := OpenPOSSession(tenantID, "", loc, "cashier-a", "system", 1000)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	insert("CART-CM", "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "pos_session": sess,
		"items": []map[string]interface{}{{"sku": "MUG", "qty": 1, "sale_price": 500.0}}})
	if _, _, err := FinalizePOSCheckout(tenantID, "CART-CM", ""); err != nil {
		t.Fatalf("sale: %v", err)
	}
	for _, m := range []struct {
		kind   string
		amount float64
	}{{CashPaidIn, 200}, {CashPaidOut, 50}, {CashSafeDrop, 1000}} {
		if _, err := RecordPOSCashMovement(tenantID, sess, "cashier-a", "system", m.kind, m.amount, "CM-FLOAT", "bag 7"); err != nil {
			t.Fatalf("%s: %v", m.kind, err)
		}
	}
	if _, err := RecordPOSCashMovement(tenantID, sess, "cashier-a", "system", CashSafeDrop, 10000, "CM-FLOAT", ""); code(err) != "POSOFF-0607" {
		t.Errorf("taking out more than the drawer holds: %v, want POSOFF-0607", err)
	}
	if _, err := RecordPOSCashMovement(tenantID, sess, "cashier-a", "system", CashPaidIn, 10, "CM-WRONG", ""); code(err) != "POSOFF-0607" {
		t.Errorf("a reason of another category: %v, want POSOFF-0607", err)
	}
	if _, err := RecordPOSCashMovement(tenantID, sess, "cashier-b", "system", CashPaidIn, 10, "CM-FLOAT", ""); code(err) != "POSOFF-0608" {
		t.Errorf("someone else's session: %v, want POSOFF-0608", err)
	}
	var paidOut int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM ` + schemaName + `.gl_postings WHERE document_type = 'POSCashMovement' AND account_code = '5290'`).Scan(&paidOut)
	if paidOut != 5000 {
		t.Errorf("a Paid Out posts to 5290: %d paise, want 5000", paidOut)
	}
	// A cash refund at the store comes out of this drawer.
	returnID, _ := CreateReturnRequest(tenantID, "Customer Return", loc, "CART-CM", "", "tester", []ReturnItemInput{{SKU: "MUG", Qty: 1}})
	_ = ApproveReturnRequest(tenantID, returnID, "approver")
	_ = ReceiveReturnRequest(tenantID, returnID, "store")
	_, refundID, err := ApplyReturnQC(tenantID, returnID, map[string]string{"MUG": "Sellable"}, "qc")
	if err != nil {
		t.Fatal(err)
	}
	_ = ApproveRefundRequest(tenantID, refundID, "manager")
	if err := ProcessRefundRequest(tenantID, refundID, "manager", "Cash"); err != nil {
		t.Fatalf("refund: %v", err)
	}
	pos, err := SessionCashPosition(tenantID, sess)
	if err != nil {
		t.Fatal(err)
	}
	// 1000 float + 500 sale - 500 refund + 200 in - 50 out - 1000 to the safe.
	if pos.CashSales != 500 || pos.CashRefunds != 500 || pos.PaidIn != 200 || pos.PaidOut != 50 || pos.SafeDrops != 1000 || pos.Expected != 150 {
		t.Fatalf("drawer: %+v, want expected 150", pos)
	}
	res, err := ClosePOSSession(tenantID, sess, "cashier-a", 150, "")
	if err != nil || res.Status != "Closed" || res.Variance != 0 {
		t.Fatalf("a count that matches closes: %v %+v", err, res)
	}

	// Over the tolerance (50 as shipped): a reason, then a manager.
	sess2, _ := OpenPOSSession(tenantID, "", loc, "cashier-b", "system", 500)
	if _, err := ClosePOSSession(tenantID, sess2, "cashier-b", 300, ""); code(err) != "POSOFF-0240" {
		t.Fatalf("200 short with no reason: %v, want POSOFF-0240", err)
	}
	res, err = ClosePOSSession(tenantID, sess2, "cashier-b", 300, "note miscounted")
	if err != nil || res.Status != "Pending Approval" || res.RequiredApprover != "Store Manager" {
		t.Fatalf("200 short with a reason waits for a manager: %v %+v", err, res)
	}
	if id, _ := GetOpenSessionForCashier(tenantID, loc, "cashier-b"); id != "" {
		t.Errorf("nothing can be sold on a session waiting for approval")
	}
	if err := DecideApproval(tenantID, "POSSession", sess2, "manager-cm", "Store Manager", loc, "Rejected", "count again"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if id, _ := GetOpenSessionForCashier(tenantID, loc, "cashier-b"); id != sess2 {
		t.Fatalf("a rejected close reopens the session for a recount")
	}
	if res, err = ClosePOSSession(tenantID, sess2, "cashier-b", 300, "really 200 short"); err != nil || res.Status != "Pending Approval" {
		t.Fatalf("recount: %v %+v", err, res)
	}
	if err := DecideApproval(tenantID, "POSSession", sess2, "manager-cm", "Store Manager", loc, "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var st string
	_ = db.DB.QueryRow(`SELECT status FROM `+schemaName+`.documents WHERE id = $1`, sess2).Scan(&st)
	if st != "Closed" {
		t.Errorf("an approved close is Closed, got %q", st)
	}

	// Shift handover.
	sess3, _ := OpenPOSSession(tenantID, "", loc, "cashier-c", "system", 400)
	prev, sess4, err := TakeOverPOSSession(tenantID, loc, "cashier-d", "system", 400, "")
	if err != nil || prev.Status != "Closed" || sess4 == "" {
		t.Fatalf("takeover: %v %+v %q", err, prev, sess4)
	}
	var to, from string
	var opening float64
	_ = db.DB.QueryRow(`SELECT data->>'handed_over_to' FROM `+schemaName+`.documents WHERE id = $1`, sess3).Scan(&to)
	_ = db.DB.QueryRow(`SELECT data->>'taken_over_from', (data->>'opening_cash')::numeric FROM `+schemaName+`.documents WHERE id = $1`, sess4).Scan(&from, &opening)
	if to != "cashier-d" || from != sess3 || opening != 400 {
		t.Errorf("handover links the sessions and carries the count: to %q from %q float %v", to, from, opening)
	}
	if _, _, err := TakeOverPOSSession(tenantID, loc, "cashier-d", "system", 400, ""); code(err) != "POSOFF-0608" {
		t.Errorf("taking over your own session: %v, want POSOFF-0608", err)
	}
}
