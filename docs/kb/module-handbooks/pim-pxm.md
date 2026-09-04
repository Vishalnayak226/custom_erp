---
title: Product Information Management
section: Module Handbooks
order: 50
summary: Items, categories and attributes with locale/channel-aware values, a 0-100 completeness score every publish gate reads, and an effective-dated price list that exists in full but has no screen yet.
audience: category manager, admin
last_verified: 2026-09-03
screens: [pim, doctype-table, reports, configuration]
---

# Product Information Management

The PIM screen is one shell (**Dashboard**, **Workbench**, **Reports**,
**My Work**, **Media Library**, plus a row of doctype-backed tabs for every
taxonomy and channel-mapping record type) built entirely on the generic
document machinery — every tab that isn't a bespoke screen is a plain
doctype table with create/list/edit/RBAC/audit/CSV-import for free. This
article covers items, categories/attributes and price lists; grouping
products for bulk action is its own article,
[Product Groups](pim-product-groups.md); day-to-day task/workflow handout is
covered in the User Guide's "Handing out PIM work" section and the PIM
dashboard's own **My Work** tab.

## Items

The Item master is the one record every other module in this system reads
from — its **HSN Code** and **GST Rate** are what makes it sellable through
POS/OMS/Procurement, both already covered in
[Point of Sale](pos-operations.md) and [Procurement](procurement.md). This
article's job is what PIM adds on top of that core record: categorisation,
enrichment content, locale/channel-aware attributes, and completeness
scoring.

## Categories and attributes (taxonomy)

Four plain doctypes, all reachable from their own PIM tabs, make up the
taxonomy:

| Doctype | What it is |
|---|---|
| **Product Family** | The category an item belongs to (e.g. "Rings", "Necklaces") — what a completeness score and a family's mandatory-attribute list are both scoped against. |
| **Attribute Definition** | One attribute's definition (e.g. "Polish", "Metal Purity") — a reusable field, not tied to one family. |
| **Attribute Group** | Groups related attributes for display (e.g. "Physical Specifications"). |
| **Family Attribute** | Which attributes are mandatory for which family — this is what a completeness score actually checks against. |

Every field-level change to any of these four is visible via a **History**
row action on its own table — reusing the ordinary audit trail every
document already writes to, not a second version-tracking mechanism.

### Locale and channel-scoped attribute values

An item's actual attribute value (e.g. this specific ring's Polish =
"High") is a separate `ProductAttributeValue` record, and it can be scoped
to a **locale**, a **channel**, both, or neither. Resolving "what is this
item's Polish value" checks, in order: a value scoped to both this locale
*and* this channel, then locale-only, then channel-only, then the global
(unscoped) value — one query, not four round-trips. A caller that doesn't
care about locale/channel scoping (the pre-multi-locale call shape) only
ever sees the global value, so nothing that predates this feature changed
behaviour.

## Completeness scoring

Every item gets a **0-100 completeness score**, scoped to a Family +
Channel + Locale combination, computed as valid-required-values ÷
total-required-values across three sources: the item's own core ERP fields,
its family's mandatory attributes (if a family is set), and — if scored for
a specific channel — that channel's own mandatory field mappings (a field
optional in PIM core can still block one specific channel's readiness). It
also checks whether the item has **Approved** enrichment content for that
locale (`PIM-0231` if content is still Pending Approval, not yet Approved).
The result write-throughs the item's own enrichment status, which is what
drives the Workbench and the readiness reports below — this is the one
number that answers "is this item actually ready to sell/publish," and it's
recomputed live, never cached and forgotten.

A missing Product Family is a hard error (`PIM-0229`) — completeness has
nothing to score against without one. A completeness check that fails
outright (not just scores low) is `PIM-0230`; a channel that specifically
requires a primary image and doesn't have one gets its own dedicated code,
`PIM-0232`, rather than folding into the generic completeness failure.

## Price lists

A **Price List Version** is an effective-dated, approval-gated snapshot of
prices per SKU for one price-list code — the same versioning pattern this
system's exchange rates use: approving a new version automatically closes
out (supersedes) any other still-open-ended Approved version of the same
price list, so there is never more than one "current" version in force at a
time. Resolving a price for a SKU as of a given date deliberately still
considers a **Superseded** version, not just the current Approved one —
"superseded" means "no longer the open-ended current version," not "was
never valid," which matters for correctly pricing a backdated document
against whatever was really in force on its date.

