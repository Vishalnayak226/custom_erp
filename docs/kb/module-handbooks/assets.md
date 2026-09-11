---
doc_id: KB-TASK-48-006
title: Register and manage fixed assets
type: procedure
status: draft
owner: finance-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: finance operator, asset custodian
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 66
summary: Register and manage fixed assets with saved-state checks and exception recovery.
screens: [assets]
topic_type: how-to
module: finance
task: Register and manage fixed assets
prerequisites: Approved acquisition evidence, location, useful life, cost and authorized asset access
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Register and manage fixed assets

Use **Fixed Assets** to maintain asset records and review the calculated straight-line depreciation and net block.

## Create and capitalize

1. Under **New Asset (Draft)** enter **Asset Number**, **Category**, **Cost**, **Useful Life (yrs)**, **Location**, **Custodian** and **Acquisition Date**.
2. Choose **Create** and verify the saved row is **Draft**. Creation alone does not capitalize the asset.
3. Reconcile the acquisition evidence, cost, location and useful life with the finance owner. For a valid Draft, choose **Capitalise** and verify **Capitalised**.
4. Review accumulated depreciation and net block alongside the approved accounting policy. A calculated figure alone is not a signed accounting or tax determination.

## Transfer or dispose

For a Capitalised asset, **Transfer** prompts for the target information and **Dispose** prompts for the disposal information. Complete these only after the authorized physical and financial decision. Review the resulting location, custodian or Disposed state; preserve the supporting evidence in the approved record system.

If loading fails or a save is uncertain, reload before creating another asset. An empty register does not prove a record is absent if access or loading failed. Escalate discrepancies with the asset number and error reference; do not reuse an asset number to force another record.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
