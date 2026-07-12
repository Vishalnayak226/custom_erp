# AI Handover Note: Developer Guide

This document provides system setup maps, port directories, command recipes, and database configurations for the next AI agent or developer taking over code construction.

---

## 1. System Environment & Port Bindings

*   **Go Runtime**: Portable Go 1.22.5 is extracted under `$env:USERPROFILE\go-portable\go`.
*   **PostgreSQL Server**: Portable PostgreSQL 16.3 is extracted under `$env:USERPROFILE\pg-portable\pgsql`.
*   **PostgreSQL Port**: Runs on port **`5435`** (using trust authentication).
*   **PostgreSQL Cluster Dir**: Data directory is located at `$env:USERPROFILE\pg-data`.
*   **Database User & DB**: User is `postgres` (no password). Database name is `custom_erp`.
*   **Web Application Port**: Serves static pages and HTTP API router on port **`8080`** (`http://localhost:8080`).
*   **Login required**: as of 2026-07-12 the app requires signing in — see §6/§7. Dev credentials are in `DEV_CREDENTIALS.local.txt` at the project root (gitignored, not in this doc).
*   **Easiest way to start/stop everything**: `.\manage.ps1` (interactive menu) or `.\manage.ps1 start` / `stop` / `restart` / `status` / `logs` — see §3.C note on a known hang with this in sandboxed/background shell contexts.

---

## 2. Core Repository Map

*   [go.mod](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/go.mod): Go module configuration (`module custom_erp`).
*   [main.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/main.go): Web server API router, Dynamic Tenant resolution, and Panic Recovery Middleware.
*   [db/db.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/db/db.go): Connection pool initialization and dynamic tenant schema-switching handler.
*   [db/migration.sql](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/db/migration.sql): Database tables, Chart of Accounts, user mappings, and default configurations.
*   [engines/numbering.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/numbering.go): Sequence numbering generator.
*   [engines/labels.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/labels.go): Translation CRUD mappings API.
*   [engines/logs.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/logs.go): Audit trail logger and panic recovery recorder.
*   [engines/inventory.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/inventory.go): Barcoded stock counts, availability read models, and temporary reservations.
*   [engines/finance.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/finance.go): Balanced double-entry GL journal ledger postings.
*   [engines/sourcing.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/sourcing.go): Rule-driven order routing, mapping, and webhook idempotency checks.
*   [engines/fulfillment.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/fulfillment.go): Store picking tasks, re-routing on task reject, and Return Anywhere.
*   [engines/marketplace.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/marketplace.go): Marketplace settlements reconciliation and logistics bookings dispatch tracker.
*   [engines/optimization.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/optimization.go): Demand forecasting, replenishment suggestions, and task SLA checks. Has a known bug — see §6.
*   [engines/saas.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/saas.go): Tenant provisioning (schema cloning) and per-tenant feature flags.
*   [engines/outbox.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/outbox.go): Real-time event-driven outbox delta sync poller, plus integration log query/retry.
*   [engines/engines_test.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/engines_test.go): Unit-style tests calling engine functions directly (not through HTTP) - despite the name, these don't exercise `main.go`'s handlers/middleware at all.
*   [main_test.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/main_test.go): The project's actual HTTP-level integration test (added 2026-07-12) - drives real handlers via `httptest`, no real socket needed. Lives at the project root in package `main` because only `main` can call its own unexported handlers.
*   [index.html](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/public/index.html): ERP UI layout.
*   [app.js](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/public/app.js): Application UI routing and translation state engine.

---

## 3. Development Command Recipes

All commands should be executed from the repository root directory `c:\Users\ABCD\Documents\Antigravity Projects\ERP`:

### A. Run Unit Tests
To run unit tests verifying numbering and translation engines:
```powershell
& "$env:USERPROFILE\go-portable\go\bin\go.exe" test ./...
```

