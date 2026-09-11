---
doc_id: DOC-B2496E8029
title: Change and validation
type: reference
status: draft
owner: engineering-owner
approvers: [documentation-maintainer, engineering-owner]
audience: [maintainers, engineering-owner]
applies_to: source documentation; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Change and validation

Describe the concrete problem and resulting behavior, then record relevant validation.

- Requirements / support scope: affected documents, or reason unchanged.
- Architecture / ADR / API / data migration: affected contracts and compatibility.
- Security / privacy / permissions: affected controls and negative-case evidence.
- User help / operations / recovery: updated KB or runbook and task verification.
- Release notes / evidence: customer action, known limits and test results.

For high-risk work, explain any “not applicable” answer. Run
`pwsh docs/update-docs.ps1 -Group Content -Check` for generated documentation changes.
Governance metadata/link health is advisory until its existing findings are resolved.
