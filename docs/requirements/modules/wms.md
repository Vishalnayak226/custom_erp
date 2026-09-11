---
doc_id: DOC-REQ-WMS
title: Warehouse operations requirements
type: normative
status: draft
owner: warehouse-process-owner
approvers: [product-owner, warehouse-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Warehouse operations requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-WMS-001

**Warehouse ownership boundary.** The selected reference configuration has one owner per warehouse. Enforce assignment and owner-stock writes consistently; refuse a second owner and reassignment while incumbent stock remains. Mixed-owner allocation/picking is a separate unsupported configuration, not an implied feature.

Acceptance: Exercise concurrent first assignments, second-owner writes, stocked-warehouse reassignment and alternate write paths. Confirm the default `single_owner` guard and explicitly identify any `mixed_unsupported` setting in deployment evidence. A future mixed-owner requirement must prove isolation across demand, reserve, allocate, pick, ship, return, count and adjustment before support can change.

## FR-WMS-002

**Receipt and custody.** Validate accepted/rejected quantity, source order, lot/serial/expiry and putaway policy.

Acceptance: Duplicate serials, expired stock and partial rejection produce explained decisions and correct owner/bin totals.

## FR-WMS-003

**Floor execution.** Provide one scan/action, recoverable task identity, supervisor escalation and approved physical devices.

Acceptance: Run receipt-to-load with gloves, scanner, interruption and weak Wi-Fi; retain hardware-specific results.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Stage 47.5 records the scope decision and guard evidence; physical-device and release evidence remain open under 47.6 and the capability register. A test file is not signed execution evidence.
