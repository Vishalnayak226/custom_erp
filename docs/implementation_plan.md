# In-House Enterprise ERP: Technical Specification Document

This document serves as the single source of truth and comprehensive technical blueprint for the In-House ERP system. It is designed to allow multiple developer AI agents to work on separate modules independently. 

All UI wireframe samples shared in early cutovers represent visual guides only. The primary objective of this document is to define the **underlying business logic, database schemas, validation engines, API patterns, and accounting rules** required to build a configurable, ledger-backed, audit-ready system.

---

## 1. Architectural Philosophy & Coding Standards

### 1.1 Non-Negotiable Core Rules
1. **No Hard deletes**: Under no circumstances should approved or posted transactions be deleted. Use status-based cancellations or reversal entries.
2. **Database Transactions**: Multi-step postings (e.g., GRN to stock ledger, barcode sequence allocation, and GL invoice booking) must run in single transactions. Roll back on any failure.
3. **Optimistic & Row-Level Locking**: Lock upstream records (e.g., PO lines during GRN, barcode statuses during Transfers) to prevent double-receipts or race conditions.
4. **Idempotency Keys**: Enforced on all external webhooks, POS payment callbacks, and GST tax filings.
5. **No Silenced Integration Failures**: Every API failure with Shopify, Unicommerce, Pine Labs, or GST must write to the integration log with status `FAILED` and support manual retry hooks.
6. **Backend Verification Only**: Never rely solely on frontend logic. Every rule, check, and permission must be verified in backend APIs.

### 1.2 Ultra-Lightweight Compiled Deployment Standard
To keep cloud hosting fees minimal for thousands of clients:
- **Single-Binary Execution (Go)**: Code must compile to a single binary (~15MB-30MB) with no interpreter or library requirements (unlike heavy Python or Node modules).
- **RAM Limits**: Server startup memory footprint must stay under ~15MB RAM per client instance, allowing high-density density hosting on cheap virtual private servers.
- **Micro-Containers**: Backend instances run inside scratch Docker images with zero bloated operating system packages.

---

## 2. Common Reusable Engines

To prevent hard-coding rules in separate forms, the following engines must be built as centralized services:

### 2.1 Numbering Engine
Generates system sequences for barcodes, transactions, and vouchers.
- **Inputs**: Document Type, Legal Entity, Store Code/Location, Financial Year.
- **Rule Matrix**: Supports custom prefixes, separators (`-`, `/`), sequence padding width, and resetting rules (annual or monthly).
- **Format**: `<Document Type>/<Location or State>/<Financial Year>/<Running Number>` (e.g., `PR/HO/26-27/000001`).

### 2.2 Workflow & Approval Engine
Manages multi-tier approvals.
- **Parameters**: Document Type, Amount Slabs (e.g. 0-10k, 10k-100k, >100k), Location, Department.
- **Rules**: Supports L1/L2/L3 approval levels, dynamic approver source (role-based, reporting manager, named user), and automatic escalation after configured hours.
- **Re-Approval Trigger**: If amount, rate, quantities, or bank details are modified in a document after approval, reset status to `Draft` and trigger re-approval.

### 2.3 Validation Engine
A unified endpoint checking transaction rules.
- **Core Checks**: Negative stock, duplicate scan detection, missing tax IDs, out-of-tolerance purchase receipt quantities, and closed financial periods.

### 2.4 Inventory Ledger Engine
The absolute source of truth for stock quantities. Current inventory must be calculated as a running sum of immutable ledger transactions.
- **Supported Postings**: `GRN`, `SALE`, `SALES_RETURN`, `TRANSFER_OUT`, `TRANSFER_IN`, `STOCK_ADJUSTMENT`, `RTV` (Return to Vendor), `DAMAGE`, `LOCK`, `UNLOCK`.

### 2.5 Accounting Posting Engine
Maps document lines to General Ledger (GL) accounts dynamically.
- **Variables**: Document Type, Item Category, Tax Type, Place of Supply, Legal Entity, and Store Location.

### 2.6 Dynamic Label Engine
Intercepts on-screen labels and replaces them using a database-mapped cache.
- **Parameters**: Original Label (case-insensitive exact match) -> Customized Display Name. Replacements must not affect technical IDs, APIs, or database schemas.

---

## 3. Data Model & Database Constraints

