# Project Progress Ledger: Custom ERP

This ledger is a living document that tracks the implementation progress, architectural decisions, and chronological build history of the Custom ERP system.

---

## 1. Project Genesis (Where We Started)
The project started from a static, client-side HTML dashboard template served via a basic HTTP server. All data operations (such as Brand and Style definitions) were mocked inside client-side variables (`db.js`) and simulated using `localStorage` data stores. There was no real database, no multi-tenant isolation, no numbering safety, and no observability.

---

## 2. Architectural Decisions & Rationale
We transitioned the mock frontend into a production-grade multi-tenant backend architecture:

| Component | Choice | Rationale |
| :--- | :--- | :--- |
| **Backend Runtime** | **Go (Golang 1.22)** | Single binary compilation, startup memory footprint < 15MB RAM per client, and extremely fast startup times (<10ms). Allows hosting high density client containers on cost-effective virtual servers. |
| **Database** | **PostgreSQL 16.3** | Supports **Schema-per-Tenant** isolation in a single instance. Provides transactional integrity (`SELECT ... FOR UPDATE` row locks) for inventory ledgers, sequence counters, and checks. |
| **Multi-Tenancy** | **Schema-per-Tenant** | Each tenant's data is isolated in a separate database schema (e.g. `tenant_default`). Resolves dynamically via a request middleware. Prevent data leakage and allows schema customization per client. |
| **Branding** | **Custom ERP** | Cleaned up all legacy sample branding (e.g. "Ditos") to establish a generic, copyright-safe, customizable system. |

---

## 3. Project Rollout Completion Ledger

```
[x] Phase 1: Core Foundation & Scale Infrastructure (Event Bus, Outbox, Availability, Reservations) -> COMPLETED
[x] Phase 2: Single Vertical Pilot (Jewellery PO-GRN-Barcode-Inventory-Transfers-POS-Finance) -> COMPLETED
[x] Phase 3: Omnichannel Pilot (Shopify Sync, Webhook Imports, Fulfillment Routing) -> COMPLETED
[x] Phase 4: Store Fulfillment (Ship-from-store, BOPIS, Return Anywhere, Tasks Dashboard) -> COMPLETED
[x] Phase 5: Scale Test (Simulate 100, 500, 1000, 2000 stores) -> COMPLETED
[x] Phase 6: Marketplace/OMS Expansion (Settlements, Logistics, Support Console) -> COMPLETED
[x] Phase 7: Advanced Optimization (Forecasting, Replenishment, SLA target tuning) -> COMPLETED
```

**Scope note (2026-07-12)**: Phases 1-7 above are the **Omnichannel Scale Add-on Blueprint's** rollout plan — they cover the kernel and omnichannel/scale backend, and "COMPLETED" here is accurate for that scope. It does **not** mean the full ERP described in the Master Blueprint is complete: POS/Finance/GST/CRM/HR/Assets UI, the approval/maker-checker workflow engine, MFA, and most of the ~80-report catalog were never part of this Phase 1-7 plan and remain unbuilt. See **[docs/pdf_blueprint_gap_analysis.md](specs/pdf_blueprint_gap_analysis.md)** for the full comparison against all 6 spec PDFs and `docs/micro_checklist.md` Stage 13 for the resulting backlog.

---

## 4. Phase 1 Build Records (What We Built)

### 4.1 Numbering/Sequence Engine
*   **Location**: `engines/numbering.go`
*   **Behavior**: Dynamically builds sequences: `<Prefix><Separator><StoreCode><Separator><FinancialYear><Separator><PaddedNumber>`.
*   **Concurrency Control**: Connects to DB, starts transaction, locks the current row inside `sequence_counters` via `SELECT ... FOR UPDATE` lock, increments, and commits. Prevents duplicate numbers under concurrent requests.

### 4.2 Dynamic Label Translation Engine
*   **Location**: `engines/labels.go` (Backend API), `app.js` (Frontend translation walker).
*   **Behavior**: Translates exact-match UI strings dynamically. The frontend traverses the DOM recursively using a TreeWalker node filter and updates labels on-the-fly based on translations cached from `/api/v1/labels`.

