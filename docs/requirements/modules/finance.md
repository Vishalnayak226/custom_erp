---
doc_id: DOC-REQ-FIN
title: Finance and tax requirements
type: normative
status: draft
owner: finance-process-owner
approvers: [product-owner, finance-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Finance and tax requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-FIN-001

**Balanced posting.** Use precise currency amounts, balanced debit/credit and legal-entity scope for economic events.

Acceptance: Boundary rounding, FX and multi-leg failure produce a balanced posting or no posting.

## FR-FIN-002

**Period close.** Reject unauthorized backdated posting and reconcile stock, receivables, payables, cash and tax before close.

Acceptance: Closed-period posting and unauthorized reopening fail; signed close identifies unresolved exceptions.

## FR-FIN-003

**Tax and payment approval.** Version tax treatment and payment parameters; qualified finance review determines applicable use.

Acceptance: Approved intra/interstate, exemption, return and rate-change fixtures reconcile; beneficiary change/duplicate payment cannot bypass review.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 37, 47.7, 47.16. A test file is not signed execution evidence.
