package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 73.1.1-73.1.3 - a bill with a bill-level offer and redeemed points,
// returned in pieces. The customer paid gross - offer - points; the sale must
// post that, each return must refund its units' share of it (money as money,
// points as points), and the points the returned money earned come back off.
// Own throwaway tenant schema.
func TestStage73POSReturnAllocation(t *testing.T) {
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
	const loc, customer = "STORE-73", "CUST-73"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store 73", "type": "Store", "sellable": "Yes"})
	for code, price := range map[string]float64{"SHIRT": 1000, "SOCKS": 500} {
		insert(code, "Item", "Active", map[string]interface{}{"code": code, "name": code, "sale_price": price,
			"hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 18.0})
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": code, "qty": 20.0}}, false); err != nil {
			t.Fatalf("stock %s: %v", code, err)
		}
	}
	insert("OFF-73", "Offer", "Active", map[string]interface{}{"name": "Ten off the bill", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10.0})
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, customer, "Earn", 300, "Seed", "SEED-73", nil); err != nil {
		t.Fatalf("seed points: %v", err)
	}
	gl := func(docType, docID, account, side string) int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(`+side+`), 0) FROM `+schemaName+`.gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3`, docType, docID, account).Scan(&v)
		return v
	}
	balance := func(cust string) int {
		b, err := GetLoyaltyBalance(tenantID, cust)
		if err != nil {
			t.Fatalf("balance: %v", err)
		}
		return b
	}
	sell := func(cart, cust string, redeem int, lines []QuoteLineRequest) *QuoteResult {
		t.Helper()
		quote, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, Lines: lines})
		if err != nil {
			t.Fatalf("quote %s: %v", cart, err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{
			"location": loc, "payment_mode": "Cash", "customer_id": cust, "redeem_points": redeem,
			"items": POSCartItemsFromQuote(quote), "gst_breakdown": quote.GSTBreakdown,
			"applied_offers": quote.AppliedOffers, "offer_discount": quote.OfferDiscount,
		})
		if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
			t.Fatalf("FinalizePOSCheckout %s: %v", cart, err)
		}
		return quote
	}
	// returnAndRefund walks a return through approve, receive, QC and the
	// refund; it returns the money QC sized and the refund's id.
	returnAndRefund := func(cart string, items []ReturnItemInput) (float64, string, string) {
		t.Helper()
		returnID, err := CreateReturnRequest(tenantID, "Customer Return", loc, cart, "", "tester", items)
		if err != nil {
			t.Fatalf("CreateReturnRequest: %v", err)
		}
		if err := ApproveReturnRequest(tenantID, returnID, "approver"); err != nil {
			t.Fatalf("approve: %v", err)
		}
		if err := ReceiveReturnRequest(tenantID, returnID, "store"); err != nil {
			t.Fatalf("receive: %v", err)
		}
		dispositions := map[string]string{}
		for _, it := range items {
			dispositions[it.SKU] = "Sellable"
		}
		refund, refundID, err := ApplyReturnQC(tenantID, returnID, dispositions, "qc")
		if err != nil {
			t.Fatalf("QC: %v", err)
		}
		if err := ApproveRefundRequest(tenantID, refundID, "manager"); err != nil {
			t.Fatalf("approve refund: %v", err)
		}
		if err := ProcessRefundRequest(tenantID, refundID, "manager", "Cash"); err != nil {
			t.Fatalf("process refund: %v", err)
		}
		return refund, refundID, returnID
	}

	// 2 shirts + 2 socks = 3000; 10% off the bill = 300; 270 points (Rs 270).
	const cart = "CART-73-OFFER"
	quote := sell(cart, customer, 270, []QuoteLineRequest{{Sku: "SHIRT", Qty: 2}, {Sku: "SOCKS", Qty: 2}})
	if quote.Subtotal != 3000 || quote.OfferDiscount != 300 || quote.Total != 2700 {
		t.Fatalf("quote subtotal %.2f offer %.2f total %.2f, want 3000 / 300 / 2700", quote.Subtotal, quote.OfferDiscount, quote.Total)
	}
	if quote.Lines[0].OfferShare != 200 || quote.Lines[0].NetTotal != 1800 || quote.Lines[1].OfferShare != 100 {
		t.Errorf("offer shares by line value: %+v", quote.Lines)
	}

	// --- 73.1.1: the sale posts what was actually paid -------------------------
	if got := gl("POSCart", cart, "4100", "credit"); got != 270000 {
		t.Errorf("revenue credited %d paise, want 270000 (3000 less the 300 offer)", got)
	}
	if got := gl("POSCart", cart, "1100", "debit"); got != 243000 {
		t.Errorf("cash debited %d paise, want 243000 (2700 less 270 paid in points)", got)
	}
	if got := gl("POSCart", cart, "5250", "debit"); got != 27000 {
		t.Errorf("points tender %d paise, want 27000", got)
	}
	// GST is backed out of what the customer paid: 2700 / 1.18 -> 411.86 tax.
	if tax := gl("POSCart", cart, "2200", "credit") + gl("POSCart", cart, "2201", "credit"); tax < 41180 || tax > 41190 {
		t.Errorf("output GST %d paise, want ~41186 (on 2700, not on 3000)", tax)
	}
	var stored []SaleLineAllocation
	var raw string
	_ = db.DB.QueryRow(`SELECT data->>'allocation' FROM `+schemaName+`.documents WHERE id = $1`, cart).Scan(&raw)
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || len(stored) != 2 {
		t.Fatalf("allocation stored on the sale: %v %q", err, raw)
	}
	var sumNet, sumOffer, sumTender int64
	sumPoints := 0
	for _, a := range stored {
		sumNet += a.NetPaise
		sumOffer += a.OfferPaise
		sumPoints += a.Points
		for _, v := range a.Tenders {
			sumTender += v
		}
	}
	if sumNet != 270000 || sumOffer != 30000 || sumPoints != 270 || sumTender != 270000 {
		t.Errorf("allocation must add back to the bill: net %d offer %d points %d tenders %d", sumNet, sumOffer, sumPoints, sumTender)
	}
	if rec, err := ReconcileSale(tenantID, cart); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("sale reconciliation: %v %+v", err, rec)
	}
	// Earned on the money paid: 2430 / 100 per point = 24.
	var earned int
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(points), 0) FROM `+schemaName+`.loyalty_point_ledger WHERE reference_id = $1 AND transaction_type = 'Earn'`, cart).Scan(&earned)
	if earned != 24 {
		t.Fatalf("points earned on the sale = %d, want 24 (on 2430 paid in money)", earned)
	}
	if got := balance(customer); got != 300-270+24 {
		t.Fatalf("balance after the sale = %d, want 54", got)
	}

	// --- return one shirt ---------------------------------------------------------
	// Shirt line: gross 2000, offer share 200, net 1800 (900 a unit); points
	// share 270 x 1800/2700 = 180 (90 a unit); so the unit refunds 810 in money.
	refund, refundID, returnID := returnAndRefund(cart, []ReturnItemInput{{SKU: "SHIRT", Qty: 1}})
	if refund != 810 {
		t.Fatalf("money refund for one shirt = %.2f, want 810 (its 900 net share less 90 paid in points)", refund)
	}
	if got := gl("RefundRequest", refundID, "1100", "credit"); got != 81000 {
		t.Errorf("cash refunded %d paise, want 81000", got)
	}
	if got := gl("RefundRequest", refundID, "5250", "credit"); got != 9000 {
		t.Errorf("points tender reversed %d paise, want 9000", got)
	}
	if got := gl("RefundRequest", refundID, "4100", "debit"); got != 90000 {
		t.Errorf("revenue reversed %d paise, want 90000 (the unit's net share)", got)
	}
	if rec, err := ReconcileReturn(tenantID, returnID); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("return reconciliation: %v %+v", err, rec)
	}
	// 73.1.3: the 90 points come back as points, valid for at least the grace.
	var restored int
	var restoredExpiry time.Time
	_ = db.DB.QueryRow(`SELECT points, expires_at FROM `+schemaName+`.loyalty_point_ledger WHERE reference_doctype = 'RefundRequest' AND reference_id = $1 AND transaction_type = 'Earn'`, refundID).Scan(&restored, &restoredExpiry)
	if restored != 90 || restoredExpiry.Before(time.Now().AddDate(0, 0, 29)) {
		t.Errorf("restored points = %d expiring %v, want 90 valid for 30 days", restored, restoredExpiry)
	}
	// 73.1.2: 810 of 2430 money came back, so 24 x 810/2430 = 8 earned points go.
	var reversed int
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(points), 0) FROM `+schemaName+`.loyalty_point_ledger WHERE reference_id = $1 AND transaction_type = $2`, refundID, LoyaltyTxnReversal).Scan(&reversed)
	if reversed != 8 {
		t.Errorf("earned points reversed = %d, want 8", reversed)
	}
	if got := balance(customer); got != 54+90-8 {
		t.Errorf("balance after the first return = %d, want 136", got)
	}

	// --- return the rest: exact to the paisa and the point ---------------------
	refund2, refundID2, _ := returnAndRefund(cart, []ReturnItemInput{{SKU: "SHIRT", Qty: 1}, {SKU: "SOCKS", Qty: 2}})
	if refund2 != 1620 {
		t.Errorf("second refund = %.2f, want 1620 (810 shirt + 810 socks)", refund2)
	}
	if total := gl("RefundRequest", refundID, "1100", "credit") + gl("RefundRequest", refundID2, "1100", "credit"); total != 243000 {
		t.Errorf("cash refunded in all %d, want exactly the 243000 the customer paid", total)
	}
	if total := gl("RefundRequest", refundID, "4100", "debit") + gl("RefundRequest", refundID2, "4100", "debit"); total != 270000 {
		t.Errorf("revenue reversed in all %d, want exactly the 270000 posted", total)
	}
	var allReversed, allRestored int
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(points), 0) FROM `+schemaName+`.loyalty_point_ledger WHERE customer_id = $1 AND transaction_type = $2`, customer, LoyaltyTxnReversal).Scan(&allReversed)
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(points), 0) FROM `+schemaName+`.loyalty_point_ledger WHERE customer_id = $1 AND transaction_type = 'Earn' AND reference_doctype = 'RefundRequest'`, customer).Scan(&allRestored)
	if allReversed != 24 || allRestored != 270 {
		t.Errorf("whole bill back: reversed %d (want all 24 earned), restored %d (want all 270 redeemed)", allReversed, allRestored)
	}
	if got := balance(customer); got != 300 {
		t.Errorf("a wholly returned bill leaves the customer where they started: balance %d, want 300", got)
	}
	if _, err := CreateReturnRequest(tenantID, "Customer Return", loc, cart, "", "tester", []ReturnItemInput{{SKU: "SOCKS", Qty: 1}}); err == nil {
		t.Errorf("nothing is left to return on the bill")
	}

	// --- D03: earned points already spent - the balance goes negative ---------
	const spender = "CUST-73-SPENT"
	sell("CART-73-SPENT", spender, 0, []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}}) // 900 paid, 9 earned
	if got := balance(spender); got != 9 {
		t.Fatalf("spender earned %d, want 9", got)
	}
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, spender, "Burn", 9, "POSCart", "ELSEWHERE", nil); err != nil {
		t.Fatal(err)
	}
	refund3, _, _ := returnAndRefund("CART-73-SPENT", []ReturnItemInput{{SKU: "SHIRT", Qty: 1}})
	if refund3 != 900 {
		t.Errorf("the cash refund is never cut to cover spent points: %.2f, want 900", refund3)
	}
	if got := balance(spender); got != -9 {
		t.Errorf("spent-then-returned balance = %d, want -9 (future earnings pay it back)", got)
	}

	// --- a bill sold before 73.1.1: rebuilt from what it recorded ---------------
	insert("CART-73-LEGACY", "POSCart", "Paid", map[string]interface{}{
		"location": loc, "payment_mode": "Cash", "customer_id": "CUST-73-OLD", "offer_discount": 100.0,
		"items": []map[string]interface{}{{"sku": "SOCKS", "qty": 2, "sale_price": 500.0}},
	})
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, "CUST-73-OLD", "Burn", 50, "POSCart", "CART-73-LEGACY", nil); err != nil {
		t.Fatal(err)
	}
	// 1000 - 100 offer = 900 net, 50 in points: one sock is 450 net, 425 money.
	if refund4, _, _ := returnAndRefund("CART-73-LEGACY", []ReturnItemInput{{SKU: "SOCKS", Qty: 1}}); refund4 != 425 {
		t.Errorf("legacy bill refund = %.2f, want 425", refund4)
	}

	// --- a bill an offer makes free posts its cost and no revenue ---------------
	insert("OFF-73-FREE", "Offer", "Active", map[string]interface{}{"name": "Everything free", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 100.0, "priority": -1.0, "stackable": "No"})
	free := sell("CART-73-FREE", "", 0, []QuoteLineRequest{{Sku: "SOCKS", Qty: 1}})
	if free.Total != 0 || gl("POSCart", "CART-73-FREE", "4100", "credit") != 0 {
		t.Errorf("free bill: total %.2f, revenue %d", free.Total, gl("POSCart", "CART-73-FREE", "4100", "credit"))
	}
}

// Stage 73.1.1 - the cap on points at the till works in points: a customer
// redeeming more than the net bill keeps the points that did not fit, and at
// Rs 2 a point that is half as many points as rupees.
func TestStage73RedemptionCapReturnsPoints(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if err := SetSetting(tenantID, "loyalty.redemption_value_per_point", "2", "tester"); err != nil {
		t.Fatalf("set point value: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "MUG", "name": "Mug", "sale_price": 300.0, "hsn_code": "6912", "tax_treatment": "Exempt"})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('MUG', 'Item', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := PostInventoryLedger(tenantID, "STORE-CAP", []interface{}{map[string]interface{}{"sku": "MUG", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, "CUST-CAP", "Earn", 500, "Seed", "SEED", nil); err != nil {
		t.Fatal(err)
	}
	cartData, _ := json.Marshal(map[string]interface{}{"location": "STORE-CAP", "payment_mode": "Cash", "customer_id": "CUST-CAP", "redeem_points": 200,
		"items": []map[string]interface{}{{"sku": "MUG", "qty": 1, "sale_price": 300.0}}})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('CART-CAP', 'POSCart', $1, 'Processing', 'system')", cartData); err != nil {
		t.Fatal(err)
	}
	out, err := FinalizePOSCheckoutCommitted(tenantID, "CART-CAP", "", "", nil)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	// 200 points = Rs 400 against a Rs 300 bill: 150 points pay it, 50 go back.
	if out.LoyaltyDiscount != 300 {
		t.Errorf("points paid %d, want 300", out.LoyaltyDiscount)
	}
	if b, _ := GetLoyaltyBalance(tenantID, "CUST-CAP"); b != 350 {
		t.Errorf("balance = %d, want 350 (500 less the 150 points used)", b)
	}
}

// Stage 73.1.4 / 73.1.5 - the points side of the quote, and the redemption
// controls at the one place points burn: the daily limit, the customer's
// one-time code, and a manager's approval above the approval value (the
// shipped LoyaltyRedemptionRequest rule: 500 and over, Store Manager). Each
// authorisation is spent by the sale it was given for. Own throwaway tenant.
func TestStage73RedemptionControls(t *testing.T) {
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
	const loc, cust = "STORE-RC", "CUST-RC"
	insert("LAMP", "Item", "Active", map[string]interface{}{"code": "LAMP", "name": "Lamp", "sale_price": 1000.0, "hsn_code": "9405", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "LAMP", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert("RC-GOODWILL", "ReasonCode", "Active", map[string]interface{}{"code": "RC-GOODWILL", "category": "Loyalty Redemption", "status": "Active"})
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, cust, "Earn", 5000, "Seed", "SEED", nil); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting(tenantID, "loyalty.redemption_value_per_point", "2", "tester"); err != nil {
		t.Fatal(err)
	}
	cart := func(id string, points int, auth LoyaltyRedemptionAuth) {
		t.Helper()
		insert(id, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash", "customer_id": cust,
			"redeem_points": points, "redemption_challenge_id": auth.ChallengeID, "redemption_approval_id": auth.ApprovalID,
			"items": []map[string]interface{}{{"sku": "LAMP", "qty": 1, "sale_price": 1000.0}}})
	}
	finalize := func(id string) error {
		_, err := FinalizePOSCheckoutCommitted(tenantID, id, "", "", nil)
		return err
	}
	balance := func() int {
		b, _ := GetLoyaltyBalance(tenantID, cust)
		return b
	}

	// --- 73.1.5: the quote prices points at the tenant's value ---------------
	q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, RedeemPoints: 900, Lines: []QuoteLineRequest{{Sku: "LAMP", Qty: 1}}})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if q.Loyalty == nil || q.Loyalty.ValuePerPoint != 2 || q.Loyalty.MaxPoints != 500 || q.Loyalty.Points != 500 || q.Loyalty.Value != 1000 || q.AmountDue != 0 {
		t.Fatalf("a Rs 1000 bill at Rs 2 a point takes at most 500 points: %+v due %.2f", q.Loyalty, q.AmountDue)
	}
	if !q.Loyalty.NeedsApproval || q.Loyalty.ApproverRole != "Store Manager" {
		t.Errorf("Rs 1000 of points is over the 500 approval rule: %+v", q.Loyalty)
	}
	small, _ := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, RedeemPoints: 100, Lines: []QuoteLineRequest{{Sku: "LAMP", Qty: 1}}})
	none, _ := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, Lines: []QuoteLineRequest{{Sku: "LAMP", Qty: 1}}})
	if small.AmountDue != 800 || small.Loyalty.NeedsApproval || small.Version == none.Version {
		t.Errorf("100 points = Rs 200 off, under the rule, and a different quote version: due %.2f %+v", small.AmountDue, small.Loyalty)
	}
	if none.Loyalty == nil || none.Loyalty.Balance != 5000 || none.Loyalty.Points != 0 || none.AmountDue != 1000 {
		t.Errorf("a customer's quote shows their balance even when nothing is redeemed: %+v", none.Loyalty)
	}

	// --- 73.1.4: approval above the value --------------------------------------
	cart("CART-RC-1", 300, LoyaltyRedemptionAuth{}) // 300 x Rs 2 = 600
	if c := code(finalize("CART-RC-1")); c != "CUSTOM-0603" {
		t.Fatalf("600 of points with no approval: %q, want CUSTOM-0603", c)
	}
	if _, err := StartTillLoyaltyRedemption(tenantID, cust, 300, "CART-RC-2", loc, "", "system", "Cashier"); err == nil {
		t.Errorf("an approval request needs a reason")
	}
	tr, err := StartTillLoyaltyRedemption(tenantID, cust, 300, "CART-RC-2", loc, "RC-GOODWILL", "system", "Cashier")
	if err != nil || !tr.NeedsApproval || tr.ApprovalID == "" || tr.ApprovalStatus != "Pending Approval" {
		t.Fatalf("start: %v %+v", err, tr)
	}
	if again, _ := StartTillLoyaltyRedemption(tenantID, cust, 300, "CART-RC-2", loc, "RC-GOODWILL", "system", "Cashier"); again == nil || again.ApprovalID != tr.ApprovalID {
		t.Errorf("asking again for the same cart returns the same request")
	}
	if err := DecideApproval(tenantID, "LoyaltyRedemptionRequest", tr.ApprovalID, "manager-rc", "Store Manager", loc, "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if balance() != 5000 {
		t.Errorf("approving a till request burns nothing - the sale does: balance %d", balance())
	}
	cart("CART-RC-2", 300, LoyaltyRedemptionAuth{ApprovalID: tr.ApprovalID})
	if err := finalize("CART-RC-2"); err != nil {
		t.Fatalf("approved redemption: %v", err)
	}
	if balance() != 5000-300+4 {
		t.Errorf("balance after the approved sale = %d, want 4704 (300 spent, 4 earned on the Rs 400 paid in money)", balance())
	}
	if st, _ := TillRedemptionApprovalStatus(tenantID, tr.ApprovalID); st != "Redeemed" {
		t.Errorf("the request is spent by its sale: %q", st)
	}
	cart("CART-RC-3", 300, LoyaltyRedemptionAuth{ApprovalID: tr.ApprovalID})
	if c := code(finalize("CART-RC-3")); c != "CUSTOM-0603" {
		t.Errorf("an approval spends once - reusing it on another cart: %q", c)
	}

	// --- 73.1.4: the one-time code ---------------------------------------------
	if err := SetSetting(tenantID, "loyalty.redemption_requires_otp", "true", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := RedeemLoyaltyPoints(tenantID, cust, 10, "API-RC"); code(err) != "CUSTOM-0602" {
		t.Errorf("the API burn is held to the same controls: %v", err)
	}
	cart("CART-RC-4", 50, LoyaltyRedemptionAuth{})
	if c := code(finalize("CART-RC-4")); c != "CUSTOM-0602" {
		t.Fatalf("no code: %q, want CUSTOM-0602", c)
	}
	challenge := func(id, ref string, points int) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.loyalty_redemption_otp_challenges (id, customer_id, points, reference_id, initiated_by, otp_hash, expires_at)
			VALUES ($1, $2, $3, $4, 'system', $5, NOW() + INTERVAL '10 minutes')`, id, cust, points, ref, hashOTP("424242")); err != nil {
			t.Fatal(err)
		}
	}
	challenge("CH-RC-4", "CART-RC-4", 50)
	if err := VerifyTillLoyaltyOTP(tenantID, "CH-RC-4", "000000"); err == nil {
		t.Errorf("a wrong code is refused")
	}
	if err := VerifyTillLoyaltyOTP(tenantID, "CH-RC-4", "424242"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// The refused attempt rolled back whole, so the same cart now carries the
	// verified code and goes through.
	if _, err := db.DB.Exec(`UPDATE ` + schemaName + `.documents SET data = data || '{"redemption_challenge_id":"CH-RC-4"}'::jsonb WHERE id = 'CART-RC-4'`); err != nil {
		t.Fatal(err)
	}
	if err := finalize("CART-RC-4"); err != nil {
		t.Fatalf("verified code: %v", err)
	}
	challenge("CH-RC-5", "CART-RC-OTHER", 50)
	_ = VerifyTillLoyaltyOTP(tenantID, "CH-RC-5", "424242")
	cart("CART-RC-5", 50, LoyaltyRedemptionAuth{ChallengeID: "CH-RC-5"})
	if c := code(finalize("CART-RC-5")); c != "CUSTOM-0602" {
		t.Errorf("a code sent for another cart does not authorise this one: %q", c)
	}
	cart("CART-RC-6", 50, LoyaltyRedemptionAuth{ChallengeID: "CH-RC-4"})
	if c := code(finalize("CART-RC-6")); c != "CUSTOM-0602" {
		t.Errorf("a spent code cannot be used again: %q", c)
	}

	// --- 73.1.4: the daily limit -------------------------------------------------
	if err := SetSetting(tenantID, "security.loyalty_max_redemptions_per_day", "2", "tester"); err != nil {
		t.Fatal(err)
	}
	challenge("CH-RC-7", "CART-RC-7", 50)
	_ = VerifyTillLoyaltyOTP(tenantID, "CH-RC-7", "424242")
	cart("CART-RC-7", 50, LoyaltyRedemptionAuth{ChallengeID: "CH-RC-7"})
	if c := code(finalize("CART-RC-7")); c != "CUSTOM-0601" {
		t.Errorf("third redemption today with a limit of 2: %q, want CUSTOM-0601", c)
	}
}

