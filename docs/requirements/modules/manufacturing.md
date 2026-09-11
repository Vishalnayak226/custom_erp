---
doc_id: DOC-REQ-MFG
title: Manufacturing and planning requirements
type: normative
status: draft
owner: manufacturing-process-owner
approvers: [product-owner, manufacturing-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Manufacturing and planning requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-MFG-001

**Plan inputs.** Identify BOM revision, demand, lead times, calendar and supply assumptions for each plan.

Acceptance: Cycles, missing units and obsolete revisions produce explainable rejects.

## FR-MFG-002

**Production custody.** Reserve/issue material and capture completion, scrap, QC and work-order cost variance.

Acceptance: Partial completion and replay cannot double-consume materials or overstate finished goods.

## FR-MFG-003

**Scheduling.** Explain capacity conflicts and planned/actual dates with owner and override reason.

Acceptance: Impossible capacity remains an exception and cannot be presented as an approved feasible plan.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 37.10, 26.9. A test file is not signed execution evidence.
