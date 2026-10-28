package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Stage 135.21: a create that names an existing goods receipt's id is
// refused, so the receipt's stock is never posted twice - whether the create
// is sent again later or twice at once.
func TestStage135GRNCreateNeverLandsOnAnExistingReceipt(t *testing.T) {
	db.InitDB(testConnStr())
	const id, store, sku = "T13521-GRN", "T13521-STORE", "T13521-SKU"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T13521-%' OR (doctype = 'StockLedgerEntry' AND data->>'sku' = $1)`, sku)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1`, sku)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.item_cost WHERE sku = $1`, sku)
	}
	seed := func() {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
			($1, 'Location', '{"code":"T13521-STORE","name":"T13521 store","type":"Store","status":"Active"}', 'Active', 'admin'),
			('T13521-VEND', 'Vendor', '{"code":"T13521-VEND","name":"T13521 vendor","status":"Active"}', 'Active', 'admin'),
			('T13521-PO', 'PurchaseOrder', '{"code":"T13521-PO","vendor":"T13521-VEND","items":"[{\"sku\":\"T13521-SKU\",\"qty\":20}]"}', 'Approved', 'admin')`, store); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	cleanup()
	defer cleanup()
	seed()

	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	create := func() (int, string) {
		payload, _ := json.Marshal(map[string]interface{}{
			"id": id, "code": id, "po_id": "T13521-PO", "location": store, "status": "Approved",
			"received_items": `[{"sku":"T13521-SKU","qty":5,"unit_cost":100}]`,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/GRN", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "GRN")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec.Code, rec.Body.String()
	}
	onHand := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(on_hand), 0) FROM tenant_default.inventory_availability WHERE sku = $1`, sku).Scan(&n)
		return n
	}

	if code, body := create(); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("first create: %d %s", code, body)
	}
	if got := onHand(); got != 5 {
		t.Fatalf("on hand after the receipt = %d, want 5", got)
	}
	if code, body := create(); code != http.StatusConflict {
		t.Errorf("the same create again: %d %s, want 409", code, body)
	}

	// Two at once on a fresh id: one posts, the other is refused.
	cleanup()
	seed()
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = create()
		}(i)
	}
	wg.Wait()
	conflicts := 0
	for _, c := range codes {
		if c == http.StatusConflict {
			conflicts++
		}
	}
	if conflicts != len(codes)-1 || onHand() != 5 {
		t.Errorf("4 creates at once: codes %v, on hand %d - want one saved, three refused, 5 on hand", codes, onHand())
	}
}
