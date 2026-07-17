# In-House ERP: Functional Modules Directory

> **Status: mixed — several sections are now built, several remain spec-only.** Sections 1–2 (Core Kernel, Industry Masters) reflect real code, as before. As of Stage 13 (see `docs/micro_checklist.md`), Section 8 (POS/Retail Checkout — basic screen), Section 10 (Sticker Printing — text-label MVP), Section 15 (Assets, Expenses, and HR — lifecycle MVPs), and parts of Section 3 (Vendors — master + RFQ/quote comparison), Section 9 (Customer Directory — loyalty point ledger MVP), and Section 16 (Finance & Accounting — GL is built, GST calc is built, full e-invoicing is not) now have real backend routes and UI. Sections 11–12 (Bulk Uploads, Dynamic Labels) are also real code, predating Stage 13. Sections 4–7, 13–14 (Procurement PO/GRN chain, Warehouse/Transfers, System Configurations, most Integrations beyond Shopify) are still planned modules with no corresponding backend routes or schema. Don't assume a module listed here exists in code without checking `main.go`/`engines/` first; see **[docs/micro_checklist.md](micro_checklist.md)** for the current build tracker and **[docs/pdf_blueprint_gap_analysis.md](pdf_blueprint_gap_analysis.md)** for the original (2026-07-12) full build-vs-spec comparison, now partially closed.

This directory lists all the functional modules and configuration packages mapped for the Inhouse ERP system. Industry-specific modules are loaded as metadata packages (DocTypes) on top of the core extensible ERP Kernel.

---

## 1. Core ERP Kernel (The System Foundation)
The base Go runtime binary that runs identical code for all tenants:
*   **DocType Meta-Registry**: Central database mappings defining all standard and custom tables, fields, validations, and lookup links.
*   **DocType Builder UI**: Admin panel allowing users to dynamically add columns, configure mandatory checks, and define grid display rules.
*   **Dynamic Label Engine**: Localization engine to customize UI terminology (e.g., renaming the abstract parent model to "Design Number" or "Style SKU", and variants to "Combination ID" or "Serial Number" dynamically).
*   **Numbering Engine**: Sequence generator rules supporting custom prefixes, digit padding, reset rules (annual/monthly), and dynamic parent-child concatenation formats.
*   **Workflow & Approval Engine**: Tiered document approvals matching amount slabs, location roles, and automatic escalations.
*   **Log Hub & Observability Dashboard**: Central tracker capturing global panic stack traces, correlation IDs, database locks, and retry actions for failed integrations.

---

## 2. Pluggable Industry Masters
Pre-configured master templates loaded based on the client's industry selection:
*   **Jewelry Master Package**: Presets for Brand, Style, Size, Color, Polish, Purity, and ProductCategory weight flags.
*   **Food & Beverage Master Package**: Presets for Brand, Batch Number, Expiry Date, Net Weight, and storage Temperature indicators.
*   **Automobile Master Package**: Presets for Make, Model, Engine Type, Fuel Type, and Serial VIN tracking.
*   **Clothing/Apparel Master Package**: Presets for Brands, Style, Size Codes (S/M/L/XL), Fabric Types, and design Patterns.

---

## 3. Vendors & Suppliers
Manages external procurement vendors and shipping contacts. Loaded dynamically on the Core Vendor/Supplier DocType models.
*   **Vendor Directory**: Tabbed cards defining:
    *   *Basic Details*: Code, Legal Name, PAN, MSME, TDS flags.
    *   *Tax Mappings*: GSTIN per state.
    *   *Address & Contacts*: Shipping/billing locations and contacts directory.
    *   *Bank Details*: Enforces manager approval on modifications.
    *   *Commercial Details*: Default payment terms and currency mappings.
*   **Suppliers**: Shipping/logistical contacts separate from accounting vendors.

---

## 4. Procurement & Purchase
Tracks goods procurement flows from internal requests to physical receipt.
*   **Purchase Requisition (PR)**: Internal requests tracking requesting cost centers and approvals.
*   **RFQ & Quotation comparison**: Dynamic comparative grids mapping vendor price quotes to identify the lowest landed cost.
*   **Purchase Orders (PO)**: PO amendments, state-wise shipping splits, and PO status logs.
*   **Quick PO Form**: A matrix input grid allowing cashiers to create POS orders rapidly.
*   **GRN (Goods Receipt Note)**: Checks items received from vendors against purchase orders, flags MRP overrides, and generates barcode sequences.
*   **Purchase Return (RTV)**: Processes vendor returns. Scans barcodes to verify original GRN, updates inventory to `RTV Pending`, and posts debit notes.

---

