---
doc_id: DOC-REQ-CONTROLS
title: Cross-cutting control requirements
type: normative
status: draft
owner: security-owner
approvers: [security-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Cross-cutting control requirements

These draft controls apply to both reference candidates. They describe acceptance obligations, not completed Stage 47/49 work. The [capability register](../product/capability-register.json) links them to every affected domain and the generated trace shows the reverse mapping.

## SEC-001

Every read, write, report, import/export, job and extension must enforce the authenticated tenant and applicable entity, location, owner, self and field scope before disclosure or effect. Acceptance: wrong-scope, revoked-session and alternate-path tests disclose no protected data and change no protected balance. Design: [access policy](../security/access-control-policy.md), Stage 47.1 and 49.2/49.3.

## SEC-002

Economic overrides and privileged recovery must preserve the approved separation of duties, actor identity and reason. Test maker/checker conflicts, forged context, replay and concurrent changes. A generic document endpoint or administrator role must not silently bypass the business command. Open privileged recovery and regulated audit controls remain release gates.

## SEC-003

Release evidence must identify dependencies, vulnerability findings, artifact identity, mitigations, accountable approval and expiry. Scan results without review do not approve a dependency or a release. Use the [secure development workflow](../security/secure-development.md).

## DATA-001

Every import, master publication and economic command must have validated source identities and reconcile input, accepted, rejected and committed outcomes. A retry must not duplicate a durable effect. Dictionary, migration mapping, rejection evidence and source/control tests must agree for the target tenant schema.

## DATA-002

Collection, access, export, retention, legal hold, anonymization and erasure must follow the signed customer/jurisdiction schedule. Acceptance includes dependent stores, jobs, logs, files and restoration behavior. No universal statutory duration or automatic permission to purge is defined here. Use the [privacy applicability review](../legal/privacy-applicability.md).

## OPS-001

Before release, demonstrate backup integrity, isolated restoration, recovery time/data-loss measurements, migration compatibility and rollback or forward-recovery decisions for the declared configuration. Preserve dated commands, artifact hashes, reconciliation and accountable approval in assurance records.

## OPS-002

Jobs, outbox deliveries and integration retries must expose saved outcome, attempts, failure reference and recovery action without leaking secrets. Operators must be able to distinguish waiting, failed and delivered work and reconcile uncertain external outcomes before retry. Replica/restart and load claims need Stage 47 evidence.
