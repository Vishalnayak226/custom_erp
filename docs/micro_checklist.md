# In-House ERP: Micro-Checklist & Build Tracker

This checklist tracks the implementation of the In-House ERP Kernel and pluggable modules at a micro-level. Developers can mark tasks as `[/]` (In Progress) and `[x]` (Completed) to evaluate builds and trace milestones.

---

## 🚀 Stage 1 - Core Foundation (In Progress)

- [/] **1.1 Base Schema Migrations**
  - [x] Establish `.gitignore`, project folder structure, and dynamic log badges.
  - [/] Create `doctype_meta` registry schema table to hold core document parameters.
  - [/] Create `doctype_fields` table to define database-validated field constraints.
  - [ ] Initialize standard system user tables and RBAC role permission schema mapping.
  - [/] Setup `system_error_logs` schema to handle panic recovery stack traces.
- [/] **1.2 Core Engines Core Logic**
  - [/] **Audit Engine**: Setup database triggers to log modifications (old value, new value, user, time).
  - [/] **Panic Handler Middleware**: Configure route catch block to capture crashes and write stack traces to the log database.
- [ ] **1.3 API Security & Gateway Foundation**
  - [ ] **Gateway Rate Limiting**: Implement Redis-based token bucket throttling (limit public logins to 5/min per IP, standard CRUD to 60/min per token).
  - [ ] **Strict Tenant Resolution**: Enforce backend-only JWT verification mapping `tenant_id` securely (prevents IDOR leaks).
  - [ ] **Prepared Parameterization**: Mandate parameterized SQL queries across all dynamic schema operations (blocks SQL injections).
  - [ ] **Payload Limits**: Configure HTTP request body size checks in Go router (limit maximum body size to 2MB).
- [ ] **1.4 Base API Endpoints**
  - [ ] Implement generic CRUD handler `GET /api/v1/doc/:doctype`.
  - [ ] Implement `GET /api/v1/doc/:doctype/:id` and `POST /api/v1/doc/:doctype` with dynamic field validation rules.
  - [ ] Implement prefix config api (`GET /api/v1/prefix` & `POST /api/v1/prefix`).
  - [ ] Implement labels translation api (`GET /api/v1/labels` & `POST /api/v1/labels`).

---

## 🎨 Stage 2 - Dynamic Configuration

- [ ] **2.1 Core Schema Builders**
  - [ ] **DocType Builder UI**: Create the admin customizer panel allowing users to add custom columns, toggle mandatory rules, and define display order.
  - [ ] **Parent-Child Vocabulary Aliasing**: Configure abstract database key mappings (`parent_document_id` / `child_document_id`) to support client-customized nomenclature.
  - [ ] **Numbering Engine**: Implement dynamic prefix, separator, padding width, and monthly/annual sequence resets. Enforce dynamic variant/child concatenation formulas (e.g. `Child = Parent + Color`).
  - [ ] **Dynamic Label Engine**: Build case-insensitive text translation cache mapping original labels to display overlays.
- [ ] **2.2 Dynamic Form Rendering Engine**
  - [ ] Implement dynamic JSON meta response reader (`GET /api/v1/doc/:doctype/meta`).
  - [ ] Build React/Vue component generator drawing inputs, selectors, date-pickers, and lookups on the fly.
  - [ ] Parse parent-child vocabulary maps to translate model references dynamically on forms and lists.
  - [ ] Implement rename fields UI overriding default labels (e.g. changing "Polish" to "Fabric" or "Engine Type").
  - [ ] Implement toggles to configure list view column visibility dynamically.

---

## 📦 Stage 3 - Master Packages

- [ ] **3.1 Industry Profile Preset Migrations**
  - [ ] Implement **Jewelry Preset**: load Brand, Style, Size, Color, and Polish fields.
  - [ ] Implement **F&B / Beverage Preset**: load Brand, Batch, Expiry, Weight, and Temperature attributes.
  - [ ] Implement **Automobile Preset**: load Make, Model, Engine Type, Fuel Type, and Serial VIN fields.
  - [ ] Implement **Clothing Preset**: load Brand, Style, Size (S/M/L/XL), Fabric, and Color fields.
- [ ] **3.2 Master Configurations**
  - [ ] Setup Organization, Location, Item, Vendor, Customer, Employee, Tax, and GL master schemas.
- [ ] **3.3 Bulk Uploads Engine**
  - [ ] Implement Excel/CSV structure verification (checks column matching before processing rows).
  - [ ] Build item import validation (validates HSN codes, duplicate keys, and category defaults).
  - [ ] Setup row-level error log exports returning failed rows with comments.

---

## 🛒 Stage 4 - Procurement

- [ ] **4.1 Procurement Docs**
  - [ ] Register `PurchaseRequisition` and `RFQ` DocTypes.
  - [ ] Implement Quotation Comparison grid calculating total landed cost.
  - [ ] Register `PurchaseOrder` with automatic multi-state shipping splits.
