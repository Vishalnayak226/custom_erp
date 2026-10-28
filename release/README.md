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
| **`Deploy.cmd`** | Puts the released commit on production (app and minn): lists any change to client data and waits for you to type YES, takes a backup (checked against its sha256) and a rollback copy, deploys, checks every site (each gets up to 90 s after the restart - Caddy answers 503 for the first seconds, 0.9.0). A few minutes. |
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
| G0a Lock | one release at a time: `reports\release.lock` (pid + start time) taken atomically, freed on every exit; a lock whose process is gone is taken over | 2026-10-26: two queued runs went G0-G10 side by side and each stopped at G11 on the other's `index.lock` |
| G0 Preflight | on main, nothing staged, not behind origin, tree quiet 10 min, Postgres up | 2026-10-18/19: sessions still writing files during a release |
| G1 Snapshot | `%TEMP%` worktree at HEAD + shipped files; held paths left out | half-built report catalog nearly shipped (0.2.0) |
| G2 Line endings | LF for text, never binaries, `i/mixed` manifests or `eol=crlf` files | `git apply` writes CRLF; four PNGs nearly corrupted (0.3.0) |
| G3 gofmt | formats files whose HEAD copy was clean | 0.2.0 / 0.4.0 |
| G4 Cache versions | bumps `VIEW_MODULE_VERSION` + `app.js?v=`, `styles.css?v=`, `qz-print.js?v=` | browsers kept old screens after a deploy (Stage 53.17); `qz-print.js` changed in 06547bb with no new `?v=` (Stage 137) |
| G4b Release number | reads the live number, applies the feat→MINOR / docs→PATCH rule, writes `internal/server/VERSION`; Deploy ships that number | prod said 0.4.0 while VERSION and the manuals said 0.1.0 (2026-10-19) |
| G4c Budget | release budget (cold core incl. `view-pos.js`) judged right after G4, before the slow gates | 0.11.0 stopped at G8 at 5:52 for 983 bytes; G4-final files make it exact this early |
| G4d PrintBridge download | `public/downloads/printbridge-setup.exe` + `printbridge-version.txt` present, and the version equals `../PrintBridge` `BuildVersion` | the ERP serves the installer and offers updates from that file (Stage 137.4) - a stale one would be offered to every PC |
| G5 Catalog spec | regenerates `report_catalog_spec_generated.go` if stale | 75.3 |
| G6 Attack surface | `surfacescan` from the release set | manifest listed held files (0.2.0) |
| G6b Security impact | `securityimpact`: last release's manifest vs the new one into the report - routes and their sign-in level, jobs, flags, outbound calls, dependencies; high-risk lines first (read, not a stop) | Stage 49.17.1: nobody saw what a release added to the surface |
| G7 Docs | KB, graphify + brain (new files filed beside a sibling), register, no broken links | unclaimed file broke the brain (0.4.0) |
| G8 Static | build, vet and the two test programs compiled side by side; `node --check` | `view-pos.js` counts in the cold core (0.4.0) |
| G9 Data scan | data statements in new migrations, `-migrate` backfills, new background jobs, and **sweep deletes** - a background sweep that starts deleting a table it never touched, or deletes differently (also listed at Deploy D1, which then waits for YES) | cost centers, attributes, reason codes, stock defaults; 0.18.0: the retention sweep gained three tables with no migration to show for it |
| G9b Deploy scripts | a changed `deploy/*.sh` / `*.service` is named in the report, with whether `deploy/deploy.ps1` ships it (it ships a fixed list) | 0.15.0 changed `restore_drill.sh`; the server kept the August copy |
| G10 Fresh DB | migrate from empty; then the **whole** suite every release, side by side: each job on its own copy of that database (engines in 3 groups balanced by last run's times, server, small packages); tests, examples and fuzz seeds as `go test` runs them; every test must report a result (none dropped by the split); failures rerun twice (pass = flaky), more than 3 flaky stops the release | from-zero failures; clock-skew flake (0.2.0); one package at a time took ~15 min (109.9); a split that skipped 2 examples (109.10) |
| G11 Commit | only if HEAD, index and every snapshot file are unchanged; tree must equal the tested tree | other sessions committing mid-release |
| G12 Sync | committed versions of regenerated files back into the shared tree where untouched | reverse diffs left behind after 0.4.0 |
| G12b Cache lines | a cache-version bump (G4) in a file the tree keeps its own copy of - held, or edited since the snapshot - is carried into that copy, that line only | 0.17.0 bumped `VIEW_MODULE_VERSION` to 66 in a held `app.js`; the working copy stayed at 65 (erp-d1 found it) |
| G13 Push | `git push origin main` | Deploy refuses anything not on origin |
| D1 Keys (Deploy) | every secret key the build reads (JWT_SECRET, AUDIT_ARCHIVE_KEY and each `loadVersionedKeyring` name in engines/) is set in `/etc/erp/erp.env` (names only are read) or already has its key file on the server; otherwise the deploy stops; a key held only in a file is named with "add NAME_1, never plain NAME" | 0.13.0: no STORED_VALUE_KEY, so the server generated one and sealed the gift cards with a key outside every backup (135.14) |
| D0b Security floor (Deploy) | `release/security-floor.txt` is the oldest release with no known, fixed vulnerability (raise it in the release that fixes one; `TestSecurityFloorIsWellFormed` keeps it parseable and never above `VERSION`). The report says when production starts below it, and when a rolled-back deploy leaves it below - with the containment to use (Activity Log → Security) until a fixed build is live | Stage 49.17.6: an automatic rollback after a failed security-fix deploy would put the vulnerable build back with nothing said |

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
