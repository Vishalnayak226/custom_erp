---
doc_id: DOC-PROD-135-001
title: Wholeops whole-platform outsider review and improvement plan
type: reference
status: draft
owner: product-owner
approvers: [product-owner, engineering-owner, security-owner, operations-owner]
audience: [product, engineering, implementation, business-owners, operations, security]
applies_to: 2026-10-23 working tree; release 0.12.0 is a separate baseline
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-23
review_by: 2027-01-21
supersedes: none
superseded_by: none
verification_scope: source and document review; no new live UI, load, vendor or customer acceptance
---

# Wholeops: outsider review and whole-platform improvement plan

## 1. Verdict, scope and evidence

Wholeops is an operations platform comprising ERP, retail/POS, inventory, WMS, OMS,
PIM/PXM, CRM, people operations and shared SaaS services. Its advantage can be that a small
organization follows one coherent transaction from setup to fulfillment to financial close,
without maintaining several incompatible accounts of the same product, customer or stock.
Its challenge is proving that coherence across the breadth already built.

Recommend depth and trust in the current reference businesses before wider industry claims.
Aim for leadership in independently usable, connected retail and distribution operations.
"Best in the world" becomes measurable customer outcomes: successful first setup, accurate
stock and money, fast daily work, safe recovery, low support demand and predictable cost.
Adding every imaginable feature would undermine those outcomes.

This is a detailed improvement proposal, not a completed functional audit. Reviewed the
current architecture, product packages, capability catalog, functional matrix, handbooks,
recent Stage 99–134 records, UI source inventory and selected domain/control implementations.
Repository HEAD was `1356183` (release 0.12.0); extensive later work was uncommitted.
No production actions, business-data writes, new browser journeys, penetration tests or
capacity tests were performed for this review. Historical passes retain their original date
and scope. File existence establishes a capability lead to investigate, not correct behavior.

Evidence labels used below:

| Label | Meaning | What it permits |
|---|---|---|
| S | Inspected source/document fact | State what exists or contradicts another record |
| H | Earlier recorded implementation or test evidence | Retain as historical; retest the affected release |
| V | Verification work proposed | Do not describe as a proven defect or a passed control |
| D | New design/market-policy decision | Keep separate from routine fixes and existing decisions |

Important source findings:

1. **S — The platform already models multiple products.** `engines/modules.go` defines PIM,
   WMS, OMS, HR, procurement, manufacturing, CRM, assets, expenses and a full-suite package.
   Prove each package works independently with its actual dependencies; package definitions
   alone do not settle BLD-023's commercial/support contract.
2. **S — Documentation has contradictory capability claims.** `finance-tax.md` says journals
   have no UI, while `public/view-journal.js` creates, reverses and retries journals.
   `pos-operations.md` and `oms-order-management.md` contain old no-UI returns claims, while
   `public/view-returns.js` and `view-pos-extras.js` expose return/exchange journeys.
   `crm-loyalty.md` says there is no dedicated CRM screen; Stage 101 and `view-crm*.js`
   demonstrate a later CRM surface. These are documentation defects, not missing-feature proof.
3. **S — Source comments can also be stale.** `pim_publish.go` describes a stub-only pipeline
   at its head, but its worker resolves a connector and calls `PublishProduct`. Establish
   behavior by platform, configuration and runtime evidence, not the old header comment.
4. **H — Stage 130 records substantial new controls and transactions.** Reconciliation,
   material issue, job work, challans, WMS handoffs and related routes need current integration
   acceptance. Stage 134 already owns the approved security, jobs and documentation follow-up.
5. **S — Earlier baseline counts/statuses are unsuitable as today's coverage report.** The
   October 4 functional matrix remains useful as test design, but its missing-UI claims
   predate many later screens. The capability catalog also carries a development version label.
   Reconcile these records without rewriting dated audit history.
6. **Measured — Artifact budgets currently pass with very little shell headroom.** A read-only
   `go run ./cmd/releasebudget` on this working tree returned cold core 184,199/184,320 bytes
   (121 bytes free) and initial JS 114,253/122,880 bytes. This is an artifact measurement,
   not an interaction/capacity result or a stripped-binary measurement. Keep new work lazy
   and recover meaningful shell headroom before another large shared-UI change.

Priority meanings: P0 blocks a release for affected customers if reproduced (stock/money,
unauthorized access, corruption, irrecoverable operation); P1 blocks dependable supported
operation; P2 improves throughput/adoption; P3 requires validated market demand. A proposed
test case is not automatically an open P0 defect.

## 2. Product model and review responsibilities

### One owner for each business truth

| Truth | Authoritative responsibility | Consumers |
|---|---|---|
| Tenant, entity, location, user and permissions | Platform/core | Every command, job, export and cached view |
| Item/SKU, family/design, variant/combination and UOM | Master data with PIM enrichment | Buying, stock, sales, production, labels, channels |
| Physical quantity, custody, condition and reservations | Inventory with WMS execution detail | POS availability, OMS promise, MRP, finance reconciliation |
| Customer commitment and fulfillment intent | Sales/OMS | Warehouse tasks, shipments, invoices, returns and service |
| Posted monetary effect and period | Finance | Operational subledgers, tax evidence, reports and close |
| External action and its acknowledgment | Connector/job evidence | Operators, reconciliations and exception queues |

Document these ownership rules against actual tables and commands in the first phase.
Do not create a new universal ledger or replace existing dedicated tables to fit this diagram.
WMS bins, location stock and OMS reservations are related views with distinct jobs; they must
reconcile without each independently booking the same physical event.

### Review the same journey through all requested lenses

