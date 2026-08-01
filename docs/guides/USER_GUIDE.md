# User Guide

**Welcome!** This guide explains how to use the ERP system. It's written in plain language for anyone using the system for the first time — no computer or accounting background needed. If a word might be unfamiliar, it's explained the first time it's used, and there's a glossary at the end.

*Need literal click-by-click steps for a screen not walked through in depth below? See **[USER_SOP.md](USER_SOP.md)** — the same plain-language style, one section per screen, covering every module.*

---

## 1. What is this system?

Think of this system as one big digital notebook that your whole business shares. Instead of writing sales in one notebook, stock in another, and money in a third — everything goes into the same place. That way, everyone (the cashier, the warehouse person, the accountant, the owner) is always looking at the same, up-to-date information.

## 2. Logging In

1. Open the app in your web browser. You'll see a **login screen**.
2. Type in your **username** and **password** (your manager or admin gives these to you).
3. Click **Login**.
4. If you have a role that needs extra security (like an Admin), you may be asked for a **6-digit code** from an authenticator app on your phone. This is called **MFA** (Multi-Factor Authentication) — it's an extra lock on the door, on top of your password.
5. If you type your password wrong too many times in a row, the system will temporarily lock your account to keep it safe. Wait a bit and try again, or ask an admin for help.

Once you're in, you'll see a **sidebar** on the left with every area of the system. *(As of this writing the menu itself shows everything to everyone, regardless of role — only the actions themselves are locked down per role, checked by the server every time, not just hidden in the menu. Menu items only your role can't actually use will simply give you a "you don't have permission" message if you click into them. Trimming the menu down to just what each role needs is a known, tracked improvement, not yet built.)*

## 3. Finding Your Way Around

The left sidebar is your main menu. Depending on your role, you might see some or all of these:

| What you see in the menu | What it's for |
|---|---|
| **Dashboard** | A quick overview when you first log in. |
| **POS / Billing** | Ring up a sale at the counter (see §4 below). |
| **Finance / GL** | See the accounting side — money in, money out. |
| **Purchase Order** | Order stock from a supplier. |
| **Inventory** | Check how much stock you have. |
| **Stock Transfer** | Move stock between stores/warehouses. |
| **Reports** | Look up numbers — sales, stock, what's owed to vendors, etc. |
| **Approvals** | Things waiting for someone (maybe you) to say yes or no to. |
| **Vendors** | Your suppliers' details. |
| **Stores** | Your shop/warehouse locations. |
| **HR / Fixed Assets / Expenses** | Staff records, company equipment, and expense claims. |
| **Setup** | A dropdown of every other reference list the system knows about (Brands, Colors, Locations, and many more) — see §6 below. |
| **Users / Roles** | Admin-only: create accounts and control what each role can do (see the Admin Guide §B.2). You'll see these in the menu regardless of your role, but only an HR/Admin account can actually use them. |

At the top of most screens, there's a **search box** and buttons to add a new record, edit one, or filter the list. These work the same way on every screen once you get used to one of them.

## 4. Making a Sale (POS / Billing)

This is the screen a cashier uses most.

1. Click **POS / Billing** in the sidebar.
2. Type or scan the item's **barcode/SKU** into the box and click **Add to Cart** (or press Enter). The item appears in the cart with its price.
3. Repeat for every item the customer is buying.
4. If the customer is a returning/loyalty customer, look them up so any points they've earned can be used or added.
5. **Any offers that apply appear on their own, above the total** — see §4.1. If the customer has a coupon, type it into the **Coupon code** box.
6. Check the total — tax is calculated automatically, you don't need to work it out.
7. Choose how they're paying and click the **checkout/pay** button.
8. The sale is now recorded — stock goes down automatically, and the accounting entries are made automatically too. You don't need to tell any other screen about this sale; the system does it for you.

**If something goes wrong mid-sale** (a barcode doesn't scan, the system shows an error), read the message on screen — it tells you exactly what's wrong (e.g. "this item is already sold" or "not enough stock") rather than just "error."

### 4.1 Offers and coupons at the till

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
5. When the stock physically arrives, someone records a **GRN** (Goods Receipt Note — basically "yes, this stock actually showed up") against that same Purchase Order. Only then does the stock count go up — an order by itself never adds stock, only a confirmed receipt does.

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

"Master data" just means the reference lists everything else points to — your vendors, your stores, and (depending on your industry setup) things like brands, colors, or product categories. Most of these live directly in the sidebar (**Vendors**, **Stores**) or under the **Setup** dropdown, which groups every other one alphabetically.

Adding a new one always works the same way, no matter which list you're in:

1. Click the list in the sidebar (or open it from **Setup**).
2. Click the **New [thing]** button, top right.
3. Fill in the fields — anything marked with a **\*** is required, everything else is optional. A "Code" field usually says *"Auto-generated upon save"* — leave it blank and the system numbers it for you.
4. Click **Save**.

To change an existing one later, find it in the list (use the search box if there are a lot) and use its row's **Edit** action the same way.

## 9. Running a Report

1. Click **Reports**.
2. Pick the report you need (e.g. Current Stock, Sales Register, Vendor Ledger, Payables Ageing).
3. Set any filters (date range, store, etc.) if the report offers them.
4. The numbers you see always come from real recorded transactions — never from someone's manual guess — so you can trust them.

## 10. Approvals

If your role can approve things (e.g. a manager approving a purchase order), you'll see an **Approvals** section listing anything waiting on you. Open an item, review it, and either approve or reject it (you can add a note explaining why). Once decided, it can't be silently changed — there's always a record of who approved what and when.

## 11. Your Profile, Password, and Session

Click your name at the bottom of the sidebar to open your account menu, then **My Profile**. From there you can see your role and (if set up) your linked employee record, change your own password, and set how long the system should wait before automatically signing you out if you step away — separate from the account-wide session limit your admin controls.

## 12. If You Get Logged Out or See an Error

- If you haven't used the system in a while, you may be logged out automatically for security (either the account-wide session limit, or your own shorter auto-logout timer from §11) — just log back in.
- If you ever see an unexpected error screen, note the **correlation ID** shown (a short code) and pass it to your admin/support person — it helps them find exactly what happened, quickly.

## 13. Glossary

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
