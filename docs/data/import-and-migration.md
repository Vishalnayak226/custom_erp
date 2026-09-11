---
doc_id: DATA-IMPORT-001
title: Import and data migration controls
type: normative
status: draft
owner: data-owner
approvers: [data-owner, process-owner, qa-owner]
audience: [implementation, data-stewards, engineering, QA]
applies_to: one approved tenant and migration package
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Import and data migration controls

A package identifies source owner, tenant, schema/release, template version, dependency
order, transforms and business control totals before execution. Preview invalid keys,
foreign references, unit/locale ambiguity, duplicate barcode/tax identities, BOM cycles and
formula-bearing spreadsheet cells before applying data. Restrict raw files to their approved
purpose and access class; the retention owner decides expiry and legal holds.

| Step | Required outcome |
|---|---|
| Discover and map | Source-to-target meanings, keys, scope, defaults and transformation reason signed by data/process owner |
| Preview | Counts and money/quantity totals; explain each accepted, rejected, unchanged and conflicting row |
| Execute | Unique package/row identity, bounded processing, explicit failure/partial-success policy and resumable outcome |
| Reconcile | Input = accepted + rejected + unchanged; stock/money/ownership totals agree to approved source |
| Rerun | Same package converges without duplicate masters or economic effects; changed package is a new reviewed version |
| Recover | Rollback/compensation boundaries are stated; posted financial history is not silently overwritten |
| Accept and retain | Owner records exceptions/approval; retain necessary evidence and expire raw data by approved policy |

Existing entry points include [CSV import](../../engines/import.go) and
[PIM import tests](../../engines/pim_import_test.go). These are implementation references,
not proof that all dirty 10k/100k packages or every domain rerun already pass. Stage 47.13
owns the remaining hardening and executable acceptance.
