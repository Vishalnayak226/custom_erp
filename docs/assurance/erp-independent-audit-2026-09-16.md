---
doc_id: DOC-ASSURANCE-ERP-AUDIT-20260916
title: Independent ERP maturity audit, 16 September 2026
type: record
status: draft
owner: qa-owner
approvers: [qa-owner, engineering-owner, product-owner, security-owner, operations-owner]
audience: [product, engineering, QA, security, implementation, operations]
applies_to: frozen 82f5517 working source captured 2026-09-16 at 02:40:44 UTC
authority: historical
confidentiality: internal
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# Independent ERP maturity audit — 16 September 2026

The ERP has substantial implemented breadth and a small server footprint. This audit found
reproducible defects in stored-content rendering, fresh installation, deployment recovery,
entitlement changes, quota error handling, role coverage and accessible forms. **This snapshot
does not meet a production release gate.** That judgment concerns observed behavior in built
paths; unfinished features are not counted as bugs or given a negative maturity score.

The deliverables are the [maturity checklist](../qa/erp-maturity-checklist.md),
[module, SaaS and UI roadmap](../product/erp-maturity-roadmap-2026-09-16.md),
[machine summary](../qa/erp-audit-2026-09-16-summary.json), and
[reproduction guide](../qa/erp-audit-reproduction.md). No application fix, deployment,
commit, purchase or production load test was made during this audit.

## Scope and confidence

The frozen input contains 987 eligible source files, based on commit
`82f5517a083b4318c7f1b38adf0f2d0815fcfc58` plus the then-current uncommitted work.
Each file has a SHA-256 recorded in `source-manifest.json`. Subsequent shared-workspace
edits are outside this assessment. Tests used a new local PostgreSQL 16.3 cluster on
`127.0.0.1:5446`, not the shared development database on 5435. All records and credentials
were synthetic. Outbound business delivery was disabled for the browser server; provider
tests used the project's local test doubles.

The host was Windows/amd64, Go 1.26.5 with CGO disabled, and Chromium 151.0.7922.34.
The repository's pinned Go 1.22.12 toolchain also compiled the source. Linux/amd64
release artifacts were cross-built three times but were not executed on Linux.
The browser server ran on loopback port 8178.

Every applicable automated family below has three iterations or explicitly records a
limitation. This is not every possible test, independent penetration-test certification,
WCAG conformance, regulatory acceptance, or evidence for untested deployments. Positive
results do not cover arbitrary datasets, countries, devices, integrations or module combinations.

## Executed results

| Check | Iteration 1 | Iteration 2 | Iteration 3 | Interpretation |
|---|---|---|---|---|
| Build / dependency integrity / Go vet | Pass | Pass | Pass | Go 1.26.5, frozen input |
| Frontend syntax and screenshot-harness tests | Pass | Pass | Pass | Four application JS entry points plus existing harness tests |
| Fresh migration runner | Fail | Fail | Fail | AUD-02; replay also fails until prerequisite is supplied |
| Full Go suite | 1,092 pass / 4 fail / 1 skip | 1,092 / 4 / 1 | 1,094 / 2 / 1 | Uncached; seeds off, 4802, 4803; test/subtest completion events |
| Browser screen traversal | 54 contexts checked | 54 | 54 | Desktop light, mobile dark, tablet keyboard; one state-dependent subview per round lacks a selected hook |
| Vendor save / Unicode / stored markup | Saved; XSS executes | Same | Same | AUD-01; Unicode survives the server round trip |
| Product package entitlements | 10 selections match | 10 match | 10 match | Nine individual packages plus full suite; catalog/state proof, not full standalone business UAT |
| Entitlement write failure | Incorrect success | Incorrect success | Incorrect success | AUD-05 |
| Quota database failure | Incorrect allow | Incorrect allow | Incorrect allow | AUD-06 |
| Local HTTP boundary checks | 8 pass | 8 pass | 8 pass | Auth, forged headers, token tenant authority, static paths/methods, unknown routes |
| Bounded list load | 72 successful requests | 72 | 72 | 5,000 Items; concurrency 1/4/8; separate rate windows |
| Dump and restore | Matching fingerprints | Matching | Matching | 74 tables / 13,020 rows including Unicode fixtures |
| Deployment doubles | Healthy path passes; 3 failure cases fail recovery criteria | Same | Same | Real remote script, mocked OS/services/network; AUD-03/04 |
| Linux stripped release build | 19,021,986 bytes | Same | Same | All three SHA-256 values identical |
| Dependency vulnerability scan | 6 repository call-path findings | Same | Same | Five remain when restricted to `cmd/server`; not five demonstrated exploits |
| Attack-surface freshness | Fail | Fail | Fail | Five new audit routes and one worker absent from approved inventory |
| Strict documentation lint | Fail | Fail | Fail | Eight findings in valid Git context; current governance work remains open |
| Documentation check-mode safety | 13 checks pass | 13 pass | 13 pass | Generated baseline normalized only in a separate fixture; original content freshness still fails |
| Race detector | Blocked | Blocked | Blocked | CGO disabled; explicit CGO attempt confirms no C compiler |

