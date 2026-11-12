package engines

// Two tills, one customer advance or one credit voucher: exactly one bill gets
// the money, the other is refused with a clear code, and the balance, the
// ledger and the GL all agree. The row lock in redeemGiftCardTendersTx is what
// holds this; the test would fail if a till could read the balance before the
// other committed.

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// tenderRace runs the same redemption from two tills (two carts, two
// transactions) at once and returns the two errors.
func tenderRace(t *testing.T, schema, loc string, tenders func(cart string) []GiftCardTender) [2]error {
	t.Helper()
	var errs [2]error
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cart := fmt.Sprintf("%s-CART%d", loc, i)
			<-start
			tx, err := db.DB.Begin()
			if err != nil {
				errs[i] = err
				return
			}
			defer tx.Rollback()
			if err := redeemGiftCardTendersTx(tx, schema, "default", cart, loc, "till"+fmt.Sprint(i), "Cash", tenders(cart)); err != nil {
				errs[i] = err
				return
			}
			errs[i] = tx.Commit()
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func tenderRaceOutcome(t *testing.T, what string, errs [2]error) {
	t.Helper()
	ok, refused := 0, 0
	for _, err := range errs {
		var ve *ValidationError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &ve) && (ve.Code == "POSOFF-0433" || ve.Code == "POSOFF-0435"):
			refused++
		default:
			t.Errorf("%s: a till failed with something other than a clear refusal: %v", what, err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("%s: %d tills got the money and %d were refused, want exactly 1 and 1 (%v)", what, ok, refused, errs)
	}
}

func TestTwoTillsApplyOneCustomerAdvance(t *testing.T) {
	e := fiNewEnv(t)
	loc, buyer, session := "ADV-"+e.marker, "BUY-"+e.marker, "SESS-"+e.marker
	e.doc("Location", loc, "Active", fmt.Sprintf(`{"code":%q,"name":"Race store","type":"Store","short_code":"RC"}`, loc))
	e.doc("Customer", buyer, "Active", `{"name":"Race buyer","phone":"8000000099"}`)
	e.doc("POSSession", session, "Open", fmt.Sprintf(`{"location":%q,"cashier":"tester","opening_cash":0}`, loc))
	adv, err := TakeCustomerAdvance("default", "tester", session, TakeAdvanceRequest{Location: loc, CustomerID: buyer, Amount: 1000, PaymentMode: "Cash", Purpose: "race"})
	if err != nil {
		t.Fatalf("take advance: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + e.schema + `.gl_postings WHERE document_id = '` + adv.Number + `' OR document_id LIKE '` + loc + `-CART%'`)
		_, _ = db.DB.Exec(`ALTER TABLE ` + e.schema + `.stored_value_ledger DISABLE TRIGGER stored_value_ledger_append_only`)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.stored_value_ledger WHERE instrument_id = $1`, adv.Number)
		_, _ = db.DB.Exec(`ALTER TABLE ` + e.schema + `.stored_value_ledger ENABLE TRIGGER stored_value_ledger_append_only`)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.customer_advances WHERE instrument_id = $1`, adv.Number)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.stored_value_instruments WHERE instrument_id = $1`, adv.Number)
	})

	// Each till applies 700 of the 1000 to its own bill.
	// Both tills prepare while the full 1000 is there; the lock is what decides.
	tenders, err := PrepareAdvanceTenders("default", buyer, []AdvanceTenderRequest{{Number: adv.Number, Amount: 700}}, 70000)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	errs := tenderRace(t, e.schema, loc, func(cart string) []GiftCardTender { return tenders })
	tenderRaceOutcome(t, "customer advance", errs)

	after, err := LoadCustomerAdvance("default", adv.Number)
	if err != nil {
		t.Fatal(err)
	}
	if after.Balance != 300 || after.Applied != 700 {
		t.Errorf("the advance after the race: balance %v applied %v, want 300 / 700 (used once)", after.Balance, after.Applied)
	}
	if n := e.count("stored_value_ledger", `instrument_id = $1 AND event_type = 'REDEEM'`, adv.Number); n != 1 {
		t.Errorf("redeem ledger rows: %d, want 1", n)
	}
	var dr int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM `+e.schema+`.gl_postings WHERE document_id LIKE $1 AND account_code = $2`, loc+"-CART%", AccountCustomerAdvLia).Scan(&dr)
	if dr != 70000 {
		t.Errorf("advance liability released in the GL: %d paise, want 70000 (once)", dr)
	}
}

func TestTwoTillsRedeemOneCreditVoucher(t *testing.T) {
	e := fiNewEnv(t)
	loc := "VCH-" + e.marker
	e.doc("Location", loc, "Active", fmt.Sprintf(`{"code":%q,"name":"Race store","type":"Store","sellable":"Yes"}`, loc))
	reg, err := RegisterStoredValueCards("default", "manager", RegisterCardsRequest{Profile: "Standard Gift Card", Generate: 1})
	if err != nil || len(reg.Numbers) != 1 {
		t.Fatalf("register: %v %+v", err, reg)
	}
	card := reg.Numbers[0]
	if _, err := IssueStoredValue("default", "cashier", StoredValueIssueRequest{CardNumber: card, Amount: 1000, PaymentMode: "UPI", Location: loc, IdempotencyKey: "race-" + e.marker}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	first, err := PrepareGiftCardTenders("default", []GiftCardTenderRequest{{CardNumber: card, Amount: 1000}}, 100000)
	if err != nil || len(first) != 1 {
		t.Fatalf("prepare: %v", err)
	}
	instrument := first[0].InstrumentID
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + e.schema + `.gl_postings WHERE document_id LIKE '` + loc + `-CART%' OR document_id = '` + instrument + `'`)
		_, _ = db.DB.Exec(`ALTER TABLE ` + e.schema + `.stored_value_ledger DISABLE TRIGGER stored_value_ledger_append_only`)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.stored_value_ledger WHERE instrument_id = $1`, instrument)
		_, _ = db.DB.Exec(`ALTER TABLE ` + e.schema + `.stored_value_ledger ENABLE TRIGGER stored_value_ledger_append_only`)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.stored_value_instruments WHERE instrument_id = $1`, instrument)
	})

	errs := tenderRace(t, e.schema, loc, func(cart string) []GiftCardTender {
		tenders, err := PrepareGiftCardTenders("default", []GiftCardTenderRequest{{CardNumber: card, Amount: 1000}}, 100000)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		return tenders
	})
	tenderRaceOutcome(t, "credit voucher", errs)

	after, err := StoredValueBalance("default", card)
	if err != nil {
		t.Fatal(err)
	}
	if after.Balance != 0 {
		t.Errorf("the voucher balance after the race: %v, want 0 (spent once)", after.Balance)
	}
	if n := e.count("stored_value_ledger", `instrument_id = $1 AND event_type = 'REDEEM'`, instrument); n != 1 {
		t.Errorf("redeem ledger rows: %d, want 1", n)
	}
	var dr int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM `+e.schema+`.gl_postings WHERE document_id LIKE $1`, loc+"-CART%").Scan(&dr)
	if dr != 100000 {
		t.Errorf("voucher liability released in the GL: %d paise, want 100000 (once)", dr)
	}
}

// Store credit (a refund to the customer's balance) is redeemed through the
// same locked path as a gift card: two tills spending the one balance.
func TestTwoTillsRedeemOneStoreCredit(t *testing.T) {
	e := fiNewEnv(t)
	loc, buyer := "SCR-"+e.marker, "BUY-"+e.marker
	e.doc("Location", loc, "Active", fmt.Sprintf(`{"code":%q,"name":"Race store","type":"Store","sellable":"Yes"}`, loc))
	e.doc("Customer", buyer, "Active", `{"name":"Race buyer","phone":"8000000098"}`)
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	id, err := issueStoreCreditTx(tx, "default", e.schema, buyer, 100000, svSource{Doctype: "RefundRequest", ID: "RF-" + e.marker, User: "manager1", Reason: "race"}, "storecredit|race-"+e.marker)
	if err != nil || id == "" {
		tx.Rollback()
		t.Fatalf("issue store credit: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + e.schema + `.gl_postings WHERE document_id LIKE '` + loc + `-CART%'`)
		_, _ = db.DB.Exec(`ALTER TABLE ` + e.schema + `.stored_value_ledger DISABLE TRIGGER stored_value_ledger_append_only`)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.stored_value_ledger WHERE instrument_id = $1`, id)
		_, _ = db.DB.Exec(`ALTER TABLE ` + e.schema + `.stored_value_ledger ENABLE TRIGGER stored_value_ledger_append_only`)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.stored_value_instruments WHERE instrument_id = $1`, id)
	})
	// Both tills prepare while the full 1000 is there; the row lock decides.
	tenders, err := PrepareStoreCreditTender("default", RoleStoreManager, buyer, 1000, 100000)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	errs := tenderRace(t, e.schema, loc, func(cart string) []GiftCardTender { return tenders })
	tenderRaceOutcome(t, "store credit", errs)

	if bal, _ := StoreCreditBalance("default", buyer); bal != 0 {
		t.Errorf("store credit after the race: %v, want 0 (spent once)", bal)
	}
	if n := e.count("stored_value_ledger", `instrument_id = $1 AND event_type = 'REDEEM'`, id); n != 1 {
		t.Errorf("redeem ledger rows: %d, want 1", n)
	}
	var dr int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit), 0) FROM `+e.schema+`.gl_postings WHERE document_id LIKE $1 AND account_code = $2`, loc+"-CART%", AccountStoreCreditLia).Scan(&dr)
	if dr != 100000 {
		t.Errorf("store credit liability released in the GL: %d paise, want 100000 (once)", dr)
	}
}
