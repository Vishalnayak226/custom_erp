---
title: A day in finance
section: Role Journeys
order: 6
summary: Payables in, receivables out, the bank reconciled, and a trial ledger that balances before you go home.
audience: finance
last_verified: 2026-11-24
screens: [finance, vendor-invoices, sales-invoices, payment-proposals, bank-reconciliation, finance-notes, approvals, reports]
doc_id: DOC-186524B8BA
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-22
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: cross-module
task: A day in finance
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# A day in finance

You are downstream of everyone. The good news is that most entries have already
been made for you: a sale posts its own ledger entries, a goods receipt posts
its own. Your job is the money either side of that - what is owed, what is paid,
and whether the books agree with the bank.

## The screens, in the order money moves

| Screen | What it is for |
|---|---|
| **Vendor Invoice** | What suppliers have billed you |
| **Payment Proposals** | Deciding what to pay, and when |
| **Sales Invoice** | What you have billed customers |
| **Debit Notes**, **Vendor Credit Notes**, **Customer Credit Notes** | Corrections in either direction |
| **Bank Reconciliation** | Making the statement and the ledger agree |
| **Trial Balance**, **Chart of Accounts**, **Accounting Periods**, **Journal Vouchers** | The ledger itself, and the trial balance |
| **E-Invoice (IRN)** | Registering invoices with the e-invoice portal |
| **Work to review** | A start page: each of these, plus who posted what and the general journal |
| **Approvals** | Anything routed to you for a decision |

All of them are under **Financial Accounting**.

## Payables

**Vendor Invoice** is where a supplier's bill is recorded and matched against
what you ordered and what you received. The three-way agreement between order,
receipt and invoice is the whole control: an invoice that does not match one of
the other two is the thing worth your attention, and the rest is data entry.

**Payment Proposals** turns approved payables into a decision about what to pay
now. Work the proposal rather than paying invoices one at a time - it is the
difference between a payment run and a series of small judgements nobody can
reconstruct later.

## Receivables

**Sales Invoice** carries the tax treatment resolved from the product's own HSN
code and the places of supply. You do not enter tax rates by hand anywhere, and
an invoice that looks wrong is almost always a product classified wrong rather
than an invoice built wrong - fix it on the Item.

Settle invoices as the money arrives. The three note entries handle the
corrections; use them rather than editing history.

## Foreign currency, if you use it

Skip this entirely if you only invoice in your own currency.

**On the invoice.** Set **Currency**. Leave **Exchange Rate** blank and the rate
is looked up using **the invoice's own date**, not today's - so back-dating an
invoice does not quietly apply this morning's rate. Type a rate yourself when a
contract fixes one; what you type wins.

The invoice keeps the amount agreed in the customer's currency; the accounts
record what that is worth in yours. Both numbers matter.

**On settlement.** **Settle** on a sales invoice, or **Pay** on a vendor
invoice, asks only for the amount and converts it at today's rate. So enter a
receipt on the day the money arrives. If your bank gave a different rate, the
difference shows in the **FX Gain/Loss Register** rather than vanishing; a bank
integration can send the actual date and rate with the receipt instead.

The gain or loss is recorded automatically. A $1,000 invoice raised at 83 and
collected at 85 brings in ₹85,000 against ₹83,000 booked; that ₹2,000 is a
genuine gain, not an error in the invoice. The reverse happens as often.

**Where you stand**, both under **Reports** in the Finance category:

- **Open FX Exposure** - every unpaid foreign-currency invoice at today's rate.
  Worth a look before a large receipt.
- **FX Gain/Loss Register** - every gain and loss recorded, and which document
  caused it.

## Reconciliation

**Bank Reconciliation** matches the statement to the ledger. Do it often enough
that an unmatched line is still explicable - a month of unmatched lines is an
archaeology project, a day of them is a phone call.

## The check that catches almost everything

**Trial Balance**, set **As Of Date** to today. Debits should equal credits and
the status should read *Balanced trial ledger*.

If it does not balance, something upstream failed to post, and the sooner you
find it the smaller the search. This is a thirty-second check and it is worth
making it a habit.

## Month end

| Step | Screen |
|---|---|
| Every receipt posted, every invoice raised | **Vendor Invoice** / **Sales Invoice** |
| Payment run completed | **Payment Proposals** |
| Bank fully reconciled | **Bank Reconciliation** |
| Corrections issued as notes, not edits | **Debit Notes**, **Vendor Credit Notes**, **Customer Credit Notes** |
| Trial ledger balances | **Trial Balance** |
| Open foreign balances restated | **Reports » Open FX Exposure** |

## Next

- [Report catalog](report-catalog.md) - every finance report and what it asks
  for.
- [A day as a store manager](journey-store-manager.md) - where most of your
  entries come from.
