---
doc_id: DOC-REQ-RET
title: Returns and refunds requirements
type: normative
status: draft
owner: store-process-owner
approvers: [product-owner, store-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Returns and refunds requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-RET-001

**Eligibility.** Resolve original price/tax/cost and cumulative returnable quantity from the sale under a concurrency-safe lock.

Acceptance: Concurrent partial returns cannot exceed sold quantity; altered economics and foreign-tenant sale IDs fail.

## FR-RET-002

**Inspection and refund.** Separate receipt, QC disposition, refund approval and payment outcome; quarantined goods stay unavailable.

Acceptance: Refund before inspection fails; QC/refund replay changes stock, GL and original tax once.

## FR-RET-003

**Exceptions.** No-receipt and goodwill require capability, reason and review; the instant-return legacy API remains retired.

Acceptance: Wrong-role exceptions and direct legacy calls fail; goodwill never bypasses cumulative sale quantity.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 47.4. A test file is not signed execution evidence.
