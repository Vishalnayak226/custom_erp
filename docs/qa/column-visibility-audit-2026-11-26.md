---
doc_id: DOC-QA-276-001
title: Column visibility and UI/UX walk of every screen on minn (ask 276) - audit record
type: record
status: draft
owner: qa-owner
approvers: [product-owner]
audience: [product, QA, implementation]
applies_to: minn.wholeops.in release 0.29.0, run 2026-11-26 21:05-22:20 IST
authority: historical
confidentiality: internal
last_verified: 2026-11-26
review_by: 2026-12-26
supersedes: none
superseded_by: none
verification_scope: Read-only Guide `columns` suite, opened from app's Control Panel as the support user (Super Admin); every sidebar screen, its tabs, its New page and its first record where one opened; each table measured at the Guide window (1413 x 653) and at 1152, 1280, 1366, 1536 and 1920 px wide; no write let through
---

# Column visibility and UI/UX walk of minn

The owner sent a screenshot of the Users list on minn with the last column cut off at the right
edge and asked for a walk of every screen to check that every column can be seen, plus a UI/UX
review. This record says what was walked, what was found and which tracker item each finding
became. The items are in `docs/micro_checklist.md` under ask 276 (276.4 onward), each with a
recommended fix, for the owner to review before anything is built. Nothing was changed in the
app.

## How it was walked

- **Tool.** Guide's new `columns` suite (`~/dev-tools/guide/suites/columns.js`), run as
  `pwsh Guide.ps1 -Site minn -Via app -Suite columns`. Writes were blocked in the browser; four
  POST calls a screen tried to make were refused and are listed under "Limits of this walk".
- **Sizes.** Each view was measured at the Guide window, then at five window widths. 1152 x 648 is
  the owner's own setup (a 1920 screen at 150% Windows scaling and 110% browser zoom); 1366 x 768
  is the most common office laptop.
- **What counts as a fault.** A column is *hidden* when 3% or less of its header shows, and *cut*
  when it is partly shown or its values run past the visible edge. For each hidden column the walk
  also checked whether the table's sideways scrollbar is on screen or below the window.
  *Squeezed* means a header on two or more lines, or a short value (28 characters or fewer)
  wrapping. *Blank* means a column empty on every row shown.
- **Coverage.** 220 views: 140 screens, 5 tabs, 71 New pages and 4 first records. 162 tables were
  measured at each size.

## Results

### Columns cut off or hidden

| Size | Tables | With a column not fully visible | Scrollbar off screen |
|---|---:|---:|---:|
| Guide window, 1413 px | 162 | 1 | 0 |
| 1152 px (owner) | 162 | 5 | 0 |
| 1280 px | 162 | 1 | 0 |
| 1366 px | 162 | 1 | 0 |
| 1536 px | 162 | 1 | 0 |
| 1920 px | 162 | 1 | 0 |

