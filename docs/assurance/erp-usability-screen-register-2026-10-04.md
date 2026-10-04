---
doc_id: DOC-ASSURANCE-USABILITY-SCREENS-20261004
title: ERP screen-by-screen usability observation register
type: record
status: draft
owner: qa-owner
approvers: [product-owner, engineering-owner]
audience: [product, design, engineering, QA]
applies_to: source release 0.1.0; browser audit on 2026-10-04
authority: historical
confidentiality: internal
last_verified: 2026-10-04
review_by: 2026-11-04
supersedes: none
superseded_by: none
---

# Screen-by-screen observation register

Companion to [the phased plan](../product/erp-module-usability-plan-2026-10-04.md).
The [sanitized JSON](erp-usability-observations-2026-10-04.json) contains exact fields, labels,
keyboard probes, response codes, source hashes and artifact filenames. No tokens or record values
are published here. Screenshots/raw outputs remain in the session scratchpad named in the JSON.

## What was actually inspected

- 20 entitlement owners, 199 metadata record types, 56 bespoke/shell routes.
- 53 lazy registry keys: 52 route entries plus the generic records wrapper; the latter was covered by the record-type pass.
- 54 actual tab-panel clicks and 16 configuration-category clicks.
- 198 actual generic New-dialog opens; 198 displayed an Edit title (UX-001).
- Desktop and phone snapshots/DOM inspection; native blank validity and Tab/Escape probes, not form submission.
- One reviewer, three lenses. No participant study, transaction/role/device acceptance or WCAG certification.
- Source files changed during initial-to-final capture: none among app.js and the 18 view modules hashed. Backend was the existing session scratch binary, not rebuilt during this audit.

The initial long pass captured a loading state at Grievance and then hit navigation/locator timing
failures. It was stopped; remaining screens were rerun with render-ready waits and periodic context
refresh. Successful observations below supersede those incomplete captures. Original failure IDs
remain in JSON, not counted as confirmed application failures. The test intentionally paces API
requests; none of its durations are a BLD-045 performance measurement.

The extension-hook-log deep link had no selected hook and returned 404; offer selection/back, but
do not infer a broken populated-hook flow. ReportExportJob's generic route is inspected as denied
when the server returns 403; follow the supported report-job UI, not a permission bypass. Any
User-link lookup 404 is recorded separately for endpoint/schema review. A control under 24px or
with no explicit DOM label is a **candidate** for accessibility testing, not automatic conformance failure.

## Bespoke routes: all three lenses

Every row is inspected, with acceptance **open**. The corresponding phase supplies full changes
and Done bar. Page errors in HR/Manufacturing are cold-order dependent; see targeted retests below.

| Route | Phase | First-time user | Daily operator | Supervisor / administrator |
|---|---|---|---|---|
| home | 55.2 | Keep role task tiles; show missing prerequisites without admin clutter | Resume recent work with scope/state | Explain missing tasks by role/module, not hidden magic |
| pos | 55.5 | Keep store/search/customer guidance and visible sale outcome | Preserve scan/tender/new-sale speed; guide session close/returns | Review approval/offline queues and receipt economics |
| rf-traceability | 55.6 | Retain task cards; explain lot/serial choices | One scan/decision per step; manual fallback | Show source/exception/audit; real-device acceptance remains open |
| returns | 55.5 | Empty state links directly to original-sale lookup | Select eligible lines/refund outcome without duplicate entry | Approval and stock/refund lineage visible |
| finance | 55.10 | Separate ordinary finance tasks from ledger setup | Remember as-of/entity; drill balances to source | Period controls and reconciled totals before close |
| vendor-invoices | 55.10 | Create from receipt/PO with supplier-bill guidance | Exception queue for quantity/price/tax differences | Visible match evidence, approval and payable balance |
| payment-proposals | 55.10 | No-eligible-invoice state links to matched invoice work | Select due invoices and preview total/payment scope | Account/entity/approval review and duplicate prevention |
| bank-reconciliation | 55.10 | Keep existing Manage Bank Accounts setup link | Statement-to-candidate match with difference explanation | Unmatched/partial history and scoped reconciliation proof |
| finance-notes | 55.10 | Choose source invoice and business reason | Source values prefill; show financial/stock effect | Authorized reversal and audit, not arbitrary status edit |
| sales-invoices | 55.10 | Explain source order/delivery and amount due | Draft/post/print/settle from one detail | Tax/GL treatment and applicable print evidence |
| fulfillment | 55.8 | Open assigned work with readable order/location | Pick/pack/ship next legal action and exceptions | Allocation/short-pick cause and custody chain |
| putaway | 55.7 | One receipt-driven task instead of peer blank forms | Suggest bin then confirm scan; advanced crossdock optional | Capacity/hold/owner constraints and exception reason |
| warehouse-cockpit | 55.7 | Default permitted warehouse and explain empty scope | Actionable queues link to floor work | Backlog/priority and warehouse constraints |
| appointment-calendar | 55.7 | Label date; missing dock setup gives direct link | Slot/door selection with arrival context | Capacity/no-show/yard linkage |
| yard-board | 55.7 | Keep setup hints; use meaningful trailer/door identities | Check-in-to-door-to-depart next steps | Occupancy/history and permissions |
| place-hold | 55.6 | Select hold code/item/location by name | Source stock/lot prefill, quantity validation | Explain unavailable stock and authorized release |
| rf-receiving | 55.7 | Choose assigned ASN or scan; inherit location | Quantity/lot/serial receipt steps with clear feedback | Mismatch and duplicate receipt safeguards |
| sortation | 55.8 | Select station and task rather than know IDs | Scan-to-slot/remaining quantity and repairable mismatch | Packing validation and order traceability |
| loading-dock | 55.8 | Choose an existing task/manifest with readable state | Dock/trailer/expected carton context from task | Missing carton/loaded/departed/BOL audit |
| bin-conditions | 55.6 | Explain from/to condition and stock effect | Scan scoped bin/item with eligible qty | Reason/permission and movement trace |
| cycle-count | 55.6 | Choose assigned count instead of Line/Recount IDs | Blind quantity, selected reason, linked recount | Variance/approval/stock reconciliation |
| asn | 55.4 | Choose PO then inherit vendor/location/remaining lines | Carrier/date and expected quantities only re-enter exceptions | Receipt links and partial/overdelivery controls |
| lpn | 55.7 | Explain carton/pallet grouping with scan/search fallback | Add/move contents and show current contents | No mixed illegal stock state; custody history |
| bin-replenishment | 55.8 | Default warehouse and explain why tasks are proposed | Review recommended moves and confirm scan | Rule units/min/max and safe repeat execution |
| wave-picking | 55.8 | Select eligible tasks; no comma-separated IDs | Reusable wave selection and pick progress | Priority/allocation and partial/failure recovery |
| mobile-picking | 55.8 | My assigned waves and readable task context | Wave input must preserve code, not phone-format it | Short-pick/assistance/duplicate scan records |
| marketplace | 55.9 | Separate booking, settlement and setup tasks | Choose orders instead of comma-separated IDs | Settlement reconciliation and shipping outcome lineage |
| oms | 55.9 | Actionable order queues; labelled search/manual lines | Saved filters and one full order detail/timeline | Mapping/hold/retry with authorization and idempotency |
| approvals | 55.2 | Explain requested decision and business consequence | Open source details before approve/reject | Role/amount/changed fields/reason and audit trail |
| purchase-orders | 55.4 | Readable vendor/ship-to and labelled lines | Prefill from approved request/quote; totals and remaining | Approval, price/tax and receipt/invoice lineage |
| grn | 55.4 | Pick PO; show remaining and accepted/rejected outcome | Progressive measurement fields, batch/serial scan and labels | Partial/excess/reject controls and stock/AP evidence |
| reports | 55.20 | Find report by question, not only technical title | Named selectors and preserved filters/drill-through | As-of/units/source scope and export status |
| inventory | 55.6 | Name/code/location and quantity meaning | Search/filter then drill source stock movements | Reserved/held/available reconciliation |
| rfq | 55.4 | Start from demand with description hints | Comparable vendor quote lines and convert eligible result | Award evidence/approval without inventing policy |
| stickers | 55.19 | Separate daily source printing from printer/template setup | Whole source or selected line, preview/copies/result | Category fallback/reprint reason and device verification |
| hr | 55.14 | Employee/team/admin entry points; cold tabs must render | Calendars, balances and pending work in context | Payroll/grievance privacy and actual state transitions |
| assets | 55.16 | Explain asset number/source/custodian/category | Lifecycle detail instead of repeated raw codes | Depreciation/GL source and authorized custody change |
| transfers | 55.6 | Source/destination names and eligible stock | Source-linked dispatch/receipt and labels | In-transit/received quantities and audit |
| expenses | 55.15 | Default employee/location; receipt/category examples | Submit/correct/track without retyping | Advance/tax/reimbursement and evidence review |
| manufacturing | 55.12 | Structured BOM lines, not sku:qty syntax | Material readiness and production next step | Cold quality/subcontracting tabs, cost/material lineage |
| pim | 55.11 | Group 21 tabs by job; guided first product | Completeness repairs, bulk tasks and media context | Family/attribute/channel ownership, import/publish review |
| doctype-builder | 55.21 | Admin-only purpose; guided safe schema changes | Search types/fields; consistent hover and keyboard selection | Permission/data migration impact before edit |
| prefix-configs | 55.3 | Keep preview and New Series; explain generated numbers | Find type/series quickly; preserve concurrent sequence safety | Reset interval/store context and change impact |
| approval-rules | 55.21 | Explain which document/amount needs which role | Readable rule editor and conflict preview | SoD/coverage/changes audit; no arbitrary bypass |
| dynamic-labels | 55.3 | Explain tenant/industry vocabulary versus record identity | Preview affected screens and search canonical/translated names | No confusing duplicate names or hidden reference change |
| extension-hooks | 55.21 | Developer/admin path with prerequisites and examples | Connection/status/redacted log links | Scoped token/URL policy and safe disable/rotation |
| extension-hook-log | 55.21 | Missing hook context should offer hook selection/back | Readable attempt/status/retry context | 404 without selected hook is not proof all hook logs are broken |
| audit-logs | 55.21 | Group audit, error, connector and job purposes | Filter/diff/drill to a correlated source, not raw dumps | Redaction, role boundaries and safe retries |
| configuration | 55.21 | Search/group settings with descriptions/defaults | Keyboard-operable categories and retained dirty state | Units/scope/impact/validation before authorized save |
| system-status | 55.21 | Explain simulated/unconfigured versus real failure | Actionable backup/queue health and runbook links | Accountable responder and actual delivery/restore proof |
| tenant-entitlements | 55.21 | Guided provision and visible plan/module consequences | Scoped tenant selection; no accidental context switch | Preview least privilege and impact, separate packaging decisions |
| tenant-usage | 55.21 | Explain counters, cap and time window | Link saturation to bounded actionable diagnosis | Tenant-safe limits and accountable operator |
| help | 55.2 | Task-language search and contextual short steps | Return to the exact task; keep role journeys | Maintain version/context accuracy without claiming untested steps |
| profile | 55.21 | Plain session/password/MFA explanations | Task-specific save feedback and safe session change | No personal data exposure; reauthentication where required |
| users | 55.21 | Guided role/location selection by name | Search/status and safe reset/deactivate flow | Effective grants and privileged-action audit |
| roles | 55.21 | Explain role versus record-type permissions | Find/filter grants and preview changes | Least privilege/field/tenant checks; do not broaden access for simplicity |