### 3.1 Table Schema Patterns
All tables must follow these standard designs:
- **Master Tables**: Must include an `active_status` boolean and standard auditing stamps (`created_at`, `created_by`, `updated_at`, `updated_by`).
- **Header Tables**: Contain metadata for a transaction (e.g., `po_header`, `invoice_header`). One row per business document.
- **Line Tables**: Contain individual line item details (e.g., `po_line`, `invoice_line`) linked via foreign key to the header table.
- **Ledger Tables**: Append-only, immutable logs tracking change balances (e.g., `inventory_ledger`, `accounting_ledger`).
- **Audit Tables**: Read-only tables capturing old value vs. new value mappings.

### 3.2 Key Database Constraints
1. **Barcode Uniqueness**: Globally unique across the entire system.
2. **Document ID Uniqueness**: Unique constraint on `DocumentNumber + DocumentType + LegalEntity + FinancialYear`.
3. **Vendor Invoice Uniqueness**: Unique constraint on `VendorID + VendorInvoiceNumber + FinancialYear`.
4. **Non-Negative Checks**: Database-level check constraints on quantities (`qty >= 0`) and prices (`amount >= 0`).
5. **Foreign Key Integrity**: Cascade deletes are blocked on all transactional lines.

---

## 4. Master Definition & Product Catalog

### 4.1 Master Definition Schemas
- **Brands & Sub Brands**: `Name` (Unique), parent brand dependencies, and auto-generated `Code`.
- **Styles & Sub Styles**: Product classification hierarchy.
- **Product Categories**: Includes `Is Weight` and `Is Net Weight` configuration flags.
- **Product Types**: Bound to categories; dictates applicable attributes.
- **Item Names**: Form mapping: `Product Category` (Dropdown), `Product Type` (Dropdown), `HSN Code` (Optional), `Sticker Type` (Optional), `Name` (Optional).
- **Colors & Secondary Colors**: Design classifications.
- **Polishes & Sizes**: Attributes used for jewelry-specific definitions.
- **HSN Codes**: Tax definitions mapping Code, Name, GST Rate, and Effective Date. Cannot be deleted once referenced in transactions.
- **Region Codes**: Geographic master.
- **Custom Attribute Masters**: Configuration parameters for dynamic attribute additions.

### 4.2 Product Schema & Catalog
- **Attributes**: Schema definitions specifying details (e.g., color, size, purity) and required status.
- **Designs**: Code, Name, Category, HSN, brand, style.
- **Combinations (SKUs)**: Generated variants mapping Design + Color + Size + Polish + Cost + MRP. Must have a globally unique `combinationId`.

---

## 5. Security & Access Control (RBAC)

### 5.1 Permission Matrix Rules
Backend API logic must check the following actions against the user's role-location mapping:
- **View**: Returns filtered records belonging to user-assigned stores/warehouses.
- **Create**: Allows saving `Draft` documents for authorized stores.
- **Edit Draft**: Allowed only for the creator while document status is `Draft` or `Returned`.
- **Edit Approved**: Generates a new document version, logs change reasons, and resets approval workflows.
- **Submit**: Checks validation engine rules. Updates status to `Submitted`.
- **Approve**: Updates document to `Approved` or shifts to the next workflow tier. Segregation of duties prevents creators from approving their own documents.
- **Reject**: Updates status to `Rejected`. Requires a mandatory rejection reason.
- **Cancel**: Validates downstream dependencies (e.g., cannot cancel PO if GRN exists). Enforces reversal postings.
- **Print**: Logs print counts and timestamps.
- **Export**: Restricts downloading to authorized users. Logs export row counts and queries.

---

## 6. Functional Module Specifications

### 6.1 Procurement & Purchase (Procure-to-Pay)
- **Purchase Requisition (PR)**: Internal request with mandatory cost center details.
- **RFQ & Quotation**: Matches verified vendors. Quotation comparison calculates total landed cost including duties and taxes. Selecting a non-lowest quote requires manager sign-off.
- **Purchase Order (PO)**: 
  - **Quick PO Matrix Grid**: Dynamic inputs matching combinations. Columns: *Design #, Category, Item Name, Style, Color, Polish, Size, Dummy7, Dummy8, MRP, Purchase Price (Purc P), Qty, Gross Weight (G.Wt)*.
  - **Splits**: Automatically splits PO lines by destination GSTIN if shipping across multiple states.