### 4.3 Audit & System Exception Hub
*   **Location**: `engines/logs.go` and panic recovery middleware in `main.go`.
*   **Behavior**: 
  - **Audits**: Writes structural log rows detailing user actions to `audit_logs` table.
  - **Panics**: Catches Go handler panics, retrieves stack trace, logs it to database under a unique correlation UUID, and returns HTTP 500. Rendered visually inside the Log Hub interface.

---

## 5. Phase 2 Build Records (What We Built)

### 5.1 DocType Metadata Builder & Registry APIs
*   **Location**: `engines/doctype.go` and routes registered in `main.go`.
*   **Behavior**: Registers custom document schemas dynamically (`POST /api/v1/meta/doctypes`) and registers custom column properties (fields name, labels, display orders, datatypes, and mandatory parameters) in `doctype_fields` (`POST /api/v1/meta/{doctype}/fields`).

### 5.2 Dynamic Form & Grid Rendering Interpreter
*   **Location**: `public/app.js` and `public/index.html`.
*   **Behavior**: Traverses dynamic field structures queried from `/api/v1/doc/{doctype}/meta` and constructs form fields (`Data`, `Number`, `Select`, `Link`) inside a single dynamic modal (`#dynamic-modal`). Automatically queries referencing list lookups for linked fields dynamically from the backend.

### 5.3 Parent-Child Vocabulary & Translation Engine
*   **Location**: `public/app.js` DOM translator.
*   **Behavior**: Dynamically maps vocabulary references per tenant, allowing users to override default nomenclature (such as mapping `Brand` to `Material Grade` or `Color` to `Fabric Metallurgy`) and translates UI elements instantly.

---

## 6. Phase 3 Build Records (What We Built)

### 6.1 Industry Profile Preset Configs & Loader
*   **Location**: `public/profiles/` directory and `SwitchIndustryProfile` in `engines/doctype.go`.
*   **Behavior**: Provides structural configuration mappings for Jewelry, Food & Beverage, Automobile, and Clothing industries. Loads templates dynamically via a database transaction: clears old field definitions, updates doctype metadata, registers customized columns, inserts default sequential counter templates, and resets translation vocabulary overlay caches.

### 6.2 Bulk Uploads Engine
*   **Location**: `engines/import.go` and HTTP route handlers in `main.go`.
*   **Behavior**: Parses raw CSV records, maps fields case-insensitively, validates formatting and constraints (numeric boundaries, date validity, select bounds, reference links), increments sequence codes dynamically inside a transactional context, and yields a structural validation report showing total successes and specific cell-level validation errors.

---

## 7. Phase 4 Build Records (What We Built)

### 7.1 Balanced Double-Entry Finance Engine
*   **Location**: `engines/finance.go` and `POST /api/v1/checkout`.
*   **Behavior**: Posts balanced journal entry lines (sum debits == sum credits) to `gl_postings`. Automatically handles bookings during checkouts: debits Cash (`1100`) & COGS (`5100`), credits Sales Revenue (`4100`) & Inventory Control (`1200`).

### 7.2 Store Fulfillment Task Manager
*   **Location**: `engines/fulfillment.go` and `POST /api/v1/fulfillment/task/transition`.
*   **Behavior**: Manages picking tasks (`FulfillmentTask`). If a task is rejected, automatically releases local reservations and triggers re-routing to the next best store node.

### 7.3 Return Anywhere Engine
*   **Location**: `engines/fulfillment.go` and `POST /api/v1/fulfillment/return`.
*   **Behavior**: Safely processes returns at any store, incrementing local stock levels and posting balanced refund GL journals.

---

## 8. Phase 5 Build Records (What We Built)

### 8.1 Concurrency Scale Simulation Engine
*   **Location**: `engines/scale.go` and `POST /api/v1/admin/scale-test`.
*   **Behavior**: Stress-tests 2,000 stores with parallel checkout workers, tracking throughput (TPS) and latencies. Verified ~456 TPS under 100 concurrent workers with 0 conflicts and a balanced post-simulation ledger.

