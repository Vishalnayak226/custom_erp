---
doc_id: KB-WMS-002
title: RF lot and serial tasks
type: procedure
status: draft
owner: warehouse-process-owner
approvers: [qa-owner]
audience: warehouse operator, supervisor
applies_to: source release 0.1.0; Stage 47.6 task shell
authority: canonical
confidentiality: internal
last_verified: 2026-09-08
review_by: 2026-10-08
supersedes: none
superseded_by: none
section: Module Handbooks
order: 26
summary: Put a lot away, issue it, update a serial or run an expiry sweep from the floor task screen.
screens: [rf-traceability]
topic_type: how-to
module: wms
task: Record a lot or serial movement
prerequisites: Permitted warehouse access, tracked item, matching bin and lot or serial
failure_behavior: Stop on refusal, verify the last outcome before retrying, and request supervisor assistance without sharing credentials.
---

# RF lot and serial tasks

Open **WMS → RF Lot & Serial** and choose the task you are performing. Read the
instruction at the top before scanning. The screen keeps a primary scan target
and shows outcome text alongside its sound, vibration and colour feedback.
Availability of sound/vibration depends on the browser and device.

These steps were checked against the task shell source. A physical scanner,
mobile browser or poor-network workflow is not certified by that source check;
use the device configuration approved for your site.

## Choose and complete a task

| Task | Enter or scan | Check before submitting |
|---|---|---|
| Put a lot away | Bin, Item, Lot / Batch, Quantity, Condition | Goods, label, quantity and condition match the destination |
| Issue a lot | Bin, Item, Lot / Batch, Quantity; optional document and customer | The physical lot and issue reference match the authorized movement |
| Change a serial | Item, Serial number, New status; reason when scrapping | The exact unit and intended status are correct |
| Sweep expired lots | Confirm the sweep when prompted | You are authorized to quarantine expired lots in this tenant's operation |

The serial choices are **In Stock**, **Allocated**, **Shipped**, **Returned** and
**Scrapped**. A status update alone is not proof that the shipment, order or
refund process is complete. Reconcile the relevant operational record too.

Read the outcome before moving to the next item. Match the physical bin and
stock to the recorded movement. A refused command is not a successful movement;
do not continue simply because the scanner beeped.

## Recover from a refusal or uncertain result

If an input was misread, correct the bin/item/lot/serial before retrying. For a
permission or workflow refusal, use the supervisor-assistance path and keep the
task reference and error details. Never borrow a supervisor's login.

If connectivity drops after submission, check the recorded state before sending
the movement again. Do not assume every RF action can be replayed safely merely
because checkout supports idempotency. For a repeated or uncertain stock change,
stop the physical movement and have the supervisor reconcile it.

For the inventory model, FEFO constraints and remaining integration limitations,
see [Batch, Serial & Expiry Traceability](traceability-batch-serial.md).