> [!WARNING]
> **Price lists are built end-to-end but reachable from nowhere.** There is
> no "Price List" tab anywhere in the PIM screen's tab list, no menu entry,
> and `ResolvePriceForSKU` — the function that actually looks up a price —
> is called from nowhere in this codebase except its own test file
> (confirmed by grep across every `.go` file). The versioning/supersession
> logic and the effective-dated resolution query are both real and correct;
> nothing in the running application creates a Price List Version, reads
> one back, or prices anything against one. Today, pricing a sale or a
> purchase order runs entirely through the mechanisms documented in
> [Point of Sale](pos-operations.md) and [Procurement](procurement.md) — a
> per-item Sale Price / Purchase Price, not this price list.

## Channels: category and field mapping

Publishing an item to a channel (see
[Channel Connectors](channel-connectors.md) for the connector/credential
side of this) can need its own category mapping (**Category Mapping**
tab, `ChannelCategoryMap`) and field mapping with per-channel mandatory
flags (**Field Mapping** tab, `ChannelFieldMap`) — this is what lets a
channel require, say, a primary image or a specific field even when PIM
core doesn't. **Validation Rules** (`ChannelValidationRule`) go further:
Min Images / Max Title Length / Required Tag rules evaluated at publish-
readiness time, checked before a publish attempt reaches the connector at
all.

## Supplier submissions

Suppliers reach this system through a limited-role login (the **Supplier**
role, one doctype only) rather than a second application — see
[Security, Roles & Approvals](security-approvals.md) for how that scoping
works. Their submissions land as `SupplierSubmission` records, reviewed
from the **Supplier Submissions** tab through the same maker-checker
approval engine every other approval-gated document uses.

## Field-level edit permissions

Some PIM fields can be restricted to specific roles independent of
whether the role can otherwise edit the record at all — attempting to
change a field you don't have edit rights to is refused with `PIM-0234`,
not silently dropped. A **bulk edit** across many items applies this and
every other validation per-item, so one item failing (a permission
restriction, a bad value) doesn't abort the whole batch — it reports which
ones succeeded and which didn't (`PIM-0235`), rather than an all-or-nothing
transaction across records that have nothing to do with each other.

## Reports

Reached from **Reports → PIM** (or the PIM screen's own **Reports** tab):
**PIM Overdue Tasks**, **PIM Product Group Readiness** (completeness per
item within a group — pairs with [Product Groups](pim-product-groups.md)),
**PIM Stalled Workflow Runs**, and **PIM Task Workload by Assignee**. All
task/workflow-oriented — the User Guide's "Handing out PIM work" section
covers what a task and a workflow run actually are.

## Error codes reference

| Code | Meaning |
|---|---|
| `PIM-0229` | No Product Family is set on this item — completeness cannot be scored. |
| `PIM-0230` | A product completeness check failed outright. |
| `PIM-0231` | (Warning) Enrichment content exists but is still Pending Approval, not yet Approved. |
| `PIM-0232` | A channel requiring a primary image has none. |
| `PIM-0233` | (Info) An uploaded media file's hash matches one already in the library — a duplicate, not an error. |
| `PIM-0234` | Attempted to edit a field this role doesn't have edit rights to. |
| `PIM-0235` | (Warning) A bulk edit succeeded for some items and failed for others — check which. |

## Troubleshooting

**An item's completeness score seems stuck at a low number.** Check, in
order: does it have a Product Family set (`PIM-0229` if not), does that
family have mandatory attributes this item hasn't filled, and does it have
**Approved** (not just submitted) enrichment content for the locale you're
scoring against.

**An attribute value I set doesn't seem to apply.** Check whether you set
it scoped to a specific locale/channel rather than the global value — a
global value only shows through when nothing more specific overrides it,
and if you're checking from a different locale/channel than you set it
under, the global row is what you'll see, not your scoped one.

**A bulk edit reports some items failed.** That's `PIM-0235` — each item is
validated independently, so check the per-item result rather than assuming
the whole batch either fully succeeded or fully failed.

**Need to price something from a Price List and can't find the screen.**
See the warning above — Price Lists have no UI today; POS and Procurement
each price from the item's own Sale/Purchase Price instead.

## What is not here yet

**Price Lists have zero UI and zero real callers** — see the warning above.
The versioning logic is complete and correct; nothing in the running
application uses it yet.

**Bulk media/asset operations for a Price List's supporting documents** and
similar cross-cutting DAM features are covered in the Media Library tab,
not this article — that tab is a real, wired screen, unlike the price list
gap above.