---
title: A day as a cashier
section: Role Journeys
order: 2
summary: Open the till, ring up sales, handle the four things that commonly go wrong, and close out with a clean variance.
audience: cashier
last_verified: 2026-08-17
screens: [pos]
---

# A day as a cashier

Almost everything you do is on one screen: **POS » POS / Billing**. This is that
screen in the order you meet it.

## Start of shift: open the till

1. Open **POS » POS / Billing**.
2. Start typing your shop's **name** into **Location** and pick it from the
   list. The code and short code work too, if that is what you know it by.
3. The bar across the top says whether a session is open there. If it says *"No
   open session"*, click **Open Session** and type **the cash physically in the
   till right now**.

You open a session once per shift, not once per sale. Until one is open,
checkout is refused with *"Cash opening is required before billing"* - which is
the single most common first-day confusion, and now you know it.

## Ringing up a sale

1. Type or scan the item's barcode into the box and press Enter. The item's
   code, its barcode and its internal id all find the same item.
2. Repeat for everything the customer is buying.
3. For a loyalty customer, type their code into **Customer Code**. **Redeem
   Points** spends their points on this sale; the discount comes off what you
   collect, and the points are only actually deducted once the sale completes.
4. Any offers that apply appear on their own above the total. If the customer
   has a coupon, type it into **Coupon code**.
5. Check the total. Tax is worked out for you.
6. Choose the payment mode and click **Complete Sale**.
7. Answer **Print receipt?**. On a till with a receipt printer configured it
   prints straight there; otherwise the browser's print dialog opens.

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
- **The Discount % box is separate** - that is a manual discount you apply, and
  a large one may need a manager. Offers never need approval; they are rules
  head office already signed off.

## The four things that go wrong

**"Cash opening is required before billing."** No session is open at that
location. Open one.

**"Not enough stock."** The system is refusing to sell what it does not have.
Tell your manager; you cannot fix this from the till.

**A large discount needs manager approval.** The sale sits as **Pending
Approval** and nothing is charged until a manager decides it on the
**Approvals** screen. It will not print a receipt in the meantime, because the
money has not been collected.

**A barcode does not scan.** Try the item's code instead. If neither finds it,
the item does not exist in the system yet - that is a job for whoever maintains
the product list.

## Reprinting a receipt

Safe at any time. The receipt is rebuilt from the recorded sale, not from
whatever happens to be on screen, so it always shows what was originally rung
up - including offers and points as their own lines, so the printed total
matches the cash that went into the drawer.

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
