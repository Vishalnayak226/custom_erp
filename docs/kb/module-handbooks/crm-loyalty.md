---
title: CRM & Loyalty
section: Module Handbooks
order: 80
summary: Earn and burn loyalty points on real sales, issue vouchers and run birthday/lapsed-customer campaigns, and read customer behaviour back through seven CRM reports.
audience: store manager, marketing, admin
last_verified: 2026-09-03
screens: [doctype-table, reports, configuration]
---

# CRM & Loyalty

There is no separate "CRM" screen in this application — Customer, Voucher
and Campaign are all ordinary masters managed from the same generic
record-list screens as everything else (**Setup → Master Data**). What makes
this a module rather than three unrelated doctypes is the loyalty ledger
underneath them: an append-only, never-directly-edited log of every point a
customer earned or burned, plus the tiering, campaign and CleverTap
integration logic that reads it.

## Loyalty points: earn and burn

A customer's balance is never a stored number — it is always computed as
`SUM(Earn) − SUM(Burn)` from the ledger. This is deliberate: the ledger is
the only source of truth, so there is no "edit balance" action anywhere in
this system, on purpose.

**Earning** happens automatically on a completed sale. The base points are
`net sale amount ÷ rupees-per-point` (**`loyalty.rupees_per_point`**,
**Settings → Configuration**), scaled by the customer's current
[tier](#loyalty-tiers)'s earn multiplier, and stamped with an expiry
(**`loyalty.point_expiry_days`** out from today). A sale doesn't earn points
on the portion already paid for with points — the amount used to compute
new points already excludes any same-checkout redemption.

**Burning (redemption)** is documented in
[Point of Sale](pos-operations.md)'s discounts-and-offers section — the
**Redeem Points** field at checkout, capped at the sale's own value, only
actually burns points inside `FinalizePOSCheckout` (an abandoned cart never
costs the customer anything). Redeeming more points than the balance holds
is refused (`CUSTOM-0134`).

**Reversal**: if a sale that already burned points then fails to complete,
the points are given back as a fresh Earn row referencing the same cart —
never by editing the original Burn, since the ledger is append-only. The
restored points deliberately carry **no expiry** of their own: they're a
correction of something that never happened, not a new accrual.

## Loyalty tiers

**Setup → Loyalty Tier Rules** (an admin config table, the same pattern as
approval rules): each tier names a **minimum lifetime spend** and an
**earn multiplier**. A customer's tier is recomputed after every earn, off
their lifetime `Paid` POS spend, and the highest threshold they meet wins.
Tiering is purely additive on top of the flat earn rate — a customer with no
formal record, no tier yet, or no configured rules at all still earns at the
plain 1× rate; nothing about tiering is a hard dependency of earning.

## Point expiry

A background worker sweeps every tenant hourly for lapsed Earn lots (past
their stamped expiry) and books a compensating Burn — tagged distinctly so
it's never confused with a customer-initiated redemption in the ledger or
in the reports below. This is a documented approximation, not true FIFO
lot-consumption: the ledger has no per-lot "remaining" tracking, so the
sweep expires `min(newly-lapsed-since-last-sweep, current balance)` — a
customer can never be pushed to a negative balance by a lot a later
redemption already effectively consumed.

## Vouchers (coupons)

A **Voucher** (**Setup → Master Data → Voucher**, individually or via bulk
CSV import — both come free from the same generic doctype/import machinery
every other master uses) is a code with a **Discount Type** (Percentage or
Flat), a value, an optional expiry date, an optional **Max Uses**, and an
optional **Customer** restriction (blank = anyone). Redemption
(`POST /api/v1/crm/voucher/redeem`) is row-locked, so two concurrent
redemptions of a `max_uses: 1` voucher can't both succeed, and a voucher
that reaches its max use count flips to **Exhausted** automatically.

This is a **separate mechanism from the Offer engine** documented in
[Point of Sale](pos-operations.md) — Offers are head-office rules that apply
automatically as a cart qualifies; a Voucher is a specific code someone was
given, checked and applied on request.

> [!WARNING]
> **Voucher validation and redemption have no caller anywhere in the app.**
> `POST /api/v1/crm/voucher/validate` and `.../redeem` are real, working
> endpoints (`engines/voucher.go`) — confirmed by grep, zero matches for
> "voucher" in any checkout-related code in `public/app.js`. A Voucher
> record can be created and listed like any other master, but there is no
> screen anywhere to actually enter a customer's voucher code and apply its
> discount. Today this needs a direct API call.

## Campaigns

A **Campaign** (**Setup → Master Data → Campaign**) names a **Trigger
Type** — **Birthday** or **Lapsed Customer** — and a message template. An
hourly worker scans every Active campaign: a Birthday campaign matches any
Active customer whose stored date of birth's month-day equals today's
(year-independent, so it recurs every year with no extra logic); a Lapsed
Customer campaign matches anyone whose most recent Paid sale is older than
**Lapsed Days** (or **`crm.default_lapsed_days`** if left blank on the
campaign itself) — someone who has never bought anything is not a match,
since "lapsed" implies having been active before. A match is logged once
per customer per day (never a duplicate send on the same day) as a CleverTap
event — see below — not sent through any other channel directly.

**Campaign ROI** attributes revenue as: every Paid sale from a customer that
campaign actually reached, dated on or after the campaign's own creation
date. This is a stated simplification (no real pre/post or control-group
comparison), the same spirit as this system's other approximated reports.

## The OTP-gated secure redemption path

A second, opt-in redemption path exists alongside the plain "Redeem Points"
field: **initiate** generates a 6-digit OTP (stored only as a hash, never
plaintext), sends it to the customer through the existing notification
system, and **verify** checks it before actually burning points. Two extra
controls layer on top of the plain path:

- **A daily redemption velocity cap** per customer
  (**`security.loyalty_max_redemptions_per_day`**) — an automatic point
  expiry never counts against it, since that's not customer-initiated.
- **Amount-based maker-checker approval**: above a configured rupee
  threshold, a verified OTP doesn't redeem immediately — it creates a Draft
  `LoyaltyRedemptionRequest` that routes through the same approval engine
  every other approval-gated document uses (see
  [Security, Roles & Approvals](security-approvals.md)), and only actually
  burns the points once that request is Approved.

> [!WARNING]
> **The OTP-gated redemption path has no caller anywhere in the app either.**
> `POST /api/v1/crm/loyalty-redemption/initiate` and `.../verify`
> (`engines/loyalty_redemption_security.go`) are real, tested endpoints with
> zero matches in `public/app.js` for either path. Today, a store that wants
> ID-verified or approval-gated redemption on top of the plain "Redeem
> Points" flow needs to call these directly; the plain, immediate path
> documented in [Point of Sale](pos-operations.md) is the only one reachable
> from the till.

## Merging duplicate customers

On the Customer record list, a row action lets you merge a duplicate
customer record into a surviving one — every order, invoice, voucher and
loyalty-point history moves to the surviving customer id, and the action
warns plainly that this cannot be undone before it runs. A second customer
record sharing the same mobile number is refused at creation in the first
place (`CUSTOM-0133`), so a merge is for records that predate that check or
were created with different contact details.

## CleverTap integration

If **Setup → Configuration → Integrations** has CleverTap credentials
(Account ID, Passcode, Region) configured, checkout events and the campaign
matches above are queued and pushed to CleverTap's real API by a background
worker — this system's own campaign log is populated either way (it's what
"already sent today" is checked against), but the actual external
notification only goes out once real credentials exist. An inbound
**segment-sync** endpoint also exists for pulling a CleverTap-defined
segment back into this system.

