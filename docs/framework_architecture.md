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

## 5. Industry-Specific Profiles & Dynamic Schema Builder

Different industries require completely distinct master attributes:
- **Jewelry**: Polish, gross/net weight, purity, stone count.
- **Food & Beverage**: Expiry date, batch number, nutritional facts, storage temperature.
- **Automobile**: Chassis number, engine type, fuel capacity, model year.
- **Clothing/Apparel**: Fabric type, size codes (S/M/L/XL), design pattern, color palette.

### 5.1 DocType Builder UI (The Schema Customizer)
To make the ERP truly industry-agnostic, the system includes a **DocType Builder UI** in the developer admin panel:
- **Add Field**: Cashiers/Admins can add new custom columns to any master record.
- **Rename Labels**: Allows renaming standard columns universally (e.g., renaming the "Polish" field to "Fabric" or "Engine Type" on screens).
- **Enforce Rules**: Dynamic checkboxes mapping whether fields are *Mandatory*, *Visible in Grid*, or *Read-Only*.

### 5.2 Pre-Configured Industry Packages
When creating a new tenant instance, the user chooses their industry profile. The Kernel then automatically runs migrations to load the corresponding DocType presets:
1.  **Jewelry Preset**: Generates Brand, Style, Size, Color, and Polish fields.
2.  **F&B Preset**: Generates Brand, Batch, Expiry, Weight, and Temperature attributes.
3.  **Automobile Preset**: Generates Make, Model, Engine Type, Fuel Type, and Serial VIN fields.
4.  **Clothing Preset**: Generates Brand, Style, Size (S/M/L/XL), Fabric, and Color fields.