| Lens | Question to answer | Review output |
|---|---|---|
| CEO/product head | Which customers get a valuable, supportable outcome? | Supported use cases, limits, adoption and margin measures |
| BA/PM | What starts/ends the process, who owns exceptions, what depends on it? | State map, business rules, dependencies and acceptance cases |
| Operator/new user | Can I finish and recover without asking the developer? | Observed task result, errors, time and assistance count |
| HOD | Can I distribute work, approve exceptions and see delays? | Queues, ownership, aging and escalation evidence |
| ERP/Account Head | Do documents, stock, subledgers and GL agree? | Source-to-posting reconciliation and controlled close |
| CA | Is configured statutory treatment applicable and supported by evidence? | Qualified review findings and explicit sign-off scope |
| WMS expert | Does the screen match physical custody, scanning and partial work? | Floor/device journeys and quantity/identity reconciliation |
| OMS expert | Does one promise survive splits, failures, cancellations and returns? | Order-to-settlement lifecycle and repeat-delivery proof |
| PIM expert | Is product identity stable and publication governed by channel/locale? | Variant/data quality, provenance and publication evidence |
| UX/accessibility | Can people perceive, understand and operate the task? | Keyboard, focus, language, touch and assisted-user findings |
| Developer/CTO | Are shared rules explicit, testable, bounded and maintainable? | Command ownership, migrations, failure contracts and measurements |
| Hacker/security head | Can I cross scope, replay intent, steal data or exhaust resources? | Authorized negative tests, findings and remediation retests |
| SaaS/operator/support | Can we onboard, isolate, upgrade, restore and offboard a client? | Lifecycle drills, responder ownership and cost evidence |

One reviewer can use these lenses to generate tests; that does not substitute for independent
operators, finance owners, a qualified CA or a security engagement.

## 3. Common contract for every page and workflow

Apply shared improvements through existing document, command, scope, task, import and error
boundaries. Preserve the lightweight Go/PostgreSQL/native-JS architecture.

1. **Purpose and next action:** title states the job; show current state, acting location,
   assignee, prerequisite, source and permitted next step. Administrative configuration stays
   discoverable without becoming the operator's main workflow.
2. **Identity and numbering:** immutable internal identity, readable document number and human
   name; tenant/entity/location/fiscal scope as configured. Preview a number as provisional;
   save allocates authoritatively. Prove concurrent uniqueness, rollover and retry semantics.
   Do not promise gap-free numbering without an approved policy and implementation.
3. **Master dependencies:** explain missing setup at the point of need; quick-create if
   authorized, return with the selection and entered work retained. Avoid free-text foreign
   keys and silent fuzzy matches. Recode changes preserve historical identity and references.
4. **Saved means persisted:** show the server-saved result, audit reference and reopened detail.
   Pending/offline/queued/posted/delivered must remain visibly different states.
5. **Transitions:** dedicated business commands own approval, posting, shipment, settlement and
   reversal. Audit generic edit, bulk import, API and background paths for equivalent guards.
   A hidden button is insufficient authorization.
6. **Partial operations:** quantities explicitly show planned, accepted, rejected, reserved,
   processed and remaining where applicable. Explain over-receipt, shortage, backorder and
   cancellation policy before accepting a consequential action.
7. **Recovery:** preserve valid input on errors; an uncertain outcome offers saved-state lookup
   and safe retry with the same command identity. A retry must not become a second transaction.
8. **Work management:** exceptions have owner, age, reason, source link and legal corrective
   action. A notification is not a task closure or provider-delivery receipt.
9. **Lists/search:** filter server-side before paging, stable ordering and clear selection
   scope. Never imply totals across all records from the loaded page. Find older records and
   matching items beyond the first page under the same authorization rules.
10. **Accessibility:** predictable Tab/Enter/Escape, visible focus, readable errors, labelled
    controls, text alongside color, adequate touch targets, zoom/reflow and scanner-safe input.
    Evaluate relevant WCAG 2.2 AA criteria, including manual keyboard and assistive checks.
11. **Print/export:** preview the issued revision, identify original/reprint and reason where
    required, expose truthful spool/physical status, and preserve authorization in exports.
12. **Help:** short task-specific steps, prerequisites and common exceptions; replaceable SOP
    segments versioned to the screen and policy. Each process explains needed setup even when
    that setup was already taught in another process.

## 4. Module-by-module improvement work

### Page ownership inventory at review time

The following maps every top-level `public/view-*.js` file found during this review. These
are source entry points, not a claim that every nested tab/action has been inspected live.
P01 expands them into routes, tabs, forms, permissions and commands, including generic
Setup pages generated from metadata rather than a dedicated file.

| UI source files (under `public/`) | Work-package owners |
|---|---|
| `view-admin.js`, `view-setup-workbook.js`, `view-control-panel.js` | M01, M30 |
| `view-item-form.js`, `view-pickers.js`, `view-master-recode.js`, `view-attribute-library.js` | M02, M14 |
| `view-rfq.js`, `view-procurement.js`, `view-bulk-purchase.js` | M03, M04 |
| `view-pos.js`, `view-pos-customer.js`, `view-pos-extras.js` | M05, M06, M19 |
| `view-returns.js` | M06 |
| `view-inventory-erp.js`, `view-transfers.js`, `view-stock-locations.js`, `view-consumption.js`, `view-store.js` | M07, M10, M20 |
| `view-warehouse.js`, `view-rf-traceability.js`, `view-wms-labour.js` | M08–M11 |
| `view-oms.js`, `view-oms-steps.js`, `view-shopify.js`, `view-handoff.js` | M12, M13, M28 |
| `view-pim.js`, `view-images.js` | M14, M27 |
| `view-manufacturing.js`, `view-jobwork.js`, `view-quality.js` | M15, M16 |
| `view-finance.js`, `view-journal.js`, `view-sales-invoice.js`, `view-einvoice.js`, `view-ewaybill.js` | M17, M18 |
| `view-crm.js`, `view-crm-messaging.js`, `view-crm-sales.js`, `view-crm-service.js`, `view-loyalty.js` | M19, M24 |
| `view-hr.js`, `view-expenses.js`, `view-assets.js`, `view-service-desk.js` | M21–M24 |
| `view-planning.js`, `view-dashboards.js`, `view-reports.js` | M25, M26 |
| `view-printing.js`, `view-reprint.js` | M27 |
| `view-api-access.js`, `view-import-jobs.js` | M28 |
| `view-users.js`, `view-roles.js` | M29 |
| `app.js` (the shell: sign-in, Home, Profile, menus), `view-documents.js`, `view-document-links.js`, `view-inbox.js`, `view-help.js` | Shared contract across M01–M30 |

Also include the `public/store/` app, customer-facing `public/p/` and `public/f/` surfaces,
login/account flows in the shell, `grn-steps.js`, transaction/template/print studios, and
every dynamic Setup/Configuration/Schema page. Customer-facing links, shared-phone views
and supplier access need their own authentication/privacy acceptance, beyond desktop menus.

