---
doc_id: DOC-REQ-BRD
title: Business requirements
type: normative
status: draft
owner: product-owner
approvers: [product-owner, finance-process-owner, operations-owner]
audience: [business-owners, process-owners, engineering, QA]
applies_to: proposed reference configurations
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Business requirements

This draft separates business needs from implementation status. The [legacy BRD](BRD.md)
remains available for provenance and unique-content mapping. Product/process approval is
pending; these requirements do not establish current support.

## BR-001

An owner must reconcile sales, returns, stock and money without reconstructing transactions
across disconnected systems. Measure unexplained exceptions and reconciliation time against
an approved baseline; finance and process owners accept the outcome.

## BR-002

Cashiers and floor operators must complete normal work, know whether a transaction saved,
and recover safely without developer interpretation. Measure observed task success, time,
error rate and recovery by role/device. Real user walkthrough evidence is required.

## BR-003

Each tenant, entity, location and stock owner must retain control of its data and decisions.
Sensitive access and economic overrides must be attributable to an authorized identity and
reviewable by the relevant owner. Verify negative access and reconciliation outcomes.

## BR-004

A business must be able to provision, operate, back up, restore and exit the system under
a documented responsibility model, retaining evidence required by its approved obligations.
Operations and implementation owners accept readiness and recovery drills.

## BR-005

The product must be economical to operate and evolve: bounded server/browser/database cost,
additive migrations and reusable controls. Measure cost/capacity at declared tenant/load
profiles. An empty development database cannot establish broad scale support.

## Stakeholders and operating model

Product leadership owns reference customers, commercial decisions and support scope. Process
owners own workflow controls; finance owns reconciliation; data stewards own masters;
security/privacy/legal own qualified controls; operations owns recovery; QA owns evidence;
users validate task completion. The vendor owns product behavior and release documentation.
Customers own approved configuration, identities, local processes and legal use.

## Scope, assumptions and approval

The [product vision](../product/vision.md) defines reference candidates and non-targets.
Go/PostgreSQL/native browser UI is a standing constraint. Business targets, legal applicability,
acceptance signatories, RTO/RPO and commercial terms require named owner decisions; the draft
does not invent them. Shared-tree edits and missing accepted release evidence are current risks.

Draft prepared 2026-09-07 from the existing backlog, audit and source structure. No approval
has been recorded. Approved changes retain stable requirement IDs and link approval evidence.