// Stage 73.1.6 - a bill paid five ways (offer, points, a gift card, Card and
// Cash fixed parts, UPI for the rest): each money part lands in its own
// clearing account, the sale reconciles, and returning one line refunds each
// part of it back the way it was paid - the gift-card share onto the card.
// Own throwaway tenant.
func TestStage73SplitTenderAndRefundByTender(t *testing.T) {
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
	gl := func(docType, docID, account string) int64 { // net debit
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit - credit), 0) FROM `+schemaName+`.gl_postings WHERE document_type = $1 AND document_id = $2 AND account_code = $3`, docType, docID, account).Scan(&v)
		return v
	}
	const loc, cust = "STORE-ST", "CUST-ST"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store ST", "type": "Store", "sellable": "Yes"})
	for code, price := range map[string]float64{"SHIRT": 1000, "SOCKS": 500} {
		insert(code, "Item", "Active", map[string]interface{}{"code": code, "name": code, "sale_price": price, "hsn_code": "6109", "tax_treatment": "Exempt"})
		if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": code, "qty": 10.0}}, false); err != nil {
			t.Fatal(err)
		}
	}
	insert("OFF-ST", "Offer", "Active", map[string]interface{}{"name": "Ten off", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10.0})
	if err := insertLoyaltyLedgerEntryInSchema(schemaName, cust, "Earn", 1000, "Seed", "SEED", nil); err != nil {
		t.Fatal(err)
	}
	reg, err := RegisterStoredValueCards(tenantID, "manager", RegisterCardsRequest{Profile: "Standard Gift Card", Generate: 1})
	if err != nil || len(reg.Numbers) != 1 {
		t.Fatalf("register card: %v", err)
	}
	card := reg.Numbers[0]
	if _, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: card, Amount: 500, PaymentMode: "Cash", Location: loc, IdempotencyKey: "st-issue"}); err != nil {
		t.Fatalf("issue card: %v", err)
	}

	// 1500 gross, 150 offer = 1350; 150 points; 200 gift card; 1000 in money:
	// Card 600 + Cash 100 fixed, UPI the rest (300).
	quote, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, CustomerID: cust, RedeemPoints: 150,
		Lines: []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}, {Sku: "SOCKS", Qty: 1}}})
	if err != nil || quote.AmountDue != 1200 {
		t.Fatalf("quote: %v due %.2f, want 1200", err, quote.AmountDue)
	}
	giftTenders, err := PrepareGiftCardTenders(tenantID, []GiftCardTenderRequest{{CardNumber: card, Amount: 200}}, RupeesToPaise(quote.AmountDue))
	if err != nil {
		t.Fatalf("gift tender: %v", err)
	}
	fixed := []POSTender{{Mode: "Card", Amount: 600}, {Mode: "Cash", Amount: 100}}
	if err := ValidatePOSTenders([]POSTender{{Mode: "Card", Amount: 1001}}, "UPI", 1000); err == nil {
		t.Errorf("a split larger than what is due in money is refused")
	}
	if err := ValidatePOSTenders([]POSTender{{Mode: "Store Credit", Amount: 10}}, "UPI", 1000); err == nil {
		t.Errorf("a split part must be Cash, Card or UPI")
	}
	if err := ValidatePOSTenders(fixed, "UPI", 1000); err != nil {
		t.Fatalf("valid split refused: %v", err)
	}
	const cart = "CART-ST"
	insert(cart, "POSCart", "Processing", map[string]interface{}{
		"location": loc, "payment_mode": "UPI", "customer_id": cust, "redeem_points": 150,
		"items": POSCartItemsFromQuote(quote), "offer_discount": quote.OfferDiscount, "applied_offers": quote.AppliedOffers,
		"gift_card_tenders": giftTenders, "tenders": fixed,
	})
	if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	for acct, want := range map[string]int64{"1101": 60000, "1100": 10000, "1102": 30000, "5250": 15000, "2610": 20000} {
		if got := gl("POSCart", cart, acct); got != want {
			t.Errorf("sale: account %s net %d, want %d", acct, got, want)
		}
	}
	if rec, err := ReconcileSale(tenantID, cart); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("a split sale reconciles: %v %+v", err, rec)
	}
	balanceBefore, _ := StoredValueBalance(tenantID, card)

	// Return the shirt. Its net 900 was paid: 100 points, 133.33 gift card,
	// 400 card, 66.67 cash and 200 UPI (each tender split by what was left).
	returnID, err := CreateReturnRequest(tenantID, "Customer Return", loc, cart, "", "tester", []ReturnItemInput{{SKU: "SHIRT", Qty: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_ = ApproveReturnRequest(tenantID, returnID, "approver")
	_ = ReceiveReturnRequest(tenantID, returnID, "store")
	refund, refundID, err := ApplyReturnQC(tenantID, returnID, map[string]string{"SHIRT": "Sellable"}, "qc")
	if err != nil || refund != 800 {
		t.Fatalf("QC: %v refund %.2f, want 800 (900 less 100 in points)", err, refund)
	}
	_ = ApproveRefundRequest(tenantID, refundID, "manager")
	if err := ProcessRefundRequest(tenantID, refundID, "manager", "Cash"); err != nil {
		t.Fatalf("process: %v", err)
	}
	for acct, want := range map[string]int64{"1101": -40000, "1100": -6667, "1102": -20000, "2610": -13333, "5250": -10000, "4100": 90000} {
		if got := gl("RefundRequest", refundID, acct); got != want {
			t.Errorf("refund: account %s net %d, want %d", acct, got, want)
		}
	}
	balanceAfter, _ := StoredValueBalance(tenantID, card)
	if d := RupeesToPaise(balanceAfter.Balance - balanceBefore.Balance); d != 13333 {
		t.Errorf("the gift-card share goes back onto the card: +%d paise, want 13333", d)
	}
	if rec, err := ReconcileReturn(tenantID, returnID); err != nil || rec.Verdict != "Balanced" {
		t.Errorf("the return reconciles: %v %+v", err, rec)
	}

	// A blocked card cannot take its share back: the refund stops with a reason.
	returnID2, _ := CreateReturnRequest(tenantID, "Customer Return", loc, cart, "", "tester", []ReturnItemInput{{SKU: "SOCKS", Qty: 1}})
	_ = ApproveReturnRequest(tenantID, returnID2, "approver")
	_ = ReceiveReturnRequest(tenantID, returnID2, "store")
	_, refundID2, err := ApplyReturnQC(tenantID, returnID2, map[string]string{"SOCKS": "Sellable"}, "qc")
	if err != nil {
		t.Fatal(err)
	}
	_ = ApproveRefundRequest(tenantID, refundID2, "manager")
	if _, err := SetStoredValueBlocked(tenantID, "manager", card, "lost", true); err != nil {
		t.Fatal(err)
	}
	if err := ProcessRefundRequest(tenantID, refundID2, "manager", "Cash"); err == nil {
		t.Errorf("a refund onto a blocked gift card must stop")
	} else if ve, ok := err.(*ValidationError); !ok || ve.Code != "POSOFF-0606" {
		t.Errorf("blocked card: %v, want POSOFF-0606", err)
	}
	if _, err := SetStoredValueBlocked(tenantID, "manager", card, "found", false); err != nil {
		t.Fatal(err)
	}
	if err := ProcessRefundRequest(tenantID, refundID2, "manager", "Cash"); err != nil {
		t.Errorf("released card: %v", err)
	}
}

// allocatePaise and unitShare are what make every split add back exactly.
func TestStage73AllocationArithmetic(t *testing.T) {
	if got := allocatePaise(100, []int64{1, 1, 1}); got[0]+got[1]+got[2] != 100 || got[0] != 34 {
		t.Errorf("100 over three equal lines = %v, want [34 33 33]", got)
	}
	if got := allocatePaise(7, []int64{0, 0}); got[0] != 7 {
		t.Errorf("no weights: the amount lands on the first line, got %v", got)
	}
	if got := allocatePaise(5, []int64{0, 3, 0}); got[1] != 5 || got[0] != 0 {
		t.Errorf("only weighted lines share: %v", got)
	}
	// The live till check's bill (2026-10-17): 2 shirts + 2 socks, 300 off,
	// 270 points, Card 1000 + UPI 200 + Cash the rest. Returning a shirt at a
	// time must give parts that add up to the unit's net, every time, and
	// both shirts together must give back the shirt line's whole tenders.
	alloc := buildSaleAllocation([]string{"SHIRT", "SOCKS"}, []int{2, 2}, []int64{200000, 100000}, []int64{20000, 10000}, 0,
		270, 100, 0, []tenderPart{{"Card", 100000}, {"UPI", 20000}, {"Cash", 123000}})
	byS := map[string]*skuSaleShare{}
	for _, a := range alloc {
		byS[a.SKU] = &skuSaleShare{Qty: a.Qty, NetPaise: a.NetPaise, Points: a.Points, Tenders: a.Tenders}
	}
	claimed := map[string]int{}
	totals := map[string]int64{}
	var net int64
	for i := 0; i < 2; i++ {
		recs := []returnItemRecord{{SKU: "SHIRT", Qty: 1}}
		applySaleAllocationToRecords(recs, byS, claimed)
		var sum int64
		for k, v := range recs[0].Tenders {
			sum += v
			totals[k] += v
		}
		if sum != recs[0].NetPaise || recs[0].Tenders[TenderLoyaltyPoints] != int64(recs[0].Points)*100 {
			t.Errorf("shirt %d: parts %d vs net %d, points %d worth %d", i+1, sum, recs[0].NetPaise, recs[0].Points, recs[0].Tenders[TenderLoyaltyPoints])
		}
		net += recs[0].NetPaise
	}
	if net != byS["SHIRT"].NetPaise {
		t.Errorf("both shirts back: net %d, want the line's %d", net, byS["SHIRT"].NetPaise)
	}
	for k, v := range byS["SHIRT"].Tenders {
		if totals[k] != v {
			t.Errorf("both shirts back: %s %d, want the line's %d", k, totals[k], v)
		}
	}
	for _, total := range []int64{1, 99, 26997, 100003} {
		for _, sold := range []int{1, 3, 7} {
			var sum int64
			for from := 0; from < sold; from++ {
				sum += unitShare(total, from, 1, sold)
			}
			if sum != total {
				t.Errorf("returning %d units one at a time from %d: %d, want %d", sold, total, sum, total)
			}
			if sold > 2 && unitShare(total, 0, 2, sold)+unitShare(total, 2, sold-2, sold) != total {
				t.Errorf("returning %d in two pieces from %d does not add up", sold, total)
			}
		}
	}
}
