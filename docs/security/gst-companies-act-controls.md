---
doc_id: DOC-GSTCTRL0001
title: GST and Companies Act record controls
type: reference
status: draft
owner: security-owner
approvers: [security-owner, finance-owner]
audience: [maintainers, implementation, auditors, domain reviewers]
applies_to: source release 0.18.0 and later
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-28
review_by: 2027-01-26
supersedes: none
superseded_by: none
---

# GST and Companies Act record controls

Stage 47.16.4. What this product does to keep an Indian business's tax invoices and books as the law expects them kept: invoices that cannot be quietly changed, reversals that leave their own entry, closed periods that stay closed, an edit log, retention and legal hold, and e-invoice / e-way bill applicability with the IRP's evidence.

Each row names the place the control is enforced and the test that proves it. A row with no test says **Gap**. `TestStage47164ControlMapNamesLiveTests` fails if a named test stops existing or a row loses both.

Live IRP / GSP / e-way credential work is not repeated here. It stays with tracker items 20.30, 20.31, 26.2.3 and 26.2.4.

This is an engineering map, not legal advice. Whether a control is enough for a particular client is decided with that client's auditor.

## Controls

| # | Control | Enforced at | Proven by | State |
|---|---------|-------------|-----------|-------|
| C1 | A GST invoice, credit note or debit note number is at most 16 characters, using only capital letters, digits, `/` and `-` (CGST Rule 46(b)). | `validateNumberSeries` checks this when a series is saved (ADMINC-0311). Since 47.12.4, the setup path's `numbering-tax-length` check also flags a series that already prints too long a number. | `TestSeriesLengthCapFollowsHomeCountry`, `TestStage769TaxDocumentNumbers`, `TestStage47124SetupReadinessIsNotRowCount` | Enforced |
| C2 | Once an invoice is posted, a record-form save cannot change the amounts it posted from. | `validatePostedAmountsFrozen` (FIN-0844), run through `ValidateTransactionalRules`. | `TestStage73D8GenericSaveKeepsServerKeysAndFreezesPostedMoney` | Enforced |
| C3 | A save cannot cancel a posted invoice or note, or send it back to Draft. The way to undo it is a credit or debit note, or the document's own reversal (CGST s.34). | `validatePostedAmountsFrozen`, using its list of statuses that would un-post a document (FIN-0844). This works whatever the admin-editable status-transition map allows; before Stage 47.16.4 that map let Approved go to Cancelled given a reason. | `TestStage47164PostedDocumentCannotBeUnpostedBySave`, `TestStage47164GenericSaveCannotUnpostOrFakeASale` | Enforced (Stage 47.16.4) |
| C4 | Editing an issued sale needs a reason, and the edit is recorded with its before and after values. | `validatePostSaleEdit` (SALESP-0616) and `RecordPostSaleEdit`. | `TestStage73PostSaleEditHistory` | Enforced |
| C5 | Once an IRN exists, the fields it certifies are fixed. | `validateEInvoiceLock` (EINV-0983). | `TestStage752EInvoice` | Enforced |
| C6 | A posted document is never deleted; it is reversed. | `postedDocumentStatuses` in the generic delete. | `TestStage47164GenericSaveCannotUnpostOrFakeASale` | Enforced |
| C7 | A till bill becomes Paid only through checkout, which takes the stock, posts the sale and issues the bill number. A save or create as Paid is refused. | `actionOnlyStatuses["POSCart"]` (GLOBAL-0019). | `TestStage47164GenericSaveCannotUnpostOrFakeASale`, `TestStage473RepeatedIdenticalRequestsPostExactlyOneSale` | Enforced (Stage 47.16.4) |
| C8 | Reversals leave their own entry. A credit note reverses the revenue and the output GST, and a cancelled till bill goes through a return and refund. | `PostCreditNote`, `CancelPOSBill`, `ReverseJournalVoucher`. | `TestStage73D10CreditNoteReversesOutputGST`, `TestStage130CancelPOSBill`, `TestJournalVoucherLifecycle` | Enforced |
| C9 | Nothing posts into a closed accounting period unless a specific backdated posting has been approved. | `rejectIfCurrentPeriodClosed` (FIN-0260), called inside `PostDoubleEntryTx`, which every posting goes through. | `TestAccountingPeriodClosesBlockPostingsAndOpenPeriodsStillWork`, `TestBackdatedPostingApproval`, `TestBLD020LeapDayAccountingPeriodBoundary` | Enforced |
| C10 | Every ledger line names the document and the person or job that posted it. The receivables ledger reconciles with the invoices, invoice by invoice. | `PostDoubleEntryTx` stamps `document_type`, `document_id` and `posted_by`. The `receivables-vs-ledger` report does the reconciliation (Stage 49.5.5). | `TestGLPostingAttributionStampsActorAndReportsFilterOnIt`, `TestStage4955ReceivablesAgreeWithLedgerByInvoice` | Enforced |
| C11 | The books themselves are never deleted. | Until Stage 47.16.4, the Super-Admin scale test began with `DELETE FROM gl_postings`, which wiped the client's whole ledger. That delete has been removed. | `TestStage47164ScaleTestNeverDeletesTheLedger` | Enforced (Stage 47.16.4); see G2 |
| C12 | Edit log (Companies (Accounts) Rules 2014, Rule 3(1)): every change to a record is logged with its old and new values, the editor and the reason. | The `log_document_changes` triggers write this, but only on `tenant_default`. A tenant created by `ProvisionTenantSchema` gets one `EDIT_<doctype>` event per save, naming the fields that changed but not their values. | `TestStage130AuditRecordsEditorAndReason` (on `tenant_default` only). Gap: see G1. | Partial |
| C13 | The audit log cannot be altered without detection. | Rows written through the audit writer are signed, and checkpoints seal the log (Stage 47.7). Nothing stops an UPDATE or DELETE, so tampering is detected afterwards rather than prevented. | `TestAuditDirectTamperIsDetected`, `TestAuditDeletedRowIsDetectedByCheckpoint`, `TestAuditArchiveFileTamperIsDetected` | Enforced (detective) |
| C14 | The audit log is kept for at least 8 years (Rule 3(5)). | By default nothing deletes audit rows; `delete_after_days` is unset. `RunAuditArchive` moves rows older than `archive_after_days` into encrypted archive files, which can be restored. No 8-year floor is enforced. | `TestAuditArchiveWriteVerifyDeleteRestoreDrill`. Gap: see G3. | Partial |
| C15 | A legal hold stops retention deletes, customer erasure, audit archiving and tenant purge. | `retentionDelete`, `AnonymizeCustomer`, `RunAuditArchive`, `PurgeTenant`. | `TestStage49187RetentionDeletesInBatchesAndHonoursLegalHold`, `TestAnonymizeCustomerRefusesUnderLegalHold`, `TestAuditArchiveSkipsHeldRows`, `TestStage4915PurgeRefusesUntilItIsSafeAndProvesRemoval` | Enforced |
| C16 | An e-invoice is raised only when it applies. That means: the seller GSTIN's IRP profile is switched on and in effect, the document type is covered, and the sale is B2B. A B2C sale or a supply within the same GSTIN is never sent. | `DecideEInvoice`. | `TestStage752EInvoice` | Enforced; see G4 |
| C17 | An e-way bill is required at or above the threshold set in `gst.ewb_threshold` (default ₹50,000), and always for job work sent to another state. | `EWBDecisionFor`. | `TestStage130EWayBill`. Gap: see G5. | Partial |
| C18 | IRP evidence is kept: the exact request sent with its hash, and the IRN, acknowledgement number and date, signed invoice and signed QR code that come back. | The `irp_documents` table, written by `irpAcknowledge`. `irp_events` is append-only. | `TestStage752EInvoice`. Gap: see G6. | Partial |

