package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
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
	case strings.HasSuffix(path, "/login") || strings.HasSuffix(path, "/mfa/verify") || strings.HasSuffix(path, "/mfa/activate"):
		// Login API: also covers MFA code submission - a 6-digit TOTP code
		// is brute-forceable without a tight budget here.
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
// configured on Shopify's side too, so an unset secret means "no real
// Shopify integration is configured for this environment" and signature
// verification is skipped rather than failing closed - the same posture
// this app already has today (no verification at all), just now able to
// actually enforce it once a real secret is set.
var shopifyWebhookSecret = os.Getenv("SHOPIFY_WEBHOOK_SECRET")
var shopifyWebhookSecretWarnOnce sync.Once

// verifyShopifyWebhookSignature checks the X-Shopify-Hmac-Sha256 header
// (base64 HMAC-SHA256 of the raw body) against shopifyWebhookSecret, using
// a constant-time comparison. Returns true if verification passed OR no
// secret is configured (logging a one-time warning in that case so an
// operator notices verification isn't actually active).
func verifyShopifyWebhookSignature(r *http.Request, body []byte) bool {
	if shopifyWebhookSecret == "" {
		shopifyWebhookSecretWarnOnce.Do(func() {
			log.Println("[SECURITY] SHOPIFY_WEBHOOK_SECRET is not set - inbound Shopify webhook signature verification is DISABLED. Set it before accepting real Shopify traffic.")
		})
		return true
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
	"/api/v1/login":   true,
	"/api/v1/version": true,
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
		"img-src 'self' data:; " +
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
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Feature '%s' is disabled for this tenant", featureName),
			})
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
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("Module '%s' is disabled for this tenant", moduleKey),
			})
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
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Rate limit exceeded. Please try again later.",
			})
			return
		}

		// Panic Recovery
		defer func() {
			if err := recover(); err != nil {
				stackTrace := string(debug.Stack())
				errMsg := fmt.Sprintf("%v", err)
				tenantID := r.Header.Get("Resolved-Tenant-ID")
				if tenantID == "" {
					tenantID = "default"
				}
				engines.LogSystemError(tenantID, correlationID, "PANIC", r.URL.Path, errMsg, stackTrace)

				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":          "A critical server error occurred.",
					"correlation_id": correlationID,
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
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or expired token"})
				return
			}
		} else if !publicRoutes[r.URL.Path] {
			// No token and this isn't one of the explicit public routes: reject.
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Authentication required"})
			return
		}

		// Attach Resolved Context fields
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Correlation-ID", correlationID)
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

		next.ServeHTTP(w, r)
	}
}

