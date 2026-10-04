---
doc_id: DOC-PRODUCT-MODULE-USABILITY-20261004
title: Every-module usability audit and phased improvement plan
type: reference
status: draft
owner: engineering-owner
approvers: [product-owner, qa-owner]
audience: [product, design, engineering, QA, implementation]
applies_to: source release 0.1.0; shared uncommitted tree reviewed on 2026-10-04
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-04
review_by: 2026-11-04
supersedes: none
superseded_by: none
verification_scope: browser inspection and implementation planning, not business acceptance or certification
format: work-register
---

# Every-module usability plan — 4 October 2026

## Verdict and scope

**Not consistently industry-practice-ready or independently usable yet.** There are useful
foundations: role Home, shared record lists, automatic series, master lookups, setup hints,
document-linked receipts, contextual Help, approval controls and scan-oriented RF tasks.
Retain these. The main weaknesses are inconsistent task guidance, technical-ID entry,
overloaded workbenches and real navigation/form defects, not a lack of modules or a need
for a new frontend framework.

This began as an **audit and plan**, not an assertion that proposed improvements had shipped.
The subsequent “continue building” authorizes Phase 55.1 implementation and disposable-data tests;
the remaining phases are planned, not implicitly accepted or complete. Business policy and access
controls are not relaxed. No commit or deployment.
Existing Stage 50 BLD/JRN gates remain authoritative; Stage 55 organizes the usability work
without declaring earlier security, performance or business acceptance complete.

“Anyone can use it” means an **authorized person can complete their routine job without a
colleague explaining the software**. Payroll configuration, accounting judgments, regulated
quality decisions and administrator operations still require qualified, authorized owners.
Help must not substitute for safe defaults, and simplicity must not bypass permissions.

## Evidence and three perspectives

The [screen register](../assurance/erp-usability-screen-register-2026-10-04.md) enumerates
every discovered screen and record type, its observation and its implementation phase.
Machine evidence is [the sanitized browser inventory](../assurance/erp-usability-observations-2026-10-04.json).
Those artifacts carry final coverage counts, failures, reruns and source-snapshot qualifications.

One reviewer used three structured lenses, **not three independent human testers**:

| Lens | Question applied to every screen | Required result |
|---|---|---|
| First-time authorized user | What is this for, what do I need, what do I do next? | Plain purpose, visible prerequisites, sensible defaults, searchable names, example and next action |
| Daily operator | Can I finish accurately with little typing, interruptions and a scanner/keyboard? | Task queue, source-document prefill, preserved context, safe retries, efficient lines and exceptions |
| Supervisor/administrator | Who may do this, what changes, how do I diagnose or reverse a mistake? | Scope and status, approval/audit chain, bounded correction, configuration owner, actionable errors |

Chromium inspection uses the disposable `custom_erp_t1` fixture via loopback port 8101,
desktop 1440×1000 and phone 390×844. Actual navigation, tab clicks and New-dialog clicks were
used, not screenshot-only opinions. Blank native validity, Tab entry and Escape/focus return
were probed on generic forms. Non-read API requests were blocked: no sale, receipt, payment,
approval, configuration save or inventory movement was posted. Background server jobs are
outside that browser guard. Screens with missing fixture data were inspected as empty states.

This is not exhaustive testing of every conditional detail/modal, role, dark theme, device,
business cycle or accessibility criterion. Desktop screenshots were visually reviewed for
the bespoke routes; forms/panels also have DOM/field inspection and representative visual
review. The register distinguishes **inspected** from **accepted**. It must not be described
as all 199 record workflows or every nested feature having passed end-to-end.

## External reference practices, not a certification claim

