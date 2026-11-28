---
doc_id: DOC-PRODUCT-SCALING-RECOVERY-PLAN
title: Scaling and recovery plan
type: reference
status: in-review
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md items 313.1-313.4, 297.5, 297.8
authority: proposed-design
confidentiality: internal
last_verified: 2026-11-28
review_by: 2027-02-26
supersedes: none
superseded_by: none
---

# Scaling and recovery plan (ask 313, 2026-11-28)

The owner asked for a plan for five problems: prove 1 lakh users, the report catalog on 2G, the
import slowing at 1 crore rows, a crash at every import step, and what to add to the servers first.
Three sources were read against this code: an outside advisor's *Low Cost ERP Scaling and Recovery
Plan* (27 Nov 2026), Google's *Site Reliability Engineering* (SRE) and Kleppmann and Riccomini's
*Designing Data-Intensive Applications*, 2nd edition (DDIA). The plan follows the advisor in most
places. Where it differs, the reason is something in this code the advisor could not see.

The rule behind every decision is the advisor's, and both books say the same: keep one primary
database, make it measurable and recoverable, and add a machine only when a measurement names it.

## Decisions

| # | Decision | Why |
|---|---|---|
| 1 | **Load test with `cmd/journeylab`, not k6.** Add a `load` mode that runs the existing journeys (J1-J7) as N users at a fixed arrival rate. First 200 users on this laptop to measure CPU per request; then one rented generator and one rented server of the target size for a few hours. | Constitution Article 1: no new tool when one exists. journeylab already drives login, PO, GRN and sale over real HTTP and checks that the books tie afterwards, which k6 would have to be taught again. |
| 2 | **Report catalog: a weak ETag, `private, no-cache`, one shared loader.** Not `max-age=300`. | An unchanged catalog becomes one 304 with no body. `max-age` would let a shared till PC show the last cashier's role-specific list to the next one. A hash of the body as ETag means nothing needs invalidating. |
| 3 | **Import lookups by primary key, through one shared helper.** Keep the ANALYZE at each doubling as a safety net. | The slow plan came from `doctype = $1 AND id = ANY($2)`. Asking by `id` alone gives the planner one sensible plan whatever the table's figures say. Per-table autovacuum and extended statistics treat the symptom. |
| 4 | **Crash at every step through a test-only hook, not a timed kill.** A table test stops the job at each step boundary; a seeded random run adds more; a zombie-worker case; one real process kill. | Every boundary gets tested on every run, and a failure replays from its seed. DDIA ch. 9 calls this fault injection and deterministic simulation testing. |
| 5 | **Grow in this order:** off-box backups and point-in-time recovery, a bigger droplet, Postgres on its own machine, imports in their own process, a second app server, PgBouncer, read replicas. Each waits for its measured trigger. | Backups are the only step that protects data; the others buy speed or uptime. A second app server first needs the rate limiters out of process memory. Replicas break read-your-own-writes, which an ERP screen relies on. |

## Order of work

| Order | Item | Done when |
|---|---|---|
| 1 | Off-box backups and point-in-time recovery (75.1.1, 290.2; the owner runs the commands) | WAL reaches the off-box bucket and a restore drill (75.1.7) records the real recovery point and recovery time |
| 2 | Report catalog ETag and shared loader (297.8) | A second page open gets 304; each page fetches the catalog once; logout and tenant switch start fresh |
| 3 | Import lookups by primary key (313.2), after 297.2.5's uncommitted change ships | EXPLAIN on a table with stale figures uses `documents_pkey`; the 1-crore run commits at a steady rate |
| 4 | Crash at every import step (313.3) | Every step boundary passes, the seeded random run passes, the zombie worker writes nothing |
| 5 | journeylab `load` mode and the 200-user run here (297.5) | CPU ms per request and DB time per request are known, and give the size of the machines to rent |
| 6 | The rented run (297.5) | Pass criteria below are met for 30-60 minutes, or the run names the bottleneck |
| 7 | Grow only what the run names (313.4) | Each step is taken only when its trigger is measured |

## Phases

