package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// storeHasState gives a default-client store a State for one test when it has
// none (49.5.6e: a PO billed to a store with no state is refused, and the
// seed store HO has none), and takes it off again afterwards.
func storeHasState(t *testing.T, store string) {
	t.Helper()
	res, err := db.DB.Exec(`UPDATE tenant_default.documents SET data = data || '{"state":"Maharashtra"}'::jsonb
		WHERE doctype = 'Location' AND (id = $1 OR data->>'code' = $1) AND COALESCE(data->>'state', '') = ''`, store)
	if err != nil {
		t.Fatalf("give store %s a state: %v", store, err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		t.Cleanup(func() {
			db.DB.Exec(`UPDATE tenant_default.documents SET data = data - 'state' WHERE doctype = 'Location' AND (id = $1 OR data->>'code' = $1)`, store)
		})
	}
}

// Stage 49.5.6b through the real save: a vendor needs a state, a PO for a
// vendor with none is refused, and the PO's supply type is the derived one
// whatever the client sends.
func TestStage4956bVendorStateAndPOSupplyOnSave(t *testing.T) {
	db.InitDB(testConnStr())
	if db.DB == nil || db.DB.Ping() != nil {
		t.Skip("no test database")
	}
	const vNone, vMH, loc, sku = "T4956S-VNONE", "T4956S-VMH", "T4956S-LOC", "T4956S-SKU"
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T4956S-%' OR (doctype = 'PurchaseOrder' AND data->>'vendor' LIKE 'T4956S-%')
			OR (doctype = 'Vendor' AND data->>'name' = 'T4956S New Vendor')`)
	}
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Vendor', '{"code":"T4956S-VNONE","name":"T4956S Stateless"}', 'Active', 'admin'),
		($2, 'Vendor', '{"code":"T4956S-VMH","name":"T4956S Mumbai","state":"Maharashtra"}', 'Active', 'admin'),
		($3, 'Location', '{"code":"T4956S-LOC","name":"T4956S Store","type":"Store","state":"Karnataka","status":"Active"}', 'Active', 'admin'),
		($4, 'Item', '{"code":"T4956S-SKU","name":"T4956S Tee","hsn_code":"6109","gst_rate":5,"tax_treatment":"Taxable"}', 'Active', 'admin')`,
		vNone, vMH, loc, sku); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))

	if rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/Vendor", token,
		map[string]interface{}{"name": "T4956S New Vendor", "status": "Active"}); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "GLOBAL-0001") {
		t.Errorf("a new vendor with no state: %d %s", rec.Code, rec.Body.String())
	}

	po := func(vendor string) map[string]interface{} {
		return map[string]interface{}{"vendor": vendor, "vendor_id": vendor, "location": loc, "target_warehouse": loc,
			"items": `[{"sku":"` + sku + `","qty":2,"rate":100}]`, "gst_mode": engines.GSTModeExclusive, "total_amount": 200, "status": "Draft",
			"interstate": false, "interstate_override": true}
	}
	if rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/PurchaseOrder", token, po(vNone)); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), engines.POVendorNoStateCode) || !strings.Contains(rec.Body.String(), "T4956S Stateless") {
		t.Errorf("a PO for a vendor with no state: %d %s", rec.Code, rec.Body.String())
	}

	// Maharashtra vendor, Karnataka store: inter-state, though the client
	// claimed intra-state with the override ticked.
	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/PurchaseOrder", token, po(vMH))
	if rec.Code != http.StatusOK {
		t.Fatalf("a PO for a vendor with a state: %d %s", rec.Code, rec.Body.String())
	}
	var raw string
	if err := db.DB.QueryRow(`SELECT data FROM tenant_default.documents WHERE doctype = 'PurchaseOrder' AND data->>'vendor' = $1`, vMH).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &saved)
	gst, _ := saved["gst_breakdown"].(map[string]interface{})
	if saved["interstate"] != true || saved["interstate_override"] != false || gst == nil || gst["igst"] == float64(0) {
		t.Errorf("saved interstate %v, override %v, breakdown %v - want the derived inter-state (IGST)", saved["interstate"], saved["interstate_override"], gst)
	}
}