## Every inspected tab / configuration panel

Tab rows inherit the parent route’s three-lens changes; observations below identify their actual
entry state and specific field burden. Empty data is not a feature failure. A rendered panel is
not proof that its save, print, permission or business calculation works.

| Parent / panel | Visible fields | Observed input/control concern | Phase |
|---|---:|---|---|
| finance / Trial Balance | 1 | Rendered; apply parent task/default/source/control contract | 55.10 |
| finance / Chart of Accounts | 0 | Rendered; apply parent task/default/source/control contract | 55.10 |
| finance / Accounting Periods | 3 | Rendered; apply parent task/default/source/control contract | 55.10 |
| reports / Dashboard | 2 | Check accessible names: dashboard-layout-picker, dashboard-add-tile-picker | 55.20 |
| reports / Current Stock | 0 | Rendered; apply parent task/default/source/control contract | 55.20 |
| reports / Sales Register | 0 | Rendered; apply parent task/default/source/control contract | 55.20 |
| reports / Vendor Ledger | 0 | Rendered; apply parent task/default/source/control contract | 55.20 |
| reports / Payables Ageing | 0 | Rendered; apply parent task/default/source/control contract | 55.20 |
| reports / Receivables Ageing | 0 | Rendered; apply parent task/default/source/control contract | 55.20 |
| reports / GST Return Summary | 2 | Rendered; apply parent task/default/source/control contract | 55.20 |
| reports / Report Catalog | 5 | Rendered; apply parent task/default/source/control contract | 55.20 |
| stickers / Print | 7 | Rendered; apply parent task/default/source/control contract | 55.19 |
| stickers / Templates | 0 | Rendered; apply parent task/default/source/control contract | 55.19 |
| hr / Attendance | 5 | Rendered; apply parent task/default/source/control contract | 55.14 |
| hr / Leave | 6 | Rendered; apply parent task/default/source/control contract | 55.14 |
| hr / Payroll Export | 2 | Rendered; apply parent task/default/source/control contract | 55.14 |
| hr / Shift Roster | 0 | renderDocTableView is not defined | 55.14 |
| hr / Payroll | 3 | Rendered; apply parent task/default/source/control contract | 55.14 |
| hr / Loans/Advances | 4 | Rendered; apply parent task/default/source/control contract | 55.14 |
| hr / Onboarding/Offboarding | 0 | renderDocTableView is not defined | 55.14 |
| hr / Appraisals | 0 | renderDocTableView is not defined | 55.14 |
| hr / Training | 0 | renderDocTableView is not defined | 55.14 |
| hr / Grievances | 0 | renderDocTableView is not defined | 55.14 |
| hr / My Requests | 0 | Rendered; apply parent task/default/source/control contract | 55.14 |
| manufacturing / Orders | 7 | Technical input: Components (sku:qty, sku:qty, ...) | 55.12 |
| manufacturing / Quality Inspections | 0 | renderDocTableView is not defined | 55.12 |
| manufacturing / MRP Suggestions | 3 | Rendered; apply parent task/default/source/control contract | 55.12 |
| manufacturing / Production Schedule | 0 | Rendered; apply parent task/default/source/control contract | 55.12 |
| manufacturing / Subcontracting | 0 | renderDocTableView is not defined | 55.12 |
| pim / Dashboard | 0 | Rendered; apply parent task/default/source/control contract | 55.11 |
| pim / Workbench | 1 | Rendered; apply parent task/default/source/control contract | 55.11 |
| pim / Reports | 1 | Rendered; apply parent task/default/source/control contract | 55.11 |
| pim / My Work | 11 | Rendered; apply parent task/default/source/control contract | 55.11 |
| pim / Media Library | 6 | Technical input: Bulk download - item codes (comma-separated) | 55.11 |
| pim / Task Templates | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Workflows | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Product Families | 3 | Check accessible names: doc-table-search | 55.11 |
| pim / Attribute Definitions | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Attribute Groups | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Family Attributes | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Channels | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Category Mapping | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Field Mapping | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Validation Rules | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Supplier Submissions | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Import Templates | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Import Schedules | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Export Templates | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Export Schedules | 2 | Check accessible names: doc-table-search | 55.11 |
| pim / Catalogs | 2 | Check accessible names: doc-table-search | 55.11 |
| audit-logs / Audit Logs | 0 | Rendered; apply parent task/default/source/control contract | 55.21 |
| audit-logs / System Errors | 0 | Rendered; apply parent task/default/source/control contract | 55.21 |
| audit-logs / Integration Payloads | 0 | Rendered; apply parent task/default/source/control contract | 55.21 |
| audit-logs / Async Jobs | 0 | Rendered; apply parent task/default/source/control contract | 55.21 |
| Configuration / Loyalty | 4 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Inventory | 1 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Security | 9 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Sales & Returns | 1 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Procurement | 4 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Point of Sale | 3 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Warehouse | 5 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Manufacturing | 3 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / HR & Payroll | 1 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / CRM | 2 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / PIM | 2 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Platform | 16 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Finance | 4 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / OMS | 2 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Localization | 1 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |
| Configuration / Integrations | 8 | Category tabIndex 0; verify keyboard entry; units/defaults/scope and change-impact preview | 55.21 |

