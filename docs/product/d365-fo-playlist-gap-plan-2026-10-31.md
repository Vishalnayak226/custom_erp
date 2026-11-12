# D365 F&O playlist: what the ERP should do, smarter (Stage 148, 2026-10-31)

User, 2026-10-31: *"Can you learn full playlist and prepare a checklist to update in erp. No copy, We need make it smarter thats all"* - playlist "Microsoft Dynamics 365 Finance & Operations for Beginners (D365 F&O Simplified)", 138 episodes (Ep 1-139, no Ep 114), 55 h.

**2026-11-01, the user:** *"No one yet but you need to build it for me, I will give to clients who needs it. So, Make plan in checklist to build"* - so every item is built; what only some clients need is switched on per client (see **Build plan**).

**How it was read:** the user approved downloading the English auto-captions (yt-dlp, outside the repo, `Downloads\d365-subs`); 136 of 138 were readable - Ep 15 (vendor fields part 1) and Ep 26 (total discount) were captioned as Hindi, so only their titles count. Eight batches of ~17 episodes were summarised episode by episode (notes kept outside the repo), every D365 feature was checked against this repo (four code sweeps, file:line evidence in the tracker items), and each finding the videos added was checked again in the code before it went in.

**What exists today:** the core cycles are solid - PO → GRN in steps → 3-way-matched bill → payment, RFQ with award, return to vendor, transfer orders with discrepancy reasons, adjustment / issue / kit / count journals with reasons, batch + serial with purity and weights on the batch, holds, multi-entity GL with FX revaluation, journals with maker-checker, GST by place of supply, bank reconciliation. **Missing or thin:** vendor price agreements and PO discounts, charge codes, B2B pricing, credit control on orders, accounts by item / customer / vendor group, bills without a PO, vendor advances, netting, rule-driven quality, GST credit eligibility and value-band rates. **Two numbers are wrong today** (Phase A).

**The idea in one line:** take D365's *outcomes*, not its screens - one rule per thing, filled in automatically, with a "why" next to every number the system chose, and a second person only where money or fraud is at stake. The videos also show where D365 fails silently (a parameter off, "Calculate" not pressed, an exclusive upper bound) - every rule here says on the line when it did not apply, and why.

**Split with Stage 146** (session erp-99, Dynamics recordings): warehouse execution, the quarantine *location* flow, loyalty, OMS / channel inventory and PIM (category tree with inherited attributes, product life-cycle status that stops reorder) are planned there.

## Decisions (taken 2026-11-01: all as recommended)

| # | Decision | Recommended | Not chosen, and why |
|---|---|---|---|
| D1 | Cost method (148.1) | Bulk items: moving average of the stock **in hand**, per item, company-wide (store transfers stay cost-neutral). Serial- or batch-tracked pieces: each carries its own receipt cost | D365 FIFO / standard / inventory-close job; one average across jewellery pieces that differ by weight and purity |
| D2 | Which buying price wins (148.6) | Most specific rule wins: vendor + item → vendor + design → vendor + category → vendor (all) → item default rate; a tie goes to the latest valid-from. The PO line shows the rule it used, or why none matched | D365 "find next" flags - nobody can say which price applied |
| D3 | How a charge goes into cost (148.8) | Per charge code: by value (default), by qty, or by weight (jewellery profile); allocated when the GRN posts; the bill trues it up | A landed-cost voucher raised by hand after the GRN (today) - it is forgotten, so freight never reaches cost |
| D4 | Where credit is checked (148.3) | Warn when the order is accepted (Credit hold), block at invoice post for customers flagged "stop"; exposure = open receivables + open orders + this order | Invoice post only (today) - an over-limit order is picked and shipped first |
| D5 | Memo / consignment stock (148.16) | Build it, as module `memo_stock`: a vendor-owned bucket at zero value; the sale raises the vendor bill | A separate consignment warehouse and ownership-change journal (D365) |
| D6 | Vendor access (148.18) | A signed PO link (14-day expiry, no login): the vendor proposes qty / date changes and uploads the invoice; the buyer applies them | A full vendor portal with accounts |
| D7 | B2B selling depth (148.30-148.32) | Build all three; summary invoicing and drop-ship as module `wholesale`, incentives as module `sales_incentives`, off by default | Switching them on for every client - a retail shop gets screens it never uses |
| D8 | Period lock after GST filing (148.25) | Yes: once a month's GSTR-1 is marked filed, sales documents of that month lock; changes go through a credit / debit note | Per-module open / on-hold / closed grid (D365) |
| D9 | AI access (148.37) | Build it read-only, as module `ai_access`, through the public API's scopes; anything an agent creates is a draft a person submits | An agent that posts documents itself |
| D10 | Goods before a PO (148.38) | Allowed with a reason; the retro PO goes to approval flagged "received before PO", within N days | Refusing it (people receive anyway and key the PO late, with no trail) |