### 1. Off-box backups and point-in-time recovery (75.1.1, 290.2)

The advisor puts this first and so does this plan. A replica copies a mistaken delete; only a
backup with WAL undoes it (DDIA ch. 6: a replica is not a backup; SRE ch. 26 on data integrity).
290.2's host side is ready, and the owner runs its one command; 75.1.1 needs the owner's bucket.
Planning targets to approve: data loss at most 15 minutes (recovery point) and the service back
within 4 hours of losing the droplet (recovery time). The restore drill (75.1.7) records the real
values; they are targets, not promises.

### 2. The report catalog on 2G (297.8)

Today `apiMiddleware` sends `Cache-Control: no-store` on every API answer, and four places fetch
`GET /api/v1/reports/catalog` (462 KB, 53 KB gzipped) on their own.

- **Server** (`internal/server/handlers_report_engine.go`): build the body as now, hash it, send
  `ETag: W/"<hash>"` and `Cache-Control: private, no-cache`. If `If-None-Match` matches, answer
  304 with no body. The handler's header replaces the middleware's `no-store`. A weak ETag
  because the compression layer changes the bytes on the wire. Put the hash-and-304 step in one
  shared response writer beside the ones in `apierror.go`, so the next large list reuses it.
- **Why it is safe:** with `no-cache` the browser asks every time, and the server answers 304 only
  when the catalog it built for *this* user, tenant and role hashes the same. A module change, a
  role change or a release changes the body and so the ETag. Nothing has to be invalidated.
- **Browser:** one loader in a shared module keeps one promise per page; the four callers
  (`view-reports.js`, `view-dashboards.js`, `shortcuts.js`) use it. A failed fetch clears the
  promise so the next call retries. Logout and tenant switch clear it. `apiFetch` does not pass
  `cache: 'no-store'` for this path, so the browser sends `If-None-Match` by itself.
- **Check:** the compression middleware must pass a 304 through with no body and keep the ETag.
- **Not now:** caching the built body in server memory. Add it only if building the catalog shows
  up in the CPU profile of phase 5.

### 3. The import slowdown (313.2)

What happened (297.2.5, 2026-11-27): the commit fell from 2,900 rows a second to 230 because the
table's figures still said the new doctype had no rows, so the planner chose the doctype index and
each 200-id lookup read every row written so far (49 s each). The fix in the working tree runs
`ANALYZE` at 20,000 rows and at each doubling.

- **Is the doubling right?** Yes, as a safety net. Between two runs the real count is at most
  about twice the recorded one, so the estimate is never off by much more than 2x. The advisor's worry
  that the gap grows is true in rows but not in ratio, and the ratio is what the planner uses.
- **Better: take the choice away from the planner.** Seven lookups ask
  `doctype = $1 AND id = ANY($2)`: `engines/import.go:515, :599, :699, :795`,
  `engines/import_staging_bulk.go:1337, :1654` and `engines/import_staging.go:694`. `id` is the
  primary key and unique in the tenant's schema. Asking by `id = ANY($2)` alone leaves one
  sensible plan, a primary-key probe per id, whatever the figures say. One shared helper reads
  `id, doctype, deleted_at` (and `data` where needed) and filters in Go; reconcile uses
  `count(*) FILTER (WHERE doctype = $1 AND deleted_at IS NULL)`. The same read also shows an id
  already held by another doctype or by a deleted record, which today's query cannot see.
- **Not adopted:** the advisor's `(tenant_id, doctype, id)` index (each tenant has its own schema
  and there is no `tenant_id` column); extended statistics (they fix estimates for columns that
  move together, not stale counts); per-table autovacuum settings for now (only if a measured
  lookup still drifts after the change).
- **Proof:** a test switches autovacuum off on a scratch table, inserts a new doctype after
  `ANALYZE` and checks that `EXPLAIN` of the helper's query uses `documents_pkey`. Then the
  1-crore run again: the commit rate must stay flat from the first chunk to the last.
- **Order:** 297.2.5's change to the same file is uncommitted from another session. This waits
  until it ships.

### 4. A crash at every import step (313.3)

