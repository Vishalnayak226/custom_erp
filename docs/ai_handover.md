# AI Handover Note: Developer Guide

Setup map, port directory, command recipes, and build history for the next AI agent or developer picking up this codebase.

---

## 1. System Environment & Port Bindings

- **Go**: Portable Go 1.22.5 at `$env:USERPROFILE\go-portable\go`.
- **PostgreSQL**: Portable 16.3 at `$env:USERPROFILE\pg-portable\pgsql`.
  - Port: **5435** (trust auth, no password)
  - Data dir: `$env:USERPROFILE\pg-data`
  - User/DB: `postgres` / `custom_erp`
- **Web app**: port **8080** (`http://localhost:8080`).
- **Login required** (since 2026-07-12). Dev credentials: `DEV_CREDENTIALS.local.txt` (project root, gitignored).
- **Start/stop everything**: `.\manage.ps1` (menu) or `start`/`stop`/`restart`/`status`/`logs`.
  - Known issue: can hang in sandboxed/background shells — see §3.C.

---

## 2. Core Repository Map

| Path | What it is |
|---|---|
| [go.mod](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/go.mod) | Module config (`module custom_erp`) |
| [cmd/server/main.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/cmd/server/main.go) | Real entrypoint — thin launcher for `internal/server.Run()`. Build via `./cmd/server`, not the repo root. |
| [internal/server/](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/internal/server/) | API router, tenant resolution, panic recovery. Split by domain into `routes.go`, `middleware.go`, `handlers_*.go` (see `README.md`). |
| [db/db.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/db/db.go) | Connection pool + tenant schema switching |
| [db/migration.sql](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/db/migration.sql) | Tables, Chart of Accounts, users, defaults |
| [engines/numbering.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/numbering.go) | Sequence numbering |
| [engines/labels.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/labels.go) | Translation CRUD |
| [engines/logs.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/logs.go) | Audit trail + panic recovery logger |
| [engines/inventory.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/inventory.go) | Stock counts, availability, reservations |
| [engines/finance.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/finance.go) | Double-entry GL postings |
| [engines/sourcing.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/sourcing.go) | Order routing, webhook idempotency |
| [engines/fulfillment.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/fulfillment.go) | Picking tasks, re-routing, Return Anywhere |
| [engines/marketplace.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/marketplace.go) | Settlement reconciliation, logistics tracking |
| [engines/optimization.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/optimization.go) | Forecasting, replenishment, SLA checks |
| [engines/saas.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/saas.go) | Tenant provisioning, feature flags |
| [engines/outbox.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/outbox.go) | Outbox sync poller, integration log retry |
| [engines/mfa.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/mfa.go) | TOTP MFA (RFC 6238, stdlib-only) |
| [engines/approval.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/approval.go) | Maker-checker approval engine |
| [engines/reports.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/reports.go) | Core report set (Stock, Sales, Vendor Ledger, Payables) |
| [engines/rfq.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/rfq.go) | Vendor RFQ / quote comparison |
| [engines/gst.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/gst.go) | GST calculation (calc-only, no e-invoicing) |
| [engines/stickers.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/stickers.go) | Barcode label printing (text, not scannable symbology) |
| [engines/hr.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/hr.go) | Employee access-link sync, payroll export |
| [engines/assets.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/assets.go) | Fixed Asset lifecycle |
| [engines/expense.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/expense.go) | Expense claims (posts GL via approval engine) |
| [engines/loyalty.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/loyalty.go) | CRM/Loyalty point ledger (scoped MVP) |
| [engines/manufacturing.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/manufacturing.go) | BOM + Production Order (scoped MVP) |
| [engines/engines_test.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/engines_test.go) | Unit-style tests, direct engine calls (no HTTP) |
| [internal/server/server_test.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/internal/server/server_test.go) | HTTP-level integration test (`httptest`, no real socket) |
| [public/index.html](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/public/index.html) | UI layout |
| [public/app.js](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/public/app.js) | UI routing + translation engine |

70+ more `engines/*.go` files exist beyond this table (bank reconciliation, payment proposals, TDS, connectors, extensions, reports, password reset, etc.) — this is a curated map of the core/oldest files, not an exhaustive list. See `README.md`'s Project Structure for the full file tree.

