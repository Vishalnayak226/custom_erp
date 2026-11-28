---
doc_id: QA-JOURNEYLAB-001
title: Journey lab, soak run and epic ledger
type: procedure
status: draft
owner: qa-owner
approvers: [qa-owner, engineering-owner]
audience: [maintainers, qa]
applies_to: scratch copies only, never client data
authority: source
confidentiality: internal
last_verified: 2026-11-28
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: journeys, a 15-minute soak and an epic compare run on a scratch copy on 2026-11-10 (erp-5c); a 200-user load run on a scratch copy on 2026-11-28 (erp-59); the 24-hour soak and the rented full load run not run
---

# Journey lab, soak run and epic ledger

Three tools that prove the product on a scratch copy, never on client data.

- `cmd/journeylab journey` drives seven business journeys over real HTTP as the proper role and reconciles the books after each (47.9.2, 47.17.2).
- `cmd/journeylab soak` repeats a light journey for a while with disturbances (47.9.5).
- `cmd/journeylab load` puts many named users on a fixed schedule and sizes the server from what each request costs (297.5).
- `cmd/epicledger` records measured before/after figures per epic (47.18.1).

## Before you run

- Build the server: `go build -o server.exe ./cmd/server` and the lab: `go build -o journeylab.exe ./cmd/journeylab`.
- Use a scratch database that has been migrated (`server.exe -migrate`). The lab refuses a database whose name does not look like scratch, test or bench unless you pass `-allow-db <name>`, and refuses a non-local server or host unless `-allow-remote`.
- Start the scratch server with the same `JWT_SECRET` the lab has. The engines read it when the process starts, so export it before both. Also export `ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1` so nothing is sent outside.
- Login is limited to five a minute; the lab waits when it has to.

## Journeys

```
journeylab journey -base-url http://localhost:8730 -db-url <scratch url> -allow-db <name> \
  -pg-bin <postgres bin> -server-bin <server.exe> -server-pid <pid> -out <folder> \
  [-journeys J1,J2,...] [-browser-evidence <browser suite json>]
```

J1 first sale, J2 procure to pay, J3 order to cash, J4 warehouse, J5 period close, J6 migration import, J7 backup and restore drill. Each step records its role, latency, queries, database growth and server memory. After each journey the lab checks ledger totals, voucher balance, no negative stock, the expected stock movement and the engine reconciliation and audit checks. Output: `journeys-<stamp>.json` and `.md` in `-out`. The `from_browser_suite` section carries the accessibility and help-usability results, copied unchanged from the browser suite; without a file it says `not_provided`.

`-inject-mismatch` adds an unbalanced ledger row first; the run must come out red. Use it to prove the checks bite.

Database figures come from `pg_stat_statements` when the database has it, otherwise from `pg_stat_database`, and the evidence says which.

A release uses: `journeylab journey ... -out release/evidence-src` and takes `overall` from the `journeys-<stamp>.json` it wrote (`pass` or `fail`; exit code 0 only on pass).

Add `-record-gate .` (the repo root) to record the run as the release gate's `journey` evidence (`release/evidence/journey.json`). Only a run of all seven journeys without `-inject-mismatch` counts; it is a pass only if every journey passed.

A release with tests on GitHub runs this itself: `release.yml`'s evidence job runs all seven journeys against the candidate with `-server-bin` and `-server-pid` (so J7 can verify the restore) and `-record-gate .` (47.17.2). See [the release-candidate gate](release-candidate-gate.md).

## Soak

```
journeylab soak -duration 20m -server-bin <server.exe> -pg-bin <postgres bin> -out <folder>
```

It creates its own private Postgres cluster on a free port, a scratch database and a server, so it can restart them freely. It never touches the shared Postgres. Disturbances at set points of the run: hard server kill and restart, outbox drain, token and rate-limiter churn, database restart, month and time-zone boundary (database level; there is no application clock seam), and an import killed while it checks its rows (313.3: a 60,000-row file goes up as a resumable upload, the server is killed hard once the check is under way, and the restarted server must carry the import on by itself within the job lease, 5 minutes, then commit and reconcile it with every valid row written once and one ImportJob; the run lasts that much longer). A till that a broken cycle left open (a database restart can land mid-close) is closed at its expected cash before the next cycle opens its own. It ends with a full reconciliation and a 24-hour disk projection. Output `soak-<stamp>.json` and `.md`.

The 24-hour run is a separate decision and is not done. Its command:

```
journeylab soak -duration 24h -server-bin <server.exe> -pg-bin <postgres bin> -out <folder>
```

## Load

```
journeylab load -base-url <server> -db-url <scratch url> -server-pid <pid> -out <folder> \
  [-users 200] [-ramp 1m] [-duration 5m] [-every-min 10s] [-every-max 30s] \
  [-timeout 30s] [-seed 1] [-xff=true] [-generator <name>]
journeylab load -merge a.json,b.json -out <folder>
```

**What it does**

