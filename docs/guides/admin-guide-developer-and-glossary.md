---
doc_id: DOC-C44AC5B356-06
title: Admin Guide: Developer reference and glossary
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Admin Guide: Developer reference and glossary

Part 6 of 6 of the [Admin Guide](admin-guide.md). Previous: [Security events and operations](admin-guide-security-and-operations.md).

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
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -o bin/erp-server.exe ./cmd/server

# Build a stripped release binary (smaller, no debug symbols)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -ldflags="-s -w" -o bin/erp-server.exe ./cmd/server

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

**Comments are dropped as files are served (283.1).** In a `.js` file, each line that holds only a `//` comment goes out as an empty line. In a `.css` file, every `/* */` comment is removed. The files on disk keep every comment. Line numbers in the browser's error messages and in its developer tools still match the source; the browser just shows blank lines where those comments were. This saves about a fifth of what the browser downloads. If the server can't be sure where code ends in a file (for example, a string that never closes), it sends that file unchanged and logs `<file> served with its comments`. `go test ./internal/webasset/` fails on any such file, and the release's `node --check` covers every script in `public/`.

**One type scale (276.16).** Screen text uses five sizes: 12, 13.5, 14, 18 and 24 px. Big display numbers and icons may be 28 px or larger. Nothing on a screen is smaller than 12 px. The sizes are written as plain values in the stylesheets, not as CSS variables, which would cost bytes on every page load. `TestPublicTypeScale` (`internal/server/ui_type_scale_test.go`) fails on any other size in a `public/` stylesheet. It skips print output (`@media print`, sheets sized in mm, and the sticker, invoice, slip and receipt rules), the handheld screens (`rf.css`), and the public pages outside the app (`p/`, `store/`, `f/` and the catalog share page).

### D.4 Architecture Reference

