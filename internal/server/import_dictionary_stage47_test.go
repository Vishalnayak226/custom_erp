package server

import (
	"custom_erp/db"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 47.13.2: the template route serves the column dictionary as a
// variant, next to the template it describes.
func TestImportTemplateDictionaryVariant(t *testing.T) {
	db.InitDB(testConnStr())
	get := func(variant string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/import/Vendor/template?variant="+variant, nil)
		req.SetPathValue("doctype", "Vendor")
		req.Header.Set("Resolved-Tenant-ID", "default")
		w := httptest.NewRecorder()
		handleGetImportTemplate(w, req)
		return w
	}
	w := get("dictionary")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/csv" ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "Vendor_dictionary.csv") {
		t.Fatalf("dictionary: %d %v", w.Code, w.Header())
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "order,column,label,type,required") || !strings.Contains(body, "gstin") || !strings.Contains(body, "(update)") {
		t.Errorf("dictionary body: %.300s", body)
	}
	if w := get("no-such-variant"); w.Code != 422 {
		t.Errorf("unknown variant: %d", w.Code)
	}
}
