# In-House ERP: Micro-Checklist & Build Tracker

This checklist tracks the implementation of the In-House ERP Kernel and pluggable modules at a micro-level. Developers can mark tasks as `[/]` (In Progress) and `[x]` (Completed) to evaluate builds and trace milestones.

---

## 🚀 Stage 1 - Core Foundation (Completed)

- [x] **1.1 Base Schema Migrations**
  - [x] Establish `.gitignore`, project folder structure, and dynamic log badges.
  - [x] Create `doctype_meta` registry schema table to hold core document parameters.
  - [x] Create `doctype_fields` table to define database-validated field constraints.
  - [x] Initialize standard system user tables and RBAC role permission schema mapping.
  - [x] Setup `system_error_logs` schema to handle panic recovery stack traces.
  - [x] Register WMS-relevant DocTypes metadata (`Item`, `PurchaseOrder`, `ASN`, `SalesInvoice`, `TransferOrder`).
- [x] **1.2 Core Engines Core Logic**
  - [x] **Audit Engine**: Setup database triggers to log modifications (old value, new value, user, time). Enforce append-only, non-editable audits.
  - [x] **Panic Handler Middleware**: Configure route catch block to capture crashes and write stack traces to the log database.
- [x] **1.3 API Security & Gateway Foundation**
  - [x] **Gateway Rate Limiting**: Implement token bucket throttling (limit public logins to 5/min per IP, standard CRUD to 60/min per token).
  - [x] **Strict Tenant Resolution**: Enforce backend-only JWT verification mapping `tenant_id` securely (prevents IDOR leaks).
  - [x] **Prepared Parameterization**: Mandate parameterized SQL queries across all operations (blocks SQL injections).
  - [x] **Payload Size Controls**: Enforce HTTP request size limits (max 2MB body limit) and file size/MIME type validation.
  - [x] **CSRF & CORS policies**: Setup SameSite cookies, CSRF tokens, and enforce strict, non-wildcard CORS domains in production.
  - [x] **Secrets Protection**: Scan codebase for hardcoded keys and store configs in env variables.
  - [x] **Object-Level Authorization**: Enforce user location, role, and document ownership checks on every fetching and editing API.
  - [x] **Environment Credentials**: Move database connection credentials to `DATABASE_URL` environment variables.
  - [x] **SSO Claim Alignment**: Enforce token claim keys consistency containing `tenant`, `role`, and `loc` properties.
- [x] **1.4 Base API Endpoints**
  - [x] Implement generic CRUD handler `GET /api/v1/doc/:doctype` supporting master, transaction, and child table reads.
  - [x] Implement `GET /api/v1/doc/:doctype/:id` and `POST /api/v1/doc/:doctype` with dynamic field validation rules.
  - [x] Implement prefix config api (`GET /api/v1/prefix` & `POST /api/v1/prefix`).
  - [x] Implement labels translation api (`GET /api/v1/labels` & `POST /api/v1/labels`).
  - [x] **Dynamic GET Filters**: Support filtering JSONB document keys directly from URL query parameters (e.g., `?status=Approved`).
  - [x] **GRN Callback Hook**: Intercept `POST /api/v1/doc/GRN` to update stock levels atomically in `inventory_availability`.
- [x] **1.5 Omnichannel Scale Foundation**
  - [x] **Base Schema Migrations**: Create tables: `inventory_availability`, `inventory_reservation`, `integration_event_outbox`, `integration_event_log`, and `dead_letter_queue`.
  - [x] **Event Bus & Outbox Pattern**: Implement dynamic outbox publishing triggers and background worker polling event logs.
  - [x] **Inventory Availability Read Model**: Create calculated Available-to-Sell (ATS) inventory read model service API.
  - [x] **Reservation Service**: Implement in-memory or database-backed temporary stock reservation checks and lockouts.

---

## 🎨 Stage 2 - Dynamic Configuration (Completed)

- [x] **2.1 Core Schema Builders**
  - [x] **DocType Builder UI**: Create the admin customizer panel allowing users to add custom columns, toggle mandatory rules, and define display order.
  - [x] **Parent-Child Vocabulary Aliasing**: Configure abstract database key mappings (`parent_document_id` / `child_document_id`) to support client-customized nomenclature.
  - [x] **Numbering Engine**: Implement dynamic prefix, separator, padding width, and monthly/annual sequence resets. Enforce dynamic variant/child concatenation formulas.
  - [x] **Dynamic Label Engine**: Build case-insensitive text translation cache mapping original labels to display overlays.
- [x] **2.2 Dynamic Form Rendering Engine**
  - [x] Implement dynamic JSON meta response reader (`GET /api/v1/doc/:doctype/meta`).
  - [x] Build React/Vue component generator drawing text, number, date, select, link, attachment, table, and scan fields on the fly.
  - [x] Parse parent-child vocabulary maps to translate model references dynamically on forms and lists.
  - [x] Implement rename fields UI overriding default labels (e.g. changing "Polish" to "Fabric" or "Engine Type").
  - [x] Implement toggles to configure list view column visibility dynamically.

---

## 📦 Stage 3 - Master Packages (Completed)

