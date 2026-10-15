---
doc_id: DOC-SOP-VIDEO-COVERAGE-20261005
title: ERP SOP video coverage and release register
type: record
status: draft
owner: qa-owner
approvers: [product-owner, operations-owner, finance-owner]
audience: [trainers, product, QA, engineering]
applies_to: local ERP working tree on 2026-10-05
authority: historical
confidentiality: internal
last_verified: 2026-10-10
review_by: 2026-11-05
supersedes: none
superseded_by: none
---

# Video coverage register

As of 2026-10-10, **25 chapters (64 replaceable segments) are recorded as drafts**
on the disposable training database, one per module, each built from shared setup
segments and ending on its saved record read back in the app. Draft is not
published: every clip still needs the clean recording pass on a fresh database,
a frame review, and its accountable reviewer below. Recording them found and fixed
real app defects (names shown as codes, dead-end buttons, a report showing zero
balances) - see docs/micro_checklist.md Stage 57.
The PO item-picker blockage from 2026-10-06 is fixed; the PO and GRN chapters record.
A chapter becomes “published” only when every referenced clip shows a verified UI
transaction, its exception, saved readback and accountable reviewer. A process with
an open functional defect may have a draft clip, but must not be taught as complete.

| Domain | Draft chapters recorded (2026-10-10) | Not yet recorded | Reviewer |
|---|---|---|---|
| Core/masters | Department, vendor, item, location, customer, users and roles | Entity, series, tax master | Administrator |
| Procurement/RFQ | Requisition, RFQ to winning quote, PO and approval, GRN, purchase return | ASN, receipt discrepancy | Buyer + receiver |
| Finance/AP/AR | Supplier bill match and pay, order invoice post and settle, debit/credit notes, journal voucher | Bank reconciliation, period close | Account Head + qualified CA |
| Sales/POS | Till open, cash sale, coupon sale, till close | Refund | Cashier + Account Head |
| Inventory | Stock transfer warehouse to shop | Count, hold, valuation, stock correction | Stock controller + Account Head |
| WMS | Bins and zones, putaway, bin-to-bin move, pick/pack/dispatch | Inbound appointment, load, 3PL billing | Warehouse owner |
| OMS/marketplace | Phone order, release, dispatch, invoice | Channel intake, notification, customer return | OMS owner |
| PIM/stickers | Family, attribute, required attribute, workbench enrichment; label printer and GRN labels | Variants, media, publish; label design, reprint | Merchandiser + print owner |
| Manufacturing/quality | Finished item, BOM, production order, inspection and approval | Planning, failed-inspection disposition | Planner + quality owner |
| HR/expenses/assets | Employee to posted payroll, leave, attendance, expense claim to payment, asset register and transfer | Asset disposal, appraisal | HR and Account Head, CA if applicable |
| CRM/service | Birthday campaign and ROI; service contract, ticket from call to closure | Loyalty redemption, lapsed-customer campaign | Commercial + service owner |
| Reports/integrations/admin | GST return summary, catalog report and CSV export | Connector diagnosis/retry, backup, access administration | Data/IT/security owners |

For every future chapter, repeat relevant master setup and explain *why* it matters
before showing the transaction. Reuse a shared setup segment by reference, not by
copying its bytes. If the master changes, one new segment propagates to every
chapter. If the transaction's business fixture changes, mark downstream dependent
clips stale and re-record those, not the whole ERP library. Preserve the old
approved edition until the replacement passes review.