The full suite was run only **after a recorded, fixture-only migration workaround** for
AUD-02. Its results cannot be represented as a successful clean install. Total measured
statement coverage was **42.9%** for the instrumented first suite, not a quality score or a
claim that every money/security branch was exercised. The source inventory found 517
top-level `Test*` declarations, zero Go `Fuzz*` declarations and zero `Benchmark*` declarations.
Existing tests still include boundary and concurrent scenarios; absence of Go fuzz functions
does not mean absence of all randomized or adversarial tests.

One archive permission test skips on Windows (`TestAuditArchiveRefusesUnsafeArchiveDir`).
The third shuffled suite's two fewer failures were traced to approval-rule fixture mutation,
not accepted as an unexplained improvement.

## Confirmed implementation defects

Priorities are this audit's remediation recommendations, not claims of independently assigned
CVSS scores. P1 should block release of the affected workflow; P2 should be fixed before the
corresponding maturity claim. Existing backlog references identify where to implement the fix.

### AUD-01 — P1: stored JavaScript executes in generic record lists

**Observed:** a Vendor name containing an image error handler survives saving and executes
when the record list renders. Three full form runs reproduced this. A separate user granted
only Vendor read/create can also submit the payload while receiving 403 from the module-admin
API. The clerk's stored payload executes in the Super Admin's browser in all three separate
cross-role observations.
The payload only sets a JavaScript flag; it sends no data anywhere.

**Cause:** `renderDocTable` passes record values to `copyableCell(val, val)` in
[app.js](../../public/app.js). `copyableCell` places `displayValue` directly into an HTML string.
Escaping the copy button's attribute does not escape the visible value. The current browser
policy allows the test event handler to execute.

**Fix gate:** render untrusted cell text through `textContent` or context-appropriate escaping;
review other uses that intentionally pass trusted markup rather than globally breaking them.
Add an actual low-privilege-writer/high-privilege-reader regression through create/import/edit
and list/report/detail views. Session/CSP hardening remains separately tracked in 47.8 / 49.4.
Evidence: `ui-task-results.json`, `xss-cross-role-create.json`, `xss-cross-role-view.json`.

### AUD-02 — P1: clean installation stops before the audit schema is ready

**Observed:** `erp-server -migrate` fails on a fresh UTF-8 database with
`relation "tenant_default.audit_checkpoints" does not exist`. Three baseline databases, three
suite databases and focused fixtures reproduce it; retry alone does not recover. The CI
filename-order migration path also reaches the same failure.

**Cause:** [audit archive migration](../../db/migrations_stage47_7_6_audit_archive.sql) depends on
[audit evidence migration](../../db/migrations_stage47_7_audit_evidence.sql), but sorts before it.
The test fixtures applied the prerequisite explicitly and then resumed the unchanged runner.

**Fix gate:** define a migration order that respects the dependency while preserving existing
migration ledgers/checksums; prove fresh install, existing-database upgrade, retry and no-op
replay. Do not blindly rename an already-applied migration. Evidence: `iteration-*-migrate.log`,
`suite-*-fixture-*.log`, `ci-migration-migrations_stage47_7_6_audit_archive.log`.

### AUD-03 — P1: failed deployment restores an incompatible frontend

**Observed:** in three unhealthy-release drills, the script reports rollback and restores
`OLD_BINARY`, but `public/app.js` still contains `NEW_PUBLIC`.

**Cause:** [deploy.ps1](../../deploy/deploy.ps1) uploads directly into `public/` before
[remote_deploy.sh](../../deploy/remote_deploy.sh) copies that directory to `public.prev`.
The snapshot is therefore already the new frontend. A migration failure also leaves the old
binary serving new static files because the upload happened first.

**Fix gate:** stage a complete release before activating it; preserve the actual prior binary
and static set as one release. Include partial upload, migration failure, unhealthy start and
successful recovery in regression tests. Evidence: `deploy-mock-results.json`, three rounds
of each case. No remote machine or real service was touched.

### AUD-04 — P1: restart-command failure bypasses deployment recovery

**Observed:** when the mocked `sudo systemctl restart erp` fails, all three runs exit before
the rollback branch. The new binary remains installed and no successful recovery is reported.