- **[`../ERP_BLUEPRINT.md`](../archive/legacy/erp-blueprint.md)** — the full project snapshot: scope, architecture, build history, known gaps. Read this first for orientation.
- **[`../architecture/framework_architecture.md`](../archive/legacy/framework-architecture.md)** — the metadata-driven Record Type engine every module is built on (internal/architectural docs still use the framework's original technical name, "DocType", when describing the underlying database tables and API shape — the UI and admin-facing docs use "Record Type" throughout).
- **[`../architecture/architecture_evaluation.md`](../archive/legacy/architecture-evaluation.md)** — why Go/PostgreSQL/schema-per-tenant, with the cost/footprint reasoning.
- **[`../requirements/PRD.md`](../archive/legacy/prd.md)** — module-by-module functional requirements and built-vs-specified status.

### D.5 Security Posture

**Browser style policy (49.4.3).** Screens style themselves with classes, not `style="..."` attributes. The enforced Content-Security-Policy is `style-src 'self'`: a style written into a page's markup is refused, and screens set run-time styles through the browser's style API instead. A report-only copy of the policy sends what it would refuse to `/api/v1/security/csp-report`. Nothing for you to set. The Trusted Types trial (`require-trusted-types-for 'script'`) joins that copy only when the server has `CSP_TRUSTED_TYPES_TRIAL=on`: it reports every markup write, which slows each screen, so turn it on only for a CSP sweep on a test server. The enforced policy tightens only when those reports stay at zero.

JWT bearer auth with expiry, TOTP MFA for privileged roles, server-side RBAC on every document operation, parameterized SQL throughout (no string-built queries), a request body size cap, per-category rate limiting, a CORS allowlist, HMAC-verified inbound webhooks, AES-256-GCM encrypted-at-rest channel credentials. Historical hardening record: **[`../operations/hardening_roadmap.md`](../archive/legacy/hardening-roadmap.md)** (closed, historical reference — not an active backlog).

**Every API request passes one shared check before it reaches any screen's logic** (the request envelope, Stage 49.4.4; `internal/server/request_envelope.go`). An integration or script that breaks a rule gets a clear error with a `correlation_id` to quote to support:

| Rule | Limit | Error |
|---|---|---|
| Method | GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS | 405 `GLOBAL-0303` |
| Body type | JSON or multipart file upload; a body with no `Content-Type` is read as JSON. The import hook also takes CSV/plain text; CSP reports take their browser types. Compressed bodies are refused. | 415 `GLOBAL-0304` |
| Body size | 2 MB by default. Item/bulk imports and import templates allow 16 MB, media upload 10 MB, bulk media upload 100 MB, and the import hook 5 MB. | 413 `GLOBAL-0305` |
| Headers | at most 100 fields and 32 KB | 431 `GLOBAL-0306` |
| JSON shape | one value, nested at most 32 levels, lists of at most 50,000 items, text values of at most 1 MB (2 MB for sticker templates, whose logos live in one field), numbers within ±9,007,199,254,740,991 and at most 64 characters long, and no field repeated in the same object | 400 `GLOBAL-0307` |
| Query string | each parameter at most once (`?code=a&code=b` is refused, because servers and proxies disagree on which value applies; `status` on the work-queue lists under *List sort / filter* is the exception) | 400 `GLOBAL-0307` |
| Time | 55 seconds per request (the server cuts connections at 60) | 504 `GLOBAL-0308` |
| List sort / filter (Stage 49.4.6) | `GET /api/v1/doc/{type}` sorts by id or `sort=recent` (newest first, id breaking a tie); a filter is one equality on a field the record type declares (or `id` / `status`); only the Returns, Material Issue, Delivery Challan and Job Work (`SubcontractOrder`) lists may repeat one, `status`, matching any of up to 20 values (`status=Requested&status=Approved`, 267.1, 267.3) | 422 for any other sort, an operator suffix (`code__gt`), a field the type does not have or more than 20 values; 400 for any other repeated parameter |
| Saved text (Stage 49.4.6) | no invisible control or text-direction character in any field; a code, SKU, tax or bank number, email or URL in one alphabet (a Cyrillic or Greek look-alike letter inside a Latin code is refused; names and descriptions take every script) | 422 `GLOBAL-0309` / `GLOBAL-0311`, naming the field |
| Edit conflicts (24.10, 290.3) | `GET /api/v1/doc/{type}/{id}`, every row of `GET /api/v1/doc/{type}` and the answer to a save each carry the record's `version`. A save to `/api/v1/doc/{type}/{id}` that sends `expected_version` is written only if the record is still at that version. The browser app sends it on every edit whose body holds a `version`; an API caller that leaves it out keeps last-write-wins | 409, *This document was modified by someone else since you last loaded it* |

A file at the import row limit (`platform.max_import_rows`, 20,000 by default) previews and imports well inside the time limit: a 20,000-row Item file took about 5 seconds each way in testing (Stage 49.4.4a). If you raise that setting far above the default, or a user still reports `GLOBAL-0308` on an import, have them split the file, as USER_GUIDE §8d.5 says.

**A record outside the caller's scope reads as missing (Stage 49.4.7).** Another store's, another employee's or another owner's record answers 404 `GLOBAL-0004` on a read, a save, a delete and its files list alike - the same as a record that does not exist - so a refusal never confirms the record is there. The refusal is still on the Activity Log as `GLOBAL-0011`. Only a new record filed under someone else's employee code - or another store's (below) - answers 403.

**A new record is filed under your own store (Stage 49.3.8a).** A user confined to one store - a store manager, a cashier, a warehouse manager - cannot save a record of a store-scoped type (a bin, a purchase order, a stock move, a wave...) that names another store: the save answers 403 `GLOBAL-0011` with *This record is for store X - you can add records for your own store (Y) only*, and moving a record of their store to another is refused the same way. A CSV import refuses those rows with the same reason and imports the rest, and the bulk purchase and sales uploads, which save through the same path, are held too. A user who works at several stores switches store at the top of the screen first. Roles that see every store - Super Admin, Admin, HR Manager, the accounts roles, Merchandiser, Setup Operator - are not limited. A record that names no store is left to the type's own rules.

**Rate limits and the per-tenant request cap (Stage 47.11.1).** Each API type has a per-minute budget, for example login 5, search 100 and reports 60, answered with 429 `SEC-0280`. Everything else is 300 a minute for a signed-in session and 60 signed out (Stage 141.4: one load of a setup screen makes 16 such calls, so 60 ran out after a few refreshes); the version and branding reads every page starts with have their own 300. Each signed-in session gets its own budget. If calls are refused, the screen shows one Rate Limit message, not one per call, and the menu keeps this browser's last copy of the user's modules and permissions. Everything else from one address shares that address's budget, including a request carrying a token this server did not issue or that has expired. Separately, each tenant may have `platform.per_tenant_max_concurrent_requests` requests in flight at once (default 15), and all tenants together may have `platform.global_max_concurrent_requests` (default 44, below the 50-connection database pool); past three quarters of that, a client at its fair share waits while one under its share still gets in. All of these are kept in the server's memory and stay bounded: An ordinary request that finds no slot waits up to 1.5 seconds for one before the 503 (Stage 47.11.3: a record list opens with its reads all at once, and 14 of 30 used to be turned away on the spot); report runs and exports do not wait, they are told to try again.
- The limiter forgets callers a minute after their last request and never tracks more than 100,000 at once.
- If a flood of new addresses fills that table, new callers share one budget per API type until it drains. Callers already being tracked keep their own budget. The server log shows `[RATE-LIMIT] key table full` while this happens.

If you run more than one server process behind a load balancer, each process keeps its own counts. A caller spread across N processes can make up to N times a budget per minute, and each process applies the tenant cap to its own database connections. That is intended: these limits slow abuse and protect each process. The protections that must hold across every process are in the database: account lockout after failed logins, MFA and reset tokens, and session revocation. The sign-in backoff (a growing wait after 3 failed tries for one name from one address, Stage 134) is per process too.

**Scheduled work with more than one server (Stage 134).** Background jobs (Background Jobs list) are claimed row by row and run on every server. The scheduled sweeps - outbox and webhooks, alerts, PO auto-cancel, depreciation, channel syncs, push alerts and the rest - run only in the one server holding the *scheduler lock* (a PostgreSQL advisory lock on one connection), so no sweep runs twice. If that server stops, its connection closes, the lock is freed and the next server to tick takes over within a minute; nothing to configure. `SCHEDULER_LEADER=off` in a server's environment makes it run every sweep regardless (the old behaviour) - only for a single server sharing its database with a tool that would hold the lock.

**What a job row says (49.8.6).** Every job names its **actor**: the user whose request queued it, or `system:<worker>` for one a sweep or another job queued (`system:outbox`, `system:audit-evidence`, `system:job:<type>`). A job type nothing in this release can run is refused when something tries to queue it, not left to dead-letter. One attempt may run for at most its type's runtime bound (5 minutes unless the type sets its own); over it, the attempt is recorded as *timed out* and retried like any other failure, and the job's lease covers the bound so a second server never picks it up meanwhile. A result larger than 64 KB is stored as a marker (truncated, original size, its first bytes). A job that ignores the stop signal at its bound cannot be killed (Go has no way to), so the runner stops waiting for it 15 seconds after the bound, fails the attempt and counts it as *abandoned*; the stray work may finish in the background, which is why every job handler must watch its context (a test enforces it).

**Retry storms and an exhausted database pool (49.10.5).** A failing job - an outbound webhook whose endpoint is down runs as one - waits 30 s, 60 s, 120 s and 240 s between its five attempts, then is dead-lettered; a wait never exceeds one hour whatever a job's attempt limit, and each worker pass takes at most 10 jobs, so a pile of failures is worked off slowly instead of hammering the endpoint. A provider re-sending one inbound webhook many times is processed once. If every database connection is busy (a stuck query, a runaway report), `GET /api/v1/health` answers **503** with `db_pool` (in use, maximum, waits) within 3 seconds instead of hanging, so a deploy check or load balancer sees the cause, and the server serves normally again as soon as connections free up - nothing to restart. One client waiting for a connection no longer holds up other clients' sign-in and health checks.

Two additions worth knowing about (Stage 29.8):

- **Sessions are re-validated per request, not just at login.** Token claims are frozen when issued, so the middleware re-reads the user's active-status/role/location on each request (30s cache, `AUTH_STATE_CACHE_SECONDS`). Deactivating or demoting someone takes effect on their open session. A database failure here returns a retryable 503, deliberately *not* a 401 — a replica hiccup must not sign everyone out.
- **A client's schema name is cached too** (item 248.1). `db.GetTenantSchema` runs on nearly every request, so its answer is kept for the same `AUTH_STATE_CACHE_SECONDS` (0 turns the cache off). Suspending, purging or deleting a client drops its entry at once through `db.ForgetTenantSchema`; anything new that changes a client's schema must call it too. The database pool holds up to 50 open and 25 idle connections.
- **Background workers share one view of the clients** (item 283.2). A worker loop lists clients with `sweepTenantSchemas(moduleKeys...)` (`engines/worker_sweep.go`), never `listTenantSchemas`: it answers the same list from a view of the clients and their module switches read at most once per `AUTH_STATE_CACHE_SECONDS` (0 reads afresh every time). Anything new that adds, suspends or resumes a client, changes its package or switches a module must call `forgetSweepView()` after it commits, as `InvalidateTenantLifecycleCache`, `SetModuleEntitlement` and `TransitionTenantPackages` do. A worker with its own queue first asks every client at once whether anything is due, with `dueSchemas(schemas, table, where)` and its own due condition; on any error that returns every client. `TestBLD044WorkerInventoryMatchesTheCode` fails a worker that calls `listTenantSchemas`.
- **A query the user left stops in Postgres** (item 283.3). Code that runs on behalf of one request without passing a context binds the request's context to its goroutine: `defer db.BindContext(ctx)()`. Every context-free query, exec, transaction and prepare on that goroutine then runs under it, and lib/pq cancels the statement in Postgres when the context ends. The report runner binds each run (with its five-minute limit) and the document list binds its count and page queries. Never bind around a write that must finish whether or not the user is still there, such as an audit entry; goroutines the work starts are not bound.
- **The signing key can be rotated without logging everyone out.** Set `JWT_SECRET_2` alongside `JWT_SECRET_1`; the highest number signs new tokens and every configured key still verifies. Wait one full token lifetime (`JWT_EXPIRY_HOURS`), then delete the old one. Full procedure in `deploy/erp.env.example`.
- **Audit evidence has its own signing key, `AUDIT_SIGNING_KEY`** (exactly 32 characters; rotate with `AUDIT_SIGNING_KEY_<n>` the same way). Rotating the JWT key above no longer touches audit evidence. Until 2026-10-09 audit rows and checkpoints were signed with the JWT key, so rotating it made all earlier evidence look tampered. Evidence signed that way still verifies as long as its JWT key stays configured. Set `AUDIT_SIGNING_KEY` in `/etc/erp/erp.env` and keep it with your other keys. If it is unset, the server generates one on its own disk (finding **SB-025**), and a database restored onto another server cannot verify its audit evidence.

### D.6 Extending the System

- **New master/transaction types**: use Database Schema Design (§B.3) — no code required for the common case.
- **New business logic**: add to `engines/` as its own file, following the existing one-file-per-module convention.
- **3rd-party integrations**: a scoped, read-only extension framework already exists (`extension-sdk/`, self-contained, meant to be handed to an external developer) for hooking into the platform without granting full core access.

#### D.6.1 Public API credentials (Stage 38 foundation)

These are different from extension tokens and browser sessions. Extension tokens are short-lived and read one doctype; a public API credential is a durable integration identity with expiry, rotation and immediate revocation. Since ask 224 (items 205.3-205.22) every key also has an **owner**, the person who made it. A call runs with the owner's own rights, capped by the modules ticked on the key, so a key can never do more than its owner. It stops the moment the owner is made inactive, and starts again when an Admin uses **Take over**. Keys made before this keep their old scopes and keep working.

After `db/migrations/migrations_stage38_2_api_credentials.sql` is applied, a Super Admin can issue/list/rotate/revoke credentials through the private `/api/v1/admin/api-credentials` endpoints documented in `../specs/public_api_v1.md`. A newly issued or rotated key is shown once and the response is marked `Cache-Control: no-store`; copy it directly into the integrator's secret store. The database retains only a one-way digest and an operator-visible prefix, so a lost key cannot be recovered—rotate it.

The curated `/api/public/v1` routes (items, inventory, order status, notification delivery receipts) accept these credentials, behind the per-credential limiter (38.3) and the idempotency contract (38.5). Do not try to send an API key to the private `/api/v1` application routes; it is not a user session and will be rejected.

**Command keys on money and stock commands (Stage 49.4.7a).** The private routes that create a new money or stock record each time they run take the checkout's command key: `POST /api/v1/finance/journal-voucher`, `/finance/payment-proposal/{id}/execute`, `/hr/run-payroll`, `/hr/disburse-loan`, `/inventory/adjustments`, `/pos/advances`, `/pos/gift-card/issue`, `/loyalty/redeem` and `/crm/voucher/redeem`. Send an `Idempotency-Key` header (or an `idempotency_key` field in the body; up to 200 characters). A retry with the same key and the same request answers the first result, marked `Idempotent-Replayed: true`, and creates nothing; the same key with a different request is refused **GLOBAL-4972**; a retry while the first is still running gets **GLOBAL-4971** (409, retryable); a refused request frees its key. If the server stopped after the command saved but before it recorded the key, the key keeps answering **GLOBAL-4971** for its 7 days (49.4.7): look for the record first, and only if it is not there, send again with a new key. Keys are per user and kept 7 days. A request with no key runs as before. The screens send one themselves. Delivery challans (create, dispatch, return, cancel), material request / issue / return, subcontract send / receive / charge, e-invoice and e-way bill actions, GRN post-child / post-all / pay, PO send and print reprint requests take the same key. Commands that move an existing record on (post, approve, cancel) are guarded by its status instead - `docs/security/idempotency-audit.md` lists every route and which it is.

**The API access screen (Stage 38.2d / 55.21, rebuilt in ask 224).** **Settings → API access** opens on the list of keys: name, owner, the modules it can reach, status, last use, and today's calls and errors. An Admin picks **Mine** or **All**; anyone else sees only their own keys. Filter by status: *Active*, *Expires soon*, *Owner inactive*, *Rotating*, *Waiting for developer*, *Expired* or *Revoked*. Anything that needs action shows above the list, with a **Fix** button when one click can mend it. The same count shows as a Home tile, *API access needs attention*.

**Who can share.** The right is called *Share module API* (`api.share`). Admin and Super Admin always hold it. A holder can give a key only the modules they use themselves: **View** needs read rights on that module and **Change** needs write rights. Today only Admin holds it. Tell your provider which other roles should get it.

**New API access** opens its own page:

1. **Name** (for example *Shopify sync*) and **Start from**: *Online store sync*, *Accounting export*, *Reporting (view only)*, *Warehouse device* or *Custom*. A quick start only ticks modules for you, and skips any module you cannot give.
2. **The module grid.** Set each enabled module to **Off**, **View** or **Change**. **Fine-tune** narrows a module to some of its document types, and has **Can approve**. Without that tick a key creates, changes and submits drafts, but approval steps stay with people. Money and period actions are never open to a key.
3. **Sign-in**:
   - **Secret key.** The integration sends `Authorization: Bearer erp_v2_…` on every call. The company code is inside the key, so no `X-Tenant-ID` is needed. Old `erp_v1_` keys still send `X-Tenant-ID`.
   - **App login (OAuth).** For platforms that expect OAuth2. The integration posts its client ID and client secret to `/api/public/v1/oauth/token` with `grant_type=client_credentials` (HTTP Basic, or `client_id`/`client_secret` in the body, plus `X-Tenant-ID`). It gets back a token valid for 15 minutes (`Authorization: Bearer erp_at_…`). There is no password grant and no refresh token. An OAuth client's secret is refused if sent directly as a bearer. An `X-Tenant-ID` (or `?tenant_id=`) that names no registered client is refused with **SAAS-0188**: check the client code.
4. **Expires**: 30 days, 90 days, 1 year (the default), a date (the key works to the end of that day), or never.
5. **Stores**: all stores, or only some. A key can reach only stores its owner can reach.
6. **IPs**: leave as *Any*, or list the addresses allowed to call.
7. **Send to developer instead.** Tick it and **Create** makes a one-use link valid for 48 hours instead of showing the key. Send the link to the developer; when they open it, the key is shown to them once and never stored. Until then the key shows *Waiting for developer*.

**Create** shows the key once, with the base address, the company code and a **Test connection** button. Copy it into the integration's secret store, tick **I've saved the key**, then **Done**. A lost key cannot be recovered; rotate it instead.

**A key's page** (click its name in the list) has six tabs:

- **Overview**: owner, sign-in, key prefix, expiry, last use, today's calls and errors, stores and IPs.
- **Modules**: change the grid and **Save**. It takes effect on the next call.
- **Activity**: the latest calls with their result, and an errors-only view.
- **Limits**: calls a minute and calls a day.
- **Webhooks**: send events for the key's document types to an https address (see *Webhooks* below).
- **Share**: a read-only docs link for the developer (see *Docs* below).

On the key's page:

- **Rotate** makes a new key. Under **Old key works for**, choose *Now*, 1 hour, 24 hours (the default) or 7 days. Both keys work during that overlap, so the integration can switch without a failed call. The old key and every token it obtained then end. A key whose developer has not opened its link yet cannot be rotated (**APIKEY-0512**): wait for them, or revoke it and make a new one.
- **Revoke** ends the key, its tokens, its docs link and its webhooks for good.
- **Take over** (Admin only) makes you the owner of a key whose owner left.

Expired token digests are swept with the API logs.

**Docs.** **Docs** on the list shows the API for the modules you can share. It has a getting-started section, each module's records, actions, reports and events, the fields for each document type (custom fields included), and every error with its cause and fix. **Download OpenAPI** and **Download Postman** on a key's Share tab give the same spec for that key only.

**Share docs** makes a read-only link for up to 90 days. The link holds no secret. It dies when you **Withdraw** it, when it expires, or when the key is revoked.

**Warnings.**

- *Expiry*: 30, 7 and 1 day before a key expires, its owner and the Admins get an email, once for each step. The key also shows under *Expires soon*.
- *Failing calls*: when one key is refused 20 times or more in 10 minutes, its owner and the Admins get an email naming the cause (at most one an hour), and the key shows above the list and on the Home tile, for example *Shopify sync is failing: needs View on Inventory*. **Fix** widens the key, but only to what you hold yourself, and the change is written to the audit log (`API_KEY_FIXED`).
- *Server errors*: a server error on the public API raises an operations alert, at most one every 10 minutes per company.

The watch runs every 15 minutes on the server that runs scheduled jobs.

**Actions and reports.**

- A key with the module can run a short, reviewed list of screen actions through `POST /api/public/v1/actions/{module}/{action}`, as its owner. `GET /api/public/v1/actions` lists the ones it can run.
- Today the list holds:
  - OMS: `order-hold`, `order-release-hold`, `order-cancel` and `order-release-to-fulfillment`.
  - Inventory: `piece-hold-place` and `piece-hold-release`.
- Money, POS, period, finance and manufacturing actions are kept off the list.
- `GET /api/public/v1/reports` lists the reports in the key's modules that the owner's role may run. `GET /api/public/v1/reports/{id}?format=json` (or `csv`) runs one, 200 rows a page (up to 1000 with `page_size`), with the report's own filters as parameters. Columns the owner may not see are left out.

**Webhooks.**

- On a key's **Webhooks** tab, enter an https address and pick events, or leave events empty for all of them. **Add** shows the signing secret once.
- Events are named `<module>.<DocType>.<event>`, for example `procurement.PurchaseOrder.submitted`. Each document can raise *created*, *updated*, *submitted*, *cancelled* and *approved*. The event is sent whether the change was made on the document itself or by a module's own action, such as cancelling an order on the OMS screen.
- A key receives events only for the document types it can read. An event holds the record's id, status and address, never its fields: the receiver reads the record with the key.
- Company-wide webhook subscriptions (not tied to a key) do not receive these document events; only a key's subscriptions do.
- Up to 10 subscriptions per key. An address that is not https or points at a private network is refused (**APIKEY-0515**).
- Each delivery is signed and retried with backoff. **Deliveries** lists each try with its answer, and **Resend** sends one again.
- Nothing is sent while outside calls are switched off.

**Unused API access expires (Stage 49.2.6).** **Settings → Configuration → Security → Revoke API access unused for** (Off by default; 30, 60, 90, 180 or 365 days) revokes, about once an hour, every key or OAuth client not used for that long — exactly as **Revoke** does, tokens included — with an audit row `API_CREDENTIAL_REVOKED` *… not used for N days (unused API access policy)* under the user *system*. The time counts from the latest of its last use (or its issue, if never used) and when the setting was last changed, so turning it on revokes nothing straight away. A revoked credential cannot be brought back: issue a new one and give it to the integrator.

**Purpose and rotation (Stage 49.2.6).** Type a **Purpose** (for example *Shopify sync*) when you issue an API key or OAuth client; it shows in the list and carries over when you rotate. **One API access per purpose** (Settings → Configuration → Security, Off by default) refuses a second active access for the same purpose. **Rotate API access every** (Off, 90, 180 or 365 days) revokes an older key or client the same way as above, after one warning in the Activity Log 14 days before (*API_CREDENTIAL_ROTATION_DUE*); rotating starts the count again, and turning it on revokes nothing straight away. Session and MFA tokens now carry an issuer and audience; tokens issued before this keep working.

**Notification delivery (Stage 73.2.13).** Order and return notifications no longer report "Sent" on a fire-and-forget call. Each one is written **Queued** together with its delivery job; the job POSTs to the channel's **Notification Channel Config** webhook (HTTPS only, public addresses only) with a `delivery_id` and an `X-Notification-Delivery` header, retries with backoff on a network error, timeout, HTTP 408/429 or 5xx (**Retrying**), and stops at **Failed** after the last attempt — **Replay** on the job in **Integration Logs → Async Jobs** resends it. A 2xx answer makes it **Accepted**; if the provider's answer says delivered or failed, that is recorded straight away. Otherwise give the provider (Zapier, Make or your own service) an API key or OAuth client with `notifications:write` and have it call `POST /api/public/v1/notifications/{delivery_id}/delivery-status` with `{"status":"delivered"|"failed","provider_ref":"…"}` (plus an `Idempotency-Key`). Delivered is final; a repeated receipt is harmless. Every step is kept in the notification's history.

**Long tasks show in the Tasks bar (297.3).** A job a user starts and waits for (an import, a photo copy) is listed in that user's **Tasks** button, with a bar, rows done and time left. To make a new job type show there: call `RegisterUserTask(jobType, "Short name")` once (in an `init`), queue it with `EnqueueUserTask(ctx, tenantID, jobType, label, payload, key)` from the request (the request's user becomes the job's owner), and call `ReportJobProgress(schema, jobID, done, total)` from the handler every few hundred rows. That call also keeps the job's lease, so a long run is never taken over by another worker. The screen that starts it calls `trackTask('job:' + id, label)` in the browser; a result with `rows_in`, `rows_bad` and `error_file` shows as *N in · M not* and an **Error file** button. Report exports are listed from their own records. Jobs that are side effects (notifications, webhooks, marketplace pushes) are not registered and stay on **Integration Logs → Async Jobs** only. The browser asks `GET /api/v1/me/tasks` every 3 seconds (every 15 while the tab is hidden), and only while a task of that user is still running; a tab with nothing running sends nothing. `POST /api/v1/me/tasks/{id}/cancel` cancels the caller's own task only: another user's answers *not found*.

**AI assistant access (148.37, module `ai_access`, off by default).** Turn the module on under **Setup → Modules** and an AI assistant can read stock, order status and reports, and leave a Draft sales order, through the public API. Under **Settings → API access → AI assistants** choose **New AI assistant**: give it a **Name**, pick the one **Acts as** user, and tick **Read** (`ai:read`) and/or **Draft orders** (`ai:draft`). The secret is shown once; the assistant exchanges it for a 15-minute token at the same token URL as any OAuth client (`/api/public/v1/oauth/token`), then calls `GET /api/public/v1/ai/stock?sku=&location=`, `GET /api/public/v1/ai/orders/{id}/status`, `GET /api/public/v1/ai/reports` and `/ai/reports/{id}`, and `POST /api/public/v1/ai/sales-order-drafts`. Every call runs with that user's own rights: their role, their stores (another store's stock is refused, and an order of another store reads as not found) and the report rules for their role. A call fails the moment the user is disabled, the client is revoked (**Revoke** in the list, or the API access screen), or the module is switched off. Read tokens cannot reach any write route. The only write is a Draft sales order, marked *made by an assistant*; it reserves nothing and cannot post or approve. Each call is rate limited like any API credential and written to the audit log with the client and the user (`AI_ACCESS_READ`, `AI_ACCESS_DENIED`, `AI_ACCESS_DRAFT`). An assistant cannot act as a Super Admin, an Admin or any user with access to every function (SEC-0309): pick an ordinary user; if that user is later promoted, the assistant stops working. Rotating an assistant's key keeps its link to its user, so the new key works at once. A new company copy and a company data reset both carry the link table (a reset removes the links with the keys). A draft is limited to 20 open per assistant, a customer name of 200 and an address of 500 letters, and Active items only; people submit drafts from the **Assistant drafts** panel on **OMS → Home**. Errors are SEC-0300 to SEC-0309.

