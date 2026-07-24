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

---

## 37. Stage 26.4 — PIM/PXM Maturity Sprint built and closed (2026-07-23)

All 9 buildable items (`26.4.1`-`26.4.9`) built, migrated, and live-verified in one pass; `26.4.10`/`26.4.11` remain `[needs product decision]` per the checklist, untouched. New migration `db/migrations_stage26_4_pim_maturity.sql`.

- **26.4.1** (attribute groups + locale/channel overrides): new `ProductAttributeGroup` doctype, `ProductAttributeDef.group` link, `ProductAttributeValue.locale`/`.channel` optional fields. `engines.ResolveAttributeValue` (new, `engines/pim.go`) resolves the most-specific override (locale+channel > locale > channel > global blank/blank) in one query via an `ORDER BY` specificity trick rather than a procedural fallback chain; wired into both `CalculateCompleteness`'s readiness check and `BuildChannelPayload`'s outbound payload build (previously only a flat `itemCode::attributeCode` lookup existed, no override concept at all).
- **26.4.2** (duplicate detection): extended `engines/master_data_validation.go`'s existing `ValidateMasterDataRules` choke point — a same-family duplicate-Item-name check and a same-language duplicate-ProductContent-title check (both plain `fmt.Errorf`, no new catalog code invented, since the Stage 23 catalog is generated from an external spreadsheet this session has no access to). `ProductContent` wasn't previously gated through `ValidateMasterDataRules` at all (only Item/Vendor/Customer were) — added that wiring in `handlers_core_doc_engine.go` with the same effective-id resolution the Item block already used.
- **26.4.3** (taxonomy versioning): zero new storage. `engines/pim_taxonomy.go`'s `GetTaxonomyHistory` reads the audit trigger's existing `audit_logs` rows (Stage 1.2, `db/migration.sql` section 16), anchoring on the CREATE/UPDATE message's exact prefix/suffix shape so one document's id can never substring-match another's audit rows. Allowlisted to the 4 taxonomy doctypes; a "History" row-action + read-only modal added to the generic doctype table for just those.
- **26.4.4** (media versioning/renditions/alt/expiry): `version_no` now genuinely increments (`nextMediaVersion`, was hardcoded `1`); `alt_text`/`expiry_date` fields; a real thumbnail (pure stdlib `image`/`image/jpeg`/`image/png` decode + hand-rolled nearest-neighbor resize + JPEG re-encode — no `golang.org/x/image` or cgo dependency) generated for jpg/png at upload, stored content-addressed alongside the original, served via new `GET /api/v1/pim/media/{id}/thumbnail`. New `media-expiry` report.
- **26.4.5** (content workflow owner/SLA/rejection comments): `owner`/`sla_due_date` additive fields + `content-sla-breach` report. Rejection comments needed no new column — `approval_log.comment` was already mandatory on reject (APPROV-0159, Stage 25.5) but had no read endpoint; new `engines.ListApprovalLog` + `GET /api/v1/approval/log` fixes that gap generically (not PIM-specific), now surfaced in the Workbench content panel.
- **26.4.6** (bulk approval + content rollback): `engines.BulkDecideApproval` loops the existing `DecideApproval` (already its own atomic per-document transaction) with per-id success/failure reporting rather than one all-or-nothing transaction — a maker-checker violation on one selected document correctly doesn't block the others. New `product_content_versions` system table (same "dedicated table, not a doctype" reasoning as `pim_publish_queue/log`) snapshots ProductContent on every real "Approved" decision (hooked directly into `DecideApproval`, best-effort so a snapshot failure can never undo an already-committed approval); `RollbackProductContentVersion` restores a snapshot as **Draft**, never silently re-Approved, consistent with this codebase's no-silent-state-change stance.
- **26.4.7** (channel validation packs + diff preview): new `ChannelValidationRule` doctype (Min Images/Max Title Length/Required Tag) evaluated inside `CheckPublishReadiness`. `payload_snapshot` (new JSONB column on `pim_publish_log`) persists what was actually sent on every attempt; `PreviewChannelDiff` diffs the payload about to be sent against the most recent snapshot for that item+channel — explicitly stated as a diff against the *last local attempt*, not a live read-back from the platform (that would need a new per-connector API call this framework doesn't otherwise need).
- **26.4.8** (marketplace error dictionary): `classifyConnectorError` (`engines/connector.go`) pattern-matches platform wording actually observed in this codebase's own connector tests (Shopify "can't be blank", BigCommerce "is a duplicate") onto the existing CONN-0225/0227/0228 codes, falling back to the old unconditional CONN-0226 for anything unrecognized — every real connector previously hardcoded CONN-0226 for every failure regardless of cause. New `error_code` column on `pim_publish_log`.
- **26.4.9** (search feed export): `GetSearchFeedExportCSV` — item/title/tags/family/category/completeness-score/has-main-image as CSV, via the same authenticated-blob-download pattern the existing report-export CSV already uses (a plain `<a href>` can't carry the Bearer token these endpoints require).
- **A real pre-existing bug found and fixed during live-browser verification**: the global CSP (`internal/server/middleware.go`, Stage 24) set `img-src 'self' data:` with no `blob:` source — but the entire PIM media gallery (Stage 15.2, unchanged by this pass) fetches images as authenticated blobs and renders them via `URL.createObjectURL`, since a plain `<img src>` can't carry the Authorization header the media endpoints require. Every media thumbnail/preview in this app has silently failed to render in a real CSP-enforcing browser since Stage 15.2 shipped — invisible to curl-only or DOM-text-only verification, only caught because this pass did a real Playwright/Chromium screenshot pass, not just API calls. Fixed by adding `blob:` to `img-src`.
- **Verification**: `go build ./...`/`go vet ./...` clean; `go test ./... -p 1` reproduces only the two pre-existing 26.0.2 failures, nothing new (plus a new `TestClassifyConnectorError` unit test, and the existing Shopify/BigCommerce/Magento connector tests confirmed still passing unchanged). Every one of the 9 items live-verified against the real dev DB: direct API calls proving each engine function's real behavior (duplicate rejection messages, locale-override resolution via a scratch test against the live DB, bulk-approve partial-failure reporting, rollback restoring the exact prior title, channel-validation-rule blocking publish, thumbnail bytes actually generated on disk), **plus** a real-browser Playwright/Chromium pass through the actual Workbench screens confirming every new form field, the History modal, the content approval-history/version-restore UI, the media gallery (with the CSP fix), and the publish diff-preview table all render and behave correctly — not just that the API layer works. All test data created during verification was deleted afterward; the portable Postgres instance (which was not running at the start of this session) was left running for continuity, matching how a live dev session ends.
- **Concurrent-session note**: `docs/ai_handover.md`/`micro_checklist.md`/`project_ledger.md` had a second session's completed OMS-retirement work (§36 above) sitting uncommitted in the same working tree when this pass finished — committed as its own separate commit (`190d025`) before this section was added, so neither session's work overwrote the other's.

---

## 38. Stage 26.12 effort plan (2026-07-24, no code)

User asked, after §36's docs-only OMS retirement, whether any OMS code had actually been migrated/built (none had — §36 was documentation only) and for an effort estimate on actually building it. Sized all 10 `Stage 26.12` items in `micro_checklist.md` (still all `[ ]`) using "sessions" — one focused, live-verified build pass, this repo's own historical unit (Stage 20 Track B.2's WMS Maturity, comparable breadth, landed in one day; Stage 20 Track B.4 added 7 reports in a day).

- **Total ≈ 7.5-9.5 sessions**, realistically 1-1.5 weeks at this repo's demonstrated cadence (Stage 13's much larger scope shipped in about a week) — not one sitting, not a quarter.
- **Per-item sizes**: 26.12.1 Order Engine **L** (~1.5-2, biggest single item, everything else depends on it), 26.12.4 Courier/Shipment/Manifest **L** (~1.25-1.5, the largest orchestration gap per §36), 26.12.3 Pick/Pack **M** (~1), 26.12.5 Returns/QC/Refund **M/L** (~1), 26.12.2 Allocation strategies **M** (~0.75-1), 26.12.8 OMS reports **M** (~0.75-1, ×7 reports but a proven framework), 26.12.7 Exception dashboard **S/M** (~0.5-0.75), 26.12.6 Inventory buckets **S** (~0.5), 26.12.9 Config masters **S** (~0.25-0.5). 26.12.10 stays P2/deferred, not counted.
- **Recommended build order** (dependency-aware, not numbering order): 26.12.9 + 26.12.6 (foundational, no dependencies) → 26.12.1 (Order Engine) → 26.12.2 → 26.12.3 → 26.12.5 → 26.12.4 (build the internal AWB/manifest engine before any real courier API — same code-complete/credentials-pending split already used for GST/Shopify/BigCommerce/Magento at 26.2.1-26.2.5) → 26.12.7 + 26.12.8 last, since reports/dashboards read off data the earlier items produce.
- **Biggest swing factors**: 26.12.1's still-open doctype design decision (new `SalesOrder`/`SalesOrderLine` pair vs. extending `POSCart`), and how much of 26.12.4 ends up being real-courier-API integration vs. an internal-only AWB/manifest engine.
- Effort tags + this build-order summary written directly into each `micro_checklist.md` Stage 26.12 item, not just here, so the estimate travels with the backlog item itself.
- **No code changed this session** — docs/checklist only, same as §34-§37.

---

## 39. Stage 26.3.4 — WMS Operations Screens built (2026-07-24, code)

Following §35's WMS retirement, user asked to actually build the remaining WMS work going forward in this repo's own stack (Go + vanilla JS), not a separate service. Picked **Stage 26.3.4** as the first slice: `engines/wms.go`'s putaway/pick-list/condition-transition/cycle-count-reconcile functions have been real, routed, working backend since Stage 20 Track B.2 with confirmed-via-grep zero frontend anywhere.

- **Built** (`public/app.js`, `public/index.html` only — no `.go` file touched): `renderPutawayView`/`submitPutaway`, `renderBinConditionsView`/`submitBinConditionTransition` (with a client-side same-condition guard), `renderCycleCountView`/`submitCycleCountReconcile`, and `window.viewPickList` (a read-only modal on the Fulfillment screen's existing task-action buttons). Three new sidebar items added to the existing "Stock" flyout (`Putaway`, `Bin Conditions`, `Cycle Count`), wired through the existing generic click-handler array, `MENU_PERMISSION_MAP` (`{ open: true }`, matching `handlers_wms.go`'s own no-role-check reality), `STATIC_VIEW_MENU_IDS`, and new `renderView()` router cases — all reusing established patterns (`apiFetch`, `showApiError`, `.table-panel`/`.form-group`/`login-error` CSS, `attachTypeahead` on Bin/Item fields, the `.modal-overlay` primitive `viewTaxonomyHistory` established for read-only detail views).
- **One real gap found and fixed along the way**: `CycleCountLine` is a Transaction doctype, and `renderSidebarSubmenu()`'s Setup submenu only lists `document_type === 'Master'` doctypes — so there was previously **no way to reach `CycleCountLine`'s generic table (and its Bulk Import button) from the UI at all**, even though Stage 20.21 was written assuming Bulk Import would be how count lines get entered. Fixed by having the new Cycle Count screen's "Manage Count Lines" button set `currentDoctype = 'CycleCountLine'` and call `renderView('doctype-table')` directly — reusing the existing generic view exactly as-is rather than building a second import mechanism.
- **One thing checked and found to be a non-issue**: `PostCycleCountAdjustment` looked, on first grep, like it had no HTTP handler wired to it — turned out to already be correctly invoked as a finalize-on-approve side effect inside `handleDecideApproval` (`internal/server/handlers_pim_pos_finance.go:815-820`) when a `CycleCountLine` is Approved. Confirmed via direct read, not just grep. Nothing to fix there.
- **Live-verified end-to-end** on a throwaway port (8146) via a scratch token (`engines.SignToken`, minted from a repo-internal `cmd/scratch_mint_token`, deleted after) and Playwright (installed to the scratchpad dir via npm cache, not the repo). Seeded real fixtures (a Bin, an `inventory_availability` row, a `FulfillmentTask`) via a repo-internal `cmd/scratch_seed_wms` + `cleanup` pair (both deleted after): put away 20 units, transitioned 10 from Good→Damaged (confirmed via real success dialogs with the exact expected message), the same-condition guard blocked correctly, cycle-count reconcile returned a real `{0 posted, 0 pending}` result for a nonexistent session, "Manage Count Lines" opened the real `CycleCountLine` table (Bulk Import/New buttons present), and the Pick List modal showed the correct bin/SKU/qty (5, matching the task's requested qty against the 10 units left in Good condition) with no shortfall. One console 404 (`/api/v1/me`) reproduced on a bare page load with no interaction — confirmed pre-existing and unrelated to this work. All seeded test data removed afterward.
- **Verification**: `go build ./...`/`go vet ./...` clean (no Go files touched), `node --check public/app.js` clean.
- Roadmap for what's next (Stage 26.10.1 stock ledger field wiring, then the Stage 26.5 items with an existing design note) stays as scoped in the approved plan — not started this pass.

---

## 40. Stage 24 addendum — cross-checked `ERP_LOOPHOLES_ANALYSIS.md` (2026-07-24, no code)

User asked to update `micro_checklist.md` with whatever that doc's newer security pass found still missing. Re-verified all 21 of its "still open" claims against live source rather than transcribed as-is (`Stage 24`'s own header note set this precedent: "4 were already fixed by later stages, 2 turned out worse than described").

- **19 of 21 were stale** — already closed by an existing `[x]` Stage 24 item, or (for its Medium #9, "no status-transition validation") by Stage 25 Batch 3's `ValidateTransactionalRules`, built after that doc's original 2026-07-20 baseline. Confirmed the least-obvious one directly: extension tokens (`SignExtensionToken`) carry no `role` claim at all, so 24.2's `requireHRAdmin` gate on `handleLabels`/`handleSequence`/`handlePrefix` already rejects them — closing its Critical #1 even though 24.2 was framed as a role fix, not an extension-token fix.
- **2 genuine gaps found**, added as `24.33`/`24.34`: (1) `_ = json.Unmarshal(...)` has crept back into 5 production call sites since 24.18, two with real fail-open risk (`transactional_validation.go:85` — malformed PO items silently compute zero ordered-qty; `handlers_core_doc_engine.go:449` — malformed prior-doc data silently feeds `ValidateTransactionalRules` an empty prior state); (2) no connection-pool monitoring exists anywhere (24.13 tuned bounds but exposes no visibility) — cheap fix, `database/sql`'s own `Stats()` needs no new dependency.
- **No code changed this session** — docs only.

---

## 41. Stage 27 — Modular Product Packaging (2026-07-24, code)

User asked for the ERP to become sellable module-by-module — PIM alone, WMS alone, any combination, or the full suite — each reachable at a clean dedicated URL (`/wms`, `/pims`, `/oms`, `/hr`, ...), on the exact same single Go binary/single Postgres instance (no microservices split, no new frontend framework), with dependency/inheritance between modules handled automatically rather than by ad hoc rule. Researched the existing Stage 14 module-entitlement system first (`public.modules`/`tenant_default.module_entitlements`/`moduleGate`) via three parallel Explore passes before designing anything, per the "reuse the existing choke point" principle — most of the enforcement mechanism already existed; the gap was narrower than it first looked.

- **`engines.ProductPackages`** (`engines/modules.go`) — a static Go map, not a new DB table, since a package has no per-tenant state of its own (the existing `module_entitlements` table already is that state; a package is only ever used to bulk-set entitlements, never read at request time, so it can never drift out of sync with what's enforced). This is the "master definition" of every sellable product: key, display name, URL prefix, granted module_keys. `ExpandPackagesToModules`, `ResolveSoleProductPackage`, `ResolveOwnedPackages`, `IsFullSuite` turn a package selection into concrete entitlements and back for the frontend's navigation hints.
- **Real, pre-existing gaps found and closed**: `wms`/`oms` had no `module_key` at all in `public.modules` before this. Five WMS floor-ops routes (`/api/v1/wms/putaway`/`pick-list`/`condition-transition`/`transfer/pack`/`cycle-count/reconcile`) were registered completely role-open with zero entitlement gate (confirmed deliberate per `handlers_wms.go`'s own comment, predating the idea of WMS as a separate sellable product); the entire Unicommerce credential/order/inventory-sync surface and the BigCommerce webhook likewise had no gate of any kind, not even the older feature-flag system. Shopify/marketplace-settlement/logistics-book/fulfillment-transition routes carried only the older, narrower `featureGate("oms_integration"/"wms_integration",...)` — which answers "is this integration configured," not "did this tenant buy this product." New migration `db/migrations_stage27_product_packaging.sql` (same shape as the Stage 18 core-module-fix migration) registers both module_keys; all of the above routes now additionally carry `moduleGate("wms"/"oms",...)` in `internal/server/routes.go`, composed alongside (not replacing) the older feature flags.
- **Dependency/inheritance**: `moduleDependencies` map + a transactional `SetModuleEntitlement` (`engines/modules.go`) — enabling a module now atomically auto-enables any unmet prerequisite; disabling a module another enabled module still depends on is refused with a named error, same convention as the pre-existing is_core refusal. Only one real optional↔optional pair exists today (`rfq` → `procurement`); the mechanism is generic and ready for more without being pre-populated speculatively.
- **New self-service endpoint** `GET /api/v1/me/modules` (`internal/server/handlers_profile.go`, mirrors the existing `handleMyPermissions` shape exactly) — any authenticated user, not just HR/Admin, can read their own tenant's enabled modules plus navigation hints. The existing HR/Admin-only `GET /api/v1/admin/tenant/module-entitlements` is untouched.
- **Provisioning**: `handleProvisionTenant` gets an optional `packages []string` field, fully backward compatible (omitted = today's exact "everything enabled" behavior). **Two real bugs found and fixed during live verification, not just written and assumed correct**: (1) a naive single-pass "enable wanted modules, disable the rest" loop iterates `public.modules` alphabetically — disabling `procurement` before `rfq` (whose new dependency-refusal rule was still blocking it, since `rfq` hadn't been disabled *yet* in the same pass) silently left `procurement` enabled for a WMS-only test tenant; fixed with an enable-first-then-multi-pass-retry-disable loop. (2) The retry loop's own bound, `pass < len(toDisable)`, re-evaluated against `toDisable` *after* it had been reassigned to the shorter `stillFailing` slice each iteration, so it exited after exactly one pass instead of the intended several — fixed by capturing `maxPasses` before the loop starts. Both caught by direct curl verification against a real provisioned tenant, not by inspection.
- **Server-side SPA path fallback** (`internal/server/routes.go`) — a loop over `engines.ProductPackages` registers `GET /<prefix>` and `GET /<prefix>/{rest...}` serving `public/index.html`, so `/wms`, `/pims`, etc. resolve instead of 404ing against the plain `http.FileServer`. A future product added to the map gets a working URL automatically, no route-table edit.
- **Frontend** (`public/app.js`, `public/styles.css`): `MENU_MODULE_MAP` + `applyModuleEntitlements()` mirror the existing role-based `MENU_PERMISSION_MAP`/`applySidebarPermissions()` pattern exactly, down to the "hide leaf item, then collapse an emptied flyout" two-pass shape — kept as a distinct `module-hidden` CSS class specifically so the two independent filters can never clobber each other's hide decision. `applyProductPathRouting()` silently rewrites bare `/` to a single-product tenant's own URL via `history.replaceState` (a pure navigation convenience — `moduleGate` server-side is still the only real access control, regardless of what the URL says). A minimal product switcher (`renderProductSwitcher()`, a plain `<select>` using `history.pushState`, no new component) appears only for a tenant with 2+ products but not the full suite. **A third real bug found during the real-browser (Playwright) verification pass**: `owned_packages` initially listed every individual package for a full-suite tenant too (such a tenant technically satisfies every package's module requirements at once), incorrectly showing the switcher for a tenant that should see exactly today's unified experience — fixed with a new `engines.IsFullSuite` check that suppresses `owned_packages` once a tenant's enabled modules cover the complete `erp_full` set.
- **Documented scope boundary, not silently decided**: the 5 `is_core` modules (`core`/`master_data`/`inventory`/`sales`/`finance`) stay permanently enabled for every tenant regardless of package purchased — unchanged, pre-existing Stage 14 behavior; re-architecting that would mean touching load-bearing invariants (checkout, GL posting, stock ledger) multiple engines assume are always present, for a much bigger change than what was asked. A single-product tenant's visible nav/URL is still fully scoped to what they bought; only the backend's always-on technical foundation stays present underneath, exactly as it already did for every tenant before this Stage.
- **Not built this pass, deliberately**: an admin UI screen for picking packages per tenant (`micro_checklist.md` 26.1.4 already tracks this exact gap separately) — this Stage makes the mechanism it would sit on top of correct and product-shaped; the picker screen itself stays a fast-follow.
- **Verified live, not just built**: `go build ./...`/`go vet ./...` clean; `go test ./... -p 1` reproduces exactly the same two known pre-existing failures (nothing new). On a throwaway port against the real dev DB: provisioned a WMS-only and a PIM-only tenant via the new `packages` field via curl, confirmed `moduleGate` actually blocks/allows the right routes per tenant, confirmed the `rfq`→`procurement` dependency both auto-enables and blocks a disable correctly, confirmed every product-prefix URL (including a nested subpath) serves the SPA shell. Real-browser pass via Playwright (scratch-minted `engines.SignToken` session, per the established method): the PIM-only tenant's sidebar correctly trims down and bare `/` redirects to `/pims` with no console errors; the existing full-suite `default` tenant renders pixel-for-pixel unchanged (full sidebar, no redirect, no switcher) — the non-negotiable regression check this whole design hinges on. All test tenant schemas and scratch tooling (`cmd/scratch_mint_token`, throwaway binaries, the Playwright scratch install) removed afterward.

---

## 42. Stage 26.1.2 — SLO/status-page dashboard (2026-07-24, code)

User asked to start building Stage 26's phases one leg at a time, beginning with whichever of 26.1's items are actually open/buildable. Of 26.1's six items, only 26.1.2 has no `[needs user input]`/`[needs scope decision]` tag and no dependency on another 26.1 item — the rest (26.1.1 hosting decision, 26.1.3 edge WAF, 26.1.6 tenant-scoped backup) are genuinely blocked or scoped-out, and 26.1.4/26.1.5 (tenant entitlement/usage admin screens) were left alone this pass since a concurrent session was actively mid-edit on the exact backend files (`routes.go`, `modules.go`) those two would need to touch (see §41's Stage 27 work, confirmed live via two `git status` snapshots taken seconds apart showing the file set still growing).

- Pure frontend, zero backend/route/table changes — 26.1.2's own scope note ("needs only a frontend screen") held exactly true. New **System Status** screen under the `Settings` sidebar flyout (`menu-system-status`, HR/Admin-only, same `adminOnly` gate as `menu-audit-logs`), wired into the router (`renderView`), `STATIC_VIEW_MENU_IDS`, and the generic menu-click binder the same way every other admin screen already is.
- Reuses the existing Stage 25.8 `GET /api/v1/ops/deployment-status`/`GET /api/v1/ops/backup-status` endpoints outright — both already HR/Admin-gated and already compute the DR-0213 (restore drill overdue)/DR-0214 (RPO breach) warnings off the Stage 17.10 error catalog, so "Stage 17.10's alerting" the checklist item called for was already sitting inside `backup-status`'s response, not something to build separately.
- `renderSystemStatusView` (`public/app.js`) renders: a warnings banner (danger-red for DR-0214, warning-amber for DR-0213), three `.stat-card` KPI tiles (environments tracked, last backup, last restore drill), and three `.table-panel` tables (latest deployment per environment, full deployment history, backup/restore-drill history) — entirely off existing `.stat-card`/`.table-panel`/`.badge-success`/`.badge-danger`/`.badge-warning`/`.badge-secondary` primitives already used elsewhere in the file. No new CSS (`styles.css` untouched).
- **Verified live, not just built**: `go build ./...`/`go vet ./...` clean (no Go files touched by this item), `node --check public/app.js` clean. Live-verified on a throwaway port (8145) against the real dev DB, same scratch-`engines.SignToken`-via-repo-internal-`cmd/`-program + Playwright method as prior sessions: logged in as a scratch HR/Admin session, opened Settings → System Status, confirmed both endpoints return 200 and the screen renders correctly against the current empty `public.deployments`/`public.ops_run_log` tables — both DR-0213/DR-0214 warnings correctly fire, all three tables correctly show their empty state, screenshot confirms visual styling matches the rest of the app. The one console warning seen (`404` on `/api/v1/me`) was traced to the synthetic scratch user id having no real profile row — unrelated to this screen, present on every scratch-token verification pass regardless of what's being tested. Scratch `cmd/scratch_mint_token` directory and the background server process removed afterward; `git status` confirms no trace.
- **Left alone, not forgotten**: 26.1.4 (tenant plan/entitlement admin screen) and 26.1.5 (tenant usage/health dashboard) are still open — 26.1.4 now has essentially everything it needs underneath it already (§41's Stage 27 `ProductPackages`/`SetModuleEntitlement`/`GET /api/v1/me/modules`), it's a build candidate for the next 26.1 pass once `routes.go`/`modules.go` are free of concurrent edits.

---

## 43. Phase 26.0/26.1 closed out — 26.0.2, 26.1.4, 26.1.5 (2026-07-24, code)

User asked to reevaluate and resume building Stage 26 "from the 1st open point, phase by phase." A fresh `git status`/`git log` check showed §41's Stage 27 work (previously mid-edit, per §42) had since landed as real commits (`ef7d1d4`/`b33dbfd`/`220406d`) — the file-churn risk that made §42 skip 26.1.4/26.1.5 no longer applied, and `routes.go`/`modules.go` were the shared choke points those two items actually needed. The literal first open item overall was `26.0.2`, not a 26.1 item — built in strict order rather than skipping back to where the previous pass left off.

- **26.0.2** (`engines/engines_test.go`) — root-caused both long-standing test failures instead of accepting the checklist's own "probably shared-DB debris" framing at face value:
  - `TestEngines/FinanceDoubleEntryAndPOS`: traced the "expects 9000, gets 9500" gap by hand-tracing every posting in the subtest. `gl_postings` is unconditionally wiped at the top of this exact subtest, so nothing external could be contaminating it by the time the trial-balance assertions run — ruling out shared-DB debris as the cause despite that being the checklist's working theory. The real cause: Stage 24.5 added an idempotency-retry check (posting/re-posting `V-003`, 500 debit/500 credit, verified to land exactly once) *between* the original balanced-entry post and the two trial-balance assertions, but never updated those assertions' expected totals (1000 → should be 1500; 9000 → should be 9500) to account for it. Fixed by correcting both expected totals with a comment explaining the math, not by touching `engines/finance.go` — there was no real posting-engine bug to fix.
  - `TestEngines/DocTypeValidationAndAuth`: this one genuinely was shared-DB debris, but traced to its exact source rather than asserted: `public/profiles/agriculture.json` defines `Brand.fefo_enabled` as `mandatory: true`; some earlier Agriculture-industry-profile test/demo run applied that profile against the persistent `default` tenant schema and never reset it. Confirmed `fefo_enabled` doesn't belong to Brand's own baseline schema at all (absent from `db/migration.sql`). Fixed by having the subtest delete that one stray `doctype_fields` row before asserting — the same "clear the specific fixture you're about to depend on" convention `GenerateSequence`'s own subtest already uses two tests earlier in the same file, just not yet applied here.
  - **Result**: `go test ./... -p 1` is fully green (`ok` on both `custom_erp/engines` and `custom_erp/internal/server`) for the first time this repo's history shows evidence of — every prior session's checklist notes describe these two as persistently failing.
- **26.1.4 — Tenant Entitlements admin screen.** Discovered almost the entire backend already existed from Stage 27 (`engines.ListModuleEntitlements`/`SetModuleEntitlement`, `GET/POST .../tenant/module-entitlement(s)`, already HR/Admin-gated) — the actual gap was purely the missing UI plus two small supporting reads. Added: `GET /api/v1/admin/tenants` (`handleListTenants` — no tenant-listing endpoint existed anywhere before this, needed for the screen's tenant picker), `GET /api/v1/admin/packages` (`handleListProductPackages`, new `engines.ListProductPackages` — exposes the Stage 27 `ProductPackages` catalog, previously Go-internal only), `POST /api/v1/admin/tenant/package` (`handleSetTenantPackage`). The apply-a-plan logic itself (`engines.ApplyPackageSelection`) is `handleProvisionTenant`'s own enable-then-disable-with-retry convergence loop, extracted verbatim into `engines/modules.go` as a shared choke point rather than duplicated — provisioning now calls the same function the admin screen does, best-effort exactly as before (errors still swallowed there, since the tenant schema is already committed by that point), while the new admin-screen caller surfaces errors properly since it has no earlier success to protect. `renderTenantEntitlementsView` (`public/app.js`): tenant `<select>`, one button per plan (`.btn-outline`), and a per-module toggle grid using `.switch`/`.slider` — CSS that already existed in `styles.css` but had zero callers anywhere in the file until now.
- **26.1.5 — Tenant Usage dashboard.** The checklist's "reuses the per-tenant concurrent-request cap counters (Stage 24.30)" pointed at `internal/server/middleware.go`'s in-memory `tenantConcurrencyLimiter` — real and live, but write-only from the outside (`acquire`/`release`, no read path). Added a `snapshot()` method (lock, copy the map, return) and one new handler, `handleTenantUsage`, that joins that live snapshot against `public.tenants`, each tenant's own `{schema}.users` active-count, and `{schema}.tenant_limits` (Stage 25.8, `SAAS-0193`) — all three pre-existing, none of it required a new metering mechanism. `renderTenantUsageView` shows tenant/in-flight/at-cap stat tiles plus a per-tenant table with badge color coding (green/amber/red) on both active-users-vs-`max_users` and in-flight-vs-cap.
- **Verified live, not just built**: `go build ./...`/`go vet ./...` clean; full `go test ./... -p 1` green (see 26.0.2 above). Both new admin screens live-verified on a throwaway port (8146/8147) against the real dev DB: 26.1.4 via curl (applied the WMS-only plan to the real second tenant `tenant_new`, confirmed exactly `wms`/`stickers`/`reports`+core stayed enabled, toggled one module individually, restored to full suite) *and* a real-browser Playwright pass clicking the same flow through the actual UI; 26.1.5's own request to itself correctly showed up as 1 in-flight (proof the counter is genuinely live, not a stub), and a temporary `max_users` limit row was inserted/verified/removed to confirm the usage-badge rendering. All scratch tooling (`cmd/scratch_mint_token`, throwaway server processes, the temporary `tenant_limits` test row) removed afterward; `git status` confirms no trace beyond the intended source changes.
- **Next**: Phase 26.2 is entirely `[needs user input]` (real integration credentials) — nothing buildable there. Phase 26.3 (GRN workbench, Purchase Requisition entry, Approval Rules admin, vendor-invoice-override/PO-amendment UI checks) is the next phase with buildable items.

## 44. Stage 26.3.1 — GRN Workbench screen (2026-07-24, code)

Continuing §43's "start building from the end of phase" direction into Phase 26.3 (the next phase with buildable items). A concurrent session was mid-edit on §43's 26.1.4/26.1.5 screens in the same Settings-area files (`public/index.html`/`app.js`, `internal/server/routes.go`/`handlers_integrations_admin.go`) at the time this session started — confirmed via `git status`/`git diff` showing an in-progress `menu-tenant-entitlements` addition this session hadn't made. Rather than risk duplicate/colliding work on the same screen, this session moved to 26.3.1 (GRN workbench), an unclaimed item in the phase the other session's own §43 "Next" note pointed at.

- **GRN Workbench** (`renderGRNWorkbenchView`, `public/app.js`, new "Goods Receipt (GRN)" entry under the Buying sidebar flyout) — GRN's first UI ever. It's a Transaction (not Master) doctype, so it was never reachable from the Setup submenu the generic doctype form relies on; its one mandatory field, `received_items`, is a JSON blob no one could realistically hand-type through that generic form anyway. PO Reference typeahead + "Load Items from PO" pre-fills lines from the PO's `items` JSON (most POs today have none — items still saves as `'[]'` from the PO create screen, tracked separately as 26.3.6 — that's a manual-entry fallback, not an error). Line shape (`sku`, `qty`, `accepted_qty`, `rejected_qty`, `rejection_reason`) matches `engines/transactional_validation.go`'s pre-existing `grnReceivedLine` exactly, so the screen inherits the full GOODSR-0089/0090 + PURCHA-0082/0084/0086/0087/0088 validation set already built in Stage 25 Batch 3 for free, rather than a parallel check. `accepted_qty` is auto-derived client-side (`qty − rejected_qty`), so it can never violate GOODSR-0089's accepted-cannot-exceed-received rule by construction. Barcode preview is a best-effort `GET /api/v1/doc/Item/{sku}` per line (falls back to the SKU itself on a miss, mirroring `engines.PrintStickers`' own degrade path) — no new backend surface at all; posts through the pre-existing `POST /api/v1/doc/GRN`.
- **Two real bugs found and fixed during live verification**, not just written and assumed correct:
  1. **Double-stock-post-on-edit** (`internal/server/handlers_core_doc_engine.go`): the GRN inventory-posting hook ran unconditionally on every save, including edits — re-saving an existing GRN with its own unchanged `received_items` would silently post the same qty into `inventory_availability` a second time. Real but dormant (GRN had zero UI before this screen, so nothing could ever trigger it) — gated to `id == ""` (create-only), the same convention the Item/PIM-profile create-only check a few lines above it already uses. Verified by directly re-POSTing an already-posted GRN and confirming `inventory_availability` stayed at 10 instead of doubling to 20.
  2. **Race in `loadGRNItemsFromPO`**: it's wired to both the PO field's own `change` event (fires on a typeahead pick) and an explicit "Load Items from PO" button — picking a PO and immediately clicking the button (which is exactly what the Playwright verification script's own sequencing did) launches two overlapping async calls; each resets the shared `grnLineItems` array then `await`s a barcode lookup per line, so the first call's later pushes land on the *second* call's fresh array once its own `await` resumes, duplicating every line. Fixed with a `grnPOLoadInFlight` reentrancy guard.
- **Live-verified end-to-end**: throwaway server on port 8177 + a scratch HR/Admin token minted via a repo-internal `cmd/scratch_mint_token_grn` program (deleted after, `git status` confirms no trace) + Playwright against real seeded fixtures (a Vendor, an Item with a real barcode + HSN/GST fields, a PurchaseOrder with a real `items` line, submitted and approved by a second real user identity to satisfy maker-checker). Confirmed: PO-linked line load populates Ordered/Received/Rejected/Short columns and the barcode badge correctly; posting a GRN increments `inventory_availability` by exactly the received qty; a same-content re-save does not double it (bug #1 fix); client-side guards block rejected-qty-exceeds-received and rejected-qty-without-a-reason before any request is sent; a real server-side PURCHA-0084 rejection ("PO already fully received") renders in the form's existing red error banner, not a raw error. All fixtures removed after — two rows needed a direct SQL delete (the generic delete endpoint correctly refuses to delete an Approved transactional document; Vendor/Item's soft-delete also left a row `DELETE` doesn't remove) plus one auto-created `PIMProductProfile` (Stage 15.2's create-hook fires on any new Item, expected). `go build ./...`/`go vet ./...`/`node --check public/app.js` clean.
- **Next**: 26.3.2 (Purchase Requisition entry screen), 26.3.3 (Approval Rules admin screen), 26.3.5/26.3.6 (vendor-invoice-override and PO-amendment UI gap checks) remain open in this phase.

For the current build tracker, see **[docs/micro_checklist.md](micro_checklist.md)**.