func main() {
	// Initialize database connection
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	}
	db.InitDB(connStr)

	// Start Outbox background poller (Scale and Omnichannel integration queue)
	engines.StartOutboxWorker(5 * time.Second)

	// Start PIM Publish Queue background poller (Stage 15.2 - stub connector,
	// see engines/pim_publish.go's file header for the real-connector caveat)
	engines.StartPublishQueueWorker(10 * time.Second)

	// Start Magento Open Source order-change poller (Stage 16.4) - the
	// substitute for native webhooks Magento Open Source does not have.
	// Adobe Commerce Cloud channels use real webhooks instead and are
	// skipped by this poller (see engines/connector_magento.go).
	engines.StartMagentoPollWorker(60 * time.Second)

	// Start Patch/Bug-Intake background worker (Stage 14.13-14.16). Never
	// mutates tenant/business state - see engines/patchintake.go's file
	// header for why that's true by construction, not just by convention.
	engines.StartPatchIntakeWorker(24 * time.Hour)

	// Start Stage 9.1 Integration Workers (Unicommerce, Pine Labs, CleverTap)
	engines.StartUnicommerceWorker(30 * time.Second)
	engines.StartPineLabsReconciliationWorker(5 * time.Minute)
	engines.StartCleverTapWorker(30 * time.Second)

	// Authentication API
	http.HandleFunc("POST /api/v1/login", apiMiddleware(handleLogin))

	// Version (Stage 14.6) - public, same tier as /login, so a client/ops
	// tool can check what build is running without authenticating first.
	http.HandleFunc("GET /api/v1/version", apiMiddleware(handleVersion))
	http.HandleFunc("POST /api/v1/auth/mfa/enroll", apiMiddleware(handleMFAEnroll))
	http.HandleFunc("POST /api/v1/auth/mfa/activate", apiMiddleware(handleMFAActivate))
	http.HandleFunc("POST /api/v1/auth/mfa/verify", apiMiddleware(handleMFAVerify))

	// Generic DocType CRUD APIs (Go 1.22 enhanced routing)
	http.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("POST /api/v1/doc/{doctype}/{id}/reactivate", apiMiddleware(handleReactivateMasterDocument))

	// Availability & Reservation APIs
	http.HandleFunc("GET /api/v1/availability", apiMiddleware(handleGetAvailability))
	http.HandleFunc("POST /api/v1/reserve", apiMiddleware(handleCreateReservation))

	// Checkout & Finance APIs
	http.HandleFunc("POST /api/v1/checkout", apiMiddleware(handleCheckout))
	http.HandleFunc("GET /api/v1/finance/trial-balance", apiMiddleware(handleTrialBalance))
	http.HandleFunc("GET /api/v1/finance/periods", apiMiddleware(handleAccountingPeriods))
	http.HandleFunc("POST /api/v1/finance/periods", apiMiddleware(handleAccountingPeriods))
	http.HandleFunc("POST /api/v1/finance/periods/{id}/close", apiMiddleware(handleCloseAccountingPeriod))

	// Approval / Workflow Engine (maker-checker)
	http.HandleFunc("POST /api/v1/approval/submit", apiMiddleware(handleSubmitApproval))
	http.HandleFunc("POST /api/v1/approval/decide", apiMiddleware(handleDecideApproval))
	http.HandleFunc("GET /api/v1/approval/pending", apiMiddleware(handleListPendingApprovals))
	http.HandleFunc("GET /api/v1/approval/rules", apiMiddleware(handleApprovalRules))

	// GST / Tax Engine
	http.HandleFunc("POST /api/v1/gst/calculate", apiMiddleware(handleCalculateGST))

	// Report Catalog (Stage 14.1: module-gated - "reports")
	http.HandleFunc("GET /api/v1/reports/current-stock", apiMiddleware(moduleGate("reports", handleCurrentStockReport)))
	http.HandleFunc("GET /api/v1/reports/sales-register", apiMiddleware(moduleGate("reports", handleSalesRegisterReport)))
	http.HandleFunc("GET /api/v1/reports/vendor-ledger", apiMiddleware(moduleGate("reports", handleVendorLedgerReport)))
	http.HandleFunc("GET /api/v1/reports/payables-ageing", apiMiddleware(moduleGate("reports", handlePayablesAgeingReport)))

	// RFQ / Vendor Quote / Quote Comparison (Stage 14.1: module-gated - "rfq")
	http.HandleFunc("GET /api/v1/rfq/quotes", apiMiddleware(moduleGate("rfq", handleGetVendorQuotesForRFQ)))
	http.HandleFunc("POST /api/v1/rfq/select-quote", apiMiddleware(moduleGate("rfq", handleSelectWinningQuote)))

	// Sticker / Barcode Printing (Stage 14.1: module-gated - "stickers")
	http.HandleFunc("POST /api/v1/stickers/print", apiMiddleware(moduleGate("stickers", handlePrintStickers)))
	http.HandleFunc("GET /api/v1/stickers/history", apiMiddleware(moduleGate("stickers", handlePrintHistory)))

	// HR Foundation (Stage 14.1: module-gated - "hr")
	http.HandleFunc("GET /api/v1/hr/payroll-export", apiMiddleware(moduleGate("hr", handlePayrollExport)))

	// Fixed Asset Management (Stage 14.1: module-gated - "assets")
	http.HandleFunc("GET /api/v1/assets/register", apiMiddleware(moduleGate("assets", handleAssetRegister)))
	http.HandleFunc("POST /api/v1/assets/capitalize", apiMiddleware(moduleGate("assets", handleCapitalizeAsset)))
	http.HandleFunc("POST /api/v1/assets/transfer", apiMiddleware(moduleGate("assets", handleTransferAsset)))
	http.HandleFunc("POST /api/v1/assets/dispose", apiMiddleware(moduleGate("assets", handleDisposeAsset)))

	// Expense Management (Stage 14.1: module-gated - "expenses")
	http.HandleFunc("POST /api/v1/expenses/verify", apiMiddleware(moduleGate("expenses", handleVerifyExpenseClaim)))
	http.HandleFunc("POST /api/v1/expenses/pay", apiMiddleware(moduleGate("expenses", handlePayExpenseClaim)))

	// CRM / Loyalty (Stage 14.1: module-gated - "crm_loyalty")
	http.HandleFunc("POST /api/v1/loyalty/redeem", apiMiddleware(moduleGate("crm_loyalty", handleRedeemLoyaltyPoints)))
	http.HandleFunc("GET /api/v1/loyalty/ledger", apiMiddleware(moduleGate("crm_loyalty", handleLoyaltyLedger)))

	// Manufacturing (Stage 14.1: module-gated - "manufacturing")
	http.HandleFunc("POST /api/v1/manufacturing/issue-material", apiMiddleware(moduleGate("manufacturing", handleIssueProductionMaterial)))
	http.HandleFunc("POST /api/v1/manufacturing/complete", apiMiddleware(moduleGate("manufacturing", handleCompleteProductionOrder)))

	// PIM Foundation MVP (Stage 15: module-gated - "pim")
	// Dashboard (Stage 16.5a) reads the existing PIM snapshot/queue state;
	// it is a fixed PIM route, so module gating happens at registration.
	http.HandleFunc("GET /api/v1/pim/dashboard", apiMiddleware(moduleGate("pim", handlePIMDashboard)))
	// Bulk edit (Stage 16.5b) is deliberately a PIM-only endpoint; its
	// handler additionally applies the target doctype's normal update RBAC.
	http.HandleFunc("POST /api/v1/pim/bulk-edit", apiMiddleware(moduleGate("pim", handlePIMBulkEdit)))
	http.HandleFunc("GET /api/v1/pim/reports/{name}", apiMiddleware(moduleGate("pim", handlePIMReport)))
	http.HandleFunc("GET /api/v1/pim/workbench", apiMiddleware(moduleGate("pim", handlePIMWorkbench)))
	http.HandleFunc("GET /api/v1/pim/completeness/{itemCode}", apiMiddleware(moduleGate("pim", handlePIMCompleteness)))
	// Media Library (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/media/upload", apiMiddleware(moduleGate("pim", handlePIMMediaUpload)))
	http.HandleFunc("GET /api/v1/pim/media/{id}/file", apiMiddleware(moduleGate("pim", handlePIMMediaFile)))
	http.HandleFunc("GET /api/v1/pim/media", apiMiddleware(moduleGate("pim", handlePIMMediaList)))
	http.HandleFunc("POST /api/v1/pim/media/{id}/deactivate", apiMiddleware(moduleGate("pim", handlePIMMediaDeactivate)))
	// Channel Publishing (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/publish", apiMiddleware(moduleGate("pim", handlePIMPublish)))
	http.HandleFunc("GET /api/v1/pim/publish/{jobID}", apiMiddleware(moduleGate("pim", handlePIMPublishJobStatus)))
	http.HandleFunc("GET /api/v1/pim/publish-log", apiMiddleware(moduleGate("pim", handlePIMPublishLog)))
	// Import/Export (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/import/{doctype}/preview", apiMiddleware(moduleGate("pim", handlePIMImportPreview)))
	http.HandleFunc("GET /api/v1/pim/import-jobs/{id}/errors.csv", apiMiddleware(moduleGate("pim", handlePIMImportJobErrors)))
	// Real Channel Connector Framework (Stage 16.1) - write-only credential
	// endpoint, HR/Admin only; there is deliberately no matching GET.
	http.HandleFunc("POST /api/v1/pim/channels/{code}/credentials", apiMiddleware(moduleGate("pim", handleSaveChannelCredential)))
	http.HandleFunc("POST /api/v1/integration/bigcommerce/webhook/{channelCode}", apiMiddleware(handleBigCommerceWebhook))

	// Shopify Integration Webhook APIs (gated by the "oms_integration" flag)
	http.HandleFunc("POST /api/v1/integration/shopify/product/map", apiMiddleware(featureGate("oms_integration", handleShopifyProductMap)))
	http.HandleFunc("POST /api/v1/integration/shopify/order", apiMiddleware(featureGate("oms_integration", handleShopifyOrderWebhook)))

	// Store Fulfillment & Returns APIs (gated by the "wms_integration" flag)
	http.HandleFunc("POST /api/v1/fulfillment/task/transition", apiMiddleware(featureGate("wms_integration", handleFulfillmentTaskTransition)))
	http.HandleFunc("POST /api/v1/fulfillment/return", apiMiddleware(featureGate("wms_integration", handleFulfillmentReturn)))

	// Administration Scale Test APIs
	http.HandleFunc("POST /api/v1/admin/scale-test", apiMiddleware(handleScaleTest))

	// Marketplace & Logistics Integration APIs (gated by the "oms_integration" flag)
	http.HandleFunc("POST /api/v1/marketplace/settlement/reconcile", apiMiddleware(featureGate("oms_integration", handleMarketplaceReconcile)))
	http.HandleFunc("POST /api/v1/marketplace/logistics/book", apiMiddleware(featureGate("oms_integration", handleLogisticsBook)))

	// Optimization & Advanced Forecasting APIs (gated by the "advanced_forecasting" flag)
	http.HandleFunc("GET /api/v1/optimization/replenishment-suggestions", apiMiddleware(featureGate("advanced_forecasting", handleReplenishmentSuggestions)))
	http.HandleFunc("GET /api/v1/optimization/sla-breaches", apiMiddleware(featureGate("advanced_forecasting", handleSLABreaches)))
	http.HandleFunc("POST /api/v1/optimization/forecast", apiMiddleware(featureGate("advanced_forecasting", handleDemandForecast)))

	// Stage 9.1: Unicommerce Integration APIs
	http.HandleFunc("POST /api/v1/unicommerce/credentials", apiMiddleware(handleUnicommerceCredentials))
	http.HandleFunc("GET /api/v1/unicommerce/credentials", apiMiddleware(handleGetUnicommerceCredentials))
	http.HandleFunc("POST /api/v1/unicommerce/order", apiMiddleware(handleUnicommerceOrder))
	http.HandleFunc("GET /api/v1/unicommerce/orders", apiMiddleware(handleListUnicommerceOrders))
	http.HandleFunc("GET /api/v1/unicommerce/inventory-syncs", apiMiddleware(handleListUnicommerceInventorySyncs))

	// Stage 9.1: Pine Labs Plutus Integration APIs
	http.HandleFunc("POST /api/v1/pinelabs/credentials", apiMiddleware(handlePineLabsCredentials))
	http.HandleFunc("GET /api/v1/pinelabs/credentials", apiMiddleware(handleGetPineLabsCredentials))
	http.HandleFunc("POST /api/v1/pinelabs/transaction", apiMiddleware(handlePineLabsTransaction))
	http.HandleFunc("POST /api/v1/pinelabs/reconcile", apiMiddleware(handlePineLabsReconcile))
	http.HandleFunc("GET /api/v1/pinelabs/transactions", apiMiddleware(handleListPineLabsTransactions))

	// Stage 9.1: CleverTap Integration APIs
	http.HandleFunc("POST /api/v1/clevertap/credentials", apiMiddleware(handleCleverTapCredentials))
	http.HandleFunc("GET /api/v1/clevertap/credentials", apiMiddleware(handleGetCleverTapCredentials))
	http.HandleFunc("GET /api/v1/clevertap/logs", apiMiddleware(handleListCleverTapLogs))

	// Integration Logs and Retry APIs
	http.HandleFunc("GET /api/v1/integration/logs", apiMiddleware(handleGetIntegrationLogs))
	http.HandleFunc("POST /api/v1/integration/retry", apiMiddleware(handleRetryIntegrationEvent))

	// Tenant Provisioning and SaaS Control APIs
	http.HandleFunc("POST /api/v1/admin/tenant/provision", apiMiddleware(handleProvisionTenant))
	http.HandleFunc("POST /api/v1/admin/tenant/feature-flag", apiMiddleware(handleSetFeatureFlag))

	// Module Registry / Per-Tenant Module Entitlements (Stage 14.1)
	http.HandleFunc("GET /api/v1/admin/modules", apiMiddleware(handleListModules))
	http.HandleFunc("GET /api/v1/admin/tenant/module-entitlements", apiMiddleware(handleGetModuleEntitlements))
	http.HandleFunc("POST /api/v1/admin/tenant/module-entitlement", apiMiddleware(handleSetModuleEntitlement))

	// Per-Tenant Version Record (Stage 14.6)
	http.HandleFunc("GET /api/v1/admin/tenant/version", apiMiddleware(handleGetTenantVersion))

	// Patch/Bug-Intake Proposals (Stage 14.13-14.16) - a triage queue and
	// decision audit trail, not an auto-executor; see engines/patchintake.go.
	http.HandleFunc("GET /api/v1/admin/patch/proposals", apiMiddleware(handleListPatchProposals))
	http.HandleFunc("POST /api/v1/admin/patch/approve", apiMiddleware(handleApprovePatchProposal))
	http.HandleFunc("POST /api/v1/admin/patch/reject", apiMiddleware(handleRejectPatchProposal))

	// 3rd-Party Extension Isolation (Stage 14.17-14.20) - out-of-process
	// webhook hooks + scoped tokens; see engines/extensions.go.
	http.HandleFunc("POST /api/v1/admin/extension/hooks", apiMiddleware(handleCreateExtensionHook))
	http.HandleFunc("GET /api/v1/admin/extension/hooks", apiMiddleware(handleListExtensionHooks))
	http.HandleFunc("DELETE /api/v1/admin/extension/hooks/{id}", apiMiddleware(handleDeleteExtensionHook))
	http.HandleFunc("GET /api/v1/admin/extension/hooks/{id}/log", apiMiddleware(handleGetExtensionHookLog))
	http.HandleFunc("POST /api/v1/admin/extension/token", apiMiddleware(handleIssueExtensionToken))

	// DocType Metadata APIs
	http.HandleFunc("GET /api/v1/doc/{doctype}/meta", apiMiddleware(handleGetDocTypeMeta))
	http.HandleFunc("GET /api/v1/meta/doctypes", apiMiddleware(handleGetDocTypes))
	http.HandleFunc("POST /api/v1/meta/doctypes", apiMiddleware(handleSaveDocType))
	http.HandleFunc("POST /api/v1/meta/{doctype}/fields", apiMiddleware(handleSaveFieldDefinition))
	http.HandleFunc("DELETE /api/v1/meta/{doctype}/fields/{id}", apiMiddleware(handleDeleteFieldDefinition))

	// Core Foundation APIs
	http.HandleFunc("/api/v1/labels", apiMiddleware(handleLabels))
	http.HandleFunc("/api/v1/sequence", apiMiddleware(handleSequence))
	http.HandleFunc("/api/v1/prefix", apiMiddleware(handlePrefix))
	http.HandleFunc("/api/v1/logs/audit", apiMiddleware(handleAuditLogs))

	// Industry Configuration & Preset Profiler
	http.HandleFunc("GET /api/v1/admin/industries", apiMiddleware(handleGetIndustries))
	http.HandleFunc("POST /api/v1/admin/industry", apiMiddleware(handleSwitchIndustry))

	// Bulk CSV Import
	http.HandleFunc("POST /api/v1/import/{doctype}", apiMiddleware(handleBulkImport))
	http.HandleFunc("GET /api/v1/import/{doctype}/template", apiMiddleware(handleGetImportTemplate))
	http.HandleFunc("/api/v1/logs/system", apiMiddleware(handleSystemLogs))
	http.HandleFunc("/api/v1/debug/panic", apiMiddleware(handleDebugPanic))

	// Serve Static Files
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	// Stage 14.9: PORT is what lets dev/test/live (and any throwaway
	// verification instance) run the exact same binary side by side on one
	// machine. Defaults to 8080 so every existing deployment/doc/script that
	// assumes the old hardcoded port keeps working unchanged.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting ERP Server on http://localhost:%s\n", port)
	if err := http.ListenAndServe(":"+port, securityHeaders(http.DefaultServeMux)); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// REST HANDLERS

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var u struct {
		ID               string
		Username         string
		PasswordHash     string
		Role             string
		FailedLoginCount int
		IsLocked         bool
	}

	// Query user details. is_locked is computed in SQL (locked_until > NOW())
	// rather than scanned and compared in Go - a real bug caught live while
	// verifying this: lib/pq returns a tz-naive `timestamp` column's value
	// tagged as UTC, but this app server's local clock is IST (UTC+5:30), so
	// comparing time.Now() (local) against the scanned value directly made a
	// genuinely-expired lock look like it was still ~5.5 hours in the
	// future. Computing the comparison in Postgres, against Postgres's own
	// NOW(), sidesteps any app-server-vs-database clock/timezone
	// reconciliation entirely rather than trying to get it right in Go.
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, username, password_hash, role, failed_login_count, (locked_until IS NOT NULL AND locked_until > NOW())
		FROM %s.users
		WHERE username = $1 AND status = 'Active'`, schema), req.Username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.FailedLoginCount, &u.IsLocked)
	if err != nil {
		// Generic security error message
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Account-level brute-force lockout (Stage 14.21-14.24), independent of
	// and in addition to the existing IP-scoped rate limiter (Stage 13.14) -
	// that one alone doesn't slow down an attempt distributed across many
	// IPs against a single account. Deliberately the same generic error
	// message as every other login failure here - a distinguishable "your
	// account is locked" response would let an attacker confirm the
	// username is valid, the exact leak this endpoint's error messages
	// have consistently avoided elsewhere (e.g. a deactivated Employee).
	if u.IsLocked {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Check password with bcrypt (supports fallback check for local seed configs)
	err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password))
	if err != nil && u.PasswordHash != req.Password {
		newCount := u.FailedLoginCount + 1
		if newCount >= accountLockoutThreshold {
			// NOW() + make_interval(...) is also computed in Postgres for the
			// same reason as the is_locked check above - the lockout window's
			// end time must be reckoned against the same clock it's later
			// compared to.
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = $1, locked_until = NOW() + make_interval(mins => $2) WHERE id = $3`, schema),
				newCount, int(accountLockoutDuration.Minutes()), u.ID)
		} else {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = $1 WHERE id = $2`, schema), newCount, u.ID)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid username or password"})
		return
	}

	// Correct password: clear any accumulated failure count/lock.
	if u.FailedLoginCount > 0 {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET failed_login_count = 0, locked_until = NULL WHERE id = $1`, schema), u.ID)
	}

	// MFA-mandatory roles (SEC-V2 Sec.12) never get a full session token
	// straight out of /login - they're routed into enrollment (first time)
	// or a TOTP challenge (subsequently) instead.
	if engines.RequiresMFA(u.Role) {
		enabled, _, mfaErr := engines.GetUserMFAStatus(tenantID, u.ID)
		if mfaErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to resolve MFA status"})
			return
		}
		if !enabled {
			enrollToken := engines.SignPurposeToken(u.ID, u.Username, tenantID, "mfa_enroll", 10*time.Minute)
			engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "MFA_ENROLLMENT_REQUIRED", "Password correct; TOTP enrollment required before a session can be issued")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"mfa_enrollment_required": true,
				"enrollment_token":        enrollToken,
			})
			return
		}
		challengeToken := engines.SignPurposeToken(u.ID, u.Username, tenantID, "mfa_challenge", 5*time.Minute)
		engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "MFA_CHALLENGE_ISSUED", "Password correct; awaiting TOTP code")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"mfa_required":    true,
			"challenge_token": challengeToken,
		})
		return
	}

	// Hardcoded default location for simplicity, can be mapped in DB users table later
	locationCode := "HO"
	token := engines.SignToken(u.ID, u.Username, u.Role, tenantID, locationCode)

	engines.LogAuditEvent(tenantID, u.Username, "LOGIN", "SUCCESS", fmt.Sprintf("User logged in successfully with role %s", u.Role))

	_ = json.NewEncoder(w).Encode(map[string]string{
		"token": token,
		"role":  u.Role,
		"user":  u.Username,
	})
}

