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

// The design grid asks the PO preview route; a preview with no rows answers as ever.
func TestStage1486PreviewAnswersTheDesignGrid(t *testing.T) {
	db.InitDB(testConnStr())
	const vendor, agreement, cat = "TEST14864-V", "TEST14864-A", "TEST14864-CAT"
	clean := func() {
		db.DB.Exec("DELETE FROM tenant_default.documents WHERE id IN ($1, $2)", vendor, agreement)
	}
	clean()
	t.Cleanup(clean)
	seed := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": vendor})
	seed(agreement, "PriceListVersion", "Approved", map[string]interface{}{"code": agreement, "price_list_code": agreement, "vendor": vendor,
		"effective_from": "2026-01-01", "currency": "INR", "status": "Approved", "items": `[{"category":"` + cat + `","price":100}]`})
	// The shipped `system` account is Inactive (SB-021), so act as a seeded admin.
	actor, dropActor := seedContractUser(t, "HR/Admin", "HO")
	defer dropActor()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/procurement/purchase-order/preview", apiMiddleware(handlePreviewPurchaseOrder))
	post := func(body string) *httptest.ResponseRecorder {
		token := engines.SignToken(actor, actor, "HR/Admin", "default", "HO", currentCredentialVersion(actor))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/procurement/purchase-order/preview", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post(`{"vendor":"` + vendor + `","items":"[]","design_rows":[{"category":"` + cat + `","qty":2},{"category":"OTHER","qty":2}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		DesignRows []engines.DesignRowPrice `json:"design_rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.DesignRows) != 2 || got.DesignRows[0].Rate != 100 || got.DesignRows[1].Rate != 0 || got.DesignRows[1].Note != "no agreement for this item" {
		t.Errorf("design rows %+v", got.DesignRows)
	}

	plain := post(`{"vendor":"` + vendor + `","items":"[]"}`)
	if plain.Code != http.StatusOK || strings.Contains(plain.Body.String(), "design_rows") {
		t.Errorf("a plain preview: %d %s", plain.Code, plain.Body.String())
	}
}
