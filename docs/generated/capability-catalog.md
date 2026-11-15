<!-- GENERATED from docs/product/capability-register.json by cmd/doclint. DO NOT EDIT. -->

# Capability catalog

Source status: **approved**. Owner: **product-owner**. Version label: **none accepted yet (register approved by the product owner 2026-11-15, ask 193; product releases are listed under Control Panel > Releases)**.

Maturity is scoped to the configurations below. Source and test-file links are implementation evidence, not proof that a deployed release passed. No Production or Certified claim is authorized without the signed evidence and approver required by the registry validator.

## Reference configurations

| ID | Industry / country | Deployment / devices | Owner model | Limit |
|---|---|---|---|---|
| REF-RETAIL-IN | single/multi-store retail / India | one Go service + PostgreSQL; tenant schemas / desktop browser; mobile/RF not certified | retailer-owned stock | Preview evaluation; tenant-role migration, deployment controls and release acceptance remain required. |
| REF-WAREHOUSE-IN | wholesale distribution with dedicated-owner warehouses / India | one Go service + PostgreSQL; tenant schemas / browser/RF candidate; physical verification pending | one stock owner per warehouse; Stage 47.5 single_owner guard | Preview evaluation of the selected single-owner configuration; deployment and release acceptance required. Mixed-owner mode (Stage 59, owner isolation in allocation, picking and movements) is built but not part of this reference configuration's evaluation. |

## Capability status

Module keys link this product register to the executable module catalog. The registry schema version and release label identify the mapping version; source checks derive DocTypes, role vocabulary, routes, screens, workers, reports, exports and core dependencies from their existing declarations.

| Capability | Module keys | Maturity / configuration | Owner | Limits / remaining gates |
|---|---|---|---|---|
| CAP-POS — Point of sale | sales, sales_incentives | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | store-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-RET — Returns and refunds | sales, oms | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | store-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-WMS — Warehouse operations | wms, stickers, inventory | Experimental / REF-RETAIL-IN, REF-WAREHOUSE-IN | warehouse-process-owner | Single-owner-per-warehouse guard implemented (47.5). Mixed-owner warehouses are a supported setting since Stage 59: orders, allocation, availability and every bin movement carry the owner, with an owner-stock exception report. Physical device, deployment and qualified release acceptance remain open. |
| CAP-OMS — Order management | oms, custom_orders, wholesale | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | order-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-PIM — Product and master data | pim, master_data | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | data-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-BUY — Procurement | procurement, rfq | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | procurement-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-INV — Inventory and transfers | inventory, memo_stock | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | inventory-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-FIN — Finance and tax | finance, assets, expenses | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | finance-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-CRM — Customer relationship and loyalty | crm_loyalty | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | customer-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-HR — People and payroll | hr | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | hr-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-MFG — Manufacturing and planning | manufacturing, quality | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | manufacturing-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-SRV — Projects and service | service | Experimental / REF-RETAIL-IN, REF-WAREHOUSE-IN | service-process-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-API — Platform and integrations | core, integrations, reports, ai_access | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | engineering-owner | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |
| CAP-KB — Knowledge Center | core | Preview / REF-RETAIL-IN, REF-WAREHOUSE-IN | documentation-maintainer | Source/test locations available; no approved deployment-specific release/UAT evidence attached. Stage gates remain authoritative. |

## Capability checks

179 rows. Source: Enterprise_Business_Platform_Suite_Completion_Blueprint.pdf v1.0, section 14 (346 rows, read 2026-10-17): the 179 rows that are not Built - verify, by the owner's choice (73.0.2, ask 175/178). The 167 Built - verify rows are proven through the journeys (73.J).

Status is the blueprint's assessment until the row's acceptance test is evidenced; the checklist item says what has been built since. A row becomes Verified only with its evidence attached. New work names the row it closes (`[row: BP-nnn]`).

