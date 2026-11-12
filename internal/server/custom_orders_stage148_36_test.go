package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Stage 148.36: the configurator through its routes. Off: the routes and the
// masters refuse. On: the quote shows each part, a missing rate refuses, saving
// makes the Item through the normal item save (next Item number, not built from
// the choices) and a Draft order, and a quote cannot be typed in by hand.
func TestStage14836ConfiguratorThroughTheRoutes(t *testing.T) {
	db.InitDB(testConnStr())
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T14836-%' OR (doctype = 'CustomOrderQuote') OR (doctype = 'SalesOrder' AND data->>'channel' = 'Custom order') OR (doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM tenant_default.documents WHERE doctype = 'SalesOrder' AND data->>'channel' = 'Custom order')) OR (doctype = 'Item' AND data->>'product_type' = 'Custom jewellery')`)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype IN ('MetalRate','CustomChoice','CustomOrderRule') AND (created_by = 'T14836' OR data::text LIKE '%T14836%')`)
		_ = engines.SetModuleEntitlement("default", engines.ModuleCustomOrders, false, "T14836")
	}
	cleanup()
	defer cleanup()
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()

	call := func(method, path, pattern string, body interface{}, h http.HandlerFunc, pv map[string]string) (int, string) {
		var rd *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = bytes.NewReader(raw)
		} else {
			rd = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		for k, v := range pv {
			req.SetPathValue(k, v)
		}
		rec := httptest.NewRecorder()
		apiMiddleware(h)(rec, req)
		return rec.Code, rec.Body.String()
	}
	quote := func(h http.HandlerFunc, path string, body interface{}) (int, string) {
		return call(http.MethodPost, path, "", body, moduleGate("custom_orders", h), nil)
	}
	order := map[string]interface{}{"metal": "T14836Gold", "purity": "22K", "making_type": "T14836Hand", "net_weight": 10,
		"customer_name": "T14836 Asha", "shipping_address": "12 MG Road"}

	// Off.
	if code, body := quote(handleCustomOrderQuote, "/api/v1/sales/custom-orders/quote", order); code != http.StatusForbidden || !strings.Contains(body, "SAAS-0191") {
		t.Fatalf("quote while off: %d %s", code, body)
	}
	if code, body := quote(handleCreateCustomOrder, "/api/v1/sales/custom-orders", order); code != http.StatusForbidden {
		t.Fatalf("order while off: %d %s", code, body)
	}
	if code, body := call(http.MethodGet, "/api/v1/doc/MetalRate", "", nil, handleGenericDoc, map[string]string{"doctype": "MetalRate"}); code == http.StatusOK {
		t.Fatalf("the rate master must refuse while off: %d %s", code, body)
	}

	if err := engines.SetModuleEntitlement("default", engines.ModuleCustomOrders, true, "T14836"); err != nil {
		t.Fatal(err)
	}
	// Masters through the generic route, as an admin would key them.
	save := func(doctype string, body map[string]interface{}) {
		t.Helper()
		if code, out := call(http.MethodPost, "/api/v1/doc/"+doctype, "", body, handleGenericDoc, map[string]string{"doctype": doctype}); code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("save %s: %d %s", doctype, code, out)
		}
	}
	save("CustomChoice", map[string]interface{}{"kind": "Metal", "value": "T14836Gold", "status": "Active", "hsn_code": "7113", "gst_rate": 3})
	save("CustomChoice", map[string]interface{}{"kind": "Purity", "value": "22K", "status": "Active"})
	save("CustomChoice", map[string]interface{}{"kind": "Making type", "value": "T14836Hand", "status": "Active", "making_basis": "Per gram", "making_value": 500})

	if code, body := quote(handleCustomOrderQuote, "/api/v1/sales/custom-orders/quote", order); code != http.StatusUnprocessableEntity || !strings.Contains(body, "SALESP-0761") {
		t.Fatalf("no rate: %d %s", code, body)
	}
	var today string
	_ = db.DB.QueryRow(`SELECT to_char(` + "(NOW() AT TIME ZONE 'Asia/Kolkata')::date" + `, 'YYYY-MM-DD')`).Scan(&today)
	save("MetalRate", map[string]interface{}{"metal": "T14836Gold", "purity": "22K", "rate_date": today, "rate_per_gram": 6000})

	code, body := quote(handleCustomOrderQuote, "/api/v1/sales/custom-orders/quote", order)
	if code != http.StatusOK {
		t.Fatalf("quote: %d %s", code, body)
	}
	var p engines.CustomOrderPrice
	_ = json.Unmarshal([]byte(body), &p)
	if p.Total != 65000 || len(p.Lines) != 2 {
		t.Fatalf("quote parts: %s", body)
	}

	// No customer: refused before any item is made.
	var items int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'Item' AND data->>'product_type' = 'Custom jewellery'`).Scan(&items)
	bad := map[string]interface{}{"metal": "T14836Gold", "purity": "22K", "making_type": "T14836Hand", "net_weight": 10}
	if code, body := quote(handleCreateCustomOrder, "/api/v1/sales/custom-orders", bad); code != http.StatusUnprocessableEntity || !strings.Contains(body, "SALESP-0766") {
		t.Fatalf("no customer: %d %s", code, body)
	}
	var after int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'Item' AND data->>'product_type' = 'Custom jewellery'`).Scan(&after)
	if after != items {
		t.Fatalf("a refused order must not leave an item behind (%d -> %d)", items, after)
	}

	code, body = quote(handleCreateCustomOrder, "/api/v1/sales/custom-orders", order)
	if code != http.StatusOK {
		t.Fatalf("make order: %d %s", code, body)
	}
	var res engines.CustomOrderResult
	_ = json.Unmarshal([]byte(body), &res)
	if res.ItemID == "" || res.OrderID == "" || res.QuoteID == "" {
		t.Fatalf("result: %s", body)
	}
	if strings.Contains(strings.ToLower(res.ItemID), "gold") || strings.Contains(res.ItemID, "22K") {
		t.Fatalf("the item ID must not be built from the choices: %s", res.ItemID)
	}
	var sale, hsn, status string
	if err := db.DB.QueryRow(`SELECT data->>'sale_price', data->>'hsn_code', status FROM tenant_default.documents WHERE id = $1 AND doctype = 'Item'`, res.ItemID).Scan(&sale, &hsn, &status); err != nil || sale != "65000" || hsn != "7113" {
		t.Fatalf("item: %s %s %v", sale, hsn, err)
	}
	var os, sku string
	_ = db.DB.QueryRow(`SELECT data->>'order_status' FROM tenant_default.documents WHERE id = $1`, res.OrderID).Scan(&os)
	_ = db.DB.QueryRow(`SELECT data->>'sku' FROM tenant_default.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1`, res.OrderID).Scan(&sku)
	if os != "Draft" || sku != res.ItemID {
		t.Fatalf("order %q line sku %q (item %q)", os, sku, res.ItemID)
	}

	// Review fix (M11/M10): a double press with one command key makes one order and one item.
	count := func() (int, int) {
		var it, so int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'Item' AND data->>'product_type' = 'Custom jewellery' AND deleted_at IS NULL`).Scan(&it)
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'SalesOrder' AND data->>'channel' = 'Custom order'`).Scan(&so)
		return it, so
	}
	it0, so0 := count()
	press := fmt.Sprintf("T14836-press-%d", time.Now().UnixNano())
	keyed := func() (int, string) {
		raw, _ := json.Marshal(order)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sales/custom-orders", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", press)
		rec := httptest.NewRecorder()
		apiMiddleware(moduleGate("custom_orders", commandKeyed("custom_orders.create", handleCreateCustomOrder)))(rec, req)
		return rec.Code, rec.Body.String()
	}
	c1, b1 := keyed()
	c2, b2 := keyed()
	it1, so1 := count()
	if c1 != http.StatusOK || c2 != http.StatusOK || !sameJSON(b1, b2) || it1 != it0+1 || so1 != so0+1 {
		t.Fatalf("two presses with one key: %d/%d items %d->%d orders %d->%d: %s | %s", c1, c2, it0, it1, so0, so1, b1, b2)
	}

	// A quote cannot be typed in by hand, even by an admin.
	if code, body := call(http.MethodPost, "/api/v1/doc/CustomOrderQuote", "", map[string]interface{}{"total": 1, "status": "Ordered"}, handleGenericDoc, map[string]string{"doctype": "CustomOrderQuote"}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "SALESP-0768") {
		t.Fatalf("typed quote: %d %s", code, body)
	}
}

// sameJSON compares two JSON bodies by value; a replayed answer can order keys differently.
func sameJSON(a, b string) bool {
	var x, y interface{}
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