| Screen | Columns | Not visible at 1152 / 1280 / 1366 / 1536 / 1920 | What is missing at 1366 | Item |
|---|---:|---|---|---|
| Inventory > Stock on Hand | 17 | 9 / 8 / 7 / 6 / 2 | Store (cut), On hand, Available to sell, Reserved, Held, Why unavailable, Next task | 276.4 |
| Settings > Users | 9 | 1 / - / - / - / - | Actions cut at 1152 (the owner's screenshot) | 276.7 |
| Procurement > Goods Receipt, first record | 10 | 1 / - / - / - / - | one line column at 1152 | 276.8 |
| Financial Accounting > Vendor Invoice, record and New | 10 | 1 / - / - / - / - | one line column at 1152 | 276.8 |

**What this means.** Most lists do not push columns off screen. They squeeze them instead:
headers wrap onto two to four lines and short values wrap, so rows get tall and fewer fit on the
screen. The one list that does lose columns, Stock on Hand, loses the quantity columns, which are
the ones people open it for.

### Squeezed columns (at the Guide window)

| Screen | Headers on 2+ lines | Short values wrapping | Item |
|---|---|---|---|
| Procurement > Purchase Order | Source requisition, Billing address, Grand total | Vendor, Billing address, Items | 276.6 |
| Settings > Users | Home store | Home store, Stores; Actions stacked on 5 lines | 276.7 |
| WMS > Dock Doors | all 7 field columns, e.g. "Service window start (HH:MM, optional)" | - | 276.8 |
| WMS > Cross-Dock Plans | ASN reference (optional), PO reference (optional) and 4 more | - | 276.8 |
| CRM > Vouchers | 7 headers, e.g. "Restricted to customer (optional)" | - | 276.8 |
| CRM > Customers > New | Contract price list (optional) and 4 more | - | 276.8 |
| CRM > Vouchers & Campaigns | 6 headers | - | 276.8 |
| Procurement > Purchase Requisitions | 6 headers | - | 276.8 |
| Procurement > Vendors | Vendor name, Bank account number, Bank IFSC | - | 276.8 |
| Inventory > Stock Locations | Physically in store?, New stores, WMS bin condition | - | 276.8 |
| Inventory > Inventory Ledger | - | When, Item, From → To, By | 276.8 |
| Financial Accounting > Vendor Invoice | - | Vendor | 276.8 |
| Settings > Prefix Configs | Store in number, Store-wise | Transaction | 276.8 |
| Settings > Integration Payloads | - | Payload runs out of its cell | 276.8 |

The headers with "(optional)" or "(HH:MM, optional)" in them are form labels reused as column
titles by the shared list screen (`public/view-documents.js`, the header row). One fix there
covers every list built on it.

### Columns blank on every row shown

These are inputs for the default column sets in 276.2, not faults:

- Procurement > Purchase Order: Source requisition (20 of 20 rows).
- Procurement > Goods Receipt: Price notes (10 of 10).
- Procurement > Vendors: GSTIN, Bank account number, Bank IFSC, Onboarding (19 of 19).
- Financial Accounting > Chart of Accounts: Parent, Flags (90 of 90).
- Inventory > Inventory Ledger: Out (200 of 200). Probably only receipts in the latest 200 rows;
  check when 276.2 is built.

False alarms: CRM > Tiers and Settings > API access hold inputs and radio buttons, not text.
Approval Rules' Actions column holds icon buttons. Async Jobs' Last error being empty is good
news.

### Speed

Median 0.69 s from opening a screen to settled, over 220 views. None took over 3 s. The slowest
were PIM Workbench first record (2.4 s), Inventory Ledger (2.3 s), Home (2.0 s) and
Configuration (1.9 s). Speed is not a problem on minn today, so no item was filed for it.

### Look and feel

Taken from the measurements and from screenshots of the worst screens:

| Finding | Item |
|---|---|
| Row actions are rows of text buttons. Purchase Order shows up to 11 per row on 4 lines, so a row is ~170 px tall and only 3 POs fit on screen. Users stacks 5. Vendors mixes a text button with icons. | 276.5, 276.6, 276.7 |
| An empty list stretches to the bottom of the window as a large blank box, with a how-to sentence, "Showing 0-0 of 0" and greyed Previous / Next. | 276.9 |
| Lists built on the shared screen are titled in the singular ("Vendor", "Dock Door"); screens with their own page use the plural ("Purchase Orders"). | 276.10 |
| Vendors: a large "old long code" renumber card sits above the list and pushes it below the fold, and the Onboarding header is cut behind the Actions column. | 276.11 |
| Inventory > Allocation has no standard page title, its From picker shows a raw location code instead of a store name, and it uses the browser's unstyled "Choose File" button. | 276.12 |
| WMS > RF Lot & Serial: no page title found. | 276.13 |
| POS > Sales History: six filters, one per full-width row, so the table starts near the bottom of the screen. | 276.14 |
| Header bar: Refresh and Reset are large labelled buttons. Reset throws away unsaved work on screen and sits right next to Refresh. | 276.15 |
| 19 font sizes in use across the app (median 6 on a screen, up to 11); text under 12 px on 137 of 220 views. | 276.16 |
| Classic Windows scrollbars with arrow buttons on the sidebar and inside panels. | 276.17 |
| Home is almost empty for a Super Admin (a setup banner and one number); Reports opens on four tiles reading 0 and blank space. | 276.18 |
| CRM > Service Tickets > New asks for the asset count on a client without the Assets module and gets "module not enabled" (403). | 276.19 |
| Overall direction for "aesthetic and futuristic". | 276.20 |

The scan for text under the title (Article 7) flagged only Knowledge Center, and that was the
article body, a false alarm. No screen scrolls the whole page sideways.

## Other things the walk saw

- `GET /doc/Location/HO` returned 404 on POS Billing, OMS Returns New and HRM Expenses. The
  support user's home store "HO" has no Location record on minn. The POS code treats a 404 as an
  unregistered store and carries on (`public/view-pos.js`, `loadPOSStoreProfile`), so this is by
  design. Not filed.
- PIM Workbench first record asks for its English content and gets 404 when none is written yet.
  This is by design. Not filed.
- Settings > Data Healer's 404s for `Item/207.4` and `Item/263.4` are already item 277.1.

## Limits of this walk

- Run as Super Admin, which sees every column and button. A store user sees fewer actions, so
  row heights for that user will be lower than measured here.
- First records opened on only 4 screens. Most lists open a record from a button, which a
  read-only walk does not press.
- Many New pages open beside or over their list, so the New measurement often re-measured the
  list. Forms were not checked field by field.
- Four POST calls were blocked as writes: POS till series, CRM segment count (a read sent as a
  POST), WMS piece codes and WMS work queue. Those screens may have shown less than a real user
  sees.
- Screenshots stay in the Guide report folder on this machine. They show client data and are not
  in the repository.