## Reports

All seven live under **Reports → CRM**:

| Report | Answers |
|---|---|
| **Customer 360** | Everything about one customer — loyalty balance, POS purchase history, invoice total — by customer id. |
| **Customer Lifetime Value** | Every customer's total spend, order count, first/last order, and whether they've churned (no order within a configurable window, default 90 days). |
| **RFM Customer Segmentation** | Recency/Frequency/Monetary scored and segmented per customer — the standard RFM model. |
| **Cohort Retention** | What fraction of each month's first-time customers came back in each month after. |
| **Loyalty Ledger Summary** | Per customer: total earned, total redeemed, current balance, transaction count. |
| **Loyalty Points Liability** | The rupee value of every outstanding, unredeemed point across all customers — what the business actually owes. |
| **Campaign ROI** | Per campaign: customers reached, attributed revenue, cost, and ROI where a cost is recorded. |

## Error codes reference

| Code | Meaning |
|---|---|
| `CUSTOM-0133` | A customer with this mobile number already exists. |
| `CUSTOM-0134` | Loyalty points requested for redemption exceed the customer's balance. |

## Troubleshooting

**"Insufficient loyalty points" on a redemption that looks like it should
work.** The balance is always computed live from the ledger, not cached —
check the customer's actual ledger history (Loyalty Ledger Summary report)
for a redemption or expiry you didn't expect.

**A customer's tier doesn't reflect a recent large purchase.** Tier is
recomputed after each *earn* event, off lifetime Paid spend. If the sale
hasn't posted as Paid yet, or `loyalty.recompute_tier_on_earn` is off, the
tier won't have updated from that sale alone.

**A birthday or lapsed-customer campaign didn't fire today.** Confirm the
Campaign is Active, and that its trigger data is actually correct — a
Birthday campaign needs `date_of_birth` set on the Customer record; a
Lapsed Customer campaign needs at least one prior Paid sale to have
"lapsed" from. The worker runs hourly, so also allow for that delay.

**A voucher or the OTP redemption path is needed but there's no button for
it.** Both are real, working server actions with no UI screen yet — see the
warnings above. They need a direct API call today.

## What is not here yet

**Voucher redemption** and **the OTP-gated secure loyalty redemption path**
are both real, tested server actions with zero UI callers — see the two
warnings above. Creating/listing Voucher and Campaign records themselves
works fine through the generic Setup screens; it's specifically the
redemption-time actions that have no button anywhere.

**No dedicated CRM dashboard or customer-facing screen** — every capability
here is reached through either a generic master-data list, a POS action
documented in [Point of Sale](pos-operations.md), or a report.