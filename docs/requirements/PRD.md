# Product Requirements Document (PRD)

Functional module inventory, user roles, workflows, and error-proofing requirements — the "what to build" document, at product level (not code level). For business rationale, see [BRD.md](BRD.md). For built-vs-planned status per item, this document cites [`../micro_checklist.md`](../micro_checklist.md) rather than duplicating it — that file is the current source of truth and changes constantly; this one describes the intended shape of the product and shouldn't need to change every time a checklist item closes.

**Sources**: consolidates this repo's own [`../specs/modules_overview.md`](../specs/modules_overview.md) and [`../specs/implementation_plan.md`](../specs/implementation_plan.md) with three richer external planning references (`Generic_Inhouse_ERP_Platform_Approach_Blueprint.docx`, `Inhouse_ERP_Master_Blueprint_Generic.docx`, `Inhouse_ERP_Platform_Functionality_Blueprint...docx`) maintained alongside this repo — those describe the full target platform in much greater depth than what's built today. **Status markers below (BUILT / PARTIAL / SPEC) are checked against `../micro_checklist.md`, not assumed from the planning references.**

---

## 1. Platform Capabilities (the Kernel every module builds on)

Per the planning references' consistent framing: an ERP built as a pile of hardcoded screens doesn't scale to multiple industries. This platform is built as a small set of reusable engines that every module uses, rather than reinventing its own version of each:

| Engine | Purpose | Status |
|---|---|---|
| DocType Meta Registry + Dynamic CRUD | Register a document type (fields, validations, lifecycle) as metadata, get a working API/form for free. | BUILT — the foundation everything else sits on. |
| RBAC Engine | Role/location/action-level access control on every operation. | BUILT. |
| Numbering Engine | Generate document numbers, barcodes, voucher numbers from configurable rules (prefix + location + FY + sequence). | BUILT (`engines/numbering.go`). |
| Workflow / Approval Engine | Amount-slab + role + location routing, maker-checker, escalation, re-approval on edit. | BUILT (`engines/approval.go`) — reused by every module that needs approval gating rather than each building its own. |
| Validation Engine | Central business-rule checks (stock, quantity, tax, duplicates, closed periods). | BUILT, distributed across each module's own validation rather than one central service — see PRD §5 (Error-Proofing) for the requirement this satisfies. |
| Audit Engine | Who changed what, old/new value, when. | BUILT (`engines/logs.go`), append-only. |
| Inventory Ledger Engine | Append-only stock movement ledger; current stock is derived, not directly overwritten. | BUILT (`engines/inventory.go`). |
| Accounting Posting Engine | Double-entry GL postings from business documents via configured mapping. | BUILT (`engines/finance.go`). |
| Dynamic Label Engine | Rename UI terms per tenant/industry without touching code. | BUILT (`engines/labels.go`). |
| Report Engine | Reusable report definitions rather than one-off report code per report. | PARTIAL — 5 reports built against the ~80-report catalog the planning references specify (see §4). |
| Integration Log Engine | Every external API call logged, retryable, correlation-traceable. | BUILT (`engines/outbox.go` + per-integration logging). |
| Notification Engine | In-app/email/SMS alerts on workflow and exception events. | SPEC only — not built; the closest built equivalent is Stage 17.10's ops alerting (`engines/alerting.go`), which targets operators, not end users. |

## 2. User Roles and Workbenches

Roles in the current build (see `../ERP_BLUEPRINT.md` §3 for enforcement detail): **HR/Admin** (full access, MFA-gated), **Manager** (location-scoped operational + approval access), **Cashier** (POS-focused), **System** (service account).

The planning references describe a richer **role-based workbench** concept — each role's home screen surfaces exactly what that role needs to act on, not a generic dashboard:

| Role | Workbench should surface | Status |
|---|---|---|
| Cashier | Open POS, customer lookup, current cart, payment, cash closing, sales history | POS screen BUILT; a unified "workbench" home view is SPEC. |
| Store/Branch Manager | Sales, store stock, transfer-in, pending approvals, manual bills, returns | Partially covered by existing Fulfillment/Approvals screens; not unified into one workbench. |
| Warehouse staff | GRN, pending transfers, pick queue | GRN/transfer screens BUILT; pick-queue workbench is SPEC (no bin/pick/pack workflow built — see §4). |
| Procurement user | PR, RFQ, quote comparison, PO draft, vendor status | BUILT as separate screens (RFQ comparison, PO creation), not a unified workbench. |
| Finance user | Vendor invoice, 3-way match, payment proposal, GST, bank reconciliation | BUILT as separate screens/reports. |
| HR/Admin | Employee, attendance, assets, expenses, store setup, user access | BUILT across HR/Assets/Expense screens. |
| Management | Sales, margin, stock ageing, cash, payables, exceptions | PARTIAL — the 5 built reports cover part of this; an exception/management dashboard is SPEC. |

## 3. Standard Document Lifecycle and Numbering

Every transactional document in this system follows the same status model (not a bespoke one per doctype) and the same numbering pattern — this consistency is a deliberate platform decision, not an accident:

**Status flow**: `Draft → Submitted → Pending Approval → Approved → Partially Processed → Completed/Closed → Cancelled/Reversed`. No hard delete of an approved/posted document — ever; correction happens via cancellation/reversal with a reason, always audit-logged. This is BUILT via the soft-delete tombstone (Stage 17.1) and the approval engine's status transitions.

**Numbering pattern**: `<DocumentType>/<Location or State>/<FinancialYear>/<RunningSequence>` (e.g. `PO/KA/26-27/000001`) — configurable prefix/location/FY/padding, database-locked to prevent duplicate numbers under concurrency, and cancelled numbers are never reused. BUILT (`engines/numbering.go`, prefix config UI).

## 4. Module-by-Module Functional Requirements

### 4.1 Core Kernel & Master Data
**Requirement**: masters (organization, location, item, vendor, customer, employee, tax/GL) must be stable and approval-gated before transactions can reference them — "master data first." **Status**: BUILT, including Stage 17.9's Location/LegalEntity/Department/CostCenter masters.