### B. Compile Backend Binaries
For local dev/testing (keeps debug symbols - easier to troubleshoot a panic's stack trace):
```powershell
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -o erp-server.exe
```
For a release build (strips symbols/debug info, ~30% smaller): `.\manage.ps1 release`, or directly:
```powershell
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -ldflags="-s -w" -o erp-server.exe
```
Windows locks a running `.exe`, so `manage.ps1 release` stops the server first if it's up (and does not restart it - run `start` yourself when ready).

### C. Start PostgreSQL (Foreground Backgrounded)
To start the database server. Note that starting `postgres.exe` directly as a long-running foreground task is required in sandboxed shells (as `pg_ctl start` spawned detached services are subject to process group termination upon session end):
```powershell
& "$env:USERPROFILE\pg-portable\pgsql\bin\postgres.exe" -D "$env:USERPROFILE\pg-data" -p 5435
```
**Known issue (found 2026-07-12):** `manage.ps1`'s `pg_ctl start ... -w` can hang indefinitely when that script itself is run as a backgrounded/sandboxed task (the underlying Postgres instance actually starts fine — confirmed via `pg_isready`/`psql` — it's specifically `pg_ctl -w`'s own readiness wait that doesn't return in that context). If `manage.ps1 start` appears stuck, check `pg_isready -p 5435` directly; if that reports ready, the DB is fine and you can proceed (e.g. start `erp-server.exe` directly) without waiting on the stuck script.

### D. Start Go ERP Web Server
To launch the compiled server binary:
```powershell
& "c:\Users\ABCD\Documents\Antigravity Projects\ERP\erp-server.exe"
```

---

## 4. Multi-Tenant Development Standards (Next Phases)
When building subsequent modules (e.g. Stage 3: Master Packages, Stage 5: GRN & Inventory):
1.  **Always Resolve Tenant**: Inspect `Resolved-Tenant-ID` header inside Go API handlers.
2.  **Scope Queries using Local Search Path**: In your database transaction, always run `db.SetSearchPath(tx, schema)` before issuing any queries. Never write hardcoded table queries without schema parameters unless scoped.
3.  **Audit Logs**: Log all document transitions, submissions, and approvals using backend triggers or explicit audit statements.
4.  **Sequence Code Generation**: Always request document numbering codes via `engines.GenerateSequence()` or construct variant concatenations using `engines.GenerateVariantCode()`.

---

## 5. Omnichannel Scale Architecture (Event-Driven & Outbox Patterns)

To support scaling from 1 store to 2,000 stores without transaction database lockouts:
1.  **Outbox Pattern for External Sync**: User-facing transactions must NEVER make synchronous HTTP calls to external channels (Shopify, payment gateways, GST APIs).
    *   Insert the record into the primary table and an event log into the `integration_event_outbox` table **within the same SQL transaction**.
    *   The background worker polls the outbox and publishes the event to the external integration gateway asynchronously.
2.  **Inventory Availability Calculations**: Do not query the raw transaction ledger directly for real-time channels. Expose calculated Available-to-Sell (ATS) stock using the read model:
    `Available to Sell = Available - Reserved - Safety Stock - Channel Holds`
3.  **Strict Idempotency keys**: Validate signature, timestamp, and unique UUID identifiers on all webhook payloads to prevent duplicate order or invoice creations.

---

## 6. Version Control & Handover Status

