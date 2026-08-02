# User Guide

> **Applies to:** Stage 30.8 · **Last verified against the running app:** 2026-08-01
> Every step below was walked through against a live server on the date shown. If something here doesn't match what you see, the document is what's wrong — please report it.

**Welcome!** This guide explains how to use the ERP system. It's written in plain language for anyone using the system for the first time — no computer or accounting background needed. If a word might be unfamiliar, it's explained the first time it's used, and there's a glossary at the end.

*Need literal click-by-click steps for a screen not walked through in depth below? See **[USER_SOP.md](USER_SOP.md)** — the same plain-language style, one section per screen, covering every module.*

---

## How do I…?

Jump straight to the thing you're trying to do.

| I want to… | Go to |
|---|---|
| Log in for the first time | §2 |
| Find a screen I can't see in the menu | §3 |
| **Ring up a sale** | §4 — read the prerequisites box first |
| Open or close the till for a shift | §4.0 |
| Apply a coupon, or understand why an offer didn't appear | §4.2 |
| Take a return | [USER_SOP §3.3](USER_SOP.md) |
| Check how much stock I have | §5 |
| **Order stock from a supplier** | §6 |
| Record stock that has arrived | §6, step 5 |
| Understand where document numbers come from | §6.1 |
| Move stock between locations | §7 |
| Add a vendor, item, brand, location… | §8 |
| Correct a record I got wrong | §8 — use the row's **Edit** icon |
| Run a report / find the right report | §9 and **[REPORT_CATALOG.md](REPORT_CATALOG.md)** |
| Approve or reject something | §10 |
| Change my own password or auto-logout | §11 |
| **Understand an error message or code** | §12 and **[ERROR_CODES.md](ERROR_CODES.md)** |
| **Set the whole thing up from scratch and make my first sale** | §13 — the full worked example |
| Know what my role is allowed to do | **[PERMISSION_MATRIX.md](PERMISSION_MATRIX.md)** |

---

## 1. What is this system?

Think of this system as one big digital notebook that your whole business shares. Instead of writing sales in one notebook, stock in another, and money in a third — everything goes into the same place. That way, everyone (the cashier, the warehouse person, the accountant, the owner) is always looking at the same, up-to-date information.

## 2. Logging In

1. Open the app in your web browser. You'll see a **login screen**.
2. Type in your **username** and **password** (your manager or admin gives these to you).
3. Click **Login**.
4. If you have a role that needs extra security (like an Admin), you may be asked for a **6-digit code** from an authenticator app on your phone. This is called **MFA** (Multi-Factor Authentication) — it's an extra lock on the door, on top of your password.
5. If you type your password wrong too many times in a row, the system will temporarily lock your account to keep it safe. Wait a bit and try again, or ask an admin for help.

Once you're in, you'll see a **sidebar** on the left. **It only shows what your role can actually use** — if a colleague has menu entries you don't, that's your role, not a fault. A whole module disappears if you have no access to anything inside it, and your business only sees the modules it has licensed. Within a screen, buttons you can't use aren't shown either: if you can view a list but not add to it, you'll see a small **"Read-only for your role"** label where the **New** button would be. Ask your administrator if you need more access.

## 3. Finding Your Way Around

The sidebar has **eleven top-level entries**. Most are module groups: hover one and its screens slide out to the right (click it instead if you are on a touchscreen or using a keyboard). Three — **Reports**, **Manufacturing** and **PIM** — have no flyout and open straight away.

| Sidebar entry | What lives inside |
|---|---|
| **POS** | POS / Billing (§4) · POS Profiles · Offline Sync Review · Offline Queue Gaps |
| **Financial Accounting** | Finance / GL · Approvals (§10) · Vendor Invoice · Payment Proposals · Bank Reconciliation · Debit / Credit Notes · Sales Invoice |
| **Sales & Marketplace** | Order Management · Fulfillment · Marketplace · Customer |
| **Reports** | Opens directly (§9). This is also where you land when you first sign in — its first tab is a dashboard of live figures. |
| **Procurement** | Purchase Requisitions · Purchase Order (§6) · ASN · **Goods Receipt** · Vendors · RFQ / Quotes |
| **Stock** | Inventory (§5) · Stock Transfer (§7) · Bin · Putaway · Bin Conditions · LPN / Cartons / Pallets · Bin Replenishment · Wave / Batch Picking · Mobile Picking · Cycle Count · Stores · Sticker Printing |
| **HRM** | HR · Fixed Assets · Expenses |
| **Manufacturing** | Opens directly. |
| **PIM** | Opens directly. |
| **Setup** | Every reference list the system knows about (§8) |
| **Settings** | Admin-only: Users · Roles · Prefix Configs · Approval Rules · Dynamic Labels · Database Schema Design · Extension Hooks · Activity Log · Configuration · System Status · Tenant Entitlements · Tenant Usage |