// The Bulk Purchase Upload's preview says so before Apply would fail.
func TestStage4956bBulkPurchasePreviewRefusesAStatelessVendor(t *testing.T) {
	c := swTestCaller(t)
	probe, err := newSWRun(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := probe.resolveLink("Location", "HO"); err != nil || id == "" {
		t.Skip("the test database has no store HO")
	}
	sfx := fmt.Sprint(time.Now().UnixNano() % 1e9)
	vendor := "T4956S Bulk Stateless " + sfx
	db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Vendor', $2, 'Active', 'admin')`,
		"T4956S-BV-"+sfx, `{"code":"T4956S-BV-`+sfx+`","name":"`+vendor+`"}`)
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, "T4956S-BV-"+sfx) })
	row := []string{"", "BPS-" + sfx, vendor, "HO", "1", "Earring", "BPSCat" + sfx, "7117 - Imitation / fashion jewellery", "3", "Red", "", "1", "100", "299", "", "", "", ""}
	file := swBook(t, "", map[string][][]string{"Purchase": {bpTestPurchaseHead, row}}, "Purchase")
	res := bpRunFile(t, c, file, false)
	if len(res.POs) != 1 || res.POs[0].Action != "error" || !strings.Contains(res.POs[0].Message, "no state") {
		t.Fatalf("preview of a PO for a vendor with no state: %+v", res.POs)
	}
}

// Stage 49.5.6c through the real routes: a save cannot set or clear the
// acceptance; the action needs Production Order update permission and a reason.
func TestStage4956cBOMChangeAcceptedOnlyByTheAction(t *testing.T) {
	db.InitDB(testConnStr())
	if db.DB == nil || db.DB.Ping() != nil {
		t.Skip("no test database")
	}
	const order, bom = "T4956S-PRO", "T4956S-BOM"
	clean := func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T4956S-%'`) }
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		('T4956S-KIT', 'Item', '{"code":"T4956S-KIT","name":"T4956S Kit"}', 'Active', 'admin'),
		('T4956S-CLOTH', 'Item', '{"code":"T4956S-CLOTH","name":"T4956S Cloth"}', 'Active', 'admin'),
		($1, 'BOM', '{"parent_item":"T4956S-KIT","components":"[{\"sku\":\"T4956S-CLOTH\",\"qty\":3}]"}', 'Active', 'admin'),
		($2, 'ProductionOrder', '{"code":"T4956S-PRO","bom_id":"T4956S-BOM","quantity":4,"location":"HO","status":"Material Issued","bom_snapshot":"[{\"sku\":\"T4956S-CLOTH\",\"qty\":2}]"}', 'Material Issued', 'admin')`,
		bom, order); err != nil {
		t.Fatalf("seed: %v", err)
	}
	acknowledged := func() interface{} {
		var raw string
		_ = db.DB.QueryRow(`SELECT data FROM tenant_default.documents WHERE id = $1`, order).Scan(&raw)
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		return d["bom_variance_acknowledged"]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	save := func(v interface{}) *httptest.ResponseRecorder {
		return doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/ProductionOrder/"+order, token, map[string]interface{}{"bom_variance_acknowledged": v})
	}
	if rec := save(true); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), engines.ActionOnlyFieldCode) || acknowledged() != nil {
		t.Errorf("a save setting the acceptance: %d %s (stored %v)", rec.Code, rec.Body.String(), acknowledged())
	}

	act := func(role, reason string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"order_id": order, "reason": reason})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/manufacturing/acknowledge-bom-variance", bytes.NewReader(body))
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-User-ID", "admin")
		r.Header.Set("Resolved-Role", role)
		w := httptest.NewRecorder()
		handleAcknowledgeBOMVariance(w, r)
		return w
	}
	if rec := act(engines.RoleCashier, "cloth substituted"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") {
		t.Errorf("a role without Production Order update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := act(engines.RoleSuperAdmin, ""); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), engines.BOMVarianceReasonCode) {
		t.Errorf("no reason: %d %s", rec.Code, rec.Body.String())
	}
	if acknowledged() != nil {
		t.Fatalf("a refused acceptance was stored")
	}
	if rec := act(engines.RoleSuperAdmin, "cloth substituted"); rec.Code != http.StatusOK || acknowledged() != true {
		t.Fatalf("the action: %d %s (stored %v)", rec.Code, rec.Body.String(), acknowledged())
	}
	if rec := save(false); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), engines.ActionOnlyFieldCode) || acknowledged() != true {
		t.Errorf("a save clearing the acceptance: %d %s (stored %v)", rec.Code, rec.Body.String(), acknowledged())
	}
}
