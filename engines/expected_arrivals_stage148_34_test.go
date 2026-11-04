package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 148.34 - Expected arrivals: PO lines and instalments plus transfers in
// transit, per store and day, late first; a changed date re-opens the vendor's
// confirmation and the line stays listed; a store user sees their own store.

func arrivalFixture(t *testing.T) *agreementFixture {
	f := newAgreementFixture(t)
	for _, loc := range []string{"TEST14834-S1", "TEST14834-S2"} {
		f.insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": loc, "state": "Maharashtra"})
	}
	f.insert("TEST14834-A", "Item", "Active", map[string]interface{}{"code": "TEST14834-A", "name": "Arrival A", "hsn_code": "6204", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	lines, _ := json.Marshal([]map[string]interface{}{
		{"sku": "TEST14834-A", "qty": 10, "rate": 5, "confirmation_no": "C-1", "confirmation_date": "2026-11-05",
			"instalments": []map[string]interface{}{{"qty": 4, "date": "2026-11-01"}, {"qty": 6, "date": "2026-11-20"}}},
		{"sku": "TEST14834-A", "qty": 5, "rate": 5, "confirmation_no": "C-2", "confirmation_date": "2026-11-05"},
	})
	f.insert("TEST14834-PO", "PurchaseOrder", "Approved", map[string]interface{}{"code": "TEST14834-PO", "po_number": "TEST14834-PO",
		"vendor": "V", "target_warehouse": "TEST14834-S1", "location": "TEST14834-S1", "expected_delivery_date": "2026-11-08", "items": string(lines)})
	bal, _ := json.Marshal([]map[string]interface{}{{"sku": "TEST14834-A", "dispatched": 8, "received": 3}})
	f.insert("TEST14834-TO", "TransferOrder", "Partially Received", map[string]interface{}{"code": "TEST14834-TO", "from_warehouse": "TEST14834-S1",
		"to_warehouse": "TEST14834-S2", "dispatched_at": "2026-11-01T10:00:00Z", "transfer_balance": string(bal)})
	return f
}

func arrivalRows(t *testing.T, asOf string) []map[string]interface{} {
	t.Helper()
	all, err := ExpectedArrivals("default", asOf)
	if err != nil {
		t.Fatalf("expected arrivals: %v", err)
	}
	var mine []map[string]interface{}
	for _, r := range all {
		if s, _ := r["store"].(string); s == "TEST14834-S1" || s == "TEST14834-S2" {
			mine = append(mine, r)
		}
	}
	return mine
}

func TestStage14834ListsPOLinesInstalmentsAndTransfersLateFirst(t *testing.T) {
	f := arrivalFixture(t)
	defer f.cleanup()
	rows := arrivalRows(t, "2026-11-10")
	// store 1: line 1 instalment 1 (late, 4), line 2 on the PO date (late, 5), line 1 instalment 2 (upcoming, 6);
	// store 2: the transfer, 5 still to come
	if len(rows) != 4 {
		t.Fatalf("want 3 PO rows and 1 transfer row, got %d: %v", len(rows), rows)
	}
	want := []struct {
		store, due, kind string
		pend             float64
	}{
		{"TEST14834-S1", "2026-11-01", "Purchase order", 4}, {"TEST14834-S1", "2026-11-08", "Purchase order", 5},
		{"TEST14834-S1", "2026-11-20", "Purchase order", 6}, {"TEST14834-S2", "", "Transfer", 5}}
	for i, w := range want {
		r := rows[i]
		if r["store"] != w.store || r["due"] != w.due || r["kind"] != w.kind || numFromInterface(r["pending"]) != w.pend {
			t.Fatalf("row %d: want %+v, got %v", i, w, r)
		}
	}
	if rows[0]["state"] != "Late 9 days" || rows[2]["state"] != "Upcoming" || rows[0]["doc_no_doctype"] != "PurchaseOrder" || rows[3]["doc_no_doctype"] != "TransferOrder" {
		t.Fatalf("state or link wrong: %v %v", rows[0], rows[3])
	}
	if rows[0]["confirmation"] != "Confirmed" || rows[0]["confirmation_no"] != "C-1" {
		t.Fatalf("confirmation not shown: %v", rows[0])
	}
	// what was closed cuts the latest instalment, and a stopped line is not listed
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = jsonb_set(data, '{items}', to_jsonb($1::text)) WHERE id = 'TEST14834-PO'",
		`[{"sku":"TEST14834-A","qty":10,"rate":5,"closed_qty":6,"instalments":[{"qty":4,"date":"2026-11-01"},{"qty":6,"date":"2026-11-20"}]},{"sku":"TEST14834-A","qty":5,"rate":5,"stopped":true}]`); err != nil {
		t.Fatal(err)
	}
	rows = arrivalRows(t, "2026-11-10")
	if len(rows) != 2 || numFromInterface(rows[0]["pending"]) != 4 {
		t.Fatalf("closed 6 cuts the 2nd instalment, the stopped line is gone: %v", rows)
	}
}

func TestStage14834ChangedDateReopensConfirmationAndKeepsTheLine(t *testing.T) {
	f := arrivalFixture(t)
	defer f.cleanup()
	// path 1: the PO save choke point, expected delivery date moved
	prior, _, _ := fetchDocData("default", "PurchaseOrder", "TEST14834-PO")
	payload := map[string]interface{}{}
	for k, v := range prior {
		payload[k] = v
	}
	payload["expected_delivery_date"] = "2026-11-15"
	payload["amendment_reason"] = "vendor moved the date" // a terms change on an Approved PO already needs one
	if err := validatePurchaseOrderEditRules("default", "TEST14834-PO", "Approved", prior, payload); err != nil {
		t.Fatalf("the save: %v", err)
	}
	lines, _ := parsePOLineMaps(str(payload, "items"))
	if str(lines[1], "confirmation_no") != "" || str(lines[1], "confirmation_reopened_at") == "" {
		t.Fatalf("line with no instalments should be re-opened: %v", lines[1])
	}
	if str(lines[0], "confirmation_no") != "C-1" {
		t.Fatalf("line with its own instalment dates is not moved by the PO date: %v", lines[0])
	}
	// the save is stored; the list keeps the line, shown as re-opened
	raw, _ := json.Marshal(payload)
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = $1 WHERE id = 'TEST14834-PO'", raw); err != nil {
		t.Fatal(err)
	}
	var reopened bool
	for _, r := range arrivalRows(t, "2026-11-10") {
		if r["line_no"] == 2 && r["kind"] == "Purchase order" && r["due"] == "2026-11-15" && r["confirmation"] == "Re-opened" {
			reopened = true
		}
	}
	if !reopened {
		t.Fatalf("the line must stay listed on its new date as Re-opened")
	}
	// path 2: a direct instalment edit through the line-control action
	if err := SetPOLineControl("default", "TEST14834-PO", "system", 1, map[string]interface{}{"instalments": []map[string]interface{}{
		{"qty": 4, "date": "2026-11-03"}, {"qty": 6, "date": "2026-11-20"}}}); err != nil {
		t.Fatalf("set instalments: %v", err)
	}
	po, _, _ := fetchDocData("default", "PurchaseOrder", "TEST14834-PO")
	l1, _ := parsePOLineMaps(str(po, "items"))
	if str(l1[0], "confirmation_no") != "" || str(l1[0], "confirmation_reopened_at") == "" {
		t.Fatalf("moving an instalment date re-opens that line: %v", l1[0])
	}
	// a new confirmation clears the re-opened mark
	if err := SetPOLineControl("default", "TEST14834-PO", "system", 1, map[string]interface{}{"confirmation_no": "C-9", "confirmation_date": "2026-11-04"}); err != nil {
		t.Fatal(err)
	}
	po, _, _ = fetchDocData("default", "PurchaseOrder", "TEST14834-PO")
	l1, _ = parsePOLineMaps(str(po, "items"))
	if str(l1[0], "confirmation_no") != "C-9" || str(l1[0], "confirmation_reopened_at") != "" {
		t.Fatalf("a new confirmation: %v", l1[0])
	}
}

