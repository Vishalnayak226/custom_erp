---
title: Your first order, end to end
section: Getting Started
order: 4
summary: Follow one order from placement to invoice, so the screens in between stop being a mystery.
audience: cashier, store manager
last_verified: 2026-11-14
screens: [oms, pos, sales-invoices]
doc_id: DOC-B7AB4C1114
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-12
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: platform
task: Your first order, end to end
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Your first order, end to end

This walks one order through every screen it touches. Do it once on a test
product and the rest of the application stops looking like a list of unrelated
menus.

## Before you start

You need one product with stock. Check **Inventory** and confirm the product
shows a positive available-to-sell figure at your location. If it shows zero,
receive some stock first - an order for stock you do not have will be accepted
but will not allocate.

## 1. Place the order

1. Open **OMS » Orders** and click **New Order**.
2. Fill in the customer's name, phone and address.
3. Click **+ Add item**, pick the product and set the quantity.
4. Click **Create Order**.

A channel order needs nothing by hand: the connector brings it in as the same
kind of order. A sale at **POS** ends at its bill and does not come through
these steps.

## 2. Watch it allocate

Open **OMS » Orders** and find your order. Its status tells you whether stock
has been committed to it:

| Status | Meaning |
|---|---|
| Reserved | Stock is held for this order at one location; it is ready to pick |
| On Hold | Something stops it; the order shows why, such as *Waiting for stock* |

An order *Waiting for stock* is also on the **Allocation pending** tile on the
same screen. Most often no location has enough available-to-sell stock: receive
stock and the order can be reserved.

## 3. Pick, pack and dispatch

A channel order first waits on **OMS → Pending Orders** until someone accepts
it (an order keyed in by staff skips this). Then each step has its own page:
**Pick** (scan the units), **Pack** (scan them into the box, then **Pack, bill
and label** - the OMS bill and the courier AWB in one go) and **Ship**
(manifest, gate pass, hand over). Each step records who did it and when.

> [!TIP]
> Click **Expedite** on the order's page to push it to the front of the pick
> queue. The queue itself puts it first, not just a label.

## 4. Invoice

The customer's bill is the **OMS bill** made at Pack; its number is the GST
invoice number. Handing the manifest over makes the **ERP bill** - a posted
Sales Invoice with the same number - so you do not raise one by hand. Both
carry the tax treatment resolved from the product's own HSN code and the
places of supply - you do not enter tax rates by hand anywhere.

## What to look at when something stalls

- **OMS » Exceptions** lists every order stuck for a reason the system knows.
- **SLA breaches** lists tasks older than their threshold.
- The order detail screen shows lines, reservations, tasks, shipments,
  invoices, returns, refunds, notifications and the audit trail in one place -
  start there before hunting through modules.
