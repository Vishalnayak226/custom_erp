---
title: Finance & Tax
section: Module Handbooks
order: 70
summary: Every posting is balanced double-entry against a period that can be locked shut, with multi-currency revaluation and 25 registered reports behind it - though several of the deepest entry points are API-only today.
audience: accountant, finance manager, admin
last_verified: 2026-09-03
screens: [finance, doctype-table, reports, configuration]
---

# Finance & Tax

Everything in this handbook sits on one mechanism: `PostDoubleEntry`, the
single function every posted document in this system goes through to reach
the general ledger. It takes a balanced set of debits and credits keyed by
account code, refuses to post into a closed accounting period, and is
idempotent by construction — a repeat call carrying the exact same posting
key (shaped `<DocType>:<DocID>:<PURPOSE>`) replays as a silent no-op instead
of posting twice, which is what makes a dropped network response safe to
retry. Everything else in this module — invoices, payments, revaluation,
manual journal entries — is a caller of that one function.

## The Finance screen

Reached from the sidebar, three tabs: **Trial Balance**, **Chart of
Accounts**, **Accounting Periods**. This is deliberately a small screen —
most of what a controller actually reads regularly lives in **Reports →
Finance** instead (25 registered reports as of this writing; see
[Reports](#reports) below), and several real, working finance actions have
**no screen at all yet** — see the warning further down before assuming
something doesn't exist just because it isn't here.

## Chart of Accounts

Every GL account has a **code**, a **name**, and a fixed **type** (Asset,
Liability, Equity, Revenue, Expense) that decides its normal balance side.
New accounts a feature needs (e.g. a new statutory-liability account) are
seeded by that feature's own migration — there's no "never touch this"
account, but adding one outside a migration means understanding which side
of the ledger it normally sits on before anything posts to it.

## Posting dimensions

A posting can optionally be tagged with up to four whole-posting dimensions
— **Cost Center**, **Department**, **Entity**, and **Project** — each
applying to every line one `PostDoubleEntry` call inserts, not per line
within it (the debit/credit maps already aggregate by account code before
they reach this function, so a per-line dimension isn't representable
without restructuring that). These dimensions are what the dimensioned P&L
reports (Cost Center P&L, Department P&L, Project P&L) and the entity/
consolidated trial balance reports filter and group by.

## Accounting periods and closing

**Create** a period (Finance → Accounting Periods) with a name and a
date range — a range overlapping any existing period (Open or Closed) is
refused outright, since an ambiguous "which period is today in" question is
worse than refusing the second period.

**Close** is one-way. There is deliberately no reopen action — correcting
something in a closed period means a fresh, dated reversal, never mutating
history, the same correction model this whole feature exists to enforce.
Once a period is Closed, any posting dated inside it is refused
(`FIN-0260`), checked from the database's own clock, not the app server's.

**Before closing**, the checklist panel runs four read-only checks and
shows which ones pass — it does not itself block the close, it's advisory:

| Check | What it means if it fails |
|---|---|
| Trial balance is balanced | Something's debits and credits don't match as of the period's end date. |
| No documents awaiting approval | At least one document is still sitting Pending Approval. |
| No vendor invoices in mismatch hold | A 3-way-match discrepancy (GRN vs. PO vs. invoice) hasn't been resolved. |
| No unmatched bank statement lines dated in this period | A reconciliation is incomplete for this period. |

**A backdated posting into an already-closed period** is possible, but only
through an explicit, named exception: a `BackdatedPostingRequest` naming the
exact document/date it authorizes, itself an approval-gated document. There
is no blanket override — each one is scoped to one specific posting.

## Trial Balance

The Trial Balance tab is **as-of-a-date**, not "since the beginning of
time" — pick a date and it shows every account's balance as it stood then,
optionally filtered by Cost Center/Department/Entity. A balanced ledger
shows total debits equal to total credits; if they don't, that's exactly
what the pre-close checklist above is designed to catch before you try to
close the period it happened in.

## Accounts Receivable: Sales Invoices

**Sidebar → Sales Invoices**: list, then **Post** and **Settle** as two
separate, explicit actions on an invoice — posting books the receivable
(Dr Accounts Receivable / Cr Revenue); settling records that it was
actually collected. A **deferred-revenue** invoice posts to a deferred
liability account instead of straight to revenue, and a background worker
recognizes it into revenue on schedule — see the **Deferred Revenue
Roll-Forward** report to track one in progress.

## Accounts Payable: Vendor Invoices and payment runs

**Sidebar → Vendor Invoices**: pay a single invoice directly, with or
without TDS deduction (`/vendor-invoice/pay` vs. `/vendor-invoice/pay-with-tds`
— reached from the invoice's own action buttons). For paying many invoices
in one run:

1. **Create a Payment Proposal** — a batch of invoices selected for payment.
2. **Execute** it — every invoice in the batch is paid through the exact
   same vendor-invoice payment path a single manual payment uses, not a
   parallel mechanism.

> [!WARNING]
> **Generating the actual bank payment file, and recording the UTR once a
> payment clears, are both real, working endpoints with no button anywhere
> in the app.** `GET /api/v1/finance/payment-proposal/{id}/payment-file` and
> `POST .../record-utr` (plus `GET .../utrs` to list them) have zero matches
> anywhere in `public/app.js`, confirmed by grep — proposal creation and
> execution work from the screen, but producing the file your bank actually
> needs, and marking a proposal's payments as cleared with their UTR
> reference, both need a direct API call today.

## Multi-currency and FX

A document in a foreign currency posts in both that currency and the base
currency, booked at the rate in effect when it posted. Two further pieces
close the loop:

- **Realised gain/loss on settlement** — when a foreign-currency invoice is
  actually paid/settled at a different rate than it was booked at, the
  difference posts automatically to a Realised FX Gain or Loss account.
- **Period-end revaluation** (`GET`/`POST /api/v1/finance/fx-revaluation`,
  the GET previews and the POST commits — deliberately two different verbs
  on the same URL, not a `dry_run` flag on one POST, so a retry or
  prefetch can never accidentally book a second adjustment) restates every
  still-open foreign-currency balance at the period-end closing rate and
  books the movement to an Unrealised FX Gain/Loss account.
  **`Open`/committed *balances* are revalued — commitments like a Purchase
  Order, Sales Order or RFQ are not**, since revaluing a commitment would
  post a gain against a control account holding no matching real balance.
  Revaluing the same document twice for the same as-of date is refused with
  a named reason, not silently skipped, since "nothing to do" and "already
  done" mean different things to whoever is closing the books.

> [!WARNING]
> **FX revaluation has no screen either.** The preview/commit split above
> is a real, deliberate safety design, but there is nowhere in the app to
> reach it — confirmed by grep, zero matches for `fx-revaluation` in
> `public/app.js`. A period-end revaluation needs a direct API call today.

Three registered reports read multi-currency data without needing their own
screen: **FX Open Item Exposure**, **FX Gain/Loss Register**, and **Trial
Balance (Presentation Currency)** — all reachable from Reports → Finance
like any other report.

## Error codes reference

| Code | Meaning |
|---|---|
| `GLOBAL-0012` | An accounting period's Start Date is later than its End Date. |
| `FIN-0260` | Posting refused — the transaction date falls inside a Closed accounting period with no approved backdated-posting exception. |
| `FIN-0261` | (Warning) A bank statement line doesn't match anything in the ledger. |

## Reports

25 reports live under **Reports → Finance** as of this writing — Balance
Sheet, Cash Book/Bank Book, Cash Flow Statement/Forecast, Budget vs Actual,
Consolidated/Entity/Presentation-Currency Trial Balance, Cost Center/
Department/Project P&L, Deferred Revenue Roll-Forward, the two FX reports
above, and more. Every one of them exports, schedules and drills down (where
marked) exactly like any other registered report — see the full
[Report Catalog](report-catalog.md) for the complete list with parameters
and columns rather than a partial one duplicated here.

## Troubleshooting

**A posting is refused: "transaction date falls within closed accounting
period."** This is `FIN-0260` — either post with today's (open-period) date
instead, or have an admin create an approved `BackdatedPostingRequest`
naming this exact document and date first.

**The pre-close checklist shows the trial balance as unbalanced.** Something
posted an unequal debit/credit somewhere — this should not happen through
any normal document action (`PostDoubleEntry` refuses an unbalanced call
outright), so treat it as worth investigating rather than working around.

**A vendor invoice won't pay — it's stuck in mismatch hold.** A 3-way-match
discrepancy between the GRN, PO and invoice needs resolving first; this is
also one of the four pre-close checklist items, so it will block a clean
close if left unresolved.

**Need to generate a bank payment file or record a UTR, but there's no
button.** See the warning under Accounts Payable above — both are real
endpoints reachable only via a direct API call today.

## What is not here yet

**Several real, tested, GL-posting actions have no UI screen at all**,
confirmed by grep against `public/app.js` (zero matches for each):

- **Manual Journal Vouchers** — `POST /api/v1/finance/journal-voucher` (plus
  reverse/retry-post/recurring-template variants) is a complete, working
  manual-GL-entry feature with its own reversal and recurring-template
  support. There is no "new journal entry" button anywhere in the app.
- **Intercompany Transactions** — cross-entity mirrored postings
  (`POST /api/v1/finance/intercompany-transaction`), used by the entity/
  consolidated trial balance reports, are API-only.
- **Landed Cost Vouchers** — allocating freight/duty/other charges onto
  landed cost (`POST /api/v1/finance/landed-cost-voucher`) is API-only.
- **FX revaluation** and **bank-payment-file generation/UTR recording** —
  see the warnings above.

In every one of these cases the underlying feature is real, tested, and
already how the reports listed above get their data — this is a coverage
gap in the frontend, not a stub in the backend.