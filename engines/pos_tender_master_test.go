package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 85.E2/85.E3/85.E5: a bill names only the store's tenders (each of
// its kind), carries a salesman when the store asks, and its money is summed
// by the tenders' own names.
func TestPOSTendersAndSalesmen(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "S85G-STORE"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, 'S85G-PAYTM', 'S85G-SM1', 'S85G-SM2')", store)
	}
	cleanup()
	defer cleanup()
	put := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	put(store, "Location", map[string]interface{}{"code": store, "name": "S85G", "type": "Store", "salesman_mode": "Mandatory"})
	put("S85G-PAYTM", "POSTender", map[string]interface{}{"name": "S85G Paytm", "kind": "UPI", "stores": store + ", OTHER", "status": "Active"})
	put("S85G-SM1", "Salesman", map[string]interface{}{"name": "Ravi", "store": store, "is_default": "Yes", "status": "Active"})
	put("S85G-SM2", "Salesman", map[string]interface{}{"name": "Left Guy", "store": store, "status": "Left"})

	var ve *ValidationError
	if err := ValidateStoreTender(tenantID, store, "S85G Paytm", "UPI"); err != nil {
		t.Errorf("the store's own tender: %v", err)
	}
	if err := ValidateStoreTender(tenantID, store, "S85G Paytm", "Card"); !errors.As(err, &ve) || ve.Code != "POSOFF-0875" {
		t.Errorf("a tender sent as the wrong kind: want POSOFF-0875, got %v", err)
	}
	if err := ValidateStoreTender(tenantID, "HO", "S85G Paytm", "UPI"); !errors.As(err, &ve) || ve.Code != "POSOFF-0875" {
		t.Errorf("a tender of another store: want POSOFF-0875, got %v", err)
	}
	if err := ValidateStoreTender(tenantID, store, "", "Cash"); err != nil {
		t.Errorf("an older till sending only the kind: %v", err)
	}

	list, mode, err := StoreSalesmen(tenantID, store)
	if err != nil || mode != "Mandatory" || len(list) != 1 || !list[0].IsDefault {
		t.Fatalf("salesmen: %v %q %v", list, mode, err)
	}
	if err := ValidateBillSalesman(tenantID, store, ""); !errors.As(err, &ve) || ve.Code != "POSOFF-0876" {
		t.Errorf("no salesman where the store asks: want POSOFF-0876, got %v", err)
	}
	if err := ValidateBillSalesman(tenantID, store, "S85G-SM2"); !errors.As(err, &ve) || ve.Code != "POSOFF-0876" {
		t.Errorf("a salesman who left: want POSOFF-0876, got %v", err)
	}
	if err := ValidateBillSalesman(tenantID, store, "S85G-SM1"); err != nil {
		t.Errorf("the store's salesman: %v", err)
	}

	cart := map[string]interface{}{"payment_mode": "Cash", "payment_tender": "Cash",
		"tenders": []interface{}{map[string]interface{}{"mode": "UPI", "name": "S85G Paytm", "amount": 300.0}},
		"allocation": []interface{}{
			map[string]interface{}{"tenders": map[string]interface{}{"Cash": 50000.0, "UPI": 20000.0}},
			map[string]interface{}{"tenders": map[string]interface{}{"UPI": 10000.0, "Gift Card": 5000.0}},
		}}
	got := cartTenderTotals(cart)
	if got["Cash"] != 500 || got["S85G Paytm"] != 300 || got["Gift Card"] != 50 {
		t.Errorf("tender totals by name: %v", got)
	}
}
