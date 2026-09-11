---
doc_id: DOC-GOV-RELEASE
title: Release acceptance and evidence template
type: template
status: draft
owner: qa-owner
approvers: [product-owner, qa-owner, security-owner, operations-owner]
audience: [release-reviewers, QA, operations, product]
applies_to: one named release and configuration per executed record
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Release acceptance and evidence template

This blank template is not an approval or executed record. Copy it to a dated assurance
record only for an actual review. Retain completed evidence and corrections as separate
records; do not overwrite an earlier failure with a later pass.

| Field | Required record |
|---|---|
| Identity | Evidence ID, UTC execution date, commit/artifact checksum, application/schema version |
| Scope | Tenant/environment identifier, country, industry, owner model, devices, integration/provider versions |
| Need and controls | BR/FR/NFR/SEC/DATA/OPS IDs and applicable design/ADR/work items |
| Automated evidence | Exact commands, fixtures/load, outputs and failures, with immutable artifact identifiers |
| Human evidence | Role participants, task/device steps, observed outcomes, interventions, accessibility and recovery |
| Reconciliation | Expected/actual stock, ownership, ledger, tender, tax, loyalty and audit outcomes as applicable |
| Operations | Provisioning, upgrade/migration, restore/rollback and support readiness |
| Customer change | Behavior changes, action/configuration/migration required, compatibility and known limits |
| Decision | Accept/reject/conditional, exception owner/expiry, named qualified reviewers and signatures/approval references |

Production entries in the capability source must link requirement, design, test, help and
accepted release evidence with an accountable approver. Merely creating a file cannot prove
the signatures or results: reviewers must inspect the underlying execution and scope.

The validator requires executed-record front matter: `type: record`, `result: accepted`,
`release` matching the registry, one `configuration`, `approved_by` matching the capability
approver, `approved_on` in ISO date form, `doc_id`, `approval_reference` and `artifact_sha256`.
Each claimed configuration needs a matching record; Certified also requires
`certification_reference`. These checks validate structure and scope, not the authenticity
of a signature or truth of a test result. Qualified reviewers verify those references.

Monthly health review: run offline doclint and archive its JSON with release/commit; assign
stale/unowned/orphaned/broken/help/budget findings. Quarterly, sample tasks with business,
floor, tenant-admin, developer, operations, implementation and QA/security participants;
include qualified audit/legal reviewers where applicable. These reviews are scheduled by
their owners; the tooling does not fabricate execution or attendance.
