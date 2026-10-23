---
doc_id: DOC-PRODUCT-NAVIGATION-SIMPLIFICATION-PLAN
title: Navigation and module plan - OMS, CRM, Inventory and WMS apart (2026-10-18)
type: reference
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md navigation stages
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-18
review_by: 2026-11-18
supersedes: none
superseded_by: none
---

# Navigation and module plan - OMS, CRM, Inventory and WMS apart; one page per step; menus instead of tab strips; one Templates page (2026-10-18)

Source: the user's messages of 2026-10-18 with screenshots of **Customer**, **Marketplace**, the **Setup** flyout,
**Manufacturing**, **Reports**, **Home**, **RF Lot & Serial** and the **Stock** flyout:

> *CRM is separate and OMS is separate. Under OMS each module will be separate. Pick/Pack/Ship - Invoice/Label/Manifest
> three pages. Accept (Pending Order) one page. Then return. See all config in one page. It should be diff page. In Setup
> and Settings, config all in one looks scary. Group them and show in another sub menu. User will choose the module and
> take action.* - *Manufacturing and PIM also show as sub menu like other.* - *Report also. It will be grouped with Sales
> and show sub group of Sales related in another flyout and so on. Home will show only txn, no master data shortcut.
> Inventory is diff and WMS is diff. Inv is part of ERP. Inv will have dashboard, ledger of inv and stock movement
> (location to location), adjustment, MRP change. All will have separate history page like we built for POS bill, PO.*

Checklist: **Stage 88** in `docs/micro_checklist.md` (same item IDs).

## Decisions

| # | Decision | Answer | By |
|---|---|---|---|
| D1 | Shape of Setup and Settings | **Two-level menu**: Setup shows module names, hovering one opens its lists; Settings shows its groups the same way | user |
| D2 | Pending Orders | Orders from a marketplace or the own website **stop at Pending** until someone accepts or rejects them; the answer is sent back to that marketplace / website | user |
| D3 | OMS step pages | **Pick · Pack · Ship** - the OMS bill and the label are made at Pack, the manifest at Ship | user |
| D4 | Two bills | The **OMS bill** (own OMS series, OMS template) goes to the customer. When the manifest is handed over, the **ERP bill** is made from it. They are separate documents | user |
| D5 | GST invoice number | **The OMS bill's number** is the GST invoice. The ERP bill links to it and is never reported to GST a second time | user |
| D6 | Templates | **One Templates page under Setup, module by module**: ERP templates (barcode, TO, TI, PO, ...) and OMS templates (OMS bill, label, manifest, ...) | user |
| D7 | Reports | A **Reports menu grouped by area** (Sales, Purchase, Inventory, Finance, GST, ...), each area opening its reports in a second menu | user |
| D8 | Home | **Transactions only** - no master-data shortcut (Vendors, Customers go) | user |
| D9 | Inventory vs WMS | **Two modules.** Inventory is ERP: Dashboard, Stock on Hand, Inventory Ledger, Stock Movement (location to location), Adjustment, MRP Change - each a list (history) first with **+ New** as its own page. WMS keeps the warehouse-floor screens | user |
| D10 | Rule for every module | Its menu holds working pages; its configuration sits in Setup ▸ that module | default |
| D11 | Settlements, Service Tickets | OMS ▸ Settlements; CRM ▸ Service Tickets (no menu entry today) | default |
| D12 | Manual orders (made by staff in OMS) | Skip Pending - the person creating them has already accepted them | default, confirm |
| D13 | Stock Adjustment | A reason on every line; goes through the approval rules; posts the stock ledger and GL at valuation rate | default, confirm |
| D14 | MRP Change | Changes the item's MRP from an effective date, keeps old and new MRP in its history, and offers a sticker reprint for stock on hand. Item-level, not batch-level | default, confirm |

## Order of work

| Order | Phase | What | Engine change |
|---|---|---|---|
| 1 | 88.1 | Menus: OMS, CRM, Inventory, WMS apart; Manufacturing, PIM, Reports, Setup, Settings as menus; Home; RF exit | none |
| 2 | 88.2 | OMS pages on what exists: Orders, Pending, Pick, Pack, Ship, Returns, Settlements | none |
| 3 | 88.3 | OMS behaviour: Pending gate with answer to the channel; OMS bill at Pack; ERP bill at manifest | yes, additive |
| 4 | 88.4 | One Templates page, module by module | small |
| 5 | 88.5 | Inventory: Dashboard, Ledger, Movement, Adjustment, MRP Change | yes, additive (2 new documents) |
| 6 | 88.6 | Guides, KB, help mapping, brain, walk-through | - |

