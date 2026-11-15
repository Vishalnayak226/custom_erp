package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 76.10: a till registers its own series while online; an offline sale
// it numbered posts under that number, once; a number from another store's
// series, a malformed one, or one already on a bill is refused; a live sale,
// or an offline one the till could not number, takes the store's number.
func TestStage7610OfflineTillBillNumbers(t *testing.T) {
	db.InitDB(testConnStr())
	var present bool
	if err := db.DB.QueryRow(`SELECT to_regclass('tenant_default.pos_till_series') IS NOT NULL`).Scan(&present); err != nil || !present {
		t.Skip("migrations_stage76_10_pos_till_series.sql is not applied to this database")
	}
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
	const loc, other = "STORE-TS", "STORE-TS2"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Store TS", "type": "Store", "sellable": "Yes", "short_code": "JAY"})
	insert(other, "Location", "Active", map[string]interface{}{"code": other, "name": "Store TS2", "type": "Store", "sellable": "Yes", "short_code": "KOR"})
	insert("SHIRT", "Item", "Active", map[string]interface{}{"code": "SHIRT", "name": "SHIRT", "sale_price": 1000.0, "hsn_code": "6109", "tax_treatment": "Taxable", "gst_rate": 5.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "SHIRT", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}

	first, err := RegisterPOSTill(tenantID, loc, "manager1", "Counter 1")
	if err != nil || first.Till != "01" || first.Base != "T01JAY" || first.Digits != 6 {
		t.Fatalf("first till: %v %+v, want till 01 base T01JAY with 6 digits", err, first)
	}
	second, err := RegisterPOSTill(tenantID, loc, "manager1", "")
	if err != nil || second.Till != "02" || second.Base != "T02JAY" {
		t.Fatalf("second till: %v %+v, want till 02 base T02JAY", err, second)
	}
	if _, err := RegisterPOSTill(tenantID, other, "manager1", ""); err != nil {
		t.Fatalf("another store's first till: %v", err)
	}
	if _, err := RegisterPOSTill(tenantID, "NO-SUCH-STORE", "manager1", ""); err == nil || !strings.Contains(err.Error(), "not a store") {
		t.Fatalf("a till was registered at a store that does not exist: %v", err)
	} else if v, ok := err.(*ValidationError); !ok || v.Code != "POSOFF-1371" {
		t.Errorf("unknown store error %T %v, want POSOFF-1371", err, err)
	}

	cartN := 0
	sell := func(offline bool, number string) (string, error) {
		t.Helper()
		cartN++
		cart := "CART-TS-" + string(rune('A'+cartN))
		q, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: loc, Lines: []QuoteLineRequest{{Sku: "SHIRT", Qty: 1}}})
		if err != nil {
			t.Fatalf("quote %s: %v", cart, err)
		}
		insert(cart, "POSCart", "Processing", map[string]interface{}{"location": loc, "payment_mode": "Cash",
			"items": POSCartItemsFromQuote(q), "gst_breakdown": q.GSTBreakdown, "offline_synced": offline, "offline_bill_number": number})
		if _, _, err := FinalizePOSCheckout(tenantID, cart, ""); err != nil {
			return cart, err
		}
		var bill string
		_ = db.DB.QueryRow("SELECT COALESCE(data->>'bill_number', '') FROM "+schemaName+".documents WHERE id = $1", cart).Scan(&bill)
		return bill, nil
	}

	bill, err := sell(true, "t01jay2627000001")
	if err != nil || bill != "T01JAY2627000001" {
		t.Fatalf("offline sale with its till number: %v, bill %q", err, bill)
	}
	if bill, err := sell(true, "T02JAY2627000001"); err != nil || bill != "T02JAY2627000001" {
		t.Fatalf("the second till's own first number: %v, bill %q", err, bill)
	}
	for _, bad := range []string{
		"T01JAY2627000001", // already on a bill
		"T01KOR2627000001", // the other store's till series
		"T09JAY2627000001", // no till 09 at this store
		"T01JAY2628000001", // not a financial year
		"T01JAY2627000000", // running number zero
		"T01JAY262700001",  // 15 characters
		"T01JAY26270000A1", // letters in the running number
	} {
		cart, err := sell(true, bad)
		if err == nil {
			t.Errorf("offline number %s was accepted", bad)
			continue
		}
		if v, ok := err.(*ValidationError); !ok || v.Code != "POSOFF-1371" {
			t.Errorf("offline number %s refused with %T %v, want POSOFF-1371", bad, err, err)
		}
		var status string
		_ = db.DB.QueryRow("SELECT status FROM "+schemaName+".documents WHERE id = $1", cart).Scan(&status)
		if status == "Paid" {
			t.Errorf("refused number %s still posted its sale", bad)
		}
	}

	live, err := sell(false, "T01JAY2627000009")
	if err != nil || !strings.HasPrefix(live, "BJAY") {
		t.Fatalf("a live sale must take the store's number, not one it sends: %v, bill %q", err, live)
	}
	if bill, err := sell(true, ""); err != nil || !strings.HasPrefix(bill, "BJAY") || bill == live {
		t.Fatalf("an offline sale with no till number takes the store's next number: %v, bill %q", err, bill)
	}
}
