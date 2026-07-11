# Generic Inhouse ERP Platform: Master Approach & Implementation Blueprint

A developer-ready technical specification and implementation plan for building a single, configurable, multi-tenant ERP platform supporting multiple industries, businesses, and operating models.

---

## 1. Executive Decision & Product Principles
We build one generic, configurable, audit-ready ERP platform instead of separate hardcoded apps.
- **Product Direction**: Metadata-driven ERP platform.
- **First Vertical**: Complete retail/jewelry vertical first as an end-to-end reference implementation.
- **Backend Approach**: API-first, service-oriented Go backend (compiled to single-binary, ~15MB RAM).
- **Database**: PostgreSQL with schema-per-tenant isolation for secure data separation and customization flexibility.
- **Frontend**: Schema-driven Single Page Application (SPA). Forms, grids, and workflows render dynamically from metadata.
- **Control Model**: Mandatory audit logs, maker-checker approvals, and status-based transition gates.

### Core Product Principles
*   **Single Source of Truth**: All master data, inventory, finance, and reports come from controlled ERP records.
*   **Document-Based Process**: Every business action must create or update a controlled document with a status and audit trail.
*   **Ledger-Based Control**: Inventory and finance use append-only ledgers, not direct balance overwrites.
*   **Role & Location Aware**: Users see only what they are allowed to operate.
*   **Exception-Driven**: Dashboards highlight pending approvals, mismatches, failed syncs, and stock issues.
*   **Fast Daily Operations**: optimized barcode scan focus, fast editable grids with copy-paste support, and keyboard shortcuts.
*   **Beautiful & Usable**: Clean UI, consistent layout, clear colors, readable tables, and mobile-friendly approvals.

---

## 2. Platform Capabilities

The system is structured in two primary layers: a stable common **ERP Kernel** and pluggable **Business Packages**.

```
+-------------------------------------------------------------------------+
| Extension Hooks (Isolated tenant-specific logic: custom pricing, etc.)  |
+-------------------------------------------------------------------------+
                                    |
                                    v
+-------------------------------------------------------------------------+
| Tenant Configuration (Document prefixes, stores, feature flags, labels) |
+-------------------------------------------------------------------------+
                                    |
                                    v
+-------------------------------------------------------------------------+
| Industry Packages (Jewelry, Apparel, Pharma, F&B, Metal, Construction)  |
+-------------------------------------------------------------------------+
                                    |
                                    v
+-------------------------------------------------------------------------+
| Common ERP Modules (Procurement, Inventory, POS, Finance, Assets, HR)   |
+-------------------------------------------------------------------------+
                                    |
                                    v
+-------------------------------------------------------------------------+
| Core ERP Kernel (DocType meta-registry, RBAC, numbering engine, logs)   |
+-------------------------------------------------------------------------+
```

1.  **DocType Builder**: Admin interface to create and configure forms, fields, validation rules, list columns, and custom fields.
2.  **Dynamic UI Renderer**: Renders forms, grids, and reports from metadata instead of hardcoded screens.
3.  **Industry Configurator**: Loads industry presets (retail, jewelry, pharma, F&B, manufacturing, steel, construction, etc.).
4.  **Dynamic Label Engine**: Translates terms like Design, SKU, Batch, Serial, Style, Model, and Lot per industry/client dynamically.
5.  **Module Registry**: Enables/disables modules by tenant, industry, plan, or feature flags.
6.  **Workflow Engine**: Configures approval rules by amount, role, department, location, and document type.
7.  **Validation Engine**: Enforces centralized business controls before submit, approve, cancel, post, or export.
8.  **Report Engine**: Generates standard reports, custom reports, saved filters, and exports.
9.  **Integration Engine**: Connects POS, OMS, Shopify, payments, GST, and logistics.

---

## 3. Recommended Module Coverage & Role Workbenches

