---
doc_id: DOC-REQ-PIM
title: Product and master data requirements
type: normative
status: draft
owner: data-owner
approvers: [product-owner, data-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Product and master data requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-PIM-001

**Golden record.** Define business key, source, steward, effective revision and approval for items and channel projections.

Acceptance: Conflicting supplier records and duplicate SKU/barcode require an attributable merge/reject with provenance.

## FR-PIM-002

**Publication.** Validate mandatory content, media, locale, unit and channel fields before approval and publication.

Acceptance: Invalid content cannot publish; retries retain one effective revision with per-channel outcomes.

## FR-PIM-003

**Import reconciliation.** Preview dependencies/transforms, retain row-level rejects and control totals, and support idempotent reruns.

Acceptance: Dirty repeated 10k/100k packages reconcile attempted/accepted/rejected/unchanged counts without formula execution.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 36, 47.13. A test file is not signed execution evidence.