#### D.6.1a PIM setup and navigation (Stage 55.11)

The PIM menu holds its working pages (Home, Workbench, My Work, Workflow runs, Media Library, Supplier Submissions, Catalogs, PIM Reports); its setup lists are under **Setup → PIM** (Stage 88.1). The Product Dashboard carries the ordered path: Item master → Product Family → Attribute Definitions → required Family Attributes → assign Item family → Workbench content/media → completeness report → optional channel mappings/validation → publish from Workbench. This mirrors the module handbook. The grouped navigation does not change server permissions or create a new product master. A local ready score is not external channel acceptance; keep a channel-response and exception review before treating an item as live.

**Marketplace layouts (140.12).** A channel whose platform is Shopify, Amazon or Myntra takes its ready-made layout from **PIM -> Home -> Marketplace feeds**: the marketplace's columns as field maps, any attribute the layout needs that the client lacks (with the marketplace's value list where it has one - Myntra's Gender and Sleeve Length, Amazon's department), and an export template in the marketplace's column order. Applying again changes nothing already there. ERP-hosted images are not public, so the image column fills only from media stored as a public http(s) address.

The Workbench's selected-channel check now uses the same publish-readiness rules as the queue gate (completeness, category map and channel validation rules). It disables Publish until locally ready and names missing fields; an external connector still requires its own receipt check. On a **Shopify** channel, Publish sends the item's design (all its variants, or the item alone if it has no design) through the channel's Shopify page publish (§B.3.20), so the product is linked and its price and stock stay in step; the channel's field mappings are not sent there (Stage 135.22). The manual UI walkthrough used a Product Family Design ID, an Item Combination SKU, a mandatory attribute and a separate ProductContent approval. A Store Manager cannot approve ProductContent merely because they can read it; the shipped approval rule requires **HR/Admin**, and the checker must differ from the submitter.

