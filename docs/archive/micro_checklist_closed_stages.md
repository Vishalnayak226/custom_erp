# Micro-Checklist — Archived Closed Stages

Stages below are **fully closed and verified**. They were moved out of
`docs/micro_checklist.md` on 2026-08-01 to keep the live tracker small enough
to read cheaply. Nothing was changed or deleted — this is the verbatim record.

Contains: Stages 1-19, 21, 22, 24, 25, 27, 28, 28.5, 29, 29.7, 29.8.

Live tracker (open items only): **[docs/micro_checklist.md](../micro_checklist.md)**

---

## Stage 1 — Core Foundation ✅

- [x] **1.1 Base schema migrations** — `.gitignore`, folder structure, `doctype_meta`/`doctype_fields` registries, RBAC role/permission tables, `system_error_logs`, core doctype metadata (Item/PurchaseOrder/ASN/SalesInvoice/TransferOrder).
- [x] **1.2 Core engine logic**
  - Audit engine — DB triggers log every change (old/new value, user, time), append-only.
  - Panic handler middleware — captures crashes, writes stack traces to the log DB.
- [x] **1.3 API security & gateway foundation**
  - Rate limiting (5/min login, 60/min standard CRUD)
  - Strict tenant resolution from a verified JWT (no IDOR leaks)
  - Parameterized SQL everywhere; allowlisted query-filter keys
  - 2MB body cap + file size/MIME validation
  - CORS allowlist (no wildcard); note: CSRF-token/cookie controls don't apply here — auth is a Bearer token, never a cookie, so classic CSRF has no attack surface
  - Secrets via env vars, no hardcoded keys
  - Object-level authorization (role/location/ownership checks on every read/write)
- [x] **1.4 Base API endpoints** — generic doc CRUD (`GET/POST /api/v1/doc/:doctype[/:id]`), prefix config API, labels translation API, dynamic JSONB query filters, GRN stock-update hook.
- [x] **1.5 Omnichannel scale foundation** — `inventory_availability`/`inventory_reservation`/outbox/dead-letter tables; event-bus + outbox pattern; Available-to-Sell read model; reservation service.

---

## Stage 2 — Dynamic Configuration ✅

- [x] **2.1 Core schema builders** — DocType Builder UI, parent-child vocabulary aliasing, numbering engine (prefix/separator/padding/reset), dynamic label/translation engine.
- [x] **2.2 Dynamic form rendering** — meta-driven form generator (text/number/date/select/link/attachment/table/scan), vocabulary-aware rendering, field-rename UI, configurable list columns.

---

## Stage 3 — Master Packages ✅

- [x] **3.1 Industry presets** — Jewelry, F&B, Automobile, Clothing.
- [x] **3.2 Master configs** — Organization, Location, Item (parent/variant), Vendor, Customer, Employee, Tax, GL master schemas.
- [x] **3.3 Bulk uploads engine** — CSV structure check before processing, item-import validation (HSN/duplicates/category), row-level error export.

---

## Stage 4 — Procurement ✅ · Stage 5 — GRN & Inventory ✅ · Stage 6 — Warehouse & Transfer ✅

- [x] **4.1** `PurchaseOrder` metadata + status/cancellation tracking.
- [x] **5.1-5.2** GRN callback hooks, barcode generation, append-only availability read model, item returns.
- [x] **6.1** Location-scoped reservations; rule-driven picking + re-routing on reject.

---

## Stage 7 — POS & Sales ✅ · Stage 8 — Finance ✅

- [x] **7.1** `POSCart`/`SalesReturn` schemas; `POST /checkout`; Shopify order webhook with idempotency.
- [x] **8.1-8.2** `GLAccount`/`GLPosting` schemas, balanced double-entry engine, automated postings for GRN/checkout/returns, `GET /finance/trial-balance`.

---

## Stage 9 — Tax & Integrations ✅

