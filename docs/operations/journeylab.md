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
last_verified: 2026-11-10
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: journeys, a 15-minute soak and an epic compare run on a scratch copy on 2026-11-10 (erp-5c); the 24-hour soak not run
---

# Journey lab, soak run and epic ledger

Three tools that prove the product on a scratch copy, never on client data.

- `cmd/journeylab journey` drives seven business journeys over real HTTP as the proper role and reconciles the books after each (47.9.2, 47.17.2).
- `cmd/journeylab soak` repeats a light journey for a while with disturbances (47.9.5).
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

## Soak

```
journeylab soak -duration 20m -server-bin <server.exe> -pg-bin <postgres bin> -out <folder>
```

It creates its own private Postgres cluster on a free port, a scratch database and a server, so it can restart them freely. It never touches the shared Postgres. Disturbances at set points of the run: hard server kill and restart, outbox drain, token and rate-limiter churn, database restart, month and time-zone boundary (database level; there is no application clock seam). It ends with a full reconciliation and a 24-hour disk projection. Output `soak-<stamp>.json` and `.md`.

The 24-hour run is a separate decision and is not done. Its command:

```
journeylab soak -duration 24h -server-bin <server.exe> -pg-bin <postgres bin> -out <folder>
```

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
