---
title: Finding your way around
section: Getting Started
order: 3
summary: The sidebar's twelve entries, the two search boxes that do different things, and the shape every list screen shares.
audience: everyone
last_verified: 2026-09-24
screens: [home, reports, inventory]
doc_id: DOC-D4BCCBCC10
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: tutorial
module: platform
task: Finding your way around
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Finding your way around

Three things account for most of the navigation in this application: the
sidebar, two search boxes, and one list-screen layout that repeats everywhere.

For task instructions and error recovery, see [Find help for your task](using-task-help.md).

## The sidebar

Fifteen top-level entries. Most are module groups - hover one and its screens
slide out to the right, or click it if you are on a touchscreen or using the
keyboard. Some groups inside a module (WMS's **Inbound**, each **Setup** module,
each **Reports** area, each **Settings** group) open a second menu beside them
the same way; on a phone it opens under its row. **Home** and **Knowledge
Center** have no flyout and open straight away.

| Entry | What is inside |
|---|---|
| **Home** | Opens directly. Where you land when you first sign in: your own quick actions (only the day-to-day screens your role can actually use), anything waiting on you, what you last touched, and - on a tenant that is still mostly empty - a "Get started" nudge toward the first master record to set up. |
| **POS** | POS / Billing · POS Profiles · Offline Sync Review · Offline Queue Gaps |
| **Financial Accounting** | Finance / GL · Approvals · Vendor Invoice · Payment Proposals · Bank Reconciliation · Debit / Credit Notes · Sales Invoice |
| **OMS** | Orders · Pending Orders · Pick · Pack · Ship · Returns · Settlements |
| **CRM** | Customers · Loyalty · Campaigns · Vouchers · Service Tickets |
| **Reports** | A dashboard of live figures, then one entry per area (Sales, Procurement, Inventory, WMS, OMS, CRM, Finance and the rest) whose reports open beside it, then Schedules. |
| **Procurement** | Purchase Requisitions · Purchase Order · ASN · Goods Receipt · Vendors · RFQ / Quotes |
| **Inventory** | Dashboard · Stock on Hand · Inventory Ledger · Stock Movement · Stock Adjustment · MRP Change · Move Stock · Stock Locations · Sticker Printing |
| **WMS** | Warehouse Cockpit, then **Inbound** (Appointment Calendar, Dock Doors, Yard Board, Trailers, RF Receiving, Putaway, Cross-Dock Plans), **Storage & Control** (Bin, Bin Conditions, LPN, Replenishment, Location Movement, Cycle Count, RF Lot & Serial, Holds) and **Outbound** (Waves, Picking, Sortation, Loading) |
| **HRM** | HR · Fixed Assets · Expenses |
| **Manufacturing** | Production Orders · Production Schedule · Subcontracting · MRP Suggestions · Supply Proposals · Quality Inspections · Certificates · Non-conformance · Maintenance Orders |
| **PIM** | Dashboard · Workbench · My Work · Media Library · Supplier Submissions · Catalogs · PIM Reports |
| **Setup** | The module names; point at one and its reference lists (Items, Locations, Bins, Brands, Currencies and the rest) open beside it. |
| **Settings** | Administrator screens in four groups: Users & Access (Users · Roles · Approval Rules) · Customize (Configuration · Prefix Configs · Dynamic Labels · Database Schema Design · Extension Hooks) · Monitoring (Activity Log · System Status) · Platform |

**You see only what your role can use.** A module missing from your menu is a
permission, not a fault. A whole group disappears when you have access to
nothing inside it, and your business only sees the modules it has licensed.

> [!NOTE]
> A supplier account is deliberately narrow. It signs in to the same
> application as everyone else but can reach only **Supplier Submissions**, and
> within it only the submissions filed under its own company.

## Two search boxes, doing different things

This trips people up more than anything else in the interface.

**The box at the very top of the window (Ctrl+K) opens things.** Type "purch"
and it offers Purchase Order, Purchase Requisitions and the purchase reports.
Pick one and it takes you there. It also takes a short code (`PO`), a report
number (`R1001`) or a full document number - see
[Keyboard shortcuts and quick codes](keyboard-shortcuts.md). It does not filter
the list you are on.

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