## Build plan

Every item is built. What only some clients need is **off by default and switched on per client**: a feature with its own screens is a module switch (`SetModuleEntitlement`, Platform Console → tenant modules); a behaviour inside a screen is a setting (`RegisterSetting`, Configuration, also the Setup Workbook). No client sees a change until it is switched on.

| Wave | Items | Size | Depends on | Done when |
|---|---|---|---|---|
| W1 Numbers right | 148.1-148.5 | L | - | Phase A's "done when"; the deploy asks YES (148.1 seeds stock value once) |
| W2 Buying core | 148.6-148.12 | L | W1 (148.1, 148.4) | A PO prices itself from an agreement, takes discounts, free qty and charges, and lands the right cost |
| W3 Buying, more | 148.13-148.15, 148.17, 148.18, 148.34, 148.38-148.40 | L | W2 | Scorecard, requisition lines, advances, rebates, vendor link, arrivals, goods before a PO, approval rules, attribute correction |
| W4 Finance and GST | 148.19-148.25, 148.42 | M | W1 (148.4) | An expense bill, dimensioned journals, set-off, GST credit and value bands, the filing lock |
| W5 Selling | 148.26-148.29, 148.41 | L | W1 (148.3), W2 (148.6) | B2B orders price themselves, promise dates, returns draft the next document |
| W6 Quality | 148.33 with 146.3.11 | M | - | A GRN lot waits for its inspection by rule |
| W7 Optional modules | 148.16, 148.30-148.32, 148.35-148.37, 148.43 | L | W2, W5 | Each works with its switch on and is invisible with it off |

Each wave is one build run: claimed in the tracker first, built, tested (go test, a browser pass), the guides updated, then released on its own.

**Per-client switches**

| Switch | Kind | Items |
|---|---|---|
| `memo_stock` | module | 148.16 |
| `sales_incentives` | module | 148.30 |
| `wholesale` | module | 148.31, 148.32 |
| `custom_orders` | module | 148.36 |
| `ai_access` | module | 148.37 |
| `procurement.vendor_rebates` | setting | 148.17 |
| `procurement.vendor_link` | setting | 148.18 |
| `procurement.goods_before_po` | setting | 148.38 |
| `finance.lock_after_gstr1` | setting | 148.25 |
| `inventory.pack_units` | setting | 148.35 |
| `sales.brand_restrictions` | setting | 148.43 |

Everything else is always on: it changes nothing until someone uses it (an agreement, a charge code, a rule).

## Phase A - get the numbers right (first; small, high value)

**Why:** two figures the business relies on are wrong today, and two finance basics are fixed in code.

