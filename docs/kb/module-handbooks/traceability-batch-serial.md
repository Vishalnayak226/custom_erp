---
title: Batch, Serial & Expiry Traceability
section: Module Handbooks
order: 20
summary: Track a lot or an individual unit from receipt to dispatch, pick the right stock first by expiry, and answer a recall in two directions.
audience: warehouse operator, category manager, admin
last_verified: 2026-08-31
screens: [grn, wave-picking, doctype-table, reports]
---

# Batch, Serial & Expiry Traceability

Some items need more than a quantity on a shelf. A food or pharma SKU needs a
lot number and an expiry date, so the oldest stock ships first and nothing
past its date goes out the door. A high-value or warrantied SKU needs an
individual serial number, so one specific unit can be traced from the carton
it arrived in to the order it shipped on. This module adds both, on top of
the existing receiving and picking flows rather than beside them.

## Turning it on for an item

Tracking is opt-in, per item, on the Item master's **Tracking Mode** field:

| Mode | Meaning |
|---|---|
| None | Default. Stock is a plain quantity, exactly as before. |
| Batch | Every unit belongs to a lot. Requires a batch/lot number at receipt. |
| Serial | Every unit is tracked individually. Requires one serial number per unit at receipt. |
| Batch and Serial | Both — a serialised unit that also belongs to a lot (e.g. a serialised device with a manufacturing batch). |

Three more Item fields only matter for a Batch-tracked item:

- **Shelf Life (days)** — if you record only a manufacture date at receipt,
  expiry is derived automatically as manufacture date + shelf life.
- **Min Shelf Life on Receipt (days)** and **Min Shelf Life on Pick (days)** —
  the two expiry gates, below. Leave at zero to not enforce either.

An item you never touch this on behaves exactly as it always has — nothing
about untracked stock changes.

## Receiving: capturing a batch or serial

Batch and serial numbers are captured on the **GRN Workbench**, the same
screen every goods receipt already goes through — there is no separate
capture step. Add a line for a tracked SKU and the row grows the fields the
item's tracking mode calls for:

- **Batch-tracked** (or Batch and Serial): a **Batch / Lot** field appears,
  marked required. Enter the lot number off the carton, plus manufacture date
  and/or expiry date if known. The first receipt of a lot number creates its
  `Batch` record automatically — there is nothing to set up in advance.
- **Serial-tracked** (or Batch and Serial): a **Serial Numbers** field appears
  (one per line, textbox), and the count must match the accepted quantity
  exactly — one serial per unit, no more, no fewer.
- Any item — even an untracked one — can still take an optional batch/lot
  number if you want to record it; it just isn't required.

**A lot's manufacture and expiry dates are set once, on the first receipt,
and never silently overwritten by a later one.** If receipt #4 of the same
lot number leaves those fields blank, the dates already on file stay put — a
mistyped date on a later delivery cannot re-date stock already on the shelf.
If you do need to correct a lot's dates, edit the `Batch` record directly
(**Setup → Master Data → Batch**).

## Picking: oldest-expiry-first, automatically

For a Batch-tracked item, wave and bin pick-list generation allocate stock
**FEFO** (first-expiry-first-out) instead of FIFO — you don't choose this per
pick, it follows from the item's tracking mode. Lots with no recorded expiry
are always picked *last*, never first, since an unknown date must not jump
ahead of a lot known to expire soon. An untracked item is unaffected and
still allocates FIFO exactly as before.

Two expiry gates apply automatically, using the Item's own thresholds:

- A lot is only offered to a pick if at least **Min Shelf Life on Pick**
  days remain on it.
- An `Expired`-status lot, or one already in quarantine, is never offered at
  all.

**Stock in a bin is only visible to FEFO once that bin/lot combination has
been explicitly recorded** — receiving a lot on a GRN creates its `Batch`
record, but does not by itself tell the system which bin the stock ended up
in. See the known gap below: today that link is written by a direct API
call, not by the ordinary Putaway screen, so a lot moved through the normal
putaway flow alone will not yet appear to FEFO.

> [!WARNING]
> **Known gap, current as of this writing.** Linking a received lot to the
> bin it was put away in (`POST /api/v1/wms/batch/putaway`), running the
> expiry sweep, recording a manual lot consumption, and the serial
> allocate/ship/return/scrap transitions (including the pack-time
> allocated-order check below) are all real, tested server actions — but
> none of them has a button in the app yet. The ordinary Putaway and Pack
> screens do not call them. Until that UI catches up, these need a direct
> API call (by an admin or integrator), not something a warehouse operator
> can do by clicking through the app. Flagged in `docs/micro_checklist.md`
> as a follow-up item.

## Serial verification at pack

A serial number is allocated to a specific order when a pick is confirmed
against it, and the system can reject packing that unit against a
**different** order (`INVENT-0104`) than the one it's reserved for. This
check exists and is tested, but — per the gap above — it is only reachable
via a direct API call today, not from the app's own Pack screen. If it's
been run for a serial and rejected, that means the scanned serial belongs to
a different order; look it up in Serial Number Inquiry to see what it's
actually reserved for.

## Expiry sweep: moving expired stock out of available

Nothing removes expired stock from the sellable pool on its own — there is
no background timer, and (per the gap above) no button in the app either.
An admin or integrator runs it as a direct API call
(`POST /api/v1/wms/batch/expiry-sweep`); it marks any lot past its expiry
date `Expired` and moves its stock from `Available` into the same
quarantine (`qc_hold`) condition the damaged-goods flow uses, through the
one shared stock-condition transition every quarantine move goes through —
so an expired lot stops being pickable everywhere at once, and the batch's
own history records it. Running the sweep twice in a row is harmless; it
only acts on lots that are still `Active` and past their date.

