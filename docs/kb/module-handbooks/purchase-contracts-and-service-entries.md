---
title: Purchase contracts and service entries
section: Module Handbooks
order: 31
summary: Buy against a vendor contract's rates and cap, and pay for services only for work you accepted.
audience: procurement officer, store manager
last_verified: 2026-11-14
screens: [purchase-contracts, service-entry]
screen_sections: [purchase-contracts=a-contract-with-a-vendor, purchase-contracts=buying-under-it, service-entry=paying-for-a-service]
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

> [!WHAT]
> A purchase contract fixes one vendor's rates for a period, with an optional
> cap on what you may buy under it.

> [!WHY]
> Every PO released under it is held to the deal: no rate above the
> contract's, no item off it, no date outside it and no release past the cap.
> The negotiated terms are enforced on each order instead of remembered.

> [!USECASE]
> You agree a year's rate for packing tape with one supplier: ₹38 a roll, up
> to ₹5,00,000. Each month's order is released from the contract, the rate
> fills itself in, and the contract shows how much of the cap is used.

![A new contract: vendor, kind, validity and cap, then each item's rate](../img/contract-new.webp)

> [!HOW]

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

> [!WHAT]
> A service entry records work a vendor has done against a service PO: a
> repair, a maintenance visit, a consultant's days.

> [!WHY]
> A service has no goods to receive, so the service entry stands in for the
> goods receipt. The cost is accrued once, when an approver accepts the work,
> and the vendor's bill is matched against accepted work, not against the PO.

> [!USECASE]
> You order 12 monthly AC services. After the March visit the store enters 1
> service done and the manager accepts it. The vendor's March bill matches the
> one accepted visit; a bill for two would be held.

![A new service entry: pick the service PO, then key what was done this time](../img/service-entry-new.webp)

> [!HOW]

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
