---
title: Journey evidence
type: record
status: archived
owner: qa-owner
authority: historical
confidentiality: internal
captured_at: 2026-11-10T17:47:05Z
---

# Journey evidence: FAIL

Generated 2026-11-10T17:47:05Z against http://localhost:8730 (database `custom_erp_5c_c`, host windows/amd64, 8 CPU). Database figures from `pg_stat_database`.

| Journey | Result | Roles | Wall ms | Calls | API p50 / p95 ms | DB queries (or tx) | DB ms | CPU ms | RSS KB | Heap B | Index B | WAL B |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| J1 First sale | FAIL | jl-cash, jl-sm, jl-sms | 79651 | 10 | 230 / 575 | 368 | 30467 | 125 | 45988 | 24576 | 0 | 41938544 |

## J1 First sale: FAIL

Reason: reconciliation did not tie

| Step | Role | ms | Calls | Retries | Bytes | HTTP | Errors |
|---|---|---:|---:|---:|---:|---|---|
| transfer order to the shop | jl-sm | 893 | 2 | 0 | 289 | 200 200 |  |
| approve and dispatch the transfer | jl-sm | 482 | 2 | 0 | 286 | 200 200 |  |
| shop receives the transfer | jl-sms | 326 | 1 | 0 | 265 | 200 |  |
| open the till | jl-cash | 135 | 1 | 0 | 107 | 200 |  |
| quote and checkout | jl-cash | 590 | 2 | 0 | 1504 | 200 200 |  |
| close the till | jl-cash | 146 | 2 | 0 | 574 | 200 200 |  |

Reconciliation

| Kind | Check | Result | Detail |
|---|---|---|---|
| gl | ledger total debits = credits | FAIL | debits 8096081, credits 8095304 |
| money | every voucher balances (debits = credits per document) | FAIL | 1 unbalanced voucher(s) |
| stock | no negative on-hand | PASS | 0 row(s) below zero |
| stock | JL-I1 at JL-W1 moved by -20 (transferred to the shop) | PASS | expected -20, observed -20 |
| stock | JL-I1 at JL-S1 moved by +18 (received less the sale) | PASS | expected +18, observed +18 |
| subledger | engine: advance-outstanding-ageing | PASS |  |
| subledger | engine: cat-ap-aging | PASS | A receipt is carried at its taxable cost; the GST on its bill goes to 1500 GST Input Credit when paid, so an item here is the goods value still owed, not the bi |
| subledger | engine: cat-ar-aging | PASS |  |
| subledger | engine: cat-gst-document-tax-detail | PASS | Returns and credit notes are not in these reports (the ERP's credit notes carry no tax split); the ledger figure is sales postings only, so they do not distort  |
| subledger | engine: cat-gstr-1-b2b-b2c-summary | PASS | Returns and credit notes are not in these reports (the ERP's credit notes carry no tax split); the ledger figure is sales postings only, so they do not distort  |
| subledger | engine: cat-gstr-1-hsn-summary | PASS | Returns and credit notes are not in these reports (the ERP's credit notes carry no tax split); the ledger figure is sales postings only, so they do not distort  |
| subledger | engine: cat-hsn-tax-summary | PASS | Returns and credit notes are not in these reports (the ERP's credit notes carry no tax split); the ledger figure is sales postings only, so they do not distort  |
| subledger | engine: cat-sales-register | PASS | Ledger is the revenue and output GST posted for these same bills and invoices. A difference is a sale whose posting failed or an invoice not yet posted - check  |
| stock | engine: cat-stock-valuation-summary | FAIL | Ledger is the inventory control account. A difference is stock valued at standard cost (never received, so never posted) or a movement whose posting failed - ch |
| stock | engine: custody-with-party | PASS |  |
| subledger | engine: gst-return-summary | PASS | Detail is the tax lines View Details lists; summary is the Total Tax Liability column. |
| stock | engine: inventory-valuation | FAIL | A remaining difference is stock moved without a GL entry (e.g. cycle-count variances, which post no GL) or GL posted to 1200 without stock (a journal) - review  |
| subledger | engine: payables-ageing | PASS | A receipt is carried at its taxable cost; the GST on its bill goes to 1500 GST Input Credit when paid, so an item here is the goods value still owed, not the bi |
| subledger | engine: po-commitments | PASS | No GL account carries purchase-order commitments, so there is no ledger tie for this report. Goods received but not yet paid sit in 2100 GRN Suspense: -0.00 at  |
| subledger | engine: points-liability | PASS | Points are expensed when redeemed (Loyalty -> Account for loyalty points), so no GL account carries this liability and there is no ledger tie. Choose "liability |
| subledger | engine: print-evidence | PASS |  |
| subledger | engine: receivables-ageing | PASS |  |
| subledger | engine: sales-register | PASS | Ledger is the revenue and output GST posted for these same sales. A difference is bill-level discounts and offers (the register shows the line price before them |
| stock | engine: stock-pools | PASS |  |
| subledger | engine: store-credit-liability | PASS |  |
| audit | audit checkpoint sealed | PASS | rows 1575-1629 sealed |
| audit | audit evidence verifies (no tampered row, no bad checkpoint) | PASS | 398 signed row(s) verified, 1231 unsigned legacy, 0 unverifiable, 18 checkpoint(s) verified |
| audit | audit checkpoints present | PASS | 18 verified checkpoint(s) |
| audit | journey left audit rows | PASS | 20 audit row(s) added during the journey |


## Browser (accessibility and help usability): not_provided

Source: from_browser_suite. accessibility and help usability need a real browser; run the browser suite and pass its output with -browser-evidence

## What this run cannot show

- journeys are driven over HTTP as each role; screen behaviour comes only from the browser section
- wall time, CPU and memory are for this machine and this data size; compare runs on the same host
- CPU and RSS are the server process's own; database CPU is not included
- database figures come from pg_stat_database: they count transactions and tuples, not individual queries (query_count_available is false); use a cluster with pg_stat_statements for query counts
