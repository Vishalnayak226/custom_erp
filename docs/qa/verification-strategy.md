---
doc_id: DOC-QA-001
title: Verification strategy and evidence boundaries
type: normative
status: draft
owner: qa-owner
approvers: [qa-owner, product-owner, security-owner, operations-owner]
audience: [QA, engineering, implementation, release-reviewers]
applies_to: named reference configurations and releases
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Verification strategy and evidence boundaries

Use [requirements traceability](../generated/requirements-traceability.md) to locate tests
and help. A location is not an execution result. The acceptance record identifies release,
configuration, fixtures, commands, outcomes, reviewers and immutable evidence references.

| Test layer | Required questions |
|---|---|
| Unit/contract | Can malformed input, invalid lifecycle, missing evidence or duplicate identity violate the contract? |
| Real database | Do concurrent/replayed/failing economic commands preserve tenant scope, ownership and reconciliation? |
| HTTP/authorization | Do wrong tenant/role/scope/field/state and alternative API/import/export paths fail before effect/disclosure? |
| Browser/task | Can real users perform and recover the task on the declared role/device without clipping, overlays or inaccessible controls? |
| Performance/capacity | Does the declared representative dataset/load meet NFR percentiles, memory, bytes and bounded growth? |
| Operations/resilience | Can the declared deployment restore/rollback, rotate/revoke, recover jobs and prove required evidence? |
| Documentation | Are links/metadata/ownership and generated outputs current, checks read-only, help findable and manuals consistent? |

Use isolated tenants/databases and synthetic non-sensitive data. Keep external delivery
simulated unless a specific provider/environment trial is authorized. Record failing controls
as open release gates; a quarantined red-team test does not become a pass merely because it
is excluded from the ordinary suite. Physical scanner/printer and real-user acceptance cannot
be replaced with a desktop screenshot or source-level CSS assertion.

Blank [UAT checklist](../guides/UAT_CHECKLIST.md), [UAT run sheet](../operations/uat_run_sheet.md)
and [release template](../governance/release-acceptance.md) are reusable forms. Completed
records are dated and preserved. [Restore drill log](../operations/restore_drill_log.md) is
historical evidence; [backup runbook](../operations/backup_restore.md) is a procedure. Retain
this distinction when splitting or migrating the existing operations documents.