---

## 3. Development Command Recipes

Run all commands from the repo root: `c:\Users\ABCD\Documents\Antigravity Projects\ERP`

**A. Unit tests**
```powershell
& "$env:USERPROFILE\go-portable\go\bin\go.exe" test ./...
```

**B. Compile backend**
- Dev build (keeps debug symbols):
  ```powershell
  & "$env:USERPROFILE\go-portable\go\bin\go.exe" build -o erp-server.exe ./cmd/server
  ```
- Release build (stripped, ~30% smaller): `.\manage.ps1 release`, or:
  ```powershell
  & "$env:USERPROFILE\go-portable\go\bin\go.exe" build -ldflags="-s -w" -o erp-server.exe ./cmd/server
  ```
- Always target `./cmd/server` — the repo root is no longer package `main` (since Stage 19).
- Windows locks a running `.exe`; `manage.ps1 release` stops the server first but does not restart it.

**C. Start PostgreSQL**
```powershell
& "$env:USERPROFILE\pg-portable\pgsql\bin\postgres.exe" -D "$env:USERPROFILE\pg-data" -p 5435
```
- Run as a foreground process in sandboxed shells — `pg_ctl start` (detached) gets killed with the session.
- Known issue: `manage.ps1`'s `pg_ctl start -w` can hang in a backgrounded/sandboxed shell even though Postgres itself started fine. Check `pg_isready -p 5435` directly if `manage.ps1 start` looks stuck.

**D. Start the server**
```powershell
& "c:\Users\ABCD\Documents\Antigravity Projects\ERP\erp-server.exe"
```

---

## 4. Multi-Tenant Development Standards

When building any new module:
1. **Resolve tenant** — read the `Resolved-Tenant-ID` header in every handler.
2. **Scope every query** — call `db.SetSearchPath(tx, schema)` before any query. Never hardcode a schema.
3. **Audit everything** — log every document transition, submission, and approval.
4. **Use the numbering engine** — `engines.GenerateSequence()` / `GenerateVariantCode()`, never a hand-rolled counter.

---

## 5. Omnichannel Scale Architecture

Design rules for scaling 1 store → 2,000 stores without locking the transaction DB:

1. **Outbox pattern for external sync** — never call Shopify/payment/GST APIs synchronously from a user-facing transaction.
   - Write the record + an outbox event in the same SQL transaction.
   - A background worker publishes the event asynchronously.
2. **Available-to-Sell (ATS)** — never query the raw ledger for real-time channels. Use:
   `ATS = Available - Reserved - Safety Stock - Channel Holds`
3. **Idempotency** — every webhook payload must carry a signature, timestamp, and unique ID to block duplicate orders/invoices.

---

## 6. Version Control & Handover Status

- **Remote**: `https://github.com/Vishalnayak226/custom_erp.git` (branch `main`)
- **Latest commit**: `c45f934` — "Stage 25 fully closed (Batches 4-6, 25.8-25.10) + Stage 22.6 role-filtered sidebar" (2026-07-23)
- **Full chronological detail**: `docs/project_ledger.md`. **Full backlog/scope-per-item**: `docs/micro_checklist.md`. This section is a condensed index, not the source of truth.
- **Standing verification practice** (true for nearly every entry below, not repeated each time): built on a throwaway port, `go build`/`go vet`/`go test ./... -p 1` run clean, live-verified against the real dev DB, test data cleaned up afterward. Only deviations (skipped verification, a failed test, left-over data) are called out per entry.

### Build timeline (condensed, newest reasoning at the bottom)

