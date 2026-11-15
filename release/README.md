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
| **`Cleanup.cmd`** | Cleans the server now (`Cleanup.cmd -Deep` adds the deep pass). Not needed day to day: the server cleans itself every night at 03:30, and every 10 days a deep pass at 04:15. |

Each run writes a report to `reports\` (kept on this machine, never committed).
If a click stops, its last line says why.

## The folder

```
release\
  Release.cmd  Deploy.cmd  Cleanup.cmd   the three clicks
  README.md                              this page
  config.json                            server, sites, local Postgres, Go
  hold.txt                               work that must not ship yet (you create it; not committed)
  security-ack.txt                       your reason for each high-risk security change (G6b; not committed)
  reports\                               one report per run, last-release.json (not committed)
  scripts\
    Invoke-Release.ps1                   gates G0-G13
    Invoke-Deploy.ps1                    the deploy click (calls deploy\deploy.ps1)
    Invoke-Cleanup.ps1                   runs deploy/server_cleanup.sh on the server now
    ReleaseCommon.ps1                    shared: config, the client-data scan
  exceptions.json  gate-policy.json      open defects and their approvals; which release-candidate checks block
  evidence\                              dated proof of backup, outbox, rollback, budget and journey runs
```

The server side stays in `deploy\`, next to `backup.sh`, because that folder is
what lands in `/opt/erp/deploy` on the box: `server_cleanup.sh` (the cleanup) and
`install_cleanup_cron.sh` (its 03:30 and 04:15 root cron lines, refreshed by every deploy), and
`erp.service` (the systemd unit) with `install_service_unit.sh`, which every
deploy runs: it installs the unit whenever it differs from the live one and puts
the old one back by itself if the ERP is not healthy on it. The Deploy click
(and `-Check`) says whether the live unit matches the repo.

From a terminal the same scripts take options:

```powershell
pwsh release\scripts\Invoke-Release.ps1 -DryRun       # every gate, nothing committed
pwsh release\scripts\Invoke-Release.ps1 -NoPush       # gates + local commit (push it yourself; Deploy then takes it)
pwsh release\scripts\Invoke-Release.ps1 -Wait         # and wait for the GitHub run to pass and move main
pwsh release\scripts\Invoke-Release.ps1 -LocalTests   # build, vet, the full tests and the smoke run on this laptop, then push main
pwsh release\scripts\Invoke-Deploy.ps1 -Check         # what a deploy would change; nothing done
pwsh release\scripts\Invoke-Cleanup.ps1 -DryRun       # what the cleanup would remove
pwsh release\scripts\Invoke-Cleanup.ps1 -Deep -DryRun # the same with the deep pass, and what is growing
```

## Holding work back

Add the paths of unfinished work to `hold.txt`, one per line (globs work; a
trailing `/` holds a whole folder; `#` starts a reason). Release leaves those
paths out and lists them in its report. Remove the lines when the work is done.

## Where the tests run (item 177)

With `config.json` `"testsOn": "github"` (the default since 2026-11-14), the click
runs the quick gates, commits, and pushes the release commit to `candidate/<version>`,
not to main. `.github/workflows/release.yml` then builds, vets and migrates an empty
database on GitHub's machines and runs every test of that exact commit. Its first job
runs the guard tests (`guard-tests.txt`: tests that read the code and registers, in
seconds) and cancels the whole run when one fails (177.7). Then engines and
internal/server in 16 shards split by last run's times (`test-times.json`, the same
split as the laptop's G10), the other packages in one job. A failed test is rerun
alone twice; a pass then counts as flaky. Only when every job passes does it
fast-forward main to the commit. Deploy waits for that run (D0f), so it can only ship
a commit GitHub passed. `trial/<name>` branches run the same checks without moving main.

Do not push main yourself while a candidate run is open: the promotion would find main
moved and stop, and the release has to be cut again. `-LocalTests` (or `"testsOn":
"local"`) runs everything on the laptop as before and pushes main directly.

## The release gates, and what taught each one

A release problem these did not catch is a missing gate: add it to
`scripts\Invoke-Release.ps1` and a row here in the same session.

