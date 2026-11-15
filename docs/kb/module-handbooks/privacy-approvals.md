---
title: Privacy approvals
section: Module Handbooks
order: 41
summary: Approve or send back an incident notification decision you were named on.
audience: admin, privacy approver
last_verified: 2026-11-14
screens: [privacy-approvals]
doc_id: DOC-6CF3130465
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2027-02-12
supersedes: none
superseded_by: none
verification_scope: screen labels and rules checked against the build on 2026-11-14
topic_type: how-to
module: security-approvals
task: Privacy approvals
prerequisites: Named as the approver of an incident notification decision
---

# Privacy approvals

When a security incident may need people told, someone decides **Notify** or
**Do not notify**. A second person, the approver named on that decision,
checks it here.

## Deciding

1. Open **Settings » Monitoring » Privacy approvals**. The list shows only the
   decisions waiting for you.
2. Read each one: the incident, who may need telling, the decision, who made
   it, the evidence reference and the reason.
3. Click **Approve** to close it for good, or **Send back** with a note of at
   least 5 characters saying why.

You cannot approve a decision you made yourself. If the decision changed after
you opened the list, the screen asks you to reload and read it again.

## Who can be the approver

An admin, or a user with the **Privacy Approver** role. That role reaches this
one list and nothing else: no records, settings or other screens. An approver
who is switched off, or loses the role, can no longer approve a decision they
were named on.

To give someone the role, an admin creates it under **Settings » Roles** with
the exact name **Privacy Approver**, then sets it on the user under
**Settings » Users**. See the admin guide, *A Privacy approver*.
