---
doc_id: DOC-REQ-BUY
title: Procurement requirements
type: normative
status: draft
owner: procurement-process-owner
approvers: [product-owner, procurement-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Procurement requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-BUY-001

**Approved demand.** Link requisition, sourcing, approved PO and amendments to supplier, legal entity and location.

Acceptance: An unapproved or stale PO cannot be received/paid through UI, API or import bypass.

## FR-BUY-002

**Three-way match.** Match invoice to approved PO and accepted receipt/tolerance before payment approval.

Acceptance: Overreceipt, duplicate supplier invoice and currency/tax mismatches produce explained control outcomes.

## FR-BUY-003

**Liabilities.** Preserve supplier returns, credits and payment allocation as posted evidence.

Acceptance: Partial receipt/invoice/payment and cancellation reconcile vendor liability to GL.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 17, 47.13. A test file is not signed execution evidence.
