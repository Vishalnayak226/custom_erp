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

---

## Stage 26 — Final Leg Maturity Completion Plan (external PDF, 2026-07-23)

`ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf` deepens Stage 20's source plan into a 12-phase (0-11) roadmap with per-module final-leg tables. Full rationale/benchmark detail: `docs/specs/erp_maturity_master_plan.md` (rewritten for this PDF — read it for *why*; this stage is *what*). Numbering `26.0`-`26.11` matches the PDF's own phase numbers 1:1. Same Track split as Stage 20: items marked **[needs user input]** can't be built by an AI session; everything else is buildable now. Items marked **[P2 — tier/scope decision]** are real capabilities from the PDF deliberately not started without an explicit go-ahead, per this repo's standing practice of surfacing scope decisions rather than guessing or silently over-building.

### Phase 26.0 — Truth Freeze and Regression Fix
- [x] **26.0.1** Reconcile the new PDF against live code/docs/git state. Corrections found and logged in `specs/erp_maturity_master_plan.md` §2: the PDF's finance-regression root cause assumption and its industry-pack gap count were both stale/wrong — see 26.0.2 and Phase 26.4's intro.
- [ ] **26.0.2** Root-cause fix for `TestEngines/FinanceDoubleEntryAndPOS` and `TestEngines/DocTypeValidationAndAuth`: re-ran both fresh on 2026-07-23 with `go test ./... -p 1` — both still fail with identical numbers to prior sessions (finance: expects 9000, gets 9500, but `balanced:true` and debits==credits==9500 throughout — the ledger is internally consistent, the test fixture expectation is wrong/stale by 500; DocType: `Brand.fefo_enabled` still marked required from earlier Agriculture-profile testing). This is accumulated fixture debris in the one shared, persistent `custom_erp` dev DB every test run writes to — not a broken posting engine. **Real fix**: give the finance/DocType tests an isolated schema or an explicit truncate/reset of their fixture rows before asserting exact totals, instead of relying on hand-cleanup in a shared DB. Re-run `-p 1` after the isolation fix to confirm both pass clean — do not patch `engines/finance.go` itself without first confirming a real bug survives isolation.

### Phase 26.1 — Production-Like Environment
- [ ] **26.1.1** Production hosting decision (provider, domain, TLS, secrets store). **[needs user input]**
- [ ] **26.1.2** SLO/status-page dashboard — buildable now, reuses Stage 25.8's deployment-status/backup-status endpoints + Stage 17.10's alerting; needs only a frontend screen.
- [ ] **26.1.3** Edge WAF/rate-limiting — depends on 26.1.1's hosting choice (reverse proxy/cloud WAF); app-level rate limiting (Stage 13.14) already covers this in the meantime.
- [ ] **26.1.4** Tenant plan/subscription/entitlement admin screen — module entitlements already exist server-side (Stage 14); needs a UI to set plan/quota per tenant.
- [ ] **26.1.5** Tenant usage metering / tenant-health dashboard — reuses the per-tenant concurrent-request cap counters (Stage 24.30).
- [ ] **26.1.6** Tenant-scoped export/restore (not just whole-DB) — extend Stage 17.3's backup engine to filter by tenant schema. **[needs scope decision: full per-tenant backup cadence vs. on-demand export only]**

### Phase 26.2 — External Credentials and Regulatory Integrations
All items code-complete already, blocked purely on real-world credentials (carried forward from Stage 20 Track A):
- [ ] **26.2.1** (=20.3) Non-production Shopify/BigCommerce/Magento credentials, then run `scripts/verify_connector_live.ps1`. **[needs user input]**
- [ ] **26.2.2** (=20.2) Real `OPS_ALERT_WEBHOOK_URL` + escalation contacts. **[needs user input]**
- [ ] **26.2.3** (=20.30) GSP/e-invoice/IRN sandbox credentials, then close the IRN flow — `engines/gst.go`'s calc engine is ready to feed a real GSP call once credentials exist. **[needs user input]**
- [ ] **26.2.4** (=20.31) e-way bill sandbox credentials, then close the e-way bill flow. **[needs user input]**
- [ ] **26.2.5** Payment-terminal (Pine Labs or similar) sandbox credentials for a real settlement test — Stage 25.7 already enforces terminal-mapping checks; only live settlement testing is missing. **[needs user input]**