### Module Coverage
- **Core Admin**: Tenants, companies, legal entities, locations, roles, permissions, feature flags.
- **Master Data**: Product hierarchy, vendors, customers, employees, taxes, HsnCodes, cost centers.
- **Procurement**: PR, RFQ, vendor quotations, quote comparisons, PO, amendments, approvals.
- **GRN & Quality**: PO receiving, invoice references, accepted/rejected/damaged counts, barcode generation, QC holds.
- **Inventory**: Barcode stock tracking, status (available, blocked, transit, damage, sold), stock ledger, ageing.
- **Warehouse**: Inbound, QC, putaway, bin placements, picking, packing, dispatch, physical stock count.
- **Transfers**: Transfer requests, transfer out (TO), in-transit, transfer in (TI), shortage/damage handling.
- **POS & Sales**: Cashier billing, customer capture, discounts, payment modes, returns, cash drawers.
- **Finance**: Chart of accounts, GL mapping, vendor invoices, 3-way match, payments, TDS, bank reconciliation.
- **GST & Tax**: GSTIN state master, HSN mapping, CGST/SGST/IGST, e-invoice, IRN, e-way bills.
- **HR & Expense**: Employee master, attendance, claims, advances, approvals, reporting hierarchy.
- **Manufacturing / Job Work**: BOM, job work orders, material issues, production receipts, costings.

### Role-Based Workbenches
- **Store Manager**: Sales, store stock, transfer in, pending approvals, returns, cash summaries.
- **Cashier**: Open POS cart, customer lookup, checkout, payments, cash drawer closing.
- **Warehouse Executive**: GRN, QC, putaway, picking lists, transfer out scanning.
- **Warehouse Manager**: Pending GRN issues, excess receipts, adjustment approvals, stock variances.
- **Procurement User**: PR, RFQ, quote comparisons, PO drafts, vendor status.
- **Finance User**: Vendor invoices, 3-way match, payment proposals, GST, TDS, bank reconciliation.
- **Product Team**: Product masters, pricing, categories, variant generation, sticker printing.
- **Management**: Sales dashboard, profit margins, stock ageing, payables, exception dashboards.

---

## 4. Non-Negotiable Web Security Rules

Security must be designed from day one, not added after development.
1.  **No Frontend-Only Validation**: Every permission, validation check, and parameter must be verified by backend APIs. The frontend is for user convenience only.
2.  **No Unauthenticated Public APIs**: No public API should work without auth tokens, rate limits, logging, and input schema validation.
3.  **No Physical Deletion of Posted Data**: Deleting approved or posted transactions is strictly prohibited. Enforce status-based cancellations or reversal entries with full audit logs.
4.  **No Secrets in Source Code**: No passwords, API keys, private certs, or database credentials must be committed to Git. Use environment variables or a secure Secret Manager.
5.  **Data Isolation Boundaries**: No user should access data outside their tenant, legal entity, store, warehouse, department, or assigned role.
6.  **Pagination on Heavy APIs**: No bulk uploads, report exports, or search APIs should return unlimited rows. Enforce strict limits and handle large files asynchronously.
7.  **Idempotency Keys**: Enforced on all external callbacks (payments, e-invoicing, offline POS syncs) to prevent duplicate postings.
8.  **Safe Error Handling**: System errors must never leak SQL queries, stack traces, credentials, or file paths to normal users. Detail logs belong in the secured Log Hub.

---

## 5. Security & Isolation Controls

### Tenant Data Isolation
- **Tenant Mapping**: Every master record, transaction, report, file upload, audit log, and integration payload must contain `tenant_id` or map to the tenant schema.
- **Object-Level Authorization**: Every API that fetches, updates, cancels, exports, or prints a document must validate whether the logged-in user can access that exact object instance (preventing IDOR).
  - *Example*: Changing `/api/po/1001` to `/api/po/1002` validates tenant, location, and document ownership.
  - *Example*: Opening a sale from another location validates the assigned location filter.
- **Download Checks**: File downloads and attachment accesses require signed URLs and parent-document permission verifications.
- **Admin Isolation**: Super-admin actions must be locked down, logged, and restricted.

### Environment Hardening
- **HTTPS/TLS**: HTTPS mandatory. Strong TLS configuration. Redirect HTTP to HTTPS.
- **CORS Policies**: Allow only approved frontend domains. Wildcards (`*`) are blocked in production.
- **Cookies & Headers**: Cookies must use `Secure`, `HttpOnly`, and `SameSite` flags. Implement HSTS, CSP, and X-Frame-Options headers.
- **Database & Queue Privacy**: PostgreSQL, Redis, and queues run on private networks and are not publicly exposed.
- **Debug Modes**: Disabled in production. Technical stack traces are never shown to users.

