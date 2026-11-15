---
doc_id: DOC-PROD-001
title: Product vision and reference customers
type: normative
status: approved
owner: product-owner
approvers: [product-owner, business-owner]
audience: [business-owners, product, engineering]
applies_to: the two reference configurations
authority: canonical
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# Product vision and reference customers

Help retail and warehouse teams transact against one consistent account of stock, money,
ownership and approvals. The intended economic benefit is less reconciliation and fewer
operational errors without a mandatory fleet of supporting services.

## Reference configurations

The two reference configurations are India retail (single/multiple stores) and India
warehouse/wholesale distribution with one stock owner per warehouse (the Stage 47.5 decision).
The [configuration and capability catalog](../generated/capability-catalog.md) states their
current evaluation limits. The owner has approved these two as the reference customers.
Commercial model, pricing and numeric success thresholds are not set; they need the business
owners' baselines. Mixed-owner warehouses are built (Stage 59: the owner travels with orders,
allocation, availability and every bin movement), but they are not part of either reference
configuration's evaluation. The old `mixed_unsupported` setting value still reads as mixed-owner mode.

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

## Approval

Approved by the product owner on 2026-11-15 (ask 193) and checked against the product as it stands that day. Approval fixes the text. It is not a support claim: capability maturity stays in the [capability register](capability-register.json), where nothing is Production or Certified.