---

## 9. Phase 6 Build Records (What We Built)

### 9.1 Marketplace Settlement & Reconciliation Engine
*   **Location**: `engines/marketplace.go` and `POST /api/v1/marketplace/settlement/reconcile`.
*   **Behavior**: Receives marketplace payout reports, validates payout math (`total - commission == net`), reconciles cart orders status to `Settled`, and posts balanced GL entries (debits Cash `1100` and Commission Expense `5200`, credits Accounts Receivable `1300`).

### 9.2 Carrier Logistics Dispatch Tracker
*   **Location**: `engines/marketplace.go` and `POST /api/v1/marketplace/logistics/book`.
*   **Behavior**: Registers carrier dispatch metadata (`LogisticsBooking` documents in `Shipped` status) to track carrier names, tracking numbers, and shipping charges.

---

## 10. Phase 7 Build Records (What We Built)

### 10.1 Sales-Velocity-Driven Replenishment Suggestions
*   **Location**: `engines/optimization.go` and `GET /api/v1/optimization/replenishment-suggestions`.
*   **Behavior**: Computes average daily sales velocity of SKUs based on POSCart checkout histories over 30 days and suggests replenishment orders (`suggestedQty = (velocity * leadTime) + safetyStock - available`).

### 10.2 Historical Sales-Based Forecasting Engine
*   **Location**: `engines/optimization.go` and `POST /api/v1/optimization/forecast`.
*   **Behavior**: Projects future SKU sales volumes based on daily historical velocity rates.

### 10.3 Picking SLA Breach Monitors
*   **Location**: `engines/optimization.go` and `GET /api/v1/optimization/sla-breaches`.
*   **Behavior**: Scans open fulfillment tasks, measures elapsed time since creation, and highlights tasks exceeding SLA thresholds.

### 10.4 Status-mismatch bug — fixed 2026-07-12
*   §10.1 and §10.2 used to compute sales velocity by querying `POSCart` documents with `status = 'completed'`, which the system never actually produces. Fixed to match `status IN ('Paid', 'Settled')` — the two real statuses `handleCheckout` and `ProcessMarketplaceSettlement` actually write. Verified with a real checkout followed by a real forecast call. See `docs/hardening_roadmap.md` Phase 2.2.

---

## 11a. SaaS Provisioning, Feature Flags & Integration Log Retries (Build Record)

*   **Tenant Provisioning**: `engines/saas.go`, `POST /api/v1/admin/tenant/provision`. Clones 19 table structures and seeds metadata from `tenant_default` into a new tenant schema. Fixed 2026-07-12 (`hardening_roadmap.md` Phase 1.6): `users` is no longer cloned into the seed loop — each new tenant gets one admin user with a unique, freshly-generated bcrypt-hashed password (`generateRandomPassword()`), not the shared placeholder hash.
*   **Feature Flags**: `engines/saas.go`, `POST /api/v1/admin/tenant/feature-flag`. Per-tenant boolean flags backed by a new `feature_flags` table. `SetFeatureFlag` is wired to the API; `IsFeatureEnabled` isn't called from any request handler yet, so flags don't currently gate any behavior.
*   **Integration Log Viewer & Retry**: `engines/outbox.go` (`GetIntegrationLogs`, `RetryIntegrationEvent`), `GET /api/v1/integration/logs` and `POST /api/v1/integration/retry`. Backend works; the frontend Log Hub view doesn't call either endpoint yet, so there's no integration-payload list or retry button in the UI.

For the full list of known gaps and the plan to close them, see **[docs/operations/hardening_roadmap.md](operations/hardening_roadmap.md)**.

---

## 11b. Real Login Flow (Build Record, 2026-07-12)

Closed `hardening_roadmap.md` Phase 1.1. Prior to this, `apiMiddleware` silently granted `HR/Admin` access to any request with no `Authorization` header — the app had no login screen at all and worked *only because of* that bug.

