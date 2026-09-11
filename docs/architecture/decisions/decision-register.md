---
doc_id: DOC-ADR-INDEX
title: Architecture decision register
type: reference
status: draft
owner: engineering-owner
approvers: [engineering-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Architecture decision register

New architecture decisions receive a stable ADR ID before implementation. A descriptive record of existing code is not retrospective approval. The [ADR template](../../governance/adr-template.md) records alternatives, consequences, controls, scope and approval reference.

| ID | Decision / current observation | State and next gate |
|---|---|---|
| ADR-001 | [Existing lightweight topology](2026-09-07-existing-topology.md) | Recorded source observation; one Go app, PostgreSQL, native browser assets |
| ADR-002 | Tenant schema plus explicit authorization dimensions | Current pattern recorded below; security review required for any boundary change |
| ADR-003 | Signed tokens plus live identity/credential state | Current pattern; 49.2.4 recovery controls verified locally, production migration and broader Stage 49 gates remain |
| ADR-004 | Atomic economic commands and durable command identity | POS/returns implemented paths; other domains require their own proof |
| ADR-005 | Enforce one stock owner per warehouse | Stage 47.5 records the 2026-09-09 scope decision; mixed-owner mode explicitly unsupported |
| ADR-006 | Independently signed audit events with range checkpoints | Model selected and implemented in 47.7; archive/restore and qualified evidence acceptance remain open |
| ADR-007 | PostgreSQL-backed jobs/outbox with in-process workers | Current pattern; replica, cancellation and recovery/load proof required |
| ADR-008 | Native JavaScript and no speculative dependencies | Standing project constraint; any exception needs measured benefit and review |

## ADR-002 — Tenant isolation

The existing design uses tenant schemas and explicit route/engine scope. Shared-row isolation and a database per tenant remain alternatives requiring separate migration/operations analysis. Schema isolation alone does not enforce entity/location/owner scope. Consequence: every alternate path must use the same authorized context and tenant-safe transactions; backups and platform tooling have broader privileges.

## ADR-003 — Sessions

The source uses signed tokens, live user-state checks and credential-version revocation. Cookie-backed sessions or a dedicated identity provider would change client, revocation, recovery and deployment contracts and require a new reviewed ADR. Preserve current revocation tests when changing claims. Stage 49 work may change this draft; its backlog and source are authoritative for unfinished recovery controls.

## ADR-004 — Economic commands

POS checkout and returns use dedicated transaction paths and command identity. A sequence of independent generic writes cannot be substituted without proving all-or-nothing economic effects and replay behavior. Consequence: APIs, imports and retries must preserve the same controls and reconciliation; external settlement remains a distinct outcome.

## ADR-005 — Owner isolation

The later Stage 47.5 work record documents the user-selected single-owner-per-warehouse closure. `engines/wms_single_owner.go` and its additive migration enforce the default owner-stock boundary; allocation and picking still do not filter by owner. The explicit `mixed_unsupported` opt-in is outside supported operation. This supersedes the earlier draft target; it does not certify deployment of the guard or authorize a broader Production claim.

## ADR-006 — Audit evidence

The Stage 47.7 work record identifies the user's 2026-09-09 choice of independently signed
events plus range checkpoints. The [implementation](../../../engines/audit_evidence.go) avoids
a previous-row dependency on every audit write; checkpoints detect removal from sealed ranges
and chain the checkpoint series. `WriteAuditEvidenceTx` supports evidence committed with a
business mutation. Legacy rows are sealed with an explicit coverage boundary, not retroactively
represented as independently signed events. The durable runner schedules checkpoint/verification
work and detects overdue verification. Consequences: key/version handling and checkpoint custody
remain operational controls, and unsealed ranges have different deletion evidence. Stage 47.7.6's
archive/export/restore work and qualified legal/security acceptance remain pending; this is not
an immutability or compliance certification.

## ADR-007 / ADR-008 — Execution cost

Current jobs, outbox and browser assets reuse the existing process and database. External brokers, caches, search services and UI frameworks would introduce lifecycle and failure modes. Propose them only against measured constraints, with a migration/rollback and operating-cost comparison. Current topology does not establish representative-scale performance.
