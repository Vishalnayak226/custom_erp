---
doc_id: DOC-D44C6028FC
title: User Acceptance Test Checklist
type: template
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

> Reusable draft template. Fill in the customer, release, roles and evidence, then obtain the accountable owner’s approval before operational use. Existing check marks or examples are not acceptance of a new deployment.

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
6. **The sidebar has twelve top-level entries**, most of them module groups with hover flyouts rather than direct links. Hover a module name (or click it, for keyboard/touch) to reveal its screens. Every section below names the module and the screen. The groups, as they read in the app today — use these exact names, older docs and screenshots show earlier ones:

   | Module group | Screens inside |
   |---|---|
   | **Home** | Opens directly - no flyout. The landing screen - see §2. |
   | **POS** | POS / Billing · POS Profiles · Offline Sync Review · Offline Queue Gaps |
   | **Financial Accounting** | Finance / GL · Approvals · Vendor Invoice · Payment Proposals · Bank Reconciliation · Debit / Credit Notes · Sales Invoice |
   | **Sales & Marketplace** | Order Management · Fulfillment · Marketplace · Customer |
   | **Reports** | Opens directly - no flyout |
   | **Procurement** | Purchase Requisitions · Purchase Order · ASN · Goods Receipt · Vendors · RFQ / Quotes |
   | **Stock** | Inventory · Stock Transfer · Bin · Putaway · Bin Conditions · LPN / Cartons / Pallets · Bin Replenishment · Wave / Batch Picking · Mobile Picking · Cycle Count · Sticker Printing |
   | **HRM** | HR · Fixed Assets · Expenses |
   | **Manufacturing** | Opens directly - no flyout |
   | **PIM** | Opens directly - no flyout |
   | **Setup** | A dynamic flyout of every Master record type in your tenant — 53 in a default install, grouped by module, with a filter box and an **Advanced** divider holding 15 system-internal lists |
   | **Settings** | Users · Roles · Prefix Configs · Approval Rules · Dynamic Labels · Database Schema Design · Extension Hooks · Activity Log · Configuration · System Status · Tenant Entitlements · Tenant Usage |

   **Home**, **Reports**, **Manufacturing** and **PIM** are direct links with no flyout.

---

## 0. Prerequisites

