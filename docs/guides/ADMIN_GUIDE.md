# Admin Guide

A complete, standalone operator manual — written so a person can pick up this system with **zero AI assistance**, starting from a bare Windows machine, and get it running, keep it running, and grow with it. It's organized in layers: start at §1 if you've never touched this system before; skip ahead if you already know the basics and need a specific procedure.

This guide reuses, rather than duplicates, the deeper operational docs that already exist — it's the table of contents and the walkthrough that ties them together. Where a topic has its own detailed doc, this guide says so and points there.

*Need literal click-by-click steps for an admin screen or a maker-checker/approval workflow? See **[ADMIN_SOP.md](ADMIN_SOP.md)** — same operator-manual voice, one section per screen and per workflow.*

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

- **Creating a user**: **Users** screen (HR/Admin only — every other role gets a 403 from the API even though the menu item itself is currently visible to everyone, see [User Guide](USER_GUIDE.md) §3). Fill in username, password (8+ characters), email, and role, then **Create User**. New accounts start **Active**.
- **Deactivating/reactivating a user**: same **Users** screen, the **Deactivate**/**Reactivate** action on each row. You can't deactivate the account you're currently logged in as. A deactivated user's login is rejected the same as a wrong password, **and any session they already have open stops working on their next click** — you do not have to wait for their sign-in to time out. The same applies to changing someone's role or location: it takes effect on their live session, not at their next login. (If you make the change directly in the database rather than through this screen, allow up to 30 seconds — see `AUTH_STATE_CACHE_SECONDS` in `deploy/erp.env.example`.)
- **Granting permissions**: **Roles** screen (also HR/Admin only) shows every currently-granted (role, record type) permission as a table, and a form above it to add or update one — pick the role and record type, check whichever of Read/Create/Update/Delete apply, **Save Grant**. A role with no row for a given record type gets **no access at all** to it (fails closed) — HR/Admin itself always has full access everywhere and never needs a row here. See the [User Guide](USER_GUIDE.md) §3 for what each role's sidebar looks like, and `../ERP_BLUEPRINT.md` §3 for how role checks are enforced (server-side, on every action — never trust a UI-only restriction).
- **HR/Admin** and other privileged roles require MFA (Multi-Factor Authentication — a 6-digit code from an authenticator app).
- **Resetting a user's two-factor setup.** On the **Users** screen each row has a **Reset 2FA** action. Use it when someone has lost both their phone *and* their recovery codes. It clears their authenticator and every recovery code they hold, and forces them to set two-factor up again on a new device at their next login — it does **not** turn MFA off for that account. (The old route, the `cmd/reset_mfa` command-line utility, still exists for the case where nobody can log in at all; it needs shell and database access on the server.)
- **Users can normally recover themselves, and should be told so** — every reset you perform is one they could have avoided:
  - Ten **single-use recovery codes** are issued when they first enrol, shown once, and accepted on the login screen in place of a 6-digit code.
  - **My Profile → Two-Factor Recovery** lets them move their authenticator to a new phone (confirming with their password, not a code, since the old device may be gone) and generate a fresh set of codes. It also shows how many codes they have left.
  - Only a scrambled fingerprint of each code is stored, so **you cannot look up a user's codes for them** — if they have lost them, the choice is regenerate (if they can still sign in) or Reset 2FA (if they can't).
- **Worth doing before you need it: keep a second HR/Admin account, enrolled on a different device.** With a single admin account, a lost phone plus lost codes means nobody left in the tenant can perform the reset, and recovery drops back to server shell access.

> **Supplier logins (PIM).** A supplier who submits product content is created here like any other user, with the role **Supplier** — there is no separate portal to set up. Their account must also be linked to the **Vendor** record they speak for; until it is, they can sign in but every screen refuses them, by design. A Supplier session can only ever see and edit submissions filed under their own vendor. See §PIM for the review side.
- If a user's login is locked out after too many failed attempts, wait for the automatic lockout window to expire, or have an admin clear it directly.

### B.3 Configuring the System (no code required)

Several things are configurable through the app's admin screens, not by editing code:

- **Operational settings, module by module** (return windows, tolerances, timeouts, limits, offer-free thresholds) — the **Configuration** screen. This is the main one; see §B.3.0. If you are looking for "where do I change *that number*", start here.
- **Document number formats** (invoice numbers, PO numbers, etc.) — **Prefix Configurations** screen. This is where every transaction number comes from; see §B.3.2, it has more to it than the name suggests.
- **Renaming terms** to match your industry's vocabulary (e.g. "Design Number" instead of "SKU") — **Dynamic Labels** screen.
- **Adding new record types or custom fields** — **Database Schema Design** screen (this is the same "metadata-driven" engine described in `../architecture/framework_architecture.md` — new master/transaction types don't need a code change). This is different from adding an actual *record* of an existing type (a new Vendor, a new Brand, etc.) — that's a business-user task, see [User Guide](USER_GUIDE.md) §8.
- **Turning modules on/off per tenant** — module entitlements, admin-only.
- **Silent printing** (labels, barcode stickers, POS receipts, sales invoices going straight to the right printer with no browser dialog) — a **Printer** record per physical printer, plus QZ Tray on each PC that prints. Full steps in **[QZ_PRINTING_SETUP.md](QZ_PRINTING_SETUP.md)**. The field that makes it one-click is **Default For** (`Shipping Label` / `Invoice` / `Sticker` / `Receipt` / `General`) — the server resolves the job to the printer holding that role, so operators never pick a printer. **Nothing depends on this being set up**: with no Printer records, or with QZ Tray not running, every one of those screens falls back to the browser print dialog exactly as before.
- **Which status changes a document is allowed to make** — **Status Transition Rules** screen (a normal master, HR/Admin to edit). Each row says: for this record type (**Entity / Doctype**), moving from **From Status** to **To Status** is **Allowed** yes/no, and optionally **Requires Reason Code**. See §B.3.1 below — this one has a rule about how it fails that's worth understanding before you edit it.

#### B.3.0 Configuration — every operational setting, in one place

**Settings → Configuration** (HR/Admin only). The left rail lists modules; pick one and you get that module's settings, each with a plain-English description of what it actually controls. Change what you need, then **Save Changes**.

Two things worth knowing:

- **A change takes effect immediately, everywhere, with no restart.** Every setting is read at the moment it is used, not cached when the server starts. Lower the sales return window and the very next return attempt is judged by the new value.
- **Out-of-range values are refused, not clamped.** Settings that could destabilise the server if set absurdly (page sizes, concurrency, batch sizes) carry a permitted range; a value outside it is rejected at save time with a message naming the limit. Nothing is silently "corrected" behind your back.

What lives where (36 settings across 12 modules):

| Module | Examples of what you can change |
|---|---|
| Sales & Returns | How many days after a sale a return is still accepted (0 = no returns at all) |
| Procurement | Vendor-invoice 3-way match tolerance %; **how many days a PO or a GRN stays editable** (0 = no time limit) |
| Point of Sale | Cash-drawer variance a cashier may close a shift with before a written reason is required |
| Loyalty | Point expiry, spend-per-point earned, rupee value per point redeemed, tier-recompute toggle |
| Inventory | How long an online stock reservation is held before it releases |
| Manufacturing | Max BOM nesting depth, production cost-variance tolerance, default MRP lead time |
| HR & Payroll | ESI wage ceiling (change it here when the statutory figure changes — no code release needed) |
| CRM | Churn threshold, default lapsed-customer period |
| Warehouse | Cycle-count intervals for A/B/C items, task productivity alert threshold |
| PIM | Max documents per bulk edit, product thumbnail size |
| Security | Session length, password-reset link validity, failed-logins-before-lockout and lockout duration, two-factor clock tolerance |
| Platform | API page-size default and cap, per-tenant concurrency, CSV import batch size, max rows for an on-screen report, default field length cap |

**Integrations** is the last entry in the rail. It holds the endpoint and credentials for the two external systems this ERP talks to — **Pine Labs** card terminals (one entry per terminal ID) and **Unicommerce**, the OMS middleware (one entry per store code). Fill in the fields and save; saving an entry whose terminal ID / store code already exists updates it in place. Existing entries are listed above the form with secrets masked. The background workers pick up a changed Base URL on their next call.

> **On the session-length setting**: if your deployment sets the `JWT_EXPIRY_HOURS` environment variable, that value applies until you explicitly change the setting on this screen — an edit here always wins from then on. So the control is never a dead knob, whatever the server is configured with.

#### B.3.0.1 Setting up POS offers

Offers are ordinary records, not a code change: create an **Offer** (HR/Admin or Store Manager) and it applies at every till on the next sale — there is nothing to deploy, restart, or push to the POS.

Fill in the name, pick the **Offer Type**, set **Applies To**, and set **Status** to Active. Only the fields belonging to your chosen type matter; leave the rest blank.

| Offer Type | Fill in | Means |
|---|---|---|
| Percentage Off | Discount % | Takes that % off whatever the offer applies to |
| Flat Off | Discount Amount | Takes a fixed rupee amount off (never more than the thing is worth) |
| Buy X Get Y | Buy Qty, Free Qty | "Buy 2 get 1 free". The **cheapest** qualifying units are the free ones |
| Bundle Price | Bundle Qty, Bundle Price | "Any 3 for ₹999". The **most expensive** qualifying units are bundled first |

**Applies To** is *Bill* (the whole sale), *Item* (put the SKU in **Scope Value**), or *Category* (put the category name in **Scope Value**).

Everything else is an optional condition, and they all have to be true at once:

- **Minimum Bill Amount / Minimum Qty** — the "spend ₹2000 and get…" family.
- **Coupon Code** — leave blank and the offer applies automatically; fill it in and it only applies when the cashier types that code.
- **Customer Tier** — restricts the offer to one loyalty tier. Note a walk-in with no customer on the sale has no tier, so these never apply to anonymous sales.
- **Valid From / Valid To** — leave either blank for open-ended.
- **Maximum Discount Cap** — the safety net on a percentage offer ("20% off, up to ₹500").
- **Priority** — lower numbers are considered first.
- **Stackable** — *Yes* lets later offers apply on top; *No* means once this one applies, nothing after it does. Use **No** for a headline offer you don't want combined with anything else.

Three behaviours worth knowing before you design a promotion:

- **A non-stackable offer stops the stack.** Combined with Priority, that's how you say "this offer instead of the others, not as well as."
- **A sale can never go below zero.** If offers add up to more than the bill, the discount is capped at the bill.
- **To switch an offer off, set Status to Inactive** — it stops applying immediately, and you keep the record and its history. Don't delete it.

#### B.3.1 Status Transition Rules — how "strict" works

Out of the box, 64 record types are **strict**: for those, a status change is refused unless a rule explicitly permits it, and the error tells the user which statuses they *can* move to. Everything else is permissive — any status change is allowed unless a rule explicitly forbids it.

This matters when you edit these rules:

- **On a strict record type, deleting a rule takes the transition away.** If someone reports "I can't move this invoice to Paid any more", the first thing to check is whether its rule row still exists and is Active.
- **Adding a record type to the strict set is not a checkbox in the UI** — it's the `strict_status_transitions` flag on the record type. Ask a developer; and seed its full matrix *first*, or you will freeze existing documents in whatever status they're already in.
- **Nothing gets permanently stuck.** A document sitting in a status the record type doesn't even define (older data from before that type had a lifecycle) can always be moved back to a valid status.
- **To turn enforcement off entirely in an emergency**, one database statement does it without deleting any of your rules: `UPDATE tenant_default.doctype_meta SET strict_status_transitions = FALSE;`

Some deliberate examples of what the shipped rules block: a vendor invoice can't go straight from Draft to Paid (that would skip the 3-way match), a disposed asset can't be re-capitalised, and an approved goods receipt whose stock has already posted can only be cancelled with a reason code.

Two shipped judgement calls you may want to change: an **approved Leave** and a **selected Vendor Quote** are both treated as final. If your process needs to reverse either, add the row — it's one entry on this screen, no code change.

#### B.3.2 Prefix Configurations — the number series behind every transaction

Nobody types a document number in this system. Purchase Orders, Goods Receipts, ASNs, RFQs, Vendor Quotes, Stock Transfers, Expense Claims, Leave, Employee Loans, Grievances, Production Orders and Attendance all draw their number from a **series** defined on this screen, at the moment the document is saved. The user sees a greyed-out box reading "Auto (PO series)" until then.

Each row is one series. The table shows a **Next Number Looks Like** column so you can see the effect of a change before anyone lives with it. **Edit** walks you through the settings:

| Setting | What it does |
|---|---|
| **Prefix** | The leading text. `PO`, `GRN`, `TRF` — whatever your business calls it. |
| **Separator** | What goes between the parts. `/` by default; `-` is common. |
| **Padding Width** | How many digits the counter is padded to. `6` gives `000042`. |
| **Reset Interval** | `ANNUAL` → `PO/HO/26-27/000001`, restarting each financial year. `MONTHLY` → `PO/HO/26-27-07/000001`, restarting monthly. `NEVER` → `PO/HO/000001`, one continuous series forever. |
| **Store Segment** | `Yes` puts the location in the number and numbers each location separately. `No` removes it and runs one shared series across all locations. |

Things to understand before you change one:

- **Reset Interval decides two things at once, on purpose.** It sets how often the counter restarts *and* whether the number shows the period. There's no way to reset annually while hiding the year, because that would re-issue last year's numbers — and since the number is also the document's identifier, the second document would be refused. `NEVER` is the only way to get a number with no year in it.
- **Store Segment works the same way.** Turning it off doesn't just hide the location; it merges the per-location counters into one. Otherwise two locations would both be handed `PO/000001`.
- **Changes apply to the next document only.** Existing documents keep the numbers they were issued. Renaming a prefix does not renumber history, which is what you want for an audit trail.
- **Gaps in a series are normal.** A number is drawn before the document is fully validated, so a rejected save leaves a gap. That is standard behaviour for a sequence counter and is not data loss.
- **Deactivating a series stops the documents that use it.** Anyone creating that record type gets "numbering configuration is inactive" (`ADMINC-0030`) and cannot save. Deactivate only when you intend to block the record type.
- **Don't give two series the same prefix.** Document numbers are unique across *every* record type, not just within one, so overlapping prefixes will eventually collide and cause failed saves.

#### Competitor undercut alerts

If your buyers record competitor prices (User Guide §8a), the system can tell them when one of your SKUs is being materially undercut, instead of waiting for someone to run the report.

It is **off by default** and takes two pieces of setup:

1. **Set the threshold.** Admin → Settings → Platform → **Competitor undercut alert threshold** (`market.undercut_threshold_pct`). It is `0` on a fresh system, which means "never alert" — a business that doesn't track competitor prices is never bothered by this. Set it to the percentage gap you actually care about; `10` means "tell me when someone is 10% or more below what we last sold it for". The maximum accepted is `90`, because a competitor apparently selling at 95% off is a typo in the imported data, not a pricing signal.
2. **Author the message.** Setup → Advanced → **Notification Template**, with **Event** set to `Competitor Undercut`. This uses the same templating as every other notification — `{{sku}}`, `{{our_price}}`, `{{competitor_price}}`, `{{undercut_pct}}`, `{{platform}}` and `{{source_url}}` are substituted into the body. Delivery goes through the matching **Notification Channel Config** webhook, exactly like order and return notifications.

Points worth knowing:

- **A given observation alerts at most once.** The check runs hourly but only looks at competitor prices *recorded since the previous run*, so a competitor who stays cheap for a month produces one alert, not seven hundred. Re-import the same SKU at a new price and you'll get a fresh alert, which is the intent.
- **It never changes a price.** The worker only reads and notifies. Repricing stays a human decision — there is no code path in it that writes to a document or a setting.
- **No template configured means no alert is sent, but the attempt is still logged.** Look in **Notification Log** for `Skipped-NoTemplate` rows if you expected an alert and got nothing — that is the usual cause.
- **A SKU with no sales history is never alerted on.** The comparison needs a price of ours to compare against, and this system takes that from what you actually sold at (see the note in User Guide §8a). Items never sold appear in the report as "No price on file" and are skipped by the alert.

### B.4 Where to Look When Something Seems Wrong

1. **`.\manage.ps1 logs`** — the fastest first check. Shows the server's own output and error logs, plus the database log.
2. **Activity Log** (in the app sidebar, formerly "Log Hub") — shows audit trails and recorded system errors from inside the running application.
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
- **On a self-hosted Linux server the above does not apply** — that box has its own scripts. Install the nightly job once with `sudo bash /opt/erp/deploy/install_backup_cron.sh` (14-day on-box retention; it also runs one backup straight away to prove it works), and prove restores with `deploy/restore_drill.sh`. **Do not assume this has been done**: a server with no scheduled backup looks exactly like one that has them, right up until you need one. *(This is not hypothetical — a 2026-08-07 inspection of the production droplet found exactly that: an empty crontab, no timer, and two hand-taken dumps that looked at a glance like a working backup history.)*
  - **`deploy.ps1` ships the binary and `public/`, not `deploy/`.** So a script existing in the repo does **not** mean it exists on the server. Check with `ls /opt/erp/deploy/` before assuming you can run one, and `scp` it up if it's missing.
  - **`restore_drill.sh` needs a connection that may create databases, which the application's own role deliberately is not.** A correctly hardened box gives the app role no `CREATEDB`, so the script's default "derive the admin connection from `DATABASE_URL`" fails with `permission denied to create database`. Pass **`DRILL_ADMIN_URL`** instead — typically the local superuser over the unix socket, e.g. `DRILL_ADMIN_URL='postgresql://postgres@/postgres?host=/var/run/postgresql'`. **Do not grant the app role `CREATEDB` to make the drill pass** — that weakens the running system in order to test a backup.
  - **The superuser usually cannot read `/opt/erp/backups`** (`/opt/erp` is `drwxr-x--- erp:erp`), so stage a copy of the newest backup somewhere it can, point `BACKUP_DIR` at it, and **rewrite the `.sha256` sidecar's absolute path to a bare filename** — `sha256sum -c` resolves the path recorded inside the file, so a staged copy otherwise fails its own checksum and reports a perfectly good backup as corrupted. The full working invocation is in **[`../operations/restore_drill_log.md`](../operations/restore_drill_log.md)**.
- **To export a single tenant** rather than the whole database — offboarding, handing a customer their data, or a scoped copy before a risky fix — use `.\manage.ps1 export-tenant -TenantSchema tenant_<name>` (or `deploy/backup.sh --tenant tenant_<name>` on Linux). Same encryption and checksum as a full backup; restore it into a scratch database, never over a live one.

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
- **[`../architecture/framework_architecture.md`](../architecture/framework_architecture.md)** — the metadata-driven Record Type engine every module is built on (internal/architectural docs still use the framework's original technical name, "DocType", when describing the underlying database tables and API shape — the UI and admin-facing docs use "Record Type" throughout).
- **[`../architecture/architecture_evaluation.md`](../architecture/architecture_evaluation.md)** — why Go/PostgreSQL/schema-per-tenant, with the cost/footprint reasoning.
- **[`../requirements/PRD.md`](../requirements/PRD.md)** — module-by-module functional requirements and built-vs-specified status.

### D.5 Security Posture

JWT bearer auth with expiry, TOTP MFA for privileged roles, server-side RBAC on every document operation, parameterized SQL throughout (no string-built queries), a request body size cap, per-category rate limiting, a CORS allowlist, HMAC-verified inbound webhooks, AES-256-GCM encrypted-at-rest channel credentials. Historical hardening record: **[`../operations/hardening_roadmap.md`](../operations/hardening_roadmap.md)** (closed, historical reference — not an active backlog).

Two additions worth knowing about (Stage 29.8):

- **Sessions are re-validated per request, not just at login.** Token claims are frozen when issued, so the middleware re-reads the user's active-status/role/location on each request (30s cache, `AUTH_STATE_CACHE_SECONDS`). Deactivating or demoting someone takes effect on their open session. A database failure here returns a retryable 503, deliberately *not* a 401 — a replica hiccup must not sign everyone out.
- **The signing key can be rotated without logging everyone out.** Set `JWT_SECRET_2` alongside `JWT_SECRET_1`; the highest number signs new tokens and every configured key still verifies. Wait one full token lifetime (`JWT_EXPIRY_HOURS`), then delete the old one. Full procedure in `deploy/erp.env.example`.

### D.6 Extending the System

- **New master/transaction types**: use Database Schema Design (§B.3) — no code required for the common case.
- **New business logic**: add to `engines/` as its own file, following the existing one-file-per-module convention.
- **3rd-party integrations**: a scoped, read-only extension framework already exists (`extension-sdk/`, self-contained, meant to be handed to an external developer) for hooking into the platform without granting full core access.

### D.7 Governance Model

Per this project's own planning references: a small central team owns the core kernel and release process; module owners own their business rules and user acceptance; client/industry-specific needs are handled through configuration first (Database Schema Design, feature flags), scoped extension hooks second, and a core code change only when a genuinely reusable platform capability is missing — never a per-client fork of the codebase.

---

## Glossary (technical → plain language, both directions)

| Term | Plain-language meaning |
|---|---|
| **Tenant** | One business's private, isolated slice of the shared system. |
| **Schema-per-tenant** | The technical method keeping each tenant's data physically separate in the database, not just filtered in the app. |
| **Record Type** | A "kind of record" (e.g. Purchase Order, Item) defined as configuration, not hardcoded. |
| **RBAC** | Role-Based Access Control — what a user can do is determined by their role. |
| **JWT** | The digital "ID card" (token) a logged-in user's browser presents on every request. |
| **MFA / TOTP** | A second login check via a time-based code from an authenticator app. |
| **GL / Ledger** | The accounting record; "double-entry" means every transaction has a matching debit and credit that must balance. |
| **Idempotent / idempotency key** | A safeguard so that if the same request arrives twice (e.g. a network retry), it only takes effect once. |
| **Outbox pattern** | A way of making sure a slow external system (like a payment gateway) never blocks or breaks a user-facing action. |
| **Correlation ID** | A tracking code that lets you follow one user action across logs and error reports. |
| **Worktree** (git) | A separate folder holding a different checked-out version of the same repository — how `test`/`live` stay independent of `dev` without being separate copies of the whole repo history. |
| **CI (Continuous Integration)** | Automated checks (build, test, security scan) that run on every code change, before it's trusted. |
