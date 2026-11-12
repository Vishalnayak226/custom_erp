---
doc_id: SEC-ATOMICITY-001
title: Atomicity and failure injection
type: reference
status: draft
owner: security-owner
approvers: [security-owner, finance-process-owner]
audience: [engineering, security]
applies_to: commands that move stock or money
authority: source
confidentiality: internal
last_verified: 2026-11-10
review_by: 2027-02-10
supersedes: none
superseded_by: none
verification_scope: failure-injection tests run on a scratch database on 2026-11-10 (erp-5c)
---

# Atomicity and failure injection (Stage 49.5.2)

What is proven about the commands that move stock or money: that a failure at any write inside the command leaves nothing behind, that a retry lands exactly once, and that a replay or a race moves nothing further. Companion to `command-catalog.md` (49.5.1, the list of commands) and `idempotency-audit.md` (49.4.7, which routes carry a key).

## Method

`internal/testfault` (test code only, imported by no non-test package) fails a command at one chosen write. `Arm` puts a BEFORE INSERT/UPDATE trigger on one tenant table; it raises `INJECTED_FAILURE[label]` only for a row whose JSON text contains the test's unique marker, so no other test or session can trip it. The trigger and function are created per call and dropped by the returned disarm and by `t.Cleanup`. `Fingerprint` records, per table, a row count and hash of the marker's rows; `Diff` compares two.

For every step of a command the runner (`engines/atomicity_fi_harness_test.go`, HTTP version in `internal/server/atomicity_injection_checkout_test.go`):