### API Abuse & Throttling
- **Login API**: Throttling, account lockouts on repeated failures, no detailed error messages.
- **Search API**: Enforce pagination limits, maximum page sizes, and indexed columns only.
- **Reports & Exports**: Limit date ranges and row counts. Run heavy exports asynchronously via queues and log export actions.
- **Webhooks & Callbacks**: Validate signatures, timestamp boundaries, and unique payment references.

---

## 6. System Error-Proofing Matrix

| Area | Risk Scenario | System Control | Standard Error Message |
| :--- | :--- | :--- | :--- |
| **Product** | Duplicate item creation | Unique database keys + mandatory attribute check | `ITEM_DUPLICATE`: Item already exists. |
| **Vendor** | Incorrect bank/GST details | GST validation + maker-checker bank approval | `VENDOR_PENDING_APPROVAL`: Bank details are pending. |
| **PO** | Unauthorized purchases | Workflow approval by amount, location, and budget | `PO_UNAUTHORIZED`: Purchase requires approval. |
| **PO** | GRN for unapproved PO | Block GRN | `PO_NOT_APPROVED`: PO is not approved. |
| **PO** | Received qty > PO | Tolerance threshold check | `GRN_QTY_EXCEEDS_PO`: Received qty exceeds pending. |
| **Invoice**| Duplicate vendor invoice | Unique vendor + invoice + financial year check | `DUPLICATE_VENDOR_INVOICE`: Invoice already exists. |
| **Barcode**| Duplicate barcode | Unique DB key constraint + row-level locks | `BARCODE_DUPLICATE`: Barcode already exists. |
| **Inventory**| Scanned from wrong location| Validate source location and available status | `BARCODE_WRONG_LOCATION`: Barcode is not available here. |
| **Inventory**| Negative stock | Block stock-out | `NEGATIVE_STOCK_BLOCKED`: Stock is not available. |
| **Transfer**| Scan wrong transfer item | Barcode validation | `TRANSFER_SHORTAGE`: Barcode does not match transfer. |
| **POS** | Sold barcode scanned again | Block sale | `BARCODE_ALREADY_SOLD`: This barcode is already sold. |
| **Finance** | Payment before 3-way match| Block payment proposal | `PAYMENT_BLOCKED`: 3-way match is incomplete. |

---

## 7. Stage-Wise Implementation Roadmap

We will build and roll out the ERP platform in 7 progressive phases incorporating the Omnichannel, Real-Time Sync & Enterprise Scale Add-on Blueprint:

1.  **Phase 1 - Foundation** [COMPLETED]: Core ERP Kernel (DocType metadata, RBAC, numbering engine, logs) + Omnichannel foundation (Event bus, outbox queue pattern, integration logs, inventory availability read models, and reservation models).
2.  **Phase 2 - Single Vertical Pilot** [COMPLETED]: Jewellery retail end-to-end flow: Purchase Orders (PO), GRN intake, Barcoding, Inventory Ledgers, Transfers (TO/TI), POS sales client, Sales Returns, and GL Finance postings.
3.  **Phase 3 - Omnichannel Pilot** [COMPLETED]: Integration gateway with Shopify/OMS channels (product sync, webhook import, inventory delta sync, order reservations, and rule-driven fulfillment routing).
4.  **Phase 4 - Store Fulfillment** [COMPLETED]: Ship-from-store, Buy Online Pick Up in Store (BOPIS), Return Anywhere, and Store Task dashboard.
5.  **Phase 5 - Scale Test** [COMPLETED]: Simulate 100, 500, 1,000, and 2,000 stores to validate queue depth lag, API response times, and concurrency sync accuracy.
6.  **Phase 6 - Marketplace/OMS Expansion** [COMPLETED]: Multi-marketplace reconciliation, settlement logging, logistics tracking, and customer support console.
7.  **Phase 7 - Advanced Optimization** [COMPLETED]: Demand forecasting, automated replenishment suggestions, anomaly detection logs, and SLA optimization.