![The sidebar, showing its twelve top-level entries](img/sidebar.png)

**You only see what your role can use.** If a module or a screen isn't in your menu, your role doesn't have access to it — that's the system working, not something missing. Ask your administrator if you need it.

**Two search boxes, and they do different things:**

- The **box at the very top of the window** finds *screens*. Type "purch" and it offers Purchase Order, Purchase Requisitions, and so on; pick one and it takes you there. It does **not** search your records.
- The **box just above a table**, on a screen, filters *that table's records*. This is how you find a particular vendor, item or invoice.

At the top right of most list screens there are **New** and **Bulk Import** buttons, and each row has **Edit** and **Delete** icons. Any of these that your role can't use simply won't be shown.

## 4. Making a Sale (POS / Billing)

This is the screen a cashier uses most.

> **Before your first sale — three things must already exist.** Skip any of them and the sale will be refused, with an error that only makes sense once you know this list.
>
> 1. **A Location to sell from.** Setup → Core → **Location**, with **Type = Store**. (Not "Stores" — see §8.)
> 2. **At least one Item, with its HSN Code and GST Rate filled in.** Setup → Inventory → **Item**. Both tax fields are required and the system will not let you save without them, because it cannot price a sale it can't tax. Stock also has to exist: an Item on its own has a quantity of zero until a **Goods Receipt** brings some in (§6).
> 3. **An open cashier session at that location.** This is the one people miss. Checkout refuses with *"Cash opening is required before billing"* until a session is open. Opening one is step 2 below.

### 4.0 Open the till for the shift

1. Click **POS / Billing** (under the **POS** module).
2. Type your store's code into **Location Code**. The bar above shows whether a session is open there.
3. If it says *"No open session … open one before selling"*, click **Open Session**, type the **cash physically in the till right now**, and confirm. The bar changes to *"Session open at …"*.
4. At the end of the shift, click **Close Session** and enter the cash you counted. The system shows what it expected, what you counted, and the difference — note it for your manager if it isn't zero.

You open a session once per shift, not once per sale.


![The POS / Billing screen, with the cashier-session bar across the top](img/pos-billing.png)

### 4.1 Ring it up

1. With a location entered and a session open, type or scan the item's **barcode/SKU** into the box and click **Add to Cart** (or press Enter). The item appears in the cart with its price. You can use the item's **code**, its **barcode**, or its internal id — all three find the same item.
2. Repeat for every item the customer is buying.
3. If the customer is a returning/loyalty customer, type their code into **Customer Code**. **Redeem Points** spends their points on this sale — the discount comes off the amount to collect automatically, and the points are only actually deducted once the sale completes.
4. **Any offers that apply appear on their own, above the total** — see §4.2. If the customer has a coupon, type it into the **Coupon code** box.
5. Check the total — tax is calculated automatically, you don't need to work it out.
6. Choose how they're paying and click **Complete Sale**.
7. The sale is now recorded — stock goes down automatically, and the accounting entries are made automatically too. You don't need to tell any other screen about this sale; the system does it for you.

