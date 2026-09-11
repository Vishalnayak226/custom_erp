---
doc_id: DOC-REQ-OMS
title: Order management requirements
type: normative
status: draft
owner: order-process-owner
approvers: [product-owner, order-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Order management requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-OMS-001

**Intake identity.** Identify channel, external order/version and tenant; duplicate deliveries converge to one order.

Acceptance: Replay events and conflicting versions; no duplicate demand or silent overwrite.

## FR-OMS-002

**Fulfillment.** Allocate eligible owner/location stock and record pick, pack, shipment and cancellation transitions.

Acceptance: Partial stock, concurrent cancellation and duplicate shipment preserve reserved/shipped quantities.

## FR-OMS-003

**Settlement.** Assign exceptions and reconcile order, shipment, invoice, fees and settlement.

Acceptance: A partial settlement or courier failure remains owned; closing a batch requires explained totals.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Stage 35 owns remaining domain work; Stage 47.5 records the single-owner guard and its scope. Mixed-owner operation is unsupported. A test file is not signed execution evidence.
