---
doc_id: DOC-117E4D23E1
title: Release, Deploy, Cleanup - the three clicks
type: reference
status: draft
owner: engineering-owner
approvers: [documentation-maintainer, engineering-owner]
audience: [maintainers, engineering-owner]
applies_to: source documentation; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-10-19
review_by: 2027-01-19
supersedes: none
superseded_by: none
verification_scope: release tooling built and run live on 2026-10-19 (release 0.5.0)
---

# Release, Deploy, Cleanup - the three clicks

Everything needed to ship the ERP lives in this folder (Stage 109). Double-click
in this order:

| Click | What it does |
|---|---|
| **`Release.cmd`** | Runs every gate below on a snapshot of the tree, then commits and pushes - only if all pass. About 15 minutes. |
| **`Deploy.cmd`** | Puts the released commit on production (app and minn): lists any change to client data and waits for you to type YES, takes a backup (checked against its sha256) and a rollback copy, deploys, checks every site. A few minutes. |
| **`Cleanup.cmd`** | Cleans the server now. Not needed day to day: the server cleans itself every night at 03:30. |

Each run writes a report to `reports\` (kept on this machine, never committed).
If a click stops, its last line says why.

## The folder

```
release\
  Release.cmd  Deploy.cmd  Cleanup.cmd   the three clicks
  README.md                              this page
  config.json                            server, sites, local Postgres, Go
  hold.txt                               work that must not ship yet (you create it; not committed)
  reports\                               one report per run, last-release.json (not committed)
  scripts\
    Invoke-Release.ps1                   gates G0-G13
    Invoke-Deploy.ps1                    the deploy click (calls deploy\deploy.ps1)
    Invoke-Cleanup.ps1                   runs deploy/server_cleanup.sh on the server now
    ReleaseCommon.ps1                    shared: config, the client-data scan
```

The server side stays in `deploy\`, next to `backup.sh`, because that folder is
what lands in `/opt/erp/deploy` on the box: `server_cleanup.sh` (the cleanup) and
`install_cleanup_cron.sh` (its 03:30 root cron, refreshed by every deploy), and
`erp.service` (the systemd unit) with `install_service_unit.sh`, which every
deploy runs: it installs the unit whenever it differs from the live one and puts
the old one back by itself if the ERP is not healthy on it. The Deploy click
(and `-Check`) says whether the live unit matches the repo.

From a terminal the same scripts take options:

```powershell
pwsh release\scripts\Invoke-Release.ps1 -DryRun       # every gate, nothing committed
pwsh release\scripts\Invoke-Release.ps1 -NoPush       # gates + local commit
pwsh release\scripts\Invoke-Deploy.ps1 -Check         # what a deploy would change; nothing done
pwsh release\scripts\Invoke-Cleanup.ps1 -DryRun       # what the cleanup would remove
```

## Holding work back

Add the paths of unfinished work to `hold.txt`, one per line (globs work; a
trailing `/` holds a whole folder; `#` starts a reason). Release leaves those
paths out and lists them in its report. Remove the lines when the work is done.

## The release gates, and what taught each one

A release problem these did not catch is a missing gate: add it to
`scripts\Invoke-Release.ps1` and a row here in the same session.

| Gate | Checks or does | Learned from |
|---|---|---|
| G0 Preflight | on main, nothing staged, not behind origin, tree quiet 10 min, Postgres up | 2026-10-18/19: sessions still writing files during a release |
| G1 Snapshot | `%TEMP%` worktree at HEAD + shipped files; held paths left out | half-built report catalog nearly shipped (0.2.0) |
| G2 Line endings | LF for text, never binaries, `i/mixed` manifests or `eol=crlf` files | `git apply` writes CRLF; four PNGs nearly corrupted (0.3.0) |
| G3 gofmt | formats files whose HEAD copy was clean | 0.2.0 / 0.4.0 |
| G4 Cache versions | bumps `VIEW_MODULE_VERSION` + `app.js?v=`, `styles.css?v=` | browsers kept old screens after a deploy (Stage 53.17) |
| G4b Release number | reads the live number, applies the feat→MINOR / docs→PATCH rule, writes `internal/server/VERSION`; Deploy ships that number | prod said 0.4.0 while VERSION and the manuals said 0.1.0 (2026-10-19) |
| G5 Catalog spec | regenerates `report_catalog_spec_generated.go` if stale | 75.3 |
| G6 Attack surface | `surfacescan` from the release set | manifest listed held files (0.2.0) |
| G7 Docs | KB, graphify + brain (new files filed beside a sibling), register, no broken links | unclaimed file broke the brain (0.4.0) |
| G8 Static | build, vet, release budget and the two test programs compiled side by side; `node --check` | `view-pos.js` counts in the cold core (0.4.0) |
| G9 Data scan | data statements in new migrations, `-migrate` backfills, new background jobs | cost centers, attributes, reason codes, stock defaults |
| G10 Fresh DB | migrate from empty; then the **whole** suite every release, side by side: each job on its own copy of that database (engines in 3 groups balanced by last run's times, server, small packages); tests, examples and fuzz seeds as `go test` runs them; every test must report a result (none dropped by the split); failures rerun twice (pass = flaky), more than 3 flaky stops the release | from-zero failures; clock-skew flake (0.2.0); one package at a time took ~15 min (109.9); a split that skipped 2 examples (109.10) |
| G11 Commit | only if HEAD, index and every snapshot file are unchanged; tree must equal the tested tree | other sessions committing mid-release |
| G12 Sync | committed versions of regenerated files back into the shared tree where untouched | reverse diffs left behind after 0.4.0 |
| G13 Push | `git push origin main` | Deploy refuses anything not on origin |

Deploy (`scripts\Invoke-Deploy.ps1`) refuses a commit that is not on origin or
that Release did not pass, and only ever deploys the number Release wrote.

## Server cleanup

`deploy/server_cleanup.sh` keeps the newest 3 rollback copies, deletes failed
builds older than 7 days, control-character file names and stale `*.new`, runs
`apt-get clean`, holds the journal at 100 MB, trims the failed-login log and keeps
snapd out. It alerts through `OPS_ALERT_WEBHOOK_URL` above 80% disk and logs one
line a run to `/var/log/erp-cleanup.log`. It never touches backups, the live or
previous build, `db/` or PostgreSQL. SSH bots are blocked by sshguard (installed
2026-10-19; SSH is key-only).