## 88.1 Menus

**Why:** today "Sales & Marketplace" mixes orders with customers; "Stock" holds 25 entries of ERP stock and warehouse
floor together; Manufacturing, PIM and Reports open one page with a strip of 9-21 tabs; the Setup menu lists ~120 lists in
one scroll; Home offers master-data shortcuts; the RF task screen has no way out.

**Build:**
- **88.1.1 OMS ▸** Orders · Pending Orders · Pick · Pack · Ship · Returns · Settlements. "Sales & Marketplace" goes. Internal
  ids (`module-fulfillment`, `menu-oms`) stay so role visibility and saved navigation keep working.
- **88.1.2 CRM ▸** Customers · Loyalty · Campaigns · Vouchers · Service Tickets.
- **88.1.3 Inventory ▸** (ERP) Dashboard · Stock on Hand · Inventory Ledger · Stock Movement · Adjustment · MRP Change ·
  Move Stock · Sticker Printing. Entries whose page is built in 88.5 appear with it.
- **88.1.4 WMS ▸** its own module, grouped: Cockpit · Inbound (Appointments, Dock Doors, Yard Board, Trailers, RF Receiving,
  Putaway, Cross-Dock) · Inside (Bins, Bin Conditions, LPN, Replenishment, Location Movement, Cycle Count, RF Lot & Serial,
  Holds) · Outbound (Waves, Wave Picking, Mobile Picking, Sortation, Loading). Hold Codes moves to Setup ▸ WMS.
- **88.1.5 Manufacturing ▸** Production (Orders, Schedule, Subcontracting) · Planning (MRP Suggestions, Supply Proposals) ·
  Quality (Inspections, Certificates, Non-conformance) · Maintenance. No tab strip when opened from the menu.
- **88.1.6 PIM ▸** working pages: Dashboard, Products, My Work, Supplier Submissions, Media Library, Catalogs, Reports. Its
  families, attributes, workflows, channel maps and import/export templates move to Setup ▸ PIM.
- **88.1.7 Reports ▸** Dashboard · Schedules · one entry per area (Sales, Purchase, Inventory, WMS, OMS, CRM, Finance, GST,
  Manufacturing, HR, ...) opening that area's reports in a second menu. Each report opens on its own, no tab strip.
- **88.1.8 Setup ▸** module names only; hovering one opens its lists in a second menu (Stock's lists split between
  Inventory and WMS). **Settings ▸** Users & Access ▸ · Customize ▸ · Monitoring ▸ · Platform ▸ (super admin).
- **88.1.9 Home** shows transaction shortcuts only: Vendors and Customers go; Order Management / Fulfillment become Orders /
  Pending Orders.
- **88.1.10 RF Lot & Serial** keeps its full-screen scanner layout (Stage 47.6.2) but gets an **Exit** button on its task
  menu. Today the browser's Back button is the only way out.
- **88.1.11** References follow: visibility maps, refresh-restore map, Home tiles, inbox and order next-step links; old view
  names redirect so bookmarks and refresh still land. The global search builds itself from the sidebar.

**Done when:** every former entry and tab is reachable in at most two hovers and a click; no menu shows more than ~15
entries at once; a role sees only the modules it has; refresh keeps the highlight on every page.

## 88.2 OMS pages on what already exists

**Why:** Fulfillment is one table of every task in every state, and Marketplace stacks settlements, reconciliation,
bookings and manifests. Several working APIs have no screen: the pick worklist (`GET /api/v1/oms/pick-queue`, 35.3.4),
packages and the pack invoice (`/api/v1/oms/shipping-packages`, 35.4.2), gate passes (35.4.4), `label.pdf`, and the channel
status push (`PushChannelOrderStatus`).

**Build:**
- **88.2.1 Orders** - today's Order Management page without its configuration blocks (connectors, unmapped SKUs, bundles
  move to Setup ▸ OMS).
- **88.2.2 Pending Orders** - orders waiting for a decision; Accept / Reject, one or many. The **Order owner (3PL client)**
  box from Fulfillment becomes a row action here.
- **88.2.3 Pick** - the pick worklist, pick-list print, scan-pick, short-pick. A WMS-run location hands off to Mobile Picking.
- **88.2.4 Pack** - scan-pack, then one action makes the package, its bill and its label.
- **88.2.5 Ship** - courier booking (AWB), manifest, handover, gate pass, delivery / RTO.
- **88.2.6 Returns** under OMS. **88.2.7 Settlements** - payouts and the reconciliation queue (its descriptive line goes).
- **88.2.8** The combined Fulfillment and Marketplace pages retire into these.
- **88.2.9 OMS settings on one page** - channel connectors, SKU mapping exceptions, bundles and OMS settings together,
  reached from Setup ▸ OMS ("see all config in one page, a different page").