- Every fifth user is a store manager at the warehouse; the rest are cashiers.
- Each cashier gets its own shop (`JL-LS00001` ...), because the server allows one open till per location. The lab creates the users and shops, opens a till in each shop, and stocks each shop through a purchase order and a goods receipt.
- Users sign in spread over `-ramp`, which is the morning login spike. Then each user opens a page every `-every-min` to `-every-max` for `-duration`.
- **Cashier pages:** item search, item pick, sale (quote and checkout, one unit), sales history, report catalog.
- **Manager pages:** item list, item search, approvals, transfer draft, sales history, report catalog.

**How it measures**

- **Fixed schedule.** A user's next page is due a set pause after the last one was *due*, whatever the reply took. A slow server therefore meets the same traffic and cannot slow the test down. Time is counted from when a page was due, so queueing shows. A page that starts over a second late counts as late.
- **No retries.** A call is never retried on a 429 or a 5xx; it counts as failed under its code. After a 401 the user signs in again once.
- **Own address per user.** Each user sends its own address in `X-Forwarded-For`, from the 198.18.0.0/15 test range, so the real login limiter sees 200 people, not one. Start the server with `TRUST_PROXY=1`; without it every sign-in after the fifth gets a 429 and the lab stops and says so. A generator on the same machine or a private network is already a trusted hop. One that reaches the server over a public address must also be listed in `TRUSTED_PROXY_CIDRS`.
- **Cost per request.** With `-server-pid`, the lab reads the server's CPU ms per request over the steady part (sign-in is counted on its own). It reads database ms per request from `pg_stat_statements`.
- **Sizing.** From those costs it works out the cores needed at 500 and 1,000 requests a second, and for 10,000 users at the run's pace, so a machine runs at 60%: cores = requests a second × ms ÷ 1,000 ÷ 0.6.
- **Books.** After the run it reconciles the books and checks that the shops' stock fell by exactly the units sold. A sale that got no answer may or may not have posted. Each one widens the allowed range by one unit and is listed as unknown.

**Output:** `load-<stamp>.json` and `.md` in `-out`.

**Laptop run, 2026-11-28 ([evidence](../qa/journeylab/load-20261128-042044.md))**

- 200 users, a 2-minute sign-in window, then 10 minutes of work.
- Pass: 0 of 6,758 pages failed; reads p95 118 ms, writes p95 684 ms.
- 12.15 server CPU ms and 5.08 database ms a request.
- 10,000 users at that pace: about 700 requests a second, 14 server cores and 6 database cores.

**Verdict**

- **Pass** when, over the whole run:
  - under 0.5% of pages failed;
  - reads p95 is under 500 ms;
  - writes p95 is under 1 s;
  - the books tie.
- **Invalid** when the generator itself went above 70% CPU, because the figures then measure the generator, not the server.
- **Exit code:** 0 on pass, 1 on fail or invalid, 2 when setup failed.

**Several generators**

- Run `load` on each machine with its own `-generator` name and a different `-seed`, against one server.
- Copy the JSON files to one place and run `-merge`. The merge adds the pages and histograms bucket by bucket, so the percentiles are those of all requests together. It takes the server's CPU and database time once, as the largest any generator measured.
- Each percentile is read from a log histogram. It is at most 5% above the exact value, never below it.

**The full run (owner's decision, 297.5)**

- Two rented machines: one server and database, one or two generators. The server runs with `TRUST_PROXY=1` and the generators' addresses in `TRUSTED_PROXY_CIDRS`.
- 30-60 minutes at the full user count, with a morning login spike, an import during sales, and many tenant schemas.
- It passes on the same budgets, plus no posting missing or doubled and no row from another tenant.
- The laptop run checks only one tenant. The cross-tenant check and the many-schema run belong to that full run.

## Epic ledger

```
epicledger record  -id <id> -label <text> -root <tree> [-binary <server.exe>] [-budget <releasebudget.json>] \
                   [-journeys <journeys.json>] [-soak <soak.json>] [-ledger docs/qa/epic-ledger.json]
epicledger compare -before <id> -after <id> [-md <file>]
```

The ledger is `docs/qa/epic-ledger.json`. Figures it cannot measure are written `not_measured` with the reason, never as zero.

When it is recorded (162.21, approved 2026-11-11):

- **Per epic, by hand.** Before an epic starts, record `before-<epic>`. When it is built, record `after-<epic>` with the journeys and soak evidence if they were run, then `compare` the two and put the table in the epic's ledger section.
- **Per release, by the release.** Gate G4f records `release-<version>` from the G4c budget report, so every release leaves a point to compare with. It is warn-only: if the entry cannot be written, the release prints a WARNING and goes on.

```
epicledger record -id before-stage-170 -label "before Stage 170" -root .
epicledger record -id after-stage-170 -label "after Stage 170" -root . -binary bin/erp-server.exe -journeys <journeys.json>
epicledger compare -before before-stage-170 -after after-stage-170 -md %TEMP%\stage170-footprint.md
epicledger compare -before release-<previous> -after release-<this>
```
