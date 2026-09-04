---
title: Product Groups
section: Module Handbooks
order: 55
summary: Group products once - by hand or by rule - then bulk edit, export and report against that group.
audience: category manager
last_verified: 2026-08-12
screens: [pim, doctype-table]
---

# Product Groups

A Product Group is a named set of products you can act on as a unit. It replaces
the pattern of filtering a list, selecting rows by hand, and doing it all again
next week.

## The two kinds

**Static** - you pick the products by hand. The group holds exactly what you put
in it, and changes only when you edit it. Use this for a campaign, a range, a
one-off cleanup list.

**Dynamic** - you save a filter, and the group is whatever currently matches. It
is re-evaluated every time it is used, so a product joins or leaves the moment
its data changes. Use this for standing quality work: "everything in Footwear
below 80% complete", "everything missing a care label".

A group is one or the other. A static group with filters, or a dynamic group
with hand-picked members, is rejected on save.

## Creating one

1. Open **PIM » Product Group** and create a record.
2. Choose **Static** or **Dynamic**.
3. For a static group, add Item rows. Each must be a real Item, and no product
   may appear twice.
4. For a dynamic group, set at least one filter:

| Filter | Matches |
|---|---|
| Family | Products in one product family |
| Completeness below | Products scoring under a threshold, 0-100 |
| Missing attribute | Products with no value for a named attribute |
| Status | Active or Inactive products |

Filters are combined with AND. There is deliberately no query language here -
the four typed filters cover the standing work, and a scripting surface would be
a much bigger thing to secure and support.

## Using one

Open **Setup » Item** and choose **Group Actions**. Pick the group; the dialog
tells you how many products it currently resolves to.

**Bulk edit the group.** Choose a field and a new value. Every guard that
applies to a hand-picked selection applies here too: the maximum-documents cap,
per-document validation, variant uniqueness, and re-approval of approved records
whose doctype is approval-gated.

> [!WARNING]
> A dynamic group is re-resolved by the server at the moment you confirm, not
> when the dialog opened. If the catalog changed in between, the edit applies to
> what matches **then**. The count in the dialog is a preview, not a promise.

**Export the group.** Downloads one CSV row per product with the group's live
completeness score, its missing fields, and the same title/description/tags the
search feed publishes. This is the file to hand an agency or a translator.

**Report on the group.** The registered report **PIM Product Group Readiness**
takes a group id and lists its members with their readiness. Schedule it like
any other report to get a standing view of a dynamic group's health.

## What is not here yet

Assigning a group as work to a person is part of the PIM task engine, which does
not exist yet. Today a group drives bulk edits, exports and reporting.
