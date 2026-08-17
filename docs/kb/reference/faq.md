---
title: Frequently asked questions
section: Reference
order: 3
summary: The questions people actually ask in the first month, answered without a tour of the menus.
audience: everyone
last_verified: 2026-08-17
---

# Frequently asked questions

Grouped by what you are trying to do. If you have an error code in front of you,
go straight to [Troubleshooting by symptom](troubleshooting-index.md) instead.

## Access and sign-in

**Why can a colleague see a screen I cannot?**
Your role. The sidebar lists only what your role can open, and buttons you
cannot use are not rendered at all. Nothing in the application lets you grant
yourself access, which is deliberate. Ask an administrator.

**Is a whole missing module also a permission?**
Not always. A module missing from *everyone's* sidebar is usually an entitlement
- something your business has not licensed. One missing from only your sidebar
is a permission. An administrator can tell the two apart under **Settings »
Tenant Entitlements**.

**I typed my password wrong a few times and now nothing works.**
Five failed sign-ins in a minute are refused for a short cooling-off period.
That is a rate limit, not a lockout. Wait a minute.

**I lost the phone with my authenticator.**
Use a recovery code: on the sign-in screen, choose *Lost your phone? Use a
recovery code*. Then set up a new device from **My Profile**. If you have lost
both the phone and the codes, an administrator must reset your two-factor setup
- nobody can look up an existing code for you, because only a fingerprint of
each is stored.

**Can I stay signed in?**
Sessions expire, and the server re-checks your account and role on every request
rather than trusting a token until it expires. So a role change or a
deactivation takes effect immediately rather than at your next sign-in.

## Selling

**The till refuses to take a sale.**
Almost always no open cashier session at that location. Open one, entering the
cash physically in the drawer.

**Why was my discount not applied straight away?**
Large manual discounts are routed to a manager. The sale sits as Pending
Approval and nothing is charged until they decide it. Offers set up by head
office never need approval.

**An offer I expected did not appear.**
Three usual causes: the cart does not meet the condition yet, the offer needs a
coupon code you have not entered, or it is restricted to a customer tier and you
have not looked the customer up.

**Can I reprint a receipt?**
Yes, at any time. It is rebuilt from the recorded sale rather than from the
screen, so it always shows what was originally rung up.

**Do I lose sales if the internet drops?**
No. Sales queue locally and sync when the connection returns. A manager reviews
anything that did not apply cleanly under **POS » Offline Sync Review**.

## Stock

**The system says we have none and I am looking at it.**
In order of likelihood: the goods receipt was never posted; the stock was
received but never put away; the bin is in a blocked or quarantined condition;
or it is reserved for another order. Only the last one is visible in a raw
count, which is why *available to sell* is the figure to read.

**What is the difference between the two stock numbers?**
Physical quantity is what is in the building. Available to sell subtracts what
is already promised to other orders. Promise customers the second one.

**Should I correct a cycle count to match the system?**
No. The variance is the only signal that something upstream is wrong. Adjusting
the count to agree with the record destroys the evidence and keeps the error.

## Buying

**Do I have to raise an RFQ before a purchase order?**
No. A purchase order does not require one, and nothing gates a PO on an RFQ
existing.

**Why can I not approve my own purchase order?**
Self-approval is refused everywhere and is not configurable. Whoever raises a
document cannot be the person who approves it.

**I edited an approved document and now it says Pending Approval again.**
That is intended. An approval is of a specific version of a document, not of the
document forever.

**Where does the PO number come from?**
It is issued on save, from the numbering rules an administrator configured under
**Settings » Prefix Configs**. You never type one.

## Products and tax

**Why will an item not save?**
Usually a missing HSN Code or GST Rate. Both are required, because the system
cannot price a sale it cannot tax.

**My product genuinely is not taxed.**
Set **Tax Treatment** to Exempt, Nil-Rated or Zero-Rated rather than typing a
zero rate - a `0` on its own is rejected because it is indistinguishable from a
field nobody has filled in. HSN is still required on all four treatments.

**An invoice has the wrong tax on it.**
Fix the Item, not the invoice. Tax is resolved from the product's HSN code and
the places of supply; an invoice that looks wrong is nearly always a product
classified wrong.

## Orders

**Did my marketplace order actually arrive?**
Look at the **Source** column in Order Management - it shows the system the
order came from and that system's own order id. If the channel's number is not
there, the order has not arrived. You do not need anyone to check a database.

**Why was my manual order refused?**
Most often *insufficient stock for reservation*. A manual order goes through the
same engine a channel order does, on purpose, so it can be refused for the same
reasons. Receive the stock and try again.

**I clicked Create Order twice.**
If you filled in **Reference**, the same reference returns the same order rather
than creating a second one. This is exactly what that field is for.

**Can I fulfil part of an order now and the rest later?**
Yes - tick the lines and use **Split selected lines**. It stays one order with
one id and one invoice chain. At least one line has to stay behind.

## Money

**The trial balance does not balance.**
Something upstream failed to post. Check **Finance / GL** with **As Of Date**
set to today, then work backwards through the day's receipts and invoices. The
sooner you look, the smaller the search.

**We received a different amount from what we invoiced in dollars.**
Expected. Between the invoice date and the payment date the rate moves. Enter
the **settlement date** and, if you have it, the **exchange rate** from the
remittance advice; the difference is recorded automatically as a foreign
exchange gain or loss.

**Should I edit an invoice to correct it?**
No - issue a debit or credit note. Editing history makes the correction
invisible to everyone who already acted on the original.

## The system itself

**Who can see what I did?**
Everything is logged - who, what, when, which record - and administrators can
read it under **Settings » Activity Log**. This is what makes it safe to give
people real access rather than routing everything through one person.

**Can we add our own fields?**
Yes. An administrator adds fields and record types under **Settings » Database
Schema Design**, and can rename any field's label without touching data using
**Dynamic Labels**.

**Can another business on this system see our data?**
No. Each tenant's records are held separately in the database, not merely
filtered in the application.

**Something I expected is not here.**
This system is under active development. Ask your administrator before assuming
you are doing it wrong - it may not be built yet.