### Phase 26.3 — Reachability and Usability Gaps (buildable now — highest value-per-effort, wires existing backend to a screen)
Already flagged as real gaps in `project_ledger.md` §29 (Stage 21.9); formalized as build items here.
- [ ] **26.3.1** GRN workbench screen — dedicated create/receive UI (barcode preview, excess/short/damage capture) against the existing GRN engine; today GRN has only a backend hook (Stage 1.4/5.1) and a report, no create screen.
- [ ] **26.3.2** Purchase Requisition entry screen + sidebar entry — backend approval flow already built (Stage 17.7), no frontend exists.
- [ ] **26.3.3** Approval Rules admin screen — expose the amount-slab→role routing config (`engines/approval.go`) for editing instead of a direct DB/API edit.
- [ ] **26.3.4** WMS operations screens — audit which of putaway/bin-condition-transition/cycle-count still lack frontend (some may already be partially covered by Stage 20 Track B.2); build whichever is missing.
- [ ] **26.3.5** Vendor invoice override — confirm Stage 24.11's approval-routed override has a real UI action, not just an API call; add one if missing.
- [ ] **26.3.6** PO amendment screen — confirm whether an approved-but-not-yet-received PO can be amended through a real edit path or only cancel-and-recreate; build an amendment flow if missing.

