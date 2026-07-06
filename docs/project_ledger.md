# Project Progress Ledger: Custom ERP

This ledger is a living document that tracks the implementation progress, architectural decisions, and chronological build history of the Custom ERP system.

---

## 1. Project Genesis (Where We Started)
The project started from a static, client-side HTML dashboard template served via a basic HTTP server. All data operations (such as Brand and Style definitions) were mocked inside client-side variables (`db.js`) and simulated using `localStorage` data stores. There was no real database, no multi-tenant isolation, no numbering safety, and no observability.

---

## 2. Architectural Decisions & Rationale
We transitioned the mock frontend into a production-grade multi-tenant backend architecture:

| Component | Choice | Rationale |
| :--- | :--- | :--- |
| **Backend Runtime** | **Go (Golang 1.22)** | Single binary compilation, startup memory footprint < 15MB RAM per client, and extremely fast startup times (<10ms). Allows hosting high density client containers on cost-effective virtual servers. |
| **Database** | **PostgreSQL 16.3** | Supports **Schema-per-Tenant** isolation in a single instance. Provides transactional integrity (`SELECT ... FOR UPDATE` row locks) for inventory ledgers, sequence counters, and checks. |
| **Multi-Tenancy** | **Schema-per-Tenant** | Each tenant's data is isolated in a separate database schema (e.g. `tenant_default`). Resolves dynamically via a request middleware. Prevent data leakage and allows schema customization per client. |
| **Branding** | **Custom ERP** | Cleaned up all legacy sample branding (e.g. "Ditos") to establish a generic, copyright-safe, customizable system. |

---

## 3. Phase 1 Completion Ledger (Stage 1)

```
[x] Stage 1: Foundation & Core Engines (Sequence, Audits, Dynamic Labels) -> COMPLETED
[ ] Stage 2: Master Definitions & Attributes (Brands, Styles, Tax Codes) -> PENDING
[ ] Stage 3: Product Catalog & Schemes (Designs, Combinations variant generation) -> PENDING
[ ] Stage 4: Procurement & Purchase (PR, PO Grid, GRN validation, Barcode generation) -> PENDING
[ ] Stage 5: Inventory Control (Stock ledger, local stock movement scan logs) -> PENDING
[ ] Stage 6: Transfers (Stock Transfer Out/In scanning, GST IRN/e-way integrations) -> PENDING
[ ] Stage 7: POS checkout client (Open POS cashier checkout terminal, loyalty, checkout) -> PENDING
[ ] Stage 8: Finance & 3-Way Match (Vendor Invoices, GL double-entry bookings) -> PENDING
[ ] Stage 9: Third-party integrations (Shopify, Pine Labs, OCAPI, CleverTap) -> PENDING
[ ] Stage 10: MIS Reporting (Aging analysis, GST filings summaries, export tools) -> PENDING
```

---

## 4. Phase 1 Build Records (What We Built)

### 4.1 Numbering/Sequence Engine
*   **Location**: `engines/numbering.go`
*   **Behavior**: Dynamically builds sequences: `<Prefix><Separator><StoreCode><Separator><FinancialYear><Separator><PaddedNumber>`.
*   **Concurrency Control**: Connects to DB, starts transaction, locks the current row inside `sequence_counters` via `SELECT ... FOR UPDATE` lock, increments, and commits. Prevents duplicate numbers under concurrent requests.

### 4.2 Dynamic Label Translation Engine
*   **Location**: `engines/labels.go` (Backend API), `app.js` (Frontend translation walker).
*   **Behavior**: Translates exact-match UI strings dynamically. The frontend traverses the DOM recursively using a TreeWalker node filter and updates labels on-the-fly based on translations cached from `/api/v1/labels`.

### 4.3 Audit & System Exception Hub
*   **Location**: `engines/logs.go` and panic recovery middleware in `main.go`.
*   **Behavior**: 
  - **Audits**: Writes structural log rows detailing user actions to `audit_logs` table.
  - **Panics**: Catches Go handler panics, retrieves stack trace, logs it to database under a unique correlation UUID, and returns HTTP 500. Rendered visually inside the Log Hub interface.
