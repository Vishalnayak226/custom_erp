---
title: Your first order, end to end
section: Getting Started
order: 4
summary: Follow one order from placement to invoice, so the screens in between stop being a mystery.
audience: cashier, store manager
last_verified: 2026-08-12
screens: [oms, pos, fulfillment, sales-invoices]
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

Point of sale is the shortest path:

1. Open **POS**.
2. Scan or search for the product.
3. Take payment.

The sale creates a Sales Order and its lines in one step.

For a channel order there is nothing to do by hand - the connector imports it
and creates the same Sales Order the POS would have.

## 2. Watch it allocate

Open **OMS » Orders** and find your order. The allocation state tells you
whether stock has been committed to it:

| State | Meaning |
|---|---|
| Pending | No stock has been reserved yet |
| Reserved | Stock is held for this order at a specific location |
| Allocated | The order is ready to be picked |

If it stays Pending, the **Allocation pending** tile on the same screen lists
every order in that state and usually explains why - most often there is no
location with enough available-to-sell stock.

## 3. Pick, pack and dispatch

**Fulfillment** shows the work queue. An order moves through picking, packing
and dispatch; each step records who did it and when.

> [!TIP]
> Use **Priority** on an order in the OMS console to push it to the front of the
> pick queue. The priority flag is honoured by the queue itself, not just shown
> as a label.

## 4. Invoice

Once dispatched, the order can be invoiced from **Sales Invoices**. The invoice
carries the tax treatment resolved from the product's own HSN code and the
places of supply - you do not enter tax rates by hand anywhere.

## What to look at when something stalls

- **OMS » Exceptions** lists every order stuck for a reason the system knows.
- **SLA breaches** lists tasks older than their threshold.
- The order detail screen shows lines, reservations, tasks, shipments,
  invoices, returns, refunds, notifications and the audit trail in one place -
  start there before hunting through modules.
