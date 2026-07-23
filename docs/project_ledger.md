# Project Progress Ledger: Custom ERP

Chronological build-record. One section per Stage/session. Points back to `docs/micro_checklist.md` for full item-by-item detail rather than duplicating it.

---

## 1. Project Genesis

Started as a static, client-side HTML dashboard. Brand/Style data lived in a mock `db.js` and `localStorage`. No real database, no multi-tenancy, no numbering safety, no observability.

---

## 2. Architectural Decisions

| Component | Choice | Why |
|---|---|---|
| Backend | Go 1.22 | Single-binary, <15MB RAM/client, <10ms startup — cheap to host at density |
| Database | PostgreSQL 16.3 | Schema-per-tenant isolation + `SELECT ... FOR UPDATE` for ledgers/sequences |
| Multi-tenancy | Schema-per-tenant | Isolates tenant data, resolved dynamically per request |
| Branding | Custom ERP | Removed legacy sample branding for a clean, reusable system |

---

## 3. Project Rollout Ledger

```
[x] Phase 1: Core Foundation & Scale Infra (Event Bus, Outbox, Availability, Reservations)
[x] Phase 2: Single Vertical Pilot (PO-GRN-Barcode-Inventory-Transfers-POS-Finance)
[x] Phase 3: Omnichannel Pilot (Shopify Sync, Webhook Imports, Fulfillment Routing)
[x] Phase 4: Store Fulfillment (Ship-from-store, BOPIS, Return Anywhere, Task Dashboard)
[x] Phase 5: Scale Test (100 → 2,000 simulated stores)
[x] Phase 6: Marketplace/OMS Expansion (Settlements, Logistics, Support Console)
[x] Phase 7: Advanced Optimization (Forecasting, Replenishment, SLA tuning)
```

**Scope note**: Phases 1-7 cover the kernel + omnichannel/scale backend only — "COMPLETED" here does *not* mean the full ERP is done. POS/Finance/GST/CRM/HR/Assets UI, the approval engine, MFA, and most of the report catalog were built later (Stage 13+). See `docs/pdf_blueprint_gap_analysis.md` for the original gap analysis and `docs/micro_checklist.md` Stage 13 for the resulting backlog.

---

## 4-10. Phase 1-7 Build Records

- **Phase 1 — Numbering, Translation, Audit**
  - `engines/numbering.go`: `<Prefix><Sep><Store><Sep><FY><Sep><Number>`, row-locked (`SELECT ... FOR UPDATE`) against duplicates.
  - `engines/labels.go`: exact-match UI string translation via a DOM `TreeWalker`.
  - `engines/logs.go`: audit log writer + panic recovery (stack trace → DB, correlation UUID, HTTP 500).
- **Phase 2 — DocType Engine, Dynamic Forms, Bulk Import**
  - `engines/doctype.go`: registers custom document schemas + field metadata dynamically.
  - `public/app.js`: renders any doctype's fields (Data/Number/Select/Link) into one dynamic modal.
  - `engines/import.go`: CSV import with case-insensitive field mapping, validation, and a cell-level error report.
- **Phase 3 — Industry Profiles**
  - `public/profiles/`: Jewelry, Food & Beverage, Automobile, Clothing presets — swaps field definitions/sequences/vocabulary per tenant.
- **Phase 4 — Finance, Fulfillment, Returns**
  - `engines/finance.go`: balanced double-entry postings (Cash+COGS debit / Sales+Inventory credit at checkout).
  - `engines/fulfillment.go`: picking tasks with auto re-routing on reject; Return Anywhere with balanced refund postings.
- **Phase 5 — Scale Test**
  - `engines/scale.go`: 2,000-store concurrency simulation — ~456 TPS @ 100 workers, 0 conflicts, ledger balanced.
- **Phase 6 — Marketplace**
  - `engines/marketplace.go`: settlement reconciliation (validates payout math, posts GL) + carrier dispatch tracking.
- **Phase 7 — Optimization**
  - `engines/optimization.go`: replenishment suggestions, sales forecasting, SLA-breach monitor.
  - **Bug fixed (2026-07-12)**: velocity queries checked `status = 'completed'`, a status the system never writes. Fixed to `IN ('Paid', 'Settled')`.

---

## 11a. SaaS Provisioning, Feature Flags, Integration Retries

- **Tenant provisioning** (`engines/saas.go`): clones 19 tables per new tenant. Fixed 2026-07-12: each tenant now gets a unique, freshly-generated admin password — no more shared placeholder hash.
- **Feature flags**: `SetFeatureFlag` is wired; `IsFeatureEnabled` wasn't checked by any handler at this point (closed later).
- **Integration log viewer/retry**: backend endpoints existed; frontend Log Hub didn't call them yet (closed later).
- Full gap list: `docs/operations/hardening_roadmap.md`.