### CAP-POS — Point of sale (8)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-079 | Pricing | Price list/rules/server authority | Partial | P0 | Client cannot choose unauthorized price | pending | none |
| BP-082 | Advance/deposit | Customer advance and redemption | Missing / benchmark gap | P1 | Advance liability and final bill reconcile | pending | none |
| BP-083 | B2B reference | PO/customer ref and invoice fields | Partial | P1 | Reference persists to invoice/report | pending | 73.2.9 |
| BP-085 | Split tender | Multiple tender lines | Partial | P0 | Refund allocates to original tender | pending | 73.1.6 |
| BP-086 | Hold/resume bill | Park and recover cart safely | Missing / benchmark gap | P1 | No duplicate on resume | pending | 73.2.5 |
| BP-089 | Exchange | Return + new sale/net settlement | Missing / benchmark gap | P1 | Tax/tender/loyalty reconcile | pending | 73.2.6 |
| BP-090 | Bill edit/change history | Controlled post-sale metadata edits | Partial | P1 | Before/after/actor/reason report | pending | 73.2.8 |
| BP-092 | Void/cancel/reprint | Controlled cashier actions | Partial | P1 | Reason/authority/audit required | pending | 73.2.8 |

### CAP-WMS — Warehouse operations (3)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-073 | Mixed-owner allocation | Owner-aware demand/allocation | Partial | P0 | Adversarial two-owner lifecycle passes | pending | none |
| BP-076 | Cross-docking | Inbound-to-outbound direct flow | Missing / benchmark gap | P3 | Build only if DC model needs | pending | none |
| BP-077 | RF/mobile certification | Real scanner/device floor test | External gate | P0 | Supported hardware matrix signed | pending | 73.B.11 |

### CAP-OMS — Order management (18)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-113 | Allocation/sourcing | Best node and owner-safe source | Partial | P0 | Allocation respects stock/owner | pending | none |
| BP-120 | Cancellation | Pre/post allocation cancel rules | Partial | P1 | Stock/payment/status reversed correctly | pending | 73.2.15 |
| BP-122 | Customer return/refund | Return Anywhere/QC/refund | Partial | P0 | Full return-to-refund accepted | pending | 73.1.6 |
| BP-123 | Marketplace settlement | Fees/payout/order recon | Partial | P0 | Settlement difference explained | pending | 73.2.16 |
| BP-124 | Notifications | Template/delivery/retry/status | Partial | P1 | No false delivery claim | pending | 73.2.13 |
| BP-125 | Connector replay | Safe retry/reconciliation | Partial | P0 | Provider fixture replay proven | pending | none |
| BP-126 | Promise/ATP | Promised date/service promise | Missing / benchmark gap | P1/P2 | Promise tied to supply/lead time | pending | 73.4.2 |
| BP-127 | Payment state | Authorization/capture/refund/chargeback orchestration | Partial | P0/P1 | Order state reconciles with payment provider | pending | none |
| BP-128 | Backorder/preorder | Controlled future supply commitment | Missing / benchmark gap | P2 | Promise and allocation rules explicit | pending | 73.4.2 |
| BP-129 | Substitution | Approved alternate item logic | Missing / benchmark gap | P2 | Customer consent and price/tax preserved | pending | 73.4.2 |
| BP-130 | Customer-service mutation | Address/item/qty changes before cutoff | Partial | P1 | Every mutation revalidates tax/stock/payment and audits | pending | 73.2.15 |
| BP-131 | Exchange/replacement | Replacement/reship linked to original order | Partial | P1 | Inventory/refund/payment lineage reconciles | pending | 73.2.15 |
| BP-132 | Unified order timeline | All internal/external events in one chronology | Partial | P1 | Operator sees authoritative next action | pending | 73.2.14 |
| BP-133 | Exception work queues | Stuck payment/allocation/fulfillment/carrier/return/settlement | Partial | P1 | No failed order requires DB inspection | pending | 73.2.14 |
| BP-134 | Fraud/order risk | Order screening/hold | Missing / benchmark gap | P2 | Build if business requires | pending | 73.4.2 |
| BP-143 | Claims | Lost/damaged carrier claim | Missing / benchmark gap | P2 | Claim value and recovery track | pending | 73.4.2 |
| BP-144 | Carrier invoice recon | Freight invoice vs bookings | Missing / benchmark gap | P2 | Freight variance explained | pending | 73.4.2 |
| BP-145 | Route/load planning | Fleet route/load optimization | Missing / benchmark gap | P3 | Only if own-fleet scope | pending | 73.4.2 |

