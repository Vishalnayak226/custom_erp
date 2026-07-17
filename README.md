# In-House Enterprise ERP System

A metadata-driven, pluggable, ledger-backed Enterprise Resource Planning (ERP) system serving retail checkout, warehouses, and e-commerce. Backend is a single Go binary; database is PostgreSQL with schema-per-tenant isolation; frontend is a vanilla JS SPA served as static files.

## Project Structure

```
├── .github/workflows/ci.yml       # CI: build, vet, test against a fresh Postgres on every push/PR
├── main.go                        # HTTP router, middleware, and all REST handlers
├── main_test.go                   # HTTP-level integration test (real handlers via httptest)
├── go.mod / go.sum                # Go module definition
├── engines/                       # Business logic: finance, inventory, doctype, auth, saas, optimization, etc.
├── db/
│   ├── db.go                      # Connection pool + tenant schema resolution
│   ├── migration.sql              # Base schema, seed data, Chart of Accounts
│   └── migrations_phase3.sql      # Phase 2/3 transactional metadata
├── public/                        # Static frontend (index.html, app.js, styles.css, profiles/)
├── docs/
│   ├── hardening_roadmap.md       # Closed roadmap (2026-07-12): security, correctness, release-hygiene backlog, all phases done
│   ├── pdf_blueprint_gap_analysis.md # Gap analysis snapshot (2026-07-12) vs the original spec PDFs — mostly closed since, see micro_checklist.md
│   ├── implementation_plan.md     # Unified Technical Specification Document (logic & constraints)
│   ├── framework_architecture.md  # Metadata-driven pluggable DocType Kernel specification
│   ├── pos_architecture.md        # Pluggable offline POS terminal specification (basic POS screen built, offline-first parts still forward-looking)
│   ├── modules_overview.md        # Functional Modules Directory (several sections now built, see status banner)
│   ├── industry_plugs.md          # Multi-industry configurator specification (4 of the listed industries are built)
│   ├── micro_checklist.md         # Stage 1-13 build tracker (current source of truth for what's built)
│   ├── architecture_evaluation.md # SaaS multi-tenant scaling & Go runtime evaluation
│   ├── project_ledger.md          # Chronological build history and architectural decisions
│   └── ai_handover.md             # Environment setup, run commands, and dev handover notes
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
go build -o erp-server.exe
./erp-server.exe
```
This serves both the API and the `public/` static frontend on `http://localhost:8080`. You'll land on a login screen — dev credentials are in `DEV_CREDENTIALS.local.txt` at the project root (gitignored; regenerate via a throwaway bcrypt script and update `db/migration.sql` + the live `users` table if it's missing).

### Frontend build (optional)
`npm run build` bundles and minifies `public/app.js` via esbuild into `public/dist/`. Not required to run the app — `public/app.js` is loaded directly by `index.html`.

## Technical Reference & Architecture

*   **Current priorities**: Use **[docs/micro_checklist.md](docs/micro_checklist.md)** (Stage 13) as the live backlog — it's kept current after every closed item. `docs/hardening_roadmap.md` (security/correctness/release-hygiene) is fully closed as of 2026-07-12.
*   **Is this a complete ERP yet?**: Short answer: much closer than before. The kernel and omnichannel/scale backend remain strong; POS, Finance/GL, GST calc, CRM/Loyalty (MVP), HR, Fixed Assets, Expense Management, Manufacturing (MVP), RFQ/vendor quotes, sticker printing, MFA, and the approval/maker-checker workflow engine are now built (Stage 13.1-13.15, see `docs/micro_checklist.md` for exact scope of each). **[docs/pdf_blueprint_gap_analysis.md](docs/pdf_blueprint_gap_analysis.md)** is the original comparison against the 6 spec PDFs, dated 2026-07-12 — treat it as a historical snapshot of what was missing *then*, not current state.
*   **System Customizations**: Read **[docs/framework_architecture.md](docs/framework_architecture.md)** to understand how the dynamic DocType metadata schemas and UI form interpreters are structured.
*   **Database & Accounting**: Read **[docs/implementation_plan.md](docs/implementation_plan.md)** for double-entry GL mappings, validation matrices, and API specifications.
*   **Task Tracking**: Use **[docs/micro_checklist.md](docs/micro_checklist.md)** to mark, revise, and verify implemented stages.
*   **Build history**: Read **[docs/project_ledger.md](docs/project_ledger.md)** for chronological architectural decisions, and **[docs/ai_handover.md](docs/ai_handover.md)** for environment/run details.

Note: `docs/pos_architecture.md`, `docs/modules_overview.md`, and `docs/industry_plugs.md` mix built and forward-looking specification — each carries a status banner explaining exactly which parts are real code today.
