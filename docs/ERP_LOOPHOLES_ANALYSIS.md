# ERP System - Loopholes & Security Analysis

**Date:** 2026-07-27 (Updated from 2026-07-26)
**Analyst:** AI Code Review
**Scope:** Full codebase review covering security, data integrity, concurrency, and architectural gaps

---

## Executive Summary

The ERP system demonstrates strong architectural patterns (schema-per-tenant isolation, maker-checker approval, double-entry bookkeeping, audit logging). This pass re-verified every item this document had previously listed as "still open" against the current codebase (Stage 24-26 shipped a lot of hardening work under other stage numbers without this document being updated to match), closed the extension-token defense-in-depth gap, and then dug into the one remaining item (document status transitions) far enough to find it hid a real, actively-exploitable maker-checker bypass, not just a cosmetic edge case — closed that too.

**Result of this pass: 19 of the 21 previously-"open" items were already fixed by later-stage work (just never reflected here); 2 were closed this session — extension token scope (defense-in-depth) and, more significantly, a direct-to-`Approved`/`Rejected` bypass of the entire approval engine via the generic doc endpoint. A narrower, lower-stakes residual of the status-transition item (a general per-doctype transition map for non-approval-gated doctypes) remains open pending a product decision.**

> **Update — 2026-07-29 (Stage 29.8): nothing is open any more.** The two items this document still carried were both waiting on the user rather than on work. They decided, and both shipped the same day: the per-doctype transition map (opt-in strict, 178 rules, 64 doctypes flagged) and the JWT session-staleness gap. That second one turned out to be **wider than this document had recorded** — the deferred item was framed as "revocation list / key rotation", but the real defect was that `ParseToken` never touched the database at all, so a *demoted* user's token kept asserting its old role for its full lifetime, which a revocation list would not have fixed. See "Remaining work" below, `micro_checklist.md` Stage 29.8, and `project_ledger.md` §67.

**Risk Level Distribution (Current, after Stage 29.8):**
- 🔴 **Critical:** 0 issues open
- 🟠 **High:** 0 issues open
- 🟡 **Medium:** 0 issues open
- 🟢 **Low:** 0 issues open

**Total resolved (all sessions to date): 34 of 34 originally identified issues fully addressed (100%), plus the two follow-on items (per-doctype transition map, JWT session staleness + key rotation) closed 2026-07-29. No open items and no pending decisions remain in this document.**

---

## ✅ FIXED ISSUES (Stage 24 Security Hardening, 2026-07-24 pass)

### ✅ #1 — SQL Injection Risk via Schema Name Interpolation
**Fixed in:** `db/db.go` (Stage 24.17)

`GetTenantSchema()` now validates schema names against a regex allowlist before returning:
```go
var validSchemaNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
// ...
if !validSchemaNameRe.MatchString(schemaName) {
    return "", fmt.Errorf("resolved schema name %q is not a valid identifier", schemaName)
}
```

**Status:** ✅ **FIXED** — Defense-in-depth at the single source of all schema names.

---

### ✅ #2 — Hardcoded Location Code Bypasses Location-Based Access Control
**Fixed in:** `internal/server/handlers_auth.go` (Stage 24.1)

The login handler now reads `location_code` from the `users` table and passes it to `SignToken`.

**Status:** ✅ **FIXED**

---

### ✅ #3 — No Idempotency Protection for Financial Operations
**Fixed in:** `engines/finance.go` + `db/migrations_stage24_security.sql` (Stage 24.5)

**Status:** ✅ **FIXED** — All call sites use convention `<DocType>:<DocID>:<PURPOSE>`.

---

### ✅ #4 — Accounting Period Validation Bypass via Backdating
**Fixed in:** `engines/accounting_periods.go` + `engines/finance.go` (Stage 24.6)

**Status:** ✅ **FIXED**

---

### ✅ #6 — Missing Rate Limiting on MFA Endpoints
**Fixed in:** `internal/server/middleware.go` (Stage 24.28)

**Status:** ✅ **FIXED**

---

### ✅ #8 — No Request Timeout Configuration
**Fixed in:** `db/db.go` (Stage 24.13)

**Status:** ✅ **FIXED**

---

### ✅ #10 — Vendor Invoice Payment Override Lacks Approval Workflow
**Fixed in:** `engines/vendor_invoice.go` + `db/migrations_stage24_security.sql` (Stage 24.11)

**Status:** ✅ **FIXED**

---

### ✅ #13 — Audit Log Triggers Can Be Disabled
**Fixed in:** `db/migrations_stage24_security.sql` (Stage 24.24)

