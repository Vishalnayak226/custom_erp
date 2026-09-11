---
doc_id: DATA-MDM-001
title: Master-data governance
type: normative
status: draft
owner: data-owner
approvers: [data-owner, process-owners, privacy-owner]
audience: [data-stewards, implementation, engineering]
applies_to: approved tenant master-data domains
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Master-data governance

Every master domain needs a business system of record and steward in addition to generated
schema facts. This policy is intended behavior; current schema and release support must be
verified against the [capability catalog](../generated/capability-catalog.md).

| Domain | Accountable business owner | Required key/scope and approval concern |
|---|---|---|
| Organization/entity/location | Business administrator | Tenant/entity/location identity, operating dates, authoritative legal details |
| Item/variant/UOM/barcode | Product data steward | Stable business key, packaging/UOM, category and allowed duplicate/merge rules |
| Supplier and bank details | Procurement + finance | Legal supplier identity, effective beneficiary and independent change control |
| Customer and contacts | Customer process + privacy | Identity, purpose, consent where applicable, duplicate/merge and access scope |
| Employee/payroll | HR + privacy | Employment identity, self/manager scope, sensitive data and access lifecycle |
| Tax/GL/currency | Finance | Effective rules, version/source, qualified applicability and posting dependencies |
| Warehouse/bin/stock owner | Warehouse process | Tenant/location/owner identity and custody rules; one owner per warehouse under the 47.5 guard; mixed-owner use unsupported |

Before a change becomes effective, record source/provenance, steward, approver, dependencies,
effective dates, validation and downstream consumers. Define conflict precedence explicitly;
never use arrival order as an unexplained survivorship rule. A merge retains aliases,
source IDs, transaction linkage and reason; irreversible combinations need reviewed preview
and recovery. Duplicate deletion is not a merge policy.

Quality checks cover business key uniqueness, referential integrity, units, country/locale,
cyclic hierarchies/BOMs, required channel attributes and restricted-field classification.
[Import/migration controls](import-and-migration.md) define reconciliation; the
[data lifecycle draft](data-lifecycle.md) defines retention/hold decisions.

The [generated dictionary](generated/dictionary.md) combines an explicitly scoped,
versioned registry capture with owned business definitions. Follow the
[dictionary workflow](dictionary-workflow.md) to refresh either layer. Its development
snapshot and draft meanings do not establish another deployment's schema or approved retention policy.