func TestStage14834StoreUserSeesTheirOwnStore(t *testing.T) {
	f := arrivalFixture(t)
	defer f.cleanup()
	const user = "manager1"
	before, err := GetUserStoreAccess("default", user)
	if err != nil {
		t.Skipf("no %s user in this database: %v", user, err)
	}
	if err := SetUserStoreAccess("default", user, StoreAccessHome, nil, "test"); err != nil {
		t.Fatal(err)
	}
	defer SetUserStoreAccess("default", user, before.Access, before.Stores, "test")
	access, limited := reportStoreScope("default", "Store Manager", user)
	if !limited || access.Home == "" {
		t.Fatalf("expected a store-limited user, got %+v", access)
	}
	// put the PO in the user's home store, the transfer elsewhere
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET data = jsonb_set(data, '{target_warehouse}', to_jsonb($1::text)) WHERE id = 'TEST14834-PO'", access.Home); err != nil {
		t.Fatal(err)
	}
	_, rows, _, err := RunReport("default", "expected-arrivals", "Store Manager", user, map[string]string{"as_of": "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	var po, transfer int
	for _, r := range rows {
		if r["doc_no"] == "TEST14834-PO" {
			po++
		}
		if r["doc_no"] == "TEST14834-TO" {
			transfer++
		}
		if r["store"] != access.Home {
			t.Fatalf("a row of another store reached the store user: %v", r)
		}
	}
	if po != 3 || transfer != 0 {
		t.Fatalf("store user: want the 3 PO rows of their store and no transfer to another store, got po=%d transfer=%d", po, transfer)
	}
	if _, rows, _, err := RunReport("default", "expected-arrivals", "Accountant", "acc-14834", map[string]string{"as_of": "2026-11-10"}); err != nil || len(rows) < 4 {
		t.Fatalf("company-wide role sees every store: %d rows, %v", len(rows), err)
	}
}