Each numbered area below has an operational objective, current source anchors, proposed
work and a concrete completion test. These are work-package definitions; they are not
claims that every described improvement is currently missing.

### M01 — Organization, core setup, industry and localization (P1)

**Objective:** an administrator configures a viable company with understandable dependencies.
**Existing:** guided setup, settings registry, setup workbook and industry controls; inspect
`view-admin.js`, `view-setup-workbook.js`, `engines/setup_workbook.go` and `settings_definitions.go`.
**Work:** build an effective-configuration checklist by chosen business workflow; show which
defaults are inherited, locked or overridden. Preview workbook changes and repeated application.
Separate tenant, legal entity, store, warehouse, bin and stock location in plain language.
Expose fiscal/calendar/timezone/currency consequences, validation and scoped numbering.
**Accept:** a fresh admin configures purchase, receipt and sale using UI/help alone; missing
bank/tax/UOM/series dependencies are repairable without losing the original task. Another
tenant is unaffected; repeated workbook application produces no duplicate masters.

### M02 — Item, vendor, customer and shared master data (P1)

**Objective:** the organization uses one reliable identity across departments.
**Existing:** `view-item-form.js`, `view-pickers.js`, attribute library, recode and validation.
**Work:** one duplicate-review policy for each master, explicit active/inactive behavior,
UOM/precision, tax defaults and effective dates. Protect used identity and economically
significant changes; show dependent documents before merge/recode. Search names and codes
consistently, including historical references. Customer privacy and vendor payment changes
need distinct permissions.
**Accept:** create, reuse, import, deactivate and authorized recode a master; old PO, stock,
invoice, barcode and channel references remain intelligible. Similar names do not silently
merge; an unprivileged user cannot change sensitive master fields.

### M03 — Requisition, RFQ and supplier selection (P1)

**Objective:** a requester obtains an approved, comparable purchase commitment.
**Existing:** requisition masters, `view-rfq.js`, procurement UI and approval engine.
**Work:** trace request → invited suppliers → quote revisions → comparison → award → PO.
Inspect the latest decisions before defining line/UOM/currency/tax/freight/lead-time semantics.
Identify eligible outstanding quantities, award approvals and repeated conversion behavior.
**Accept:** two suppliers quote comparable lines, a reviewer selects the winner and the buyer
creates the linked PO once; a late/revised quote and rejected award remain auditable. An old
"no conversion" handbook statement alone must not trigger duplicate implementation.

### M04 — PO, receiving/GRN, vendor bill and purchase return (P0 integrity; P1 flow)

**Objective:** ordered, physically accepted, billed and payable quantities agree.
**Existing:** stepped GRN, inclusive pricing, bulk purchase upload, `grn_steps_post.go`,
vendor invoice and purchase-return engines; Stages 94, 129 and 130.
**Work:** reconcile partial/child receipts, rejection, replacement, backorder and closure;
surface price/quantity/charge variances with the configured tolerance and authorized action.
Preserve PO revision, supplier invoice uniqueness and accepted-value lineage. Separate
validation, posting and supplier-document evidence in the bulk path.
**Accept:** PO 100, receipt 80 with 75 accepted and 5 rejected, then another delivery and a
supplier return; receipt stock, open commitment, bill match, AP and reversals reconcile.
Repeat a lost-response post/import and prove one effect. The buyer can resolve a mismatch
through the UI without editing posted financial facts.

### M05 — Retail/POS, tills, pricing and tender (P0 integrity; P1 operation)

**Objective:** a cashier makes a correct, fast sale and closes a reconciled till.
**Existing:** `view-pos*.js`, quote/checkout engines, tenders, sessions and offline replay.
**Work:** verify server-priced offers, tax, rounding, mixed tender, advances, voucher/store
credit and original-tender refund; explain authorization for discounts, cancel and cash moves.
Inspect staff switching, till ownership, concurrent baskets and offline/reconnect conflicts.
Keep unaccepted queued sales visibly pending.
**Accept:** two cashiers run normal, declined, uncertain and offline sales, then cash/card
close with a variance. Stock, GL, receipt and stored-value/loyalty effects reconcile. Repeated
checkout/tender acknowledgment never spends a balance or issues stock twice.

### M06 — Returns, exchanges, RTO and refund (P0 integrity; P1 recovery)

**Objective:** resolve a return while controlling item condition and money independently.
**Existing:** `view-returns.js`, POS exchange and return/refund reconciliation engines.
**Work:** unify entry from sale/order/service into the existing return lifecycle; show
eligibility, previously returned quantity, pickup/receipt, QC disposition, refund approval
and actual payment outcome. Explain exchange price differences and store-credit choices.
**Accept:** partial return, damaged return, RTO, exchange and provider-timeout refund remain
linked to the original sale. Cumulative returns/refunds cannot exceed eligibility. Restock
follows approved disposition; original tax and loyalty/stored-value consequences reconcile.

### M07 — Inventory, stock locations, transfers, holds and counts (P0 integrity)

**Objective:** anyone can explain where stock is, why it is unavailable and what changed it.
**Existing:** inventory ERP, transfer lifecycle, stock-location views and stock control reports.
**Work:** consistent stock vocabulary and as-of reconciliation; linked movement history;
transfer dispatch/receipt/shortage/excess/return states; blind count/recount/approval and
movement cutoffs. Separate condition, custody, ownership and reservation. Show reasons when
negative-stock rules permit an exception, including later valuation consequences.
**Accept:** two stores complete partial transfers with shortage/damage, hold/release and
count while another user sells/moves stock. Opening + movements = closing at location and
tracked dimensions. Dispatch and receipt cannot both create quantity; holds cannot become
available through another picking or sale route.

### M08 — WMS inbound: appointments, yard, ASN, receive, putaway (P1)

**Objective:** a receiver turns arriving goods into correctly located, usable stock.
**Existing:** `wms-start-here.md`, warehouse view, RF receiving, appointment/yard/putaway engines.
**Work:** one guided arrival → dock → ASN/PO → accepted receipt → LPN/bin putaway journey;
validate capacity, item mixing, condition and ownership policy. Preserve partial receiving,
overage, unknown barcode, blocked bin and cross-dock decisions with accountable exceptions.
**Accept:** scan a multi-line partial delivery on real target hardware, interrupt and resume
on another device, then put away or cross-dock. Location stock, bin detail, lot/LPN and source
receipt reconcile; repeated scanning does not double-receive.