**Cause:** `set -e` exits at the unguarded restart command before the health/rollback branch.
**Fix gate:** every failure after activation must enter an explicit recovery path; recovery
failure must remain distinguishable. Include permission/service-manager failures, not only
HTTP 503. Evidence: the `restart-fails` cases in `deploy-mock-results.json`.

### AUD-05 — P1 for SaaS package changes: failed disable is reported as success

**Observed:** with PIM enabled, apply the HR-only package while a fixture trigger rejects
disabling PIM. `ApplyPackageSelection` returns nil and PIM stays enabled, three out of three runs.
Without the injected write failure, all ten package selections match their declared module sets.

**Cause:** [modules.go](../../engines/modules.go) stops its retry loop when no progress is made
but returns nil even when `toDisable` remains nonempty. This is a reproducible engine contract
failure; no claim is made that an ordinary client can install the fault-injection trigger.

**Fix gate:** return a meaningful failure and the resulting entitlement state; define atomicity
or explicit recoverable partial-state semantics for a package change. Test a failed disable,
concurrent change, retry and audit trail. Evidence: `package-probe-results.json`.

### AUD-06 — P2: quota lookup allows operations when its database read fails

**Observed:** an exceeded configured limit rejects the operation while connected, but the same
check returns nil after the probe's database handle is closed. Three controlled repetitions.

**Cause:** [tenant_limits.go](../../engines/tenant_limits.go) treats every query error as an absent
configuration row. An unconfigured limit is deliberately allowed; a database error is different.
**Fix gate:** distinguish `sql.ErrNoRows` from operational failure and propagate the latter.
This proves the helper's behavior, not an end-to-end exploit through every caller; some callers
may already fail on an earlier query. Evidence: `tenant-limit-probe.json`.

### AUD-07 — P2: shipped Store metadata is absent from role templates

**Observed:** `TestKnownModulesMatchTheTenantSchema` fails in all three full suites and three
focused runs: the database contains module `Store`, but `knownModules` does not.
**Impact:** the broad Administrator/Auditor template construction can omit that module.
**Fix gate:** reconcile the shipped module vocabulary and test actual role access, preserving
the distinction between Administrator and Super Admin. See
[role_templates.go](../../engines/role_templates.go). Evidence: `focused-*-role-catalog.log`.

### AUD-08 — P2: generic record forms lack accessible input names

**Observed:** the Vendor dialog exposes eight unnamed editable controls in Chromium's
accessibility tree in each of three runs. Its ten input/select fields have no associated label
or `aria-label`; some can acquire a fallback name from a placeholder. Visible labels alone are
not programmatic associations. Other screens also have unlabeled-control candidates.

