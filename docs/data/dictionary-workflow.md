---
doc_id: DOC-DATA-004
title: Data dictionary capture and review
type: procedure
status: draft
owner: data-owner
approvers: [engineering-owner, privacy-owner]
audience: [data stewards, implementers, developers]
applies_to: development metadata capture and source registry projections
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Data dictionary capture and review

Read the [dictionary](generated/dictionary.md) for document types and its JSON companion
for fields, physical keys, links, protected fields and report columns. The checked-in
snapshot identifies its environment, tenant schema and date. It does not prove a
customer deployment's schema or business definitions.

## Capture structural metadata

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

## Generate and validate offline

Run `pwsh docs/update-docs.ps1 -Group Content`, then the same command with `-Check`.
Normal generation reads the recorded snapshot, business overlay and source registries;
it never connects to PostgreSQL. Missing sources, duplicate fields and orphan definitions
fail before any projection is published. The dictionary includes the snapshot SHA-256.

For a schema change, compare the snapshot and projection diff, review keys and nullable
changes against migrations, check import mappings and protected-field classifications,
and attach the review to the release. A field absent from the sensitive registry is
unclassified; it is not approved for disclosure. A configured Link is an application
relationship and must not be represented as a database foreign key unless captured as one.