*   **Backend**: `main.go` `apiMiddleware` now rejects any request without a valid Bearer token, except `POST /api/v1/login` itself.
*   **Frontend**: new login screen (`public/index.html`/`app.js`/`styles.css`), logout button in the sidebar, and `apiFetch`'s 401 handling now logs the user out and returns to the login screen instead of a dead-end alert.
*   **Credentials**: all 4 seed users reset to unique bcrypt hashes (`db/migration.sql`) — the previous shared hash's plaintext was never recorded anywhere. Dev-only plaintext is in `DEV_CREDENTIALS.local.txt` (gitignored, project root).
*   **Token expiry (Phase 1.4)**: also closed 2026-07-12 — issued tokens now embed an `exp` claim (default 24h TTL, `JWT_EXPIRY_HOURS` overridable) and `ParseToken` rejects expired ones.

---

## 11. Version Control & Git History

*   **Remote Repository**: `https://github.com/Vishalnayak226/custom_erp.git`
*   **Branch**: `main`
*   **Latest Commit**: `ccbc539`
*   **Commit Message**: `Close Stage 13.13e: Manufacturing (scoped MVP - BOM + Production Order)`
*   **Date**: 2026-07-17

---

## 12. Stage 13 Build Records (What We Built, 2026-07-12 through 2026-07-17)

Closed the "business-user-facing ERP" gap identified in `docs/pdf_blueprint_gap_analysis.md` §1 — the codebase through Phase 7 was strong on kernel/omnichannel architecture but thin on the modules an actual end user touches day to day. Every item below shipped as its own commit, live-verified against a throwaway server instance before committing. Full scope decisions, bugs found/fixed, and verification detail live in `docs/micro_checklist.md`; this is a summary index, not the detail.

*   **13.1-13.3 Security & feature gating**: `securityHeaders` middleware (CSP/HSTS/X-Frame-Options/X-Content-Type-Options), `featureGate()` wrapper.
*   **13.4-13.7 Frontend for existing backend**: POS checkout screen, Finance/GL screen, Fulfillment screen, Marketplace screen (`public/app.js` `renderPOSView`/`renderFinanceView`/`renderFulfillmentView`/`renderMarketplaceView`). Fixed two location-filter bugs in `handleGenericDoc` where non-admin users saw zero results — first because `FulfillmentTask` stores `location_code` not `location`, then a deeper SQL `COALESCE(...) IS NULL` gap that hid records from doctypes with no location concept at all (e.g. `MarketplaceSettlement`).
*   **13.8 Maker-checker approval/workflow engine**: `engines/approval.go` — amount-slab + role + location routing, `approval_rules`/`approval_log` tables, `SubmitForApproval`/`DecideApproval`. Reused later by ExpenseClaim (13.13c) rather than rebuilt. A concurrent session later hardened `DecideApproval` against a TOCTOU double-decision race with a `FOR UPDATE` row lock (commit `bffea51`).
*   **13.9-13.10 Vendor/Customer masters + GST fields**: Vendor/Customer doctypes; Item `hsn_code`/`gst_rate` fields plus GST GL accounts.
*   **TOTP MFA**: `engines/mfa.go` — RFC 6238 from stdlib only (`crypto/hmac`, `crypto/sha1`, `encoding/base32`), enrollment/activation/challenge flow, required for HR/Admin roles only. Custom HMAC-signed purpose-tokens (`SignPurposeToken`) carry the MFA challenge state.
*   **13.11 Report catalog**: `engines/reports.go` — Current Stock, Sales Register, Vendor Ledger, Payables Ageing (4 prioritized reports, not the full ~80-report spec catalog).
*   **13.12 RFQ / Vendor Quote / Quote Comparison**: `engines/rfq.go` — `GetVendorQuotesForRFQ`, transactional `SelectWinningQuote` (winner→Selected, others→Rejected, RFQ→Closed).
*   **13.13a HR Foundation**: `engines/hr.go` — Employee/Attendance/Leave doctypes, Employee↔user access-link sync, payroll export.
*   **13.13b Fixed Asset Management**: `engines/assets.go` — capitalize/straight-line-depreciate/transfer/dispose lifecycle, asset register (fixed a display bug where disposed assets showed full original cost as net block instead of 0).
*   **13.13c Expense Management**: `engines/expense.go` — claim date-window + duplicate-bill validation, verify, pay (posts GL: Debit Employee Expense + GST Input Credit, Credit Cash/Bank). Reuses the 13.8 approval engine rather than a bespoke workflow.
*   **13.13d CRM/Loyalty (scoped MVP)**: `engines/loyalty.go` — append-only point ledger (earn/redeem, balance always `SUM(Earn)-SUM(Burn)`), wired into POS checkout as an additive side-effect. No campaigns/segmentation/vouchers.
*   **13.13e Manufacturing (scoped MVP)**: `engines/manufacturing.go` — single-level BOM explosion, linear Production Order (Draft → Material Issued → Completed), reuses the existing inventory stock-floor check. No routing/work centers, MRP, or QC gates.
*   **13.14 Per-API-type rate limiting**: `RateLimiter` keyed `ip:category` (login/bulk-upload/report/webhook/search/default) instead of just `ip` — fixed a real bug class where heavy traffic on one endpoint exhausted the shared budget for an unrelated one.
*   **13.15 Sticker/barcode printing**: `engines/stickers.go` — print + history, `@media print` CSS label layout. Scoped to text labels, not real scannable symbology (unverifiable in this dev environment).