- **GRN & MRP Validation**:
  - Matches accepted PO quantity. Enforces over-receipt tolerances (e.g., max 2% over-delivery if configured).
  - Validation: Received MRP must match PO price rules. Mismatches block posting.
  - **Barcode Generation**: Barcodes are generated at GRN completion for accepted items only.
- **Purchase Return (RTV)**: Barcodes scanned for return must exist in the local store and map back to a GRN. RTV sets barcode status to `RTV Pending` and dispatches as `Stock Out`.

```
Procurement Error-proofing Matrix:
+------------------------------+--------------------------------------+-------------------------------------------------+
| Scenario                     | Control                              | Error Message                                   |
+------------------------------+--------------------------------------+-------------------------------------------------+
| PO not approved              | Block GRN creation                   | PO is not approved. GRN requires approved PO.   |
| Received qty > pending PO    | Block or route to tolerance approval  | Received quantity cannot exceed pending PO.     |
| Duplicate vendor invoice     | Unique vendor + invoice + FY check   | Vendor invoice number already exists.           |
| Inactive vendor selected     | Block PO release                     | Selected vendor is inactive or blocked.         |
| Rejected qty posted to stock | Prevent available stock posting      | Rejected qty cannot be posted as available.     |
+------------------------------+--------------------------------------+-------------------------------------------------+
```

### 6.2 Inventory & Warehouse Controls
- **Inventory Status Model**:
  - `Available`: Sellable or transferable.
  - `Blocked`: Reserved for orders.
  - `QC Hold`: Received under inspection.
  - `Rejected`: Failed GRN/QC inspection.
  - `RTV Pending`: Queue for vendor return.
  - `In Transit`: Dispatched but not received.
  - `Damaged`: Local damage (requires repair/write-off).
  - `Sold`: Dispatched to retail customers.
  - `Lost`: Shrinkage log.
- **Stock Movement**: Scan-based movement between local locations (Inward -> QC -> Main -> Damage).
- **Physical Count & Variance**: File upload parses barcode lists. Variance comparison report lists differences (`Sys Qty - Phy Qty = Diff`). Posting variances creates correction logs in the inventory ledger.

```
Inventory Error-proofing Matrix:
+------------------------------+--------------------------------------+-------------------------------------------------+
| Scenario                     | Control                              | Error Message                                   |
+------------------------------+--------------------------------------+-------------------------------------------------+
| Barcode not found            | Block transaction                    | Barcode does not exist in inventory.            |
| Barcode in wrong location    | Block transaction                    | Barcode is not available at this location.       |
| Barcode already sold         | Block sale/transfer                  | This barcode is already sold.                   |
| Duplicate barcode scan       | Block duplicate inside transaction   | Barcode is already scanned in this document.    |
| Negative stock attempt       | Block stock-out                      | Stock is not available for this transaction.    |
+------------------------------+--------------------------------------+-------------------------------------------------+
```

### 6.3 Transfers & compliance
- **Stock Transfer Out (TO)**: Scan-based dispatch. Enforces source barcode availability. Updates status to `In Transit`.
- **Stock Transfer In (TI)**: Scans inbound barcodes. Must match TO reference. Discrepancies generate shortage/damage logs.
- **GST IRN / E-Way Bill**: Branch transfers across states generate GST transfer invoices. Calls e-invoice API to retrieve IRN hash and signed QR code.

### 6.4 POS Checkout Terminal & Sales
- **Checkout Client**: Form fields: Mobile, Name, DOB, Gender, GSTIN.
- **Cart Grids**: Scans barcode -> fetches combination attributes -> calculates prices and tax -> updates cart grid table (*Code, Item, Rate, Qty, Discount, Cost, SGST, Amount*).
- **Bill Summary**: Computes Gross, GST, Sub-Total, and discount rules. Large font displays Total. Enforces payment matching (Tender total must equal invoice total).
- **Sales Return**: Sales returns must link to the original sales invoice. Enforces tax reversals matching original GST calculations.

---

## 7. Finance, Accounting & GST Controls

### 7.1 Double-Entry GL Mapping
The system automatically executes double-entry postings for the following events:
- **GRN**:
  - `Debit` Inventory Asset Account (based on cost)
  - `Credit` GR/IR Clearing Account
