---
title: The supplier portal
section: Module Handbooks
order: 32
summary: What a supplier sees when it signs in, and how it tells you a delivery is coming.
audience: procurement officer, admin, supplier
last_verified: 2026-11-14
screens: [supplier-portal]
doc_id: DOC-F31DF81453
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
module: procurement
task: The supplier portal
prerequisites: A user with the Supplier role, linked to its vendor
---

# The supplier portal

A supplier signs in with its own login and sees only its own records, read-only.

## Giving a supplier a login

An administrator opens **Setup » Users**, adds the user with the role
**Supplier** and links it to the vendor. That user opens straight on the
supplier portal.

## What the supplier sees

The portal opens on the supplier's name with four tabs:

| Tab | Shows |
|---|---|
| **Orders** | Approved and closed POs, with the items still open |
| **ASNs** | The delivery notices sent |
| **Bills** | Each bill's match and payment status, amount paid and owed |
| **Payments** | Date, amount and reference of each payment |

A supplier never sees rates, MRP, discounts, totals, charges, internal notes or
approvals. Another supplier's record answers *Not found*.

## Telling you a delivery is coming

1. On **Orders**, click **Send notice** on an open order.
2. Change the quantity if needed. It can never be more than is still open,
   counting earlier notices.
3. Add the expected date, carrier and tracking.
4. Click **Send notice**.

The notice reaches the warehouse as an ordinary ASN with the status
**Expected**.
