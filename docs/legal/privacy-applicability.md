---
doc_id: LEGAL-PRIV-001
title: Privacy and India applicability review
type: template
status: draft
owner: legal-owner
approvers: [legal-owner, privacy-owner, business-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Privacy and India applicability review

This is the review worksheet for qualified privacy/legal and business owners. No customer applicability, compliance conclusion or legal retention duration has been approved. [MeitY’s official DPDP Rules 2025 page](https://www.meity.gov.in/documents/act-and-policies/digital-personal-data-protection-rules-2025-gDOxUjMtQWa) publishes the rules and enforcement timeline; use the actual notified text and commencement notifications when completing this worksheet. Do not use an earlier consultation draft as current law.

## Determine the deployment scope

Record legal entities, countries, customer/vendor roles, processing locations, data principals, business purposes, personal-data categories, processors/subprocessors, integration recipients and cross-border flows. Include customer/employee/contact records, authentication/recovery, orders, receipts, exports, telemetry, support access, logs, files and backups.

For each relevant obligation record the exact source/provision, source version/date, commencement date, applicability reasoning, accountable qualified reviewer, implemented control, test/evidence, gap/mitigation and review expiry. Assess India's DPDP framework and any applicable sectoral, tax, accounting, labor, payment or contractual obligations separately; product geography alone is not an applicability determination.

## Control review table

| Topic | Decision and evidence required | Accountable reviewer |
|---|---|---|
| Notice, purpose and permitted processing | Approved purpose/data map, notice and processing basis for the deployment | Privacy/legal + business |
| Access and disclosure | Roles/scopes, protected fields, export recipients and support access | Security + data |
| Requests and correction | Identity verification, request handling, reconciliation and response evidence | Privacy + operations |
| Retention, hold and erasure | Category-specific schedule, dependency/backups/restore behavior, approved exceptions | Legal + data + operations |
| Incident and notification | Applicable triggers/time requirements, responsibilities and approved channels | Security + qualified legal |
| Vendors and transfers | Contracts, processor obligations, recipients, locations and approved safeguards | Procurement + legal |

Attach the signed applicability decision in the approved evidence store and its non-sensitive reference in the [legal register](document-register.md). No checked box here authorizes a purge or establishes compliance. [Data lifecycle](../data/data-lifecycle.md), [control requirements](../requirements/control-requirements.md) and [release acceptance](../governance/release-acceptance.md) carry the implementation/evidence links.