- [x] **3.1 Industry Profile Preset Migrations**
  - [x] Implement **Jewelry Preset**: load Brand, Style, Size, Color, and Polish fields.
  - [x] Implement **F&B / Beverage Preset**: load Brand, Batch, Expiry, Weight, and Temperature attributes.
  - [x] Implement **Automobile Preset**: load Make, Model, Engine Type, Fuel Type, and Serial VIN fields.
  - [x] Implement **Clothing Preset**: load Brand, Style, Size (S/M/L/XL), Fabric, and Color fields.
- [x] **3.2 Master Configurations**
  - [x] Setup Organization, Location, Item (parent/variant), Vendor, Customer, Employee, Tax, and GL master schemas.
- [x] **3.3 Bulk Uploads Engine**
  - [x] Implement Excel/CSV structure verification (checks column matching before processing rows). No direct silent imports.
  - [x] Build item import validation (validates HSN codes, duplicate keys, and category defaults).
  - [x] Setup row-level error log exports returning failed rows with comments.

---

## 🛒 Stage 4 - Procurement (Completed Backend Mappings)

- [x] **4.1 Procurement Docs**
  - [x] Register `PurchaseOrder` metadata and field validations.
  - [x] Link documents to statuses and tracks cancellation states.

---

## 💎 Stage 5 - GRN and Inventory (Completed Backend Mappings)

- [x] **5.1 GRN Reconciliation**
  - [x] Build GRN callback hooks and validate incoming item counts.
  - [x] Generate barcode numbers dynamically for accepted goods.
- [x] **5.2 Stock Ledger & Returns**
  - [x] Implement append-only fast availability read models.
  - [x] Process and validate item returns.

---

## 🚚 Stage 6 - Warehouse and Transfer (Completed Sourcing & Re-routing)

- [x] **6.1 Warehouse Logistics & Transfers**
  - [x] Implement location-scoping and automated order reservations.
  - [x] Configure rule-driven picking tasks and re-routing on rejection.

---

## 💳 Stage 7 - POS and Sales (Completed Webhooks & Checkouts)

- [x] **7.1 POS Checkout & Webhooks**
  - [x] Register `POSCart` and `SalesReturn` metadata schemas.
  - [x] Build POST /api/v1/checkout to deduct stock and complete checkout.
  - [x] Build POST /api/v1/integration/shopify/order webhook with idempotency checks.

---

## 📊 Stage 8 - Finance (Completed Balanced GL Ledger)

- [x] **8.1 Accounting Engine**
  - [x] Register `GLAccount` and `GLPosting` schema tables.
  - [x] Build balanced double-entry accounting routine validating debits == credits.
- [x] **8.2 Ledger Postings**
  - [x] Automate postings for GRNs, cashier checkouts, and returns.
  - [x] Expose GET /api/v1/finance/trial-balance.

---

## 🔌 Stage 9 - Tax and Integrations

- [x] **9.1 API Channel Syncs**
  - [x] Implement Shopify product/inventory mapping, delta stocks, and order webhooks.
  - [x] Implement Unicommerce inventory sync & multi-marketplace order ingestion.
  - [x] Implement Pine Labs Plutus payment terminal reconciliation checkouts.
  - [x] Implement CleverTap customer order event log syncing.
  - [x] Implement Marketplace settlements (Shopify/Amazon) payout reconciliation and commission bookings.
  - [x] Implement logistics dispatch tracking bookings and carrier registration.
- [ ] **9.2 Error Logs Hub**
  - [ ] Build Log Hub screen displaying integration payloads and system panic backtraces.
  - [ ] Implement `Retry` buttons for failed payloads.
  - [ ] Verify signature tokens on incoming external webhooks and callbacks.

---

## 📈 Stage 10 - Reports and Dashboards

- [x] **10.1 Reports Engine**
  - [x] Implement replenishment suggestions reports with safety stock and lead times parameters.
  - [x] Implement demand forecasting projection reports.
  - [x] Implement picking task SLA breach monitoring alerts reports.
  - [x] Implement Trial Balance GL ledger balanced summaries reports.

---

## 🧪 Stage 11 - QA and Go-Live

- [x] **11.1 Test Coverage**
  - [x] Perform concurrency scale stress-testing (100 concurrent workers, 1,000 transactions across 2,000 store nodes).
  - [x] Run UAT scripts mapping end-to-end checkouts, settlements, logistics bookings, replenishment reorders, and SLA breaches.
  - [x] Validate database schema integrity and run trial migrations.
  - [x] Execute security validation checklists (cross-tenant role boundaries and token verification).

---

## 🚀 Stage 12 - Multi-Industry Scale

- [ ] **12.1 Multi-Tenant SaaS Operations**
  - [ ] Deploy automatic tenant provisioning workflows.
  - [ ] Setup feature flag controls per tenant.
  - [ ] Load remaining industry templates (Pharma, Metal, Construction, etc.).
- [x] **12.2 Intellectual Property & Binary Safety**
  - [ ] Obfuscate, minify, and bundle frontend SPA scripts to prevent reverse-engineering.
  - [x] Strip debug tables and symbols from release Go binaries (`go build -ldflags="-s -w"`).
  - [x] Setup automated backups, encryption, and monthly recovery test drills.