## 11b. Real Login Flow (2026-07-12)

- Closed `hardening_roadmap.md` Phase 1.1 — previously `apiMiddleware` silently granted HR/Admin access with no token at all.
- New login screen, logout button, 401 handling that returns to login instead of a dead end.
- All 4 seed users reset to unique bcrypt hashes; plaintext dev creds in `DEV_CREDENTIALS.local.txt` (gitignored).
- Token expiry added: `exp` claim, 24h default, `JWT_EXPIRY_HOURS` override.

---

## 11. Version Control & Git History

- **Remote**: `https://github.com/Vishalnayak226/custom_erp.git`
- **Branch**: `main`
- **Latest commit**: `c45f934` — "Stage 25 fully closed (Batches 4-6, 25.8-25.10) + Stage 22.6 role-filtered sidebar" (2026-07-23)
- This section is a point-in-time pointer, not updated every commit. `docs/ai_handover.md` §6 is the actively-maintained rolling pointer — confirm with `git log --oneline -5` before trusting either.

---

## 12. Stage 13 — Master Blueprint Functional Scope (2026-07-12 → 17)

Closed the "business-user-facing ERP" gap: strong kernel/omnichannel architecture, but thin on day-to-day modules. Every item shipped as its own commit, live-verified on a throwaway instance first.

- **13.1-13.3** Security headers (CSP/HSTS/etc.) + `featureGate()` wrapper.
- **13.4-13.7** POS, Finance/GL, Fulfillment, Marketplace screens for existing backend. Fixed 2 location-filter bugs hiding records from non-admins.
- **13.8** Maker-checker approval engine (`engines/approval.go`) — amount-slab + role + location routing. Reused by nearly every later approval-gated feature. Hardened later against a TOCTOU double-decision race.
- **13.9-13.10** Vendor/Customer masters, Item GST fields.
- **MFA**: TOTP (RFC 6238, stdlib-only), required for HR/Admin.
- **13.11** Report catalog: Current Stock, Sales Register, Vendor Ledger, Payables Ageing.
- **13.12** RFQ / vendor quote comparison.
- **13.13a-e** HR Foundation, Fixed Assets, Expense Management, CRM/Loyalty (scoped MVP), Manufacturing (scoped MVP).
- **13.14** Per-category rate limiting (`ip:category`, not just `ip`) — fixed cross-endpoint interference.
- **13.15** Sticker/barcode printing (text labels, not scannable symbology).

Full detail: `docs/micro_checklist.md` Stage 13.

---

## 13. Stage 14-17 (2026-07-18 → 19)

- **Stage 14 — Control plane**: `promote.ps1`/`manage.ps1 -Env`/`environments.json` deployment pipeline; module governance; patch automation; extension isolation; security hardening (account lockout, Shopify HMAC verification); Go 1.22.5 → 1.22.12.
  - Docker was built on request, then **reverted on request** — standing policy is no Docker dependency.
- **Stage 15 — PIM Foundation**: Family/Attribute framework, approval-gated `ProductContent`, completeness engine, first file-upload infra, stub channel-publish queue, CSV import/export preview.
- **Stage 16 — Real connectors**: Shopify/BigCommerce/Magento (code-complete, live-store verification deferred pending credentials); PIM dashboard/bulk-edit/reports/field-permissions.
- **Stage 17 — Execution queue**:
  - 17.1 soft-delete (additive `deleted_at`)
  - 17.2 CSV formula-injection sanitization
  - 17.3 backup/restore baseline
  - 17.4 accounting-period control
  - 17.5 GST enforcement at PO + checkout
  - 17.6 transfer-order dispatch/receive lifecycle
  - 17.7 purchase requisition workflow
  - 17.8 vendor invoice + 3-way match + payment
  - 17.9 Location/LegalEntity/Department/CostCenter masters

Full detail: `docs/micro_checklist.md` Stages 14-17.

---

## 14. Stage 17.10-17.11 (2026-07-19)

Both blocked only on real-world input, not implementation.

- **17.10 Runbook + alerting**: `docs/operations/incident_runbook.md` (P0-P3, rollback, log locations); `engines/alerting.go` (`SendOpsAlert` → Slack/Teams webhook, safe no-op when unset). Verified against a mock webhook receiver. **Still needs**: real escalation contacts + a real webhook URL.
- **17.11 Live connector verification**: `scripts/verify_connector_live.ps1` drives a real publish given real credentials. Not run end-to-end — no real store credentials exist in this environment.

