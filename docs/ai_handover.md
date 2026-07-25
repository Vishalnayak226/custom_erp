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
- **Latest commit**: this session (2026-07-25) committed four in sequence on top of `a698087`/`7d86264`: `cd86eb8` (Phase 26.3 close-out: 26.3.2/26.3.3/26.3.5/26.3.6), then the Stage 26.12 foundation+Order-Engine+Allocation/Sourcing commit (26.12.9/26.12.6/26.12.1/26.12.2), then a small `engines_test.go`-only commit adding the `OrderEngine`/`AllocationSourcing` test coverage that first commit's message had claimed but the test file itself (mid-edit by a concurrent session at the time) hadn't yet carried, then this session's own **26.12.4 (Courier/Shipment/Manifest)** commit - see the bullet below. Run `git log --oneline -6` for exact hashes. **A concurrent session's work remains UNCOMMITTED as of this note**: Stage 26.12.3 (Fulfillment Pick/Pack: `engines/fulfillment_pickpack.go`, `db/migrations_stage26_12_3_pick_pack.sql`, its share of `internal/server/handlers_wms.go`/`routes.go`/`engines/engines_test.go`) and **Stage 26.5 (WMS Enterprise Maturity Sprint) — now fully built and closed, all 10 items** (`engines/wms_picking.go`/`wms_putaway_ext.go`/`wms_receiving.go`/`wms_pack_count.go`, `engines/wms_enterprise_test.go`, `internal/server/handlers_wms_enterprise.go`, `db/migrations_stage26_5_wms_enterprise.sql`, plus its share of `engines/wms.go`/`transactional_validation.go`/`handlers_core_doc_engine.go`/`routes.go`/`public/app.js`/`public/index.html`) - see `project_ledger.md` §51, `micro_checklist.md` Phase 26.5. None of it overlaps the four commits above (confirmed via `git diff` before each commit - two of the four used a hand-built partial patch via `git apply --cached` on a single hunk, specifically to commit only this session's own lines out of a shared file mid-edit by the other session, verified compilable in isolation via `git stash push --keep-index` before committing). **A live git-hygiene incident happened mid-Stage-26.5-build**: another concurrent session's own "verify staged-only" workflow (a `git stash -u --keep-index`-shaped sequence) briefly swept every uncommitted file in the tree — including all of this Stage's new/edited files — into a stash, then auto-popped it back seconds later; nothing was lost (re-confirmed by re-grepping for this Stage's own code markers before continuing), but it's a concrete demonstration of why `git status`/`git diff` must be re-checked immediately before every commit in this tree, not just once at session start. Review `git diff` file-by-file before committing any of it. `TEMP_menu_naming_comparison.md` remains untracked/uncommitted (a separate session's own in-progress doc, not part of the Stage tracker) — leave it alone.
- **Stage 26.4 — PIM/PXM Maturity Sprint built and closed (2026-07-23, code)** — all 9 buildable items (`26.4.1`-`26.4.9`: attribute groups + locale/channel overrides, duplicate-item/content detection, audit-log-backed taxonomy history, media versioning/thumbnails/alt-text/expiry, content owner/SLA/rejection-comment surfacing, bulk approval + content-version rollback, channel validation packs + publish diff preview, a marketplace connector-error dictionary, a search-feed CSV export) built, migrated (`db/migrations_stage26_4_pim_maturity.sql`), and live-verified — both via direct API calls against the real dev DB and a real-browser Playwright/Chromium pass through the actual Workbench screens. Found and fixed one real pre-existing bug in the process: the CSP's `img-src` had no `blob:` source, silently blocking every PIM media thumbnail/image from ever rendering in a real browser since Stage 15.2. Full detail: `project_ledger.md` §37.
- **Stage 26 planning added (2026-07-23, no code)** — `ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf` (successor to the plan behind Stage 20) reconciled against live repo and broken into `micro_checklist.md` Stage 26 (`26.0`-`26.11`, ~85 items). Two of the PDF's own claims were found stale/wrong and corrected rather than carried forward — see `docs/specs/erp_maturity_master_plan.md` §2 and `project_ledger.md` §34: (1) industry packs — only Logistics/Transportation is genuinely missing, not 7; (2) the finance trial-balance "regression" is very likely shared-DB test-fixture debris, not broken finance code (see "Test suite" note below, updated with a fresh 2026-07-23 re-run).
- **Standalone WMS project retired, knowledge migrated (2026-07-23, no code)** — a second, independently-started project (separate Go service + React frontend, built against a different "Universal WMS Master Blueprint" PDF) was read in full, compared against this repo's existing WMS work (Stage 20 Track B.2, already covers most of it) and Stage 26.5 (already plans the rest), then retired — its architecture conflicted with this repo's no-new-framework/single-server rules. Durable design content kept at new file `docs/specs/wms_master_blueprint_reference.md`; two concrete gaps it surfaced (`engines/stickers.go` prints text not scannable barcodes; `WriteStockLedgerEntry` needs idempotency/location/user fields before 26.10.1's report is buildable) annotated inline on the relevant `micro_checklist.md` items. Full detail: `project_ledger.md` §35. **The standalone project's own folder was deleted this session** — the user reviewed the migrated docs above and explicitly confirmed full deletion (not archive).
- **Standalone OMS project retired, knowledge migrated (2026-07-23, no code)** — a third, independently-started project (separate Go multi-tenant service + a mock vanilla-JS frontend, built against `Inhouse_OMS_Master_Blueprint.pdf`/`Inhouse_OMS_Module_BRD_Pack.pdf`) was read in full, compared against this repo's existing order/fulfillment/marketplace work (`engines/sourcing.go`/`inventory.go`/`fulfillment.go`/`marketplace.go`/`unicommerce.go` — real but MVP-thin), then retired — same architecture conflict as WMS. Durable design content kept at new file `docs/specs/oms_master_blueprint_reference.md`; folded into a new **`Stage 26.12` — OMS/Order Management Maturity Sprint** (`micro_checklist.md`), since unlike WMS this repo had no Order-Management phase at all before this pass. Full detail: `project_ledger.md` §36. **Unlike the WMS folder, the standalone OMS folder was deleted this session** — the user explicitly asked for it once migration was verified; its GitHub remote (`custom_oms`) preserves history.
- **Stage 26.12 effort plan (2026-07-24, no code)** — user asked whether any OMS code had actually been built (none had) and for an effort estimate. All 10 items sized in "sessions" (this repo's own historical build-pass unit): **total ≈ 7.5-9.5 sessions, realistically 1-1.5 weeks** at this repo's demonstrated cadence. 26.12.1 (Order Engine) and 26.12.4 (Courier/Shipment/Manifest) are the two **L**-sized items and the biggest swing factors (an open doctype design decision, and real-courier-API vs. internal-only scope respectively). Recommended build order: 26.12.9+26.12.6 → 26.12.1 → 26.12.2 → 26.12.3 → 26.12.5 → 26.12.4 → 26.12.7+26.12.8 last. Effort tags written directly onto each `micro_checklist.md` Stage 26.12 item. Full detail: `project_ledger.md` §38.
- **Stage 26.3.4 — WMS Operations Screens built (2026-07-24, code)** — the first real WMS build (not just docs/planning) after §35's retirement: Putaway, Bin Conditions, and Cycle Count screens plus a Pick List modal on Fulfillment, all pure frontend (`public/app.js`/`index.html` only) on top of `engines/wms.go`'s already-working Stage 20 backend, which had zero UI until now. One real gap found+fixed: `CycleCountLine` (a Transaction doctype) had no way to be reached from the Setup submenu (Master-only filter) and thus no Bulk Import entry point — fixed with a direct link into the generic doctype-table view. Live-verified end-to-end via a scratch token + Playwright against real seeded fixtures (put away, condition-transitioned, reconciled, pick-list viewed) — all test data + scratch `cmd/` tools removed after. Full detail: `project_ledger.md` §39.
- **Stage 24 addendum (2026-07-24, no code)** — cross-checked `docs/ERP_LOOPHOLES_ANALYSIS.md`'s 21 "still open" findings against live source; 19 were stale (already fixed by Stage 24 or Stage 25's `ValidateTransactionalRules`), 2 genuine gaps added as `24.33`/`24.34` (ignored-JSON-unmarshal-error regression in 5 spots, no connection-pool monitoring). Full detail: `project_ledger.md` §40.
- **Stage 27 — Modular Product Packaging built (2026-07-24, code)** — user asked for PIM/WMS/OMS/HR/etc. to become independently sellable, any combination, each at its own URL (`/wms`,`/pims`,`/oms`,`/hr`,...), same single binary/DB. Built on top of the existing Stage 14 module-entitlement system rather than a new mechanism: new `wms`/`oms` module_keys (`db/migrations_stage27_product_packaging.sql`) close a real pre-existing gap (WMS floor-ops routes and the entire Unicommerce/BigCommerce surface had **no** entitlement gate at all before this); a `engines.ProductPackages` Go map is the master product/URL definition; a small dependency graph (`rfq`→`procurement` today) makes `SetModuleEntitlement` auto-enable prerequisites and refuse disabling a still-depended-on module; a new self-service `GET /api/v1/me/modules` and a server-side SPA path-fallback loop make the URLs and frontend nav-filtering (`MENU_MODULE_MAP`/`applyModuleEntitlements()`, mirroring the existing role-based sidebar filter) actually work. Three real bugs found and fixed during live verification (not just written and assumed correct) — see `project_ledger.md` §41 for all three. Full-suite tenants confirmed pixel-for-pixel unchanged (the core regression guarantee). Committed across `ef7d1d4`/`b33dbfd`/`220406d`.
- **Stage 26.1.2 — System Status screen built (2026-07-24, code)** — started Stage 26 "one leg at a time" per user request; of Phase 26.1's six items, 26.1.2 was the only one both buildable now and untouched by the concurrent Stage 27 session above. Pure frontend (`public/app.js`/`index.html` only, no Go/route/table changes) — a new HR/Admin-only "System Status" screen under Settings wires the already-existing Stage 25.8 `GET /api/v1/ops/deployment-status`/`GET /api/v1/ops/backup-status` endpoints (which already compute the Stage 17.10 DR-0213/DR-0214 overdue warnings) into stat tiles + 3 tables, off existing `.stat-card`/`.table-panel`/`.badge-*` CSS only. Live-verified via a throwaway port (8145) + scratch-token Playwright pass against the real dev DB — renders correctly against today's empty deployment/backup history, no console errors from the new code. 26.1.4/26.1.5 (tenant entitlement/usage admin screens) deliberately left open — they'd need `routes.go`/`modules.go`, the exact files Stage 27 above was mid-edit on. Full detail: `project_ledger.md` §42.
- **26.0.2 + 26.1.4 + 26.1.5 — Phase 26.0/26.1 closed out (2026-07-24, code)** — user asked to reevaluate and resume building Stage 26 "from the 1st open point, phase by phase" once Stage 27's file churn had settled (confirmed via `git log`: the Stage 27 work above had landed as real commits, not still in flight). **26.0.2** (test suite fixture isolation, `engines/engines_test.go`): traced both long-standing failures to their actual root cause rather than assuming shared-DB debris by default — finance's "9000 vs 9500" was a stale test assertion (Stage 24.5's idempotency-retry check added an untallied 500/500 posting), not DB contamination (`gl_postings` is wiped unconditionally at that subtest's own start); DocType's `Brand.fefo_enabled` genuinely was leftover Agriculture-industry-profile-application debris in the shared `default` schema, now reset defensively before asserting. `go test ./... -p 1` is fully green for the first time. **26.1.4/26.1.5** (`public/app.js`, `internal/server/handlers_integrations_admin.go`, `engines/modules.go`, `internal/server/middleware.go`): new **Tenant Entitlements** (plan/module picker per tenant, reusing Stage 27's `ProductPackages`/entitlement engine via a newly-extracted `engines.ApplyPackageSelection` choke point) and **Tenant Usage** (live per-tenant concurrency + configured-limit health, off Stage 24.30's limiter and Stage 25.8's `tenant_limits`) admin screens. Both live-verified via curl + real-browser Playwright against a real second tenant, with all test mutations reverted afterward. Full detail: `project_ledger.md` §43.
- **Stage 26.3.1 — GRN Workbench screen built (2026-07-24, code)** — continued into Phase 26.3 (the next phase with buildable items, per §43's own "Next" note) after finding a concurrent session mid-edit on 26.1.4's exact files; picked an unclaimed item instead of risking a collision. New "Goods Receipt (GRN)" screen (`renderGRNWorkbenchView`, `public/app.js`, Buying sidebar) — GRN's first UI ever (it's a Transaction doctype, never reachable from the Setup submenu the generic doctype form needs). PO-linked line pre-fill + manual add, line shape matching `engines/transactional_validation.go`'s pre-existing `grnReceivedLine` exactly so it inherits the full GOODSR/PURCHA validation set already built in Stage 25 Batch 3, plus a best-effort barcode-preview lookup — no new backend surface beyond the existing `POST /api/v1/doc/GRN`. Two real bugs found and fixed during live verification: (1) the GRN inventory-posting hook (`handlers_core_doc_engine.go`) ran on every save including edits, which would silently double-post stock on any future re-save — gated to create-only (`id == ""`), same convention as the pre-existing Item/PIM-profile check just above it; (2) a race between `loadGRNItemsFromPO`'s two trigger paths (PO field `change` event + explicit button) that could duplicate lines — fixed with a reentrancy guard. Live-verified end-to-end on a throwaway port (8177) + Playwright against real seeded fixtures (Vendor/Item/PurchaseOrder, PO approved by a second real identity to satisfy maker-checker): PO-load, post-to-stock, idempotent re-save, and both client- and server-side validation error surfacing all confirmed; all fixtures removed after. `go build`/`go vet`/`node --check` clean. Full detail: `project_ledger.md` §44.
- **Stage 26.12 — OMS Maturity Sprint foundation items (2026-07-24, code, UNCOMMITTED as of this note)** — started building the effort-planned Stage 26.12 (§38 above), beginning with its own recommended order's two dependency-free items: **26.12.9** (`AllocationRule`/`ReasonCode`/`StatusTransitionRule` config masters, zero frontend code needed — the generic Setup-submenu/dynamic-form machinery already renders any Master doctype) and **26.12.6** (added the ATP formula's missing Blocked/QC-Hold/Damaged/Channel-Buffer buckets to `inventory_availability`, extracting a shared `computeATS` helper so `GetAvailableToSell`/`CreateReservation`/`FindBestFulfillmentNode` — three previously-independent copies of the same formula — can't drift out of sync; closes a real over-admission risk as a side effect, not just dedup). `go build`/`go vet`/`go test ./... -p 1` clean; live-verified over real HTTP (throwaway port 8199) against the dev DB, test data cleaned up after. Full detail: `project_ledger.md` §45. **Next**: `26.12.1` (Order Engine) needs its own `[needs design decision]` (new `SalesOrder`/`SalesOrderLine` pair vs. extending `POSCart`) resolved before it can start — everything else in the sprint builds on that decision.
- **Phase 26.3 closed out — 26.3.2, 26.3.3, 26.3.5, 26.3.6 (2026-07-24, code, UNCOMMITTED as of this note)** — with §44 (26.3.1 GRN) already done, picked up the rest of the phase. **26.3.2** Purchase Requisition: sidebar entry into the existing generic doctype-table view (no bespoke screen needed, flat schema) plus Submit-for-Approval/Convert-to-RFQ-or-PO row actions calling pre-existing, previously-uncalled Stage 17.7 endpoints. **26.3.3** Approval Rules admin screen: Stage 24.8's list/create/edit API already existed; added the missing delete path (`engines.DeleteApprovalRule` + a new `DELETE` case) and a real form-modal UI (role `<select>`, not free text). **26.3.5** Vendor invoice override: confirmed the Stage 24.11 override endpoint was real but had no UI action; added an "Override & Pay" button for MismatchHold rows — verified via direct in-browser function-output inspection rather than a full 4-doctype fixture chain, since none existed in the dev DB this pass. **26.3.6** PO amendment: confirmed a real edit path already existed server-side (`ResetToPendingOnEdit`, from an earlier go-live QA pass) — the gap was the bespoke Purchase Orders screen having zero edit action for any status; added an "Amend" button using that screen's own simple field set rather than exposing the generic form's raw JSON `items` field. No backend changes for 26.3.2/26.3.5/26.3.6; 26.3.3 added one small Go delete function + handler case. `go build`/`go vet`/`go test ./... -p 1` green (incl. the concurrent Stage 26.12 work above). Full detail: `project_ledger.md` §46. **Phase 26.3 is now fully closed.** **Next**: Phase 26.5 (WMS Enterprise Maturity Sprint) is the next phase with buildable, unblocked items.
- **Stage 26.12.1 — Order Engine (2026-07-24, code)** — the user resolved the design decision left open at the end of the bullet above: a new `SalesOrder`/`SalesOrderLine` doctype pair, not extending `POSCart`. New `engines/orders.go`: `CreateSalesOrder` runs a fixed SKU-mapping → address → payment validate chain (a failing check places the order **On Hold** with a routable `hold_reason` rather than rejecting outright, mirroring `FindBestFulfillmentNode`'s own OMNI-0247 "never hard-reject" precedent one layer earlier), a Hold engine (`PlaceOrderHold`/`ReleaseOrderHold` — release re-runs the *same* validate chain rather than a bespoke resume path), and a stage-gated cancellation matrix (`CancelOrder`, checking the new `StatusTransitionRule` master first and falling back to a hardcoded Shipped/Delivered/Closed/Cancelled blocklist) requiring a mandatory, category-matched `ReasonCode`. New endpoints `POST /api/v1/orders`/`.../{id}/hold`/`.../{id}/release-hold`/`.../{id}/cancel`, `moduleGate("oms",...)`. Deliberately does not rewire existing channel-order webhooks (still create `POSCart` as before) or add a frontend screen yet — both explicit, documented follow-ups, not oversights. **Real gap found+fixed live**: the release-hold endpoint originally always said `"released"` even when the order stayed On Hold; fixed with a new `engines.GetOrderStatus` so the response reflects what actually happened. `go build`/`go vet`/`go test ./... -p 1` clean (new `OrderEngine` subtest); live-verified over real HTTP (throwaway port 8213) including a full create→hold→release→cancel lifecycle and `moduleGate` enforcement. Full detail: `project_ledger.md` §47.
- **Stage 26.12.2 — Allocation/Sourcing Engine (2026-07-25, code)** — new `engines.ResolveAllocationPlan` (`engines/sourcing.go`) runs the tenant's configured `AllocationRule` rows (26.12.9) in priority order, trying five strategies beyond the pre-existing Highest-ATS default: Nearest Pincode (needed a new additive `Location.pincode` field, `db/migrations_stage26_12_2_allocation_sourcing.sql`), Lowest Workload (open `FulfillmentTask` count), Oldest Stock (`inventory_availability.updated_at` as a documented proxy), Split Shipment (the only strategy allowed to spread lines across multiple locations), and Manual (always routes to a human, short-circuits the search). Falls back to the exact pre-26.12.2 behavior when no rules are configured; once rules exist, exhausting all of them without a plan holds the order with a new `HoldAllocationFailed`/`ALLOCATION_FAILED` code instead of a new "exception queue" table — the On-Hold `SalesOrder` rows filtered by `hold_reason` already are that queue. `CreateSalesOrder`/`ReleaseOrderHold` reworked to reserve per line (`plan.LineLocations[i]`) instead of one shared location per order, since the schema already supported this. Reallocation-on-reject (`fulfillment.go`) deliberately left untouched — its existing single-transaction boundary already gives it the rollback-on-failure property the design note asks for. `go build`/`go vet`/`go test ./... -p 1` clean (new `AllocationSourcing` subtest covering all 5 non-default strategies + the exception path); live-verified over real HTTP (throwaway port 8237) — a real order routed to the nearer of two seeded locations by pincode, and a second real order on a Manual-ruled channel went On Hold with `ALLOCATION_FAILED` despite being fully in stock. All fixtures cleaned up after. Full detail: `project_ledger.md` §48.
- **Stage 26.12.3 — Fulfillment Pick/Pack (2026-07-25, code, UNCOMMITTED as of this note)** — new `engines/fulfillment_pickpack.go` (a separate file from `fulfillment.go`'s existing `CreateFulfillmentTasks`/`TransitionTaskStatus`, both untouched) layers scan-validated Pick and Pack stages on top of `FulfillmentTask.items` via three new additive per-line JSON fields (`picked_qty`/`packed_qty`/`short_qty`). `ScanPickItem`/`ScanPackItem` resolve a scanned barcode (falling back to SKU/code, same convention as the 26.3.1 GRN barcode preview) against the first still-short task line for that SKU, naming the actual product on a mismatch rather than a bare "invalid scan"; the hard rule that packed qty can never exceed picked qty falls directly out of `ScanPackItem`'s own match condition rather than a separate check. `ShortPickLine` marks a line's whole remaining shortfall short in one action, gated on a mandatory `ReasonCode` (reused the exact 26.12.1 Hold/Cancel convention; needed one small migration adding a "Short Pick" category value, `db/migrations_stage26_12_3_pick_pack.sql`). `CompletePackTask` is the explicit, blockable completion action — hard-blocks unless every line both fully resolved its pick and packed exactly what was picked. Deliberately cross-checked `engines/wms.go` first per the design note and kept it separate rather than merged: `GenerateBinPickList` answers "which bin to walk to," this answers "did the picker/packer get the right units" — orthogonal concerns. Deliberately skipped auto-generating an invoice on pack completion (no Order→Shipment→Invoice wiring exists yet to generate one from). New endpoints (`internal/server/handlers_wms.go`, `routes.go`): `POST /api/v1/wms/pick-scan`/`pack-scan`/`short-pick`/`pack-complete`, `moduleGate("wms",...)`. `go build`/`go vet`/`go test ./... -p 1` clean (new `PickPack` subtest, 14 assertions across one fixture); live-verified over real HTTP (throwaway port 8241) — full pick→short-pick→pack→complete lifecycle through the real endpoints, final document state confirmed exactly (`picked_qty=2`/`short_qty=1`/`packed_qty=2`/`status=Packed`). All fixtures cleaned up after. Full detail: `project_ledger.md` §49. **Next**: per the sprint's recommended order, `26.12.5` (Returns/RTO/QC/Refund) — `26.12.4` (Courier/Shipment/Manifest) after that.
- **Stage 26.12.4 — Courier/Shipment/Manifest (2026-07-25, code, committed)** — built out of numbering order (ahead of 26.12.5) in a concurrent-session pass alongside the 26.12.3 bullet above, since it lives in a disjoint file (`engines/marketplace.go`) with no real dependency on 26.12.5. Extended `CreateLogisticsBooking` from a bare carrier/tracking/charge record (hardcoded `status="Shipped"` at creation) into a real Shipment engine: a new `CourierServiceArea` master backs a serviceability check (`CheckCourierServiceability`, pincode-prefix match + priority - a blank carrier auto-selects, an explicit one is validated unless nothing's configured yet); AWB assignment is a locally-generated placeholder (real courier API deliberately out of scope, same credentials-gated-later pattern as 26.2.1-26.2.5); a new `Manifest` doctype groups AWB-assigned bookings by courier+location via each booking's linked `FulfillmentTask`; `HandoverManifest` is the design note's split-shipment-aware closure cascade - booking→Handed Over, task→Dispatched (reusing `TransitionTaskStatus`, not duplicating its stock logic), parent `SalesOrder`→Shipped only once every one of its `FulfillmentTask`s is Dispatched, else Partially Fulfilled. `RecordDeliveryEvent`/`RecordRTO` add tracking sync and RTO capture (stock/refund handling stays 26.12.5's job); `GenerateShippingLabel` returns plain text. Wired `WMSLOG-0138`/`WMSLOG-0139`, reserved in the error catalog since Stage 25 with nothing to attach them to until now. New endpoints under the pre-existing `moduleGate("oms", featureGate("oms_integration",...))` pair. Frontend extends the existing Stage 13.7 Marketplace screen (new Manifests panel, extra Logistics Bookings columns/actions) rather than a new screen. `go build`/`go vet`/`go test ./... -p 1` clean (new `ShipmentManifestEngine` subtest: serviceability ordering/fallback, auto-select vs. rejected-carrier, a real two-location split-shipment order confirmed Partially Fulfilled then Shipped across two handovers, refused re-handover, tracking/RTO transition guards, label content). Live-verified over real HTTP (throwaway port 8253) and a real-browser Playwright pass against the actual Marketplace screen - booked a shipment through the live form, viewed its label, generated and handed over a manifest via the real confirm dialog, and independently confirmed over HTTP that a manually-linked `SalesOrder`+`FulfillmentTask` correctly cascaded to `Shipped` with stock deducted. All fixtures and scratch tooling removed after. **Two files needed a hand-built partial-hunk commit** (`internal/server/routes.go`, `engines/engines_test.go`) since a concurrent session was mid-edit on the same files at commit time - verified the exact staged content compiled/tested clean in isolation via `git stash push --keep-index` before committing. Full detail: `project_ledger.md` §50. **Next**: `26.12.5` (Returns/RTO/QC/Refund) remains open; `26.12.7`/`26.12.8` (dashboard/reports) are buildable after that.
- **Stage 26.5 — WMS Enterprise Maturity Sprint, all 10 items built and closed (2026-07-25, code, UNCOMMITTED as of this note)** — built concurrently with the Stage 26.12 (OMS) session's own §49/§50 work in the same tree. **26.5.1 ASN**: found and fixed a real pre-existing collision — `ASN` already existed as a bare, never-wired stub doctype since the original `db/migration.sql` (fields `asn_number`/`po_number`/`status`/`location`), so this extends it additively (`po_id`/`vendor`/`carrier`/`tracking_number`/`expected_date`/`expected_items`) rather than building a parallel doctype, per this repo's own reuse rule — the live dev DB needed a one-time field-row cleanup after the first migration pass collided with it, folded into the migration file so a fresh DB only sees the corrected shape. **26.5.2 QC sampling**: the real gap was that GRN's existing accepted/rejected split (Stage 25 Batch 3) was validated but never *acted on* at posting time — `PostGRNReceiptWithQC` now routes rejected/damaged qty into the `qc_hold`/`damaged` ATP buckets Stage 26.12's foundation added but nothing wrote to. **26.5.3-26.5.6, 26.5.8-26.5.10**: cross-dock staging, LPN/carton/pallet grouping, bin-to-bin replenishment, wave/batch picking (both design notes from the retired WMS prototype implemented exactly as specified), cartonization suggestion, an ABC-tiered cycle-count planner, and a blind-recount + mandatory-variance-reason-code workflow, all new engine files plus small additive edits to `wms.go`/`transactional_validation.go`/`handlers_core_doc_engine.go`. **26.5.7 short-pick**: already fully covered by the concurrent session's `ShortPickLine` (26.12.3) — only added wave-level shortfall visibility on top. New endpoints in their own `handlers_wms_enterprise.go` file specifically to avoid colliding with the concurrent session's in-flight edits to `handlers_wms.go`. Frontend: 4 new sidebar screens (ASN, LPN, Bin Replenishment, Wave Picking) plus new panels/buttons on the existing Putaway, GRN Workbench, Transfers, and Cycle Count screens. `go build`/`go vet` clean; new `engines.TestWMSEnterprise` (8 subtests, real Postgres) all pass; full pre-existing `go test ./... -p 1` suite stays green. Live-verified: real HTTP calls as `manager1`/Store Manager against every new endpoint, plus a real-browser Playwright pass through all 8 new/changed screens confirming zero console errors — the ASN doctype collision above was caught exactly this way, not by the unit tests. All scratch/smoke-test fixtures removed from the shared dev DB after. Full detail: `project_ledger.md` §51.
- **Stage 26.12.5/26.12.7/26.12.8/26.12.10 — Returns/RTO/QC/Refund, exception/reconciliation dashboard, OMS reports, notification dispatch (2026-07-25, code)** — closes out all 10 Stage 26.12 items, built concurrently with the Stage 26.5/26.12.4 sessions in the same tree (new files only, plus additive-only edits to shared ones, to avoid collision). **26.12.5**: new `engines/returns.go`, a request/approval-gated `ReturnRequest`/`RefundRequest` workflow distinct from the pre-existing instant `ProcessReturnAnywhere` (POS in-store path, untouched) — `Requested→Approved/Rejected→Received→QC Complete/Closed`, both a Customer Return and an RTO (requires the linked `LogisticsBooking` already `RecordRTO`'d) share it. QC assigns one of 6 dispositions per line, driving which 26.12.6 inventory bucket it lands in and whether it's refund-eligible (Sellable/Damaged/Repairable refund in full; Missing/Wrong-Item/Rejected refund nothing) — refund is always priced from the *original* order line, never a return-time input. The revenue-side GL post is gated behind a separate `RefundRequest` approval, distinct from the inventory-side post which happens at QC. **26.12.7/26.12.8**: 9 new `ReportDefinition` catalog entries (`engines/oms_reports.go`) — no new dashboard mechanism or routes, all reachable via the existing report-catalog framework. **26.12.10**: its P2 scope decision (real generic webhook dispatch vs. log-only vs. skip) was put to the user rather than guessed at; new `engines/notifications.go` fire-and-forget POSTs a rendered template to a tenant-configured webhook URL, mirroring `alerting.go`'s own pattern, wired into the order and return/refund lifecycles. **A second independent git-hygiene "files briefly vanished" incident was observed and self-resolved mid-build** — see the note in the bullet above; re-confirmed via `go build`/`go vet`/`git diff` before proceeding, nothing lost. `go build`/`go vet`/`go test ./... -p 1` clean (new `ReturnsRTOQCRefund` subtest); live-verified over real HTTP (throwaway port 8253) including a real local webhook receiver actually receiving a correctly-rendered notification POST. All fixtures and scratch tooling removed after. Full detail: `project_ledger.md` §52.
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
- **Extension-hooks gaps + GitHub/legal docs** (2026-07-23, commits `66170e6`/`7c64343`/`2184793`) — closed the 3 buildable extension-hooks gaps (HTTPS enforcement on `target_url`, `extensions_test.go` coverage, admin UI); restructured the 3 tracked docs into short bullets; added a GitHub org-security checklist + developer contract template. Tracked in `docs/extension_hooks_checklist.md`/`docs/github_checklist.md`/`docs/Contract/`, outside the Stage numbering.
- **Stage 26 planning** (2026-07-23, no code) — reconciled `ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf` against live repo state; corrected two stale PDF claims (industry-pack gap count, finance-regression root cause); broke the buildable work into `micro_checklist.md` Stage 26 (`26.0`-`26.11`, ~85 items) mirroring the PDF's own 12-phase roadmap. Rewrote `docs/specs/erp_maturity_master_plan.md` in place for the new PDF. Ref: `project_ledger.md` §34.
- **Standalone WMS project retired** (2026-07-23, no code) — see bullet above (§6 top). Ref: `project_ledger.md` §35.
- **Standalone OMS project retired, knowledge migrated** (2026-07-23, no code) — read both source PDFs and the standalone project in full; found this repo already has real MVP-thin building blocks (`engines/sourcing.go`/`inventory.go`/`fulfillment.go`/`marketplace.go`/`unicommerce.go`) covering a meaningful slice of OMS scope, but no Order-Management backlog phase at all. New `docs/specs/oms_master_blueprint_reference.md`; new `micro_checklist.md` Stage 26.12 (10 items). Standalone folder deleted per user request. Ref: `project_ledger.md` §36.
- **Stage 26.4 — PIM/PXM Maturity Sprint** (2026-07-23, code) — built and closed all 9 buildable items: `ProductAttributeGroup` + locale/channel value overrides (`engines.ResolveAttributeValue`), duplicate-item/content detection, audit-log-backed taxonomy history (no new table), real media version numbers + alt text + expiry + stdlib-only thumbnail rendition, content owner/SLA fields + existing approval-log surfaced for rejection comments, bulk approve/reject + a `product_content_versions` snapshot/rollback mechanism, `ChannelValidationRule` packs + a publish diff-preview against a new `payload_snapshot` column, a `classifyConnectorError` marketplace error dictionary, and a search-feed CSV export. Live-verified via direct API calls plus a real Playwright/Chromium pass through the Workbench UI - found and fixed a real pre-existing bug in the process (CSP `img-src` missing `blob:`, silently breaking every PIM media thumbnail in a real browser since Stage 15.2). Ref: `project_ledger.md` §37.
- **Stage 26.3.4 — WMS Operations Screens** (2026-07-24, code) — Putaway, Bin Conditions, Cycle Count screens + a Fulfillment "View Pick List" modal, pure frontend on top of already-working Stage 20 `engines/wms.go` backend that had zero UI. Found+fixed one gap: `CycleCountLine` had no reachable Bulk Import entry point (Setup submenu is Master-doctype-only) - fixed with a direct link into the generic doctype-table view. Live-verified end-to-end (scratch token + Playwright + seeded fixtures, all cleaned up after). Ref: `project_ledger.md` §39.
- **Stage 26.12 effort plan** (2026-07-24, no code) — sized all 10 Stage 26.12 items (still all `[ ]`, none built) in "sessions"; total ≈ 7.5-9.5 sessions, ~1-1.5 weeks at this repo's demonstrated cadence. Effort tags + a dependency-aware build order (26.12.9+26.12.6 → 26.12.1 → 26.12.2 → 26.12.3 → 26.12.5 → 26.12.4 → 26.12.7+26.12.8) written onto `micro_checklist.md`'s Stage 26.12 items directly. Ref: `project_ledger.md` §38.
- **Stage 26.12.9 + 26.12.6 + 26.12.1 + 26.12.2 + 26.12.3 + 26.12.4** (2026-07-24/25, code) — OMS Maturity Sprint build proper: config masters, the ATP bucket formula, the Order Engine (`SalesOrder`/`SalesOrderLine`, Hold engine, stage-gated cancellation), the Allocation/Sourcing Engine (5 configurable strategies over the `AllocationRule` master + an allocation-exception hold path), Fulfillment Pick/Pack (scan-validated Pick/Pack stages + short-pick reason capture on `FulfillmentTask`, kept deliberately separate from `engines/wms.go`'s bin-level pick lists), then Courier/Shipment/Manifest (serviceability-driven AWB assignment, manifest grouping, the split-shipment-aware handover cascade that flips a `SalesOrder` to Shipped/Partially Fulfilled, tracking sync, RTO). Ref: `project_ledger.md` §45-§50.

---

## Current State (check before trusting anything above)

- **Git**: this section's commit hash goes stale fast in a tree with this much concurrent-session activity — trust §6's "Latest commit" bullet and `git log --oneline -10` over any specific hash written here. As of this note, this session's own four commits (Phase 26.3 close-out, Stage 26.12 foundation/Order-Engine/Allocation-Sourcing, the `OrderEngine`/`AllocationSourcing` test-coverage catch-up, and 26.12.4 Courier/Shipment/Manifest) are in; a concurrent session's 26.12.3 and apparent Stage 26.5 work is not yet committed. Run `git status --porcelain` to confirm current state before trusting anything else in this file.
- **Unrelated in-flight work**: `docs/Contract/`, `docs/extension_hooks_checklist.md`, `docs/github_checklist.md`, `TEMP_menu_naming_comparison.md` — a separate session's legal/security planning docs, nothing to do with the Stage tracker. Don't sweep these into an unrelated commit.
- **Local binaries are stale**: `custom_erp.exe`/`erp-server.exe` were last built 2026-07-20, before every commit from Stage 23 follow-up onward. No server is currently running on port 8080. Rebuild before assuming the live app reflects any recent work.
- **Concurrent sessions are common in this repo** — this tree has repeatedly seen 2-3 sessions (and the user) editing it in the same window. Before any edit or commit:
  1. Run `git status`/`git diff` first — never assume the tree is clean.
  2. Never `git add -A` — stage only the files you actually reviewed.
  3. If a build fails transiently mid-edit, it may be another session's in-flight change — wait and retry before debugging.
  - Past collisions (for pattern recognition, not action needed): a Stage-numbering clash (20 vs 21) caught via `git status` and renumbered before shipping; three sessions editing the same files in one window on 2026-07-20; two sessions independently picking the identical Stage 25 batch on 2026-07-23 and converging on the same fix.
- **Known issues**: `docs/operations/hardening_roadmap.md` (security/correctness backlog) fully closed as of 2026-07-12, historical record only. `docs/specs/pdf_blueprint_gap_analysis.md` is a superseded 2026-07-12 snapshot. Active backlog lives in `docs/micro_checklist.md`.
- **Test suite**: fully green as of Stage 26.0.2 (2026-07-24) — the two long-standing failures this bullet used to document (`FinanceDoubleEntryAndPOS`'s stale trial-balance assertion, `DocTypeValidationAndAuth`'s shared-DB `Brand.fefo_enabled` debris) were both root-caused and fixed, not worked around. Re-confirmed clean again 2026-07-25 after the Stage 26.12.2 Allocation/Sourcing work (`go test ./... -p 1`, whole suite, incl. a new `AllocationSourcing` subtest).
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