- [ ] App is reachable at `http://localhost:8080` (or your environment's URL) and shows a login screen, not an error page.
- [ ] You have the login credentials. Dev environment: see `DEV_CREDENTIALS.local.txt` at the project root (not committed to git — ask whoever set up the environment if you don't have it). There are 4 seed users, one per role:

  | Username | Role | Notes |
  |---|---|---|
  | `admin` or `system` | Super Admin | Full access to everything. Requires an authenticator app code (MFA) at login. |
  | `manager1` | Store Manager | Can view most masters/transactions; cannot create Purchase Orders, cannot read Vendor/Item/Customer (see note below). |
  | `cashier1` | Cashier | Scoped to POS-related screens; cannot read Vendor/Item/Customer/Location (see note below). |

- [ ] **Two accounts are mandatory, not a convenience.** Maker-checker refuses self-approval, so a single account can never complete an approval-gated document (Purchase Order, Purchase Requisition, Expense Claim, GRN, PIM content). Have a maker and a checker logged in on separate browsers/profiles before you start §11 Approvals, or you will get stuck and think it is a bug. See ADMIN_SOP.md §B.10.
- [ ] **A cashier session must be open before any sale.** POS checkout hard-blocks with *"Cash opening is required before billing"* until you open one. §3 covers it — do not skip it.
- [ ] **Role Access Notes (read once, applies everywhere below)**: the sidebar is trimmed per role — a role with no read access to a screen does not see it, and a module flyout disappears once all of its entries are hidden. So a screen missing from `manager1`s or `cashier1`s menu is expected, not a broken link; log in as `admin` to see everything. Inside a screen, a role that can read but not create sees a **"Read-only for your role"** label instead of the New/Bulk Import buttons, and no row Edit/Delete icons (this is §24.12). The server enforces all of it regardless of what the menu shows. `manager1` and `cashier1` cannot read Vendor, Item, or Customer records by default, which limits how much of Purchase Orders/POS/RFQ/Manufacturing they can meaningfully test — either use `admin`, or grant the reads yourself mid-testing on the **Roles** screen (§19).

---

## 1. Login / Logout

- [ ] **1.1 Login screen loads** with Username/Password fields and a "Sign In" button.
- [ ] **1.2 Wrong password** shows an error message and does not let you in.
- [ ] **1.3 Correct login (non-MFA user, e.g. `manager1` or `cashier1`)** takes you straight to **Home**, the default landing screen (or back to whichever screen that browser was last on).
- [ ] **1.4 Correct login (MFA user, `admin`/`system`)** prompts for a 6-digit authenticator code after the password, and only lets you in with the correct one.
- [ ] **1.5 Sidebar shows your name/role** in the bottom-left account area once logged in.
- [ ] **1.6 Sign out** (account menu, bottom-left → Sign Out) returns you to the login screen and you can't get back in by pressing the browser Back button.
- [ ] **1.7 Session persists** across a page refresh (F5) while logged in — you should not be bounced back to the login screen.

---

## 2. Landing screen

- [ ] **No Dashboard entry in the sidebar.** The old Dashboard screen was retired in August 2026 (everything on it was derived counts and shortcuts into Settings screens). Seeing one means you are testing an older build.
- [ ] **Home loads as the landing screen** with no error on a fresh browser profile (clear `localStorage` first, or use a private window). It is not the retired Dashboard: it links only to real task screens (never a Settings/admin one), and its tiles are role-derived - see the next three checks.
- [ ] **Home's quick-action tiles match what this login can actually do.** As `cashier1`: **Point of Sale** is present; **Purchase Orders**/**Finance / GL**'s own doctype tiles are not. As `admin`: most tiles are present. A tile you cannot use is simply absent - never shown greyed out or disabled.
- [ ] **A tile reaches its screen in one click** - e.g. click **Point of Sale** on Home and you land on POS / Billing directly.
- [ ] **On a tenant with little or no data yet**, Home shows a **"Get started"** panel naming the first missing master (e.g. Vendor, Item) with a link to create one, instead of an empty grid with no explanation.
- [ ] **Last screen is remembered**: navigate to another screen, refresh (F5), and you return to that screen rather than to Home.

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
- [ ] "Customer details" / "Redeem Points" (with a customer on the sale) return a response, not a silent failure. Customer details shows points and value, tier and benefits, the spend to the next tier, recent bills and returns, and the customer's usable vouchers (73.3.2).
- [ ] **Discount % field (Stage 20.10)**: with a session open and at least one cart line, leave Discount % at 0 and complete the sale as normal — should complete immediately same as before. Then try a discount at or above the configured threshold (10% by default) — the sale should return "requires ... approval" instead of completing, and the cart clears. Log in as a *different* user with the required role (e.g. Store Manager) and decide it from **Approvals** (§7) — only then does the sale actually complete (stock deducted, GL posted). The *same* cashier who submitted it should be rejected as a maker-checker violation if they try to approve their own cart.
- [ ] "Complete Sale" with at least one cart line, a payment mode selected, and an open session completes the sale, clears the cart, and offers a **Print Receipt** confirm dialog (Stage 20.14) — accepting opens the browser print dialog with a receipt layout.
- [ ] **Process a Return panel (Stage 20.11)**, below the sale cart: enter the original order/cart number and a return location, add a SKU line (typeahead-backed), and submit — should confirm the return processed and restock. (The underlying return-processing feature is old; this panel is its first-ever UI entry point.)
- [ ] Session status bar: click **Close Session**, enter a counted-cash figure — should report the expected cash and the variance (counted minus expected), not just a bare confirmation.

---

## 4. Finance / GL

- [ ] Loads a trial balance table (Account Code, Account Name, Type, Debit, Credit) with Total Debits/Credits and a balanced/unbalanced status indicator.
- [ ] **Who posted what.** Post anything (a sale, a journal voucher) as yourself. **Work to review → Who posted what** for today lists your username with the lines you posted; **View Details** shows exactly those lines. **General journal** with Attribution = *Before attribution* lists only old lines with a blank poster.
- [ ] **Close tasks gate the close.** **Accounting Periods → + New Period**, then **Close tasks**: five tasks, each with a live check. **Close period** is disabled. As Accountant/Super Admin assign an owner to each; as each owner **Sign off** with evidence (under 10 characters is refused). Signing a task you do not own is refused. Once all five are signed, **Close period** works; a posting dated in the period is then refused.
- [ ] **Reopen needs a second person.** On the closed period **Request reopen** with a reason → it waits in **Approvals**. Approving it as the same user is refused; approving as a different Accountant/Super Admin reopens it as **round 2** with all five tasks unsigned and the history showing the reset.
- [ ] **Bank workbench.** Import statement lines; **Rule match** with a tolerance matches a line whose reference names a book document and records the difference; **Find match** only enables **Match selected** when the ticked entries add up to the line; **Raise exception** needs an owner and reason; **Explain** is refused for a non-Accountant; a card **Payout** with a fee moves 1101 to the bank and the fee to 5230. A line in a closed period cannot be changed.
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
- [ ] **+ Reconcile Payout**, then "Reconcile" with all fields filled, returns to the settlements list with a new row.
- [ ] **Logistics Bookings panel** loads below it with its own form and table; submitting "Book" adds a row.

### 6A. Order Management — queues, timeline and changes (Stage 73.2.13-73.2.15)

- [ ] **Stuck payments / Carrier exceptions / Returns and refunds / Settlement** buttons show counts; each lists orders with why, since when, hours and the next step; clicking the order opens it.
- [ ] An order opens with a **Next step** line and button that matches its state (Reserved → Release to fulfillment; payment Pending → Confirm payment; packed → Book shipment), and a **Timeline** in date order including notifications and status changes.
- [ ] **Edit lines** on a Released order with nothing picked: raise a quantity → the pick task shows Cancelled, the order is Reserved again, stock reserved for the extra; a confirmed payment that no longer covers the total puts it On Hold for payment. Asking for more than is in stock changes nothing and says how much is free.
- [ ] Once a pick task is Picking, **Edit lines** is refused; **Cancel order** still works (task Cancelled, stock released). Once Packed, **Cancel** is refused with the reason.
- [ ] **Reship / replace** on a delivered order: send 1 again → a new ₹0 order showing "Reship of …"; the original lists it; sending more than shipped is refused; clicking **Create order** twice gives one order.
- [ ] A manual order sent twice with the same Reference says **Existing Order**, not "created".
- [ ] A notification on the order reads Queued/Accepted/Delivered/Failed or Skipped-… — never "Sent" for a new one.
- [ ] A return or refund raised against the order's invoice shows on the order and in the **Returns and refunds** queue.
- [ ] **Channel connectors**: a channel with no credentials reads **Not connected - add its credentials on Marketplace**; one with credentials but no run reads **Never synced**; a failed run shows its error with secrets masked.
- [ ] **Manage bundles → New Product Bundle**: Components has Component SKU / Quantity columns; + Add Line, pick a SKU, quantity 2, Save → the list shows "SKU × 2". A bundle that contains its own SKU is refused.
- [ ] In any SKU/record suggestion list, **Escape** closes the list only - the form stays open with what you typed.

---

## 7. Approvals

- [ ] The **Approvals** menu opens **My work**: approvals for your role (not ones you submitted), order-desk queues (if you can read orders) and catalog tasks assigned to you, overdue first; the filters' counts match the rows; **Open** on an order-desk row lands on Order Management with that queue showing.

- [ ] Loads a table of documents awaiting your sign-off (Doctype, Document ID, Amount, Location, Approve/Reject buttons). Empty is fine ("Nothing awaiting approval") if nothing is pending.
- [ ] To generate something to approve: submit a Purchase Order for approval elsewhere in the app (as `admin`), then come back here and confirm it appears.
- [ ] "Approve" removes the item from the list and the underlying document's status changes to Approved.
- [ ] "Reject" prompts for an optional reason, then removes the item and marks the document Rejected.

---

## 8. Reports

Four tabs across the top: **Current Stock**, **Sales Register**, **Vendor Ledger**, **Payables Ageing**.

- [ ] Each tab loads its own table without error when clicked (an empty table with a "no data" row is a pass if there's genuinely nothing to report).
- [ ] Switching tabs doesn't require a page reload and the active tab is visually highlighted.
- [ ] **Report Catalog — find by question.** Type *who owes us* in **Find a report** → Receivables Ageing is selected. Required dates start filled (month to date). Run → a line states the filters, row count, time and time zone.
- [ ] **Control totals.** Inventory Valuation, Receivables Ageing, Sales Register, GST Return Summary and Loyalty Points Liability (with Loyalty → Account for loyalty points = liability when earned: ties to 2630; otherwise "no ledger tie") show detail / summary / ledger with a cut-off and say whether they tie; Payables Ageing ties to 2100 GRN Suspense, and **Open Purchase Commitments** (the PO view) says it has no ledger account. As a role that cannot see amounts, the strip shows only the verdict.
- [ ] **Drill-through keeps scope.** **View Details** on a Receivables Ageing bucket lists exactly the invoices counted in that bucket (an invoice due in future sits in 0-30 days), and states the filters. Leave the report and come back → the last filters are still set.
- [ ] **Private exports.** **Export in Background**, then open **My exports**: status, **Cancel** while Pending, **Download CSV** when done. Another user cannot download your export by its id.
- [ ] **Payroll Reconciliation** for a month with posted payslips: each component reads **Tied**; an unposted payslip shows under *Payslips not yet posted*.
- [ ] **Schedules.** Reports → Schedules: your scheduled report or digest shows owner, frequency, next run (date / Due now / Paused), last run, result with rows, and masked recipients. **Pause** → Paused; **Resume** → Active; **Run now** → Delivered with a new last-run time, and a second Run now within a minute is refused. Another non-admin user does not see your schedule; a Super Admin sees all of them with owners.
- [ ] **Recent runs.** Report Catalog → pick a report → open **Recent runs**: your runs with when, time taken and rows. Pressing Run adds a row; changing report refreshes the list.
- [ ] **Part settlement.** Post a 1,000 sales invoice, Settle 400, then Settle the offered 600: Part paid / 600.00, then Paid / 0.00; Payments (2) lists both; Receivables Ageing still ties to 1300. Match a 1,180 bill (18% GST), Pay 500, then the rest: Part paid / 680.00, then Paid; Pay w/ TDS disappears after the first payment. A credit note naming an open invoice lowers its outstanding (any excess shows as refunded).

---

## 9. RFQ / Quotes

- [ ] "New RFQ" form (RFQ Number, description, etc.) creates a new RFQ that appears in the list below.
- [ ] Clicking into an RFQ shows its quotes list; **+ Record Quote** opens the "Submit Quote" form (Quote Number, **Vendor** — has typeahead as of Stage 18, Quoted Price, Lead Time).
- [ ] Submitting a quote adds it to the quotes table with status "Submitted".
- [ ] "Select as Winner" on a submitted quote marks it Selected and closes out the RFQ (other quotes should no longer be selectable). *(Stage 73.2: the screen now uses the line-based flow below; the old one-price quote rows above apply only to RFQs recorded before it.)*
- [ ] **+ New RFQ** (Stage 73.2.1): deliver-to, quotes-due date and item lines - one with an Item, one free text with its own unit; **Create RFQ** opens it under the list with its lines. An RFQ with no lines is refused. **Edit lines** works only while Draft.
- [ ] **Vendors**: invite two vendors with a reply-by date (an inactive vendor is refused); mark one **Declined** with a reason. Statuses read Invited / Responded / Declined.
- [ ] **+ Record Quote** (Stage 73.2.2, D06): per line unit, unit price, tax %, lead days or Not quoted, plus validity, freight and terms. A quote in boxes of 10 is converted to the RFQ's unit (via the item's unit conversion or **RFQ units in one**); a past validity date is refused. A vendor's revised quote replaces the earlier one.
- [ ] **Comparison**: landed cost per unit per line with **best**, totals, validity and lines quoted; **Recommended** goes to the cheapest quote that covers every line, not to a cheaper partial one.
- [ ] **Award** (Stage 73.2.3): refused until each free-text line has an item or is left out; then a Draft PO is created with the quote's prices, freight and terms, linked to the RFQ and quote; the RFQ closes showing the PO, other quotes are Rejected, and no Award buttons remain. A second award is refused.

---

## 10. Sticker Printing

- [ ] Printer dropdown is populated from real Printer records (not hardcoded).
- [ ] Entering a valid SKU and clicking "Add" adds it to the print queue list below the form.
- [ ] "Reprint Reason" field accepts free text (this one is intentionally not a picker — it's a note field).
- [ ] Generating/printing the sheet produces a print-history entry visible further down the page.
- [ ] On **+ Print Stickers**, Print from Transaction comes first and **Printer setup on this computer (QZ Tray) and channel label PDFs** is collapsed at the bottom; opening it shows the QZ status, Detect Printers and Test Print.

### 10A. Service desk, contracts and projects (Stage 55.18)

- [ ] **Setup → Service Ticket** shows the **Service desk** above the list: overdue first, contract coverage, a Next step button; Assign → the row moves to Start work; clicking the ticket number shows its status history.
- [ ] **Setup → Service Contract**: Show a contract → covered or not, visits used, its tickets. **Setup → Project**: Show a project → revenue, expense, net and the documents behind them.

### 10B. Setup path and API access (Stages 55.3, 38.2d)

- [ ] **Settings → Configuration** starts with **Setup path**: steps in order, each Done/Next/To do/Waiting; adding the missing record and reopening moves the step to Done.
- [ ] **Settings → Extension Hooks → API access**: + New API access as an OAuth client with one scope → secret shown once; the token URL returns a token for that client; a data call with the token works only within its scope; Recent calls lists both calls; Revoke → the token is refused.
- [ ] **Bulk Import** a CSV where two rows are wrong → the result offers **Fix and resume the 2 failed rows**. Download failed rows (each with its reason), correct them, upload → **Check** reports without saving and only then enables **Resume** → Resume imports just those rows; the job reads "0 still to fix, 2 fixed since". The same job opens from **My work** and from the Import Job list's lookup.
- [ ] A PIM import schedule with an **Owner** runs as that person (the Import Job shows them); with an inactive owner the run is stopped with a reason, and a failed run appears in the owner's **My work**.
- [ ] On a New form for a numbered record type (e.g. Grievance), **Numbering for this record type** opens Prefix Configurations on that type's series with its next number shown.
- [ ] A second Vendor with the same GSTIN (any letter case) is refused: "Another vendor already has this GSTIN".
- [ ] **Settings → Configuration**: **Find a setting** finds settings across modules; change one, switch module and back - your change is still there; a changed setting shows its **Default**; leaving for Home asks before discarding, and Cancel keeps you on the page.
- [ ] **Settings → Approval Rules**: **Who approves what** lists each record type's bands with gaps/overlaps marked; type a record type and amount → it names the approving role.
- [ ] **Settings → System Status**: **Incident alerts** says Delivered / Delivered - no responder / Configured, not sent / Never configured and names the missing settings; a backup or restore drill that never ran shows **Never recorded** as a warning.

---

## 11. HR

Three tabs: **Attendance**, **Leave**, **Payroll Export**.

- [ ] **Attendance tab**: Employee dropdown is populated with real employees; **Location field has typeahead** (Stage 18). Marking attendance for an employee/date adds a row to the table below.
- [ ] **Leave tab**: same Employee dropdown pattern; submitting a leave request adds it to the table.
- [ ] **Payroll Export tab**: loads without error and offers an export action.
- [ ] **Tabs by path.** Tabs are grouped **My work / Team / HR administration**. A Store Manager sees no Payroll, Payroll Export or Loans tab; a role without HR access sees only My Requests.
- [ ] **Leave help.** Pick an employee and dates on **+ Apply Leave** → Days fills in (both ends counted), the type's balance for the year shows, and dates overlapping existing leave are warned about. On My Requests the balance is your own.
- [ ] **Payroll.** **+ Run Payroll** defaults to last month; payslip rows show the deductions; **Reconcile payroll** opens Payroll Reconciliation.
- [ ] **Onboarding checklist.** Onboarding/Offboarding → + New checklist with 3 steps → *0 of 3*. Open it and tick a step → "Done by <you> · <time>" and *1 of 3*; tick the rest → *Completed*. The employee's own linked login sees it read-only.
- [ ] **Appraisal review.** As HR choose the cycle, **Start** an employee (KRAs filled in), enter self ratings, **Submit for review**. As a Store Manager open it, enter manager ratings (Overall fills with the weighted average), **Approve review** → *Reviewed*. One you submitted yourself shows a note and no Approve button.
- [ ] **Grievance visibility.** Raise a grievance as an Employee Self-Service login. As a Store Manager it is listed but Description and Resolution Notes read "Hidden from your role" and there is no Resolve button. As another employee or a Cashier it appears neither in Grievances nor in **Approvals**. As Super Admin, Resolve with no notes is refused; with notes → *Resolved - actioned*, also on the employee's My Requests.

---

## 12. Fixed Assets

- [ ] **+ New Asset** opens the "Create" form with Category, **Location (typeahead, Stage 18)**, **Custodian (typeahead against Employee, Stage 18)**, Acquisition Date, etc. New asset starts at status **Draft**.
- [ ] Creating an asset adds it to the asset register table (Asset #, Category, Location, Custodian, Cost, Accum. Depreciation, Net Block, Status).
- [ ] Full lifecycle: **Draft** → "Capitalise" → **Capitalised** → "Transfer" (stays Capitalised, location/custodian change) or "Dispose" (moves to a disposed/terminal status). Confirm the right button(s) appear at each status.
  - ⚠️ **Known limitation**: the Transfer action's new-location/new-custodian prompts are plain browser pop-ups, not the typeahead picker used elsewhere — this is a documented, deferred gap (`docs/micro_checklist.md` Stage 18), not something newly broken.

---

- [ ] **Lifecycle detail.** **Details** shows the source (GRN or by hand), the schedule with posted / to come and the current year bold, the depreciation postings, the GL lines with who posted them, and transfers. **Capitalise** first shows the posting and the monthly depreciation.
- [ ] **Depreciation posts monthly.** Capitalise an asset dated months back: the register shows 0.00 posted with "… due, not posted". As Accountant or Super Admin click **Run depreciation**: one month posted per ended month, Details lists one row per month dated at its month-end; running again posts nothing. A Store Manager does not see the button (the API answers ASSET-0536). A month inside a closed period is listed as not posted and posts after the period is reopened.
- [ ] **Disposal with proceeds.** Dispose shows the book value; proceeds above it show a gain to 4300, below it a loss to 5300; negative proceeds are refused. Confirm lists Dr 1410 / Dr cash-bank / Cr 1400 / gain or loss; afterwards 1400 and 1410 are zero for the asset.

## 13. Expenses

- [ ] "Create Draft" form loads with Employee dropdown, **Location (typeahead, Stage 18)**, Category dropdown, Amount/GST/Advance fields, and a free-text Purpose field.
- [ ] Creating a claim adds it to the table below with a status badge, starting at **Draft**.
- [ ] **Defaults and total.** **+ New Expense Claim** starts with your employee, your store and today. Amount 500, GST 90, advance 100 → "To reimburse … = 490.00". GST above the amount, or an advance above the bill, is refused.
- [ ] **Correction.** Reject a submitted claim with a comment → **History** shows the steps and the comment; **Correct and resubmit** opens it filled in; saving returns it to Draft and it can be submitted again.
- [ ] **Paid once, advance cleared.** Pay a claim with an advance → the GL shows the bank credited with claim + GST − advance and 1600 credited with the advance; paying again is refused.
- [ ] **Receipts attach.** New claim with a JPEG and a PDF under **Receipt photo / PDF** → the list shows a paperclip and 2; **History** lists both; **View** shows the photo on the page and downloads the PDF. A row with none says "no receipt".
- [ ] **Receipts refused.** A text file renamed `.jpg` is refused (EXP-0541); a file over 5 MB is refused; a 6th file on one claim is refused (EXP-0543); the same file twice is refused (EXP-0544).
- [ ] **Receipts are private and lock on submit.** A manager at another store (or a colleague) cannot open the claim or its receipts (403). After **Submit for Approval**, Attach and Remove disappear and the API refuses changes (EXP-0545); a Super Admin can still remove one.
- [ ] Full lifecycle: **Draft** → "Submit for Approval" → appears in **Approvals** (§7) → Approve/Reject there → **Approved** → "Finance Verify" → **Verified** → "Mark Paid" → **Paid**. Confirm each button only appears at its correct status.

---

## 14. Manufacturing

- [ ] **Supply Proposals (Stage 73.2.18)**: Manufacturing → Supply Proposals, pick a location with safety stock 10, **Generate proposals** (Reorder) - items below their reorder point are listed with suggested quantities; generating again refreshes, never duplicates. Tick one, **Purchase order** + vendor, **Approve selected**: one Draft PO is named; the row shows Converted with the PO. Approving it again is refused. **Transfer** needs a different From location; **Production** needs an item with an active BOM. **Reject selected** asks for a reason. **Show: All** lists converted and rejected rows with who and when.

- [ ] **BOM (Bill of Materials) form**: **Parent Item field has typeahead against Item** (Stage 18); components are typed rows (Stage 55.12, was a `sku:qty` list) — Item picker, Qty per finished unit, Expected scrap %, **+ Add component** / **Remove**. Refused: an item on two rows, a row with a quantity but no item, a component equal to the parent, no components at all. Creating a BOM adds it to the BOM list.
- [ ] **Materials check (Stage 55.12)**: on **+ New Production Order**, choosing BOM, Quantity and Location shows Required / Available / Short per component; a large quantity shows the short line and the warning, a small one says all available; a backflushed (packaging) component says *as output is reported*. On the list, **Materials** on a Draft order opens the same check under the row, and **Next step** reads Planner (Draft / Completed without cost) or Floor (Material Issued / In Process, with what is still to make). Issuing a short order is refused (MANUFA-0143) with no stock moved.
- [ ] **Production Order form**: BOM dropdown is populated from real BOMs; **Location field has typeahead** (Stage 18). New order starts at **Draft**.
- [ ] Full Production Order lifecycle: **Draft** → "Issue Material" → **Material Issued** → "Complete (Receive FG)" → **Completed**. Confirm each button only appears at its correct status and stock moves as expected (raw materials consumed, finished goods received — cross-check against Reports → Current Stock, §8, if you want to verify the actual quantity movement).
- [ ] **Quality inspections (Stage 55.13)**: + New Inspection for *Goods received* on a GRN with a plan - the plan's parameters fill the readings; a missing reading is refused (QUALIT-0562); an item not on the GRN is refused (QUALIT-0561). Record one Fail → **Decide** offers only Hold stock / Rework / Use as-is; the inspector's own concession is refused (QUALIT-0564); **Hold stock** raises hold_qty by the inspected quantity (available-to-sell drops by it), records the hold and a linked NCR; deciding again is refused. A *Production output* inspection shows **Submit for approval** and lands in Approvals.
- [ ] **Non-conformance (Stage 55.13)**: the NCR button on the inspection opens Non-conformance; **Investigate** needs a Quality reason code; **Plan action** without an owner is refused (QUALIT-0565); **Close** without evidence is refused; closed row shows the evidence and who closed it. A past due date shows *overdue*.
- [ ] **Certificates (Stage 55.13)**: + New Certificate with one Fail result saves as Fail Draft with only **Reject**; Reject asks to confirm and reports the bins moved to QC-Hold; a Pass certificate shows **Release**.
- [ ] **Maintenance (Stage 55.13)**: + New Order with a two-task checklist and no date → Draft → **Schedule** (date) → **Start** → **Complete** with one task unticked is refused (QUALIT-0566); with both ticked, a part and hours it completes and the row shows who, hours and parts. + New Schedule saves a recurring schedule listed under *Recurring schedules*.

---

## 15. PIM (Product Information Management)

The tab bar has 9 entries, but only the first 3 are "real" PIM tabs — the other 6 immediately take you to the same generic master table used elsewhere in the app (§16), just pre-filtered to that doctype. Don't expect PIM-specific styling on those 6; a plain table is correct.

- [ ] **15.1 Dashboard tab**: loads 8 stat cards (Products, Incomplete, Pending approval, Ready to publish, Published, Missing main image, Publish queued, Publish failed) with no error. Clicking a card navigates you either to the Workbench tab or (for "Pending approval") to the ProductContent master table.
- [ ] **15.2 Workbench tab — list**: selecting a Product Family filters the item list; selecting an item opens a detail panel below with a completeness score (e.g. "72%") and a list of missing fields (or "Nothing - fully complete").
- [ ] **15.3 Workbench tab — attribute values**: in the item detail panel, the Attribute dropdown is populated from real Attribute Definitions; picking one, entering a value, and clicking Save updates the item (re-check the completeness score changes if the attribute was one of the missing ones).
- [ ] **15.4 Workbench tab — content**: fill in Title/Short Description/Long Description/SEO Title/Tags and click "Save Draft" — it saves without leaving the page. Click "Submit for Approval" — the content should then show up in **Approvals** (§7) for an Super Admin to decide.
- [ ] **15.5 Workbench tab — media**: choose an image file, pick a Role (Main Image/Gallery/Variant Image/Lifestyle/Certificate/Internal QC/Video/Other), click Upload — a thumbnail appears in the gallery below. "Deactivate" on a thumbnail removes it from the gallery.
- [ ] **15.6 Workbench tab — channel publishing**: the Channel dropdown is populated from real Channel records (not hardcoded); clicking "Publish" produces a result — success with an external ID, or a clear rejection reason — as a new row in the publish log table below.
- [ ] **15.7 Reports tab**: pick a report from the dropdown (**Content aging**, **Duplicate media**, **Channel mapping gaps**, **Attribute quality**) and click "Run report" — each produces a results table (or a clear "no results" state) without error.
- [ ] **15.8 Product Families / Attribute Definitions / Family Attributes / Channels / Category Mapping / Field Mapping** (the other 6 tabs): each one takes you to a generic master table for that doctype; "New <X>" creates a record and it appears in the table.
- [ ] **15.9 Bulk Import** (on the **Item** master table specifically — Setup → Item, or via the Workbench list — not a PIM-tab feature): "Bulk Import" → "Download Template CSV" gives you a real CSV; uploading a filled-in copy shows a preview/result summary (rows imported / rows rejected with reasons), not a silent pass-through.
- [ ] **15.10 Bulk Edit** (Item master table only): select 2+ rows via the checkboxes, click "Edit Selected", pick a field and a new value, confirm — all selected rows update to that value. If the doctype is approval-gated, previously-Approved rows should drop back to Pending Approval, not stay silently Approved with an unreviewed change.

---

## 16. Setup (sidebar submenu)

This submenu is **built dynamically** from every registered "Master" doctype, so the exact list of entries will grow over time — you're not limited to a fixed set of names. As of this checklist's last update it includes at least: Location, Legal Entity, Department, Cost Center, Employee, Item, Printer, Batch, Brand, Color, Model, Size, Style, Vendor, Customer, Channel, Product Family, Attribute Definition, Family Attribute, POS Profile, plus any custom doctypes registered via Database Schema Design.

For **each** item in the submenu:
- [ ] Clicking it loads a table (existing records, or "No records found" if empty) with a working search box.
- [ ] "New <X>" opens a form built from that doctype's real fields (text/number/dropdown/typeahead as configured) and saving adds a new row to the table.
- [ ] Deleting a record (trash icon) removes it from the table (soft-delete — see `docs/architecture/` if you need to verify it's recoverable, not gone).
- [ ] "Bulk Import" → "Download Template CSV" → upload a filled copy → shows a preview/result summary, not a silent pass-through. (Available on every master, most relevant on Item — see PIM §15.9.)
- [ ] On **Item** specifically (or any other PIM-module master): select 2+ rows via the checkboxes → "Edit Selected" appears in the bulk-edit bar → bulk-updates all selected rows (see PIM §15.10 for the full flow).

*(You don't need to repeat this full sequence for all 15-20+ entries individually — spot-check at least 3-4 including Vendor, Location, and Item, since those back this session's new typeahead fields elsewhere in the app.)*

---

## 17. Vendors / POS Profiles (in the Procurement / POS module flyouts respectively)

- [ ] **Vendors**: same generic table/create/delete behavior as any Setup entry (it points at the same underlying screen).
- [ ] **Stores is gone, and its absence is the check.** There must be **no "Stores" entry** in the Stock flyout and none in the Setup master list. It was retired in Stage 30.5.5 because nothing in the system could select one; a shop is a **Location with Type = Store**. If you see a Stores entry, you are on an older build. Confirm instead that **Setup → Core → Location → New Location** offers **Address, City, Contact Phone** and **Manager** — the four fields folded in from the old Stores list.
- [ ] **POS Profiles (Stage 20.6)**: same generic table/create/delete behavior as Vendors above. Creating one (profile name, Location, default payment mode, opening cash float, status) adds it to the table. This is metadata only right now — nothing else in the app reads it yet (POS session opening still asks for the cash float directly rather than defaulting from a profile).
- [ ] **Bin (Stage 20.16)**: same generic table/create/delete behavior. Creating one (bin code, Location, zone/aisle/rack, capacity, status) adds it to the table. Also metadata only — actual putaway/pick-list/condition-transition operations are backend-only right now (§24).

---

## 18. Purchase Order

- [ ] Purchase Order opens on the **list** of saved orders (count beside the title) with **+ New Purchase Order** top right; that opens the order page (breadcrumb **Purchase Orders › New purchase order**; one fixed screen, only the item list scrolls) with PO Number, **Vendor**, **Billing address** and **Shipping address** (store pickers, each with the store's address and **Edit store** under it; Shipping fills with the billing store), GST treatment, the **Add item** row, and the totals and tax in the top bar. Browser **Back** returns to the list and **Forward** brings the half-typed order back; **Create Draft** lands on the list (Stage 65.8 / 68 / 69).
- [ ] **List first, New as its own page, everywhere (Stage 69).** Goods Receipt, Purchase Return, Stock Transfer, Location Movement (history of moves), Journal Vouchers, ASN, RFQ (and an RFQ's quotes), Expenses, Fixed Assets, Payment Proposals, Yard Board, Sticker Printing → Print (print history), Order Management (**+ New Order**), Marketplace (settlements / bookings / manifests), Manufacturing → Orders and HR → Attendance / Leave / Payroll / Loans/Advances / My Requests each open on their list with **+ New …**; the form page has a breadcrumb back; saving lands on the list with the new record; Back from the form does not reload the screen. **Receive goods** on a PO opens the GRN's New page with the PO loaded; **Book shipment** from Fulfillment opens Marketplace's booking page filled.
- [ ] "Calculate GST" with an amount and rate filled in shows a CGST/SGST or IGST breakdown.
- [ ] "Create Draft" (as `admin` — other roles are expected to be denied here, see Role Access Notes) adds the PO to the table below with status "Draft".
- [ ] "Submit for Approval" on a Draft PO changes its status to "Pending Approval" and it should then appear under **Approvals** (§7).
- [ ] Typing a Vendor/Location value that matches nothing is still accepted (no error) — confirms the picker suggests but doesn't force a match, by design.

---

## 19. Inventory / Stock Transfer / Users / Roles (Stock / Settings module flyouts)

All four of these used to be dead links falling through to a "Module Setup Pending" placeholder. As of this checklist's last update, none of them are placeholders anymore — if you land on "Module Setup Pending" for any of these, that's a regression, not expected behavior.

- [ ] **Inventory**: a searchable table of current stock per SKU/location (same underlying data as Reports → Current Stock, §8, but with a client-side search box that tab doesn't have). Typing in the search box filters the table without a page reload.
- [ ] **Stock Transfer**: a form to create a `TransferOrder` (from/to warehouse, items) plus a list with status-appropriate action buttons. Full lifecycle: **Draft** → "Send for approval" → **Pending Approval** → "Approve" (a different user) → **Approved** → "Dispatch" → **Dispatched** → "Receive" → **Partially Received** / **Received**. Confirm each button only appears at its correct status.
  - [ ] **Approval (Stage 73.1.7, D04)**: the creator's own "Approve" is refused (maker-checker). With Settings → Inventory → *Transfer direct-approval limit* at 0, "Approve now" offers to send for approval instead; set it to 1000 and a ₹500 transfer is approved alone by a Store Manager of the sending store, a ₹1000 one is not.
  - [ ] **Tracked items**: dispatch of a serial-tracked item asks for exactly one serial number per unit; a batch-tracked item takes the earliest-expiry lots from the bins (Details shows which). A wrong serial count or an untraceable lot is refused with the item named.
  - [ ] **Partial and discrepant receipt**: receive part of a transfer with "stay in transit" ticked → **Partially Received**; receive the rest with 1 damaged and a *Transfer Discrepancy* reason → **Received**; Inventory shows the damaged unit on hand but not sellable. Damaged/lost/extra without a reason is refused. Record Receipt twice posts once.
  - [ ] **Return to source and cancel**: "Return to source" while in transit puts the units back at the sending store (**Returned** when all went back); "Cancel" works only before dispatch and needs a *Cancellation* reason.
  - [ ] **Pack (Stage 20.19, optional step)**: from **Approved**, a "Pack" button prompts for a Box ID per line item, then moves the order to **Packed** — a new status between Approved and Dispatched. "Dispatch" still works directly from Approved too (packing is optional, not a required gate); from **Packed**, only "Dispatch" appears.
- [ ] **Users**: a "New User" form (username, password, email, role) and a table of existing users with Deactivate/Reactivate buttons. Creating a user with a duplicate username shows a clear "already taken" error, not a generic failure. You cannot deactivate your own account (should show a clear error, not silently no-op).
- [ ] **Roles**: an "Add or Update a Grant" form (Role, Record Type, Read/Create/Update/Delete checkboxes) and a table of every currently-configured `(role, doctype)` permission. Saving a grant for a role/doctype pair that already has one updates it in place rather than duplicating the row. This is a real, live way to close gaps like Stage 18's "Store Manager/Cashier can't read Vendor/Item" finding — try granting `Store Manager` read access to `Item` here and confirm the POS SKU typeahead (§3) then starts suggesting for that role.

**Users** and **Roles** are Super Admin-only on the backend (`requireHRAdmin` in every handler) — expect a clear 403, not a crash, if you reach either as a non-admin role. **Inventory** is gated only by the "reports" module being enabled for the tenant (any role can see it once that's on); **Stock Transfer** goes through the ordinary `role_permissions` check for the `TransferOrder` doctype, same as any other generic-doc screen.

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
- [ ] "Register New Record Type" (prompts for name/module/type) creates a new empty record type that then appears in the left-hand list and in **Setup** (§16) if registered as type "Master".
- [ ] "Add Field" on a record type (prompts for fieldname/label/fieldtype/mandatory/options) adds a new field, and that field then appears on that record type's create form elsewhere in the app.
- [ ] Deleting a field removes it from the record type's configuration and its create form.
- [ ] ⚠️ **Known limitation**: there is no RBAC editor on *this* screen specifically — that's now the **Roles** screen instead (§19). Don't spend time looking for one here; that's expected, not a missed feature. (This note used to say role/doctype access was database-only with no UI anywhere — that's no longer true as of the Roles screen shipping; only the "not on this particular page" half still holds.)

---

## 23. Activity Log (formerly "Log Hub" in the sidebar)

Three tabs: **Audit Logs**, **System Errors**, **Integration Payloads**.

- [ ] **Audit Logs tab** (default) loads a table (User, Action, Details, Timestamp) reflecting real actions — spot-check that at least one action you took earlier in this checklist (e.g. creating a PO, deciding an approval) shows up here.
- [ ] **System Errors tab** loads without error.
- [ ] **"Test Panic Recovery"** button (top-right of the page) deliberately triggers a server-side panic and should come back with a handled error response, not a browser network failure — then a new row should appear on the System Errors tab reflecting it.
- [ ] **Integration Payloads tab** loads a table of integration events; any row with status "Failed" shows a "Retry" button — clicking it asks for confirmation, then queues a retry (confirmation toast, not a silent no-op). Rows with other statuses show no Retry button — that's correct, not missing functionality.

---

## 24. Screens this checklist used to tell you not to test — test them

> **This section was a release-process defect, not just a documentation one.** It previously listed nine capabilities as having "no UI screen at all" and told testers *"there's genuinely nothing to click."* Eight of them are shipped, working screens — two of which (Vendor Invoice and Sales Invoice) were even fully documented in USER_SOP §7 and §11 at the same time. A tester following this checklist could sign off a release without opening any of them. **Corrected 2026-08-01 by re-driving every row against the live sidebar.**

Each of the following now has its own section in this checklist. Work through them like any other:

- [ ] **Goods Receipt (GRN)** — **Procurement → Goods Receipt** — see §24.1
- [ ] **Vendor Invoice** — **Financial Accounting → Vendor Invoice** — see §24.2
- [ ] **Purchase Requisition** — **Procurement → Purchase Requisitions** — see §24.3
- [ ] **ASN (Advance Shipment Notice)** — **Procurement → ASN** — see §24.4
- [ ] **Sales Invoice** — **Financial Accounting → Sales Invoice** — see §24.5
- [ ] **Putaway** — **Stock → Putaway** — see §24.6
- [ ] **Bin-grouped pick lists** — **Stock → Wave / Batch Picking** and **Mobile Picking** — see §24.7
- [ ] **Bin condition transitions** — **Stock → Bin Conditions** — see §24.8
- [ ] **Cycle counts** — **Stock → Cycle Count** — see §24.9
- [ ] **Chart of Accounts** — **Financial Accounting → Finance / GL → Chart of Accounts tab** — see §24.10

### 24.1 Goods Receipt

- [ ] **Procurement → Goods Receipt** loads and lists existing receipts (or an empty state that names the next step).
- [ ] **Load Items from PO** against an Approved PO populates the line table with that PO's items.
- [ ] **Load Items from ASN** does the same from an ASN.
- [ ] **Add Line** adds a manual line; each line takes an SKU and a received quantity.
- [ ] The **GRN number is greyed out** and reads "Auto (GRN series)" — you cannot type one.
- [ ] **Post Receipt** succeeds, and **Inventory (§9) shows the stock actually went up** at the receiving location. This is the single most important row in this section: a GRN that closes a PO without moving stock is the defect Stage 30.2.1 fixed, and it returned HTTP 200 while doing nothing.
- [ ] Posting with no **Location** still works — it defaults from the PO's target warehouse — and the stock lands at that warehouse.
- [ ] If the ledger post fails, the receipt shows as **Cancelled** and the PO stays open; you are told, not given a false success.
- [ ] **Rejected stays open (Stage 73.2.4, D05)**: receive 10 of a 10-unit line with 2 rejected. The PO's **Balance** shows accepted 8, rejected 2, **open 2**; **Receive goods** loads 2. A replacement of 3 is refused as over-receipt; 3 delivered with 1 rejected is accepted.
- [ ] **Short-close**: on **Balance**, Short-close without a reason is refused; with a *Cancellation* reason the PO turns **Closed**, a Draft purchase return (claim) is raised for the 2 rejected units, **Receive goods** disappears and a new receipt against the PO is refused. Invoiced / paid columns follow the receipt's vendor invoice.

### 24.2 Vendor Invoice

- [ ] **Financial Accounting → Vendor Invoice** loads and lists invoices.
- [ ] **+ New Vendor Invoice** opens the create form; it starts as **Draft**.
- [ ] **Match** on a Draft invoice, given the PO and GRN it should match, moves it to **Matched** when the amounts agree.
- [ ] Deliberately mismatched amounts move it to **MismatchHold** instead, with the reason shown.
- [ ] A **Matched** invoice shows **Pay** and **Pay w/ TDS**; paying posts to the GL.
- [ ] A **MismatchHold** invoice shows **Override & Pay**. It **demands a written reason** (cancelling or submitting a blank reason does nothing) and reports *"Override submitted - routed for approval"* — it must **not** pay immediately.
- [ ] The override then appears on the **Approvals** screen for Super Admin.
- [ ] A `Draft` invoice cannot jump straight to `Paid` (this would skip the 3-way match; it is refused with GLOBAL-0019).

### 24.3 Purchase Requisition

- [ ] **Procurement → Purchase Requisitions** loads.
- [ ] Creating one auto-numbers it and starts it as **Draft**.
- [ ] The **Description** field suggests previously-used wordings as you type, and accepts a new one.
- [ ] Submitting routes it by amount to the same bands as a PO, and it appears in **Approvals**.

### 24.4 ASN

- [ ] **Procurement → ASN** loads and lists ASNs.
- [ ] **+ New ASN**, **Add Line**, then **Save ASN** creates one and returns to the ASN list; the **ASN number is auto-issued**.
- [ ] The ASN's **PO Number** field holds the *referenced PO's* number and is **not** overwritten with the ASN's own number.
- [ ] A saved ASN can be loaded from the Goods Receipt screen (§24.1).

### 24.5 Sales Invoice

- [ ] **Financial Accounting → Sales Invoice** loads and lists invoices.
- [ ] **+ New Sales Invoice** creates a Draft.
- [ ] **Post** recognises the receivable (check the GL and the Receivables Ageing report).
- [ ] **Settle** marks it Paid.
- [ ] A POS sale does **not** appear here — POS sales settle immediately and are a different flow.

### 24.6 Putaway

- [ ] **Stock → Putaway** loads.
- [ ] The **Bin** and **SKU** fields both auto-suggest as you type.
- [ ] Putting received stock away moves it into the named bin, and **Bin (§17)** reflects it.
- [ ] **Inbound chain (Stage 55.7)**: book an Inbound appointment with its ASN; **Yard Board → + Check In Trailer** with *Booked appointment* fills trailer/carrier; checking the same trailer in again is refused (INVENT-0498). **Assign to Door** lists doors as Free / Occupied (with the trailer) and today's bookings; an occupied door has no **Spot here**. At the door, **Receive** opens RF Receiving with the ASN loaded.
- [ ] **RF Receiving at phone width (390 px, no sideways scroll)**: *Expected deliveries* lists the location's ASNs, door first; a scan not on the ASN is refused with **Get a supervisor**; a scan asks *How many?* prefilled with what is still expected; more than that is refused; 6 of 10 posts with the warning that the ASN stays open; receiving the last 4 marks the ASN *Received*; another receipt against it is refused (GOODSR-0558).
- [ ] **Putaway work list**: **Put it away now** opens Putaway at the location; *Waiting for putaway* shows on hand / in bins / waiting; **Put away** fills the form; bin → Enter → item → Enter → qty → Enter puts it away with a toast and the waiting figure drops; a wrong bin shows the refusal in a dialog. Cross-dock sits folded under *Cross-dock instead of shelving (advanced)*.

### 24.7 Wave / Batch Picking and Mobile Picking

- [ ] **Wave from a selection (Stage 55.8)**: choose the location; *Orders ready to wave* lists Pending/Picking orders in no wave; tick two → **Put 2 selected into the wave** → a Planned wave in *Open waves*; an order at another location / already in a wave / packed is reported by name (INVENT-0499 when none qualify); ticks survive leaving the location box. **Release** → **Pick list** and **Pick on a phone** appear.
- [ ] **Phone picking (390 px)**: Location → the released wave is in the list; *N confirmed · M to go*; after the last line the summary names the short order (item, how many); **Mark N order(s) packed** packs only the complete ones (Pending → Picking → Packed), the short one keeps its status, no stock moves until Dispatch. A wave with nothing allocatable says the stock is missing or held, and lists the short orders.
- [ ] **Loading (Stage 55.8)**: open loads listed with door/trailer/manifest/scanned; **Open** carries on; door, trailer and manifest are pickers; *Still to load* lists the manifest's invoiced packages not yet scanned; completing with fewer cartons than expected asks first.
- [ ] **Labour & billing (Stage 55.8)**: Warehouse Cockpit → **Labour**: **Plan** shows hours, headcount and *No standard*; **+ New labour standard** (new operation 360/h, element 5 s + 2 s/unit, travel 30 s + 0.5 s/unit, allowance 10% + 4 s) then **Preview** 10 units = 25 + 35 + 10 = 70 s, cost 7.00, *matches the planning engine*. **Billing**: a manual charge 4 × 5.00 with 10% markup and 18% tax shows "4 × 5.00 = 20.00; +10% markup → net 22.00; tax 18% = 3.96; total 25.96"; a storage rate with a missing day, a minimum-charge charge and an old charge without rate evidence each appear under *Check before invoicing* (INVENT-0500 without an owner).
- [ ] **Stock → Wave / Batch Picking** generates a consolidated pick list in zone/aisle/rack walking order.
- [ ] The per-order allocation table shows any shortfall as a badge rather than hiding it.
- [ ] **Stock → Mobile Picking** shows one pick line at a time with **Previous** / **Confirm & Next**.
- [ ] A wave with nothing binned reports that in words rather than showing an empty table.

### 24.8 Bin Conditions

- [ ] **Stock → Bin Conditions** loads.
- [ ] Moving stock Good → Damaged (and QC-Hold, RTV) is accepted and reflected in the bin's contents.
- [ ] An illegal transition is refused with a message naming the legal destinations.

### 24.9 Cycle Count

- [ ] **Stock → Cycle Count** loads and can **start a count** — this is the step the old checklist claimed did not exist.
- [ ] Counted quantities can be entered and reconciled from the same screen.
- [ ] A non-zero variance routes a `CycleCountLine` to **Approvals** for a Store Manager.
- [ ] **Recount trigger (Stage 73.1.8)**: set Settings → Configuration → Warehouse → *Recount when a count is off by* = 5. A first count 18 short is listed *sent for recount* with its reason and a new line; a count 1 short still goes to approval. Reconciling before the recount is entered shows *not counted yet*. After the blind recount, its difference goes to approval with the first count beside it; once approved it posts once (a second post is refused). Set the trigger back to 0.

### 24.9a Several 3PL owners in one warehouse (Stage 59)

- [ ] With the ownership model at **one owner per warehouse**, putaway for a second owner at a warehouse that already holds the first owner's stock is refused with "This warehouse holds one client's stock only", and nothing is put away.
- [ ] Switch to **several owners per warehouse**. Put away 6 for owner A and 4 for owner B in the same bin (Putaway → Owner). Putaway with no owner there is refused ("say whose stock this is").
- [ ] Release an order with no owner at that warehouse: refused, naming the owners. Set its owner on **Fulfillment → Order owner**, release again: the task shows the owner. Changing a released order's owner is refused.
- [ ] Owner A's order for 7: the pick list offers 6 and a shortfall of 1 — owner B's 4 are never offered. Owner B's pick list never names A's units.
- [ ] Damage or move one of A's units in the shared bin: refused without an owner, accepted with owner A; moving A's units into a bin owned by B is refused.
- [ ] **Reports → Owner Stock Exceptions** is empty; after a bin is emptied outside the owner flow it lists the bin with what to do.
- [ ] The page banner no longer shows the 3PL owner caveat.

### 24.10 Chart of Accounts and the Trial Balance

- [ ] **Finance / GL → Chart of Accounts** lists the seeded GL accounts (code, name, type) — it is a real screen, not "a fixed backend table with no view".
- [ ] **Finance / GL → Trial Balance** requires an **As Of Date** and defaults it to today.
- [ ] Changing the As Of date to a date before any posting shows zero debits/credits **but still lists every account** — proving the date genuinely bounds the figures rather than emptying the report.
- [ ] Clearing the As Of date does not silently show an all-time total.
- [ ] **Journal vouchers**: a `JournalVoucher` record type exists with its own approval rule — create one from Setup and confirm it routes to Approvals. (The old checklist's claim that there is "no create/edit screen for manual GL journal entries" was wrong.)

### 24.11 Genuinely generated records — nothing to create directly

These are still produced *by* other actions rather than through a form of their own, so there is no create screen to test. Confirm the action that generates them instead:

- **POS Invoice, Stock Ledger Entry, GL Post** — generated by completing a POS sale and by GL posting (test via POS §3 and Finance §4).
- **PIM: Import Job, Product Attribute Value (as its own list), PIM Product Profile** — supporting records for the PIM flows in §15.

### 24.12 Permission-gated affordances (Stage 30.5.7)

- [ ] Log in as a **non-admin** role and open a record type that role can read but not create (e.g. **Item** as a Store Manager). The **New** and **Bulk Import** buttons must be **absent**, replaced by a **"Read-only for your role"** label.
- [ ] Row **Edit** and **Delete** icons are likewise absent without update/delete grants.
- [ ] A record type the role *can* create still shows its **New** button — confirm the gating is reading real grants and not just hiding everything.
- [ ] Granting create access on **Settings → Roles** and reloading makes the buttons appear.

## 25. Item & Material Master — packaging, gifts, gift cards (Stage 72; Admin Guide §B.3.8, User Guide §4.6)

Set up first (as an admin): a sellable store; an earring item (category *Earrings*) and a necklace (category *Necklace*) with stock; **Packaging** items *Small Red Box*, *Large Red Box*, *Premium Box* and a **Consumable - Periodic** *Butter Paper*, all with stock at that store; a **Gift** item *Gift Plant* with stock; Backflush Rules: Earrings → Small Red Box (Group *Box*, Reason needed = Yes), Necklace → Large Red Box (Box), Premium Box on any item (Box), All items → Butter Paper (Group *Butter paper*, Cashier may change = No); an Offer *Gift With Purchase* scoped to the necklace with Gift item = Gift Plant, Gift qty 1.

- [ ] **Packaging is automatic (UAT-07).** Ring up 2 earrings + 1 necklace: the panel under the items shows *Uses (not on the bill): 2 × Small Red Box, 1 × Large Red Box, 3 × Butter Paper*; complete the sale; the boxes and paper leave the store's stock; the bill and receipt show no packaging line.
- [ ] **Change the box with a reason (UAT-08).** Before completing, **Packaging** → necklace → *Premium Box*, reason typed → only the premium box is used, marked * in the panel; *Reports → Backflush Consumption* shows the change and the reason.
- [ ] **Reason is enforced.** Change the earrings' box without a reason → the till keeps the usual box and says a reason is needed.
- [ ] **No box (UAT-09).** Choose **None** for a line → no box is used for it.
- [ ] **Short stock (UAT-10).** With less butter paper than the sale uses, the till warns and the sale still completes; **Setup → Inventory → Item Exception** (or *Reports → Item Exceptions*) shows an Open shortfall. With a rule set to *Block*, the sale is refused until another option is picked.
- [ ] **Packaging can't be billed (UAT-28).** Scan a box → the line comes off with *"… is not sold at the till"*; an Item Exception *Non-sellable POS attempt* is logged once.
- [ ] **Returns don't put packaging back (UAT-12).** Return the earrings → earring stock comes back, box stock does not.
- [ ] **Butter paper by count (UAT-13).** **Inventory → Consumption Entry**, Periodic count, Butter Paper, counted closing qty → *book − counted* leaves stock; counting more than the book is refused.
- [ ] **Gift With Purchase (UAT-14/15).** With only earrings on the bill, scanning the Gift Plant is refused (not earned). Add the necklace → the panel shows *Gift eligible*; scan the plant → it rings up at 0.00 *Free gift (offer)*; plant stock goes down; *Reports → Gift With Purchase Register* lists it.
- [ ] **Gift card issue (UAT-16/17).** **Gift Card → Register new cards** (generate 1) → print the number. **Issue**, 1,000 cash → a separate issue receipt prints; **Check balance** shows 1,000. An issue refused (amount under the minimum) leaves the card *Inactive*.
- [ ] **Pay with a gift card (UAT-18).** On a 3,000 bill, **Gift Card → Pay**, 600 → bar shows *Gift card −600*, Total 2,400; complete → card balance 400, *Partially redeemed*; the receipt shows the gift card line. *Reports → Gift Card Liability* shows 400 outstanding.
- [ ] **Duplicate and blocked cards (UAT-20).** Registering the same number again is refused; a **blocked** card cannot pay; **Release** returns it to its balance.
- [ ] **Services and kits (UAT-05/21).** A **Service** item sells with *Not stocked* in Available and no stock movement; a **Virtual Kit** (Product Bundle, fulfilment *Virtual*) sold at the till takes its components out of stock.
- [ ] **Treatment is frozen after history (UAT-24).** On an item with sales, change its item type to *Service* → refused with *"already has stock history"*.
- [ ] **Classification register.** *Reports → Item Classification Register* lists every item with class, stock, valuation, sellability and consumption, and which ones the item overrides.

## 26. Offers, points, split payment and returns at the till (Stage 73.1.1–73.1.6; User Guide §4.1, §4.7; Admin Guide *Loyalty redemption controls*)

Set up first: a sellable store with an open session; a ₹1,000 and a ₹500 taxable item with stock; an active **10% off the bill** offer; a customer with 1,000 points; a funded gift card; a **Loyalty Redemption** reason code.

- [ ] **The offer reaches the books.** Ring up 2 × ₹1,000 + 2 × ₹500 for the customer → Total **2,700**. Complete for cash. *Reports → POS Sale Reconciliation* shows the bill **Balanced**, revenue 2,700, tax backed out of 2,700 (not 3,000).
- [ ] **Points at the tenant's value.** Set **Loyalty → Redemption value per point** to 2. **Redeem Points** says *1 point = 2 off* and caps the bill at half its value in points; 100 points take 200 off the Total.
- [ ] **Approval above the value.** Redeem points worth 500 or more → the till asks for a reason and names a request waiting under **Approvals**; **Complete Sale** is refused until a *different* Store Manager at that store approves it; after approval the sale completes; the same approval cannot be used on another bill.
- [ ] **One-time code.** Turn on *Customer confirms every redemption with a one-time code* (with a Loyalty Redemption OTP template) → Redeem Points asks for the code; a wrong code redeems nothing; the right code lets the sale complete.
- [ ] **Daily limit.** Set *Max loyalty redemptions per customer per day* to 1 → the customer's second redemption today is refused.
- [ ] **Split payment.** On a 2,430 bill, **More… → Split payment**, Card 1,000 + UPI 200, Payment Mode Cash → cash tendered 1,300 shows change 70; complete → the finished bill reads *Card 1000.00 + UPI 200.00 + Cash 1230.00*; the receipt prints each part; the trial balance shows 1,000 in card clearing, 200 in UPI clearing, 1,230 in cash. Parts larger than the bill are refused.
- [ ] **Return refunds what was paid.** Return one ₹1,000 shirt from the 2,700 bill (with 270 points used) → the Return tab shows its price as **900.00**; after inspection the refund is **810** in money plus **90 points**; **Refund** shows *It goes back as: …* by tender and does not ask how it is paid; the customer's balance gets the 90 points back and loses the points the shirt earned.
- [ ] **Returned in pieces, exact.** Return the second shirt and both socks → total refunded equals exactly what the bill collected in each tender; the points earned on the bill are fully reversed.
- [ ] **Spent points can go negative.** A customer who spent the points a bill earned, then returns it → full cash refund, balance negative; Redeem Points then offers nothing.
- [ ] **Hold and resume (73.2.5).** Ring up 2 lines for a customer, **More… → Hold this bill** with a note → the till clears; **More… → Resume a held bill** lists it with the note and total; resume → the lines and customer are back under the same bill number; complete it → it posts once and no longer appears as held. A second till resuming the same bill is refused; **Discard it** needs a reason.
- [ ] **Exchange (73.2.6).** Sell a 1,000 shirt (900 after the offer). **More… → Exchange**, that bill, 1 shirt Sellable → *Exchange credit: −900.00*; scan socks worth 450 → Total 0.00 and *(450.00 back to the customer)*; complete → the bill says *give the customer 450.00 back*; the drawer's expected cash drops by 450; *2620 Exchange Credit Clearing* is back to zero. Swapping for a 1,800 coat → the customer pays 900. The credit cannot be used on a second bill.
- [ ] **Issued voucher at the till (73.2.10).** Create a Voucher *WELCOME100* (Flat 100, Max Uses 1). Ring up a 1,000 item with the 10% offer, type *welcome100* in **Coupon code** → the panel shows *Voucher WELCOME100 − issued voucher −100.00*, Total 800; complete → revenue 800 and the voucher is *Exhausted*; on the next bill the same code says why it did not apply. An *Online*-only voucher and another customer's voucher are refused with their reason.
- [ ] **Loyalty screen (73.2.11, 73.3.1, 73.2.12).** **Sales & Marketplace → Loyalty**, pick a customer → balance, tier and history in words. **Adjust points** +50 with a Loyalty Adjustment reason → posted at once and listed; +600 → *waiting under Approvals*, posted when another manager approves; a debit larger than the balance is refused. **Tiers**: give Silver *Free alterations* and 30 grace days, Save → kept. A customer who spends past Silver's threshold moves up on their next purchase. **Points movement** → opening + movements = closing; with *Account for loyalty points* = liability, *Books less points outstanding* is 0 for points earned after the switch.
- [ ] **Cash in / out (73.2.7).** With a session open (float 1,000) and one 900 cash sale, **More… → Cash in / out** shows *… = 1900.00 expected*; a **Paid Out** of 50 with a Cash Movement reason → *1850.00 expected*, and the trial balance shows 50 in *5290 Till Petty Cash Expenses*; a Safe Drop larger than the drawer is refused.
- [ ] **Close off by more than the tolerance.** Close counting 500 → the till says the variance and asks why; with a reason the session shows **Pending Approval** and nothing can be sold on it; a Store Manager (not the cashier) rejecting it reopens the session; approving it closes it.
- [ ] **Shift handover.** A second cashier signs in on the till, **More… → Take over this till**, types the count → the first session closes against it, the second opens with it as float, and each names the other.
- [ ] **Post-sale edit history (73.2.8).** Post a Draft sales invoice (**Post**), then **Edit** it, change the **Due Date**, **Save** → a box asks *Reason for the change*; **Cancel** leaves it unchanged; type a reason and **OK** → saved. *Reports → Report Catalog → Post-Sale Edit History* shows one row: Sales Invoice, its number, Approved, Due Date, old → new date, your user, the reason. Saving again without a change asks nothing.
- [ ] **Customer PO / reference (73.2.9).** **Order Management → + New Order**, fill *Customer PO / reference* = PO-77001 with an item and address → **Create Order**. **Open** the order → *Customer PO / Ref PO-77001*. **Edit** → change it to PO-77002 → saved. Ship and invoice the order → the invoice has the same reference and its **Print** shows *Customer PO / Ref*. *Reports → Report Catalog → Sales by Customer Reference*, reference *77002* → the order and its invoice.
- [ ] **A cashier's day with no instructions (55.5).** Sign in as a **Cashier** set up at a store → the till opens on that store; **Open Session** with a float. Add an item with no stock and **Complete Sale** → the message names it and its available quantity; **Remove** it and **Complete Sale** again → the sale completes. **New Sale** → empty cart, cursor in Scan. **Return** tab → bill number → **Look Up Bill** → 1 unit → **Raise Return** (Return Location already filled) → the box says what the customer gets back and opens Returns. **Approve → Receive Goods → Inspect** (one form, a condition list per item) → *A refund of … is now pending approval*; **Refund** → approve, pay. **Sale** tab → **Close Session**, count the drawer → variance 0.
- [ ] **Vouchers, campaigns and the customer's history (55.17).** **Loyalty → Customer**, pick a customer with bills → points worth, spend and *… more reaches* the next tier, last bills, returns and usable vouchers. **Vouchers & campaigns** → each voucher says what it gives, for whom, where, its dates, *0 of 1* used and *Usable now* (an expired one says *Not usable: expired on …*); a Birthday campaign shows its message with a sample name and *Would pick today* = customers with a birthday today. A campaign with a cost and no sends shows *Nothing sent yet*.
- [ ] **Second-person approval (55.5a).** Turn on **Settings → OMS → A second person approves returns and refunds**. A cashier raises a return and clicks **Approve** → *Another person must approve this*; a manager approves it → Approved; the same for its refund. Turn it off → the cashier can approve again.
- [ ] **No consent, no SMS / WhatsApp (55.17a).** **+ New Customer** at the till with a phone → the consent box has both unticked; tick WhatsApp → **Customer details** says *Offers: agreed by WhatsApp*. A Birthday campaign **Sent by** SMS does not pick a customer who agreed only to WhatsApp (*Skipped: no consent*); a campaign with no **Sent by** shows *Choose the channel … nothing is sent until then*.
- [ ] **Gift card share goes back on the card.** Return an item from a bill part-paid by gift card → the card's balance rises by its share; with the card blocked, processing the refund stops with *"goes back to a gift card that is blocked"*; after **Release** it processes.

---

## Sign-off

| Tested by | Date | Environment (URL) | Role(s) used | Overall result |
|---|---|---|---|---|
| | | | | ☐ Pass ☐ Pass with known limitations only ☐ Fail — see notes |

**Notes / issues found (not already listed as a ⚠️ Known limitation above):**

-
-
-
