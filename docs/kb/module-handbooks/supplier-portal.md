---
title: The supplier portal
section: Module Handbooks
order: 32
summary: What a supplier sees when it signs in, and how it tells you a delivery is coming.
audience: procurement officer, admin, supplier
last_verified: 2026-11-14
screens: [supplier-portal]
screen_sections: [supplier-portal=what-the-supplier-sees, supplier-portal=telling-you-a-delivery-is-coming]
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

> [!WHAT]
> The supplier portal is the screen a supplier's own login opens on: its
> orders, delivery notices, bills and payments, read-only.

> [!WHY]
> Suppliers stop calling to ask what is still open, whether a bill matched or
> when it was paid, and they see none of your rates, MRP or internal notes.

> [!USECASE]
> A mill wants to know whether last month's bill was paid. It signs in, opens
> **Bills** and sees it matched and paid, with the reference under
> **Payments**.

> [!HOW]

1. The supplier signs in; the portal opens on their name.
2. **Orders** shows their open POs and **ASNs** the delivery notices sent.
3. **Bills** shows each bill's match and payment status, and **Payments** each
   payment.

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

> [!WHAT]
> A delivery notice (ASN) tells your warehouse what is on its way, against
> which order, and when.

> [!WHY]
> The warehouse sees it as an **Expected** ASN, so it can plan the dock, and
> the goods receipt loads its lines with **Load Items from ASN**.

> [!USECASE]
> The mill dispatches 150 of the 300 sarees ordered. It sends a notice for 150
> with the carrier and tracking number, and the receiver later loads those
> lines from the ASN.

> [!HOW]

1. On **Orders**, click **Send notice** on an open order.
2. Change the quantity if needed. It can never be more than is still open,
   counting earlier notices.
3. Add the expected date, carrier and tracking.
4. Click **Send notice**.

The notice reaches the warehouse as an ordinary ASN with the status
**Expected**.
