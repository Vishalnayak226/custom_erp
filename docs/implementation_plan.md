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

We will build the ERP system in 12 progressive stages:

1.  **Stage 1 - Core Foundation**: Setup Tenant, User, RBAC, DocType registry, security rules, and Log Hub.
2.  **Stage 2 - Dynamic Configuration**: Build DocType builder UI, Dynamic Form Renderer, Label Engine, and Numbering Engine.
3.  **Stage 3 - Master Packages**: Initialize pre-configured industry masters (Jewelry, F&B, Automobile, Clothing presets) and CSV/Excel uploads.
4.  **Stage 4 - Procurement**: Implement Requisitions, RFQs, Quote comparisons, PO creation, and approvals.
5.  **Stage 5 - GRN and Inventory**: Enforce GRN reconciliation, barcode generation, Quality checks, and RTV returns.
6.  **Stage 6 - Warehouse and Transfer**: Manage Bin storage, putaway, dispatch transfers (TO), receipt transfers (TI), and compliance filings.
7.  **Stage 7 - POS and Sales**: Build POS profiles, Cash registers, offline caching checkout, and sales returns.
8.  **Stage 8 - Finance**: Implement Vendor invoices, 3-way matching, payments, and GL posting controls.
9.  **Stage 9 - Tax and Integrations**: Integrate GST/IRN, Pine Labs, Shopify, and message syncs.
10. **Stage 10 - Reports and Dashboards**: Setup drilldown report engines, dashboards, and automated exports.
11. **Stage 11 - QA and Go-Live**: End-to-end integration testing, migration templates, and cutover checklists.
12. **Stage 12 - Multi-Industry Scale**: Deploy SaaS monitoring, CI/CD pipelines, and multi-tenant subscriptions.

---

## 8. Definition of Done (DoD) for Security

An API or feature is not complete until:
1.  It passes authentication, authorization, input validation, and rate limiting middlewares.
2.  It is tested against unauthorized user, wrong tenant, and wrong location injection attempts.
3.  Heavy endpoints implement pagination, request timeout, or async queues.
4.  No secrets or credentials exist in source code or Git.
5.  Security logs write to the Admin Log Hub with a unique `correlation_id`.
6.  Production settings run with HTTPS, secure headers (HSTS, CSP), and disabled debug modes.
7.  UAT and pen-testing security validation checklists are signed off before go-live.