// handleMFAEnroll issues a fresh (pending, not-yet-active) TOTP secret for
// the account named in a mfa_enroll purpose token. Safe to call more than
// once before activation - each call simply replaces the pending secret.
func handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_enroll" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This endpoint requires a pending MFA enrollment token from /api/v1/login"})
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	username := r.Header.Get("Resolved-Username")

	secret, err := engines.GenerateTOTPSecret()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to generate MFA secret"})
		return
	}
	if err := engines.SetPendingMFASecret(tenantID, userID, secret); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to store MFA secret"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret":      secret,
		"otpauth_url": engines.BuildOTPAuthURL(secret, username, "CustomERP"),
	})
}

// handleMFAActivate confirms a pending TOTP secret by verifying a code
// against it, activates MFA for the account, and - since this is also the
// completion of the original login attempt - issues the real session token.
func handleMFAActivate(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_enroll" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This endpoint requires a pending MFA enrollment token from /api/v1/login"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	_, secret, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || secret == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No pending MFA enrollment found - call /api/v1/auth/mfa/enroll first"})
		return
	}
	if !engines.VerifyTOTPCode(secret, req.Code) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid MFA code"})
		return
	}
	if err := engines.ActivateMFA(tenantID, userID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to activate MFA"})
		return
	}

	role, username, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "MFA activated but failed to issue session"})
		return
	}
	token := engines.SignToken(userID, username, role, tenantID, "HO")
	engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_ENROLLED_AND_VERIFIED", "TOTP enrollment completed and verified")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "role": role, "user": username})
}

// handleMFAVerify completes login for an already-enrolled MFA account by
// checking a TOTP code against the stored active secret.
func handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Resolved-Purpose") != "mfa_challenge" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This endpoint requires an MFA challenge token from /api/v1/login"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")

	enabled, secret, err := engines.GetUserMFAStatus(tenantID, userID)
	if err != nil || !enabled || secret == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "MFA is not enrolled for this account"})
		return
	}
	if !engines.VerifyTOTPCode(secret, req.Code) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid MFA code"})
		return
	}

	role, username, err := engines.LookupUserRoleAndUsername(tenantID, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "MFA verified but failed to issue session"})
		return
	}
	token := engines.SignToken(userID, username, role, tenantID, "HO")
	engines.LogAuditEvent(tenantID, username, "LOGIN", "MFA_VERIFIED", "TOTP code verified, session issued")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token, "role": role, "user": username})
}

