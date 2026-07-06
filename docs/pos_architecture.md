# In-House ERP: Pluggable POS Architecture Specification

This document defines the technical design and business logic for the Point of Sale (POS) system. It combines standard retail POS features (barcode scan, loyalty, discounts) with learnings from restaurant POS patterns (kitchen tickets, seating layouts, bill splits) and ERPNext POS designs (offline-first databases, cash opening/closing registers, POS profiles).

The POS is designed as an **extensible module** on top of our core ERP kernel, making it adaptable to retail, food & beverage (F&B), and service-oriented checkouts.

---

## 1. Architectural inspiration & POS Models

By reviewing successful POS systems and ERPNext POS implementations, we integrate key operational paradigms:

| Platform / Source | Core Pattern | Key Advantage | Implementation in Our ERP |
| :--- | :--- | :--- | :--- |
| **ERPNext POS** | **Offline-First Caching & POS Profiles** | Checkout keeps running during internet downtime. Syncs invoices back when connection resumes. Enforces store-specific POS Profiles. | Frontend SPA uses local storage (IndexedDB/PouchDB) for SKU catalog caching. Tracks cash handovers via explicit Cash Opening & Closing documents. |
| **Restaurant POS** | **KOT & Seating Layouts** | Kitchen Order Tickets (KOT) are routed to printers, and orders are bound to physical floor tables. | Extensible orders schema maps custom location markers (e.g. Table 4, Seat B) and dispatches print jobs to distinct print queues. |
| **Retail POS** | **High-speed Barcode Scanning** | Minimizes cashier keystrokes. | Barcode parsing triggers immediate cart item additions with hotkey overrides (`Ctrl+D` for discounts, `Alt+G` for scanner focus). |

---

## 2. POS Profile & Access Controls (The Configurations)

Instead of hardcoding checkout options per client, POS environments are controlled by a **POS Profile** metadata document:

- **`pos_profile` Schema**:
  - `profile_id` (Primary Key)
  - `store_id` (Links to Store location)
  - `default_warehouse` (Default stock depletion source)
  - `default_customer` (Standard Walk-In account)
  - `allowed_payment_modes` (JSON array: Cash, Card, UPI, Vouchers)
  - `allow_manual_discount` (Boolean)
  - `max_discount_percent` (Enforces maximum cashier override limits)
  - `offline_sync_enabled` (Boolean)

---

## 3. Cash Drawer Registers & Open/Close Workflows

To ensure strict financial reconciliation and audit-ready drawers, checkouts must follow a structured session model:

```
[Cash Opening Entry] ---> [Shift Sales / Transactions] ---> [Cash Closing Entry]
(Count starting float)    (Standard invoice checkout)        (Count cash + compare expected)
                                                                    |
                                                                    v
                                                     [Drawer Variance Reconciliation]
```

### 3.1 Cash Opening Entry
Before performing any sales, the cashier must post a **Cash Opening Entry**:
- **Fields**: POS Profile, Cashier User, Date & Time, Starting Cash Float Amount.
- **System Action**: Locks the cashier interface until the opening entry is verified. Sets session state to `OPEN`.

### 3.2 Cash Closing Entry
At the end of a shift, the cashier posts a **Cash Closing Entry**:
- **Fields**:
  - Expected sales amounts (computed by system across payment modes).
  - Actual counted cash and card slips in the drawer.
  - Variance (Actual - Expected).
  - Reason for variance.
- **System Action**: Posts shift totals, prints closing summary receipt, releases the cashier session, and updates session state to `CLOSED`.

---

## 4. Database Syncing & Offline-First Strategy

To prevent sales disruption during internet failures, the POS client implements an **offline-first local database caching system**:

1.  **Catalog Synchronizer**: On session open, the client downloads the active SKU catalog, pricing rules, and customer directory to the browser's local **IndexedDB**.
2.  **Offline Billing**: Cart additions, calculations, and tax computations run locally in the browser. Completed bills are saved to an offline queue inside IndexedDB.
3.  **Automatic Synchronization**: A background worker polls network connectivity:
    *   *Online state*: Dequeues offline invoices, posts them to the core Go endpoint `/api/v1/doc/SalesInvoice`, and clears local queue.
    *   *Duplicate check*: Employs client-generated UUIDs as idempotency keys to guarantee no double-billing occurs during network drops.

---

## 5. Extensible POS Cart Schema (Retail, F&B, Services)

The checkout document (`SalesInvoice`) utilizes an extensible layout format to support multiple industry variants:

```json
{
  "invoice_id": "SI-WH01-26-27-000001",
  "pos_profile": "PROF-01",
  "customer_id": "CUST-002",
  "customer_details": {
    "mobile": "9876543210",
    "name": "Jane Doe"
  },
  "industry_context": {
    "type": "RESTAURANT", 
    "table_number": "Table 12",
    "number_of_guests": 4,
    "kot_id": "KOT-9842"
  },
  "cart_lines": [
    {
      "barcode": "8901234567",
      "item_id": "ITM-01",
      "qty": 1.000,
      "rate": 450.00,
      "discount": 50.00,
      "taxable_amount": 400.00,
      "tax_components": {
        "cgst": 18.00,
        "sgst": 18.00
      },
      "total_amount": 436.00
    }
  ],
  "payment_methods": [
    { "mode": "Cash", "amount": 200.00 },
    { "mode": "UPI", "amount": 236.00 }
  ],
  "cash_handover_session": "SESS-0042"
}
```

### Industry-Specific Extensions:
- **Retail**: Cart lines focus on individual barcode scanning, serial numbers, and automatic discount code calculation.
- **F&B / Restaurant**: Enforces table mappings, split-bill allocations (by guest count or line items), and generates kitchen tickets (KOT) dispatched to distinct back-house thermal printers.
- **Services**: Integrates calendar time-slot mappings and provider ID assignments to track sales commission variables.
