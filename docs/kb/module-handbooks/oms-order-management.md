---
title: Order Management
section: Module Handbooks
order: 60
summary: Every channel and manual order lands through one Order Engine - validated, allocated across locations by a priority-ordered rule chain, and held with a routable reason rather than ever hard-rejected.
audience: order desk, store manager, admin
last_verified: 2026-09-03
screens: [oms]
---

# Order Management

Every order in this system — a Shopify webhook, a BigCommerce poll, a
manually typed-in phone order — lands through exactly one creation path:
`CreateSalesOrder`. There is no second, parallel order-intake mechanism to
keep in sync; every channel adapter and the manual-entry screen are all thin
callers of the same function, which is what makes the rest of this
module — allocation, holds, mutations, reporting — behave identically no
matter where an order came from.

## The OMS Workbench

One screen (**sidebar → Order Management**), not tabs: dashboard tiles,
channel-connector sync health with unmapped-SKU exceptions, bundle/kit
operations, a global search across order id/channel order id/AWB/phone/
customer/SKU, manual order entry, and the main order queue with saved
views and bulk actions. See [Channel Connectors](channel-connectors.md) for
the connector/credential setup this screen's health panel reads, and
[Point of Sale](pos-operations.md) for the till side of a sale.

## Order creation: the validate chain

Every new order runs through a fixed three-step check, in this exact
order, before anything is reserved:

1. **SKU mapping** — every line's SKU must resolve to an Active item
   (matched by code, barcode, or internal id).
2. **Address** — non-empty and containing something pincode-shaped.
3. **Payment** — must already be Confirmed (the prepaid gate) before stock
   is reserved against it.

**A failing check never hard-rejects the order.** It lands the order
**On Hold** with a routable reason code instead — `SKU_MAPPING_FAILED`,
`ADDR_INVALID`, or `PAYMENT_PENDING` — so a human or a queue can decide
what to do next, rather than the order simply vanishing. These are the
Order Engine's own internal routing codes (stored on the order's
`hold_reason`), deliberately separate from the formal error-code catalog.

**Releasing a hold re-runs the exact same chain** — never a separate
"resume" path that could quietly diverge from what a fresh order would be
checked against. If the order is now clean, it proceeds straight to
allocation (below); if a *different* check now fails, the hold reason
updates to reflect that. Only lines still Pending are touched — a line that
already reserved, shipped, or cancelled before the hold was placed is left
alone.

## Allocation & sourcing

Once an order passes validation, the **Allocation/Sourcing Engine**
(`ResolveAllocationPlan`) decides which location(s) fulfil it, running every
**Active Allocation Rule** scoped to the order's channel (or a blank-channel
global rule) in ascending priority order, trying each rule's strategy until
one produces a usable plan — the first rule to succeed wins:

| Strategy | Picks |
|---|---|
| **Highest ATS** | The single location with the most available-to-sell stock. |
| **Nearest Pincode** | The single location closest to the shipping address. |
| **Lowest Workload** | The single location with the least open work right now. |
| **Oldest Stock** | The single location holding the oldest stock for these SKUs. |
| **Split Shipment** | Different lines fulfilled from different locations, if no one location can cover the whole order. |
| **Manual** | Always fails to produce a plan and stops the search immediately — this means "route to a human," not "skip me if I don't fit." Configure it as the lowest-priority catch-all so richer rules get a real chance first. |

**With no Allocation Rule configured at all**, the system falls back to the
simple pre-rule-engine behaviour (single highest-ATS node, always finds
something) — a tenant that has never touched this master sees identical
behaviour to before the rule engine existed. **Once at least one rule IS
configured**, exhausting every rule with no usable plan holds the order with
reason `ALLOCATION_FAILED` rather than ever falling back silently — the
resulting On Hold queue, filtered to that reason, **is** the allocation
exception queue; there's no separate table for it.

## Holds: three different things share the word "hold"

- **Order-level hold** (`PlaceOrderHold`/`ReleaseOrderHold`) — the whole
  order, either automatic (a failed validate-chain check) or manual (a CS
  agent, which requires an Active reason code in the Hold category).
- **Order-line-level hold** (`HoldOrderLine`/`ReleaseOrderLineHold`) — one
  line within an order, reachable from the same order-detail panel.
- **Inventory/SKU hold** (`PlaceHold`/`ReleaseHold`, a WMS-level concept
  scoped to a SKU+location+optional batch, not tied to any specific order)
  — a different mechanism entirely, reached from its own **Place Hold**
  screen, requiring an Active `HoldCode` and released only through a
  approved `HoldReleaseRequest`, never directly. If you're looking for "why
  can't this SKU be picked at all," this is the one to check, not an order
  hold.

## Editing, switching facility, priority, and splitting

All four are reachable from an order's detail panel in the Workbench:

