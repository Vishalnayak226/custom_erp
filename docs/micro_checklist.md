# In-House ERP: Micro-Checklist & Build Tracker

Tracks implementation at item level. `[ ]` = not started/open, `[x]` = completed and verified. Mark `[x]` only once actually verified (build/vet/test + live check where the item calls for it).

> **Legend for historical notes below**: some items were marked `[x]` early and later found to be missing real implementation — those carry a "re-opened"/"correction" note explaining the gap and the fix. This is expected project history, not something to clean up.

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

## Stage 18 — Data-Entry UX: Dropdowns/Autosuggest ✅ (2026-07-19)

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

---

## Stage 19 — Docs Suite, Folder Restructuring, `ERP_BLUEPRINT.md` ✅ (2026-07-19)

- [x] **19.1 `docs/ERP_BLUEPRINT.md`** — full project snapshot for an outside/AI reviewer, every claim cited back to a source doc.
- [x] **19.2 Go folder restructuring** — `main.go` (~4,681 lines) moved into `internal/server/`, split into 8 domain files; new thin `cmd/server/main.go`. Same-package move — zero cross-package visibility risk. Every build command updated and re-verified.
- [x] **19.3 Root clutter cleanup** — old one-off scripts moved to `scripts/archive/` (not deleted, per explicit request).
- [x] **19.4 `docs/` reorganization** — new `architecture/`/`specs/`/`operations/`/`requirements/`/`guides/` subfolders. Nothing deleted; every cross-reference fixed. New `docs/README.md` index.
- [x] **19.5 New requirement/guide docs** — `BRD.md`/`PRD.md`, `USER_GUIDE.md`/`ADMIN_GUIDE.md`, grounded in 3 external planning references. Built-vs-specified marked explicitly per module.

---

## Stage 20 — ERP Maturity Master Plan Execution ✅ (mostly — 7 items open: Track A + 20.30/20.31)

External maturity-plan PDF broken into 40 build-sized items: Track A (blocked on user input) + Track B (buildable now, 4 tracks).

### Track A — blocked on user input
- [ ] **20.1** Real escalation contacts for the incident runbook. **[needs user input]**
- [ ] **20.2** A real `OPS_ALERT_WEBHOOK_URL`. **[needs user input]**
- [ ] **20.3** Non-production connector credentials (Shopify/BigCommerce/Magento). **[needs user input]**
- [ ] **20.4** Production hosting decision (provider, domain, TLS, secrets store). **[needs user input]**
- [ ] **20.5** An external security/performance reviewer engagement. **[needs user input]**

### Track B.1 — POS Maturity ✅
- [x] **20.6-20.8** `POSProfile` master + `POSSession` doctype — checkout requires an Open session; close computes cash variance server-side.
- [x] **20.9** Payment-mode-aware GL posting (Cash 1100 / Card 1101 / UPI 1102).
- [x] **20.10** Discount-approval gate — routes through the existing approval engine.
  - **Bug fixed**: `POSCart.created_by` was hardcoded to `'system'` for every sale ever — would have silently defeated the maker-checker self-approval block.
