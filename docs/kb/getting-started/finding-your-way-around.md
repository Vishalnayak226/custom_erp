---
title: Finding your way around
section: Getting Started
order: 3
summary: The sidebar's eleven entries, the two search boxes that do different things, and the shape every list screen shares.
audience: everyone
last_verified: 2026-08-17
screens: [reports, doctype-table, inventory]
---

# Finding your way around

Three things account for most of the navigation in this application: the
sidebar, two search boxes, and one list-screen layout that repeats everywhere.

## The sidebar

Eleven top-level entries. Most are module groups - hover one and its screens
slide out to the right, or click it if you are on a touchscreen or using the
keyboard. **Reports**, **Manufacturing** and **PIM** have no flyout and open
straight away.

| Entry | What is inside |
|---|---|
| **POS** | POS / Billing · POS Profiles · Offline Sync Review · Offline Queue Gaps |
| **Financial Accounting** | Finance / GL · Approvals · Vendor Invoice · Payment Proposals · Bank Reconciliation · Debit / Credit Notes · Sales Invoice |
| **Sales & Marketplace** | Order Management · Fulfillment · Marketplace · Customer |
| **Reports** | Opens directly. Also where you land when you first sign in - its first tab is a dashboard of live figures. |
| **Procurement** | Purchase Requisitions · Purchase Order · ASN · Goods Receipt · Vendors · RFQ / Quotes |
| **Stock** | Inventory · Stock Transfer · Bin · Putaway · Bin Conditions · LPN / Cartons / Pallets · Bin Replenishment · Wave / Batch Picking · Mobile Picking · Cycle Count · Sticker Printing |
| **HRM** | HR · Fixed Assets · Expenses |
| **Manufacturing** | Opens directly. |
| **PIM** | Opens directly. |
| **Setup** | Every reference list the system knows about - Items, Locations, Bins, Brands, Currencies and the rest. |
| **Settings** | Administrator screens: Users · Roles · Prefix Configs · Approval Rules · Dynamic Labels · Database Schema Design · Extension Hooks · Activity Log · Configuration · System Status · Tenant Entitlements · Tenant Usage |

**You see only what your role can use.** A module missing from your menu is a
permission, not a fault. A whole group disappears when you have access to
nothing inside it, and your business only sees the modules it has licensed.

> [!NOTE]
> A supplier account is deliberately narrow. It signs in to the same
> application as everyone else but can reach only **Supplier Submissions**, and
> within it only the submissions filed under its own company.

## Two search boxes, doing different things

This trips people up more than anything else in the interface.

**The box at the very top of the window finds screens.** Type "purch" and it
offers Purchase Order, Purchase Requisitions and so on. Pick one and it takes
you there. It does not search your records.

**The box just above a table filters that table's records.** This is how you
find a particular vendor, item or invoice.

If a search "isn't finding" something, check which box you are typing into.

## The shape of a list screen

Nearly every screen is the same three parts:

1. A **title and action buttons** at the top right - typically **New** and
   **Bulk Import**. Any button your role cannot use is not rendered at all, so
   the screen is honest about what you can do rather than refusing you after
   you click. Where you can read a list but not add to it, a small
   **Read-only for your role** label appears where **New** would be.
2. A **filter box** and the table itself. Column headers stay frozen at the top
   while you scroll.
3. **Edit** and **Delete** icons per row, again only where your role has that
   permission.

Open a record and you get its fields, plus - on transaction records - the
related documents: lines, reservations, tasks, shipments, invoices, returns and
the audit trail on one screen. Start there before hunting through modules.

## Help, from wherever you are

- The **?** button in the header opens help for the screen you are on.
- The full Knowledge Center is at **/help**, with search over every article.
- Every error dialog carries a code such as `GLOBAL-0001`. It is the fastest
  route to an answer - see [Reading an error code](error-codes.md).

## When a screen tells you something is not set up

Screens that need master data check for it and say so:

- A **banner** at the top of a screen names what is missing and links to the
  screen that creates it. Dismissing it lasts for this browser session only, on
  purpose - a permanent dismissal is how a half-configured system stays that
  way.
- A **hint under a picker** whose list is empty tells you the same thing for
  that one field, and stays visible until it is fixed. A picker that does have
  options shows its "can't find what you need?" hint only while focused, so a
  form with eight pickers is not eight permanent lines of advice.

Both are permission-aware. If you are not allowed to create the missing record,
you are told to ask an administrator rather than shown a link that would refuse
you.

## Deep links

The address bar always describes where you are, and those addresses work when
opened in a new tab or shared with a colleague:

- `#/view/inventory` - a screen.
- `#/setup/Vendor` - a master list.
- `/help/first-order` - a Knowledge Center article.