### M09 — WMS outbound: replenish, wave, pick, sort, pack, load (P0/P1)

**Objective:** ship the correct authorized units with uninterrupted custody and recoverable work.
**Existing:** wave/mobile picking, sortation/loading and Stage 130 WMS handoffs.
**Work:** tie reservation and physical picks to the same demand; provide short-pick,
substitution policy, damaged-bin and reassignment paths. Verify durable confirmations,
package contents, departure authority and restart behavior. Explain the distinct meaning
of picked, packed, loaded, handed over and shipped.
**Accept:** two pickers share a wave; one disconnects, one short-picks, one package fails
verification. Complete the remaining work without duplicate picks or shipment. Loading the
wrong package/trailer is refused; resumed work starts at the saved line. Quantities reconcile
from location through bins/packages to one shipment and financial issue.

### M10 — Lot/serial/expiry, recall and quality disposition (P0/P1)

**Objective:** trace a physical unit both backward and forward and block unsafe use.
**Existing:** traceability engine, RF traceability screen, bin conditions and lot reports.
**Work:** verify identity propagation through every receipt/transfer/issue/return path,
FEFO rules and exceptions, expiry cutoff, quarantine and release authority. Provide a recall
worklist from lot → on-hand → shipments/customers → returns/disposition, with export scope.
**Accept:** a lot with several receipts and partial shipments is fully accounted for;
expired/quarantined stock is blocked through POS, OMS, WMS and production. A serial cannot
be issued twice; a recalled lot's affected orders and remaining quantities are discoverable.
Use GS1 event concepts where interoperability is required; no compliance claim from field names.

### M11 — Warehouse labor, workload and capacity (P2)

**Objective:** supervisors allocate achievable work without hiding queue overload.
**Existing:** `view-wms-labour.js`, task dispatch and warehouse cockpit.
**Work:** confirm task ownership, skill/shift/zone eligibility, aging, reassignment and
capacity visibility; separate observed throughput from assumed standards. Protect worker
privacy and make manual priority overrides auditable. Do not promise labor billing or
optimized labor planning from a task list.
**Accept:** a supervisor balances an overloaded shift, handles absent staff and resumes
unclaimed work; task totals and durations can be explained from source events. Proposed
productivity claims require a real baseline and employee/process-owner review.

### M12 — OMS promise, order intake, allocation and exceptions (P0/P1)

**Objective:** accept one customer promise and explain its fulfillment status reliably.
**Existing:** `CreateSalesOrder`, OMS steps, allocation, channel health and exception queues.
**Work:** retain original channel identity and price facts; distinguish order, payment,
allocation, shipment and invoice state. Test reservation expiry, split fulfillment, store
switch, address/payment hold and changes after release. Provide legal recovery from each
exception with an assignee, time and source history.
**Accept:** duplicate/out-of-order channel intake, simultaneous allocation, partial stock,
cancellation during pick and delayed payment cause no duplicate commitment or oversell.
An operator traces each remaining unit to reservation/task/shipment or an explicit exception.

### M13 — Delivery, courier, marketplace settlement and notifications (P1)

**Objective:** book, deliver, communicate and reconcile an order using external evidence.
**Existing:** marketplace/courier engines, channel adapters and OMS Pack/Ship interfaces.
**Work:** map which screen calls which adapter; make configured provider, simulation mode,
AWB source and last acknowledgment explicit. Reconcile booking retries, labels, pickup,
manifest, NDR, RTO, COD/payout fees and notification delivery. Follow actual provider signature
contracts; document per-provider exceptions to any shared verifier.
**Accept:** booking succeeds remotely but the local response is lost; status inquiry/retry
recovers one booking. Delayed/repeated callbacks cannot regress terminal shipment state.
Shipment, invoice, collection/refund and payout differences are explainable. Local fixtures
prove recovery; each advertised provider requires a separate credentialed acceptance run.

### M14 — PIM/PXM, product variants, pricing, media and publication (P1)

**Objective:** a steward produces accurate, approved, channel-ready product information once.
**Existing:** workbench, taxonomy/attribute library, product groups, media, supplier submissions,
catalogs, imports and connector publishing. Inspect `view-pim.js` and the current publish worker.
**Work:** preserve design/family → combination/SKU identity; display required fields by
family/channel/locale, permitted editors and direct correction. Verify media rights/expiry,
version provenance and intended public/internal roles. Tie effective prices to consumers;
verify whether each older price-list gap still exists. Close import result ownership/resume,
supplier isolation and schedule semantics against current code before adding new workflows.
**Accept:** one manual and one imported multi-variant product reaches publish-ready; rejected
content/media is corrected and reapproved, restricted edits fail, duplicate import preserves
identity, and remote rejection/drift returns an actionable task. Local readiness, queued job,
successful delivery and live channel listing stay distinguishable.

### M15 — Manufacturing, MRP, scheduling, materials and job work (P1)

**Objective:** plan and make supported products with traceable materials, output and cost.
**Existing:** BOM/routing, MRP/scheduling, material issue, job work and new charge/challan flows.
**Work:** make BOM revision/effectivity, yield/UOM, shortage and scheduling assumptions visible;
control release, issue, partial completion, scrap/rework and finished-goods quality. Reconcile
outside-party custody and unused materials; identify the current actual-cost inputs before
proposing deeper costing. Separate executable planning from advisory suggestions.
**Accept:** build 10 FG using 30 RM, with partial output, scrap and one subcontracted operation;
material at plant/vendor, WIP, good/rejected output and cost reconcile. Insufficient material,
capacity clash and a changed BOM produce explicit choices; repeats create no second issue.

### M16 — Quality, maintenance and equipment (P1/P2)

**Objective:** quality decisions control usable stock; maintenance controls equipment readiness.
**Existing:** inspection, certificate, NCR/CAPA and preventive maintenance engines and quality UI.
**Work:** link inspection plans/results and disposition to receipt/lot/production sources;
clarify sampling, approver and release policy. Link CAPA verification to the original issue.
For maintenance, connect asset, planned date, downtime, work performed, spares and return to use.
**Accept:** failed inspection blocks consumption/sale until authorized release; CAPA closure
needs evidence. An overdue or completed maintenance task has a traceable result and owner.
Regulated quality signatures/validation require their own approved industry scope.

