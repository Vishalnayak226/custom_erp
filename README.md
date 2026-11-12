---
doc_id: DOC-B335630551
title: In-House Enterprise ERP System
type: reference
status: draft
owner: engineering-owner
approvers: [documentation-maintainer, engineering-owner]
audience: [maintainers, engineering-owner]
applies_to: source documentation; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-11-10
review_by: 2027-02-08
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# In-House Enterprise ERP System

A metadata-driven, pluggable, ledger-backed Enterprise Resource Planning (ERP) system serving retail checkout, warehouses, and e-commerce. Backend is a single Go binary; database is PostgreSQL with schema-per-tenant isolation; frontend is a vanilla JS SPA served as static files.

## Project Structure

The rules for where things live and how they are named are in **[CONTRIBUTING.md](CONTRIBUTING.md)**, the project constitution. A test (`internal/repolayout`) fails the build when a file breaks them.

```
├── CONTRIBUTING.md        # The constitution: the rules every change to code and data follows
├── README.md              # This file
├── SECURITY.md            # How to report a vulnerability
├── cmd/                   # One main.go per program
│   ├── server/            #   the ERP server (launcher only - calls internal/server.Run())
│   └── <tool>/            #   command-line tools: doclint, gendocs, releasebudget, tenantctl, reset_mfa, ...
├── engines/               # Business logic in one Go package: finance, inventory, POS, PIM, CRM, approvals, ...
├── internal/
│   ├── server/            # HTTP routes, middleware and handlers; VERSION is embedded at build time
│   ├── kb/                # In-app help topics (embedded in the server)
│   ├── docgen/            # Generated-docs tooling
│   ├── securityscan/      # Attack-surface and security checks
│   ├── supplychain/       # Dependency and build-provenance checks
│   └── repolayout/        # Checks the folder and naming rules in CONTRIBUTING.md
├── db/                    # Connection pool, migration runner and every migration file
│   ├── migration.sql      #   base schema and seed data
│   └── migrations_*.sql   #   incremental migrations, applied in name order by `erp-server -migrate`
├── public/                # The browser app (HTML, CSS, ES-module JS), served as-is - no build step
├── docs/                  # All documentation - docs/README.md maps every folder
├── deploy/                # What runs on the production host: service unit, Caddy, backup/restore, deploy
├── release/               # The three release clicks (Release, Deploy, Cleanup) and their gates
├── scripts/               # Developer and operations helpers still in use
├── extension-sdk/         # The contract for outside extension developers
├── .github/               # CI, pull request template, CODEOWNERS
├── manage.ps1             # Start/stop Postgres + the server on a dev machine; `release` builds bin/erp-server.exe
├── promote.ps1            # Promote a commit between the local dev/test/live environments
├── environments.json      # The dev/test/live registry read by manage.ps1 and promote.ps1
└── go.mod / go.sum, Dockerfile, docker-compose.yml, railway.json
```

Local-only and git-ignored: `bin/` (builds), `.local/` (private notes and dev credentials), `logs/`, `backups/`, `media_store/`.

For which file belongs to which part of the system, see **[docs/brain/README.md](docs/brain/README.md)**.

## Getting Started

### Prerequisites
- Go 1.22+
- PostgreSQL 16.x (a portable install works — see `docs/ai_handover.md` for the exact setup used in development, including port and credentials)

### Running Locally

**Easiest way (Windows/PowerShell):** `.\manage.ps1` — a single script to start/stop/restart Postgres + the Go server together, with a status check and log viewer. Run it with no argument for an interactive menu, or `.\manage.ps1 start` / `stop` / `restart` / `status` / `logs` / `release` directly. `release` rebuilds `bin/erp-server.exe` stripped (`-ldflags="-s -w"`, ~30% smaller) for actual deployment — regular `start` uses the unstripped dev build.

**Manual way:**
```bash
# 1. Ensure PostgreSQL is running and reachable via DATABASE_URL
#    (defaults to postgres://postgres@localhost:5435/custom_erp?sslmode=disable if unset)

# 2. Apply the schema: db/migrations/migration.sql, then every db/migrations/migrations_*.sql in order
go run ./cmd/server -migrate

# 3. Build and run the server from the repository root (it serves ./public)
go build -o bin/erp-server.exe ./cmd/server
./bin/erp-server.exe
```
This serves both the API and the `public/` static frontend on `http://localhost:8080`. You'll land on a login screen — dev credentials are in `.local/DEV_CREDENTIALS.local.txt` (git-ignored; regenerate via a throwaway bcrypt script and update the live `users` table if it's missing).

### Frontend build
There is no frontend build step. The Go server serves the browser assets directly from `public/`; the shell loads native ES-module screen chunks on demand. No bundler, transpiler, generated `public/dist/`, or npm install is required to run the app.

## Technical Reference & Architecture

*   **Start here**: **[docs/README.md](docs/README.md)** — the documentation front door: pick what you want to do (use, set up, build, run, decide) and it links the one page that answers it, with a map of every `docs/` folder.
*   **Current priorities**: **[docs/micro_checklist.md](docs/micro_checklist.md)** is the live backlog — kept current after every closed item.
*   **Rules for every change**: **[CONTRIBUTING.md](CONTRIBUTING.md)** — the project constitution: where things live, how they are named, how data and the database change, and how the rules are enforced.
*   **New here?**: **[docs/engineering/developer-setup.md](docs/engineering/developer-setup.md)** gets the code running; **[docs/ai_handover.md](docs/ai_handover.md)** §6 has the current state of the working tree. **[docs/guides/admin-guide.md](docs/guides/admin-guide.md)** is the fuller standalone operator manual. **[docs/guides/user-guide.md](docs/guides/user-guide.md)** is the client-facing, non-technical walkthrough.
*   **Business/product scope**: **[docs/requirements/business-requirements.md](docs/requirements/business-requirements.md)**, **[docs/requirements/product-requirements.md](docs/requirements/product-requirements.md)** and the **[capability catalog](docs/generated/capability-catalog.md)** (what is supported today).
*   **System architecture**: **[docs/architecture/current-architecture.md](docs/architecture/current-architecture.md)** and **[docs/brain/README.md](docs/brain/README.md)** (which file belongs to which part of the system).
*   **Operations**: **[docs/operations/backup-restore.md](docs/operations/backup-restore.md)**, **[docs/operations/incident-runbook.md](docs/operations/incident-runbook.md)**, **[docs/operations/connector-live-verification.md](docs/operations/connector-live-verification.md)**.
*   **Build history**: **[docs/project_ledger.md](docs/project_ledger.md)** for chronological architectural decisions.

Older material (the September blueprint, the original BRD/PRD, framework and parity plans, past audits) lives in **[docs/archive/](docs/archive/readme.md)**, whose table also says where every moved file went.
