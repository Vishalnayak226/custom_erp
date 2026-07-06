# In-House ERP Kernel: Metadata-Driven Framework Architecture

This document defines the core framework architecture for the In-House ERP system, drawing inspiration from modern open-source platforms such as **Odoo** (modular plugin apps), **ERPNext / Frappe** (metadata-driven DocTypes), and **Nocobase** (dynamic schema builders). 

Rather than building a hardcoded, isolated collection of forms for a single industry, we establish a **lightweight, extensible ERP Kernel** in Go. Industry-specific features (such as jewelry procurement, retail POS, or manufacturing) are loaded as pluggable metadata schemas and serverless logic hooks.

---

## 1. Architectural inspiration & Learnings

By reviewing successful open-source architectures, we extract key design patterns:

| Platform | Core Pattern | Key Advantage | How We Adapt It |
| :--- | :--- | :--- | :--- |
| **ERPNext (Frappe)** | **DocTypes (Document Types)** | Schemas, forms, and views are configured as metadata database rows, not hardcoded files. | Our Go backend serves generic `/api/v1/doc/:doctype` endpoints. The frontend dynamically renders forms from field definitions. |
| **Odoo** | **App Pluggability** | Core database has modular apps. Custom models inherit from base models. | Modules are configured as JSON packages that register custom fields into a global `JSONB` meta column. |
| **Nocobase** | **Dynamic Collections** | Direct database schema creation and field mapping at runtime. | The Go Kernel supports running dynamic migrations for custom schemas/tables per tenant. |
| **Akaunting** | **Modular Accounting** | Strict GL and double-entry segregation. | Core accounting engine listens to DocType status changes to post ledger transactions. |

---

## 2. The Extensible ERP Kernel

The Go backend functions as a minimal core runtime engine containing only system prerequisites:

```
                            +----------------------------------+
                            |       Go ERP Kernel Core         |
                            +----------------------------------+
                                             |
             +-------------------------------+-------------------------------+
             |                               |                               |
             v                               v                               v
+-------------------------+     +-------------------------+     +-------------------------+
|     DocType Engine      |     |  Event Hook Controller  |     |  Workflow / Status Log  |
| - Meta Registry         |     | - before_save()         |     | - State transitions     |
| - Generic CRUD Handlers |     | - after_save()          |     | - Multi-tier approvals  |
+-------------------------+     +-------------------------+     +-------------------------+
             |                               |                               |
             +-------------------------------+-------------------------------+
                                             |
                                             v
                            +----------------------------------+
                            |   Industry Modules (JSON/Meta)   |
                            |                                  |
                            |   - Jewelry Master Definition    |
                            |   - Retail POS Checkout Cart     |
                            |   - Warehouse Transfers          |
                            +----------------------------------+
```

### 2.1 The DocType Schema Model
A `DocType` is the definition of a document, master record, or ledger. It is represented in the database as:
- **`doctype_meta`**: Stores document type definitions (e.g. Name: `PurchaseOrder`, Module: `Procurement`, Table: `po_header`).
- **`doctype_fields`**: Stores individual field definitions for each DocType:
  - `name`: Technical database identifier (e.g., `delivery_date`).
  - `label`: Screen display name (e.g., "Expected Delivery").
  - `fieldtype`: Data validator (e.g., `Text`, `Int`, `Decimal`, `Date`, `Select`, `Link` to another DocType).
  - `mandatory`: Boolean indicating if validation blocks empty submissions.
  - `options`: Dropdown choices or lookup tables for Link types.

### 2.2 Generic API Routes
Instead of custom controllers for every form, the Go Kernel exposes unified routes:
- `GET /api/v1/doc/:doctype` - Fetch listing (supports pagination, filtering, query params).
- `GET /api/v1/doc/:doctype/:id` - Fetch individual document record.
- `POST /api/v1/doc/:doctype` - Create document (validates fields against `doctype_fields` schema).
- `PUT /api/v1/doc/:doctype/:id` - Update document.
- `DELETE /api/v1/doc/:doctype/:id` - Soft deletes or cancels document.

---

## 3. Dynamic UI Generation

The Single Page Application (SPA) frontend does not contain hardcoded HTML forms for separate masters. Instead, it utilizes a **Form Component Interpreter**:

1.  User clicks "New Brand". The router detects the `brands` view, which maps to DocType `Brand`.
2.  Frontend calls `GET /api/v1/doc/Brand/meta`.
3.  The API returns the dynamic field configuration:
    ```json
    [
      { "name": "name", "label": "Brand Name", "fieldtype": "Text", "mandatory": true },
      { "name": "code", "label": "Brand Code", "fieldtype": "Text", "mandatory": false }
    ]
    ```
4.  The frontend loops over the JSON, rendering text inputs, selects, or toggles automatically.
5.  **Impact**: Anyone can add a custom field to the system by editing the DocType setup. The UI renders it immediately without code recompilations.

---

## 4. Custom Logic Hooks & Pluggability

When a dynamic document is saved, the Kernel runs the **Event Controller**:

### 4.1 Local Event Hooks (Go Core)
Standard actions (such as logging audits, generating sequence numbers via the Numbering Engine, or writing to the inventory ledger) run locally inside the Go binary.

