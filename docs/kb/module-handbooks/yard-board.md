---
doc_id: KB-TASK-48-002
title: Trailer check-in and yard movement
type: procedure
status: draft
owner: warehouse-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: yard operator, dock supervisor
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 62
summary: Trailer check-in and yard movement with saved-state checks and exception recovery.
screens: [yard-board]
topic_type: how-to
module: warehouse
task: Trailer check-in and yard movement
prerequisites: Trailer identity, permitted yard access and confirmed door assignment
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Trailer check-in and yard movement

Use **Yard Board** when a trailer arrives, moves to a door or departs.

## Receive and move a trailer

1. Match the physical trailer number with the planned arrival. Enter **Trailer No** and, where available, **Carrier**, **Driver** and **Yard Slot**.
2. Choose **Check In** once. Verify the trailer appears with status **InYard** and the correct slot.
3. After the supervisor confirms the door and physical movement, choose **Assign to Door**, enter the dock door and verify **AtDoor**.
4. Choose **Check Out** only after departure is confirmed. The departed record leaves the active board.

## Reconcile the board

An empty board means no active records were returned; it does not replace a physical yard check. On a failed or uncertain save, reload and reconcile the trailer before retrying. Keep its reference and the displayed error for the supervisor if the status disagrees with the yard.

Do not change the trailer identity to get around a duplicate or access refusal. Follow the site's approved traffic and door safety procedure before moving equipment. [Dock appointments](appointment-calendar.md) plan the slot; [loading](loading-dock.md) records cartons and departure of a load.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
