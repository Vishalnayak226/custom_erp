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

// Ask 272.1/272.2: a design's number - which lives on the design and can
// differ from its Design ID - finds the design's combinations in the item
// search, only where the search covers the design field; and the till's
// availability call, with describe=1, gives each combination's Design No.
func TestItemSearchFindsCombinationsByDesignNumber(t *testing.T) {
	db.InitDB(testConnStr())
	docs := map[string][2]string{
		"T272-FAM":     {"ProductFamily", `{"code":"T272-FAM","name":"Necklace","design_number":"T272DN-79928"}`},
		"T272-COMBO-1": {"Item", `{"code":"T272-COMBO-1","name":"Necklace Red","family":"T272-FAM","parent_product_code":"T272-FAM","barcode":"T272-BC1"}`},
		"T272-OTHER":   {"Item", `{"code":"T272-OTHER","name":"Other"}`},
	}
	cleanup := func() {
		for id := range docs {
			_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
		}
	}
	cleanup()
	defer cleanup()
	for id, d := range docs {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`, id, d[0], d[1]); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	get := func(path string, h http.HandlerFunc, doctype string) []byte {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if doctype != "" {
			req.SetPathValue("doctype", doctype)
		}
		rec := httptest.NewRecorder()
		apiMiddleware(h)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d %s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	list := func(query string) string {
		t.Helper()
		var rows []map[string]interface{}
		_ = json.Unmarshal(get("/api/v1/doc/Item?"+query+"&limit=50", handleGenericDoc, "Item"), &rows)
		var got []string
		for _, r := range rows {
			if c, _ := r["code"].(string); docs[c][0] == "Item" {
				got = append(got, c)
			}
		}
		sort.Strings(got)
		return strings.Join(got, ",")
	}
	cases := []struct{ query, want string }{
		{"q=T272DN-79928", "T272-COMBO-1"},                  // the till's box: every field
		{"q=t272dn-799&q_fields=@identity", "T272-COMBO-1"}, // an item box: part of it, any case
		{"q=T272DN-79928&q_fields=name,code", ""},           // a search that skips the design field
		{"q=T272DN-79928&q_fields=parent_product_code", "T272-COMBO-1"},
	}
	for _, c := range cases {
		if got := list(c.query); got != c.want {
			t.Errorf("%s: got %q, want %q", c.query, got, c.want)
		}
	}

	var avail struct {
		ATS      map[string]int    `json:"ats"`
		DesignNo map[string]string `json:"design_no"`
	}
	_ = json.Unmarshal(get("/api/v1/availability?skus=T272-COMBO-1,T272-OTHER&location=HO&describe=1", handleGetAvailability, ""), &avail)
	if _, ok := avail.ATS["T272-COMBO-1"]; !ok {
		t.Errorf("availability lost its ats: %+v", avail)
	}
	if avail.DesignNo["T272-COMBO-1"] != "T272DN-79928" {
		t.Errorf("describe=1: design_no %v, want T272-COMBO-1 -> T272DN-79928", avail.DesignNo)
	}
	if _, ok := avail.DesignNo["T272-OTHER"]; ok {
		t.Errorf("an item with no design has no Design No: %v", avail.DesignNo)
	}
	avail.DesignNo = nil
	_ = json.Unmarshal(get("/api/v1/availability?skus=T272-COMBO-1&location=HO", handleGetAvailability, ""), &avail)
	if avail.DesignNo != nil {
		t.Errorf("without describe=1 the reply is as before, got design_no %v", avail.DesignNo)
	}
}
