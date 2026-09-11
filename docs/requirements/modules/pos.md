---
doc_id: DOC-REQ-POS
title: Point of sale requirements
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

# Point of sale requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-POS-001

**Authoritative economics.** The server derives permitted price, tax, offers and cost from tenant configuration.

Acceptance: Alter every amount, stale quote and cross-location override; reject or requote without posting.

## FR-POS-002

**Atomic outcome.** A tenant command key produces at most one reconciled sale across stock, document, GL, tax, tender and loyalty.

Acceptance: Inject failure at each mutation boundary and retry after response loss; prove zero or one complete economic outcome.

## FR-POS-003

**Tender and recovery.** Show outstanding tender, provider status and what saved; reconcile cashier close and explain safe retry.

Acceptance: Exercise split tender, provider timeout and restart; no duplicate charge or unowned difference.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 47.2, 47.3. A test file is not signed execution evidence.