### M17 — Finance: AP/AR, journals, reconciliation and close (P0 integrity)

**Objective:** an Account Head can explain every balance and close a controlled period.
**Existing:** finance workbench, journals, invoices, settlements, bank reconciliation and
transaction control totals (`transaction_controls.go`); separate financial and tax guides.
**Work:** trace every posting to source/revision, actor, approval, entity, period and reversal;
reconcile AP/AR to GL, inventory valuation, tender clearing, advances and stored-value
liabilities. Make partial/unallocated payments and bank differences actionable. Verify the
same as-of cutoff is used across detail, summary and ledger; do not hide timing differences.
**Accept:** run a complete operational month with partial payments, notes, FX if supported,
duplicate bank lines and a corrected posting. Trial balance balances; subledgers tie or show
owned exceptions. Closed-period changes are rejected or use a documented authorized path.
Account Head approval and qualified CA review remain separate from developer tests.

### M18 — Tax, e-invoice, e-waybill and statutory evidence (P0 applicability; P1 operation)

**Objective:** generate accurate configured tax documents and manage provider failure visibly.
**Existing:** GST, IRP interface, e-waybill, delivery challans and tax reports.
**Work:** inventory supported transaction treatments, entity registrations, place-of-supply,
rounding, returns/cancellation, document revision and identifiers. Freeze issued facts while
allowing lawful correction paths. Reconcile source tax to statutory export/provider receipt;
distinguish configured example rates from approved entity policy.
**Accept:** applicable cases reviewed by a qualified CA agree from transaction to ledger and
provider/export; timeout, rejection and cancellation retain one traceable history. Regulatory
requirements must be checked against current official sources during implementation; this
plan supplies no new legal interpretation or statutory certification.

### M19 — CRM, consent, loyalty, campaigns and customer service (P1/P2)

**Objective:** service and retain customers with correct identity, permission and value balances.
**Existing:** Stage 101 customer profile, consent ledger, segments, send gate, journeys,
feedback, tickets, inbox, loyalty and sales pipeline.
**Work:** unify timeline/source links, merge survivorship, consent by purpose/channel and
withdrawal propagation. Show campaign audience preview/exclusions and actual provider outcome;
control journey reentry/duplicates. Reconcile points, vouchers, gift cards and store credit
with sale/return/refund. Keep restricted customer fields out of broader exports and devices.
**Accept:** consent withdrawal prevents an already-queued prohibited send; duplicate events
do not issue a second reward. Customer merge preserves orders, consent provenance and value
without cross-customer leakage. Sender validation remains external to simulated campaign runs.

### M20 — Store app and shared/mobile devices (P1)

**Objective:** a floor worker receives, counts, serves and hands work to the till safely.
**Existing:** Stage 102 app, PIN device identity, Today/stock/floor pages and push worker.
**Work:** explicit device registration/revocation, staff/location scope, safe local caching,
offline status, barcode handling and interrupted handoff. Clarify task assignment versus a
push alert; test browser lifecycle, expired login and second-user use of a shared phone.
**Accept:** real phone receive/count/basket handoff passes with Wi-Fi loss and screen lock;
revoked staff/device cannot continue from cache or retry queues. Count and basket remain
pending until server acceptance. Preserve real-phone gates already recorded in Stage 102.

### M21 — HR, attendance, leave, payroll and offboarding (P1)

**Objective:** pay the right person correctly and protect employment data.
**Existing:** HR self-service, attendance/leave, salary structures, payroll and reconciliation.
**Work:** effective-dated employment/pay policy, overnight shifts, local business dates,
leave accrual/balance, arrears/adjustment and payroll lock/reversal. Separate payroll processing
from export to another payroll system. Offboarding must revoke access, settle advances and
recover custody without exposing grievances or salary to unrelated managers.
**Accept:** joiner/leaver and overnight attendance cross a month boundary; leave correction,
payrun approval, GL and payment/export reconcile. A second finalization does not duplicate
payables. Statutory payroll and compensation rules require jurisdiction-specific owner review.

### M22 — Expenses, advances and reimbursement (P1)

**Objective:** reimburse justified expenditure once, against evidence and policy.
**Existing:** expense claim approval/finance verification/payment UI and engines.
**Work:** receipt deduplication review, policy/category limits, advance application, cost-center
allocation, tax evidence and rejection/correction. Separate Mark Paid from a bank-confirmed
payment; expose the settlement reference and unresolved difference.
**Accept:** one partial claim against an advance, one rejected receipt and an uncertain payment
can be resolved without a duplicate reimbursement; expense, employee advance and bank/GL agree.

### M23 — Fixed assets, custody, depreciation and disposal (P1/P2)

**Objective:** preserve physical and financial accountability through an asset's life.
**Existing:** asset register, capitalization, transfer/disposal and maintenance links.
**Work:** verify source invoice/acquisition, useful-life and method policy, depreciation
posting versus preview, custody confirmation, impairment/disposal and supporting evidence.
Show the distinction between a physical transfer and a legal-entity financial transfer.
**Accept:** buy → capitalize → transfer custodian → depreciate → dispose, with source evidence,
GL/register reconciliation and no depreciation posted twice. Method changes follow approved
accounting policy; a calculated net block is not proof of a posted journal.

### M24 — Projects, service contracts and technical service (P1/P2)

**Objective:** deliver promised service with accountable time, materials, cost and closure.
**Existing:** service management/desk engines, CRM service, project/job-cost records.
**Work:** clarify which desk owns customer cases versus technical work and link them instead
of duplicating tickets. Verify contract/warranty entitlement, skill assignment, SLA business
calendar, escalation, materials, resolution, reopening and billing handoff as supported.
**Accept:** an order-linked complaint becomes assigned technical work and a documented
resolution, with contract decision, consumed parts/cost and customer history intact. Separate
service-accounting depth needing a new business contract from defects in existing tasks.

### M25 — Planning, forecasts, replenishment and dashboards (P2)

**Objective:** tell a planner what action to take and why, using trustworthy data.
**Existing:** planning screen, inventory replenishment, MRP and Stage 100 custom dashboards.
**Work:** disclose freshness, scope, demand history, lead time, seasonality/stockout treatment,
formula and recommended-versus-approved state. Add drill-through to the contributing records.
Validate datasets and digest permissions after the owner's rights change.
**Accept:** a planner explains one recommendation from its inputs and converts it through an
approved transaction; dashboard/export agrees with the same source cutoff. Forecast accuracy
claims need backtesting against a simple baseline and the chosen planning horizon.

