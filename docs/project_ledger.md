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

## 3. Project Rollout Completion Ledger

```
[x] Phase 1: Core Foundation & Scale Infrastructure (Event Bus, Outbox, Availability, Reservations) -> COMPLETED
[x] Phase 2: Single Vertical Pilot (Jewellery PO-GRN-Barcode-Inventory-Transfers-POS-Finance) -> COMPLETED
[x] Phase 3: Omnichannel Pilot (Shopify Sync, Webhook Imports, Fulfillment Routing) -> COMPLETED
[x] Phase 4: Store Fulfillment (Ship-from-store, BOPIS, Return Anywhere, Tasks Dashboard) -> COMPLETED
[x] Phase 5: Scale Test (Simulate 100, 500, 1000, 2000 stores) -> COMPLETED
[x] Phase 6: Marketplace/OMS Expansion (Settlements, Logistics, Support Console) -> COMPLETED
[x] Phase 7: Advanced Optimization (Forecasting, Replenishment, SLA target tuning) -> COMPLETED
```

**Scope note (2026-07-12)**: Phases 1-7 above are the **Omnichannel Scale Add-on Blueprint's** rollout plan — they cover the kernel and omnichannel/scale backend, and "COMPLETED" here is accurate for that scope. It does **not** mean the full ERP described in the Master Blueprint is complete: POS/Finance/GST/CRM/HR/Assets UI, the approval/maker-checker workflow engine, MFA, and most of the ~80-report catalog were never part of this Phase 1-7 plan and remain unbuilt. See **[docs/pdf_blueprint_gap_analysis.md](pdf_blueprint_gap_analysis.md)** for the full comparison against all 6 spec PDFs and `docs/micro_checklist.md` Stage 13 for the resulting backlog.

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

---

## 5. Phase 2 Build Records (What We Built)

### 5.1 DocType Metadata Builder & Registry APIs
*   **Location**: `engines/doctype.go` and routes registered in `main.go`.
*   **Behavior**: Registers custom document schemas dynamically (`POST /api/v1/meta/doctypes`) and registers custom column properties (fields name, labels, display orders, datatypes, and mandatory parameters) in `doctype_fields` (`POST /api/v1/meta/{doctype}/fields`).

### 5.2 Dynamic Form & Grid Rendering Interpreter
*   **Location**: `public/app.js` and `public/index.html`.
*   **Behavior**: Traverses dynamic field structures queried from `/api/v1/doc/{doctype}/meta` and constructs form fields (`Data`, `Number`, `Select`, `Link`) inside a single dynamic modal (`#dynamic-modal`). Automatically queries referencing list lookups for linked fields dynamically from the backend.

### 5.3 Parent-Child Vocabulary & Translation Engine
*   **Location**: `public/app.js` DOM translator.
*   **Behavior**: Dynamically maps vocabulary references per tenant, allowing users to override default nomenclature (such as mapping `Brand` to `Material Grade` or `Color` to `Fabric Metallurgy`) and translates UI elements instantly.

---

## 6. Phase 3 Build Records (What We Built)

### 6.1 Industry Profile Preset Configs & Loader
*   **Location**: `public/profiles/` directory and `SwitchIndustryProfile` in `engines/doctype.go`.
*   **Behavior**: Provides structural configuration mappings for Jewelry, Food & Beverage, Automobile, and Clothing industries. Loads templates dynamically via a database transaction: clears old field definitions, updates doctype metadata, registers customized columns, inserts default sequential counter templates, and resets translation vocabulary overlay caches.

### 6.2 Bulk Uploads Engine
*   **Location**: `engines/import.go` and HTTP route handlers in `main.go`.
*   **Behavior**: Parses raw CSV records, maps fields case-insensitively, validates formatting and constraints (numeric boundaries, date validity, select bounds, reference links), increments sequence codes dynamically inside a transactional context, and yields a structural validation report showing total successes and specific cell-level validation errors.

---

## 7. Phase 4 Build Records (What We Built)

### 7.1 Balanced Double-Entry Finance Engine
*   **Location**: `engines/finance.go` and `POST /api/v1/checkout`.
*   **Behavior**: Posts balanced journal entry lines (sum debits == sum credits) to `gl_postings`. Automatically handles bookings during checkouts: debits Cash (`1100`) & COGS (`5100`), credits Sales Revenue (`4100`) & Inventory Control (`1200`).

### 7.2 Store Fulfillment Task Manager
*   **Location**: `engines/fulfillment.go` and `POST /api/v1/fulfillment/task/transition`.
*   **Behavior**: Manages picking tasks (`FulfillmentTask`). If a task is rejected, automatically releases local reservations and triggers re-routing to the next best store node.

### 7.3 Return Anywhere Engine
*   **Location**: `engines/fulfillment.go` and `POST /api/v1/fulfillment/return`.
*   **Behavior**: Safely processes returns at any store, incrementing local stock levels and posting balanced refund GL journals.

---

## 8. Phase 5 Build Records (What We Built)

### 8.1 Concurrency Scale Simulation Engine
*   **Location**: `engines/scale.go` and `POST /api/v1/admin/scale-test`.
*   **Behavior**: Stress-tests 2,000 stores with parallel checkout workers, tracking throughput (TPS) and latencies. Verified ~456 TPS under 100 concurrent workers with 0 conflicts and a balanced post-simulation ledger.