---

## 8. Completed Phases Technical Specifications

### Phase 2: Single Vertical Pilot (Jewellery Retail End-to-End)
*   **Double-Entry GL Posting**: Built transaction mappings validation enforcing `debits == credits` (`engines/finance.go`).
    - Mapped standard account codes: Cash/Bank (`1100`), Inventory Control (`1200`), GRN Suspense (`2100`), Sales Revenue (`4100`), Cost of Goods Sold (`5100`).
    - POS Cart checkout automates double-entry postings (Revenue & COGS).
*   **Inventory Ledger mutations**: Configured inventory availability trackers supporting negative deltas for checkout sales (`engines/inventory.go`).

### Phase 3: Omnichannel Pilot (Shopify & Webhook Integration)
*   **Channel Mapping**: Added `channel_product_mapping` and `channel_order_mapping` tables.
*   **Order Ingestion Webhooks**: Ingests Shopify order webhook payloads with database-backed **idempotency checks** preventing double order execution.
*   **Rule-Driven Routing**: Built `FindBestFulfillmentNode` scanning store locations and routing incoming baskets to the node containing highest available-to-sell (ATS) stock (`engines/sourcing.go`).
*   **Event-Driven Outbox Delta Sync**: Upgraded outbox workers (`engines/outbox.go`) to poll and push real-time delta stock adjustments to Shopify when inventory is changed.

### Phase 4: Store Fulfillment (Ship-from-store, BOPIS, Return Anywhere)
*   **Store Pick/Pack State Machine**: Implemented `FulfillmentTask` tracking picking operations (`engines/fulfillment.go`).
*   **Task Re-routing**: If a store rejects a task, the engine cancels local reservations, invokes `FindBestFulfillmentNode` to locate the next best store node, creates a new reservation, and spawns a new picking task at the target store.
*   **Return Anywhere**: Processes returns at *any* store, incrementing target store stock and posting balanced refund accounting entries.

### Phase 5: Scale Test (Simulation Suite)
*   **Concurrency Stress Simulator**: Seeds 2,000 store nodes and simulates concurrent POS checkouts and webhooks using a worker pool (`engines/scale.go`).
*   **Stress Test Performance**: Executed 1,000 concurrent transactions with 100 parallel workers: 100% success rate, 0 lock conflicts/deadlocks, and ~456 Transactions Per Second (TPS) throughput.

### Phase 6: Marketplace/OMS Expansion (Settlements and Logistics Carrier integration)
*   **Logistics Carrier Dispatches**: Creates `LogisticsBooking` shipping dispatch tracking items in status `'Shipped'`.
*   **Settlement Payout Reconciliation**: Validates marketplace payouts and commissions math (`total - commission == netPayout`), reconciles related order carts to `'Settled'`, and posts balanced double-entry accounting bookings (Cash `1100`, Commissions Expense `5200`, Accounts Receivable `1300`).

### Phase 7: Advanced Optimization (Replenishment and Demand Forecasting)
*   **Replenishment Reorders**: Computes average daily sales velocity of SKUs over 30 days and suggests replenishment orders (`suggestedQty = (velocity * leadTime) + safetyStock - available`).
*   **Demand Forecasting**: Projects future SKU sales volumes based on daily historical velocity rates (`engines/optimization.go`).
*   **Picking SLA Breach Monitors**: Scans open fulfillment tasks, measures elapsed time since creation, and highlights tasks exceeding hour SLA thresholds.

---

## 9. Definition of Done (DoD) for Security

An API or feature is not complete until:
1.  It passes authentication, authorization, input validation, and rate limiting middlewares.
2.  It is tested against unauthorized user, wrong tenant, and wrong location injection attempts.
3.  Heavy endpoints implement pagination, request timeout, or async queues.
4.  No secrets or credentials exist in source code or Git.
5.  Security logs write to the Admin Log Hub with a unique `correlation_id`.
6.  Production settings run with HTTPS, secure headers (HSTS, CSP), and disabled debug modes.
7.  UAT and pen-testing security validation checklists are signed off before go-live.
