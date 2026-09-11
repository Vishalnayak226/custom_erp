---
doc_id: DOC-REQ-CRM
title: Customer relationship and loyalty requirements
type: normative
status: draft
owner: customer-process-owner
approvers: [product-owner, customer-process-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Customer relationship and loyalty requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-CRM-001

**Purpose and identity.** Record customer identity/contact purpose and consent where required; steward merges duplicates.

Acceptance: Unauthorized export and cross-tenant lookup fail; merging preserves transactions and consent history.

## FR-CRM-002

**Points ledger.** Earn, redeem, reverse and expire points through evidence coupled to the authoritative transaction.

Acceptance: Refund/retry cannot earn or spend twice; displayed balance reconciles to events.

## FR-CRM-003

**Campaign control.** Preview eligible audiences and separate simulated tests from real dispatch.

Acceptance: Opt-out/suppression apply at delivery; dry-run work cannot contact a real customer.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 26.7, 47.3. A test file is not signed execution evidence.
