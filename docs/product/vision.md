---
doc_id: DOC-PROD-001
title: Product vision and reference customers
type: normative
status: draft
owner: product-owner
approvers: [product-owner, business-owner]
audience: [business-owners, product, engineering]
applies_to: proposed reference configurations
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Product vision and reference customers

Help retail and warehouse teams transact against one consistent account of stock, money,
ownership and approvals. The intended economic benefit is less reconciliation and fewer
operational errors without a mandatory fleet of supporting services.

## Reference candidates

The candidates are India retail (single/multiple stores) and India warehouse/wholesale
distribution with one stock owner per warehouse, as recorded by the 2026-09-09 Stage 47.5 decision. The
[configuration and capability catalog](../generated/capability-catalog.md) states their
current evaluation limits. Product leadership must approve reference customers, commercial
model, pricing, success thresholds and release scope. The current single-owner guard does not
provide mixed-owner allocation isolation. The opt-in `mixed_unsupported` mode is outside
supported use and requires a separately scoped future design and acceptance.

## Principles and outcomes

Preserve the Go modular monolith, PostgreSQL and native browser UI. Explain transaction state
and recovery in business language. Centralize server authorization and economic authority.
Keep documentation operational without a runtime service. Verify a defined set of journeys
before expanding supported combinations.

Measure setup time, task completion without developer help, unexplained reconciliation
exceptions, duplicate transactions, support incidents per tenant and operating cost per
tenant. Business owners must agree baselines, samples and targets before improvement claims.

## Scope and non-targets

Reference journeys cover onboarding, sale/close/return, purchase-to-pay, receipt-to-shipment,
finance close and governed master changes. Other industry packs, regulated workflows,
countries and certified hardware require separate configuration evidence. This vision grants
no automatic certification, statutory applicability decision or unrestricted multi-owner use.
Infrastructure expansion requires measured need and a reviewed architecture decision.