### CAP-PIM — Product and master data (19)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-004 | Item type taxonomy | Stock/valuated/non-valuated/non-stock/service/backflush/gift/voucher/asset/RM/WIP/FG | Partial | P1 | Each type has correct stock/value/sale/purchase/accounting behavior | pending | none |
| BP-006 | Duplicate/merge | Safe merge with references and balances | Missing / benchmark gap | P2 | Merge preserves documents, loyalty and audit | pending | 73.4.4 |
| BP-007 | Effective dating | Rules/master versions by valid-from/to | Partial | P1 | Future rule activates without rewriting prior transactions | pending | 73.4.4 |
| BP-008 | Configuration promotion | Dev/test/prod config export/import/diff | Missing / benchmark gap | P1 | Promote approved config without data overwrite | pending | 73.4.9 |
| BP-009 | Setup advisor | Missing dependency -> guided create/fix path | Partial | P1 | Novice completes setup without dead end | pending | none |
| BP-010 | Data stewardship | Owner/steward/quality state/provenance | Partial | P2 | Critical masters have steward and quality exception queue | pending | none |
| BP-153 | Supplier submission | Vendor-scoped content flow | Partial | P1 | Real supplier isolation tested | pending | none |
| BP-156 | Resume partial jobs | Continue failed bulk job | Partial | P2 | No full re-upload needed | pending | 73.4.4 |
| BP-157 | Schedule/job ownership | Persistent publish/import schedules | Partial | P2 | Owner and status visible | pending | 73.4.4 |
| BP-158 | Product lifecycle | Launch/change/phase-out/EOL | Missing / benchmark gap | P2 | Retired product rules enforced | pending | 73.4.4 |
| BP-159 | Assortment/ranging | Store/channel/cluster assortment | Missing / benchmark gap | P2 | Sell/stock range governed | pending | 73.4.4 |
| BP-160 | Markdown lifecycle | Price markdown calendar | Missing / benchmark gap | P2 | Effective date and approval | pending | 73.4.4 |
| BP-161 | Taxonomy/categories | Hierarchies, classifications and inheritance | Partial | P1 | Family/category rules drive consistent attributes | pending | none |
| BP-162 | Localization | Language/market/channel content variants | Partial | P2 | Fallback and market overrides deterministic | pending | none |
| BP-163 | Data quality stewardship | Rule violations/owner/remediation queue | Partial | P1 | Owner can bulk remediate and revalidate | pending | none |
| BP-164 | Duplicate/merge | Golden product duplicate detection and safe merge | Missing / benchmark gap | P2 | References and external IDs survive merge | pending | 73.4.4 |
| BP-165 | Change management | Effective-dated product revision/change reason | Partial | P2 | Historic transaction keeps old product snapshot | pending | none |
| BP-166 | Digital shelf monitoring | Channel publish status/content drift | Missing / benchmark gap | P2 | Detect channel drift and stale external content | pending | 73.4.4 |
| BP-167 | Market intelligence | Competitor observations/alerts | Optional / scale-gated | P3 | Only after explicit scope/terms approval | pending | 73.4.4 |

