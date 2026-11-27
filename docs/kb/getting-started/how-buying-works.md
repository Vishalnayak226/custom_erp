---
title: How buying works
section: Getting Started
order: 6
summary: One purchase, step by step - the order, its approval, the goods receipt, stock, the vendor bill and the payment, and what each step posts to the books.
audience: everyone
last_verified: 2026-11-27
doc_id: DOC-EFBBCED76B
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
module: procurement
task: Understand how a purchase moves from order to payment
prerequisites: None - this page explains; the Procurement handbook has the steps on each screen
---

# How buying works

One purchase, from the order to the payment. Click **Next** or press the
arrow keys. The lit box is where the step happens.

```steps
diagram: PO=Purchase order, GRN=Goods receipt, STOCK=Stock, BILL=Vendor bill, PAY=Payment
links: PO>GRN, GRN>STOCK, GRN>BILL, BILL>PAY

## Raise the purchase order
The buyer picks the vendor and the items on Procurement › Purchase Order and saves a draft.
A purchase order moves no stock and posts nothing to the books.
Tip: Already know the vendor? Skip the requisition and the RFQ and start here.
show: PO

## Get it approved
Submitting sends the order for approval by its amount: by default a Store Manager up to ₹49,999 and a Super Admin from ₹50,000.
Nothing can be received against an order still waiting for approval.
Tip: An amended order goes back for approval before more goods can be received.
show: PO

## Receive the goods
When the cartons arrive, Procurement › Goods Receipt loads the vendor's open orders; key what arrived and the vendor's invoice number, date and total.
Saving the receipt still posts nothing.
Tip: Scanning an item already on the receipt counts one more.
show: PO>GRN, GRN

## Verify and price
Verify records what is really in hand, what was rejected or damaged, and the agreed price.
Each line is then checked against its last purchase price, so a new item or a changed price is seen before it posts.
Tip: When everything matches, Post all runs verify, price check and posting in one go.
show: GRN

## Post: stock goes in
Posting puts the goods into stock at the receipt price, with the MRP on the batch.
The books take Inventory (1200) up, and GRN Suspense (2100), what you owe the vendor, up by the same amount.
Tip: Only a posted goods receipt changes stock, never the purchase order.
show: GRN>STOCK, STOCK

## The vendor bill
Posting the last part makes the vendor bill from the invoice keyed at Receive, with a debit or credit note for any gap between their bill and your count.
A bill recorded by hand is matched against its order and receipt, within 2% by default.
Tip: A bill further off than that waits on hold until someone overrides it with a reason.
show: GRN>BILL, BILL

## Pay
Pay part or all of the bill by bank, UPI, cheque or cash, with TDS withheld where it applies.
Paying clears GRN Suspense (2100) against Cash/Bank (1100); the TDS goes to TDS Payable (2300).
Tip: Not paying today? Leave it in Payables; the Payables Ageing report shows what falls due.
show: BILL>PAY, PAY
```

## Where to read more

- Each screen, step by step: [Procurement](../module-handbooks/procurement.md).
- The accounts and the ledger: [Finance and tax](../module-handbooks/finance-tax.md).
- The other half of the business: [How a sale works](how-a-sale-works.md).