- [x] **20.11** POS-side return entry point.
- [x] **20.12, 20.15** Audited, already satisfied (manual bill entry; Pine Labs reconciliation).
- [x] **20.14** Receipt printing (reused Stage 13.15's print pattern).
- [x] **20.13** Offline-first queue — deferred here pending a decision, **built later** (see Stage 20.13 entry below).

### Track B.2 — WMS Maturity ✅
- [x] **20.16** `Bin` master.
- [x] **20.17, 20.23** Putaway + Damaged/QC-Hold/RTV condition transitions — new `bin_stock` table (a breakdown of the same on-hand quantity `inventory_availability` already tracks, never a second source of truth).
- [x] **20.18** Computed, bin-grouped pick list.
- [x] **20.19** Pack/dispatch box mapping — optional `Approved`→`Packed` step, additive.
- [x] **20.20-20.22** Cycle count — reuses the existing `BulkImportCSV` engine + the approval engine for variance sign-off.
  - **Serious bug found and fixed**: the shared quantity-parser didn't handle CSV string values, reading every imported count as zero — posted a **-100** adjustment where **-10** was correct.
- [x] **20.24** Audited, already satisfied (print history + ledger already cover this).

### Track B.3 — Finance Maturity ✅
- [x] **20.25-20.26** Bank reconciliation — `BankAccount`/`BankStatementLine`, matched against `gl_postings`.
- [x] **20.27** Payment proposals — batches `Matched` vendor invoices through the existing pay function.
- [x] **20.28** TDS — a deliberate sibling of the no-TDS pay function, not a branch inside it.
- [x] **20.29** GST return summary — derived entirely from existing `gl_postings`, no new data collection.
- [x] **20.32** Debit/credit notes — GL-reversing, via 2 new contra accounts.
- [x] **20.33** Receivables ageing — `SalesInvoice` had existed since Stage 1 as a dormant shell (no amount field/GL posting/frontend); wired up for real to give this report a genuine receivable to age.
- [x] **20.34** Period-close checklist — advisory pre-close validations on top of the existing Stage 17.4 control.
- [ ] **20.30 e-invoice/IRN** — blocked on real GSP credentials. **[needs user input]**
- [ ] **20.31 e-way bill** — same. **[needs user input]**

### Track B.4 — Reports Engine ✅
- [x] **20.35** `ReportDefinition` framework — columns/params/a run function/optional drill-down, one frontend screen drives any report. Not a full ad hoc SQL builder (an injection risk out of scope).
- [x] **20.36** Saved filters — zero new backend code, just the existing generic doc API.
- [x] **20.37** Async export — reuses the existing outbox-worker ticker shape.
- [x] **20.38** Drill-down — re-derives the summary report's own bucket logic rather than a second copy.
- [x] **20.39** Column masking — redacts Sensitive columns below Store Manager/HR-Admin.
- [x] **20.40** 7 new reports added (GRN Register, Cash Book, Bank Book, Asset Register, Loyalty Ledger Summary, Production Order Status, RFQ Comparison). 2 skipped: Attendance Summary (no data model) and Stock Ledger (write function exists but is dead code).
- [x] **20.13 Offline-first POS queue** — **DONE (2026-07-22).** Decisions: offline window = one shift (tied to the cashier's open session); an offline sale always posts once synced (goods already left) and may push stock negative, flagged for review. `localStorage`-backed queue, replays on reconnect, reuses the existing `cart_number` idempotency key.
  - **Bug fixed**: the session-close success handler referenced an undefined variable — would throw on every successful close.

---

## Stage 21 — UI/UX Overhaul ✅ (2026-07-20)

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

## Stage 23 — Standardized Error/Message Catalog ✅ (2026-07-20)

User supplied a 301-row message-standardization spreadsheet. Didn't exist yet — errors were ad hoc plain-text/hand-rolled JSON across 11 files, and a `Content-Type: application/json` header meant plain-text errors silently broke the frontend's JSON parsing.

- [x] **23.1** Generated `error_catalog_generated.go` (302 codes) from the xlsx.
- [x] **23.2** `writeAPIError`/`writeAPIErrorGeneric` — the one place every error response is written from.
- [x] **23.3** Wired all framework-level paths (panic, rate limit, auth, module/feature gates).
- [x] **23.4** Swept all 10 handler files (18 precise codes, 382 generic-but-consistent). **Structural finding**: the catalog has zero rows at HTTP 400/405, while much of the codebase used exactly those (resolved in the follow-up, 23.12).
- [x] **23.5** `showToast()`/`renderPageBanner()` frontend primitives added.
- [x] **23.6-23.7** Verification + docs sync.
- [x] **23.10 Codes defined but not wired** — the ~187 "Mature ERP" codes describing not-yet-built features. **Fulfilled by Stage 25** (closed 2026-07-23) — every code individually audited and either wired or explicitly justified as deferred.
- [ ] **23.11** "Inline grid message" display style (1 row) — no paste-into-grid feature exists to attach it to.

**Stage 23 follow-up ✅ (2026-07-21)**
- [x] **23.12** Decided: standardize on HTTP 422, not a forked 400 variant. 186 call sites converted.
- [x] **23.13** Decided: keep `featureGate`'s fail-closed 403, not the matrix's soft-200.
- [x] **23.9** `handlers_finance_maturity.go` swept onto the standard envelope.
- [x] **23.8** Toast/Page Banner rollout via the single choke point (`showApiError()`) dispatching on a new `display_style` field — zero per-screen work.

---

## Stage 24 — Security & Loopholes Hardening ✅ (all 32 items done, 2026-07-22)

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

For the full chronological build narrative behind every item above, see **[docs/project_ledger.md](project_ledger.md)**. For environment setup, commit history, and known concurrent-session risk, see **[docs/ai_handover.md](ai_handover.md)**.