- **Phases 1-7** — Core Foundation → Advanced Optimization. Built and pushed. Phase 7 has a known bug (see "Known issues" below).
- **Real login (2026-07-12)** — Login screen added; `apiMiddleware` now rejects unauthenticated requests except `/login`. Token expiry added (`tokenTTL()`, 24h default via `JWT_EXPIRY_HOURS`).
- **SaaS provisioning & feature flags** — Per-tenant schema cloning + a unique random admin password per tenant. Feature flags are set-only; no handler enforces them yet.
- **Error Logs Hub & Outbox retries** — Backend endpoints built; frontend never wired up. Webhook signature verification was missing (closed later, Stage 14).
- **Premium UI overhaul** — Stripe/Linear-style dashboard, custom dialogs for alert/confirm. Some `prompt()` calls stayed raw (closed later, Stage 21).
- **Stage 13** (2026-07-12–17) — closed the business-user-facing gap.
  - Security headers, feature gating, POS/Finance/Fulfillment/Marketplace screens
  - Maker-checker approval engine (reused by every later approval-gated feature)
  - TOTP MFA, Vendor/Customer masters, GST fields, report catalog, RFQ comparison
  - Per-category rate limiting, sticker printing
  - HR, Fixed Assets, Expense, CRM/Loyalty, Manufacturing MVPs
  - Ref: `project_ledger.md` §12
- **Stage 14-17** (2026-07-18–19)
  - 14: deployment pipeline, module governance, security hardening, Go 1.22.12. Docker built then reverted (standing no-Docker policy).
  - 15: PIM Foundation (Family/Attribute, approval-gated content, first file-upload infra)
  - 16: real Shopify/BigCommerce/Magento connectors, PIM dashboard/bulk-edit
  - 17: soft-delete, CSV sanitization, backup/restore, accounting periods, GST enforcement, transfer-order lifecycle, purchase requisition, vendor invoice 3-way match, location masters
  - Ref: `project_ledger.md` §13
- **Stage 17.10-17.11** (2026-07-19) — built, blocked only on real-world input.
  - 17.10: incident runbook + Slack/Teams alerting — needs real contacts + webhook URL
  - 17.11: connector live-verification script — needs real store credentials
  - Ref: `project_ledger.md` §14
- **Stage 18** (2026-07-19) — dropdown/autosuggest UX.
  - `attachTypeahead()` wired into 12 fields across 7 screens
  - Bug fixed: new component wiped its own results before rendering
  - Bug fixed (pre-existing): Location/LegalEntity/Dept/CostCenter 403'd for everyone — module never registered
  - Gap flagged, not fixed: Store Manager/Cashier can't read Vendor/Item/Customer
  - Ref: `project_ledger.md` §16
- **Stage 19** (2026-07-19) — docs suite + folder restructuring.
  - `ERP_BLUEPRINT.md`; `main.go` moved into `internal/server/`
  - `docs/` reorganized into subfolders; BRD/PRD/User+Admin guides added
  - Ref: `project_ledger.md` §15
- **Stage 20 planning** (2026-07-19) — reviewed the maturity master-plan PDF, broken into 40 items: Track A (blocked on user input) + Track B (buildable). Ref: `specs/erp_maturity_master_plan.md`.
- **Stage 20 Track B.1 — POS Maturity** (2026-07-20)
  - `POSProfile`/`POSSession`; checkout requires an open session
  - Payment-mode-aware GL posting (Cash/Card/UPI)
  - Discount gate routes through the approval engine
  - Bug fixed: `POSCart.created_by` hardcoded to `'system'`, defeating maker-checker
  - Ref: `project_ledger.md` §18
- **Stage 20 Track B.2 — WMS Maturity** (2026-07-20)
  - Bin master, putaway, pick lists, pack/dispatch, cycle counts
  - Bug fixed: CSV-imported quantities parsed as zero — posted -100 instead of -10
  - Ref: `project_ledger.md` §20
- **Nav redesign + renames** (2026-07-20)
  - "DocType Builder" → "Database Schema Design", "Log Hub" → "Activity Log"
  - Sidebar: 27 flat links → 12 module flyouts
  - Bug fixed: flyout menu ran off the viewport bottom
  - Ref: `project_ledger.md` §21
- **Same-day follow-ups** — flyout dimming tried then reverted (user disliked the fade); "DocType" text swept to "Record Type"; "Stores" master fixed (3 compounding bugs: zero fields, zero permissions, a singular/plural nav typo).
- **Stage 21 — UI/UX overhaul** (2026-07-20)
  - Table scroll fix, KPI style fix, killed remaining raw `prompt()` calls
  - Icon-minimal sidebar, account-menu redesign, new User Profile screen + idle-timeout
  - `CLAUDE.md`'s "first principle" added
  - Ref: `project_ledger.md` §17
