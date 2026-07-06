# In-House Enterprise ERP: Technical Specification Document

This document serves as the single source of truth and comprehensive technical blueprint for the In-House ERP system. It is designed to allow multiple developer AI agents to work on separate modules independently.

Rather than building isolated, hardcoded screens for a single industry, this system establishes a **metadata-driven pluggable ERP Kernel** (inspired by Odoo, ERPNext/Frappe, and Nocobase). The core Go backend manages document definitions (DocTypes), sequence numbering, and security, while specific industry screens (e.g., jewelry procurement, retail POS, or logistics) are loaded dynamically as configuration packages.

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

### 2.1 The DocType Meta-Registry
The core metadata directory of the ERP.
- **`doctype_meta`**: Stores document type metadata definitions (e.g. DocType: `Brand`, `PurchaseOrder`, `SalesInvoice`).
- **`doctype_fields`**: Stores individual field definitions for each DocType (name, label, fieldtype e.g. text/int/decimal/link, validation rules, mandatory flags).
- **Generic CRUD Endpoint**: `/api/v1/doc/:doctype` processes all database writes and reads dynamically using metadata validation, eliminating the need to write custom controllers for separate forms.

### 2.2 DocType Builder UI (The Schema Customizer)
To make the ERP system highly customizable across different industries:
- **Schema Customization**: User panel allowing admins to dynamically add custom columns, toggle mandatory rules, and set display order.
- **Rename Labels**: Overrides the standard schema labels (e.g. renaming the "Polish" field to "Fabric" or "Engine Type" universally across POS and master tables).

### 2.3 Numbering Engine
Generates system sequences for barcodes, transactions, and vouchers.
- **Inputs**: Document Type, Legal Entity, Store Code/Location, Financial Year.
- **Rule Matrix**: Supports custom prefixes, separators (`-`, `/`), sequence padding width, and resetting rules (annual or monthly).
- **Format**: `<Document Type>/<Location or State>/<Financial Year>/<Running Number>` (e.g., `PR/HO/26-27/000001`).

### 2.4 Workflow & Approval Engine
Manages multi-tier approvals.
- **Parameters**: Document Type, Amount Slabs (e.g. 0-10k, 10k-100k, >100k), Location, Department.
- **Rules**: Supports L1/L2/L3 approval levels, dynamic approver source (role-based, reporting manager, named user), and automatic escalation after configured hours.
- **Re-Approval Trigger**: If amount, rate, quantities, or bank details are modified in a document after approval, reset status to `Draft` and trigger re-approval.

### 2.5 Validation Engine
A unified endpoint checking transaction rules.
- **Core Checks**: Negative stock, duplicate scan detection, missing tax IDs, out-of-tolerance purchase receipt quantities, and closed financial periods.

### 2.6 Inventory Ledger Engine
The absolute source of truth for stock quantities. Current inventory must be calculated as a running sum of immutable ledger transactions.
- **Supported Postings**: `GRN`, `SALE`, `SALES_RETURN`, `TRANSFER_OUT`, `TRANSFER_IN`, `STOCK_ADJUSTMENT`, `RTV` (Return to Vendor), `DAMAGE`, `LOCK`, `UNLOCK`.

### 2.7 Accounting Posting Engine
Maps document lines to General Ledger (GL) accounts dynamically.
- **Variables**: Document Type, Item Category, Tax Type, Place of Supply, Legal Entity, and Store Location.

### 2.8 Dynamic Label Engine
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

## 4. Security & Access Control (RBAC)

### 4.1 Permission Matrix Rules
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

## 5. Pluggable Modules & Business Logics

The following modules represent pre-configured DocType packages loaded on top of the dynamic core kernel:

### 5.1 Industry-Specific Master Templates
To support diverse business profiles, the system loads custom attribute presets on startup:
- **Jewelry Preset**: Brands, Styles, ProductCategory (with weight toggles), Colors, Polishes, and Sizes.
- **Food & Beverage Preset**: Batches, Expiry parameters, Net Weight, and Temperature thresholds.
- **Automobile Preset**: Make, Model, Engine Type, Fuel Type, and unique chassis VIN tracking.
- **Clothing Preset**: Apparel Brands, Styles, Size Codes (S/M/L/XL), Fabric Types, and Patterns.

### 5.2 Procurement & Purchase (Procure-to-Pay)
- **DocTypes**: `PurchaseRequisition`, `RFQ`, `VendorQuotation`, `PurchaseOrder`.
- **Quick PO Matrix Input**: Front-end grid component translating matrix values (Design x Color x Size) into standard PO line documents dynamically.
- **GRN & MRP Validation**: Validates received invoices against PO tolerances. Barcodes are generated at GRN completion for accepted items only.
- **RTV (Return to Vendor)**: Scans barcodes, verifies they are in stock, and dispatches them under `RTV Pending` status.

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

### 5.3 Inventory, Warehousing & Transfers
- **DocTypes**: `StockLocation`, `StockMovement`, `StockTransferOut`, `StockTransferIn`.
- **Stock Ledger**: Reconciles current stock by summing ledger transactions. Supports physical stock count spreadsheet uploads, calculating variances (`Sys Qty - Phy Qty = Diff`), and generating adjustment vouchers.
- **Logistics**: Picking lists, bin maps, and packaging box mappings.

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

### 5.4 Pluggable POS Checkout (Retail, F&B & Services)
- **DocTypes**: `POSProfile`, `POSInvoice`, `CashOpeningEntry`, `CashClosingEntry`.
- **Cash Opening & Closing Register**: Enforces drawer session control. Tethers sessions to float counts and tracks cashier cash variances at shift closing.
- **Offline Caching Engine**: Utilizes IndexedDB for client-side product catalog, customer details, and price rules storage. Completes transactions offline and syncs back automatically using UUID idempotency keys.
- **Extensible Layouts**:
  - *Retail (Fashion/Jewelry)*: Enforces high-speed barcode scanning, loyalty card points redemption, and coupon limits.
  - *F&B (Restaurant)*: Dynamic table seating configuration, kitchen ticket (KOT) print routing, and bill split allocations (by item or seat).
  - *Services (Spa/Clinics)*: Handles calendar booking integration and provider commission logs.

---

## 6. Finance, Accounting & GST Controls

### 6.1 Double-Entry GL Mapping
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

## 7. Integrations & Central System Log Hub

### 7.1 Mapped API Channels
- **Shopify, Unicommerce, Pine Labs, OCAPI, CleverTap**: Payload sync maps with masked credentials.
- **GST e-Invoice & IRN**: Auto-generates IRNs on dispatch and stores ack codes/signed QR payloads.

### 7.2 Centralized System Log & Exception Dashboard
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

## 8. Implementation Sequence & Exit Criteria

```
+---------------------------------------------------------------------------------------+
|  Stage 1: Core ERP Kernel & DocType Registry (API framework, dynamic CRUD router)     |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 2: Dynamic Form Rendering Engine (JS interpreter rendering from DocType meta)  |
+---------------------------------------------------------------------------------------+
                                          |
                                          v
+---------------------------------------------------------------------------------------+
|  Stage 3: Pluggable Industry Masters (Jewelry, F&B, Automobile, Clothing presets)     |
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
