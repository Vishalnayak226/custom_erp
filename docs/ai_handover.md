---
doc_id: DOC-HANDOVER
title: Current developer handover
type: reference
status: active
owner: engineering-owner
approvers: [engineering-owner]
audience: [developers, maintainers]
applies_to: shared development tree; verify state before editing
authority: canonical
confidentiality: internal
last_verified: 2026-09-10
review_by: 2026-10-10
supersedes: historical handover captured 2026-09-09
superseded_by: none
---

# Current developer handover

Read §6 first, then the procedure for your task. Historical reasoning is preserved in the
[2026-09-09 handover capture](archive/ai-handover-2026-09-09.md); its old claims are not current support.

## 1. System Environment & Port Bindings

Development PostgreSQL was listening on loopback port **5435** on 2026-09-10.
The Stage 48 capture server on 8168 was stopped after verification; recheck listeners before starting a server.
The application uses `PORT`; start a scratch instance on an unused port for verification.
Keep environment values/credentials in approved local configuration, never in this handover.

## 2. Core Repository Map

[Architecture](architecture/current-architecture.md), [data/runtime views](architecture/data-and-runtime-views.md),
[brain navigation](brain/README.md), [data dictionary](data/generated/dictionary.md).
The server entrypoint is `cmd/server/main.go`; build `./cmd/server`, not the repository root.
Domain logic is in `engines/`, HTTP/auth in `internal/server/`, native UI in `public/`.

## 3. Development Command Recipes

[Developer setup and checks](engineering/developer-setup.md), [contributing](engineering/contributing.md),
[deployment and service health](operations/service-operations.md), [upgrade/rollback](operations/upgrade-and-rollback.md).
On this workstation fresh Go binaries may need TEMP output followed by PowerShell Copy-Item
because Controlled Folder Access blocks writing inside Documents. The docs wrapper handles this.

## 4. Multi-Tenant Development Standards

Use authenticated server-resolved scope, safe tenant schema resolution and transaction-local
search paths. Tenant scope alone does not establish entity/location/owner/self/field permission.
Follow [control requirements](requirements/control-requirements.md) and [data boundaries](architecture/data-and-runtime-views.md).

## 5. Omnichannel Scale Architecture

One Go application, PostgreSQL and native browser assets remain the standing topology.
[Decision register](architecture/decisions/decision-register.md) records current patterns and open decisions.
The later Stage 47.5 decision selects one owner per warehouse, enforced by the default guard.
Mixed-owner mode is explicitly unsupported. Physical RF and 47.7.6 archive/restore acceptance
remain open; do not claim audit/scale certification.

## 6. Version Control & Handover Status