- [x] **9.1 Channel syncs** — Shopify product/inventory/orders; Unicommerce, Pine Labs, CleverTap integrations (each: credentials, outbox-driven sync, background worker); Marketplace settlement reconciliation; logistics dispatch tracking.
- [x] **9.2 Error Logs Hub** — audit trail + panic backtrace viewer; Integration Payloads tab with Retry button; webhook signature verification (Shopify + generic `VerifyWebhookHMAC` for others — Unicommerce/Pine Labs/CleverTap are outbound-only, so it doesn't apply to them).

---

## Stage 10 — Reports & Dashboards ✅

- [x] **10.1** Replenishment suggestions, demand forecasting (status-mismatch bug fixed 2026-07-12 — was querying a status the system never writes), SLA-breach monitor, trial balance summary.

---

## Stage 11 — QA & Go-Live ✅ (2026-07-21)

- [x] **11.1 Test coverage** — 100-worker/2,000-node concurrency stress test; real HTTP-level integration test (login→checkout→forecast); schema/migration validation.
  - **Cross-tenant security re-verified (2026-07-21)**: new `TestCrossTenantIsolationAndTokenSecurity` proves tenant isolation holds even against a spoofed `X-Tenant-ID` header, and that a missing/malformed/tampered token is always rejected.
  - **Bug found & fixed**: `ProvisionTenantSchema` never learned about the `field_permissions` table (added later, Stage 16.7) — every new tenant was missing it, causing a 500 on every single-doc read. Added to the clone/seed lists.

---

## Stage 12 — Multi-Industry Scale ✅ (2026-07-21)

- [x] **12.1 Multi-tenant SaaS ops** — automatic provisioning (unique per-tenant admin credential); feature-flag controls (setter only — enforcement closed later, Stage 13.2).
  - **All 10 industry templates now live** (Pharma/Metal/Construction/Medical/Semiconductor/Agriculture added to the original 4) — the profile files already existed, only the backend allowlist was missing them.
- [x] **12.2 IP & binary safety** — frontend minified/obfuscated, no sourcemap leak; release binaries stripped (9,446 KB → 6,602 KB).
  - Automated backups + encryption + monthly restore drills — **done** (Stage 17.3 backup/restore baseline + AES-256 encryption-at-rest, commit `99d63de`). This line stayed marked "re-opened" long after both gaps had actually closed.

---

## Stage 13 — Master Blueprint Functional Scope ✅ (2026-07-13 → 17)

Closed the gap between the strong backend/kernel (Stages 1-12) and an actually usable business app. Grouped into 5 phases by risk/effort; every item shipped as its own commit, live-verified on a throwaway instance first.

### Phase A — Security
- [x] **13.1 Security response headers** — HSTS/CSP/X-Frame-Options/X-Content-Type-Options via one `securityHeaders` middleware wrapping the whole mux.
- [x] **13.2 Feature-flag route gating** — new `featureGate()` wrapper; wired to the 3 seeded flags (oms/wms/forecasting). Fails closed if a flag can't be confirmed enabled.
- [x] **13.3 MFA (TOTP)** — stdlib-only (`crypto/hmac`+`sha1`+`base32`), required for HR/Admin. Login routes an MFA-required role into enrollment or challenge via a short-lived purpose token.
  - **Bug fixed**: the purpose token initially didn't carry `tenantID`, silently blanking tenant scoping for the whole MFA flow.

### Phase B — Frontend for existing backend logic
- [x] **13.4 POS billing screen** — pure frontend against the already-working availability/checkout APIs.
- [x] **13.5 Finance/GL screen** — read-only trial balance view.
- [x] **13.6 Fulfillment/reservation workbench** — status-driven task actions.
  - **Bug fixed**: the location filter only checked a field literally named `location`; `FulfillmentTask` actually stores `location_code` — non-admins got zero results. Fixed with a `COALESCE`.
- [x] **13.7 Marketplace settlement screen** — settlements + logistics bookings.
  - **Bug fixed (deeper case of 13.6's bug)**: doctypes with *no* location field at all (`MarketplaceSettlement`/`LogisticsBooking`) were also silently hidden from non-admins, since `COALESCE(NULL,NULL) = 'HO'` is never true in SQL. Fixed to also allow the `IS NULL` case through.

### Phase C — The structural gap
- [x] **13.8 Maker-checker approval engine** — `engines/approval.go`, amount-slab → role routing (PurchaseOrder pilot: <50k Store Manager, 50k+ HR/Admin). Enforces maker≠checker, role match, and location match on every decision. Reused by nearly every later approval-gated feature.
  - Re-approval-on-edit: editing an Approved document resets it to Pending Approval automatically.

### Phase D — Functional module breadth
- [x] **13.9 Vendor/Customer masters** — registered as real Master doctypes, full generic CRUD for free.
- [x] **13.10 GST/Tax engine** — CGST/SGST/IGST split calculation (calc-only; e-invoice/IRN explicitly out of scope, no GSP credentials).
- [x] **13.11 Report catalog** — Current Stock, Sales Register, Vendor Ledger, Payables Ageing.
- [x] **13.12 RFQ / vendor quote comparison** — quote comparison list + one-transaction winner selection (auto-rejects the rest, closes the RFQ).
- [x] **13.13a HR Foundation** — Employee/Attendance/Leave, access-link sync (deactivating an Employee blocks their linked login), payroll export.
  - **Bug fixed**: Leave approve/reject buttons sent a partial payload to an update endpoint that replaces the whole document — every approval failed validation. Fixed to resubmit the full record.
- [x] **13.13b Fixed Asset Management** — capitalize → depreciate (straight-line, calculated live not stored) → transfer → dispose, posting real balanced GL entries.
  - **Bug fixed**: a disposed asset's register still showed full original cost as net block instead of zero.
- [x] **13.13c Expense Management** — claim → verify → pay, reusing the approval engine. Date-window + duplicate-bill checks on create.
  - **Bug fixed**: Cashier had create-but-not-update access, so a Cashier who filed their own claim had no way to submit it.
- [x] **13.13d CRM/Loyalty (scoped MVP)** — append-only point ledger, earn at checkout / burn via a separate redeem endpoint. No campaigns/segmentation/vouchers.
- [x] **13.13e Manufacturing (scoped MVP)** — single-level BOM + linear Production Order (Draft → Material Issued → Completed), reuses the existing inventory floor-check. No routing/MRP/QC.

### Phase E — Ops/scale hardening
- [x] **13.14 Per-category rate limiting** — keyed `ip:category` (login/bulk-upload/report/webhook/search/default) instead of just `ip`, fixing cross-endpoint interference.
- [x] **13.15 Sticker/barcode printing** — text labels (not scannable symbology — unverifiable without a physical scanner), print history log.

Full item-by-item verification detail for 13.1-13.15 lives in this file's git history — condensed here per this repo's documentation cleanup pass (2026-07-23).

---

## Stage 14 — Control Plane, Governance, Patch Automation, Extension Isolation, Security ✅ (2026-07-17/18, Phases A-F; G/Docker reverted)

- [x] **14.1-14.5 Module registry + per-tenant entitlements** — `public.modules` catalog (13 modules, core ones un-disableable) + `tenant_default.module_entitlements`; `moduleGate()` wraps HR/Assets/Expenses/Loyalty/Manufacturing/RFQ/Stickers/Reports route groups.
- [x] **14.6-14.8 App versioning** — embedded `VERSION` file, `git_commit`/`build_time` via `-ldflags`, public `GET /api/v1/version`, per-tenant version stamping at provisioning.
- [x] **14.9-14.12 Dev/test/live pipeline** — 3 databases on one Postgres instance (not 3 clusters — cheaper, still fully isolated); `PORT` env var support; `environments.json` + `manage.ps1 -Env`; `promote.ps1` (build/test gate → git-worktree checkout → migrate → stripped binary → restart → `public.deployments` record) with `-Rollback`.
  - **Bug fixed**: `fleet-status`'s deployment-history lookup only ran when an environment was currently up, defeating the point of an audit trail.
- [x] **14.13-14.16 Patch/bug-intake automation** — daily worker classifies `system_error_logs` via deterministic regex rules (no AI/LLM code generation anywhere). Worker only ever writes to 2 audit tables — it can never mutate tenant/business state, by construction, in any environment.
- [x] **14.17-14.20 3rd-party extension isolation** — out-of-process HTTP webhook hooks (`document.before_save`/`after_save`) instead of Go's `plugin` package (same-crash-domain risk). Synchronous before-save with goroutine+timeout+recover so a hanging/panicking hook can neither crash nor block the server — live-verified with a real concurrent request during a hung hook call.
- [x] **14.21-14.24 Security hardening** — CI wired to apply all Stage 14 migrations (was silently testing pre-Stage-14 schema); `govulncheck` baseline (30 reachable CVEs, all in the Go stdlib, none in this project's own 2 dependencies); account-level brute-force lockout (10 attempts / 15 min).
  - **Bug fixed**: lockout expiry compared a Postgres UTC timestamp against Go's local (IST) clock — a genuinely-expired lock looked ~5.5 hours in the future. Fixed by doing the whole comparison in SQL against Postgres's own clock.
  - Inbound Shopify webhook HMAC signature verification added.
- [x] **14.25 Containerization** — built, then **reverted** on explicit request (standing no-Docker policy). `promote.ps1`/`manage.ps1 -Env` remains the one real deployment path.
- [x] **Go toolchain upgrade** — 1.22.5 → 1.22.12 on explicit request; 30 → 27 reachable CVEs (remaining 27 need a 1.23+ major version jump).

> **Cross-package test note**: `go test ./...` runs `custom_erp`/`custom_erp/engines` concurrently against the same shared schema, which can occasionally false-fail `TestEngines/FinanceDoubleEntryAndPOS`. Use `go test ./... -p 1` to serialize and confirm it's not a real regression.

---

## Stage 15 — PIM Foundation MVP ✅ (2026-07-17/18)

Media Library and Channel Publishing explicitly deferred to 15.2 (needed genuinely new file-upload infra this codebase didn't have yet).

- [x] **15.1 PIM Foundation** — Family/Attribute framework as 4 new doctypes (not custom fields bolted onto Item); parent/variant grouping via 3 new optional Item fields; `ProductContent` reuses the existing approval engine (zero new approval code).
- [x] **15.2 PIM V2: Media Library, Channel Publishing, Import/Export** — locale/channel-aware completeness; `PIMProductProfile` 8-state lifecycle; real Media Library (content-addressed, MIME-sniffed, auth-gated retrieval — this codebase's first file-upload infra); Channel Publishing queue (stub connector at this point — real connectors came in Stage 16); job-tracked CSV import/export preview.
  - **Bug fixed**: `PIMProductProfile`'s id collided with its own Item's id (both doctypes share one `documents.id` space) — profiles silently failed to create. Fixed with a composite id.
  - **Bug fixed (pre-existing)**: bulk import read `Resolved-Role` instead of `Resolved-User-ID` as `created_by`, so every bulk-imported row failed its FK constraint silently.

---

## Stage 16 — Real E-Commerce Connectors + Remaining PIM Gaps ✅ (2026-07-18 → 21)

- [x] **16.1 Connector framework** — `AES-256-GCM` credential encryption; `ChannelConnector` interface with a stub fallback (zero breaking change to Stage 15.2); safe outbound HTTP (timeout+goroutine+recover, mirroring the extension-hook pattern).
  - **Bug fixed**: the publish-queue status update reused one SQL placeholder in two incompatible contexts, silently failing on every attempt — a job never left `Queued` and was "re-published" every tick with no end condition (caught via 9+ duplicate log rows for one job).
- [x] **16.2 Shopify connector** — GraphQL Admin API, 3-step staged media upload. Code-complete; live-store verification deferred pending a real dev-store credential.
- [x] **16.3 BigCommerce connector** — REST v3 Catalog + inbound webhook (HMAC-verified). Code-complete, same deferral.
- [x] **16.4 Magento/Adobe Commerce connector** — REST `/V1/products`, EAV-style attributes; polling worker for Magento Open Source (no native webhooks). Code-complete, same deferral.
  - All 3 connectors covered by `connector_platforms_test.go` against `httptest` fakes — no live platform touched.
- [x] **16.5a-c PIM dashboard** — 8-counter catalog-health snapshot (incomplete profiles, pending approvals, publish queue state, etc.), backend + frontend.
- [x] **16.6 PIM reports** — content-aging, duplicate-media, channel-mapping-gap, attribute-quality.
- [x] **16.7 Field-level permissions** — `field_permissions` table, default-allow; generic reads/writes now respect it (e.g. Cashier can't see Item cost/GST).
- [x] **16.8 Stage 16 close-out** — full verification pass; commit deliberately left for whoever commits next, since this shared tree had 2+ concurrent sessions actively committing to it at the time.

---

## Stage 17 — Controlled Post-PIM Execution Queue ✅ (2026-07-19)

- [x] **17.1 Soft delete** — additive `deleted_at` tombstone; masters reactivatable; Approved transactional docs can't be deleted.
- [x] **17.2 CSV formula-injection protection** — shared `sanitizeCSVCell`, applied to every import/export/error-file path.
- [x] **17.3 Backup/restore baseline** — `manage.ps1 backup`/`restore`, SHA-256 sidecars, a real restore drill performed and verified.
- [x] **17.4 Accounting-period control** — Open/Closed periods; `PostDoubleEntry` rejects new postings inside a Closed period.
- [x] **17.5 GST posting enforcement** — HSN/rate gate at checkout and PO creation; tax-inclusive breakdown auto-computed, posted to dedicated GST GL accounts.
- [x] **17.6 Transfer-order dispatch/receive** — new `in_transit` inventory state; short-receive records a variance instead of silently reconciling.
- [x] **17.7 Purchase requisition workflow** — reuses the approval engine; one-time Approved→Draft-RFQ/PO conversion.
- [x] **17.8 Vendor invoice + 3-way match + payment** — DB-level duplicate-invoice constraint; match compares PO/GRN/invoice amounts within a tolerance; payment posts GL before flipping status.
- [x] **17.9 Location & organizational masters** — Location/LegalEntity/Department/CostCenter masters; existing free-text location fields kept as-is (validated against active masters, not migrated to Link fields) — 103 legacy codes seeded so validation never breaks existing data.
- [x] **17.10 Runbook + alerting** — P0-P3 incident runbook; Slack/Teams alerting (safe no-op when unset). **Still needs**: real escalation contacts + a real webhook URL.
- [x] **17.11 Live connector verification tooling** — script + docs for a real end-to-end platform publish. **Still needs**: real dev-store credentials.

---

## Stage 18 — Data-Entry UX: Dropdowns/Autosuggest ✅ (2026-07-19, fully closed 2026-07-25)

- [x] **18.1 Typeahead audit + build.** Chart of Accounts/GRN screens don't exist as buildable targets — scoped instead to real free-text fields. Stayed UI-only (no schema/validation change), matching Stage 17.9's own precedent.
  - New `attachTypeahead()` component, backed by the existing generic search endpoint.

  | View | Field(s) | Suggests from |
  |---|---|---|
  | Purchase Orders | Vendor, Warehouse, Location | Vendor, Location |
  | POS | Location, Customer, SKU | Location, Customer, Item |
  | RFQ | Vendor | Vendor |
  | HR Attendance | Location | Location |
  | Assets | Location, Custodian | Location, Employee |
  | Expenses | Location | Location |
  | Manufacturing | Item, Location | Item, Location |
  | Marketplace | Channel | Channel (fixed a hardcoded Shopify/Amazon-only dropdown) |

  - **Bug fixed**: `openMenu()` called `closeMenu()` to clear stale results, which also wiped the just-fetched results — the menu could never render.
  - **Bug fixed (pre-existing, Stage 17.9)**: Location/LegalEntity/Department/CostCenter were gated to a `'core'` module never registered anywhere — every role got 403 on every read/write since Stage 17.9 shipped.
  - **Gap flagged, not fixed**: Store Manager/Cashier lack read access to Vendor/Item/Customer, so the pickers are code-correct but not usable by those roles.
  - Verified live via Playwright against the real dev DB.
- [x] **18.2 Data-Entry Integrity (Relational Constraints)** — **DONE (2026-07-25), closed together with 24.37 below (same root cause).** See 24.37 for detail.

---

## Stage 19 — Docs Suite, Folder Restructuring, `ERP_BLUEPRINT.md` ✅ (2026-07-19)

- [x] **19.1 `docs/ERP_BLUEPRINT.md`** — full project snapshot for an outside/AI reviewer, every claim cited back to a source doc.
- [x] **19.2 Go folder restructuring** — `main.go` (~4,681 lines) moved into `internal/server/`, split into 8 domain files; new thin `cmd/server/main.go`. Same-package move — zero cross-package visibility risk. Every build command updated and re-verified.
- [x] **19.3 Root clutter cleanup** — old one-off scripts moved to `scripts/archive/` (not deleted, per explicit request).
- [x] **19.4 `docs/` reorganization** — new `architecture/`/`specs/`/`operations/`/`requirements/`/`guides/` subfolders. Nothing deleted; every cross-reference fixed. New `docs/README.md` index.
- [x] **19.5 New requirement/guide docs** — `BRD.md`/`PRD.md`, `USER_GUIDE.md`/`ADMIN_GUIDE.md`, grounded in 3 external planning references. Built-vs-specified marked explicitly per module.

---

## Stage 21 — UI/UX Overhaul ✅ (2026-07-20, fully closed 2026-07-25)

- [x] **21.1** Scroll + frozen header app-wide — one shared CSS rule (`.table-panel { overflow: hidden }` → `auto`) fixed nearly every list/table screen at once. Also fixed 2 more CSS bugs found in the same pass (unstyled KPI numbers, dead icon rules).
- [x] **21.2** Killed the last 4 raw `window.prompt()` calls.
- [x] **21.3** Icon-minimal sidebar (26 per-item icons removed, user's explicit choice).
- [x] **21.4** Account-menu + sign-out redesign.
- [x] **21.5** New self-service User Profile screen.
- [x] **21.6** Personal idle-timeout auto-logout.
- [x] **21.7** Sidebar scroll-position bug (found live) — fixed to center the active item instead of snapping to an edge.
- [x] **21.8** PIM's tabs were discarding the whole shell on navigation — extracted a reusable shell header.
- [x] **21.9 Full User/Admin SOP docs** — **DONE (2026-07-22).** Literal click-by-click procedures for every screen.
  - **Real gap found and fixed**: every generic record list had Delete but no Edit action at all. Backend already supported it — added an Edit button + optimistic-locking conflict handling.
- [x] **21.10** Grouped Database Schema Design's master list by module → later converted to a hover-flyout on user follow-up.
- [x] **21.11 Standard-ERP module renaming** — **DONE (2026-07-22).** Odoo/ERPNext-style naming for the ~12 top-level sidebar labels (POS→Point of Sale, Finance→Accounting, etc.), individual screen names left alone.
- [x] **21.12** Swept remaining "DocType" UI text to "Record Type"; deleted a confirmed-dead modal.
- [x] **21.13** "Stores" master fix — 3 compounding bugs: zero fields, zero role_permissions, a singular/plural sidebar typo.
- [x] **21.14** The "Vanilla JS" Ceiling & Ultra-Fast UI — **Foundation built and live-verified (2026-07-25)**, deliberately scoped as a seed (a handful of high-value pieces, per user discussion), not a full migration of all 41 `render*View` functions. 1) **Web Components**: `public/components/erp-typeahead.js`, this codebase's first (`customElements.define`), a Shadow-DOM-encapsulated drop-in replacement for `attachTypeahead()` (same debounce/keyboard-nav/search behavior, `.value` getter/setter so existing call sites need no changes) — ships its own scoped stylesheet via `adoptedStyleSheets` built from the same `var(--token)` design tokens (CSS custom properties inherit through shadow boundaries even though rule selectors don't), so it's visually identical with zero global CSS duplication. Wired into one real call site (Purchase Orders' Vendor field) as live proof; `attachTypeahead()` itself is untouched and still used by the other ~14 call sites — a template for incremental migration, not a break-everything rewrite. 2) **SWR caching**: `swrFetch()`/`swrCacheGet`/`swrCacheSet` (sessionStorage-backed, per-tab) wired into `renderDocTableView` — the one shared choke point ~15 sidebar screens (Vendors, Stores, Bins, POS Profiles, Purchase Requisitions, Offline Sync Review, ...) already funnel through. A cached visit renders instantly with zero network wait; a background revalidation silently re-renders only if the data actually changed. Cold start (nothing cached) falls back to the exact original await-then-render path unchanged. **Live-verified via Playwright** (real browser, throwaway port, zero console/page errors): typed into the Web Component, got real suggestions, picked one, value landed correctly; navigated to a cached doctype-table screen and confirmed the row count rendered on the very next tick with no network wait. `node --check`/`go build`/`go vet`/`go test ./... -p 1` clean.

---

## Stage 22 — Full Page-by-Page QA Sweep ✅ (2026-07-20)

- [x] **22.1** Enumerated and live-tested every page — found Inventory/Transfers/Users/Roles were dead "Module Setup Pending" mocks (pre-existing).
- [x] **22.2** Literal walkthrough of the guide docs' own steps — found the underlying "add a master record" mechanism works fine, the guides just never documented it.
- [x] **22.3** Fixed all 4 dead screens for real.
  - **Bug fixed**: the generic update path replaces (not merges) the whole JSONB blob — a naive status-only edit would have wiped other fields.
  - **Bug fixed**: a duplicate-username create leaked a raw Postgres constraint error to the UI.
- [x] **22.4** Updated the guide docs with the walkthroughs that never existed, plus an honest correction that the sidebar isn't actually role-filtered (flagged as 22.6).
- [x] **22.5** Final rebuild/vet/test + live re-verification; all QA test data removed.
- [x] **22.6 Sidebar role-filtering** — **DONE (2026-07-23).** Decision: derive automatically from `role_permissions`, not a hardcoded allowlist, so a newly-created role is reflected with no code change.

---

## Stage 24 — Security & Loopholes Hardening ✅ (34 of 34 items done — 24.35-24.38 closed 2026-07-25, 24.33/24.34 closed 2026-07-26, see addendum)

User supplied a 34-finding AI code review. Every finding re-verified against live source first — 4 were already fixed by later stages, 2 turned out worse than described (zero role check at all, not just a narrower gap).

> **Status correction (2026-07-22)**: this section read "Not Started" even though all 24 core items were already implemented and committed (`99d63de`) by a concurrent session — the checklist was simply never flipped. Re-verified every item individually against live source below.

### Critical — Access control & auth
- [x] **24.1** Real per-user location code in the JWT (was hardcoded to `"HO"` for everyone).
- [x] **24.2** `handleLabels`/`handleSequence`/`handlePrefix` had zero role check at all — gated behind `requireHRAdmin`.
- [x] **24.3** `handleSwitchIndustry` had zero role check + an unsanitized file-path build from client input — gated + allowlisted.
- [x] **24.4** MFA brute-force rate limiting — audited, already satisfied.

### Critical/High — Data integrity & concurrency
- [x] **24.5** Idempotency keys for financial postings.
- [x] **24.6** Accounting-period check now validates the document's own transaction date, not just today's date.
- [x] **24.7** Row-lock + missing index on reservation creation.
- [x] **24.8** Approval-rule overlap validation at save time.
- [x] **24.9** GST rounding precision (stdlib `math.Round`, not a new decimal dependency).
- [x] **24.10** Optimistic locking (`version` column) on the generic document engine.
- [x] **24.11** Vendor invoice override now routes through the approval engine instead of a bare reason string.
- [x] **24.12** Soft-delete status-transition validation — audited, already satisfied.

### Operational hardening
- [x] **24.13** DB pool tuning + HTTP server timeouts.
- [x] **24.14** Health check endpoint.
- [x] **24.15** Graceful shutdown on SIGTERM/SIGINT.
- [x] **24.16** Debug panic endpoint gated to non-production.

### Defense-in-depth
- [x] **24.17** Schema-name allowlist before SQL interpolation.
- [x] **24.18** Stop silently swallowing JSON unmarshal errors — down from 27 occurrences to 1 safe one (left alone, file was mid-edit by a concurrent session).
- [x] **24.19** JWT gained `iat`/`jti` claims (stdlib only, not a `golang-jwt` dependency).
- [x] **24.20** Pagination on the audit logs endpoint.
- [x] **24.21** `reset_frequency` validated against an allowlist.
- [x] **24.22** `ParseToken` collapsed to one generic error message.
- [x] **24.23** `crypto/rand` for barcode generation (was `math/rand`).
- [x] **24.24** Audit log tamper-evidence (hash chain).
- [x] **24.25-24.26** Upload content-type validation + CSRF — audited, already satisfied/not applicable.

### Built on explicit request, despite being scoped as deferred/needs-decision
- [x] **24.27** Startup guard refuses to boot in `ENV=production` while the default admin seed credential is still active.
- [x] **24.28** Full password-reset flow via stdlib `net/smtp` — no enumeration oracle, token hash stored not raw.
- [x] **24.29** Stdlib-only circuit breaker for connector calls (5 failures → 30s open → 1 half-open trial).
- [x] **24.30** Per-tenant concurrent-request cap (15), instead of a full per-tenant DB pool.
- [x] **24.31** Field `max_length` check wired into the one shared validation path (10,000-char default).
- [x] **24.32** `BulkImportCSV` now batches 500 rows per transaction instead of one giant transaction.

**Verified**: `go build`/`go vet` clean; `go test ./... -p 1` clean except the long-standing `TestEngines/FinanceDoubleEntryAndPOS` regression (confirmed pre-existing via `git stash`) and a DB-state-drift `TestEngines/DocTypeValidationAndAuth` failure (an unrelated `Brand` doctype picked up extra mandatory fields from earlier industry-profile testing on the shared dev DB).

### Addendum (2026-07-24): cross-checked against `docs/ERP_LOOPHOLES_ANALYSIS.md`
That doc (dated 2026-07-24, i.e. *after* this Stage closed) lists 21 issues as "still open." Re-verified every one against live source rather than trusted as-is, same discipline as the rest of this Stage — **19 of the 21 are stale**, already closed by the items above (its Critical #1 by 24.2's role-gate — extension tokens carry no `role` claim at all per `SignExtensionToken`, so `requireHRAdmin` already rejects them; High #2/#6, Medium #7/#8/#9/#10/#13/#14/#15/#16, Low #17/#20/#21 each map directly onto an `[x]` item above; Medium #9 "no status-transition validation" is additionally covered by Stage 25 Batch 3's `ValidateTransactionalRules`, built after that doc's original 2026-07-20 baseline). High #5 (CSV dry-run transaction isolation) is also moot — 24.32's 500-row batching already bounds how long any one transaction is held, which addresses the actual concern (lock contention) more directly than the doc's suggested `READ UNCOMMITTED` fix would have. Low #18 (hardcoded dev seed credentials in `db/migration.sql`) is intentionally unchanged — they're dev-setup fixtures every documented local-setup flow depends on; 24.27's boot-guard is the correct mitigation, not removal.

**Two genuine gaps found**, not covered by anything above — both closed 2026-07-26:
- [x] **24.33** `_ = json.Unmarshal(...)` (ignored error) has crept back into production code since 24.18 — 5 current occurrences, two with real fail-open risk: `engines/transactional_validation.go:85` (malformed `PurchaseOrder.items` silently computes zero ordered-qty, which could let an over-receipt check pass incorrectly) and `internal/server/handlers_core_doc_engine.go:449` (malformed prior document data silently feeds `ValidateTransactionalRules` an empty prior-state, which could let an invalid status transition through). **DONE.** `fetchPOItemQuantities` now returns the real unmarshal error instead of a silently-empty map; its two callers (`validateGRNRules`'s PURCHA-0082/84/87/88 checks, `validateASNRules`'s ASN-0271 check) now distinguish `sql.ErrNoRows` (PO genuinely not found/no lines — still a legitimate skip) from any other error (now fails closed, rejecting the write) instead of collapsing both into the same silent skip. `handlers_core_doc_engine.go`'s prior-document-data unmarshal now returns a 500 instead of silently continuing with `priorData == nil`, which every edit-gate check downstream (`validatePurchaseOrderEditRules` etc.) would otherwise treat as "this is a create, nothing to gate." `engines/transactional_validation.go:212` and `engines/pim_content_versions.go:67` (lower-risk) and `internal/server/handlers_pim_pos_finance.go:130` (not a real risk) left as-is, per the original scoping. `go build`/`go vet`/`go test ./... -p 1` clean.
- [x] **24.34** No connection pool monitoring/metrics exists anywhere (confirmed via grep — no `db.Stats()` call, no `/metrics`/pool-stats endpoint). 24.13 tuned the pool's bounds but exposes no visibility into it. **DONE.** `handleHealth` (`internal/server/handlers_integrations_admin.go`) now also returns a `db_pool` object (`db.DB.Stats()`: max/open/in-use/idle connections, wait count, wait duration ms) alongside the existing liveness check — no new dependency, no new endpoint. Live-verified via curl against a scratch server and used directly as the "DB-utilization" figure in 26.11.2's own load-test run below.

### Addendum (2026-07-25): Code Audits & Architecture Loopholes — all 4 closed 2026-07-25
- [x] **24.35** Inbound webhook fail-open risk — **DONE.** `verifyShopifyWebhookSignature` (`internal/server/middleware.go`) now fails **closed**: an unset `SHOPIFY_WEBHOOK_SECRET` rejects the request (still logs a one-time warning) instead of skipping verification, matching `engines.VerifyWebhookHMAC`'s (BigCommerce) existing fail-closed posture. No test/script relied on the old fail-open behavior (grepped first).
- [x] **24.36** Offline POS Idempotency/traceability risk — **DONE**, with an honestly-scoped residual limit. Root cause: the offline queue lived only in `localStorage`, so a gap between what was queued and what synced left zero server-side trace. Added a best-effort heartbeat (`sendOfflineQueueHeartbeat()` in `app.js`, fired on every queue mutation plus `visibilitychange`/`pagehide`) POSTing currently-queued `cart_number`s to a new `POST /api/v1/pos/offline-heartbeat`, stored in a new `pos_offline_heartbeats` table (`engines.RecordOfflineHeartbeat`). `ClosePOSSession` now diffs the last heartbeat against what actually synced (`detectOfflineQueueGap`) and, on a mismatch, writes a new browsable `POSOfflineQueueGap` doctype record (HR/Admin + Store Manager read, new "Offline Queue Gaps" sidebar entry) plus an audit-log entry and a `DispatchNotification` alert — shown to the closing cashier too, not just reviewers. **Residual, stated honestly**: a device with truly zero connectivity from the moment an item is queued until storage is wiped can still leave no trace — no client-server architecture can close that specific case, since the server was never contacted at all. This converts "always untraceable" into "untraceable only in that one narrow window," a materially real improvement. Live-verified end-to-end over real HTTP: heartbeat → never-synced-cart → gap correctly detected/recorded/logged on close; a second run confirmed no false positive shape (an actually-synced cart produces no gap check failure in the underlying `EXISTS` logic). Migration: `db/migrations_stage24_addendum_offline_heartbeat.sql`, applied to `tenant_default` + `tenant_new_schema`.
- [x] **24.37** Stage 18 Gap (Free-text fields) — **DONE, together with 18.2.** Two compounding causes, both fixed: (1) **Role permissions** — Store Manager/Cashier had zero `role_permissions` rows for Vendor/Item/Customer, so the existing typeahead pickers 403'd for exactly the roles meant to use them (Cashier's own POS Customer picker included). Added read-only grants (Store Manager: Vendor/Item/Customer; Cashier: Item/Customer), same shape as the existing Store Manager/Location grant. (2) **Actual constraints** — the generic `Link` fieldtype mechanism (`engines/doctype.go ValidateDocument`'s existence check) already existed and is already used by ~25 other fields, it just hadn't been applied to the specific fields Stage 18.1 wired a typeahead onto: `PurchaseOrder.vendor`, `VendorQuote.vendor` → `Link(Vendor)`; `BOM.parent_item` → `Link(Item)`; `Asset.custodian` → `Link(Employee)`. Verified safe before converting: checked every existing value in the live dev DB against its target doctype first (found and backfilled one real orphan — a pre-existing `PurchaseOrder` with free-text vendor `"Nike Corp"` and no matching Vendor record — before enforcing, so the conversion doesn't retroactively break an already-legitimate document). Location fields (`Attendance`/`Asset`/`ExpenseClaim`/`ProductionOrder`'s own "Location" pickers) reuse the existing Stage 17.9 `ValidateLocationReference` choke point instead (widened its `locationFieldsByDoctype` map) rather than converting fieldtype, since that dedicated mechanism already existed for exactly this. **Bonus bug fixed along the way**: the generic dynamic-modal's `Link`-type `<select>` (`app.js`, `openDynamicModal`) was building its option `value` from `item.name` instead of `item.id` — silently wrong (and the existence check would always fail) for any target doctype where name ≠ id/code (Vendor/Customer/Location/Item all qualify). Migration: `db/migrations_stage24_addendum_data_integrity.sql`, applied to `tenant_default` + `tenant_new_schema`. Live-verified: role grants confirmed live, fieldtypes confirmed live, backfilled Vendor record confirmed live. `go test ./... -p 1` clean (no test relied on the pre-fix free-text behavior).
- [x] **24.38** MFA Physical bypass — **DONE**, honestly scoped: host-shell access is an inherent, unpreventable privilege (raw SQL is always possible with DB access) — this closes the *silent/blanket/untraceable* part, not physical-access-implies-trust itself. `cmd/reset_mfa` rewritten: now requires `-schema`, `-user` (a specific username — `-all-admins` is an explicit opt-in for the old blanket behavior) and `-reason`; defaults to a dry run (`-yes` required to actually execute); writes a real `audit_logs` entry per reset with the same tamper-evident checksum chain (`auditChecksum`, mirroring `engines.LogAuditEvent`'s exact formula) every other MFA action leaves, capturing the OS username/hostname of whoever ran it in `details`.

### Addendum (2026-07-26): `docs/ERP_LOOPHOLES_ANALYSIS.md` itself brought up to date
The 2026-07-24 addendum above cross-checked that doc's content and found 19/21 of its "still open" items already stale (fixed elsewhere), but never edited the doc itself — it was still telling a reader "21 open issues" as of this morning. Rewrote it end-to-end: every item re-verified against current source with a file/function citation, the 19 stale ones moved to a new "Verified Fixed/Mitigated" section, Medium #9 (status transitions) kept explicitly open with a `[needs design decision]` tag (per-doctype valid-transition map is a business call, not an engineering one), and Medium #12 (CSRF) reclassified as **Not Applicable** rather than open/fixed — verified by grepping for `http.Cookie`/`SetCookie` (zero hits): this app is Bearer-token-only, so there's no ambient credential for CSRF to ride on.
- [x] Added one explicit defense-in-depth line to `requireHRAdmin` (`internal/server/handlers_admin_identity.go`) rejecting `Resolved-Purpose == "extension"` outright — not a fix for a live bypass (confirmed none exists, matching this file's own 2026-07-24 note), just closing the same gap at the shared choke point instead of relying solely on "extension tokens happen to carry no role claim." Live-verified against a scratch instance: extension token → 403 on `POST /api/v1/prefix`; HR/Admin token → unaffected.
- Initially also re-tightened `engines/transactional_validation.go`'s `poData` unmarshal (the one this file's own 24.33 note above explicitly left as-is, grouped with `pim_content_versions.go:67` as lower-risk) — reverted after finding that note, to respect the already-recorded decision rather than second-guess it from a fresh read of the file.
- Considered adding a second DB-pool-stats endpoint before noticing 24.34 above had already put `db_pool` on `handleHealth` in this same tree — not added, to avoid a second way of exposing the same stats.

### Addendum (2026-07-27): Medium #9 status-transition finding turned out to be a live maker-checker bypass — closed
User asked to also fix the one item the 2026-07-26 addendum above left open (`[needs design decision]`, per-doctype status transitions). Digging in rather than re-stating the prior framing found the actual risk was far more serious than "an edge case that could lock out a legitimate transition": `handleGenericDoc`'s generic doc create/update path took `payload["status"]` verbatim with zero restriction - **any user with plain create/update permission on an approval-gated doctype (PurchaseOrder, VendorInvoice, JournalVoucher, LoyaltyRedemptionRequest, ...) could set `"status": "Approved"` directly and completely bypass `SubmitForApproval`/`DecideApproval` - no role check, no maker-checker segregation-of-duties check, nothing.**
- [x] Closed the doctype-agnostic-but-decision-free half of Medium #9: `handleGenericDoc` (`internal/server/handlers_core_doc_engine.go`) now rejects (`GLOBAL-0019`) a client-supplied `status` of `Approved`/`Rejected` that differs from the document's prior status, whenever `engines.IsApprovalGated` says the doctype has approval rules configured - no per-doctype business input needed, since the valid states here are already the approval engine's own contract, not a guess. `statusVal != priorStatus` is the load-bearing condition: an edit that merely round-trips an already-`Approved` document's unchanged status (GET included it, PUT sent the whole object back) still passes through untouched - the pre-existing `wasApproved`/`ResetToPendingOnEdit` logic still fires afterward exactly as before.
- **The genuinely-still-open half is narrower now**: a general per-doctype transition map for non-approval-gated doctypes (Masters' Active/Inactive, GRN/ProductionOrder's own status fields) remains a real scope/UX decision, not a security gap - `docs/ERP_LOOPHOLES_ANALYSIS.md`'s own updated Medium #9 entry keeps the narrowed `[needs design decision]` tag rather than closing it outright.
- **Verified**: `go build`/`go vet` clean; `go test ./... -p 1` - `internal/server` clean, `engines` has the same two pre-existing unrelated failures noted in the addendum above (concurrent session's in-flight `reports_stage26_10.go`). Live-verified over real HTTP (throwaway port 8299, real `admin`/`manager1` users, not scratch fake ids - the first attempt with a fake user id surfaced an unrelated `documents_created_by_fkey` violation, a testing artifact not a bug, corrected by using real seeded users): direct create-as-`Approved` → 422 `GLOBAL-0019`; direct edit-to-`Approved` from `Draft` → 422; normal `Draft` create and an unrelated-field edit → 200 both; full legitimate submit→approve (by a second real user, `manager1`) → 200; editing the now-`Approved` PO with `amendment_reason` and status round-tripped unchanged → 200, correctly reset to `Pending Approval` by the existing re-approval-on-edit logic afterward. All scratch fixtures and the `cmd/` token-minting tool removed after.

---

## Stage 25 — Mature-ERP Validation Coverage ✅ (fully closed, 2026-07-23)

Stage 23 had backlogged "~187 unwired Mature-ERP codes." A recount found only 7 of 187 actually wired — the other 180 sit almost entirely inside modules already built (Stage 1-22), not unbuilt subsystems. A separate "Production Mandatory" tier (deployment/backup/subscription-limit, 12 codes) sits outside the 187 entirely.

Each batch is a real per-code audit (does the underlying field/hook exist?), not a blind sweep — every batch lists both what got wired and what was deliberately deferred with a reason.

- [x] **25.1-25.2 Batch 1: Global/Common** — added a `ValidationError{Code, SubFor, Message}` type to `engines/doctype.go`'s `ValidateDocument`, the one generic validation path every doctype's create/update already runs through. One shared change → every doctype gets precise codes for its 4 existing scenarios, plus a real new check (`CreateAccountingPeriod` had no start-after-end-date validation).
- [x] **25.3 Batch 2: Master Data** — 6 real checks (Item HSN format/required-when-taxable, duplicate barcode; Vendor GSTIN/bank-account format; Customer mobile format). Remaining Master Data codes deferred — the underlying fields don't exist on the doctype yet.
- [x] **25.4 Batch 3: Manufacturing/HR/GRN/PO/Sales/Transfer/Vendor/WMS/Assets/Expense** — 29 of 75 wired, 46 deferred (several engines are genuinely thin MVPs by design; Purchase Return/RTV references a doctype that was never built).
  - **Real gap closed**: `ProcessReturnAnywhere` took an unverified free-text order ID with zero cross-check against any real sale — now resolves against a real sale.
  - **2 bugs fixed during live verification**: a fully-designed check was never actually coded; a GRN quantity query didn't exclude cancelled GRNs.
- [x] **25.5 Batch 4: Admin/Config, DocType/Metadata, Reports, Data Import, Procurement/RFQ, Notifications, Mobile/Device, Observability, Customer/CRM, Approvals, Order Mgmt** — ~30 of ~57 wired (built across two sessions concurrently, reconciled into one combined entry).
  - **2 bugs fixed**: a config error mapped to a blanket 500 instead of 422; a field-name collision silently overwrote existing fields.
  - New pattern: a **log-only tag** for scenarios where the catalog wants a blocking rejection but this codebase already made a deliberate non-blocking design choice.
- [x] **25.9** `FIN-0260` (period locked, 409) — revisited, still not wired: `PostDoubleEntry` has 28 call sites across 12 files, no single choke point to attach it to. A genuine structural gap, not a batch miss.
- [x] **25.6 Batch 5: Channel Connectors + Omnichannel** — 4 of 10 real.
  - **Bug fixed**: a circuit-breaker-open publish job was marked `Failed` instead of `Queued`, contradicting the breaker's own retry design.
- [x] **25.7 Batch 6: POS Cash Drawer + Extension Hooks** — 5 of 6 real, the highest hit rate of any batch. Cash-drawer variance-tolerance and Pine Labs terminal-mapping checks both went from "computed but never checked" to enforced.
- [x] **25.8 Genuinely new API surface** — deployment-status and backup-status endpoints (reusing existing `public.deployments`); a new subscription-limit check.
  - **2 gaps fixed along the way**: a failed deployment recorded nothing; a backup's checksum sidecar was never verified before restore.
- [x] **25.10** Re-audited, confirmed the remaining 8 Global/Common codes are still genuinely generic (no hook to attach to).

**Stage 25 is fully closed** — all 6 batches plus 25.8/25.9/25.10.

---

---

## Stage 27 — Modular Product Packaging ✅ (2026-07-24, all 8 items closed 2026-07-26)

User request: sell PIM/WMS/OMS/HR/etc. as fully independent products — any single module, any combination, or the full suite — each reachable at its own URL (`/wms`, `/pims`, `/oms`, `/hr`, ...), on the same single Go binary/single Postgres instance, with internal module clubbing/dependency handled automatically. Full design rationale in the session's plan; summary below. See `project_ledger.md` §41 for the narrative and the two real bugs found while verifying.

- [x] **27.1** `engines.ProductPackages` — a static Go map (not a new DB table: packages have no per-tenant state of their own, the existing `module_entitlements` table already is that state) is the "master definition" of every sellable product: package key, display name, URL prefix, and which `module_key`s it grants. `ExpandPackagesToModules`/`ResolveSoleProductPackage`/`ResolveOwnedPackages`/`IsFullSuite` (`engines/modules.go`) turn a package selection into concrete entitlements and back.
- [x] **27.2** New `wms`/`oms` module_keys (`db/migrations_stage27_product_packaging.sql`, same shape as the Stage 18 core-module-fix migration) — these two product areas had **no module-entitlement gate at all** before this (confirmed by reading the code, not assumed): 5 WMS floor-ops routes were registered role-open with zero gate, and the entire Unicommerce/BigCommerce integration surface likewise; Shopify/marketplace/fulfillment routes carried only the older, narrower `featureGate("oms_integration"/"wms_integration",...)`. All now additionally carry `moduleGate("wms"/"oms",...)` (`internal/server/routes.go`) — the older feature flags stay, composed alongside, per their own documented "is this integration configured" vs. "is this product licensed" distinction.
- [x] **27.3** Module dependency/inheritance: `moduleDependencies` map + a transactional `SetModuleEntitlement` (`engines/modules.go`) — enabling a module auto-enables its unmet prerequisites atomically; disabling a module still depended on by another enabled module is refused with a clear error. Only one real optional↔optional dependency exists today (`rfq` → `procurement`) — the mechanism is generic and ready for more, not pre-populated speculatively.
- [x] **27.4** Self-service `GET /api/v1/me/modules` (`internal/server/handlers_profile.go`, mirrors the existing `handleMyPermissions` pattern) — any authenticated user, not just HR/Admin, can read their own tenant's enabled modules plus a `sole_package`/`owned_packages` navigation hint. The existing HR/Admin-only `GET /api/v1/admin/tenant/module-entitlements` is untouched.
- [x] **27.5** Provisioning gets an optional `packages []string` field (`handleProvisionTenant`) — reuses 100% of existing entitlement plumbing (`engines.SetModuleEntitlement` in a loop), omitted entirely preserves today's exact "everything enabled" behavior. **Real bug found and fixed during live verification**: a naive single-pass "enable wanted, disable the rest" loop iterates `public.modules` alphabetically, so disabling `procurement` before `rfq` (both alphabetically ahead of no one relevant) hit the new dependency-refusal check and silently left `procurement` enabled - fixed with an enable-first, then multi-pass-retry-disable loop that converges regardless of iteration order. A second bug in the *fix itself* (`pass < len(toDisable)` re-evaluated against the shrinking slice, exiting after one pass) was caught by the same live test and fixed by capturing `maxPasses` up front.
- [x] **27.6** Server-side SPA path fallback (`internal/server/routes.go`) — a small loop over `engines.ProductPackages` registers `GET /<prefix>` and `GET /<prefix>/{rest...}` serving `public/index.html`, so a direct/bookmarked hit on `/wms`, `/pims`, etc. works against the plain `http.FileServer` that would otherwise 404 it. Adding a future product to the map gets a working URL automatically.
- [x] **27.7** Frontend entitlement-aware nav (`public/app.js`/`public/styles.css`) — `MENU_MODULE_MAP` + `applyModuleEntitlements()` mirror the existing role-based `MENU_PERMISSION_MAP`/`applySidebarPermissions()` exactly (own `module-hidden` CSS class so the two filters can't clobber each other), hiding sidebar items whose module is disabled and collapsing an emptied flyout. `applyProductPathRouting()` silently rewrites bare `/` to a single-product tenant's own URL via `history.replaceState` (never a reload, never an access-control decision - `moduleGate` is still the only real enforcement). A minimal `renderProductSwitcher()` (plain `<select>`, `history.pushState`) appears only for a tenant with 2+ products but not the full suite. **Real bug found and fixed during live-browser verification**: `owned_packages` initially listed every package for a full-suite tenant too (such a tenant technically satisfies every individual package's requirements), incorrectly showing the switcher - fixed with `engines.IsFullSuite`, which suppresses `owned_packages` when a tenant has the complete module set.
- [x] **27.8** [fast-follow] An admin UI screen for picking packages per tenant — built 2026-07-26. **DONE.** 26.1.4's Tenant Entitlements screen already covered *adjusting an existing tenant's* packages; the actual gap was `POST /api/v1/admin/tenant/provision`'s `packages` field having **zero browser UI at all** (only ever reachable via curl/scripts). New collapsed-by-default "+ Provision New Tenant" panel at the top of `renderTenantEntitlementsView` (`public/app.js`) — tenant ID/schema name inputs, a checkbox per `engines.ProductPackages` entry (reusing the same `packages` data already fetched for the "Apply a Plan" buttons below it), a "Provision Tenant" button that POSTs to the existing endpoint, and the pre-existing `showOneTimeSecretDialog` (the same "shown once, never retrievable again" chrome extension-hook secrets/tokens already use) to surface the generated admin password. No new backend route, no new dialog system — reuses what 27.1-27.7 and earlier stages already built. Live-verified via Playwright: toggle expands the form, 9 package checkboxes render correctly, provisioning a real tenant succeeds, the one-time-secret dialog shows the correct title/message, and the new tenant immediately appears in the entitlements picker dropdown after closing the dialog. `go build`/`go vet`/`node --check` clean.

**Explicit, documented scope boundary**: the 5 `is_core` modules (`core`/`master_data`/`inventory`/`sales`/`finance`) stay permanently enabled for every tenant regardless of which package(s) they bought - unchanged, pre-existing Stage 14 behavior. A single-product tenant's *visible* nav/URL is still fully scoped to what they bought; only the backend's always-on technical foundation stays present and inert underneath, exactly as it already did for every tenant before this Stage.

**Verification (2026-07-24):** `go build ./...`/`go vet ./...` clean; `go test ./... -p 1` reproduces exactly the same two known pre-existing failures (nothing new). Live-verified against the real dev DB on a throwaway port: provisioned a WMS-only and a PIM-only tenant via the new `packages` field, confirmed `moduleGate("wms"/"oms"/"pim",...)` actually blocks/allows correctly per tenant, confirmed the `rfq`→`procurement` dependency auto-enables/blocks-disable correctly, confirmed all product-prefix URLs (`/wms`,`/pims`,`/oms`,`/hr`,`/erp`,...) serve the SPA shell including nested subpaths. Real-browser (Playwright) pass: a PIM-only tenant's sidebar correctly trims to Dashboard/POS/Accounting/Reports/Stock/PIM/Setup/Settings and bare `/` silently redirects to `/pims`, no console errors; the existing full-suite `default` tenant is pixel-for-pixel unchanged (full sidebar, no redirect, no switcher) - the non-negotiable regression check. Test tenant schemas and scratch tooling cleaned up afterward.

---

## Stage 29 — OMS Operations Follow-up ✅ (2026-07-27)

- [x] **29.1 Unified OMS workbench** — new Order Management screen joins existing SalesOrder, FulfillmentTask, LogisticsBooking, and SalesInvoice documents into one operational table with hold/release/cancel/view actions. It uses the generic document API rather than a duplicate read backend.
- [x] **29.2 Channel intake → SalesOrder** — Shopify and Unicommerce order intake now normalize payloads into `CreateSalesOrder`, so SKU mapping, validation, allocation, reservations, hold routing, and idempotency are shared with manual/API orders. Legacy mapping tables remain populated for connector compatibility.
- [x] **29.3 Shipment/delivery notifications** — the existing notification dispatcher now receives `Order Shipped` at the all-fulfillment-tasks-dispatched closure point and `Order Delivered` from courier tracking ingestion.
- [x] **29.4 SalesOrder-to-invoice automation** — full shipment creates one deterministic, idempotent Draft SalesInvoice (`INV-<SalesOrder>`); posting/settlement remain explicit finance actions, so shipping does not silently write GL entries.
- [x] **29.5 Flyout hover navigation** — sidebar and Schema Designer module flyouts now open as soon as the pointer reaches the module row/arrow, with the same behavior on keyboard focus; click remains available for touch devices. The sidebar stacking layer is above sticky table headers, so the full menu stays visible and clickable over table UI.
- [x] **29.6 Purchase requisition numbering and lookup masters** — requisition numbers are generated server-side from the configurable `PR` Prefix Config (Settings → Prefix Configs), not typed by a requester. Requirement descriptions are learned into `PurchaseRequisitionDescription` and suggested on later entries; Department uses the existing Department master with the same type-ahead picker.
- [x] **23.11 clarification** — remains intentionally parked. No paste-into-grid UI exists, so an “inline grid message” cannot be implemented correctly without separately designing that product surface.

---

## Stage 28 — Configurability, Theming & Deployment ✅ (2026-07-27, all 4 items closed, committed 2026-07-28 as `a3b4c16`)

User request (one batch): all operational config in the admin UI, module by module ("nothing hardcoded"); a dark/light/system theme; user-controllable report columns saved as Universal/Personal profiles; Caddy reverse proxy for automatic TLS. Built + verified + checkpointed one at a time. See `project_ledger.md` §63.

- [x] **28.1** Module-by-module admin **Configuration** screen backed by a generic settings registry (`engines/settings_registry.go`/`settings_definitions.go`, `db/migrations_stage28_system_settings.sql`, `internal/server/handlers_settings.go`). Every setting declares module/label/type/default once; `GET/PUT /api/v1/admin/settings` (HR/Admin-only) + a generic UI render it. First wave = 7 settings that were hardcoded constants, each Default = the old constant so behavior is unchanged until edited: Loyalty point expiry (365d), earn rate (₹100/pt), tier-recompute-on-earn toggle; Inventory online reservation TTL (86400s, wired via `CreateReservation`'s `expirySec<=0` choke point + `fulfillment.go`'s inline reservation + the 4 online-order callers passing `0`); Security loyalty-OTP validity (5m), daily redemption cap (5), default new-user idle-timeout (select). `system_settings` added to `ProvisionTenantSchema`'s clone list. The framework is the single home all future config must use — adding one is a `RegisterSetting` + a `GetSetting*` read. Live-verified: settings GET/PUT persist, out-of-range/invalid-option rejected with a specific 422, non-admin 403; new `TestSettingsRegistry` green.
- [x] **28.2** **Theme: light / dark / system.** Dark palette layered on the existing `:root` CSS tokens (`:root[data-theme="dark"]` + `prefers-color-scheme` scoped `:not([data-theme="light"])` so System follows the OS, explicit choices win); all structural literal-color backgrounds (`#f8fafc`/`#f1f5f9`/`#f3f4f6`/`#e2e8f0`/toggle track) tokenized so dark mode is complete (semantic status colors kept literal). Persisted per-user (`users.theme_preference`, `migrations_stage28_user_theme.sql`, surfaced/set via `/api/v1/me`, validated) + localStorage for a no-flash pre-paint apply (inline `<head>` script), reconciled with the server value on load. 3-segment toggle in the account popover. Live-verified: Playwright both themes render clean (zero console errors), `/me` round-trips, invalid rejected.
- [x] **28.3** **Report column control + profiles.** Columns chooser (show/hide + ↑/↓ reorder, no drag lib) on the report catalog + saveable `ReportColumnProfile`s (`migrations_stage28_report_column_profiles.sql`, mirrors `ReportFilterPreset`) in two scopes: **Personal** (owner-only, client-filtered) and **Universal** (shared). Universal creatable only by HR/Admin or Store Manager — enforced client-side AND server-side (`handlers_core_doc_engine.go` create choke point). Live-verified: hide+reorder applies exactly (Available hidden, SKU/On-Hand reordered), Personal/Universal filtering, Cashier→Universal = 403.
- [x] **28.4** **Caddy reverse proxy + automatic TLS.** The `deploy/` config (Caddyfile with auto Let's Encrypt / systemd unit / env example / migrate+backup scripts / README) was already built by a concurrent session; this added the app-side support and reconciled: a `HOST` env (`routes.go`) to bind the listener loopback-only behind the proxy (default unset = historical all-interfaces bind), and `TRUST_PROXY`+`clientIP()` (`middleware.go`) so per-IP rate limiting reads `X-Forwarded-For` (else every client shares Caddy's loopback bucket). Wired `HOST=127.0.0.1`/`TRUST_PROXY=1` into `deploy/erp.env.example`. Live-verified: binds `127.0.0.1` under HOST; XFF rate-limit bucketing correct (5×401 then throttled for one IP, fresh bucket for another). One deploy step needs the user: real Let's Encrypt issuance requires their public domain + DNS (`caddy validate` deferred — Caddy not installed locally).

---

---

## Stage 28.5 — UI defect batch: dialog legibility, flyout reliability, global search ✅ (2026-07-28)

Four defects reported by the user in one session, all in `public/app.js`/`public/styles.css` only (no Go, no schema, no dependency). See `project_ledger.md` §65.

- [x] **28.5.1 Popup text invisible in dark mode.** `.custom-dialog-body` carried a literal `color: #334155` — dark slate on the dark `--panel-bg` (#1a2436), ~1.3:1 contrast, i.e. the message in every `showCustomAlert`/`showCustomConfirm`/`showCustomPrompt` was unreadable. Fixed at the source rather than one rule at a time: **every** literal colour below the token blocks is now a `var()` token. Added semantic tokens (`--on-primary`, `--text-placeholder`, `--danger-color/-hover/-strong/-soft-bg/-soft-border`, `--warning-*`, `--success-*`) to all three token blocks (`:root`, `[data-theme="dark"]`, the `prefers-color-scheme` block) and routed 25 literal-colour rules + 9 inline `style="color:#..."` strings in `app.js` through them. Light-theme values are the exact previous hexes, so light mode is byte-identical in appearance. Also gave `.custom-dialog-body` explicit `color: inherit` rules for the elements callers inject (p/li/td/th/h*/label/strong/code) plus `a`/`small`/`.text-muted` — deliberately *not* a `*` selector, which would have flattened an intentional `.badge-danger` dropped into a dialog. A rule comment above the dark token block now states that no rule below may use a literal colour, with the two sanctioned exceptions (print-only `.receipt`/`.sticker-label`, which are always ink-on-paper, and translucent rgba scrims/glows). Measured after the fix, dark mode: dialog body 12.99:1, badges 7.87–11.54:1, banners 7.87–9.40:1, `pre` 11.54:1.
- [x] **28.5.2 Sidebar submenu closing before it could be used / clicks doing nothing.** Reported as "in POS nothing is working, if I click anything it is not opening anything". Not a routing bug — every menu handler was correctly bound (verified by clicking all 38 sidebar entries programmatically: 37/38 navigated, the 1 being Dashboard already being the current view). The flyout was disappearing out from under the pointer, so the click landed on the page behind it. **Four distinct causes**, each confirmed by a scripted pointer-path repro before and after: (a) the hide timer was per-container but called the *global* `closeSubmenus()`, so sliding from module A to B left A's timer running and it shut B 200ms later — replaced with one shared timer that any open cancels; (b) the 8px gap between the sidebar row and the flyout panel is outside the container's box, so crossing it (or pausing in it, which reaching for a submenu item requires) fired `mouseleave` and started the close — added an invisible `.menu-flyout-bridge` hit area, a child of the container, spanning exactly that gap; (c) `mouseleave` was bound alongside `pointerenter`, and pointer events fire *ahead* of their compatibility mouse events, so the leave handler ran **after** the next row's enter handler had already cancelled the pending close and scheduled a fresh one nothing cancelled — both sides are now `pointer*`; (d) sibling entries with no flyout of their own (Dashboard/Reports/Manufacturing/PIM) had no listeners at all, so they were dead zones where a close scheduled on the way past went through unopposed. Added on top: hovering a *different* module now requires a 140ms dwell before it takes the open menu (500ms if the pointer is measurably travelling *towards* the open panel), so a diagonal reach across intervening rows no longer steals it; sidebar scroll repositions the open flyout instead of closing it; Escape closes; a click on the module row can no longer close a menu the user just hovered open (only a second, deliberate click on an already-pinned row does).
- [x] **28.5.3 Long flyouts running off the bottom of the screen.** Found while verifying 28.5.2, same interaction. `openFlyout` capped a flyout's height to the space *below* its trigger, so Stock's 12 screens (and Setup's ~25 master doctypes) anchored low in the sidebar put their last items off-screen — reachable only by scrolling inside the panel, and in practice not reachable at all because the pointer had to leave the panel to get there. It now takes its natural height and slides up to keep its bottom on screen, scrolling internally only when taller than the whole viewport. Verified: all 12 Stock items on screen at a 900px viewport (last item bottom 751 of 900).
- [x] **28.5.4 Global search did nothing.** The top-bar box promised "Search menu, category, type or HSN" but only filtered the table you already had open — on the dashboard, or any non-table screen, typing had no effect at all. Added `setupGlobalSearchSuggest()`: an index built from the live sidebar DOM (every destination, including the ones only reachable by hovering the right module) plus every registered record type, matched with label-hits ranked above module-hits, rendered in a dropdown reusing the existing `.typeahead-menu`/`.typeahead-item` vocabulary. Picking one dispatches a click on the real sidebar element wherever one exists, so routing/permission-filtering/active-item highlighting stay in one place instead of being duplicated. Keyboard (↑/↓/Enter/Escape), mouse, an explicit empty state, and a dedupe so a record type the sidebar already lists as a screen doesn't appear twice. The pre-existing table filtering is untouched and still runs alongside. Permission- and entitlement-hidden entries are excluded because the index reads the same `perm-hidden`/`module-hidden` classes the sidebar itself uses.

- [x] **28.5.5 Follow-up: click-to-open needed two or three clicks, and a parked cursor didn't bring the menu back.** Reported straight after 28.5.2 shipped, and both were caused by that same change. (a) The click handler 28.5.2 added was a *toggle* — a row you had already hovered open closed on the click, so it read as "click it two or three times before it sticks". A module row now only ever **opens**; closing is moving away, clicking outside the nav, or Escape. The `flyoutPinned` bookkeeping that toggle needed is gone. (b) While the pointer rests on a row, `pointerenter` never fires again, so anything that closed the menu in the meantime — navigating away from it, an outside click, Escape, or the sidebar re-rendering and replacing the DOM node under a stationary cursor — left it shut with the cursor still sitting right there. `pointermove` on the container now re-opens when nothing is open, so the smallest movement brings it straight back. Verified 8/8 on a dedicated repro (one click opens while another module is showing; second and third clicks keep it open; outside-click closes; cursor-still-on-row reopens; parked 3s without moving stays open; one click reopens after navigating away), with the 34/34 navigation pass and the three original flyout scenarios re-run green.

**Testing gotcha found here (worth knowing next time):** sidebar module labels are **tenant-configurable**, not fixed strings — mid-session the shared dev DB's active profile changed and the same modules rendered as `POS`/`Financial Accounting`/`Procurement`/`HRM` instead of `Point of Sale`/`Accounting`/`Buying`/`HR & Assets`, which broke a Playwright check that matched on the English name. UI tests against this app must key off element IDs or DOM order, never the visible label. A test that "suddenly fails" on a nav label is very likely this, not a regression.

**Verification (2026-07-28):** `node --check public/app.js` clean. Live Playwright against the running dev server as `manager1`: all 38 sidebar entries click-navigate; hover→click via a realistic pointer path 34/34 (a continuous diagonal corner-cut, the worst realistic case, went from 6/34 before the fix to 32/34 after); the three original flyout repro scenarios all pass; search suggestions correct for `buy`/`invoice`/`bin`/`pos` with a proper empty state for a non-match, and both Enter-to-navigate and click-to-navigate confirmed; zero console/page errors throughout. Dark and light both screenshot-checked, with computed contrast ratios recorded above. **Not changed:** the `category`/`HSN` half of the search placeholder — suggesting menus and record types is navigation over data the client already holds; searching product records across entities would need a new server-side endpoint and is a separate piece of work, not a bug fix.

---

---

## Stage 29.7 — QC exhaustive-report follow-ups (O1 index, O2 sslmode) ✅ (2026-07-29)

The two non-blocking observations from `QC_EXHAUSTIVE_REPORT.md` (§FINDINGS SUMMARY, O1/O2), closed on the user's instruction to fix them plus any other open items. See `project_ledger.md` §66.

- [x] **29.7.1 O1 — gl_postings reporting index.** `db/migrations_stage29_gl_postings_reporting_index.sql` adds `idx_gl_postings_account_created` on `(account_code, created_at) INCLUDE (debit, credit, cost_center)`, backfilled into every existing `tenant_%` schema (new tenants inherit it via `ProvisionTenantSchema`'s `LIKE ... INCLUDING ALL`). **The QC report's literal recommendation — a bare `(account_code, created_at)` — was measured and does not work**: on a seeded 1M-row `gl_postings` the planner produced a byte-identical Seq Scan plan with the bare index present and absent, because the reports need `debit`/`credit` per row and a non-covering index means a heap visit per row, which costs more than the seq scan it would replace. The `INCLUDE` payload is what makes the plans flip to index-only scans (`Heap Fetches: 0`). Backfill guard matches on index *definition*, not name, because `LIKE ... INCLUDING ALL` does not preserve index names — a name-only `IF NOT EXISTS` would have built a second redundant copy on any tenant provisioned after the migration.
- [x] **29.7.2 O1 (part 2) — sargable date predicates, without which the index is inert.** Nine gl_postings report queries spelled their range as `created_at::date BETWEEN $1 AND $2`; the cast wraps the indexed column in a function call so it can never be a range seek. Rewritten to the equivalent half-open `created_at >= $1::date AND created_at < ($2::date + 1)` in `finance_reports_stage26.go` (P&L, balance sheet, cash flow, tax ledger, statutory export), `gl_cost_center.go` (cost-centre P&L), `reports.go` (`glAccountNetBalance`, GST txn count) and `report_definitions.go` (`gstReturnDrillDown`). The convention is documented once at the top of `finance_reports_stage26.go` so new queries follow it. **The index and this rewrite only pay off together** — neither is worth shipping alone.
- [x] **29.7.3 O2 — sslmode documented for production.** `deploy/erp.env.example` now explains why the shipped `sslmode=disable` is only valid for a same-box loopback DB, what to switch to for any off-box/managed Postgres (`require` minimum, `verify-full` + `sslrootcert=` preferred), and how to verify what was actually negotiated (`pg_stat_ssl`). Records the lib/pq-specific gotcha that omitting `sslmode` yields `require`, **not** psql/libpq's `prefer` (verified in `lib/pq@v1.12.3/ssl.go`, where `mode == ""` shares a branch with `SSLModeRequire`). `deploy/README.md` A2 gained a matching callout at the point where the reader chooses where Postgres lives.

**Verification (2026-07-29):** `go build ./...` + `go vet ./engines/...` clean; `go test ./... -p 1` fully green (including the finance GL-totals test that has historically been flaky). Migration applied to two throwaway databases and to the dev DB (both `tenant_default` and `tenant_new_schema`): applies clean, re-runs are no-ops, a schema without `gl_postings` is skipped, a tenant provisioned post-migration inherits the index and a re-run does **not** duplicate it. Every rewritten predicate was proved equivalent to its original against 1M seeded rows — zero differing rows on P&L and balance sheet, matching totals on cash flow/GST/drill-down/statutory export, and an explicit boundary case confirming a posting at `23:59:59.999` on the last day is still included while `00:00:00` on the next day is excluded. Measured effect at 1M rows: P&L for one quarter 87ms/2,480 buffers → 5ms/162; cash flow 109ms/20,402 → 4ms/933; cost-centre P&L 95ms/20,359 → 9ms/158. Index cost 47MB against a 159MB heap.

**Two GL reports are deliberately not helped, and are the honest residue of O1:**

- [ ] **29.7.4 Trial balance is an unbounded full aggregate** — `GetTrialBalance` (`engines/finance.go`) sums the *entire* ledger with no date filter, so it must touch every row by definition; no index can fix that, and the QC report's "add an index" framing of O1 was wrong on this specific query. The real fix is to bound it to a period or as-of date — which is how a trial balance is conventionally scoped anyway — but that changes the report's parameters and its screen. **[needs design decision: should Trial Balance take a mandatory as-of date (matching Balance Sheet) or an optional period filter defaulting to the open accounting period? Either is a small change once the shape is chosen.]**
- [ ] **29.7.5 Statutory GL export can't use an account-leading index** — `GetStatutoryGLExport` filters on date alone with no `account_code`, so `idx_gl_postings_account_created` cannot seek for it. Left to seq-scan on purpose: it is an async background CSV export (`CreateReportExportJob`), not an interactive screen, so a second index on `(created_at)` would tax every posting write to speed up a job nobody waits on. Revisit only if it becomes interactive or the export window starts timing out.

---

## Stage 29.8 — Both remaining loophole items closed (status-transition map, JWT session staleness) ✅ (2026-07-29)

The last two open items in `ERP_LOOPHOLES_ANALYSIS.md` were both blocked on the user, not on work: one was `[needs design decision]`, the other deferred by standing policy. Asked, and the user decided (2026-07-29):

| Question | Decision |
|---|---|
| Enforcement posture for the transition map | **Opt-in strict, per doctype** — fail-open unless a doctype is flagged |
| Scope of the first pass | **Transactional lifecycles + Masters** |
| Session invalidation mechanism | **Middleware re-checks live user state** (not a jti denylist) |
| Signing-key rotation | **Build multi-key rotation** |

See `project_ledger.md` §67.

- [x] **29.8.1 Per-doctype status-transition map.** Extends the **existing** `StatusTransitionRule` master (Stage 26.12.9) rather than adding a parallel mechanism — it already had the right shape (`from_status`/`to_status`/`allowed`/`requires_reason_code`) and an admin screen; it was just limited to four OMS entities and only consulted for order cancellation. `entity` widened from a 4-value Select to Data (so a doctype added later needs no migration), validated in code against the live doctype list plus the four legacy OMS names. New `doctype_meta.strict_status_transitions` boolean, default FALSE. Enforcement attached at the one shared choke point every generic-doc write already passes through (`ValidateTransactionalRules`), so all current and future callers are covered by construction. Rejections reuse catalog code `GLOBAL-0019` ("Invalid status transition", 422) — the code that already existed for exactly this — and the message lists the legal destinations rather than just refusing.
- [x] **29.8.2 Matrices seeded: 178 rules, 64 doctypes flagged strict.** Master `Active`↔`Inactive` pairs are **generated** from `doctype_fields` rather than hand-listed, so every current and future Active/Inactive master is covered without maintaining a list. Transactional lifecycles listed explicitly (VendorInvoice can't jump `Draft`→`Paid` and skip the 3-way match; a `Disposed` Asset can't be re-capitalised; an `Approved` GRN whose stock already posted can only be reason-coded to `Cancelled`). Approval-engine edges (`Draft`→`Pending Approval`→`Approved`/`Rejected`, and `Approved`→`Pending Approval` for `ResetToPendingOnEdit`) are also **generated**, gated on both endpoints being real options for that doctype. Deliberately NOT flagged strict: worker-driven types (`ImportJob`, `ReportExportJob`, `POSOfflineQueueGap`, `POSOfflineSyncVariance`) whose status is written by background code, and the four legacy OMS entities.
- [x] **29.8.3 Live user-state re-check (session staleness).** `ParseToken` is pure HMAC and never touched the DB, so a deactivated user kept full access for the rest of `JWT_EXPIRY_HOURS` (24h default) and a demoted user's token kept asserting the old role. `apiMiddleware` now re-reads the user's row per request behind a 30s cache (`AUTH_STATE_CACHE_SECONDS`), with the DB authoritative for role **and** location. Chosen over a jti denylist because a denylist can't fix the role half at all. Invalidation hooked into all three mutation sites (admin set-status, admin set-location, `SyncEmployeeAccessLink`) so a deactivation bites on the next request, not at the end of the cache window. Extension tokens are skipped (no user row exists); MFA tokens are checked. A DB failure returns a retryable 503, explicitly **not** a 401 — a replica hiccup must not log everyone out.
- [x] **29.8.4 JWT signing-key rotation.** `JWT_SECRET_<n>`: highest number signs, every configured key verifies, so a planned rotation is add-key → wait one token TTL → delete-old-key with nobody logged out. Plain `JWT_SECRET` still works unchanged as the legacy key, and with no numbered key set the emitted token is **byte-identical** to before — inert until opted into. Verification deliberately tries all keys instead of selecting by the token's `kid`: `kid` lives inside the payload and is attacker-controlled until the HMAC has already passed, so using it to pick the trust anchor would mean trusting unverified input. `kid` is still emitted for ops visibility, just never load-bearing.

**Verification (2026-07-29):** `go build ./...`, `go vet ./...` clean. 19 new subtests in `engines/stage29_8_test.go` + 2 end-to-end wiring tests in `internal/server/stage29_8_test.go` (deactivated user's live token → 401 `GLOBAL-0009`; `RFQ` `Draft`→`Closed` → 422 `GLOBAL-0019` with the document provably unchanged, while `Draft`→`Sent` still succeeds). Full suite green across 5 consecutive runs with strict mode live on all 64 doctypes.

**Live-verified against a real server** (throwaway instance on :8099, real HTTP, real dev DB; instance stopped and scratch helper deleted afterwards):

| Check | Result |
|---|---|
| `RFQ` `Draft`→`Closed` (skips `Sent`) | 422 `GLOBAL-0019`, stored status still `Draft` |
| `RFQ` `Draft`→`Sent` (listed) | 200, stored status `Sent` |
| Active user, valid token | 200 |
| Same token after deactivation in DB | 401 `GLOBAL-0009` |
| Same token after demotion HR/Admin→Cashier, hitting an HR/Admin endpoint | 403 `GLOBAL-0011` — **the half a jti denylist could not have fixed** |
| Rotation: token signed by key 1 and key 2, both keys configured | both 200 (`kid=1` / `kid=2` present) |
| Rotation: after deleting `JWT_SECRET_1` and restarting | key-1 token 401, key-2 token 200 |

The last two rows walk the exact rotation procedure documented in `deploy/erp.env.example` end to end, including the retirement step.

**Two pre-existing tests were corrected, not worked around:** `TestPIMDashboardRouteRequiresAuthenticationAndHonorsModuleGate` and `TestCrossTenantIsolationAndTokenSecurity` minted tokens for user ids that had no row in `users`. That is now correctly a dead session. The cross-tenant test's own comment claimed its token was "indistinguishable from a real login" — no longer true, so both tests now seed the user row a real login would have required.

**Bugs this pass found in its own work, by testing against a clone of the dev DB before touching it:**
- The first matrices were written from `db/migration.sql`'s original option strings, but later migrations had extended four doctypes (`PurchaseOrder` gained `Pending Approval`/`Rejected`, `ProductionOrder` gained `In Process`, `TransferOrder` gained `Packed`, `CycleCountLine` gained `Recount Requested`). A real PurchaseOrder was sitting in `Pending Approval` that strict mode would have **frozen permanently**. Fixed by generating the approval edges from each doctype's live options instead of a static list.
- A real `SalesInvoice` was sitting in status `Active`, which isn't in its own option set (legacy debris). No rule can name a `from_status` the schema doesn't declare, so strict mode would have trapped that row forever. Added an escape: a write leaving an **undeclared** status is always allowed — the destination is still constrained by `ValidateDocument`, so this can only move debris back into a legal state.

- [ ] **29.8.5 Judgement calls worth a second look** — `Leave` treats `Approved`/`Rejected` as terminal, so revoking an approved leave is currently blocked; same for `VendorQuote` `Selected`. Both are one admin-editable `StatusTransitionRule` row away from being allowed if that turns out to be wrong in practice — flagged rather than guessed at.

---

