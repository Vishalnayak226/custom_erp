# User Guide

**Welcome!** This guide explains how to use the ERP system. It's written in plain language for anyone using the system for the first time — no computer or accounting background needed. If a word might be unfamiliar, it's explained the first time it's used, and there's a glossary at the end.

---

## 1. What is this system?

Think of this system as one big digital notebook that your whole business shares. Instead of writing sales in one notebook, stock in another, and money in a third — everything goes into the same place. That way, everyone (the cashier, the warehouse person, the accountant, the owner) is always looking at the same, up-to-date information.

## 2. Logging In

1. Open the app in your web browser. You'll see a **login screen**.
2. Type in your **username** and **password** (your manager or admin gives these to you).
3. Click **Login**.
4. If you have a role that needs extra security (like an Admin), you may be asked for a **6-digit code** from an authenticator app on your phone. This is called **MFA** (Multi-Factor Authentication) — it's an extra lock on the door, on top of your password.
5. If you type your password wrong too many times in a row, the system will temporarily lock your account to keep it safe. Wait a bit and try again, or ask an admin for help.

Once you're in, you'll see a **sidebar** on the left with all the areas of the system you're allowed to use. You won't see everything — only what your role needs. That's normal and it's on purpose, to keep the system simple and safe.

## 3. Finding Your Way Around

The left sidebar is your main menu. Depending on your role, you might see some or all of these:

| What you see in the menu | What it's for |
|---|---|
| **Dashboard** | A quick overview when you first log in. |
| **POS / Billing** | Ring up a sale at the counter (see §4 below). |
| **Finance / GL** | See the accounting side — money in, money out. |
| **Purchase Orders** | Order stock from a supplier. |
| **Inventory** | Check how much stock you have. |
| **Transfers** | Move stock between stores/warehouses. |
| **Reports** | Look up numbers — sales, stock, what's owed to vendors, etc. |
| **Approvals** | Things waiting for someone (maybe you) to say yes or no to. |
| **Vendors** | Your suppliers' details. |
| **Stores** | Your shop/warehouse locations. |
| **HR / Fixed Assets / Expenses** | Staff records, company equipment, and expense claims. |

At the top of most screens, there's a **search box** and buttons to add a new record, edit one, or filter the list. These work the same way on every screen once you get used to one of them.

## 4. Making a Sale (POS / Billing)

This is the screen a cashier uses most.

1. Click **POS / Billing** in the sidebar.
2. Type or scan the item's **barcode/SKU** into the box and click **Add to Cart** (or press Enter). The item appears in the cart with its price.
3. Repeat for every item the customer is buying.
4. If the customer is a returning/loyalty customer, look them up so any points they've earned can be used or added.
5. Check the total — tax is calculated automatically, you don't need to work it out.
6. Choose how they're paying and click the **checkout/pay** button.
7. The sale is now recorded — stock goes down automatically, and the accounting entries are made automatically too. You don't need to tell any other screen about this sale; the system does it for you.

**If something goes wrong mid-sale** (a barcode doesn't scan, the system shows an error), read the message on screen — it tells you exactly what's wrong (e.g. "this item is already sold" or "not enough stock") rather than just "error."

## 5. Checking Stock

1. Click **Inventory** in the sidebar.
2. Use the search box to find an item by name or code.
3. The screen shows how much is available right now.

If you need to know how much stock is *actually free to sell* (not already reserved for another order), that number accounts for anything already promised elsewhere — it's not just a raw count sitting in the warehouse.

## 6. Ordering More Stock (Purchase Orders)

1. Click **Purchase Orders**.
2. Click to create a new one.
3. Pick the vendor (supplier) you're ordering from, and add the items and quantities you need.
4. Save it. Depending on the amount, it might need someone else's **approval** before it's official — that's a safety check, not a bug. You'll see it move to "pending approval," and once approved, it's ready to send to the vendor.
5. When the stock physically arrives, someone records a **GRN** (Goods Receipt Note — basically "yes, this stock actually showed up") against that same Purchase Order. Only then does the stock count go up — an order by itself never adds stock, only a confirmed receipt does.

## 7. Running a Report

1. Click **Reports**.
2. Pick the report you need (e.g. Current Stock, Sales Register, Vendor Ledger, Payables Ageing).
3. Set any filters (date range, store, etc.) if the report offers them.
4. The numbers you see always come from real recorded transactions — never from someone's manual guess — so you can trust them.

## 8. Approvals

If your role can approve things (e.g. a manager approving a purchase order), you'll see an **Approvals** section listing anything waiting on you. Open an item, review it, and either approve or reject it (you can add a note explaining why). Once decided, it can't be silently changed — there's always a record of who approved what and when.

## 9. If You Get Logged Out or See an Error

- If you haven't used the system in a while, you may be logged out automatically for security — just log back in.
- If you ever see an unexpected error screen, note the **correlation ID** shown (a short code) and pass it to your admin/support person — it helps them find exactly what happened, quickly.

## 10. Glossary

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