// Generic CRUD handler wrapping security RBAC authorization and validation rules
func handleGenericDoc(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")

	// Resolve parameters using Go 1.22 enhanced routing Value methods
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Extension token handling (Stage 14.17-14.20): a token issued by
	// SignExtensionToken has no role (it's not a user session) - it carries
	// Resolved-Scope-Doctype instead, and is authorized here explicitly
	// rather than falling through to checkPermission below (which would
	// just deny it, correctly but with a generic and less useful error).
	// Read-only, and only for the exact doctype it was scoped to - a hired
	// 3rd-party developer's extension can look up what it needs to react to
	// a hook, never write, never see another doctype or tenant.
	if r.Header.Get("Resolved-Purpose") == "extension" {
		scopeDoctype := r.Header.Get("Resolved-Scope-Doctype")
		if r.Method != http.MethodGet || doctype != scopeDoctype {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("This token is scoped to read-only access on '%s'", scopeDoctype)})
			return
		}
		// Falls through to the normal GET handling below with an empty
		// role - the location filter's "role != HR/Admin" branch still
		// applies (an extension token is never location-exempt), and no
		// module-gate/RBAC bypass beyond the doctype-scope check above.
	} else {
		// 1. RBAC permissions verification (skipped for a scoped extension
		// token, which was already authorized above on narrower terms).
		action := ""
		switch r.Method {
		case http.MethodGet:
			action = "read"
		case http.MethodPost:
			action = "create"
			if id != "" {
				action = "update"
			}
		case http.MethodDelete:
			action = "delete"
		}
		allowed, permErr := checkPermission(tenantID, role, doctype, action)
		if permErr != nil {
			http.Error(w, permErr.Error(), http.StatusInternalServerError)
			return
		}
		if !allowed {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("You do not have permission to %s %s documents.", action, doctype)})
			return
		}
	}

	// 1b. Module-wise access control (Stage 14.1). {doctype} is a runtime
	// path param, so unlike the fixed module routes (moduleGate wraps those
	// at registration time) this has to resolve module_key per-request here.
	// A doctype with no module_key assigned (moduleKey == "") is treated as
	// ungated/core - matches this migration's additive, fail-open-for-
	// unmapped-doctypes design (existing doctypes keep working exactly as
	// before until explicitly mapped).
	if moduleKey, mErr := engines.ModuleForDoctype(tenantID, doctype); mErr == nil && moduleKey != "" {
		if enabled, _ := engines.IsModuleEnabled(tenantID, moduleKey); !enabled {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Module '%s' is disabled for this tenant", moduleKey)})
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		if id != "" {
			// Retrieve single document
			var dataStr string
			var status string
			err = db.DB.QueryRow(fmt.Sprintf(`
				SELECT data, status FROM %s.documents 
				WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&dataStr, &status)
			if err == sql.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Document not found"})
				return
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			var dataMap map[string]interface{}
			_ = json.Unmarshal([]byte(dataStr), &dataMap)
			dataMap["id"] = id
			dataMap["status"] = status
			if dataMap, err = engines.FilterFieldsForRole(tenantID, role, doctype, dataMap); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Location Filter Validation (Object-Level Auth). Not every doctype
			// names this field "location" - FulfillmentTask uses "location_code" -
			// so check both rather than silently skipping the check (and letting
			// through a doc from another location) whenever a doctype uses the
			// other name.
			docLoc, hasLoc := dataMap["location"]
			if !hasLoc {
				docLoc, hasLoc = dataMap["location_code"]
			}
			if hasLoc && fmt.Sprintf("%v", docLoc) != location && role != "HR/Admin" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "This document does not belong to your assigned location."})
				return
			}

			_ = json.NewEncoder(w).Encode(dataMap)
		} else {
			// Retrieve multiple documents (support search, location filtering, and custom query filters)
			searchQuery := r.URL.Query().Get("q")
			query := fmt.Sprintf("SELECT id, data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL", schema)
			var args []interface{}
			args = append(args, doctype)
			argIndex := 2

			// Location filtering: non-admins can only see records for their location.
			// COALESCE covers both field names in use across doctypes ("location"
			// vs FulfillmentTask's "location_code") - matches the single-doc GET
			// check above, which does the same for the same reason. The "IS NULL"
			// half matters just as much as the match itself: plenty of doctypes
			// (MarketplaceSettlement, LogisticsBooking) have no location concept
			// at all, and SQL's NULL = $x is never true - without this, every
			// non-admin would silently see zero rows of any location-less
			// doctype, not "all of them" (which is the correct behavior for a
			// doctype with nothing to scope by).
			if role != "HR/Admin" {
				query += fmt.Sprintf(" AND (COALESCE(data->>'location', data->>'location_code') = $%d OR COALESCE(data->>'location', data->>'location_code') IS NULL)", argIndex)
				args = append(args, location)
				argIndex++
			}

			// Dynamic search parameter filters check (WMS/OMS query filters)
			for key, vals := range r.URL.Query() {
				if key == "q" || key == "tenant_id" || key == "limit" || key == "offset" || len(vals) == 0 {
					continue
				}
				if !safeFilterKeyRe.MatchString(key) {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Invalid filter parameter name: %q", key)})
					return
				}
				query += fmt.Sprintf(" AND data->>'%s' = $%d", key, argIndex)
				args = append(args, vals[0])
				argIndex++
			}

			// Pagination: bounds the response even when the caller doesn't ask for a
			// specific page, so this endpoint can never return an unbounded result set.
			// Note: when a search term (q) is active, the limit/offset bound the SQL-level
			// candidate set that gets fetched *before* the in-memory search filter below -
			// a search could miss a match sitting past the current page's window. Moving
			// search into SQL would remove that edge case but is a larger change than this
			// item calls for.
			limit := defaultListLimit
			if v := r.URL.Query().Get("limit"); v != "" {
				if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			if limit > maxListLimit {
				limit = maxListLimit
			}
			offset := 0
			if v := r.URL.Query().Get("offset"); v != "" {
				if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
					offset = parsed
				}
			}
			query += fmt.Sprintf(" ORDER BY id LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
			args = append(args, limit, offset)

			rows, err := db.DB.Query(query, args...)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			docs := []map[string]interface{}{}
			for rows.Next() {
				var docID string
				var dataStr string
				var status string
				if err := rows.Scan(&docID, &dataStr, &status); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				var dataMap map[string]interface{}
				_ = json.Unmarshal([]byte(dataStr), &dataMap)
				dataMap["id"] = docID
				dataMap["status"] = status
				if dataMap, err = engines.FilterFieldsForRole(tenantID, role, doctype, dataMap); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				// Local search match
				if searchQuery != "" {
					matched := false
					for _, val := range dataMap {
						if strings.Contains(strings.ToLower(fmt.Sprintf("%v", val)), strings.ToLower(searchQuery)) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}

				docs = append(docs, dataMap)
			}
			_ = json.NewEncoder(w).Encode(docs)
		}

	case http.MethodPost:
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid payload JSON", http.StatusBadRequest)
			return
		}
		if err := engines.RejectRestrictedFieldWrites(tenantID, role, doctype, payload); err != nil {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// 2. Server-side metadata validation engine check
		err = engines.ValidateDocument(tenantID, doctype, payload)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Expense claim controls (Stage 13.13c, MB 16.2): date window and
		// duplicate-bill check, only on creation of a new claim - not on
		// later edits to an existing one.
		if doctype == "ExpenseClaim" && id == "" {
			if err := engines.ValidateExpenseClaimControls(tenantID, payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}

		// PIM variant uniqueness (Stage 15): unlike the ExpenseClaim check
		// above, this runs on create AND update - an edit can introduce a
		// duplicate variant combination just as easily as a create can.
		// effectiveID mirrors the docID resolution a few lines below (path
		// id on update; client-supplied payload["id"] on a create that sets
		// one explicitly, e.g. "id: code" - this codebase's own
		// convention; blank otherwise, which is fine since a fresh
		// server-generated UUID can never collide with a stored sibling).
		if doctype == "Item" {
			effectiveID := id
			if effectiveID == "" {
				if payloadID, exists := payload["id"]; exists && payloadID != nil {
					effectiveID = fmt.Sprintf("%v", payloadID)
				}
			}
			if err := engines.ValidateItemVariantUniqueness(tenantID, effectiveID, payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}

		// Setup Document ID and attributes
		docID := ""
		if id != "" {
			docID = id
		} else if payloadID, exists := payload["id"]; exists && payloadID != nil {
			docID = fmt.Sprintf("%v", payloadID)
		} else {
			docID = generateUUID()
		}

		// Re-approval-on-edit (Stage 13.8): capture the status this document
		// had *before* this write, so an edit to an already-Approved
		// approval-gated document can be forced back into the approval
		// queue after the upsert below, regardless of what status the
		// incoming payload itself claims.
		wasApproved := false
		if docID != "" {
			var priorStatus string
			if errPrior := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), doctype, docID).Scan(&priorStatus); errPrior == nil {
				wasApproved = priorStatus == "Approved"
			}
		}

		payloadBytes, _ := json.Marshal(payload)
		statusVal := "Active"
		if s, exists := payload["status"]; exists && s != nil {
			statusVal = fmt.Sprintf("%v", s)
		}

		// Extension before_save hooks (Stage 14.17-14.20): synchronous, and
		// a failure blocks the save outright - a 3rd-party pricing/
		// validation hook that doesn't run must not let this proceed with
		// an unreviewed value. No-op (zero network calls) when no hook is
		// registered for this doctype, which is the overwhelmingly common
		// case for every tenant that hasn't set one up.
		if errHook := engines.InvokeBeforeSaveHooks(tenantID, doctype, docID, payload); errHook != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": errHook.Error()})
			return
		}

		// Perform Upsert using parameterized parameters (SQL Injection Safe)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				data = EXCLUDED.data,
				status = EXCLUDED.status,
				updated_at = CURRENT_TIMESTAMP`, schema)
		_, err = db.DB.Exec(query, docID, doctype, payloadBytes, statusVal, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Extension after_save hooks (Stage 14.17-14.20): fired async - the
		// save already committed, so a notification/sync hook's failure
		// can't roll it back and shouldn't slow down the response.
		engines.InvokeAfterSaveHooksAsync(tenantID, doctype, docID, payload)

		if wasApproved {
			if gated, errGate := engines.IsApprovalGated(tenantID, doctype); errGate == nil && gated {
				if errReset := engines.ResetToPendingOnEdit(tenantID, doctype, docID, userID, role, payload); errReset != nil {
					engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "APPROVAL_RESET_FAILED", r.URL.Path, errReset.Error(), "")
				}
			}
		}

		// PIM Product Profile auto-create (Stage 15.2, V2 §6.1 step 2):
		// "PIM profile is auto-created with status PIM Draft." Create-only
		// (id == "" means this request hit the create route, not update) -
		// EnsurePIMProductProfile itself is also idempotent (ON CONFLICT DO
		// NOTHING), so this is belt-and-suspenders, not load-bearing.
		if doctype == "Item" && id == "" {
			if errProfile := engines.EnsurePIMProductProfile(tenantID, docID); errProfile != nil {
				engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "PIM_PROFILE_CREATE_FAILED", r.URL.Path, errProfile.Error(), "")
			}
		}

		// HR Access Link Hook (Stage 13.13a, MB 16.3): an Employee's
		// active/inactive status controls their linked ERP user's ability
		// to log in.
		if doctype == "Employee" {
			empUserID, _ := payload["user_id"].(string)
			empStatus, _ := payload["status"].(string)
			if errSync := engines.SyncEmployeeAccessLink(tenantID, empUserID, empStatus); errSync != nil {
				engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "ACCESS_LINK_SYNC_FAILED", r.URL.Path, errSync.Error(), "")
			}
		}

		// GRN Callback Hook: Automatically post received items to inventory ledger
		if doctype == "GRN" {
			locationCode, _ := payload["location"].(string)
			// GRN's own registered schema (db/migrations_phase3.sql) declares the mandatory
			// field as "received_items", a JSON-encoded string (same convention as BOM's
			// "components" field, engines/manufacturing.go fetchBOM) - not a raw "items"
			// array key, which was never part of GRN's declared schema and left this stock
			// posting silently unreachable for any caller filling in the actual mandatory field.
			var items []interface{}
			if receivedItemsStr, ok := payload["received_items"].(string); ok && receivedItemsStr != "" {
				_ = json.Unmarshal([]byte(receivedItemsStr), &items)
			}
			if locationCode != "" && len(items) > 0 {
				errLedger := engines.PostInventoryLedger(tenantID, locationCode, items)
				if errLedger != nil {
					log.Printf("Error posting GRN items to stock ledger: %v", errLedger)
				}
			}

			// Publish inventory transaction changed outbox event
			tx, errTx := db.DB.Begin()
			if errTx == nil {
				_ = db.SetSearchPath(tx, schema)
				_ = engines.PublishEvent(tx, schema, "inventory.stock_changed", map[string]interface{}{
					"grn_id":   docID,
					"location": locationCode,
				})
				_ = tx.Commit()
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "saved",
			"id":     docID,
		})

	case http.MethodDelete:
		if id == "" {
			http.Error(w, "Document ID is required", http.StatusBadRequest)
			return
		}

		var status, documentType string
		err = db.DB.QueryRow(fmt.Sprintf(`SELECT d.status, m.document_type FROM %s.documents d JOIN %s.doctype_meta m ON m.name = d.doctype WHERE d.id = $1 AND d.doctype = $2 AND d.deleted_at IS NULL`, schema, schema), id, doctype).Scan(&status, &documentType)
		if err == sql.ErrNoRows {
			http.Error(w, "Document not found or already deleted", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if documentType == "Transaction" && status == "Approved" {
			http.Error(w, "Approved transactional documents cannot be deleted", http.StatusBadRequest)
			return
		}
		_, err = db.DB.Exec(fmt.Sprintf("UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL", schema), id, doctype)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		engines.LogAuditEvent(tenantID, userID, "SOFT_DELETE_"+doctype, "SUCCESS", "Document ID: "+id)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func checkPermission(tenantID string, role string, doctype string, action string) (bool, error) {
	if role == "HR/Admin" {
		return true, nil
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false, err
	}

	column := ""
	switch action {
	case "read":
		column = "allow_read"
	case "create":
		column = "allow_create"
	case "update":
		column = "allow_update"
	case "delete":
		column = "allow_delete"
	default:
		return false, fmt.Errorf("invalid permission action: %s", action)
	}

	var allowed bool
	query := fmt.Sprintf("SELECT COALESCE(%s, false) FROM %s.role_permissions WHERE role = $1 AND doctype_name = $2", column, schema)
	err = db.DB.QueryRow(query, role, doctype).Scan(&allowed)
	if err == sql.ErrNoRows {
		// Default: deny if no mapping rule exists
		return false, nil
	}
	return allowed, err
}

func handleLabels(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	switch r.Method {
	case http.MethodGet:
		labels, err := engines.GetLabels(tenantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(labels)

	case http.MethodPost:
		var req struct {
			OriginalText string `json:"original_text"`
			CustomText   string `json:"custom_text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if req.OriginalText == "" || req.CustomText == "" {
			http.Error(w, "Fields original_text and custom_text are required", http.StatusBadRequest)
			return
		}

		err := engines.SaveLabel(tenantID, req.OriginalText, req.CustomText)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	case http.MethodDelete:
		orig := r.URL.Query().Get("original_text")
		if orig == "" {
			http.Error(w, "Query parameter original_text is required", http.StatusBadRequest)
			return
		}

		err := engines.DeleteLabel(tenantID, orig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleSequence(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DocType       string `json:"doc_type"`
		StoreCode     string `json:"store_code"`
		FinancialYear string `json:"financial_year"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if req.DocType == "" || req.FinancialYear == "" {
		http.Error(w, "doc_type and financial_year are required", http.StatusBadRequest)
		return
	}

	code, err := engines.GenerateSequence(tenantID, req.DocType, req.StoreCode, req.FinancialYear)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	engines.LogAuditEvent(tenantID, "system", "GENERATE_SEQUENCE", "SUCCESS", fmt.Sprintf("Generated %s sequence code: %s", req.DocType, code))

	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func handlePrefix(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, doc_type, prefix, separator, padding_width, reset_frequency, active_status 
			FROM %s.prefix_configs ORDER BY doc_type`, schema))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type PrefixConfig struct {
			ID             string `json:"id"`
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
		}

		configs := []PrefixConfig{}
		for rows.Next() {
			var c PrefixConfig
			err := rows.Scan(&c.ID, &c.DocType, &c.Prefix, &c.Separator, &c.PaddingWidth, &c.ResetFrequency, &c.ActiveStatus)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			configs = append(configs, c)
		}
		_ = json.NewEncoder(w).Encode(configs)

	case http.MethodPost:
		var req struct {
			DocType        string `json:"doc_type"`
			Prefix         string `json:"prefix"`
			Separator      string `json:"separator"`
			PaddingWidth   int    `json:"padding_width"`
			ResetFrequency string `json:"reset_frequency"`
			ActiveStatus   bool   `json:"active_status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		query := fmt.Sprintf(`
			INSERT INTO %s.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status) 
			VALUES ($1, $2, $3, $4, $5, $6) 
			ON CONFLICT (doc_type) DO UPDATE SET 
				prefix = EXCLUDED.prefix, 
				separator = EXCLUDED.separator, 
				padding_width = EXCLUDED.padding_width, 
				reset_frequency = EXCLUDED.reset_frequency, 
				active_status = EXCLUDED.active_status`, schema)
		_, err = db.DB.Exec(query, req.DocType, req.Prefix, req.Separator, req.PaddingWidth, req.ResetFrequency, req.ActiveStatus)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		engines.LogAuditEvent(tenantID, "admin", "UPDATE_PREFIX_CONFIG", "SUCCESS", fmt.Sprintf("Updated prefix config for doc_type: %s", req.DocType))
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT id, user_id, action, status, details, created_at FROM %s.audit_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type AuditLog struct {
		ID        string `json:"id"`
		UserID    string `json:"user_id"`
		Action    string `json:"action"`
		Status    string `json:"status"`
		Details   string `json:"details"`
		CreatedAt string `json:"created_at"`
	}

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Action, &l.Status, &l.Details, &l.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logs = append(logs, l)
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.DB.Query(fmt.Sprintf("SELECT log_id, correlation_id, severity, module_source, error_message, stack_trace, created_at FROM %s.system_error_logs ORDER BY created_at DESC LIMIT 100", schema))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type SystemLog struct {
		LogID         string         `json:"log_id"`
		CorrelationID sql.NullString `json:"correlation_id"`
		Severity      string         `json:"severity"`
		ModuleSource  string         `json:"module_source"`
		ErrorMessage  string         `json:"error_message"`
		StackTrace    string         `json:"stack_trace"`
		CreatedAt     string         `json:"created_at"`
	}

	logs := []SystemLog{}
	for rows.Next() {
		var l SystemLog
		if err := rows.Scan(&l.LogID, &l.CorrelationID, &l.Severity, &l.ModuleSource, &l.ErrorMessage, &l.StackTrace, &l.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logs = append(logs, l)
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleDebugPanic(w http.ResponseWriter, r *http.Request) {
	panic("Deliberate testing panic: Dynamic recovery log engine operational!")
}

// handleReactivateMasterDocument is the only way to clear a soft-delete
// tombstone. Transactions remain immutable once deleted; masters can be
// restored by someone with their normal update permission.
func handleReactivateMasterDocument(w http.ResponseWriter, r *http.Request) {
	tenantID, role := r.Header.Get("Resolved-Tenant-ID"), r.Header.Get("Resolved-Role")
	doctype, id := r.PathValue("doctype"), r.PathValue("id")
	allowed, err := checkPermission(tenantID, role, doctype, "update")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "You do not have permission to reactivate this document."})
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var documentType string
	if err := db.DB.QueryRow(fmt.Sprintf("SELECT document_type FROM %s.doctype_meta WHERE name = $1", schema), doctype).Scan(&documentType); err != nil {
		http.Error(w, "Unknown document type", http.StatusNotFound)
		return
	}
	if documentType != "Master" {
		http.Error(w, "Only master documents can be reactivated", http.StatusBadRequest)
		return
	}
	result, err := db.DB.Exec(fmt.Sprintf("UPDATE %s.documents SET deleted_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2 AND deleted_at IS NOT NULL", schema), id, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.Error(w, "Deleted document not found", http.StatusNotFound)
		return
	}
	engines.LogAuditEvent(tenantID, r.Header.Get("Resolved-User-ID"), "REACTIVATE_"+doctype, "SUCCESS", "Document ID: "+id)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reactivated"})
}

func handleGetDocTypeMeta(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	doctype := r.PathValue("doctype")

	fields, err := engines.GetDocTypeMeta(tenantID, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fields, err = engines.FilterFieldMetaForRole(tenantID, role, doctype, fields)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(fields)
}

func handleGetDocTypes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	list, err := engines.GetDocTypes(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSaveDocType(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		Name         string `json:"name"`
		Module       string `json:"module"`
		DocumentType string `json:"document_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	err := engines.SaveDocType(tenantID, req.Name, req.Module, req.DocumentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleSaveFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	var req engines.FieldMeta
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	err := engines.SaveFieldDefinition(tenantID, doctype, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func handleDeleteFieldDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	id := r.PathValue("id")

	err := engines.DeleteFieldDefinition(tenantID, doctype, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func handleGetIndustries(w http.ResponseWriter, r *http.Request) {
	list := []map[string]string{
		{"code": "JEWELRY", "name": "Jewelry Industry"},
		{"code": "FOOD_BEV", "name": "Food and Beverage Industry"},
		{"code": "AUTO", "name": "Automobile Industry"},
		{"code": "CLOTHING", "name": "Clothing & Apparel Industry"},
	}
	_ = json.NewEncoder(w).Encode(list)
}

func handleSwitchIndustry(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")

	var req struct {
		IndustryCode string `json:"industry_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	profilePath := fmt.Sprintf("./public/profiles/%s.json", strings.ToLower(req.IndustryCode))
	err := engines.SwitchIndustryProfile(tenantID, profilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to switch industry: %v", err), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Industry configuration profile reloaded successfully"})
}

func handleBulkImport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	// Bug fix (found while verifying Stage 15.2's import preview, which
	// copies this handler's shape): this read "Resolved-Role" (e.g.
	// "HR/Admin") into a variable literally named userID, which then got
	// written as documents.created_by - a column with a hard FK to
	// users(id). A role string is never a valid user id, so every bulk
	// import row insert has always failed its FK constraint. Fixed to the
	// actual user id header, matching every other handler in this file
	// (e.g. handleCapitalizeAsset, handlePIMPublish).
	userID := r.Header.Get("Resolved-User-ID")

	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "Multipart payload exceeds limit", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "CSV file is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jobID, errJob := engines.RecordImportJob(tenantID, doctype, res, userID)
	if errJob != nil {
		engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "IMPORT_JOB_RECORD_FAILED", r.URL.Path, errJob.Error(), "")
	}

	responseBytes, _ := json.Marshal(res)
	var responseMap map[string]interface{}
	_ = json.Unmarshal(responseBytes, &responseMap)
	if jobID != "" {
		responseMap["import_job_id"] = jobID
	}
	_ = json.NewEncoder(w).Encode(responseMap)
}

// handlePIMImportPreview (Stage 15.2, V2 §6.2/§16 Phase 3): the same CSV
// parse/validate/existence-check logic as handleBulkImport, run with
// dryRun=true - nothing is written, giving the create/update/reject preview
// V2's Import Job screen wants before a user commits a bulk file.
func handlePIMImportPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")
	userID := r.Header.Get("Resolved-User-ID")

	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "Multipart payload exceeds limit", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "CSV file is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	res, err := engines.BulkImportCSV(tenantID, doctype, file, userID, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

// handlePIMImportJobErrors serves a completed ImportJob's row-level failures
// as a downloadable CSV, same Content-Disposition pattern as the CSV import
// template endpoint above.
func handlePIMImportJobErrors(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	jobID := r.PathValue("id")

	csvBytes, err := engines.GetImportJobErrorCSV(tenantID, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=import_errors_%s.csv", jobID))
	_, _ = w.Write(csvBytes)
}

// handleSaveChannelCredential (Stage 16.1) stores a channel's API
// credential fields (access token, shop domain, etc. - shape varies by
// platform) encrypted at rest via engines.SaveChannelCredential. Write-
// only by design: this handler never reads a credential back, and there
// is no corresponding GET route anywhere in this file.
func handleSaveChannelCredential(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure channel credentials"})
		return
	}
	channelCode := r.PathValue("code")
	var fields map[string]string
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := engines.SaveChannelCredential(tenantID, channelCode, fields); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "channel": channelCode})
}

// handleBigCommerceWebhook (Stage 16.3) verifies and acknowledges an
// inbound BigCommerce webhook (product/inventory/order events). The
// channel code in the URL identifies which stored credential's
// webhook_secret field to verify against - BigCommerce webhook payloads
// do not self-identify which of possibly several configured channels
// they belong to. Scope note, stated explicitly: this acknowledges and
// logs a verified webhook rather than driving a full order-import
// pipeline the way the existing Shopify order webhook does - BigCommerce
// order sync-back is not yet built, only inbound signature verification
// (Part A.7 of the Stage 16 plan) plus a place for that logic to grow
// into.
func handleBigCommerceWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	channelCode := r.PathValue("channelCode")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	cred, credErr := engines.GetChannelWebhookSecret(tenantID, channelCode)
	if credErr != nil || cred == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "channel has no webhook secret configured"})
		return
	}
	sig := r.Header.Get("X-Bc-Webhook-Signature")
	if !engines.VerifyBigCommerceWebhook(body, sig, cred) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid webhook signature"})
		return
	}

	engines.LogAuditEvent(tenantID, "system", "BIGCOMMERCE_WEBHOOK_RECEIVED", "SUCCESS", fmt.Sprintf("channel=%s bytes=%d", channelCode, len(body)))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
}

func handleGetImportTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	doctype := r.PathValue("doctype")

	templateBytes, err := engines.GenerateCSVTemplate(tenantID, doctype)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s_template.csv", doctype))
	_, _ = w.Write(templateBytes)
}

func handleGetAvailability(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sku := r.URL.Query().Get("sku")
	location := r.URL.Query().Get("location")

	if sku == "" || location == "" {
		http.Error(w, "Query parameters 'sku' and 'location' are required", http.StatusBadRequest)
		return
	}

	res, err := engines.GetAvailableToSell(tenantID, sku, location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Sku          string `json:"sku"`
		Location     string `json:"location"`
		Qty          int    `json:"qty"`
		ResType      string `json:"res_type"`
		ExpirySecond int    `json:"expiry"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Sku == "" || req.Location == "" || req.Qty <= 0 {
		http.Error(w, "Fields 'sku', 'location', and positive 'qty' are required", http.StatusBadRequest)
		return
	}

	expiry := req.ExpirySecond
	if expiry <= 0 {
		expiry = 300 // default 5 minutes
	}

	resID, err := engines.CreateReservation(tenantID, req.Sku, req.Location, req.Qty, req.ResType, expiry)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":         "reserved",
		"reservation_id": resID,
	})
}

func handleCheckout(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CartNumber  string `json:"cart_number"`
		Location    string `json:"location"`
		PaymentMode string `json:"payment_mode"`
		CustomerID  string `json:"customer_id"`
		Items       []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid checkout payload", http.StatusBadRequest)
		return
	}

	if req.CartNumber == "" || req.Location == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'cart_number', 'location', and 'items' are required", http.StatusBadRequest)
		return
	}

	// Reject non-positive qty/prices before any side effect runs. Below this line,
	// item.Qty is negated to decrement stock (see loop below) - an already-negative
	// qty would flip to positive and silently ADD stock instead of being rejected,
	// and would do so via PostInventoryLedger's own committed transaction, before
	// the later GL-posting step even runs its own (unrelated) sign validation.
	for _, item := range req.Items {
		if item.Sku == "" || item.Qty <= 0 {
			http.Error(w, fmt.Sprintf("Item quantity must be positive (sku=%q, qty=%d)", item.Sku, item.Qty), http.StatusBadRequest)
			return
		}
		if item.SalePrice < 0 || item.CostPrice < 0 {
			http.Error(w, fmt.Sprintf("Item prices cannot be negative (sku=%q)", item.Sku), http.StatusBadRequest)
			return
		}
	}

	totalSalePrice := 0
	totalCostPrice := 0
	for _, item := range req.Items {
		totalSalePrice += int(item.SalePrice) * item.Qty
		totalCostPrice += int(item.CostPrice) * item.Qty
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to resolve tenant schema"})
		return
	}

	// Idempotency guard: atomically claim this cart_number before any side effect
	// (inventory decrement, GL posting) runs. Without this, a duplicate submission
	// - a network retry, a double-click, or two requests racing - would each pass
	// through independently and double-deduct stock / double-post GL, while the
	// final document row (a plain upsert) silently overwrites to look like only
	// one sale happened. Only the request whose INSERT/claim actually applies
	// proceeds; a duplicate of an already-Paid cart gets the original result
	// replayed back, and a duplicate that arrives while the first is still being
	// processed is told to wait rather than reprocessing.
	payloadBytes, _ := json.Marshal(req)
	claimQuery := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSCart', $2, 'Processing', 'system')
		ON CONFLICT (id) DO UPDATE SET
			data = EXCLUDED.data, status = 'Processing', updated_at = CURRENT_TIMESTAMP
		WHERE %s.documents.status = 'Failed'
		RETURNING id`, schema, schema)
	var claimedID string
	claimErr := db.DB.QueryRow(claimQuery, req.CartNumber, payloadBytes).Scan(&claimedID)
	if claimErr == sql.ErrNoRows {
		var existingStatus, existingData string
		lookupErr := db.DB.QueryRow(fmt.Sprintf(
			`SELECT status, data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema),
			req.CartNumber).Scan(&existingStatus, &existingData)
		if lookupErr == nil && existingStatus == "Paid" {
			var existing struct {
				Items []struct {
					Qty       int     `json:"qty"`
					SalePrice float64 `json:"sale_price"`
					CostPrice float64 `json:"cost_price"`
				} `json:"items"`
			}
			replaySale, replayCost := 0, 0
			if json.Unmarshal([]byte(existingData), &existing) == nil {
				for _, it := range existing.Items {
					replaySale += int(it.SalePrice) * it.Qty
					replayCost += int(it.CostPrice) * it.Qty
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "completed",
				"cart_number": req.CartNumber,
				"sale_total":  replaySale,
				"cost_total":  replayCost,
			})
			return
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "This cart is already being processed or was already completed"})
		return
	} else if claimErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to claim checkout"})
		return
	}

	markFailed := func() {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Failed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), req.CartNumber)
	}

	// Convert items structure to interface slice for PostInventoryLedger (with negative qty!)
	itemsInterface := make([]interface{}, len(req.Items))
	for i, item := range req.Items {
		itemsInterface[i] = map[string]interface{}{
			"sku": item.Sku,
			"qty": -item.Qty, // Negative to decrement available stock
		}
	}

	// Decrement inventory availability
	if err := engines.PostInventoryLedger(tenantID, req.Location, itemsInterface); err != nil {
		markFailed()
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Inventory decrement failed: %v", err)})
		return
	}

	// Post balanced accounting bookings
	if err := engines.PostSalesFinanceBooking(tenantID, req.CartNumber, totalSalePrice, totalCostPrice); err != nil {
		markFailed()
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("GL Booking posting failed: %v", err)})
		return
	}

	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Paid', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'POSCart' AND id = $1`, schema), req.CartNumber)

	// Loyalty point earn (Stage 13.13d, scoped MVP): purely additive - a
	// failure here is logged but never fails an already-completed sale.
	// Deliberately not wired into inventory/GL above; this checkout flow's
	// idempotency/claim logic is load-bearing and this stays outside it.
	if req.CustomerID != "" {
		if errEarn := engines.EarnLoyaltyPoints(tenantID, req.CustomerID, totalSalePrice, req.CartNumber); errEarn != nil {
			engines.LogSystemError(tenantID, r.Header.Get("Resolved-Correlation-ID"), "LOYALTY_EARN_FAILED", r.URL.Path, errEarn.Error(), "")
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "completed",
		"cart_number": req.CartNumber,
		"sale_total":  totalSalePrice,
		"cost_total":  totalCostPrice,
	})
}

func handleTrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	res, err := engines.GetTrialBalance(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func handleAccountingPeriods(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")

	switch r.Method {
	case http.MethodGet:
		periods, err := engines.ListAccountingPeriods(tenantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(periods)

	case http.MethodPost:
		if role != "HR/Admin" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can create accounting periods"})
			return
		}
		var req struct {
			PeriodName string `json:"period_name"`
			StartDate  string `json:"start_date"`
			EndDate    string `json:"end_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PeriodName == "" || req.StartDate == "" || req.EndDate == "" {
			http.Error(w, "period_name, start_date, and end_date are required", http.StatusBadRequest)
			return
		}
		id, err := engines.CreateAccountingPeriod(tenantID, req.PeriodName, req.StartDate, req.EndDate, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "created"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleCloseAccountingPeriod(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can close accounting periods"})
		return
	}
	periodID := r.PathValue("id")
	if err := engines.CloseAccountingPeriod(tenantID, periodID, userID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "closed"})
}

// handleSubmitApproval moves a Draft document into the approval queue.
func handleSubmitApproval(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Doctype    string `json:"doctype"`
		DocumentID string `json:"document_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.DocumentID == "" {
		http.Error(w, "Fields 'doctype' and 'document_id' are required", http.StatusBadRequest)
		return
	}

	allowed, err := checkPermission(tenantID, role, req.Doctype, "update")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "You do not have permission to submit this document."})
		return
	}

	if err := engines.SubmitForApproval(tenantID, req.Doctype, req.DocumentID, userID, role); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "submitted"})
}

// handleDecideApproval approves or rejects a Pending Approval document.
func handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	location := r.Header.Get("Resolved-Location")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Doctype    string `json:"doctype"`
		DocumentID string `json:"document_id"`
		Decision   string `json:"decision"`
		Comment    string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Doctype == "" || req.DocumentID == "" || req.Decision == "" {
		http.Error(w, "Fields 'doctype', 'document_id', and 'decision' are required", http.StatusBadRequest)
		return
	}

	if err := engines.DecideApproval(tenantID, req.Doctype, req.DocumentID, userID, role, location, req.Decision, req.Comment); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "APPROVAL_DECISION", req.Decision, fmt.Sprintf("%s %s: %s", req.Doctype, req.DocumentID, req.Decision))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "decided", "decision": req.Decision})
}