### M26 — Reports, exports, reconciliations and executive information (P1)

**Objective:** decision-makers can trust a number and reach the transactions behind it.
**Existing:** registered reports, custom datasets, control totals and stored reconciliation runs.
**Work:** document each metric's grain, inclusions, currency, timezone, cutoff and late-arrival
behavior. Enforce scope before aggregation; reconcile UI, scheduled output and export. Bounded
streaming/cancellation, formula-safe CSV and repeatable schedules must preserve the same rules.
**Accept:** reports on a known month tie to stock/subledgers/GL, including period-end late
events; a scoped role cannot infer another entity's totals. A large canceled export releases
work, and a failed scheduled report creates an owned exception.

### M27 — Documents, stickers, printers, media and SOPs (P1)

**Objective:** the physical/output document matches the saved business event reliably.
**Existing:** template/sticker studios, PrintBridge routing, print issue/history/reprint,
preview and version snapshots; Stages 127, 130, 133 and 134.1.
**Work:** build the device/language/DPI/template matrix; configure one printer once, explain
offline/wrong-language/label-direction failures and recover without false "printed" claims.
Preserve the issued document revision, reasons/copies and selection scope. Media needs durable
authorized storage and restore; SOP segment dependencies need change/version checks.
**Accept:** real label and A4/receipt devices reproduce preview size/direction and scannable
identity; retries do not duplicate issued history. Replace one SOP segment after a process
change, retain the process start/end outline and rebuild only affected presentations.

### M28 — Integrations, imports, APIs and extensions (P0 boundary; P1 recovery)

**Objective:** exchange data safely while keeping one recoverable business outcome.
**Existing:** connector setup, API access, import jobs/workbooks, webhooks and extension hooks.
**Work:** supported adapter contract table: authentication, mapping, version, idempotency,
rate limits, raw-body signature, retries, reconciliation and sandbox availability. Imports
need preview, explicit create/update identity, row errors, job owner and deterministic resume
semantics. Extensions declare capabilities and cannot bypass tenant or transaction controls.
**Accept:** replay, out-of-order event, schema drift, secret rotation, partial import and
provider outage produce an actionable outcome with no double stock/money effect. Verify API
scope, field restrictions, export and worker behavior independently of navigation permission.

### M29 — Identity, approvals, security and audit (P0 boundary)

**Objective:** users can perform only authorized work, with reliable evidence of consequential actions.
**Existing:** route capabilities, role/scope policies, field permissions, maker-checker,
session controls, signed audit and security baseline; Stage 134 owns approved hardening.
**Work:** one role/entity/location/owner/self/field capability map across UI, generic writes,
commands, jobs, imports, print and exports. Challenge stale sessions, ID substitution, approval
threshold splitting, replays, HTML/CSV injection, SSRF, uploads and audit archive/restore.
Review emergency access and support impersonation as explicit controlled capabilities.
**Accept:** two-tenant/role adversarial tests fail closed, including background tasks after
revocation; no economic action self-approves through an alternate path. Audit integrity survives
archive/restore and key rotation. Stage 134 partial progress must not be called full ASVS coverage.

### M30 — SaaS control plane, deployment, recovery and support (P1)

**Objective:** run several clients with predictable isolation, reliability and support cost.
**Existing:** package/entitlement catalog, tenant lifecycle, control panel, release scripts,
backup tooling, health and operational alerts. Stage 134.11 proposes separate Railway staging.
**Work:** settle BLD-023's dependency/support contract; verify provision/upgrade/downgrade,
disable/re-enable, trial/suspension, legal-hold/offboarding and retained exports. Define metering
and subscription semantics before billing. Inventory DB/media/config/secrets recovery together;
test safe migrations, version skew, rollback/forward recovery and tenant fairness.
**Accept:** provision two selected packages on clean tenants, deny unentitled UI/API/jobs,
upgrade one under load, restore it under authorization and prove no residue/cross-tenant access.
An outage alert reaches a named responder. Declare measured RPO/RTO and restore evidence;
backup-file presence alone is insufficient. New commercial/billing policy remains a decision.

## 5. Stability and engineering work across all modules

### E01 — Economic command audit

Inventory every operation that changes quantity, value, custody, status or external state.
For each capture actor/scope, input/version preconditions, lock order, transaction boundary,
idempotency key, event/outbox and rollback/reversal semantics. Trace UI, generic edit, bulk,
API and worker entry points to that command. Prioritize sale/refund, receipt, transfer,
material issue, gift/store value and posting. Prove conservation and one business effect
under concurrency, timeout-after-commit and repeated delivery. Event delivery may repeat;
the business effect must deduplicate durably.

### E02 — Data types and historical correctness

Audit money and quantity representation at actual boundaries before proposing migration.
Use tested decimal/minor-unit policy for money and defined UOM precision; do not blindly
convert every existing numeric column or assume all currencies have two decimals. Test tax
rounding, split/partial allocations and proportional reversals. Separate UTC instants from
business dates, local scheduling and accounting cutoffs. Preserve effective policy/version
snapshots and revision evidence. Add constraints/indexes only after measuring/data profiling;
keep schema changes additive and backward compatible.

### E03 — Jobs and durable recovery

Reuse Stage 134.7's PostgreSQL job runner; specify lease/heartbeat, fencing or equivalent
stale-worker protection, retry classification, bounded backoff and poison-job handling.
Prove two instances, process death, lease expiry, disable/re-enable and cancellation.
Migrate existing workers incrementally with observable ownership; an additional queue service
is not justified without measured need. Treat provider uncertainty with inquiry/reconciliation,
not infinite retry of a consequential request.

### E04 — Performance, fairness and bounded resources

Record the current budgets and baselines per actual build; the old 10-byte overrun is historical,
not a finding about this tree. Preserve current release gates rather than raising them.
Measure cold/warm navigation, input/interaction latency, long sessions, large tables, scans,
imports/exports, query plans, lock waits, connection pool, worker memory and idle cost.
Use small/typical/large fixtures and one dominant tenant alongside a small tenant. Set
concurrency and admission limits from evidence. Keep new screen code lazy; add indexes at
proven query boundaries. Cache keys must include relevant tenant, scope, version and locale;
revocation/config changes must invalidate the right results.