---

## 9. Phase 6 Build Records (What We Built)

### 9.1 Marketplace Settlement & Reconciliation Engine
*   **Location**: `engines/marketplace.go` and `POST /api/v1/marketplace/settlement/reconcile`.
*   **Behavior**: Receives marketplace payout reports, validates payout math (`total - commission == net`), reconciles cart orders status to `Settled`, and posts balanced GL entries (debits Cash `1100` and Commission Expense `5200`, credits Accounts Receivable `1300`).

### 9.2 Carrier Logistics Dispatch Tracker
*   **Location**: `engines/marketplace.go` and `POST /api/v1/marketplace/logistics/book`.
*   **Behavior**: Registers carrier dispatch metadata (`LogisticsBooking` documents in `Shipped` status) to track carrier names, tracking numbers, and shipping charges.

---

## 10. Phase 7 Build Records (What We Built)

### 10.1 Sales-Velocity-Driven Replenishment Suggestions
*   **Location**: `engines/optimization.go` and `GET /api/v1/optimization/replenishment-suggestions`.
*   **Behavior**: Computes average daily sales velocity of SKUs based on POSCart checkout histories over 30 days and suggests replenishment orders (`suggestedQty = (velocity * leadTime) + safetyStock - available`).

### 10.2 Historical Sales-Based Forecasting Engine
*   **Location**: `engines/optimization.go` and `POST /api/v1/optimization/forecast`.
*   **Behavior**: Projects future SKU sales volumes based on daily historical velocity rates.

### 10.3 Picking SLA Breach Monitors
*   **Location**: `engines/optimization.go` and `GET /api/v1/optimization/sla-breaches`.
*   **Behavior**: Scans open fulfillment tasks, measures elapsed time since creation, and highlights tasks exceeding SLA thresholds.

### 10.4 Status-mismatch bug — fixed 2026-07-12
*   §10.1 and §10.2 used to compute sales velocity by querying `POSCart` documents with `status = 'completed'`, which the system never actually produces. Fixed to match `status IN ('Paid', 'Settled')` — the two real statuses `handleCheckout` and `ProcessMarketplaceSettlement` actually write. Verified with a real checkout followed by a real forecast call. See `docs/hardening_roadmap.md` Phase 2.2.

---

## 11a. SaaS Provisioning, Feature Flags & Integration Log Retries (Build Record)

*   **Tenant Provisioning**: `engines/saas.go`, `POST /api/v1/admin/tenant/provision`. Clones 19 table structures and seeds metadata from `tenant_default` into a new tenant schema. Fixed 2026-07-12 (`hardening_roadmap.md` Phase 1.6): `users` is no longer cloned into the seed loop — each new tenant gets one admin user with a unique, freshly-generated bcrypt-hashed password (`generateRandomPassword()`), not the shared placeholder hash.
*   **Feature Flags**: `engines/saas.go`, `POST /api/v1/admin/tenant/feature-flag`. Per-tenant boolean flags backed by a new `feature_flags` table. `SetFeatureFlag` is wired to the API; `IsFeatureEnabled` isn't called from any request handler yet, so flags don't currently gate any behavior.
*   **Integration Log Viewer & Retry**: `engines/outbox.go` (`GetIntegrationLogs`, `RetryIntegrationEvent`), `GET /api/v1/integration/logs` and `POST /api/v1/integration/retry`. Backend works; the frontend Log Hub view doesn't call either endpoint yet, so there's no integration-payload list or retry button in the UI.

For the full list of known gaps and the plan to close them, see **[docs/hardening_roadmap.md](hardening_roadmap.md)**.

---

## 11b. Real Login Flow (Build Record, 2026-07-12)

Closed `hardening_roadmap.md` Phase 1.1. Prior to this, `apiMiddleware` silently granted `HR/Admin` access to any request with no `Authorization` header — the app had no login screen at all and worked *only because of* that bug.

*   **Backend**: `main.go` `apiMiddleware` now rejects any request without a valid Bearer token, except `POST /api/v1/login` itself.
*   **Frontend**: new login screen (`public/index.html`/`app.js`/`styles.css`), logout button in the sidebar, and `apiFetch`'s 401 handling now logs the user out and returns to the login screen instead of a dead-end alert.
*   **Credentials**: all 4 seed users reset to unique bcrypt hashes (`db/migration.sql`) — the previous shared hash's plaintext was never recorded anywhere. Dev-only plaintext is in `DEV_CREDENTIALS.local.txt` (gitignored, project root).
*   **Token expiry (Phase 1.4)**: also closed 2026-07-12 — issued tokens now embed an `exp` claim (default 24h TTL, `JWT_EXPIRY_HOURS` overridable) and `ParseToken` rejects expired ones.

---

## 11. Version Control & Git History

*   **Remote Repository**: `https://github.com/Vishalnayak226/custom_erp.git`
*   **Branch**: `main`
*   **Latest Commit**: `fb183dc`
*   **Commit Message**: `Update docs/micro_checklist.md and artifacts walkthroughs for Phase 6`
*   **Date**: 2026-07-11
