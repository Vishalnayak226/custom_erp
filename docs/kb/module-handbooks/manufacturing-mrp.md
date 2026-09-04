---
title: Manufacturing & MRP
section: Module Handbooks
order: 100
summary: Build a multi-level BOM, run a production order through routing/QC/completion, and let MRP tell you what to build next and what raw materials that would need.
audience: production planner, plant manager, admin
last_verified: 2026-09-03
screens: [manufacturing]
---

# Manufacturing & MRP

A production order's life is a straight line — Draft, material issued,
worked through its routing, completed — but almost every step along that
line has a real check behind it: a BOM that changed after you committed to
it, a work center that can't actually fit what you're asking of it today, a
quality gate that has to pass before finished goods become sellable stock.
This handbook covers the whole line, plus MRP's answer to "what should I
build next," and subcontracting for a step someone outside the business does
for you.

Reached from the **Manufacturing** screen: Orders, Quality, MRP, Schedule
and Subcontracting tabs. Work Centers, Routings and BOMs beyond the quick
form on the Orders tab are managed under **Setup**.

## The Bill of Materials (BOM)

A **BOM** names a finished good (**Parent Item**) and its component lines,
each a SKU and a quantity. The quick form on the Orders tab (code, parent
item, `sku:qty, sku:qty` components) covers a simple, single-level BOM;
everything below is edited on the BOM record itself under **Setup**.

- **Multi-level BOMs**: a component line can itself be a sub-assembly — set
  its `sub_bom` to another BOM's id instead of issuing it as a raw material
  directly. Building a production order's material list recursively resolves
  every sub-BOM down to pure raw materials, multiplying quantities through
  each level, and merges the same leaf SKU reached by more than one path into
  a single line. Nesting is capped by **`manufacturing.max_bom_explosion_depth`**
  (**Settings → Configuration**, default 10) — the same setting doubles as
  the circular-reference guard, so a BOM that references itself (directly or
  through a chain of sub-BOMs) is refused rather than explored forever.
- **Scrap %** per component line issues extra quantity to cover expected
  wastage — a line with `scrap_percent: 5` issues 5% more than the pure
  qty-times-order-quantity requirement.
- **By-products** are a separate list on the BOM (SKU + quantity-per-unit)
  that get posted into stock automatically alongside the parent item when
  a batch completes — a real secondary output, not scrap.
- **Alternate / effective-dated BOMs**: more than one Active BOM can exist
  for the same parent item. The one marked **Default** and whose effective
  date window covers today is what a new production order picks up
  automatically; with no default configured, the most recently created
  Active BOM for that item is used instead — so a tenant with exactly one
  BOM per item behaves exactly as if this feature didn't exist.
- **QC Required** (Yes/No) and **Standard Cost** are both optional fields on
  the BOM that gate/inform later steps — see Quality and Costing below.

## Running a production order

1. **Create the order** (Orders tab) against a BOM, with an order quantity
   and a location.
2. **Issue Material** (`POST /api/v1/manufacturing/issue-material`) explodes
   the BOM (recursively, folding in scrap %) and decrements every resulting
   raw material from stock at the order's location in one call, moving the
   order to **Material Issued**. Refused up front, per SKU, if there isn't
   enough raw material on hand (`MANUFA-0143`) — checked explicitly before
   posting, not left to fail midway through. The exact exploded requirement
   is snapshotted at this moment, which is what makes BOM-change detection
   at completion possible (below).
