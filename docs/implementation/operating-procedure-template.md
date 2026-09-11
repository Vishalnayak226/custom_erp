---
doc_id: DOC-IMPL-002
title: Customer operating procedure template
type: template
status: draft
owner: implementation-owner
approvers: [tenant-owner, process-owner, qa-owner]
audience: [implementation-consultant, tenant-administrator, process-owner]
applies_to: one customer, role, workflow and accepted release configuration
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-08
review_by: 2026-10-08
supersedes: none
superseded_by: none
---

# Customer operating procedure template

Use this blank template to agree how a customer performs a specific task. Keep
product instructions in the canonical KB topic and record the customer's local
responsibilities, checks and exceptions here. Copy the template into the
customer's controlled documentation system; do not put restricted execution
evidence or private contacts in this repository.

## Scope and control

| Required field | Customer value |
|---|---|
| Procedure ID, title and version | To be supplied |
| Customer, tenant and sites | To be supplied |
| Module, task and accountable process owner | To be supplied |
| Release, accepted capability/configuration and device | To be supplied |
| Canonical KB topic and verified source version | To be supplied |
| Normal operator, supervisor and independent approver | To be supplied |
| Effective date, next review and superseded procedure | To be supplied |
| Training/accessibility/language needs | To be supplied |
| Approval record and controlled evidence location | To be supplied |

A filled form is not approved until the responsible customer owner records
acceptance. A tenant-admin procedure must distinguish tenant configuration from
platform deployment, database administration and developer work.

## Task steps and evidence

| Step | Responsible role | Prerequisite / canonical instruction | Local check and retained evidence | Refusal or exception action |
|---|---|---|---|---|
| Before starting | To be supplied | Identity, scope, configuration, source records, device readiness | Approved readiness check | Stop or escalate to named role |
| Perform task | To be supplied | Link the exact KB task/section | Business reference and expected state | Follow the task's recovery instruction |
| Verify outcome | Independent role where required | Reconciliation and approval rule | Stock/money/provider/control totals as applicable | Investigate differences before dependent work |
| Close and hand over | To be supplied | Next task, support and retention requirements | Completion reference and unresolved exceptions | Record owner and due date |

Describe the real evidence a person must check. A success dialog alone is not
proof of provider settlement, physical stock, approval independence or complete
data migration. Do not authorize credential sharing, bypass flags, direct
database edits or an unreviewed retry to work around a refusal.

## Validation and approval record

Record the normal case, permission refusal, bad input, interrupted/uncertain
response, duplicate/retry, exception and reconciliation walkthroughs relevant to
the task. Include the actual operator role and supported device. Record results,
findings, corrective actions and the approved scope; blank checks are pending.

The process owner accepts the workflow; QA verifies the evidence; finance,
security, legal or operations approve their applicable controls. Record any
exception's scope, owner, expiry and compensating procedure. Revalidate on a
material release, configuration, role, device, legal or business-process change.

Use [Returns and refunds](../kb/module-handbooks/returns-and-refunds.md) as an
example of product instructions with explicit outcomes and recovery. The legacy
[User SOP](../guides/USER_SOP.md) and [Admin SOP](../guides/ADMIN_SOP.md) remain
parity-review inputs; this template does not approve them or discard their unique
controls. Track onboarding, UAT and cutover through the
[implementation workbook](onboarding-workbook.md).