- [ ] **4.2 PO Matrix & Approvals**
  - [ ] Build Quick PO matrix entry grid translating SKU combinations dynamically.
  - [ ] Implement PO amendment version-controlled workflow and re-approval triggers.

---

## 💎 Stage 5 - GRN and Inventory

- [ ] **5.1 GRN Reconciliation**
  - [ ] Build GRN receiving board matching PO quantities within tolerance.
  - [ ] Implement MRP validation checking received prices against PO price bounds.
  - [ ] Integrate Barcode Generator creating 10-digit barcodes for accepted GRN items only.
- [ ] **5.2 Stock Ledger & Returns**
  - [ ] Implement append-only `inventory_ledger` engine.
  - [ ] Register `PurchaseReturn` (RTV) DocType. Verify barcode exists in store and links to original GRN.
- [ ] **5.3 Physical Count**
  - [ ] Build stock count spreadsheet importer mapping barcode entries.
  - [ ] Build Stock Variance Report comparing system counts to physical counts.
  - [ ] Implement variance adjustment posting logs creating correction ledger items.

---

## 🚚 Stage 6 - Warehouse and Transfer

- [ ] **6.1 Warehouse Logistics**
  - [ ] Implement Bin storage, putaway rules, and picking lists.
- [ ] **6.2 Transfers**
  - [ ] Register `StockTransferOut` DocType. Enforce source barcode status locks.
  - [ ] Register `StockTransferIn` DocType. Verify incoming barcodes and log shortages.
- [ ] **6.3 Tax compliance**
  - [ ] Integrate branch transfer tax invoicing and automatically call e-invoice APIs for interstate dispatches.

---

## 💳 Stage 7 - POS and Sales

- [ ] **7.1 Drawer Session Controls**
  - [ ] Register `CashOpeningEntry` and `CashClosingEntry` DocTypes.
  - [ ] Implement shift reconciliation locks validating counted cash variance.
- [ ] **7.2 Offline-First Database**
  - [ ] Configure IndexedDB local catalog storage for offline checkout.
  - [ ] Build automatic background queue synchronizer with UUID-based idempotency.
- [ ] **7.3 POS Layout Mappings**
  - [ ] Retail Layout: barcode scan, coupon limits, and customer loyalty points.
  - [ ] F&B Layout: Dynamic table seating arrangement maps, split bill routines, and kitchen ticket (KOT) printing.
  - [ ] Service Layout: Calendar booking time-slots and provider commission loggers.

---

## 📊 Stage 8 - Finance

- [ ] **8.1 Accounting Engine**
  - [ ] Register `ChartOfAccounts` and `GlAccount` DocTypes.
  - [ ] Build dynamic GL Mapping registry binding document categories to debits/credits.
  - [ ] Implement 3-Way Match validation checking vendor invoices against PO & GRN rules.
- [ ] **8.2 Ledger Postings**
  - [ ] Automate debit/credit postings for GRN, Invoices, Payments, Sales, and Returns.
  - [ ] Setup bank statement reconciliation tools.

---

## 🔌 Stage 9 - Tax and Integrations

- [ ] **9.1 API Channel Syncs**
  - [ ] Implement Shopify product/inventory mapping.
  - [ ] Implement Unicommerce inventory sync.
  - [ ] Implement Pine Labs Plutus payment terminal integrations.
  - [ ] Implement CleverTap customer event sync.
- [ ] **9.2 Error Logs Hub**
  - [ ] Build Log Hub screen displaying integration payloads and system panic backtraces.
  - [ ] Implement `Retry` buttons for failed payloads.

---

## 📈 Stage 10 - Reports and Dashboards

- [ ] **10.1 Reports Engine**
  - [ ] Implement reports viewer with date, store, brand, and category filters.
  - [ ] Build Inventory Ageing (0-90+ days) and GST invoice filings export tools.
  - [ ] Implement scheduled email reports.

---

## 🧪 Stage 11 - QA and Go-Live

- [ ] **11.1 Test Coverage**
  - [ ] Run UAT scripts mapping end-to-end flows.
  - [ ] Perform concurrency testing for parallel GRNs, transfers, and POS sales.
  - [ ] Validate data migration templates and run trial loads.

---

## 🚀 Stage 12 - Multi-Industry Scale

- [ ] **12.1 Multi-Tenant SaaS Operations**
  - [ ] Deploy automatic tenant provisioning workflows.
  - [ ] Setup feature flag controls per tenant.
  - [ ] Load remaining industry templates (Pharma, Metal, Construction, etc.).
- [ ] **12.2 Intellectual Property & Binary Safety**
  - [ ] Obfuscate, minify, and bundle frontend SPA scripts to prevent reverse-engineering.
  - [ ] Strip debug tables and symbols from release Go binaries (`go build -ldflags="-s -w"`).