- **148.1 Cost follows the stock in hand.** `item_cost` keeps the *lifetime* qty and value received (`db/migrations/migrations_stage37_3_costing_valuation.sql:17-36`, `engines/costing.go:61-69`), and sales COGS uses that rate (`engines/sales_invoice.go:204`). Example: 1,000 units at ₹100 received and sold, then 10 at ₹200 - the rate is ₹101, not ₹200. A landed charge is also spread over every unit ever received. Build (D1): on-hand qty and value per item, average recomputed at receipt, issues at the rate; serial / batch pieces at their own receipt cost (a return or credit note reverses that cost - no "marking"); a bill or late charge that differs from the GRN adjusts the cost of the units still on hand and posts the sold share to a COGS variance at bill time (gold rates move daily - no month-end close); count gains and losses at the current average, never the last price (D365's default, Ep 73); the bill is matched per line - unit-price tolerance per item group, over-billing only, plus a weight tolerance in grams for bullion vendors. One migration seeds on-hand value = on-hand × today's rate; posted history is not restated. A control total compares GL 1200 with stock value.
- **148.2 Channel shipping charge on the invoice.** Shopify and marketplace orders store `shipping_charge` (`engines/shopify_sync.go:902`, `engines/marketplace.go:182`) and nothing bills it, so a paid shipping fee is missing from the tax invoice and from GST. Build: the order invoice carries it as a charge line at the order's GST rate. Check first: does any client store charge shipping?
- **148.3 Credit control on the order.** Orders and invoices hold the customer as a typed name and the credit check matches by name (`engines/budgeting.go:131-141`), only at invoice post (`engines/sales_invoice.go:73`). Build (D4): a customer link beside the name (additive column, back-filled by exact name match); exposure checked at accept → `Credit` hold (existing hold codes, `engines/orders.go:35-46`), released through the approval inbox; every hold shows who is working on it and since when; payment terms default from the customer; a "one-time customer" flag keeps walk-in buyers out of exposure and history.
- **148.4 Accounts by item / customer / vendor group.** Posting profiles exist for 16 events (`engines/posting_profiles.go`), but revenue, COGS, inventory, receivables, payables and GRN suspense post to fixed accounts. Build: those events go through `ResolvePostingAccounts` with today's account as the default rule.
- **148.5 Chart of accounts from the app.** Accounts arrive only by migration. Build: add / rename / block an account, a parent for report grouping, a "no manual posting" flag on control accounts; maker-checker.

**Done when:** the ₹100 / ₹200 example gives ₹200 in a test; a serial piece sells at its own cost; a Shopify order with shipping prints it on the invoice with GST; an over-limit order lands on Credit hold; a posting rule moves one item group's revenue; a new account is added from the screen.

## Phase B - buying smarter

**Why:** buying is the busiest cycle for the current clients (Stage 143 MINN purchase file), and its prices and charges are typed by hand.

- **148.6 Vendor price agreements.** Reuse the dated, approved `PriceListVersion` engine (sales-only today) for vendors (D2). A row: vendor + item, design or category (one row covers a whole category - D365's discount groups without a group master), valid from / to, qty breaks with inclusive min and max (D365's upper bound is exclusive - "1 to 100" gave nothing at 100, Ep 21), line discount %, the vendor's own item code (printed on the PO, matched in PO uploads), optional "count qty across the whole PO" (D365's multiline discount as one flag), optional committed qty or value with a progress bar on the PO. Saving refuses gaps and overlaps. The PO line shows "₹1,240 · VA-12, 50+ break" or "no agreement: expired 12 Oct". Alert 7 days before expiry. The row also carries the vendor's pack multiple, and approved supply proposals become one Draft PO per default vendor (Stage 146.4.2 points here).
- **148.7 PO discounts and free qty.** Line discount (% or ₹) and an order discount spread over lines; two discounts compound (20 % + 30 % = 44 %, Ep 20), stated under the line. Scheme free qty as explicit "buy N get M" with dates, recalculated on every qty change (D365 needs a button, Ep 35); free units received at zero so the average sees the real unit price.
- **148.8 Charge codes that cost themselves.** One charge-code master for buying and selling: GL account, GST rate, payee (the vendor, or a third party such as the carrier - parked in a clearing account until that bill comes), into item cost or not, allocation basis (D3) and scope (stocked lines only, include / exclude a line), repeat on every partial invoice or once. Accounts are checked when the code is saved, not at invoice (Ep 96). Auto charges by rule (vendor group × item group) post straight to lines; per-vendor freight terms say who bears freight and insurance (FOB / CIF, imports). At GRN post the cost charges are allocated through the existing landed-cost engine; the freight bill trues up the estimate; a return raised from a receipt reverses that receipt's charges pro rata (D365 leaves it manual, Ep 54); inter-store freight adds to the receiving store's cost. A line that got no charge says why.
- **148.9 Vendor master basics.** Hold (no new PO / no payment / both), vendor group, default payment terms (with "end of month" and early-payment discount days and %; the payment run picks the discount date), TDS section on the vendor, MSME flag capping the due date at 45 days (MSMED Act, section 43B(h)).
- **148.10 Vendor changes wait for approval.** Today edits apply at once and an alert fires afterwards (`engines/security_detection.go:162`). Build: the fields that need approval are a list (bank, GSTIN, payment terms, credit limit, group); the change waits with a before / after view, the live values stay in force until a second person approves, and payments to that vendor pause meanwhile.
- **148.11 PO line control.** Delivery instalments per line (qty + date each, received separately, the PO open until all are in; dates feed 148.34); stop one line with a reason; vendor confirmation no. and date; under / over % defaulted from the item, changeable on the line and read live (D365 copies it once, Ep 62), also on transfers; a "full delivery only" flag; receiving at least (100 - under %) closes the rest; a warning on a vendor challan no. already used this year; a transfer whose lines are all cancelled shows Cancelled; a weekly overdue-lines list with "close rest".
- **148.12 Purchase price kept current.** The bill, after matching, sets the purchase rate (the GRN price is provisional - D365 does the same, Ep 40); every change is kept in history; a jump above X % asks for a reason; a printed MRP never changes by itself.
- **148.13 Vendor scorecard from the data.** On-time %, fill rate, QC reject %, price variance, bill mismatches - computed; shown on the vendor and in the RFQ comparison (73.4.1 "supplier scorecard").
- **148.14 Requisition with item lines.** Item lines, each with its own store; "combine open requests for this vendor" makes one PO with each line traced to its request. Reorder-driven drafts: 146.1.4 (reorder counts stock already on order).
- **148.15 Vendor advance against a PO.** The advance posts to its own asset account (not payables); the bill stays at its gross amount and a settle line applies the advance (Ep 123).
- **148.16 Memo / consignment stock.** Module `memo_stock` (off by default): a vendor-owned bucket at zero value, excluded from our cost; the sale line raises the vendor bill; no ownership journal.
- **148.17 Volume rebates.** Setting `procurement.vendor_rebates` (off by default; closes the rebate part of 73.4.1): a target on the vendor agreement; one claim per vendor per period - calculated → approved (accrual) → settled (debit note); two tiers; after 148.6.
- **148.18 Vendor PO link and follow-up.** Setting `procurement.vendor_link` (off by default): the signed link; the vendor's changes arrive as a qty / date proposal the buyer applies line by line (auto-accept only for trusted vendors); an applied change re-opens confirmation; a vendor's unit name ("pieces" = Nos) is remembered per vendor; an uploaded invoice waits for buyer review; the buyer can key an offline vendor's reply on the RFQ comparison; overdue lines send a reminder carrying the link. Rule-based, no AI (D365's supplier agent, Ep 139, only *proposes* changes too).
- **148.38 Goods received before the PO.** Setting `procurement.goods_before_po` (off by default). Today a GRN must link a PO (`engines/grn_pricing.go:191`); switched on, receive with a reason code; the retro PO goes to approval flagged, within N days (Ep 31).
- **148.39 PO approval rules beyond amount.** Approval bands route by amount only (`engines/approval.go`). Build: a rule table - amount band + vendor group or category → role; escalate after N days; a delegate (Ep 41-42). No visual workflow editor.
- **148.40 Correct a piece's attributes.** Purity, karat and weights are captured at the GRN with no correction path. Build: "correct attributes" on a batch or serial (weight after hallmarking, karat re-test) with a history row; its purchase and sale links stay (Ep 67).