Today's tests stop a job mid-read and mid-commit (`TestImportBulkServerDiesMidSlice`). Validate
and the gaps between steps are not covered, and the real kills in 297.2.5 were timed by hand.

- **Already right:** the rows and their staging status commit in one transaction
  (`writeImportChunk` marks them `Committed` in the same transaction). The saved cursor is only a
  shortcut, so a crash after the commit and before the cursor save cannot write a row twice. The
  test must prove this window.
- **One hook.** A package-level test seam beside `importSliceChunks`: a func variable, nil in
  production, one nil check per chunk, called with the step and the point. Points: before and after each
  chunk of stage, validate, commit and reconcile; after the commit transaction but before the
  cursor save; between steps; after the last chunk but before the batch is marked complete.
- **Table test.** For each point, the hook stops the job, the test marks its claim dead the way
  the `dies()` helper does, and the job restarts. After every restart: row count, distinct ids,
  quantity and value totals match a clean one-pass run, and reconcile balances.
- **Seeded random run.** Picks several points in one import from a seed, prints the seed, and
  replays exactly with `-seed`. Cheap and repeatable (DDIA ch. 9).
- **Zombie worker.** The hook pauses worker A past its claim; worker B takes over and finishes;
  A wakes and must write nothing (the job and commit claims are checked by `job_id`, which acts
  as DDIA's fencing token). A paused process is the crash a kill does not test.
- **One real kill.** A small watcher in `cmd/journeylab` polls the job's step through the API and
  kills the server it started when the step reaches validate, then restarts it and checks the
  totals. This proves the in-process tests match a real dropped connection.
- **Separate:** a full droplet loss is phase 1's restore drill, not a worker crash.

### 5. The load test (297.5)

**What 1 lakh means** (taken from the owner's problem statement): 1 lakh named users, 10,000
working at once, a step every 10-30 seconds, about 500-1,000 requests a second.

**The `load` mode in `cmd/journeylab`:**

- **Fixed arrival rate.** Each virtual user's next step is scheduled from the last scheduled
  time, and latency is measured from the scheduled time. A slow reply must not delay the next
  start, or the test hides its own queue (DDIA ch. 2: coordinated omission).
- **Histograms, not averages.** Log-bucketed counts per step, which add across generators;
  percentiles are read from the sum, never averaged (DDIA ch. 2). Page-level time, not only
  per endpoint: a page with four calls is as slow as its slowest call.
- **CPU per request.** Server and Postgres CPU seconds divided by requests. SRE ch. 21: requests
  a second is a poor capacity unit; the cost per request is what sizes a machine.
- **Retries counted.** A run that passes only by retrying is a failed run.
- **Login limiter.** The limiter allows 5 sign-ins a minute per address. On the rented server the
  generator's address goes in `TRUSTED_PROXY_CIDRS` and each virtual user sends its own address in
  `X-Forwarded-For`, so the real limiter still runs. No code change.

**Data:** `cmd/synthtenant` (47.9.1) builds one tenant per run, so run it with several shapes and
seeds for skew: a few large, many small. The load mode adds the 1 lakh named users across them. Each tenant is a schema, so a realistic schema count also tests catalog memory
per connection and migration time. One tenant of 1 lakh users would hide both.

**Mix**, to be confirmed from production's access log (the owner runs a read-only script): mostly
lists, search and item pick; sales; a smaller share of PO, GRN and approvals. Separate scenarios:

- a **morning login spike**: 10,000 sign-ins in a few minutes at full password cost;
- an **import during sales**: the 1-crore file committing while tills sell;
- **ramp and spike**, then back to normal load, checking it recovers (SRE ch. 22);
- **past the limit**: raise the rate until it fails, and check it fails cleanly with 429/503 and
  recovers, not by timing out everything (SRE ch. 22).

**Pass criteria** for 30-60 minutes at the target rate (the advisor's numbers):

- under 0.5% failed requests and no dropped arrivals;
- reads p95 under 500 ms, ordinary writes p95 under 1 s; p99 recorded;
- no missing or doubled posting (journeylab's books check) and no row from another tenant;
- the generator under 70% CPU, so it is not the limit.

The existing per-endpoint budgets of 47.9.4 stay the gate at normal load.

**Sizing.** From the 200-user run: needed cores = requests a second × CPU ms per request ÷ 1,000
÷ 0.6 (60% busy at peak). The same for Postgres. That gives the rented server's size. Rent the
generator and the server in the droplet's region for a few hours, never point the run at
production, and delete both after.

### 6. What to add first (313.4)

| Step | Add | Trigger (measured in phase 5 or in production) |
|---|---|---|
| 0 | Nothing new; the pool is already capped (50 open, 25 idle in `db/db.go`) | - |
| 1 | Off-box backups and point-in-time recovery | Now (phase 1) |
| 2 | A bigger droplet | CPU or memory over budget at target load. Confirm today's size first (recorded 2026-10-19 as 1 vCPU). |
| 3 | Postgres on its own machine | Postgres and the app compete: CPU steal, I/O wait, or the till slowing while an import or report runs |
| 4 | Imports and jobs in their own process | Imports raise till p95 past budget. Needs a worker-only start mode, which does not exist yet; a lower CPU weight for the job pool is the cheaper first try |
| 5 | A second app server and a load balancer | App CPU saturated after step 2, or one app machine must be able to fail. First: the login and address limiter (`globalLimiter`) moves to Postgres; the per-tenant concurrency cap can stay per server with its limit split |
| 6 | PgBouncer in transaction mode | Servers × 50 connections pass Postgres's `max_connections`. Every `search_path` change is already `SET LOCAL`, so transaction mode works |
| 7 | Read replicas | A measured read load the primary cannot carry. Screens read their own writes, so only reports could go there (26.10.6) |

A standby database is a separate choice: it buys recovery in minutes instead of hours. Buy it only
if the approved recovery time is shorter than the restore drill can meet, and only with fencing
and a rehearsed failover. An unfenced failover can lose acknowledged writes (DDIA ch. 6).

## Not adopted

| Advisor's proposal | Instead | Why |
|---|---|---|
| k6 | journeylab `load` mode | No new tool (Article 1); journeylab already checks the books |
| `max-age=300` on the catalog | `private, no-cache` and an ETag | Shared till PCs |
| `(tenant_id, doctype, id)` index | Lookup by primary key | One schema per tenant, no `tenant_id` column |
| Per-table autovacuum tuned for the import | Lookup by primary key, ANALYZE at doubling kept | Removes the cause; tune only if a measurement still asks |
| Temporary id table and join for large id sets | Keep 200-id batches | With a key probe per id, 200 ids is small |
| Partitioning | Not now | No measured query or upkeep problem needs it (the advisor agrees) |

## Owner actions

1. Approve the decisions (or "all recommended"), and say whether journeylab or k6.
2. Run 290.2's command and create 75.1.1's bucket.
3. Approve the recovery targets: 15 minutes of data, 4 hours to recover.
4. Confirm the droplet's current size.
5. When phase 5's local run is done: rent one generator and one server for a few hours.

## Guardrails

- First principle: phase 5's local run is 200 users, never 10,000, and runs only when no other
  session is building. The full run goes on rented machines.
- Article 5: no step changes client data. Import tests run on scratch schemas with cleanup.
- 297.2.5's ANALYZE change and the `X-Tenant-ID` change in `cmd/journeylab` are uncommitted work
  from another session; nothing here edits them before they ship.

## Sources

- The advisor's plan: all five decisions, the pass criteria, the recovery targets.
- SRE: ch. 21 (overload, cost per request, retry budgets), ch. 22 (load tests, cascading failure,
  recovery), ch. 24 (idempotent jobs), ch. 26 (data integrity, restore). The PDF read was the
  Russian edition.
- DDIA 2nd ed.: ch. 2 (percentiles, coordinated omission, scaling up first), ch. 5 (idempotent
  steps), ch. 6 (replicas are not backups, failover losing writes), ch. 9 (fault injection,
  deterministic simulation, process pauses, leases and fencing tokens).
