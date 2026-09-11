---
doc_id: KB-TASK-48-004
title: Sort orders into put-wall slots
type: procedure
status: draft
owner: warehouse-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: sortation operator, warehouse supervisor
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 64
summary: Sort orders into put-wall slots with saved-state checks and exception recovery.
screens: [sortation]
topic_type: how-to
module: warehouse
task: Sort orders into put-wall slots
prerequisites: Provisioned Sort Station and slots, fulfillment task, counted goods
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Sort orders into put-wall slots

Use **Sortation / Put-Wall** to assign an order to a slot and record the quantity placed there.

## Assign and fill a slot

1. Enter **Sort Station** and choose **Load Slots**. Verify the physical station and the displayed slot numbers.
2. Enter **Fulfillment Task / Order**. Supply **SKU** and **Qty Expected** where the operation uses them, then choose **Assign to Slot**.
3. Find the assigned slot in the refreshed table. Match its order and SKU to the physical goods before placing anything.
4. Enter the quantity actually placed in that slot and choose **Confirm**. Read **Confirmed / Expected** and the updated status before the next movement.
5. Once a slot is **Filled**, hand it over to packing under the site's procedure, then choose **Clear**. Verify it is available again before assigning another order.

## Resolve an exception

No slots means the station has not been provisioned; ask the supervisor to provision from the SortStation master. If an assignment or confirmation is refused, refresh the slots and check the task, quantities and permissions. A timeout is an unknown outcome: reconcile the confirmed count before retrying so the physical count is not entered twice.

The screen accepts a quantity confirmation; it does not by itself prove each physical item was scanned correctly. Do not clear a slot while goods still belong to its previous order.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