## 5. Stock & Store Management
Defines physical locations, sequence generation parameters, and inventory adjustments.
*   **Stores**: Registry of retail shops, franchise units, and central warehouse locations.
*   **Prefix Config**: Configures distinct sequential prefixes (PO, GRN, TO, SI) per store.
*   **Number Generation**: Custom sequence padding widths, ranges, and reset logic.
*   **Stock Update (Manual Adjustments)**: Processes manual inventory adjustments, creating a documented audit path rather than directly editing stock values.

---

## 6. Inventory & Warehouse
Source of truth for barcode-level stock, locations, and warehouse logistics.
*   **Stock Locations**: Grid mapping system locations (*Main, Inward, Return, Damage, In-Transit, E-Com Sales, Sold, Purchase Return*).
*   **Local Stock Movements**: Moves barcodes internally between storage areas (e.g. Inward -> QC).
*   **Movement History**: Auditable logs of all historical stock transfers.
*   **Inventory View (Dashboard)**: Visual charts showing stock values (MRP/Cost), location volumes, and age groupings (0-90+ days).
*   **Stock Ledger**: Append-only transaction log of all barcode events.
*   **Physical Stock Count & Variance**: Tools to upload location counts, compute discrepancies (`Sys Qty - Phy Qty = Diff`), and post adjustments.
*   **Warehouse Logistics**: Mappings for picking lists, box packaging, and bin placements.

---

## 7. Inter-Store Transfers
Manages movement of inventory between stores.
*   **Stock Transfer Out (TO)**: Scan-based dispatch queues. Updates stock status to `In Transit`.
*   **Stock Transfer In (TI)**: Scans incoming transfer items, verifies quantities, and checks for shortages/damages.
*   **GST Transfer compliance**: Integrates with e-invoice APIs for interstate branch transfers.

---

## 8. POS / Retail Checkout
Front-counter terminal checkout interface.
*   **Open POS Checkout Client**:
    *   *Customer Cards*: DOB, Mobile, Name, optional GSTIN.
    *   *Cart Grid*: Displays scanned barcodes, rates, discounts, taxes, and totals.
    *   *Redemptions*: Applies loyalty points and promo codes.
*   **Sales History**: Query tool for transactions.
*   **Manual Discount Audits**: Logs overrides entered via cashier hotkeys.
*   **POS Configurations**: Standard default choices (gender, customer create locations).
*   **Cash Handover**: Registers shift change drawer balances.
*   **Salesman Reports**: Performance metrics by salesperson.

---

## 9. Customer Directory
*   **Customer Master**: Database listing customer demographics (DOB, Email, Gender, Marital Status).
*   **Add Customer Form**: Modal dialog for fast checkout customer creation.

---

## 10. Sticker Printing Subsystem
*   **Templates**: Define sticker dimension patterns (40x20mm), DPI, and barcode types.
*   **Printer Configurations**: Thermal printer drivers mapping connection interfaces (USB/Ethernet/Wi-Fi).
*   **Print Stickers client**: Inward receipt print hooks and single tag queries.
*   **Print History**: Tracker logs containing print counts and timestamps.

---

## 11. Bulk Uploads
Parses CSV/Excel files and performs validation checks before posting.
*   **Combination Upload**, **Design Upload**, **Stock Upload**, **PO Upload**.

---

## 12. Dynamic Labels & Text Replacement
*   **UI Replacements**: Dynamic dictionary mapping original screen text labels to custom company terms (e.g., *SKU* -> *CombinationId*).

---

## 13. System Configurations
*   **Design Form Settings**: Field visibility toggles for the Design creation screen.
*   **Quick PO V2 Settings**: Toggle columns in the Quick PO grid.
*   **Purchase Form Settings**: default settings for PO, GRN, and Barcode generation.
*   **Days Configurations**: SLA rules and cancellation limits for POs, dispatches, and returns.

---

## 14. Integrations
API mappings and integration loggers.
*   **CleverTap**: Push marketing sync.
*   **OCAPI**: Salesforce Commerce Cloud integrations.
*   **Shopify**: E-commerce catalog, inventory, and order fulfillment sync.
*   **Pine Labs**: POS card payment terminals.
*   **Unicommerce**: Marketplace order sync.
*   **GST e-Invoice**: Tax IRN filings, E-Way Bill threshold configurations, and IRN reconciliation dashboard logs.

---

## 15. Assets, Expenses, and HR
*   **Fixed Assets**: Track asset acquisition, capitalization date, depreciation, and disposal.
*   **Expenses**: Processes employee claims, advances, and receipt audits.
*   **HR Foundation**: Manages employee directory, leaves, attendance, and ERP access credentials.

---

## 16. Finance & Accounting
*   **Chart of Accounts (COA)**: GL accounts definitions.
*   **Gl Mappings**: Maps transactions (GRN, Invoice, POS, COGS) to accounting rules.
*   **Vendor Payables & 3-Way Match**: Checks invoices against PO and GRN details before posting to accounting ledgers.
