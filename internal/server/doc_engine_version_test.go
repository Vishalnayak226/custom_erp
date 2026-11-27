package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDocSaveVersionConflict (290.3): every read gives a record's version, a
// save answers the version it made, and a save that names an old version is
// refused with a 409 instead of overwriting the newer edit. A save with no
// version keeps the old last-write-wins.
func TestDocSaveVersionConflict(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const doctype = "ReorderPointConfig"
	schema, _ := db.GetTenantSchema(tenantID)
	clean := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = '" + doctype + "' AND data->>'item' = 'S2903-ITEM'")
	}
	clean()
	defer clean()

	call := func(method, path string, body map[string]interface{}) (int, map[string]interface{}, []map[string]interface{}) {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.SetPathValue("doctype", doctype)
		if parts := strings.Split(strings.SplitN(path, "?", 2)[0], "/"); len(parts) == 6 {
			id, _ := url.PathUnescape(parts[5])
			r.SetPathValue("id", id)
		}
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		r.Header.Set("Resolved-User-ID", "admin")
		r.Header.Set("Resolved-Username", "admin")
		r.Header.Set("Resolved-Location", "HO")
		w := httptest.NewRecorder()
		handleGenericDoc(w, r)
		var one map[string]interface{}
		var many []map[string]interface{}
		if strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
			_ = json.Unmarshal(w.Body.Bytes(), &many)
		} else {
			_ = json.Unmarshal(w.Body.Bytes(), &one)
		}
		if w.Code != 200 && w.Code != 409 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Code, one, many
	}
	doc := func(extra map[string]interface{}) map[string]interface{} {
		d := map[string]interface{}{"code": "S2903-A", "item": "S2903-ITEM", "location_code": "S2903-LOC", "lead_time_days": 2, "safety_stock_qty": 5, "status": "Active"}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}

	_, created, _ := call("POST", "/api/v1/doc/"+doctype, doc(nil))
	id, _ := created["id"].(string)
	if id == "" || created["version"] != 1.0 {
		t.Fatalf("create answered %v, want an id and version 1", created)
	}
	path := "/api/v1/doc/" + doctype + "/" + url.PathEscape(id)

	if _, read, _ := call("GET", path, nil); read["version"] != 1.0 {
		t.Fatalf("read gives version %v, want 1", read["version"])
	}
	code, saved, _ := call("POST", path, doc(map[string]interface{}{"safety_stock_qty": 6, "expected_version": 1}))
	if code != 200 || saved["version"] != 2.0 {
		t.Fatalf("save from version 1: %d %v, want 200 and version 2", code, saved)
	}
	// A second user still holding version 1 is refused, and the first edit stays.
	if code, refused, _ := call("POST", path, doc(map[string]interface{}{"safety_stock_qty": 9, "expected_version": 1})); code != 409 || !strings.Contains(refused["error"].(string), "modified by someone else") {
		t.Fatalf("stale save: %d %v, want 409", code, refused)
	}
	if _, read, _ := call("GET", path, nil); read["safety_stock_qty"] != 6.0 || read["version"] != 2.0 {
		t.Fatalf("after the refused save the record is %v, want qty 6 at version 2", read)
	}
	// No version named: the old last-write-wins.
	if code, saved, _ := call("POST", path, doc(map[string]interface{}{"safety_stock_qty": 7})); code != 200 || saved["version"] != 3.0 {
		t.Fatalf("save with no version: %d %v, want 200 and version 3", code, saved)
	}
	// A list row carries the real version, not one copied into the stored data.
	if code, saved, _ := call("POST", path, doc(map[string]interface{}{"version": 1, "expected_version": 3})); code != 200 || saved["version"] != 4.0 {
		t.Fatalf("save with a stale version in its body: %d %v, want 200 and version 4", code, saved)
	}
	_, _, rows := call("GET", "/api/v1/doc/"+doctype+"?limit=500", nil)
	found := false
	for _, row := range rows {
		if row["id"] == id {
			found = true
			if row["version"] != 4.0 {
				t.Errorf("list row has version %v, want 4", row["version"])
			}
		}
	}
	if !found {
		t.Fatalf("record %s missing from the list", id)
	}
}