| Gate | Checks or does | Learned from |
|---|---|---|
| G0a Lock | one release at a time: `reports\release.lock` (pid + start time) taken atomically, freed on every exit; a lock whose process is gone is taken over | 2026-10-26: two queued runs went G0-G10 side by side and each stopped at G11 on the other's `index.lock` |
| G0 Preflight | on main, nothing staged, not behind origin, tree quiet 10 min, Postgres up | 2026-10-18/19: sessions still writing files during a release |
| G1 Snapshot | waits (up to 10 min) for the last release's code map redraw, which works in the same folder; `%TEMP%\erp_release` worktree at HEAD + shipped files (always that folder: Go's build cache is keyed by it, so a new folder each run recompiled everything - 177.5); held paths left out; with anything held, the guides / KB / manuals manifests start from HEAD (G7 regenerates them) | half-built report catalog nearly shipped (0.2.0); a manifest written with held work in the tree stopped G7 as "orphaned" (0.19.0, 2026-10-29) |
| G2 Line endings | LF for text, never binaries, `i/mixed` manifests or `eol=crlf` files (one `git check-attr` per 200 paths, not one per file) | `git apply` writes CRLF; four PNGs nearly corrupted (0.3.0); 71 s of per-file git calls (177.5) |
| G3 gofmt | formats files whose HEAD copy was clean (one `gofmt -l` per 200 files; only the files it names are checked against HEAD) | 0.2.0 / 0.4.0 |
| G4 Cache versions | bumps `VIEW_MODULE_VERSION` + `app.js?v=`, `styles.css?v=`, `qz-print.js?v=` | browsers kept old screens after a deploy (Stage 53.17); `qz-print.js` changed in 06547bb with no new `?v=` (Stage 137) |
| G4b Release number | reads the live number, applies the feat→MINOR / docs→PATCH rule, writes `internal/server/VERSION`; Deploy ships that number. The rule is in `internal/releasever`, so `cmd/releasever` builds without engines | prod said 0.4.0 while VERSION and the manuals said 0.1.0 (2026-10-19); compiling engines for it took 130 s of every click (177.5) |
| G4c Budget | release budget (cold core incl. `view-pos.js`, and the stripped Linux server binary built as CI builds it, limit 40 MiB) judged right after G4, before the slow gates | 0.11.0 stopped at G8 at 5:52 for 983 bytes; G4-final files make it exact this early. G4c never measured the binary, so CI was red on every push at the old 25 MiB limit while every release passed (177.4) |
| G4d PrintBridge download | `public/downloads/printbridge-setup.exe` + `printbridge-version.txt` present, and the version equals `../PrintBridge` `BuildVersion`; from 0.3.0 also `printbridge-update.json`, whose version, file name, size and SHA-256 match the served exe and which carries a signature | the ERP serves the installer and offers updates from that file (Stage 137.4) - a stale one would be offered to every PC; installed agents replace themselves from the update file (203.3), so one that does not match the exe stops every PC updating |
| G4f Footprint ledger | records entry `release-<version>` in `docs/qa/epic-ledger.json` (`cmd/epicledger record` with the G4c budget report). Warn-only: a failure prints WARNING and the release goes on | 162.21: the ledger was only recorded by hand per epic, so releases left no point to compare |
| G4e Compliance reviews | no row of `docs/security/compliance-control-matrix.json` is past its `review_by` date (`go test` with `ERP_COMPLIANCE_REVIEW_GATE=1` runs `TestComplianceMatrixReviewsAreNotOverdue`; an ordinary `go test` only logs OVERDUE). Fix: review the row with its owner, move `last_reviewed` and `review_by` | Stage 47.16.1a: an overdue review printed OVERDUE in every build and stopped nothing |
| G5 Catalog spec | regenerates `report_catalog_spec_generated.go` if stale; with tests on GitHub, only when the release changes its inputs (`docs/specs/reports/`, `cmd/gencatalog/`) - the GitHub run checks it every time (`gencatalog -check`) | 75.3; the regenerate compiled all of engines on every click (177.5) |
| G6 Attack surface | `surfacescan` from the release set | manifest listed held files (0.2.0) |
| G6b Security impact | `securityimpact`: last release's manifest vs the new one into the report - routes and their sign-in level, jobs, flags, outbound calls, dependencies; high-risk lines first. **Stops** while a high-risk line has no reason in `release/security-ack.txt` (git-ignored; one line each: the change as printed after `!`, then `\|` and the reason); the reasons go into the report | Stage 49.17.1: nobody saw what a release added to the surface; a high-risk change needed the owner's written reason (49.17.1, ask 187) |
| G7 Docs | KB, checklist module map (counts refreshed; same skeleton in tracker and archive, no open item archived), brain (every file filed in a region - new files beside a sibling - and the map drawn from the last release's code graph; the graph is rebuilt after the push, G13b), register, no broken links | unclaimed file broke the brain (0.4.0); tracker rearranged by module (2026-11-10); rebuilding the code graph took 3-8 min of every click (177.5) |
| G8 Static | with tests on GitHub: only `node --check` here, build and vet run in the GitHub run; otherwise build, vet and the two test programs compiled side by side; `node --check` | `view-pos.js` counts in the cold core (0.4.0) |
| G9 Data scan | data statements in new migrations, `-migrate` backfills, new background jobs, and **sweep deletes** - a background sweep that starts deleting a table it never touched, or deletes differently (also listed at Deploy D1, which then waits for YES) | cost centers, attributes, reason codes, stock defaults; 0.18.0: the retention sweep gained three tables with no migration to show for it |
| G9b Deploy scripts | a changed `deploy/*.sh` / `*.service` is named in the report, with whether `deploy/deploy.ps1` ships it (it ships a fixed list) | 0.15.0 changed `restore_drill.sh`; the server kept the August copy |
| G9c Cleanup settings ship Off | a setting the release adds whose key names a retention, cleanup, purge or expiry must default to `0` / empty, so a deploy never starts deleting client data; a reviewed exception marks its Key line `// G9c-ok: <why>` | 0.20.0 added `platform.import_file_retention_days` at 30: the hourly sweep deleted old import files and failed rows on app and minn until 0.21.0 set it to 0 |
| G10 Fresh DB | with tests on GitHub: only the split's times are refreshed from the last GitHub run (its `shard-*` artifacts) into `test-times.json`; the GitHub run does the rest. With `-LocalTests`: migrate from empty; then the **whole** suite every release, side by side: each job on its own copy of that database (engines in 3 groups balanced by last run's times, server, small packages); tests, examples and fuzz seeds as `go test` runs them; every test must report a result (none dropped by the split); failures rerun twice (pass = flaky), more than 3 flaky stops the release | from-zero failures; clock-skew flake (0.2.0); one package at a time took ~15 min (109.9); a split that skipped 2 examples (109.10) |
| G10 Guards first | the tests in `guard-tests.txt` (security-cost review, worker owners, report catalog spec, route capabilities, bypass, safe-override and egress lists, error catalog, personal-data register) run before the long tests, through `Invoke-GuardTests.ps1`: on GitHub as the first job, which cancels the run when one fails; with `-LocalTests` at the start of G10 on their own database copy. A listed test that no longer runs is a failure, so a rename cannot drop a guard | ask 174 lost about 40 min to nine such failures found at the end of the suite (177.7) |
| G10a Browser smoke | not run with tests on GitHub (it needs this laptop's Guide, Edge and dev data); with `-LocalTests`: Guide (`config.json` "guide", outside the repo) runs its `smoke` suite on the snapshot: admin and cashier1 signed in, the POS screen, one sale end to end (PO, GRN, sale, same-day return, numbers read back), the Sales Register showing that bill; migrations also run over a copy of the dev data (`custom_erp_smoke`, cloned once). Stops on an uncaught browser error, a failed request or 5xx, a step that could not be done, a PANIC row, a migrate or server-start failure, or a file the run wrote into the snapshot; number mismatches and other server error rows are warnings; skipped with a note without Guide, Node, Playwright or Edge (158.4) | a release can pass every test and still not open or sell in the browser |
| G10b Release-candidate | `cmd/releasegate`: one evidence record per candidate in `reports\` (base commit + tree, migrations with checksums, SBOM, config contract, surface diff, the G8/G10 results, `govulncheck`, exceptions, run evidence), JSON + Markdown. Evidence is written by `journeylab journey -record-gate .`, `node docs/qa/perf-budget.cjs --record-gate .`, `deploy/restore_drill.sh` (ingested at D5) or `releasegate record`. **Blocks** on: a malformed commit/tree, a migration without a checksum, no SBOM or licence, a build/vet/test not passed, an open P0, a P1 without a release-key signature, any expired exception, a missing `release/exceptions.json`. **Warns and records** (switch to block in `release/gate-policy.json`): missing, stale, failed or other-commit run evidence (backup/restore, outbox, rollback, browser budgets, journeys, race, dynamic), `govulncheck` findings or not run, high-risk surface changes. [Rules, signing a P1, recording evidence](../docs/operations/release-candidate-gate.md) | Stage 47.17.3 / 49.17.2: SBOM and exceptions existed nowhere in the release; first `govulncheck` run found 13 Go standard-library findings nobody had seen |
| G11 Commit | only if HEAD, index and every snapshot file are unchanged; tree must equal the tested tree | other sessions committing mid-release |
| G12 Sync | committed versions of regenerated files back into the shared tree where untouched | reverse diffs left behind after 0.4.0 |
| G12b Cache lines | a cache-version bump (G4) in a file the tree keeps its own copy of - held, or edited since the snapshot - is carried into that copy, that line only | 0.17.0 bumped `VIEW_MODULE_VERSION` to 66 in a held `app.js`; the working copy stayed at 65 (erp-d1 found it) |
| G13 Push | with tests on GitHub: `git push` of the release commit to `candidate/<version>`, the run's link in the report, and with `-Wait` `gh run watch` until it passes and main moves; with `-LocalTests`: `git push origin main` | Deploy refuses anything not on origin; 0.24.0 took 52 min on the laptop, 35 of them the tests (177) |
| G13b Code map redraw (after the click) | `Invoke-BrainRedraw.ps1` starts in the background at below-normal priority with 2 workers: rebuilds the code graph in the release worktree (`graphify_update.py`, which caches graphify's path lookups: 466 s to 130 s, same graph), redraws `docs/brain`, puts the three outputs and `status.json` in `%TEMP%\erp_release_brain`, keeps the graph for the next G7 and removes the worktree. A failure is a note at D6, never a stop | 177.5: the graph rebuild was the slowest part of the click |
| D0a Records after the release (Deploy) | when `origin/main` is ahead of the last passed Release run only by commits to `docs/micro_checklist.md`, `docs/project_ledger.md`, `docs/ai_handover.md` and the code map D6 brings in (`docs/brain/brain-map.md`, `docs/brain/brain.html`, `docs/generated/brain-manifest.json`; the server never reads them), Deploy ships the release commit itself; a commit touching any other file still stops the deploy | 0.23.0: the records were committed while the deploy waited for the owner, and Deploy refused the released build |
| D0f GitHub run (Deploy) | when the last release's tests run on GitHub and its commit is not on `origin/main` yet, Deploy finds that `release.yml` run, waits for it (`gh run watch`), stops if it failed or main did not move, and fetches again; `-Check` only reports a run still going. Local commits of only the working records (D0a's list) no longer stop it | 177.3: with the tests on GitHub, main moves only after the run, so Deploy has to wait for it |
| D1 Keys (Deploy) | every secret key the build reads (JWT_SECRET, AUDIT_ARCHIVE_KEY and each `loadVersionedKeyring` name in engines/) is set in `/etc/erp/erp.env` (names only are read) or already has its key file on the server; otherwise the deploy stops; a key held only in a file is named with "add NAME_1, never plain NAME" | 0.13.0: no STORED_VALUE_KEY, so the server generated one and sealed the gift cards with a key outside every backup (135.14) |
| D1b Alerts (Deploy) | `OPS_ALERT_WEBHOOK_URL` is set in `/etc/erp/erp.env` (name only). A warning, not a stop: unset, panics, failed or stale backups and error bursts reach only the server log (158.2) | production ran without alerts and nobody checked |
| D0b Security floor (Deploy) | `release/security-floor.txt` is the oldest release with no known, fixed vulnerability (raise it in the release that fixes one; `TestSecurityFloorIsWellFormed` keeps it parseable and never above `VERSION`). The report says when production starts below it, and when a rolled-back deploy leaves it below - with the containment to use (Activity Log → Security) until a fixed build is live | Stage 49.17.6: an automatic rollback after a failed security-fix deploy would put the vulnerable build back with nothing said |
| D0d Upgrade channel (Deploy) | reads `UPGRADE_CHANNEL` from the host env file (that one line only) and the build's `release/channel.txt` (`stable` by default, `candidate` for an early build); `cmd/upgradegate` decides: `hold` takes nothing, a `stable` host takes no candidate build, no value counts as stable, an unknown value stops. `-Yes` does not override it; change the host channel by hand | 162.8: the channel was recorded at install but nothing read it |
| D0e Release-candidate record (Deploy) | runs `releasegate verify` on the commit Deploy ships (base = its parent, tree = its tree) against the `release/reportselease-candidate-<tree>.json` Release wrote. Stops, before the backup and before anything on the server changes, when the record is missing, is for another base or tree, or its verdict is fail (the blocking checks are printed). `-Check` only says it would stop. [Gate doc](../docs/operations/release-candidate-gate.md) | Stage 47.17.3 / 49.17.2: the record was written but nothing downstream read it |
| D4a Deep cleanup stamp (Deploy) | after D4, one line: when the 10-day deep cleanup last ran on the server (`/var/lib/erp-cleanup/deep.last`) and how many days ago; over 11 days is marked **overdue** (check the 04:15 cron and `/var/log/erp-cleanup.log`), and a server where it has never run says so. A note, never a stop | 109.12: every deploy reinstalled the cron, but nothing reported whether a deep pass ever ran |
| D4b Certificate expiry (Deploy) | after D4, one line per host: days until its TLS certificate expires and its issuer, read the way a browser reads it; under 21 days (Caddy renews at 30) or a name/chain problem is marked bold. A note, never a stop | 49.6.3: nothing watched certificate renewal until the ops alert webhook (20.2) is set |
| D5 restore-drill record (Deploy) | after the post-deploy checks, reads `$APP/backups/restore-drill-evidence.json` (written by every `restore_drill.sh` run, pass or fail) and records it as `release/evidence/backup_restore.json` for the deployed commit; the next Release ships it. Never blocks the deploy; says so when the server has no record. | Stage 49.17.2: the drill ran on the server and left no record the gate could read |
| D6 Code map (Deploy) | when the G13b redraw is of the deployed commit, waits for it (up to 10 min) and copies its three files into the shared tree where they are unchanged since HEAD, to commit with the working records; a failed or missing redraw is a note | 177.5: the exact map is drawn after the click |
| D0c / D3 Release signature (Deploy) | `deploy.ps1` signs the linux binary with the release key - `%USERPROFILE%\.erp-release\signing.key` (or `ERP_RELEASE_SIGNING_KEY`), never in the repo; the first deploy without one creates it and writes `release/signing.pub` into the main tree for the next Release to ship - and checks the signature against the committed `release/signing.pub` before shipping. On the server `remote_deploy.sh` checks it with the server's openssl against `/etc/erp/release-signing.pub` before the new binary first runs (migrate): no signature, a changed binary or another key → `DEPLOY-REFUSED`, nothing run or swapped, the binary kept as `erp-server.failed-unsigned-*`. The first signed deploy pins the key it brings. D0c stops early on a missing key (with `release/signing.pub` committed) or a server openssl older than 3 | Stage 49.10.6b.3: any binary that reached the server would have been migrated with and run - nothing proved the release machine built it |

Deploy (`scripts\Invoke-Deploy.ps1`) refuses a commit that is not on origin or
that Release did not pass, and only ever deploys the number Release wrote.

## Server cleanup

`deploy/server_cleanup.sh` keeps the newest 3 rollback copies, deletes failed
builds older than 7 days, control-character file names and stale `*.new`, runs
`apt-get clean`, holds the journal at 100 MB, trims the failed-login log and keeps
snapd out. It alerts through `OPS_ALERT_WEBHOOK_URL` above 80% disk and logs one
line a run to `/var/log/erp-cleanup.log`. It never touches backups, the live or
previous build, `db/` or PostgreSQL. Over 70% disk it keeps 2 rollback copies.
SSH bots are blocked by sshguard (installed 2026-10-19; SSH is key-only).

**Every 10 days (Stage 109.12)** the 04:15 cron line runs `server_cleanup.sh
--deep --if-due`: it asks daily and runs once the last deep pass is 10 days old
(`/var/lib/erp-cleanup/deep.last`; `Cleanup.cmd -Deep` runs one now and restarts
the count). On top of the nightly steps it removes packages nothing needs any
more (old kernels; `apt-get autoremove --purge`), temp files untouched for 10
days (not systemd's private ones), crash dumps over 10 days old and rotated
system logs over 180 days old - never younger, CERT-In keeps 180 days. Then it
measures backups, the ERP folder, PostgreSQL, logs, caches, temp, `/root`,
`/home`, `/boot` and `/usr` against the last deep pass
(`/var/lib/erp-cleanup/sizes.tsv`), says which grew most and in how many days
the disk is full at that rate, and alerts under 30 days. Its log line ends
`deep grew=<area>:+<n>MB full_in=<n>d` (`deep=baseline` the first time).