---

## 15. Stage 19 — Docs Suite + Folder Restructuring (2026-07-19)

- `docs/ERP_BLUEPRINT.md` — full project snapshot for an outside reader.
- Go restructuring: `main.go` (~4,681 lines) moved from repo root into `internal/server/`, split into 8 domain files; new thin `cmd/server/main.go` entrypoint. Same-package reorganization — no cross-package visibility risk.
- `docs/` reorganized into `architecture/`/`specs/`/`operations/`/`requirements/`/`guides/`; nothing deleted, every cross-reference fixed.
- New `docs/requirements/BRD.md`/`PRD.md`, `docs/guides/USER_GUIDE.md`/`ADMIN_GUIDE.md`.
- Found and fixed while in the area: a `$args` shadowing bug and 3 unapproved-verb function names in `promote.ps1`.

---

## 16. Stage 18 — Dropdown/Autosuggest UX (2026-07-19)

- Audited every create/edit form; the two originally-named examples (Chart of Accounts, GRN screen) don't exist as buildable targets — scoped instead to real free-text Vendor/Location/Customer/Item/Employee fields.
- Stayed UI-only by design — no schema/validation changes, matching Stage 17.9's precedent.
- New `attachTypeahead()` component wired into 12 fields across 7 screens.
- **Bug fixed**: `openMenu()` wiped its own results before rendering them.
- **Bug fixed (pre-existing, Stage 17.9)**: Location/LegalEntity/Department/CostCenter were gated to a `'core'` module never registered anywhere — every role got `403` on every read/write since Stage 17.9 shipped.
- **Gap flagged, not fixed**: Store Manager/Cashier lack read access to Vendor/Item/Customer, so the new pickers are code-correct but not usable by those roles yet.

---

## 17. Stage 21 — UI/UX Overhaul (2026-07-20)

Started from 2 screenshotted bugs (Finance/GL clipping, a raw OS prompt), broadened per the user's own follow-up choices.