**Status:** ✅ **FIXED**

---

### ✅ #16 — Inventory Availability Race Condition in Reservation
**Fixed in:** `engines/inventory.go` (Stage 24.7)

**Status:** ✅ **FIXED**

---

### ✅ #18 — Missing Index on inventory_reservation.expires_at
**Fixed in:** `db/migrations_stage24_security.sql` (Stage 24.7)

**Status:** ✅ **FIXED**

---

### ✅ #21 — No Concurrent Edit Detection (Optimistic Locking)
**Fixed in:** `db/migrations_stage24_security.sql` (Stage 24.10)

**Status:** ✅ **FIXED**

---

### ✅ #22 — Password Reset Token Not Implemented
**Fixed in:** `engines/password_reset.go` + `db/migrations_stage24b_deferred_hardening.sql` (Stage 24.28)

**Status:** ✅ **FIXED**

---

### ✅ #32 — Single Global Database Connection Pool
**Fixed in:** `internal/server/middleware.go` (Stage 24.30)

**Status:** ✅ **MITIGATED** — Per-tenant concurrency cap applied; full per-tenant pools deferred as architecture decision.

---

## ✅ VERIFIED FIXED / MITIGATED (2026-07-26 pass)

Every item below was listed as "Still Open" as of the 2026-07-24 revision of this document. Re-checking the actual code found that later work (Stage 24.14 through 24.34, including a dedicated cross-check against this exact document recorded in `docs/micro_checklist.md`'s 2026-07-24/26 addenda) had already closed nearly all of them — this document simply hadn't been updated to match. Each entry below cites the exact code that closes it, so it can be spot-checked.

### ✅ (was Critical #1) — Extension Token Scope Not Enforced in All Handlers
**Closed by construction; defense-in-depth added this session in:** `internal/server/handlers_admin_identity.go` (`requireHRAdmin`)

Investigation found the specific handlers named in the original finding (`handleLabels`, `handleSequence`, `handlePrefix`) already gated their write paths through `requireHRAdmin`, and `SignExtensionToken` never issues a `role` claim — so `role != "HR/Admin"` was already denying these today (also independently confirmed in `docs/micro_checklist.md`'s 2026-07-24 addendum). No live bypass existed. This session added one explicit line at the shared choke point behind all 17 `requireHRAdmin` call sites anyway, as belt-and-suspenders against a future `role_permissions`-style misconfiguration ever granting `""` a role, rather than relying solely on the absence-of-a-claim behavior:
```go
func requireHRAdmin(w http.ResponseWriter, r *http.Request, role string) bool {
    if r.Header.Get("Resolved-Purpose") == "extension" {
        writeAPIErrorGeneric(w, r, http.StatusForbidden, "Extension tokens cannot access admin/configuration endpoints")
        return false
    }
    if role != "HR/Admin" {
        ...
```
Live-verified against a scratch instance: an extension token now gets an explicit 403 from `/api/v1/prefix` (POST) and the new pool-stats path, while its intended narrow use (GET on its scoped doctype via `handleGenericDoc`) is unaffected.

**Status:** ✅ **FIXED**

---

### ✅ (was High #2) — Unsafe JSON Unmarshaling Without Error Handling
**Fixed in:** `internal/server/handlers_core_doc_engine.go` (Stage 24.18), `engines/approval.go`'s `ListPendingApprovals` (Stage 24.18), `engines/transactional_validation.go`'s `fetchPOItemQuantities`/`validateGRNRules`/`validateASNRules` (Stage 24.33)

The exact scenario the original finding cited — a failed `json.Unmarshal` into a nil map followed by `dataMap["id"] = id`, which panics on nil-map assignment — is fixed everywhere `documents.data` is read back (single-doc GET, list GET, `ListPendingApprovals`): each now checks the error and returns a clean 500 / logs-and-skips-the-row instead of crashing. Stage 24.33 separately closed a fail-*open* variant of the same root problem: a malformed `PurchaseOrder.items` JSON used to silently compute a zero ordered-qty map, which could let an over-receipt check pass incorrectly instead of blocking it — `fetchPOItemQuantities` now returns the real error, and its two callers distinguish "PO genuinely not found" (`sql.ErrNoRows`, still a legitimate skip) from any other error (now fails closed).

A handful of `_ = json.Unmarshal(...)` call sites remain by deliberate choice, reviewed and confirmed low-risk (per `docs/micro_checklist.md`'s own 24.33 note): `engines/transactional_validation.go`'s GRN-vs-PO-data re-fetch, `engines/pim_content_versions.go`'s version history, and one CSV-import-preview self-round-trip in `handlers_pim_pos_finance.go`. Each only *reads* from the target afterward, never writes into it, so a failed unmarshal degrades to an empty/zero-value read rather than a nil-map-write panic — the same holds for `engines/manufacturing_mrp.go`'s scrap/rework/operation-progress logs. (This session initially "tightened" the `transactional_validation.go` one to fail closed too, before finding the standing decision to leave it alone for exactly this reason — reverted to match.)

**Status:** ✅ **FIXED** (crash-risk sites and the fail-open PO-items case) / reviewed-safe-as-is (remaining internal round-trips, by standing decision)

---

### ⚠️ (was High #3) — Weak Custom JWT Implementation
**Mitigated in:** `engines/auth.go` (Stage 24.19, 24.22)

Hardened using stdlib only, deliberately *not* adopting `github.com/golang-jwt/jwt` (this repo's no-new-dependency-unless-genuinely-necessary rule):
- `iat` (issued-at) and `jti` (unique token ID) added to every claim set.
- `ParseToken` collapses every failure mode (malformed / bad signature / expired) to one generic `errInvalidToken` (closes the timing/information-leak half of this finding too — see old High #6 below).

**Two sub-gaps remain deliberately deferred, not fixed:**
- **Token revocation** — no server-side revocation list; a leaked token can only be neutralized by waiting out its TTL or rotating `JWT_SECRET` (invalidates every token platform-wide). `jti` is already minted on every token specifically so a revocation list could key off it later.
- **Key rotation** — a single active `JWT_SECRET`, no key-ID claim or multi-key verification.

Both are pure engineering (no new dependency needed for either), but this repo already has a standing, explicit policy of not building speculative security infrastructure ahead of a real need (see `docs/extension_hooks_checklist.md`'s identical stance on extension-token revocation). Re-opening this is a **product/ops decision**, not a code gap: revisit if a real leaked-token incident or a compliance requirement makes revocation/rotation necessary.

**Status:** ⚠️ **MITIGATED — 2 sub-items intentionally deferred pending a future need, not a current decision request**

---

### ✅ (was High #4) — No Input Length Validation on API Endpoints
**Fixed in:** `engines/doctype.go` (Stage 24.31)

`ValidateDocument` enforces a `defaultFieldMaxLength` (10,000 chars) blanket cap on every field, tightenable/loosenable per-field via `DocTypeField.MaxLength`:
```go
if len(valStr) > limit {
    return &ValidationError{SubFor: f.Label, Message: fmt.Sprintf("Field %q (%s) exceeds the maximum allowed length of %d characters", f.Label, f.Fieldname, limit)}
}
```
The 2 MB whole-request-body cap (`http.MaxBytesReader`, `internal/server/middleware.go`) already bounded the extreme case; this closes the "one wildly oversized field in an otherwise-small request" gap the finding described.

**Status:** ✅ **FIXED**

---

### ✅ (was High #5) — CSV Import Lacks Transaction Isolation for Dry-Run
**Mitigated in:** `engines/import.go` (Stage 24.32)

Rather than the originally-suggested isolation-level change, the actual root concern (a transaction held open for the entire file, contending with concurrent writers) is addressed by batching: `importBatchRows = 500` bounds every transaction — dry-run or not — to at most 500 rows before it commits/rolls back and a fresh one starts. Worst-case lock hold time is now one batch, not the whole file, regardless of file size.

**Status:** ✅ **MITIGATED**

---

### ✅ (was High #6) — No Protection Against Timing Attacks on Token Validation
**Fixed in:** `engines/auth.go` (Stage 24.22)

`ParseToken` returns the single sentinel `errInvalidToken` for every failure mode (malformed, bad signature, missing/malformed expiry, expired) — no more distinct error text per case.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #7) — Missing Pagination on Audit Logs Endpoint
**Fixed in:** `internal/server/handlers_core_doc_engine.go` (Stage 24.20)

`handleAuditLogs` now accepts `limit`/`offset` query params (same convention as the generic doc-list endpoint), capped at `maxListLimit`, defaulting to the old hardcoded 100 when neither is passed.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #8) — No Validation on Prefix Config Reset Frequency
**Fixed in:** `internal/server/handlers_core_doc_engine.go` (Stage 24.21)

`handlePrefix`'s POST rejects any `reset_frequency` outside `{ANNUAL, MONTHLY, NEVER}` — the exact 3-value set `engines/numbering.go`'s sequence generator understands.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #9) — No Validation on Document Status Transitions — **the security-relevant half fixed; the rest genuinely still needs a decision**
**Fixed in:** `internal/server/handlers_core_doc_engine.go` (`handleGenericDoc`, generic doc create/update path)

Re-investigating this (rather than only re-confirming `GLOBAL-0019` was dead code, as the prior pass had) surfaced that the actual risk here was much more severe than "an edge case that could lock out a legitimate transition": **any user with plain create/update permission on an approval-gated doctype could set `"status": "Approved"` (or `"Rejected"`) directly in their payload and completely bypass the maker-checker approval engine** — no `SubmitForApproval`, no `DecideApproval`, no role check, no maker-checker segregation-of-duties check. `statusVal` was taken verbatim from client payload with zero restriction before being written straight to the `documents` table.

This sub-case doesn't need a business-rules decision to close — approval-gated doctypes already have a fully-defined state machine owned by the approval engine itself (`Draft`/`Pending Approval`/`Approved`/`Rejected`), so "a bare doc write can't claim `Approved`/`Rejected` out from under that engine" is a pure invariant, not a per-doctype judgment call. Fix:
```go
if (statusVal == "Approved" || statusVal == "Rejected") && statusVal != priorStatus {
    if gated, errGate := engines.IsApprovalGated(tenantID, doctype); errGate == nil && gated {
        writeAPIError(w, r, "GLOBAL-0019", "")
        return
    }
}
```
`statusVal != priorStatus` is the important part: an edit that merely round-trips an already-`Approved` document's unchanged status (the common "GET included status, PUT sent the whole object back" pattern) still passes through untouched, exactly as before — the pre-existing `wasApproved`/`ResetToPendingOnEdit` logic still forces it back to `Pending Approval` afterward, unaffected. Live-verified end to end against a scratch instance: direct create-as-`Approved` → 422 `GLOBAL-0019`; direct edit-to-`Approved` from `Draft` → 422; normal `Draft` create/edit → 200, unaffected; full legitimate `SubmitForApproval`→`DecideApproval` (different actor) → 200; editing an already-`Approved` PO with status round-tripped unchanged → 200, then correctly reset to `Pending Approval` by the existing re-approval-on-edit logic, exactly as before.

**What's still genuinely open, unchanged from the prior finding:** a *general* per-doctype valid-transition map for doctypes that aren't approval-gated (Masters' Active/Inactive, GRN/ProductionOrder's own richer flows, etc.) still doesn't exist and still needs the business-rules owner to specify it — that part really does need a decision, and this fix deliberately doesn't guess at it. The difference is that the part which *didn't* need a decision (protecting the approval engine's own already-defined states) is no longer sitting open just because the harder part was still undecided.

**[needs design decision, narrowed: only the non-approval-gated per-doctype transition map remains — e.g. should a plain Master's `Active`→`Inactive`→`Active` cycle be restricted, should GRN/ProductionOrder's own status fields get a formal map instead of their current ad-hoc edit-rule checks, etc. Lower urgency now that the maker-checker bypass itself is closed.]**

**Status:** ✅ **FIXED** (the actual security-relevant bypass) / 🟡 **narrower residual item still open, still a decision, now lower-stakes**

---

### ✅ (was Medium #10) — No Validation on Approval Rule Amount Ranges
**Fixed in:** `engines/approval.go` (Stage 24.8)

`UpsertApprovalRule` rejects saving a rule whose `[min_amount, max_amount]` range overlaps an existing rule for the same doctype, before insert/update.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #11) — GST Calculation Floating-Point Precision Issues
**Mitigated in:** `engines/gst.go` (Stage 24.9)

`round2()` rounds every tax figure to 2 decimal places (paise) using stdlib `math.Round` before it goes anywhere else, closing the `18.018000000000003`-style artifact the finding described. Deliberately not `shopspring/decimal` per this stage's own no-new-dependency scoping note — sufficient for this app's actual currency (INR, 2 decimal places everywhere). Would need revisiting only if a future requirement adds a currency with a different minor-unit precision.

**Status:** ✅ **MITIGATED**

---

### ✅ (was Medium #12) — Missing CSRF Protection — **Not Applicable, verified**

Verified rather than assumed: grepped the entire codebase for `http.Cookie`/`SetCookie` — zero results anywhere. Every authenticated request in this app carries a Bearer token in the `Authorization` header (`public/app.js`'s `localStorage.getItem('erp_token')` → `headers['Authorization'] = 'Bearer ' + token`), never a cookie. CSRF exploits rely on the browser *automatically* attaching an ambient credential (a cookie) to a cross-site request; there is no such credential here for a forged request to ride on — a malicious page cannot read another origin's `localStorage` to forge the header itself.

**Status:** ✅ **NOT APPLICABLE to current architecture** — revisit only if the auth model ever adds cookie-based sessions.

---

### ✅ (was Medium #13) — No Validation on Industry Profile File Path
**Fixed in:** `internal/server/handlers_core_doc_engine.go` (Stage 24.3)

`handleSwitchIndustry` checks `req.IndustryCode` against the `validIndustryCodes` allowlist and rejects anything unrecognized *before* the profile path is ever constructed or touched on disk.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #14) — No Health Check Endpoint
**Fixed in:** `internal/server/routes.go` (Stage 24.14)

`GET /api/v1/health`, same public tier as `/version` (no bearer token required) — for a load balancer/process supervisor to poll.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #15) — Missing Content-Type Validation on File Uploads
**Fixed in:** `engines/pim_media.go` (`validateMediaFile`)

Checks the extension allowlist AND sniffs actual file content via `http.DetectContentType` — a renamed executable (`virus.exe` → `virus.jpg`) is rejected because its sniffed content type won't match `image/*`, regardless of filename/extension.

**Status:** ✅ **FIXED**

---

### ✅ (was Medium #16) — Background Workers Not Gracefully Shutdown
**Fixed in:** `internal/server/routes.go` (Stage 24.15)

A single cancellable `context.Context` is threaded into every background worker at startup; SIGINT/SIGTERM cancels it and calls `srv.Shutdown` with a bounded 15s timeout, so in-flight requests finish and workers stop their current tick instead of being killed mid-flight.

**Status:** ✅ **FIXED**

---

### ✅ (was Low #17) — Debug Endpoint Left Enabled in Production
**Fixed in:** `internal/server/routes.go` (Stage 24.16)

`/api/v1/debug/panic` is only registered when `os.Getenv("ENV") != "production"`.

**Status:** ✅ **FIXED**

---

### ✅ (was Low #18) — Hardcoded Dev Credentials in Migration Script
**Fixed in:** `engines/auth.go` (`EnforceNoDefaultAdminCredentialInProduction`, Stage 24.27)

Checked at startup (`routes.go`'s `Run()`): refuses to start when `ENV=production` and `tenant_default`'s seed `admin` account still carries the exact hash `db/migration.sql` ships; logs a warning otherwise (expected in dev).

**Status:** ✅ **FIXED**

---

### ✅ (was Low #19) — No Connection Pool Monitoring
**Fixed in:** `internal/server/handlers_integrations_admin.go` (`handleHealth`)

`GET /api/v1/health` now returns a `db_pool` object (`max_open_connections`, `open_connections`, `in_use`, `idle`, `wait_count`, `wait_duration_ms`) straight from `sql.DB.Stats()` alongside the existing liveness check. No new dependency — `database/sql` already tracks all of this internally. (This session initially added a second, HR/Admin-gated endpoint for the same data before noticing `handleHealth` already covered it in this same working tree — removed to avoid two parallel ways of exposing the same stats, per this repo's own "reuse the existing choke point" convention.)

**Status:** ✅ **FIXED**

---

### ✅ (was Low #20) — Barcode Generation Not Cryptographically Secure
**Fixed in:** `engines/inventory.go` (`GenerateBarcode`, Stage 24.23)

Uses `crypto/rand` instead of a wall-clock-seeded `math/rand` source.

**Status:** ✅ **FIXED**

---

### ✅ (was Low #21) — No Circuit Breaker for External Integrations
**Fixed in:** `engines/connector_http.go` (Stage 24.29)

A small stdlib-only (`sync.Mutex` + `time`, no `gobreaker` dependency) circuit breaker keyed per platform (`shopify`/`bigcommerce`/`magento`), opens after 5 consecutive failures, self-resets (half-open trial) after a 30s cooldown. Wraps every outbound connector call in `doConnectorRequest`.

**Status:** ✅ **FIXED**

---

## RECOMMENDATIONS BY PRIORITY

### Remaining work

**None.** Both items below were closed on 2026-07-29 (Stage 29.8) once the user made the calls they were waiting on. Full detail in `micro_checklist.md` Stage 29.8 and `project_ledger.md` §67.

1. ~~**[needs design decision]** General per-doctype valid-transition map for non-approval-gated doctypes.~~ ✅ **CLOSED.** User chose opt-in-strict enforcement (fail-open unless a doctype is flagged) scoped to transactional lifecycles **and** masters. Built by extending the existing `StatusTransitionRule` master rather than adding a parallel mechanism: `entity` widened from a 4-value Select to code-validated free text, new `doctype_meta.strict_status_transitions` flag, enforcement attached to `ValidateTransactionalRules` (the shared choke point every generic-doc write already passes), rejections reusing the catalog's existing `GLOBAL-0019`. 178 rules seeded, 64 doctypes flagged strict. Masters' `Active`↔`Inactive` pairs and the approval-engine edges are generated from each doctype's live options rather than hand-listed — which is what caught two real bugs during verification (four doctypes had gained statuses since `migration.sql`, stranding a live PurchaseOrder; and a `SalesInvoice` sat in a status not in its own option set, which no rule could ever free).

2. ~~JWT token revocation list / key rotation — deferred by standing policy.~~ ✅ **CLOSED**, and the deferral turned out to be hiding a bigger gap than "revocation". `ParseToken` never touched the database, so a deactivated user kept full access for the rest of `JWT_EXPIRY_HOURS` (24h default) and a **demoted** user's token kept asserting the old role indefinitely. A jti denylist — the obvious fix — would have addressed the first and done nothing for the second. The user chose the live-state re-check instead: `apiMiddleware` re-reads the user row per request behind a 30s cache (`AUTH_STATE_CACHE_SECONDS`), making the database authoritative for status, role and location, with invalidation hooked into all three mutation sites. Separately, multi-key signing rotation (`JWT_SECRET_<n>`) now allows a planned key rotation with nobody logged out; verification tries every configured key rather than selecting by the token's own `kid`, since `kid` is attacker-controlled until the HMAC has already passed.

**Note this corrects an earlier claim in this document and in `QC_EXHAUSTIVE_REPORT.md` (R3.3):** "Deactivate Employee → login blocked ✅" was true but incomplete — it blocked *new* logins only. The live session survived until token expiry. That is now genuinely closed.

---

## POSITIVE FINDINGS

The codebase demonstrates several strong security and design patterns:

✅ **Schema-per-tenant isolation** — Strong multi-tenancy
✅ **Maker-checker approval workflow** — Prevents unauthorized actions
✅ **Double-entry bookkeeping** — Financial data integrity
✅ **Audit logging via DB triggers** — Tamper-evident audit trail
✅ **Account lockout after failed logins** — Brute-force protection
✅ **Rate limiting by API category** — DoS protection
✅ **CORS allowlist** — Prevents unauthorized cross-origin access
✅ **Security headers (CSP, HSTS)** — Browser-level protections
✅ **Panic recovery with alerting** — Operational visibility
✅ **Extension token scoping** — Principle of least privilege, now enforced defense-in-depth at the shared admin-gate choke point too
✅ **Location-based object filtering** — Data segregation
✅ **Soft deletes** — Data retention for compliance
✅ **Idempotency keys for financial postings** — Prevents duplicate entries
✅ **Optimistic locking** — Prevents lost updates
✅ **Password reset flow** — Self-service password recovery
✅ **Per-tenant concurrency limits** — Prevents noisy-tenant starvation
✅ **Schema name validation** — SQL injection defense-in-depth
✅ **Audit log checksums** — Tamper-evidence chain
✅ **Health check endpoint** — operability
✅ **Graceful shutdown** — no dropped in-flight requests/workers on deploy
✅ **Circuit breaker + token bucket on outbound connector calls** — resilient integrations
✅ **Content-sniffed file upload validation** — rejects disguised executables
✅ **Connection pool visibility** — live `sql.DB.Stats()` exposed to ops

---

## FIXED ISSUES SUMMARY

| # | Issue | Severity | Status | Stage |
|---|-------|----------|--------|-------|
| 1 | SQL Injection via Schema Names | 🔴 Critical | ✅ Fixed | 24.17 |
| 2 | Hardcoded Location Code | 🔴 Critical | ✅ Fixed | 24.1 |
| 3 | No Idempotency Protection | 🔴 Critical | ✅ Fixed | 24.5 |
| 4 | Accounting Period Backdating | 🔴 Critical | ✅ Fixed | 24.6 |
| 6 | Missing MFA Rate Limiting | 🟠 High | ✅ Fixed | 24.28 |
| 8 | No Request Timeout | 🟠 High | ✅ Fixed | 24.13 |
| 10 | Vendor Invoice Override | 🟠 High | ✅ Fixed | 24.11 |
| 13 | Audit Log Triggers Disabled | 🟠 High | ✅ Fixed | 24.24 |
| 16 | Inventory Reservation Race | 🟡 Medium | ✅ Fixed | 24.7 |
| 18 | Missing Index | 🟡 Medium | ✅ Fixed | 24.7 |
| 21 | No Optimistic Locking | 🟡 Medium | ✅ Fixed | 24.10 |
| 22 | Password Reset Missing | 🟡 Medium | ✅ Fixed | 24.28 |
| 32 | Single DB Pool | 🟡 Medium | ✅ Mitigated | 24.30 |
| — | Extension Token Scope (non-generic-doc handlers) | 🔴 Critical | ✅ Fixed (already closed by construction; +explicit guard 2026-07-26) | `requireHRAdmin` |
| — | Unsafe JSON Unmarshal (crash-risk + fail-open sites) | 🟠 High | ✅ Fixed | 24.18, 24.33 |
| — | Weak Custom JWT | 🟠 High | ⚠️ Mitigated (2 sub-gaps deferred) | 24.19, 24.22 |
| — | No Input Length Validation | 🟠 High | ✅ Fixed | 24.31 |
| — | CSV Dry-Run Transaction Isolation | 🟠 High | ✅ Mitigated | 24.32 |
| — | Timing Attack on Token Validation | 🟠 High | ✅ Fixed | 24.22 |
| — | Audit Log Pagination | 🟡 Medium | ✅ Fixed | 24.20 |
| — | Prefix Reset Frequency Validation | 🟡 Medium | ✅ Fixed | 24.21 |
| — | Approval Rule Range Overlap | 🟡 Medium | ✅ Fixed | 24.8 |
| — | GST Floating-Point Precision | 🟡 Medium | ✅ Mitigated | 24.9 |
| — | CSRF Protection | 🟡 Medium | ✅ Not Applicable | verified 2026-07-26 |
| — | Industry Profile Path Validation | 🟡 Medium | ✅ Fixed | 24.3 |
| — | Health Check Endpoint | 🟡 Medium | ✅ Fixed | 24.14 |
| — | Upload Content-Type Validation | 🟡 Medium | ✅ Fixed | pim_media.go |
| — | Graceful Worker Shutdown | 🟡 Medium | ✅ Fixed | 24.15 |
| — | Document Status Transitions (approval-engine bypass) | 🟡 Medium | ✅ Fixed | `handleGenericDoc`, 2026-07-27 |
| — | Debug Endpoint in Production | 🟢 Low | ✅ Fixed | 24.16 |
| — | Hardcoded Dev Credentials | 🟢 Low | ✅ Fixed | 24.27 |
| — | Connection Pool Monitoring | 🟢 Low | ✅ Fixed | `handleHealth` `db_pool` field |
| — | Barcode Not Cryptographically Secure | 🟢 Low | ✅ Fixed | 24.23 |
| — | No Circuit Breaker | 🟢 Low | ✅ Fixed | 24.29 |

**Total Fixed/Mitigated/N-A: 34 out of 34 originally identified issues addressed at the security-relevant level (100%)**

---

## QC VERIFICATION (2026-07-29)

A separate 3-round exhaustive QC was performed across all modules. See `docs/QC_EXHAUSTIVE_REPORT.md` for full detail.

**QC Results:**
- Round 1: 15/15 modules passed
- Round 2: 22/22 edge case scenarios passed
- Round 3: 12/12 cross-module integration flows passed
- **Defects found: 0**
- **Non-blocking observations: 2** (index optimization recommendation, production env documentation)

**Observations from QC:**
1. O1 — Trial balance query on gl_postings lacks composite index; FULL SCAN at 10M+ rows. Add `(account_code, created_at)` index post-launch.
2. O2 — Document sslmode=require in deploy/erp.env.example for production deployments.

These are performance/documentation observations, not defects.

## RE-EVALUATION (2026-08-20) — post-Stage 44 review

A fresh full-codebase review was performed against the current tree (now ~190 Go files, Stages 29-44 shipped since the 2026-07-29 QC). Findings below are based on **reading the actual code**, not inferred.

### Verified HARDENING (landed since 2026-07-29)

| Area | File | Evidence |
|---|---|---|
| Token claim-injection (delimiter smuggling) | `engines/auth.go` | `claimVal()` = `url.QueryEscape` on every claim; `SplitN(pair,"=",2)` parser; regression tests in `auth_claim_injection_test.go` ✅ |
| JWT signing-key rotation | `engines/auth.go` | `JWT_SECRET_<n>` ring, `kid` claim, verify-against-all-keys (kid never trust-bearing) ✅ |
| Live user-state re-check | `engines/auth_livestate.go` + `middleware.go` | Deactivation/demotion takes effect within 30s cache TTL; DB errors fail-retryable (503), not 401 ✅ |
| Collision-free document IDs | `engines/docid.go` | Atomic strict-increasing nanos + per-process crypto/rand suffix; for the UnixNano-PK-collision bug (durability audit #2) ✅ |
| Per-doctype status-transition map | `engines/status_transition.go` | Closes the residual half of old **Medium #9** (opt-in strict mode, reason-code requirement, legacy-debris escape hatch) ✅ |
| Per-tenant hostnames | `internal/server/tenant_host.go` | RFC-1123 label validation, reserved-label blocklist, host-vs-token tenant agreement check, unknown-host 404 gate, TLS-ask gate ✅ |
| Public API platform | `engines/public_api_credentials.go`, `public_api_runtime.go`, `middleware_public_api.go` | Scope allowlist, SHA-256 key hashing, constant-time compare, per-credential burst+daily quota, idempotency keys, traffic log ✅ |
| MFA recovery codes | `engines/mfa_recovery.go` | 10×50-bit codes, SHA-256 hashed, atomic redeem (`UPDATE ... WHERE used_at IS NULL`), no modulo bias (256%32=0) ✅ |
| Embedded migration runner | `db/migrate.go` + `migrate_test.go` | `go:embed` all .sql, per-file TX, ledger, numeric-aware ordering — supersedes broken `deploy/migrate.sh` ✅ |
| Proxy-safe client IP | `middleware.go` | Right-to-left X-Forwarded-For walk (Stage 43.3), trusted-CIDR server, session-fingerprint rate-limit keys ✅ |
| Frontend XSS mitigation (partial) | `public/app.js` | `escapeHTMLText()` now exists and is used in hundreds of interpolation sites (OMS console, PO composer, GRN workbench, gate/yard screens) ✅ |

### STILL OPEN (genuinely)

1. **🟢 Token revocation list / key-rotation operationalisation** — still deliberately deferred by standing policy (not a code gap; `jti` infrastructure is in place).
2. **🟡 CSP `'unsafe-inline'`** — the last third of the residual-XSS item below; requires refactoring the remaining `onclick=` attributes to `addEventListener` before it can be dropped.

### ✅ CLOSED 2026-08-21 (Stage 46) — was "STILL OPEN" above

1. **Money precision (durability audit #7)** — **FIXED.** Asked directly which way to close it (`BIGINT` paise migration vs. the `math.Round` intermediate); the user chose the full migration. `gl_postings.debit`/`.credit` are now `BIGINT` paise; `PostDoubleEntry` takes `map[string]int64`. The truncation is fixed at its actual source (`pos_checkout.go`'s per-unit price before the qty multiply, `PostSalesGSTBooking`'s GST split) via new `RupeesToPaise`/`PaiseToRupees` helpers, not just widened at the storage layer. External API/report contract deliberately unchanged — every boundary converts back to rupees — so no frontend change was needed. Verified against a from-scratch throwaway database: the audit's own ₹999/18%-GST example now posts ₹179.82 of tax instead of ₹178. **Not yet applied to the production database** — a deliberate separate step pending the user's go-ahead, since it mutates existing financial data in place. Full detail: `docs/micro_checklist.md` Stage 46, `docs/project_ledger.md` §109.
2. **Residual Stored-XSS, Item code/SKU half** — **FIXED.** `itemCodePattern` now exists in `engines/master_data_validation.go`'s `validateItemMasterRules`, rejecting HTML/JS metacharacters in an Item's `code` field — the exact gap this document previously confirmed was missing. The `'unsafe-inline'` half of this same finding remains open, listed above.

### Honest verdict

The system is materially stronger than at the 2026-07-29 QC, and stronger again after Stage 46: both items this document carried as genuinely open — money precision and the Item code/SKU charset gap — are now closed. **Two items remain open**, listed above, neither blocking on more analysis: `'unsafe-inline'` needs the `onclick=`→`addEventListener` refactor, and token revocation/key-rotation stays deliberately deferred by standing policy pending a real need.

**Conditional production readiness: YES for deployments that accept the residual `'unsafe-inline'` CSP surface; the integer-rupee accounting caveat from the 2026-08-20 revision no longer applies. Stage 46's migration deployed to production 2026-08-21 (commit `2575097`, see `micro_checklist.md` 46.7) — live `:8080` now runs the paise-precision ledger.**

---

*End of Analysis — Updated 2026-08-21*
