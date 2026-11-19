---
doc_id: DOC-5C0E6A47D1
title: Release-candidate gate
type: procedure
status: draft
owner: engineering-owner
approvers: [documentation-maintainer, engineering-owner]
audience: [maintainers, engineering-owner]
applies_to: source documentation; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-11-06
review_by: 2027-02-06
supersedes: none
superseded_by: none
verification_scope: tool built and run on a snapshot on 2026-11-06 (erp-5c group e); not yet run inside a real release
---

# Release-candidate gate

Stage 47.17.3 (clean-release gate) and 49.17.2 (release-candidate gate). Release
step **G10b** runs `cmd/releasegate` after every test has passed and before the
commit. It writes one **release-candidate record** (JSON and a short Markdown
page, in `release\reports\`) bound to the exact candidate: the previous release
commit, the tree G11 will commit, the migrations with checksums, the SBOM, the
config contract, the surface diff, the results G8/G10 produced, the exceptions
register and the run evidence. It stops the release only for the rules marked
**blocks** below; everything new starts as a **warning that is recorded**.

## What was already gated, recorded, or missing

| Requirement (47.17.3 / 49.17.2) | Before | Now |
|---|---|---|
| Exact commit | G11 commits only the tested tree | recorded: base commit + tree sha in the record (**blocks** if not a sha). The new commit's sha exists only after G11 |
| Signed commit | not done; only the binary is signed (Deploy D3) | unchanged - decision 3 below |
| Exact migrations, checksums | G10 migrates from empty; the runner stores checksums in `schema_migrations` | recorded: every `db/migrations/*.sql` with the same LF-normalised sha256 the runner stores; added/edited since last release (**blocks** if one has no checksum) |
| Artifact checksum | Deploy D3 signs the linux binary and the server checks it | `-artifact name=path` records one; Release builds none, so it shows **deferred** to Deploy |
| SBOM / provenance | `cmd/releasemanifest` existed, no release step called it | recorded: the manifest (commit, toolchain, dependency ledger, migration and static-asset hashes) is written beside the record and its sha256 kept (**blocks** if it cannot be built) |
| Config contract | D1 checks secret keys only | recorded: the environment names the build reads, a hash, and the names added/removed since the last release |
| Clean build / vet / tests | G8, G10 | recorded from that run, not re-run (**blocks** if any is not `pass`) |
| Route / permission / surface diff | G6 regenerates, G6b prints a diff into the report | recorded; high-risk lines in the record; policy `surface_high_risk` (warn) |
| Authorization diff | G6b lists each route's sign-in level | same record |
| Dependency scan | none | `govulncheck` (installed on the release machine) - policy `govulncheck` (block). Not installed or no network: **not-run**, never a pass |
| Dependency and licence record | `dependency-ledger.json` tests | recorded: counts by type, licences, entries without one (**blocks**) |
| Secret / static | `go vet`, the `securityscan` tests inside G10, D1 | unchanged (inside G8/G10) |
| Fuzz | fuzz seeds run inside G10 | unchanged; real fuzzing is not part of a release |
| Race | none | evidence kind `race` (needs cgo) |
| Dynamic security run | none | evidence kind `dynamic` |
| Zero P0, zero unresolved P1, approved expiring exceptions | none | `release/exceptions.json` (**blocks**) |
| Performance / accessibility budgets | G4c: cold-core bytes only | evidence kind `browser_budget` |
| Journey evidence | none | evidence kind `journey` |
| Backup / restore | Deploy takes a backup and checks its sha256; `deploy/restore_drill.sh` exists | evidence kind `backup_restore` |
| Worker / outbox drill | none | evidence kind `outbox_drill` |
| Rollback / roll-forward | Deploy keeps the previous build and rolls back on its own | evidence kind `rollback` |
| Docs manifest | G7 | unchanged |
| Supported limits | "Availability model" in `docs/operations/service-operations.md` | checked present (policy `supported_limits`, warn) |

## Defects and exceptions: `release/exceptions.json`

One entry per **open** defect. A release carries a defect only through an entry.

```json
{ "schema_version": 1, "exceptions": [
  { "id": "D-12", "severity": "P2", "title": "Home page LCP over budget",
    "approver": "owner", "approved_on": "2026-11-06", "expires": "2027-01-15",
    "covers": ["browser_budget"] } ] }
```

| Severity | Rule |
|---|---|
| P0 / critical | never carried: the gate fails while one is open (close it by setting `"status": "closed"` once fixed) |
| P1 / high | needs an approver, `approved_on`, `expires` within **30 days**, and a **signature** from the release key |
| P2 / P3 | needs an approver and an `expires` within **180 days** |
| any, past `expires` | **fails the gate** (the expiry day itself still counts) |

`covers` lists run-evidence kinds the entry accepts. A known miss (evidence that
failed, is stale or is missing) is then recorded as **excepted**, never as a pass.
An exception that is itself invalid (expired, unsigned P1) covers nothing.

**Signing a P1** is a human act with the release key
(`%USERPROFILE%\.erp-release\signing.key`, the key Deploy signs binaries with; its
public half is `release/signing.pub`). The signature covers the entry's id,
severity, title, approver, dates and `covers`, so changing any of them
afterwards breaks it:

```powershell
go run ./cmd/releasegate payload -id D-12 -out $env:TEMP\p.txt
go run ./cmd/releasesign sign -key $env:USERPROFILE\.erp-release\signing.key -in $env:TEMP\p.txt
# paste the printed line into the entry's "signature"
```

The gate's refusal message for an unsigned P1 prints these two commands.

## Run evidence: `release/evidence/<kind>.json`

Kinds: `backup_restore`, `outbox_drill`, `rollback`, `browser_budget`, `journey`,
`race`, `dynamic`. Record one after the run:

```powershell
go run ./cmd/releasegate record -kind backup_restore -result pass -by "name" -note "restore_drill.sh on app, 10 tables"
```

It writes `release/evidence/<kind>.json` with today's date and the HEAD commit
(`-commit` overrides). The release ships it with the next commit. A file counts
when it passes, is not older than its limit (35 days; 14 for `browser_budget`,
`journey`, `race`), names who ran it, and its commit is the release base or in its
history. Missing, stale, failed or another-commit evidence is a **warning** by
default and **fails** when `release/gate-policy.json` sets that kind to `block`,
or to `block_after_pass` once the kind has passed once (the file's `first_pass`,
which `record` keeps through later runs).
A failed run is not hidden: file an exception that `covers` the kind.

Who writes each kind (all through `releasegate.WriteEvidence`, so the first pass is kept):

| Kind | Producer |
|---|---|
| `journey` | `journeylab journey -record-gate .`; counts only a run of all seven journeys, not `-inject-mismatch`; any blocked or failed journey records fail |
| `browser_budget` | `node docs/qa/perf-budget.cjs --record-gate .`; a full run only. PASS records pass; a miss, even a known one, records fail (carry it with a P2 exception); an error or a partial run records nothing |
| `backup_restore` | `deploy/restore_drill.sh` writes `$BACKUP_DIR/restore-drill-evidence.json` on the server; Deploy step D5 ingests it with `releasegate record -from <file> -commit <deployed commit>` |
| others | `releasegate record` by hand |

### Browser budgets and journeys on GitHub (47.9.4, 47.17.2)

With tests on GitHub, the click no longer reads these two kinds. G10b passes
`-github-kinds browser_budget,journey`, so the record shows both as **deferred**
to `release.yml`'s evidence job, and G0 stops the release if promote does not
need that job. The job:

1. downloads the last run's `release-evidence` artifact into `release/evidence/`,
   so each kind keeps its `first_pass`;
2. builds the candidate, migrates an empty database and starts the server on
   `127.0.0.1:8740` with outside side effects off;
3. runs `perf-budget.cjs --record-gate .` (Playwright and Chromium installed for
   the run) and `journeylab journey -record-gate .`, all seven journeys;
4. runs `go run ./cmd/releasegate judge -kinds browser_budget,journey`: the same
   rules, policy and exceptions as G10b. It prints one line per kind into the run
   summary and fails the job when a kind would block;
5. uploads the two evidence files as `release-evidence` (kept 90 days) and the raw
   reports as `evidence-reports`.

Only evidence made on the candidate commit counts. A miss before the kind's first
pass warns; after it, the job fails and main does not move. That includes a harness
failure: if the server, Playwright or `pg_dump` breaks, the run records nothing, and
the judge then fails once the kind has passed. To ship past a known miss, file a P2
exception that `covers` the kind.

```powershell
go run ./cmd/releasegate judge -kinds browser_budget,journey   # same verdict on the laptop
```

## Policy: `release/gate-policy.json`

Each optional check is `"warn"` or `"block"`; run evidence can also be
`"block_after_pass"`: a warning until that kind first passes, a block from then on.
The schedule approved on 2026-11-11 (item 162.20):

| Check | Mode |
|---|---|
| `backup_restore`, `outbox_drill`, `rollback` | `block_after_pass` |
| `browser_budget`, `journey` | `block_after_pass`; made by `release.yml`'s evidence job (ask 241) |
| `race`, `dynamic` | `warn` |
| `govulncheck` | `block` (since the Go 1.27.4 toolchain, 162.1) |

A test (`TestShippedPolicyFollowsTheApprovedSchedule`) holds the file to this table. Rules that always block: commit
binding, migration checksums, SBOM, licence record, results, the exceptions
register (including its being present).

## Using the record

`go run ./cmd/releasegate verify -record <file> -commit <base> [-tree <tree>]`
refuses a record for another commit or tree, or one whose verdict is not pass.

## Decisions

1. **govulncheck** is used because it is installed. Mode `block` (approved 2026-11-11, 162.20)
   since Go 1.27.4 cleared the 13 standard-library findings (162.1; 1.27.3 still
   had one).
2. **Evidence modes.** `block_after_pass` for `backup_restore`, `rollback`,
   `outbox_drill`, `browser_budget` and `journey`; `race` and `dynamic` stay
   `warn` (approved 2026-11-11, 162.20). A known miss such as the LCP budget is
   filed as a P2 exception that `covers` the kind.
3. **Signed commits.** Not added: the binary is signed and checked on the
   server, and the record binds the tree. Add it only if an auditor asks
   (approved 2026-11-11, 162.20).
4. **Deploy checks the record.** Built (49.17.2): step D0e of `Invoke-Deploy.ps1` runs
   `releasegate verify` for the commit it ships and stops on a blocking failure; a
   missing record is a stop too ("run Release.cmd").
