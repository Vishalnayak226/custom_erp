package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// stage1462Fixture is a throwaway tenant with a documents inserter, shared by
// the Stage 146.2 loyalty earn tests.
func stage1462Fixture(t *testing.T) (tenantID, schema string, insert func(id, doctype, status string, data map[string]interface{})) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert = func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	return
}

// Stage 146.2.2 - a dated Loyalty Scheme with its stores: no two overlap for a
// store, a sale earns under its own store's scheme on the bill date, no scheme
// is exactly today's tenant-wide rate, and the store is on every ledger row.
func TestStage14622LoyaltySchemeByStoreAndDate(t *testing.T) {
	tenantID, schema, insert := stage1462Fixture(t)
	if err := SetSetting(tenantID, "loyalty.rupees_per_point", "100", "tester"); err != nil {
		t.Fatal(err)
	}
	code := func(err error) string {
		if v, ok := err.(*ValidationError); ok {
			return v.Code
		}
		return ""
	}
	scheme := func(id string, data map[string]interface{}) error {
		data["code"], data["name"] = id, id
		if _, ok := data["status"]; !ok {
			data["status"] = "Active"
		}
		return ValidateMasterDataRules(tenantID, id, "LoyaltyScheme", data)
	}
	save := func(id string, data map[string]interface{}) {
		t.Helper()
		if err := scheme(id, data); err != nil {
			t.Fatalf("scheme %s refused: %v", id, err)
		}
		insert(id, "LoyaltyScheme", "Active", data)
	}
	save("LS-S1", map[string]interface{}{"valid_from": "2020-01-01", "stores": "S1", "rupees_per_point": 50})
	save("LS-S2", map[string]interface{}{"valid_from": "2020-01-01", "stores": "S2", "rupees_per_point": 200})

	if c := code(scheme("LS-X", map[string]interface{}{"valid_from": "2024-01-01", "valid_to": "2024-12-31", "stores": "S1,S3", "rupees_per_point": 10})); c != "CUSTOM-0690" {
		t.Errorf("a second scheme on S1 in the same dates: code %q, want CUSTOM-0690", c)
	}
	if c := code(scheme("LS-X", map[string]interface{}{"valid_from": "2024-01-01", "rupees_per_point": 10})); c != "CUSTOM-0690" {
		t.Errorf("an all-stores scheme overlaps a store's scheme: code %q, want CUSTOM-0690", c)
	}
	if err := scheme("LS-S1", map[string]interface{}{"valid_from": "2020-01-01", "stores": "S1", "rupees_per_point": 40}); err != nil {
		t.Errorf("a scheme saved again does not clash with itself: %v", err)
	}
	if err := scheme("LS-Y", map[string]interface{}{"valid_from": "2019-01-01", "valid_to": "2019-12-31", "stores": "S1", "rupees_per_point": 10}); err != nil {
		t.Errorf("the same store in other dates is fine: %v", err)
	}
	if err := scheme("LS-Z", map[string]interface{}{"valid_from": "2020-01-01", "stores": "S1", "rupees_per_point": 10, "status": "Inactive"}); err != nil {
		t.Errorf("an inactive scheme does not clash: %v", err)
	}
	if c := code(scheme("LS-W", map[string]interface{}{"valid_from": "2025-01-01", "valid_to": "2024-01-01", "stores": "S9", "rupees_per_point": 10})); c != "CUSTOM-0692" {
		t.Errorf("valid to before valid from: code %q, want CUSTOM-0692", c)
	}
	if c := code(scheme("LS-W", map[string]interface{}{"valid_from": "2025-01-01", "stores": "S9", "rupees_per_point": 0})); c != "CUSTOM-0692" {
		t.Errorf("zero spend per point: code %q, want CUSTOM-0692", c)
	}

	insert("T146-C1", "Customer", "Active", map[string]interface{}{"code": "T146-C1", "name": "Scheme customer"})
	cart := func(id, store string) {
		insert(id, "POSCart", "Paid", map[string]interface{}{"customer_id": "T146-C1", "location": store, "items": []map[string]interface{}{{"sku": "A", "qty": 1, "sale_price": 1000}}})
	}
	cart("T146-B-S1", "S1")
	cart("T146-B-S2", "S2")
	cart("T146-B-S3", "S3")
	for _, id := range []string{"T146-B-S1", "T146-B-S2", "T146-B-S3"} {
		if err := EarnLoyaltyPoints(tenantID, "T146-C1", 1000, id); err != nil {
			t.Fatal(err)
		}
	}
	ledger := func(ref string) (points int, location string) {
		var loc *string
		if err := db.DB.QueryRow(`SELECT points, location FROM `+schema+`.loyalty_point_ledger WHERE reference_id = $1 AND transaction_type = 'Earn'`, ref).Scan(&points, &loc); err != nil {
			t.Fatalf("ledger row for %s: %v", ref, err)
		}
		if loc != nil {
			location = *loc
		}
		return
	}
	for ref, want := range map[string][3]interface{}{"T146-B-S1": {20, "S1"}, "T146-B-S2": {5, "S2"}, "T146-B-S3": {10, "S3"}} {
		if p, l := ledger(ref); p != want[0].(int) || l != want[1].(string) {
			t.Errorf("%s: %d point(s) at %q, want %d at %q (S1 scheme 50, S2 scheme 200, S3 none = 100)", ref, p, l, want[0], want[1])
		}
	}
}