**Done when:** a PO line fills its price from an agreement and says why or why not; 10 + 1 free gives the right average; a PO freight charge lands in item cost at GRN post and its return reverses it; a bank change waits for a second person; a line closes itself within its under-delivery %.

## Phase C - finance and GST

- **148.19 Bills without a PO.** A `VendorInvoice` needs a PO and a GRN (`db/migrations/migrations_stage17g_vendor_invoice.sql:9-10`). Build: an expense bill - lines by expense category with the vendor's default accounts, split by cost centre at entry, GST input with an eligible / blocked choice per line (148.42), TDS, approval by amount; a PO can be linked later; it pays like any bill.
- **148.20 Journal lines with dimensions.** Dimensions apply to a whole posting today (`engines/finance.go:47-80`). Build: per-line cost centre / department / store, narration and tag; a required-dimension table by account range with one exception list (salary needs department and employee); a dimension value is suspended, never deleted.
- **148.21 Journal templates.** Save as template (amounts or % split), "copy last month"; show the recurring templates the Journal screen hides today (`public/view-journal.js:46`).
- **148.22 Free-text invoice lines to their own account.** Non-stock invoice lines always post Cr 4100 (`engines/sales_invoice.go:167-176`). Build: income account and GST per line.
- **148.23 Customer-vendor set-off.** Pair a customer and a vendor (suggested when PAN / GSTIN match); a set-off voucher posts two legs through a clearing account, with approval, a reversal date and a printed statement; suggested when both balances are open.
- **148.24 Screens for API-only features.** FX revaluation (dry run → post) and intercompany entry exist but have no button.
- **148.25 Lock a month after GST filing.** Setting `finance.lock_after_gstr1` (off by default): sales documents only, plus an On-hold period status.
- **148.42 GST credit eligibility and value bands.** No credit-eligibility or value-band logic exists (`engines/gst.go`). Build: an eligible / blocked choice on each purchase line, defaulted from HSN or vendor (blocked GST, section 17(5), goes into cost or expense, not input credit); an optional value band on an HSN rate row, so an apparel or footwear line takes its rate from its price (Ep 136). Value bands only matter for apparel / footwear clients.

