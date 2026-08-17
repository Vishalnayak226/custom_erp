---
title: A day as a store manager
section: Role Journeys
order: 3
summary: Morning queues, approvals, stock that needs ordering, and the four checks worth making before you close.
audience: store manager
last_verified: 2026-08-17
screens: [reports, oms, approvals, inventory, purchase-orders, grn, pos, transfers]
---

# A day as a store manager

You sit between the till and everything behind it. The job splits into three:
clear what is blocked, keep stock ahead of demand, and decide the things only a
manager can decide.

## Morning: clear what is blocked

Start on **Reports** - it is where you land at sign-in, and its first tab is a
dashboard of live figures. Then work the queues in this order, because each one
feeds the next.

**1. Approvals.** **Financial Accounting » Approvals** lists everything waiting
on you: purchase orders, large manual discounts from the till, anything else
your business has put behind a rule. An approval you leave sitting is a purchase
that is not happening and a sale that has not been charged.

You cannot approve your own document. If Approve is missing on something you
raised, that is the rule working.

**2. Orders on hold.** **Sales & Marketplace » Order Management**, filter
**Status = On Hold**. Every dropdown shows a count, so you can see the size of
the backlog before opening it. Open an order, read the hold reason, fix the
cause, click **Release hold**. Resolved several with the same cause? Tick them
and release them together.

**3. The four tiles** at the top of Order Management are live counts and each
opens the report behind it: integration exceptions, SLA breached, allocation
pending, reconciliation variance. *Allocation pending* is the one that usually
means "you are out of stock and do not know it yet".

## Keeping stock ahead of demand

**Check what you have.** **Stock » Inventory**, search by name or code. The
figure you care about is what is *free to sell* - it already accounts for stock
promised to other orders, so it is not a raw warehouse count.

**Order more.** **Procurement » Purchase Order**: pick the vendor, target
warehouse and location, add lines with quantity and rate, **Create Draft**, then
**Submit for Approval**. You do not type a PO number; it is issued on save.

Prices, HSN and tax on each line are resolved by the server from the Item
master, so you cannot accidentally order at a tax classification that disagrees
with the product. A line that errors usually means the item is missing its HSN
code.

**Receive it.** **Procurement » Goods Receipt** » **Load Items from PO**,
confirm what actually arrived, **Post Receipt**. Then check **Inventory** moved.
Receiving is the step that turns a purchase into sellable stock, and a receipt
that was never posted is the most common cause of "we definitely have that in
the back".

**Move it between shops.** **Stock » Stock Transfer**.

## What only you can decide

- **Manual discounts** above the threshold, from the Approvals screen. Nothing
  is charged to the customer until you decide.
- **Cash variances** at session close. The cashier reports; you explain.
- **Holds and priorities.** **Expedite** on an order pushes it to the top of
  both this queue and the warehouse's picking worklist - it is honoured by the
  queue itself, not just shown as a label.
- **Cancellations**, which always ask for a reason code.

## Taking an order by hand

Phone orders, wholesale walk-ins and replacements go in through **Order
Management » New manual order**. Shipping address is required; customer name and
phone are optional but are what make the order findable later.

Put your own order number in **Reference**. The same reference twice returns the
same order rather than creating a duplicate, so a double-click cannot cost you
anything.

A manual order goes through exactly the same engine a marketplace order does -
stock reserved, allocation run, hold rules evaluated. That means it can be
refused, most often for *insufficient stock for reservation*. That is not a
fault; it is the system declining to promise stock you do not have.

## Finding one order fast

The search box at the top of Order Management searches, in one go: the ERP's
order id, the channel's order id, an AWB or tracking number, the customer's
phone, the customer's name, and any SKU on the order. Each result says which of
those matched. Type whatever the customer reads out over the phone.

Filters you use repeatedly can be saved with **Save this view**. Views are
private to you.

## Before you close

| Check | Where | Looking for |
|---|---|---|
| Sessions closed | **POS » POS / Billing** | No session left open overnight |
| Today's sales | **Reports » Sales Register** | The day's total matches the till |
| Nothing stuck | **Order Management**, filter On Hold | Ideally empty |
| Receipts posted | **Stock » Inventory** | Today's deliveries actually raised stock |

## Next

- [A day as a warehouse operator](journey-warehouse-operator.md) - what happens
  to the orders you release.
- [Open a shop and make your first sale](open-a-shop-and-make-your-first-sale.md) -
  the whole loop in one worked example.