**Cause:** the shared dynamic form builder creates sibling labels without `for`/matching input
IDs. **Fix gate:** fix the shared builder, then validate accessible names, instructions, errors,
required state and keyboard use on each field type. This relates to
[WCAG 2.2 name, role and value](https://www.w3.org/TR/WCAG22/#name-role-value), but this audit
does not claim full WCAG evaluation. Evidence: `ui-task-results.json` and form screenshots.

### AUD-09 — P2: successful modal submission leaves focus on its Save control

**Observed:** after a successful save and modal closure, `document.activeElement` is still
the dialog's Save button in all three runs. Focus does not return to the initiating New Vendor
control or the saved row. **Fix gate:** restore a visible, useful focus target on save, cancel
and error recovery; prove keyboard task continuity and prevent hidden dialogs entering tab
order. Evidence: `ui-task-results.json`; shared dynamic-dialog code in `app.js`.

## Verification and release-gate findings

**QA-DEF-01 — approval tests mutate shared seeded policy.** The two checkout tests fail with
the freshly shipped 10% POSCart approval rule and pass after that rule is removed. This was
reproduced three times for each condition. The price-tamper fixture uses `ON CONFLICT DO NOTHING`
and then deletes that same rule in cleanup, even when the fixture did not create it. This
explains the order-sensitive full-suite outcomes. Give tests isolated policy scope or restore
the preexisting row; assert the intended approval lifecycle rather than assuming every 200
checkout response means a paid sale. Evidence: `focused-test-results.json` and
`internal/server/stage47_a02_price_tamper_redteam_test.go`.

**QA-DEF-02 — attack-surface evidence is stale.** The frozen manifest lacks five audit archive
routes, its scheduler and two environment flags. Its own freshness test catches this in every
suite. This is inventory drift, not proof that the routes lack authorization. Review and
regenerate the manifest after the in-progress archive changes are finalized.

**Dependency gate.** Three scans on Go 1.26.5 identify GO-2026-6218, GO-2026-6091,
GO-2026-6090, GO-2026-6089, GO-2026-5972 and GO-2026-5026 on repository call paths. The
server-only scan excludes the HTML-template/tooling finding GO-2026-6091 and retains five.
The scanner identifies Go 1.26.6 as fixing these findings. Upgrade and rescan a supported,
pinned toolchain, then revalidate the release; the repo's separate Go 1.22.12 pin is a
toolchain-governance concern even though its build succeeds. Exploitability requires the
relevant runtime inputs/configuration: for example, the
[official GO-2026-6089 advisory](https://pkg.go.dev/vuln/GO-2026-6089) concerns servers configured
for unencrypted HTTP/2. Call-graph reachability alone does not establish that configuration.

**Documentation work remains open.** In a Git-aware copy of the frozen source, strict lint
reports eight findings: the oversized handover and seven unregistered documents. Initial
Git-less snapshot lint additionally produced three false missing-file findings; those are
excluded. An ignored local `CLAUDE.md` link also needed local context. The checked-in generated
content was stale; the safety suite initially stopped at its clean-baseline requirement.
Regenerating only an isolated documentation fixture allows the read-only safety checks to be
assessed separately: all 13 cases pass in each of three rounds. None of this closes Stage 48's
approval, migration or retention gates.

## Performance, footprint and UI assessment

| Measurement | Observed | Boundary |
|---|---|---|
| Linux/amd64 stripped binary | 18.14 MiB; identical hash in three builds | Meets the draft ≤25 MiB budget; no Linux runtime measurement |
| Windows HTTP process after browsing/load | 36.96 MiB working set; 67.67 MiB private bytes in three adjacent samples | Working set is a Windows proxy for resident memory, not a Linux cgroup result or long soak |
| Item list, 5,000 Items, 50-row page | p95 35.0–103.2 ms across nine samples; 216/216 HTTP 200 | Local requests include connection overhead; 24 requests per concurrency cell is a small sample |
| Oversized requested page | Capped at 1,000 records | Bound is present; a 1,000-row response still costs more than a normal page |
| `app.js` over HTTP gzip | 256,076 bytes (250.1 KiB), 1,066,509 bytes decoded | Exceeds NFR-COST-001's 120 KiB initial-JS target |
| Measured shell + four asset responses | At least 288.9 KiB compressed | Already exceeds the 180 KiB cold-core target before counting remaining assets |
| Browser reflow sample | No document-level horizontal overflow in 162 screen/device observations | Empty/default data; not proof for dense tables, all dialogs or real RF workflows |
| Restore fixture | 0.5 MB dump; 1.49–2.11 s restore command | Small local drill, not approved business RTO/RPO |

An initial load experiment intentionally preserved its actual 429 results: 120 of 216 requests
were throttled after exceeding the real 100/minute search budget. Those rejection latencies
were **not** counted as successful request performance. The repeated performance run spaced
rounds into separate rate windows and had zero rejected requests. This also demonstrates the
limiter acting under local excess demand; it is not a DDoS resilience test.

The UI has useful shared tokens, responsive navigation, setup guidance and explicit empty
states. The strongest next improvements are shared-form accessibility and safe rendering,
task-focused workspaces, clearer state transitions, dense-table usability and loading only
the code a screen needs. The [roadmap](../product/erp-maturity-roadmap-2026-09-16.md) records
concrete design acceptance criteria. A framework rewrite is not justified by these results.

## Not executed or not established

- Race detection and Unix archive-directory permissions need a compatible Linux/compiler environment.
- Firefox, WebKit, real screen readers, contrast/zoom/manual accessibility certification, scanners,
  printers, payment terminals, physical offline/reconnect and real-user task acceptance remain unverified.
- No real email, payment, marketplace, tax filing, bank, courier, SSO/SCIM or production edge trial.
- No production load, multi-day soak, multi-region failover, disk-full/OOM chaos, destructive
  recovery, point-in-time recovery, or independently executed legal/payroll/accounting acceptance.
- No exhaustive combinatorial, mutation, model-based or coverage-guided fuzz campaign. These
  are named checklist work, not claimed passes from the existing regression suite.
- Automatic approval review rejected the command to stop/restart the owned local HTTP process
  with `blocked by policy`. Restart, shutdown and process-crash recovery remain unexecuted.
  The loopback HTTP fixture and dedicated PostgreSQL cluster were left running; neither is production.

## Follow-through

Fix and repeat AUD-01 through AUD-05 before releasing those affected workflows; repair the
test-fixture and inventory gates so a green suite has meaning. Continue existing Stages 47–49
under their own scope and approvals. Build newly proposed module/package capabilities only
after choosing a reference customer journey and country/industry scope. Keep the modular
monolith and prove its budgets before adding services, workers or new runtime dependencies.
