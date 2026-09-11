---
doc_id: KB-TASK-48-003
title: Place and release an inventory hold
type: procedure
status: draft
owner: inventory-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: inventory operator, store manager
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 63
summary: Place and release an inventory hold with saved-state checks and exception recovery.
screens: [place-hold]
topic_type: how-to
module: inventory
task: Place and release an inventory hold
prerequisites: Configured Hold Code, SKU, authorized location, counted quantity and lot where applicable
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Place and release an inventory hold

Use **Place Hold** to prevent a quantity from being allocated while a quality or operational issue is investigated.

## Place the hold

1. Identify the goods physically and confirm their SKU and location. For a lot-specific issue, retain the **Batch / Lot No**.
2. Select **Hold Code**, **SKU** and **Location** from their suggestions. Enter a positive **Qty**, the batch when applicable and a useful **Reason**.
3. Choose **Place Hold** once. Retain the returned hold reference and reconcile the affected stock before continuing allocation work.
4. Label or segregate the physical goods according to the site's approved procedure. A system hold does not move the goods physically.

## Release through approval

Raise a **Hold Release Request** for the affected hold and follow the Store Manager approval workflow. Preserve the investigation and release reason. Do not remove a hold by editing stock quantities or creating an offsetting inventory transaction.

If the action is refused, review the item, location, available quantity and access scope. If the response is uncertain, inspect the existing hold before retrying. Escalate with the hold reference, error code and correlation ID; do not repeatedly submit the same quantity.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
