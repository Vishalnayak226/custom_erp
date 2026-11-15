---
title: Purchase contracts and service entries
section: Module Handbooks
order: 31
summary: Buy against a vendor contract's rates and cap, and pay for services only for work you accepted.
audience: procurement officer, store manager
last_verified: 2026-11-14
screens: [purchase-contracts, service-entry]
doc_id: DOC-AA067F25BD
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
task: Purchase contracts and service entries
prerequisites: Signed-in account with procurement access; an approver for Activate and Accept
---

# Purchase contracts and service entries

## A contract with a vendor

A contract fixes a vendor's rates for a period, with an optional cap on what you
may buy under it.

1. Open **Procurement » Contracts** and click **New contract**.
2. Pick the **Vendor** and the **Kind**, set **Valid from** and **Valid to**, and
   optionally a **Store** and a **Cap value**.
3. Click **Add item** for each item and give its rate (and an optional cap
   quantity).
4. Click **Save**. The contract is a **Draft**.
5. A purchase-order approver opens it and clicks **Activate**. The terms are now
   locked.

## Buying under it

1. Open an **Active** contract and click **Release PO**. The new PO opens with the
   vendor, store and contract filled in.
2. Add lines. Leave the rate blank to take the contract rate.
3. Submit the PO for approval as usual.

The contract refuses a rate above its own, an item not on it, a PO dated outside
its validity, another vendor's PO, and a release that would pass the cap.

The contract shows **Released ... of ...** against its cap, and lists each
release PO. A release counts once it is submitted; a cancelled one gives its
value back. **Close** ends a contract early.

## Paying for a service

1. On **New purchase order**, set **Type** to **Service**. A service PO is never
   received on a goods receipt, and no stock moves.
2. When work is done, open **Procurement » Service Entry**, click **New service
   entry**, pick the service PO and enter the quantity done on each line. You
   cannot enter more than was ordered, less what is already accepted.
3. Click **Save**. An approver opens the entry and clicks **Accept**. The cost is
   accrued once; clicking **Accept** again changes nothing.
4. On the vendor's bill, set the **PO** and the **Service entry**. The bill is
   matched against the accepted work. A bill for more than that is held.

> [!TIP]
> On the PO list, a service PO's **Receive goods** button reads **Service
> entry** and opens this screen.
