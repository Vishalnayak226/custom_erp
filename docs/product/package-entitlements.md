---
doc_id: DOC-PROD-003
title: Package entitlements and support scope
type: normative
status: approved
owner: product-owner
approvers: [product-owner, business-owner]
audience: [business-owners, product, support, engineering]
applies_to: every client package and add-on
authority: canonical
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# Package entitlements and support scope

What a client gets with each package, what it needs, and what we do not support. It is the
product side of the contract (build checklist BLD-023, decision 135.10, approved by the owner
under ask 193). It matches the package list in `engines/modules.go` and the bundle rule in
`engines/saas_package_transition.go`.

## The core and the packages

Every client runs on the always-on core: master data, inventory, sales and POS, and finance.
The core cannot be switched off. Reports come with any package.

| Package | Sold | Adds to the core | Needs |
|---|---|---|---|
| Full ERP | on its own | everything below, plus labels | nothing else |
| PIM (product information) | on its own | PIM, labels | nothing else |
| WMS (warehouse) | on its own | WMS, labels | nothing else |
| OMS (orders) | on its own | OMS | nothing else |
| HR and People | add-on only | HR | Full ERP, PIM, WMS or OMS |
| Procurement and Vendors | add-on only | Procurement and RFQ | Full ERP, PIM, WMS or OMS |
| Manufacturing | add-on only | Manufacturing | Full ERP, PIM, WMS or OMS |
| CRM and Loyalty | add-on only | CRM and Loyalty | Full ERP, PIM, WMS or OMS |
| Fixed Assets | add-on only | Fixed Assets | Full ERP, PIM, WMS or OMS |
| Expenses | add-on only | Expense management | Full ERP, PIM, WMS or OMS |

## Not supported

- An add-on with no Full ERP, PIM, WMS or OMS. The system refuses it (code SAAS-0191) and says why.
- An empty selection, or a package we do not sell.
- Switching off a core module.
- Any other mix not in the table. We do not add packages or combinations without an owner decision.

## Downgrade, disable and re-enable

A package change is one all-or-nothing step, recorded with who made it. Before it runs, the
client sees a preview: modules gained, modules lost and the open work in them.

- Data is kept. Nothing is deleted, so switching a module back on restores everything as it was.
- Work is frozen. A switched-off module refuses new work and its background jobs skip the client.
  Drafts and pending approvals stay as they are until the module is back.
- No access carries over. A user's access to a module ends when the module does.

## What we support

We support the packages above, used as sold, on the hosted service. Supported means we
investigate and fix faults in a sold package, and restore the service. Targets are the ones in
the [availability model](../operations/service-operations.md#availability-model-stage-49131):
sign-in p95 under 1 s, sales and saves p95 under 2 s, restore point at most 24 hours back and
restore time at most 4 hours. No other response times are promised in this document.

Not supported: unsupported combinations above, changes made to the database outside the
product, and extensions we did not review.

## Legal terms

The customer terms, the data processing agreement and the support terms are written by
counsel, not here. This page describes the product; it is not a contract.
