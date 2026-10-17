package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 72.6 and 72.8 - consumption entry and gift cards as stored value
// (the user's blueprint 9.6, 11 and UAT-13, UAT-16..20, UAT-27). Own
// throwaway tenant schema - provisioned AFTER the 72.8 migration, which is
// exactly the client whose cloned ledger has no trigger until the engine
// puts it there.
func TestStage72StoredValueAndConsumptionEntry(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}
	gl := func(doc, account, side string) int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(`+side+`), 0) FROM `+schemaName+`.gl_postings WHERE document_id = $1 AND account_code = $2`, doc, account).Scan(&v)
		return v
	}
	const loc = "STORE-SV"
	insert(loc, "Location", map[string]interface{}{"code": loc, "name": "Store SV", "type": "Store", "sellable": "Yes"})
	const profile = "Standard Gift Card"

	// --- register ---------------------------------------------------------------
	reg, err := RegisterStoredValueCards(tenantID, "manager", RegisterCardsRequest{Profile: profile, Generate: 3})
	if err != nil || reg.Registered != 3 || len(reg.Numbers) != 3 {
		t.Fatalf("generate 3 cards: %v %+v", err, reg)
	}
	for _, n := range reg.Numbers {
		if len(n) != 16 || luhnCheckDigit(n[:15]) != int(n[15]-'0') {
			t.Errorf("generated %s is not a 16-digit Luhn number", MaskCardNumber(n))
		}
	}
	cardA, cardB, cardC := reg.Numbers[0], reg.Numbers[1], reg.Numbers[2]
	if _, err := RegisterStoredValueCards(tenantID, "manager", RegisterCardsRequest{Profile: profile, Numbers: []string{cardA}}); code(err) != "POSOFF-0432" {
		t.Errorf("registering a known number again: err = %v, want POSOFF-0432", err)
	}
	if _, err := RegisterStoredValueCards(tenantID, "manager", RegisterCardsRequest{Profile: profile, Numbers: []string{"1234"}}); code(err) != "POSOFF-0431" {
		t.Errorf("a short number: err = %v, want POSOFF-0431", err)
	}
	if v, err := StoredValueBalance(tenantID, cardA); err != nil || v.Status != SVStatusInactive || v.Balance != 0 || v.Masked != MaskCardNumber(cardA) {
		t.Errorf("new card: %v %+v", err, v)
	}
	if _, err := PrepareGiftCardTenders(tenantID, []GiftCardTenderRequest{{CardNumber: cardA, Amount: 10}}, 100000); code(err) != "POSOFF-0434" {
		t.Errorf("paying with an unissued card: err = %v, want POSOFF-0434", err)
	}

	// --- issue: funded only when it commits (UAT-16/17) --------------------------
	if _, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: cardA, Amount: 50, PaymentMode: "Cash", Location: loc, IdempotencyKey: "k-low"}); code(err) != "POSOFF-0435" {
		t.Errorf("below the minimum: err = %v, want POSOFF-0435", err)
	}
	if v, _ := StoredValueBalance(tenantID, cardA); v.Status != SVStatusInactive {
		t.Errorf("a refused issue must leave the card unfunded: %+v", v)
	}
	rec, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: cardA, Amount: 1000, PaymentMode: "UPI", Location: loc, IdempotencyKey: "k-a"})
	if err != nil || rec.Event != SVEventActivate || rec.Balance != 1000 || rec.ExpiresOn == "" {
		t.Fatalf("issue: %v %+v", err, rec)
	}
	var cardAID string
	_ = db.DB.QueryRow(`SELECT instrument_id FROM `+schemaName+`.stored_value_instruments WHERE card_number = $1`, cardA).Scan(&cardAID)
	if gl(cardAID, "1102", "debit") != 100000 || gl(cardAID, "2610", "credit") != 100000 {
		t.Errorf("issue posting: UPI clearing %d, liability %d (paise), want 100000 each", gl(cardAID, "1102", "debit"), gl(cardAID, "2610", "credit"))
	}
	if again, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: cardA, Amount: 1000, PaymentMode: "UPI", Location: loc, IdempotencyKey: "k-a"}); err != nil || !again.Replayed || again.EntryID != rec.EntryID {
		t.Errorf("a retry must replay, not fund twice (UAT-27): %v %+v", err, again)
	}
	if _, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: cardB, Amount: 1000, PaymentMode: "UPI", Location: loc, IdempotencyKey: "k-a"}); code(err) != "POSOFF-0436" {
		t.Errorf("same key, other card: err = %v, want POSOFF-0436", err)
	}
	if _, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: cardA, Amount: 500, PaymentMode: "Cash", Location: loc, IdempotencyKey: "k-a2"}); code(err) != "POSOFF-0432" {
		t.Errorf("issuing an issued card that cannot be reloaded: err = %v, want POSOFF-0432", err)
	}
	if v, _ := StoredValueBalance(tenantID, cardA); v.Balance != 1000 {
		t.Errorf("balance after the refused re-issue = %v, want 1000", v.Balance)
	}

	// --- redeem as a tender inside the sale (UAT-18/19) ---------------------------
	if _, err := PrepareGiftCardTenders(tenantID, []GiftCardTenderRequest{{CardNumber: cardA, Amount: 1500}}, 500000); code(err) != "POSOFF-0433" {
		t.Errorf("more than the balance: err = %v, want POSOFF-0433", err)
	}
	if _, err := PrepareGiftCardTenders(tenantID, []GiftCardTenderRequest{{CardNumber: cardA, Amount: 600}}, 50000); code(err) != "POSOFF-0435" {
		t.Errorf("more than the bill: err = %v, want POSOFF-0435", err)
	}
	tenders, err := PrepareGiftCardTenders(tenantID, []GiftCardTenderRequest{{CardNumber: cardA, Amount: 600}}, 500000)
	if err != nil || len(tenders) != 1 || tenders[0].Masked != MaskCardNumber(cardA) {
		t.Fatalf("tender: %v %+v", err, tenders)
	}
	insert("RING", "Item", map[string]interface{}{"code": "RING", "name": "Ring", "sale_price": 5000.0, "hsn_code": "7113", "tax_treatment": "Exempt"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "RING", "qty": 5.0}}, false); err != nil {
		t.Fatal(err)
	}
	cart := "CART-SV-1"
	cartData, _ := json.Marshal(map[string]interface{}{"location": loc, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": "RING", "qty": 1, "sale_price": 5000.0}}, "gift_card_tenders": tenders})
	if _, err := db.DB.Exec(`INSERT INTO `+schemaName+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'cashier')`, cart, cartData); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
		t.Fatalf("FinalizePOSCheckout with a gift card: %v", err)
	}
	if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if v, _ := StoredValueBalance(tenantID, cardA); v.Balance != 400 || v.Status != SVStatusPartial {
		t.Errorf("after paying 600: %+v, want 400 left, partially redeemed", v)
	}
	if gl(cart, "2610", "debit") != 60000 || gl(cart, "1100", "credit") != 60000 {
		t.Errorf("redemption posting: liability %d, cash credit %d, want 60000 each", gl(cart, "2610", "debit"), gl(cart, "1100", "credit"))
	}
	var events int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schemaName+`.stored_value_ledger WHERE instrument_id = $1`, cardAID).Scan(&events)
	if events != 2 {
		t.Errorf("card A events = %d, want 2 (ACTIVATE, REDEEM) - a replay must not redeem twice", events)
	}
	// The ledger cannot be edited (blueprint 11.4).
	if _, err := db.DB.Exec(`UPDATE ` + schemaName + `.stored_value_ledger SET amount_paise = 0`); err == nil {
		t.Errorf("the gift card ledger accepted an UPDATE")
	}

	// --- block / unblock / cancel ------------------------------------------------
	if _, err := SetStoredValueBlocked(tenantID, "manager", cardA, "", true); code(err) != "POSOFF-0434" {
		t.Errorf("block without a reason: err = %v", err)
	}
	if v, err := SetStoredValueBlocked(tenantID, "manager", cardA, "reported lost", true); err != nil || v.Status != SVStatusBlocked {
		t.Errorf("block: %v %+v", err, v)
	}
	if _, err := PrepareGiftCardTenders(tenantID, []GiftCardTenderRequest{{CardNumber: cardA, Amount: 100}}, 500000); code(err) != "POSOFF-0434" {
		t.Errorf("paying with a blocked card: err = %v, want POSOFF-0434", err)
	}
	if v, err := SetStoredValueBlocked(tenantID, "manager", cardA, "found", false); err != nil || v.Status != SVStatusPartial {
		t.Errorf("unblock returns the card to its balance status: %v %+v", err, v)
	}
	if _, err := CancelStoredValueIssue(tenantID, "manager", cardA, "customer changed mind", loc); code(err) != "POSOFF-0434" {
		t.Errorf("cancelling a used card: err = %v, want POSOFF-0434", err)
	}
	if _, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: cardC, Amount: 2000, PaymentMode: "Card", Location: loc, IdempotencyKey: "k-c"}); err != nil {
		t.Fatal(err)
	}
	if r, err := CancelStoredValueIssue(tenantID, "manager", cardC, "wrong amount loaded", loc); err != nil || r.Amount != 2000 || r.PaymentMode != "Card" {
		t.Errorf("cancel an unused issue: %v %+v", err, r)
	}
	if v, _ := StoredValueBalance(tenantID, cardC); v.Status != SVStatusCancelled || v.Balance != 0 {
		t.Errorf("cancelled card: %+v", v)
	}

	// --- reports ------------------------------------------------------------------
	rows, err := storedValueLiabilityReport(tenantID, map[string]string{})
	if err != nil || len(rows) != 3 {
		t.Fatalf("liability report: %v %d rows", err, len(rows))
	}
	outstanding := 0.0
	for _, r := range rows {
		outstanding += r["balance"].(float64)
		if c, _ := r["card"].(string); len(c) > 9 {
			t.Errorf("report shows more than the last four digits: %q", c)
		}
	}
	if outstanding != 400 {
		t.Errorf("outstanding liability = %v, want 400", outstanding)
	}

	// --- 72.6 consumption entry (UAT-13) --------------------------------------------
	insert("BUTTER", "Item", map[string]interface{}{"code": "BUTTER", "name": "Butter paper", "item_type": "Consumable - Periodic"})
	insert("TAPE", "Item", map[string]interface{}{"code": "TAPE", "name": "Tape", "item_type": "Packaging"})
	insert("FITTING", "Item", map[string]interface{}{"code": "FITTING", "name": "Fitting service", "item_type": "Service"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{
		map[string]interface{}{"sku": "BUTTER", "qty": 1000.0}, map[string]interface{}{"sku": "TAPE", "qty": 20.0},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := RecordStockReceiptCost(tenantID, "TAPE", 20, 3000); err != nil {
		t.Fatal(err)
	}
	counted := func(n int) *int { return &n }
	if _, err := PostConsumptionEntry(tenantID, "manager", ConsumptionEntryRequest{Location: loc, Mode: "Periodic count", Lines: []ConsumptionEntryLine{{SKU: "BUTTER", CountedQty: counted(1200)}}}); code(err) != "INVENT-0422" {
		t.Errorf("counting more than the book: err = %v, want INVENT-0422", err)
	}
	if _, err := PostConsumptionEntry(tenantID, "manager", ConsumptionEntryRequest{Location: loc, Mode: "Manual issue", Lines: []ConsumptionEntryLine{{SKU: "FITTING", IssuedQty: 1}}}); code(err) != "INVENT-0421" {
		t.Errorf("consuming a service: err = %v, want INVENT-0421", err)
	}
	res, err := PostConsumptionEntry(tenantID, "manager", ConsumptionEntryRequest{Location: loc, Mode: "Periodic count", CountReference: "WK-42", IdempotencyKey: "count-1",
		Lines: []ConsumptionEntryLine{{SKU: "BUTTER", CountedQty: counted(250)}}})
	if err != nil || len(res.Lines) != 1 || res.Lines[0]["consumed_qty"] != 750 {
		t.Fatalf("periodic: %v %+v", err, res)
	}
	if again, err := PostConsumptionEntry(tenantID, "manager", ConsumptionEntryRequest{Location: loc, Mode: "Periodic count", IdempotencyKey: "count-1",
		Lines: []ConsumptionEntryLine{{SKU: "BUTTER", CountedQty: counted(250)}}}); err != nil || !again.Replayed || again.ID != res.ID {
		t.Errorf("a retry must not consume twice: %v %+v", err, again)
	}
	if b, _ := BookStockAt(tenantID, "BUTTER", loc); b != 250 {
		t.Errorf("butter paper after the count = %d, want 250", b)
	}
	if gl(res.ID, "5180", "debit") != 0 {
		t.Errorf("non-valuated butter paper must post no GL at issue")
	}
	man, err := PostConsumptionEntry(tenantID, "manager", ConsumptionEntryRequest{Location: loc, Mode: "Manual issue", Reason: "shop fit-out", Lines: []ConsumptionEntryLine{{SKU: "TAPE", IssuedQty: 4}}})
	if err != nil {
		t.Fatalf("manual: %v", err)
	}
	if gl(man.ID, "5160", "debit") != 4*3000 || gl(man.ID, "1200", "credit") != 4*3000 {
		t.Errorf("valuated tape: 5160 %d, 1200 %d, want 12000 each", gl(man.ID, "5160", "debit"), gl(man.ID, "1200", "credit"))
	}
}