### 4.2 External Webhooks (Tenant Customization)
If Client A requires custom business rules (e.g., verifying gold purity against current market prices before booking a GRN):
1.  Add a hook rule to `after_save` in the DocType metadata configuration.
2.  The Go Kernel sends a payload to the tenant's configured serverless endpoint:
    ```
    POST https://lambda.client-a.com/verify-grn
    ```
3.  The lambda returns `{ "status": "APPROVED" }` or `{ "status": "REJECTED", "message": "Gold purity mismatch" }`.
4.  The Core Kernel processes the decision and saves the ledger transaction.

---

## 5. Dynamic Industry Configurator & Presets

The system includes a centralized **Industry Configurator** allowing users to switch the operational scope of the ERP dynamically:
*   **JSON Industry Packages**: Standardized configurations mapping DocTypes, workflows, field overrides, and sidebar navigation menus per industry (Pharma, Metal, Agriculture, construction, etc.).
*   **The Switch API**: `POST /api/v1/admin/industry` triggers database updates reloading `doctype_meta` and `doctype_fields` records. 
*   **UI Adaptability**: When the active industry is updated, the frontend dynamic rendering engine changes immediately, updating sidebar submenus, form headers, and column names without requiring code changes.

---

## 6. Dynamic Parent-Child (Design & Combination) Vocabulary Aliasing

Different companies use different terms for parent products and their children (variants):
*   **Company A (Jewelry/Fashion)**: Parent = *Design Number*, Child = *Combination ID* or *SKU*.
*   **Company B (Electronics)**: Parent = *Model Number*, Child = *Serial Number*.
*   **Company C (Automobile)**: Parent = *Item Template*, Child = *Chassis Code*.
*   **Company D (Clothing)**: Parent = *Style SKU*, Child = *Size Variant*.

### 6.1 Unified Abstract Mappings
The core ERP database schema enforces the relations abstractly:
- **`parent_document_id`** (e.g., the base design / model definition).
- **`child_document_id`** (e.g., the specific sellable variant SKU / combination / barcode).

### 6.2 Frontend Dynamic Label Translation
- The **Dynamic Label Engine** stores these semantic mappings per tenant. 
- When rendering grid tables (e.g., PO checkout cart, Stock Variance report, or POS billing screen), the frontend automatically overrides the column headers to match the tenant's chosen term (e.g., replacing "Design Number" with "Parent SKU" or "Chassis Number" dynamically).

### 6.3 Flexible Sequence Formatting
For ID generation:
1.  **Auto-Generator Rules**: The **Numbering Engine** allows the client to define concatenation formulas:
    *   *Example*: Child SKU = `{Parent_Code} + '-' + {Color_Code} + {Size_Code}`.
2.  **Manual Input Overrides**: Toggle option per tenant schema to allow cashiers/designers to type custom vendor design/parent codes manually, rather than forcing system auto-generation.

---

## 7. Operational Guidelines for Multi-Industry & Multi-Location Deployments

### 7.1 Client Industry Allocation & Freezing Rules
- **Access Control (SaaS Console)**: You can assign specific industry profiles (e.g., only `RETAIL` or only `PHARMA`) to specific tenants based on their subscription tier, or release all profiles so they can run multiple operations.
- **Industry Switching & Freezing**:
  - *Unfrozen (No Active Transactions)*: If a client has not yet posted transactions to the stock or accounting ledgers, they can freely switch their active industry. The database metadata re-registers instantly, altering the UI menus and fields.
  - *Frozen (Active Transactions Exist)*: Once transaction data is recorded, the active industry profile is **locked**. The configurator blocks changing the industry template to prevent database table corruption and ledger misalignment. 
  - *Workaround*: To change industries on a live tenant, they must purge/archive historical transaction ledgers or create a new isolated tenant schema database.

### 7.2 Multi-Location & Multi-Entity Corporate Hierarchy
For organizations running multiple distinct entities (e.g. North India entity with GSTIN, South India entity with GSTIN, and US entity with EIN):

```
                       [ Parent Group Tenant Schema ]
                                      |
         +----------------------------+----------------------------+
         |                                                         |
         v                                                         v
[ Legal Entity 1: India South ]                             [ Legal Entity 2: US East ]
- Base Currency: INR                                        - Base Currency: USD
- Tax Rule: Indian GST (CGST/SGST/IGST)                      - Tax Rule: US State Sales Tax
- GSTIN: 29AAAAA1111A1Z1                                    - EIN: 12-3456789
         |                                                         |
         +------------------+                                      +------------------+
         |                  |                                                         |
         v                  v                                                         v
  [ Store: BLR ]     [ Warehouse: WH ]                                         [ Store: NYC ]
```

1.  **Data Isolation**: All entities live within the **same tenant database schema**, allowing consolidated financial reporting and a shared parent product catalog.
2.  **Locational Scoping**:
    *   Every transaction document (Purchase Order, Transfer Out, Sales Invoice) must be stamped with a `legal_entity_id`, `store_id`, and `currency`.
    *   **User RBAC Boundaries**: Cache workers and location managers are assigned specific location permissions. A cashier at the Bengaluru store cannot view stock, sales logs, or customer details of the New York City store unless granted Group-Admin rights.
3.  **Local compliance & Tax Engine Rules**:
    *   When creating an invoice, the system resolves the `store_id` state and tax jurisdiction rules.
    *   *India South (BLR)*: Applies CGST + SGST (for intra-state sales) or IGST (for inter-state transfers).
    *   *US East (NYC)*: Applies localized US State Sales Tax rates and bypasses GST format validations.