Product Family is a required core completeness check; a family-less Item cannot become publish-ready by filling unrelated fields. **Min Images** counts active customer-facing image assets in Main, Gallery, Variant Image or Lifestyle roles, not PDF certificates, Internal QC assets or video. A custom validation message retains its rule type prefix so the Workbench can show the relevant correction action. The isolated browser run verified rule block → Add an image → upload → ready; it does not prove a marketplace accepted the media. An approved **Media Rule Exception** (Setup → PIM; `MediaRuleException`, 55.11) lowers a channel's Min Images for one item on that channel: only an Approved, undeleted exception whose last day is empty or today or later (business day) counts, and the lowest such number wins. The migration adds an approval rule (Super Admin, as product content) where the client has none; Store Managers and Super Admins raise them; editing an approved one sends it back for approval. Delete the approval rule to let exceptions be approved by a plain save to Approved.

**Declaration packs** (Setup → PIM → **Declaration Packs**; `DeclarationPack`, 47.13.6) turn a shipped list of declaration fields on for one category and every category under it. A pack is data in `engines/pim_declaration_packs.json`; the one shipped, **Packaged goods declarations (India) - example**, lists manufacturer/packer/importer, country of origin, generic name, net quantity and unit, month of manufacture, best before (optional), MRP, unit sale price, consumer care and a label-artwork reference. Whether a pack applies to a category is a legal call, so nothing is on until someone raises a record: pick the **Pack**, the **Category**, a **Channel** (blank = every channel), **Effective from**, an optional **Last day**, **Reviewed by** (the qualified reviewer) and **Evidence** (what they checked), then **Submit for Approval**. A Super Admin approves (the migration adds that rule where the client has none). Approving adds the pack's attributes the client lacks, as **Attribute Definitions** with the codes `LM_MAKER`, `LM_GENERIC_NAME`, `LM_NET_QTY`, `LM_NET_QTY_UNIT`, `LM_MFG_DATE`, `LM_BEST_BEFORE`, `LM_UNIT_SALE_PRICE`, `LM_CONSUMER_CARE` and `LM_ARTWORK_REF`; an attribute you already have with the same code is reused. Country of origin and MRP are read from the Item. While an Approved record is in date (business day), an item of that category is not ready to publish to that channel until each required field has a value: the Workbench's **Not ready for…** shows `declarations (<pack>): <missing fields>`. **Reports → Declaration gaps** (R9063, filter by category id or name) lists every item still short. A save without a shipped pack, a reviewer or evidence, or with a last day before the effective date, is refused with PIM-9362. Editing an approved record sends it back for approval. If you delete the approval rule, create the attributes above yourself.

