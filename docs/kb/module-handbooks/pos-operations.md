---
title: Point of Sale
section: Module Handbooks
order: 10
summary: Open a till, ring up a sale with automatic offers and tax, take a return, print silently, and reconcile the drawer at close — all backed by the same server-side checks whether the till is online or not.
audience: cashier, store manager, admin
last_verified: 2026-09-02
screens: [pos, doctype-table, reports, approvals, configuration]
---

# Point of Sale

A POS sale looks like one click — scan, total, pay — but three separate
engines sit behind it: a cashier-session guard that refuses to sell without
an open till, an offer evaluator that recomputes every discount server-side
so nothing a browser displays can be trusted blindly, and a checkout path
that posts stock and the general ledger atomically, with an offline queue in
front of all of it so a lost connection never stops a sale. This handbook
covers the till lifecycle, the sale itself, returns, discounts and offers,
offline mode, and closing the day — going deeper into setup and
troubleshooting than the existing walkthrough in
[USER_GUIDE.md §4](../../guides/USER_GUIDE.md) and
[USER_SOP.md §3](../../guides/USER_SOP.md).

## Before the first sale

Three things have to exist first, or checkout is refused with an error that
only makes sense once you know the list:

1. **A Location with Type = Store**, to sell from.
2. **An Item with HSN Code and GST Rate filled in**, and stock already
   received against it (a new Item starts at zero quantity).
3. **An open cashier session at that location** — see below.

## Till lifecycle: opening and closing a session

A **POSSession** is the cashier's shift. Nothing rings up without one open,
and the check is real: `handleCheckout` looks up an Open POSSession keyed to
the caller's own resolved identity (never anything the request body claims),
and refuses with **"Cash opening is required before billing"**
(`POSOFF-0238`) if none exists.

**Opening one** (**POS → POS / Billing**, top bar): type the location, click
**Open Session**, enter the cash physically in the till. The server refuses a
second concurrently-open session for the same cashier (any location) or the
same location (any cashier) — one till, one active operator at a time. A
**POS Profile** can optionally be attached to a session (default payment
mode, invoice number series, a default opening float) but it is not
required — opening a session only actually needs a location.

**Closing one**, at end of shift: enter the cash counted. The server, never
the browser, computes what was **expected** — the opening float plus every
Cash-mode `Paid` cart tagged with this session — and shows it against what
was **counted**. The gap is the **variance**.

- A cart with no `payment_mode` recorded at all (data from before payment
  modes existed) is treated as Cash for this sum, so an old cart's cash
  still lands in the expected total.
- If `|variance|` exceeds the configured tolerance (**₹50 by default** —
  **Settings → Configuration**, "Cash drawer variance tolerance",
  `pos.drawer_variance_tolerance`), the close is refused
  (`POSOFF-0240`) until a written reason is supplied. Below tolerance, the
  session closes with the variance simply recorded — no reason required.
- Closing is refused for anyone other than the cashier who opened that
  session.

## Ringing up a sale

1. Scan or type the SKU (code, barcode, or internal id all resolve the same
   item) and press Enter. Edit **Qty**, **Sale Price**, **Cost Price**
   per line directly in the cart table.
2. Optionally look up a **Customer Code** — needed for loyalty points and
   for any offer restricted to a customer tier.
3. Applicable offers and coupon results appear automatically above the
   total — see **Discounts and offers** below.
4. Set **Discount %** and **Payment Mode**, click **Complete Sale**.

**What checkout actually does, in order**, per `handleCheckout`
(`internal/server/handlers_pim_pos_finance.go`):

1. Rejects any non-positive qty or negative price before anything else runs
   — this matters because qty is later negated to decrement stock, and a
   pre-negative qty would silently flip to a stock *increase* instead of
   being rejected.
2. Computes GST for every line (`ComputeGSTForLines`) — `sale_price` is
   treated as tax-inclusive (MRP convention); the taxable amount is backed
   out of it, not added on top.
3. Pre-checks loyalty redemption against the customer's real balance if
   **Redeem Points** was used — but the points are only actually burned
   later, inside finalize, so an abandoned cart costs the customer nothing.
4. Re-evaluates offers server-side (never trusts the preview the cashier
   saw) via the same `EvaluatePOSOffers` engine covered below.
5. Confirms the open session, exactly as at the till-open check above.
6. If the discount requires approval (see next section), the cart claims as
   `Draft` and stops here — no stock or GL movement yet.
7. Otherwise, claims the cart atomically by `cart_number` — a duplicate
   submission (network retry, double-click, a race) never double-decrements
   stock or double-posts the GL; the original result is replayed back
   instead.
