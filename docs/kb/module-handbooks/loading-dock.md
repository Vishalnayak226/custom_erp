---
doc_id: KB-TASK-48-005
title: Load cartons and record departure
type: procedure
status: draft
owner: warehouse-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: loading operator, dock supervisor
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 65
summary: Load cartons and record departure with saved-state checks and exception recovery.
screens: [loading-dock]
topic_type: how-to
module: warehouse
task: Load cartons and record departure
prerequisites: Permitted dock door and trailer, packed cartons, manifest where used, printer for the bill of lading
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Load cartons and record departure

Use **Loading** after packing and staging are complete.

## Open and load

1. To resume, enter **Existing Loading Task ID** and choose **Open**. Otherwise enter **Dock Door**, **Trailer**, optional **Manifest ID** and **Expected Cartons**, then choose **Open New Load**.
2. Retain the load ID. Confirm the displayed door, trailer and status against the physical trailer before scanning.
3. In **Scan Package / Carton**, scan or type one package code and press Enter. Verify the package row and scanned count after each accepted scan.
4. After all intended cartons are reconciled, choose **Complete Load**. The server runs its completion checks; success changes the status to **Loaded**.
5. Enter **Pallets Out** and **Pallets In** from the actual exchange, then choose **Record Exchange & Depart** at departure. Verify **Departed**.
6. Use **Print Bill of Lading** in Loaded or Departed state. Check the load, trailer, packages and totals before handing over the printout.

## Resolve an exception

Do not scan a substitute package to bypass a refused carton, or depart a load whose completion check failed. Reload the existing load and reconcile cartons after a timeout; do not open a duplicate load to continue. Escalate the load/package reference and displayed error to the supervisor.

A printed bill is a projection of the saved load and is not proof of physical delivery. Follow the approved carrier and site handoff process. [Yard Board](yard-board.md) tracks the trailer separately.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
