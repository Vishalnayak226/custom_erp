# In-House Enterprise ERP System

A metadata-driven, pluggable, ledger-backed Enterprise Resource Planning (ERP) system serving retail checkout, warehouses, and e-commerce. Backend is a single Go binary; database is PostgreSQL with schema-per-tenant isolation; frontend is a vanilla JS SPA served as static files.

## Project Structure

```
├── .github/workflows/ci.yml       # CI: build, vet, test against a fresh Postgres on every push/PR
├── cmd/
│   ├── server/main.go             # Real entrypoint - launcher only, calls internal/server.Run()
│   └── reset_mfa/main.go          # Standalone utility: resets MFA for HR/Admin users
├── internal/server/               # HTTP router, middleware, and all REST handlers (was main.go pre-2026-07-19;
│   │                               # split into these files by domain during the Stage 19 folder restructuring)
│   ├── routes.go                  # Run() - DB init, background workers, full route table
│   ├── middleware.go              # Rate limiting, CORS, webhook signature verification, apiMiddleware chain
│   ├── handlers_auth.go           # Login, TOTP MFA
│   ├── handlers_core_doc_engine.go # Generic /api/v1/doc/:doctype engine, DocType Builder, industry switch
│   ├── handlers_pim_pos_finance.go # CSV/PIM import, channel credentials, POS checkout, Finance/GL, approvals, GST, reports
│   ├── handlers_procurement_pim2.go # RFQ, stickers, payroll export, PIM V2 (dashboard/bulk/media/publish)
│   ├── handlers_operations.go     # Assets, Expense, CRM/Loyalty, Manufacturing, fulfillment, transfers, vendor invoice, optimization
│   ├── handlers_integrations_admin.go # Unicommerce/Pine Labs/CleverTap, tenant provisioning, modules, extensions
│   ├── server_test.go             # HTTP-level integration test (real handlers via httptest)
│   ├── pim_dashboard_test.go / soft_delete_test.go # Additional integration tests
│   └── VERSION                    # App version, go:embed'd into the binary at build time
├── go.mod / go.sum                # Go module definition
├── engines/                       # Business logic: finance, inventory, doctype, auth, saas, optimization, etc.
├── db/
│   ├── db.go                      # Connection pool + tenant schema resolution
│   ├── migration.sql              # Base schema, seed data, Chart of Accounts
│   └── migrations_phase3.sql      # Phase 2/3 transactional metadata
├── public/                        # Static frontend (index.html, app.js, styles.css, profiles/)
├── scripts/                       # Operational scripts (connector live-verification, etc.) - see scripts/archive/ for retired one-offs
├── docs/
│   ├── ERP_BLUEPRINT.md           # Full project snapshot for an outside/AI reviewer - start here for "what is this system"
│   ├── micro_checklist.md         # Live backlog/build tracker - current source of truth for what's built
│   ├── project_ledger.md          # Chronological build history and architectural decisions
│   ├── ai_handover.md             # Environment setup, run commands, and dev handover notes
│   ├── README.md                  # Index of everything else in docs/
│   ├── architecture/              # Design docs: framework_architecture.md, architecture_evaluation.md, pos_architecture.md
│   ├── specs/                     # Spec-vs-built docs: implementation_plan.md, industry_plugs.md, modules_overview.md, pdf_blueprint_gap_analysis.md
│   ├── operations/                # backup_restore.md, incident_runbook.md, connector_live_verification.md, hardening_roadmap.md
│   ├── requirements/               # BRD.md, PRD.md
│   └── guides/                    # USER_GUIDE.md (client-facing), ADMIN_GUIDE.md (operator manual)
└── package.json                   # Frontend build script (esbuild bundling of public/app.js)
```

## Getting Started

### Prerequisites
- Go 1.22+
- PostgreSQL 16.x (a portable install works — see `docs/ai_handover.md` for the exact setup used in development, including port and credentials)

### Running Locally

**Easiest way (Windows/PowerShell):** `.\manage.ps1` — a single script to start/stop/restart Postgres + the Go server together, with a status check and log viewer. Run it with no argument for an interactive menu, or `.\manage.ps1 start` / `stop` / `restart` / `status` / `logs` / `release` directly. `release` rebuilds `erp-server.exe` stripped (`-ldflags="-s -w"`, ~30% smaller) for actual deployment — regular `start` uses the unstripped dev build.

**Manual way:**
```bash
# 1. Ensure PostgreSQL is running and reachable via DATABASE_URL
#    (defaults to postgres://postgres@localhost:5435/custom_erp?sslmode=disable if unset)

# 2. Apply the schema
psql -f db/migration.sql
psql -f db/migrations_phase3.sql

# 3. Build and run the server
go build -o erp-server.exe ./cmd/server
./erp-server.exe
```
This serves both the API and the `public/` static frontend on `http://localhost:8080`. You'll land on a login screen — dev credentials are in `DEV_CREDENTIALS.local.txt` at the project root (gitignored; regenerate via a throwaway bcrypt script and update `db/migration.sql` + the live `users` table if it's missing).

### Frontend build (optional)
`npm run build` bundles and minifies `public/app.js` via esbuild into `public/dist/`. Not required to run the app — `public/app.js` is loaded directly by `index.html`.

## Technical Reference & Architecture

*   **Start here**: **[docs/ERP_BLUEPRINT.md](docs/ERP_BLUEPRINT.md)** — a full project snapshot (scope, architecture, build history, known gaps) written for an outside reviewer with no other context. **[docs/README.md](docs/README.md)** indexes everything else in `docs/`.
*   **Current priorities**: **[docs/micro_checklist.md](docs/micro_checklist.md)** is the live backlog — kept current after every closed item.
*   **New here?**: **[docs/ai_handover.md](docs/ai_handover.md)** has environment setup, port map, and run commands. **[docs/guides/ADMIN_GUIDE.md](docs/guides/ADMIN_GUIDE.md)** is the fuller standalone operator manual. **[docs/guides/USER_GUIDE.md](docs/guides/USER_GUIDE.md)** is the client-facing, non-technical walkthrough.
*   **Business/product scope**: **[docs/requirements/BRD.md](docs/requirements/BRD.md)** (business goals/scope) and **[docs/requirements/PRD.md](docs/requirements/PRD.md)** (functional module spec, built-vs-planned status).
*   **System architecture**: **[docs/architecture/framework_architecture.md](docs/architecture/framework_architecture.md)** (metadata-driven DocType kernel) and **[docs/architecture/architecture_evaluation.md](docs/architecture/architecture_evaluation.md)** (stack/multi-tenancy rationale).
*   **Operations**: **[docs/operations/backup_restore.md](docs/operations/backup_restore.md)**, **[docs/operations/incident_runbook.md](docs/operations/incident_runbook.md)**, **[docs/operations/connector_live_verification.md](docs/operations/connector_live_verification.md)**.
*   **Build history**: **[docs/project_ledger.md](docs/project_ledger.md)** for chronological architectural decisions.

Note: several docs under `docs/specs/` (`implementation_plan.md`, `pos_architecture.md`, `modules_overview.md`, `industry_plugs.md`, `pdf_blueprint_gap_analysis.md`) mix built and forward-looking specification — each carries a status banner explaining exactly which parts are real code today; `docs/ERP_BLUEPRINT.md` §2 gives the current built-vs-spec picture in one table.