### CAP-BUY — Procurement (10)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-032 | RFQ vendor invitation | Invite vendors and track response | Partial | P1 | Vendor assignment/status visible | pending | 73.2.1 |
| BP-033 | Supplier quotation | Unit/UOM/tax/freight/lead time/terms | Partial | P1 | Comparable total is unambiguous | pending | 73.2.2 |
| BP-034 | Quote comparison/award | Score/compare/select/reject | Partial | P1 | Award creates controlled follow-on | pending | 73.2.3 |
| BP-036 | Blanket/contract | Outline agreement and release PO | Missing / benchmark gap | P2 | Consumption against contract tracked | pending | 73.4.1 |
| BP-037 | Service procurement | Service PO + service entry sheet | Missing / benchmark gap | P2 | Invoice cannot exceed accepted service | pending | 73.4.1 |
| BP-039 | GRN | Partial/rejected/damaged receipt + QC | Partial | P0 | Accepted/rejected/open qty reconcile | pending | 73.2.4 |
| BP-042 | Supplier onboarding | Qualification/docs/approval | Missing / benchmark gap | P2 | Approved vendor only can be sourced | pending | 73.4.1 |
| BP-043 | Supplier scorecard | OTIF/quality/price/claims | Missing / benchmark gap | P2 | Score derived from transactions | pending | 73.4.1 |
| BP-044 | Supplier rebates | Accrual/settlement of incentives | Missing / benchmark gap | P3 | Only if commercial model uses rebates | pending | 73.4.1 |
| BP-045 | Supplier portal | Quote/ASN/invoice/status collaboration | Partial | P2 | Supplier sees only own records | pending | none |

### CAP-INV — Inventory and transfers (4)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-050 | Transfer | Dispatch/in-transit/receive | Partial | P0 | No duplication; partial discrepancy works | pending | 73.1.7 |
| BP-053 | Transfer tracked identity | Batch/serial on transfer | Partial | P0 | Tracked item transfer retains identity | pending | 73.1.7 |
| BP-054 | Cycle count | Blind count/recount/variance | Partial | P0 | Variance approved and posted once | pending | 73.1.8 |
| BP-059 | Owner stock | Per-owner inventory slices | Partial | P0 | No cross-owner allocation/movement | pending | none |

### CAP-FIN — Finance and tax (21)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-016 | Bank reconciliation | Statement import/match/exception | Partial | P0 | Bank closing = book +/- explained recon items | pending | 73.2.16 |
| BP-018 | Bank connectivity | Bank API/SFTP acknowledgment and status | Missing / benchmark gap | P2 | External status reconciles to payment | pending | 73.4.3 |
| BP-027 | Period close | Task/reconciliation/lock/reopen | Partial | P0 | Closed period blocks normal posting | pending | 73.1.10 |
| BP-028 | GL actor attribution | User/service identity on postings | Partial | P0 | Every GL row attributable to source actor/command | pending | 73.1.9 |
| BP-029 | Lease accounting | ROU asset/liability/lease payments | Missing / benchmark gap | P2 | Lease schedule reconciles to GL if in scope | pending | 73.4.3 |
| BP-030 | Treasury/liquidity | Cash positioning/forecast/risk | Missing / benchmark gap | P3 | Build only if treasury scope requires | pending | 73.4.3 |
| BP-195 | Acquisition | Create/capitalize from AP/PO | Partial | P1 | Cost/source link retained | pending | none |
| BP-199 | Impairment/revaluation | Accounting adjustments | Missing / benchmark gap | P2 | Only if policy requires | pending | 73.4.6 |
| BP-200 | CIP/componentization | Construction in progress/components | Missing / benchmark gap | P3 | Build if asset profile requires | pending | 73.4.6 |
| BP-202 | Maintenance work order | Corrective task/parts/labour | Partial | P2 | Cost and downtime captured | pending | 73.4.6 |
| BP-203 | Meter/counter | Usage-based maintenance | Missing / benchmark gap | P3 | Only if needed | pending | 73.4.6 |
| BP-205 | Asset leasing | ROU/lease liability | Missing / benchmark gap | P2 | If material leased assets | pending | 73.4.6 |
| BP-232 | Expense policy | Limits/category/rules | Partial | P1 | Exception approval visible | pending | 73.4.8 |
| BP-233 | Advance settlement | Advance and final settlement | Partial | P2 | Advance liability reconciles | pending | 73.4.8 |
| BP-234 | Travel request | Trip authorization/booking | Missing / benchmark gap | P3 | Only if needed | pending | 73.4.8 |
| BP-235 | Corporate card | Card feed/match | Missing / benchmark gap | P3 | Only if used | pending | 73.4.8 |
| BP-322 | E-invoice/IRN | Generate/store/cancel/retry | External gate | P0 | GSP/IRP sandbox + prod evidence | pending | 20.30 |
| BP-323 | E-way bill | Generate/update/cancel/retry | External gate | P0 | Live applicability/test evidence | pending | 20.31, 74.4.6 |
| BP-324 | GST returns/recon | GSTR extracts and reconciliation | Missing / benchmark gap | P1/P2 | If ERP owns statutory return process | pending | 73.4.10 |
| BP-325 | Export/LUT/IGST | International/export tax treatment | Partial | P1 | CA-approved scenarios | pending | 73.4.10 |
| BP-328 | Retention/audit | Statutory retention/legal hold | Partial | P1 | Qualified retention matrix | pending | 73.4.10 |

