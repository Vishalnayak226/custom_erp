package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"custom_erp/engines"
)

// Stage 14.6: application version, embedded from the repo-root VERSION file
// at compile time so it can never drift out of sync with the binary that
// reads it. gitCommit/buildTime default to "dev"/"unknown" for a bare
// `go build` and are only populated by manage.ps1's release action, via
// `-ldflags "-X main.gitCommit=... -X main.buildTime=..."`.
//
//go:embed VERSION
var embeddedVersion string

var (
	gitCommit = "dev"
	buildTime = "unknown"
)

// currentAppVersion returns the embedded VERSION file's content with
// surrounding whitespace (the trailing newline every text file has)
// trimmed - callers want "0.1.0", not "0.1.0\n".
// App version/build-stamp vars, rate limiting, CORS, inbound-webhook
// signature verification, and the security/feature/module gating middleware
// chain apiMiddleware wraps every route in. Extracted from the original
// single-file main.go (Stage 19 folder restructuring, 2026-07-19) - see
// routes.go for where these are actually wired together.

func currentAppVersion() string {
	return strings.TrimSpace(embeddedVersion)
}

// RequestContext holds basic metadata for tracking execution
type RequestContext struct {
	TenantID      string
	CorrelationID string
	UserID        string
	Role          string
	LocationCode  string
}

// Simple sliding window rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
	}
}

func (rl *RateLimiter) Allow(ip string, limit int, duration time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-duration)

	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit {
		rl.requests[ip] = valid
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

var globalLimiter = NewRateLimiter()

// Per-tenant concurrency quota (24.30, loophole #32). This app runs one
// shared db.DB connection pool (db/db.go's dbMaxOpenConns, currently 50)
// across every tenant's schema via search_path, not a separate pool per
// tenant - a real per-tenant *pool* would be a bigger architecture change
// than this single-process/single-Postgres-instance app's current scale
// justifies (24.30's own checklist note). This bounds each tenant's share
// of that one shared pool instead: capping how many of a tenant's requests
// can be in-flight at once (each holding a connection) so one noisy tenant
// can't starve every other tenant of the shared pool.
const perTenantMaxConcurrentRequests = 15

type tenantConcurrencyLimiter struct {
	mu      sync.Mutex
	inFlight map[string]int
}

var tenantConcurrency = &tenantConcurrencyLimiter{inFlight: make(map[string]int)}

// acquire returns true (and reserves a slot) if tenantID is below its
// concurrency cap. Callers that get true MUST call release exactly once.
func (l *tenantConcurrencyLimiter) acquire(tenantID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[tenantID] >= perTenantMaxConcurrentRequests {
		return false
	}
	l.inFlight[tenantID]++
	return true
}

func (l *tenantConcurrencyLimiter) release(tenantID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[tenantID] > 0 {
		l.inFlight[tenantID]--
	}
}

// snapshot returns a copy of current in-flight request counts per tenant,
// for the Stage 26.1.5 tenant-usage/health dashboard (handleTenantUsage).
// Only tenants with at least one in-flight request have an entry - callers
// must treat a missing tenant as 0, not as "unknown".
func (l *tenantConcurrencyLimiter) snapshot() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.inFlight))
	for k, v := range l.inFlight {
		out[k] = v
	}
	return out
}

// rateLimitCategory classifies a request into one of SEC-V2 5's API types
// and returns that category's per-minute budget (Stage 13.14). Categories
// SEC-V2 names that don't apply to this codebase are omitted rather than
// faked: Payment Callback (no payment gateway integration exists),
// GST/IRN retry (no real IRN integration - Stage 13.10 scoped that out
// explicitly), and POS Offline Sync (no offline-sync feature exists).
// Webhook signature/timestamp validation is tracked separately
// (micro_checklist Stage 9.2) - this function only assigns its rate budget.
func rateLimitCategory(path, method string) (category string, limit int) {
	switch {
	case strings.HasSuffix(path, "/login") || strings.HasSuffix(path, "/mfa/verify") || strings.HasSuffix(path, "/mfa/activate") ||
		strings.HasSuffix(path, "/forgot-password") || strings.HasSuffix(path, "/reset-password"):
		// Login API: also covers MFA code submission (brute-forceable
		// 6-digit TOTP) and password reset request/completion (24.28) -
		// both are exactly the kind of low-frequency, high-sensitivity
		// action that needs the tightest budget, not the 60/min default.
		return "login", 5
	case strings.HasPrefix(path, "/api/v1/import/"):
		// Bulk Upload API: file processing is the heaviest per-request cost
		// in this codebase, so it gets the tightest non-login budget.
		return "bulk-upload", 10
	case strings.HasPrefix(path, "/api/v1/reports/") || strings.HasSuffix(path, "/finance/trial-balance"):
		// Report API: SEC-V2 asks these be restricted/queued as "heavy".
		return "report", 20
	case strings.HasPrefix(path, "/api/v1/integration/shopify/"):
		// Webhook API: bursts from the external platform are expected and
		// legitimate, so this gets a higher budget than login/reports.
		return "webhook", 30
	case strings.HasPrefix(path, "/api/v1/doc/") && method == "GET":
		// Search API: the generic doc list/get endpoint. Already paginated
		// server-side (Stage 1.4/hardening_roadmap Phase 2.4), so a
		// generous budget is safe here.
		return "search", 100
	default:
		return "default", 60
	}
}

