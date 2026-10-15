---
doc_id: DOC-PRODUCT-ERP-FUNCTIONAL-VERIFICATION-20261004
title: Every-module ERP functional verification and repair plan
type: reference
status: draft
owner: engineering-owner
approvers: [product-owner, qa-owner, finance-owner, operations-owner]
audience: [product, engineering, QA, implementation]
applies_to: local ERP working tree on 2026-10-04
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-04
review_by: 2026-11-04
supersedes: none
superseded_by: none
verification_scope: plan and prior limited UI evidence, not a completed operational simulation
format: work-register
---

# Every-module functional ERP verification plan

## Scope correction and honest baseline

The [Stage 55 screen audit](../assurance/erp-usability-screen-register-2026-10-04.md)
is **not** an end-to-end functional audit. It opened 56 routes, 198 generic New dialogs,
54 tab panels, 16 configuration panels and 92 report parameter forms. It did **not** save
one example of each of 199 record types, reconcile their effects or prove every workflow.
The [2026-10-05 first operational UI run](../assurance/erp-functional-audit-2026-10-05.md)
adds actual master-to-procurement and OMS transactions, but finds AP, transfer and fulfillment
blockers and is also **not** an every-module acceptance result. Its six findings are the first
repair queue; the user asked to defer those fixes until ERP build completion.
Stage 55.1 fixed shared New/Edit and one real Department create/edit, not all transactions.
The [user QA round](erp-user-qa-2026-10-04.md) contains stronger, but limited, live PO →
GRN evidence and real deployed-build failures. Treat UI presence as inventory, not as a pass.

The requested outcome is: **an authorized novice can configure prerequisites, complete a
normal transaction and its exception path without coaching, then see the correct persisted
record, number, linked documents, stock/accounting/payment effect and audit trail**. Admin
setup must be equally understandable. Visual polish is secondary to these results.

