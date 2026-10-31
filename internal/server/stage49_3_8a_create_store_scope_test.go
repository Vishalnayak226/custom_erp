package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// TestStage4938aCreateNamingAnotherStoreIsRefused (found by 49.3.8): the
// generic save checked location scope on an update and a delete, not on a
// create, so a store-limited user could file a new record under another
// store - one they could never see again. Now a save and a CSV import are
// both held to the session's own store, per doctype from doctypeScopes: a
// Store Manager at HO adds an HO bin; a bin for another store is refused on
// screen and in a file (and an HO bin cannot be moved there); an Admin, who
// holds the global location scope, adds one anywhere.
func TestStage4938aCreateNamingAnotherStoreIsRefused(t *testing.T) {
	db.InitDB(testConnStr())
	if scope, ok := engines.ScopeForDoctype("Bin"); !ok || !scope.LocationMandatory {
		t.Fatalf("the test's doctype, Bin, is no longer location-scoped: %+v", scope)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("POST /api/v1/import/{doctype}", apiMiddleware(handleBulkImport))
	users := seedGeneratedCaseUsers(t)
	manager, admin := users[engines.RoleStoreManager], users[engines.RoleAdmin]
	suffix := contractUniqueID()
	ip := mfaTestIP()

	other := "A38A-" + suffix
	raw, _ := json.Marshal(map[string]interface{}{"code": other, "name": "Another store", "location_type": "Store"})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')`, other, string(raw)); err != nil {
		t.Fatalf("seed the other store: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Bin' AND data->>'bin_code' LIKE $1`, "B38A-%-"+suffix)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, other)
	})
	bins := func(code string) (n int, location string) {
		db.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(data->>'location'), '') FROM tenant_default.documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1 AND deleted_at IS NULL`, code).Scan(&n, &location)
		return
	}
	save := func(token, path, code, location string) (*httptest.ResponseRecorder, map[string]interface{}) {
		body := map[string]interface{}{"bin_code": code, "location": location, "status": "Active"}
		rec := doRequestFromIP(t, mux.ServeHTTP, http.MethodPost, path, token, body, ip)
		out := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}

	// Own store: saved.
	own := "B38A-HO-" + suffix
	rec, out := save(manager, "/api/v1/doc/Bin", own, "HO")
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("Store Manager at HO adding an HO bin: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := bins(own); n != 1 {
		t.Fatalf("the HO bin was not stored (%d rows)", n)
	}
	ownID, _ := out["id"].(string)

	// Another store: refused, nothing stored.
	elsewhere := "B38A-X-" + suffix
	rec, out = save(manager, "/api/v1/doc/Bin", elsewhere, other)
	if rec.Code != http.StatusForbidden || out["code"] != "GLOBAL-0011" || !strings.Contains(rec.Body.String(), other) {
		t.Fatalf("Store Manager at HO adding a bin for %s: %d %s, want 403 GLOBAL-0011 naming the store", other, rec.Code, rec.Body.String())
	}
	if n, _ := bins(elsewhere); n != 0 {
		t.Fatal("the bin for another store was stored")
	}

	// An HO bin cannot be moved to another store either.
	if ownID != "" {
		rec, _ = save(manager, "/api/v1/doc/Bin/"+ownID, own, other)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("Store Manager moving an HO bin to %s: %d %s, want 403", other, rec.Code, rec.Body.String())
		}
		if _, loc := bins(own); loc != "HO" {
			t.Fatalf("the HO bin moved to %q", loc)
		}
	}

	// Full access: an Admin at HO adds a bin for another store.
	adminBin := "B38A-AD-" + suffix
	if rec, _ = save(admin, "/api/v1/doc/Bin", adminBin, other); rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("Admin adding a bin for %s: %d %s", other, rec.Code, rec.Body.String())
	}
	if n, _ := bins(adminBin); n != 1 {
		t.Fatal("the Admin's bin for another store was not stored")
	}

	// The CSV import, through the same middleware: the own-store row is
	// imported, the other store's row refused with the reason; an Admin's
	// file for another store goes in.
	importCSV := func(token, body string) map[string]interface{} {
		t.Helper()
		var form bytes.Buffer
		mw := multipart.NewWriter(&form)
		part, _ := mw.CreateFormFile("file", "bins.csv")
		part.Write([]byte(body))
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/import/Bin", bytes.NewReader(form.Bytes()))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.RemoteAddr = ip + ":54321"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
		}
		res := map[string]interface{}{}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		return res
	}
	importedOwn, importedElsewhere := "B38A-IH-"+suffix, "B38A-IX-"+suffix
	res := importCSV(manager, fmt.Sprintf("bin_code,location,status\n%s,HO,Active\n%s,%s,Active\n", importedOwn, importedElsewhere, other))
	if res["success_rows"] != float64(1) || res["failed_rows"] != float64(1) || !strings.Contains(fmt.Sprint(res["errors"]), "for store "+other) {
		t.Fatalf("Store Manager importing an HO bin and one for %s: want 1 imported and 1 refused naming the store, got %v", other, res)
	}
	if n, _ := bins(importedOwn); n != 1 {
		t.Fatal("the imported HO bin was not stored")
	}
	if n, _ := bins(importedElsewhere); n != 0 {
		t.Fatal("the imported bin for another store was stored")
	}
	adminImported := "B38A-IA-" + suffix
	if res = importCSV(admin, fmt.Sprintf("bin_code,location,status\n%s,%s,Active\n", adminImported, other)); res["success_rows"] != float64(1) {
		t.Fatalf("Admin importing a bin for %s: %v", other, res)
	}
	if n, _ := bins(adminImported); n != 1 {
		t.Fatal("the Admin's imported bin for another store was not stored")
	}
}

// TestStage4938aLocationOutOfScopeRule: the rule itself - a doctype outside
// the registry, or without a location dimension, is never refused; a record
// naming no store is left to validation; a role with the global location
// scope may name any store; a session with no store may name none.
func TestStage4938aLocationOutOfScopeRule(t *testing.T) {
	limited := engines.SessionScope{Role: engines.RoleStoreManager, LocationCode: "HO"}
	cases := []struct {
		name    string
		doctype string
		session engines.SessionScope
		data    map[string]interface{}
		want    string
	}{
		{"own store", "Bin", limited, map[string]interface{}{"location": "HO"}, ""},
		{"another store", "Bin", limited, map[string]interface{}{"location": "ST-B"}, "ST-B"},
		{"location_code field", "Wave", limited, map[string]interface{}{"location_code": "ST-B"}, "ST-B"},
		{"no store named", "Bin", limited, map[string]interface{}{"location": "  "}, ""},
		{"optional store, another", "Zone", limited, map[string]interface{}{"location": "ST-B"}, "ST-B"},
		{"full access", "Bin", engines.SessionScope{Role: engines.RoleAdmin, LocationCode: "HO"}, map[string]interface{}{"location": "ST-B"}, ""},
		{"template global location", "PurchaseOrder", engines.SessionScope{Role: "Merchandiser", LocationCode: "HO"}, map[string]interface{}{"location": "ST-B"}, ""},
		{"session without a store", "Bin", engines.SessionScope{Role: engines.RoleStoreManager}, map[string]interface{}{"location": "HO"}, "HO"},
		{"not location-scoped", "Leave", limited, map[string]interface{}{"location": "ST-B"}, ""},
		{"not in the registry", "Brand", limited, map[string]interface{}{"location": "ST-B"}, ""},
	}
	for _, c := range cases {
		if got := engines.LocationOutOfScope(c.doctype, c.session, c.data); got != c.want {
			t.Errorf("%s: LocationOutOfScope = %q, want %q", c.name, got, c.want)
		}
	}
}
