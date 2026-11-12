package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 65.8: the PO preview carries the chosen stores' Billing and Shipping
// address - the same blocks the printed PO uses - so the composer can show
// them under its pickers. Neither block is sent when its store is not chosen.
func TestPurchaseOrderPreviewCarriesStoreAddresses(t *testing.T) {
	db.InitDB(testConnStr())
	const bill, ship = "LOC-PO658-BILL", "LOC-PO658-SHIP"
	clean := func() {
		db.DB.Exec("DELETE FROM tenant_default.documents WHERE doctype = 'Location' AND id IN ($1, $2)", bill, ship)
	}
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Location', '{"code":"LOC-PO658-BILL","name":"Billing Store","type":"Store","status":"Active","address":"12, 5th Cross","city":"Bengaluru","pincode":"560078"}', 'Active', 'system'),
		($2, 'Location', '{"code":"LOC-PO658-SHIP","name":"Shipping Warehouse","type":"Warehouse","status":"Active","address":"Plot 4, Peenya","city":"Bengaluru","pincode":"560058"}', 'Active', 'system')`, bill, ship); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/procurement/purchase-order/preview", apiMiddleware(handlePreviewPurchaseOrder))
	preview := func(body string) map[string]json.RawMessage {
		token := engines.SignToken("system", "system", "Super Admin", "default", "HO", currentCredentialVersion("system"))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/procurement/purchase-order/preview", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview %s: status=%d body=%s", body, rec.Code, rec.Body.String())
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	out := preview(`{"location":"LOC-PO658-BILL","target_warehouse":"LOC-PO658-SHIP","items":"[]"}`)
	var billTo, shipTo engines.PrintStore
	if err := json.Unmarshal(out["bill_to"], &billTo); err != nil || billTo.Name != "Billing Store" || billTo.Address != "12, 5th Cross" || billTo.Pincode != "560078" {
		t.Errorf("bill_to = %+v (err %v), want the billing store's name and address", billTo, err)
	}
	if err := json.Unmarshal(out["ship_to"], &shipTo); err != nil || shipTo.Name != "Shipping Warehouse" || shipTo.Address != "Plot 4, Peenya" {
		t.Errorf("ship_to = %+v (err %v), want the shipping warehouse's name and address", shipTo, err)
	}
	if _, ok := out["lines"]; !ok {
		t.Errorf("preview lost its own fields: %v", out)
	}

	out = preview(`{"items":"[]"}`)
	if _, ok := out["bill_to"]; ok {
		t.Errorf("bill_to sent with no billing store chosen: %s", out["bill_to"])
	}
	if _, ok := out["ship_to"]; ok {
		t.Errorf("ship_to sent with no shipping store chosen: %s", out["ship_to"])
	}
}
