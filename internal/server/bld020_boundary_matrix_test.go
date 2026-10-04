package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// BLD-020 (pairwise compatibility and boundary matrix). Reuses
// moduleHTTPFixture/seedStage47User/stage47Token (module_manifest_test.go,
// stage47_redteam_helpers_shared_test.go) rather than building new fixture
// machinery. The role/tenant-size/module scope this matrix combines is the
// one the product/QA owner decided in bld-002-test-configuration-inventory.md
// §9: REF-RETAIL-IN (Cashier as its least-privileged role) and
// REF-WAREHOUSE-IN (Picker), against Small/Mid/Large tenant-size tiers
// defined there via the existing tenant_limits/max_users mechanism. Browser/
// device/locale is fixed (Chromium/3-viewport/en-IN) by that same decision,
// so it is not a combinatorial axis here.

func bld020Request(t *testing.T, handler http.Handler, token, method, path string, body []byte, seq int) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.RemoteAddr = fmt.Sprintf("198.19.%d.%d:12345", seq/250, seq%250+1)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

// TestBLD020UnicodeRoundTrip closes the Unicode clause of MC-100/BLD-020: an
// automated create->read proof at the HTTP/validation layer, not just the
// manual browser-audit storage check MC-099 already covers (PASS, but not
// executable). Real Indian-regional-script text plus an emoji, through the
// same generic doc endpoint every screen uses.
func TestBLD020UnicodeRoundTrip(t *testing.T) {
	handler, token := moduleHTTPFixture(t)
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	code := "TEST-BLD020-UNI-" + stage47UniqueID()
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ReasonCode' AND data->>'code' = $1", code)
	})

	unicodeDescription := "मूल्य समायोजन / விலை சரிசெய்தல் / মূল্য সমন্বয় 🎉 café naïve"
	payload, _ := json.Marshal(map[string]interface{}{
		"code": code, "description": unicodeDescription, "category": "Other", "status": "Active",
	})
	r := bld020Request(t, handler, token, "POST", "/api/v1/doc/ReasonCode", payload, 1)
	if r.Code != http.StatusOK && r.Code != http.StatusCreated {
		t.Fatalf("create with Unicode fields: %d %s", r.Code, r.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("could not read created id from response %s: %v", r.Body.String(), err)
	}

	var stored string
	if err := db.DB.QueryRow("SELECT data->>'description' FROM "+schema+".documents WHERE doctype = 'ReasonCode' AND id = $1", created.ID).Scan(&stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != unicodeDescription {
		t.Fatalf("counterexample: Unicode description did not round-trip byte-exact.\nwant: %q\ngot:  %q", unicodeDescription, stored)
	}

	r2 := bld020Request(t, handler, token, "GET", "/api/v1/doc/ReasonCode/"+created.ID, nil, 2)
	if r2.Code != http.StatusOK {
		t.Fatalf("read via HTTP: %d %s", r2.Code, r2.Body.String())
	}
	if !strings.Contains(r2.Body.String(), "café naïve") || !strings.Contains(r2.Body.String(), "🎉") {
		t.Fatalf("counterexample: HTTP read response does not contain the Unicode value intact: %s", r2.Body.String())
	}
}

// TestBLD020MalformedAndOversizedRequests: a malformed JSON body must be
// refused cleanly (4xx, an error envelope), never a 500 or a hang; a body
// over the global 2MB cap (middleware.go's http.MaxBytesReader) must be
// refused the same way. Neither had a dedicated HTTP-layer test before this
// (confirmed by research before writing it) - only a field-level JSONTable
// validator test and unrelated business-domain "oversized" checks existed.
func TestBLD020MalformedAndOversizedRequests(t *testing.T) {
	handler, token := moduleHTTPFixture(t)

	t.Run("malformed JSON body", func(t *testing.T) {
		malformed := []byte(`{"code": "TEST-BLD020-MALFORMED", "description": `)
		r := bld020Request(t, handler, token, "POST", "/api/v1/doc/ReasonCode", malformed, 10)
		if r.Code < 400 || r.Code >= 500 {
			t.Fatalf("counterexample: malformed JSON body got %d (want a clean 4xx), body=%s", r.Code, r.Body.String())
		}
	})

	t.Run("oversized body over the 2MB cap", func(t *testing.T) {
		huge := make([]byte, 3<<20) // 3 MiB, over middleware.go's 2 MiB MaxBytesReader cap
		for i := range huge {
			huge[i] = 'a'
		}
		body, _ := json.Marshal(map[string]interface{}{
			"code": "TEST-BLD020-OVERSIZED", "description": string(huge), "category": "Other", "status": "Active",
		})
		r := bld020Request(t, handler, token, "POST", "/api/v1/doc/ReasonCode", body, 11)
		schema, err := db.GetTenantSchema("default")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND data->>'code' = 'TEST-BLD020-OVERSIZED'")
		})
		if r.Code < 400 {
			t.Fatalf("counterexample: a >2MB request body was accepted (%d) instead of refused", r.Code)
		}
		var stray int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND data->>'code' = 'TEST-BLD020-OVERSIZED'").Scan(&stray); err != nil {
			t.Fatal(err)
		}
		if stray != 0 {
			t.Fatalf("counterexample: a refused oversized request still wrote %d row(s)", stray)
		}
	})
}

