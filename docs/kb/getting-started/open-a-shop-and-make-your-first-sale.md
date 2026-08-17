---
title: Open a shop and make your first sale
section: Getting Started
order: 5
summary: Half an hour, ten steps, from an empty system to money in the till - and proof that the accounting followed by itself.
audience: store manager, admin
last_verified: 2026-08-17
screens: [pos, purchase-orders, grn, inventory, approvals, finance, reports]
---

# Open a shop and make your first sale

Every other article is per-screen. This one is the opposite: one continuous path
from an empty system to a completed sale, so you can see how the pieces connect
rather than reading about them one at a time. Allow about half an hour.

> [!IMPORTANT]
> **You need two user accounts.** Approvals refuse self-approval, so whoever
> raises the purchase order cannot be the person who approves it. Ask your
> administrator for a second login before you start.

## 1. Create the place you trade from

**Setup » Location » New.** Give it a code (`MAIN`), a name, and set
**Type = Store**. Save.

This is the record that makes a shop real to the rest of the system. Every
transaction points at a Location.

## 2. Create the supplier you buy from

**Procurement » Vendors » New.** Name and contact details. Save.

## 3. Create something to sell

**Setup » Item » New.** Fill in the name and code, and - this is the step people
skip - the **HSN Code** and **GST Rate**. The system refuses to save without
them, because it cannot price a sale it cannot tax.

Selling something that is not taxed? A `0` in **GST Rate** on its own is
rejected, because it is indistinguishable from a rate nobody has filled in yet.
Set **Tax Treatment** instead:

| Tax Treatment | Use it for | GST Rate |
|---|---|---|
| **Taxable** | Everything ordinary. The default. | Must be greater than 0 |
| **Exempt** | Goods exempted by notification - fresh produce, unbranded grain. | Leave 0 or blank |
| **Nil-Rated** | Goods whose tariff rate is genuinely 0% - salt, certain cereals. | Leave 0 or blank |
| **Zero-Rated** | Exports and SEZ supplies under LUT or bond. | Leave 0 or blank |

**HSN Code is required on all four** - it goes on the invoice whatever the rate
is, and the nil and exempt parts of GSTR-1 are reported HSN-wise too. You cannot
have it both ways either: a non-taxable treatment may not carry a GST Rate above
zero.

You now have an item with **zero stock**. That is expected.

## 4. Order some stock

**Procurement » Purchase Order.** Pick your vendor, target warehouse and
location, add your item as a line with a quantity and rate, and click **Create
Draft**.

You do not type a PO number. It is issued on save, from the numbering rules your
administrator configured.

Click **Submit for Approval**.

## 5. Approve it, as the other user

Sign in with your second account, open **Financial Accounting » Approvals**,
find the purchase order and click **Approve**.

> [!TIP]
> If Approve is refused, you are signed in as the person who raised it. That is
> the check working, not a fault. Use the other account.

## 6. Receive the stock

Back as the first user: **Procurement » Goods Receipt**. Click **Load Items from
PO**, choose your approved PO, confirm the quantities that actually arrived, and
click **Post Receipt**.

**Now open Stock » Inventory.** The quantity should have gone up.

> [!WARNING]
> If stock did not move, stop here. Everything downstream depends on this step
> having worked, and continuing will only produce a second confusing failure.

## 7. Open the till

**POS » POS / Billing.** Start typing your shop's name into **Location** and
pick it from the list - typing the code `MAIN` finds it too. Click **Open
Session** and enter the cash physically in the drawer.

## 8. Sell something

Scan or type your item's code, press Enter, choose a payment mode, and click
**Complete Sale**. Say yes to the receipt if you want to see it: it goes
straight to the till printer where one has been configured, and otherwise
through the browser's print dialog.

## 9. Check that everything moved on its own

You should not have to tell any other screen about that sale. Confirm it:

| Check | Where | What you should see |
|---|---|---|
| Stock went down | **Stock » Inventory** | Quantity reduced by what you sold |
| The sale was recorded | **Reports » Sales Register** | Your sale, with its total |
| The books balanced | **Financial Accounting » Finance / GL** | Set **As Of Date** to today - debits equal credits, status reads "Balanced trial ledger" |
| The money is owed to the vendor | **Reports » Vendor Ledger** | Your purchase order against that vendor |

This table is the point of the whole exercise. Four screens nobody touched now
agree with each other.

## 10. Close the till

Back on **POS / Billing**, click **Close Session** and enter the counted cash.
The system shows expected, counted and the variance.

## That is the skeleton

Buy, receive, sell - and the accounting follows by itself. Every other feature
hangs off this loop. From here:

- [Your first order, end to end](first-order.md) covers the same ground from the
  order's point of view, including channel orders and fulfillment.
- The role journeys narrow it to one job: start with
  [A day as a store manager](journey-store-manager.md).