- **Root cause, one shared rule**: `.table-panel` was `overflow: hidden` in a flex layout that already sized correctly — one-line fix to `overflow: auto` made the existing sticky-header rule work app-wide.
- Fixed 2 more CSS bugs found in the same pass (unstyled KPI numbers, dead icon rules).
- Migrated the last 4 raw `window.prompt()` calls to the app's custom dialog.
- Icon-minimal sidebar (26 per-item icons removed, user's explicit choice).
- Account-menu + sign-out redesign; new self-service **User Profile** screen + idle-timeout auto-logout.
- Added `CLAUDE.md`'s "first principle" (lightweight/future-proof/solid) per the user's mid-session request.
- **Numbered 21, not 20** — collided with a concurrent session's Stage 20 backlog draft; caught via `git status` and renumbered before shipping.

---

## 18. Stage 20 Track B.1 — POS Maturity (2026-07-20)

- `POSProfile` master + `POSSession` doctype — checkout now requires an Open session; session close computes cash variance server-side.
- Payment-mode-aware GL posting: Cash (1100) / Card (1101) / UPI (1102), instead of always 1100.
- Discount above a configurable % routes through the approval engine before the sale completes.
- **Bug fixed**: `POSCart.created_by` was hardcoded to `'system'` for every sale ever — would have silently defeated the maker-checker self-approval block for this exact feature.
- Also closed: 20.11 (POS-side return entry), 20.14 (receipt printing). 20.13 (offline queue) left open pending a decision (built later, §31).

---

## 19. Stage 22 — Full Page QA Sweep (2026-07-20)

- Playwright sweep hit all 60 reachable pages — zero console/5xx errors, but **Inventory, Transfers, Users, and Roles were dead "Module Setup Pending" mocks** (pre-existing).
- Built all four for real: Inventory (existing report + search box), Transfers (new bespoke view), Users/Roles (new HR/Admin-only endpoints).
- **Bugs fixed**: the generic update path replaced (not merged) the whole JSONB blob; a duplicate-username create leaked a raw Postgres error to the UI.
- Guides updated with the walkthroughs that never existed, plus an honest correction that the sidebar isn't actually role-filtered (closed later, §33).

---

## 20. Stage 20 Track B.2 — WMS Maturity (2026-07-20)

- `Bin` master + `bin_stock` table back putaway, a computed bin-grouped pick list, and Good/Damaged/QC-Hold/RTV condition transitions.
- `TransferOrder` gains an optional Approved→Packed step before dispatch (additive).
- `CycleCountLine` reuses the existing `BulkImportCSV` engine + the approval engine for variance sign-off.
- **Serious bug found and fixed**: the shared quantity-parser didn't handle CSV string values, silently reading every imported count as zero — one test run posted a **-100** adjustment where **-10** was correct. Fixed and re-verified across zero-variance/shortfall/overage cases.

---

## 21. Sidebar Redesign + Terminology Renames (2026-07-20)

- "DocType Builder" → "Database Schema Design"; "Log Hub" → "Activity Log".
- Sidebar: ~27 flat links collapsed into 12 module-grouped hover flyouts.
- **Bug fixed**: flyout `max-height` was viewport-flat, so a long flyout anchored low in the list ran off the bottom — now computed per-instance.
- **Tried and reverted same day**: a dimming backdrop behind an open flyout — user found the fade felt like an unwanted transition.
- "DocType" swept to "Record Type" everywhere user-facing; internal identifiers left alone.
- **"Stores" master fix — 3 compounding bugs**: zero fields, zero `role_permissions` (default-deny for every role), and a sidebar typo (`'Store'` vs the registered `'Stores'`). Fixed via migration + a one-line JS fix.

---

## 22. Standardized Error/Message Catalog — Stage 23 (2026-07-20)

User supplied a 301-row message-standardization spreadsheet. It didn't exist yet: errors were ad hoc (205 plain-text, 152 hand-rolled JSON), and `apiMiddleware`'s `Content-Type: application/json` header meant every plain-text error silently broke the frontend's JSON parsing.

- Generated `internal/server/error_catalog_generated.go` (302 codes) from the xlsx via `scripts/gen_error_catalog.py`.
- `internal/server/apierror.go`'s `writeAPIError`/`writeAPIErrorGeneric` — the one place every error response is written from.
- Wired all framework-level paths (panic, rate limit, auth, module/feature gates) + all ~400 existing handler call sites (18 precise codes, 382 generic-but-consistent).
- New `showToast()`/`renderPageBanner()` frontend primitives.
- **Structural finding**: the catalog has zero rows at HTTP 400/405, while much of the codebase used exactly those — flagged as a decision gap (resolved in the follow-up, §25).

Full design: `docs/specs/message_catalog.md`.

---

## 23. Stage 20 Track B.3 — Finance Maturity (2026-07-20)

- New doctypes: `BankAccount`, `BankStatementLine`, `PaymentProposal`, `TDSSection`, `DebitNote`, `CreditNote`.
- New engines: `bank_reconciliation.go`, `payment_proposal.go`, `tds.go`, `notes.go`.
- **Interesting find**: `SalesInvoice` existed since Stage 1 as a registered doctype with zero amount field/GL posting/frontend — a dormant shell. New `engines/sales_invoice.go` wires it up for real, giving the new Receivables Ageing report a genuine receivable to age.
- Closed 2 frontend gaps found along the way: VendorInvoice and Accounting Periods both had working APIs but zero screens.
- 5 new Finance screens, 2 new Reports tabs.

---

## 24. Stage 20 Track B.4 — Reports Engine (2026-07-20)

Stage 20 is now fully closed except Track A and 20.30/20.31 (GSP-blocked).

- New `ReportDefinition` framework (`engines/report_registry.go`) — columns/params/a `Run` function/an optional drill-down, registered once per report. Not a full ad hoc SQL builder (an injection risk out of scope).
- Saved filters + async export (reuses the existing outbox-worker ticker shape).
- Column-level masking redacts Sensitive columns below Store Manager/HR-Admin.
- 7 new reports added (GRN Register, Cash Book, Bank Book, Asset Register, Loyalty Ledger Summary, Production Order Status, RFQ Comparison).
- **2 candidates deliberately skipped**: Attendance Summary (no data model exists) and Stock Ledger (the write function exists but is dead code, never called).

---

## 25. Stage 23 Follow-Up (2026-07-21)

Stage 23 closed with 2 open product decisions + 4 backlog items. This session decided and built.

- **Decided**: standardize on HTTP 422, not a forked 400 variant — the catalog is the one authoritative source. 186 call sites converted.
- **Decided**: keep `featureGate`'s fail-closed 403, not the matrix's soft-200 — a disabled-feature response should never look like success.
- **23.9**: `handlers_finance_maturity.go` swept onto the standard error envelope.
- **23.8**: rather than migrating ~70 screens by hand, found the single choke point (`showApiError()`) and made it dispatch on a new `display_style` field — Toast/Page Banner now work with zero per-screen changes.
- **Regression found, not fixed here**: `TestEngines/FinanceDoubleEntryAndPOS` fails a trial-balance assertion — confirmed pre-existing via `git stash`, unrelated to this session.

---

## 26. Stage 25 Batches 1-2 — Mature-ERP Validation Coverage (2026-07-21)

Stage 23 had backlogged "~187 unwired Mature-ERP codes." Recount found only 7 of 187 actually wired — but the other 180 sit almost entirely inside modules already built (Stage 1-22), not in unbuilt subsystems.

- **Batch 1**: added a `ValidationError{Code, SubFor, Message}` type to `engines/doctype.go`'s `ValidateDocument` — the one generic validation path every doctype already runs through. One shared change gives every doctype precise codes for its 4 existing scenarios.
- **Batch 1 bonus**: a real new check — `CreateAccountingPeriod` had no start-after-end-date validation.
- **Batch 2**: new `engines/master_data_validation.go` — 6 real Master Data checks (Item HSN format/required-when-taxable, duplicate barcode; Vendor GSTIN/bank-account format; Customer mobile format).
- Remaining Master Data codes deliberately deferred — the underlying fields (`tax_category`, `uom`, `mrp`, `pan`, etc.) don't exist on the doctype yet.

Full per-code breakdown: `docs/micro_checklist.md` Stage 25.

---

## 27. Stage 24 Audit — Found It Already Built (2026-07-22)

Asked to start building Stage 24. Before writing code, read the actual source for each item — found **all 24 build items already implemented and committed** in `99d63de`, which a concurrent session had built alongside Stage 20 Track B.3/B.4. The checklist was simply never flipped from its planning-draft state.

- Re-verified every item individually against live source, not the commit message alone.
- Live-verified the highest-risk ones: token `loc` claim, role gates + path-traversal allowlist, optimistic-lock 409/version-bump, health endpoint, debug-panic gate (`ENV=production` → real 404), JWT `iat`/`jti`.
- **One open thread**: a JSON-unmarshal-error sweep left 1 safe production-code occurrence, deliberately left alone since the file was mid-edit by a concurrent session.
- Only 24.27-24.32 remained genuinely open at this point (built same day, §30).

---

## 28. Stage 25 Batch 3 (2026-07-22)

Manufacturing / HR-Payroll / GRN / Purchase Order+RTV / Sales-POS+Sales Return / Stock Transfer / Vendor Invoice / WMS / Fixed Assets / Expense — 75 codes.

- **29 of 75 wired**, 46 deliberately deferred — several engines are genuinely thin MVPs by original design, and Purchase Return/RTV (5 codes) references a doctype that was never actually built.
- New `engines/transactional_validation.go` — same choke-point pattern as Batch 2, reused across 6 doctypes.
- **Real gap closed**: `ProcessReturnAnywhere` took an unverified free-text order ID with zero cross-check against any real sale. Now resolves against a real `POSCart`/`SalesInvoice`.
- **2 bugs found and fixed during live verification**: a fully-designed check (`GOODSR-0095`) was never actually coded; a GRN-received-quantity query didn't exclude cancelled GRNs.

Full per-module breakdown: `docs/micro_checklist.md` Stage 25.4.

---

## 29. Stage 21.9 — Full User/Admin SOP Docs (2026-07-22)

- New `docs/guides/USER_SOP.md` (437 lines) / `ADMIN_SOP.md` (233 lines) — literal click-by-click procedures, grounded in a full read of `app.js`'s render functions.
- **Real gap found and fixed, not just documented**: every generic record-list screen had Delete but no Edit action at all. Backend already supported it — added an Edit button, prefillable modal, and optimistic-locking conflict handling.
- Live-verified: an edit with the correct `expected_version` persisted and bumped the version; a stale version correctly `409`'d.
- Other gaps found and left documented (not fixed): no GRN-creation screen, no `PurchaseRequisition` sidebar entry, WMS bin ops are API-only, no Approval Rules admin screen.

---

## 30. Stage 24, Items 24.27-24.32 (2026-07-22)

The deferred/needs-decision half of Stage 24, built on explicit request.

- **24.27**: a startup guard (`EnforceNoDefaultAdminCredentialInProduction`) refuses to boot in `ENV=production` while the default admin seed hash is still active.
- **24.28**: full password-reset flow via stdlib `net/smtp` — no enumeration oracle, token hash stored not raw, live-verified end-to-end.
- **24.29**: a stdlib-only circuit breaker for connector calls (5 failures → 30s open → 1 half-open trial), proven by a new test.
- **24.30**: a per-tenant concurrent-request cap (15) instead of a full per-tenant DB pool.
- **24.31**: a `doctype_fields.max_length` column + a 10,000-char blanket default, wired into the one shared validation path.
- **24.32**: `BulkImportCSV` now batches 500 rows per transaction instead of one giant transaction.
- **New regression found (DB-state drift, not a code bug)**: `TestEngines/DocTypeValidationAndAuth` fails on an unrelated `Brand` field accumulated from earlier industry-profile testing.

---

## 31. Stage 20.13 + 21.11 (2026-07-22)

- **20.13 decisions**: offline window = one shift, tied to the cashier's open session; an offline sale always posts once synced and may push stock negative, flagged for review.
- **20.13 build**: `localStorage`-backed offline queue in `app.js`, replays on reconnect; reuses the existing `cart_number` idempotency key — no new dedup mechanism.
- **Bug fixed**: the session-close success alert referenced an undefined variable.
- **21.11 decision**: Odoo/ERPNext-style sidebar module naming (POS→Point of Sale, Finance→Accounting, etc.), scoped to the ~12 top-level labels only.
- Live-verified both items end-to-end on throwaway instances; all test data removed.

---

## 32. Stage 25 Batch 4 (2026-07-23)

Admin/Config, DocType/Metadata, Reports, Data Import, Procurement/RFQ, Notifications, Mobile/Device, Observability, Customer/CRM, Approvals, Order Mgmt.

- Built across two sessions working this batch concurrently — confirmed no line-level overlap; the two efforts turned out complementary.
- **~30 of ~57 codes wired combined**.
- **2 bugs fixed**: `handleSequence` mapped a client-correctable config error to a blanket 500 instead of 422; `SaveFieldDefinition`'s upsert silently overwrote any existing/system field reusing a name.
- New pattern established: a **log-only tag** for scenarios where the catalog wants a blocking rejection but this codebase already made a deliberate non-blocking design choice.
- `handlers_report_engine.go` brought onto the Stage 23 standardized error envelope (had never been swept).

---

## 33. Stage 22.6 + Stage 25 Batches 5-6, 25.8-25.10 — Stage 25 Fully Closed (2026-07-23)

- **22.6**: sidebar visibility now derives automatically from `role_permissions` (not a hardcoded allowlist) — new self-service `GET /api/v1/me/permissions`.
- **25.6 (Channel Connectors + Omnichannel)**: 4 of 10 real. **Bug fixed**: a circuit-breaker-open publish job was marked `Failed` instead of `Queued`, contradicting the breaker's own retry design.
- **25.7 (POS Cash Drawer + Extension Hooks)**: 5 of 6 real — the highest hit rate of any batch. Cash-drawer variance-tolerance check and Pine Labs terminal-mapping check both went from "computed but never checked" to enforced.
- **25.8 (new API surface)**: deployment-status and backup-status endpoints (reusing existing `public.deployments`); a new `SAAS-0193` subscription-limit check. **2 gaps fixed along the way**: a failed deployment recorded nothing; a backup's checksum sidecar was never verified before restore.
- **25.10**: re-audited, confirmed the remaining 8 Global/Common codes are still genuinely generic.

**Stage 25 is now fully closed** — all 6 batches plus 25.8/25.9/25.10.

---

## 34. Stage 26 Planning — Final Leg Maturity Completion Plan (2026-07-23, no code)

User supplied `ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf` — a deepened successor to the plan behind Stage 20, with a 12-phase (0-11) roadmap and per-module final-leg tables (PIM, WMS, POS, Finance, Procurement, CRM, HR, Manufacturing, Reports, SaaS Ops). Read in full, then cross-checked line-by-line against live repo state rather than transcribed as-is.

- **Two stale/incorrect claims in the PDF, found and corrected:**
  - Industry Packs: the PDF claims only 4 profiles exist and asks for 7 more (Pharma, Medical Device, Steel, Construction, Agriculture, Semiconductor, Logistics/Transportation). `public/profiles/` actually already has 10 real profile files (Stage 12.1, 2026-07-21) — 6 of the PDF's 7 requested packs already map onto existing ones. Only **Logistics/Transportation** is genuinely new.
  - P0-1 finance regression: the PDF assumes `TestEngines/FinanceDoubleEntryAndPOS`'s 9000-vs-9500 mismatch is a broken double-entry posting bug needing a code fix. Re-ran `go test ./... -p 1` fresh (previously only confirmed via `git stash` across commits, never re-examined this closely) — the trial balance itself is `balanced:true` with debits==credits==9500 throughout; the *test's own fixture expectation* is 500 short, not the ledger. Combined with every prior session finding this reproduces identically regardless of what changed, root cause is almost certainly accumulated fixture debris in the one shared, persistent `custom_erp` dev DB every `go test` run writes to (no per-run isolation) — not broken finance code. Same root cause explains the `DocTypeValidationAndAuth` failure (`Brand.fefo_enabled` leftover from earlier industry-profile testing). Real fix identified: isolate/reset test fixtures before asserting exact totals, not patch `engines/finance.go`.
- Broken into **Stage 26** (`micro_checklist.md`), ~85 items across `26.0`-`26.11` matching the PDF's own phase numbers, each item phrased to reuse an existing engine/pattern (approval engine, `BulkImportCSV`, `ReportDefinition`, master-data-validation choke point, audit engine) rather than proposing a parallel mechanism — consistent with every prior stage's own precedent.
- Several PDF-requested capabilities flagged `[P2 — tier/scope decision]` rather than built or silently dropped: WMS slotting/RF-voice/robotics/3PL billing, full HR appraisal cycles, manufacturing finite scheduling/subcontracting, CRM CLV/churn analytics, a dedicated BI data mart — each genuinely useful but not justified without a real pilot-scale customer or a measured bottleneck first.
- Full rationale/benchmark detail: `docs/specs/erp_maturity_master_plan.md` (rewritten in place for this PDF, old 2026-07-19 plan kept as its own §6 history rather than a separate file).
- **No code changed this session** — `go build`/`go vet` clean, `go test ./... -p 1` reproduces exactly the two known pre-existing failures above, nothing new.

---

## 35. Standalone WMS project retired, knowledge migrated (2026-07-23, no code)

User had independently started a second project (`Antigravity Projects/WMS`) before this repo's own Stage 20 Track B.2/Stage 26.5 WMS work existed: a separate Go service (`custom_wms` module, port 8081) plus a React/Vite/TypeScript frontend (port 5173), sharing the Postgres cluster via its own `wms_tenant_<id>` schema convention, built against a 24-section "Universal WMS Master Blueprint" PDF. User asked for everything of value to be migrated into this repo so the standalone project could be scrapped entirely.

- **Read in full and compared against live repo state**, not transcribed as-is: the standalone project's own docs (`implementation_plan.md`, `DEVELOPMENT.md`, `micro_checklist.md`, `ai_handover.md`), its ~3,100 lines of Go domain code (`domain/inventory/engine.go`, `domain/outbound/{allocation,wave}.go`, `domain/replenishment/generator.go`, `domain/audit/generator.go`, `domain/inbound/putaway.go`, plus SSO/rate-limit middleware), its 1,391-line single-file React dev console, and the full 44-page source blueprint PDF.
- **Finding: this repo already has almost everything the standalone project was trying to build.** `engines/wms.go` + `inventory.go`/`transfer_orders.go`/`fulfillment.go`/`sourcing.go`/`optimization.go`/`outbox.go` (Stage 20 Track B.2, done) already cover Bin master, putaway, bin-grouped pick lists, Good/Damaged/QC-Hold/RTV condition transitions, approval-gated cycle counting, and vendor replenishment suggestions — in-process, same tenant/schema convention, same `documents` table model, vanilla-JS frontend. This repo's own **Stage 26.5 "WMS Enterprise Maturity Sprint"** roadmap (sourced from a *different* PDF, `ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf`) already mirrors the blueprint's Phase-2/3 capability map almost item-for-item — ASN, QC sampling, cross-dock, LPN/carton, bin-to-bin replenishment, wave/batch picking, short-pick, cartonization, ABC cycle-count, blind-count. The two blueprints were very likely authored for/by the same person; the gap was never "unknown to this repo," only "not yet built."
- **Architecture conflict, not just duplication**: the standalone project's separate Go service + React/Vite frontend + per-tenant-schema-per-service convention directly violates this repo's `CLAUDE.md` (no new frontend framework, no new third-party dependency, one lightweight server). Its Go code was read in full but **not copied** — no source file, dependency, or schema convention was ported.
- **What was kept**: the blueprint's durable, architecture-agnostic content (inventory state machine, movement-type catalogue, RBAC matrix, barcode/label rules, KPI list, SOP topic list, UAT test-scenario table, hard rules) written up in a new file, **`docs/specs/wms_master_blueprint_reference.md`**, cross-referenced against what this repo already has line by line. Two concrete, previously-undocumented gaps surfaced in the process and were annotated inline on the existing `micro_checklist.md` items rather than filed as new ones: (1) `engines/stickers.go` prints text labels, not scannable barcode symbology — worth knowing before anyone builds a real scanner screen against 26.3.4/26.5; (2) `WriteStockLedgerEntry` (dead code, tracked at 26.10.1) is missing `idempotency_key` + from/to location/status + user/device fields the deferred Stock Ledger report actually needs — this is the concrete reason that report was deferred, not just "not built yet." Design-note annotations (not new items) also added to 26.5.5 (bin-to-bin replenishment shortfall/fill logic), 26.5.6 (wave aggregation + S-shape sort), and 26.5.9 (a naive random-sample cycle-count fallback), each validated by the retired prototype's working-but-MVP-quality implementation.
- **No code changed this session** — docs/checklist only. **Standalone folder deleted this session** (2026-07-23) — the user reviewed the migrated docs above and explicitly chose full deletion (not archive) once satisfied; the folder's own local git history went with it.

---

## 36. Standalone OMS project retired, knowledge migrated (2026-07-23, no code)

User had independently started a third project (`Antigravity Projects/OMS`) before this repo had any dedicated order-management work: a separate Go multi-tenant service (own `go.mod`, JWT/OAuth verification, canary version routing, planned WebAssembly-sandboxed customer extensions via `wazero`) plus a mock/localStorage-driven vanilla-JS frontend (`index.html`/`js/{app,state,mockData}.js`/`js/services/*.js`, role-selector standing in for real auth), built against two PDFs — `Inhouse_OMS_Master_Blueprint.pdf` (30-section, four-engine OMS design) and `Inhouse_OMS_Module_BRD_Pack.pdf` (17 module BRDs). User asked for everything of value to be migrated into this repo so the standalone project could be scrapped entirely — same request as the WMS retirement one day earlier (§35), and handled the same way.

- **Read in full and compared against live repo state**, not transcribed as-is: both source PDFs, the standalone project's own docs (`README.md`, `docs/architecture_plan.md`, `docs/development_plan.md`, `docs/ledger.md`, `docs/ai_handover.md`), and its code (`js/app.js`/`state.js`/`mockData.js`/`services/{order,inventory,allocation,fulfillment,shipping,return}.js`, `src/main.go`, `src/auth`, `src/config`, `src/db`).
- **Finding: this repo already has real, working MVP-thin pieces covering a meaningful slice of OMS scope**, unlike the "almost nothing existed" WMS case — `engines/sourcing.go` (`ImportChannelOrder`, `MapChannelProduct`, `FindBestFulfillmentNode`), `engines/inventory.go` (`CreateReservation`, `GetAvailableToSell`, row-locked and already concurrency-safe), `engines/fulfillment.go` (`FulfillmentTask`, `ProcessReturnAnywhere`), `engines/marketplace.go` (`CreateLogisticsBooking`, `ProcessMarketplaceSettlement`), and `engines/unicommerce.go` (a second channel-import path reusing the generic outbox/retry pattern). But **unlike WMS, this repo's Stage 26 roadmap had no Order-Management phase at all** — the gap was real backlog structure, not just unbuilt items inside an existing phase.
- **Architecture conflict, not just duplication**: the standalone project's separate Go multi-tenant service + mock vanilla-JS frontend + its own 3-service assumption (ERP:8080 / OMS client-side / WMS:8081, now stale since WMS was absorbed directly per §35) directly violates this repo's `CLAUDE.md` (no new frontend framework, no new third-party dependency, one lightweight server). Its Go and JS code was read in full but **not copied** — no source file, dependency, or routing/sandboxing convention was ported.
- **What was kept**: the two blueprints' durable design content (four-engine model, domain model, multi-level status architecture, the 7-term ATP formula, allocation strategies, pick/pack/scan rules, courier/AWB/manifest requirements, cancellation stage matrix, return/RTO/QC/refund flow, exception/reconciliation pattern, report list, config masters) written up section-by-section against live code in a new file, **`docs/specs/oms_master_blueprint_reference.md`**. A direct gap-check (`grep -r "Hold|OrderHold|ReasonCode|reason_code|CancellationReason" engines/`) confirmed no order-hold or cancellation-reason-code mechanism exists anywhere today, closing an open question rather than leaving it assumed. Folded into a new **`Stage 26.12` — OMS/Order Management Maturity Sprint** (`micro_checklist.md`), 10 items, each phrased to extend an existing engine (`FindBestFulfillmentNode`, `FulfillmentTask`, `CreateLogisticsBooking`, `ProcessReturnAnywhere`, `inventory_availability`, the existing outbox/`ReportDefinition`/doctype-meta frameworks) rather than propose a parallel one — same discipline as every prior Stage, including 26.5's WMS sprint the day before.
- **No code changed this session** — docs/checklist only.
- **Standalone folder deleted this session** (differs from §35's WMS entry, which left the folder pending user confirmation) — the user explicitly asked for deletion once migration was verified. Verified the reference doc's coverage against the standalone project's full file list before deleting; its GitHub remote (`custom_oms`) preserves the history regardless.

For the current build tracker, see **[docs/micro_checklist.md](micro_checklist.md)**.