8. `FinalizePOSCheckout` posts the inventory decrement and the GL entries
   together.

The response includes `sale_total`, `cost_total`, the GST breakdown, the
loyalty discount actually applied (capped at the sale value, with any
unusable remainder returned to the customer's balance), the offers applied,
and `amount_due` — what to actually collect after loyalty and offers are
both taken off.

## Discounts and offers

Two separate discount mechanisms exist on the same sale, and they behave
differently on purpose.

**Offers** (`Offer` documents, evaluated by `EvaluatePOSOffers` in
`engines/pos_offers.go`) are head-office rules the cashier does not apply by
hand — they just appear as the cart qualifies:

| Offer type | What it does |
|---|---|
| Percentage Off | A percentage off the bill, an item, or a category |
| Flat Off | A flat rupee amount off, capped at the value of what it applies to |
| Buy X Get Y | The cheapest qualifying units in each complete buy+get group are free |
| Bundle Price | Every complete group of N units is priced as a bundle; the most expensive units are bundled first |

Offers are sorted by **priority** (lower first, then name for a
deterministic tie-break). A non-stackable offer that applies **ends
evaluation** — nothing after it is considered — so "one big offer OR several
small ones" is predictable rather than depending on row order. A coupon code
that matches no live, qualifying offer is reported back as unmatched rather
than silently dropped, so the cashier can tell the customer why it didn't
apply. A customer-tier-restricted offer needs the customer looked up
*before* it will show at all.

`POST /api/v1/pos/offers/preview` is what the till calls as the cart
changes, purely so the cashier sees the discount before payment — it is
explicitly **not** what the sale trusts. Checkout re-runs
`EvaluatePOSOffers` from scratch against the tenant's own `Offer` rules, so a
tampered client can't invent a discount, and a cart replayed later from the
offline queue is priced against whatever rules are live *when it lands*, not
whatever was live when it was rung up.

**Manual discount** (the **Discount %** field) is different: it is the
cashier's own call, not a head-office rule, and it is what the
discount-approval gate below watches. Offers never trigger that gate.

### Discount-approval gate

A manual discount at or above a configured threshold does not complete the
sale — it routes through the same maker-checker engine every other
approval-gated document in this ERP uses
(`RequiredApproverRoleForAmount("POSCart", discount_pct)`). The seeded
default is **10% or more → Store Manager approval**; below that, checkout
completes immediately. The cashier sees *"requires manager approval before
it completes"*; the sale sits as **Pending Approval** until a manager
decides it on the **Approvals** screen. Approving finalizes the sale exactly
as if checkout had gone straight through (stock and GL post then, not at
submission); rejecting cancels it — nothing ever posts.

An admin retunes the threshold or the required role on **Settings →
Approval Rules**, record type `POSCart` — note this rule bands on the
*discount percentage*, not the cart's rupee total, which is the one doctype
in this engine that works that way.

## Returns

The **Process a Return** panel on the POS / Billing screen is deliberately
separate from the sale cart, so an in-progress return can never get mixed
into an in-progress sale. Enter the original order/cart number and the
return location, add each SKU being returned, and submit.

This calls `POST /api/v1/fulfillment/return` (`ProcessReturnAnywhere` in
`engines/fulfillment.go`) — a same-visit, refund-in-full return, not the
QC-and-disposition workflow. Before anything is posted:

- The original bill must resolve (`SALESR-0131` if it doesn't) — `POSCart`
  is checked first since it carries per-line item data; a `SalesInvoice`
  match only proves a bill exists, with no quantity to cross-check against.
- The return must fall inside the configured return window — **30 days by
  default** (**Settings → Configuration**, "Sales return window",
  `sales.return_window_days`; 0 disables returns entirely). Outside it,
  `SALESR-0129`.
- The returned quantity, added to everything already returned against this
  same order, cannot exceed what was actually sold (`SALESR-0130`).

Once accepted, stock is put back at the return location immediately and two
reversing double-entry postings are made in the same call: revenue is
debited and Cash/Bank credited for the sale price, and Inventory is debited
against COGS for the cost price — so the refund and the stock movement both
land the moment the return is submitted, with no separate approval step.

> [!WARNING]
> **This panel is a refund, not an exchange, and it does not capture a
> batch or serial number.** `ProcessReturnAnywhere` only restocks and
> refunds — there is no "swap for a different SKU" option here. A separate,
> more capable return workflow exists server-side — `POST /api/v1/returns`
> and its QC-disposition step (`ApplyReturnQC`, `engines/returns.go`) *do*
> support recording an exchange SKU per line — but as of this writing that
> whole `/api/v1/returns` family has no caller anywhere in the app; there is
> no screen for it. If a customer wants an exchange rather than a refund,
> today's real workflow is a return here plus a fresh sale for the new item,
> not a single "exchange" action. If the returned item is batch- or
> serial-tracked, see
> [Batch, Serial & Expiry Traceability](traceability-batch-serial.md) — this
> panel does not ask for a lot or serial number on the way back in.

## Offline mode

If the till loses its connection mid-shift, selling does not stop.
`checkoutOnlineOrQueue` tries `POST /api/v1/checkout` first; on a genuine
network failure (not a 401 or a 429, which get handled normally) the sale is
pushed onto a queue kept in the browser's own `localStorage`
(`erp_pos_offline_queue`) instead, and the cart clears as if the sale had
gone through.

- **Replay is oldest-first**, triggered by the browser's `online` event and
  a 30-second fallback poll. Each queued cart is resubmitted to
  `/api/v1/checkout` with `offline_synced: true`, using the *same
  cart_number* it was queued under — that is what makes retrying a
  still-queued sale safe, since checkout's own idempotency claim treats a
  repeat of the same cart_number as a replay, not a new sale.
- **A network failure during replay stops the sync there** — that cart, and
  everything queued after it, stays queued for the next attempt; nothing is
  reordered or dropped just because a later cart happened to succeed first.
- **A real rejection (not a network failure) is dropped**, with an alert
  naming the cart number — retrying a sale the server will never accept
  would otherwise block the queue forever over one bad cart.
- **`offline_synced: true` is what lets a replayed sale push stock
  negative** instead of being rejected outright — the goods already left
  the store and the payment was already taken before the till ever saw a
  connection again. `FinalizePOSCheckout` flags the resulting shortfall as
  a `POSOfflineSyncVariance` record instead of silently absorbing it.
- **A small badge on the session bar** shows how many sales are still
  queued locally.
- **A best-effort heartbeat** (`POST /api/v1/pos/offline-heartbeat`) fires
  on every queue change and on tab-hide/close, telling the server which
  cart_numbers are currently queued *before* they sync — a checkpoint from
  before a gap, not just whatever eventually arrives. This can't see a cart
  that is queued and lost with zero connectivity ever in between (nothing
  can, the server was never told), but it catches the far more common case
  of a queue that existed for a while, with at least one connectivity blip,
  before being cleared.
- At session close, if any cart_number from the last heartbeat still never
  landed as a real document, a **POSOfflineQueueGap** record is written for
  review — this never blocks the close.

Both review lists are plain record-list screens, reached from the **POS**
sidebar flyout: **Offline Sync Review** (`POSOfflineSyncVariance` — replayed
sales priced differently than the till showed at the time) and **Offline
Queue Gaps** (`POSOfflineQueueGap` — carts heartbeated but never actually
synced).

## Printing the receipt

Every completed sale can print; the only question is whether it goes
straight to the till printer or opens the browser's print dialog. Full setup
is in [QZ_PRINTING_SETUP.md](../../guides/QZ_PRINTING_SETUP.md) — this section
covers what's specific to the receipt itself.

- **Only a `Paid` cart can print.** `BuildReceiptPayload` refuses anything
  else — Draft or Pending Approval is money not yet collected, Failed is a
  sale that never happened — with `GLOBAL-0019` rather than printing a
  receipt for nothing.
- **The total always agrees with what a reprint shows**, because the
  receipt recomputes from the stored `POSCart` document (its `sale_price` ×
  `qty` per line, its stored `applied_offers`/`offer_discount`, its
  `redeem_points`), never from whatever the browser still has in memory.
  Offer and loyalty lines print as their own line items, not folded into the
  total, so the printed receipt matches the cash actually in the drawer.
- **A 58mm roll** needs **Label Width (mm)** set to `58` on the Printer
  record whose **Default For** is `Receipt`, or lines are laid out for 80mm
  and wrap.
- Every print (or failed attempt) is logged to `print_job_log`, viewable
  from **Reports → the print audit trail**, so a disputed reprint traces to
  an operator and a time.

## Day-close reconciliation

Closing the cashier session (above) *is* the day-close reconciliation step
— the expected-vs-counted cash variance it computes is the whole point.
Two reports round it out:

| Report | What it answers |
|---|---|
| **Sales Register** (`sales-register`) | Every `Paid`/`Settled` POSCart — cart number, location, payment mode, sale total, date — the flat list a manager reconciles the till against. A corrupt cart is listed with `data_issue` filled in rather than silently dropped from the count. |
| **Cash Book** | Every cash-affecting posting with a running balance, across the whole business, not POS-specific. |

Reached from **Reports** in the sidebar. Both export and schedule like any
other registered report.

## Error codes reference

| Code | Meaning |
|---|---|
| `POSOFF-0238` | No open cashier session at this location/cashier — open one before billing. |
| `POSOFF-0240` | Drawer variance at session close exceeds the configured tolerance without a reason given. |
| `POSOFF-0241` | An offline-replayed cart hit a cart_number the server has in a state that isn't a clean replay (still Processing, or Failed from a prior partial attempt) — a genuine sync conflict, not an ordinary duplicate. |
| `POSOFF-0243` | The Pine Labs payment terminal used for a card/UPI transaction isn't mapped or is inactive. |
| `SALESR-0129` | Return attempted outside the configured return window. |
| `SALESR-0130` | Return quantity, plus anything already returned against this order, exceeds what was sold. |
| `SALESR-0131` | No original bill found for the order reference given. |
| `GLOBAL-0019` | Receipt print attempted on a cart that isn't `Paid`. |
| `CUSTOM-0134` | Loyalty redemption requested exceeds the customer's actual points balance. |

## Troubleshooting

**"Cash opening is required before billing" on every sale.** No Open
POSSession exists for this cashier at this location — open one first. Note
a session is per cashier *and* per location; a session open at one store
does not cover another.

**Session close refuses to complete, asking for a reason.** The
counted-vs-expected variance is beyond the configured tolerance (₹50 by
default). Either recount the drawer, or enter a written reason — the close
proceeds either way once a reason is given.

**A sale I rang up doesn't show in the expected-cash total at close.** Only
`Paid` carts tagged to *this* session count, and only Cash-mode ones (Card
and UPI sales post to their own clearing accounts, not the till). If the
sale is still `Pending Approval` (a large discount awaiting a manager), it
hasn't posted yet and won't show until approved.

**An offer I expected doesn't appear.** Check the cart actually qualifies
(minimum spend, minimum qty), that the customer is looked up first if the
offer is tier-restricted, and that a coupon code is spelled correctly — an
unmatched code is reported in plain words rather than silently ignored.

**The discount panel and the amount charged disagree.** The panel
(`/pos/offers/preview`) is a preview only; checkout always recomputes for
real. If they ever disagree, the charged amount is the correct one — this
usually means an offer's validity window or stock changed between the
preview and the click.

**A return is rejected with "no original bill found."** The order/cart
number doesn't match any `POSCart` or `SalesInvoice` on file — check for a
typo, or that it's really this business's own sale (not a marketplace
order, which follows a different return path).

**A return is rejected for exceeding the return window or the sold
quantity.** These are `SALESR-0129`/`SALESR-0130` — see the reference table
above. The return window is configurable; the sold-quantity check is not
(it also nets off everything already returned against the same order, so a
second partial return can't add up to more than was ever sold).

**Offline sales aren't syncing.** Check the badge on the session bar for a
non-zero queued count, and confirm the browser actually shows online — the
sync loop only runs when `navigator.onLine` is true, triggered by the
browser's own `online` event plus a 30-second poll. A sale the server
outright rejects (not a connectivity problem) is dropped with an alert
naming the cart number, not retried forever.

**"Print receipt?" doesn't go straight to the printer.** QZ Tray isn't
installed/running on this PC, or no Active Printer has **Default For** set
to `Receipt` — see [QZ_PRINTING_SETUP.md](../../guides/QZ_PRINTING_SETUP.md).
Nothing is broken either way; the browser's own print dialog opens instead.

## What is not here yet

**Exchanges are not a first-class action at POS.** The in-store return
panel only refunds; the server-side return workflow that does support an
exchange SKU (`ApplyReturnQC`'s `exchange_for`, part of the broader
`/api/v1/returns` → QC-disposition → refund-request flow in
`engines/returns.go` and `internal/server/handlers_returns.go`) has no UI
screen anywhere in the app — confirmed by grep, `ReturnRequest` and
`RefundRequest` are never referenced in `public/app.js`. That whole richer
workflow (courier RTO intake, QC disposition per line, exchange-for-SKU,
separate refund approval) is real and tested, but reachable only by direct
API call today, the same shape of gap the
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md) handbook
documents for its own post-receipt actions.

**Batch/serial capture does not happen at POS return.** `ProcessReturnAnywhere`
restocks by SKU and quantity only — see the warning above.

**There is no dedicated "Z-report" or end-of-day summary document.** Day
close is the session-close variance plus the Sales Register report, not a
single generated end-of-day statement.
