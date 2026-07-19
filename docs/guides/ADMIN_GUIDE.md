# Admin Guide

A complete, standalone operator manual — written so a person can pick up this system with **zero AI assistance**, starting from a bare Windows machine, and get it running, keep it running, and grow with it. It's organized in layers: start at §1 if you've never touched this system before; skip ahead if you already know the basics and need a specific procedure.

This guide reuses, rather than duplicates, the deeper operational docs that already exist — it's the table of contents and the walkthrough that ties them together. Where a topic has its own detailed doc, this guide says so and points there.

---

## Part A — Foundation (for someone who has never seen this system)

### A.1 What is this, in one paragraph?

This is business software (an ERP — Enterprise Resource Planning system) that runs as one program (`erp-server.exe`) plus one database (PostgreSQL). It handles point-of-sale, inventory, purchasing, accounting, and more, for one or more businesses ("tenants") at once. Everything lives in two things: the program, and the database it talks to. There's no cloud dependency, no external service required to run it locally.

### A.2 What you need before you start

- **Go** (the programming language this is written in) — a "portable" install (no installer needed, just extracted files) lives at `%USERPROFILE%\go-portable\go`.
- **PostgreSQL** (the database) — a portable install lives at `%USERPROFILE%\pg-portable\pgsql`, with its data at `%USERPROFILE%\pg-data`.
- **PowerShell** — comes with Windows; this is the "terminal" you'll type commands into.
- **Git** — for pulling/pushing code changes.

If any of these aren't already installed on the machine you're setting up, download the portable/zip versions of Go and PostgreSQL (not the installer versions) and extract them to the paths above — the scripts in this repo assume those exact locations.

### A.3 Starting the system for the first time

Open PowerShell, navigate to the repository folder, and run:

```powershell
.\manage.ps1
```

This opens an interactive menu. Choose **1) Start** — it starts the database, waits for it to be ready, then starts the ERP server. Choose **4) Status** any time to see what's currently running.

Once it says running, open a web browser and go to `http://localhost:8080`. You'll see a login screen. Development login credentials live in `DEV_CREDENTIALS.local.txt` at the project root (this file is intentionally excluded from version control — never commit real credentials).

### A.4 Stopping the system

From the same menu, choose **2) Stop**. Or run `.\manage.ps1 stop` directly. This is safe to do any time — it shuts down cleanly.

### A.5 The single most important safety rule

**Never delete the database data folder** (`%USERPROFILE%\pg-data`) unless you have a recent, verified backup (see §C.3) and you mean to start over. That folder *is* the business's data — every sale, every stock count, every ledger entry.

---

## Part B — Day-to-Day Operation

### B.1 The `manage.ps1` command reference

Run these from the repository root in PowerShell:

| Command | What it does |
|---|---|
| `.\manage.ps1` | Interactive menu (safest option if unsure). |
| `.\manage.ps1 start` | Start database + server. |
| `.\manage.ps1 stop` | Stop server + database. |
| `.\manage.ps1 restart` | Stop then start. |
| `.\manage.ps1 status` | Show what's currently running and its port. |
| `.\manage.ps1 logs` | Show the last lines of the server and database logs. |
| `.\manage.ps1 release` | Rebuild the server as an optimized, smaller binary (stops the server first if running; does not restart it — run `start` after). |
| `.\manage.ps1 backup` | Back up every environment's database (see §C.3). |
| `.\manage.ps1 restore -Env <env> -File <path>` | Restore a database from a backup — destructive, requires typed confirmation (see §C.3). |
| `.\manage.ps1 fleet-status` | One-shot report across dev/test/live: which are up, their version, last deployment. |

Add `-Env test` or `-Env live` to target an environment other than the default (`dev`) — see §D for what environments are.

### B.2 User and Role Management

- New users are created as records in the system itself (via the **Users** screen in the app, if your role has access, or directly by an HR/Admin).
- Roles determine what a user can see and do — see the [User Guide](USER_GUIDE.md) §3 for what each role's sidebar looks like, and `../ERP_BLUEPRINT.md` §3 for how role checks are enforced (server-side, on every action — never trust a UI-only restriction).
- **HR/Admin** and other privileged roles require MFA (Multi-Factor Authentication — a 6-digit code from an authenticator app). To reset a user's MFA if they lose their device: `cmd/reset_mfa` is a small standalone utility for exactly this — build and run it (`go run ./cmd/reset_mfa`, see its own `main.go` for exact usage), or ask a developer.
- If a user's login is locked out after too many failed attempts, wait for the automatic lockout window to expire, or have an admin clear it directly.