*   **Remote GitHub URL**: `https://github.com/Vishalnayak226/custom_erp.git` (Branch: `main`)
*   **Build Handover Milestone**:
    - **Phases 1-7** (Core Foundation, Single Vertical Pilot, Omnichannel Sync, Store Fulfillment, Concurrency Scale Testing, Marketplace Expansion, and Advanced Optimization Engines) are built and pushed to GitHub. Phase 7 has a known functional bug — see below.
    - **Real login flow (2026-07-12)**: closed `hardening_roadmap.md` Phase 1.1 — the app now requires signing in. Added a login screen, reset all 4 seed users to unique credentials (`DEV_CREDENTIALS.local.txt`, gitignored — dev-only, rotate before real use), and `apiMiddleware` now rejects any request without a valid token except `/api/v1/login` itself. Token expiry (Phase 1.4) is also closed — `engines/auth.go`'s `tokenTTL()` embeds an `exp` claim (default 24h, `JWT_EXPIRY_HOURS` overridable) and `ParseToken` validates it.
    - **SaaS Provisioning & Feature Flags**: Built schemas cloning structures, seeding default metadata, and setting flag boundaries per tenant. Provisioning now generates a unique, freshly-random bcrypt-hashed admin credential per tenant (`generateRandomPassword()` in `engines/saas.go`) rather than cloning a shared placeholder hash. Feature flags are still set-only — `IsFeatureEnabled` isn't checked by any route handler yet, so flags don't gate anything in practice (tracked in `docs/pdf_blueprint_gap_analysis.md` Phase A).
    - **Error Logs Hub & Outbox Retries**: Built API endpoints for log querying and failed integration event resets. The frontend Log Hub view does not yet call these endpoints (no integration-payload list or retry button in the UI). **Webhook signature verification was not actually implemented** — no signature check exists on the Shopify order webhook or any other callback handler.
    - **Premium UI Overhaul**: Created a high-fidelity Stripe/Linear-style dashboard with KPI tickers, soft glowing focus borders, and centered glassmorphism dialog modals. The custom dialog migration covers `alert`/`confirm`; several `prompt()` calls (DocType Builder, Prefix Config edit) are still raw browser prompts.
*   **Dev Server State**: Live background process running on port `8080` (Go backend) and frontend served on port `8080`.
*   **Verification Status**: `go build`, `go vet ./...`, and `go test ./...` are all fully clean as of 2026-07-12 — first time this session. The `DocTypeValidationAndAuth` failure that persisted through all of Phase 1 was `SwitchIndustryProfile` silently dropping `Brand.status` on industry switch; fixed in `hardening_roadmap.md` Phase 2.6 (merge instead of delete-then-insert) plus a one-time data restore on the live dev DB. The `CalculateSalesVelocity`/checkout status mismatch is also fixed (Phase 2.2), verified with a real checkout → real forecast call. Phase 1 and Phase 2 of `hardening_roadmap.md` are both fully closed.
*   **Known issues & active roadmap**: See **[docs/hardening_roadmap.md](hardening_roadmap.md)** for the full, prioritized list (security, correctness, test/CI, release hygiene) found by that audit and not yet fixed.
*   **Build-completeness vs the original spec PDFs (2026-07-12)**: See **[docs/pdf_blueprint_gap_analysis.md](pdf_blueprint_gap_analysis.md)** — the codebase was built against the Omnichannel Scale Add-on Blueprint's rollout plan (kernel + omnichannel backend, which is solid), not the Master Blueprint's full functional module list (POS/Finance/GST/CRM/HR/Assets UI, maker-checker approvals, MFA, security headers), most of which hasn't been started. Includes a phased plan for closing the gap.

---

## 7. Handover Notes for Incoming AI (Claude / Codex / Gemini)

Welcome! The core system is built and operational, but not yet production-hardened — read `docs/hardening_roadmap.md` before treating any "[x] completed" checklist item as a fully closed, secure implementation. To verify the build or resume development, follow these steps:
1.  **Repository Setup**: Pull latest code from `main` branch.
2.  **Run Database**: Ensure PostgreSQL is running on port `5435`.
3.  **Run Tests**: Execute `go test ./...` to verify all business rules.
4.  **Run Server**: Launch server `./erp-server.exe` or `go run main.go`.
5.  **Build Assets**: Execute `npm run build` to package frontend minified scripts.
6.  **Access App**: Navigate to `http://localhost:8080` in your browser. You'll land on a login screen — credentials are in `DEV_CREDENTIALS.local.txt` (gitignored, project root; regenerate via a throwaway `golang.org/x/crypto/bcrypt` script if missing, then update `db/migration.sql` and the live `tenant_default.users` table). All native alerts/confirms have been replaced with a custom-styled Promise-based modal layout (except a few `prompt()` calls not yet migrated — see `hardening_roadmap.md` "Smooth").
7.  **Handover Ledger**: Reference `docs/project_ledger.md` for historical build logs and chronological records of what was built.