### CAP-CRM — Customer relationship and loyalty (12)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-093 | Customer 360 | Unified profile and history | Partial | P1 | Tier/points/orders/returns/cases visible | pending | 73.3.2 |
| BP-096 | Tier admin UI | Configure thresholds/benefits | Partial | P1 | Admin can change without API | pending | 73.3.1 |
| BP-099 | Redemption value UI | Configured currency per point | Partial | P0 | UI/server totals identical | pending | 73.1.5 |
| BP-100 | POS redemption fraud | OTP/velocity/manager approval | Partial | P0 | Till path enforces controls | pending | 73.1.4 |
| BP-101 | Return earn reversal | Reverse points earned | Missing / benchmark gap | P0 | Partial return reverses pro-rata | pending | 73.1.2 |
| BP-102 | Return redeem restore | Restore points, not cash | Missing / benchmark gap | P0 | Refund tender matches original funding | pending | 73.1.3 |
| BP-103 | Voucher POS use | Issued voucher redeem at till | Partial | P1 | Voucher validates/redeems atomically | pending | 73.2.10 |
| BP-104 | Gift card | Stored value lifecycle | Missing / benchmark gap | P1 | Liability/balance/refund reconcile | pending | 72.8 |
| BP-105 | Store credit wallet | Credit note wallet balance | Missing / benchmark gap | P1 | Separate from loyalty/gift | pending | 74.2.2 |
| BP-108 | Loyalty liability | Outstanding/breakage report + GL | Partial | P0 | Ledger liability reconciles | pending | 73.2.12 |
| BP-109 | Manual adjustment | Controlled point adjustment | Partial | P1 | Reason/approval required | pending | 73.2.11 |
| BP-110 | History at POS | Balance/tier/history context | Partial | P1 | Cashier sees authorized useful context | pending | 73.3.2 |