### E05 — Code structure and developer workflow

Prefer a modular monolith with explicit domain commands, shared policies and narrow interfaces.
Refactor high-risk shared seams when a defect or repeated change justifies it. No full rewrite,
frontend framework, broker or additional dependency is proposed. Give new modules a repeatable
pattern: schema/validation → command/scope → UI/task flow → meaningful tests → help → evidence.
Keep test fixtures isolated per session, migrations reproducible and source/SQL test coverage
distinct from UI/provider/device evidence. Review ownership before editing shared files.

### E06 — Security and privacy program

Map applicable ASVS controls to code, tests and owners; prioritize boundary exploits and
economic abuse, not scanner count. Define supported threat models for tenant admin, cashier,
integration credential, supplier user, compromised browser and support operator. Include
upload/media/download paths, egress, backups/secrets and mobile caches. Retest remediation
with authorization and isolated data. Maintain retention, subject access/deletion and legal
hold behavior across operational tables, exports, caches, logs and backups. Commission
independent assessment when the selected release and scope are ready.

## 6. Operate one connected organization to verify the result

Use a disposable organization with head office, distribution center, two stores, a small
workshop, two suppliers, a direct customer and a channel. Distinct users act as requester,
buyer, receiver, picker, supervisor, cashier, merchandiser, order desk, HR, finance and admin.
An Account Head and CA review their respective evidence separately. Begin setup through the
UI without invisible SQL seed shortcuts; API/DB reads may corroborate persisted effects.

| Episode | Activity | Required reconciliation |
|---|---|---|
| Setup | Create organization, roles, stores/bins, masters, series and opening balances | Saved config, unique numbers, role scope and an effective policy snapshot |
| Buy | Request/compare/order 100 RM; receive 80, accept 75, reject 5 | 75 usable units; remaining/replacement per approved policy; accepted bill and AP |
| Make | Produce 10 FG consuming 30 RM, with a separate scrap/rework case | RM becomes 45 before other explicit events; WIP/output/cost reconcile |
| Move | Dispatch/receive 6 FG to Store A; separate partial/short/damaged transfer | DC 4, Store A 6 on the normal path; exception quantities remain explainable |
| Sell | Sell 1 FG at Store A, take payment, close till | Store A 5; sale, tenders, tax and GL agree |
| Fulfill | Channel order 2 FG from DC, reserve/pick/pack/ship/invoice | DC 2 after shipment; one order/issue/invoice despite replay |
| Recover | Return one sold item with approved QC/refund; repeat a lost response | Restock follows disposition; refund/tax/value ledgers reconcile once |
| Enrich | Variant/media/import/approval/channel publish and one rejection | Stable SKU; permitted edits; local and remote publication outcomes distinct |
| People/service | Leave, payroll, expense, asset task and order-linked service case | Private scoped data, approvals, liabilities, custody and service history |
| Close | Bank match, AP/AR/stock/value reconciliation, trial balance and lock | Owned exceptions, controlled correction, signed operational/finance review |
| Restore | Kill a worker, simulate provider outage, restore backup on separate infrastructure | Durable resumed work, reconciled effects, measured data loss and recovery time |

For each episode capture role, release/schema/config, screen path, input, source/target
numbers, before/after quantities and values, persisted readback, errors and recovery action.
Repeat normal, partial, denied, repeated, interrupted and concurrent variants. Use one
connected dataset so a broken handoff cannot be hidden by a fresh unrelated fixture.

## 7. Execution sequence and deliverables

These phases are proposed sequencing for this plan, not new parallel implementation trackers.
Reuse the existing item when it owns the work; add a small scoped item only for a proven gap
with no owner. Earlier accepted product decisions remain binding unless explicitly reversed.

| Phase | Outcome / deliverable | Dependencies and existing owners | Exit bar |
|---|---|---|---|
| P01 | Trustworthy current capability baseline and page/action inventory | Stage 135 plan; 84.8, 134.9, capability registry | Every routed/lazy/mobile/config page assigned to M01–30 and a role; stale claims reconciled; residual uninspected actions explicit |
| P02 | Critical command and authorization controls | P01; 47/49, 134.2–.5/.8, Stage 130 guards | All consequential entry paths mapped; reproduced release-blocking integrity/security defects fixed and retested |
| P03 | Organization and master setup that a new admin can complete | P01–02; 91, 125, 128, 131 | Clean-tenant setup and dependency recovery pass using current guides |
| P04 | Buying through accepted stock and payable | P02–03; 55.4, 94, 126, 129, 130 | Partial/rejected/retry procurement journey reconciles through AP |
| P05 | Inventory and executable warehouse work | P02–04; 55.6/.7/.8, 80, 106, 130 | Counts, holds, inbound/outbound, lot/serial and transfer exceptions reconcile; target device evidence |
| P06 | Sale/order through delivery, return and settlement | P02–05; 55.5/.9, 72/73, 88, 99, 101/102 | POS and channel journeys reconcile under cancellation, retry and provider uncertainty |
| P07 | Governed product-to-channel lifecycle | P03 + P06 connector boundaries; 55.11, 81, 95 | Manual/import/supplier paths, roles, media, effective pricing and publish failure recovery pass |
| P08 | Production, job work, quality and maintenance depth | P04–05; 55.12/.13, 78, 130 | Material/WIP/output/custody and cost lineage pass with partial/rework paths |
| P09 | People, assets, expenses, CRM and service integration | P02–03; 55.14–.17, 73, 101/102 | Each operational lifecycle reconciles to its financial/privacy boundary |
| P10 | Accountable financial and tax close | Posting contracts start P02; final gate after P04–09; 55.10, 74/75 | Subledgers, stock/value and GL tie; Account Head/CA findings and approvals recorded |
| P11 | Measured SaaS reliability, capacity and lifecycle | Start P02, finish with selected packages; BLD-023–030, 045–055, 134.6/.7/.11 | Package lifecycle, two-instance recovery, restore, tenant fairness and existing release budgets pass |
| P12 | Independent customer pilot and supportable release | All included paths; 55.22, release acceptance | New users complete tasks, named owners accept, actual providers/devices verified, runbooks and rollback ready |

