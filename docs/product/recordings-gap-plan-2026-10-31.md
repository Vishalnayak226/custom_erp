---
doc_id: DOC-PRODUCT-STAGE146-RECORDINGS-GAP-PLAN
title: Stage 146 plan - what the vendor recordings show that the ERP lacks, built our way
type: reference
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 146
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-31
review_by: 2026-12-31
supersedes: none
superseded_by: none
---

# Stage 146 plan: the missing pieces from the vendor recordings

**As of 2026-11-03. All decisions taken** (the user's `Downloads\Remarks.xlsx` 2026-11-01; D15 and D17 in chat 2026-11-03). The user's rule for the whole plan: *"If we have already not required. This is just to re-assure we have the base or not. If we have more ignore it."* - so only real gaps stay in it. Status is tracked as **Stage 146** in `docs/micro_checklist.md` with the same item IDs.

**Input:** 12 meeting recordings in the user's `Downloads\Telegram Desktop\Dynamics Loyalty Management Demo 1` folder, 9 h 15 m; `Infor Wms(1)` is a copy of `Infor Wms`. Almost all were made for Kushal's Fashion Jewellery (about 78 stores, an e-com warehouse, Shopify and marketplaces through Unicommerce):

| Recording | What it really is | Blueprint |
|---|---|---|
| Microsoft Dynamics - Discussion 1 - ERP | PwC demo of D365 **Advanced Warehouse Management** (Jul 2023) | [wms-d365](research/recordings/wms-d365.md) |
| Microsoft Teams - Infor Wms; PwC Process WMS Demo 1, 2 | Infor LN Cloud's built-in Warehousing, not the separate Infor WMS product (Jun 2023); PwC walking the client's own flow on D365, PO to store-transfer pick (Aug 2023) | [wms-infor-pwc](research/recordings/wms-infor-pwc.md) |
| Dynamics Loyalty Management Demo 1, 2 | D365 Commerce **loyalty** (CodeValue, PwC; Oct 2023) - Demo 1 also covers D365 order routing beside Unicommerce | [loyalty-d365](research/recordings/loyalty-d365.md) |
| Manoj Unicommerce Learning 1, 3, 6 | Kushal's internal **Unicommerce** training: purchase / reorder, inbound, listings (Dec 2022) | [unicommerce](research/recordings/unicommerce.md) |
| Saastify 1st meeting; Unbxd Training | **PIM**: a Saastify sales demo; Kushal's catalogue team trained on its live Unbxd tenant | [pim-unbxd-saastify](research/recordings/pim-unbxd-saastify.md) |

Each blueprint has the vendor's modules, screens, fields, flows and rules with timestamps, a gap table that checks every capability against this repo with `file:line` evidence, a ranked list of what is missing, and what is not worth adopting. This plan takes the ranked lists and turns them into Stage 146 items.

**How the recordings were read:** audio transcribed locally (faster-whisper, translate-to-English for the Tamil / Telugu mixed sessions), de-duplicated screen frames OCR'd every 15 s, then one reader per domain checked each capability against the code. The headline claims below were re-checked by hand in the code.

**Built our way, not copied.** Every item extends an engine, doctype, screen or choke point that already exists (named in the item). Vendor screen layouts, menu names, multi-document chains and card / currency models are not copied - section 4 lists what was left out and why.

**What the ERP already does as well or better** (one line each; detail in the blueprints): loyalty tiers, expiry, pending points, return reversal, approved adjustments, OTP and daily cap on redemption, points as a tender with liability accounting, real-time balances; bins, put-away, pick by bin, waves, ASN, QC buckets, cross-dock, LPN, replenishment, cycle counts, RF receiving and picking; PO amend / copy / close, GRN shortage and excess with debit notes, inward location until put-away, bulk PO upload, nearest-store sourcing, invoice on shipment; PIM attributes, completeness, approvals, channel export templates, Shopify publish with links.

## 1. Decisions

| # | Decision | Recommended | Status |
|---|---|---|---|
| D1 | Unicommerce: does the ERP sit **beside** it or replace it? | Beside - Kushal's said it keeps Unicommerce as its order system (loy1 36:32). Our own OMS stays for clients without Unicommerce | **Decided (user):** our own OMS is the order system - *"No need of Unicommerce. But Build Unicommerce Integration."* The integration serves clients who run Unicommerce |
| D2 | Who issues the customer's GST invoice for a Unicommerce order? | Unicommerce; the ERP books it under Unicommerce's number (the 88.3 OMS-bill rule mirrored), so the sale counts once | Decided: as recommended |
| D3 | Who runs e-com purchasing and GRN? | The ERP - one GRN, one cost; Unicommerce is fed by the stock push | **Already in place** - nothing to build |
| D4 | Codes per piece (serials minted at GRN) - for which items and stores? | Opt-in per item type with a store switch, default off; on for the e-com warehouse and high-value types | Decided: as recommended |
| D5 | "One item per bin" - where is it set? | Per Zone with a Bin override, values Mixed / Single item / Single item single lot, default Mixed so nothing changes for existing clients | Decided: as recommended |
| D6 | How handheld work is handed out | An open pool where the first worker to start a task locks it (the client's choice, pwc2 01:16); priority Pick (sales) > Replenish > Put-away > Move > Count; a desktop put-away is still allowed and closes the matching task | Decided: as recommended |
| D7 | Do store transfers join waves? | First make plain-item transfers leave their bins (146.1.3); then transfers join waves by route | Decided: as recommended |
| D8 | Bonus points - on the Offer screen or a separate list? | On Offer, as an Offer type: one place for promotions, one trusted evaluator | Decided: as recommended |
| D9 | Loyalty scope - a dated scheme master or settings plus a store exclusion list? | A dated Loyalty Scheme master with stores; it also gives scheduled rate changes | Decided: as recommended (*"If good bring it"*) |
| D10 | Points as a tender (today) or as a price discount? | Keep the tender unless the client's tax advisor says points must reduce the taxable value; then a per-scheme option | Decided: as recommended - nothing to build |
| D11 | Points on online orders | Not now - the client gives none online today (loy2 34:22); build on the channel the order started in when asked | **Decided (user):** build it as a setting, **on by default** |
| D12 | Moving points between customers | Allow approved, OTP-confirmed transfers; never shared balances (85 D1 / CRM D5 stand) | Decided: as recommended |
| D13 | Shopify metafield naming | Field-map target `custom.<attribute_code>`; definitions created on first publish | Decided: as recommended |
| D14 | Marketplace templates | One template per channel + channel category, loaded from the marketplace's own file | Decided: as recommended |
| D15 | Value maps (AMT GREEN → Amethyst Green) | Their own reusable record, referenced by a transform step | **Decided (user):** as recommended, *"But mapping is diff Marketplace to marketplaces"* - one set of maps per marketplace, used by all of its templates |
| D16 | Channel sign-off ("certify") | A maker-checker sign-off storing a snapshot and hash; no second editable copy | **Dropped (user):** *"This is example ignore it"* - 146.5.8 not built |
| D17 | Ship-from-store limits (per-channel stores, daily cap, distance) | **Now: build** - with D1 our own OMS routes online orders to stores, so these limits apply (orders from Unicommerce keep its facility, 146.4.1) | **Decided (user):** *"yes add all"* - all four limits |
| D18 | License plates at receipt | Only if the client uses physical trays; they said pouches go loose into bins (dyn 34:27) | Decided: as recommended |
| D19 | Weighing scale at pack | Defer until PrintBridge (31.1.9) is live; it is the bridge to a USB scale | Decided: as recommended |
| D20 | When does an approved transfer hold its stock? | At approval - transfers have no separate release step, and approval is when a store is promised the goods | **Decided (user):** *"Not Required. Keep as config to turn on and off. Build it."* - a setting, off by default |
| D21 | Re-inspection by stock age: per category or item; does an open inspection stop selling? | Per category with an item override; no hold by default, optional per rule | Decided: as recommended |
| D22 | Transfer fill policy default | "Ship what is there, close the rest" for replenishment; "Complete" for new-store and launch allocations; per transfer, defaulted from the store | Decided: as recommended |
| D23 | Does an allocation upload also approve the transfers? | No - it makes Drafts, then one bulk approve, so maker-checker stays | Decided: as recommended |
| D23a | Is an allocation upload all or nothing? | **Changed by ask 237.7 (the user, 2026-11-18: *"if some issue upload/load the correct download incorrect one in separate file with reason"*):** the good rows make the Drafts, bad rows are left out and download with a reason. Drafts and the bulk approve stay as D23 says | Decided (user) |

## 2. Order of work

| Step | Phase | Why first | Waits on |
|---|---|---|---|
| 1 | 146.1 Fix what is wrong today | Real defects found while reading | - |
| 2 | 146.5.1, 146.5.3 Shopify gets the PIM content; transform rules | The catalogue team's daily work; small and visible | D13, D15 |
| 3 | 146.3.1, 146.3.4 One item per bin, home bin, bin generator and labels | The client's direct ask (dyn 43:42); bin labels unblock scan-confirm (140.12.20) | D5 |
| 4 | 146.2.1-146.2.3 Bonus points, dated scheme with stores, items not payable by points | Festival promotions | D8, D9 |
| 5 | 146.4.1 Real Unicommerce integration; 146.4.4 store limits | The integration needs Unicommerce's API documents; the store limits do not wait for them | API documents (146.4.1 only) |
| 6 | 146.3.2, 146.3.3 Codes per piece; handheld work queue | Larger warehouse items | D4, D6 |
| 7 | The rest of 146.2, 146.3, 146.4, 146.5 | Each stands alone | Its decision |
| 8 | Deferred: 146.3.9, 146.3.10, 146.3.16 | Only when a client asks, or PrintBridge is live | - |

## 3. Phases

### 146.1 Fix what is wrong today
**Why:** each was found while checking a recording and re-checked in the code; the ERP or its guide says one thing and does another.
- **146.1.1 Tiers re-evaluated daily and before the earn.** `RecomputeLoyaltyTier` has one caller, the earn path, which runs after the multiplier (`engines/loyalty.go:251`, `:277`). A lapsed customer keeps the tier until the next bill, and that bill earns and gets tier-only offers at the old tier, though ADMIN_GUIDE says ageing spend lowers a tier. Fix: a daily CRM job, re-evaluate before the multiplier, a timeline event per change. S.
- **146.1.2 Transform rules run on blank values.** `engines/connector.go:202` applies a rule only when the value is non-empty, so `default_if_empty` can never fire on an export. S.
- **146.1.3 Plain items leave their bins on transfer dispatch.** Untracked lines skip the bin step (`engines/transfer_orders.go:400`); a binned warehouse would show bin stock above on-hand. Confirmed by reading every path (pack, pick task, reconciliation): nothing takes them off, so put-away of new receipts of that SKU can be refused and picks sent to empty bins. A test reproduces it first; after release, `BinStockReconciliation` per client lists the over-stated SKUs for a count. S.
- **146.1.4 Reorder counts stock already on order.** The suggestion uses `available` only (`engines/planning.go:117-147`), so the next run re-proposes what is on an open PO. Position = available + inward / QC-pending + open PO + in transit − waiting back orders; add min, max and order multiple to `ReorderPointConfig`. Stage 148.14's reorder-driven drafts point here. M.
- **146.1.5 A short pick holds the missing pieces.** `ShortPickLine` only releases the reservation, so the missing pieces stay sellable. Move them to a "not found" hold, out of ATP, and raise a count task for the bin. S-M.
- **146.1.6 Unicommerce: the guide and screen say what it does.** The worker only logs "Would push" (`engines/unicommerce.go:320-334`) while ADMIN_GUIDE and the Integrations blurb (`public/view-admin.js:2033`) say it sends stock and orders. Correct the text now; the real connector is 146.4.1. S.
- **146.1.7 An approved transfer holds its stock** (D20). Approval reserves nothing and `computeATS` has no pending-transfer input (`engines/inventory.go:483`), so online sales can take the units and the transfer then fails at dispatch - the client's own question for a merged DC (inf 49:58). Reserve at approval, consume at dispatch, release on cancel or a lowered quantity; an old unreleased hold shows on the Warehouse Cockpit. A setting, off by default (D20). S-M.
**Done when:** each has a Go test that failed before the fix, and the guide text matches.

### 146.2 Loyalty depth (D365 Commerce loyalty)
**Why:** D365 runs loyalty as dated, store-scoped schemes with earn and redeem rules by category or product; ours is one tenant-wide rate on the bill amount.
- **146.2.1 Bonus points as an Offer type** (D8). Extra points per ₹ on a category / product group / SKU list, per unit, or per bill; validity window, tier and stores; base points always apply; stackable or highest-priority wins; follows pending and expiry; the till previews "you will earn N". Extends `engines/pos_offers.go` (Gift With Purchase is the precedent); `EarnLoyaltyPoints` writes one ledger row per rule. L.
- **146.2.2 Dated loyalty scheme with stores; the store on every ledger row** (D9). A Loyalty Scheme master (valid from / to, stores, spend per point, value per point); no two schemes overlap for a store; a sale earns under its own store's scheme on the bill date; additive `location` on `loyalty_point_ledger`. M.
- **146.2.3 Items that cannot be paid with points.** A flag on the item or product group; the redeemable cap is the net of eligible lines (`quoteLoyalty`, `engines/pos_quote.go:628`); the points tender is spread over eligible lines only. S-M.
- **146.2.4 A point worth less than ₹1.** `loyalty.redemption_paise_per_point`, read by `loyaltyPointValuePaise`; points stay whole. S.
- **146.2.5 Block a member; tags that never earn; offers by customer tag.** Blocked = no earn, no redeem, balance kept, reason audited; tags such as Staff earn nothing; checks at `EarnLoyaltyPoints` and `RedeemLoyaltyPointsTx`. S.
- **146.2.6 Approved points transfer between customers** (D12). Usable points only; source confirms by OTP; approval above a value (approval engine, like `LoyaltyPointAdjustment`); the target keeps the source points' expiry; daily cap. S-M.
- **146.2.7 Birthday and anniversary points.** A journey step "give points" (`engines/crm_journeys.go`), once per occasion per year, idempotent. S.
- **146.2.8 Till and history: expiring soon, pending, store and cashier.** `TillCustomerContext` shows expiring-in-30-days and pending; history gets store and cashier; template variables `points.earned`, `points.balance`. S.
- **146.2.9 Points as a price discount** - nothing to build; points stay a tender (D10).
- **146.2.10 Points on online orders** (D11). A setting, on by default; points follow the channel the order started in and stay pending until the return window after delivery. S-M.
**Done when:** a Diwali bonus rule, a store-scoped scheme and an excluded item each show the right points at the till and in the ledger, with a Go test per rule; USER_GUIDE loyalty and ADMIN_GUIDE loyalty settings updated.

### 146.3 Warehouse execution (D365 Advanced WMS, Infor, PwC)
**Why:** the ERP's WMS has the building blocks; the recordings show the rules that make them foolproof for small, look-alike jewellery pieces.
- **146.3.1 One item per bin, and home bins per combination** (D5). A single-item bin refuses a put-away or move of a second SKU, naming both; a SKU's home bins (several, in priority order; "allow other items" per bin) are the first put-away suggestion and show on the GRN / RF line; a full or blocked home bin falls back with a reason. Enforced once in `PutawayToBin` and `MoveBinStock`; home bin reuses `BinReplenishmentRule`; CSV setup. S-M.
- **146.3.2 Codes per piece minted at GRN, on desktop and handheld** (D4). For opted-in items, posting a GRN mints one serial per accepted piece from a number series; stickers print the piece code; manual and minted codes can mix; reprint never re-mints; RF receiving can scan or mint. Extends `serial_tracking.go`, `barcode_policy.go` (a "per piece" mode), the sticker engine. Merges D365's serial generation and Unicommerce's item-level codes. M-L.
- **146.3.3 A work queue on the handheld** (D6). Receipts create pending put-away tasks, waves create pick tasks, "My next task" by type priority; completion closes the pending task; cancelling the source cancels its tasks. Open pool, lock on start; a task can be split into two that add up; the list filters by source document. Engines exist (`engines/warehouse_task.go`, `POST /api/v1/wms/tasks/next`); missing are the emitters and one RF screen. M-L.
- **146.3.4 Bin level and position, a bin generator, bin labels.** Shelf and position fields; generate Aisle × Rack × Shelf × Bin with a code format, idempotent; Code 128 bin labels from the list or the generator; a pick sequence per bin (default code order) drives the pick sort. S-M.
- **146.3.5 Transfers picked from bins, in waves by route** (D7). After 146.1.3: one wave may mix sales orders and transfers for a route or courier. M.
- **146.3.6 Packing list per box.** Package no., weights, dimensions, contents; printable once the package is closed; a `printShapes` entry for `ShippingPackage`. S.
- **146.3.7 Put-away rules by category or design.** Most specific wins (item > category > location), the PackTemplate precedent. S.
- **146.3.8 Inward gate-entry register.** Party, PO, parcels, transporter, LR, vehicle, photo; the GRN starts from it; "no GRN after N hours" report. S.
- **146.3.9 Weighing scale at pack** - deferred (D19).
- **146.3.10 License plates at receipt** - deferred (D18).
- **146.3.11 Re-inspection by stock age (fading polish)** (D21). A rule "category X sitting N days since receipt is re-inspected"; a daily job raises inspections per location, one per age cohort; outcomes pass, re-polish on a job-work order, markdown flag, or scrap; a "due in 7 days" report. Ages from the per-receipt-date barcode where known. Shares `InspectionPlan.applies_to` with **148.33** - build them together. The client raised it twice (inf 34:04, pwc1 07:38). M.
- **146.3.12 One allocation upload makes one transfer per store** (D23). Rows of store, SKU, qty; dry run with per-row errors and source shortfalls; an allocation run ID refuses a duplicate upload; Drafts, then one bulk approve. The client's overnight store allocation (pwc2 26:25). S-M.
- **146.3.13 Transfer fill policy** (D22). "Complete" waits until every line can be filled; "ship what is there" closes the rest with a reason code; optional minimum fill % per store. Today a transfer ships exactly its approved quantity or fails. S-M.
- **146.3.14 Inbound-only and outbound-only bin and zone blocks.** Drain a bin for re-slotting while picks continue, or freeze picks while receipts continue; reason and who / when kept. S.
- **146.3.15 Put-away split across bins, and a capacity warning.** The suggestion fills the best bin's room then the next, never mixing a single-item bin; a warning % before the hard capacity block. S.
- **146.3.16 Leave an item out of cycle counts.** Only if the client names such items (packaging, display props). S.
- Quality checks asked for by rule on receipt are **Stage 148.33**, not here (add Infor's per-item "QC mandatory / warehouse may decide" switch there).
**Done when:** each rule has a Go test at its choke point and a Guide run on a scratch server for the screens; USER_GUIDE WMS updated.

### 146.4 Unicommerce integration and store routing
**Why:** our own OMS is the order system (D1); a client that runs Unicommerce still needs a real integration with no double-selling or one-hour stock lag (loy1 40:09).
- **146.4.1 A real Unicommerce integration** (D1, D2). Sellable stock pushed per Unicommerce facility on change (delta, never negative, buffer off); orders arrive with code, channel, customer, address, prices, tax and facility, are reserved there and never re-sourced; status and cancellations go back; Unicommerce's invoice is booked under its number; returns and RTO create the return and credit note into QC. Built as an `OmnichannelConnector` (outbox retries, health, credentials, the worker Shopify uses); the stub worker and dead `SyncUnicommerceInventory` (103.4) retired. **Needs Unicommerce's API documents before scoping.** L.
- **146.4.2 Vendor SKU and pack multiple; one PO per vendor from proposals** - built inside **Stage 148.6** (vendor price agreements), not separately.
- **146.4.3 Back-order list that feeds buying** - built inside **73.4.2**.
- **146.4.4 Ship-from-store limits** (D17, decided: build all four). Which stores serve which channel, a per-store online switch, a daily cap per store, real distance from a PIN-code master instead of the numeric PIN difference (`engines/sourcing.go:261`), a location priority; each rule hard or soft, in `qualifyingLocations`. M.
- **146.4.5 Marketplace listings: pull, bulk link, per-listing stock-sync switch.** For channels the ERP connects directly. M.
- **146.4.6 Small purchasing conveniences.** (a) approval reminder then escalation; (b) a vendor invoice dated before its PO warns and is reported; (c) "stickers for this PO" for vendor pre-labelling; (d) notify on a GRN with rejections; (e) default return store per channel; (f) email when a long import / export finishes. S each.
**Done when:** a recorded Unicommerce order runs end to end on a sandbox (stock push, order, cancel, invoice, return) with no double count in GST reports.

### 146.5 PIM (Unbxd, Saastify)
**Why:** the client's catalogue team works in Unbxd daily; these are the steps they still do by hand or across three imports.
- **146.5.1 Shopify gets the approved PIM content** (D13). `publishShopifyOne` sends only the design name, family description, brand and HSN (`engines/shopify_sync.go:442-462`); the PIM payload is built only for the log (`engines/pim_publish.go:525`). Send approved title, description, SEO, tags, product type and attribute metafields; design values on the product, combination values on the variant; re-sync images when they change. M.
- **146.5.2 Marketplace templates from the marketplace's own file, per category, with validation** (D14). Load Amazon / Myntra category XLSX into a template with columns, mandatory flags and accepted values; coverage counts; conditional mandatory; a bad value skips that item with column and reason; parent rows where the variation is clean. `ReadXLSX`, `ChannelFieldMap`, `ChannelValidationRule`, `RunPIMExport`. L.
- **146.5.3 Transform steps: value map, join, constant, pad, regex, strip HTML, split, first non-empty** (D15). New steps in `pimTransformFunctions`; a value-map record keyed by marketplace (attribute, our value, their value), so each marketplace keeps its own names and every template of that marketplace uses them. S-M.
- **146.5.4 One wide product sheet import.** Item fields + attributes + content in one sheet, one preview, one commit; unknown headers listed; blanks do not erase unless asked. Built inside **47.13.1**'s staging pipeline. M.
- **146.5.5 Design-level values, grid entry, bulk set.** A design's value applies to its combinations unless overridden; a grid of product group × attribute group; set an attribute or add / remove a tag across a group. M.
- **146.5.6 Feed runs: filtered, recorded, really emailed.** Template + product group + "ready only" + format + destination; every run stored with skipped items and reasons; email through the real SMTP sender (today only logged, `engines/outbox.go:185`). M.
- **146.5.7 Dynamic product groups by attribute value.** Conditions on attribute or field with a typed operator list. S.
- **146.5.9 Image positions per channel and a stable public URL.** Positions from the file suffix; a per-channel main image; `image_url_1..8` feed columns; the URL decision already open under 140.12.24. M.
- **146.5.10 Tasks with an attribute checklist.** A task lists the attributes to fill for a product set and completes itself when they are filled. S-M.
- **146.5.11 Attribute types and checks on save.** Yes / No, Multi-select, Long text, URL; every value checked against its definition at `ValidateDocument`. S-M.
- **146.5.12 Typed product relationships.** Similar, complete the look, alternative, upsell, cross-sell, add-on; CSV; to Shopify after 146.5.1. M.
- **146.5.13 Small PIM items.** Attribute fill-rate report; multi-SKU paste search; "live on" channel links; Amazon product type per category; brand vocabulary check. Attribute-group edit rights are **47.13.4**. S each.
**Done when:** an approved product reaches Shopify with tags and metafields, an Amazon category template exports only valid items with reasons for the rest, and the catalogue steps are in USER_GUIDE PIM.

## 4. Not adopted

| Vendor feature | Why not here |
|---|---|
| Physical loyalty cards, card numbers and card types; several programmes per customer; several point currencies | The phone is the member (85 D1); one point currency keeps the till and SMS simple; spend-based tiers already do a ranking point's job |
| D365 Marketing / Customer Insights CDP, AI segments | Already ruled out (CRM plan §4); segments, RFM, CLV, journeys exist |
| Stock-holding receive / pack / bay-door locations with "work" moves between them | We model these as states (unbinned, in pick, package, loading); same answer, fewer scans |
| Location check digits, mixed-batch / mixed-status rules per location, SSCC / GS1 container labels | Pharma and pallet features; bin labels plus scan-confirm cover jewellery |
| A no-code mobile menu / step designer | A framework of its own; RF screens stay hand-built |
| Per-item-group costing method, store price groups | Company-wide moving average is deliberate; MRP-printed prices are the same in every store (148 keeps the item-group policy question) |
| Unicommerce's "purchase cart", separate put-away documents per source, D365 transfer order per online order, DOM as a batch job | Our proposal approval, one derived put-away list and synchronous allocation do the same with fewer documents |
| PIM AI generation and credits, script transforms, "AI map", collections editor, ticketing, SFTP, Drive / n8n flows | Local-only decision 26.4.11 (D8), 36.5.4, and the first principle; Shopify builds collections from tags and type |
| Channel sign-off ("certify") | The user, 2026-11-01: only an example in the training (D16) |
| Zero-padding internal codes into EAN | Wrong practice; internal ranges must be blocked from external product IDs |
| Infor OS / ION hub; project pegging, Kanban, EOQ, season safety stock; floor-space capacity; shelf life; batch numbers from a series | One application, nothing to federate; the client is trading-only and reorder points cover retail; per-receipt-date barcodes already give ageing by arrival |
| Customer × item sale blocks; one-click approval from an e-mail link; "zero receipt" / "confirm as final" warehouse switches | A B2B / EDI construct; a bearer approval link is a phishing surface (web push and the inbox cover it); the GRN steps already fix this behaviour |

## 5. Guardrails

- Every item extends the named engine, doctype or choke point; no parallel second way of doing the same thing.
- Schema changes are additive migrations; new screens are lazy view modules.
- Approval-gated flows (points transfer, sign-off, blocks) use the maker-checker engine; reports use `ReportDefinition`; uploads use `BulkImportCSV`.
- No new dependency; Unicommerce and Shopify are plain HTTPS.
- Points stay separate from money; a bill or return is never edited by loyalty.
- Each item updates USER_GUIDE / ADMIN_GUIDE in the same change, and anything a guide promises is tested.
