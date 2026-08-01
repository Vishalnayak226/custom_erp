# User SOP — Step-by-Step Procedures

This is the deep companion to **[USER_GUIDE.md](USER_GUIDE.md)**. The Guide explains *what the system is* and walks through two examples (Making a Sale, Moving Stock) in full click-by-click detail. This SOP gives that same literal, click-by-click depth for **every** screen in the sidebar — one section per screen, in the order it appears in the menu. If a term isn't explained here, check the Guide's glossary (§13) first; new terms this SOP introduces are collected in §30 below.

This document assumes you've already read USER_GUIDE.md §2 (Logging In) and §3 (Finding Your Way Around). It does not repeat those.

---

## 1. Before You Begin — Patterns Used on Every Screen

A few things work the same way everywhere in this system. Learn them once here instead of re-reading them on every screen below.

- **The header search box** (top of the screen, "Search menu, category, type or HSN..."). Despite the placeholder text, it does **not** search across the whole app — it only filters the table you're currently looking at, and only on screens that show a plain record list (what this SOP calls a "record-list screen" — Vendors, Stores, Brands, Colors, and everything else reached from **Setup** or a similar list). On any other screen (POS, Reports, PIM, etc.) it does nothing.
- **Creating a new record on a record-list screen**: click the plus-icon **New [Record Type]** button (top right), fill in the fields — anything with a red **\*** is required — and click **Save**. A field showing greyed-out placeholder text like *"Auto-generated upon save"* is numbered for you: leave it alone, you can't type in it.
- **Document numbers are never typed.** Every transaction — Purchase Order, Goods Receipt, ASN, RFQ, Vendor Quote, Stock Transfer, Expense Claim, Leave, Employee Loan, Grievance, Production Order, Attendance — gets its number from a series when you save, shown as *"Auto (PO series)"* until then. Administrators control the format on the **Prefix Configurations** screen; see [ADMIN_GUIDE](ADMIN_GUIDE.md) §B.3.2. Gaps in a series are normal (a failed save doesn't reuse its number) and are not missing documents.
- **There is currently no "Edit" action on a plain record-list screen** — only a trash-can **Delete** icon at the end of each row. If you need to correct a mistake in a record like a Vendor, Brand, or Color, the only way today is to delete it and create it again with the right values (deleting is permanent and asks you to confirm first). The one exception is **PIM record types** (Product Families, Attribute Definitions, etc. — see §27), which support a multi-select **Bulk Edit** that changes one field across several records at once.
- **Bulk Import**: most record-list screens also have a **Bulk Import** button (top right, next to New). It opens a dialog where you can download a template CSV, fill it in, and upload it. Before committing, click **Preview (no changes written)** to see exactly what would be created/updated/rejected without actually saving anything — always do this first on a large file. Then click **Process Import** to actually save it.
- **Errors**: a red banner across the top of a form, a small pop-up ("toast") in the corner, or a centered dialog box will tell you what went wrong in plain language — read it, it's specific (e.g. "not enough stock" rather than just "error"). If you see a **correlation ID**, pass it to your admin if you need help.
- **Fields that auto-suggest as you type** (Vendor, Location, Item/SKU, Employee, Customer) are a convenience only — you can still type a value that isn't in the list; picking a suggestion just fills the field in faster.

---

## 2. Dashboard

1. Click **Dashboard** in the sidebar (or it's what you see right after logging in).
2. You'll see four summary tiles (registered record types, audit history count, active tenant, platform health) and four shortcut cards: **Database Schema Design**, **Dynamic Labels**, **Prefix Configs**, and **Activity Log**. These four are admin/config tools — see **[ADMIN_SOP.md](ADMIN_SOP.md)** for what to do with them. Clicking a card takes you straight to that screen.

---

## 3. POS / Billing

Reached via the **POS** flyout → **POS / Billing**.

### 3.1 Open a cashier session (required before any sale)

1. In the **Location Code** box near the top, type the store/location code you're selling from (e.g. `HO`).
2. The bar above it shows whether a session is already open at that location. If it says *"No open session at [location] - open one before selling,"* click **Open Session**.
3. You'll be asked for the **opening cash float** — type the cash amount physically in the till at the start of the shift and confirm. The bar now shows *"Session open at [location]"* and a **Close Session** button appears.
4. At the end of the shift, click **Close Session**, enter the **counted cash** actually in the till, and confirm. The system tells you the Expected cash, Counted cash, and the Variance between them (if any) — write this down for your manager if it's non-zero.

### 3.2 Ring up a sale

1. With a location entered and a session open, click into **Scan or Enter SKU**, scan or type the item's barcode/SKU, and press **Enter** (or click **Add to Cart**). The item appears in the cart table with its **Available** stock count.
2. Repeat for every item. If the same SKU is scanned again, its quantity just increases by one instead of adding a duplicate row.
3. For each cart line you can edit **Qty**, **Sale Price**, and **Cost Price** directly in the table cells — the **Line Total** updates automatically. A line with a **Remove** button lets you take it back out.
4. **Optional — loyalty customer**: type the customer's code into **Customer Code**, then click **Check Points** to see their balance, or **Redeem Points** to convert points into a discount value (enter how many points to redeem when prompted; the system tells you the resulting discount value — apply that manually by lowering a line's Sale Price, it isn't applied automatically).
5. Set **Discount %** and **Payment Mode** (Cash/Card/UPI) at the bottom. The **Total** updates live.
6. Click **Complete Sale**.
   - **Normal case**: the sale is recorded, a dialog shows the completed sale number and total, and asks if you want to **print a receipt** — click Yes/No. Stock and accounting entries update automatically; you don't need to do anything else.
   - **Discount-approval case**: if the discount you applied is above your store's configured threshold, the sale does **not** complete immediately — a dialog tells you *"This sale requires manager approval before it completes."* The cart is cleared client-side, but the sale itself sits as **Pending Approval** until a manager decides it from the **Approvals** screen (§6). Nothing is charged or posted until then.
7. **If something goes wrong mid-sale** — a barcode doesn't scan, or checkout is rejected — the red message box above the SKU field or the error dialog explains exactly why (e.g. insufficient stock); fix that and try again.

### 3.3 Process a return

Below the sale cart is a separate **Process a Return** panel — kept deliberately separate so an in-progress return can never get mixed into an in-progress sale.

1. Enter the **Original Order / Cart Number** the return is against (e.g. the `POS-...` number from the original sale's receipt) and the **Return Location**.
2. Scan or type each SKU being returned into **SKU to Return** and press Enter, or click **Add Line**. Edit **Qty**, **Sale Price**, and **Cost Price** per line the same way as the sale cart.
3. Click **Submit Return**. On success, the returned stock is put back into inventory immediately and the cart clears.

---

## 4. POS Profiles

Reached via the **POS** flyout → **POS Profiles**. This is a plain record-list screen (§1) — one profile per till/location combination.

1. Click **New POSProfile** (top right).
2. Fill in **Profile Name** (required), **Location** (required, a Location code), **Default Payment Mode** (required — Cash/Card/UPI), **Invoice Number Series** (optional), and **Default Opening Cash Float** (optional). **Status** defaults to Active/Inactive.
3. Click **Save**.

---

## 5. Finance / GL

Reached via the **Financial Accounting** flyout → **Finance / GL**. Three tabs at the top: **Trial Balance**, **Chart of Accounts**, and **Accounting Periods**.

### 5.1 Trial Balance tab (default)

A read-only, live view — nothing to fill in. It shows Total Debits, Total Credits, and whether the ledger is balanced (a green or red status dot), then every GL account with its debit/credit totals. These numbers always come from actual posted transactions.

### 5.2 Chart of Accounts tab

A read-only reference list of every GL account configured for your tenant — Account Code, Account Name, and Type (Asset/Liability/Equity/Revenue/Expense). No debit/credit totals here — that's what the Trial Balance tab is for; this tab is the master list of accounts themselves, not a balance report.

### 5.3 Accounting Periods tab

1. Click the **Accounting Periods** tab button.
2. To create one: fill in **Period Name** (e.g. `FY2026-Q3`), **Start Date**, **End Date**, and click **Create Period**. It starts **Open**.
3. Every existing period is listed with its status. An **Open** period shows a **Close Checklist** button.
4. Click **Close Checklist** to see a pass/fail list of pre-close checks (e.g. unposted documents) before you commit — this is advisory, it won't stop you from closing even if a check fails, it just shows you the risk. Read it, then confirm to close.
5. **Closing a period is permanent — there is no reopen.** Only close a period once you're sure everything for it has been posted.

---

## 6. Approvals

Reached via the **Financial Accounting** flyout → **Approvals**. This is a shared inbox — anything anywhere in the system that needs a second person's sign-off shows up here (Purchase Orders, Expense Claims, POS discounts, PIM content, and more), not just Finance items.

1. Each row shows the **Record Type**, its **Document ID**, an **Amount** (if applicable), and a **Location**.
2. Click **Approve** to accept it, or **Reject** to decline it — you'll be asked to optionally type a reason for a rejection.
3. Once decided, the document moves on automatically (e.g. an approved Purchase Order becomes ready to send, an approved POS sale finally posts its stock/GL entries). There is no "undo" — every decision is permanently logged with who decided it and when.
4. If the list is empty, it just says *"Nothing awaiting approval."*

---

## 7. Vendor Invoice

Reached via the **Financial Accounting** flyout → **Vendor Invoice**.

1. Click **+ New Vendor Invoice** to open the standard record-list New form (§1) — fill in Invoice Number, Vendor Code, PO Reference, GRN Reference, Invoice Amount, and Financial Year, then Save. It starts as **Draft**.

> **Note**: this form asks for a **GRN Reference**, but as of this writing there is no screen anywhere in the app to create a GRN (Goods Receipt Note) record — if your business needs one, ask your admin how GRN numbers are being tracked in your setup.

2. On the list, a **Draft** or **MismatchHold** invoice shows a **Match** button. Click it, then enter the **PO ID/number** and **GRN ID/number** it should match against when prompted. If the amounts line up, the invoice becomes **Matched**; if they don't, it goes to (or stays in) **MismatchHold** and needs to be matched again with corrected references.
3. Once **Matched**, two buttons appear:
   - **Pay** — pays the invoice in full from Cash/Bank after a confirmation prompt.
   - **Pay w/ TDS** — pick a TDS section from the dropdown next to the button (e.g. showing its withholding rate), then click it to pay with tax withheld under that section.
4. A **Paid** invoice has no further actions.

---

## 8. Payment Proposals

Reached via the **Financial Accounting** flyout → **Payment Proposals**. This batches several **Matched** vendor invoices into one payment run.

1. The top panel lists every currently **Matched** invoice with a checkbox. Tick the ones you want to pay together.
2. Click **Create Proposal from Selected**. It appears below as a new proposal, status **Draft**, with its total amount and invoice count.
3. Click **Execute** on a Draft proposal to pay every invoice in it via the normal vendor-invoice payment path, after a confirmation prompt.
4. If any invoice in the batch fails to pay, a dialog lists exactly which ones and why — the rest still succeed; nothing is all-or-nothing.

---

## 9. Bank Reconciliation

Reached via the **Financial Accounting** flyout → **Bank Reconciliation**.

1. If you have no bank accounts yet, click **Manage Bank Accounts** — this opens the standard record-list screen for BankAccount (Bank Name, Account Number, IFSC Code, Branch, GL Account Code, all required except Branch/IFSC). Create one, then come back to Bank Reconciliation.
2. Click **Statement Lines / Import CSV** to open the BankStatementLine record-list screen, where you can add lines one at a time or use **Bulk Import** (§1) to bring in a whole bank statement.
3. Back on the main Bank Reconciliation screen, pick a **Bank Account** from the dropdown and click **Reconcile**.
4. The result shows how many lines matched, and lists any statement lines or GL postings still left unmatched, so you can investigate the gap.

---

## 10. Debit / Credit Notes

Reached via the **Financial Accounting** flyout → **Debit / Credit Notes**. Two independent panels on one screen.

1. **Debit Notes** (adjustments to a vendor): click **+ New Debit Note**, fill in Note Number, Vendor, Reference PO (optional), Amount, Reason, then Save. It starts **Draft**.
2. **Credit Notes** (adjustments to a customer): same pattern via **+ New Credit Note**, with Customer and Reference Cart instead.
3. Either type shows a **Post** button while **Draft**. Click it, confirm — **this books the GL reversal immediately and cannot be undone** — and the note becomes **Posted**.

---

## 11. Sales Invoice

Reached via the **Financial Accounting** flyout → **Sales Invoice**. This is the credit-sale flow (bill a customer now, collect payment later — separate from a POS cash/card sale).

1. Click **+ New Sales Invoice**, fill in Invoice Number, Customer, Location, Total Amount, then Save. It starts **Draft**.
2. Click **Post** on a Draft invoice to recognize the receivable in the ledger — it becomes **Approved**.
3. Once the customer actually pays, click **Settle** and confirm — it becomes **Paid**. (This is the source feeding the Receivables Ageing report, §14.)

---

## 12. Fulfillment

Reached via the **Sales & Marketplace** flyout → **Fulfillment**. This is the pick/pack/dispatch queue for orders routed to your location.

1. Each task shows its **Status**: Pending, Picking, Packed, Dispatched, or Rejected.
2. From **Pending**: click **Start Picking** to move it to Picking, or **Reject** to decline it.
3. From **Picking**: click **Mark Packed**, or **Reject**.
4. From **Packed**: click **Dispatch**. Once Dispatched, no further action shows here.

---

## 13. Marketplace

Reached via the **Sales & Marketplace** flyout → **Marketplace**. Two independent panels.

### 13.1 Settlements

1. Fill in **Settlement ID**, pick a **Channel** (Shopify/Amazon, plus any additional Channel records your admin has configured), **Total Sale**, **Commission**, **Net Payout**, and a comma-separated list of **Order IDs**.
2. Click **Reconcile**. The settlement appears in the table below with a **Reconciled** or pending status badge.

### 13.2 Logistics Bookings

1. Fill in **Order ID**, **Carrier**, **Tracking Number**, and **Shipping Charge**.
2. Click **Book**. The booking appears in the table below.

---

## 14. Reports

Reached via **Reports** in the sidebar. A row of tab buttons across the top switches between reports.

### 14.1 The six built-in tabs

- **Current Stock** — every SKU/location with On Hand, Available, Committed, Reserved, and Safety Stock. No filters — it's the full current snapshot.
- **Sales Register** — every completed POS sale with location, payment mode, total, and date.
- **Vendor Ledger** — every purchase order with vendor, amount, status, and date.
- **Payables Ageing** — Approved-but-not-yet-Closed purchase orders, bucketed by age since creation.
- **Receivables Ageing** — Approved-but-not-yet-Paid sales invoices (§11), bucketed by age.
- **GST Return Summary** — pick a **From**/**To** date range and click **Run**. Shows taxable value and CGST/SGST/IGST output tax already calculated per-transaction in that window. This is a summary report only — it does not e-file or generate an IRN.

None of these first six tabs take any other filters — what you see is everything in that category.

### 14.2 Report Catalog tab

This is the newer, general-purpose reporting framework — every report registered in the system (old and new) is reachable from here, grouped by category in the **Report** dropdown.

1. Pick a report from the **Report** dropdown.
2. Fill in whatever parameters appear next to it (dates, codes, etc. — a report with no parameters shows none). A field is required if the report needs it; you'll get an inline error naming the missing one if you try to run without it.
3. Click **Run** to see the results table.
4. **Drill-down**: if the report supports it, each row has a **View Details** button — click it to expand an inline sub-table of the underlying records behind that row, without leaving the page.
5. **Save Filter**: click it, name your current parameter values, and they're saved to the **Saved Filter** dropdown (only visible to you, tied to your username) for next time. Pick one from that dropdown to instantly re-fill the parameters.
6. **Export in Background**: click it to queue an async CSV export job instead of just viewing on-screen. A status line shows *"Export queued (job ...)... waiting for it to complete"* and polls automatically every couple of seconds. Once ready, click **Download CSV** to save the file — you don't need to keep the tab open and re-click; it keeps polling for you.

---

## 15. Purchase Order

Reached via the **Procurement** flyout → **Purchase Order**.

1. Fill in **Vendor**, **Target Warehouse**, **Location** (all required), **Total Amount** (the taxable value), and optionally a **GST Rate %** plus an **Interstate** checkbox. **PO Number** is greyed out and reads "Auto (PO series)" — it is issued when you save (see §1).
2. Click **Calculate GST** to preview the CGST/SGST or IGST breakdown for what you've entered so far — this is just a calculator, it doesn't change what gets saved.
3. Click **Create Draft**. It appears in the list below as **Draft**.
4. Click **Submit for Approval** on a Draft PO. Depending on the amount, it routes to a Store Manager (under a configured threshold) or HR/Admin (at or above it) — you'll see it move to **Pending Approval**, then **Approved** or **Rejected** once decided (§6).

> **Note**: as of this writing there is no screen to record a GRN (goods receipt) against an Approved PO, so a PO's own status here won't show stock as physically received — see the note under §7.

---

## 16. Vendors

Reached via the **Procurement** flyout → **Vendors**. A standard record-list screen (§1).

1. Click **New Vendor** (top right). Fill in Vendor Name (required), GSTIN, Bank Account Number, Bank IFSC, Contact Phone, Contact Email (all optional), and Status. Vendor Code is auto-generated — leave it blank.
2. Click **Save**.

---

## 17. RFQ / Quotes

Reached via the **Procurement** flyout → **RFQ / Quotes**. This lets you request quotes from several vendors for the same requirement and compare them before creating a Purchase Order.

1. Fill in **Item / Requirement Description**, **Quantity**, and **Target Date**, then click **Create RFQ**. It starts **Draft**/open for quotes. **RFQ Number** is issued on save (see §1).
2. Click **View Quotes** on an RFQ row to open its quote comparison panel below the list.
3. To record a vendor's quote: fill in **Vendor**, **Quoted Price**, **Lead Time (days)**, and click **Submit Quote**. Repeat for each vendor quoting. **Quote Number** is issued on save (see §1).
4. Once you have quotes to compare, click **Select as Winner** next to the one you're accepting. Confirm — this marks that quote as the winner, **rejects every other quote automatically**, and closes the RFQ. This can't be undone, so make sure you've compared everything first.

---

## 18. Inventory

Reached via the **Stock** flyout → **Inventory**.

1. Use the **Search by SKU or location...** box to filter the list as you type.
2. The table shows, per SKU/location: **On Hand** (physical count), **Available** (what's actually free to sell — already-reserved stock excluded), **Committed**, **Reserved**, and **Safety Stock**.

This screen is read-only — there's no way to adjust stock counts directly here; stock changes come from sales, transfers, and (where applicable) manufacturing/returns.

---

## 19. Stock Transfer

Reached via the **Stock** flyout → **Stock Transfer**. Moves stock between two locations through a Draft → Approved → (optional Packed) → Dispatched → Received lifecycle.

1. Fill in **From Warehouse** and **To Warehouse**. **Transfer Number** is issued on save (see §1).
2. Add one or more lines: enter a **SKU** and **Qty**, click **Add Line**. Repeat for every item — a transfer needs at least one line.
3. Click **Create Transfer**. It starts as **Draft**.
4. Click **Mark Approved** when it's ready to go.
5. From **Approved**, you have two options:
   - Click **Pack** to record which items go in which physical box — you'll be prompted for a **Box ID** per line (typing the same Box ID for multiple lines groups them into one box). This is optional; it's just a confirmation step.
   - Click **Dispatch** directly (works from either Approved or Packed) — confirm, and the stock leaves the source location and sits "in transit."
6. Once it physically arrives, click **Receive**. You'll be prompted for the **quantity actually received** for each line, pre-filled with the dispatched quantity — if less arrived than was sent, type the lower number; that shortage is recorded rather than hidden.

---

## 20. Bin

Reached via the **Stock** flyout → **Bin**. A standard record-list screen (§1) defining physical storage locations within a warehouse.

1. Click **New Bin** (top right). Fill in **Bin Code** and **Location** (required), and optionally **Zone**, **Aisle**, **Rack**, **Capacity**.
2. Click **Save**.

> **Note**: this screen only manages the bin *records themselves*. Putaway (assigning received stock to a bin), pick-lists driven by bin location, and bin condition transitions (e.g. Good → Damaged) all exist as backend capability but have no screen in the app yet — see [ADMIN_SOP.md](ADMIN_SOP.md) for what's API-only today.

---

## 21. Stores

Reached via the **Stock** flyout → **Stores**. A standard record-list screen (§1).

1. Click **New Stores** (top right). Fill in **Store Code**, **Store Name** (required), Address, City, Contact Phone, Store Manager (all optional), and Status.
2. Click **Save**.

---

## 22. Sticker Printing

Reached via the **Stock** flyout → **Sticker Printing**.

1. Pick a **Printer** from the dropdown (create one first if the list is empty — see the note below), set **Copies per SKU**, and optionally a **Reprint Reason**.
2. Scan or type each SKU into **Scan or Enter SKU** and press Enter, or click **Add**. Repeat for every item to print. Each added SKU shows with a small **x** button to remove it.
3. Click **Print Stickers**. The system opens your browser's print dialog with one label per SKU per copy (name, barcode value as text, SKU, and HSN code if set), and logs the print in history below with who printed it and when.
4. **Print history** at the bottom shows every past print run, including any reprint reason given.

> Printer records themselves aren't managed from this screen — they're a Master-type record type, so create one via **Setup** (§28) if none exist yet, using the doctype name **Printer**.

---

## 23. HR

Reached via the **HRM** flyout → **HR**. Three tabs: **Attendance**, **Leave**, **Payroll Export**.

### 23.1 Attendance tab

1. Pick an **Employee**, a **Date**, a **Location**, and a **Status** (Present/Absent/Late/Leave/Holiday/WeeklyOff). **Attendance Code** is issued on save (see §1).
2. Click **Save**. It appears immediately in the list below.

### 23.2 Leave tab

1. Fill in **Employee**, **Leave Type** (Casual/Sick/Earned/Unpaid), **From**/**To Date**, and **Days**, then click **Apply**. It starts **Applied**. **Leave Code** is issued on save (see §1).
2. If you have approval rights, an **Applied** leave request shows **Approve** and **Reject** buttons right in the list — click one to decide it directly (no separate Approvals-screen trip needed for this particular doctype).

### 23.3 Payroll Export tab

1. Pick a **From** and **To** date, click **Export**.
2. A table shows, per employee: Present Days, Absent Days, Late Days, and Approved Leave Days for that period — ready to hand to payroll.

### 23.4 Employee records

Employees themselves are a Master-type record, so they're created and edited under **Setup** (§28), not on this HR screen — look for **Employee** in that dropdown.

---

## 24. Fixed Assets

Reached via the **HRM** flyout → **Fixed Assets**. Lifecycle: Draft → Capitalised → (any number of Transfers) → Disposed.

1. Fill in **Asset Number**, **Category**, **Cost**, **Useful Life (yrs)**, **Location**, **Custodian**, and **Acquisition Date** — all required except Category/Custodian — then click **Create**. It starts **Draft**.
2. Click **Capitalise** on a Draft asset to bring it onto the books — it becomes **Capitalised**, and depreciation starts being calculated automatically from here on (it's computed live on every view, not stored, so the numbers you see are always current as of today).
3. From **Capitalised**, click **Transfer** to move it to a new location/custodian (you'll be prompted for both — custodian is optional), or **Dispose** to write it off. Disposal asks for a **disposal type** (Sale, Scrap, or WriteOff) and warns that it writes off the asset's remaining net book value permanently.
4. The list shows **Cost**, **Accum. Depreciation**, and **Net Block** (Cost minus Accumulated Depreciation) live for every asset.

---

## 25. Expenses

Reached via the **HRM** flyout → **Expenses**. Lifecycle: Claim → Manager Approval → Finance Verification → Payment.

1. Fill in **Employee**, **Location**, **Expense Date**, **Category** (Conveyance/Travel/Food/Hotel/Fuel/Repair/Medical/Marketing/StoreExpense/Other), **Amount**, **GST Amount** (optional), **Advance Adjusted** (optional), and **Purpose**. Click **Create Draft**. **Claim Number** is issued on save (see §1).
2. Click **Submit for Approval** — this routes through the same Approvals screen (§6) as everything else; a manager approves or rejects it there.
3. Once **Approved**, click **Finance Verify** on the claim (this is a separate step from the manager approval — Finance checks the claim independently).
4. Once **Verified**, click **Mark Paid**, confirm — this posts the payment to the ledger and closes the claim out.

---

## 26. Manufacturing

Reached via **Manufacturing** in the sidebar. Single-level Bill of Materials (BOM) plus a linear Production Order lifecycle: Draft → Material Issued → Completed.

### 26.1 Create a BOM

1. Fill in **BOM Code** and **Parent Item** (the finished good's SKU).
2. In **Components**, type each raw-material SKU and quantity as `SKU:QTY`, comma-separated — e.g. `RAW-A:2, RAW-B:1`. Getting this format wrong shows an inline error explaining the expected shape.
3. Click **Create BOM**.

### 26.2 Run a Production Order

1. Pick a **BOM** from the dropdown, set **Quantity** and **Location**, click **Create Order**. It starts **Draft**. **Order Number** is issued on save (see §1).
2. Click **Issue Material** — this consumes the BOM's raw-material components from stock at that location; the order becomes **Material Issued**.
3. Click **Complete (Receive FG)**, confirm — this receives the finished good quantity into inventory and closes the order out as **Completed**.

---

## 27. PIM (Product Information Management)

Reached via **PIM** in the sidebar. Three bespoke tabs (Dashboard, Workbench, Reports) plus six tabs that are really shortcuts into record-list screens for PIM's underlying record types, all sharing the same **PIM** title/tab bar so it never feels like you've left the module.

### 27.1 Dashboard tab

A grid of clickable stat cards (Products, Incomplete, Pending approval, Ready to publish, Published, Missing main image, Publish queued, Publish failed). Clicking one jumps to either the **Workbench** tab or, for "Pending approval," straight to the **ProductContent** record list.

### 27.2 Workbench tab — the main day-to-day screen

1. Optionally pick a **Family** from the filter dropdown to narrow the list.
2. Each row shows an item's **Completeness** score (a colored badge — green ≥80%, amber ≥40%, red below) and how many fields are **Missing**. Click any row to open its detail panel below.
3. In the detail panel:
   - **Add / Update Attribute Value**: pick an **Attribute**, type a **Value**, click **Save**.
   - **Content**: fill in Language (defaults `en`), Title (required), Short Description, Long Description, SEO Title, Tags. Click **Save Draft** to save without submitting, or **Submit for Approval** to save and immediately send it into the same Approvals queue (§6) as everything else — an admin approves or rejects it there.
   - **Media**: pick a file (jpg/png/webp/gif/pdf) and a **Role** (Main Image/Gallery/Variant Image/Lifestyle/Certificate/Internal QC/Video-Other), click **Upload**. Existing media shows as thumbnails below with a **Deactivate** button on each.
   - **Channel Publishing**: pick a **Channel**, click **Publish**. A log below shows every past publish attempt for this item with its status (Published/Failed) and external ID.

### 27.3 Reports tab

1. Pick a report from the dropdown: **Content aging**, **Duplicate media**, **Channel mapping gaps**, or **Attribute quality**.
2. Click **Run report**. Results render as a plain table; *"No issues found"* means a clean pass.

### 27.4 The six doctype tabs

**Product Families**, **Attribute Definitions**, **Family Attributes**, **Channels**, **Category Mapping**, **Field Mapping** each open the standard record-list screen (§1) for their underlying record type, still inside the PIM header/tab bar. These, along with **Item**, are the record types where the **Bulk Edit** multi-select action (mentioned in §1) is available: tick several rows' checkboxes, click **Edit Selected**, pick a field and a new value, and confirm — it updates every selected record in one go. (Any record among them that was Approved gets bumped back to Pending Approval automatically if its record type is approval-gated.)

---

## 28. Setup

The **Setup** dropdown in the sidebar groups every other reference-list record type the system knows about, alphabetically — the base set is **Brands, Sub Brands, Styles, Sub Styles, Product Categories, Product Types, Item Names, Colors, Secondary Colors, Fabric Colors, Polishes**, plus **Location, Legal Entity, Department, Cost Center**, and **Employee** (also reachable this way — see §23.4). Exactly which entries you see depends on what's registered in your tenant (an admin can register more via Database Schema Design — see ADMIN_SOP.md).

Every entry works exactly like §1 describes: **New [Type]**, fill in required fields, Save; no per-row Edit, only Delete; Bulk Import available for CSV loading. Item (used throughout POS/PIM/Inventory) also lives here if it isn't reachable more directly elsewhere in your setup.

---

## 29. My Profile & Sign Out

Click your name/avatar at the bottom of the sidebar to open the account popover.

1. **My Profile**: shows your Username, Role, Status, linked Employee (if any), and whether MFA is enabled — all read-only. Below that:
   - **Contact & Session**: edit your **Email** and your personal **Auto Logout (inactivity)** timer (Never / 15 min / 30 min / 1 hour / 2 hours), click **Save Changes**.
   - **Change Password**: enter your Current Password, a New Password (8+ characters), confirm it, click **Update Password**.
2. **Sign Out**: click it, confirm in the dialog. You'll see a brief "Signing you out..." transition before landing back on the login screen.

---

## 30. Glossary Addendum

Terms used in this SOP not already in USER_GUIDE.md's glossary (§13):

| Term | In plain English |
|---|---|
| **3-way match** | Checking that a Vendor Invoice's amount agrees with both its Purchase Order and its GRN before it's allowed to be paid. |
| **TDS** | Tax Deducted at Source — a portion of a vendor payment withheld and remitted to the tax authority instead of paid to the vendor directly. |
| **Ageing (Payables/Receivables)** | How long money owed (to a vendor) or owed to you (by a customer) has been outstanding, grouped into time buckets. |
| **Drill-down** | Expanding a report row in place to see the individual transactions that make up that row's total. |
| **Saved Filter** | A named set of report parameter values you can re-apply later without retyping them. |
| **Completeness score** | PIM's percentage measure of how many of an item's expected fields/media are actually filled in. |
| **BOM (Bill of Materials)** | The list of raw-material components and quantities needed to make one unit of a finished good. |
| **Net Block** | An asset's Cost minus its Accumulated Depreciation so far — its current book value. |
| **Session (POS)** | A cashier's open till at one location, from Open Session to Close Session, used to reconcile the physical cash at day's end. |

---

*This system is under active development. Two things worth knowing if you hit them: there is currently no screen to record a GRN (goods receipt) against a Purchase Order, and Bin-level putaway/pick-list/condition-change actions exist only as backend capability, not as screens yet. Neither is something you're doing wrong — ask your administrator.*
