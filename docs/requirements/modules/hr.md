---
doc_id: DOC-REQ-HR
title: People and payroll requirements
type: normative
status: draft
owner: hr-process-owner
approvers: [product-owner, hr-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# People and payroll requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-HR-001

**Identity lifecycle.** Link employment and access without giving general HR platform authority.

Acceptance: Disable/termination revokes effective access; manager/self scope applies to every lookup/export.

## FR-HR-002

**Private payroll.** Enforce sensitive salary, bank and personal-field restrictions before serialization.

Acceptance: Wrong-role list/detail/search/export cannot infer protected data through totals or errors.

## FR-HR-003

**Payroll evidence.** Version approved inputs, parameters, period and outputs; qualified payroll review precedes statutory/payment use.

Acceptance: Reruns and adjustments preserve prior evidence and reconcile approved payroll to payment/GL.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 26.8, 47.1, 47.16. A test file is not signed execution evidence.