There is no single universal ERP behavior: numbering continuity, tax, returns, approval
thresholds, accounting policy and regulated processes depend on company/jurisdiction.
Use documented product rules and owner decisions for those, then test them. External
products are comparators, not an automatic specification. For example, Microsoft explains
number-sequence scope and continuous/noncontinuous tradeoffs; Odoo describes PO/receipt/
vendor-bill three-way matching; ERPNext documents separate source, delivery, stock and GL
effects. [Microsoft numbering](https://learn.microsoft.com/en-us/dynamics365/fin-ops-core/fin-ops/organization-administration/number-sequence-overview),
[Odoo bill control](https://www.odoo.com/documentation/18.0/applications/inventory_and_mrp/purchase/manage_deals/control_bills.html),
[ERPNext sales invoice](https://docs.frappe.io/erpnext/sales-invoice),
[ERPNext stock reconciliation](https://docs.frappe.io/erpnext/stock-reconciliation).

## Method: five business perspectives on *one complete business journey*

One tester can apply these lenses, but that is not five independent users. For each row below,
repeat the journey with (1) a new authorized user following only on-screen guidance and the
published guide, (2) the daily operator handling partial/incorrect/duplicate work, and
(3) the supervisor/admin configuring prerequisites, reviewing exceptions and auditing effects;
then (4) the **Account Head** tracing each financial event through source documents, tax,
subledger, GL, approvals, reconciliation, corrections and close, and (5) a qualified **CA
(Chartered Accountant)** independently reviewing applicable statutory/tax treatment,
period evidence, audit trail and report/export support. The CA must first identify the
jurisdiction, entity and reporting obligations that apply; no generic ERP screen is proof
of compliance. Engineering/security review remains a separate cross-cutting lens.
Real participants and domain-owner acceptance are later gates, not inferred from one tester.

Each mutation must have a disposable-database evidence bundle:

1. **Prerequisites and defaults:** create or select real master data via the UI; select by human
   name while submitting the correct ID/code; missing setup must lead to an actionable path.
2. **Create and read back:** save through the UI, capture response/status/ID, reload by list and
   detail, and compare every business field, line, amount, tax, UOM, date and status. Edit only
   while allowed; verify rejected edits leave the persisted record unchanged.
3. **Numbering:** configure/preview the relevant series as an authorized admin; verify generated
   number's format, scope, fiscal period and uniqueness with two concurrent saves and a retry.
   Do not demand gap-free numbers unless the approved policy requires a continuous sequence.
4. **Lineage and effects:** follow predecessor → current → successor links; compare source/target
   quantities and values; inspect stock, reservations, GL, payments, audit and outbox/jobs where
   relevant. No double posting on reload, retry, Back or duplicate submit.
5. **Exception and access:** partial, invalid, unavailable, denied, cancelled and corrected cases;
   least-privileged role; clear reason and next action; no secret or cross-tenant data leak.
6. **Independent use:** walk the guide literally; a first-time authorized person should identify
   the next action and result without a developer. Record time, errors, help sought and any
   unresolved product-policy decision. Do not call a feature complete because a form renders.
7. **Account Head and CA review:** for financially material journeys, retain the source voucher,
   posting date/period, tax basis and rate, dimensions, subledger and GL entries, reversal/credit,
   bank/stock valuation tie-out, approval evidence and export. Account Head confirms operational
   books and close controls; a qualified CA reviews applicable statutory treatment and evidence
   independently. Record disputed policy or missing functionality as open, not compliant-by-default.

For all **199 metadata record types**, classify as editable master/transaction, derived/read-only,
system/internal or retired. Every editable type needs a UI save/readback/validation result;
read-only/internal/retired types need a documented reason and correct access path. For all 56
routes and 92 report parameter forms, exercise a meaningful populated case, not only a blank
screen. Reuse the Stage 55 screen register as the row inventory. Keep exact fixture IDs,
before/after totals, browser trace and database queries in a sanitized assurance record.

## Functional coverage matrix — all 20 module owners

Status means **functional evidence**, not whether a page exists. `Partial` names the narrow
proof; `Open` means no end-to-end result yet. Each row includes normal and exception cases.

| Module owner | Journey and invariant to prove in the UI + persisted data | Current evidence / gate |
|---|---|---|
| Core | Legal entity, location, department, cost center and sequence configured; two records receive unique scoped numbers; effective settings survive reload and permission checks. | **Partial:** one Department create/edit and restricted-role denial in Stage 55.1; other setup and sequence concurrency open. |
| Master data | Item, Vendor, Customer, UOM, design/variant, tax/HSN, batch/serial save; name/ID/code resolve consistently; duplicate prevention and quick-create return to source. | **Partial:** user QA verified Item/Vendor search and reported deployed ID/code drift; full type matrix open. |
| Procurement | Requisition → RFQ/vendor quotes → approved PO → partial/rejected GRN → invoice; numbers, carried lines, received stock and three-way-match/AP agree. | **Partial:** local PO line readback and GRN Load Items from PO passed; deployed symptom and RFQ vendor/status gap remain (user QA A3/A4). |
| RFQ | Invite/assign vendor, receive comparable quotes, approve selection and convert once to PO; vendor and status history persist. | **Open:** vendor-assignment/status flow reported missing. |
| Sales | Customer/order → allocation → shipment/return → invoice/payment; no duplicate stock or receivable effect; POS sale/refund/till reconciles. | **Open:** prior UI and POS tests do not cover a complete reconciled journey. |
| Inventory | Receipt/transfer/count/hold changes on-hand, reserved and available by correct location, lot/serial and source; reverse/partial cases reconcile. | **Open / known defect:** Transfer Order location ID rejected as code in user QA A2; current shared-tree fix is in progress, not accepted. |
| WMS | Appointment/ASN → dock/receiving → zone/bin/LPN/putaway → pick/pack/load/depart; task and stock state agree after each step. | **Open / known defect:** Bin creation dead-ends without Zone (A6); physical scanner untested. |
| OMS | Channel/manual intake creates SalesOrder once; allocation, exception, shipment, notification, invoice and refund lineage remain intact across retry. | **Open:** screen presence is not ingestion-to-invoice proof; external vendor delivery separate. |
| Finance | Source AP/AR, journals, debit/credit notes, partial payments, bank match and close; balanced GL/aging and no double posting. | **Open / known issue:** PO reference dropdown absent in vendor notes (A10); statutory treatment needs owner review. |
| PIM | Family/variant/media/attributes save; validation and publish-ready state round-trip; channel payload and reimport retain identity. | **Open:** 21 tabs rendered, but full product-to-publish result untested. |
| Manufacturing | BOM → plan/work order → material issue → partial/complete output; WIP, inventory and cost reconcile by source. | **Open:** cold tabs now load; production lineage not proven. |
| Quality | Inspection against source/lot records readings and disposition; failed stock is not accepted; NC/CofA approval and audit persist. | **Open:** forms inspected only. |
| HR | Employee, shifts, attendance, leave and payroll across local dates/timezone; request/approval, balance, pay result and private role scope correct. | **Open:** cold tabs fixed; no payroll-to-payment acceptance. |
| Expenses | Claim with receipt → approval/rejection/correction → payable/reimbursement; balances and attachments survive reload. | **Open:** screen inspection only. |
| Assets | Acquisition/source → custody/transfer/maintenance → depreciation/retirement as supported; register and GL agree. | **Open:** screen inspection only; unsupported lifecycle steps need explicit gap decision. |
| CRM/loyalty | Campaign/offer eligibility, voucher issue/redemption and return effect; balances, consent and edit history persist. | **Open / known issue:** Offer Management edit action absent (A8); targeting is a separate product decision. |
| Service | Ticket linked to customer/contract; assign/escalate/close with history and permission; project cost/source linkage if available. | **Open:** no ticket-to-resolution proof. |
| Stickers | Source GRN/Transfer Order → category template → preview/selected line/whole document → print and reprint audit; barcode identity verified. | **Partial:** GRN whole/line PDFs and some browser fallbacks tested; conditional/device paths open under BLD-041. |
| Reports | Run populated parameters; totals drill to same authorized records; export/schedule/cancel/retry preserve scope and reconcile with source. | **Open:** 92 parameter forms inspected, not 92 result reconciliations. |
| Integrations | Connect/map/test; order or inventory message creates one business result; failed payload redacted, retry idempotent, status/audit truthful. | **Open:** local code/tests do not establish each provider's live contract or delivery. |

## Reference organization: operate it, do not just open its screens

Use a **new isolated local database** for the run, never a production tenant or another session's
fixture. The scenario is a representative trading-and-light-manufacturing company, not a claim
that every customer has this organization. Choose its legal entity, fiscal period, currency,
tax treatment and approval thresholds from **documented test policy**, never infer law from
competitor screens. The values below are quantities, not accounting/tax policy.

| Unit / actor | Setup and recurring responsibility | Evidence at month end |
|---|---|---|
| Head office; tenant admin | Legal entity, fiscal year, numbered series, roles, warehouses, store, departments, cost centers, bank, tax and approval rules | Effective configuration, unique scoped numbers, least privilege, audit history |
| Procurement; requester, buyer, approver | Request material, invite suppliers, compare quotes, approve PO, handle backorder and invoice discrepancy | Requisition/RFQ/PO/GRN/invoice links, open ordered quantity and payable |
| Distribution center; receiver, picker, supervisor | Receive by lot/bin, reject damaged quantity, put away, count, transfer, pick, pack and dispatch | Stock movement lineage, bin/lot balance, shortage and exception history |
| Small workshop; planner and operator | BOM/routing, issue components, complete finished goods, record quality decision | Input consumption, output quantity, WIP/cost and rejected stock |
| Store; cashier and manager | Receive transfer, open till, sell, return, reconcile cash/card and close session | Store stock, receipts/refunds, tender/cash variance and sales GL |
| Online desk; OMS operator | Ingest one channel order, prevent duplicate intake, allocate, ship, notify, invoice, handle return | One SalesOrder, shipment, invoice, payment/refund and event timeline |
| Finance; AP and AR operators | Match supplier bill, post/settle invoices, bank reconcile and prepare expense/asset checks | Source-linked vouchers, AP/AR aging, bank exceptions and correction history |
| Account Head | Own chart/mapping, approve controlled corrections, reconcile stock/POS/procurement/payroll to subledgers and GL, then close period | Balanced trial balance, explained variances, approval/segregation evidence and signed close checklist |
| CA (qualified, independent of build) | Review applicable GST/TDS/other statutory treatments, invoice/credit-note series, input/output tax, period cutoffs and audit/export evidence against the entity's approved policy | Documented applicability, sampled source-to-return tie-outs and explicit qualified sign-off or unresolved findings; no presumed certification |
| People and support; employee, manager, HR, service agent | Attendance/leave/payroll, expense, ticket and contract response | Approved balances, pay/expense result, case closure and privacy checks |
| Product/data; merchandiser and steward | Create item/variant, attributes/media, catalog completeness, label template and publish-ready result | Identity survives import/UI/print/channel mapping; exceptions are repairable |

### One operational month, with exact test activities

**Day 0 — configure, by UI only.** Admin creates legal entity, DC warehouse, store location,
bins/zone, workshop, departments/cost centers, bank and approved numbering series. Create
separate users for each role; log in as those users for their steps. Create a raw material `RM`,
finished good `FG` with BOM of **3 RM per FG**, one lot/serial-tracked item, one service item,
Vendor and Customer. Record opening stock and opening GL/bank balances explicitly; do not rely
on invisible seed data. Verify two simultaneous saves produce distinct numbers in the correct
series and period; failed validation consumes/does not consume a number according to the
approved sequence policy, not an assumed gap-free rule.

**Days 1–3 — procure and receive.** Request **100 RM**, route to buyer, RFQ/compare/approve as
supported, and create a PO for 100 at a controlled test rate. The supplier delivers **80**:
receiver accepts **75**, rejects **5** with a reason, and leaves **20** undelivered. Record a
partial GRN, bin/lot putaway and a vendor bill for the accepted quantity. Compare PO ordered,
received, accepted, rejected, open and invoiced fields after reload. Expected saleable stock
increase is **75 RM**, not 80 or 100; an overbill for 80 must be held/explained rather than
silently payable. Check approved AP/GL impact and a second GRN/retry for double count.

**Days 4–7 — make and move.** Planner releases a work order for **10 FG**. Operator issues
**30 RM** and records 10 good FG; quality records a pass and a separate failed-case sample.
Expected RM on hand becomes **45**, FG at DC becomes **10**, subject only to explicit separate
opening/adjustment transactions. Transfer **6 FG** from DC to Store, with dispatch and receipt
as separate states; DC becomes **4**, Store **6**. A count variance and a hold/release scenario
must post only through approved paths and reconcile to movement history, not direct row edits.

**Days 8–15 — sell, fulfill and return.** Cashier opens a till, sells **1 FG** at Store and
closes with tender reconciliation; Store FG becomes **5**. Online desk ingests a channel order
for **2 FG** exactly once, allocates at DC, picks/packs/ships, records delivery notification
state and generates its sales invoice/payment result; DC FG becomes **2** after shipment. Retry
the same channel payload and shipping event: neither may create a second order, stock issue or
invoice. Then process one eligible sale return/refund, including its approved restock/condition
decision; calculate the new quantity from that decision and trace credit/refund and GL effects.
Do not hard-code a restock result for a damaged return.

**Days 16–25 — run the other functions in context.** Create a PIM variant and category sticker
from the above item; print a GRN/transfer label with the same identity and scan it if physical
hardware is available. Submit/approve an employee leave request and an expense with receipt;
run a controlled payroll preview/finalization as supported, checking local-date and privacy
rules. Register an asset from a purchase where supported and verify depreciation preview.
Open/assign/close a customer service ticket linked to the shipped order. Run a report whose
source set includes these documents; drill and export, verifying totals and permissions.
For each integration, use a local fixture for deterministic intake/retry and separately record
what real-vendor acceptance still requires.

**Month end — Account Head reconciliation, then CA challenge.** Finance checks that debits equal credits; opening
stock + accepted receipts + production output + eligible returns − component consumption −
shipped/sold stock ± approved adjustments equals closing stock by item/location/lot. AP aging
matches accepted supplier bills less settlements/notes; AR aging matches sales invoices less
receipts/credits; bank matches tender and payment postings within documented timing. Review
audit trails, failed jobs, approvals, report totals and period locks. Try a backdated posting
after close with a nonprivileged role: it must be rejected or go through an explicit approved
exception path. The Account Head reviews the actual posting/reversal treatment, signs the
operational close and explains differences. The CA then challenges applicable tax/period
cutoffs, source-to-return totals and immutable evidence with its own samples; any statutory
interpretation needs that qualified review, not an AI or developer assertion. Operations
also signs the stock/source quantities.

At **every activity** capture the acting role, screen path, form data, generated number,
response, list/detail readback, source/target IDs, before/after balances, and visible next action.
For anything absent or unsupported, mark `MISSING` with the blocked operational step; do not
fill it with a screenshot, mocked response or invented workflow. The same dataset should support
all modules, so a failure in one exposes downstream damage rather than being hidden by fresh
unrelated fixtures. Repeat a small set of journeys from all five business lenses, then obtain
separate real-user, Account Head and qualified CA reviews where applicable.

## Execution order and release gate

1. **Source-to-pay first:** master/sequence → requisition/RFQ → PO → GRN → vendor invoice/GL.
   Reproduce deployed discrepancies using a permitted test copy, not production edits. The
   current tree's PO → GRN success does not explain the user's deployed failure by itself.
2. **Stock and order-to-cash:** fix ID/code invariants; test transfer/bin/hold/count, then
   SalesOrder → fulfillment → invoice/payment/return and POS close.
3. **Finance and operations:** reconcile AP/AR/GL, then WMS, manufacturing, quality, HR/payroll,
   expenses and assets with normal and exception cases.
4. **Catalog, service and outputs:** PIM, CRM/loyalty, service, stickers, reports and integrations;
   distinguish local proof from real carrier/payment/printer/provider acceptance.
5. **Close only with evidence:** every module row has named tester/role, fixture, UI trace,
   persisted readback, number check, downstream reconciliation, negative path and documented
   residual/owner approval. A visual tidy-up or passing unit tests alone never closes a row.

Follow the existing Stage 55.3–55.21 domain phases for repairs, but put this functional gate
ahead of their cosmetic work. Stage 55.22 cannot close until the 199-type classification,
all applicable business journeys and real user/domain-owner acceptance are recorded.
No production mutation, deployment, policy invention or vendor/hardware acceptance is implied.
