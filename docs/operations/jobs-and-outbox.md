---
doc_id: OPS-JOB-001
title: Job and outbox recovery
type: procedure
status: draft
owner: operations-owner
approvers: [operations-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Job and outbox recovery

The existing [job runner](../../engines/jobrunner.go) and [outbox worker](../../engines/outbox.go) execute asynchronous work in the application process using PostgreSQL state. Handlers and providers differ; do not assume every job is safe to replay.

1. Confirm tenant, job/event ID, type, status, attempt count, timestamps and correlation reference through the authorized operations view/API.
2. Inspect the recorded failure and any external provider outcome. Distinguish not-started, failed-before-effect and unknown-after-send cases.
3. Resolve configuration, permission, payload or provider problems through their owning procedure. Do not edit durable payloads or mark work successful directly in SQL.
4. For retry-capable work, use the authorized retry action after proving idempotency/reconciliation. The server reports queued-for-retry; that is not delivered success.
5. Use cancellation only where the registered handler and workflow support it. It does not reverse a provider action already completed.
6. Observe the terminal outcome and reconcile the business/provider reference. Preserve attempts, operator identity and reasoning in the execution record.

Escalate repeated failures, backlog growth, tenant-scope uncertainty and ambiguous external outcomes with IDs and redacted diagnostics. Restart/replay/concurrent-worker acceptance belongs to the release's resilience evidence. Job/outbox state is not a substitute for customer-facing settlement or audit acceptance.
