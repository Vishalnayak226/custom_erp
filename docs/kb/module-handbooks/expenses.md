---
doc_id: KB-TASK-48-007
title: Submit and settle an expense claim
type: procedure
status: draft
owner: finance-process-owner
approvers: [qa-owner, documentation-maintainer]
audience: employee, approving manager, finance operator
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
section: Module Handbooks
order: 67
summary: Submit and settle an expense claim with saved-state checks and exception recovery.
screens: [expenses]
topic_type: how-to
module: finance
task: Submit and settle an expense claim
prerequisites: Employee identity, permitted location, receipt evidence and approved expense policy
failure_behavior: Reload the saved record before retrying an uncertain operation; retain the reference and escalate refusals.
---

# Submit and settle an expense claim

Use **Expenses** to move a claim from Draft through approval, verification and payment.

## Submit a claim

1. Select the **Employee** and **Location**. Enter **Expense Date**, **Category**, **Amount**, **GST Amount**, **Advance Adjusted** and a specific **Purpose** using the supporting receipts and approved policy.
2. Choose **Create Draft**. The system supplies the claim number; retain it and check the saved amounts before submission.
3. Choose **Submit for Approval** on the Draft row. Follow the approval queue for the manager's decision. Do not treat Pending Approval as reimbursed.

## Verify and settle

After manager approval, an authorized finance operator chooses **Finance Verify** and checks the receipt evidence, policy, tax treatment and advance reconciliation. A Verified claim offers **Mark Paid**. Record payment only after following the approved payment process, then verify **Paid** against the settlement record.

The button records a workflow outcome; do not assume it independently proves bank settlement. Retain the claim and payment references for reconciliation. A rejected claim needs the reason addressed through the permitted workflow, not an unlinked duplicate claim.

If a save or transition times out, reload the claim and inspect its status before retrying. If the list is unexpectedly empty, check loading and access before entering another claim. Give support the claim number and error/correlation reference without sharing personal receipt data in ordinary chat.

These steps were reviewed against the current screen source. Customer role, device and business acceptance remain release-review activities.
