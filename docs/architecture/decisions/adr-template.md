---
doc_id: DOC-GOV-ADR
title: Architecture decision template
type: template
status: draft
owner: engineering-owner
approvers: [engineering-owner]
audience: [engineering, security, data, operations]
applies_to: one governing decision
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Architecture decision template

An ADR records a decision and its consequences, not a promise of future functionality.
Assign a stable ADR ID. Separate proposed from accepted status and link the actual approving
record; a developer's description cannot imply domain/legal/customer approval.

| Required section | Record |
|---|---|
| Context and concern | Trigger, affected users/controls, requirement IDs, source/release |
| Constraints and alternatives | Existing topology, cost/performance/security data and viable options |
| Decision | Exact boundary, selected option and accountable approvers |
| Consequences | Benefits, limitations, failure modes, privacy/operation/resource costs |
| Implementation and proof | Stage/issue, migration/rollback, tests, help and release evidence |
| Review | Trigger/date, supersedes/superseded-by, unresolved scope |

For an existing governing constraint, label the record as a retrospective description and
cite its existing authority. Do not invent historical signatures. Rejected options and
superseded decisions remain retained records with replacement links.
