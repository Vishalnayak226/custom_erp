---
doc_id: DOC-REQ-INV
title: Inventory and transfers requirements
type: normative
status: draft
owner: inventory-process-owner
approvers: [product-owner, inventory-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Inventory and transfers requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-INV-001

**Scoped quantity.** Keep UOM, lot, serial, location and owner consistent between availability and the stock ledger.

Acceptance: Concurrent reservations, releases and picks cannot create stock or cross a hold/owner boundary.

## FR-INV-002

**Transfer custody.** Represent dispatch, in-transit, receipt and shortage/damage as evidenced transitions.

Acceptance: Lost responses, partial receipt and repeated scans preserve custody and quantity.

## FR-INV-003

**Correction.** Use an approved adjustment or reversal with reason and correlation; retain posted movement history.

Acceptance: Replay changes quantity once and reconciles to its source and accounting effect.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Stage 42 records warehouse depth; Stage 47.5 records the single-owner guard. Configuration-specific release evidence remains required. A test file is not signed execution evidence.