// TestBLD020UnsupportedAPIVersionIsHandledCleanly closes the "API client
// version" clause honestly: this codebase has never shipped a second public
// API version (confirmed by research before writing this - no negotiation
// header, no v0/v2 route, docs/api/compatibility-policy.md is an unimplemented
// draft). So the real, buildable case is that a request naming an unsupported
// version resolves predictably - a clean 404, not an ambiguous fallback to
// v1 behavior or a crash - rather than fabricating a compatibility scenario
// against a version that has never existed.
func TestBLD020UnsupportedAPIVersionIsHandledCleanly(t *testing.T) {
	handler, token := moduleHTTPFixture(t)
	for i, path := range []string{"/api/public/v2/items", "/api/public/v0/items", "/api/public/v1/__unknown_endpoint__"} {
		r := bld020Request(t, handler, token, "GET", path, nil, 20+i)
		if r.Code != http.StatusNotFound {
			t.Fatalf("counterexample: unsupported/unknown API path %s = %d (want 404), body=%s", path, r.Code, r.Body.String())
		}
	}
}

// TestBLD020RoleTenantSizeEntitlementMatrix combines role and tenant-size in
// one high-risk operation (POST /api/v1/admin/users), beyond either
// dimension alone: the Super Admin role can create users but is still bound
// by the tenant's max_users size limit at its exact boundary; a non-admin
// operational role from each BLD-002-decided reference candidate cannot
// reach the operation at all, regardless of any size limit.
func TestBLD020RoleTenantSizeEntitlementMatrix(t *testing.T) {
	handler, adminToken := moduleHTTPFixture(t)
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}

	for _, role := range []string{"Cashier", "Picker"} { // REF-RETAIL-IN's and REF-WAREHOUSE-IN's least-privileged role
		t.Run(role+" cannot create users regardless of tenant size", func(t *testing.T) {
			userID, cleanup := seedStage47User(t, role, "")
			t.Cleanup(cleanup)
			token := stage47Token(userID, role, "")
			body, _ := json.Marshal(map[string]string{"username": "TEST-BLD020-" + role, "password": "irrelevant-Pw1!", "role": engines.RoleCashier})
			r := bld020Request(t, handler, token, "POST", "/api/v1/admin/users", body, 30)
			if r.Code != http.StatusForbidden {
				t.Fatalf("counterexample: role %s reached user-creation with %d (want 403): %s", role, r.Code, r.Body.String())
			}
		})
	}

	t.Run("Super Admin is still bound by the tenant's max_users size limit at its exact boundary", func(t *testing.T) {
		var activeUserCount int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".users WHERE status = 'Active'").Scan(&activeUserCount); err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".tenant_limits (tenant_id, limit_key, limit_value) VALUES ($1, 'max_users', $2) "+
				"ON CONFLICT (tenant_id, limit_key) DO UPDATE SET limit_value = EXCLUDED.limit_value",
			"default", activeUserCount); err != nil {
			t.Fatalf("seed max_users=%d limit: %v", activeUserCount, err)
		}
		t.Cleanup(func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".tenant_limits WHERE tenant_id = 'default' AND limit_key = 'max_users'")
		})

		bodyAt := func(username string) []byte {
			b, _ := json.Marshal(map[string]string{"username": username, "password": "Str0ng!Pw12345", "role": engines.RoleCashier, "email": username + "@test.invalid"})
			return b
		}

		// At the limit (active count == limit), one more user is refused.
		blocked := "TEST-BLD020-ATLIMIT-" + stage47UniqueID()
		r := bld020Request(t, handler, adminToken, "POST", "/api/v1/admin/users", bodyAt(blocked), 31)
		if r.Code != http.StatusUnprocessableEntity {
			t.Fatalf("counterexample: user creation at the exact max_users limit (%d active, limit %d) got %d (want 422), body=%s",
				activeUserCount, activeUserCount, r.Code, r.Body.String())
		}
		defer db.DB.Exec("DELETE FROM "+schema+".users WHERE username = $1", blocked)

		// Raise the limit by exactly one; the identical request now succeeds.
		if _, err := db.DB.Exec("UPDATE "+schema+".tenant_limits SET limit_value = $1 WHERE tenant_id = 'default' AND limit_key = 'max_users'", activeUserCount+1); err != nil {
			t.Fatalf("raise limit: %v", err)
		}
		allowed := "TEST-BLD020-OVERLIMIT-" + stage47UniqueID()
		r2 := bld020Request(t, handler, adminToken, "POST", "/api/v1/admin/users", bodyAt(allowed), 32)
		if r2.Code != http.StatusOK && r2.Code != http.StatusCreated {
			t.Fatalf("counterexample: user creation one over the previous limit (now %d active, limit %d) was refused: %d %s",
				activeUserCount, activeUserCount+1, r2.Code, r2.Body.String())
		}
		defer db.DB.Exec("DELETE FROM "+schema+".users WHERE username = $1", allowed)
	})
}
