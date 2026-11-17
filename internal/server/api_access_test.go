package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 205.3-205.9: a key made on the API access screen reaches the records API as
// its owner, and every ceiling answers with its own code.

type apiAccessFixture struct {
	mux        http.Handler
	adminToken string
	module     string
	other      string // a document type in a module the key does not hold
	seq        int
}

func newAPIAccessFixture(t *testing.T) *apiAccessFixture {
	t.Helper()
	db.InitDB(testConnStr())
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })
	id, cleanup := seedStage47User(t, engines.RoleSuperAdmin, "HO")
	t.Cleanup(cleanup)
	f := &apiAccessFixture{mux: mux, adminToken: stage47Token(id, engines.RoleSuperAdmin, "HO")}
	mod, err := engines.ModuleForDoctype("default", "PurchaseOrder")
	if err != nil || mod == "" {
		t.Skip("no module for PurchaseOrder")
	}
	f.module = mod
	f.other = "Customer"
	if m, _ := engines.ModuleForDoctype("default", f.other); m == "" || m == mod {
		t.Skip("no second module to test against")
	}
	return f
}

func (f *apiAccessFixture) do(method, path, bearer string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	f.seq++
	var rdr *strings.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = strings.NewReader(string(raw))
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.RemoteAddr = fmt.Sprintf("198.19.%d.%d:12345", f.seq/250, f.seq%250+1)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func apiAccessErrCode(rec *httptest.ResponseRecorder) string {
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Code
}

func TestAPIAccessScreenAndRecordsAPI(t *testing.T) {
	f := newAPIAccessFixture(t)

	// The module list has the contract's fields.
	rec := f.do(http.MethodGet, "/api/v1/api-access/modules", f.adminToken, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("modules: %d %s", rec.Code, rec.Body.String())
	}
	var modules []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &modules); err != nil || len(modules) == 0 {
		t.Fatalf("modules body: %v %s", err, rec.Body.String())
	}
	for _, field := range []string{"key", "name", "summary", "in_plan", "can_view", "can_change", "reason", "doctypes"} {
		if _, ok := modules[0][field]; !ok {
			t.Errorf("modules row lacks %q", field)
		}
	}

	// A role without api.share cannot reach the screen routes.
	cid, cleanup := seedStage47User(t, "Cashier", "HO")
	t.Cleanup(cleanup)
	if rec := f.do(http.MethodGet, "/api/v1/api-access/keys", stage47Token(cid, "Cashier", "HO"), nil, nil); rec.Code != http.StatusForbidden {
		t.Errorf("cashier on the screen route: %d %s", rec.Code, rec.Body.String())
	}

	// Make a key: View and Change on the PO module only.
	rec = f.do(http.MethodPost, "/api/v1/api-access/keys", f.adminToken, map[string]interface{}{
		"name": "t205 key", "modules": map[string]string{f.module: "write"}, "expires": "30d", "deliver": "show",
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
		APIKey     string `json:"api_key"`
		TenantCode string `json:"tenant_code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.APIKey == "" || created.TenantCode != "default" || created.Credential.ID == "" {
		t.Fatalf("create answer = %s", rec.Body.String())
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, created.Credential.ID) })
	key := created.APIKey

	t.Run("the key needs no tenant header and names itself", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/public/v1/me", key, nil, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"t205 key"`) {
			t.Fatalf("/me: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong tenant is the generic refusal", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/public/v1/me", key, nil, map[string]string{"X-Tenant-ID": "someone-else"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong tenant: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a module the key does not hold", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/public/v1/records/"+f.other, key, nil, nil)
		if rec.Code != http.StatusForbidden || apiAccessErrCode(rec) != engines.APIKeyModuleNotOnKey {
			t.Fatalf("other module: %d %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"fix"`) || !strings.Contains(body, `"docs_url"`) {
			t.Errorf("refusal lacks fix or docs_url: %s", body)
		}
	})

	t.Run("approval stays with people unless the key may", func(t *testing.T) {
		rec := f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder/PO-NONE/approve", key, map[string]string{}, map[string]string{"Idempotency-Key": "t205-approve-1"})
		if rec.Code != http.StatusForbidden || apiAccessErrCode(rec) != engines.APIKeyNoApproval {
			t.Fatalf("approve without can_approve: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("schema lists fields", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/public/v1/records/PurchaseOrder/schema", key, nil, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"fields"`) || !strings.Contains(rec.Body.String(), `"class":"rw"`) {
			t.Fatalf("schema: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a create replayed with the same Idempotency-Key makes one record", func(t *testing.T) {
		const vendor = "T205-VENDOR"
		storeHasState(t, "HO")
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, vendor)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
			($1, 'Vendor', '{"code":"T205-VENDOR","name":"T205 Vendor","state":"Maharashtra"}', 'Active', 'admin')`, vendor); err != nil {
			t.Fatal(err)
		}
		count := func() int {
			var n int
			db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype='PurchaseOrder' AND data->>'vendor_id'=$1`, vendor).Scan(&n)
			return n
		}
		t.Cleanup(func() {
			db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1 OR (doctype='PurchaseOrder' AND data->>'vendor_id'=$1)`, vendor)
		})
		po := map[string]interface{}{"vendor": vendor, "vendor_id": vendor, "target_warehouse": "HO", "location": "HO",
			"items": "[]", "gst_mode": engines.GSTModeExclusive, "interstate": false, "total_amount": 0, "status": "Draft"}
		hdr := map[string]string{"Idempotency-Key": "t205-po-create-1"}
		first := f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder", key, po, hdr)
		if first.Code >= 300 {
			t.Fatalf("first create: %d %s", first.Code, first.Body.String())
		}
		second := f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder", key, po, hdr)
		if second.Code != first.Code || second.Body.String() != first.Body.String() {
			t.Errorf("replay differs: %d %s vs %d %s", second.Code, second.Body.String(), first.Code, first.Body.String())
		}
		if n := count(); n != 1 {
			t.Fatalf("%d purchase orders after a replayed create, want 1", n)
		}
		// A mutating call without the header is refused.
		if rec := f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder", key, po, nil); rec.Code < 400 {
			t.Errorf("create without Idempotency-Key: %d", rec.Code)
		}
	})

	t.Run("changed_since lists what changed, oldest first, with a cursor", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/public/v1/records/PurchaseOrder?changed_since=2000-01-01T00:00:00Z&limit=1", key, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("changed_since: %d %s", rec.Code, rec.Body.String())
		}
		var rows []map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &rows)
		if len(rows) == 1 {
			if rows[0]["updated_at"] == nil {
				t.Errorf("a changed_since row lacks updated_at")
			}
			if rec.Header().Get("X-Next-Cursor") == "" {
				t.Errorf("a full page lacks X-Next-Cursor")
			}
		}
		if bad := f.do(http.MethodGet, "/api/public/v1/records/PurchaseOrder?changed_since=yesterday", key, nil, nil); bad.Code != http.StatusUnprocessableEntity {
			t.Errorf("bad changed_since: %d", bad.Code)
		}
	})

	t.Run("activity shows the refusals with their fix", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/v1/api-access/keys/"+created.Credential.ID+"/activity?errors_only=1", f.adminToken, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("activity: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), engines.APIKeyModuleNotOnKey) {
			t.Errorf("activity lacks the refusal: %s", rec.Body.String())
		}
	})

	t.Run("owner who is no longer active stops the key", func(t *testing.T) {
		var owner string
		db.DB.QueryRow(`SELECT owner_user_id FROM tenant_default.api_credentials WHERE id=$1`, created.Credential.ID).Scan(&owner)
		db.DB.Exec(`UPDATE tenant_default.users SET status='Inactive' WHERE id=$1`, owner)
		t.Cleanup(func() {
			db.DB.Exec(`UPDATE tenant_default.users SET status='Active' WHERE id=$1`, owner)
			engines.InvalidateLiveUserState("default", owner)
		})
		engines.InvalidateLiveUserState("default", owner)
		rec := f.do(http.MethodGet, "/api/public/v1/records/PurchaseOrder", key, nil, nil)
		if rec.Code != http.StatusForbidden || apiAccessErrCode(rec) != engines.APIKeyOwnerInactive {
			t.Fatalf("inactive owner: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("revoke ends the key", func(t *testing.T) {
		if rec := f.do(http.MethodDelete, "/api/v1/api-access/keys/"+created.Credential.ID, f.adminToken, nil, nil); rec.Code != http.StatusOK {
			t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
		}
		if rec := f.do(http.MethodGet, "/api/public/v1/me", key, nil, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("revoked key: %d %s", rec.Code, rec.Body.String())
		}
	})
}
