package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Stage 146.3.4: the bin generator is held to the session's store like a
// hand-made Bin (49.3.8a), and what it makes reads back the same.
func TestStage14634BinGeneratorStoreScope(t *testing.T) {
	db.InitDB(testConnStr())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/wms/bin-generator", apiMiddleware(handleBinGenerator))
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	users := seedGeneratedCaseUsers(t)
	manager, admin := users[engines.RoleStoreManager], users[engines.RoleAdmin]
	suffix := contractUniqueID()
	ip := mfaTestIP()

	other := "G34-" + suffix
	raw, _ := json.Marshal(map[string]interface{}{"code": other, "name": "Another store", "location_type": "Store"})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')`, other, string(raw)); err != nil {
		t.Fatalf("seed the other store: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Bin' AND data->>'bin_code' LIKE $1`, "G34%"+suffix+"%")
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, other)
	})
	count := func(like string) (n int) {
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'Bin' AND data->>'bin_code' LIKE $1 AND deleted_at IS NULL`, like).Scan(&n)
		return
	}
	gen := func(token, location, aisle string, preview bool) (*httptest.ResponseRecorder, map[string]interface{}) {
		rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodPost, "/api/v1/wms/bin-generator", token,
			map[string]interface{}{"location": location, "aisles": aisle, "bins": "1", "format": "{aisle}-{bin}", "preview": preview}, ip)
		out := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}

	// Another store: refused 403 GLOBAL-0011, nothing written.
	rec, out := gen(manager, other, "G34X"+suffix, false)
	if rec.Code != http.StatusForbidden || out["code"] != "GLOBAL-0011" || !strings.Contains(rec.Body.String(), other) {
		t.Fatalf("Store Manager generating at %s: %d %s, want 403 GLOBAL-0011", other, rec.Code, rec.Body.String())
	}
	if n := count("G34X" + suffix + "%"); n != 0 {
		t.Fatalf("%d bins written for another store", n)
	}
	if rec, _ := gen(manager, other, "G34X"+suffix, true); rec.Code != http.StatusForbidden {
		t.Fatalf("preview for another store: %d, want 403", rec.Code)
	}

	// Own store: made, and it reads back like a hand-made one.
	aisle := "G34A" + suffix
	if rec, _ = gen(manager, "HO", aisle, false); rec.Code != http.StatusOK {
		t.Fatalf("Store Manager generating at HO: %d %s", rec.Code, rec.Body.String())
	}
	hand := "G34H-1-" + suffix
	body := map[string]interface{}{"bin_code": hand, "location": "HO", "status": "Active", "aisle": aisle}
	if r := doRequestFromIP(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/Bin", manager, body, ip); r.Code < 200 || r.Code >= 300 {
		t.Fatalf("hand-made bin: %d %s", r.Code, r.Body.String())
	}
	read := func(code string) (map[string]interface{}, string) {
		var s, status string
		if err := db.DB.QueryRow(`SELECT data::text, status FROM tenant_default.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1 AND deleted_at IS NULL`, code).Scan(&s, &status); err != nil {
			t.Fatalf("read %s: %v", code, err)
		}
		m := map[string]interface{}{}
		_ = json.Unmarshal([]byte(s), &m)
		return m, status
	}
	g, gs := read(aisle + "-1")
	h, hs := read(hand)
	for _, m := range []map[string]interface{}{g, h} {
		if m["id"] == nil || m["code"] == nil {
			t.Fatalf("a bin is missing its id or code: %v", m)
		}
		delete(m, "id")
		delete(m, "code")
		delete(m, "bin_code")
		delete(m, "aisle")
		delete(m, "position")
	}
	if gs != hs || !reflect.DeepEqual(g, h) {
		t.Fatalf("generated bin differs from a hand-made one: %v (%s) vs %v (%s)", g, gs, h, hs)
	}

	// Full access: an Admin generates for another store.
	if rec, _ = gen(admin, other, "G34Z"+suffix, false); rec.Code != http.StatusOK {
		t.Fatalf("Admin generating at %s: %d %s", other, rec.Code, rec.Body.String())
	}
}
