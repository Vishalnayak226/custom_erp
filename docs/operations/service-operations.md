---
doc_id: OPS-RUN-001
title: Running the service
type: procedure
status: draft
owner: operations-owner
approvers: [operations-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-10-26
review_by: 2026-12-26
supersedes: [OPS-REL-001, OPS-JOB-001]
superseded_by: none
---

# Running the service

Platform operators start here; tenant administrators use the [tenant admin manual](../user/tenant-admin-manual.html). Choose the approved environment and deployment record before running commands.

| I need to… | Section |
|---|---|
| Deploy or configure an environment | [Deploy and configure](#deploy-and-configure) |
| Watch health and react to a symptom | [Observe and respond](#observe-and-respond) |
| Ship a new release or back one out | [Upgrade and rollback](#upgrade-and-rollback) |
| Recover a failed background job or outbox message | [Job and outbox recovery](#job-and-outbox-recovery) |
| Know what each kind of work may use under load | [Availability model](#availability-model-stage-49131) |

## Deploy and configure

Follow the platform-specific [deployment procedure](../../deploy/README.md), with the release's source/artifact identity, migration set, credentials from the approved secret system and previous artifact/backup references. Native service deployment and optional container packaging are different configurations; record the one actually used. Do not copy a developer's trust-authentication database settings to production.

Record configuration keys and intended values by non-secret reference: database/TLS, tenant lifecycle, environment delivery mode, identity/session, backup locations/keys, outbound provider settings and alert destination. Validate required values without writing secrets to logs or evidence. Keep real provider actions disabled in development unless a scoped trial is authorized.

## Observe and respond

Check service readiness, error/latency percentiles, database connections/locks/storage, job/outbox backlog and oldest age, backup freshness and external delivery failures. Record dataset, tenant count, request mix and measurement window with every capacity claim. Use [NFRs](../requirements/nonfunctional-requirements.md) and Stage 47 as the proposed budgets; the deployment owner must approve actual SLO/RTO/RPO targets.

| Symptom | First safe action | Evidence / follow-up |
|---|---|---|
| Application unavailable | Inspect service and database health in the intended environment | Correlation/time, service logs and last deployment; follow [incident runbook](incident_runbook.md) |
| Growing latency/backlog | Inspect query/lock/pool and job age before increasing limits | Workload profile and measured bottleneck; [job and outbox recovery](#job-and-outbox-recovery) |
| Stale or failed backup | Verify scheduler, destination, key access and recent result | [Backup/restore procedure](backup_restore.md), then isolated drill |
| Wrong business result | Contain the affected path with authorized controls; preserve evidence | Reconcile sale/stock/GL/provider state; do not repair via ad hoc SQL |
| Unexpected external send | Verify environment/delivery controls and affected jobs | Preserve provider/command references; incident owner decides containment |

Secrets, on-call contacts and customer hostnames stay in approved configuration/contact systems. This document names accountable roles only. Record incident execution separately from the reusable procedure.

## Upgrade and rollback

Prepare a reviewed release record before changing a shared environment. Identify the target, release/artifact hashes, migration list, compatibility window, known limits, backups and accountable go/no-go owner.

### Before upgrade

Verify the release on an isolated representative fixture; run the migration sequence and required application/docs/security checks. Test old/new binary compatibility with additive schema changes. Capture backup identity and verify restoration separately. Plan any required downtime, session invalidation, provider pause and user action in reviewed release notes.

### Upgrade and observe

Use the existing deployment/migration tooling from [deployment](../../deploy/README.md). Apply the reviewed migrations in order, record results, start the intended artifact and verify readiness, identity/tenant access, a reconciled business smoke test, jobs and backup monitoring. Watch the agreed observation window before customer acceptance.

### Rollback decision

Stop further release actions when an acceptance gate fails. Compare the failure with the pre-approved rollback triggers and compatibility assessment. Reverting a binary is safe only when the current schema and newly written data remain compatible. Do not undo additive migrations or restore a database casually: that can discard valid business activity and external outcomes.

Choose the approved binary rollback, forward repair or recovery plan; preserve logs, backups and failed-artifact identity. If a restore is required, reconcile the data-loss window and provider state with the business owner. Record the resulting release, checks, outstanding risks and customer communication approval. A local test does not authorize production deployment.

## Job and outbox recovery

The existing [job runner](../../engines/jobrunner.go) and [outbox worker](../../engines/outbox.go) execute asynchronous work in the application process using PostgreSQL state. Handlers and providers differ; do not assume every job is safe to replay.

1. Confirm tenant, job/event ID, type, status, attempt count, timestamps and correlation reference through the authorized operations view/API.
2. Inspect the recorded failure and any external provider outcome. Distinguish not-started, failed-before-effect and unknown-after-send cases.
3. Resolve configuration, permission, payload or provider problems through their owning procedure. Do not edit durable payloads or mark work successful directly in SQL.
4. For retry-capable work, use the authorized retry action after proving idempotency/reconciliation. The server reports queued-for-retry; that is not delivered success.
5. Use cancellation only where the registered handler and workflow support it. It does not reverse a provider action already completed.
6. Observe the terminal outcome and reconcile the business/provider reference. Preserve attempts, operator identity and reasoning in the execution record.

Escalate repeated failures, backlog growth, tenant-scope uncertainty and ambiguous external outcomes with IDs and redacted diagnostics. Restart/replay/concurrent-worker acceptance belongs to the release's resilience evidence. Job/outbox state is not a substitute for customer-facing settlement or audit acceptance.

## Availability model (Stage 49.13.1)

What each kind of work may use, and what happens to it under pressure. Every limit below is in the code today (read 2026-10-26); the targets are **proposed** and wait for the owner's numbers.

| Work | Shares | Limit | Under pressure | Proposed target |
|---|---|---|---|---|
| Sign-in, MFA, health, cancel, Security controls | the client's request slots, plus a **priority lane** | `platform.per_tenant_max_concurrent_requests` (15) **+ 3 reserved** (49.13.2); sign-in 5/min per caller; username backoff and lockout | keeps its 3 reserved slots when ordinary work fills the 15 | sign-in p95 < 1 s |
| Sales, receipts, saves (command finalization) | request slots; the shared database pool (50 connections) | the client's 15 slots; reports may hold at most half | ordinary work answers 503 `Retry-After: 2`; reports cannot starve it | p95 < 2 s; never a lost or double sale (idempotency keys) |
| Lookups and search | request slots; per-caller rate limits | 100 searches/min, 60 other calls/min per caller; list pages capped (`platform.max_list_limit`) | 429 with `Retry-After`; the browser waits once, then retries | p95 < 1 s |
| Reports and exports | half the client's slots; **live report runs** | 7 report requests per client; live runs ≤ 7 per client and ≤ 20 per server, abandoned runs counted until their query ends (49.13.3); 5 min hard stop; 55 s per request | GLOBAL-0310 503 "still running", or Export runs it in the background | on-screen report p95 < 10 s; larger ones are exports |
| Background jobs and integrations | the job runner (scheduler leader only) | 10 jobs claimed per client per 10 s pass; bounded attempts, then dead-letter | queue; a poison job stops at its attempt limit (detection *Background jobs giving up*) | oldest ready job < 5 min (queue-saturation alert) |
| Restore | backups (nightly, plus the off-box copy) | RPO ≤ 24 h (≤ 26 h off-box); RTO not yet measured | - | RTO and RPO to be set by the owner and proven by the restore drill |

**What sheds first:** a flood of reports or exports (half-slot cap, then GLOBAL-0310), then ordinary calls over a caller's rate (429), then the client's ordinary slots (503). Sign-in and the emergency controls are the last to go (priority lane). One client's flood cannot take another client's slots: every limit above is per client, except the database pool and the 20 live report runs per server.

**Still open:** a measured load test of the normal-screen burst (47.11.3) and an RTO measured by a timed restore drill.
