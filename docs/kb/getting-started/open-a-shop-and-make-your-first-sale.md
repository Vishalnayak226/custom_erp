---
title: Open a shop and make your first sale
section: Getting Started
order: 5
summary: Half an hour, ten steps, from an empty system to money in the till - and proof that the accounting followed by itself.
audience: store manager, admin
last_verified: 2026-11-15
screens: [pos, purchase-orders, grn, inventory, approvals, finance, reports]
doc_id: DOC-8A016643F6
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-13
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: platform
task: Open a shop and make your first sale
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Open a shop and make your first sale

Every other article is per-screen. This one is the opposite: one continuous path
from an empty system to money in the till, so you can see how the pieces connect
rather than reading about them one at a time. Allow about half an hour.

The same walkthrough is section 13 of the user guide. The guide is the master
copy; this article follows it step for step.

> [!IMPORTANT]
> **One login is enough while self-approval is allowed:** whoever raises the
> purchase order may approve it too. A new client starts with **Who can approve
> their own documents** set to *Nobody*. Then you need a second login to approve
> the order - ask your administrator for one before you start.

## 1. Create the place you trade from

**Setup » Core » Location » New Location.** Give it a code (`MAIN`), a name, and
set **Type = Store**. Save.

This is the one record that makes a shop real to the rest of the system. Every
transaction points at a Location.

## 2. Create the supplier you buy from

**Procurement » Vendors » New Vendor.** Name and contact details. Save.

## 3. Create something to sell

**Setup » Inventory » Item » New Item.** Fill in the name and code, and - this is
the step people skip - the **HSN Code** and **GST Rate**. Both are required: the
system refuses to save without them, because it cannot price a sale it cannot
tax. Save.

**Selling something that isn't taxed?** A `0` in **GST Rate** on its own is still
rejected, because the system cannot tell it apart from a rate nobody has filled
in yet. Set **Tax Treatment** instead. It sits next to GST Rate, in the **Price
and tax** section.

| Tax Treatment | Use it for | GST Rate |
|---|---|---|
| **Taxable** | Everything ordinary. What you get if you leave the field alone. | Must be greater than 0 |
| **Exempt** | Goods exempted by notification - fresh produce, unbranded grain. | Set to 0 for you |
| **Nil-Rated** | Goods whose tariff rate is genuinely 0% - salt, certain cereals. | Set to 0 for you |
| **Zero-Rated** | Exports and SEZ supplies made under LUT or bond. | Set to 0 for you |

**HSN Code is still required on all four** - it goes on the invoice whatever the
rate is, and the nil and exempt part of GSTR-1 is reported HSN-wise too. You
cannot have it both ways either: an item with a non-taxable treatment may not
carry a GST Rate above 0.

You now have an item with **zero stock**. That is expected.

## 4. Order some stock

**Procurement » Purchase Order.** Pick your vendor, target warehouse and
location, enter the amount, and click **Create Draft**.

You do not type a PO number. It is issued on save, from the numbering rules your
administrator configured.

Click **Submit for Approval**.

## 5. Approve it (as the other user)

Sign in with your second account, open **Financial Accounting » Approvals**,
find the purchase order and click **Approve**.

> [!TIP]
> If Approve is refused, you are signed in as the person who raised it. That is
> the check working, not a fault. Use the other account.

## 6. Receive the stock

Back as the first user: **Procurement » Goods Receipt**. Click **+ New Goods
Receipt**, pick the **Vendor**, tick your approved PO and click **Load ticked
POs** - or use **Receive goods** on the PO's row. Confirm the quantities that
actually arrived, type the vendor's invoice number, date and total, and click
**Save receipt**. Then work through the steps: **Verify**, **Check prices**, type
the **MRP** and **Save and post** each one - or **Post all** when everything
matches.

**Now check Inventory » Stock on Hand.** The quantity should have gone up.

> [!WARNING]
> If stock did not move, stop here. Everything downstream depends on this step
> having worked, and continuing will only produce a second confusing failure.

## 7. Open the till

**POS » POS / Billing.** Check that the store at the top right of the screen is
your shop - click it and pick the shop if you work at several. Click **Open
Session** and enter the cash physically in the drawer. While the session is open
you stay at that shop.

> [!TIP]
> **Shop not in the list at the top?** Either you have not been given it - an
> administrator adds it in **Users » Stores** - or it is set to **Sellable = No**.
> Open Setup » Core » **Location**, find it, set **Sellable = Yes** and save.

## 8. Sell something

Scan the item, or type part of its **name**, press Enter and pick it from the
list. Choose a payment mode. For **Cash**, type what the customer handed you into
**Cash tendered** and read off the **Change due**. Click **Complete Sale**.

The till now **freezes on the finished bill**: the bill number, the lines, the
total collected and the change. Click **Print Receipt** to see it on screen;
**Print** there sends it straight to the till printer where one has been set up,
and otherwise through the browser's print dialog. Click **New Sale (Reset)** when
you are ready for the next customer.

## 9. Check that everything moved on its own

You should not have to tell any other screen about that sale. Confirm it:

| Check | Where | What you should see |
|---|---|---|
| Stock went down | **Inventory » Stock on Hand** | Quantity reduced by what you sold |
| The sale was recorded | **Reports » Sales » Sales Register**, then **Run** | Your sale, with its total |
| The books balanced | **Financial Accounting » Trial Balance** | Set **As Of Date** to today - debits equal credits, and the status reads "Balanced trial ledger" |
| The money is owed to the vendor | **Reports » Procurement » Vendor Ledger**, then **Run** | Your purchase order against that vendor |

This table is the point of the whole exercise. Four screens nobody touched now
agree with each other.

## 10. Close the till

Back on **POS / Billing**, click **Close Session** and enter the counted cash.
The system shows expected, counted and the variance.

The expected cash is the X-report's own figure, so a close never disagrees with
the X-report. When the till's bills took card or UPI, the close also asks for the
**card terminal's** and the **UPI app's** totals and keeps their difference from
what the bills say. **Reports » Sales & POS » Z-report (till close)** shows each
closed till afterwards.

## That is the skeleton

Buy, receive, sell - and the accounting follows by itself. Every other feature
hangs off this loop. From here:

- [Your first order, end to end](first-order.md) covers the same ground from the
  order's point of view, including channel orders and fulfillment.
- The role journeys narrow it to one job: start with
  [A day as a store manager](journey-store-manager.md).
