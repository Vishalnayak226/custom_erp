# Usability & Manual Audit — Layman End-to-End Pass

**Date:** 2026-07-30 · **Method:** read the user manual first, then drove the live app against it as a first-time user with no prior knowledge of the codebase. Server built from `main` at `e2ba733`, Postgres `custom_erp`, logged in as `manager1` (Store Manager) and `admin` (HR/Admin, real TOTP MFA).

**Bottom line.** The *engine* is in far better shape than the *manual*. 45 of 46 catalog reports run, maker-checker is correctly enforced, GST posts correctly, and the procure-to-pay chain (PO → approve → GRN → stock → sale) works end to end. But a first-time user following the manual **cannot complete the flagship "Making a Sale" flow**, and the manual actively tells users that shipped features don't exist — eight of them in ADMIN_SOP's "known gaps" table alone, nine more in the UAT checklist that gates release sign-off. In one case it advises deleting and re-creating data to work around a button that is right there.

The dominant failure mode is not missing functionality. It is that **the docs were accurate at some earlier Stage, the app moved on, and nothing re-verified them.**

---

## Scoring

| Dimension | Score | Note |
|---|---|---|
| Backend correctness | 8.5/10 | 45/46 reports run; business rules enforced properly |
| Core flow completability (as a layman) | **2/10** | Blocked at POS by the SKU-lookup defect |
| Manual accuracy | **3/10** | 13 confirmed false or contradictory claims, across all four guides |
| Manual coverage | 4/10 | 15 of 52 screens wholly undocumented |
| Manual structure vs. SAP Help | **2/10** | 0 screenshots, 0 error reference, no task index |
| Dropdown / config easiness | 4/10 | 10 of 18 source lists empty; 19 raw-JSON fields |

---

## Part A — P0: the layman cannot complete the core flow

### A1. POS accepts only the item's hidden internal UUID — not its code, not its barcode

