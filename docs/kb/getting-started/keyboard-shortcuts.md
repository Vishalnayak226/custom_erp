---
title: Keyboard shortcuts and quick codes
section: Getting Started
order: 4
summary: Ctrl+K opens anything - a screen, a code like PO or GRN, a report number like R1001, or a document number. Plus the keys that work on every screen.
audience: everyone
last_verified: 2026-10-26
screens: [home, reports, doctype-table]
doc_id: DOC-D50AE08CD5
type: reference
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
review_by: 2026-11-26
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
topic_type: reference
module: platform
task: Keyboard shortcuts and quick codes
prerequisites: Signed-in account; each code opens only screens your role can already open
---

# Keyboard shortcuts and quick codes

Press **?** on any screen (when you are not typing) to see the keys and codes,
with **Report numbers** (every report's number), **Guide** (this page) and
**Print** (a shortcut card to keep by the keyboard). On a Mac, use **⌘** wherever this page says
**Ctrl**.

## The search box: Ctrl+K

**Ctrl+K** (or **/**) puts you in the search box at the top. Type, then press
**Enter** to open the first result, or use **↑ ↓** to choose. **Esc** closes it.

| You type | What opens |
|---|---|
| A name, e.g. `purchase` | Matching screens, record lists and reports |
| A code, e.g. `PO` | That screen (codes are listed below) |
| A code and `+`, e.g. `PO+` | A new record on that screen |
| A code and a number, e.g. `PO 42` | Document number 42 of that kind. If several stores have a 42, you pick one |
| A report number, e.g. `R1001` | That report |
| A full document number, e.g. `PO/HO/26-27/00103` | That document, shown in its record list |
| Nothing (just Ctrl+K) | Your **Favourites**, **My work** counts and **Recent** screens |

A code, number or document opens only if your role can open it from the menu.
Each result shows its code or report number on the right, so you learn them as
you go.

**Scan to open:** with the cursor outside any field, scan the barcode printed on
a document and it opens.

## Keys

Hover over a button to see its key in the tooltip.

### Anywhere

| Key | Does |
|---|---|
| Ctrl+K or / | Search box |
| ? | This list of keys and codes |
| Alt+N | New record on this screen |
| Esc | Close a dialog, menu or the search box |

### Lists

| Key | Does |
|---|---|
| ↑ ↓ | Move between rows |
| Enter | Open the row |
| Space | Tick the row (for the actions on ticked rows) |

### Forms and New pages

| Key | Does |
|---|---|
| Ctrl+S | Save |
| Ctrl+Enter | Save and submit (Submit, Post or Send for approval, where the screen has one) |
| Ctrl+P | Print the document |
| Enter | Next field. On New transaction pages only; at the last field it moves to Save, and one more Enter saves |
| Ctrl+D | Add a line (presses the screen's **Add line**, with what you typed) |
| Ctrl+Delete | Remove the line the cursor is on (a box or button in that line) |

**Kept for the browser:** F1-F12, Ctrl+N, Ctrl+T, Ctrl+W and Alt+F / E / D do
what your browser does with them. **POS / Billing** keeps its own keys.
In a picker or a notes box, Enter still picks or starts a new line.

## Codes

The same codes work for every company. A code shows only if your role has the
screen.

| Area | Code | Opens |
|---|---|---|
| Go to | `HOME` | Home |
| Go to | `INB` | My work and approvals |
| Go to | `RPT` | Reports |
| Go to | `HELP` | Knowledge Center |
| Sales | `POS` | POS / Billing |
| Sales | `SO` | Sales Order |
| Sales | `SI` | Sales Invoice |
| Sales | `SR` | Sales Return |
| Sales | `CN` | Credit Note |
| Sales | `DN` | Debit Note |
| Sales | `EI` | E-Invoice (IRN) |
| Sales | `CUS` | Customers |
| Sales | `CRM` | CRM |
| Sales | `LOY` | Loyalty |
| Buying | `PRQ` | Purchase Requisition |
| Buying | `RFQ` | Request for Quotation |
| Buying | `PO` | Purchase Order |
| Buying | `ASN` | Advance Shipment Notice |
| Buying | `GRN` | Goods Receipt |
| Buying | `PR` | Purchase Return |
| Buying | `VI` | Vendor Invoice |
| Buying | `VEN` | Vendors |
| Stock | `SOH` | Stock on Hand |
| Stock | `SL` | Inventory Ledger |
| Stock | `TO` | Stock Movement (Transfer Order) |
| Stock | `SA` | Stock Adjustment |
| Stock | `MRP` | MRP Change |
| Stock | `MI` | Material Issue |
| Stock | `DC` | Delivery Challan |
| Stock | `MS` | Move Stock |
| Stock | `CC` | Cycle Count |
| Stock | `STK` | Sticker Printing |
| Stock | `ITM` | Items |
| Warehouse | `PUT` | Putaway |
| Warehouse | `PICK` | Pick |
| Warehouse | `PACK` | Pack |
| Warehouse | `SHIP` | Ship |
| Warehouse | `WAVE` | Wave / Batch Picking |
| Money | `GL` | Finance / GL |
| Money | `JV` | Journal Voucher |
| Money | `PAY` | Payment Proposal |
| Money | `BR` | Bank Reconciliation |
| Money | `EXP` | Expense Claim |
| Money | `FA` | Fixed Assets |
| Making | `PRO` | Production Order |
| Making | `JW` | Job Work |
| Making | `QI` | Quality Inspection |
| People | `HR` | HR |
| Admin | `USR` | Users |
| Admin | `ROLE` | Roles |
| Admin | `CFG` | Configuration |
| Admin | `NUM` | Number series (Prefix Configs) |

## Report numbers

Every report has a fixed number: **R**, then the area digit, then three digits.
Sales Register is **R1001**. A number never changes and is never reused, and it
is the same for every company, so you can say "run R3004" to anyone.

| Numbers | Area |
|---|---|
| R1xxx | Sales & POS |
| R2xxx | Procurement & GRN |
| R3xxx | Inventory, Stock Transfer, External Movement, Inventory Loss & Audit |
| R4xxx | WMS |
| R5xxx | Accounting, Finance, GST & Compliance, Expenses, Assets |
| R6xxx | CRM, OMS, E-commerce & Integration, Service |
| R7xxx | PIM, Manufacturing |
| R8xxx | HR |
| R9xxx | MIS, Master Data & Audit, Admin |

The number shows beside the report's name in the **Reports** menu, on the report
page, and in the name of an exported file. To find a number, press **?** and
choose **Report numbers**, or see the [Report catalog](../reference/report-catalog.md).

## Favourites and recent

- **☆ beside a screen's title** adds it to your favourites (★). Click again to
  remove it. Favourites appear first when you press Ctrl+K, on any computer you
  sign in to.
- **Recent** shows the last 10 screens, reports and documents you opened in
  this browser.

## After you save

A bar at the bottom of the screen offers the next move. It closes by itself after 15
seconds, or with **×**.

| Button | Does |
|---|---|
| Print | Print the document you just saved |
| New | Start another one |
| Copy | A new record with the same details, to change and save |
| Next step | Opens the next document in the chain, ready to fill - once this one can move on (not while it is a draft or waiting for approval) |

| After | Next step |
|---|---|
| Purchase Requisition | Request for Quotation |
| Request for Quotation | Purchase Order |
| Purchase Order (approved) | Goods Receipt against that PO |
| ASN | Goods Receipt |
| Goods Receipt | Vendor Invoice |
| Vendor Invoice | Payment Proposal |
| Sales Order | Pick, then Pack, then Ship |

## Less typing

- **Copy a record:** the **Copy** icon on a record list row, **Copy** on a
  Purchase Order in the PO list, or **Copy** in the after-save bar. The number,
  status and dates are not copied. Check it and save.
- **Your last choice comes back:** a drop-down for store, location, warehouse,
  counter, salesman, cost centre or payment mode starts on what you last picked
  on that screen, in this browser. It is highlighted so you can see it was
  filled for you.
- **Unsaved work is kept:** if the tab closes while a New form has details in
  it, the form offers **Restore** or **Discard** next time. Only the form's own
  boxes are kept, not the rows of a line table.
- **Saved views:** on a record list, type a search in the box above the table,
  then pick **Save this search as a view…** in the views drop-down beside it.
  Pick the view later to run the same search. Views follow you to any computer.
- **Ticked rows:** tick rows on a record list (Space ticks the row you are on).
  A bar offers **Export** (a CSV of the ticked rows), **Submit for approval**
  (where the list has that action) and **Delete**; Submit and Delete do up to 40
  rows a run.
- **Approvals:** tick rows and click **Approve ticked** or **Reject ticked**.
  Each is decided exactly as if you had clicked its own button. A password
  reset is always decided on its own.