## Restricting a lot to certain customers

If a customer contractually requires (or refuses) certain lot attributes —
country of origin, a particular grade — record it as a **Lottable
Constraint** (**Setup → Master Data → Lottable Constraint**): a customer, an
item (or blank to apply to everything that customer buys), an attribute key,
and the allowed values. Recording a Batch's own lottable attributes happens
on the `Batch` record's **Attributes** field at receipt or afterward.

This is enforced at the point a lot is manually consumed against a customer
order (the `Consume` action, below) — a lot whose attributes don't satisfy
the customer's constraint is rejected (`INVENT-0114`, the same code used
elsewhere in the app for stock reserved against someone else). It is **not**
yet applied while a wave pick list is being generated, because a wave pools
demand for a SKU across every order in it before a lot is chosen — filtering
per-customer at that pooled stage is a bigger design question than this
release solves. For now, treat a wave-picked, lottable-constrained SKU as
needing a manual consume/verify step rather than trusting the wave alone.

## Recording a manual consumption

`POST /api/v1/wms/batch/consume` is the explicit action that deducts a
specific lot against an order, independent of the pick flow — it's what an
admin or integrator uses to record which lot actually went out against an
order a wave pick didn't already resolve lot-by-lot, or to apply a lottable
constraint by hand. Per the gap above, there is no app screen for this yet.

## Finding a lot or a serial: the five reports

All five live under **Reports → WMS** — no separate traceability screen, so
they get CSV/PDF export and scheduling for free like every other report.

| Report | Answers |
|---|---|
| **Batch Near-Expiry Watchlist** | What's expiring within N days (default 30) — the report a food/pharma warehouse opens every morning. |
| **Batch Stock Inquiry** | Where is lot X right now — filterable by item, location, or batch/lot. The first question of a recall: what do we still hold. |
| **Batch Movement History (Recall)** | Everywhere lot X has been, in order — the chain of custody, including which orders it shipped against. The second question of a recall: who already has it. |
| **Serial Number Inquiry** | Where is this specific unit now (or what serial-tracked stock of an item exists), filterable by status (In Stock / Allocated / Shipped / Returned / Scrapped). |
| **Serial Movement History** | The full chain of custody for one serial number, receipt to dispatch. |

A recall does not need any extra setup to be answerable — both directions
fall directly out of the batch/serial numbers already captured at receipt.

## Unit conversion (UOM)

If you buy, store, or bill an item in a different unit than you sell or pick
it in, define a **UOM** pair once (**Setup → Master Data → UOM Conversion**:
item, from-unit, to-unit, factor — e.g. "1 CASE = 12 EA"). One entry covers
both directions; you don't need a second row for the inverse. This is
currently used in three places:

- **Cartonization** — pack suggestions convert a UOM'd quantity to eaches
  first.
- **Pick lists** — if a task line carries a pick UOM, the pick list shows a
  display-only quantity in that unit alongside the actual pick quantity
  (which always stays in eaches). A missing conversion never blocks a pick —
  it just means no display hint.
- **3PL storage billing** — a billing rate can be quoted per case (or
  whatever unit) instead of per each.

It is **not** wired into pricing — a price list is still entered per the
item's base unit.

## Troubleshooting

**"Batch / Lot is required" on a GRN line.** The item's Tracking Mode is
Batch or Batch and Serial. Enter the lot number from the carton; if it's a
new lot, the `Batch` record is created for you.

**A batch/lot save is rejected as a duplicate.** Batch numbers are unique
**per item**, not globally — two different SKUs can legitimately share the
same lot text (e.g. two suppliers both shipping something labelled
"LOT-001"). If you're sure it's genuinely a new lot, check you have the
right item selected.

**A batch is rejected with an expiry date before its manufacture date.**
Always a typo — fix whichever date is wrong. This is refused rather than
saved, because an inverted date would make FEFO allocate that lot first,
permanently.

**FEFO picking skips stock you can see in a bin.** For a batch-tracked
item, only stock already linked to a lot in `bin_stock_batch` is eligible —
and today that link is written by a direct API call
(`/wms/batch/putaway`), not by the ordinary Putaway screen. See the known
gap above; this is the most common way FEFO looks "broken" when the data
is really just never linked.

**A serial was rejected while packing (`INVENT-0104`).** The scanned unit is
reserved for a different order than the one being packed. Look it up in
Serial Number Inquiry to see what it's actually allocated to.

**Expired stock still shows as available.** The expiry sweep is an API
action with no app button yet (see the known gap above) — someone with
API/admin access needs to call it. It's idempotent, so calling it again if
unsure does no harm.

**A lot is rejected against a customer order (`INVENT-0114`).** Either a
Lottable Constraint on that customer/item rejects this lot's attributes, or
the stock is reserved for a different order. Check the constraint before
assuming it's a data error.

## What is not here yet

**UI coverage today is receipt and read-only only.** Capturing a batch/lot
or serial number happens through the GRN Workbench, and the five reports
are fully usable from Reports → WMS. Everything else that acts on a batch
or serial after receipt — linking it to a bin, consuming it against an
order, sweeping expired stock, and the serial allocate/ship/return/scrap/
pack-mismatch transitions — is a real, tested server action reachable only
by direct API call, with no app screen yet. That is the single gap worth
knowing about before relying on this module for daily floor work.

Scanning a pick at the mobile RF screen does not yet consume a specific lot
or serial automatically either — it updates the task's picked quantity
only; the explicit consume action above is what actually closes the loop.
Scheduling the expiry sweep to run on its own, rather than needing someone
to trigger it, is a separate, deliberate open decision.