## Phase D - selling

- **148.26 Pricing reaches B2B.** Sales orders, direct invoices and CRM quotes take typed rates. Build: the POS resolver fills the line from the customer's price list and qty breaks and shows why; jewellery prices keyed by karat and weight band per design group, not one row per SKU; a quota on a price-list line (D365's sales agreement as a counter); the existing 9 / 49 / 99 ending rules apply to price-list rows with a preview; changing an agreement flags the open lines it affects; the quote shows margin while it is priced; a rate below the floor asks for a reason or puts a Margin hold.
- **148.27 Promise date on order lines.** Stock on hand ships today; otherwise confirmed PO lines (148.11) and transfers in transit, plus courier days, skipping closed days, with a cut-off hour per store and channel; the reason is shown ("2 in Mumbai, 3 on PO-123 due 3 Nov"). A late-lines report by item and customer with days late. Covers the ATP part of 73.4.2; the back-order list is 146.4.3.
- **148.28 Returns drive the next document.** One return document with its RMA no. at creation and a required reason; a line taken from the invoice carries its cost; a restocking-fee rule (a charge code on the disposition); dispositions draft the next document - credit, replace, return-to-vendor, scrap (to quarantine). A credit note on stock items needs a posted return receipt (D365's direct route adds stock with no receipt, Ep 115).
- **148.29 Quote expiry.** `valid_until` is saved but never enforced: expire the quote, remind the owner 3 days before.
- **148.41 Reserve a piece for a customer.** Hold a piece for a named customer without an order - optional advance, expiry - so the till and picking skip it. Check the serial `reserved_for` field (`engines/serial_tracking.go`) first; it may cover part of this.
- **148.30 Sales incentives.** Module `sales_incentives` (off by default): % of margin after discount by category with slabs, accrued at invoice, split between salesmen by %, returns claw back, each salesman sees their own earnings in the store app; payout via payroll.
- **148.31 One invoice per customer per period.** Module `wholesale` (off by default): one optional bill-to customer on the customer (receives the balance and the invoice); grouping keys bill-to, currency, place of supply, customer PO - set once per customer, never a silent split; a delivery challan per address; instalment dates on a line.
- **148.32 Drop-ship.** Module `wholesale`: an item flag with a default vendor; saving the order raises the PO to the customer's address; qty and dates stay in step; the vendor's confirmation ships the line; one confirm-all list.

## Phase E - quality by rule

- **148.33 Inspection asked for by rule.** Inspections are manual; only production has a gate. Build: "quality required" on the item type, plus vendor rules (new vendor, reject rate above X %); the whole GRN lot is blocked while the sample is tested, the sample sized from the received qty; reusable tests with numeric limits and one acceptance %; result lines default to Fail and must add up to the received qty; a karat or weight result writes to the batch; a fail blocks sale but still lets the goods move to quarantine (location flow: 146); the vendor's bill waits until the lot is released; a vendor with N clean lots in a row is sampled less; results entered on a phone through the store app; per item "QC mandatory / warehouse may decide"; a Before mode puts the matched qty into qc_hold inside `PostGRNReceiptPart` instead of refusing the GRN (from the Infor recording, Stage 146). Re-test dates for dated goods as a report only; built together with 146.3.11 (re-inspection by age, same `InspectionPlan.applies_to`).

## Phase F - stock and product

- **148.34 Expected arrivals.** One list per store and day: PO lines and instalments due, transfers in transit, late ones first; open it by PO or transfer no. and receive part of it there. A changed PO date re-opens confirmation instead of dropping the line from the list (D365 drops it, Ep 71).
- **148.35 Buy in packs, stock in pieces.** Setting `inventory.pack_units` (off by default): an optional purchase unit + factor per item (piece-to-gram per item, never global); the stock unit locks after the first movement; price per stock unit.
- **148.36 Custom-order configurator.** Module `custom_orders` (off by default). No metal-rate master exists yet, so this adds one (rate per metal and purity per day). Fixed choices (metal, purity, stones, size, making type) and a small rule table that adds the setting and stone lines; price from the day's metal rate + making charge.

## Phase G - AI

- **148.37 Read-only access for AI assistants.** Module `ai_access` (off by default): stock, order status and reports through the public API's scopes, acting with the signed-in user's rights; an agent never posts - what it creates is a draft a person submits (the D365 demo turns an email into a sales order with no approval, Ep 138).
- **148.43 Brand-restricted dealers.** Setting `sales.brand_restrictions` (off by default): an allowed-brand list on the customer (none / warn / block, optional dates) checked by the order line (D365's approved customer list, Ep 134).

## Not adopted

| D365 feature (episode) | Why not |
|---|---|
| Marking (66) | Serial / batch pieces carry their own cost and bulk items use the moving average (D1) - the right cost without tying receipts to issues |
| "Find next" flags, generic currency (22, 127) | D2's single precedence rule; a base plus a tier is two rows, both shown on the line |
| Discount groups, multiline / total discount setups (23, 25-26) | Agreement rows by category, "count across the PO", and the order discount (148.6, 148.7) |
| Purchasing policy by category (47) | Approval rules (148.39) and agreements cover it |
| Item model group as a new master (12) | Policy already sits on ItemType / ProductType; 148.33 adds the missing flags there |
| Tax groups × item tax groups (135-136) | GST by HSN rate + place of supply, plus 148.42 |
| Number series per legal entity (2) | Store-scoped series stay unique per GSTIN per year; GST rule 46 allows several series |
| Party model, customer divisions (105-106) | One optional bill-to customer (148.31) covers the payer case |
| Sales agreements as documents (98) | A quota on the customer price-list line (148.26) |
| Configurable batch disposition codes (85) | The fixed batch statuses already block sale |
| Intercompany sales / purchase orders, IC drop-ship (131-132) | Transfers and intercompany journals exist; revisit only if two entities of one client trade by order |
| Virtual entities, dual-write (75, 137) | The doctype and public APIs serve integrations; the floor-app benefit comes through the store app (148.33) |
| Smart rounding on purchase prices (128) | Rounding belongs on selling prices (148.26) |
| Post the good journal lines, park the bad ones (88) | Imports preview every row before an all-or-nothing commit, by design (`engines/import.go`) |

## Guardrails

- Additive schema only; today's accounts, prices and costs stay the default until an admin adds a rule.
- Reuse what exists: `PriceListVersion`, posting profiles, hold codes, the approval engine, the landed-cost engine, the report framework.
- Every number the system fills in shows where it came from, and every rule that did not apply says why.
- Approval only where money or fraud is at stake (vendor changes, credit, below-floor price, set-off, chart of accounts, goods before a PO).
- Items that already sit in 73.4.1 / 73.4.2 close there when built here.

## Episode → item map

| Episodes | Topic | Here |
|---|---|---|
| 1-2 | Legal entity, number sequences | Built |
| 3-5 | Debit / credit, chart of accounts, GL setup | 148.5, 148.25 |
| 6-11, 13 | Products, product masters, attributes, category hierarchy, life cycle | Built; category tree and life cycle → 146 |
| 12 | Item model groups, item groups | 148.1, 148.4, 148.33 |
| 14-17 | Vendor, its fields, payment terms, 3-way tolerance | 148.9, 148.1 |
| 18-23, 25-26 | Purchase trade agreements, discounts | 148.6, 148.7 |
| 24, 27-30, 129 | Charges, landed cost, vendor surcharge, rebate via charge | 148.8 |
| 31 | PO creation (goods before the PO) | Built; 148.38 |
| 32, 36, 39 | PO processing, short close, multiple invoices | Built |
| 33, 58, 93 | Posting profiles | 148.4 |
| 34-35, 37-38 | Delivery schedule, supplementary items, stop line, vendor item code, over / under delivery | 148.11, 148.7, 148.6 |
| 40 | Latest purchase price | 148.12 |
| 41-42 | PO workflow | 148.39 |
| 43-44, 47 | Purchase requisition, consolidation, purchasing policy | 148.14 |
| 45 | Vendor evaluation | 148.13 |
| 46 | RFQ | Built; offline replies 148.18 |
| 48-49, 51 | Purchase agreements, rebates | 148.6, 148.17 |
| 50 | Consignment | 148.16 |
| 52 | Invoice matching | Built; per-line in 148.1 |
| 53 | Proposed vendor changes | 148.10 |
| 54 | Vendor return | Built; charge reversal 148.8 |
| 55-56, 139 | Vendor collaboration, supplier agent | 148.18 |
| 57, 59-65, 68-69, 72-73 | Sites, transfer orders, journals, counting | Built; 148.11, 148.27 |
| 66 | Marking | Not adopted (D1) |
| 67 | Transfer journal | Built; 148.40 |
| 70-71 | Item arrival, arrival overview | 148.34 |
| 74-79 | Quality, blocking, quarantine | 148.33; quarantine location → 146 |
| 80-82, 120 | Financial dimensions, advanced rules, tags | 148.20 |
| 83-87 | Serial, batch, disposition, inspection, batch attributes | Built; 148.1, 148.33 |
| 88-89 | General journals, voucher templates, periodic | 148.20, 148.21 |
| 90-91 | Currencies, FX revaluation | Built; screen 148.24 |
| 92, 101 | Customer, order holds | 148.3, 148.26 |
| 94-96, 99-100 | Sales orders, sales trade agreements, deliveries, units | Built; 148.26, 148.27, 148.35 |
| 97 | Sales quotations | 148.29, 148.26 |
| 98 | Sales agreements | 148.26 |
| 102, 116 | Sales charges, adjust charges on invoice | 148.8, 148.2 |
| 103-104 | Summary update | 148.31 |
| 105-106 | Party associations, divisions | 148.31, 148.3 |
| 107-108 | ATP, lead time | 148.27 |
| 109 | Commissions | 148.30 |
| 110-111 | Direct delivery | 148.32 |
| 112-113, 115 | Return orders, dispositions, restocking fee, replacement | 148.28 |
| 117-118 | Vendor invoice journals | 148.19 |
| 119, 121-122 | Dimension- and constraint-based configuration | 148.36 |
| 123 | Vendor prepayment | 148.15 |
| 124-126 | Physical vs financial value, running average | 148.1 |
| 127-128 | Generic currency, smart rounding | Not adopted; endings in 148.26 |
| 130 | Free text invoice | 148.22 |
| 131-132 | Intercompany | 148.24 (screen); orders not adopted |
| 133 | Customer-vendor netting | 148.23 |
| 134 | Approved customer list | 148.43 |
| 135-136 | Sales tax | Built (GST); 148.42 |
| 137 | Virtual entities | Not adopted |
| 138 | ERP MCP | 148.37 |
