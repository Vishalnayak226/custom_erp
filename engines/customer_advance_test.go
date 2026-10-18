package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 85.D3: a customer advance is stored value for one customer. Pinned:
// the receipt posts Dr cash / Cr 2640 (goods, no GST) or splits out GST
// (services); only the payer can use it; using it moves it off the liability
// (and gives back a services advance's receipt-time GST); a refund needs a
// reason, gets an AVR voucher and closes the advance; the drawer counts cash
// advances in and out.
func TestCustomerAdvanceLifecycle(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store, buyer, other, session = "S85F-STORE", "S85F-BUYER", "S85F-OTHER", "S85F-SESSION"
	cleanup := func() {
		rows, _ := db.DB.Query("SELECT instrument_id FROM "+schema+".customer_advances WHERE location = $1", store)
		var ids []string
		for rows != nil && rows.Next() {
			var id string
			rows.Scan(&id)
			ids = append(ids, id)
		}
		if rows != nil {
			rows.Close()
		}
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1 OR document_id IN (SELECT voucher_number FROM "+schema+".customer_advance_refunds WHERE instrument_id = $1)", id)
			db.DB.Exec("DELETE FROM "+schema+".customer_advance_refunds WHERE instrument_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".customer_advances WHERE instrument_id = $1", id)
			// The ledger is append-only by trigger; test rows go with the trigger off.
			db.DB.Exec("ALTER TABLE " + schema + ".stored_value_ledger DISABLE TRIGGER stored_value_ledger_append_only")
			db.DB.Exec("DELETE FROM "+schema+".stored_value_ledger WHERE instrument_id = $1", id)
			db.DB.Exec("ALTER TABLE " + schema + ".stored_value_ledger ENABLE TRIGGER stored_value_ledger_append_only")
			db.DB.Exec("DELETE FROM "+schema+".stored_value_instruments WHERE instrument_id = $1", id)
		}
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = 'S85F-CART'")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3, $4)", store, buyer, other, session)
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE store_code = $1", store)
	}
	cleanup()
	defer cleanup()
	put := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	put(store, "Location", map[string]interface{}{"code": store, "name": "S85F Store", "type": "Store", "short_code": "S85F"})
	put(buyer, "Customer", map[string]interface{}{"name": "Advance Buyer", "phone": "8000000087"})
	put(other, "Customer", map[string]interface{}{"name": "Someone Else", "phone": "8000000088"})
	put(session, "POSSession", map[string]interface{}{"location": store, "cashier": "tester", "opening_cash": 0})

	gl := func(doc string) map[string]int64 {
		out := map[string]int64{}
		rows, err := db.DB.Query("SELECT account_code, SUM(debit) - SUM(credit) FROM "+schema+".gl_postings WHERE document_id = $1 GROUP BY 1", doc)
		if err != nil {
			t.Fatalf("gl: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var acct string
			var net int64
			rows.Scan(&acct, &net)
			out[acct] = net
		}
		return out
	}

	var ve *ValidationError
	if _, err := TakeCustomerAdvance(tenantID, "tester", session, TakeAdvanceRequest{Location: store, CustomerID: buyer, Amount: 0}); !errors.As(err, &ve) || ve.Code != "POSOFF-0871" {
		t.Errorf("no amount: want POSOFF-0871, got %v", err)
	}
	goods, err := TakeCustomerAdvance(tenantID, "tester", session, TakeAdvanceRequest{Location: store, CustomerID: buyer, Amount: 1000, PaymentMode: "Cash", Purpose: "Custom lehenga", IdempotencyKey: "s85f-1"})
	if err != nil {
		t.Fatalf("take goods advance: %v", err)
	}
	if len(goods.Number) > 16 || goods.Number[:2] != "CA" || goods.Status != "Open" || goods.Balance != 1000 {
		t.Fatalf("goods advance: %+v", goods)
	}
	again, _ := TakeCustomerAdvance(tenantID, "tester", session, TakeAdvanceRequest{Location: store, CustomerID: buyer, Amount: 1000, PaymentMode: "Cash", IdempotencyKey: "s85f-1"})
	if again == nil || again.Number != goods.Number {
		t.Errorf("a repeat with the same key must return the first advance, got %+v", again)
	}
	if g := gl(goods.Number); g["1100"] != 100000 || g["2640"] != -100000 {
		t.Errorf("goods receipt posting: %v (want Dr 1100 1000.00, Cr 2640 1000.00)", g)
	}

	services, err := TakeCustomerAdvance(tenantID, "tester", session, TakeAdvanceRequest{Location: store, CustomerID: buyer, Amount: 1180, PaymentMode: "UPI", SupplyType: "Services", GSTRate: 18})
	if err != nil {
		t.Fatalf("take services advance: %v", err)
	}
	if g := gl(services.Number); g["1102"] != 118000 || g["2640"] != -100000 || g["2200"]+g["2201"] != -18000 {
		t.Errorf("services receipt posting: %v (want Dr 1102 1180, Cr 2640 1000, Cr GST 180)", g)
	}

	if _, err := PrepareAdvanceTenders(tenantID, other, []AdvanceTenderRequest{{Number: goods.Number, Amount: 100}}, 100000); !errors.As(err, &ve) || ve.Code != "POSOFF-0873" {
		t.Errorf("another customer's advance: want POSOFF-0873, got %v", err)
	}
	tenders, err := PrepareAdvanceTenders(tenantID, buyer, []AdvanceTenderRequest{{Number: goods.Number, Amount: 400}, {Number: services.Number, Amount: 590}}, 500000)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := redeemGiftCardTendersTx(tx, schema, tenantID, "S85F-CART", store, "tester", "Cash", tenders); err != nil {
		tx.Rollback()
		t.Fatalf("redeem: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if g := gl("S85F-CART"); g["2640"] != 40000+59000-9000 || g["2200"]+g["2201"] != 9000 {
		t.Errorf("using the advances: %v (want 2640 down 900, the 90 GST of half the services advance given back)", g)
	}
	after, _ := LoadCustomerAdvance(tenantID, goods.Number)
	if after.Balance != 600 || after.Applied != 400 || after.Status != "Partially used" {
		t.Errorf("after use: %+v", after)
	}

	if _, _, err := RefundCustomerAdvance(tenantID, "tester", session, goods.Number, 0, "", store, ""); !errors.As(err, &ve) || ve.Code != "POSOFF-0874" {
		t.Errorf("refund with no reason: want POSOFF-0874, got %v", err)
	}
	refunded, voucher, err := RefundCustomerAdvance(tenantID, "tester", session, goods.Number, 0, "Order cancelled", store, "s85f-r1")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if len(voucher) > 16 || voucher[:3] != "AVR" || refunded.Balance != 0 || refunded.Refunded != 600 || refunded.Status != "Refunded" {
		t.Errorf("refund: voucher %q advance %+v", voucher, refunded)
	}
	if g := gl(voucher); g["2640"] != 60000 || g["1100"] != -60000 {
		t.Errorf("refund posting: %v (want Dr 2640 600, Cr 1100 600 - back to cash, how it was paid)", g)
	}

	cash, err := SessionCashPosition(tenantID, session)
	if err != nil {
		t.Fatalf("session cash: %v", err)
	}
	if cash.AdvancesIn != 1000 || cash.AdvancesOut != 600 || cash.Expected != 400 {
		t.Errorf("drawer: in %v out %v expected %v (want 1000 / 600 / 400; the UPI advance is not cash)", cash.AdvancesIn, cash.AdvancesOut, cash.Expected)
	}
}
