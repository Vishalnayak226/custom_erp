# User Acceptance Test Checklist

A single walkthrough script for a human tester to click through every module and page in the app and confirm it works. No coding knowledge needed — if a step says "click X, expect Y" and you see Y, tick the box and move on.

Companion to [USER_GUIDE.md](USER_GUIDE.md) (explains what each screen is *for*) and [ADMIN_GUIDE.md](ADMIN_GUIDE.md) (explains how to run/operate the system). This document only tests that things work.

---

## How to use this checklist

1. Work top to bottom, in order — later sections sometimes depend on data created earlier (e.g. Approvals needs something submitted first).
2. Each item has a checkbox. Tick `[x]` if it behaves as described. If it doesn't, write down what you saw next to the item (or in a copy of this file) instead of guessing why.
3. **Some screens are known to be unfinished placeholders, not bugs** — they're marked ⚠️ **Known limitation** below. Seeing "Module Setup Pending" on those specific screens is a *pass*, not a fail. If you see that message anywhere *not* marked with ⚠️ below, that's worth reporting.
4. **Some actions will correctly refuse you** depending on which user you're logged in as — that's the security/role system working, not a bug. See "Role Access Notes" under each section, and the master notes in [Prerequisites](#0-prerequisites).
5. Re-run this checklist after any significant change to `public/app.js`, `public/index.html`, or the database schema — it's meant to be reusable, not one-time.
6. **Sidebar navigation was regrouped by module.** The left sidebar now shows only ~12 top-level entries; most of the screens named throughout this checklist live inside a module's **hover flyout** (hover the module name, or click it for keyboard/touch), not as their own top-level sidebar link anymore. Every section below still tells you which screen to open — just expect to hover/click a module group first. Current grouping: **POS** (POS/Billing, POS Profiles) · **Finance** (Finance/GL, Approvals) · **Fulfillment & Marketplace** (Fulfillment, Marketplace) · **Reports** (own top-level entry) · **Procurement** (Purchase Orders, Vendors, RFQ/Quotes) · **Inventory & WMS** (Inventory, Transfers, Bin Master, Stores, Sticker Printing) · **HR & Assets** (HR, Fixed Assets, Expenses) · **Manufacturing** and **PIM** (own top-level entries) · **Master Definition** (unchanged dynamic flyout of ~25 master doctypes) · **Admin & Settings** (Users, Roles, Prefix Configs, Dynamic Labels, Database Schema Design, Activity Log). Dashboard stays a direct top-level link.

---

## 0. Prerequisites

- [ ] App is reachable at `http://localhost:8080` (or your environment's URL) and shows a login screen, not an error page.
- [ ] You have the login credentials. Dev environment: see `DEV_CREDENTIALS.local.txt` at the project root (not committed to git — ask whoever set up the environment if you don't have it). There are 4 seed users, one per role:

  | Username | Role | Notes |
  |---|---|---|
  | `admin` or `system` | HR/Admin | Full access to everything. Requires an authenticator app code (MFA) at login. |
  | `manager1` | Store Manager | Can view most masters/transactions; cannot create Purchase Orders, cannot read Vendor/Item/Customer (see note below). |
  | `cashier1` | Cashier | Scoped to POS-related screens; cannot read Vendor/Item/Customer/Location (see note below). |

- [ ] **Role Access Notes (read once, applies everywhere below)**: the sidebar shows the same menu to every role — it does **not** hide items you're not allowed to use. If a screen shows a red "You do not have permission to..." message instead of data, and you're logged in as `manager1` or `cashier1`, that is very likely expected — re-test that specific step as `admin` before reporting it as a bug. Known gap as of 2026-07-19 (tracked in `docs/micro_checklist.md` Stage 18): `manager1` and `cashier1` cannot read Vendor, Item, or Customer records at all, which limits how much of Purchase Orders/POS/RFQ/Manufacturing they can meaningfully test — use `admin` for full coverage of those screens. As of the **Roles** screen shipping (§19), this is now something you can fix yourself mid-testing (log in as `admin`, grant the read, log back in as the other role) rather than a hard blocker requiring direct database access.

---

## 1. Login / Logout

- [ ] **1.1 Login screen loads** with Username/Password fields and a "Sign In" button.
- [ ] **1.2 Wrong password** shows an error message and does not let you in.
- [ ] **1.3 Correct login (non-MFA user, e.g. `manager1` or `cashier1`)** takes you straight to the Dashboard.
- [ ] **1.4 Correct login (MFA user, `admin`/`system`)** prompts for a 6-digit authenticator code after the password, and only lets you in with the correct one.
- [ ] **1.5 Sidebar shows your name/role** in the bottom-left account area once logged in.
- [ ] **1.6 Sign out** (account menu, bottom-left → Sign Out) returns you to the login screen and you can't get back in by pressing the browser Back button.
- [ ] **1.7 Session persists** across a page refresh (F5) while logged in — you should not be bounced back to the login screen.

---

## 2. Dashboard

- [ ] Loads with no error, shows summary stat tiles (Record Types Registered, Audit History Count, Active Schema Tenant, Platform Core Health).
- [ ] Quick-launch cards (Database Schema Design, Dynamic Labels, Prefix Configs, Activity Log) navigate to the right screen when clicked.

---

## 3. POS / Billing

- [ ] Screen loads with a session status bar at the top, then Location Code, Customer Code, and "Scan or Enter SKU" fields.
- [ ] **Cashier session required (Stage 20.7)**: enter a Location Code — the status bar shows "No open session... open one before selling" with an **Open Session** button. Click it, enter an opening cash float — status changes to "Session open at <location>" and the button becomes **Close Session**. "Complete Sale" fails with a clear error if you skip this step (don't expect it to silently work like before Stage 20).
- [ ] Trying to open a second session for the same cashier or location while one is already open is rejected with a clear error, not a silent duplicate.
- [ ] **Typeahead**: typing 2+ characters into Location Code shows a suggestion dropdown (needs a role that can read Location — `admin`); picking a suggestion fills the field.
- [ ] Typing a SKU that doesn't exist and pressing Enter shows an error/no-match message, not a blank crash.
- [ ] Adding a real, in-stock SKU adds a line to the cart table with quantity/price fields you can edit.
- [ ] Cart total updates as you change quantity or price on a line.
- [ ] "Remove" on a cart line removes it and updates the total.
- [ ] "Check Points" / "Redeem Points" (with a Customer Code entered) return a response, not a silent failure.
- [ ] **Discount % field (Stage 20.10)**: with a session open and at least one cart line, leave Discount % at 0 and complete the sale as normal — should complete immediately same as before. Then try a discount at or above the configured threshold (10% by default) — the sale should return "requires ... approval" instead of completing, and the cart clears. Log in as a *different* user with the required role (e.g. Store Manager) and decide it from **Approvals** (§7) — only then does the sale actually complete (stock deducted, GL posted). The *same* cashier who submitted it should be rejected as a maker-checker violation if they try to approve their own cart.
- [ ] "Complete Sale" with at least one cart line, a payment mode selected, and an open session completes the sale, clears the cart, and offers a **Print Receipt** confirm dialog (Stage 20.14) — accepting opens the browser print dialog with a receipt layout.
- [ ] **Process a Return panel (Stage 20.11)**, below the sale cart: enter the original order/cart number and a return location, add a SKU line (typeahead-backed), and submit — should confirm the return processed and restock. (The underlying return-processing feature is old; this panel is its first-ever UI entry point.)
- [ ] Session status bar: click **Close Session**, enter a counted-cash figure — should report the expected cash and the variance (counted minus expected), not just a bare confirmation.

---

## 4. Finance / GL

- [ ] Loads a trial balance table (Account Code, Account Name, Type, Debit, Credit) with Total Debits/Credits and a balanced/unbalanced status indicator.
- [ ] ⚠️ **Known limitation**: this screen is view-only — there is no button to create a new GL account or post a manual journal entry from the UI. That's expected; do not report it as missing unless you were specifically asked to test a create flow.

---

## 5. Fulfillment

- [ ] Loads a table of fulfillment tasks with Task ID, Order ID, Location, Status, and status-appropriate action buttons.
- [ ] Full lifecycle: **Pending** → "Start Picking" or "Reject" → **Picking** → "Mark Packed" or "Reject" → **Packed** → "Dispatch" → **Dispatched** (no further actions). Confirm each transition button only appears at its correct status and moves the task to the next one.
- [ ] Only tasks for your own location are visible unless you're `admin`.

---

## 6. Marketplace

- [ ] **Settlements panel** loads with a form (Settlement ID, Channel, Total Sale, Commission, Net Payout, Order IDs) and an existing-settlements table below it.
- [ ] Channel dropdown includes Shopify/Amazon plus any real Channel records configured in the system (Stage 18 fix — previously hardcoded to just Shopify/Amazon).
- [ ] Submitting "Reconcile" with all fields filled adds a new row to the settlements table.
- [ ] **Logistics Bookings panel** loads below it with its own form and table; submitting "Book" adds a row.

---

## 7. Approvals

- [ ] Loads a table of documents awaiting your sign-off (Doctype, Document ID, Amount, Location, Approve/Reject buttons). Empty is fine ("Nothing awaiting approval") if nothing is pending.
- [ ] To generate something to approve: submit a Purchase Order for approval elsewhere in the app (as `admin`), then come back here and confirm it appears.
- [ ] "Approve" removes the item from the list and the underlying document's status changes to Approved.
- [ ] "Reject" prompts for an optional reason, then removes the item and marks the document Rejected.

---

## 8. Reports

Four tabs across the top: **Current Stock**, **Sales Register**, **Vendor Ledger**, **Payables Ageing**.

- [ ] Each tab loads its own table without error when clicked (an empty table with a "no data" row is a pass if there's genuinely nothing to report).
- [ ] Switching tabs doesn't require a page reload and the active tab is visually highlighted.

---

## 9. RFQ / Quotes

- [ ] "New RFQ" form (RFQ Number, description, etc.) creates a new RFQ that appears in the list below.
- [ ] Clicking into an RFQ shows its Quotes panel with a "Submit Quote" form (Quote Number, **Vendor** — has typeahead as of Stage 18, Quoted Price, Lead Time).
- [ ] Submitting a quote adds it to the quotes table with status "Submitted".
- [ ] "Select as Winner" on a submitted quote marks it Selected and closes out the RFQ (other quotes should no longer be selectable).

---

## 10. Sticker Printing

- [ ] Printer dropdown is populated from real Printer records (not hardcoded).
- [ ] Entering a valid SKU and clicking "Add" adds it to the print queue list below the form.
- [ ] "Reprint Reason" field accepts free text (this one is intentionally not a picker — it's a note field).
- [ ] Generating/printing the sheet produces a print-history entry visible further down the page.

---

## 11. HR

Three tabs: **Attendance**, **Leave**, **Payroll Export**.

- [ ] **Attendance tab**: Employee dropdown is populated with real employees; **Location field has typeahead** (Stage 18). Marking attendance for an employee/date adds a row to the table below.
- [ ] **Leave tab**: same Employee dropdown pattern; submitting a leave request adds it to the table.
- [ ] **Payroll Export tab**: loads without error and offers an export action.

---

## 12. Fixed Assets

- [ ] "Create" form loads with Category, **Location (typeahead, Stage 18)**, **Custodian (typeahead against Employee, Stage 18)**, Acquisition Date, etc. New asset starts at status **Draft**.
- [ ] Creating an asset adds it to the asset register table (Asset #, Category, Location, Custodian, Cost, Accum. Depreciation, Net Block, Status).
- [ ] Full lifecycle: **Draft** → "Capitalise" → **Capitalised** → "Transfer" (stays Capitalised, location/custodian change) or "Dispose" (moves to a disposed/terminal status). Confirm the right button(s) appear at each status.
  - ⚠️ **Known limitation**: the Transfer action's new-location/new-custodian prompts are plain browser pop-ups, not the typeahead picker used elsewhere — this is a documented, deferred gap (`docs/micro_checklist.md` Stage 18), not something newly broken.

---

## 13. Expenses

- [ ] "Create Draft" form loads with Employee dropdown, **Location (typeahead, Stage 18)**, Category dropdown, Amount/GST/Advance fields, and a free-text Purpose field.
- [ ] Creating a claim adds it to the table below with a status badge, starting at **Draft**.
- [ ] Full lifecycle: **Draft** → "Submit for Approval" → appears in **Approvals** (§7) → Approve/Reject there → **Approved** → "Finance Verify" → **Verified** → "Mark Paid" → **Paid**. Confirm each button only appears at its correct status.

---

## 14. Manufacturing

- [ ] **BOM (Bill of Materials) form**: **Parent Item field has typeahead against Item** (Stage 18); Components field accepts a `sku:qty` list (this is intentionally a free-text mini-syntax, not a picker — see `docs/micro_checklist.md` Stage 18 for why). Creating a BOM adds it to the BOM list.
- [ ] **Production Order form**: BOM dropdown is populated from real BOMs; **Location field has typeahead** (Stage 18). New order starts at **Draft**.
- [ ] Full Production Order lifecycle: **Draft** → "Issue Material" → **Material Issued** → "Complete (Receive FG)" → **Completed**. Confirm each button only appears at its correct status and stock moves as expected (raw materials consumed, finished goods received — cross-check against Reports → Current Stock, §8, if you want to verify the actual quantity movement).

---

## 15. PIM (Product Information Management)

The tab bar has 9 entries, but only the first 3 are "real" PIM tabs — the other 6 immediately take you to the same generic master table used elsewhere in the app (§16), just pre-filtered to that doctype. Don't expect PIM-specific styling on those 6; a plain table is correct.

- [ ] **15.1 Dashboard tab**: loads 8 stat cards (Products, Incomplete, Pending approval, Ready to publish, Published, Missing main image, Publish queued, Publish failed) with no error. Clicking a card navigates you either to the Workbench tab or (for "Pending approval") to the ProductContent master table.
- [ ] **15.2 Workbench tab — list**: selecting a Product Family filters the item list; selecting an item opens a detail panel below with a completeness score (e.g. "72%") and a list of missing fields (or "Nothing - fully complete").
- [ ] **15.3 Workbench tab — attribute values**: in the item detail panel, the Attribute dropdown is populated from real Attribute Definitions; picking one, entering a value, and clicking Save updates the item (re-check the completeness score changes if the attribute was one of the missing ones).
- [ ] **15.4 Workbench tab — content**: fill in Title/Short Description/Long Description/SEO Title/Tags and click "Save Draft" — it saves without leaving the page. Click "Submit for Approval" — the content should then show up in **Approvals** (§7) for an HR/Admin to decide.
- [ ] **15.5 Workbench tab — media**: choose an image file, pick a Role (Main Image/Gallery/Variant Image/Lifestyle/Certificate/Internal QC/Video/Other), click Upload — a thumbnail appears in the gallery below. "Deactivate" on a thumbnail removes it from the gallery.
- [ ] **15.6 Workbench tab — channel publishing**: the Channel dropdown is populated from real Channel records (not hardcoded); clicking "Publish" produces a result — success with an external ID, or a clear rejection reason — as a new row in the publish log table below.
- [ ] **15.7 Reports tab**: pick a report from the dropdown (**Content aging**, **Duplicate media**, **Channel mapping gaps**, **Attribute quality**) and click "Run report" — each produces a results table (or a clear "no results" state) without error.
- [ ] **15.8 Product Families / Attribute Definitions / Family Attributes / Channels / Category Mapping / Field Mapping** (the other 6 tabs): each one takes you to a generic master table for that doctype; "New <X>" creates a record and it appears in the table.
- [ ] **15.9 Bulk Import** (on the **Item** master table specifically — Master Definition → Item, or via the Workbench list — not a PIM-tab feature): "Bulk Import" → "Download Template CSV" gives you a real CSV; uploading a filled-in copy shows a preview/result summary (rows imported / rows rejected with reasons), not a silent pass-through.
- [ ] **15.10 Bulk Edit** (Item master table only): select 2+ rows via the checkboxes, click "Edit Selected", pick a field and a new value, confirm — all selected rows update to that value. If the doctype is approval-gated, previously-Approved rows should drop back to Pending Approval, not stay silently Approved with an unreviewed change.

---

## 16. Master Definition (sidebar submenu)

This submenu is **built dynamically** from every registered "Master" doctype, so the exact list of entries will grow over time — you're not limited to a fixed set of names. As of this checklist's last update it includes at least: Location, Legal Entity, Department, Cost Center, Employee, Item, Printer, Batch, Brand, Color, Model, Size, Style, Vendor, Customer, Channel, Product Family, Attribute Definition, Family Attribute, POS Profile, plus any custom doctypes registered via Database Schema Design.

For **each** item in the submenu:
- [ ] Clicking it loads a table (existing records, or "No records found" if empty) with a working search box.
- [ ] "New <X>" opens a form built from that doctype's real fields (text/number/dropdown/typeahead as configured) and saving adds a new row to the table.
- [ ] Deleting a record (trash icon) removes it from the table (soft-delete — see `docs/architecture/` if you need to verify it's recoverable, not gone).
- [ ] "Bulk Import" → "Download Template CSV" → upload a filled copy → shows a preview/result summary, not a silent pass-through. (Available on every master, most relevant on Item — see PIM §15.9.)
- [ ] On **Item** specifically (or any other PIM-module master): select 2+ rows via the checkboxes → "Edit Selected" appears in the bulk-edit bar → bulk-updates all selected rows (see PIM §15.10 for the full flow).

*(You don't need to repeat this full sequence for all 15-20+ entries individually — spot-check at least 3-4 including Vendor, Location, and Item, since those back this session's new typeahead fields elsewhere in the app.)*

---

## 17. Vendors / Stores / POS Profiles (in the Procurement / Inventory & WMS / POS module flyouts respectively)

- [ ] **Vendors**: same generic table/create/delete behavior as any Master Definition entry (it points at the same underlying screen).
- [ ] ⚠️ **Known limitation — Stores**: clicking this currently does **not** load correctly (a naming mismatch between the menu code and the registered doctype, plus the doctype has no fields configured yet). Expect either an empty/broken screen or a load error. This is a known gap, not something to spend time debugging — note it as "confirmed still broken" and move on.
- [ ] **POS Profiles (Stage 20.6)**: same generic table/create/delete behavior as Vendors above. Creating one (profile name, Location, default payment mode, opening cash float, status) adds it to the table. This is metadata only right now — nothing else in the app reads it yet (POS session opening still asks for the cash float directly rather than defaulting from a profile).
- [ ] **Bin Master (Stage 20.16)**: same generic table/create/delete behavior. Creating one (bin code, Location, zone/aisle/rack, capacity, status) adds it to the table. Also metadata only — actual putaway/pick-list/condition-transition operations are backend-only right now (§24).

---

## 18. Purchase Orders

- [ ] "New Purchase Order" form loads with PO Number, **Vendor (typeahead, Stage 18)**, **Target Warehouse (typeahead against Location, Stage 18)**, **Location (typeahead, Stage 18)**, Total Amount, GST Rate, Interstate checkbox.
- [ ] "Calculate GST" with an amount and rate filled in shows a CGST/SGST or IGST breakdown.
- [ ] "Create Draft" (as `admin` — other roles are expected to be denied here, see Role Access Notes) adds the PO to the table below with status "Draft".
- [ ] "Submit for Approval" on a Draft PO changes its status to "Pending Approval" and it should then appear under **Approvals** (§7).
- [ ] Typing a Vendor/Location value that matches nothing is still accepted (no error) — confirms the picker suggests but doesn't force a match, by design.

---

## 19. Inventory / Transfers / Users / Roles (Inventory & WMS / Admin & Settings module flyouts)

All four of these used to be dead links falling through to a "Module Setup Pending" placeholder. As of this checklist's last update, none of them are placeholders anymore — if you land on "Module Setup Pending" for any of these, that's a regression, not expected behavior.

- [ ] **Inventory**: a searchable table of current stock per SKU/location (same underlying data as Reports → Current Stock, §8, but with a client-side search box that tab doesn't have). Typing in the search box filters the table without a page reload.
- [ ] **Transfers**: a form to create a `TransferOrder` (from/to warehouse, items) plus a list with status-appropriate action buttons. Full lifecycle: **Draft** → "Mark Approved" → **Approved** → "Dispatch" → **Dispatched** → "Receive" → **Received**. Confirm each button only appears at its correct status.
  - ⚠️ **Known limitation**: "Mark Approved" is a direct status edit, not routed through the maker-checker Approvals screen (§7) — `TransferOrder` has no `approval_rules` entry configured. This is a documented, deliberate scope decision, not a bug.
  - [ ] **Pack (Stage 20.19, optional step)**: from **Approved**, a "Pack" button prompts for a Box ID per line item, then moves the order to **Packed** — a new status between Approved and Dispatched. "Dispatch" still works directly from Approved too (packing is optional, not a required gate); from **Packed**, only "Dispatch" appears.
- [ ] **Users**: a "New User" form (username, password, email, role) and a table of existing users with Deactivate/Reactivate buttons. Creating a user with a duplicate username shows a clear "already taken" error, not a generic failure. You cannot deactivate your own account (should show a clear error, not silently no-op).
- [ ] **Roles**: an "Add or Update a Grant" form (Role, Record Type, Read/Create/Update/Delete checkboxes) and a table of every currently-configured `(role, doctype)` permission. Saving a grant for a role/doctype pair that already has one updates it in place rather than duplicating the row. This is a real, live way to close gaps like Stage 18's "Store Manager/Cashier can't read Vendor/Item" finding — try granting `Store Manager` read access to `Item` here and confirm the POS SKU typeahead (§3) then starts suggesting for that role.

**Users** and **Roles** are HR/Admin-only on the backend (`requireHRAdmin` in every handler) — expect a clear 403, not a crash, if you reach either as a non-admin role. **Inventory** is gated only by the "reports" module being enabled for the tenant (any role can see it once that's on); **Transfers** goes through the ordinary `role_permissions` check for the `TransferOrder` doctype, same as any other generic-doc screen.

---

## 20. Prefix Configs

- [ ] Loads a table of document-numbering prefix rules (doctype, prefix, separator, digit count, reset frequency).
- [ ] Editing a prefix rule saves and is reflected in the table.
- [ ] (Optional deeper check) Creating a new document of that doctype elsewhere in the app produces an auto-generated number matching the configured prefix pattern.

---

## 21. Dynamic Labels

- [ ] Loads a table/form for renaming terminology used throughout the app (e.g. relabeling "Vendor" to "Supplier").
- [ ] Saving a custom label and navigating to a screen that uses that term shows the new label instead of the original.
- [ ] Clearing/resetting a custom label reverts the screen back to the original term.

---

## 22. Database Schema Design (formerly "DocType Builder" in the sidebar — same screen, renamed for a general audience; "DocType" terminology has since been fully removed from the UI/docs too, replaced by "Record Type" throughout)

- [ ] Left panel lists every registered Record Type; clicking one loads its field configuration on the right.
- [ ] "Register New Record Type" (prompts for name/module/type) creates a new empty record type that then appears in the left-hand list and in **Master Definition** (§16) if registered as type "Master".
- [ ] "Add Field" on a record type (prompts for fieldname/label/fieldtype/mandatory/options) adds a new field, and that field then appears on that record type's create form elsewhere in the app.
- [ ] Deleting a field removes it from the record type's configuration and its create form.
- [ ] ⚠️ **Known limitation**: the page subtitle mentions "setup RBAC rules," but there is no RBAC editor on *this* screen specifically — that's now the **Roles** screen instead (§19). Don't spend time looking for one here; that's expected, not a missed feature. (This note used to say role/doctype access was database-only with no UI anywhere — that's no longer true as of the Roles screen shipping; only the "not on this particular page" half still holds.)

---

## 23. Activity Log (formerly "Log Hub" in the sidebar)

Three tabs: **Audit Logs**, **System Errors**, **Integration Payloads**.

- [ ] **Audit Logs tab** (default) loads a table (User, Action, Details, Timestamp) reflecting real actions — spot-check that at least one action you took earlier in this checklist (e.g. creating a PO, deciding an approval) shows up here.
- [ ] **System Errors tab** loads without error.
- [ ] **"Test Panic Recovery"** button (top-right of the page) deliberately triggers a server-side panic and should come back with a handled error response, not a browser network failure — then a new row should appear on the System Errors tab reflecting it.
- [ ] **Integration Payloads tab** loads a table of integration events; any row with status "Failed" shows a "Retry" button — clicking it asks for confirmation, then queues a retry (confirmation toast, not a silent no-op). Rows with other statuses show no Retry button — that's correct, not missing functionality.

---

## 24. Modules with no page to test (backend/API-only — don't go looking)

Every module and doctype in the system is accounted for somewhere in this checklist — either as a section above, or explicitly listed here because it has **no UI screen at all** as of this checklist's last update. These are real, working backend features (unit/integration-tested, some live-verified — see `docs/project_ledger.md`), just not reachable by clicking around the app yet. If you're trying to test one of these and can't find it, that's expected — it's not a broken link, there's genuinely nothing to click:

- **GRN (Goods Receipt Note)** — vendor invoice 3-way match's receiving side (Stage 17.8). No screen.
- **Vendor Invoice** — the 3-way match + payment feature itself (Stage 17.8). No screen (Expenses §13's "Mark Paid" is a *different* payment flow — ExpenseClaim, not VendorInvoice).
- **Purchase Requisition** — the pre-PO requisition workflow (Stage 17.7). No screen.
- **ASN (Advance Shipment Notice)** — inbound shipment tracking. No screen.
- **Chart of Accounts / manual GL journal entries** — GL accounts are a fixed backend table, not an editable master; Finance (§4) is view-only. No create/edit screen for either.
- **POS Invoice, Sales Invoice, Stock Ledger Entry, GL Post** — these are records generated *by* other actions (completing a POS sale, GL postings from GST/checkout, etc.), not things you create directly through a form. Nothing to test here beyond confirming the actions that generate them work (POS §3, Finance §4).
- **PIM: Import Job, Product Attribute Value (as its own list), PIM Product Profile** — internal/supporting records for the PIM flows already covered in §15; no separate screen of their own.
- **Putaway, bin-grouped pick lists, bin condition transitions (Damaged/QC-Hold/RTV), cycle counts** — Stage 20 Track B.2 WMS backend (`/api/v1/wms/*`). Bin *master data* has a screen (§17); actually assigning stock to a bin, generating a pick list, moving stock between conditions, or reconciling a cycle count are all backend/API-only right now. `CycleCountLine` rows are created via the same "Bulk Import" flow any master/transaction table supports (§16) - just no dedicated WMS screen wraps the putaway/pick-list/condition/reconcile actions yet.

---

## Sign-off

| Tested by | Date | Environment (URL) | Role(s) used | Overall result |
|---|---|---|---|---|
| | | | | ☐ Pass ☐ Pass with known limitations only ☐ Fail — see notes |

**Notes / issues found (not already listed as a ⚠️ Known limitation above):**

-
-
-