### 4.2 Point of Sale (POS)
**Requirement**: barcode/SKU billing, GST calculation, loyalty earn/redeem, GL posting — plus (per the planning references' fuller POS Architecture) a POS profile per store, cash drawer open/close with expected-vs-actual reconciliation, offline billing with an idempotent sync queue, and industry extensions (restaurant KOT/table/split-bill, retail barcode scan). **Status**: BUILT for the synchronous online case only. **SPEC, not built**: offline queue, cash session model, KOT/split-bill.

### 4.3 Procurement
**Requirement**: Purchase Requisition → RFQ → Vendor Quote → Quote Comparison → Purchase Order → PO Amendment (version-controlled) → GRN → 3-Way Match → Payment, each step validated against the one before it (a GRN cannot exist against an unapproved PO; a payment cannot proceed without a completed 3-way match). **Status**: BUILT end-to-end (Stage 17.6-17.8), including the DB-level duplicate-vendor-invoice constraint the planning references call out specifically.

### 4.4 GRN, Barcode, and Purchase Return
**Requirement**: barcode/stock-identity generation only for *accepted* GRN quantity (never rejected/damaged), globally unique and never reused even if the GRN is later cancelled, MRP validated against the PO. **Status**: BUILT for GRN and stock-identity generation; a dedicated MRP-mismatch approval route is not separately implemented (handled as a general GRN validation).

### 4.5 Inventory & Warehouse
**Requirement**: ledger-first design (current stock is *derived* from the ledger, never the ledger derived from a stock counter), a full inventory status model (Available/Blocked/QC Hold/In Transit/Damaged/Sold/Returned), and full warehouse operations (putaway, bin management, pick/pack, cycle count). **Status**: BUILT — Available-to-Sell read model, barcoded stock counts, temporary reservations, physical count/variance. **SPEC, not built**: bin-level location tracking, putaway, and pick/pack workflows — current warehouse scope stops at receiving and transfers.

### 4.6 Transfers
**Requirement**: Transfer Out (source scans, stock → In Transit) → Transfer In (destination scans, shortage/damage captured as an exception, not silently reconciled) → closure. **Status**: BUILT (Stage 17.6, `engines/transfer_orders.go`), including the shortage/damage exception behavior explicitly required by the planning references rather than auto-reconciling.

### 4.7 Sales & Returns (beyond POS)
**Requirement**: sales returns reference the original invoice (no orphan returns), refund follows the original payment mode, returned stock goes to QC/Return-Received rather than immediately back to Available. **Status**: BUILT as part of the POS/checkout flow; not a separately specified return-without-sale exception path.

### 4.8 Finance & GL
**Requirement**: chart of accounts, GL mapping by document/transaction/tax/location type, accounting-period close control (no posting into a closed period without special approval), GST calculation and enforcement (CGST/SGST intra-state, IGST inter-state) at the point of PO creation and checkout. **Status**: BUILT. **SPEC, not built**: e-invoice/IRN generation and e-way bill (the planning references specify this in detail — GSTIN master, IRN request/response logging, retry, e-way threshold logic — none of it implemented; GST is calculated and posted, not filed).

### 4.9 Reports
**Requirement**: a reusable report-definition engine (filters, columns, role-based access, export logging, drilldown to source document) rather than hand-coded reports, covering the ~80-report catalog the planning references enumerate across Procurement, Inventory, Warehouse, Sales/POS, Finance, GST, Audit, and Assets/Expense/HR categories. **Status**: PARTIAL — 5 reports built (Current Stock, Sales Register, Vendor Ledger, Payables Ageing, PIM Dashboard), prioritized by actual need rather than built exhaustively; no generic report-definition engine, each report is purpose-built.

### 4.10 HR & Payroll
**Requirement**: employee master, attendance, leave, and an export a business can feed into their own statutory payroll process. **Status**: BUILT as described — by design, this module does not run payroll or file statutory returns (see BRD.md §5).

### 4.11 Fixed Assets
**Requirement**: PR → PO → GRN → Vendor Invoice → Capitalization → Depreciation → Transfer → Disposal, with net book value (not original cost) reported once disposed. **Status**: BUILT.

### 4.12 Expense Management
**Requirement**: Claim → Manager Approval → Finance Verification → Payment → Accounting, reusing the approval engine rather than a bespoke workflow, with duplicate-bill and date-window controls. **Status**: BUILT.

### 4.13 CRM / Loyalty
**Requirement**: an append-only points ledger (balance always derivable as `SUM(Earn) − SUM(Burn)`, never a mutable counter — audit-safe by construction). **Status**: BUILT as a scoped MVP — no campaigns, segmentation, or vouchers (the planning references' fuller CRM scope — customer source/purpose tracking, birthday/anniversary triggers — is SPEC only).

### 4.14 Manufacturing / Job Work
**Requirement**: BOM → Work Order → Material Issue → Production Receipt → QC → Costing. **Status**: BUILT as a scoped MVP (single-level BOM, linear Production Order) — no routing/work centers, MRP, or QC gates.

### 4.15 Multi-Industry Configuration
**Requirement**: industry-specific master fields, validations, vocabulary, and reports loaded as configuration, not code — e.g. Jewellery's Design/Combination/Barcode model vs. Apparel's Style/Size-curve vs. Pharma's batch/expiry/FEFO requirements (see the planning references' industry package table for the full per-industry detail). **Status**: PARTIAL — 4 of 10+ specified industry profiles wired in (Jewelry, F&B, Automotive, Clothing); Pharma, Construction, Metal/Steel, Medical Devices, Semiconductors, Agriculture remain SPEC only.

### 4.16 PIM & Omnichannel
**Requirement**: enrich product content through an approval workflow, score completeness per channel, and publish to real e-commerce platforms without the outbound call ever blocking the transaction that triggered it. **Status**: BUILT and unit-tested against each platform's real API shape (Shopify/BigCommerce/Magento); live-store verification is the one remaining gap, blocked on real credentials only the account owner can supply (see [`../operations/connector_live_verification.md`](../operations/connector_live_verification.md)).

### 4.17 Bulk Upload
**Requirement**: every bulk import uses a versioned template, validates the file structure before row validation, returns row-level errors with the exact reason (never a silent partial import unless explicitly allowed), and logs the upload batch (rows attempted/succeeded/failed, uploaded by/when). **Status**: BUILT as a reusable import framework with CSV formula-injection sanitization (Stage 17.2) and job-tracked preview.

### 4.18 Sticker/Barcode Printing
**Requirement**: configurable sticker templates, printer mapping, print-by-GRN/barcode-range/PO, and print history with reprint reason. **Status**: BUILT — text labels, not scannable symbology (stated limitation, unverifiable in this dev environment without physical hardware).

### 4.19 Security & Multi-Tenancy
**Requirement**: schema-per-tenant isolation, MFA for privileged roles, permission checks enforced server-side (never trusting frontend-only validation), field-level restriction on sensitive data (vendor bank details, cost/margin). **Status**: BUILT — TOTP MFA for HR/Admin, JWT bearer auth with expiry, account lockout, CORS allowlist, parameterized SQL throughout.

### 4.20 Deployment, Backup, and Incident Response
**Requirement**: promote a change dev → test → live with a build/test gate and one-command rollback; scheduled backup with a drilled restore procedure; alert a human when something breaks. **Status**: BUILT — `promote.ps1`/`manage.ps1`, drilled backup/restore, and alerting/runbook, with the one remaining gap being real escalation-contact/webhook configuration only the operator can supply.

## 5. Error-Proofing Requirements (a first-class product requirement, not an afterthought)

The planning references treat error-proofing as its own product layer — the system should prevent a wrong transaction *before* it becomes a finance, inventory, tax, or audit problem, not detect it afterward. This is a representative sample of the full matrix (see the planning references for the complete list per module); status reflects what's actually enforced in this codebase today:

| Risk | Required Control | Status |
|---|---|---|
| GRN attempted against an unapproved PO | Block, with the exact reason named | BUILT |
| Received quantity exceeds pending PO quantity | Block or route to tolerance approval | BUILT |
| Duplicate vendor invoice (same vendor + invoice number + financial year) | Database-level unique constraint | BUILT |
| Rejected GRN quantity posted as available stock | Block — only accepted quantity becomes available stock | BUILT |
| Negative stock | Block stock-out | BUILT |
| Same barcode scanned twice in one transaction | Block duplicate scan | Not separately verified as a distinct control (barcode inventory model exists; explicit same-transaction double-scan block not confirmed in code review) |
| Creator approves their own document | Maker-checker segregation | BUILT (`engines/approval.go`) |
| Posting into a closed accounting period | Block | BUILT (Stage 17.4) |
| Duplicate offline POS sync after a network retry | Idempotency key | N/A — offline POS not built (§4.2) |
| External integration call fails | Log the failure, never silently drop it, make it retryable | BUILT (integration event outbox + retry) |

## 6. Explicitly Deferred (tracked, not forgotten)

- **Data-entry UX (dropdowns/autosuggest)** — flagged as a real usability risk for non-technical end users; not yet scoped into implementation-sized work. See `../micro_checklist.md` Stage 18.
- **AI Assist** — out of scope pending a dedicated product/design conversation (see BRD.md §5).
- **Full warehouse operations** (bin/putaway/pick/pack), **offline POS**, **e-invoice/IRN**, and the **~75 remaining report definitions** — all specified in detail in the planning references, none built. Not silently dropped: each is named explicitly here and in the relevant module section above so a future scoping pass starts from an accurate list, not a guess.
