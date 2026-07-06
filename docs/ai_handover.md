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
*   [db/migration.sql](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/db/migration.sql): Database tables and default prefix configs schema.
*   [engines/numbering.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/numbering.go): Row-level locked running sequence numbering generator.
*   [engines/labels.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/labels.go): Translation CRUD mappings API.
*   [engines/logs.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/logs.go): Audit trail logger and panic error recovery database recorder.
*   [engines/engines_test.go](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/engines/engines_test.go): Unit tests covering dynamic labels and numbering logic.
*   [index.html](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/public/index.html): ERP UI layout, configuration modals, and Log Hub views.
*   [app.js](file:///c:/Users/ABCD/Documents/Antigravity%20Projects/ERP/public/app.js): Application router, UI rendering logic, and dynamic DOM TreeWalker translator.

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
When building subsequent modules (e.g. Stage 2: Master Definitions, Stage 4: Procurement):
1.  **Always Resolve Tenant**: Inspect `Resolved-Tenant-ID` header inside Go API handlers.
2.  **Scope Queries using Local Search Path**: In your database transaction, always run `db.SetSearchPath(tx, schema)` before issuing any queries. Never write hardcoded table queries without schema parameters unless scoped.
3.  **Audit Logs**: Log all document transitions, submissions, and approvals using `engines.LogAuditEvent()`.
4.  **Sequence Code Generation**: Always request document numbering codes via `engines.GenerateSequence()`.