The single most damaging defect found. `GetItemGSTInfo` ([engines/gst.go:78](engines/gst.go#L78)) resolves the scanned SKU with `WHERE doctype='Item' AND id = $1` — it matches the **`id` column**, not `code` and not `barcode`.

Newly created items are assigned a UUID `id`. Reproduced live, on an item created through the documented flow:

| What the user types into "Scan or Enter SKU" | Result |
|---|---|
| `QA-TSHIRT-01` (the Code they entered) | `item 'QA-TSHIRT-01' not found` |
| `8901234567890` (the Barcode they entered) | `item '8901234567890' not found` |
| `7d2c4e3f-f3c4-2b7d-78ae-06ed17ab1b00` (internal UUID) | accepted |

It is worse than a plain mismatch: the POS field's own autocomplete makes the failure **certain**. `attachTypeahead`'s default `valueFields` is `['code','name','id']` ([public/app.js:507](public/app.js#L507)) — it fills in `code` first. So selecting the item from the app's own dropdown writes the exact value the backend rejects. The UUID is displayed nowhere in the POS screen, so there is no workaround in the UI at all.

USER_GUIDE §4 says *"Type or scan the item's barcode/SKU."* That instruction cannot succeed as written.

**Fix:** resolve the SKU by `code` → `barcode` → `id` at the one shared choke point (`GetItemGSTInfo`, plus the equivalent lookup in the checkout/PO paths), so every existing caller inherits it.

### A2. HSN/GST are marked optional on the form but hard-block both selling and buying

`hsn_code` and `gst_rate` are `mandatory: false` in the Item schema, so the New Item form shows no `*`. But an item without an HSN code is rejected at **both** POS checkout and Purchase Order creation.

A layman therefore creates a product that looks saved and complete, and only discovers it is unusable at the till. The message they get is:

> Tax configuration is missing for this transaction. Please contact administrator.

It does not say *which* item, does not say *HSN code*, and tells an administrator to contact an administrator. The engine's own message — `item 'X' is missing hsn_code - required before it can be sold or purchased` — is discarded before it reaches the user (see D4).

**Fix:** mark `hsn_code`/`gst_rate` mandatory on Item (or validate at save time with a clear message), and surface the specific engine detail.

---

## Part B — P1: the manual states things that are false

Each of these was verified against the running app or the source. These matter more than gaps, because a gap makes a user ask; a false statement makes them act wrongly.

### B1. "There is no Edit action — delete it and create it again" — **false, and destructive advice**

USER_SOP §1 states there is no per-record Edit on record-list screens and that *"the only way today is to delete it and create it again."* ADMIN_SOP Part D repeats it as *"verified absent in the UI code itself."*

An Edit button exists on every row ([public/app.js:11328](public/app.js#L11328)), and I used it live to add an HSN code to an item (POST to `/api/v1/doc/{doctype}/{id}`, version-checked, returned `saved`). USER_GUIDE §8 correctly says to use Edit — so the two user-facing documents **directly contradict each other**, and the SOP's version tells users to destroy records unnecessarily.

### B2. ADMIN_SOP Part D "Known Gaps" — 8 of 9 rows are wrong

Every one of these has a working sidebar screen today:

| Doc claim | Reality |
|---|---|
| GRN creation — "No screen anywhere creates one" | **Goods Receipt** workbench, `renderGRNWorkbenchView` (~142 lines) |
| Purchase Requisition — "No sidebar item, no view" | **Purchase Requisitions** menu item, record-list view |
| Putaway — "API only" | **Putaway** screen (~74 lines) |
| Pick List — "API only" | **Wave / Batch Picking** + **Mobile Picking** |
| Bin condition transition — "API only" | **Bin Conditions** screen (~49 lines) |
| Cycle count — "API only" | **Cycle Count** screen (~128 lines) |
| Approval Rules screen — "API only" | **Approval Rules** screen with full create/delete |
| Per-record Edit — "verified absent" | See B1 |

USER_SOP §7, §15, §20 and its closing note repeat the GRN and Bin claims to end users.

### B3. ADMIN_SOP §B.9 hands admins curl commands for a screen that exists

§B.9 is titled *"Changing approval-rule thresholds/roles (no screen — API only)"* and supplies a bearer-token curl recipe. The **Approval Rules** screen under Settings does this with full CRUD (`renderApprovalRulesView`, [public/app.js:12076](public/app.js#L12076)).

### B4. "The menu shows everything to everyone" — false

USER_GUIDE §3 says role-based menu trimming is *"a known, tracked improvement, not yet built."* It is fully built: `applySidebarPermissions()` toggles `.perm-hidden` per item and hides a module container when all its children are hidden ([public/app.js:1055](public/app.js#L1055)), plus Stage 27's `.module-hidden` entitlement filter. Confirmed live — `manager1` gets a scoped doctype list, `admin` gets `is_admin: true`.

### B5. The Guide's navigation does not match the actual menu

USER_GUIDE §3 presents a flat sidebar with "POS / Billing", "Purchase Order", "Inventory", "Stock Transfer", "Vendors", "Stores" as top-level entries. The real sidebar has **8 module groups with hover flyouts**; none of those screens is top-level. A user reading only the Guide will look for "Purchase Order" in the sidebar and not find it (it is under **Procurement**). USER_SOP has the correct breadcrumbs — the Guide was never updated to match.

### B6. USER_GUIDE §4's sale walkthrough omits a mandatory step

§4 goes straight from "click POS / Billing" to scanning. Checkout is hard-blocked server-side without an open cashier session:

> Cash opening is required before billing. Please open the shift drawer.

USER_SOP §3.1 covers it correctly. The Guide — the document a cashier actually reads — does not mention sessions at all.

### B7. USER_SOP §28's Setup list does not match the real menu

§28 says Setup contains 11 named retail lists plus Location/LegalEntity/Department/CostCenter/Employee. In reality `renderSidebarSubmenu()` **wipes** the hardcoded HTML list and rebuilds it from every `document_type='Master'` doctype — **51 entries** in this tenant. Most of the names in §28 (Sub Brands, Sub Styles, Product Categories, Product Types, Item Names, Secondary Colors, Fabric Colors, Polishes) **do not exist as doctypes at all**; the real ones are singular (`Brand`, `Style`, `Color`) and sit alongside `RoboticsIntegrationCredential`, `ChannelValidationRule`, `StatusTransitionRule`, `BinReplenishmentRule`, etc.

### B8. USER_SOP §1's search description is stale

§1 says the header box *"does not search across the whole app — it only filters the table you're currently looking at."* Stage 28.5 added a global suggest menu that indexes screens and doctypes and navigates to them. (The SOP is still half-right: it does not search **records**, and the placeholder — *"Search menu, category, type or HSN…"* — over-promises HSN/record search that is not implemented.)

### B9. UAT_CHECKLIST.md would have a tester sign off without testing 8+ shipped screens

The fourth guide has the same disease as the SOPs, but the consequence is worse because this document gates a release. Its closing section (line ~272) states that the following have *"no UI screen at all"* and that *"there's genuinely nothing to click"*:

**GRN, Vendor Invoice, Purchase Requisition, ASN, Sales Invoice, Putaway, bin-grouped pick lists, bin condition transitions, cycle counts.**

Every one has a working screen today. **Vendor Invoice** and **Sales Invoice** are not only shipped but fully documented in USER_SOP §7 and §11 — so the doc set contradicts itself across files. A tester working this checklist would skip all of them and record a clean pass.

### B10. Two users are required, and no document says so

Maker-checker correctly refuses `you cannot approve or reject a document you submitted`. A single-admin shop following the manual will be permanently unable to complete a Purchase Order. No guide states the two-user prerequisite.

---

## Part C — Manual coverage and structure

### C1. 15 of 52 sidebar screens are wholly undocumented

Neither SOP mentions: **Order Management, Customer(partial), ASN (Advance Shipment), Purchase Requisitions, Bin Conditions, Bin Replenishment, LPN / Cartons / Pallets, Wave / Batch Picking, Mobile Picking, Cycle Count, Offline Sync Review, Offline Queue Gaps, Extension Hooks, Configuration, System Status, Tenant Entitlements, Tenant Usage.**

Effective coverage is worse than the count suggests: **Putaway** and **Goods Receipt** *are* mentioned — only in the tables that say they don't exist.

### C2. Reports: 46 exist, 6 are named

USER_SOP §14 documents the six legacy tabs and describes the Report Catalog generically. The catalog holds **46 reports** across 12 categories (Finance 12, OMS 9, CRM 7, WMS 3, Procurement 3, Exceptions 3, …). None are listed, so a user cannot discover that "Balance Sheet", "Cash Flow Statement", "Stock Ledger" or "SLA Breach" exist, nor what parameters they need.

### C3. No error-code reference — 601 codes in code, 302 fully cataloged, 0 documented

`internal/server/error_catalog_generated.go` is a structured catalog of 302 codes carrying `Module`, `Scenario`, `UserMessage`, **`UserAction`**, `Severity`, and `DisplayStyle`. Not one appears in any guide, even though every error dialog in the app shows a code. This is the single biggest **cheap** win available: a reference appendix can be generated from the catalog rather than written.

### C4. Structural gaps against a SAP-Help-class bar

| Feature | USER_GUIDE | USER_SOP | ADMIN_GUIDE | ADMIN_SOP |
|---|---|---|---|---|
| Screenshots / diagrams | 0 | 0 | 0 | 0 |
| Version / "applies to" stamp | ✗ | ✗ | ✓ | ✓ |
| Prerequisites block per task | ✗ | partial | ✓ | ✗ |
| Troubleshooting / error table | ✗ | ✗ | ✗ | ✗ |
| Role-permission matrix | ✗ | ✗ | partial | partial |
| Task-oriented index ("how do I…") | ✗ | ✗ | ✗ | ✗ |
| Glossary | ✓ | ✓ | ✓ | ✓ |

Also missing: no end-to-end scenario tying modules together (the thing a new user most needs), no field-level reference for any form, and no search/index across the doc set.

---

## Part D — App defects found while testing

### D1. A GRN can close a PO while posting **zero stock**, silently

Stock posting reads `payload["location"]` ([internal/server/handlers_core_doc_engine.go:653](internal/server/handlers_core_doc_engine.go#L653)), but **`location` is not a declared GRN field** (`code`, `po_id`, `received_items`, `status`, `asn_id`). Any GRN created outside the bespoke Workbench — the generic record-list form, the API, bulk import — cannot supply it, so `if locationCode != "" && ...` is skipped.

Reproduced: created a GRN via the generic path → HTTP 200 `saved`, stock stayed **0**, and the PO moved to **Closed**. Goods "received" on paper, inventory never moved, no warning anywhere. Re-running with `location` supplied posted the stock correctly (0 → 10), confirming the Workbench path is sound.

Compounding it, a genuine ledger failure is only `log.Printf`'d — the response is still `200 saved`.

**Fix:** declare `location` on GRN (defaulting from the PO's `target_warehouse`), and fail loudly instead of logging.

### D2. Six endpoints return HTTP 500 — five tables missing, and no migration runner

`/admin/audit-logs/verify`, `/pinelabs/credentials`, `/pinelabs/transactions`, `/unicommerce/credentials`, `/unicommerce/inventory-syncs`, `/unicommerce/orders` all 500. Root cause from the log: `relation "tenant_default.pinelabs_transactions" does not exist` (and four siblings).

Those tables *are* defined in `db/migration.sql` (lines ~1160/1190) — but that file is a **create-from-scratch** script. There are **63 migration files and no runner, no `schema_migrations` table, no ordering metadata**. Adding a table to `migration.sql` therefore never reaches an existing database. Any deployment that was provisioned before those lines landed has six broken screens.

A background reconciliation job also retries against the missing table roughly every 8 minutes, filling the error log for every tenant.

### D3. Two reports/screens die on a single NULL value

- `customer-ledger` → HTTP 503. Cause: one `SalesInvoice` row has no `invoice_number`, and the query scans `data->>'invoice_number'` into a non-nullable Go `string`. One legacy row breaks the report for everyone.
- `/admin/audit-logs/verify` → 500 with `converting NULL to string is unsupported` on `checksum`.

The pattern is mostly handled well (106 `COALESCE(data->>…)` sites) but **10 unguarded sites remain** across `crm_analytics.go`, `crm_reports_stage26.go`, `finance_reports_stage26.go`, `reports.go`, `reports_stage26_10.go`.

Separately, the Sales Register **silently skips** corrupt carts (`[REPORTS] corrupt POSCart …` ×5) — the report under-reports rather than flagging.

### D4. The error envelope throws away the useful half of every message

`writeAPIError` ([internal/server/apierror.go:104](internal/server/apierror.go#L104)) responds with `entry.UserMessage` only. It discards:
- the engine's **specific** `ValidationError.Message` (e.g. *which* item is missing an HSN code), and
- the catalog's own **`UserAction`** field, which exists on all 302 entries and is never sent.

This is why A2's error reads "contact administrator" instead of naming the item and the field. One change at one choke point improves every error in the product.

Related: `META-0199` (Select value not allowed) renders as *"Please select a valid value from the list. Free text is not allowed for this field."* — dropping both the **field name** and the **allowed values** that the internal message carries. On a form with several Select fields the user cannot tell which one is wrong.

### D5. Redeeming loyalty points burns them without applying any discount

Clicking **Redeem Points** calls `RedeemLoyaltyPoints` ([engines/loyalty.go:97](engines/loyalty.go#L97)), which writes a `Burn` ledger entry **immediately** — the customer's points are spent at that instant, independent of whether the sale ever completes. The cart is never touched. The UI then instructs the cashier:

> Redeemed N point(s) for a discount of X. **Apply this manually to a cart line's Sale Price** before completing the sale.

Three ways the customer loses money, all of them ordinary human error at a busy till:
1. The cashier forgets to lower the price — customer pays full price *and* loses the points.
2. The sale is abandoned after redeeming — points are gone, with no reversal path anywhere in the UI.
3. The discount is mistyped — wrong amount, no cross-check.

USER_GUIDE §4 tells customers their points "can be used," implying this is automatic. **Fix:** apply the redemption to the cart total automatically, and either defer the burn until checkout succeeds or reverse it when the cart is cleared.

### D6. Report browsing trips the rate limiter

Reports are capped at **20/minute per IP** ([internal/server/middleware.go:169](internal/server/middleware.go#L169)) against a catalog of 46. Clicking through the catalog to see what each report does produces a bare *"Too many requests"* toast with no explanation or retry hint. Because the key is the IP, a shop where several users share one egress address consumes the budget collectively.

---

## Part E — "Dead easy?" — dropdown and configuration findings

### E1. 10 of 18 core master lists are empty, and empty dropdowns give no guidance

| Empty list | Screens it strands |
|---|---|
| **Employee** | HR Attendance, Leave, Expenses, Payroll — every employee picker blank |
| BankAccount | Bank Reconciliation |
| Printer | Sticker Printing |
| BOM | Production Order |
| ProductFamily / ProductAttributeDef | PIM Workbench |
| POSProfile | POS Profiles |
| CostCenter / Department / LegalEntity | GL, HR, Finance |
| Stores | (inert — see E4) |

An empty picker renders as `<option value="">Select employee</option>` and nothing else — no hint, no link. The entire HR module is unusable until someone discovers, from a single line buried in USER_SOP §23.4, that Employees are created under Setup.

### E2. 59 of 61 empty states are dead ends

The app already has the right pattern in two places — *"No items found. Create one under Setup » Item."* and *"No bank accounts yet - use 'Manage Bank Accounts' to add one."* The other **59** are bare (`No assets yet.`, `No expense claims yet.`, …), telling the user nothing about how to proceed.

### E3. 19 form fields demand hand-typed JSON — 12 of them mandatory

Users are asked to type things like `[{"sku":"...","qty":2}]` into a text box. Affected: `Appraisal`, `AppraisalCycle`, `ASN`, `BOM`, `GRN`, `OnboardingChecklist` (×2), `PaymentProposal`, `PIMProductProfile`, `POSInvoice`, `PurchaseOrder`, `ReportColumnProfile`, `ReportExportJob`, `ReportFilterPreset`, `ReturnRequest`, `Routing`, `ScheduledReport`, `TransferOrder`.

Bespoke screens build the JSON for the big ones (PO, Transfer, BOM, GRN). But **five are Master doctypes, so they sit directly in the Setup menu** — `BOM`, `Routing`, `AppraisalCycle`, `ReportColumnProfile`, `ReportFilterPreset` — where the only editor is the generic form. `AppraisalCycle.kra_template` is mandatory and labelled `KRA/KPI Template (JSON: [{"kra":"...","weight":..}])`.

### E4. `Stores` is a completely inert list — and the manual points users at it

`Location` already has `type: Store,Warehouse,HO` and is what every transaction uses (103 records). `Stores` is a parallel doctype with **zero Link fields pointing at it and zero references in Go code** — verified by query. It is empty, yet it is promoted in the sidebar (Stock → Stores) and described in USER_GUIDE §3 as *"Your shop/warehouse locations"* with a full procedure in USER_SOP §21.

A user who follows the manual and creates their shop in **Stores** will then be unable to select it anywhere.

### E5. Setup is a flat alphabetical dump of 51 technical doctype names

No grouping, no search, no descriptions. A shopkeeper opening Setup sees `AllocationRule`, `BinReplenishmentRule`, `ChannelFieldMap`, `NotificationChannelConfig`, `RoboticsIntegrationCredential`, `StatusTransitionRule`, `StorageBillingRate` interleaved with `Brand` and `Color`.

### E6. Purchase Order shows two mandatory fields both labelled "PO Number"

`po_number` → "PO Number" and `code` → "PO Number"; plus `vendor` → "Vendor" and `vendor_id` → "Vendor Code". All four mandatory. The generic form renders duplicate-labelled required fields with no way to tell them apart.

### E7. Permission denial arrives only after the form is filled

A Store Manager can open Setup → Item, complete the New Item form, click Save, and only then receive *"You do not have permission to perform this action."* Since role-based hiding already exists (B4), the create action should be gated the same way.

### E8. Smaller inconsistencies

- **Missing typeahead** on three fields whose siblings have one: `pos-return-location`, `mkt-manifest-location`, `user-location`.
- **Location typeahead over 103 records** with no grouping by `type`.
- **Employee pickers are inconsistent** — `<select>` in HR screens, typeahead in `asset-custodian`.
- **Manual codes where masters auto-generate**: `Attendance Code`, `Transfer Number`, `Claim Number`, `cart_number` are hand-typed, while master Code fields say "Auto-generated upon save".
- **Row actions are icon-only** (Edit/Delete carry `title` but no visible label).
- **Checkout response reports `gst_rate: 0`** while correctly charging 5% (tax 47.52 on 950.48).
- **`docs/ai_handover.md` says Postgres port 5435**; the live instance runs on **5432**.

---

## What is genuinely good

Worth stating plainly, because the fix list above is long:

- **45 of 46 catalog reports run**, with drill-down, saved filters, column profiles and async CSV export.
- **Maker-checker is correctly enforced**, including self-approval refusal, with a clear message.
- **Business-rule messages are often excellent** — *"PO cannot be released until approval is completed."*, *"Cash opening is required before billing. Please open the shift drawer."* The problem is the generic catalog messages, not the specific ones.
- **The procure-to-pay chain is correct** end to end: PO → approval → GRN → stock 0→10 → sale → stock 8, with GST computed correctly.
- **Role/module permission filtering is real** and applied at both menu and API layers.
- **The doc set's plain-language register is genuinely good** — USER_GUIDE's tone is right. It is accuracy and coverage that failed, not the writing.

---

*Backlog derived from this audit: `docs/micro_checklist.md` Stage 30.*
