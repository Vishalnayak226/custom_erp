---
doc_id: DATA-HANDBOOK-001
title: Data handbook
type: normative
status: draft
owner: data-owner
approvers: [data-owner, process-owners, privacy-owner, legal-owner, security-owner, engineering-owner, qa-owner]
audience: [data-stewards, implementation, engineering, privacy, security, operations, QA]
applies_to: approved tenant master-data domains, migration packages, retention decisions and dictionary projections
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: [DATA-MDM-001, DATA-IMPORT-001, DATA-LIFE-001, DOC-DATA-004]
superseded_by: none
---

# Data handbook

One page for the rules around business data: who owns each master domain, how data is
imported and migrated, how long it is kept, and how the data dictionary is refreshed.
This is intended behavior; current schema and release support must be verified against
the [capability catalog](../generated/capability-catalog.md).

| I need to… | Section |
|---|---|
| Know who owns customers, items, suppliers and other masters | [Master-data governance](#master-data-governance) |
| Load or migrate a customer's data | [Import and data migration controls](#import-and-data-migration-controls) |
| Decide how long data is kept or how it is erased | [Data lifecycle and retention decisions](#data-lifecycle-and-retention-decisions) |
| Refresh or read the data dictionary | [Data dictionary capture and review](#data-dictionary-capture-and-review) |

## Master-data governance

Every master domain needs a business system of record and steward in addition to generated
schema facts.

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
[Import and migration controls](#import-and-data-migration-controls) define reconciliation;
[the data lifecycle section](#data-lifecycle-and-retention-decisions) defines retention and hold decisions.

## Import and data migration controls

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

## Data lifecycle and retention decisions

This section defines the decision record, not legal retention durations or compliance.
Qualified owners must approve applicability before automated erasure/retention is enabled.
Stages 47.7, 47.16 and 49.6 retain the implementation and legal-control gaps.

Each category records purpose, system of record, personal/financial/authentication/audit
classification, tenant/entity/location/owner scope, permitted roles, export/masking,
retention trigger/duration, legal hold, deletion/anonymization method and evidence owner.
Include primary rows, JSON copies, files, imports, search indexes, jobs, logs, audit and backups.

Legal hold must take precedence over scheduled deletion where applicable, with authorized
creation/release and evidence. Erasure cannot silently corrupt required economic or audit
history: identify approved anonymization or retained statutory evidence and communicate
the exact limits. Backup expiry/restore behavior must not reintroduce supposedly erased
active data without the approved restoration controls.

Before any lifecycle action, preview affected objects/counts, evaluate dependencies/holds,
record authorization and prove the postcondition across relevant stores. Tenant suspension
and offboarding tools are [separate lifecycle controls](../../cmd/tenantctl/main.go), not
permission to infer retention periods or purge arbitrary evidence.

## Data dictionary capture and review

Read the [dictionary](generated/dictionary.md) for document types and its JSON companion
for fields, physical keys, links, protected fields and report columns. It combines an
explicitly scoped, versioned registry capture with owned business definitions. The
checked-in snapshot identifies its environment, tenant schema and date. It does not prove
a customer deployment's schema, business definitions or approved retention policy.

### Capture structural metadata

Use an authorized development connection with read access to the intended tenant's
metadata. Supply the connection through your approved secret/environment mechanism;
do not commit it. Choose a fresh output directory outside the repository.

```powershell
go run ./cmd/gendocs -capture-data-registry -db $env:ERP_METADATA_DATABASE_URL -tenant tenant_default -environment development -stamp 2026-09-09 -out $env:ERP_DOCS_STAGING
```

The command opens one read-only, repeatable-read transaction with a 30-second timeout.
It reads doctype/field configuration and information-schema columns/key relationships.
It never initializes the database, seeds roles, reads business rows or exports column
defaults. Only Link/Table targets are retained from field options. Failure publishes
no snapshot. Permission snapshots use a separate explicit command and are assurance
records, not dictionary inputs.

Review the staged snapshot for tenant customizations before copying its single JSON
file to `docs/data/registry-snapshot.json`. Preserve the previous revision in version
control. Review [business definitions](business-definitions.json) with the process
stewards; add doctype-specific meanings where the domain definition is insufficient.
Retention remains subject to the approved customer/jurisdiction schedule and legal holds.

### Generate and validate offline

Run `pwsh docs/update-docs.ps1 -Group Content`, then the same command with `-Check`.
Normal generation reads the recorded snapshot, business overlay and source registries;
it never connects to PostgreSQL. Missing sources, duplicate fields and orphan definitions
fail before any projection is published. The dictionary includes the snapshot SHA-256.

For a schema change, compare the snapshot and projection diff, review keys and nullable
changes against migrations, check import mappings and protected-field classifications,
and attach the review to the release. A field absent from the sensitive registry is
unclassified; it is not approved for disclosure. A configured Link is an application
relationship and must not be represented as a database foreign key unless captured as one.
