# Generic Inhouse ERP Platform: Approach & Implementation Blueprint

A developer-ready technical specification and implementation plan for building a single, configurable, multi-tenant ERP platform supporting multiple industries, businesses, and operating models.

---

## 1. Executive Decision
We build one generic, configurable, audit-ready ERP platform instead of separate hardcoded apps.
- **Product Direction**: Metadata-driven ERP platform.
- **First Vertical**: Complete retail/jewelry vertical first as an end-to-end reference implementation.
- **Backend Approach**: API-first, service-oriented Go backend (ultra-lightweight, compiles to single-binary, ~15MB RAM).
- **Database**: PostgreSQL with schema-per-tenant isolation for secure data separation and customization flexibility.
- **Frontend**: Schema-driven Single Page Application (SPA). Forms, grids, and workflows render dynamically from metadata.
- **Customization**: Configuration-based, feature flags, metadata, and dynamic hooks.
- **Control Model**: Mandatory audit logs, maker-checker approvals, and status-based transition gates.

---

## 2. Product Strategy: One Solution for All
The ERP is structured in two primary layers: a stable common **ERP Kernel** and pluggable **Business Packages**.

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

---

## 3. Recommended Build Approach
The build is executed in phases, establishing the core foundation before expanding:

*   **Phase 0: Blueprint**: Freeze architecture, schemas, and API conventions.
*   **Phase 1: Kernel**: Core engine setup (DocType registry, RBAC, numbering, audit log hub).
*   **Phase 2: Master Data**: Location, product, vendor, customer, and GL masters.
*   **Phase 3: First Vertical**: End-to-end retail/jewelry flow (PR -> PO -> GRN -> Barcode -> Transfer -> POS -> Return -> Finance).
*   **Phase 4: Reporting**: Reusable report definition renderer.
*   **Phase 5: Integrations**: Payments, GST IRN, Shopify/OMS.
*   **Phase 6: Multi-Industry**: Add industry presets (Pharma, Metal, Construction).
*   **Phase 7: SaaS Scale**: Provisioning workflows and subscription feature flags.

---

## 4. Target Architecture & API Security

```
                                +-----------------------------+
                                |         API Gateway         |
                                |  (Auth, Rate Limit, Tenant) |
                                +-----------------------------+
                                               |
                                               v
                                +-----------------------------+
                                |      ERP Kernel Services    |
                                |  (DocType, Numbering, etc.) |
                                +-----------------------------+
                                               |
                        +----------------------+----------------------+
                        |                      |                      |
                        v                      v                      v
            +---------------------+  +-------------------+  +------------------+
            | PostgreSQL Database |  |   Redis / Queue   |  |  Object Storage  |
            |  (Tenant Schemas)   |  |   (Metadata Cache)|  |  (Attachments)   |
            +---------------------+  +-------------------+  +------------------+
```

- **API Rate Limiting**: The gateway throttles automated calls (like loops in Postman or cURL scripts) using Redis token buckets (e.g. limit standard CRUD calls to 60/min per user; public logins to 5/min per IP). Rejections return `429 Too Many Requests`.
- **Tenant Resolver**: Maps subdomains/tokens to the correct PostgreSQL tenant schema. Verified strictly via JWT backend signatures (IDOR-safe).
- **Intellectual Property Protection**: Production Go binaries are compiled using symbol table strips (`go build -ldflags="-s -w"`), and JS frontend files are minified/obfuscated to hinder reverse-engineering. No source maps are exposed in production.
- **Log Hub**: Central dashboard capturing integration payloads and Go panic recoveries.

---

## 5. Core ERP Kernel
No functional module bypasses the Kernel. It consists of the following engines:
1.  **DocType Meta Registry**: Dynamic document, field, and layout definitions.
2.  **Dynamic CRUD Handler**: Unified endpoints for `/api/v1/doc/:doctype`.
3.  **RBAC Engine**: Location and tenant-level access control.
4.  **Numbering Engine**: Formats document sequences (`DOC/STORE/FY/SEQ`).
5.  **Workflow Engine**: Amount slabs, maker-checker, and correction flows.
6.  **Validation Engine**: Checks negative stock, duplicate scans, and tax codes.
7.  **Audit Engine**: Logs old vs. new values.
8.  **Inventory Ledger Engine**: Immutable stock ledger postings.
9.  **Accounting Posting Engine**: Auto-maps transactions to GL debits/credits.
10. **Label Engine**: Dynamic on-screen terminology translations.
11. **Report Engine**: Compiles drilldown exports from report metadata.
12. **Integration Log Engine**: Tracks external API statuses and retries.

---

## 6. Non-Negotiable Web Security Rules

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

## 7. Developer Security Checklist

| Area | Mandatory Control | Developer Action |
| :--- | :--- | :--- |
| **Authentication** | Secure JWT with rotation. | Implement JWT middleware; expire inactive sessions; lockout failed logins. |
| **Tenant Isolation**| Cross-tenant block. | Enforce `tenant_id` resolver in every query; run automated leakage tests. |
| **Rate Limiting** | Prevent script/Postman floods. | Add IP/token-level limits; return HTTP 429 on abuse. |
| **Request Limits** | Prevent payload buffer crashes. | Enforce 2MB request body size limits in backend router. |
| **Input Validation**| Stop SQL injections & XSS. | Parameterize SQL queries; strict schema validation; HTML sanitization. |
| **CSRF / CORS** | Block malicious page calls. | Use SameSite cookies, CSRF tokens; allow only approved CORS domains. |
| **Uploads** | Secure file uploads. | Validate MIME type/extension; run malware scan; store outside web root. |
| **Audit Trail** | Log sensitive operations. | Log create, update, cancel, approve, export, and login actions. |

