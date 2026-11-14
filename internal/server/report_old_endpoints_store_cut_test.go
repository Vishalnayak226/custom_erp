package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// 157.4c: the old GET /reports/current-stock and /reports/sales-register
// endpoints cut a store-limited user's rows to their stores, as the report
// page does; a full-access user still sees every store.
func TestStage1574cOldReportEndpointsTakeTheStoreCut(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID, user, role = "default", "manager1", "Store Manager"
	before, err := engines.GetUserStoreAccess(tenantID, user)
	if err != nil {
		t.Skipf("no %s user in this database: %v", user, err)
	}
	if err := engines.SetUserStoreAccess(tenantID, user, engines.StoreAccessHome, nil, "test"); err != nil {
		t.Fatalf("pin %s to the home store: %v", user, err)
	}
	defer engines.SetUserStoreAccess(tenantID, user, before.Access, before.Stores, "test")
	stores, cut := engines.ReportStoreCut(tenantID, role, user)
	if !cut || len(stores) != 1 {
		t.Fatalf("want one store for %s, got %v (cut %v)", user, stores, cut)
	}
	home := stores[0]
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const sku, other = "SKU-1574C", "OTHER-1574C"
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE 'CART-1574C-%%'`, schema))
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_availability WHERE sku = $1`, schema), sku)
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, wh := range []string{home, other} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)`, schema), sku, wh); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1::text, 'POSCart', jsonb_build_object('cart_number', $1::text, 'location', $2::text, 'items', jsonb_build_array(jsonb_build_object('qty', 1, 'sale_price', 10))), 'Paid', 'admin')`, schema),
			"CART-1574C-"+wh, wh); err != nil {
			t.Fatalf("seed cart: %v", err)
		}
	}

	get := func(path, role, user string, h http.HandlerFunc) []map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Resolved-Tenant-ID", tenantID)
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-User-ID", user)
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", path, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return rows
	}
	ours := func(rows []map[string]interface{}, key string) map[string]bool {
		seen := map[string]bool{}
		for _, r := range rows {
			if loc, _ := r[key].(string); loc == home || loc == other {
				if key == "location" && r["cart_number"] != "CART-1574C-"+loc {
					continue
				}
				seen[loc] = true
			}
		}
		return seen
	}
	for _, c := range []struct {
		path, key string
		h         http.HandlerFunc
	}{
		{"/api/v1/reports/current-stock?q=" + sku, "location_code", handleCurrentStockReport},
		{"/api/v1/reports/sales-register", "location", handleSalesRegisterReport},
	} {
		limited := get(c.path, role, user, c.h)
		for _, r := range limited {
			if loc, _ := r[c.key].(string); loc != home {
				t.Fatalf("%s showed %s store %q to a user limited to %q", c.path, c.key, loc, home)
			}
		}
		if seen := ours(limited, c.key); !seen[home] {
			t.Fatalf("%s hid the user's own store %q", c.path, home)
		}
		if seen := ours(get(c.path, "Super Admin", "admin", c.h), c.key); !seen[home] || !seen[other] {
			t.Fatalf("%s: a full-access user saw %v, want both stores", c.path, seen)
		}
	}
}