- **Stage 22 — QA sweep** (2026-07-20)
  - Found Inventory/Transfers/Users/Roles were dead mocks — built all 4 for real
  - Bug fixed: generic update replaced (not merged) JSONB; raw Postgres error leaked to UI
  - Ref: `project_ledger.md` §19
- **Stage 23 — error/message catalog** (2026-07-20)
  - 302-code catalog generated from the user's xlsx; one shared `writeAPIError`/`writeAPIErrorGeneric` choke point
  - Toast/Page Banner UI primitives added
  - Gap flagged: catalog has no HTTP 400/405 rows (resolved in the follow-up below)
  - Ref: `project_ledger.md` §22, `specs/message_catalog.md`
- **Stage 24 plan** (2026-07-20) → **audited 2026-07-22, found already built** (see below).
- **Stage 20 Track B.3 — Finance Maturity** (2026-07-20)
  - Bank reconciliation, payment proposals, TDS, debit/credit notes
  - `SalesInvoice` (dormant since Stage 1) wired up for real, enabling Receivables Ageing
  - Ref: `project_ledger.md` §23
- **Stage 20 Track B.4 — Reports Engine** (2026-07-20)
  - `ReportDefinition` framework, saved filters, async export, column masking
  - 7 new reports added; 2 skipped (no underlying data model)
  - Stage 20 now fully closed except Track A + 20.30/20.31 (blocked on GSP credentials)
  - Ref: `project_ledger.md` §24
- **Stage 25 Batches 1-2** (2026-07-21)
  - Audited all 187 "Mature ERP" catalog codes — only 7 were wired
  - Added a shared `ValidationError` type to the one generic validation path
  - Master Data checks: HSN format, GSTIN format, mobile format, duplicate barcode
  - Ref: `project_ledger.md` §26
- **Stage 23 follow-up** (2026-07-21)
  - Decided: standardize on HTTP 422 (186 call sites converted)
  - Decided: keep `featureGate`'s fail-closed 403
  - Toast/Page Banner wired via a `display_style` field
  - Regression found (pre-existing, not fixed): `FinanceDoubleEntryAndPOS` trial-balance mismatch
  - Ref: `project_ledger.md` §25
- **Doc sync pass #1** (2026-07-21, no code) — fixed stale "uncommitted" notes and commit pointers; confirmed zero new dependencies and additive-only migrations since Stage 1; refined `CLAUDE.md`'s pattern-reuse guidance.
- **Stage 25 Batch 3** (2026-07-22)
  - Manufacturing/HR/GRN/PO/Sales/Transfer/Vendor/WMS/Assets: 29 of 75 codes wired
  - 2 bugs fixed: a missing check design had never been coded; cancelled GRNs still counted toward PO-received totals
- **Stage 24 audit** (2026-07-22) — found all 24 core items already built and committed; live-verified the highest-risk ones (token claims, role gates, optimistic locking).
- **Stage 24.27-24.32** (2026-07-22) — built all 6 previously-deferred items on request: startup guard against the default admin credential, password reset flow, connector circuit breaker, per-tenant request quota, field max-length check, batched CSV import.
  - New regression found (DB-state drift, not a code bug): `DocTypeValidationAndAuth` fails on an unrelated `Brand` field
- **Stage 21.9 — SOP docs** (2026-07-22)
  - `USER_SOP.md` + `ADMIN_SOP.md` written
  - Real gap fixed: generic record lists had Delete but no Edit — added Edit + optimistic-locking conflict handling
  - Ref: `project_ledger.md` §29
- **Stage 20.13 + 21.11** (2026-07-22)
  - Offline POS queue (localStorage, replays on reconnect, allows negative stock with a flagged variance)
  - Bug fixed: undefined variable in the session-close success alert
  - Sidebar module renaming (Odoo/ERPNext-style labels)
  - Ref: `project_ledger.md` §31