## Targeted retests

- hr/roster: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- hr/onboarding: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- hr/appraisals: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- hr/training: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- hr/grievances: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- manufacturing/quality: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- manufacturing/subcontracting: shared records renderer before click = undefined; renderDocTableView is not defined; API failures 0.
- Mobile Picking: typed `WAVE-0001`; retained `0001`; hint `Wave ID India numbers are 10 digits. For another country, start with + and its dialling code.`; native validity true. No Load/Pick action executed.
- Fresh Department New dialog: title `Edit Department`; Tab sequence inside dialog = false → false → true → true → true → true → true; modal inert = false; Escape closed true, focus returned to `doc-create-button`. No viewport resize in this targeted probe.

## Every metadata record screen

Each row is a real list/New inspection, not a claim that a dedicated workbench is missing. Many
records intentionally have richer entry paths elsewhere. Technical fields in an advanced setup
record may be appropriate; daily-user exposure is what must change. All rows inherit 55.1/55.2.
“System-review” means verify whether generic create/edit belongs in specialist-only access; it
is not evidence that ordinary users can mutate the record or that the API lacks controls.
The record module/phase mapping preserves the current entitlement owner even when a business
journey crosses modules. Full field names/types/links are available in JSON.

| Record (owner) | Phase | Observed form | First-time-user change | Daily-operator change | Supervisor/control acceptance |
|---|---|---|---|---|---|
| AllocationRule (oms) | 55.9 | 6 fields / 4 required; Edit Allocation Rule | Explain Allocation Rule purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| AllocationStrategy (inventory) | 55.8 | 5 fields / 2 required; Edit Allocation Strategy | Explain Allocation Strategy purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| Appointment (inventory) | 55.7 | 11 fields / 6 required; Edit Appointment | Explain prerequisites; name/code lookup: PurchaseOrder, ASN | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Scheduled,CheckedIn,InProgress,Completed,Cancelled,NoShow |
| Appraisal (hr) | 55.14 | 7 fields / 4 required; Edit Appraisal | Structured editor for Ratings (JSON: [{"kra":"...","self_rating":..,"manager_rating":..}]) | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| AppraisalCycle (hr) | 55.14 | 4 fields / 3 required; Edit Appraisal Cycle | Explain Appraisal Cycle purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Draft,Active,Closed |
| ASN (procurement) | 55.4 | 10 fields / 4 required; Edit ASN | Structured editor for Expected Items JSON* | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Expected,Received,Cancelled |
| Asset (assets) | 55.16 | 13 fields / 6 required; Edit Asset | Explain prerequisites; name/code lookup: Employee | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Capitalised,Disposed |
| Attendance (hr) | 55.14 | 5 fields / 3 required; Edit Attendance | Explain prerequisites; name/code lookup: Employee | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Present,Absent,Late,Leave,Holiday,WeeklyOff |
| BackdatedPostingRequest (finance) | 55.10 | 5 fields / 5 required; Edit Backdated Posting Request | Explain Backdated Posting Request purpose; group required basics and defaults | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| BankAccount (finance) | 55.10 | 6 fields / 4 required; Edit Bank Account | Explain Bank Account purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| BankStatementLine (finance) | 55.10 | 7 fields / 4 required; Edit Bank Statement Line | Explain prerequisites; name/code lookup: BankAccount | Source prefill, related documents and next legal action; preserve calendar context | Verify scoped permissions, audit and safe correction |
| Batch (master_data) | 55.3 | 10 fields / 3 required; Edit Batch | Structured editor for Lottable Attributes JSON | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Quarantined,Expired,Blocked,Consumed |
| Bin (inventory) | 55.7 | 12 fields / 3 required; Edit Bin | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| BinReplenishmentRule (inventory) | 55.8 | 6 fields / 5 required; Edit Bin Replenishment Rule | Explain prerequisites; name/code lookup: Bin, Item | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| BOM (manufacturing) | 55.12 | 8 fields / 2 required; Edit BOM | Explain prerequisites; name/code lookup: Item | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| Brand (master_data) | 55.3 | 5 fields / 2 required; Edit Partner Brand | Explain Brand purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| Budget (finance) | 55.10 | 7 fields / 5 required; Edit Budget | Explain prerequisites; name/code lookup: CostCenter | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| BundleAssembly (oms) | 55.9 | 11 fields / 8 required; Edit Bundle Assembly | Explain prerequisites; name/code lookup: ProductBundle, Item, Location | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Completed,Failed |
| Campaign (crm_loyalty) | 55.17 | 6 fields / 4 required; Edit Campaign | Explain Campaign purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| CapturedCharge (inventory) | 55.8 | 14 fields / 11 required; Edit Captured Charge | Explain Captured Charge purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying; preserve calendar context | System-review: source ownership, redaction and immutable history |
| CartonType (inventory) | 55.8 | 6 fields / 3 required; Edit Carton Type | Explain Carton Type purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| CertificateOfAnalysis (quality) | 55.13 | 6 fields / 4 required; Edit Certificate Of Analysis | Explain prerequisites; name/code lookup: InspectionPlan | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Released,Rejected |
| Channel (pim) | 55.11 | 13 fields / 3 required; Edit Channel | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction |
| ChannelCategoryMap (pim) | 55.11 | 4 fields / 3 required; Edit Channel Category Map | Explain prerequisites; name/code lookup: Channel | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction; advanced setup |
| ChannelFieldMap (pim) | 55.11 | 6 fields / 4 required; Edit Channel Field Map | Explain prerequisites; name/code lookup: Channel, PIMTransformRule | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction; advanced setup |
| ChannelSKUException (oms) | 55.9 | 8 fields / 5 required; Edit Channel SKU Exception | Explain prerequisites; name/code lookup: Channel, Item | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Open,Resolved |
| ChannelSyncRun (oms) | 55.9 | 9 fields / 6 required; Edit Channel Sync Run | Explain prerequisites; name/code lookup: Channel | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| ChannelValidationRule (pim) | 55.11 | 5 fields / 3 required; Edit Channel Validation Rule | Explain prerequisites; name/code lookup: Channel | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction; advanced setup |
| ChargeCode (inventory) | 55.8 | 14 fields / 4 required; Edit Charge Code | Explain Charge Code purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ChargeContract (inventory) | 55.8 | 10 fields / 3 required; Edit Charge Contract | Explain Charge Contract purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| ChargeGroup (inventory) | 55.8 | 3 fields / 2 required; Edit Charge Group | Explain Charge Group purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| Color (master_data) | 55.3 | 6 fields / 3 required; Edit Gold Color | Explain Color purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| CompetitorPrice (pim) | 55.11 | 12 fields / 4 required; Edit Competitor Price | Explain prerequisites; name/code lookup: Item | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| ContentAssistLog (pim) | 55.11 | 10 fields / 3 required; Edit Content Assist Log | Explain Content Assist Log purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| CostCenter (core) | 55.3 | 3 fields / 2 required; Edit Cost Center | Explain Cost Center purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| CourierServiceArea (oms) | 55.9 | 5 fields / 3 required; Edit Courier Service Area | Explain Courier Service Area purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| CourierTrackingEvent (oms) | 55.9 | 6 fields / 5 required; Edit Courier Tracking Event | Explain prerequisites; name/code lookup: LogisticsBooking | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| CreditNote (finance) | 55.10 | 8 fields / 4 required; Edit Credit Note | Explain prerequisites; name/code lookup: SalesOrder, SalesInvoice | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Posted |
| CrossDockPlan (inventory) | 55.7 | 8 fields / 6 required; Edit Cross Dock Plan | Explain prerequisites; name/code lookup: ASN, PurchaseOrder | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Planned,Fulfilled,Cancelled |
| Currency (finance) | 55.10 | 5 fields / 3 required; Edit Currency | Explain Currency purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| Customer (sales) | 55.5 | 13 fields / 2 required; Edit Customer | Explain Customer purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive,Merged |
| CycleClass (inventory) | 55.6 | 6 fields / 5 required; Edit Cycle Class | Explain Cycle Class purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| CycleCountLine (inventory) | 55.6 | 12 fields / 5 required; Edit Cycle Count Line | Explain Cycle Count Line purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Posted,Recount Requested |
| DashboardDigest (reports) | 55.20 | 9 fields / 5 required; Edit Dashboard Digest | Explain Dashboard Digest purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| DashboardLayout (reports) | 55.20 | 5 fields / 3 required; Edit Dashboard Layout | Structured editor for Tiles (JSON)* | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction |
| DebitNote (finance) | 55.10 | 6 fields / 5 required; Edit Debit Note | Explain prerequisites; name/code lookup: Vendor | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Posted |
| DeferredRevenueSchedule (finance) | 55.10 | 7 fields / 5 required; Edit Deferred Revenue Schedule | Explain prerequisites; name/code lookup: SalesInvoice | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Active,Completed |
| Department (core) | 55.3 | 3 fields / 2 required; Edit Department | Explain Department purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| DockDoor (inventory) | 55.7 | 8 fields / 2 required; Edit Dock Door | Structured editor for Equipment (optional, comma-separated) | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| Employee (hr) | 55.14 | 12 fields / 2 required; Edit Employee | Explain Employee purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| EmployeeLoan (hr) | 55.14 | 7 fields / 4 required; Edit Employee Loan | Explain prerequisites; name/code lookup: Employee | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Active,Closed |
| ExchangeRate (finance) | 55.10 | 10 fields / 7 required; Edit Exchange Rate | Explain prerequisites; name/code lookup: Currency | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| ExpenseClaim (expenses) | 55.15 | 14 fields / 6 required; Edit Expense Claim | Explain prerequisites; name/code lookup: Employee, Project, Currency | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Verified,Paid |
| FloorAssistRequest (wms) | 55.7 | 4 fields / 3 required; Edit Floor Assist Request | Explain Floor Assist Request purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Open,Acknowledged,Resolved |
| FulfillmentTask (inventory) | 55.8 | 6 fields / 4 required; Edit Fulfillment Task | Explain Fulfillment Task purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Pending,Picking,Packed,Dispatched,Rejected |
| GatePass (oms) | 55.9 | 11 fields / 3 required; Edit Gate Pass | Explain prerequisites; name/code lookup: Manifest | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Issued,Completed,Discarded |
| GLPost (finance) | 55.10 | 5 fields / 5 required; Edit GL Post | Explain GLPost purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| Grievance (hr) | 55.14 | 6 fields / 4 required; Edit Grievance | Explain prerequisites; name/code lookup: Employee | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| GRN (procurement) | 55.4 | 6 fields / 4 required; Edit GRN | Structured editor for Received Items JSON* | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Pending,Approved,Cancelled |
| HelpArticleFeedback (core) | 55.3 | 3 fields / 2 required; Edit Help Article Feedback | Explain Help Article Feedback purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| Hold (inventory) | 55.6 | 7 fields / 5 required; Edit Hold | Explain Hold purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Active,Released |
| HoldCode (inventory) | 55.6 | 4 fields / 2 required; Edit Hold Code | Explain Hold Code purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| HoldReleaseRequest (inventory) | 55.6 | 3 fields / 2 required; Edit Hold Release Request | Explain prerequisites; name/code lookup: Hold | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| ImportJob (pim) | 55.11 | 7 fields / 6 required; Edit Import Job | Explain Import Job purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| InspectionPlan (quality) | 55.13 | 4 fields / 3 required; Edit Inspection Plan | Explain Inspection Plan purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| IntercompanyTransaction (finance) | 55.10 | 9 fields / 9 required; Edit Intercompany Transaction | Explain prerequisites; name/code lookup: LegalEntity | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Posted,Partially Posted |
| Item (inventory) | 55.6 | 38 fields / 4 required; Edit Item | Explain prerequisites; name/code lookup: ProductFamily, Brand | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction |
| JournalVoucher (finance) | 55.10 | 13 fields / 4 required; Edit Journal Voucher | Explain prerequisites; name/code lookup: CostCenter, Department, Currency, LegalEntity, Project | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Posted,Reversed,Recurring Template |
| LaborAllowance (inventory) | 55.8 | 5 fields / 2 required; Edit Labor Allowance | Explain Labor Allowance purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LaborElement (inventory) | 55.8 | 6 fields / 4 required; Edit Labor Element | Explain Labor Element purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LaborOperation (inventory) | 55.8 | 6 fields / 3 required; Edit Labor Operation | Explain Labor Operation purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LaborStandard (inventory) | 55.8 | 7 fields / 4 required; Edit Labor Standard | Structured editor for Element Codes (comma-separated)*; Allowance Codes (comma-separated) | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LandedCostVoucher (finance) | 55.10 | 3 fields / 3 required; Edit Landed Cost Voucher | Explain prerequisites; name/code lookup: GRN | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Applied |
| Leave (hr) | 55.14 | 7 fields / 6 required; Edit Leave | Explain prerequisites; name/code lookup: Employee | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Applied,Approved,Rejected |
| LegalEntity (core) | 55.3 | 5 fields / 2 required; Edit Legal Entity | Explain Legal Entity purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LoadingTask (inventory) | 55.8 | 11 fields / 4 required; Edit Loading Task | Explain prerequisites; name/code lookup: Manifest | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Planned,Loading,Loaded,Departed |
| Location (core) | 55.3 | 14 fields / 3 required; Edit Location | Explain prerequisites; name/code lookup: LegalEntity, Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LogisticsBooking (inventory) | 55.6 | 20 fields / 6 required; Edit Logistics Booking | Explain prerequisites; name/code lookup: FulfillmentTask, Manifest, ShippingPackage | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify AWB Assigned,Manifested,Handed Over,In-Transit,Delivered,RTO |
| LottableConstraint (master_data) | 55.3 | 6 fields / 4 required; Edit Lottable Constraint | Structured editor for Allowed Values (comma-separated)* | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| LoyaltyRedemptionRequest (crm_loyalty) | 55.17 | 5 fields / 4 required; Edit Loyalty Redemption Request | Explain Loyalty Redemption Request purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Redeemed |
| LPN (inventory) | 55.7 | 4 fields / 3 required; Edit LPN | Explain LPN purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Open,Closed,Shipped; advanced setup |
| MaintenanceOrder (quality) | 55.13 | 7 fields / 4 required; Edit Maintenance Order | Explain prerequisites; name/code lookup: Asset, MaintenanceSchedule | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Scheduled,InProgress,Completed,Cancelled |
| MaintenanceSchedule (quality) | 55.13 | 6 fields / 5 required; Edit Maintenance Schedule | Explain prerequisites; name/code lookup: Asset | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Paused |
| Manifest (oms) | 55.9 | 5 fields / 4 required; Edit Manifest | Explain Manifest purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Open,Handed Over |
| MarketplaceSettlement (finance) | 55.10 | 7 fields / 7 required; Edit Marketplace Settlement | Explain Marketplace Settlement purpose; group required basics and defaults | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Unreconciled,Reconciled |
| MarketplaceSettlementLine (finance) | 55.10 | 12 fields / 6 required; Edit Marketplace Settlement Line | Explain Marketplace Settlement Line purpose; group required basics and defaults | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| NDRCase (oms) | 55.9 | 7 fields / 5 required; Edit NDR Case | Explain prerequisites; name/code lookup: LogisticsBooking | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Open,Reattempt Scheduled,Closed,RTO |
| NonConformanceReport (quality) | 55.13 | 7 fields / 3 required; Edit Non Conformance Report | Explain prerequisites; name/code lookup: ReasonCode | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Investigating,CorrectiveActionPlanned,Closed |
| NotificationChannelConfig (oms) | 55.9 | 4 fields / 3 required; Edit Notification Channel Config | Explain Notification Channel Config purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| NotificationLog (oms) | 55.9 | 7 fields / 4 required; Edit Notification Log | Explain Notification Log purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| NotificationTemplate (oms) | 55.9 | 6 fields / 4 required; Edit Notification Template | Explain Notification Template purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| Offer (sales) | 55.5 | 20 fields / 4 required; Edit Offer | Explain Offer purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| OMSSavedView (oms) | 55.9 | 4 fields / 3 required; Edit OMS Saved View | Explain OMSSaved View purpose; group required basics and defaults | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| OnboardingChecklist (hr) | 55.14 | 6 fields / 4 required; Edit Onboarding Checklist | Structured editor for Checklist Items JSON ([{task, done}]); Document Locker JSON ([{name, url}]) | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify In Progress,Completed |
| PackingValidationTemplate (inventory) | 55.8 | 7 fields / 5 required; Edit Packing Validation Template | Explain Packing Validation Template purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PackStation (inventory) | 55.8 | 5 fields / 1 required; Edit Pack Station | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PackTemplate (inventory) | 55.8 | 9 fields / 2 required; Edit Pack Template | Structured editor for Documents Required (comma-separated, optional); Labels Required (comma-separated, optional) | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PasswordResetRequest (hr) | 55.14 | 5 fields / 4 required; Edit Password Reset Request | Explain prerequisites; name/code lookup: User | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Failed |
| PaymentProposal (finance) | 55.10 | 4 fields / 4 required; Edit Payment Proposal | Structured editor for Invoice IDs (JSON)* | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Executed |
| Payslip (hr) | 55.14 | 12 fields / 7 required; Edit Payslip | Explain prerequisites; name/code lookup: Employee | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Posted |
| PhysicalInventory (inventory) | 55.6 | 8 fields / 3 required; Edit Physical Inventory | Explain Physical Inventory purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Counting,Reconciling,Closed,Cancelled |
| PIMCatalog (pim) | 55.11 | 7 fields / 3 required; Edit PIM Catalog | Explain prerequisites; name/code lookup: PIMProductGroup | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMExportSchedule (pim) | 55.11 | 10 fields / 5 required; Edit PIM Export Schedule | Explain prerequisites; name/code lookup: PIMExportTemplate | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMExportTemplate (pim) | 55.11 | 7 fields / 4 required; Edit PIM Export Template | Explain prerequisites; name/code lookup: Channel | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMImportSchedule (pim) | 55.11 | 11 fields / 4 required; Edit PIM Import Schedule | Explain prerequisites; name/code lookup: PIMImportTemplate | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMImportTemplate (pim) | 55.11 | 5 fields / 3 required; Edit PIM Import Template | Explain PIMImport Template purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMProductGroup (pim) | 55.11 | 9 fields / 3 required; Edit PIM Product Group | Explain prerequisites; name/code lookup: ProductFamily, ProductAttributeDef | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMProductProfile (pim) | 55.11 | 6 fields / 3 required; Edit PIM Product Profile | Structured editor for Missing Fields (JSON) | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| PIMTask (pim) | 55.11 | 17 fields / 5 required; Edit PIM Task | Explain prerequisites; name/code lookup: Item | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Open,In Progress,Blocked,Done,Cancelled |
| PIMTaskTemplate (pim) | 55.11 | 10 fields / 4 required; Edit PIM Task Template | Explain PIMTask Template purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMTransformRule (pim) | 55.11 | 4 fields / 2 required; Edit PIM Transform Rule | Explain PIMTransform Rule purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMWorkflowDefinition (pim) | 55.11 | 4 fields / 2 required; Edit PIM Workflow Definition | Explain PIMWorkflow Definition purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PIMWorkflowRun (pim) | 55.11 | 9 fields / 4 required; Edit PIM Workflow Run | Explain prerequisites; name/code lookup: PIMWorkflowDefinition, Item | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| POSCart (sales) | 55.5 | 7 fields / 4 required; Edit POS Cart | Explain POSCart purpose; group required basics and defaults | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| POSCostingGap (sales) | 55.5 | 5 fields / 5 required; Edit POS Costing Gap | Explain POSCosting Gap purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| POSInvoice (sales) | 55.5 | 5 fields / 5 required; Edit POS Invoice | Structured editor for Sales Items JSON* | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| POSOfflineQueueGap (sales) | 55.5 | 6 fields / 6 required; Edit POS Offline Queue Gap | Explain POSOffline Queue Gap purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| POSOfflineSyncVariance (sales) | 55.5 | 6 fields / 6 required; Edit POS Offline Sync Variance | Explain POSOffline Sync Variance purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| POSPriceOverride (sales) | 55.5 | 11 fields / 9 required; Edit POS Price Override | Explain POSPrice Override purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Approved,Pending Approval,Rejected,Consumed |
| POSProfile (sales) | 55.5 | 6 fields / 4 required; Edit POS Profile | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| POSSession (sales) | 55.5 | 8 fields / 5 required; Edit POS Session | Explain prerequisites; name/code lookup: POSProfile | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Open,Closed |
| PrepaidExpenseSchedule (finance) | 55.10 | 8 fields / 6 required; Edit Prepaid Expense Schedule | Explain Prepaid Expense Schedule purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Completed |
| PreShipValidationRule (inventory) | 55.8 | 6 fields / 4 required; Edit Pre Ship Validation Rule | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PriceListVersion (sales) | 55.5 | 6 fields / 3 required; Edit Price List Version | Explain prerequisites; name/code lookup: Currency | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Superseded |
| Printer (stickers) | 55.19 | 10 fields / 2 required; Edit Printer | Explain Printer purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ProductAttributeDef (pim) | 55.11 | 6 fields / 3 required; Edit Product Attribute Def | Explain prerequisites; name/code lookup: ProductAttributeGroup | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ProductAttributeGroup (pim) | 55.11 | 4 fields / 2 required; Edit Product Attribute Group | Explain Product Attribute Group purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ProductAttributeValue (pim) | 55.11 | 7 fields / 5 required; Edit Product Attribute Value | Explain prerequisites; name/code lookup: Item, ProductAttributeDef, Channel | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| ProductBundle (oms) | 55.9 | 7 fields / 5 required; Edit Product Bundle | Explain prerequisites; name/code lookup: Item | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ProductContent (pim) | 55.11 | 11 fields / 5 required; Edit Product Content | Structured editor for Tags (comma-separated) | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| ProductFamily (pim) | 55.11 | 4 fields / 2 required; Edit Product Family | Explain Product Family purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ProductFamilyAttribute (pim) | 55.11 | 5 fields / 3 required; Edit Product Family Attribute | Explain prerequisites; name/code lookup: ProductFamily, ProductAttributeDef | Fast scoped search, duplicate-safe maintenance and return to task | Verify scoped permissions, audit and safe correction |
| ProductionOrder (manufacturing) | 55.12 | 10 fields / 4 required; Edit Production Order | Explain prerequisites; name/code lookup: BOM, Routing | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Material Issued,In Process,Completed |
| ProductMedia (pim) | 55.11 | 15 fields / 7 required; Edit Product Media | Structured editor for Tags (comma-separated) | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Active,Inactive |
| Project (finance) | 55.18 | 4 fields / 2 required; Edit Project | Explain Project purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| PurchaseOrder (procurement) | 55.4 | 14 fields / 5 required; Edit Purchase Order | Explain prerequisites; name/code lookup: Vendor, Currency | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected,Closed |
| PurchaseRequisition (procurement) | 55.4 | 6 fields / 5 required; Edit Purchase Requisition | Explain Purchase Requisition purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Converted,Rejected |
| PurchaseRequisitionDescription (procurement) | 55.4 | 3 fields / 2 required; Edit Purchase Requisition Description | Explain Purchase Requisition Description purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| PutawayStrategy (inventory) | 55.7 | 5 fields / 2 required; Edit Putaway Strategy | Structured editor for Criteria (comma-separated: velocity, zone_sequence, capacity, hazmat_temp, batch_consolidation)* | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| QualityInspection (manufacturing) | 55.12 | 5 fields / 4 required; Edit Quality Inspection | Explain prerequisites; name/code lookup: ProductionOrder | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| RateGroup (inventory) | 55.8 | 7 fields / 2 required; Edit Rate Group | Explain Rate Group purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ReasonCode (oms) | 55.9 | 6 fields / 3 required; Edit Reason Code | Explain Reason Code purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ReceiptValidationRule (inventory) | 55.7 | 4 fields / 2 required; Edit Receipt Validation Rule | Explain prerequisites; name/code lookup: Vendor | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| RecurringSalesContract (sales) | 55.5 | 7 fields / 6 required; Edit Recurring Sales Contract | Explain Recurring Sales Contract purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Paused,Cancelled |
| RefundRequest (oms) | 55.9 | 8 fields / 4 required; Edit Refund Request | Explain prerequisites; name/code lookup: ReturnRequest | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Pending,Approved,Processed,Rejected |
| ReorderPointConfig (inventory) | 55.6 | 6 fields / 5 required; Edit Reorder Point Config | Explain Reorder Point Config purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ReportColumnProfile (reports) | 55.20 | 5 fields / 5 required; Edit Report Column Profile | Explain Report Column Profile purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| ReportExportJob (reports) | 55.20 | No New; 403,403 | Explain Report Export Job purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| ReportFilterPreset (reports) | 55.20 | 4 fields / 4 required; Edit Report Filter Preset | Explain Report Filter Preset purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| ReportRunLog (reports) | 55.20 | 4 fields / 3 required; Edit Report Run Log | Explain Report Run Log purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| ReturnRequest (oms) | 55.9 | 11 fields / 4 required; Edit Return Request | Structured editor for Items (JSON) | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Requested,Approved,Rejected,Received,QC Complete,Closed |
| RFQ (rfq) | 55.4 | 5 fields / 3 required; Edit RFQ | Explain RFQ purpose; group required basics and defaults | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Sent,Closed |
| RoboticsIntegrationCredential (inventory) | 55.8 | 3 fields / 2 required; Edit Robotics Integration Credential | Explain Robotics Integration Credential purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| Routing (manufacturing) | 55.12 | 3 fields / 2 required; Edit Routing | Explain Routing purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| SalaryStructure (hr) | 55.14 | 10 fields / 4 required; Edit Salary Structure | Explain prerequisites; name/code lookup: Employee | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| SalesInvoice (sales) | 55.5 | 19 fields / 4 required; Edit Sales Invoice | Structured editor for Invoice Lines (JSON) | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Approved,Paid,Cancelled |
| SalesOrder (oms) | 55.9 | 14 fields / 4 required; Edit Sales Order | Explain Sales Order purpose; group required basics and defaults | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| SalesOrderLine (oms) | 55.9 | 14 fields / 5 required; Edit Sales Order Line | Explain prerequisites; name/code lookup: SalesOrder, Item | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| SalesReturn (sales) | 55.5 | 3 fields / 3 required; Edit Sales Return | Explain Sales Return purpose; group required basics and defaults | Source prefill, related documents and next legal action | Verify scoped permissions, audit and safe correction |
| ScheduledReport (reports) | 55.20 | 10 fields / 5 required; Edit Scheduled Report | Structured editor for Parameters (JSON, optional) | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| SerialNumber (master_data) | 55.3 | 10 fields / 3 required; Edit Serial Number | Explain Serial Number purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify InStock,Allocated,Shipped,Returned,Scrapped |
| ServiceContract (service) | 55.18 | 9 fields / 5 required; Edit Service Contract | Explain prerequisites; name/code lookup: Asset, RecurringSalesContract | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Expired,Cancelled |
| ServiceTicket (service) | 55.18 | 12 fields / 5 required; Edit Service Ticket | Explain prerequisites; name/code lookup: Asset, ServiceContract | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Assigned,InProgress,Resolved,Closed,Cancelled |
| Shift (hr) | 55.14 | 7 fields / 4 required; Edit Shift | Explain Shift purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| ShiftAssignment (hr) | 55.14 | 6 fields / 5 required; Edit Shift Assignment | Explain prerequisites; name/code lookup: Employee, Shift | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Assigned,Cancelled |
| ShippingPackage (oms) | 55.9 | 14 fields / 3 required; Edit Shipping Package | Explain prerequisites; name/code lookup: FulfillmentTask, SalesOrder, SalesInvoice, ShippingPackage, LoadingTask | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Invoiced,Shipped,Cancelled |
| SortSlot (inventory) | 55.8 | 8 fields / 3 required; Edit Sort Slot | Explain prerequisites; name/code lookup: FulfillmentTask | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Empty,Assigned,Filled,Cleared |
| SortStation (inventory) | 55.8 | 5 fields / 2 required; Edit Sort Station | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| StatusTransitionRule (oms) | 55.9 | 7 fields / 4 required; Edit Status Transition Rule | Explain Status Transition Rule purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| StickerTemplate (stickers) | 55.19 | 7 fields / 4 required; Edit Sticker Template | Structured editor for Categories (comma-separated, matched against Item > Category) | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| StockLedgerEntry (inventory) | 55.6 | 15 fields / 6 required; Edit Stock Ledger Entry | Explain Stock Ledger Entry purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| StorageBalanceSnapshot (inventory) | 55.8 | 7 fields / 7 required; Edit Storage Balance Snapshot | Explain Storage Balance Snapshot purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying; preserve calendar context | System-review: source ownership, redaction and immutable history |
| StorageBillingRate (inventory) | 55.8 | 11 fields / 5 required; Edit Storage Billing Rate | Explain Storage Billing Rate purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive; advanced setup |
| Style (master_data) | 55.3 | 6 fields / 5 required; Edit Jewelry Style | Explain Style purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| SubcontractOrder (manufacturing) | 55.12 | 11 fields / 7 required; Edit Subcontract Order | Explain prerequisites; name/code lookup: Vendor | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Sent,Received,Cancelled |
| SupplierSubmission (pim) | 55.11 | 13 fields / 6 required; Edit Supplier Submission | Structured editor for Tags (comma-separated) | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Pending Approval,Approved,Rejected |
| TaskCompletionLog (inventory) | 55.8 | 5 fields / 2 required; Edit Task Completion Log | Explain Task Completion Log purpose; group required basics and defaults | Use source/task outcome viewer; avoid routine re-keying | System-review: source ownership, redaction and immutable history |
| TaskDispatchStrategy (inventory) | 55.8 | 5 fields / 2 required; Edit Task Dispatch Strategy | Structured editor for Sort Order (comma-separated: priority, ageing, type)* | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| TDSSection (finance) | 55.10 | 5 fields / 4 required; Edit TDS Section | Explain TDSSection purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| Trailer (inventory) | 55.7 | 5 fields / 1 required; Edit Trailer | Explain Trailer purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| TrainingProgram (hr) | 55.14 | 4 fields / 1 required; Edit Training Program | Explain Training Program purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| TrainingRecord (hr) | 55.14 | 6 fields / 4 required; Edit Training Record | Explain prerequisites; name/code lookup: Employee, TrainingProgram | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Enrolled,Completed,Failed |
| TransferOrder (inventory) | 55.6 | 5 fields / 4 required; Edit Transfer Order | Structured editor for Transfer Items JSON* | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Draft,Approved,Packed,Dispatched,Received |
| TravelSection (inventory) | 55.8 | 5 fields / 2 required; Edit Travel Section | Explain Travel Section purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| UOM (master_data) | 55.3 | 3 fields / 1 required; Edit UOM | Explain UOM purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| UOMConversion (master_data) | 55.3 | 5 fields / 5 required; Edit UOM Conversion | Explain UOMConversion purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| UserWorkSchedule (hr) | 55.14 | 6 fields / 4 required; Edit User Work Schedule | Explain User Work Schedule purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive |
| Vendor (procurement) | 55.4 | 10 fields / 2 required; Edit Vendor | Explain Vendor purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| VendorInvoice (procurement) | 55.4 | 10 fields / 7 required; Edit Vendor Invoice | Explain prerequisites; name/code lookup: Vendor, PurchaseOrder, GRN, Currency | Source prefill, related documents and next legal action; preserve calendar context | Use allowed lifecycle transitions; verify Draft,Matched,MismatchHold,Approved,Paid |
| VendorQuote (rfq) | 55.4 | 6 fields / 4 required; Edit Vendor Quote | Explain prerequisites; name/code lookup: RFQ, Vendor | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Submitted,Selected,Rejected |
| Voucher (crm_loyalty) | 55.17 | 7 fields / 3 required; Edit Voucher | Explain Voucher purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task; preserve calendar context | Use allowed lifecycle transitions; verify Active,Inactive,Expired,Exhausted |
| WarehouseTask (inventory) | 55.8 | 20 fields / 3 required; Edit Warehouse Task | Explain Warehouse Task purpose; group required basics and defaults | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Pending,Assigned,In Progress,Completed,Cancelled,Exception |
| Wave (inventory) | 55.8 | 7 fields / 4 required; Edit Wave | Explain prerequisites; name/code lookup: WaveTemplate | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify Planned,Released,In Progress,Complete,Closed |
| WaveTemplate (inventory) | 55.8 | 10 fields / 1 required; Edit Wave Template | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| WebhookSubscription (integrations) | 55.21 | 6 fields / 5 required; Edit Webhook Subscription | Explain Webhook Subscription purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| WeeklySchedule (hr) | 55.14 | 6 fields / 5 required; Edit Weekly Schedule | Structured editor for Work Days (comma-separated)* | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| WorkCenter (manufacturing) | 55.12 | 5 fields / 2 required; Edit Work Center | Explain Work Center purpose; group required basics and defaults | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |
| YardCheckIn (inventory) | 55.7 | 9 fields / 2 required; Edit Yard Check In | Explain prerequisites; name/code lookup: Appointment | Source prefill, related documents and next legal action | Use allowed lifecycle transitions; verify InYard,AtDoor,Departed |
| Zone (inventory) | 55.7 | 8 fields / 1 required; Edit Zone | Explain prerequisites; name/code lookup: Location | Fast scoped search, duplicate-safe maintenance and return to task | Use allowed lifecycle transitions; verify Active,Inactive |