// handleListPendingApprovals returns the caller's approval inbox.
func handleListPendingApprovals(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	location := r.Header.Get("Resolved-Location")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	results, err := engines.ListPendingApprovals(tenantID, role, location)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handleApprovalRules lists the amount-slab/role routing configuration.
// Read-only for now (edited directly via seed data / a future admin form,
// same as this project's other configuration tables started out).
func handleApprovalRules(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rules, err := engines.GetApprovalRules(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rules == nil {
		rules = []engines.ApprovalRule{}
	}
	_ = json.NewEncoder(w).Encode(rules)
}

// handleCalculateGST computes the CGST/SGST/IGST split for a taxable amount
// and rate (Stage 13.10). The rate itself comes from the caller (typically
// an Item's HSN-classified gst_rate field) - this endpoint is the
// calculation step, not an HSN-to-rate lookup service.
func handleCalculateGST(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TaxableAmount float64 `json:"taxable_amount"`
		GSTRate       float64 `json:"gst_rate"`
		Interstate    bool    `json:"interstate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	result, err := engines.CalculateGST(req.TaxableAmount, req.GSTRate, req.Interstate)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// Report catalog (Stage 13.11) - prioritized per the gap analysis's own
// list: Current Stock, Sales Register, Vendor Ledger, Payables Ageing.
func handleCurrentStockReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetCurrentStockReport(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleSalesRegisterReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetSalesRegisterReport(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.SalesRegisterEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleVendorLedgerReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	vendor := r.URL.Query().Get("vendor")
	results, err := engines.GetVendorLedgerReport(tenantID, vendor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handlePayablesAgeingReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetPayablesAgeingReport(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

// RFQ / Vendor Quote / Quote Comparison (Stage 13.12). RFQ/VendorQuote
// creation and listing go through the existing generic doc endpoint like
// Vendor/Customer did (Stage 13.9) - these two handlers cover only the
// comparison view and the winner-selection action, which need logic the
// generic endpoint doesn't have.
func handleGetVendorQuotesForRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rfqID := r.URL.Query().Get("rfq_id")
	if rfqID == "" {
		http.Error(w, "Query parameter 'rfq_id' is required", http.StatusBadRequest)
		return
	}
	results, err := engines.GetVendorQuotesForRFQ(tenantID, rfqID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleSelectWinningQuote(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RfqID   string `json:"rfq_id"`
		QuoteID string `json:"quote_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RfqID == "" || req.QuoteID == "" {
		http.Error(w, "Fields 'rfq_id' and 'quote_id' are required", http.StatusBadRequest)
		return
	}
	if err := engines.SelectWinningQuote(tenantID, req.RfqID, req.QuoteID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "selected"})
}

// Sticker / Barcode Printing (Stage 13.15). Printer master creation/listing
// go through the existing generic doc endpoint like Vendor/Customer/RFQ did;
// these two handlers cover the print action and history, which need logic
// the generic endpoint doesn't have.
func handlePrintStickers(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Skus          []string `json:"skus"`
		PrinterCode   string   `json:"printer_code"`
		ReprintReason string   `json:"reprint_reason"`
		Copies        int      `json:"copies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}
	if req.PrinterCode == "" {
		http.Error(w, "Field 'printer_code' is required", http.StatusBadRequest)
		return
	}
	labels, err := engines.PrintStickers(tenantID, req.Skus, req.PrinterCode, userID, req.ReprintReason, req.Copies)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRINT_STICKERS", "SUCCESS", fmt.Sprintf("Printed %d sticker(s) on %s", len(labels), req.PrinterCode))
	_ = json.NewEncoder(w).Encode(labels)
}

func handlePrintHistory(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetPrintHistory(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.PrintHistoryEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// handlePayrollExport implements MB 16.3's "Payroll Interface": exports
// approved attendance/leave data for an external payroll system to consume.
func handlePayrollExport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		http.Error(w, "Query parameters 'from' and 'to' are required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	results, err := engines.GetPayrollExport(tenantID, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.PayrollExportEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// PIM Foundation MVP (Stage 15). Family/Attribute/Content CRUD all use the
// same generic doc endpoint as Vendor/Customer/Employee; these two handlers
// cover the Product Workbench (blueprint section 7/18) and its per-item
// drill-down, which need the completeness-scoring logic the generic
// endpoint doesn't have.
func handlePIMDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	dashboard, err := engines.GetPIMDashboard(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(dashboard)
}

func handlePIMBulkEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Doctype string      `json:"doctype"`
		IDs     []string    `json:"ids"`
		Field   string      `json:"field"`
		Value   interface{} `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload JSON", http.StatusBadRequest)
		return
	}
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	userID := r.Header.Get("Resolved-User-ID")
	allowed, err := checkPermission(tenantID, role, req.Doctype, "update")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("You do not have permission to update %s documents.", req.Doctype)})
		return
	}
	updatedIDs, err := engines.BulkUpdateDocuments(tenantID, req.Doctype, req.IDs, req.Field, req.Value, userID, role)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "updated", "updated_count": len(updatedIDs), "ids": updatedIDs,
	})
}

func handlePIMReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rows, err := engines.ListPIMReport(r.Header.Get("Resolved-Tenant-ID"), r.PathValue("name"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}

func handlePIMWorkbench(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	family := r.URL.Query().Get("family")
	results, err := engines.ListWorkbench(tenantID, family)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.WorkbenchEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handlePIMCompleteness(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	itemCode := r.PathValue("itemCode")
	locale := r.URL.Query().Get("locale")
	channelID := r.URL.Query().Get("channel")
	result, err := engines.CalculateCompleteness(tenantID, itemCode, locale, channelID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// handlePIMMediaUpload (Stage 15.2). Media files are larger than the global
// 2MB JSON body cap set in apiMiddleware - re-wrapping r.Body with a bigger
// MaxBytesReader here, before ParseMultipartForm reads it, raises the limit
// for this route only; every other route keeps the existing 2MB ceiling.
func handlePIMMediaUpload(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Upload exceeds 10MB limit or is malformed", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File is mandatory under multipart FormFile 'file'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read uploaded file", http.StatusBadRequest)
		return
	}

	itemCode := r.FormValue("item")
	mediaRole := r.FormValue("media_role")

	asset, err := engines.SaveMediaFile(tenantID, fileBytes, header.Filename, itemCode, mediaRole, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(asset)
}

// handlePIMMediaFile streams a stored media file back - authenticated (this
// route sits behind apiMiddleware+moduleGate like every other PIM route),
// not the unauthenticated static file server public/ uses (main.go's
// http.FileServer(http.Dir("./public")) below) - the pragmatic in-house
// equivalent of "private storage + signed URL" (see engines/pim_media.go).
func handlePIMMediaFile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mediaID := r.PathValue("id")
	path, fileType, err := engines.GetMediaFile(tenantID, mediaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "stored file missing", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", fileType)
	_, _ = w.Write(fileBytes)
}

func handlePIMMediaList(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	itemCode := r.URL.Query().Get("item")
	if itemCode == "" {
		http.Error(w, "item query parameter is required", http.StatusBadRequest)
		return
	}
	results, err := engines.ListMediaForItem(tenantID, itemCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.ProductMediaAsset{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handlePIMMediaDeactivate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mediaID := r.PathValue("id")
	if err := engines.DeactivateMedia(tenantID, mediaID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deactivated"})
}

// handlePIMPublish (Stage 15.2) queues a publish job after a readiness
// check - see engines.QueuePublish/CheckPublishReadiness for the rules and
// the stub-connector caveat (no real channel credentials exist in this
// environment).
func handlePIMPublish(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ItemCode string `json:"item_code"`
		Channel  string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	jobID, alreadyQueued, err := engines.QueuePublish(tenantID, req.ItemCode, req.Channel, userID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	// Bug fix (found during Stage 16.1 live verification): this used to
	// hardcode "status": "Queued" unconditionally, even when alreadyQueued
	// returns an existing job that's already Published (or Failed) -
	// misleading the caller into thinking a fresh, unprocessed job was
	// created. Look up the job's real current status instead.
	status := "Queued"
	if jobStatus, errStatus := engines.GetPublishJobStatus(tenantID, jobID); errStatus == nil {
		status = jobStatus.Status
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id": jobID, "already_queued": alreadyQueued, "status": status,
	})
}

func handlePIMPublishJobStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	jobID, err := strconv.Atoi(r.PathValue("jobID"))
	if err != nil {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}
	status, err := engines.GetPublishJobStatus(tenantID, jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}

func handlePIMPublishLog(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	itemCode := r.URL.Query().Get("item")
	if itemCode == "" {
		http.Error(w, "item query parameter is required", http.StatusBadRequest)
		return
	}
	results, err := engines.ListPublishLogForItem(tenantID, itemCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.PublishLogEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

// Fixed Asset Management (Stage 13.13b). Asset creation/listing use the
// same generic doc endpoint as Vendor/Customer/RFQ/Printer/Employee; these
// handlers cover the lifecycle actions (capitalise/transfer/dispose) and
// the depreciation-calculated register view, which need logic the generic
// endpoint doesn't have.
func handleAssetRegister(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	results, err := engines.GetAssetRegister(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []engines.AssetRegisterEntry{}
	}
	_ = json.NewEncoder(w).Encode(results)
}

func handleCapitalizeAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetID string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" {
		http.Error(w, "Field 'asset_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.CapitalizeAsset(tenantID, req.AssetID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "ASSET_CAPITALIZE", "SUCCESS", fmt.Sprintf("Asset %s capitalised", req.AssetID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "capitalised"})
}

func handleTransferAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	username := r.Header.Get("Resolved-Username")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetID      string `json:"asset_id"`
		NewLocation  string `json:"new_location"`
		NewCustodian string `json:"new_custodian"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" || req.NewLocation == "" {
		http.Error(w, "Fields 'asset_id' and 'new_location' are required", http.StatusBadRequest)
		return
	}
	if err := engines.TransferAsset(tenantID, req.AssetID, req.NewLocation, req.NewCustodian, username); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "transferred"})
}

func handleDisposeAsset(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssetID      string `json:"asset_id"`
		DisposalType string `json:"disposal_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetID == "" || req.DisposalType == "" {
		http.Error(w, "Fields 'asset_id' and 'disposal_type' are required", http.StatusBadRequest)
		return
	}
	if err := engines.DisposeAsset(tenantID, req.AssetID, req.DisposalType); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "ASSET_DISPOSE", "SUCCESS", fmt.Sprintf("Asset %s disposed (%s)", req.AssetID, req.DisposalType))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "disposed"})
}

// Expense Management (Stage 13.13c). Claim creation/listing and Manager
// Approval use the existing generic doc endpoint + approval engine (Stage
// 13.8); these two handlers cover the Finance Verification and Payment
// stages that come after.
func handleVerifyExpenseClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClaimID == "" {
		http.Error(w, "Field 'claim_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.VerifyExpenseClaim(tenantID, req.ClaimID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "EXPENSE_VERIFY", "SUCCESS", fmt.Sprintf("Expense claim %s finance-verified", req.ClaimID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
}

func handlePayExpenseClaim(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ClaimID string `json:"claim_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClaimID == "" {
		http.Error(w, "Field 'claim_id' is required", http.StatusBadRequest)
		return
	}
	payable, err := engines.PayExpenseClaim(tenantID, req.ClaimID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "EXPENSE_PAY", "SUCCESS", fmt.Sprintf("Expense claim %s paid, payable_amount=%d", req.ClaimID, payable))
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "paid", "payable_amount": payable})
}

// CRM/Loyalty (Stage 13.13d, scoped MVP). Redemption is a standalone action
// (not wired into checkout's GL math - see handleCheckout) that burns
// points and returns their rupee discount value; the cashier applies that
// as a manual price adjustment before submitting the checkout.
func handleRedeemLoyaltyPoints(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CustomerID  string `json:"customer_id"`
		Points      int    `json:"points"`
		ReferenceID string `json:"reference_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == "" || req.Points <= 0 {
		http.Error(w, "Fields 'customer_id' and a positive 'points' are required", http.StatusBadRequest)
		return
	}
	discountValue, err := engines.RedeemLoyaltyPoints(tenantID, req.CustomerID, req.Points, req.ReferenceID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"discount_value": discountValue})
}

func handleLoyaltyLedger(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		http.Error(w, "Query parameter 'customer_id' is required", http.StatusBadRequest)
		return
	}
	balance, err := engines.GetLoyaltyBalance(tenantID, customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ledger, err := engines.GetLoyaltyLedger(tenantID, customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ledger == nil {
		ledger = []engines.LoyaltyLedgerEntry{}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"balance": balance, "ledger": ledger})
}

// Manufacturing (Stage 13.13e, scoped MVP). BOM/ProductionOrder creation
// and listing use the same generic doc endpoint as Vendor/Customer/etc;
// these two handlers cover the material-issue and completion actions,
// which need logic (BOM explosion, inventory posting) the generic endpoint
// doesn't have.
func handleIssueProductionMaterial(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		http.Error(w, "Field 'order_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.IssueProductionMaterial(tenantID, req.OrderID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRODUCTION_MATERIAL_ISSUE", "SUCCESS", fmt.Sprintf("Material issued for production order %s", req.OrderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "material_issued"})
}

func handleCompleteProductionOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	userID := r.Header.Get("Resolved-User-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		http.Error(w, "Field 'order_id' is required", http.StatusBadRequest)
		return
	}
	if err := engines.CompleteProductionOrder(tenantID, req.OrderID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	engines.LogAuditEvent(tenantID, userID, "PRODUCTION_ORDER_COMPLETE", "SUCCESS", fmt.Sprintf("Production order %s completed, finished goods received", req.OrderID))
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

func handleShopifyProductMap(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if !verifyShopifyWebhookSignature(r, body) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid webhook signature"})
		return
	}

	var req struct {
		Sku        string `json:"sku"`
		ChannelSku string `json:"channel_sku"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid mapping payload", http.StatusBadRequest)
		return
	}

	if req.Sku == "" || req.ChannelSku == "" {
		http.Error(w, "Fields 'sku' and 'channel_sku' are required", http.StatusBadRequest)
		return
	}

	err = engines.MapChannelProduct(tenantID, "Shopify", req.Sku, req.ChannelSku)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "mapped",
		"sku":     req.Sku,
		"channel": "Shopify",
	})
}

func handleShopifyOrderWebhook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if !verifyShopifyWebhookSignature(r, body) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid webhook signature"})
		return
	}

	var req struct {
		ID        string `json:"id"`
		LineItems []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"line_items"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid webhook payload", http.StatusBadRequest)
		return
	}

	if req.ID == "" || len(req.LineItems) == 0 {
		http.Error(w, "Fields 'id' and 'line_items' are required", http.StatusBadRequest)
		return
	}

	// Convert structure to slice of maps
	var items []map[string]interface{}
	for _, item := range req.LineItems {
		items = append(items, map[string]interface{}{
			"sku": item.Sku,
			"qty": item.Qty,
		})
	}

	orderID, err := engines.ImportChannelOrder(tenantID, "Shopify", req.ID, items)
	if err != nil {
		if err.Error() == "ORDER_ALREADY_IMPORTED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "ignored",
				"details": "Order already processed (idempotency check)",
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "imported",
		"order_id": orderID,
	})
}

func handleFulfillmentTaskTransition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"` // "Picking", "Packed", "Dispatched", "Rejected"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid transition payload", http.StatusBadRequest)
		return
	}

	if req.TaskID == "" || req.Status == "" {
		http.Error(w, "Fields 'task_id' and 'status' are required", http.StatusBadRequest)
		return
	}

	err := engines.TransitionTaskStatus(tenantID, req.TaskID, req.Status)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "transitioned",
		"task_id":    req.TaskID,
		"new_status": req.Status,
	})
}