### CAP-HR — People and payroll (19)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-207 | Jobs/positions | Position and reporting structure | Missing / benchmark gap | P2 | Needed for enterprise HCM | pending | 73.4.7 |
| BP-208 | Recruitment | Requisition/candidate/hire | Missing / benchmark gap | P2 | If recruiting in scope | pending | 73.4.7 |
| BP-209 | Onboarding | Joiner tasks/docs/access | Missing / benchmark gap | P1 | Joiner checklist and identity link | pending | 73.4.7 |
| BP-213 | Payroll | Run/calc/export | Partial | P0/P1 | Gross-net-bank-GL reconcile | pending | 73.2.17 |
| BP-215 | Statutory payroll India | PF/ESI/PT/LWF/TDS etc. | Missing / benchmark gap | P1 | Qualified payroll owner acceptance | pending | 73.4.7 |
| BP-216 | Loans/advances | Employee loan/advance deduction | Partial | P2 | Schedule and payroll reconcile | pending | none |
| BP-217 | Benefits | Eligibility/enrolment/plans/deductions | Missing / benchmark gap | P2 | Employee eligibility and payroll deduction reconcile | pending | 73.4.7 |
| BP-218 | Compensation review | Merit/increment/bonus planning and approvals | Missing / benchmark gap | P2 | Effective-dated approved changes reach payroll | pending | 73.4.7 |
| BP-219 | Performance/appraisal | Goals/review/cycle | Partial | P2 | Lifecycle accepted | pending | 73.4.7 |
| BP-220 | Continuous feedback/360 | Feedback/check-ins/360 evaluation | Missing / benchmark gap | P3 | Build if talent scope requires | pending | 73.4.7 |
| BP-221 | Succession/talent | Talent pools/succession/readiness/career moves | Missing / benchmark gap | P3 | Build if enterprise talent scope requires | pending | 73.4.7 |
| BP-222 | Skills/competencies | Skill profile/proficiency/gaps | Missing / benchmark gap | P2/P3 | Feeds learning and role planning | pending | 73.4.7 |
| BP-223 | Training/learning | Training program/record/certification | Partial | P2 | Completion/expiry/compliance visible | pending | none |
| BP-224 | Grievance | Case/decision/privacy | Partial | P2 | Restricted evidence | pending | 73.D.2 |
| BP-225 | Disciplinary/HR case | Restricted employee relations workflow | Missing / benchmark gap | P2 | Need-to-know evidence and audit | pending | 73.4.7 |
| BP-226 | Onboarding/crossboarding | Joiner/internal move tasks/docs/equipment | Missing / benchmark gap | P1 | Checklist complete before lifecycle milestone | pending | 73.4.7 |
| BP-227 | Offboarding | Terminate/clearance/access revoke | Missing / benchmark gap | P1 | Identity disabled and assets cleared | pending | 73.4.7 |
| BP-228 | ESS/MSS | Employee/manager self-service | Partial | P1/P2 | Role-safe self-service journeys | pending | 73.4.7 |
| BP-229 | People analytics | Headcount/attrition/absence/payroll/talent metrics | Partial | P2 | Row-level privacy and metric definitions | pending | 73.4.7 |

### CAP-MFG — Manufacturing and planning (17)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-169 | BOM version/effective date | Controlled BOM revisions | Missing / benchmark gap | P2 | Historical order retains version | pending | 73.4.5 |
| BP-174 | Operation confirmation | Qty/time/machine/labour by operation | Missing / benchmark gap | P1 | Partial confirmation/WIP tracked | pending | 73.4.5 |
| BP-175 | Scrap/rework | Scrap/rework/repair flow | Missing / benchmark gap | P1 | Stock/cost variance correct | pending | 73.4.5 |
| BP-176 | Co/by-product | Multiple outputs | Missing / benchmark gap | P2 | Yield/cost allocation defined | pending | 73.4.5 |
| BP-177 | WIP accounting | WIP/variance/settlement | Partial | P1 | WIP + variance reconcile GL | pending | 73.4.5 |
| BP-178 | Batch genealogy | Input-to-output lot genealogy | Partial | P1 | Recall trace passes | pending | 73.4.5 |
| BP-180 | Alternate BOM/routing | Substitution/alternate process | Missing / benchmark gap | P2 | Controlled selection | pending | 73.4.5 |
| BP-181 | Finite capacity | Constraint-based schedule | Optional / scale-gated | P3 | Build only if needed | pending | 73.4.5 |
| BP-184 | Production inspection | In-process/final QC | Partial | P1 | QC blocks output release | pending | none |
| BP-185 | Return inspection | Customer return QC | Partial | P1 | Disposition controls restock | pending | none |
| BP-190 | Sampling | Sampling plans/AQL | Missing / benchmark gap | P2 | If inspection volume needs | pending | 73.4.5 |
| BP-191 | Supplier quality | Vendor quality score/SCAR | Missing / benchmark gap | P2 | Supplier issues feed score | pending | 73.4.5 |
| BP-192 | Calibration | Inspection equipment calibration | Missing / benchmark gap | P3 | Only if regulated measurements | pending | 73.4.5 |
| BP-193 | Recall | Forward/backward lot/serial trace | Partial | P1 | Complete customer/source trace | pending | 73.4.5 |
| BP-249 | Supply proposals | Purchase/transfer/production recommendations | Partial | P1 | Planner approves proposals | pending | 73.2.18 |
| BP-252 | Scenario planning | What-if versions | Missing / benchmark gap | P3 | Optional | pending | none |
| BP-253 | S&OP | Consensus demand/supply/finance plan | Missing / benchmark gap | P3 | Only if scale warrants | pending | none |