## Report-catalog parameter screens

Every entry below was selected in the actual report dropdown; its parameter form was inspected
at desktop and phone sizes. **Run/Export was not invoked**: this does not verify SQL, output,
totals, masking or drill-through. Category sample screenshots and per-report field observations
are in the JSON. All belong to 55.20 plus their business domain. The three lenses for each are:
first-time user needs a plain business question and usable filters; operator needs sensible
defaults/saved filters and source drill-through; owner must verify scope, sensitive columns,
units/timezone and reconciliation. Native parameter controls alone do not prove those outcomes.

| Report / category | Actual parameter labels | Observed labels / result verification |
|---|---|---|
| Budget vs Actual (Finance) | From *; To * | Parameter form rendered; output not run |
| Dunning Queue (Finance) | No parameters | Parameter form rendered; output not run |
| Cash Flow Forecast (Finance) | As Of *; Horizon (days) | Parameter form rendered; output not run |
| Campaign ROI (CRM) | No parameters | Parameter form rendered; output not run |
| Inventory Valuation (Inventory) | No parameters | Parameter form rendered; output not run |
| Customer Lifetime Value (CRM) | Churn Threshold Days (default 90) | Parameter form rendered; output not run |
| Cohort Retention (CRM) | No parameters | Parameter form rendered; output not run |
| RFM Customer Segmentation (CRM) | No parameters | Parameter form rendered; output not run |
| Loyalty Points Liability (CRM) | No parameters | Parameter form rendered; output not run |
| Customer 360 (CRM) | Customer ID * | Parameter form rendered; output not run |
| Open FX Exposure (Finance) | As Of (YYYY-MM-DD, default today); Rate Type (Closing/Spot/Average) | Parameter form rendered; output not run |
| FX Gain/Loss Register (Finance) | From (YYYY-MM-DD, default 3 months back); To (YYYY-MM-DD, default today); Kind (realised / unrealised, blank for both) | Parameter form rendered; output not run |
| Trial Balance in Presentation Currency (Finance) | As Of (YYYY-MM-DD, default today); Present In (ISO code, e.g. USD); Rate Type (Closing/Spot/Average) | Parameter form rendered; output not run |
| Deferred Revenue Roll-Forward (Finance) | No parameters | Parameter form rendered; output not run |
| Prepaid Expense Roll-Forward (Finance) | No parameters | Parameter form rendered; output not run |
| Profit & Loss (Finance) | From *; To *; Cost Center (optional); Department (optional); Entity (optional) | Parameter form rendered; output not run |
| Balance Sheet (Finance) | As Of *; Cost Center (optional); Department (optional); Entity (optional) | Parameter form rendered; output not run |
| Cash Flow Statement (Finance) | From *; To * | Parameter form rendered; output not run |
| GL Drill-down (by Account) (Finance) | Account Code * | Parameter form rendered; output not run |
| Customer Ledger (Sales) | Customer (optional) | Parameter form rendered; output not run |
| Tax Ledger (Finance) | From (optional); To (optional) | Parameter form rendered; output not run |
| Statutory Audit Export (Full GL) (Finance) | Period Start *; Period End * | Parameter form rendered; output not run |
| Cost Center P&L (Finance) | From *; To * | Parameter form rendered; output not run |
| Department P&L (Finance) | From *; To * | Parameter form rendered; output not run |
| Knowledge Center Feedback (Admin) | No parameters | Parameter form rendered; output not run |
| Entity Trial Balance (Finance) | From *; To * | Parameter form rendered; output not run |
| Intercompany Reconciliation (Finance) | As Of * | Parameter form rendered; output not run |
| Consolidated Trial Balance (Finance) | As Of * | Parameter form rendered; output not run |
| Production Cost Variance (Manufacturing) | No parameters | Parameter form rendered; output not run |
| Competitor Price Gap (Sales) | Platform (optional); Our SKU (optional); Observed on or after (optional); We are (optional) | Parameter form rendered; output not run |
| OMS Exception Queue (OMS) | No parameters | Parameter form rendered; output not run |
| OMS Reconciliation Variance (OMS) | No parameters | Parameter form rendered; output not run |
| Order Aging (OMS) | No parameters | Parameter form rendered; output not run |
| SLA Breach (OMS) | Threshold (minutes, default 120) | Parameter form rendered; output not run |
| Allocation Pending (OMS) | No parameters | Parameter form rendered; output not run |
| Stock Mismatch (OMS) | No parameters | Parameter form rendered; output not run |
| Return Aging (OMS) | No parameters | Parameter form rendered; output not run |
| Reserved Stock (OMS) | No parameters | Parameter form rendered; output not run |
| Courier Performance (OMS) | No parameters | Parameter form rendered; output not run |
| Orphaned Channel Orders (pre-35.1 intake) (OMS) | No parameters | Parameter form rendered; output not run |
| PIM Product Group Readiness (PIM) | Product Group ID or code * | Parameter form rendered; output not run |
| PIM Task Workload by Assignee (PIM) | No parameters | Parameter form rendered; output not run |
| PIM Overdue Tasks (PIM) | Assignee (blank = everyone) | Parameter form rendered; output not run |
| PIM Stalled Workflow Runs (PIM) | No parameters | Parameter form rendered; output not run |
| Demand Forecast (Trend) (Inventory) | SKU *; Location *; Forecast Days * | Parameter form rendered; output not run |
| Reorder Suggestions (Configured) (Inventory) | Location *; Default Lead Time (days) *; Default Safety Stock * | Parameter form rendered; output not run |
| Pegged Demand & Supply (Inventory) | SKU *; Location (for on-hand supply) | Parameter form rendered; output not run |
| Production Capacity Schedule (Manufacturing) | No parameters | Parameter form rendered; output not run |
| POS Sale Reconciliation (Finance) | From; To | Parameter form rendered; output not run |
| Project P&L (Job Costing) (Finance) | From *; To * | Parameter form rendered; output not run |
| Current Stock (Inventory) | No parameters | Parameter form rendered; output not run |
| Sales Register (Sales) | No parameters | Parameter form rendered; output not run |
| Vendor Ledger (Procurement) | Vendor (optional) | Parameter form rendered; output not run |
| Payables Ageing (Finance) | No parameters | Parameter form rendered; output not run |
| Receivables Ageing (Finance) | No parameters | Parameter form rendered; output not run |
| GST Return Summary (Finance) | From *; To * | Parameter form rendered; output not run |
| GRN Register (Procurement) | No parameters | Parameter form rendered; output not run |
| Cash Book (Finance) | No parameters | Parameter form rendered; output not run |
| Bank Book (Finance) | Bank Account * | Parameter form rendered; output not run |
| Asset Register (Assets) | No parameters | Parameter form rendered; output not run |
| Loyalty Ledger Summary (CRM) | No parameters | Parameter form rendered; output not run |
| Production Order Status (Manufacturing) | No parameters | Parameter form rendered; output not run |
| RFQ Comparison Export (Procurement) | RFQ ID * | Parameter form rendered; output not run |
| Stock Ledger (Inventory) | SKU (optional); Location (optional); Voucher Type (optional); From (optional); To (optional) | Parameter form rendered; output not run |
| Stale Approvals (Exceptions) | Threshold Hours (default 24) | Parameter form rendered; output not run |
| Failed Syncs (Exceptions) | No parameters | Parameter form rendered; output not run |
| Attendance Summary (HR) | From (optional); To (optional) | Parameter form rendered; output not run |
| Negative Stock Flags (Exceptions) | No parameters | Parameter form rendered; output not run |
| Report Performance (BI) | From (optional); To (optional) | Parameter form rendered; output not run |
| Serial Number Inquiry (WMS) | Item Code (optional); Serial Number (optional); Status (optional) | Parameter form rendered; output not run |
| Serial Movement History (WMS) | Serial Number *; Item Code (optional) | Parameter form rendered; output not run |
| Service SLA Breaches (Service) | No parameters | Parameter form rendered; output not run |
| Unsettled Marketplace Orders (OMS) | No parameters | Parameter form rendered; output not run |
| Settlement Variance Queue (OMS) | No parameters | Parameter form rendered; output not run |
| Batch Near-Expiry Watchlist (WMS) | Within (days, default 30) | Parameter form rendered; output not run |
| Batch Stock Inquiry (WMS) | Item Code (optional); Location (optional); Batch / Lot (optional) | Parameter form rendered; output not run |
| Batch Movement History (Recall) (WMS) | Batch / Lot *; Item Code (optional) | Parameter form rendered; output not run |
| 3PL Storage & Handling Billing (WMS) | Owner (optional); Period Start *; Period End * | Parameter form rendered; output not run |
| Cross-Facility Inventory Inquiry (WMS) | SKU * | Parameter form rendered; output not run |
| Facility Roll-Up Inventory Inquiry (WMS) | Facility (Location code) * | Parameter form rendered; output not run |
| Labour Enterprise Productivity (WMS) | From (optional); To (optional) | Parameter form rendered; output not run |
| Labour Facility Performance (WMS) | From (optional); To (optional) | Parameter form rendered; output not run |
| Labour Task Productivity (WMS) | From (optional); To (optional) | Parameter form rendered; output not run |
| Labour User Performance (WMS) | From (optional); To (optional) | Parameter form rendered; output not run |
| Labour Standards Audit (WMS) | No parameters | Parameter form rendered; output not run |
| Labour Cost (WMS) | From (optional); To (optional) | Parameter form rendered; output not run |
| 3PL Storage Billing v2 (WMS) | Owner (optional); Period Start *; Period End * | Parameter form rendered; output not run |
| Owner Stock Inquiry (3PL) (WMS) | Item Code (optional); Location (optional); Owner (optional) | Parameter form rendered; output not run |
| Labor Productivity (WMS) | From (optional); To (optional) | Parameter form rendered; output not run |
| Slotting / Re-Slotting Suggestions (WMS) | Location * | Parameter form rendered; output not run |
| Unslotting Suggestions (discontinued items) (WMS) | Location * | Parameter form rendered; output not run |
| Clean-Location Consolidation Suggestions (WMS) | Location * | Parameter form rendered; output not run |

## Follow-through / acceptance not yet performed

For **each** register entry, its build phase must record: named first-time-user task, daily-user
fast path, supervisor/control case; empty/populated/invalid/denied/error recovery; applicable
print/help/back-navigation; actual save/post outcome on disposable data; reconciliation and
role-specific result. Conditional workflows and hardware are still explicit open coverage.
The checklist keeps all Stage 55 implementation phases unchecked until that evidence exists.
