---
doc_id: DOC-REQ-KB
title: Knowledge Center requirements
type: normative
status: draft
owner: documentation-maintainer
approvers: [product-owner, documentation-maintainer, qa-owner]
audience: [product, engineering, QA]
applies_to: proposed reference configurations; release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Knowledge Center requirements

Intended behavior drafted from the existing backlog. This does not claim availability. See the [capability catalog](../../generated/capability-catalog.md) for scoped maturity and [PRD core](../product-requirements.md) for shared rules.

## FR-KB-001

**Canonical source.** One KB topic owns each task/tutorial/reference/explanation/recovery instruction; manuals are curated projections.

Acceptance: Manual and in-app articles agree on the same source version; no competing task copy.

## FR-KB-002

**Context and audience.** Map each user view to role, task, prerequisites, help owner, verified release and recovery behavior.

Acceptance: Every supported screen opens relevant help; wrong-role content and missing mapping are detectable.

## FR-KB-003

**Accessible help.** Serve bounded authenticated help with usable headings, keyboard navigation, meaningful links and media alternatives.

Acceptance: Real owner/floor users complete reference tasks and find help in two decisions; retain observed results.

## Review and evidence

The process owner validates the outcome; QA executes the scenarios. [Traceability](../../generated/requirements-traceability.md) identifies implementation/test/help locations. Open Stage work: 39, 48.7. A test file is not signed execution evidence.