3. **Work through routing, if one is configured** — see
   [Routing, work centers and operations](#routing-work-centers-and-operations).
4. **Complete the order.** A one-shot **Complete** posts the entire
   remaining quantity; **Partial Completion**
   (`POST /api/v1/manufacturing/partial-complete`) posts a smaller batch and
   leaves the order **In Process** until cumulative completed quantity
   reaches the order quantity. Either way, completing to the *last* unit of
   the order runs two checks the first partial batch does not:
   - **BOM variance** (`MFG-0276`): if the BOM has changed since material was
     issued, closing the order is refused until someone either re-plans it
     or explicitly acknowledges the variance
     (`POST /api/v1/manufacturing/acknowledge-bom-variance`) and proceeds
     anyway.
   - **The QC gate**, if the BOM has QC Required set — see below.

   A completion that would push cumulative completed quantity past the
   order quantity is refused outright (`MANUFA-0145`), and finished goods
   (plus any by-products) post into stock at the order's location the
   moment each batch completes — not held back until the whole order
   closes.

## Quality gate before completion

If a BOM's **QC Required** is Yes, the *last* unit of a production order
cannot complete until a **Quality Inspection** record for that order has
been submitted and Approved through the ordinary maker-checker approval
flow. The result recorded on that inspection is what decides the outcome:

- **Approved with result Pass** clears the gate.
- **Approved with result Fail** blocks completion outright (`MFG-0278`) —
  route the batch to rework or scrap instead of trying again with the same
  inspection.
- **No Approved inspection yet** is a softer, still-pending block
  (`MANUFA-0148`) — submit one.

This is a different, narrower thing than Stage 37.9's separate Quality &
Maintenance module (inspection plans, certificates of analysis, NCR/CAPA,
preventive maintenance) — that's a broader QMS concern with its own scope;
this gate is specifically "did this production order's own batch pass," one
Quality Inspection record at a time.

## Routing, work centers and operations

A **Routing** (Setup) is an ordered list of operations, each naming a
**Work Center**, a setup time and a run-time-per-unit. Attaching a routing
to a production order (**Routing** field) is optional — an order with none
skips straight from material issue to completion with no operation-by-
operation tracking.

**Confirm Operation** (`POST /api/v1/manufacturing/confirm-operation`) marks
one routing step done, in sequence — confirming operation 3 before
operation 2 is refused (`MANUFA-0147`). Confirming any operation moves the
order to **In Process**. If a work center has a configured daily capacity
(**Capacity Hours/Day**) and this single order's run time for that operation
alone would exceed it for one day, confirmation still succeeds but comes
back with a capacity warning (`MFG-0277`) rather than blocking — informational,
not a hard stop.

## Scrap and rework

**Post Scrap** (`POST /api/v1/manufacturing/scrap`) logs a written-off
quantity against an order with a required reason (`MANUFA-0146` if omitted)
— audit-only, since the material was already decremented at issue time (or
was never producible), so there's nothing further to remove from stock here.

**Send to Rework** (`POST /api/v1/manufacturing/rework`) similarly just logs
a defective quantity and a reason against the order's running rework total
— it records the event rather than modelling a full re-issue/rework
sub-order loop, which is a deliberate scope line, not an oversight.

## MRP: what to build next

**MRP tab** (`GET /api/v1/manufacturing/mrp-suggestions`, a location + lead
time + safety stock) reuses the same reorder-point formula the inventory
replenishment suggestions use — recent sales velocity × lead time, plus
safety stock — but applied to a manufactured item's *finished-good* stock
instead of a purchased item's. For every item with an Active BOM whose
available stock has fallen under its reorder point, it suggests a production
quantity, then explodes that suggested quantity through the item's active
BOM to show **which raw materials would fall short** if you actually built
that much — so a planner sees the purchasing consequence of a production
suggestion in the same screen, not as a separate lookup. Like every other
suggestion engine in this system, it's read-only: nothing is created until
someone acts on it.

## Production scheduling: finite vs. infinite

**Schedule tab** (`GET /api/v1/manufacturing/production-schedule`) sequences
every open, routed production order's operations against each work center's
configured capacity, and shows two dates side by side for every operation:

- **Infinite** — where the operation would land if capacity were unlimited:
  its order's own due date (or today, if none is set), no queuing.
- **Finite** — where it actually lands once work-center capacity is
  respected: a simple greedy walk forward, earliest-due-order-first, to the
  first day that work center has room left.

An operation whose finite date lands past its order's own due date is
flagged **Overflow** — the visible sign that this work center's capacity is
the thing actually constraining your promise dates, not a plan someone
picked out of the air. Also available as the **Production Capacity
Schedule** report. This is a suggestion, the same as MRP above — nothing is
auto-scheduled or auto-created from it.

## Subcontracting (outside processing)

A **SubcontractOrder** models a step someone outside the business does for
you: **Send** (`POST /api/v1/manufacturing/subcontract-order/send`) moves
the named raw/semi-finished item out of stock at the order's location;
**Receive** (`.../receive`) moves the processed/finished item back in once
it returns, with the actual received quantity recorded separately from what
was sent. Both post through the same inventory-ledger path every other
stock movement uses, tagged to the SubcontractOrder, so they show up in the
Stock Ledger report like any other movement. Send only works from Draft;
Receive only works from Sent — trying either out of order is refused
(`MANUFA-0142`).

## Costing

**Record Actual Cost** (`POST /api/v1/manufacturing/record-actual-cost`,
Completed orders only) logs the real total cost incurred for an order —
typically summed by hand from GRN and labour postings elsewhere, since this
system deliberately does not do full material+labour+overhead absorption
costing. What it *does* do is compare that number against the BOM's
**Standard Cost** × order quantity: the **Production Cost Variance** report
lists every costed order's variance in both amount and percentage, flagging
(`MFG-0279`) any order whose variance exceeds
**`manufacturing.production_cost_variance_tolerance_percent`**
(**Settings → Configuration**, default 10%) for review.

Also available: the **Production Order Status** report, a flat list of every
order's BOM, finished item, quantity, status and date.

## Error codes reference

| Code | Meaning |
|---|---|
| `MANUFA-0140` | The BOM referenced doesn't exist. |
| `MANUFA-0141` | The BOM exists but isn't Active. |
| `MANUFA-0142` | The routing (or subcontract order) referenced doesn't exist, isn't Active, or is in the wrong status for the action attempted. |
| `MANUFA-0143` | Not enough raw material on hand to issue for this order. |
| `MANUFA-0144` | The production order has already been released (material already issued). |
| `MANUFA-0145` | Completing this quantity would exceed the order's total quantity. |
| `MANUFA-0146` | A reason is required to post a scrap quantity. |
| `MANUFA-0147` | Operations must be confirmed in sequence. |
| `MANUFA-0148` | A Quality Inspection must be submitted and Approved (result Pass) before this order can complete. |
| `MFG-0276` | The BOM changed after this order's material was issued — re-plan or acknowledge the variance. |
| `MFG-0277` | (Warning) This operation exceeds the work center's configured daily capacity — informational, not blocking. |
| `MFG-0278` | Quality Inspection result was Fail — this batch cannot be released to stock. |
| `MFG-0279` | (Warning) Actual cost variance against standard cost exceeds the configured tolerance. |

## Troubleshooting

**"Raw material stock is insufficient" when issuing material.** Check
available stock for the flagged SKU at the order's own location — receive
or transfer more in before issuing, or reduce the order quantity.

**Completion is refused over a BOM change.** Someone edited the BOM after
this order's material was already issued. Either re-plan the order against
the current BOM, or acknowledge the variance to proceed with what was
actually issued.

**Completion is refused pending quality approval, even though I inspected
it.** The Quality Inspection record needs to be **Approved** (not just
submitted) through the approval flow, with **result = Pass**, before the
last unit of an order can complete.

**An operation won't confirm.** Operations confirm strictly in sequence —
confirm every earlier operation on the routing first.

**MRP suggests building something with no raw-material shortfall shown.**
That's the expected shape when you already hold enough raw material — the
shortfall list only ever lists what's actually short, not every component.

## What is not here yet

**`GET /api/v1/manufacturing/active-bom`** (resolves which BOM a new order
for an item should default to) is a real, working endpoint with no caller
anywhere in `public/app.js`, confirmed by grep — the Orders tab's quick BOM
form has you name a BOM directly rather than looking one up this way.

**Rework has no re-issue loop.** Sending a quantity to rework logs the event
and a running total; it does not spin up a second production order or
re-issue material automatically — that stays a manual follow-up today.

**Costing is actual-vs-standard only**, not full absorption costing —
there's no automatic roll-up of GRN/labour postings into an order's actual
cost; that total is supplied by hand.