- **Edit** re-runs the same address/payment checks a new order goes
  through — if the edit leaves the order unfulfillable, it's placed On Hold
  with a reason rather than silently saved broken.
- **Switch Facility** moves an order's not-yet-picked lines to a different
  location — either a location you name directly, or leave it blank to
  re-run the allocation engine and let it find a better node ("Reallocate").
  Lines already Dispatched/Cancelled/Returned are left exactly where they
  are, since the goods have physically moved. The destination is reserved
  **before** the source reservation is released, specifically so there's
  never a window where the stock is unreserved at both ends for a
  concurrent order to take.
- **Priority** flags an order Expedite, honoured by both the console's own
  list ordering and by pick-list generation itself — an expedited order's
  tasks are generated ahead of normal-priority ones.
- **Split** moves selected lines onto a new, separate order.
- **Cancel** requires an Active reason code in the Cancellation category,
  and is blocked once an order reaches Shipped/Delivered/Closed/Cancelled —
  configurable per-tenant via the Status Transition Rule master, falling
  back to that fixed blocklist if no rule has been configured yet.
  Cancelling releases any reservation the cancelled lines were holding.

Bulk hold/release/cancel are also available directly from the order queue,
reporting a per-order outcome rather than an all-or-nothing batch result.

## Manual order entry

A manual order goes through the identical Order Engine every channel import
uses — the same allocation, reservations and holds all apply, there is no
shortcut path. See `USER_GUIDE.md` §9A for the click-through walkthrough;
this article covers the mechanics behind it.

## Returns and refunds

There are genuinely two different return paths in this system, at very
different depths.

The **quick, in-store refund** — same-visit, refund only, no batch/serial
capture — is documented in [Point of Sale](pos-operations.md); it's what a
till actually uses today.

A second, much richer workflow exists **entirely server-side**:
`CreateReturnRequest` → Approve/Reject → `ReceiveReturnRequest` →
`ApplyReturnQC` (per-line disposition, with an optional exchange-for-SKU) →
a resulting `RefundRequest` → Approve/Reject → `ProcessRefundRequest`, plus
`ScheduleReturnReversePickup` for booking a courier pickup of the returned
goods. This is a complete, tested, courier-integrated return/refund/QC
state machine.

> [!WARNING]
> **The entire `ReturnRequest`/`RefundRequest` workflow has no UI anywhere.**
> Confirmed by grep: zero matches for `ReturnRequest`, `RefundRequest`, or
> `/api/v1/returns` anywhere in `public/app.js`. QC disposition, exchange-
> for-SKU, and reverse-pickup booking are all real and reachable only by a
> direct API call today — the same finding [Point of Sale](pos-operations.md)
> already made from the till side of this gap. If a customer needs an
> exchange rather than a refund, today's real workflow is a POS return plus
> a fresh sale for the new item, not this richer single workflow.

## Error codes reference

| Code | Meaning |
|---|---|
| `OMNI-0245` | (Error) An ATS (available-to-sell) mismatch was detected. |
| `OMNI-0246` | (Warning) A stock reservation expired before it was consumed. |
| `OMNI-0247` | No fulfillment node was available for this order. |
| `OMNI-0248` | (Warning) A courier pickup window expired. |

## Reports

12 registered reports under **Reports → OMS**: **Allocation Pending**,
**Order Aging**, **Reserved Stock**, **Return Aging**, **SLA Breach**,
**Courier Performance**, **OMS Exception Queue**, **OMS Reconciliation
Variance**, **Settlement Variance Queue**, **Orphaned Channel Orders**
(a pre-Stage-35.1 legacy-intake cleanup aid). Full parameters/columns in the
[Report Catalog](report-catalog.md).

## Troubleshooting

**An order is stuck On Hold with reason `SKU_MAPPING_FAILED`.** At least
one line's SKU doesn't resolve to an Active item by code, barcode, or
internal id — fix the mapping (or the item's status) and release the hold;
release re-runs the same check.

**An order is stuck On Hold with reason `ALLOCATION_FAILED`.** Every
configured Allocation Rule was tried and none produced a usable plan —
usually genuinely insufficient stock anywhere, or a rule chain that ends in
a strategy too narrow to match. Check stock across locations, or adjust the
rule chain.

**Switch Facility fails partway with "nothing was moved past this point."**
The destination had no available stock for one of the lines being moved —
everything up to that line already moved successfully (reservations are
released at the source only after a successful reserve at the destination),
so nothing is left unreserved; retry once stock is available, or pick a
different target.

**Looking for exchange handling and can't find it.** See the warning
above — today's real workflow is a POS refund plus a new sale, not a
single exchange action.

## What is not here yet

**The full `ReturnRequest`/`RefundRequest` QC-disposition and reverse-
pickup workflow has zero UI** — see the warning above. It is real, tested,
and API-only.