- Organize around outcomes and linked end-to-end processes rather than database tables:
  [Microsoft business process catalog](https://learn.microsoft.com/en-us/dynamics365/guidance/business-processes/overview).
- Build on role home, assisted setup and page-context help:
  [Business Central getting ready](https://learn.microsoft.com/en-us/dynamics365/business-central/ui-get-ready-business).
- Distinguish first-use emptiness, a filter with no matches, unavailable data and access restrictions;
  give a useful next step: [SAP Fiori empty states](https://www.sap.com/design-system/fiori-design-web/v1-96/foundations/best-practices/global-patterns/designing-for-empty-states).
- Source-linked receiving and invoicing should inherit valid context and expose remaining quantities:
  [ERPNext Purchase Receipt](https://docs.frappe.io/erpnext/purchase-receipt) and
  [Purchase Invoice](https://docs.frappe.io/erpnext/purchase-invoice).
- Barcode operation is a distinct task interaction, not just a smaller desktop form:
  [Odoo Barcode](https://www.odoo.com/documentation/19.0/applications/inventory_and_mrp/barcode.html).
- Structured BOMs and production work link components, output and execution:
  [ERPNext BOM](https://docs.frappe.io/erpnext/bill-of-materials) and
  [Work Order](https://docs.frappe.io/erpnext/work-order).
- Quality decisions belong to their incoming, outgoing or production source:
  [ERPNext Quality Inspection](https://docs.frappe.io/erpnext/quality-inspection).
- Employee and manager work should have distinct entry points:
  [Microsoft manager self service](https://learn.microsoft.com/en-us/dynamics365/human-resources/mss-overview).
- Labels, instructions and identifiable errors are testable accessibility requirements:
  [WCAG labels/instructions](https://www.w3.org/WAI/WCAG22/Understanding/labels-or-instructions),
  [error identification](https://www.w3.org/WAI/WCAG22/Understanding/error-identification.html),
  [target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).
  The 24-CSS-pixel criterion has spacing and other exceptions; a small-control flag alone
  does not prove a violation. The proposed 44px RF control target is our product choice.

Recommendations below are design judgments informed by these references and the observed app.
They are not an obligation to copy another vendor, add its whole feature set, or claim statutory compliance.

## First fixes, before a redesign

| ID / priority | Observed problem | Narrow change and closure proof |
|---|---|---|
| UX-001 / P0 correctness | Generic New clicks produce an Edit title. `#doc-create-button` passes the click event to `openDynamicModal(existingRecord)`; `!!existingRecord` selects edit mode. The Item screenshot also shows Type prefilled with `click`, the event type. | Wrap the create handler and validate record identity explicitly. Test actual New and Edit clicks, defaults, number preview, create-vs-update request and cancel for every shared field type. No incorrect-save outcome is claimed from this read-only audit. |
| UX-002 / P0 navigation | Five HR and two Manufacturing tabs call `renderDocTableView` before its lazy module is loaded; initial navigation order can mask this. | Declare/await the shared module dependency without bypassing route permissions. Cold-enter each tab, navigate away/back, fail/retry the chunk, and verify Help plus applicable print. Keep BLD-041 open until its full Done bar passes. |
| UX-003 / P0 input correctness | Mobile Picking's Wave ID displays phone guidance; real typing of `WAVE-0001` retains only `0001` and uses `inputmode=tel`, despite available semantic metadata intended to prevent this. | Trace every hint/phone decorator, not just the main format matcher. Wave/lot/serial/barcode examples must survive real keyboard and scanner input unchanged; phone fields must retain their own validation. See targeted evidence in the register. |
| UX-004 / P1 discoverability | WMS asks for Line IDs, Task IDs, Wave IDs, or comma-separated IDs; Manufacturing asks for `sku:qty` component syntax. | Assigned-work queues, source links, searchable/scannable selectors and structured line editors. Keep code scanning as a fast path, not a prerequisite for knowing internal IDs. |
| UX-005 / P1 accessibility | Named candidates include OMS search/line inputs, PO line inputs, inventory search, report selectors and the appointment date. | Verify accessible names in the accessibility tree, associate label/error/help, keyboard-operate full forms. Do not treat placeholder text as the only label. |
| UX-006 / P1 guidance | Several empty screens name missing setup or another workflow but provide no direct next action; approvals show IDs without obvious review context. | Reuse setup-hint components with permission-aware links; give approvals a document review surface before deciding. |
| UX-007 / P2 hierarchy | PIM exposes 21 peer tabs; some workbenches mix administration, daily entry, history and exceptions. | Group by task and role with progressive disclosure, while preserving deep links and power-user access. |
| UX-008 / P1 keyboard and reflow | Fresh Department New leaves focus on the underlying New button; first Tab goes to the background table search before entering the dialog. Escape and focus return work. At 390px PIM tab text visibly overlaps, although the document-overflow heuristic reports false. | Move focus into opened dialogs and contain keyboard interaction appropriately; keep Escape/return behavior. Reflow PIM navigation with readable non-overlapping labels and reachable active tab. Inspect pixels as well as overflow counters. |
| UX-009 / P1 setup recovery | PasswordResetRequest's User reference fetches `/api/v1/doc/User`, which returns 404 in this fixture. | Use the supported scoped identity lookup/entry path; test a named user and access-denied case. Do not create a second User doctype to silence a wrong lookup. |

## Shared design contract

Every phase below inherits these requirements; implement once at the narrowest safe shared
boundary, then verify every consumer. Reuse vanilla JavaScript, shared typeahead, numbering,
form/dialog, error and Help infrastructure; no new UI framework or runtime service is proposed.

1. **Task-led entry:** clear business title/purpose; one primary next action; location, legal entity,
   owner and date context where relevant. Daily work, master setup and advanced administration
   are separate. Deep links, active navigation and breadcrumbs must agree.
2. **Friendly references:** display name plus stable code; lookup/scanning, keyboard selection,
   permitted quick-create and return-to-task. Never silently create sensitive masters such as
   vendors, accounts, tax codes or employees. Preserve the already-agreed requisition-description
   auto-master behavior with deduplication; other quick-create flows require explicit save.
3. **Forms:** business order and groups, correct data types/units, defaults from source and role,
   generated numbers read-only, optional/advanced sections collapsed, retained input on error,
   field-level errors and a summary. Existing-record edit and new-record creation are distinct.
4. **Record lists/details:** readable columns, server search/pagination, saved filters where useful,
   list-to-detail and related documents, clear selected scope for bulk actions. Separate system
   outputs/logs from manually maintained masters; decide permissions per type, not by broad UI hiding.
5. **Truthful actions:** document-specific lifecycle, clear consequences and approval requirements;
   preview affected totals/stock before irreversible steps, real saved/posting result, safe retry,
   no duplicate submission and auditable reversal. Do not force every master into a transaction lifecycle.
6. **No dead ends:** distinguish loading, no data, no matches, missing setup, permission denial and
   failure. Provide a valid next action or name the responsible role. Preserve entered data and
   filter/scroll context. Respect BLD-036's accepted section-refresh residual rather than silently reopening it.
7. **Access and resilience:** keyboard, visible focus, modal focus return, accessible names,
   inline error association, screen-reader announcements, contrast, responsive layouts and scan targets.
   Tenant/field/role gates remain server-enforced. Test dark mode and 200% zoom during build acceptance.
8. **Self-service support:** contextual short steps, terminology/examples, outcome confirmation,
   correlation/reference number and relevant Help. Prefer fixing the screen over adding a long manual.

## Complete module ownership map

The live metadata contains 20 entitlement keys. Counts are record types, **not a claim that
each is an independent module or a complete process**. WMS functionality is mostly owned by
`inventory`; Customer is owned by `sales`, Project by `finance`, and QualityInspection by
`manufacturing`. Preserve these boundaries unless a separately approved architecture change says otherwise.

| Entitlement key | Types | Primary phases | Assessment and direction |
|---|---:|---|---|
| core | 5 | 55.2–3, 55.21 | Useful shared setup; make dependency/role guidance explicit |
| master_data | 8 | 55.3, 55.6 | Searchable consistent masters; industry-specific labels need clear context |
| procurement | 7 | 55.4 | Existing PO/GRN journey; simplify source inheritance and exception handling |
| rfq | 2 | 55.4 | RFQ/quotes present; guide comparison and approved conversion |
| sales | 14 | 55.5 | Strong recent POS improvements; preserve them, finish role/task and recovery acceptance |
| inventory | 50 | 55.6–8 | Broad depth, several expert-ID/operator usability gaps |
| wms | 1 | 55.7–8 | Floor assistance is one record owner, not the full warehouse feature boundary |
| oms | 21 | 55.9 | Unified workbench exists; exception-led work and connector context need simplification |
| finance | 19 | 55.10, 55.18 | Core accounting surfaces exist; source-driven AP/AR/close and review context |
| pim | 27 | 55.11 | Extensive capability; 21-tab navigation and onboarding burden |
| manufacturing | 6 | 55.12 | Existing production features; cold-tab defect and technical component entry |
| quality | 5 | 55.13 | Generic records need linked inspection/nonconformance/maintenance journeys |
| hr | 17 | 55.14 | Broad HR tabs; lazy dependency defect and mixed employee/admin workflows |
| expenses | 1 | 55.15 | Existing claim entry; source receipts, status tracking and reimbursement clarity |
| assets | 1 | 55.16 | Existing asset form; lifecycle and accounting context should be visible |
| crm_loyalty | 3 | 55.17 | Campaign/voucher/redemption records; show customer task and economic outcome |
| service | 2 | 55.18 | Generic contracts/tickets; task-led linked case handling |
| stickers | 2 | 55.19 | Source-based printing exists; separate template/printer setup from routine printing |
| reports | 7 | 55.20 | Existing catalog/dashboard; clearer filters, drill-through and job outcomes |
| integrations | 1 | 55.21 | Existing integration/admin screens; diagnosis and safe retry, not blind resubmission |

## Phase-by-phase microchecklist

Build items below are **open unless explicitly verified and checked**. Each phase includes all of its subitems when scheduled.
Dependencies: 55.1 before broad acceptance; 55.2 before repeating UI patterns; 55.3 before
new-user business journeys. Domain work follows the order below; 55.22 is the cross-module
release gate. Owners are roles to assign, not claims of external owner approval.

### 55.1 — Shared correctness (frontend owner + QA; BLD-035/038/041)

- [ ] Fix UX-001 using actual click-path regression tests, not a direct helper call that misses the event argument.
- [ ] Fix UX-002 and test every lazy screen's secondary tabs from a fresh browser context.
- [ ] Resolve UX-003 with field semantics across all hint/validation hooks; preserve exact scanned characters.
- [ ] Repair UX-008's shared initial-dialog focus and UX-009's identity reference; keep the layout part of UX-008 in 55.2/55.11.
- [ ] Triage any failed/retried register checks against a fresh source snapshot; separate harness timing from application errors.
- [ ] **Done:** no incorrect New/Edit mode; cold HR/Manufacturing tabs render; scan semantics pass; relevant full-suite results and UI evidence recorded. No unrelated checked BLD item is silently reopened or closed.

### 55.2 — Shared UX shell, lists, forms and Help (frontend/design + accessibility QA)

- [ ] Apply the shared contract; verify active sidebar on click, deep link and Back; keep hover and keyboard menu access.
- [ ] Close confirmed accessible-name gaps, modal focus failures and error-recovery gaps across the register.
- [ ] Add consistent task headers, list/detail context, empty-state action and status/action vocabulary.
- [ ] Keep setup/search discoverable; test menus above sticky tables, focus visibility and small-screen overflow.
- [ ] **Done:** representative master, transaction, workbench and RF screens pass keyboard/screen-reader/phone tests; every register row has a tested shared-pattern path. No shared library/framework addition.

### 55.3 — Core, masters and industry setup (implementation/data owner)

- [ ] Guided prerequisites: Legal Entity → Location/Department/Cost Center → units/masters → numbering → role access; resume checklist without deleting work.
- [ ] Make Item/Vendor/Customer/reference selection searchable by name/code; consistent permitted quick-create and duplicate detection.
- [ ] Group Item fields by basic, inventory, commercial and industry sections; preserve Jewellery Design ID/Combination ID and industry-lock override safeguards.
- [ ] Show Prefix Configuration preview and '+ New Series' from permitted task context; automatic numbers remain server-owned.
- [ ] Cover Batch/Serial/UOMConversion/Brand/Style/Color and every core/master_data register row, including derived fields and history.
- [ ] **Done:** new authorized operator sets up only needed prerequisites then creates the first valid document without memorized IDs; numbering/concurrent-save and industry lock tests remain green.

### 55.4 — RFQ and procurement (buyer + receiving + AP)

- [ ] One guided chain: requisition → RFQ/quotes → PO → ASN/GRN → vendor invoice; show upstream and downstream records and next eligible action.
- [ ] Requisition defaults department and number; description hints/dedup preserve existing behavior. Quote comparison shows comparable units, tax, delivery and total, without inventing award policy.
- [ ] PO line editor: labelled item/quantity/UOM/rate/tax, vendor and ship-to defaults, useful totals and approval result.
- [ ] GRN loads remaining ordered quantities; handles partial, excess, rejected/damaged quantities and reasons explicitly; hides weight/dimension detail unless needed.
- [ ] Explain three-way-match differences and route an override for authorized approval; never present unmatched invoices as payable without existing policy checks.
- [ ] **Done:** normal and partial/rejected procurement journeys reconcile PO/stock/AP totals in disposable data; user can find where a blocked document needs attention and return to it after setup.

### 55.5 — Sales, POS and returns (cashier + supervisor)

- [ ] Preserve Stage 53 store binding, search/customer quick-add, tender/change, frozen outcomes and truthful receipt totals.
- [ ] Guide open till → sell → payment/approval/offline outcome → next sale → reconcile/close; make blocked stock/price conditions actionable.
- [ ] Find original sale for return, select eligible quantities, explain refund method/approval and route correctly; Returns empty state links to the entry task.
- [ ] Separate pricing/offer/recurring-contract administration and POS exception/gap records from cashier work.
- [ ] **Done:** cashier completes sale, held/approved sale, partial return and session close without outside instructions; duplicate/offline recovery, permission and applicable receipt paths pass. Card-terminal/hardware acceptance remains separately gated.

### 55.6 — Inventory and stock transfer (stock controller)

- [ ] Inventory rows show human item/location identity plus on-hand/reserved/available/held explanation; drill to movements and source.
- [ ] Source-driven transfers with valid From/To, available stock, batch/serial/UOM and dispatch/receive state; source sticker actions stay reachable.
- [ ] Count tasks show scoped bin/item work; hide expected quantity for blind count, select reasons by name, make recount/approval references automatic.
- [ ] Holds/condition changes explain stock impact and eligible release approval; master lookup replaces free-ID guessing.
- [ ] **Done:** transfer, blind count/recount and hold/release reconcile stock with source/audit; no unauthorized adjustments, no lost batch/serial identity.

### 55.7 — WMS inbound, yard and floor work (warehouse operator/supervisor)

- [ ] My work/warehouse scope links appointments → yard → ASN receiving → putaway/LPN; choose or scan task instead of memorizing IDs.
- [ ] Missing dock/zone/bin setup has permitted deep links and return context; door/trailer/location choices show readable occupancy and state.
- [ ] RF receiving and putaway use one next scan/quantity decision with visible success/failure and manual fallback.
- [ ] Keep cross-dock and advanced routing available as explicit variants, not three equally prominent blank forms for a novice.
- [ ] **Done:** assigned inbound task to final bin works by keyboard and phone-size scanner simulation; wrong location/lot/duplicate scan and assist-request routes are safe. Physical RF acceptance remains open until tested.

### 55.8 — WMS outbound, labour and billing (warehouse supervisor + billing)

- [ ] Eligible task selection creates/reuses a wave; guided mobile pick → sort/pack → load → depart, with selected counts and remaining work.
- [ ] Replace comma-separated Task IDs and repeated Wave IDs with scoped selections; Loading opens a known task with dock/trailer/manifest context.
- [ ] Labour operation/element/standard/allowance and travel/task records get a setup wizard, units, effective context and a worked preview using existing engine semantics.
- [ ] Charge codes/groups/contracts, rates and captured/storage charges show source quantities, calculation explanation and billing-review exceptions; system snapshots are not casual operator-entry forms.
- [ ] **Done:** partial/short pick, loading mismatch and normal dispatch preserve stock/task lineage; local labour/billing examples reconcile existing calculations. Keep single-owner warehouse restriction visible; do not imply multi-owner 3PL support or accepted billing policy.

### 55.9 — OMS, marketplaces and delivery (order desk)

- [ ] Default to actionable queues: unallocated, mapping failure, hold, pick/ship overdue, return/refund; saved views have meaningful labels.
- [ ] One order detail contains source/customer/items/stock allocation/payment/shipment/notifications/invoice and event timeline.
- [ ] Manual lines have labelled item/quantity/price controls; channel ingestion remains SalesOrder-based, not a return to POSCart intake.
- [ ] Guided mapping/hold resolution and retry show expected effect and idempotency; settlement/booking choose real orders rather than comma-separated IDs.
- [ ] **Done:** locally seeded order → allocation → shipping → invoice and exception/refund journeys are legible and reconciled; notification queued vs delivered is truthful. Vendor delivery and external acceptance remain separate.

### 55.10 — Finance and period close (accountant + AP/AR approver)

- [ ] Task home for receivables, payable exceptions, proposals, reconciliation and close; distinguish posted entries from drafts/requests.
- [ ] Source-linked invoices/notes/payment proposals with evidence, due balances, partial settlement and approval context; empty proposals link to eligible invoice work.
- [ ] Reconciliation workbench links statement lines to candidates and explains differences; preserve bank/account/entity scope.
- [ ] Expose journal balance, effective period, currency/rate, cost-center/project and source; guide existing intercompany, landed cost, deferral/prepayment and budget flows.
- [ ] Close checklist shows exceptions and drill-through; backdated requests follow approval, not a free editable status.
- [ ] **Done:** AP/AR/GL/stock-linked examples reconcile including partial/credit cases; accountant signs off actual treatment. UX review alone never certifies GST/TDS/accounting compliance.

### 55.11 — PIM and catalog operations (merchandiser + data steward)

- [ ] Group 21 tabs into Products, My work, Media, Channels/publishing and Advanced setup while preserving all deep links and permissions.
- [ ] Repair the observed phone-width tab overlap; zero document overflow alone is not a usability pass.
- [ ] Guided family/attributes → product/variants → media → channel validation → publish; Jewellery design/combination structure remains explicit.
- [ ] Workbench exposes completeness reasons, editable permitted fields and next correction; preserve existing dashboard/bulk editing instead of proposing duplicate implementations.
- [ ] Import/export mappings use sample preview, error rows, validation and resumable job results; supplier submission review and scheduled runs have clear owners.
- [ ] **Done:** a product reaches local publish-ready state from an import/manual path; validation failure is repairable without technical IDs. No AI Assist work or external channel acceptance inferred.

### 55.12 — Manufacturing (planner + shop-floor operator)

- [ ] Replace `sku:qty` entry with typed component lines and readable BOM/output/UOM; support existing BOM/routing/work-center semantics.
- [ ] Production task flow shows required materials, availability, quantity, scheduling and next legal action; separate planning from floor completion.
- [ ] Quality and subcontracting tabs work cold; source links explain inputs sent, output expected/received and exceptions.
- [ ] **Done:** local normal/short-material/partial-output production scenarios preserve material/output/cost lineage; planner validates current capacity and costing behavior rather than assuming advanced APS exists.

### 55.13 — Quality and maintenance (quality owner + maintenance technician)

- [ ] Link InspectionPlan and QualityInspection to incoming/production/outgoing source; display criteria, readings, pass/fail and authorized disposition.
- [ ] NonConformanceReport and CertificateOfAnalysis expose source/lot, accountable action and approved output; do not claim regulatory certification.
- [ ] MaintenanceSchedule/Order show asset, due work, task checklist, parts/time and closure evidence with existing permissions.
- [ ] **Done:** failed inspection cannot be mistaken for accepted stock; correction/maintenance completion is traceable. New regulated validation depth, if needed, stays an explicit capability decision.

### 55.14 — HR, attendance and payroll (employee + manager + HR)

- [ ] Distinct My requests, team approvals and HR administration; repair all cold tabs first.
- [ ] Employee/shift/attendance/leave forms use relevant person/calendar context; explain balances and conflicts without exposing another employee's private data.
- [ ] Payroll preview explains period, inputs, exceptions and authorized finalize/export; loan/repayment links and salary structures are administrator tasks.
- [ ] Guided onboarding, appraisal cycle, training and grievance journeys; sensitive grievance visibility must be explicit.
- [ ] **Done:** employee submits/tracks a request and manager reviews it from a restricted role; HR runs a local payroll example with no timezone/date drift or unauthorised disclosure. Employment/statutory policy requires owner acceptance.

### 55.15 — Expenses (employee + manager + AP)

- [ ] Default employee/location/date; receipt attachment, category/amount/tax/advance help and readable validation.
- [ ] Show draft → submission → decision → reimbursement links using actual backend states; retained rejected-input correction and receipt access.
- [ ] **Done:** employee submits/corrects/tracks a claim; authorized approver sees evidence and AP sees what is still owed, without duplicate reimbursement.

### 55.16 — Fixed assets (asset custodian + accountant)

- [ ] Human-readable asset identity, permitted numbering policy, custodian/location/category lookup and acquisition source.
- [ ] Lifecycle detail links acquisition, depreciation, transfer/maintenance and retirement capabilities actually supported; show calculation preview before approved posting.
- [ ] **Done:** supported acquisition/depreciation/custody scenario has readable source and balance history; gaps in disposal/revaluation capability are decision backlog, not invented UI-only features.

### 55.17 — CRM, campaigns and loyalty (customer desk + marketing)

- [ ] Customer detail unifies permitted sales/returns/loyalty context; customer remains sales-owned, no duplicate customer master.
- [ ] Campaign/Voucher setup previews eligibility, validity and economic effect; redemption shows usable balance and approval/decline reason.
- [ ] Distinguish notification templates/channel configuration from marketing permission; preserve existing consent/access rules.
- [ ] **Done:** eligible/ineligible redemption and return effects are explainable and reconciled; external campaign delivery is not claimed from local UI. Sales-pipeline expansion remains a product decision.

### 55.18 — Service and project context (service desk + project owner)

- [ ] ServiceTicket list is an assigned queue with customer/contract/context, status, due work and readable history.
- [ ] ServiceContract shows coverage and linked tickets; only promise response commitments already represented by the engine/policy.
- [ ] Project detail links the existing finance cost/revenue documents and shows owner/status/budget context; do not imply a separate full project-management module.
- [ ] **Done:** authorized user logs, assigns, follows and closes an existing supported ticket workflow; project costs can be traced to source. New SLA/billing/timesheet engines require separate approved scope.

### 55.19 — Stickers and document printing (operator + print administrator)

- [ ] Daily flow: choose source → review whole-document/selected lines → template/printer → preview → print/result; setup is a separate path.
- [ ] Explain category template fallback, copies, missing barcode and reprint reason; retain Stage 52 GRN/Transfer Order actions.
- [ ] Per applicable screen verify browser fallback and cancellation/error; separately test QZ/device path, alignment and scanner readability with real hardware.
- [ ] **Done:** all locally applicable print paths have evidence/N/A reason in BLD-041; no duplicate print-history on retry; hardware gates stay open until physically accepted.

### 55.20 — Reports, dashboards and scheduled output (analyst + business owner)

- [ ] Catalog organized by business question with plain description and trusted default date/entity/location filters.
- [ ] Named controls, consistent filter summary, units/timezone/as-of/freshness; preserve filters on drill-through and Back.
- [ ] ReportRunLog/ExportJob/digests/schedules show owner, next run, result, cancellation/error and private delivery context; templates/presets remain reusable.
- [ ] **Done:** report total drills to the same scoped source set; empty/large/failed/export-cancel cases are understandable. Meet existing NFR budgets; this paced audit does not replace BLD-045 performance testing.

### 55.21 — Integrations, administration and operations (tenant admin + support)

- [ ] Guide connection setup/test/status, mapping, redacted log and safe retry; failures explain owner/action and correlation ID without exposing secrets.
- [ ] Missing extension-hook context leads to hook selection/return, not an endless generic retry. Keep developer extension setup out of daily-user navigation.
- [ ] Users/roles use readable scope previews and least privilege; approval rules show amount/role consequences, effective configuration and audit.
- [ ] Configuration categories support keyboard access, search, defaults/units, dirty-state protection and authorized save; industry/entitlement changes disclose impact.
- [ ] System status/backup/usage distinguish real alert, never configured and simulated environment; provide runbook/responder links without weakening safety notices.
- [ ] **Done:** restricted admin performs permitted setup and diagnoses a local failed job with no cross-tenant data; denial/retry/secret-redaction tests pass. External alerts require actual responder receipt, not merely a saved webhook URL.

### 55.22 — Cross-module acceptance and handoff (QA + named business owners)

- [ ] Re-run the complete register against a fixed build: fresh entry, populated/empty/filtered/denied/error states, key nested dialogs and all sub-tabs.
- [ ] Perform real disposable-data end-to-end journeys: source-to-pay, order-to-cash/return, receive-to-ship/count, plan-to-produce, employee-to-pay and record-to-report.
- [ ] Test beginner, daily-operator and supervisor roles separately; no Super Admin-only acceptance. Include desktop, phone, keyboard, assistive technology and applicable scanners/printers.
- [ ] Proposed usability target: five representative participants per priority persona, at least 90% of assigned routine tasks completed unaided; report actual denominators and failures. This is a project target, not a vendor standard or proof that all people can use it.
- [ ] Proposed discovery targets: assigned routine task found within 30 seconds and primary task reachable within three navigation actions; zero incorrect/duplicate posting, lost entered data or permission bypass. Specialized expert decisions are excluded from novice tasks explicitly.
- [ ] Reconcile business results; re-run relevant Go/browser/security/print suites and BLD-045 budgets. Update Help/guides from literal walkthroughs; retain historical evidence.
- [ ] **Done:** per-screen results, participant results and accountable domain sign-offs exist; unresolved defects/residuals are explicit. No checkbox closes solely because screenshots look improved.

## Boundaries and how to execute

Start with **55.1**, then **55.2**. Each phase is its full set of microitems and its Done bar,
not only its first subsection. Before editing, re-read the live checklist and affected code,
because another session is working in the same uncommitted tree. Preserve their changes.

All proposed interface repairs within existing capabilities can be built locally after the user
requests implementation. Product/architecture decisions remain separate: BLD-023 SaaS packaging,
new tax/legal policy, multi-owner 3PL, a new CRM/project suite, new statutory payroll or regulated
quality certification, vendor contracts, notification consent policy and real hardware acceptance.
Do not turn these into silent requirements of a visual cleanup.

AI Assist remains out of scope. The permanently parked grid-paste inline-message item remains
parked. No Redis, broker, major framework migration or blanket permission broadening is proposed.

When a phase is verified, update this register, `docs/micro_checklist.md`, the matching BLD/JRN
entry, ledger and handover §6. Record exact failing checks and accepted residuals; never replace
them with “industry standard” or “all tested” without evidence.
