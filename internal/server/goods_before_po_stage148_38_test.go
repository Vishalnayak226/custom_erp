package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 148.38: a receipt with no PO is refused as it always was; with the
// setting on it takes a reason code, posts its stock the normal way, and is
// listed until its retro PO is approved.
func TestStage14838GRNWithoutPOThroughTheHandler(t *testing.T) {
	db.InitDB(testConnStr())
	const store, sku, vend = "T14838-STORE", "T14838-SKU", "T14838-VEND"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T14838-%' OR (doctype = 'StockLedgerEntry' AND data->>'sku' = $1)`, sku)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1`, sku)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.item_cost WHERE sku = $1`, sku)
		_ = engines.SetSetting("default", "procurement.goods_before_po", "false", "admin")
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Location', '{"code":"T14838-STORE","name":"T14838 store","type":"Store","status":"Active"}', 'Active', 'admin'),
		('T14838-VEND', 'Vendor', '{"code":"T14838-VEND","name":"T14838 vendor","status":"Active"}', 'Active', 'admin')`, store); err != nil {
		t.Fatalf("seed: %v", err)
	}
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	create := func(id string, extra map[string]interface{}) (int, string) {
		body := map[string]interface{}{"id": id, "code": id, "location": store, "status": "Approved", "vendor": vend,
			"received_items": `[{"sku":"T14838-SKU","qty":5,"unit_cost":100,"invoice_rate":100}]`}
		for k, v := range extra {
			body[k] = v
		}
		payload, _ := json.Marshal(body)
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

	// Off: exactly today's refusal, even with a reason and a forged flag.
	if code, body := create("T14838-G0", map[string]interface{}{"before_po_reason": "GBP-URGENT", "received_before_po": "Yes"}); code != http.StatusUnprocessableEntity || !strings.Contains(body, "po_id") {
		t.Fatalf("off: %d %s, want 422 naming po_id", code, body)
	}
	if onHand() != 0 {
		t.Fatalf("off: stock moved")
	}

	if err := engines.SetSetting("default", "procurement.goods_before_po", "true", "admin"); err != nil {
		t.Fatalf("setting: %v", err)
	}
	// On, no reason: refused with its own code.
	if code, body := create("T14838-G1", nil); code != http.StatusUnprocessableEntity || !strings.Contains(body, "PURCHA-5030") {
		t.Fatalf("on, no reason: %d %s, want 422 PURCHA-5030", code, body)
	}
	// On, a reason of another kind: refused.
	if code, body := create("T14838-G1b", map[string]interface{}{"before_po_reason": "GRN-REJ-QUALITY"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("on, wrong reason: %d %s, want 422", code, body)
	}
	// On, a reason: received, stock moves the normal way.
	if code, body := create("T14838-G2", map[string]interface{}{"before_po_reason": "GBP-URGENT"}); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("on, reason: %d %s", code, body)
	}
	if got := onHand(); got != 5 {
		t.Fatalf("on hand = %d, want 5", got)
	}
	var flag, due, reason string
	if err := db.DB.QueryRow(`SELECT COALESCE(data->>'received_before_po',''), COALESCE(data->>'before_po_due',''), COALESCE(data->>'before_po_reason','') FROM tenant_default.documents WHERE id = 'T14838-G2'`).Scan(&flag, &due, &reason); err != nil {
		t.Fatal(err)
	}
	if flag != "Yes" || due == "" || reason != "GBP-URGENT" {
		t.Fatalf("flag/due/reason = %q/%q/%q", flag, due, reason)
	}

	// An edit through the form can neither clear the mark nor link a PO by hand.
	edit, _ := json.Marshal(map[string]interface{}{"id": "T14838-G2", "code": "T14838-G2", "location": store, "status": "Approved", "vendor": vend,
		"received_items":   `[{"sku":"T14838-SKU","qty":5,"unit_cost":100,"invoice_rate":100}]`,
		"before_po_reason": "GBP-URGENT", "received_before_po": "", "retro_po_id": "PO-FAKE"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/GRN/T14838-G2", bytes.NewReader(edit))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("doctype", "GRN")
	req.SetPathValue("id", "T14838-G2")
	rec := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec, req)
	var retro string
	if err := db.DB.QueryRow(`SELECT COALESCE(data->>'received_before_po',''), COALESCE(data->>'retro_po_id','') FROM tenant_default.documents WHERE id = 'T14838-G2'`).Scan(&flag, &retro); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("the edit was refused: %d %s", rec.Code, rec.Body.String())
	}
	if flag != "Yes" || retro != "" {
		t.Fatalf("after an edit (%d %s): flag %q, retro PO %q", rec.Code, rec.Body.String(), flag, retro)
	}
}
