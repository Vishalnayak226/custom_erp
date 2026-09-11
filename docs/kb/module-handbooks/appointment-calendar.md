---
doc_id: KB-TASK-48-001
title: Dock appointments
type: procedure
status: draft
owner: warehouse-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: dock planner, warehouse supervisor
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 61
summary: Dock appointments with saved-state checks and exception recovery.
screens: [appointment-calendar]
topic_type: how-to
module: warehouse
task: Dock appointments
prerequisites: Active dock door, service window, carrier and trailer reference
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Dock appointments

Use **Appointment Calendar** to schedule inbound or outbound work at a dock door.

## Schedule an appointment

1. Select the date. Use the arrow buttons to move by a day, or **Switch to Week** to review the week by door.
2. Choose **New Appointment**. Enter **Dock Door**, **Type**, **Carrier**, **Trailer No**, **Date**, **Start (HH:MM)** and **End (HH:MM)**.
3. Check the physical door, operating window and planned duration with the dock supervisor. Choose **Save**.
4. Verify that the appointment appears against the intended door and date. Keep its reference with the inbound or outbound work order.

## Resolve an exception

If no active doors appear, have the administrator configure **Dock Doors** before scheduling. A refused capacity or service-window check needs a valid door or time; do not invent a second door record to bypass it. If Save times out, reload the calendar and look for the appointment before submitting again.

The calendar is a plan. Physical arrival and movement belong on the [Yard Board](yard-board.md); a saved appointment alone is not a check-in or goods receipt.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