- **2026-09-09/10 (this session, continued — Stage 47.5 + 47.7) — UNCOMMITTED.** The two remaining Stage 47 items that were blocked on a *decision*, not on effort. The user took both decisions this session, from evidence gathered first rather than in the abstract.

  **47.5 (A-05) — CLOSED via the enforced-de-scope limb.** The deciding evidence: the entire dev database held **two** `bin_stock_owner` rows and both were the A-05 red-team fixture. Nobody runs mixed-owner 3PL, so the *claim* of isolation was the risk, not the missing feature. New `db/migrations_stage47_5_single_owner_guard.sql` + `engines/wms_single_owner.go`: a `warehouse_owner` table whose PRIMARY KEY **is** `location_code` (so a second owner cannot be stored), checked at `RecordOwnerStock` — the one API that can put a second owner in a building. An unassigned warehouse **adopts its first owner**, so tenants that never configured ownership are unaffected; it is the *second* owner that becomes impossible. Setting `wms.stock_ownership_mode` defaults to `single_owner`; `mixed_unsupported` exists so a real pilot can opt in knowingly. New routes `POST /api/v1/wms/owner/assign-warehouse`, `GET /api/v1/wms/owner/mixed-locations`. **The migration deliberately does not rewrite pre-existing mixed data** — it seeds only unambiguous locations and `RAISE WARNING`s about the rest, because aborting would block every later migration and guessing would re-attribute somebody's stock. Verified on the dev DB: it found the one mixed location, refused to guess, and warned. `docs/ERP_BLUEPRINT.md`'s "multi-owner (3PL) stock segregation" claim corrected.

  **47.7 (A-07/A-30) — mostly closed; stays `[ ]` on 47.7.6's archive drill alone.** Model chosen: **independently signed events + checkpoints**. What existed was the worst of both — `engines/logs.go` hashed each row from the previous row's checksum (chain semantics) *without* locking, and its own comment concluded sibling rows were harmless. They are not: siblings are exactly what made the verifier report a break on clean data. Locking instead would have serialized every audit write per tenant on a table that takes a row on nearly every request, right after 47.3 made checkout deliberately concurrent. New `engines/audit_evidence.go`: per-row HMAC over length-prefixed canonical content (no ordering dependency), plus checkpoints over `seq` ranges to catch **deletion**, which per-row signatures cannot. `WriteAuditEvidenceTx` writes evidence inside the caller's transaction (47.7.3). Jobs ride **Stage 38.6's runner** (`StartAuditEvidenceScheduler` only enqueues, hourly, idempotent per tenant-hour), and `AuditVerificationOverdue` alerts on verification that did not *happen*. New routes `GET /api/v1/admin/audit-logs/evidence`, `POST /api/v1/admin/audit-logs/checkpoint`; the old chain verifier is left untouched because it reports on the historical `checksum` column.

  **The legacy rows are SEALED, NOT BACKFILLED** — 345,808 of 383,810 (90.1%). A signature computed today proves only that a row exists today; backfilling would turn a known gap into a false assurance an auditor would rely on. Every verification returns a `coverage_statement` saying in words that those rows are **not counted as verified**, and a test asserts that negative.

  **A real bug the new tests caught:** Postgres `TIMESTAMP` stores microseconds, `time.Now()` carries nanoseconds — so the value signed differed from the value stored and *every fresh row failed its own signature*. Found because the tamper test reported a row as tampered before anything touched it. Fixed by truncating to microseconds in both `audit_evidence.go` and `logs.go`.

  **A-05's red-team test now passes and is promoted out of the `stage47redteam` tag**, joining A-02/A-03/A-04. All four originally-red findings with buildable closures are green on the ordinary path. `engines/environment.go`'s conditional-claim register is **updated, not cleared**: 47.2–47.4 retired, 47.5/47.6/47.7 rewritten to the limitations that genuinely remain (owner-blind picking, uncertified RF hardware, the legacy audit coverage boundary).

  **Things a next session needs to know:**
  - **Two migrations applied to the dev DB by hand** (`migrations_stage47_5_single_owner_guard.sql`, `migrations_stage47_7_audit_evidence.sql`). Production has NOT had them, nor 47.2/47.3/47.6's.
  - **Audit checkpoints are a shared-DB test hazard.** A checkpoint seals a `seq` window; any later test whose cleanup deletes audit rows inside it breaks checkpoint verification for every subsequent run. My tests now delete the checkpoints they create — keep that discipline. If the suite ever fails oddly across unrelated packages, check `SELECT count(*) FROM tenant_default.audit_checkpoints` first.
  - **A key rotation must bump `AuditSigVersion`.** A checkpoint or row signed under a different key with the *same* version string will read as tampered. The verifier already skips rows whose `sig_version` differs, which is the intended escape hatch.
  - **`go test ./...` in the background is unreliable on this machine** — two runs reported 10.5h and 16.7h elapsed because the machine slept mid-run, producing package FAILs with no `--- FAIL` detail. Run it in the foreground in chunks; each package finishes in ~1-2 minutes.
  - **Two of this stage’s own tests flaked once on a shared-DB collision** and have not recurred: `TestStage472QuoteVersionDetectsChangedInputsAndStalePrices` and `TestStage474ExceptionPathsAreSupervisorOnlyAndEvidenced` failed on one combined `./engines/ ./internal/server/` run, then passed in isolation and on three subsequent full runs of both packages. This is the documented shared-fixture flakiness this repo already has, not a defect in the 47.2/47.4 work — but if it recurs, both tests key off POS pricing and return-eligibility state that other tests in the same package mutate, so that is where to look first.
  - **One pre-existing failure is NOT mine and I left it alone:** `TestCuratedRepositoryManualsHaveResolvableLocalLinks` fails on two broken `/help/` links in `docs/kb/module-handbooks/inventory-wms-operations.md` pointing at `docs/generated/capability-catalog.md` and `docs/data/master-data-governance.md`. Both target files exist but are **untracked** — the concurrent Stage 48 docs-governance session's in-flight work. Theirs to finish registering.



  **🛑 DATA-LOSS INCIDENT, 2026-09-11 (dev DB only — but read this before running ANY engines test).** The peer-built `engines/audit_archive_test.go` (47.7.6, uncommitted) **destroyed the dev database's entire audit history**: `tenant_default.audit_logs` went from **389,431 rows to 1**. Measured, not inferred. Mechanism, from the code: `archiveTestSetup` writes a "boundary" checkpoint to seal all pre-existing rows (its own comment: "hundreds of thousands of historical rows"), then calls `RunAuditArchive`, which archives **every eligible checkpoint window** — including that boundary. It wrote 418,052 rows (seq 1–418,674) to `AUDIT_ARCHIVE_DIR = t.TempDir()`, which Go deletes when the test ends. All 16 archive files are gone; no dev backup exists; **the rows are unrecoverable**. Consequences: (a) `TestAuditLegacyRowsAreSealedNotFabricated` now *skips* on this DB (no legacy rows left to test against); (b) any investigation needing pre-2026-09-11 dev audit history is impossible. **Production is NOT affected** — it has its own `audit_logs` and the nightly backup — but this code carries a production-risk defect: it archived 2-month-old rows under the shipped 365-day `archive_after_days` policy, so the retention age check is either broken or bypassed by the test; if that reaches the hourly scheduler on prod, the first tick could archive everything. **Before this file is committed or run again** it needs: (1) archive selection that cannot sweep a boundary checkpoint the test itself created — or the test run on a scratch schema; (2) the age check proven against the policy; (3) restore before the TempDir is cleaned; (4) a guard refusing to archive a window whose `row_count` exceeds a sane per-run bound. All four live `erp-*` sessions were told to stop running it. I did not edit the peer's file.

  **⚠ CROSS-SESSION: a peer (erp-f8) built 47.7.6 in `engines/audit_archive.go` (+ test, + `db/migrations_stage47_7_6_audit_archive.sql`) and patched `verifyCheckpoints`/`digestRange` in my `audit_evidence.go` additively. That session ENDED before my review reached it, and a successor is actively editing `audit_archive_test.go` (mtime 07:41 on 2026-09-11). Whoever continues it must read this:**
  1. **Their design is sound and better than I first assumed.** `verifyCheckpoints` line ~268 still compares the archive-derived digest/count against the checkpoint's *signed* values, so repointing `archive_id` at an unrelated archive fails. Their `checkpoint_id` match and `restored_at` guards in `verifyArchivedWindow` are correct. Their `digestSigIDPairs` extraction is byte-identical (evidence: under concurrent-test interference the mismatch was on *count*, not digest — drift would fail every checkpoint on digest with matching counts).
  2. **ONE RESIDUAL GAP, not yet fixed:** `restored_at` is a nullable, **unsigned** column — `signArchiveManifest` (audit_archive.go ~line 350) covers `version|tenant|checkpointID|fromSeq|toSeq|rowCount|unsignedCount|rowDigest|fileSHA` and nothing else. So the DB-write attacker their restore guard defends against can `UPDATE audit_archives SET restored_at = NULL` before repointing `archive_id`, and all three `verifyArchivedWindow` guards then pass. The guard is defeated by exactly the capability it defends against. **Fix:** on restore, DELETE the `audit_archives` manifest row instead of flagging it (file may stay on disk); with no manifest, `loadArchiveMeta` fails → fail closed. If a restore audit trail is wanted, write it as a *signed* row via `WriteAuditEvidenceTx` (`entity_type='AuditArchive'`) in the same transaction as the delete — evidence that cannot be quietly cleared, reusing the mechanism 47.7 is built on. Then `restored_at` and its check become unnecessary.
  3. **Shared-DB test hazard, now proven real:** audit checkpoints seal a `seq` window, so ANY test whose cleanup deletes `audit_logs` rows inside a window sealed by a concurrently-running test breaks that test's checkpoint verification. Two `engines.test.exe` processes were observed running at once (mine + a peer's) and both failed on each other — "416058 rows now, 416060 at checkpoint" is this, not a defect. **Do not run `go test ./engines/` from two sessions simultaneously.** 27 leftover `AUDIT*-`-marker rows were in `audit_logs` afterwards; expect them.

  **Still open:** 47.7.6 (peer's, mid-flight, with the gap above); 47.6.6 (physical hardware). **Six migrations applied to dev by hand, NONE to production.** Not committed — review `git status` before staging; at least three sessions' work is in this tree.

- **Objective:** complete Stage 48 documentation governance. Technical work and verification
  are in progress; qualified product/process/legal/UAT approval and reviewed migration commits remain separate gates.
- **HEAD:** `fed51b4` at this capture. Work is **uncommitted**. Run `git status` before editing/staging;
  preserve concurrent Stage 49 changes, and stage only individually reviewed files.
- **Stage 48:** safe staged generators/checks; capability and reverse requirement traceability;
  owned core drafts; 49 KB topics including all previously missing screen topics; generated manuals;
  dictionary captured from 199 doctypes/1,542 fields; help-link guards, cached external checks and metadata inventory.
  [Checklist](micro_checklist.md) owns remaining items; [migration register](governance/migration-and-retention-register.md)
  and [24-path migration review](governance/migration-review.md) plus [monthly/quarterly procedure](governance/health-and-walkthroughs.md) own governance review.
- **Recovery:** detached local worktree `stage48-baseline-20260909` preserves 889 hash-verified files
  from the pre-migration state. [Manifest](assurance/stage48-baseline-2026-09-09.json) records identities;
  capture is not an approved commit/tag and must not be presented as release acceptance.
- **Concurrent Stage 49:** edge/static method hardening and password/session controls are present.
  Passwords use the shared policy; tokens now carry `cv`. `SignToken` takes a sixth credential-version
  argument: new fixture users use their initialized value; persistent users require the current DB value.
  The local development password-hardening migration was applied by that session; production was not.
  Stage 49.2.4 recovery-email reauthentication and dual-control helpdesk reset are closed locally
  per ledger §141; its additional migration is also local-only. Inspect the shared diff before staging.
- **Concurrent Stage 47.7:** signed audit events/checkpoints, transaction helper, retention/hold schema
  and tests appeared after the first docs snapshot. Preserve these files. The live checklist records
  the selected model; 47.7.6 archive/restore remains open. Documentation acceptance is still separate.
- **Checks:** current build/vet and fresh generator/linter/KB/docgen tests pass. The broad application
  suite is not green: fresh-schema `Store` module/role-registry mismatch reproduces; shared fixture/time
  and server-order failures need QA follow-up. See [September 10 verification](assurance/stage48-verification-2026-09-10.md).
  Prior 14-screen capture and manual keyboard/reflow checks passed; human acceptance remains pending.
- **Next safe action:** review the Stage 48 evidence and migration package, resolve DOC48-07 with the
  QA/data/access owners, and obtain the remaining scoped approvals before individual migration batches. Do not deploy or remove evidence.

## Current State (check before trusting anything above)

The live [micro-checklist](micro_checklist.md), [ledger](project_ledger.md), source and actual environment
take precedence over this dated snapshot. A test-file reference is not a test result.

## 7. Handover Notes for Incoming AI (Claude / Codex / Gemini)

Keep this handover under 150 lines. Put stable procedures in owned engineering/operations docs and
dated execution history in records. Update this §6, the checklist and ledger together. Never rewrite
historical findings or claim customer/qualified approval on the basis of local automated checks.