### CAP-SRV — Projects and service (8)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-237 | WBS/milestones | Structured project plan | Missing / benchmark gap | P2 | If project business in scope | pending | 73.4.8 |
| BP-238 | Project budget | Budget/forecast/actual | Partial | P2 | Variance visible | pending | 73.4.8 |
| BP-239 | Resource/timesheet | Resource assignment/time | Missing / benchmark gap | P2 | If services business | pending | 73.4.8 |
| BP-240 | Project billing | T&M/fixed/milestone billing | Missing / benchmark gap | P2 | Invoice linked to approved basis | pending | 73.4.8 |
| BP-241 | Project revenue recognition | Deferred/accrued project revenue | Partial | P2 | Revenue policy reconciles | pending | 73.4.8 |
| BP-243 | SLA/escalation | Service SLA timers/escalation | Missing / benchmark gap | P2 | Overdue queue/action | pending | 73.4.8 |
| BP-245 | Field service | Schedule/dispatch/parts/time | Missing / benchmark gap | P3 | Only if field service scope | pending | 73.4.8 |
| BP-246 | Knowledge base | Resolved case -> knowledge | Missing / benchmark gap | P3 | Optional | pending | 73.4.8 |

### CAP-API — Platform and integrations (40)

| Row | Capability | Target | Status | Priority | Acceptance test | Evidence | Checklist |
|---|---|---|---|---|---|---|---|
| BP-259 | Drill-through | Report row -> source | Partial | P1 | Authorized source opens | pending | 73.2.19 |
| BP-260 | Control totals | Detail/summary reconciliation | Partial | P0/P1 | Totals match ledger/source | pending | 73.2.19 |
| BP-262 | Semantic KPI catalog | Metric owner/grain/formula | Missing / benchmark gap | P2 | One meaning per KPI | pending | 73.4.9 |
| BP-263 | Data mart/read replica | Separate BI workload | Optional / scale-gated | P3 | Only after thresholds | pending | 73.4.9 |
| BP-268 | Workflow designer | Cross-module declarative workflow | Partial | P2 | Reuse one engine | pending | 73.4.9 |
| BP-269 | Delegation/substitution | Out-of-office approval delegate | Missing / benchmark gap | P2 | Time-bounded audited delegate | pending | 73.4.9 |
| BP-270 | SLA/escalation | Due dates/escalations | Missing / benchmark gap | P2 | Escalation deterministic | pending | 73.4.9 |
| BP-271 | Task inbox | Personal/role work queue | Partial | P1 | All pending actions visible | pending | 73.3.3 |
| BP-275 | OAuth2 client auth | Standard delegated/client auth | Partial | P2 | If enterprise integrations require | pending | 73.4.9 |
| BP-281 | EDI/SFTP batch | Enterprise batch interface | Missing / benchmark gap | P2 | If partner/bank needs | pending | 73.4.9 |
| BP-282 | Provider live acceptance | Real contract/provider tests | External gate | P0 | Per provider sign-off | pending | none |
| BP-287 | SSO | OIDC/SAML | Missing / benchmark gap | P2 | If enterprise clients require | pending | 73.4.9 |
| BP-288 | SCIM/provisioning | Directory user lifecycle | Missing / benchmark gap | P3 | If enterprise clients require | pending | 73.4.9 |
| BP-290 | Access recertification | Periodic review of grants | Missing / benchmark gap | P2 | Owner signs access review | pending | 73.4.9 |
| BP-292 | Audit archive/restore | Archive/legal hold/restore proof | Partial | P0 | Restore and verify archived evidence | pending | none |
| BP-293 | Security monitoring | Detection/alert taxonomy | Partial | P1 | Injected events detected | pending | none |
| BP-294 | Pentest/DAST | Independent application/API test | External gate | P0 | High/critical retested | pending | 49.10.4, 49.10.7 |
| BP-295 | Incident response | Contain/forensics/notification drills | Partial | P1 | Tabletop + technical drills | pending | none |
| BP-296 | WAF/edge | Managed WAF/origin lock | External gate | P0 | Production edge verified | pending | none |
| BP-305 | Feature flags | Controlled rollout | Missing / benchmark gap | P2 | Risky feature can be canaried | pending | 73.4.9 |
| BP-306 | Config transport | Promote config across env | Missing / benchmark gap | P1 | Diff/review/promote | pending | 73.4.9 |
| BP-312 | Ops alerts | Real webhook + responder/escalation | External gate | P0 | Alert delivery drill | pending | none |
| BP-314 | Restore drill | Isolated restore + reconcile | Partial | P0 | RPO/RTO measured | pending | 49.13.5 |
| BP-315 | WAF/hosting | Production edge/provider posture | External gate | P0 | Signed go-live decision | pending | none |
| BP-316 | Performance | Representative load/device tests | Partial | P1 | SLOs met | pending | none |
| BP-317 | Business UAT | Real user sign-off | External gate | P0 | Defect closure + sign-off | pending | 74.U.1 |
| BP-318 | Hypercare | Named owner/rollback triggers | External gate | P0 | Pilot support plan active | pending | none |
| BP-329 | Responsive UI | Desktop/tablet/phone layouts | Partial | P1 | Reference viewport pass | pending | none |
| BP-330 | RF scan UX | Scan-first WMS | Partial | P0/P1 | Real RF device pass | pending | 73.B.11 |
| BP-332 | Offline WMS | Defined degraded behavior | Missing / benchmark gap | P2 | If warehouse requires | pending | none |
| BP-333 | Silent printing | Printer bridge/QZ | Partial | P1 | Physical printer signed | pending | none |
| BP-334 | Barcode scanning | Code128/QR/device scanner | Partial | P1 | Real scanner signed | pending | none |
| BP-335 | Card terminal | Mapped terminal/payment settlement | External gate | P0 | Real terminal settlement pass | pending | none |
| BP-336 | Accessibility | WCAG 2.2 AA target | Partial | P1 | Keyboard/screen-reader/zoom evidence | pending | none |
| BP-338 | Migration framework | Dependency/mapping/recon/cutover | Partial | P1 | Counts/qty/value balance | pending | 73.4.10 |
| BP-342 | Privacy rights - employee/vendor | Subject rights beyond customer | Partial | P1 | Applicable subjects supported | pending | 73.4.10 |
| BP-343 | Consent/preferences | Purpose/channel preference | Partial | P2 | Marketing suppression enforced | pending | 73.4.10, 49.6.8 |
| BP-344 | Retention/archive | Policy/hold/archive/restore | Partial | P0/P1 | No uncontrolled deletion | pending | 73.4.10 |
| BP-345 | Test data masking | Safe non-prod datasets | Missing / benchmark gap | P1 | No production PII/secrets in test | pending | 73.4.10 |
| BP-346 | Data quality queue | Rule violations/owner/remediation | Missing / benchmark gap | P2 | Exceptions measurable 15. Reference basis 15.1 Supplied project evidence • README.md - project structure and technical reference index. • current-architecture.md - current topology, trust boundaries, transaction/idempotency caveats and owner isolation limits. • ai_handover.md - latest development/release state and recent committed/uncommitted/deployed work. | pending | 73.4.10 |
