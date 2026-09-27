---
doc_id: DOC-QA-ERP-AUDIT-REPRO
title: Reproducing the September ERP maturity audit
type: reference
status: draft
owner: qa-owner
approvers: [qa-owner, engineering-owner, operations-owner]
audience: [engineering, QA, security]
applies_to: frozen local 2026-09-16 audit input and disposable fixtures
authority: source
confidentiality: internal
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# Reproducing the September ERP maturity audit

Read the [report](../assurance/erp-independent-audit-2026-09-16.md) and
[machine summary](erp-audit-2026-09-16-summary.json) before interpreting raw logs.
The tools below are optional development tooling and add no ERP runtime dependency.
They are a record of this controlled audit, not a universal production test command.

## Preserved inputs and outputs

The working fixture is `%TEMP%/erp-audit-20260916/` with `source/`, `evidence/`, `pgdata/`,
`probe/`, `package-probe/` and a Git-aware `safety-source/`. The source manifest records the
987 included files and their hashes. Application builds and suites ran against `source/`;
documentation safety normalization happened only in `safety-source/`.

A local review bundle is retained under `.claude/erp-audit-20260916/`:
`source.zip`, `evidence.zip`, and `bundle-manifest.json`. This directory is ignored by Git;
retain the bundle with the release evidence if it must survive workstation cleanup. It
contains synthetic records/screenshots and exploit-test evidence, so keep its internal
classification. Bearer-token files, database dumps, database cluster files and executable
binaries are excluded from the evidence ZIP. The machine summary is part of the reviewable
documentation even when the local bundle is not available.

## Preconditions

1. Use a frozen source copy and a **new disposable PostgreSQL cluster**, not merely another
   tenant in shared development. This audit used PostgreSQL 16.3, UTF-8/locale C, a local
   `erp_audit` fixture user, loopback binding and port 5446. Trust authentication is appropriate
   only for this loopback scratch fixture; it is not a deployment recommendation.
2. Confirm neither 5446 nor 8178 belongs to another process. The September fixture may still
   own these ports: process restart/stop was rejected by automatic approval review. Do not
   assume a port is disposable or kill a process based only on its port.
3. Set both `DATABASE_URL` and `TEST_DATABASE_URL` to an explicitly named audit database.
   The existing helpers otherwise fall back to the shared development database on 5435.
4. Use synthetic data and no production secrets. Clear external-provider credentials.
   Start browser fixtures with `ENV=test` and external business side effects disabled.
   The unit suite needs its in-process/local HTTP doubles; do not block those and then
   misclassify the resulting failures as application defects.
5. Have Go, Python 3, Node, portable PostgreSQL tools, and local Playwright/Chromium available.
   Git Bash is only needed for the mocked deployment script. No install is performed by
   these audit scripts. Windows race detection also needs a compatible C compiler.

## Runners

From the repository, supply explicit paths. The following illustrates the original layout;
choose a new fixture directory and database names before repeating on an already-used cluster.
The scripts intentionally do not drop or overwrite existing databases.

```powershell
$auditFixture = Join-Path $env:TEMP 'erp-audit-20260916'
$auditPg = Join-Path $env:USERPROFILE 'pg-portable/pgsql/bin'
python docs/qa/run-maturity-audit.py --source "$auditFixture/source" --evidence "$auditFixture/evidence" --pg-bin "$auditPg" --port 5446
python docs/qa/run-maturity-suites.py --source "$auditFixture/source" --evidence "$auditFixture/evidence" --pg-bin "$auditPg" --port 5446
```

- `run-maturity-audit.py`: three builds/static/syntax/inventory/vulnerability rounds, fresh
  migration and replay, and the CI migration-order rehearsal. Full suites are conditional on
  a successful migration; the output must not silently imply they ran when install failed.
- `run-maturity-suites.py`: records the known clean-install failure again, applies only the
  specific missing prerequisite to newly created databases, resumes the unchanged migration
  runner, then runs three suites with order off/4802/4803. This is explicitly a workaround
  fixture, not a repaired application.
- `audit-browser.cjs SOURCE EVIDENCE http://127.0.0.1:8178`: traverses 54 dispatched views in
  three viewport/theme contexts, records errors and preliminary DOM observations, and takes
  selected screenshots. It requires a real synthetic admin session in `ui-token.txt` and
  verifies the live identity. The hook-log subview needs a hook selection; its direct-route
  error is not counted as a module defect.
- `audit-ui-tasks.cjs SOURCE EVIDENCE`: three real Vendor form submissions, accessibility-tree
  observations and harmless stored-markup probes. These are writes to the disposable fixture.
  Do not run on a customer database. The original raw response-string Unicode check needed
  JSON decoding; the separate parsed cross-role check is the authoritative persistence evidence.
- `audit-http.py --evidence EVIDENCE`: eight local boundary checks, payload measurements,
  oversized-page checks and three paced rounds of 24 requests at each concurrency level 1/4/8.
  The fixture must contain 5,000 Items. Preserve 429s; never count their latency as successful work.
- `audit-deploy-mock.py --source SOURCE --evidence EVIDENCE --bash GIT_BASH`: runs the actual
  remote script inside new verified TEMP children with local command doubles. No SSH, real
  system service, webhook or external HTTP is invoked. Models the uploader's ordering before
  calling the script. Twelve cases: three rounds each of success, bad health, failed restart
  and failed migration. The first invalid Git-Bash PATH experiment is retained but excluded.
- `audit-restore.py --evidence EVIDENCE --pg-bin PG_BIN`: dumps the quiescent
  `erp_audit_repro1` synthetic database, restores into three new databases and compares all
  table row counts/content fingerprints. The tiny local timings are not production RTO/RPO.
- `summarize-maturity-audit.py --evidence EVIDENCE --output OUTPUT_JSON`: checks frozen input
  hashes and compiles the recorded outcomes without converting skips/failures into passes.

The Go quota and package fault probes are preserved in the evidence bundle's `probes/` folder.
They require their explicitly hardcoded scratch database URLs. The quota probe closes only
its own connection handle. The package probe installs a trigger that rejects a PIM-disable
write only inside `erp_audit_repro3`. Do not copy those fixtures into development or production.
Focused checkout reproductions compare freshly seeded policy with an explicit variant that
removes the fixture's 10% rule; they are diagnostic experiments, not a recommended policy change.

## Interpreting and extending the evidence

Test/subtest events include parent tests. Coverage is statement coverage from the first
instrumented run, whose failing tests remain failures. A package selection test proves
entitlement state, not the whole module's usability or independent deployability. An automated
accessibility tree is not a screen-reader/contrast review. Static call-graph vulnerability
findings need configuration and input-path triage.

For the next iteration, add the actual Linux hardware/data sizes, supported browser/device
matrix and customer process fixtures. Close defects with targeted regression tests, then
repeat the relevant complete workflow and release gates. Preserve this historical input and
results; create a new dated record for the fixed source instead of editing failures into passes.