### B.3 Configuring the System (no code required)

Several things are configurable through the app's admin screens, not by editing code:

- **Document number formats** (invoice numbers, PO numbers, etc.) — **Prefix Configs** screen.
- **Renaming terms** to match your industry's vocabulary (e.g. "Design Number" instead of "SKU") — **Dynamic Labels** screen.
- **Adding new record types or custom fields** — **DocType Builder** screen (this is the same "metadata-driven" engine described in `../architecture/framework_architecture.md` — new master/transaction types don't need a code change).
- **Turning modules on/off per tenant** — module entitlements, admin-only.

### B.4 Where to Look When Something Seems Wrong

1. **`.\manage.ps1 logs`** — the fastest first check. Shows the server's own output and error logs, plus the database log.
2. **Log Hub** (in the app sidebar) — shows audit trails and recorded system errors from inside the running application.
3. **`docs/operations/incident_runbook.md`** — the full incident-response procedure: severity levels, escalation, rollback, and exactly where every kind of log lives. Read this before an incident happens, not during one.

---

## Part C — Operator / Platform-Team Level

### C.1 Environment Layout

This system can run up to three independent copies side by side, sharing the same PostgreSQL server but each with its own database and port:

| Environment | Purpose | Default port |
|---|---|---|
| `dev` | The main working copy — this repository folder itself. | 8080 |
| `test` | A staging copy for verifying a change before it goes live. | per `environments.json` |
| `live` | The real, production environment. | per `environments.json` |

`test` and `live` live in their own separate folders (git "worktrees") created by `promote.ps1` (§D.1) — they are not manually maintained copies, they're produced by the promotion process itself.

### C.2 Deployment Pipeline

See §D for the full deployment procedure. In short: a change is tested in `dev`, promoted to `test`, verified, then promoted to `live` — never edited directly in `live`.

### C.3 Backup and Restore

Full procedure and the latest verified restore-drill record: **[`../operations/backup_restore.md`](../operations/backup_restore.md)**. Summary:

- `.\manage.ps1 backup` creates a timestamped, SHA-256-verified backup of every environment's database that currently exists. Do this on a schedule (a Windows Task Scheduler recipe is in the linked doc) — not just before risky changes.
- Restoring is deliberately hard to do by accident: `.\manage.ps1 restore -Env <env> -File <backup>` requires the target server to be stopped and an exact typed confirmation (`RESTORE <environment>`).
- **Perform an actual restore drill monthly**, not just backups — a backup you've never restored from is unverified. Record the date, file, duration, and result (the linked doc shows the format).

### C.4 Incident Response and Alerting

Full procedure: **[`../operations/incident_runbook.md`](../operations/incident_runbook.md)** — severity levels (P0-P3), escalation contacts, rollback steps, and every log location in one place. Automated alerting (a message to Slack/Teams when the system panics, a backup fails, or errors spike) is built and only needs `OPS_ALERT_WEBHOOK_URL` set to your real destination — see that doc §3 for exact setup.

### C.5 Connector / Integration Verification

If you're enabling a real Shopify/BigCommerce/Magento connection for a tenant: **[`../operations/connector_live_verification.md`](../operations/connector_live_verification.md)** — the exact credentials format and a script that verifies the connection against the real platform before you trust it.

---

## Part D — Developer / CTO Level

### D.1 Deployment (`promote.ps1`)

```powershell
.\promote.ps1 -From dev -To test     # promote dev's current commit to test
.\promote.ps1 -From test -To live    # promote test's current commit to live
.\promote.ps1 -Rollback -Env live    # roll back to the previous known-good deployment
```

`promote.ps1` refuses to promote a "dirty" (uncommitted) tree, runs the full build/vet/test gate first and refuses to promote on any failure, applies any pending database migrations to the target, and records every promotion (and rollback) in a `deployments` table — `.\manage.ps1 fleet-status` reads from that table.

### D.2 Building and Testing

```powershell
# Build for local dev (keeps debug symbols)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -o erp-server.exe ./cmd/server

# Build a stripped release binary (smaller, no debug symbols)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -ldflags="-s -w" -o erp-server.exe ./cmd/server

# Run the full test suite (use -p 1: a known cross-package DB timing issue
# can cause a false failure without it - see micro_checklist.md's Stage 14
# testing note)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" test ./... -p 1

# Static analysis
& "$env:USERPROFILE\go-portable\go\bin\go.exe" vet ./...
```

CI (`.github/workflows/ci.yml`) runs all of the above plus a vulnerability scan (`govulncheck`) and a secrets scan (`gitleaks`) on every push automatically.

### D.3 Project Layout

See `README.md`'s "Project Structure" section for the full current map. In short: `cmd/server/main.go` is the real entrypoint (a thin launcher); `internal/server/` holds all HTTP handlers and middleware, split into files by domain; `engines/` holds business logic (finance, inventory, approval workflow, etc.) as its own Go package; `db/` holds the connection pool and every SQL migration file; `public/` is the hand-written frontend, served as static files.

### D.4 Architecture Reference

- **[`../ERP_BLUEPRINT.md`](../ERP_BLUEPRINT.md)** — the full project snapshot: scope, architecture, build history, known gaps. Read this first for orientation.
- **[`../architecture/framework_architecture.md`](../architecture/framework_architecture.md)** — the metadata-driven DocType engine every module is built on.
- **[`../architecture/architecture_evaluation.md`](../architecture/architecture_evaluation.md)** — why Go/PostgreSQL/schema-per-tenant, with the cost/footprint reasoning.
- **[`../requirements/PRD.md`](../requirements/PRD.md)** — module-by-module functional requirements and built-vs-specified status.

### D.5 Security Posture

JWT bearer auth with expiry, TOTP MFA for privileged roles, server-side RBAC on every document operation, parameterized SQL throughout (no string-built queries), a request body size cap, per-category rate limiting, a CORS allowlist, HMAC-verified inbound webhooks, AES-256-GCM encrypted-at-rest channel credentials. Historical hardening record: **[`../operations/hardening_roadmap.md`](../operations/hardening_roadmap.md)** (closed, historical reference — not an active backlog).

### D.6 Extending the System

- **New master/transaction types**: use the DocType Builder (§B.3) — no code required for the common case.
- **New business logic**: add to `engines/` as its own file, following the existing one-file-per-module convention.
- **3rd-party integrations**: a scoped, read-only extension framework already exists (`extension-sdk/`, self-contained, meant to be handed to an external developer) for hooking into the platform without granting full core access.

### D.7 Governance Model

Per this project's own planning references: a small central team owns the core kernel and release process; module owners own their business rules and user acceptance; client/industry-specific needs are handled through configuration first (DocType Builder, feature flags), scoped extension hooks second, and a core code change only when a genuinely reusable platform capability is missing — never a per-client fork of the codebase.

---

## Glossary (technical → plain language, both directions)

| Term | Plain-language meaning |
|---|---|
| **Tenant** | One business's private, isolated slice of the shared system. |
| **Schema-per-tenant** | The technical method keeping each tenant's data physically separate in the database, not just filtered in the app. |
| **DocType** | A "kind of record" (e.g. Purchase Order, Item) defined as configuration, not hardcoded. |
| **RBAC** | Role-Based Access Control — what a user can do is determined by their role. |
| **JWT** | The digital "ID card" (token) a logged-in user's browser presents on every request. |
| **MFA / TOTP** | A second login check via a time-based code from an authenticator app. |
| **GL / Ledger** | The accounting record; "double-entry" means every transaction has a matching debit and credit that must balance. |
| **Idempotent / idempotency key** | A safeguard so that if the same request arrives twice (e.g. a network retry), it only takes effect once. |
| **Outbox pattern** | A way of making sure a slow external system (like a payment gateway) never blocks or breaks a user-facing action. |
| **Correlation ID** | A tracking code that lets you follow one user action across logs and error reports. |
| **Worktree** (git) | A separate folder holding a different checked-out version of the same repository — how `test`/`live` stay independent of `dev` without being separate copies of the whole repo history. |
| **CI (Continuous Integration)** | Automated checks (build, test, security scan) that run on every code change, before it's trusted. |
