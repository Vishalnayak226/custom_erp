---
title: A day as a warehouse operator
section: Role Journeys
order: 4
summary: Receive, put away, pick, pack, dispatch - and the counting work that keeps the numbers honest.
audience: warehouse operator
last_verified: 2026-08-17
screens: [grn, asn, putaway, fulfillment, wave-picking, mobile-picking, cycle-count, lpn, bin-replenishment, bin-conditions, transfers, stickers]
---

# A day as a warehouse operator

Goods come in, get put somewhere, get taken out again, and leave. Each of those
has a screen, and the discipline that matters is doing them in that order rather
than in the order the paperwork arrives.

## Inbound

**Know what is coming.** **Procurement » ASN** holds advance shipment notices -
what a supplier says they have dispatched. An ASN is not stock; it is a promise
you can plan a dock around.

**Receive it.** **Procurement » Goods Receipt**. Use **Load Items from PO** so
the lines come from the order rather than being typed, then correct the
quantities to what physically arrived. **Post Receipt**.

> [!IMPORTANT]
> Posting the receipt is what creates stock. A delivery sitting on the dock with
> no posted GRN does not exist as far as every other screen is concerned - and
> that is the single most common cause of "the system says we have none".

**Put it away.** **Stock » Putaway** turns received stock into stock in a
specific bin. Until then it is in the system but not in a place, and picking
cannot route anyone to it.

## Where things live

**Stock » Bin** is the map. **Stock » Bin Conditions** records the state a bin
is in - damaged, quarantined, blocked - so that stock which exists but must not
be sold is visibly separated rather than quietly wrong.

**Stock » LPN / Cartons / Pallets** tracks handling units: a carton or pallet
identified as one thing, so a whole unit can be moved, received or shipped
without scanning every item inside it.

**Stock » Bin Replenishment** moves stock from bulk storage into pick faces
before a picker finds the face empty. Working the replenishment list first thing
is what stops the pick queue stalling mid-morning.

## Outbound

**Fulfillment** is the main work queue: pick, pack, dispatch, each step
recording who did it and when. Orders flagged **Expedite** sort to the top, and
that is honoured by the queue itself.

Two alternatives for higher volume:

- **Stock » Wave / Batch Picking** groups many orders into one pass through the
  warehouse, so a picker walks the route once rather than once per order.
- **Stock » Mobile Picking** is the handheld view of the same work, for scanning
  as you go rather than working from a printed list.

**Stock » Sticker Printing** produces the labels - shelf, item or shipping - and
is worth doing in a batch rather than one at a time.

## Counting

**Stock » Cycle Count** is a rolling count of part of the warehouse rather than
a full annual stop. Count what the screen asks for, enter what you actually
found, and let the variance be what it is.

> [!WARNING]
> Do not "correct" a count to match the system. The variance is the only signal
> anyone has that something upstream is wrong, and a count adjusted to agree
> with the record destroys the evidence and keeps the error.

## Moving stock between locations

**Stock » Stock Transfer**. A transfer is two half-events - out of one place and
into another - and stock in transit belongs to neither until the receiving end
confirms it. Confirm arrivals the day they arrive.

## When picking cannot find the stock

In order of likelihood:

1. **The receipt was never posted.** Check **Goods Receipt** for an unposted
   record against the PO.
2. **It was received but never put away**, so it has no bin to be picked from.
3. **The bin is in a condition that blocks it** - check **Bin Conditions**.
4. **It is reserved for another order.** The available-to-sell figure in
   **Inventory** already accounts for this; a raw count will not.
5. **It genuinely is not there**, which a cycle count will confirm.

## Next

- [A day as a store manager](journey-store-manager.md) - who releases the orders
  you pick.
- [Your first order, end to end](first-order.md) - the whole path an order takes
  through your queues.
