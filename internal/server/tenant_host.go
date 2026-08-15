package server

// Stage 44: per-tenant hostnames - <slug>.<TENANT_BASE_DOMAIN> selects a
// tenant, so each client gets their own branded address instead of every
// tenant sharing one hostname.
//
// The whole feature is off unless TENANT_BASE_DOMAIN is set. Unset, every
// function here returns "no match" and request handling is byte-for-byte what
// it was before this file existed - which is what keeps the SSH-tunnel and
// localhost dev paths working unchanged.

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// tenantHostCacheTTL bounds how stale the slug->tenant map may be. Host
// resolution runs on every request, so this must not be a DB round trip each
// time; 60s is short enough that provisioning a tenant feels immediate and
// long enough that the query is effectively free.
const tenantHostCacheTTL = 60 * time.Second

// reservedHostLabels never resolve to a tenant, no matter what a row in
// public.tenants claims. Two reasons: "app" is the platform's own host (the
// admin/console surface, and the one name enable_tls.sh installs a normal
// certificate for), and the rest are names infrastructure conventionally
// owns. Without this, provisioning a tenant whose slug is "www" or "mail"
// would quietly take over that hostname.
var reservedHostLabels = map[string]bool{
	"app": true, "www": true, "api": true, "admin": true,
	"sandbox": true, "staging": true, "status": true, "static": true,
	"cdn": true, "mail": true, "smtp": true, "imap": true,
	"ns1": true, "ns2": true, "autodiscover": true, "_domainkey": true,
}

func tenantBaseDomain() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("TENANT_BASE_DOMAIN")))
}

// hostLabel returns the single subdomain label that host contributes under
// base, or "" if host is not a usable per-tenant hostname.
//
// Deliberately strict - only one level deep. "a.b.example.com" returns ""
// rather than "a.b", because a certificate for a two-level name needs a
// wildcard Let's Encrypt cannot issue over HTTP-01, so accepting it here
// would produce a tenant that resolves but can never be served over TLS.
func hostLabel(host, base string) string {
	if base == "" || host == "" {
		return ""
	}
	h := strings.ToLower(strings.TrimSpace(host))
	// Host carries a port whenever the client sent one (":8080" in dev, and
	// on any non-default port). SplitHostPort errors when there is no port,
	// which is the common case, so the error is the signal to keep h as-is.
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	// A trailing dot is a legal fully-qualified form and some clients send it.
	h = strings.TrimSuffix(h, ".")

	suffix := "." + base
	if !strings.HasSuffix(h, suffix) {
		return ""
	}
	label := strings.TrimSuffix(h, suffix)
	if label == "" || strings.Contains(label, ".") {
		return ""
	}
	if reservedHostLabels[label] || !validHostLabel(label) {
		return ""
	}
	return label
}

// validHostLabel applies the DNS label rules (RFC 1123): 1-63 chars, letters
// digits and hyphens only, not starting or ending with a hyphen.
func validHostLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

var tenantHostCache = struct {
	sync.RWMutex
	slugs  map[string]string // host_slug -> tenant_id
	loaded time.Time
}{slugs: map[string]string{}}

// refreshTenantHostCache reloads the slug map when it is older than the TTL.
//
// On a query error the previous snapshot is kept rather than cleared: a
// transient DB blip should not sign every tenant out of their own hostname,
// and the token claim - not this map - is what actually authorises access.
func refreshTenantHostCache() {
	tenantHostCache.RLock()
	fresh := time.Since(tenantHostCache.loaded) < tenantHostCacheTTL
	tenantHostCache.RUnlock()
	if fresh {
		return
	}

	tenantHostCache.Lock()
	defer tenantHostCache.Unlock()
	// Re-check under the write lock: several requests can get past the read
	// check at once, and only the first should do the query.
	if time.Since(tenantHostCache.loaded) < tenantHostCacheTTL {
		return
	}
	tenantHostCache.loaded = time.Now()

	if db.DB == nil {
		return
	}
	rows, err := db.DB.Query(
		`SELECT tenant_id, LOWER(host_slug) FROM public.tenants
		  WHERE host_slug IS NOT NULL AND host_slug <> ''`)
	if err != nil {
		return
	}
	defer rows.Close()

	next := make(map[string]string)
	for rows.Next() {
		var tenantID, slug string
		if err := rows.Scan(&tenantID, &slug); err != nil {
			return
		}
		next[slug] = tenantID
	}
	if rows.Err() != nil {
		return
	}
	tenantHostCache.slugs = next
}