Security, accessibility, documentation and resource limits apply in every phase, not just
their final acceptance phase. Estimate effort only after P01 identifies actual remaining
work; dated older proposals are not a reliable estimate for the current tree.

## 8. Scorecard, acceptance evidence and product decisions

### Measures

| Measure | Gate / proposed target | Evidence |
|---|---|---|
| Unauthorized or cross-tenant consequential actions | Zero successful attempts in the declared adversarial matrix | Versioned role/scope negative results |
| Duplicate economic effects / unexplained conservation differences | Zero in accepted journeys | Independent stock/value/posting reconciliation |
| Critical user journeys | All selected supported normal/exception/recovery cases pass | Role-operated UI results, readback and controls |
| New-user unassisted success | Proposed pilot target ≥90% of sampled routine tasks; report sample size and assists | Observed real users; establish baseline before claiming improvement |
| Accessibility | Relevant WCAG 2.2 AA criteria verified for included journeys | Automated plus manual/assistive findings |
| Browser/server performance | Existing NFR/release limits; publish distributions and outliers | Three reproducible contexts with build/fixture/device details |
| Recovery | Meet approved per-package RPO/RTO; no invented SLA | Restore and worker/provider interruption drills |
| Support burden | Trend task failures and tickets per active tenant/transaction | Defined denominators, severity and time window |
| Cost/fairness | Existing artifact gates plus measured DB/log/media/backup/tenant load | Resource reports and dominant-tenant comparison |
| Documentation/SOP health | Each included workflow has matching instructions and current segments | Literal guide walk, link/generation checks and revision map |

The 90% pilot target is proposed, not existing evidence or an approved commercial promise.
Absolute "no bugs" is not a defensible release claim; use severity, exposure, observed
failure rate and verified recovery. Reproduced critical defects block their affected scope.

### Completion record for each work package

Record: objective and owner; current behavior and evidence label; linked existing item;
user story and state transitions; rules/permissions; exact implementation delta; migrations
and rollback/forward recovery; normal/exception/concurrency tests; live role/UI and persisted
results; device/provider acceptance; reconciliation; guide/SOP changes; resource/security
impact; remaining decisions; release and reviewer. Checkbox completion requires the relevant
exit bar, rather than source existence or one happy-path screen load.

### Decisions needing product leadership or qualified ownership

- **Supported products and dependency/support contract:** settle BLD-023 with the existing
  package graph, supported customer configurations, required shared services and disable rules.
- **Business-policy gaps:** first locate later answers in the live tracker. Only genuinely
  unresolved quote/award, tolerances, partial close, costing, refund and retention rules need
  another decision; do not reopen Stage 130/134 approvals by default.
- **Market expansion:** multi-owner 3PL, cross-border/statutory packs, regulated pharma,
  advanced APS, full manufacturing costing, subscription billing and AI remain distinct
  expansion candidates requiring customers, economics and acceptance scope. Existing
  single-owner constraints and the standing AI exclusion remain until explicitly changed.
- **External acceptance:** credentials, real devices, Account Head/CA review and independent
  security assessment require the appropriate participants. Complete local preparation first.

## 9. Evidence map and research basis

Local starting points (re-read at implementation; the shared tree continues to change):

- [Architecture and boundaries](../architecture/current-architecture.md),
  [vision](vision.md), [capability catalog](../generated/capability-catalog.md),
  [package definitions](../../engines/modules.go).
- [Existing execution backlog](../micro_checklist.md),
  [BLD acceptance/dependency plan](erp-build-checklist.md),
  [functional organization scenario](erp-functional-verification-plan-2026-10-04.md),
  [earlier module usability plan](erp-module-usability-plan-2026-10-04.md).
- [WMS current setup/handoffs](../kb/module-handbooks/wms-start-here.md),
  [PIM handbook](../kb/module-handbooks/pim-pxm.md),
  [mastering procure-to-pay](../kb/module-handbooks/procurement.md),
  [returns UI](../../public/view-returns.js), [journals UI](../../public/view-journal.js).
- [Source transaction controls](../../engines/transaction_controls.go),
  [route capability boundary](../../internal/server/route_capabilities.go),
  [scope policy](../../engines/scope_policy.go),
  [publish worker](../../engines/pim_publish.go),
  [release acceptance](../governance/release-acceptance.md).

External references checked 2026-10-23; benchmarks inform the proposal, not certification:

- [OWASP ASVS](https://asvs.owasp.org/) supplies testable application-security requirements.
  Map selected controls to evidence rather than treating a scanner pass as comprehensive coverage.
- [WCAG 2.2](https://www.w3.org/TR/WCAG22/) supplies accessibility criteria for the manual and
  automated acceptance matrix. A visual review alone cannot establish conformance.
- [GS1 EPCIS](https://www.gs1.org/standards/epcis) informs interoperable traceability events.
  Use it when trading-partner scope requires it; a full EPCIS service is not automatically needed.
- [Microsoft invoice matching](https://learn.microsoft.com/en-us/dynamics365/finance/accounts-payable/accounts-payable-invoice-matching)
  illustrates price/quantity/receipt comparison and configurable discrepancy tolerances.
  It is a workflow benchmark, not accounting law for this product's customers.
- [Microsoft warehouse overview](https://learn.microsoft.com/en-us/dynamics365/supply-chain/warehousing/warehouse-management-overview)
  distinguishes directed warehouse execution from simpler inventory management.
- [Microsoft product information overview](https://learn.microsoft.com/en-us/dynamics365/supply-chain/pim/product-information)
  informs shared product definitions, variants, identifiers and distribution of product information.
- [AWS SaaS Lens](https://docs.aws.amazon.com/pdfs/wellarchitected/latest/saas-lens/wellarchitected-saas-lens.pdf)
  informs tenant isolation, tenant-aware operations and resource fairness. These principles
  do not prescribe a move from the existing hosting or monolith.

## 10. Review completion and next concrete work

Stage 135.1 completes the requested source-based outsider review and detailed plan only.
All M01–30 acceptance work remains proposed or under its existing build owner; this document
does not close any module or supersede the capability registry. Next execute P01: produce a
current route/page/action-to-role inventory, reconcile the concrete stale handbook/catalog
claims, and select the first connected organization journey for release-specific verification.
Stage 134 work already in progress should retain its owner and approved scope.
