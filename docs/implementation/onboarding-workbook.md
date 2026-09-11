---
doc_id: DOC-IMPL-001
title: Tenant implementation and cutover workbook
type: template
status: draft
owner: implementation-owner
approvers: [tenant-owner, data-owner, operations-owner, qa-owner]
audience: [implementation, tenant-admins, business-owners]
applies_to: one customer and approved configuration
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Tenant implementation and cutover workbook

Blank reusable template. Copy into a dated customer evidence record for an actual project;
record approvals and results there. A normal tenant administrator is not asked to install
Go/PostgreSQL or obtain platform credentials to operate an already provisioned service.

| Work package | Record and accountable acceptance |
|---|---|
| Discovery/scope | Business outcomes, legal entity/country, sites, owners, volumes, workflows and exclusions; tenant/product owner |
| Supported configuration | Exact capability/release/device/integration scope and unresolved limits; product/implementation |
| Provisioning/access | Tenant lifecycle, named admins, identity/MFA, capability templates and scope migration preview; platform/tenant owner |
| Configuration | Calendar/currency/tax/UOM/numbering/workflow decisions with effective version; process/data/finance owners |
| Data migration | Sources, mappings/dependencies, package IDs, rejects and stock/money control totals; data/process owners |
| Connector/device readiness | Sandbox/live boundary, provider versions, credentials mechanism, scanners/printers and failure/recovery trials; integration/operations |
| Business UAT/training | Real role journeys, exceptions, accessibility, reconciliation and training/help evidence; business users/QA |
| Cutover | Freeze window, last delta, signed totals, backup/restore point, readiness and explicit go/no-go; tenant/operations |
| Rollback | Trigger, decision owner, data/application boundaries, reconciliation and customer communication; operations/finance |
| Hypercare/support handoff | Coverage, incident route, known issues, evidence location, review date and acceptance; support/tenant owner |

Use [data migration controls](../data/import-and-migration.md),
[customer operating procedures](operating-procedure-template.md),
[role/access policy](../security/access-control-policy.md), [user role journeys](../kb/role-journeys/role-journeys-overview.md)
and [release acceptance](../governance/release-acceptance.md). Credentials, private contacts
and restricted customer data belong in approved systems, not this committed blank template.
