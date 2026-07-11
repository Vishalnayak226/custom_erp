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
*   [engines/optimization.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/optimization.go): Demand forecasting, replenishment suggestions, and task SLA checks.
*   [engines/outbox.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/outbox.go): Real-time event-driven outbox delta sync poller.
*   [engines/engines_test.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/engines_test.go): Comprehensive integration tests suite.
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
To build the compiled server executable `erp-server.exe`:
```powershell
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -o erp-server.exe
```

### C. Start PostgreSQL (Foreground Backgrounded)
To start the database server. Note that starting `postgres.exe` directly as a long-running foreground task is required in sandboxed shells (as `pg_ctl start` spawned detached services are subject to process group termination upon session end):
```powershell
& "$env:USERPROFILE\pg-portable\pgsql\bin\postgres.exe" -D "$env:USERPROFILE\pg-data" -p 5435
```

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
    - **Phases 1-7** (Core Foundation, Single Vertical Pilot, Omnichannel Sync, Store Fulfillment, Concurrency Scale Testing, Marketplace Expansion, and Advanced Optimization Engines) are 100% completed, integrated, verified, and pushed to GitHub.
    - **SaaS Provisioning & Feature Flags**: Built schemas cloning structures, seeding default metadata, and setting flag boundaries per tenant.
    - **Error Logs Hub & Outbox Retries**: Built API endpoints for log querying, failed integration event resets, and webhook signature verification checks.
    - **Premium UI Overhaul**: Created a high-fidelity Stripe/Linear-style dashboard with KPI tickers, soft glowing focus borders, and centered glassmorphism dialog modals.
*   **Dev Server State**: Live background process running on port `8080` (Go backend) and frontend served on port `8080`.
*   **Verification Status**: All UAT tests pass cleanly (`ok custom_erp/engines 2.776s`).

---

## 7. Handover Notes for Incoming AI (Claude / Codex / Gemini)

Welcome! The project is fully complete and operational. To verify the build or resume development, follow these steps:
1.  **Repository Setup**: Pull latest code from `main` branch.
2.  **Run Database**: Ensure PostgreSQL is running on port `5435`.
3.  **Run Tests**: Execute `go test ./...` to verify all business rules.
4.  **Run Server**: Launch server `./erp-server.exe` or `go run main.go`.
5.  **Build Assets**: Execute `npm run build` to package frontend minified scripts.
6.  **Access App**: Navigate to `http://localhost:8080` in your browser. All native alerts/confirms have been replaced with a custom-styled Promise-based modal layout.
7.  **Handover Ledger**: Reference `docs/project_ledger.md` for historical build logs and chronological records of what was built.