// safeFilterKeyRe allowlists dynamic query-filter keys before they're spliced into SQL
// (data->>'<key>'). Only plain identifiers are permitted - no quotes, operators, or whitespace.
var safeFilterKeyRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,63}$`)

// Pagination bounds for the generic document list endpoint. defaultListLimit applies
// even when the caller passes no limit/offset at all, so the endpoint can never return
// a truly unbounded result set; maxListLimit caps what a caller can explicitly request.
const defaultListLimit = 500
const maxListLimit = 1000

// corsAllowedOrigins is the explicit CORS allowlist. Local dev origins are always
// included; CORS_ALLOWED_ORIGINS (comma-separated) adds more for real deployments.
// An Origin not in this set never gets Access-Control-Allow-Origin/-Credentials -
// the browser blocks the cross-origin response, which is the point.
var corsAllowedOrigins = loadCORSAllowlist()

// Account-level brute-force lockout thresholds (Stage 14.21-14.24). 10
// attempts is deliberately more permissive than the IP-scoped rate
// limiter's 5/min (Stage 13.14) - that one already stops a single-source
// burst; this one exists for the distributed case, so it shouldn't also
// punish a legitimate user who mistypes a password a few times.
const (
	accountLockoutThreshold = 10
	accountLockoutDuration  = 15 * time.Minute
)

// shopifyWebhookSecret (Stage 14.21-14.24, closes the same gap the
// re-opened Stage 9.2 item tracked) - same override-via-env-var pattern as
// JWT_SECRET/CORS_ALLOWED_ORIGINS. Unlike JWT_SECRET, this can't be
// auto-generated and silently persisted: it has to match a value
// configured on Shopify's side too.
//
// 24.35 (2026-07-25): an unset secret used to mean "skip verification,
// accept the request anyway" - that fails OPEN, so anyone who knows (or
// guesses) this endpoint's URL could POST an unauthenticated forged
// "Shopify" order/product payload and have it processed as genuine. Fails
// CLOSED instead now, matching engines.VerifyWebhookHMAC's (BigCommerce)
// posture: no secret configured means the integration isn't set up for
// this environment, so inbound webhook traffic is rejected outright, not
// silently trusted.
var shopifyWebhookSecret = os.Getenv("SHOPIFY_WEBHOOK_SECRET")
var shopifyWebhookSecretWarnOnce sync.Once

// verifyShopifyWebhookSignature checks the X-Shopify-Hmac-Sha256 header
// (base64 HMAC-SHA256 of the raw body) against shopifyWebhookSecret, using
// a constant-time comparison. Returns false (rejects the request) if no
// secret is configured (logging a one-time warning in that case so an
// operator notices the endpoint is currently rejecting all traffic) or if
// the signature doesn't match.
func verifyShopifyWebhookSignature(r *http.Request, body []byte) bool {
	if shopifyWebhookSecret == "" {
		shopifyWebhookSecretWarnOnce.Do(func() {
			log.Println("[SECURITY] SHOPIFY_WEBHOOK_SECRET is not set - inbound Shopify webhooks are being REJECTED (fail-closed). Set it before wiring up real Shopify traffic.")
		})
		return false
	}
	sig := r.Header.Get("X-Shopify-Hmac-Sha256")
	if sig == "" {
		return false
	}
	h := hmac.New(sha256.New, []byte(shopifyWebhookSecret))
	h.Write(body)
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// publicRoutes lists the only endpoints reachable with no bearer token at
// all (Stage 14.6 adds /api/v1/version to what was previously just /login -
// a client/ops tool needs to be able to check what build is running without
// first authenticating). Deliberately a small, explicit allowlist rather
// than a path-prefix rule, so adding a new public route is always a
// one-line, reviewable decision.
var publicRoutes = map[string]bool{
	"/api/v1/login":                true,
	"/api/v1/version":              true,
	"/api/v1/health":               true,
	"/api/v1/auth/forgot-password": true,
	"/api/v1/auth/reset-password":  true,
}

func loadCORSAllowlist() map[string]bool {
	allowed := map[string]bool{
		"http://localhost:8080": true,
		"http://127.0.0.1:8080": true,
	}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				allowed[o] = true
			}
		}
	}
	return allowed
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Middleware wrapper to inject TenantID, User Claims, and enforce security policies
// securityHeaders sets browser-enforced defensive headers on every response -
// static assets and API alike - by wrapping the whole mux once, rather than
// depending on every future route remembering to add apiMiddleware.
//
// script-src/style-src include 'unsafe-inline': public/index.html and
// public/app.js render onclick="..." attribute handlers throughout the UI
// (21 occurrences today), and CSP treats those the same as inline <script>
// tags. Dropping 'unsafe-inline' would break every button in the app until
// those are refactored to addEventListener - a separate frontend task, not
// part of this header change. frame-ancestors 'none' (plus X-Frame-Options
// for older browsers) still gives real clickjacking protection regardless.
func securityHeaders(next http.Handler) http.Handler {
	// style-src/font-src allow Google Fonts specifically (public/styles.css
	// @imports fonts.googleapis.com, which serves @font-face rules pointing at
	// fonts.gstatic.com) - the only external resource this app actually loads.
	const csp = "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com; " +
		// blob: (not just data:) is required for the PIM media gallery
		// (Stage 15.2/26.4.4): images/thumbnails are fetched as authenticated
		// blobs and rendered via URL.createObjectURL, since a plain <img src>
		// can't carry the Authorization header the media endpoints require -
		// found via a live browser check (CSP was silently blocking every
		// media thumbnail/preview in the gallery until this was added).
		"img-src 'self' data: blob:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// Strict-Transport-Security is a no-op over plain HTTP (dev today) and
		// only takes effect once served over TLS - safe to set unconditionally.
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

// featureGate 403s a request if the resolved tenant has the named feature
// flag disabled. Must be composed inside apiMiddleware (e.g.
// apiMiddleware(featureGate("wms_integration", handler))) since it reads
// Resolved-Tenant-ID, which apiMiddleware sets before calling next. Any
// failure to positively confirm "enabled" (DB error, flag never registered
// for this tenant) blocks the request - same fail-closed default
// engines.IsFeatureEnabled already applies for an unregistered flag.
func featureGate(featureName string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		enabled, _ := engines.IsFeatureEnabled(tenantID, featureName)
		if !enabled {
			// Decided (docs/micro_checklist.md 23.13): the matrix's SAAS-0192
			// "Feature flag disabled" row specifies a soft 200/inline
			// response, but this gate deliberately keeps failing closed with
			// 403. A disabled-feature request is exactly the kind of
			// authorization check that should never look like success to a
			// caller that didn't pass it - a soft 200 here would mean every
			// caller (including this codebase's own frontend fetch helpers,
			// which treat any 2xx as "request succeeded") has to separately
			// inspect the body to discover the action never ran, which is a
			// worse and more error-prone contract than a plain 403. Kept as
			// a generic-coded 403 rather than adopting the matrix's row.
			writeAPIErrorGeneric(w, r, http.StatusForbidden, fmt.Sprintf("Feature '%s' is disabled for this tenant", featureName))
			return
		}
		next(w, r)
	}
}

// moduleGate 403s a request if the resolved tenant has the named functional
// module disabled (Stage 14.1, public.modules / tenant_default.module_entitlements
// - see engines/modules.go). Sibling of featureGate: same composition rule
// (must sit inside apiMiddleware so Resolved-Tenant-ID is already set), same
// fail-closed default. featureGate gates optional external integrations
// (Shopify/WMS/forecasting); moduleGate gates whole functional areas of the
// core product (HR, Manufacturing, Assets, ...) - the two are independent
// and can both wrap the same handler.
func moduleGate(moduleKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("Resolved-Tenant-ID")
		enabled, _ := engines.IsModuleEnabled(tenantID, moduleKey)
		if !enabled {
			// SAAS-0191 "Module disabled for tenant" - exact scenario + status
			// (403) match for this gate.
			writeAPIError(w, r, "SAAS-0191", "")
			return
		}
		next(w, r)
	}
}

func apiMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		correlationID := generateUUID()
		w.Header().Set("X-Correlation-ID", correlationID)
		w.Header().Set("Content-Type", "application/json")
		// Published to the request early (rather than only at the end of this
		// function, with the other Resolved-* fields) so writeAPIError/
		// writeAPIErrorGeneric can read it from any rejection point below,
		// including ones that fire before tenant/auth resolution.
		r.Header.Set("Resolved-Correlation-ID", correlationID)

		// 1. CORS Headers (explicit allowlist - never reflect an arbitrary Origin)
		origin := r.Header.Get("Origin")
		if origin != "" && corsAllowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// 2. Payload size limit (Max 2MB)
		r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

		// 3. Rate Limiter - per-API-type budgets (Stage 13.14, SEC-V2 5).
		// Keyed by ip+category rather than ip alone, so heavy traffic on one
		// API type (e.g. search) can't exhaust the budget for an unrelated
		// one (e.g. login) - they're independent buckets, not a shared pool.
		ip := strings.Split(r.RemoteAddr, ":")[0]
		category, limit := rateLimitCategory(r.URL.Path, r.Method)
		if !globalLimiter.Allow(ip+":"+category, limit, time.Minute) {
			// SEC-0280 "Rate limit exceeded" - exact scenario + status (429) match.
			writeAPIError(w, r, "SEC-0280", "")
			return
		}

		// Panic Recovery. Logging stays a direct engines.LogSystemError call
		// (not the writeAPIError/logForEntry path) so severity is literally
		// "PANIC" - LogSystemError special-cases that exact string to fire an
		// immediate ops alert (engines/logs.go), which the catalog's
		// "Critical" severity for GLOBAL-0302 must not silently swallow.
		defer func() {
			if err := recover(); err != nil {
				stackTrace := string(debug.Stack())
				errMsg := fmt.Sprintf("%v", err)
				tenantID := r.Header.Get("Resolved-Tenant-ID")
				if tenantID == "" {
					tenantID = "default"
				}
				engines.LogSystemError(tenantID, correlationID, "PANIC", r.URL.Path, errMsg, stackTrace)

				entry := errorCatalog["GLOBAL-0302"]
				writeResponse(w, entry.HTTPStatus, apiErrorBody{
					Error:         entry.UserMessage,
					Code:          entry.Code,
					CorrelationID: correlationID,
					Retryable:     entry.Retryable,
				})
			}
		}()

		// 4. Token & Tenant Resolution
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID == "" {
			tenantID = r.URL.Query().Get("tenant_id")
		}
		if tenantID == "" {
			tenantID = "default"
		}

		userID := ""
		username := ""
		role := ""
		locationCode := ""
		purpose := ""
		scopeDoctype := ""

		// Inspect Authorization Header
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := engines.ParseToken(tokenStr)
			if err == nil {
				userID = claims["id"]
				username = claims["user"]
				role = claims["role"]
				tenantID = claims["tenant"]
				locationCode = claims["loc"]
				purpose = claims["purpose"]
				scopeDoctype = claims["scope_doctype"]
			} else {
				// GLOBAL-0009 "Session expired" - closest catalog match for a
				// rejected/expired bearer token (401).
				writeAPIError(w, r, "GLOBAL-0009", "")
				return
			}
		} else if !publicRoutes[r.URL.Path] {
			// No token and this isn't one of the explicit public routes: reject.
			writeAPIError(w, r, "GLOBAL-0009", "")
			return
		}

		// Attach Resolved Context fields (Resolved-Correlation-ID was already
		// set at the top of this function, before rate-limit/auth could reject).
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-User-ID", userID)
		r.Header.Set("Resolved-Username", username)
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-Location", locationCode)
		// Resolved-Purpose is only non-empty for narrowly-scoped MFA
		// enrollment/challenge tokens (see engines.SignPurposeToken) - a full
		// session token has no "purpose" claim, so this stays "".
		r.Header.Set("Resolved-Purpose", purpose)
		// Resolved-Scope-Doctype is only non-empty for an extension token
		// (Stage 14.17-14.20, engines.SignExtensionToken) - enforced
		// explicitly in handleGenericDoc, not a general-purpose claim.
		r.Header.Set("Resolved-Scope-Doctype", scopeDoctype)

		// 24.30: per-tenant concurrency quota, checked only now that
		// tenantID is fully resolved (a token's own tenant claim, if
		// present, already overrode the header/query-param/default value
		// above). Rejects fast rather than letting an already-overloaded
		// tenant's next request queue indefinitely for a shared pool
		// connection another tenant needs too.
		if !tenantConcurrency.acquire(tenantID) {
			writeAPIErrorGeneric(w, r, http.StatusServiceUnavailable, "Too many concurrent requests in flight for this tenant - please retry shortly")
			return
		}
		defer tenantConcurrency.release(tenantID)

		next.ServeHTTP(w, r)
	}
}