- **Stage 22.6 + Stage 25 Batches 5-6/25.8/25.10** (2026-07-23) — **Stage 25 fully closed**.
  - Sidebar now role-filtered, derived from `role_permissions`
  - Channel Connectors/Omnichannel: 4/10 wired; bug fixed (circuit-breaker-open jobs wrongly marked Failed)
  - POS Cash Drawer/Extension Hooks: 5/6 wired
  - New deployment-status/backup-status endpoints + subscription-limit check
  - Ref: `project_ledger.md` §33
- **Stage 25 Batch 4** (2026-07-23) — Admin/Config through Order Mgmt: ~30/57 wired (two sessions worked this concurrently).
  - 2 bugs fixed: a config error mapped to a blanket 500 instead of 422; a field-name collision silently overwrote existing fields
  - Ref: `project_ledger.md` §32
- **Doc sync pass #2** (2026-07-23, no code) — fixed a header/intro contradiction in Stage 25's status, and re-bumped commit pointers that had gone stale again.

---

## Current State (check before trusting anything above)

- **Git**: `main` @ `c45f934`, everything above committed. One legitimate in-flight doc edit may remain in `micro_checklist.md` — run `git status --porcelain` to confirm.
- **Unrelated in-flight work**: `docs/Contract/`, `docs/extension_hooks_checklist.md`, `docs/github_checklist.md`, `TEMP_menu_naming_comparison.md` — a separate session's legal/security planning docs, nothing to do with the Stage tracker. Don't sweep these into an unrelated commit.
- **Local binaries are stale**: `custom_erp.exe`/`erp-server.exe` were last built 2026-07-20, before every commit from Stage 23 follow-up onward. No server is currently running on port 8080. Rebuild before assuming the live app reflects any recent work.
- **Concurrent sessions are common in this repo** — this tree has repeatedly seen 2-3 sessions (and the user) editing it in the same window. Before any edit or commit:
  1. Run `git status`/`git diff` first — never assume the tree is clean.
  2. Never `git add -A` — stage only the files you actually reviewed.
  3. If a build fails transiently mid-edit, it may be another session's in-flight change — wait and retry before debugging.
  - Past collisions (for pattern recognition, not action needed): a Stage-numbering clash (20 vs 21) caught via `git status` and renumbered before shipping; three sessions editing the same files in one window on 2026-07-20; two sessions independently picking the identical Stage 25 batch on 2026-07-23 and converging on the same fix.
- **Known issues**: `docs/operations/hardening_roadmap.md` (security/correctness backlog) fully closed as of 2026-07-12, historical record only. `docs/specs/pdf_blueprint_gap_analysis.md` is a superseded 2026-07-12 snapshot. Active backlog lives in `docs/micro_checklist.md`.
- **Test suite**: two known, pre-existing, unrelated failures reproduce on every run — not regressions, already root-caused as DB-state drift:
  - `TestEngines/FinanceDoubleEntryAndPOS` — trial-balance mismatch
  - `TestEngines/DocTypeValidationAndAuth` — `Brand` doctype has picked up extra mandatory fields from unrelated industry-profile testing
- Every test file's `db.InitDB` uses a hardcoded connection string to the real `custom_erp` DB on port 5435 — `DATABASE_URL` has no effect on `go test`. Apply new migrations to `custom_erp`, not just `custom_erp_test`.

---

## 7. Handover Notes for Incoming AI (Claude / Codex / Gemini)

Welcome! The core system plus a growing set of business modules (POS, Finance/GL, GST, MFA, approvals, HR, Fixed Assets, Expenses, CRM/Loyalty, Manufacturing) are built and operational.

Before treating any `[x]` checklist item as fully closed, read its entry in `docs/micro_checklist.md` — each one documents its exact scope and any deliberate limitations.

**To resume development:**
1. Pull latest `main`.
2. Start PostgreSQL on port `5435`.
3. Run `go test ./...` to verify business rules.
4. Start the server: `./erp-server.exe` or `go run ./cmd/server`.
5. Open `http://localhost:8080` — log in with `DEV_CREDENTIALS.local.txt` (gitignored, project root; regenerate via a throwaway bcrypt script if missing, then update `db/migration.sql` and the live `users` table).
6. See `docs/project_ledger.md` for the full chronological build history.