## Gaps (each needs a decision before it is built)

- **G1 - The edit log on every tenant.** Every tenant should get old and new values for each change, as `tenant_default` already does. The trigger function writes straight into `tenant_default.audit_logs`, so it cannot simply be attached to another schema. It needs a schema-aware function, plus the trigger added in `ProvisionTenantSchema` and in a migration for existing tenants. *Recommended:* build it, writing one row per save rather than one per field, so `audit_logs` stays within the 49.18 weight budget. It needs an OK because it adds audit rows to production.
- **G2 - Ledger rows the database will not change.** `gl_postings` has no trigger refusing UPDATE or DELETE. Two engine paths update `created_at` (`journal_voucher.go`, `asset_depreciation.go`), and the scale test still posts synthetic sales into a real client's ledger. *Recommended:* make the scale test sandbox-only first (38.7 / 47.12.6). Then make those two paths write the date when the row is inserted. Then add an append-only trigger like the one on `stored_value_ledger`.
- **G3 - An 8-year floor on audit retention.** Add a setting floor so `delete_after_days`, if it is ever acted on, cannot go below 2,922 days, and so an archive file is not deleted inside that window. *Recommended:* yes, as a 49.6 / 47.16.2 retention-matrix entry.
- **G4 - Turnover-based applicability.** An IRP profile's `turnover_band` is only a label. Whether a GSTIN must e-invoice is decided by an administrator switching its profile on; the product does not calculate aggregate turnover. *Recommended:* keep it that way, and show a readiness warning when sales of the type the profile covers exceed ₹5 crore in a financial year while the profile is off.
- **G5 - E-way bill value basis.** For a sales invoice, the value comes from each line's `amount`, or `taxable` when that is missing. Rule 138 counts the consignment value including tax. Check which of the two `amount` holds before relying on the threshold for invoices just under ₹50,000.
- **G6 - Raw IRP response and immutable acknowledgements.** `irp_attempts` keeps only the outcome and the error, and `irp_documents` has no append-only trigger. *Recommended:* keep the raw signed response body, and refuse updates to the acknowledgement fields once they are set.