**Done when:** one order is walked Pending → Pick → Pack → Ship → Returns from the OMS menu alone, on a scratch server.

## 88.3 OMS behaviour

**Why:** D2, D4, D5. Orders release to the warehouse on their own today, and the pack step makes a Draft ERP Sales
Invoice (35.4.2) - there is no OMS bill.

**Build:**
- **88.3.1 Pending gate.** Marketplace and website orders that pass the checks stop at Pending. Accept releases them to
  Pick; Reject cancels them with a reason. Both answers go to the channel through `PushChannelOrderStatus`; a failed push
  is shown on the order and retried. Manual orders skip Pending (D12). Additive status and setting, with a migration.
- **88.3.2 OMS bill at Pack.** A separate OMS bill per package with its own OMS number series, printed with the OMS
  template and sent to the customer. Its number is the GST invoice number (D5).
- **88.3.3 ERP bill at manifest handover.** Handing a manifest over makes the ERP bill (Sales Invoice) for each OMS bill in
  it, linked to the OMS bill's number, and posts it. GST returns read the OMS number and count the sale once.
- **88.3.4** The 35.4.2 pack-time Draft Sales Invoice is replaced by 88.3.2 / 88.3.3; no existing invoice is changed.

**Done when:** a marketplace order waits at Pending, Accept is pushed to the channel, Pack prints an OMS bill numbered from
the OMS series, the manifest handover makes one posted ERP bill linked to it, and the GST return summary lists that sale
once under the OMS number.

## 88.4 One Templates page

**Why:** templates sit in three places today: Setup ▸ Core ▸ Print Template (the gallery, 80.6), the Templates tab of
Stock ▸ Sticker Printing, and the shipping label inside Marketplace.

**Build:**
- **88.4.1** Setup ▸ Templates: one page, grouped by module - ERP (PO, GRN, TO, TI, Sales Invoice, barcode / stickers, ...)
  and OMS (OMS bill, label, manifest, pick list). Each opens the existing designer for that kind.
- **88.4.2** The other two entry points send people here.

**Done when:** every template the system prints is reachable from one page, and nowhere else lists them.

## 88.5 Inventory (ERP)

**Why:** D9. Inventory today is one stock-on-hand table. A stock ledger exists (`StockLedgerEntry`) without a page;
Stock Adjustment and MRP Change have no document at all.

**Build** (each a list first, **+ New** as its own page, `public/txn-screen.js`):
- **88.5.1 Dashboard** - stock value and units by location, low stock, recent movements, adjustments waiting approval.
- **88.5.2 Inventory Ledger** - every stock movement with item, location, document, in / out and running balance; filters.
- **88.5.3 Stock Movement** - location to location, on the existing transfer documents.
- **88.5.4 Stock Adjustment** - new document (D13).
- **88.5.5 MRP Change** - new document (D14).

**Done when:** each page is list-first with its own New page, an adjustment moves stock and GL after approval, and an MRP
change updates the item from its effective date with both prices in its history.

## 88.6 Guides, KB, help mapping, brain

- **88.6.1** USER_GUIDE / ADMIN_GUIDE: every changed menu path rewritten and walked literally; new pages documented.
- **88.6.2** Help articles mapped to the new pages; KB regenerated (`docs/update-docs.ps1 -Group KB`); brain redrawn.
- **88.6.3** Walk-through on a scratch server with a manager and a cashier role, refresh on every new page.

## Not adopted

| Idea | Why not |
|---|---|
| One Setup page with a module rail; setup inside each module | Considered for D1; the user chose the two-level menu |
| Sales Invoice moves into OMS | It is the ERP bill and stays in Financial Accounting; OMS makes it at manifest |
| Batch-level MRP | D14 keeps MRP on the item; batch MRP is a separate change if ever needed |

## Guardrails

- No new framework or library. Menus reuse `.menu-flyout` and `submenu-group-label`; pages reuse `txnPagedSection` and the
  existing designers. 88.1, 88.2 and 88.4 change no engine or schema; 88.3 and 88.5 are additive migrations.
- Internal ids and view names stay; old views redirect.
- Stage 85/86/87 sessions have uncommitted edits in `public/app.js`, `public/index.html`, the guides and the trackers:
  stage by hunk.
- Noticed, not in this plan: the Customer list's column headers carry form hints ("(optional)", "(blank = unlimited)"),
  and the two customers have codes in two formats (C00001 and Customer/HQ/2026/000001).