For mapped imports, **Setup → PIM → PIM Import Template** has a row action to inspect the resolved mapping, preview a CSV without writes, and import only a zero-error preview of the selected file. The resulting import job ID and authenticated error-row download are shown in the dialog. Rows that fail stay on the job: **Fix and resume** (on the import result, the **Import Job** list lookup, and **My work**) downloads them with their reasons and imports only the corrected rows, after a **Check** that saves nothing (User Guide §3, *When some rows of a Bulk Import fail*). Every **Import Schedule** has an **Owner**: its imports run and are recorded as that person, with their field-write restrictions (blank = whoever created the schedule, never "system"); a missing or inactive owner stops the run with the reason in **Last Run Status**, and a failed run lands in the owner's **My work**. **Supplier Submissions** Draft rows now have Submit for Approval. The shipped supplier-submission rule requires a **Super Admin** checker who differs from the maker; approval copies content to ProductContent as a Draft. ProductContent then needs its own separate **HR/Admin** checker and will load into Workbench for editing. The supplier-login/vendor isolation path itself remains to be browser-tested. Schedule owners and resume were built and browser-tested on 2026-10-18; the Stage 55.11 Done bar still waits on the Supplier-login run.

**Marketplace templates, feeds and design values (146.5.2, 146.5.5, 146.5.6).** Users load a marketplace's category sheet under PIM -> Marketplace feeds -> Templates (USER_GUIDE section 8b-1); roles with *Create* on **Channel Category Template** may load, **PIM Feed** is Super Admin and Store Manager by default. **Feed email is really sent** through the server's mail account (`SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, `SMTP_USER`, `SMTP_PASSWORD`, section on document email). Until `SMTP_HOST` is set the run says **Not sent** and nothing is claimed; a sandbox client, or one with *Outside calls* off (or a development server), shows **Simulated**. This also applies to an existing PIM export schedule with a recipient: once the mail account is set, its mail is now sent. Scheduled feeds: a PIM Export Schedule may name a **Feed** instead of a template; the schedule runs it and records the run like **Run** does. Settings -> Configuration -> PIM -> **Variants use the design's attribute values** (off as shipped) makes a variant read its design's attribute value when it has none. Design values are written by Super Admin and any role with *Update* on **Product Attribute Value**, at most *Maximum documents per bulk edit* designs at a time, all or none, one audit row (`pim.design_value.set`) per design.

**Attribute stewardship and image order per channel (47.13.4, 146.5.9).** Stewardship entries are changed only by a **Stewardship Change** request that goes through Approvals (approval rule: Store Manager; the requester cannot approve their own); the approved entry is an **Attribute Stewardship** document (read only, shown at PIM -> Stewardship). Roles with *Create* on **Stewardship Change** see New Entry; Super Admin, HR/Admin and Store Manager have it as shipped. One request per attribute waits at a time: a second request for the same attribute, even sent at the same moment, is refused until the first is decided. A direct save, import or delete of an **Attribute Stewardship** document is refused (PIM-9150). With no approved entry every attribute write behaves as before. Where an entry says **System of record wins**, a ProductAttributeValue write from another source is refused (PIM-9151) at the document save, CSV and PIM imports, the product sheet, bulk edit and Design values. Clearing a value or deleting it counts as a write. The ERP sets a value's *source* and *source ref*, not the person saving: ERP with the user for a save, bulk edit or Design values, and Import for a CSV import or the product sheet. So where the system of record is a **Person**, only that person can change the value. No channel pull writes these values yet, so an entry with **Channel** as the system of record and **System of record wins** holds the value fixed in the ERP until one exists. If an approved request could not be applied, its row shows **Not applied** and the reason; the next approved request clears it. Values also carry a *confidence* field. Image order per channel needs *Update* on **Product Media**; it is stored on the media record and changes nothing for a channel that has no order.

**Public image addresses for marketplace feeds (146.5.9).** Marketplaces fetch product pictures themselves, without signing in. Turn on **Public image addresses for marketplace feeds** (**Settings**, *PIM / PIMS*) and each feed export fills *Image url* (and *Image url 2* to *5*) with a signed address, `/media/<client>/<picture>/<signature>`, for every JPEG, PNG, WebP or GIF picture kept in the ERP; a picture that already has a public web link keeps it. The address names one picture: change a single character and it answers *not found*. The setting is off by default and cannot be switched on until the server has a signing key: whoever runs the server sets `PUBLIC_MEDIA_SIGNING_KEY` (at least 32 characters, for example from `openssl rand -hex 32`) in `/etc/erp/erp.env` and restarts it (see `deploy/erp.env.example`). The addresses start with the **Web address for customer links** setting, or the client's own host name if that is empty; if neither is set, the column stays empty. To change the key, put the old one in `PUBLIC_MEDIA_SIGNING_KEY_PREVIOUS` and the new one in `PUBLIC_MEDIA_SIGNING_KEY`, restart, run the feeds again, and clear the previous key (and restart) once marketplaces have re-fetched. Anyone holding an address can fetch that one picture: switch the setting off to stop all of a client's addresses at once. A picture that is deactivated or deleted stops serving at once, but a marketplace or a CDN may keep its own copy. One caller may fetch up to 600 pictures a minute (the same limit as picture reads in the app).


#### D.6.1b Lifecycle, scheduled changes and merge (168.5, 168.9, 168.10)

**Block sale of End of life items** (**Settings**, key `pim.lifecycle_block_eol_sale`, off by default). Off: the till warns when an End of life item, or a Launch item before its launch date, is sold. On: that sale is refused (PIM-9202 and PIM-9203), at the till and when an order is created. Purchase orders always refuse Phase-out and End of life lines (PIM-9200). Scheduled item changes are applied by a job that runs every 10 minutes on the business day; one failed change never stops the others, and each result is in the audit log (`item.scheduled_change.applied` / `.failed`). Only Super Admin can create a scheduled change; a Store Manager can read them. A product merge (`POST /api/v1/pim/items/merge`, admin or a role allowed to delete Items) is one transaction through the command spine: stock moves with ItemMerge ledger lines, the cost is blended, nothing is posted to the general ledger because both items use the same inventory account, and the merge is audited as `item.merge`. Refusals are MASTER-1400, a wrong role is MASTER-1401.

#### D.6.2 PIM tasks and workflows (Stage 36.2)

Four doctypes arrive with `db/migrations/migrations_stage36_2_pim_tasks.sql`: `PIMTaskTemplate` and `PIMWorkflowDefinition` (Masters, authored under Setup → PIM) and `PIMTask` and `PIMWorkflowRun` (Transactions, driven from PIM → My Work). The migration is additive throughout and includes the standard backfill loop, so tenants provisioned before Stage 36.2 receive the same doctypes, fields, permissions and indexes as new ones.

**Default permissions**, adjustable per role as usual under Settings → Roles:

| Role | Templates & Workflows | Tasks | Runs |
|---|---|---|---|
| Super Admin | full | full | full |
| Store Manager | read/create/update (no delete) | full | read/create/update |
| Cashier | none | read + update (progress their own work) | read only |

Deleting a workflow definition is withheld from Store Manager on purpose: removing a definition out from under live runs is an authoring action, not an operating one.

Three things worth knowing before you support this module:

- **A task is not an approval, and the two are deliberately separate.** No task action can move a document's approval state, and completing a task is not an approval decision. If someone reports "the approval didn't happen when I finished the task", that is the design, not a bug.
- **`Done` and `Cancelled` are terminal.** A finished task cannot be re-opened, because finishing it may already have advanced a workflow onto its next stage. The supported answer is **Create follow-up**, which is offered in the task's own dialog.
- **Editing a live workflow definition can strand its runs.** A run whose stored stage group no longer exists in the edited definition is **stopped with a named reason** rather than restarted from the beginning — restarting would re-create every task the product had already been through. Cancel and restart such runs deliberately. Prefer creating a new definition over reshaping one that has runs in flight.

`GET /api/v1/pim/assignable-users` is a deliberately narrow endpoint returning active users' **username and role only**, gated on update rights over `PIMTask`. It exists because `/api/v1/admin/users` is Super Admin gated and a Store Manager still needs to see who they may assign work to. Do not widen it into a general user-listing endpoint.

For monitoring, three registered reports are available under Reports → PIM: **PIM Task Workload by Assignee**, **PIM Overdue Tasks** and **PIM Stalled Workflow Runs**. The last is the one to schedule — a blocked run has no open tasks by definition, so it never surfaces in anyone's inbox on its own.

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

## Two-factor for sensitive actions (Stage 49.2.3)

Administrators must use an authenticator code at sign-in. This setting extends that to other people, only for the actions that can do the most harm.

**Where:** Settings, Security, "MFA tier policy" (`security.mfa_tier_policy`).

| Value | What happens |
|---|---|
| Off (default) | Nothing changes. |
| Report only | Every action that would have been refused is logged (audit action `MFA_TIER`, status `REPORT_WOULD_REFUSE`) and still runs. The security posture shows the 14-day count. |
| Required | A person with no authenticator is refused on these actions: finance approvals and payments, payroll, connector and API credentials, bulk exports, break-glass controls (freezes, sign out everyone, reset a user's MFA) and saving or undoing role permissions. An enrolled person is asked for the code at sign-in. Administrators are covered at sign-in already. |

**Recommended rollout:** set Report only for two weeks, read the "Two-factor for sensitive actions" line under Security, ask each person listed to set up an authenticator in Profile, then switch to Required. If the line reads 0 for a week, it is safe to switch.

**Recovery codes:** shown once, on screen, when a person enrols, activates or re-enrols. Ten single-use codes, stored only as a one-way hash; a used code never works again. They cannot be shown again: reset MFA for the person instead.

**Passkeys (WebAuthn, 49.2.3):** a person can add up to ten passkeys in My Profile and use one instead of the code at sign-in; no setting is needed, and the passkey is tied to the host name people sign in on (change the host name and they add the passkey again). Attestation is not checked, so a passkey proves the person, not the device model. Reset MFA for a person (Users) also removes their passkeys. Details: `docs/security/mfa-assurance-tiers.md`.

**Changing role permissions (49.3.6)** is in the same tier list ("Privilege change"): under Required, an administrator with no authenticator cannot save or undo a role change.

**Cross-site write check (49.4.2):** the server compares the `Origin` of every data-changing request with the allowed list and its own host. By default (`CSRF_ORIGIN_CHECK` unset or `report`) it only logs `[csrf-origin]` lines. After a week with no lines for your own address, set `CSRF_ORIGIN_CHECK=enforce` in the server environment. `off` disables it.

## Staged imports: approval rule and retention (Stage 47.13.1)

A staged import is a record of type Import Batch. To require a second person, add an approval rule for Import Batch (Settings, approval rules, with the role that approves); with no rule a batch is approved directly by whoever commits it. Whether the same person may approve their own batch follows the existing self-approval setting. Raw staged rows are removed by the existing file-retention sweep (`platform.import_file_retention_days`, default 0 = off, also held by legal hold); no new setting. Approved or pending batches are never swept. The staging table is `import_staging_rows` in each client's schema, and it is cleared by a tenant reset. Staged imports record an Import Job with control totals and file lineage when they reconcile; stated column totals (DATAIM-0487) gate approval. Opening balances post through Journal Vouchers and stock adjustments, not an import.

**Large files (297.2).** A file over 1 MB goes up as a resumable upload: `POST /api/v1/uploads`, then `PUT /api/v1/uploads/{id}` pieces of up to 1 MiB, each with an `Upload-Offset` header; `GET` says how far it got. A piece that does not start where the server got to is refused (DATAIM-0488, 409) with the right offset in the `Upload-Offset` header. An upload belongs to the user who started it; another user's id is not found (DATAIM-0489). The bytes are kept under `MEDIA_STORE_DIR/uploads/<schema>/`, and each upload has a row in `resumable_uploads`. Limits (DATAIM-0490): 4 GB a file, 5 unfinished uploads per user, and free disk of eight times the file plus 2 GB. An upload left for a day is removed by the daily retention sweep, unless an import is still reading it. The file is deleted once its rows are staged.

Then `POST /api/v1/import-batches` with JSON `{upload_id, doctype, expected_totals}` stages it. Reading, checking and committing more than 2,000 rows run as the background job `import_batch`, in slices of 20 seconds, so one large import never holds a worker. Progress is saved after every chunk in the same transaction as the rows. A job that fails, or whose server dies mid-slice, is resumed from the batch (`POST /api/v1/import-batches/{id}/resume`; its Resume button shows when the step has not been heard from for 2 minutes). If nobody resumes it, the next worker carries it on when its claim runs out, within about 6 minutes. A step asked for while another runs is refused (DATAIM-0491). The row cap is `platform.max_staged_import_rows` (default 1 crore); the direct import keeps `platform.max_import_rows`. The rate-limit bucket `upload-piece` allows 600 pieces a minute and does not spend the shared one. The `bulk-upload` bucket (10 a minute) counts only file work under `/api/v1/import/`; the screens' `GET .../template` and `GET .../load-order` lookups spend the ordinary session budget.
