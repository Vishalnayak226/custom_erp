package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// seedTenantHostCache installs a fixed slug->tenant map and marks it fresh, so
// the code under test never reaches for a database. The previous contents are
// restored afterwards because the cache is package state shared by every test
// in this package.
func seedTenantHostCache(t *testing.T, slugs map[string]string) {
	t.Helper()
	tenantHostCache.Lock()
	prevSlugs, prevLoaded := tenantHostCache.slugs, tenantHostCache.loaded
	tenantHostCache.slugs = slugs
	tenantHostCache.loaded = time.Now()
	tenantHostCache.Unlock()

	t.Cleanup(func() {
		tenantHostCache.Lock()
		tenantHostCache.slugs, tenantHostCache.loaded = prevSlugs, prevLoaded
		tenantHostCache.Unlock()
	})
}

func TestHostLabel(t *testing.T) {
	const base = "wholeops.in"

	cases := []struct {
		name string
		host string
		base string
		want string
	}{
		{"plain subdomain", "acme.wholeops.in", base, "acme"},
		{"uppercase is normalised", "ACME.WholeOps.IN", base, "acme"},
		{"port is stripped", "acme.wholeops.in:8080", base, "acme"},
		{"trailing dot is stripped", "acme.wholeops.in.", base, "acme"},
		{"digits and hyphens are legal", "acme-corp2.wholeops.in", base, "acme-corp2"},

		{"apex is not a tenant", "wholeops.in", base, ""},
		{"reserved label app", "app.wholeops.in", base, ""},
		{"reserved label www", "www.wholeops.in", base, ""},
		{"reserved label sandbox", "sandbox.wholeops.in", base, ""},
		{"two levels deep", "a.b.wholeops.in", base, ""},
		{"unrelated domain", "acme.example.com", base, ""},
		{"leading hyphen", "-acme.wholeops.in", base, ""},
		{"trailing hyphen", "acme-.wholeops.in", base, ""},
		{"underscore is not a legal label", "acme_corp.wholeops.in", base, ""},
		{"empty host", "", base, ""},

		// The suffix test is anchored on ".base", not "base", so a domain
		// someone else registered that merely ends in the same letters is not
		// mistaken for a subdomain of ours.
		{"suffix confusion", "acme.evilwholeops.in", base, ""},

		// Feature off: no base domain configured means nothing is a tenant
		// host, which is what keeps localhost and the SSH tunnel unchanged.
		{"no base domain configured", "acme.wholeops.in", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostLabel(tc.host, tc.base); got != tc.want {
				t.Errorf("hostLabel(%q, %q) = %q, want %q", tc.host, tc.base, got, tc.want)
			}
		})
	}
}

func TestValidHostLabel(t *testing.T) {
	valid := []string{"a", "acme", "acme-corp", "a1", "0acme", "a-b-c"}
	for _, s := range valid {
		if !validHostLabel(s) {
			t.Errorf("validHostLabel(%q) = false, want true", s)
		}
	}

	invalid := []string{"", "-a", "a-", "a_b", "a.b", "A", "acme corp", "üñî"}
	for _, s := range invalid {
		if validHostLabel(s) {
			t.Errorf("validHostLabel(%q) = true, want false", s)
		}
	}

	// 63 is the DNS label ceiling: 63 passes, 64 does not.
	sixtyThree := ""
	for i := 0; i < 63; i++ {
		sixtyThree += "a"
	}
	if !validHostLabel(sixtyThree) {
		t.Error("a 63-character label should be valid")
	}
	if validHostLabel(sixtyThree + "a") {
		t.Error("a 64-character label should be invalid")
	}
}

func TestTenantForHost(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "wholeops.in")
	seedTenantHostCache(t, map[string]string{
		"acme":  "acme",
		"beta":  "beta-industries",
		"gamma": "gamma",
	})

	cases := []struct {
		name       string
		host       string
		wantTenant string
		wantOK     bool
	}{
		{"known slug", "acme.wholeops.in", "acme", true},
		{"slug differing from tenant id", "beta.wholeops.in", "beta-industries", true},
		{"slug with port", "gamma.wholeops.in:443", "gamma", true},
		{"unknown slug", "nosuch.wholeops.in", "", false},
		{"reserved label", "app.wholeops.in", "", false},
		{"apex", "wholeops.in", "", false},
		{"foreign domain", "acme.example.com", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTenant, gotOK := tenantForHost(tc.host)
			if gotTenant != tc.wantTenant || gotOK != tc.wantOK {
				t.Errorf("tenantForHost(%q) = (%q, %v), want (%q, %v)",
					tc.host, gotTenant, gotOK, tc.wantTenant, tc.wantOK)
			}
		})
	}
}

func TestTenantForHostDisabledWithoutBaseDomain(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "")
	seedTenantHostCache(t, map[string]string{"acme": "acme"})

	// Without the env var the whole feature is inert - this is the property
	// that makes the change a no-op for the tunnel-only and localhost paths.
	if tenantID, ok := tenantForHost("acme.wholeops.in"); ok {
		t.Errorf("tenantForHost returned (%q, true) with no base domain configured", tenantID)
	}
}