func handleFulfillmentReturn(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReturnLocation  string `json:"return_location"`
		OriginalOrderID string `json:"original_order_id"`
		Items           []struct {
			Sku       string  `json:"sku"`
			Qty       int     `json:"qty"`
			SalePrice float64 `json:"sale_price"`
			CostPrice float64 `json:"cost_price"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid return payload", http.StatusBadRequest)
		return
	}

	if req.ReturnLocation == "" || req.OriginalOrderID == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'return_location', 'original_order_id', and 'items' are required", http.StatusBadRequest)
		return
	}

	// Convert items structure to interface slice
	itemsInterface := make([]interface{}, len(req.Items))
	for i, item := range req.Items {
		itemsInterface[i] = map[string]interface{}{
			"sku":        item.Sku,
			"qty":        item.Qty,
			"sale_price": item.SalePrice,
			"cost_price": item.CostPrice,
		}
	}

	err := engines.ProcessReturnAnywhere(tenantID, req.ReturnLocation, req.OriginalOrderID, itemsInterface)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Save dynamic SalesReturn document
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil {
		payloadBytes, _ := json.Marshal(req)
		query := fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by) 
			VALUES ($1, 'SalesReturn', $2, 'Returned', 'system')`, schema)
		_, _ = db.DB.Exec(query, fmt.Sprintf("RET-%s", req.OriginalOrderID), payloadBytes)
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":            "refunded",
		"original_order_id": req.OriginalOrderID,
		"returned_location": req.ReturnLocation,
	})
}

func handleScaleTest(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NumStores       int `json:"num_stores"`
		NumWorkers      int `json:"num_workers"`
		NumTransactions int `json:"num_transactions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid scale test parameters", http.StatusBadRequest)
		return
	}

	if req.NumStores <= 0 || req.NumWorkers <= 0 || req.NumTransactions <= 0 {
		http.Error(w, "Parameters 'num_stores', 'num_workers', and 'num_transactions' must be positive integers", http.StatusBadRequest)
		return
	}

	// 1. Seed test data
	err := engines.SeedScaleTestData(tenantID, req.NumStores, "BAR-SCALE", 1000)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to seed scale data: %v", err)})
		return
	}

	// 2. Run simulation
	report, err := engines.RunScaleSimulation(tenantID, req.NumWorkers, req.NumTransactions, "BAR-SCALE", req.NumStores)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to execute scale simulation: %v", err)})
		return
	}

	_ = json.NewEncoder(w).Encode(report)
}

func handleMarketplaceReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Channel      string   `json:"channel"`
		SettlementID string   `json:"settlement_id"`
		TotalSale    int      `json:"total_sale"`
		Commission   int      `json:"commission"`
		NetPayout    int      `json:"net_payout"`
		OrderIDs     []string `json:"order_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid reconciliation payload", http.StatusBadRequest)
		return
	}

	if req.SettlementID == "" || req.Channel == "" || req.TotalSale <= 0 {
		http.Error(w, "Fields 'settlement_id', 'channel', and positive 'total_sale' are required", http.StatusBadRequest)
		return
	}

	err := engines.ProcessMarketplaceSettlement(tenantID, req.Channel, req.SettlementID, req.TotalSale, req.Commission, req.NetPayout, req.OrderIDs)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":        "reconciled",
		"settlement_id": req.SettlementID,
		"net_received":  fmt.Sprintf("%d", req.NetPayout),
	})
}

func handleLogisticsBook(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OrderID        string `json:"order_id"`
		Carrier        string `json:"carrier"`
		TrackingNumber string `json:"tracking_number"`
		ShippingCharge int    `json:"shipping_charge"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid logistics payload", http.StatusBadRequest)
		return
	}

	if req.OrderID == "" || req.Carrier == "" || req.TrackingNumber == "" {
		http.Error(w, "Fields 'order_id', 'carrier', and 'tracking_number' are required", http.StatusBadRequest)
		return
	}

	bookingID, err := engines.CreateLogisticsBooking(tenantID, req.OrderID, req.Carrier, req.TrackingNumber, req.ShippingCharge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":          "shipped",
		"booking_id":      bookingID,
		"tracking_number": req.TrackingNumber,
	})
}

func handleReplenishmentSuggestions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	locCode := r.URL.Query().Get("location_code")
	if locCode == "" {
		http.Error(w, "Query parameter 'location_code' is required", http.StatusBadRequest)
		return
	}

	// Parse optional lead_time and safety_stock parameters
	leadTime := 7
	safetyStock := 10
	if ltStr := r.URL.Query().Get("lead_time"); ltStr != "" {
		_, _ = fmt.Sscanf(ltStr, "%d", &leadTime)
	}
	if ssStr := r.URL.Query().Get("safety_stock"); ssStr != "" {
		_, _ = fmt.Sscanf(ssStr, "%d", &safetyStock)
	}

	suggestions, err := engines.GetReplenishmentSuggestions(tenantID, locCode, leadTime, safetyStock)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(suggestions)
}

func handleSLABreaches(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	threshold := 120.0 // Default to 2 hours
	if threshStr := r.URL.Query().Get("threshold"); threshStr != "" {
		_, _ = fmt.Sscanf(threshStr, "%f", &threshold)
	}

	reports, err := engines.GetSLABreaches(tenantID, threshold)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(reports)
}

func handleDemandForecast(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		LocationCode string `json:"location_code"`
		SKU          string `json:"sku"`
		ForecastDays int    `json:"forecast_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid forecasting payload", http.StatusBadRequest)
		return
	}

	if req.LocationCode == "" || req.SKU == "" || req.ForecastDays <= 0 {
		http.Error(w, "Fields 'location_code', 'sku', and positive 'forecast_days' are required", http.StatusBadRequest)
		return
	}

	forecasted, err := engines.ForecastDemand(tenantID, req.LocationCode, req.SKU, req.ForecastDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"location_code":     req.LocationCode,
		"sku":               req.SKU,
		"forecast_days":     req.ForecastDays,
		"forecasted_demand": forecasted,
	})
}

// =========================================================================
// Stage 9.1: Unicommerce Integration Handlers
// =========================================================================

func handleUnicommerceCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure Unicommerce credentials"})
		return
	}
	var req struct {
		StoreCode string `json:"store_code"`
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
		BaseURL   string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.StoreCode == "" || req.APIKey == "" || req.APISecret == "" || req.BaseURL == "" {
		http.Error(w, "Fields 'store_code', 'api_key', 'api_secret', and 'base_url' are required", http.StatusBadRequest)
		return
	}
	if err := engines.SaveUnicommerceCredential(tenantID, req.StoreCode, req.APIKey, req.APISecret, req.BaseURL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "store_code": req.StoreCode})
}

func handleGetUnicommerceCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	creds, err := engines.GetUnicommerceCredentials(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(creds)
}

func handleUnicommerceOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ChannelOrderID string `json:"channel_order_id"`
		StoreCode      string `json:"store_code"`
		Items          []struct {
			Sku string `json:"sku"`
			Qty int    `json:"qty"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ChannelOrderID == "" || req.StoreCode == "" || len(req.Items) == 0 {
		http.Error(w, "Fields 'channel_order_id', 'store_code', and 'items' are required", http.StatusBadRequest)
		return
	}
	var items []map[string]interface{}
	for _, item := range req.Items {
		items = append(items, map[string]interface{}{
			"sku": item.Sku,
			"qty": item.Qty,
		})
	}
	orderID, err := engines.ImportUnicommerceOrder(tenantID, req.ChannelOrderID, req.StoreCode, items)
	if err != nil {
		if err.Error() == "ORDER_ALREADY_IMPORTED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "details": "Order already imported (idempotency check)"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "imported", "order_id": orderID})
}

func handleListUnicommerceOrders(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	orders, err := engines.ListUnicommerceOrders(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if orders == nil {
		orders = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(orders)
}

func handleListUnicommerceInventorySyncs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	syncs, err := engines.ListUnicommerceInventorySyncs(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if syncs == nil {
		syncs = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(syncs)
}

// =========================================================================
// Stage 9.1: Pine Labs Plutus Integration Handlers
// =========================================================================

func handlePineLabsCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure Pine Labs credentials"})
		return
	}
	var req struct {
		TerminalID string `json:"terminal_id"`
		APIKey     string `json:"api_key"`
		MerchantID string `json:"merchant_id"`
		BaseURL    string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TerminalID == "" || req.APIKey == "" || req.MerchantID == "" || req.BaseURL == "" {
		http.Error(w, "Fields 'terminal_id', 'api_key', 'merchant_id', and 'base_url' are required", http.StatusBadRequest)
		return
	}
	if err := engines.SavePineLabsCredential(tenantID, req.TerminalID, req.APIKey, req.MerchantID, req.BaseURL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "terminal_id": req.TerminalID})
}

func handleGetPineLabsCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	creds, err := engines.GetPineLabsCredentials(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(creds)
}

func handlePineLabsTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TransactionID string  `json:"transaction_id"`
		TerminalID    string  `json:"terminal_id"`
		CartNumber    string  `json:"cart_number"`
		Amount        float64 `json:"amount"`
		PaymentMode   string  `json:"payment_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TransactionID == "" || req.TerminalID == "" || req.CartNumber == "" || req.Amount <= 0 {
		http.Error(w, "Fields 'transaction_id', 'terminal_id', 'cart_number', and positive 'amount' are required", http.StatusBadRequest)
		return
	}
	paymentMode := req.PaymentMode
	if paymentMode == "" {
		paymentMode = "Card"
	}
	if err := engines.RecordPineLabsTransaction(tenantID, req.TransactionID, req.TerminalID, req.CartNumber, req.Amount, paymentMode); err != nil {
		if err.Error() == "TRANSACTION_ALREADY_RECORDED" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "details": "Transaction already recorded"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "recorded", "transaction_id": req.TransactionID})
}

