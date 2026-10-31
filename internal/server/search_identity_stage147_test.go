package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 147.2: an item box (q_fields=@identity) finds an item by its Design
// ID, Combination ID or barcode - letters allowed (D100000, 100000D) - and not
// by its name; the PO's box, asking for the name too, finds it by name.
func TestItemIdentitySearchDesignCombinationBarcode(t *testing.T) {
	db.InitDB(testConnStr())
	docs := map[string]string{
		"T147QW-100000D": `{"code":"T147QW-100000D","name":"Ring","parent_product_code":"T147QW-D100000","family":"T147QW-D100000"}`,
		"T147-BC":        `{"code":"T147-BC","name":"Kada","barcode":"T147QW-B7"}`,
		"T147-NAME":      `{"code":"T147-NAME","name":"T147QW Necklace"}`,
	}
	cleanup := func() {
		for id := range docs {
			_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	}
	cleanup()
	defer cleanup()
	for id, data := range docs {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, id, data); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	list := func(query string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "Item")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", query, rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		var got []string
		for _, r := range rows {
			if c, _ := r["code"].(string); docs[c] != "" {
				got = append(got, c)
			}
		}
		sort.Strings(got)
		return strings.Join(got, ",")
	}
	cases := []struct{ query, want string }{
		{"q=t147qw-d1000&q_fields=@identity", "T147QW-100000D"},                                  // Design ID, any case
		{"q=T147QW-100000D&q_fields=@identity", "T147QW-100000D"},                                // Combination ID
		{"q=T147QW-B7&q_fields=@identity", "T147-BC"},                                            // barcode
		{"q=T147QW+Necklace&q_fields=@identity", ""},                                             // never the name
		{"q=T147QW+Necklace&q_fields=name,code,barcode,parent_product_code,family", "T147-NAME"}, // the PO's box
	}
	for _, c := range cases {
		if got := list(c.query + "&limit=50"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.query, got, c.want)
		}
	}
	for _, f := range engines.SearchIdentityFields("default", "Item") {
		if f == "name" || f == "item_name" {
			t.Errorf("Item @identity must not include %s", f)
		}
	}
}
