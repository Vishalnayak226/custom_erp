package engines

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 77.8: a bill sold at one store and returned at another is booked at
// the selling store with returns.centralised on, and its goods move to the
// store that took them on a transfer received there - Good into Inward,
// Damage into Damage. Off, the return is booked where it is taken.
func TestStage778CentralisedReturn(t *testing.T) {
	f := stage1484Setup(t)
	const soldAt, taken = "S1484", "S778B"
	f.insert(taken, "Location", "Active", "system", map[string]interface{}{"code": taken, "name": "Store 778B", "type": "Store", "sellable": "Yes"})
	sell := func(id string) {
		t.Helper()
		data, _ := json.Marshal(map[string]interface{}{"location": soldAt, "payment_mode": "Cash",
			"items": []map[string]interface{}{{"sku": "TEE1484", "qty": 2, "sale_price": 100.0}, {"sku": "SHOE1484", "qty": 1, "sale_price": 100.0}}})
		if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'cashier1484')", id, data); err != nil {
			t.Fatal(err)
		}
		if _, _, err := FinalizePOSCheckout(f.tenantID, id, ""); err != nil {
			t.Fatalf("checkout %s: %v", id, err)
		}
	}
	field := func(doctype, id, name string) string {
		var v string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'%s', '') FROM %s.documents WHERE doctype = $1 AND id = $2`, name, f.schema), doctype, id).Scan(&v)
		return v
	}
	stock := func(sku, store string) (avail, inward, damaged, qc, transit int) {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT available, inward, damaged, qc_hold, in_transit FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, f.schema),
			sku, store).Scan(&avail, &inward, &damaged, &qc, &transit)
		return
	}

	// Off: booked where it is taken, as before.
	sell("CART-778-OFF")
	off, err := CreateReturnRequest(f.tenantID, "Customer Return", taken, "CART-778-OFF", "", "tester", []ReturnItemInput{{SKU: "TEE1484", Qty: 1}})
	if err != nil {
		t.Fatalf("return with the setting off: %v", err)
	}
	if loc, at := field("ReturnRequest", off, "return_location"), field("ReturnRequest", off, "collected_at"); loc != taken || at != "" {
		t.Fatalf("setting off: booked at %q, collected at %q", loc, at)
	}
	if e, err := ResolveReturnEligibility(f.tenantID, "CART-778-OFF"); err != nil || e.Centralised {
		t.Fatalf("eligibility says centralised with the setting off: %+v %v", e, err)
	}

	// On: booked at the selling store, taken here.
	if err := SetSetting(f.tenantID, SettingCentralisedReturn, "true", "admin"); err != nil {
		t.Fatal(err)
	}
	sell("CART-778-ON")
	aTee, _, _, _, _ := stock("TEE1484", soldAt)
	aShoe, _, aShoeDmg, _, _ := stock("SHOE1484", soldAt)
	rid, err := CreateReturnRequest(f.tenantID, "Customer Return", taken, "CART-778-ON", "", "tester",
		[]ReturnItemInput{{SKU: "TEE1484", Qty: 2}, {SKU: "SHOE1484", Qty: 1}})
	if err != nil {
		t.Fatalf("centralised return: %v", err)
	}
	if loc, at, orig := field("ReturnRequest", rid, "return_location"), field("ReturnRequest", rid, "collected_at"), field("ReturnRequest", rid, "original_store"); loc != soldAt || at != taken || orig != soldAt {
		t.Fatalf("setting on: booked at %q, collected at %q, sold at %q", loc, at, orig)
	}
	if e, err := ResolveReturnEligibility(f.tenantID, "CART-778-ON"); err != nil || !e.Centralised {
		t.Fatalf("eligibility does not say centralised: %+v %v", e, err)
	}
	// The taking store lists it.
	if fields, _ := StoreViewListFields(f.tenantID, "ReturnRequest"); len(fields) != 2 || fields[1] != "collected_at" {
		t.Fatalf("the taking store's list does not match collected_at: %v", fields)
	}
	if err := ApproveReturnRequest(f.tenantID, rid, "approver"); err != nil {
		t.Fatal(err)
	}
	if err := ReceiveReturnRequest(f.tenantID, rid, "store"); err != nil {
		t.Fatal(err)
	}
	disp := map[string]string{"TEE1484": "Sellable", "SHOE1484": "Damaged"}
	// An exchange would take the new item from the selling store's stock.
	if _, _, err := ApplyReturnQC(f.tenantID, rid, disp, "qc", map[string]string{"TEE1484": "SHOE1484"}); err == nil || !strings.Contains(err.Error(), "booked at") {
		t.Fatalf("exchange on a centralised return: want refused, got %v", err)
	}
	if _, _, err := ApplyReturnQC(f.tenantID, rid, disp, "qc"); err != nil {
		t.Fatalf("QC: %v", err)
	}

	// Two transfers, Good and Damage, both received at the taking store.
	var made []map[string]interface{}
	var raw string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'transfer_orders', '[]') FROM %s.documents WHERE id = $1`, f.schema), rid).Scan(&raw)
	_ = json.Unmarshal([]byte(raw), &made)
	if len(made) != 2 {
		t.Fatalf("want 2 transfers (Good, Damage), got %s; note %q state %q", raw, field("ReturnRequest", rid, "transfer_note"), field("ReturnRequest", rid, "transfer_state"))
	}
	if s := field("ReturnRequest", rid, "transfer_state"); s != "Received" {
		t.Fatalf("transfer state %q", s)
	}
	for _, m := range made {
		id, _ := m["id"].(string)
		var status string
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE id = $1`, f.schema), id).Scan(&status)
		if status != "Received" || field("TransferOrder", id, "from_warehouse") != soldAt || field("TransferOrder", id, "to_warehouse") != taken ||
			field("TransferOrder", id, "source_return") != rid || field("TransferOrder", id, "transfer_number") == "" {
			t.Fatalf("transfer %s: %s, %s -> %s, return %q, number %q", id, status, field("TransferOrder", id, "from_warehouse"),
				field("TransferOrder", id, "to_warehouse"), field("TransferOrder", id, "source_return"), field("TransferOrder", id, "transfer_number"))
		}
	}

	// The goods are at the taking store - Good in Inward, Damage in Damage -
	// and the selling store is back where it was before the return.
	if _, inward, _, _, transit := stock("TEE1484", taken); inward != 2 || transit != 0 {
		t.Fatalf("Good at %s: inward %d, in transit %d - want 2, 0", taken, inward, transit)
	}
	if _, _, damaged, _, _ := stock("SHOE1484", taken); damaged != 1 {
		t.Fatalf("Damaged at %s: %d, want 1", taken, damaged)
	}
	if tee, _, _, qc, transit := stock("TEE1484", soldAt); tee != aTee || qc != 0 || transit != 0 {
		t.Fatalf("selling store TEE: available %d (was %d), qc %d, in transit %d", tee, aTee, qc, transit)
	}
	if shoe, _, dmg, _, _ := stock("SHOE1484", soldAt); shoe != aShoe || dmg != aShoeDmg {
		t.Fatalf("selling store SHOE: available %d (was %d), damaged %d (was %d)", shoe, aShoe, dmg, aShoeDmg)
	}

	// A second run changes nothing.
	ids, err := TransferCentralisedReturn(f.tenantID, rid, "qc")
	if err != nil || len(ids) != 2 {
		t.Fatalf("rerun: %v %v", ids, err)
	}
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'TransferOrder' AND data->>'source_return' = $1`, f.schema), rid).Scan(&n)
	if _, inward, _, _, _ := stock("TEE1484", taken); n != 2 || inward != 2 {
		t.Fatalf("rerun made %d transfers, inward %d", n, inward)
	}
}