For the current build tracker (what's still open), see **[docs/micro_checklist.md](micro_checklist.md)**.

---

## 13. Stage 14-17 Build Records (What We Built, 2026-07-18 through 2026-07-19)

Full scope decisions, bugs found/fixed, and live-verification detail for every item below live in `docs/micro_checklist.md` (Stages 14-17); this is a summary index, not the detail, matching the convention §12 established for Stage 13.

*   **Stage 14 — Control plane, versioning, module governance, patch automation, extension isolation, security hardening.** A real multi-environment deployment pipeline (`environments.json` + `promote.ps1` + `manage.ps1 -Env` — git-worktree checkout → stripped native binary → restart, one Postgres instance per environment on its own port); module/feature governance (`public.modules`/`tenant_default.module_entitlements`, `moduleGate()`); a patch/bug-intake pipeline with worker wiring; 3rd-party extension isolation and source-protection controls; security hardening (account-level brute-force lockout with a real timezone-comparison bug found and fixed during live verification, Shopify inbound webhook HMAC signature verification, a generic `VerifyWebhookHMAC` helper for future webhooks); Go toolchain bumped 1.22.5 → 1.22.12 (30 → 27 reachable `govulncheck` CVEs — the rest need a 1.23+ major-version jump, not attempted). **Containerization was built on explicit request (14.25), then reverted on explicit request (2026-07-19)** — the user's standing preference is no Docker dependency in this project's workflow; `promote.ps1`/`manage.ps1 -Env` remains the one real deployment path.
*   **Stage 15 — PIM (Product Information Management) Foundation MVP + V2 alignment.** `engines/pim.go`: Family/Attribute framework as generic doctypes, parent/variant grouping on `Item`, approval-gated `ProductContent` (reuses the Stage 13.8 approval engine, zero new approval code), a completeness-scoring engine, and a Product Workbench screen. V2 added locale/channel-aware completeness, an 8-state `PIMProductProfile` publish lifecycle, a genuinely new Media Library (`engines/pim_media.go` — this codebase's first file-upload infrastructure: content-addressed storage, MIME allowlist + content-sniffing, auth-gated retrieval outside `public/`), a Channel Publishing framework (`engines/pim_publish.go` — queue/idempotency/retry machinery real, connector itself a stub pending real platform credentials at the time), and job-tracked CSV Import/Export preview. AI Assist stayed explicitly out of scope.
*   **Stage 16 — Real e-commerce channel connectors + remaining PIM gaps.** A connector framework (`engines/connector.go`) with an AES-256-GCM encrypted credential vault (`engines/channel_credentials.go`) and real, code-complete connectors for Shopify, BigCommerce, and Magento/Adobe Commerce (`engines/connector_shopify.go`/`connector_bigcommerce.go`/`connector_magento.go` — each unit-tested against an `httptest` fake platform; live-store verification explicitly deferred until real dev-store credentials are supplied). Closed the remaining PIM gaps: dashboard (`engines/pim_reports.go` `GetPIMDashboard`), bulk edit (`engines/pim_bulk.go`), the 4 PIM reports, and field-level permissions (`engines/field_permissions.go` + `db/migrations_stage16_field_permissions.sql`). The same commit (`c09d72e`) also landed Stage 9 integration connectors (`engines/clevertap.go`, `engines/pinelabs.go`, `engines/unicommerce.go`) and Log Hub frontend wiring — recorded here for the index; see `docs/micro_checklist.md` and the commit itself for their scope detail, not independently re-verified in this summary pass.
*   **Stage 17 — Controlled post-PIM execution queue.** **17.1** generic-document soft delete (additive `deleted_at` tombstone, `SOFT_DELETE_<doctype>` audit event, master reactivation endpoint, Approved transactional documents blocked from deletion). **17.2** CSV formula-injection protection (`sanitizeCSVCell`, applied to every import and export/error-file path). **17.3** backup/restore baseline (`manage.ps1 backup`/`restore -Env`, SHA-256 sidecars, a real restore drill recorded in `docs/backup_restore.md`). **17.4** accounting-period control (`engines/accounting_periods.go` — admin-managed periods, `PostDoubleEntry` rejects postings inside a Closed one). **17.5** GST posting enforcement at both PurchaseOrder creation and checkout (`engines/gst.go` — HSN/rate gate, tax-inclusive-rate breakdown auto-computed and stored, checkout posts the split to new `2200`/`2201`/`2202` GL accounts). **17.6** transfer-order dispatch/receive lifecycle (`engines/transfer_orders.go` — new `in_transit` column, row-locked, shortage variance recorded not silently reconciled). **17.7** purchase requisition workflow (`engines/procurement.go` — reuses the existing approval engine unchanged; one-time Approved→Draft-RFQ/PO conversion). **17.8** vendor invoice + three-way match + payment (`engines/vendor_invoice.go` — DB-level duplicate-invoice constraint, `Match3Way` genuinely uses GRN's received quantities against the PO's own rates, payment posts GL before flipping status). **17.9** Location/LegalEntity/Department/CostCenter masters (`engines/location_masters.go` — 103 legacy location codes seeded so validation never breaks existing data; wired into PurchaseOrder/TransferOrder only, explicitly not a blanket retrofit). **17.10-17.11**: code/docs/tooling built and verified per §14 below; each still has one real-world input only the user can supply before it's fully DONE. See `docs/micro_checklist.md` Stage 17 for full detail.

---

## 14. Stage 17.10-17.11 Build Record (What We Built, 2026-07-19)

Both items were blocked on real-world input the AI cannot supply (operational contacts; non-production platform credentials) rather than on implementation — per user direction, everything buildable ahead of that input was built and verified this session. Full detail in `docs/micro_checklist.md`'s 17.10/17.11 entries.

*   **17.10 Runbook and alerting.** `docs/operations/incident_runbook.md` (P0-P3 severity table, rollback via `promote.ps1 -Rollback`/`manage.ps1 restore`, log locations, escalation-contacts table left as an explicit placeholder). `engines/alerting.go`: `SendOpsAlert` posts a truncated, secret-free summary to a Slack/Teams-compatible webhook (`OPS_ALERT_WEBHOOK_URL`, safe no-op when unset); `StartAlertMonitor` polls for a sustained error rate (20+ `system_error_logs` rows/5 min per tenant, one alert per cooldown window). Panic recovery (`engines/logs.go`) and failed backups (`manage.ps1`'s new `Send-OpsAlert`) both wired to the same mechanism. 4 new unit tests (`engines/alerting_test.go`); `go build`/`vet`/`test ./... -p 1` clean. **Live-verified** on a throwaway instance against a local mock webhook receiver (no real Slack/Teams destination existed to point at): triggered the existing `/api/v1/debug/panic` route, confirmed both the `system_error_logs` row and the webhook payload (severity/source/message only, no stack trace) arrived correctly. Open: real escalation contacts and a real `OPS_ALERT_WEBHOOK_URL` — the code path is proven, only the destination is missing.
*   **17.11 Live connector verification.** `scripts/verify_connector_live.ps1`: given real credentials (`CONNECTOR_CREDENTIALS.local.json`, gitignored) and a pre-approved disposable Item, creates a throwaway Channel, runs the existing PIM readiness check, triggers a real publish, polls to a terminal status, reports the platform's own external id or rejection, then cleans up its own fixtures. Deliberately does not script Item/ProductContent creation itself (that stays on the already-verified PIM Workbench + approval UI path) or login/MFA (no TOTP secret stored on disk for a one-time script). `docs/operations/connector_live_verification.md` documents the exact credential shape (read directly from each connector's source, not guessed) and procedure. Both PowerShell scripts verified to parse with zero syntax errors. Not run end-to-end — no real store credentials exist in this environment, which is this item's entire remaining blocker.

---

## 15. Stage 19 Build Record — Documentation Suite & Folder Restructuring (What We Built, 2026-07-19)

Per explicit user request, in stated order: a full project blueprint for outside/AI review, a real folder restructuring ("as per standard development practice" — the user chose the full `cmd/`+`internal/` Go split over a lighter-touch option when asked, accepting the added risk explicitly), a `docs/` reorganization with nothing deleted, and a new BRD/PRD/User Guide/Admin Guide. Full detail and verification steps: `docs/micro_checklist.md` Stage 19.

*   **`docs/ERP_BLUEPRINT.md`** — a full project snapshot for an external reader, five-persona framing (CEO/Product/CTO/Developer/Tech-lead), every claim cited back to an existing source doc.
*   **Go restructuring.** `main.go` (~4,681 lines, package `main`, root of the repo since Phase 1) moved to `internal/server/` (renamed `package server`), split into 8 domain files within that same package, with a new thin `cmd/server/main.go` entrypoint. The riskier alternative — decomposing into several separate *packages* — was deliberately scoped out as separate future work; this stayed a same-package reorganization, so no cross-package visibility risk. Every build command that assumed repo-root-as-package-main (`manage.ps1`, `promote.ps1`, including an `-ldflags -X main.gitCommit=...` target that had to become a full import-path reference) was updated and re-verified. `go build`/`vet`/`test ./... -p 1` clean throughout; live-verified via a fresh binary build and smoke test (login, version, RBAC-denied doc read, static file serving, panic recovery) on a throwaway port. Found and fixed, while in the area: `promote.ps1`'s `$args` automatic-variable shadowing and 3 PSScriptAnalyzer unapproved-verb function names.
*   **`docs/` reorganization.** New `architecture/`, `specs/`, `operations/`, `requirements/`, `guides/` subfolders; `micro_checklist.md`/`project_ledger.md`/`ai_handover.md`/`ERP_BLUEPRINT.md` stay at `docs/` root per `CLAUDE.md`'s hardcoded sync-convention paths. New `docs/README.md` index. Nothing deleted — every cross-reference (clickable links and prose path citations) fixed to the new locations across the whole repo, verified by a full grep sweep; historical narrative mentions left untouched.
*   **BRD/PRD/User Guide/Admin Guide.** Grounded in three external planning references the user pointed to mid-session — extracted from `.docx` to plain text since they're binary — which describe a considerably larger target platform (full warehouse bin/putaway/pick-pack, offline POS, e-invoicing/IRN, an ~80-report catalog, a formal error-proofing matrix) than what's built today. Every status claim in the new documents is checked against `micro_checklist.md`, not assumed from those references. User Guide is plain-language/non-technical; Admin Guide is a standalone, zero-AI-assistance operator manual layered from first-time setup through developer/CTO-level detail, reusing rather than duplicating the existing operations docs.

For the current build tracker (what's still open), see **[docs/micro_checklist.md](micro_checklist.md)**.