// invalidateTenantHostCache forces the next lookup to re-read the table.
//
// Called whenever a slug is assigned or cleared. Without it a newly named
// hostname would be refused for up to tenantHostCacheTTL - and not merely by
// the app: the on-demand TLS ask gate answers from this same map, so the
// tenant would get a certificate error rather than a 404, which is a much
// more confusing first impression of their brand-new address.
func invalidateTenantHostCache() {
	tenantHostCache.Lock()
	tenantHostCache.loaded = time.Time{}
	tenantHostCache.Unlock()
}

// normalizeHostSlug validates an operator-supplied slug against exactly the
// rules a hostname will later be held to, and returns it lowercased.
//
// The empty string is legal and means "this tenant has no hostname of its
// own" - the state every tenant starts in and the one the migration leaves
// behind for any tenant_id that is not already a legal DNS label.
func normalizeHostSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", nil
	}
	if !validHostLabel(slug) {
		return "", errInvalidHostSlug
	}
	if reservedHostLabels[slug] {
		return "", errReservedHostSlug
	}
	return slug, nil
}

var (
	errInvalidHostSlug  = errors.New("a hostname label may only contain lowercase letters, digits and hyphens, may not start or end with a hyphen, and may be at most 63 characters")
	errReservedHostSlug = errors.New("that label is reserved for the platform or for infrastructure and cannot be given to a tenant")
)

