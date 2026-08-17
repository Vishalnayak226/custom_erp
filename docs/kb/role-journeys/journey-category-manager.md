---
title: A day as a category manager
section: Role Journeys
order: 5
summary: Own the product record, get it complete enough to publish, and keep it that way as the range changes.
audience: category manager
last_verified: 2026-08-17
screens: [pim, doctype-table, marketplace, reports, stickers]
---

# A day as a category manager

Your product is the thing every other screen depends on. A sale cannot be priced
without its tax fields, a warehouse cannot label it without its codes, and a
marketplace will not list it without its content. Most of this job is making
sure that record is complete before anyone downstream discovers it is not.

## The two screens you live in

**Setup » Item** is the master record: codes, tax classification, pricing
fields, the identity everything else points at.

**PIM** is the content and readiness layer over it: attributes, families,
completeness, groups, and the feed that goes out to channels.

They are one product seen twice, not two products.

## Getting an item right the first time

The fields that block other people if you leave them out:

| Field | What breaks without it |
|---|---|
| **Item code** | Nothing can reference the product |
| **HSN Code** | The item cannot be saved, and no invoice can classify it |
| **GST Rate** or **Tax Treatment** | The same - a `0` rate alone is rejected, because it is indistinguishable from an unfilled field |
| **Barcode** | The till cannot scan it |
| **Purchase price** | Purchase order lines cannot be priced |

For anything genuinely untaxed - produce, unbranded grain, exports - set **Tax
Treatment** to Exempt, Nil-Rated or Zero-Rated rather than typing a zero rate.
HSN is still required on all of them: it goes on the invoice whatever the rate
is, and the nil and exempt parts of GSTR-1 are reported HSN-wise.

## Working in bulk, not one record at a time

This is the difference between the job taking a morning and taking a week.

**Bulk Import.** Every list screen with create permission has it. Upload a CSV
of line items rather than typing rows.

**Product Groups.** Define a set of products once and then act on it repeatedly.
A **static** group is what you put in it; a **dynamic** group is a saved filter,
re-evaluated every time it is used, so products join and leave as their data
changes. Use dynamic groups for standing work - "everything in Footwear below
80% complete" - and static groups for a campaign or a one-off cleanup.

From **Setup » Item » Group Actions** you can bulk edit a group, export it as a
CSV with each product's live completeness and missing fields, or report on it.
The export is the file to hand an agency or a translator.

Full detail: [Product Groups](pim-product-groups.md).

> [!WARNING]
> A dynamic group is re-resolved by the server at the moment you confirm a bulk
> edit, not when the dialog opened. The count in the dialog is a preview, not a
> promise.

## Completeness, and why it is the metric that matters

A product is not "done" when it saves. It is done when it has everything the
channels you sell on require. The completeness score is what turns that from an
opinion into a queue you can work: sort by it, fix the worst, watch it move.

The registered report **PIM Product Group Readiness** takes a group id and lists
its members with their readiness. Schedule it and you have a standing view of a
dynamic group's health rather than a number someone has to go and look up.

## Publishing outward

**Marketplace** is where the range meets the channels. What you can list is
bounded by what is complete, which is why the two previous sections come first.

Suppliers can propose content themselves: a supplier account reaches only
**Supplier Submissions** and sees only its own company's. Approved text does not
go live automatically - it becomes a draft your side still reviews and publishes
on its own schedule, and a rejection always carries a written reason the
supplier can read.

## The weekly rhythm

| When | What |
|---|---|
| Daily | New products in, checked against the blocking-field table above |
| Daily | Supplier submissions reviewed - a queue that is never cleared stops being used |
| Weekly | Readiness report on your dynamic groups; fix the worst-scoring |
| Weekly | Marketplace listing errors - usually one missing attribute repeated many times |
| Per campaign | A static group, bulk edited, exported for whoever writes the copy |

## Next

- [Product Groups](pim-product-groups.md) - the mechanism in full.
- [Report catalog](report-catalog.md) - everything you can measure this with.
