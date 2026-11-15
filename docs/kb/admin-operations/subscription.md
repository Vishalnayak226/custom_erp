---
title: Your Subscription
section: Admin & Operations
order: 30
summary: See your plan, when the paid period ends, this month's use against what the plan includes, and the GST invoices for each payment.
audience: admin
last_verified: 2026-11-15
screens: [subscription]
doc_id: DOC-98833D1E8A
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-15
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: how-to
module: operations
task: Your Subscription
prerequisites: Signed-in Admin of the client
---

# Your Subscription

**Settings > Customize > Subscription** (short code `SUBS`) shows a client's
own plan. Only an Admin sees it. Nothing on this screen changes anything: plans
are chosen and paid with the platform owner.

## What the screen shows

- **Status**, in plain words: for example *Paid up to 30 Nov 2026*, a failed
  payment with the day everything keeps working until, *Cancelled* with the
  last day of access, or paused, stopped or ended. Your data is kept in every
  case.
- **Plan** and **Period ends**: the paid period runs to that date. A cancelled
  subscription keeps working until then.
- **Grace until** appears only after a failed payment. Everything keeps working
  for 7 days; after that the client is paused until the payment goes through,
  and paying resumes it.
- **This month**: documents, active users and storage, each against what the
  plan includes. Months follow Indian time. **Over** shows the amount for use
  above the plan where the plan prices it; a meter with no price stops at the
  limit instead (message `SAAS-0193`).
- **Invoices**: one GST tax invoice for each payment, with the taxable amount,
  CGST and SGST (within the supplier's State) or IGST (across States), and the
  total. An invoice still being numbered shows **Pending**.

## When something looks wrong

- *Only an Admin can see the subscription*: sign in as the client's Admin.
- *No subscription*: the client has no plan with the platform yet. Ask the
  platform owner.
- A module you paid for is missing, or one you dropped is still there: ask the
  platform owner to reconcile the plan. No records are deleted when a module is
  switched off; switching it back on brings everything back.

The platform owner's side (plans, payments, reconcile, invoice settings) is in
the admin guide, *Packages and the SaaS subscription*.
