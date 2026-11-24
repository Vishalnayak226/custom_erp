---
title: A day as a cashier
section: Role Journeys
order: 2
summary: Open the till, ring up sales, handle the four things that commonly go wrong, and close out with a clean variance.
audience: cashier
last_verified: 2026-11-24
screens: [pos]
doc_id: DOC-A976B97AB9
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-22
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: cross-module
task: A day as a cashier
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# A day as a cashier

Almost everything you do is on one screen: **POS » POS / Billing**. This is that
screen in the order you meet it.

## Start of shift: open the till

1. Open **POS » POS / Billing**.
2. Check the store named in the bar at the top of the screen. If you work at
   more than one, click it and pick yours. The store cannot change while a till
   session is open there.
3. The till says whether a session is open there. If it says *"No open
   session"*, click **Open Session** and, when it asks for the opening cash
   float, type **the cash physically in the till right now**.

You open a session once per shift, not once per sale. Until one is open,
checkout is refused with *"Cash opening is required before billing"* - which is
the single most common first-day confusion, and now you know it.

## Ringing up a sale

1. Scan the item's barcode into **Scan or search** and press Enter. You can
   also type part of its name or code there and pick it from the suggestions.
2. Repeat for everything the customer is buying.
3. For a loyalty customer, type their mobile number into **Mobile** and press
   Enter, or search by **Name**. With no customer the sale is Walk-in. **Redeem
   Points** spends their points on this sale; the discount comes off what you
   collect, and the points are only actually deducted once the sale completes.
4. Any offers that apply appear on their own above the total. If the customer
   has a coupon, type it into **Coupon code**.
5. Check the total. Tax is worked out for you.
6. Choose the **Payment Mode**. For cash, type what the customer handed you
   into **Cash tendered** to see the change due. Click **Complete Sale**.
7. The finished bill stays on screen. **Print Receipt** shows it and prints it;
   the paper, template and printer are chosen there. **Print GST Invoice** prints
   a tax invoice when the customer needs one. Click **New Sale (Reset)** for the
   next customer.

Stock falls and the accounting entries are made in that same moment. You never
have to tell another screen about a sale.

## Offers: you do not apply them

Whatever head office has set up is worked out automatically as you build the
cart and shown by name above the total, with what each one took off.

- **Automatic offers just appear.** Remove an item and the offer disappears
  again if the cart no longer qualifies.
- **Coupon offers need the code.** Case does not matter and you can enter more
  than one, separated by a space or comma. A code that does not apply says so in
  plain words rather than being silently ignored - check the spelling, and
  whether the cart actually meets the conditions.
- **Some offers are customer-specific.** Look the customer up *before* expecting
  a loyalty-tier offer to show; with no customer on the sale it will not appear.
- **The panel is a preview.** The discount is recalculated for real when you
  take payment. If the two ever disagree, the amount charged is the correct one.
- **The Discount box is separate** - that is a manual discount you apply, as a
  percentage or in rupees (**Discount as**), and a large one may need a manager. Offers never need approval; they are rules
  head office already signed off.

## The four things that go wrong

**"Cash opening is required before billing."** No session is open at this
store. Open one.

**"Not in stock here. Not added."** The till will not sell what this store does
not have; *"only 2 in stock here"* means the cart already holds all of it. Stock
that is damaged or held is listed as not sellable here. Tell your manager; you
cannot fix this from the till.

**A large discount needs manager approval.** The sale sits as **Pending
Approval** and nothing is charged until a manager decides it on the
**Approvals** screen. It will not print a receipt in the meantime, because the
money has not been collected.

**A barcode does not scan.** Type part of the item's name or code into the same
box instead. If neither finds it,
the item does not exist in the system yet - that is a job for whoever maintains
the product list.

## Reprinting a receipt

**POS » Sales History** lists past bills; open one and click **Reprint**. If
your business controls reprints it asks why, and one past the free reprints
waits for an approver. The receipt is rebuilt from the
recorded sale, not from whatever happens to be on screen, so it always shows
what was originally rung up - including offers and points as their own lines,
so the printed total matches the cash that went into the drawer.

## End of shift: close the till

Click **Close Session** and enter the cash you counted. The system shows what it
expected, what you counted and the difference.

> [!TIP]
> Note the variance for your manager if it is not zero, and do it now rather
> than tomorrow. A variance with an explanation attached the same evening is a
> countable event; the same variance found a week later is an investigation.

## If the network drops

Keep selling. Sales are queued locally and synced when the connection returns.
Your manager reviews anything that could not be applied cleanly under **POS »
Offline Sync Review**, and **Offline Queue Gaps** shows any sale the queue
believes is missing. Neither is your job, but it is worth knowing they exist so
that a dropped connection does not feel like lost money.

## Next

- [Your first order, end to end](first-order.md) - what happens to a sale after
  you complete it.
- [Reading an error code](error-codes.md) - turning a refusal into an action.