**If something goes wrong mid-sale** (a barcode doesn't scan, the system shows an error), read the message on screen — it tells you exactly what's wrong (e.g. "this item is already sold" or "not enough stock") rather than just "error."

**If you applied a large discount**, the sale may not complete straight away: a dialog says it needs manager approval, and the sale sits as **Pending Approval** until a manager decides it from the **Approvals** screen. Nothing is charged until then.

### 4.2 Offers and coupons at the till

You do not apply offers by hand. Whatever your head office has set up in the ERP is worked out automatically as you build the cart, and shown in a panel just above the total — each offer by name, what it did ("10% off the bill", "buy 2 get 1 free — 1 free unit"), and how much it took off.

- **Automatic offers just appear.** Add the qualifying items and the offer shows up. Remove an item and it disappears again if the cart no longer qualifies. If an offer needs a minimum spend, it appears once the cart crosses it.
- **Coupon offers need the code.** Type it into the **Coupon code** box. Case doesn't matter, and you can enter more than one separated by a space or comma. If a code doesn't apply to this cart, the panel says so in plain words rather than silently ignoring it — check the spelling, and whether the cart actually meets the offer's conditions.
- **Some offers only apply to certain customers** (a loyalty tier, for instance). Look the customer up *before* expecting those to show — with no customer on the sale, a tier-restricted offer will not appear.
- **The final say is the server's, not this screen's.** The panel is a preview; the discount is recalculated for real when you take payment. In normal use the two agree. If they ever don't, the amount charged is the correct one.
- **The discount box is separate.** The **Discount %** field is still there for a manual, cashier-applied discount, and large manual discounts may still need a manager's approval. Offers are not manual discounts and don't need approval — they're the rules head office already signed off.

## 5. Checking Stock

1. Click **Inventory** in the sidebar.
2. Use the search box to find an item by name or code.
3. The screen shows how much is available right now.

If you need to know how much stock is *actually free to sell* (not already reserved for another order), that number accounts for anything already promised elsewhere — it's not just a raw count sitting in the warehouse.

## 6. Ordering More Stock (Purchase Order)

1. Click **Purchase Order**.
2. Click to create a new one.
3. Pick the vendor (supplier) you're ordering from, and add the items and quantities you need. **You don't type a PO number** — the box shows "Auto (PO series)" and is greyed out on purpose. The number is issued when you save; see §6.1.
4. Save it. Depending on the amount, it might need someone else's **approval** before it's official — that's a safety check, not a bug. You'll see it move to "pending approval," and once approved, it's ready to send to the vendor.
5. When the stock physically arrives, record a **GRN** (Goods Receipt Note — "yes, this stock actually showed up") on **Procurement → Goods Receipt**. Click **Load Items from PO**, pick the order, adjust any quantity that arrived short, and click **Post Receipt**. **Only then does the stock count go up** — an order by itself never adds stock, only a confirmed receipt does. Check **Inventory** afterwards to confirm it moved.


![The Purchase Order screen; the PO Number box is greyed out and filled in on save](img/purchase-order.png)

### 6.1 Where document numbers come from

You never type the number on a Purchase Order, Goods Receipt, Stock Transfer, Expense Claim, Leave request, or any other transaction. The field is greyed out and reads something like **"Auto (PO series)"**, and the real number appears once you save — for example `PO/HO/26-27/000042`.

That number is issued by the system, in order, from a numbering series your administrator controls (see the Admin Guide, §B.3.2). Reading the example above left to right: the **PO** part is the document type, **HO** is the location it was raised at, **26-27** is the financial year, and **000042** is the running count. Your administrator can change any of that — including removing the location or the year entirely — so yours may look different, and that's fine.

Two things worth knowing:

- **Numbers can have gaps, and that's normal.** If a save fails validation after a number was drawn, that number isn't reused. A gap means "something was started and not completed", never "a document went missing".
- **You can't reuse or choose a number.** This is deliberate. When people typed their own numbers, two colleagues creating a PO at the same time could pick the same one — and the second save would quietly overwrite the first, with no warning to either of them. Now that can't happen.

## 7. Moving Stock Between Locations (Stock Transfer)

1. Click **Stock Transfer** in the sidebar.
2. Fill in the From and To warehouse/location, then add one or more line items (SKU + quantity) using the **Add Line** button — a transfer needs at least one line before it can be created. The Transfer Number is filled in for you when you save (§6.1).
3. Click **Create Transfer**. It starts as a **Draft**.
4. Once it's ready to go, click **Mark Approved**.
5. Click **Dispatch** to move the stock out of the source location (it sits "in transit" until received).
6. When it physically arrives, click **Receive** and confirm the quantity that actually showed up for each line — if less arrived than was dispatched, entering the lower number records that shortage rather than hiding it.

## 8. Managing Master Data (Vendors, Stores, Brands, and Similar Lists)

"Master data" just means the reference lists everything else points to — your vendors, your locations, your items, and things like brands, colors or sizes. A few of the most-used ones also appear directly in a module (**Vendors** under Procurement, **Customer** under Sales & Marketplace), but **Setup** holds all of them.

The **Setup** flyout is **grouped by module** (Core, Master Data, Inventory, HR, Finance, Procurement, Sales and so on) with a **filter box** at the top — type a few letters of what you want and the list narrows. At the bottom is an **Advanced** section, collapsed by default, holding the technical lists most businesses never touch. If you filter, matches inside Advanced are shown too, so nothing is ever hidden from a search.

> **"Stores" is not where you set up your shop.** The record type every transaction actually uses is **Location** (Setup → Core), with its **Type** set to Store, Warehouse or HO. Create your shop as a **Location**. The separate **Stores** list is an address book that nothing else reads.

Adding a new one always works the same way, no matter which list you're in:

1. Click the list in the sidebar (or open it from **Setup**).
2. Click the **New [thing]** button, top right.
3. Fill in the fields — anything marked with a **\*** is required, everything else is optional. A "Code" field usually says *"Auto-generated upon save"* — leave it blank and the system numbers it for you.
4. Some fields are small tables rather than boxes — a recipe’s components, a routing’s operations. Use **+ Add Line** to add a row and **Remove** to take one out. You never have to type these in a technical format.
5. If a dropdown is empty, it says so and offers a **create one first** link straight to the list you need. That is the normal way to find out you are missing a prerequisite.
6. Click **Save**.

To change an existing one later, find it in the list (use the search box above the table if there are a lot) and click its row’s pencil **Edit** icon. You never need to delete and recreate a record to correct it.


![The Inventory screen: on hand against what is actually free to sell](img/inventory.png)

## 9. Running a Report

1. Click **Reports** in the sidebar (it is a top-level entry, not inside a module).
2. Pick the report you need. There are 46 of them across 12 categories — **[REPORT_CATALOG.md](REPORT_CATALOG.md)** lists every one, what it answers, and what it needs from you.
3. Set any filters the report offers (date range, store, vendor…). A filter marked required has to be filled in before the report will run.
4. Where a report supports it, click a figure to **drill down** into the individual transactions behind it, or use **Export in Background** for a large export that would otherwise time out.
5. The numbers always come from real recorded transactions — never from someone’s manual guess — so you can trust them.

**The Trial Balance asks for an "As Of Date"** and starts on today. It shows every posting up to and including that date, so setting it to a month-end gives you that month’s closing position. Every account is listed either way; a date before you started trading correctly shows all zeros.


![The report catalog](img/reports.png)

## 10. Approvals

If your role can approve things (e.g. a manager approving a purchase order), you'll see an **Approvals** section listing anything waiting on you. Open an item, review it, and either approve or reject it (you can add a note explaining why). Once decided, it can't be silently changed — there's always a record of who approved what and when.

## 11. Your Profile, Password, and Session

Click your name at the bottom of the sidebar to open your account menu, then **My Profile**. From there you can see your role and (if set up) your linked employee record, change your own password, and set how long the system should wait before automatically signing you out if you step away — separate from the account-wide session limit your admin controls.

## 12. If You Get Logged Out or See an Error

- If you haven't used the system in a while, you may be logged out automatically for security (either the account-wide session limit, or your own shorter auto-logout timer from §11) — just log back in.

### 12.1 How to read an error message

Every error dialog has up to three lines, and they mean different things:

1. **The headline** — what went wrong in plain words ("Required value is missing.").
2. **The detail** — the specific thing ("Field \"HSN Code\" (hsn_code) is required"). This is the line that usually tells you what to fix.
3. **What to do** — the suggested next step ("Enter the missing value.").

Underneath there's a **code** like `GLOBAL-0001` and a **correlation ID**. Look the code up in **[ERROR_CODES.md](ERROR_CODES.md)** for a fuller explanation; give the correlation ID to your admin if you need help, because it lets them find exactly this event in the log.

### 12.2 The errors people hit most

| What you see | What it actually means |
|---|---|
| *"Cash opening is required before billing"* | No cashier session is open at this location. §4.0. |
| *"Please enter HSN Code to continue"* | The item is missing its tax details. Every item needs an HSN Code and a GST Rate before it can be sold. |
| *"item not found"* at the till | The SKU isn't recognised. Code, barcode and internal id all work — check for a typo, or whether the item exists at all. |
| *"Selected Vendor does not exist or is inactive"* | You typed a vendor that isn't in the list. Use the suggestions as you type, or create the vendor first (§8). |
| *"…cannot move from 'X' to 'Y'"* | The document can't jump to that status from where it is. The message lists the statuses it *can* move to. |
| *"…requires a reason_code"* | You're reversing a decision (revoking an approved leave, un-selecting a vendor quote). Give a reason and it will go through. |
| *"This sale requires manager approval"* | The discount is over your store's threshold. The sale waits in Approvals; nothing is charged yet. |
| *"You do not have permission…"* | Your role can't do that. See **[PERMISSION_MATRIX.md](PERMISSION_MATRIX.md)**, then ask an admin. |
| *"Too many requests"* | You've run reports faster than the limit allows. The message says how long to wait. |

---

## 13. Open a Shop and Make Your First Sale — the full worked example

Everything above is per-screen. This section is the opposite: one continuous path from an empty system to money in the till, so you can see how the pieces connect. Allow about half an hour.

**You need two user accounts to complete this.** Approvals refuse self-approval, so the person who raises the purchase order cannot be the person who approves it. Ask your administrator for a second login before you start (ADMIN_SOP §B.10).

### Step 1 — Create the place you trade from

**Setup → Core → Location → New Location.** Give it a code (`MAIN`), a name, and set **Type = Store**. Save.

> Do **not** use the "Stores" list for this. Nothing else in the system reads it. §8.

### Step 2 — Create the supplier you buy from

**Procurement → Vendors → New Vendor.** Name and contact details. Save.

### Step 3 — Create something to sell

**Setup → Inventory → Item → New Item.** Fill in the name and code, and — this is the step people skip — the **HSN Code** and **GST Rate**. Both are required; the system refuses to save without them, because it can't price a sale it can't tax. Save.

At this point you have an item with **zero stock**. That's expected.

### Step 4 — Order some stock

**Procurement → Purchase Order.** Pick your vendor, target warehouse and location, enter the amount, and click **Create Draft**. Note that you don't type a PO number — it's issued on save (§6.1).

Click **Submit for Approval**.

### Step 5 — Approve it (as the *other* user)

Log in as your second account, go to **Financial Accounting → Approvals**, find the PO, and click **Approve**.

> If Approve is refused, you're logged in as the person who raised it. That's the check working. Use the other account.

### Step 6 — Receive the stock

Back as the first user: **Procurement → Goods Receipt**. Click **Load Items from PO**, choose your approved PO, confirm the quantities that actually arrived, and click **Post Receipt**.

**Now check Stock → Inventory.** The quantity should have gone up. If it hasn't, stop here — everything downstream depends on this having worked.

### Step 7 — Open the till

**POS → POS / Billing.** Type `MAIN` into **Location Code**, click **Open Session**, and enter the cash physically in the drawer.

### Step 8 — Sell something

Scan or type your item's code, press Enter, choose a payment mode, and click **Complete Sale**. Say yes to the receipt if you want to see it.

### Step 9 — Check that everything moved on its own

You should not have to tell any other screen about that sale. Confirm it:

| Check | Where | What you should see |
|---|---|---|
| Stock went down | **Stock → Inventory** | Quantity reduced by what you sold |
| The sale was recorded | **Reports → Sales Register** | Your sale, with its total |
| The books balanced | **Financial Accounting → Finance / GL** | Set **As Of Date** to today — debits equal credits, and the status reads "Balanced trial ledger" |
| The money is owed to the vendor | **Reports → Vendor Ledger** | Your purchase order against that vendor |

### Step 10 — Close the till

Back on **POS / Billing**, click **Close Session** and enter the counted cash. The system shows expected, counted, and the variance.

That's the whole loop: buy → receive → sell → and the accounting follows by itself. Every other feature in this guide hangs off this skeleton.

---

## 14. Glossary

| Term | In plain English |
|---|---|
| **GST** | The government sales tax added to a sale, calculated automatically. |
| **GRN** | Proof that ordered stock actually arrived — "Goods Receipt Note." |
| **GL / Ledger** | The accounting record of every rupee moving in or out of the business. |
| **SKU / Barcode** | The unique code identifying one specific product. |
| **MFA** | A second security check (a code from your phone) in addition to your password. |
| **Approval / Maker-checker** | A rule that important actions need a second person to say yes, so no one person can make a big mistake (or fraud) alone. |
| **Tenant** | Your business's own private copy of the system — other businesses using the same system can never see your data. |
| **Role** | What kind of user you are (Cashier, Manager, HR/Admin, etc.) — it decides what you can see and do. |
| **Correlation ID** | A tracking code shown when something goes wrong, so support can find exactly what happened. |

---

*This system is under active development — not every feature described in the full product plan exists yet. If something you expect to see isn't there, it may not be built yet rather than something you're doing wrong; ask your administrator.*