---

## 8. Common Threats & Required Protection

- **Brute-force login**: Account lockout policy, MFA for admin/finance roles, rate limit controls.
- **Postman/API flooding**: Gateway token bucket rate limits, request timeouts, and async job queues for heavy queries.
- **SQL injection**: Prepared statements (no direct string query concatenation), ORM parameters validation.
- **XSS**: Escaped outputs, content security policies (CSP), and input text sanitizers.
- **Duplicate Callback**: Idempotency key checks, unique transaction reference constraints.
- **Large Export Abuse**: Export permission profiles, row limits, watermarks, and export logging.

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

---

## 10. Metadata & DocType Framework

Every master, transaction, and report is represented as a DocType:

```
doctype_meta Table:
- doctype_code (e.g. PurchaseOrder)
- module_code (e.g. Procurement)
- table_name (e.g. po_header)
- is_transaction (boolean)
- has_child_table (boolean)
- lifecycle_statuses (JSON state transition mappings)

doctype_fields Table:
- field_name (e.g. vendor_gstin)
- display_label (e.g. "Vendor GSTIN")
- field_type (e.g. Text, Decimal, Select, Link)
- mandatory (boolean)
- read_only_rule (status/role boundaries)
- validation_rule (format validation regex)
```

---

## 11. Dynamic UI & Label Engine

The frontend SPA dynamically renders:
- **Forms**: Reads metadata fields and renders inputs, sections, grids, and action buttons.
- **List Views**: Renders columns according to permissions and user configs.
- **Dynamic Labels**: Intercepts UI text and applies localized replacements (e.g., mapping parent-child relations as *Design/Combination* in jewelry, *Model/VIN* in automobiles, or *Recipe/Batch* in pharma).

---

## 12. Multi-Tenant Data Design

- **Schema-per-Tenant**: Connects connections to isolated database schemas.
- **Tenant Context**: Injected at the API Gateway and enforced by the database layer. No tenant ID should be accepted blindly from the frontend.
- **Versioned Migrations**: DB upgrades are run sequentially and logged per tenant.

---

## 13. Module Registry & Industry Packages

Modules and features are enabled via configuration files:

```json
{
  "industry_code": "METAL_FAB",
  "industry_name": "Metal and Steel Fabrication",
  "doctype_overrides": [
    {
      "doctype": "Brand",
      "new_label": "Material Grade",
      "fields": [
        { "name": "alloy_composition", "label": "Alloy Composition (%)", "fieldtype": "Text", "mandatory": true }
      ]
    },
    {
      "doctype": "Combination",
      "new_label": "Heat Number",
      "fields": [
        { "name": "cut_length", "label": "Cut Length (mm)", "fieldtype": "Decimal", "mandatory": true }
      ]
    }
  ]
}
```

---

## 14. Pluggable POS Architecture
- **POS Profile**: Manages checkout rules, default warehouses, and cashier limits.
- **Drawer Registers**: opening/closing sessions track float cash counts and drawer variance audits.
- **Offline Catalog**: Caches SKU schemas locally inside browser IndexedDB. Syncs invoices back with UUID-based idempotency.

---

## 15. Finance, GST & Compliance

- **Accounting Postings**:
  - *GRN*: Debit Inventory, Credit GR/IR Clearing.
  - *Vendor Invoice*: Debit GR/IR, Debit Input GST -> Credit Vendor Payable.
  - *Payment*: Debit Vendor Payable -> Credit Bank, Credit TDS Payable.
  - *Sale*: Debit Cash/Clearing -> Credit Sales, Credit Output GST.
  - *COGS*: Debit COGS, Credit Inventory.
- **GST Controls**: State-wise GSTIN validation, Place of Supply determination, and automatic e-invoice IRN filings.

---

## 16. Integrations & Log Hub
- **Integration Log Standard**: Every payload is logged with masked credentials and supports retry hooks.
- **Observability**: A developer Log Hub panel displays system errors, Stack Traces from Go panics, and correlation IDs.

---

## 17. System Error-Proofing Matrix

| Area | Risk Scenario | System Control | Standard Error Message |
| :--- | :--- | :--- | :--- |
| **PO** | GRN for unapproved PO | Block GRN | `PO_NOT_APPROVED`: PO is not approved. |
| **PO** | Qty exceeds pending PO | Enforce tolerance rule | `GRN_QTY_EXCEEDS_PO`: Received qty exceeds pending. |
| **Invoice**| Duplicate vendor invoice | Unique vendor+invoice check | `DUPLICATE_VENDOR_INVOICE`: Invoice number already exists. |
| **Barcode**| Duplicate barcode | Unique DB key constraint | `BARCODE_DUPLICATE`: Barcode already exists. |
| **Inventory**| Scanned from wrong location| Check current location | `BARCODE_WRONG_LOCATION`: Barcode is not available here. |
| **Inventory**| Negative stock | Block stock-out | `NEGATIVE_STOCK_BLOCKED`: Stock is not available. |
| **POS** | Sold barcode scanned again | Block sale | `BARCODE_ALREADY_SOLD`: This barcode is already sold. |
| **Finance** | Payment before 3-way match| Block payment | `PAYMENT_BLOCKED`: 3-way match is incomplete. |

---

## 18. Stage-Wise Implementation Roadmap

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
