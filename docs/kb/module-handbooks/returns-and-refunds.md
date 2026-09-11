---
doc_id: KB-RET-001
title: Returns and refunds
type: procedure
status: draft
owner: returns-process-owner
approvers: [finance-owner, qa-owner]
audience: cashier, store manager, returns operator
applies_to: source release 0.1.0; Stage 47.4 return workflow
authority: canonical
confidentiality: internal
last_verified: 2026-09-08
review_by: 2026-10-08
supersedes: none
superseded_by: none
section: Module Handbooks
order: 15
summary: Look up a bill, raise a return, inspect the goods, and progress an approved refund without entering a sale price.
screens: [returns]
topic_type: how-to
module: returns
task: Return sold goods and reconcile the refund
prerequisites: Original bill, permitted return location, return and refund permissions, physical goods for inspection
failure_behavior: Refresh the return before retrying; retain its reference and escalate refusals without changing the original sale.
---

# Returns and refunds

Use **POS / Billing → Process a Return** to raise the request, then open
**Returns** to progress it. A request alone does not mean stock has been
received or money has been paid. The screen shows the current state and the
actions available for it.

These instructions were checked against the Stage 47.4 source. Customer role,
device and end-to-end acceptance remain part of the release review.

## Before you begin

Have the original bill number, the goods and the receiving location ready.
Your administrator must grant the relevant return/refund permissions and
location scope. Use your own account. A missing action is a reason to ask the
administrator to check access; it is not a reason to borrow another login.

## Raise the return at the till

1. In **Process a Return**, enter **Original Bill / Cart Number** and choose
   **Look Up Bill**.
2. Read **Sold**, **Already Returned** and **Still Returnable** for each line.
   The price comes from the original bill. You cannot replace it with today's
   price or type a different refund amount.
3. Choose **Return Location** and enter **Return Qty** only for the goods being
   returned. The request must meet the server's remaining-quantity and return
   eligibility checks.
4. Choose **Raise Return** and retain the return reference. **Refund if all
   accepted** is an estimate; inspection determines eligibility.

If the bill cannot be resolved, stop and have the original transaction checked.
Do not create a substitute sale, edit the original bill or retry with another
customer's reference to get past a refusal.

## Progress the request in Returns

Use **Show** to filter the queue and **Refresh** to retrieve its current state.
Match the original bill, location and items before taking an action.

| Current state | Action | Expected evidence |
|---|---|---|
| Requested | Approve, or reject with a reason | Updated request state and decision |
| Approved | Book reverse pickup where configured; receive after the goods arrive | Pickup reference where applicable, then Received state |
| Received | Inspect each SKU and choose its disposition | Recorded dispositions and refund-eligible total |
| QC Complete | Progress the refund | Refund reference, approval decision and payment method |
| Closed or Rejected | Review the record | Completed outcome; no further workflow action is offered |

At inspection, choose **Sellable**, **Damaged**, **Repairable**, **Missing**,
**Wrong-Item** or **Rejected** for each line. The current inspection dialog uses
one disposition per SKU line; it cannot split that line across conditions.
An exchange SKU can be requested in the inspection dialog. Availability and
eligibility are checked by the server; the dialog is not proof that an exchange
has been fulfilled.

## Approve and process the refund

The refund flow looks up the request's pending refund. If it needs approval,
confirm the amount first. Then record the payment method when prompted.
Permission and workflow checks still apply to each step. Do not treat the
appearance of a confirmation dialog as authorization to pay.

After processing, refresh and retain both return and refund references. Match
the amount and method to the actual cash movement or provider evidence and the
finance records. The application recording a method does not itself prove an
external card or UPI provider settled the payment.

## When something goes wrong

- **No refund is pending:** refresh and check whether inspection is complete,
  whether any line is refundable, or whether the refund was already processed.
- **Timeout or uncertain response:** refresh and look for the existing return
  or refund before creating another request. Keep the original reference.
- **Wrong state, scope or quantity:** read the refusal, verify the bill and
  location, then ask the responsible manager to resolve the cause.
- **Unexpected amount or stock condition:** stop payment/dispatch for that
  case and give support the return ID, refund ID, timestamp and correlation ID
  shown by the error. Do not include passwords or session tokens.

The former instant return endpoint has been retired. Do not use old scripts or
old instructions that promise an immediate refund and restock from that path.
For general error handling, see [Reading an error code](error-codes.md).