1. arms the fault and runs the command; it must fail, and the error must carry that step's label (so it failed at the intended write, not elsewhere);
2. fingerprints before and after: stock, GL, documents, outbox, loyalty, bins, reservations must be identical (audit rows are left out of this check only, because a failed attempt may legitimately record that it failed);
3. disarms and retries: the effect must be there once;
4. replays: same answer (or a plain refusal where the command's idempotency is its document status), and the fingerprint, audit included, must not move;
5. races four callers: the effect must still be there once.

`TestFaultHarnessSeesANonAtomicWrite` is the negative control: a write outside the transaction leaves its first half behind and the fingerprint reports it.

## Command by command

| Command | Steps failed | Proven | Result |
|---|---|---|---|
| POS checkout (`pos.checkout`, HTTP) | availability, stock ledger, GL, cart paid, outbox, idempotency completion | full rollback, sensible API error, same key retries once, replay returns the same sale and moves nothing, concurrent duplicates make one sale | passes, no bug |
| Inventory adjustment post | availability, stock ledger, GL, document Posted | as above; race | bug found and fixed: a repeat call wrote a second "posted" audit row |
| Transfer dispatch | availability, document Dispatched | as above; race | passes |
| Transfer receipt (by receipt key) | availability, document Received, stock ledger | as above; race | passes |
| Journal voucher post (approved) | GL, document Posted | as above; race | passes |
| GRN receipt (`PostGRNReceiptWithQC`) | availability, stock ledger | as above; race | bug found and fixed: see below |
| Credit note post | GL, document Posted | as above; race | bug found and fixed: see below |
| Vendor invoice payment | GL, document Paid | as above; race | bug found and fixed: see below |
| Prepaid schedule start; amortization recognition (162.9) | GL, document stamp | as above; the GL now posts in the stamping transaction | passes after fix |
| Early-payment discount; GRN rate note; GRN correction; vendor credit note leg (162.9) | GL, document, cost, stock | one transaction each; the GRN correction cost move is now fatal | passes after fix |
| Till close and its approval (162.9) | status, variance GL | a close that cannot post its variance is refused and stays open | passes after fix |
| GRN receipt `inventory.stock_changed` event (162.11) | availability, outbox row | event written in the receipt transaction; a repeat never publishes a second | passes after fix |
| Reverse journal voucher (162.10) | reversal voucher, original marked reversed | was three writes; now one transaction under a row lock | bug found and fixed |
| Vendor credit note, payslip post, cycle count post, purchase return, sales invoice cancel, refund processing (162.10) | GL, stock, document status | as above; race (purchase return skips `sequence_counters`: a failed post leaves a number gap) | passes, no bug |
| Period close (162.10) | status change | failed close leaves the period Open and unlogged; replay refused (test only) | passes |

47.2-47.5 mapping (proof that already exists, found by test name): server price (47.2) `TestStage472ClientSubmittedPricesNeverReachTheSale`, `...CashierNeverReceivesCostFields`; exactly-once sale (47.3) `TestStage473RepeatedIdenticalRequestsPostExactlyOneSale`, `...ConcurrentTillsSellingTheSameSKU`, `...FailureAfterStockLeavesNothingPosted`, `...CompletedSaleReconciles`; exactly-once return (47.4) `TestStage474RepeatedSubmissionRaisesOneReturn`, `...CumulativeQuantityCannotBeExceededByTwoClerks`, `...RefundCannotBeProcessedTwice`, `...InspectionPostsStockAndTaxAtomically`. This stage adds the failure-at-each-write dimension for checkout and for the engine commands above.

## Bugs found and fixed (each red first)

- **Credit note post** (`engines/notes.go`): the GL rows were posted in their own transaction before the note's status update. A failure at the status update left the GL posted with the note still Draft. Now `PostDoubleEntryTx` in the command's own transaction. Red: `TestAtomicityCreditNotePost/document-posted` ("gl_postings 0 -> 2"). The same change was made to debit note post.
- **Vendor invoice payment** (`engines/vendor_invoice.go`) and the TDS variant (`engines/tds.go`): same shape (GL committed, then the Paid flip). Red: `TestAtomicityVendorInvoicePay/document-paid`. Fixed the same way.
- **GRN receipt** (`engines/wms_receiving.go`): the stock ledger and GL skip a repeat by idempotency key, but the availability update did not, so a second call (retry, second caller, race of four) added the stock again while the books showed it once (on hand 20 / 40 against a ledger of 10). Red: `TestAtomicityGRNReceipt` replay and race. Now a receipt whose ledger lines already exist is a no-op, and the GRN row is locked so concurrent calls queue. The HTTP handler already guarded this by posting only on create; the engine now holds on its own.
- **Inventory adjustment** (`engines/inventory_documents.go`): a repeat call wrote another POST_INVENTORY_ADJUSTMENT audit row though nothing moved. Red: replay audit_logs 7 -> 8. Now logged only when it posted.

## Not proven, and why

- **Other `PostDoubleEntry` (own-transaction) call sites (162.9 moved the ones named here into the caller's transaction; what remains is below)** still follow the "GL first, status after" shape and are not yet covered by a scenario: deferred revenue / prepaid schedules (`deferred_prepaid.go`), debit note via GRN pricing (`grn_pricing.go`), GRN GST credit note and GRN correction (`grn_steps_post.go`), marketplace cart (`marketplace.go`), POS cash movement (`pos_cash_movements.go`), vendor early-payment discount (`vendor_master.go`). Each should be moved to `PostDoubleEntryTx` the same way and given a scenario. (Debit note post and the two payment paths above are done.)
- **Not yet in the matrix:** sales return and refund (HTTP; the 47.4 tests prove exactly-once, not failure at each write), purchase return, period close, payroll run, order cancellation and its credit notes, vendor credit note, reverse journal voucher, stock count posting. The harness takes each as a new scenario of about 20 lines.
- **GRN handler** (`handlers_core_doc_engine.go`): saves the GRN document, posts stock in a separate transaction and compensates by marking the GRN Cancelled on failure; the `inventory.stock_changed` event is published in a third transaction with errors ignored, so a crash between the stock commit and the event loses the event. Not changed here (it needs an outbox write inside the receipt transaction); suggested item below.
- **Not injectable:** external calls with no compensation (payment terminal, WhatsApp, e-invoice, marketplace pushes), post-commit best-effort effects (loyalty earn, CRM events, incentives, audit rows), and document-number gaps (a number is taken before the transaction by design; a failed command can leave a gap).
- **`RetryPostApprovedJournalVoucher`** replaces every failure with "posting is still blocked", hiding the cause; the scenario therefore runs the same transaction body (`fiPostVoucher`) so the failing step can be identified.

## Suggested items

- Move every remaining `PostDoubleEntry` caller that holds its own transaction to `PostDoubleEntryTx` (list above) and add a scenario for each.
- Write the `inventory.stock_changed` outbox row inside the GRN receipt transaction.
- Extend the matrix to sales return/refund over HTTP, period close, payroll, cancellation and purchase return.