- **Vendor Invoice Posting**:
  - `Debit` GR/IR Clearing Account
  - `Debit` Input CGST / SGST / IGST Accounts
  - `Credit` Vendor Payables Account
- **Payment Settlement**:
  - `Debit` Vendor Payables Account
  - `Credit` Bank Account
  - `Credit` TDS Payable Account (if applicable)
- **Sale checkout**:
  - `Debit` Cash / Card / UPI Clearing Account
  - `Credit` Sales Revenue Account
  - `Credit` Output CGST / SGST / IGST Accounts
- **Cost of Goods Sold (COGS)**:
  - `Debit` COGS Account
  - `Credit` Inventory Asset Account

---

## 8. Sticker Printing Subsystem

- **Sticker Templates**: Configures printed label dimensions (e.g. 40x20mm), resolution (DPI), and orientation (Landscape/Portrait).
- **Printers Configuration**: Manages thermal printers (Type, Connection e.g. USB/Ethernet, DPI, Paper).
- **Print Stickers Client**:
  - *Bulk Inward Printing*: Auto-loads items based on GRN receipts, enabling bulk printing of barcode tags.
  - *Single Print*: On-demand search queue for individual item tags.

---

## 9. Integrations & Central System Log Hub

Every integration interface must log transaction payloads inside the database integration table:

### 9.1 Logging Schema (`integration_log`)
- `integration_log_id` (Primary Key)
- `integration_name` (e.g., Shopify, Pine Labs, GST e-Invoice)
- `direction` (Inbound / Outbound)
- `business_reference` (Invoice No, Barcode, etc.)
- `request_payload` (JSON payload, sensitive credentials masked)
- `response_payload` (JSON payload response)
- `status` (Success, Failed, Retrying)
- `error_code`, `error_message`
- `retry_count`
- `correlation_id` (UUID tracking actions across systems)

### 9.2 Centralized System Log & Exception Dashboard
For crash-proof resilience and instant observability, the system maintains a unified **Log Hub console** mapping all runtime exceptions:
1. **Middleware Panic Handler**: Catch Go router runtime crashes, log full stack trace mapping line number references to `system_error_logs`, and serve a standard error JSON response, keeping the binary online.
2. **Schema: `system_error_logs`**:
   - `log_id` (UUID Primary Key)
   - `tenant_id` (UUID client tracking)
   - `correlation_id` (UUID request mapping)
   - `severity` (Severity level badge: Panic / Error / Warning)
   - `module_source` (e.g. POS, RTV, Stock Ledger)
   - `error_message` & `stack_trace`
   - `timestamp`
3. **Log Hub Admin Panel**: User screen displaying all errors, search query filter by correlation ID, error stack trace drawer view, and `Retry` hooks to re-dispatch failed payloads.

---

## 10. Implementation Sequence & Exit Criteria

```
+---------------------------------------------------------------------------------------+
|  Stage 1: Foundation & Core Engines (Sequence, Audits, Dynamic Labels)                |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 2: Master Definitions & Attributes (Brands, Styles, Tax Codes)                 |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 3: Product Catalog & Schemes (Designs, Combinations variant generation)         |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 4: Procurement & Purchase (PR, PO Grid, GRN validation, Barcode generation)    |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 5: Inventory Control (Stock ledger, local stock movement scan logs)            |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 6: Transfers (Stock Transfer Out/In scanning, GST IRN/e-way integrations)      |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 7: POS checkout client (Open POS cashier checkout terminal, loyalty, checkout) |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 8: Finance & 3-Way Match (Vendor Invoices, GL double-entry bookings)            |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 9: Third-party integrations (Shopify, Pine Labs, OCAPI, CleverTap)             |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 10: MIS Reporting (Aging analysis, GST filings summaries, export tools)        |
+---------------------------------------------------------------------------------------+
```

### Exit Criteria per Stage:
- Stage 1-3 must pass unit validation, and verify schema migrations.
- Stage 4-7 must complete concurrency testing (parallel receipts and scans must block duplicate generation).
- Stage 8-9 must successfully test fallback retries (interrupted Pine Labs or GST API connections must recover cleanly via integration log hooks).
- Stage 10 must pass UAT testing using mock transactional data volumes.
