package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 49.3.4 - object / field / action negative matrix, two clients. Client
// B's administrator and cashier ask the generic record endpoints for client
// A's record by id, by search, by count, by number lookup and through its
// attachments, and try to change and delete it. None of it may show or touch
// A's record, and every answer must be exactly the answer for an id that does
// not exist anywhere - so no response confirms that A's record is there. The
// same sequential id in both clients reads each client's own record.
//
// The action half: a CSV import adds and changes records, so a role with
// neither create nor update on the record type is refused (403 GLOBAL-0011)
// exactly as its own record save is - before 49.3.4 any signed-in role could
// import any record type.
func TestStage4934TwoClientNegativeMatrix(t *testing.T) {
	db.InitDB(testConnStr())
	engines.SetMediaStoreDirForTest(t.TempDir())
	t.Cleanup(func() { engines.SetMediaStoreDirForTest("media_store") })
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	type client struct{ id, schema string }
	a, b := client{"mxa" + suffix, "tenant_mxa" + suffix}, client{"mxb" + suffix, "tenant_mxb" + suffix}
	for _, c := range []client{a, b} {
		c := c
		t.Cleanup(func() {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + c.schema + " CASCADE")
			db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, c.id)
			db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, c.id)
			engines.InvalidateTenantLifecycleCache(c.id)
		})
		if _, err := engines.ProvisionTenantSchema(c.id, c.schema, "0.1.0-test"); err != nil {
			t.Fatalf("ProvisionTenantSchema %s: %v", c.id, err)
		}
	}
	seedUser := func(c client, id, role string) string {
		t.Helper()
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, role, status, location_code)
			VALUES ($1, $1, 'x', $2, 'Active', 'HO')`, c.schema), id, role); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
		return engines.SignToken(id, id, role, c.id, "HO", 1)
	}
	adminA := seedUser(a, "mx_admin_a", engines.RoleAdmin)
	adminB := seedUser(b, "mx_admin_b", engines.RoleAdmin)
	cashierB := seedUser(b, "mx_cashier_b", engines.RoleCashier)
	engines.ResetLiveUserStateCache()

	const doctype = "PurchaseReturn"
	onlyA, sameID, missing := "PR-MX-A-"+suffix, "PR-MX-SAME-"+suffix, "PR-MX-NONE-"+suffix
	seedDoc := func(c client, id, secret string) {
		t.Helper()
		data, _ := json.Marshal(map[string]interface{}{"code": id, "location": "HO", "remarks": secret})
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'admin')`, c.schema), id, doctype, data); err != nil {
			t.Fatalf("seed %s in %s: %v", id, c.id, err)
		}
	}
	const secretA, secretB = "MXSECRET-A", "MXSECRET-B"
	seedDoc(a, onlyA, secretA)
	seedDoc(a, sameID, secretA)
	seedDoc(b, sameID, secretB)
	att, err := engines.AddAttachment(a.id, doctype, onlyA, "proof.pdf", []byte("%PDF-1.4\n% "+secretA+"\n"), "mx_admin_a")
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	ip := 0
	call := func(method, path, token string, body []byte, contentType string) *httptest.ResponseRecorder {
		ip++
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.RemoteAddr = fmt.Sprintf("198.51.100.%d:4934", ip%250+1)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	answer := func(rec *httptest.ResponseRecorder) string {
		var env map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		code, _ := env["code"].(string)
		return fmt.Sprintf("%d %s", rec.Code, code)
	}

	// Positive control: A's own administrator sees A's record and its file, so
	// every refusal below is the client boundary, not a broken route.
	if rec := call(http.MethodGet, "/api/v1/doc/"+doctype+"/"+onlyA, adminA, nil, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), secretA) {
		t.Fatalf("client A reading its own record: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodGet, "/api/v1/attachments/file/"+att.ID, adminA, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("client A reading its own file: %d %s", rec.Code, rec.Body.String())
	}

	probes := []struct{ name, other, absent string }{
		{"read by id", "/api/v1/doc/" + doctype + "/" + onlyA, "/api/v1/doc/" + doctype + "/" + missing},
		{"number lookup", "/api/v1/doc-find?id=" + onlyA, "/api/v1/doc-find?id=" + missing},
		{"attachment list", "/api/v1/attachments/" + doctype + "/" + onlyA, "/api/v1/attachments/" + doctype + "/" + missing},
		{"attachment counts", "/api/v1/attachments/" + doctype + "?ids=" + onlyA, "/api/v1/attachments/" + doctype + "?ids=" + missing},
		{"file download", "/api/v1/attachments/file/" + att.ID, "/api/v1/attachments/file/00000000-0000-4000-8000-000000000000"},
		{"search and count", "/api/v1/doc/" + doctype + "?count=true&q=" + secretA, "/api/v1/doc/" + doctype + "?count=true&q=MXNOTHING"},
	}
	clientB := []struct{ name, token string }{{"admin", adminB}, {"cashier", cashierB}}
	for _, who := range clientB {
		for _, p := range probes {
			other := call(http.MethodGet, p.other, who.token, nil, "")
			absent := call(http.MethodGet, p.absent, who.token, nil, "")
			t.Logf("client B %s, %s: %s", who.name, p.name, answer(other))
			if strings.Contains(other.Body.String(), secretA) || strings.Contains(other.Body.String(), onlyA) {
				t.Errorf("client B %s, %s: the answer carries client A's record: %s", who.name, p.name, other.Body.String())
			}
			if answer(other) != answer(absent) {
				t.Errorf("client B %s, %s: A's record answers %q, a missing one %q - the difference confirms A's record exists", who.name, p.name, answer(other), answer(absent))
			}
			if other.Header().Get("X-Total-Count") != absent.Header().Get("X-Total-Count") {
				t.Errorf("client B %s, %s: total %q against %q", who.name, p.name, other.Header().Get("X-Total-Count"), absent.Header().Get("X-Total-Count"))
			}
		}
		// The same sequential id reads B's own record, never A's.
		if rec := call(http.MethodGet, "/api/v1/doc/"+doctype+"/"+sameID, who.token, nil, ""); strings.Contains(rec.Body.String(), secretA) {
			t.Errorf("client B %s reading the id both clients use got client A's record: %s", who.name, rec.Body.String())
		}
	}
	// Changing and deleting by A's id never reaches A's record (after every
	// read above, since a save by B may create B's own record under that id).
	for _, who := range clientB {
		edit, _ := json.Marshal(map[string]interface{}{"code": onlyA, "location": "HO", "remarks": "overwritten by B"})
		call(http.MethodPost, "/api/v1/doc/"+doctype+"/"+onlyA, who.token, edit, "application/json")
		call(http.MethodDelete, "/api/v1/doc/"+doctype+"/"+onlyA, who.token, nil, "")
		var remarks string
		var deleted bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'remarks', deleted_at IS NOT NULL FROM %s.documents WHERE id = $1`, a.schema), onlyA).Scan(&remarks, &deleted); err != nil || remarks != secretA || deleted {
			t.Fatalf("client B %s changed or deleted client A's record: remarks %q deleted %v (%v)", who.name, remarks, deleted, err)
		}
	}

	// The action half: a cashier holds read only on Brand, so its record save
	// is refused - and now so is its import of Brand records.
	if rec := call(http.MethodPost, "/api/v1/doc/Brand", cashierB, []byte(`{"code":"MXB-`+suffix+`","name":"x"}`), "application/json"); rec.Code != http.StatusForbidden {
		t.Fatalf("cashier saving a Brand: %d %s, want 403", rec.Code, rec.Body.String())
	}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, _ := mw.CreateFormFile("file", "brands.csv")
	part.Write([]byte("code,name\nMXB-" + suffix + ",Imported by a cashier\n"))
	mw.Close()
	if rec := call(http.MethodPost, "/api/v1/import/Brand", cashierB, form.Bytes(), mw.FormDataContentType()); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") {
		t.Fatalf("cashier importing Brand records: %d %s, want 403 GLOBAL-0011", rec.Code, rec.Body.String())
	}
	var brands int
	db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Brand' AND data->>'code' = $1`, b.schema), "MXB-"+suffix).Scan(&brands)
	if brands != 0 {
		t.Fatal("the cashier's import created a Brand record")
	}
}