// The gate is what stops nosuchclient.<base> from quietly serving the default
// tenant once a wildcard origin certificate means every name completes a
// handshake. Its false cases matter more than its true one: a gate that also
// refused the platform host or localhost would take the whole app down.
func TestTenantHostGate(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "wholeops.in")
	seedTenantHostCache(t, map[string]string{"acme": "acme"})

	cases := []struct {
		name     string
		host     string
		wantCode int
	}{
		{"live tenant host is served", "acme.wholeops.in", http.StatusOK},
		{"live tenant host with port", "acme.wholeops.in:443", http.StatusOK},
		{"platform host is served", "app.wholeops.in", http.StatusOK},
		{"apex is served", "wholeops.in", http.StatusOK},
		{"localhost is served", "localhost:8080", http.StatusOK},
		// Caddy calls the ask gate over loopback mid-handshake; a gate that
		// refused that would deadlock certificate issuance against itself.
		{"loopback is served", "127.0.0.1:8080", http.StatusOK},
		{"foreign domain is served", "erp.example.com", http.StatusOK},
		{"two levels deep is served", "a.b.wholeops.in", http.StatusOK},

		{"unknown tenant host is refused", "nosuch.wholeops.in", http.StatusNotFound},
		{"unknown tenant host with port", "nosuch.wholeops.in:443", http.StatusNotFound},
	}

	gate := tenantHostGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("gate for Host %q = %d, want %d", tc.host, rec.Code, tc.wantCode)
			}
		})
	}
}

// Same inertness guarantee the rest of the feature has: with no base domain
// configured the gate must pass everything through untouched, or the localhost
// and SSH-tunnel paths would start 404ing.
func TestTenantHostGateInertWithoutBaseDomain(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "")
	seedTenantHostCache(t, map[string]string{"acme": "acme"})

	gate := tenantHostGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, host := range []string{"nosuch.wholeops.in", "acme.wholeops.in", "localhost:8080"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("with no base domain configured, Host %q must pass through; got %d", host, rec.Code)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "wholeops.in")

	allowed := []string{
		"https://acme.wholeops.in",
		"https://wholeops.in",
		// Reserved labels are still legitimate browser origins - app.<base>
		// is the console itself. This check is about shape, not tenancy.
		"https://app.wholeops.in",
		// The explicit allowlist still wins first, unchanged.
		"http://localhost:8080",
		"http://127.0.0.1:8080",
	}
	for _, o := range allowed {
		if !originAllowed(o) {
			t.Errorf("originAllowed(%q) = false, want true", o)
		}
	}

	denied := []string{
		"",
		// Plain http under the base domain means someone downgraded.
		"http://acme.wholeops.in",
		// A port implies something other than the public listener.
		"https://acme.wholeops.in:8443",
		"https://a.b.wholeops.in",
		"https://evil.com",
		"https://acme.evilwholeops.in",
		"https://wholeops.in.evil.com",
		"https://acme.wholeops.in/path",
		"https://acme.wholeops.in@evil.com",
		"null",
	}
	for _, o := range denied {
		if originAllowed(o) {
			t.Errorf("originAllowed(%q) = true, want false", o)
		}
	}
}

func TestOriginAllowedWithoutBaseDomain(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "")

	if originAllowed("https://acme.wholeops.in") {
		t.Error("no base domain configured: a tenant origin must not be allowed")
	}
	if !originAllowed("http://localhost:8080") {
		t.Error("the built-in dev allowlist must keep working with no base domain")
	}
}

func TestHandleTLSAsk(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "wholeops.in")
	seedTenantHostCache(t, map[string]string{"acme": "acme"})

	cases := []struct {
		name     string
		query    string
		wantCode int
	}{
		{"live tenant host issues", "?domain=acme.wholeops.in", http.StatusOK},
		{"unknown tenant is refused", "?domain=nosuch.wholeops.in", http.StatusNotFound},
		{"reserved label is refused", "?domain=app.wholeops.in", http.StatusNotFound},
		{"foreign domain is refused", "?domain=evil.com", http.StatusNotFound},
		{"apex is refused", "?domain=wholeops.in", http.StatusNotFound},
		{"missing domain is a bad request", "", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/tls-ask"+tc.query, nil)
			rec := httptest.NewRecorder()
			handleTLSAsk(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("handleTLSAsk(%q) = %d, want %d", tc.query, rec.Code, tc.wantCode)
			}
		})
	}
}

// The ask gate is the only thing standing between a stranger's DNS record and
// unbounded certificate issuance, so "refuses everything when the feature is
// off" is worth locking down on its own.
func TestHandleTLSAskRefusesAllWithoutBaseDomain(t *testing.T) {
	t.Setenv("TENANT_BASE_DOMAIN", "")
	seedTenantHostCache(t, map[string]string{"acme": "acme"})

	req := httptest.NewRequest(http.MethodGet, "/internal/tls-ask?domain=acme.wholeops.in", nil)
	rec := httptest.NewRecorder()
	handleTLSAsk(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("with no base domain configured, ask must refuse; got %d", rec.Code)
	}
}
