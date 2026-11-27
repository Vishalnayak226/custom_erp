---
title: How a sale works
section: Getting Started
order: 7
summary: One sale at the till, step by step - the till session, the bill, the payment, stock going down, what the books record and the day close.
audience: everyone
last_verified: 2026-11-27
doc_id: DOC-FA14D8D9AE
type: reference
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-25
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: pos-operations
task: Understand what one sale at the till does to stock and the books
prerequisites: None - this page explains; the Point of Sale handbook has the steps on each screen
---

# How a sale works

One sale at the till, from opening the till to closing the day. Click
**Next** or press the arrow keys. The lit box is where the step happens.

```steps
diagram: TILL=Till session, BILL=The bill, PAID=Payment, STOCK=Stock, BOOKS=The books, CLOSE=Day close
links: TILL>BILL, BILL>PAID, PAID>STOCK, PAID>BOOKS, BOOKS>CLOSE

## Open the till
The cashier opens a session on POS › POS / Billing and enters the cash in the drawer.
Every bill rung up belongs to that session; with none open, billing is refused.
Tip: One till has one cashier at a time.
show: TILL

## Ring up the bill
Scan each item into the cart and look up the customer for loyalty points; offers apply by themselves.
A cart changes neither stock nor the books.
Tip: A code, a barcode or the internal id all find the same item.
show: TILL>BILL, BILL

## Take payment
Cash, card or UPI, and loyalty points can pay part of it.
Complete Sale is one transaction: the next two steps happen together, or not at all.
Tip: With the network down, a sale can queue on the till; it goes through once the network is back, and never twice.
show: BILL>PAID, PAID

## Stock goes down
The items leave the store's stock the moment the sale completes.
Their cost moves from Inventory (1200) to Cost of Goods Sold (5100).
Tip: An item never received and with no standard cost posts no cost line; finance sees it as a costing gap.
show: PAID>STOCK, STOCK

## The books
The money lands in Cash (1100), Card (1101) or UPI (1102), and the sale in Sales Revenue (4100).
The GST part then moves out of revenue to the GST payable accounts, so revenue keeps only the taxable amount.
Tip: Each journal carries the store's cost centre, so the store's P&L needs no extra work.
show: PAID>BOOKS, BOOKS

## Close the day
Closing the session asks for the cash counted and shows the gap from what was expected.
The Sales Register and the Cash Book in Reports are what the manager checks against the till.
Tip: Keep the card and UPI totals to hand; the close may ask for them too.
show: BOOKS>CLOSE, CLOSE
```

## Where to read more

- Each screen, step by step: [Point of Sale](../module-handbooks/pos-operations.md).
- The accounts and the ledger: [Finance and tax](../module-handbooks/finance-tax.md).
- The other half of the business: [How buying works](how-buying-works.md).