### Phase 26.4 — PIM/PXM Maturity Sprint
Per PDF §6.1, each extends existing Stage 15/16 PIM infrastructure — no parallel PIM model.
- [ ] **26.4.1** Attribute groups + locale/channel attribute overrides on the existing Family/Attribute framework (Stage 15.1).
- [ ] **26.4.2** Duplicate-item/content detection — reuse the Stage 25 master-data-validation choke point pattern (`engines/master_data_validation.go`).
- [ ] **26.4.3** Taxonomy versioning (family/attribute-set change history) — additive, audit-log-backed (reuse Stage 1.2's audit engine).
- [ ] **26.4.4** Media versioning + renditions + alt text + expiry on the existing Media/DAM library (Stage 15.2).
- [ ] **26.4.5** Content workflow: owner assignment + SLA + rejected-field comments on the existing approval-gated `ProductContent` (Stage 15.1) — additive fields, not a new engine.
- [ ] **26.4.6** Bulk approval + rollback-to-prior-content-version for PIM content.
- [ ] **26.4.7** Channel validation packs + per-channel diff preview before publish, on the existing Channel Publishing queue (Stage 16).
- [ ] **26.4.8** Marketplace error dictionary — map real connector error codes to the Stage 23 message catalog instead of raw passthrough.
- [ ] **26.4.9** Search/discovery feed export (synonyms, ranking attributes, merchandising flags) — a read-only export report off existing PIM data, not a search engine.
- [ ] **26.4.10** Supplier portal (submission + QC-approval workflow for supplier-provided content). **[needs product decision: supplier auth model — separate portal vs. limited-role login]**
- [ ] **26.4.11** AI content-assist scope. **[needs product decision — explicitly excluded until governance/audit/prompt-safety scope is defined, per the PDF's own §6.1 note]**

### Phase 26.5 — WMS Enterprise Maturity Sprint
Per PDF §6.2, extends Stage 20 Track B.2's Bin/putaway/pick/pack/cycle-count engines — `bin_stock` stays a breakdown of `inventory_availability`, never a second source of truth (Stage 20.17's own precedent).
- [ ] **26.5.1** ASN (advance shipment notice) capture before GRN — feeds 26.3.1's GRN workbench.
- [ ] **26.5.2** QC sampling on GRN — accept/reject/damage sub-quantities beyond today's whole-line accept.
- [ ] **26.5.3** Cross-dock/flow-through putaway rule (skip bin placement when a transfer/sale is already waiting).
- [ ] **26.5.4** LPN/carton/pallet grouping on top of `bin_stock` — additive.
- [ ] **26.5.5** Bin-to-bin replenishment min/max triggers — reuse Stage 10's replenishment-suggestion pattern, scoped to bins instead of vendor reorder. Design note (validated by a retired prototype, see `docs/specs/wms_master_blueprint_reference.md`): shortage = max_qty − current_qty per bin/SKU rule; fill from reserve locations holding that SKU, ordered by highest qty first, until the shortage is covered or reserves run out.
- [ ] **26.5.6** Wave/batch pick-list grouping — extend Stage 20.18's computed pick list. Design note (validated by a retired prototype, see `docs/specs/wms_master_blueprint_reference.md`): aggregate each SKU's total qty across every order in the wave, allocate that total FIFO/oldest-first across `AVAILABLE` stock, then distribute the allocated qty back per order-needing-it, and sort the resulting pick tasks by zone-then-bin for an S-shape walking route.
- [ ] **26.5.7** Short-pick handling (partial pick → variance/backorder flag).
- [ ] **26.5.8** Cartonization at pack step — extends Stage 20.19's pack/dispatch mapping.
- [ ] **26.5.9** ABC cycle-count planner (auto-schedule high-velocity SKUs more often) on Stage 20.20's cycle-count engine. A naive random-bin sampler (no velocity weighting) was prototyped in a retired standalone project and works as a placeholder/fallback if true ABC velocity-weighting isn't ready first — see `docs/specs/wms_master_blueprint_reference.md`.
- [ ] **26.5.10** Blind-count + recount workflow + variance root-cause codes — additive fields on `CycleCountLine`.
- [ ] **26.5.11** [P2 — tier/scope decision, don't build speculatively] Slotting/re-slotting optimizer, labor standards/productivity dashboard, RF/voice/mobile picking, 3PL multi-owner billing, robotics/conveyor/scale API integration — each needs a real warehouse-scale pilot to justify the investment.
- **(info, not a build item)** A standalone WMS project (separate Go service + React frontend) was independently started against a "Universal WMS Master Blueprint" PDF before this Stage 26.5 roadmap existed. Retired 2026-07-23 — its architecture conflicted with this repo's no-new-framework/single-server rules, and this Stage already covers nearly its entire feature scope. Durable design content kept at `docs/specs/wms_master_blueprint_reference.md`; retirement rationale at `project_ledger.md`.

### Phase 26.6 — Finance/Tax Close Sprint
- [ ] **26.6.1** P&L / Balance Sheet reports — new `ReportDefinition` entries off existing `gl_postings`, same pattern as Trial Balance/Ageing.
- [ ] **26.6.2** Cash-flow statement report — same framework, off `BankStatementLine`/`gl_postings`.
- [ ] **26.6.3** GL drill-down + vendor/customer ledger + tax-ledger reports — extends Stage 20.38's drill-down pattern.
- [ ] **26.6.4** Journal voucher + reversal + recurring-journal entry — posts through the existing `PostDoubleEntry`.
- [ ] **26.6.5** Payment-file (bank-file) generation for a payment-proposal batch + duplicate-UTR check — extends Stage 20.27.
- [ ] **26.6.6** Backdated-posting approval — route through the approval engine instead of a blanket period-lock rejection (Stage 17.4/24.6 already validate transaction date; this adds a signed-off override path).
- [ ] **26.6.7** Statutory audit export (structured full-GL export for a closed period) — reuses the async-export pattern (Stage 20.37).
- [ ] **26.6.8** Intercompany/cost-center/profit-center postings and reports — extends Stage 17.9's Department/CostCenter masters into finance postings.
- [ ] **26.6.9** e-invoice/IRN and e-way bill flows. **Blocked on 26.2.3/26.2.4** — the GST calc engine (Stage 17.5) is ready to feed a real GSP call once credentials exist.
- [ ] **26.6.10** `FIN-0260` period-locked posting code — still not wired (Stage 25.9: `PostDoubleEntry` has 28 call sites across 12 files, no single choke point). Real fix is a genuine refactor — introduce one wrapper every call site routes through, matching the "one choke point" pattern used elsewhere in Stage 25 — not a quick per-site patch.

### Phase 26.7 — CRM/Loyalty Sprint
- [ ] **26.7.1** RFM (recency/frequency/monetary) segmentation — a report over existing `SalesInvoice`/`POSCart`/loyalty-ledger data, no new customer data model needed.
- [ ] **26.7.2** Voucher/coupon issuance + redemption — new doctype, reuses the loyalty ledger's earn/burn pattern (Stage 13.13d).
- [ ] **26.7.3** Loyalty tiering + accrual/expiry rules — additive fields on the existing loyalty ledger.
- [ ] **26.7.4** Campaign definition (birthday/lapsed-customer triggers) + communication log — reuses the existing CleverTap outbound integration (Stage 9.1) for delivery.
- [ ] **26.7.5** Fraud/staff-restriction rules + OTP redemption on loyalty burn.
- [ ] **26.7.6** Points-liability report + campaign-ROI report — new `ReportDefinition` entries.
- [ ] **26.7.7** Customer 360 profile — a read-model report over existing customer-linked documents, not a new customer master.
- [ ] **26.7.8** [P2 — tier/scope decision] Customer householding/merge, CLV/cohort/churn analytics, two-way CleverTap segment sync — real, but meaningfully larger; scope after the above ship.

### Phase 26.8 — HR/Payroll Sprint
- [ ] **26.8.1** Shift roster / store schedule — extends the existing Attendance doctype (Stage 13.13a).
- [ ] **26.8.2** Salary structure + statutory deduction calc (PF/ESI/PT/TDS-on-salary) as a real payroll *processing* engine, a deliberate sibling to the existing payroll *export* (Stage 13.13a) — same pattern Stage 20.28 used for vendor-TDS.
- [ ] **26.8.3** Payslip generation + payroll-to-GL posting — reuses `PostDoubleEntry`, new payroll GL accounts.
- [ ] **26.8.4** Loans/advances against salary — new doctype, deducted in the payroll run.
- [ ] **26.8.5** Employee self-service: leave request + expense-claim submission from the employee's own login (the claims/approval flow already exists, Stage 13.13c — this is about self-initiated submission).
- [ ] **26.8.6** Onboarding/offboarding checklist + document locker — extends the Employee master with a checklist doctype.
- [ ] **26.8.7** [P2 — tier/scope decision] Full KRA/KPI appraisal cycles, training, grievance handling — needs HR-domain process design input before building.

### Phase 26.9 — Manufacturing/MRP Sprint
- [ ] **26.9.1** Multi-level BOM (a BOM referencing another BOM as a component) — extends Stage 13.13e's single-level BOM.
- [ ] **26.9.2** Alternate BOM + effective-dating — additive fields.
- [ ] **26.9.3** Work centers + routing (operations, setup/run time) — new doctype pair feeding the existing Production Order.
- [ ] **26.9.4** Scrap/yield factors on BOM lines + co/by-product output.
- [ ] **26.9.5** Basic MRP reorder suggestion for manufactured items — reuses Stage 10's replenishment-suggestion pattern rather than a new planning engine.
- [ ] **26.9.6** WIP tracking + partial completion + rework on the Production Order state machine (today linear: Draft → Material Issued → Completed).
- [ ] **26.9.7** QC gate before a Production Order can complete — reuses the approval engine.
- [ ] **26.9.8** Standard/actual costing + variance report on completed Production Orders.
- [ ] **26.9.9** [P2 — tier/scope decision] Finite/infinite capacity scheduling, subcontracting/outside-processing — needs a real manufacturing pilot customer before over-building.

### Phase 26.10 — Reports and BI Sprint
- [ ] **26.10.1** **Stock ledger wiring** (PDF Appendix A item 7) — `StockLedgerEntry`/`WriteStockLedgerEntry` exist but are dead code (confirmed 2026-07-20, still true 2026-07-23). Wire into GRN, checkout, transfer dispatch/receive, putaway, condition transitions, and cycle-count posting — everywhere `inventory_availability` already changes — then build the Stock Ledger report Stage 20.40 explicitly deferred for this reason. Before wiring: `WriteStockLedgerEntry` (`engines/inventory.go:49`) today only takes `item_id/warehouse_id/qty/voucher_type/voucher_id` — add `idempotency_key` (dedupe retried writes) and `from/to location_id` + `from/to status` + `user_id/device_id` as additive fields first, or the resulting report still can't show movement/location or survive a retried call. Field shape cross-checked against `docs/specs/wms_master_blueprint_reference.md` §4.
- [ ] **26.10.2** Attendance Summary report — deferred at Stage 20.40 for "no data model"; buildable once 26.8.1's roster/schedule doctype exists.
- [ ] **26.10.3** Role-based executive dashboards (KPI cards + trend charts) — a frontend-only layer over existing reports, reusing the `ReportDefinition` framework's role/column masking (Stage 20.39).
- [ ] **26.10.4** Scheduled report delivery (cron-style run + email/webhook drop) — extends Stage 20.37's async export with a schedule field, reuses the existing outbox-worker ticker.
- [ ] **26.10.5** Exception queues (stale approvals, failed syncs, negative-stock flags) as a dashboard widget — reuses Stage 17.10's SLA-breach monitor pattern.
- [ ] **26.10.6** [P2 — tier/scope decision] Dedicated data mart/read replica for heavy BI queries — only justified once real report-query load is measured against the live Postgres instance; don't build ahead of a measured bottleneck, per `CLAUDE.md`'s lightweight-first principle.

### Phase 26.11 — Security, Scale, UAT and Go-Live
- [ ] **26.11.1** External penetration test engagement. **[needs user input]**
- [ ] **26.11.2** Load/soak/spike test at 1/100/1000/2000-store scale — **partially buildable now**: `engines/scale.go` (the original Phase 5 concurrency simulator) already exists; re-run it against the current, much larger schema and record fresh latency/queue-lag/DB-utilization numbers instead of the original baseline.
- [ ] **26.11.3** DR/restore drill in a real production-like environment — needs 26.1.1's hosting decision first; the backup/restore mechanism itself is already proven (Stage 17.3 drill).
- [ ] **26.11.4** Migration rehearsal (apply every migration file in order against a fresh DB, not the accumulated dev DB) — buildable now; would have caught the Stage 11 `field_permissions`-missing-from-provisioning bug even earlier had it existed then.
- [ ] **26.11.5** Business UAT cycle with signed defect/closure log, using the existing `docs/guides/UAT_CHECKLIST.md`. **[needs real business users]**
- [ ] **26.11.6** Hypercare window definition (on-call owner, duration, rollback trigger) for the first pilot. **[needs user/business decision]**

**Verification note (2026-07-23):** `go build ./...` and `go vet ./...` clean; `go test ./... -p 1` reproduces exactly the two known pre-existing failures documented in 26.0.2 — nothing new, no code changed by this planning pass.

---

For the full chronological build narrative behind every item above, see **[docs/project_ledger.md](project_ledger.md)**. For environment setup, commit history, and known concurrent-session risk, see **[docs/ai_handover.md](ai_handover.md)**.