func handlePineLabsReconcile(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can run Pine Labs reconciliation"})
		return
	}
	result, err := engines.ReconcilePineLabsTransactions(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func handleListPineLabsTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	txns, err := engines.ListPineLabsTransactions(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if txns == nil {
		txns = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(txns)
}

// =========================================================================
// Stage 9.1: CleverTap Integration Handlers
// =========================================================================

func handleCleverTapCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can configure CleverTap credentials"})
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
		Passcode  string `json:"passcode"`
		Region    string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.AccountID == "" || req.Passcode == "" {
		http.Error(w, "Fields 'account_id' and 'passcode' are required", http.StatusBadRequest)
		return
	}
	region := req.Region
	if region == "" {
		region = "in1"
	}
	if err := engines.SaveCleverTapCredential(tenantID, req.AccountID, req.Passcode, region); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved", "account_id": req.AccountID})
}

func handleGetCleverTapCredentials(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	creds, err := engines.GetCleverTapCredentials(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if creds == nil {
		creds = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(creds)
}

func handleListCleverTapLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	logs, err := engines.ListCleverTapEventLogs(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(logs)
}

func handleGetIntegrationLogs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	logs, err := engines.GetIntegrationLogs(tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(logs)
}

func handleRetryIntegrationEvent(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		EventID string `json:"event_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid retry payload", http.StatusBadRequest)
		return
	}

	if req.EventID == "" {
		http.Error(w, "Field 'event_id' is required", http.StatusBadRequest)
		return
	}

	err := engines.RetryIntegrationEvent(tenantID, req.EventID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "queued_for_retry",
		"event_id": req.EventID,
	})
}

func handleProvisionTenant(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can provision new tenants"})
		return
	}

	var req struct {
		TenantID   string `json:"tenant_id"`
		SchemaName string `json:"schema_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid provisioning payload", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" || req.SchemaName == "" {
		http.Error(w, "Fields 'tenant_id' and 'schema_name' are required", http.StatusBadRequest)
		return
	}

	adminPassword, err := engines.ProvisionTenantSchema(req.TenantID, req.SchemaName, currentAppVersion())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":              "provisioned",
		"tenant_id":           req.TenantID,
		"schema_name":         req.SchemaName,
		"admin_username":      "admin",
		"admin_password":      adminPassword,
		"admin_password_note": "Shown once - store it securely now. It is not persisted in plaintext anywhere and cannot be retrieved again.",
	})
}

func handleSetFeatureFlag(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("Resolved-Tenant-ID")
	role := r.Header.Get("Resolved-Role")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can modify feature flags"})
		return
	}

	var req struct {
		FeatureName string `json:"feature_name"`
		Enabled     bool   `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid feature-flag payload", http.StatusBadRequest)
		return
	}

	if req.FeatureName == "" {
		http.Error(w, "Field 'feature_name' is required", http.StatusBadRequest)
		return
	}

	err := engines.SetFeatureFlag(tenantID, req.FeatureName, req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "updated",
		"feature_name": req.FeatureName,
		"enabled":      req.Enabled,
	})
}

// handleListModules returns the global module catalog (tenant-independent) -
// what modules exist at all, regardless of any tenant's entitlements.
func handleListModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view the module catalog"})
		return
	}

	modules, err := engines.ListModules()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(modules)
}

// handleGetModuleEntitlements returns the module catalog joined with the
// resolved tenant's current enabled/disabled state for each module.
func handleGetModuleEntitlements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view module entitlements"})
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = r.Header.Get("Resolved-Tenant-ID")
	}

	entitlements, err := engines.ListModuleEntitlements(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(entitlements)
}

// handleSetModuleEntitlement enables or disables one module for a tenant.
// Core modules are rejected server-side by engines.SetModuleEntitlement -
// this handler just surfaces that error as a 400 rather than re-checking it.
func handleSetModuleEntitlement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can modify module entitlements"})
		return
	}

	var req struct {
		TenantID  string `json:"tenant_id"`
		ModuleKey string `json:"module_key"`
		Enabled   bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid module-entitlement payload", http.StatusBadRequest)
		return
	}
	if req.ModuleKey == "" {
		http.Error(w, "Field 'module_key' is required", http.StatusBadRequest)
		return
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = r.Header.Get("Resolved-Tenant-ID")
	}

	grantedBy := r.Header.Get("Resolved-Username")
	if err := engines.SetModuleEntitlement(tenantID, req.ModuleKey, req.Enabled, grantedBy); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "updated",
		"tenant_id":  tenantID,
		"module_key": req.ModuleKey,
		"enabled":    req.Enabled,
	})
}

// handleVersion (Stage 14.6) reports the running binary's build identity.
// Public (see publicRoutes) - an ops tool or client should be able to check
// what's running without authenticating first, same as any /version
// endpoint elsewhere.
func handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"version":    currentAppVersion(),
		"git_commit": gitCommit,
		"build_time": buildTime,
	})
}

// handleGetTenantVersion (Stage 14.6) surfaces what version was recorded
// against a tenant's schema at last provision/promotion time, alongside the
// live binary's own version, so a mismatch is visible from the API without
// SSH'ing into an instance. app_version here is a point-in-time compat/audit
// record, not live per-request dispatch - one running process can only ever
// serve one binary version, regardless of which tenant is asking.
func handleGetTenantVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view tenant version records"})
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = r.Header.Get("Resolved-Tenant-ID")
	}
	if tenantID == "" || tenantID == "default" {
		tenantID = "default"
	}

	var recordedVersion, pinnedVersion sql.NullString
	err := db.DB.QueryRow(`SELECT app_version, pinned_version FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&recordedVersion, &pinnedVersion)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unknown tenant_id"})
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	liveVersion := currentAppVersion()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tenant_id":        tenantID,
		"recorded_version": recordedVersion.String,
		"pinned_version":   pinnedVersion.String,
		"live_version":     liveVersion,
		"mismatch":         recordedVersion.Valid && recordedVersion.String != liveVersion,
	})
}

// handleListPatchProposals (Stage 14.13-14.16) lists patch-intake proposals,
// optionally filtered by status (defaults to "pending" - the queue an admin
// actually needs to act on; pass ?status=all to see everything).
func handleListPatchProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view patch proposals"})
		return
	}

	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	if status == "all" {
		status = ""
	}

	proposals, err := engines.ListPatchProposals(status)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if proposals == nil {
		proposals = []engines.PatchProposal{}
	}
	_ = json.NewEncoder(w).Encode(proposals)
}

// decidePatchProposalRequest is the shared payload shape for both
// approve/reject - kept as two distinct routes (rather than one route with
// a decision field) so each action's intent is explicit in the URL, the
// same convention handleSetFeatureFlag/handleSetModuleEntitlement don't
// need but approval/rejection - a one-way decision - benefits from.
type decidePatchProposalRequest struct {
	ProposalID int    `json:"proposal_id"`
	Notes      string `json:"notes"`
}

func handleApprovePatchProposal(w http.ResponseWriter, r *http.Request) {
	decidePatchProposal(w, r, "approved")
}

func handleRejectPatchProposal(w http.ResponseWriter, r *http.Request) {
	decidePatchProposal(w, r, "rejected")
}

// decidePatchProposal records a human decision only - it never takes any
// further action itself. See engines/patchintake.go's package doc comment
// for why that boundary is deliberate: applying a real fix (a
// module-entitlement toggle, a code change promoted via promote.ps1) stays
// a separate, manual step using the tools already built in Phases A/C.
func decidePatchProposal(w http.ResponseWriter, r *http.Request, decision string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can decide patch proposals"})
		return
	}

	var req decidePatchProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid patch-proposal decision payload", http.StatusBadRequest)
		return
	}
	if req.ProposalID == 0 {
		http.Error(w, "Field 'proposal_id' is required", http.StatusBadRequest)
		return
	}

	decidedBy := r.Header.Get("Resolved-Username")
	if err := engines.DecidePatchProposal(req.ProposalID, decision, decidedBy, req.Notes); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "updated",
		"proposal_id": req.ProposalID,
		"decision":    decision,
	})
}

// handleCreateExtensionHook registers a new hook and returns its secret -
// shown exactly once, same "generated + never retrievable again" pattern
// tenant admin passwords already use (engines.ProvisionTenantSchema).
func handleCreateExtensionHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can register extension hooks"})
		return
	}

	var req struct {
		HookPoint string `json:"hook_point"`
		Doctype   string `json:"doctype"`
		TargetURL string `json:"target_url"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid extension-hook payload", http.StatusBadRequest)
		return
	}
	if req.Doctype == "" {
		http.Error(w, "Field 'doctype' is required (use '*' to match every doctype)", http.StatusBadRequest)
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	createdBy := r.Header.Get("Resolved-Username")
	id, secret, err := engines.RegisterExtensionHook(tenantID, req.HookPoint, req.Doctype, req.TargetURL, req.TimeoutMs, createdBy)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":          id,
		"secret":      secret,
		"secret_note": "Shown once - store it securely now. It is not persisted in plaintext anywhere and cannot be retrieved again.",
	})
}

func handleListExtensionHooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view extension hooks"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	hooks, err := engines.ListExtensionHooks(tenantID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if hooks == nil {
		hooks = []engines.ExtensionHook{}
	}
	_ = json.NewEncoder(w).Encode(hooks)
}

func handleDeleteExtensionHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can delete extension hooks"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	hookID := r.PathValue("id")
	if err := engines.DeleteExtensionHook(tenantID, hookID); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": hookID})
}

func handleGetExtensionHookLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can view extension hook logs"})
		return
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	hookID := r.PathValue("id")
	logEntries, err := engines.GetExtensionHookLog(tenantID, hookID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if logEntries == nil {
		logEntries = []engines.ExtensionHookLogEntry{}
	}
	_ = json.NewEncoder(w).Encode(logEntries)
}

// handleIssueExtensionToken issues a tenant-and-doctype-scoped token for a
// 3rd-party developer's extension code to call back into this API with -
// the only credential it ever receives, alongside the inbound hook payload
// itself. Never the core repo, never a full session, never another tenant.
func handleIssueExtensionToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	role := r.Header.Get("Resolved-Role")
	if role != "HR/Admin" {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Only HR/Admin can issue extension tokens"})
		return
	}

	var req struct {
		ScopeDoctype string `json:"scope_doctype"`
		TTLMinutes   int    `json:"ttl_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid extension-token payload", http.StatusBadRequest)
		return
	}
	if req.ScopeDoctype == "" {
		http.Error(w, "Field 'scope_doctype' is required", http.StatusBadRequest)
		return
	}
	ttl := time.Duration(req.TTLMinutes) * time.Minute
	if req.TTLMinutes <= 0 || ttl > 24*time.Hour {
		ttl = time.Hour
	}

	tenantID := r.Header.Get("Resolved-Tenant-ID")
	token := engines.SignExtensionToken(tenantID, req.ScopeDoctype, ttl)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":              token,
		"scope_doctype":      req.ScopeDoctype,
		"expires_in_minutes": int(ttl.Minutes()),
	})
}