// handleSetTenantHostSlug assigns (or clears) a tenant's own hostname.
//
// Stage 44 shipped public.tenants.host_slug with no supported way to write it
// - provisioning never set it and no endpoint touched it, so the only way to
// give a client their address was raw SQL against production. This is that
// missing half.
//
// Deliberately its own endpoint rather than a field on some larger tenant
// update: it is the one write that changes which certificate gets issued and
// which hostname resolves where, and it wants its own audit trail and its own
// permission check rather than riding along inside a general edit.
func handleSetTenantHostSlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !engines.IsSuperAdmin(r.Header.Get("Resolved-Role")) {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Only HR/Admin can set a tenant hostname")
		return
	}

	var req struct {
		TenantID string `json:"tenant_id"`
		HostSlug string `json:"host_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Invalid payload")
		return
	}
	if strings.TrimSpace(req.TenantID) == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "Field 'tenant_id' is required")
		return
	}

	slug, err := normalizeHostSlug(req.HostSlug)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// NULL rather than '' for the cleared case, so the partial unique index
	// keeps ignoring these rows - any number of tenants may have no hostname,
	// but '' would collide on the second one.
	var value interface{}
	if slug != "" {
		value = slug
	}
	res, err := db.DB.Exec(
		`UPDATE public.tenants SET host_slug = $1 WHERE tenant_id = $2`, value, req.TenantID)
	if err != nil {
		// The partial unique index on LOWER(host_slug) is the authority on
		// collisions, not a pre-flight SELECT: checking first and inserting
		// after would leave a race between two operators naming the same
		// hostname. Translate its violation into something an operator can
		// act on instead of surfacing a driver message.
		if strings.Contains(strings.ToLower(err.Error()), "tenants_host_slug_lower_key") {
			writeAPIErrorGeneric(w, r, http.StatusConflict,
				"Another tenant already uses the hostname '"+slug+"'")
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "No tenant '"+req.TenantID+"'")
		return
	}

	invalidateTenantHostCache()

	tenantID, userID, _ := resolvedContext(r)
	engines.LogAuditEvent(tenantID, userID, "TENANT-HOST-SLUG", "Update",
		"host_slug for tenant '"+req.TenantID+"' set to '"+slug+"'")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tenant_id": req.TenantID,
		"host_slug": slug,
	})
}

// tenantForHost maps a request's Host header to a tenant id.
//
// The bool is what callers must branch on: a false means "this hostname says
// nothing about the tenant", not "tenant unknown", and the caller falls back
// to the pre-existing header/query/default chain.
func tenantForHost(host string) (string, bool) {
	label := hostLabel(host, tenantBaseDomain())
	if label == "" {
		return "", false
	}
	refreshTenantHostCache()

	tenantHostCache.RLock()
	defer tenantHostCache.RUnlock()
	tenantID, ok := tenantHostCache.slugs[label]
	return tenantID, ok
}

// hostIsUnknownTenant reports whether a request arrived on a hostname that is
// shaped like a tenant address under TENANT_BASE_DOMAIN but names no live
// tenant.
//
// False for everything else, and that distinction is the point: localhost, the
// apex, a reserved label, a foreign domain and (with the feature off) every
// hostname all answer false, so the caller leaves them entirely alone.
func hostIsUnknownTenant(host string) bool {
	if hostLabel(host, tenantBaseDomain()) == "" {
		// Not a per-tenant hostname at all - nothing here has an opinion
		// about it.
		return false
	}
	_, ok := tenantForHost(host)
	return !ok
}

// tenantHostGate refuses those hostnames outright.
//
// Stage 44.10, forced by 26.1.3b. This used to be handled a layer down:
// on-demand TLS asked the app
// before obtaining a certificate, so a hostname that named no tenant never got
// one and never completed a handshake. Behind Cloudflare that gate is gone -
// the origin serves one wildcard certificate for every name under the base
// domain, so an unknown hostname now reaches Go. Without this it would fall
// through to X-Tenant-ID and then to "default", and nosuchclient.<base> would
// quietly serve the default tenant's login page.
//
// Not a security boundary - the token claim still scopes every query, and it
// did before this existed. It is the product guarantee that a per-client
// address means a client.
//
// Wraps the whole mux rather than apiMiddleware, because the SPA shell and the
// static files are served outside that middleware and are exactly what a
// browser asks for first.
func tenantHostGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hostIsUnknownTenant(r.Host) {
			// Content-Type set explicitly: writeResponse commits the header
			// before encoding, so nothing downstream can sniff it afterwards,
			// and this path never runs apiMiddleware - which is what sets it
			// for every ordinary API error.
			w.Header().Set("Content-Type", "application/json")
			// writeResponse rather than writeAPIErrorGeneric, deliberately.
			// The envelope is the same one every other error uses (this is
			// the single function all of them write through), but the catalog
			// path is skipped: GLOBAL-0004 carries LogRequired, and an
			// unknown hostname is a stranger's typo or a scanner sweeping
			// subdomains, not a system error. Sending it through the catalog
			// would let anyone fill public system_errors at one row per
			// request just by walking random names under the base domain.
			writeResponse(w, http.StatusNotFound, apiErrorBody{
				Error: "No workspace is registered at this address.",
				Code:  "GLOBAL-0004",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed decides whether an Origin gets CORS headers.
//
// The explicit CORS_ALLOWED_ORIGINS allowlist still wins first and is
// unchanged. The addition is that when TENANT_BASE_DOMAIN is set, https
// origins at the base domain or one label under it are accepted too -
// otherwise every newly provisioned tenant would need an app restart with a
// longer CORS_ALLOWED_ORIGINS before its own hostname could call the API.
//
// Scoped tightly on purpose: https only (an http origin under the base domain
// means someone downgraded, and is never legitimate here), no port, no path,
// and the same single-label rule tenant hostnames themselves follow.
func originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	if corsAllowedOrigins[origin] {
		return true
	}
	base := tenantBaseDomain()
	if base == "" {
		return false
	}
	const scheme = "https://"
	if !strings.HasPrefix(origin, scheme) {
		return false
	}
	h := strings.ToLower(origin[len(scheme):])
	// An Origin is scheme+host+optional port and nothing else; anything with
	// a path, query or fragment is malformed and not worth guessing about.
	if strings.ContainsAny(h, "/?#@") || strings.Contains(h, ":") {
		return false
	}
	if h == base {
		return true
	}
	// Reserved labels are still valid origins (app.<base> is the console
	// itself), so this checks shape only, not tenant existence.
	label := strings.TrimSuffix(h, "."+base)
	if label == h || label == "" || strings.Contains(label, ".") {
		return false
	}
	return validHostLabel(label)
}

// handleTLSAsk backs Caddy's on_demand_tls "ask" directive: before Caddy asks
// Let's Encrypt for a certificate for a hostname it has never seen, it calls
// this and issues only on a 2xx.
//
// This gate is the whole reason on-demand TLS is safe to turn on. Without it,
// anyone who pointed a DNS record at this box could make Caddy request
// certificates on demand - burning the ACME rate limit and filling the cert
// store with hostnames that have nothing to do with this deployment.
//
// Answers only from the slug cache, so it costs no DB round trip in the TLS
// handshake path.
func handleTLSAsk(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		http.Error(w, "domain required", http.StatusBadRequest)
		return
	}
	if _, ok := tenantForHost(domain); !ok {
		// 404 rather than 403: Caddy treats any non-2xx as "do not issue",
		// and this is literally "no such tenant hostname here".
		http.Error(w, "unknown host", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
