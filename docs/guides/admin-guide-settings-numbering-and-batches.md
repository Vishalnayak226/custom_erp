---
doc_id: DOC-C44AC5B356-02
title: Admin Guide: Settings, numbering and batches
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Admin Guide: Settings, numbering and batches

Part 2 of 6 of the [Admin Guide](admin-guide.md). Previous: [Foundation, users and settings](admin-guide-foundation-users-and-settings.md). Next: [Items, printing and stock](admin-guide-items-printing-and-stock.md).

*Continues [Part B — Day-to-Day Operation](admin-guide-foundation-users-and-settings.md#part-b--day-to-day-operation) › [B.3 Configuring the System (no code required)](admin-guide-foundation-users-and-settings.md#b3-configuring-the-system-no-code-required).*

#### B.3.0.4 Which locations a till may sell from (`Location.sellable`, Stage 53)

Every **Location** record carries a **Sellable** flag (`Yes` / `No`), next to its Type. It answers one question: *may a POS till ring up a sale here?*

It is deliberately **not** the same question as **Type**. A warehouse with a trade counter does sell; a shop being fitted out does not. Type is what the place *is*; Sellable is what it may *do*. Set them independently.

**What it gates — three places, all of them:**

| Where | Behaviour when `Sellable = No` |
|---|---|
| The **POS till** | The till sells from the working store at the top of the screen; a store with `Sellable = No` shows *"… is not a selling store"* on the session line and offers no Open Session. |
| **Open Session** | Refused, with error `POSOFF-0245`: *"… is not a selling location — a POS till cannot sell from it."* |
| **Complete Sale** | Refused with the same error, independently of the session check. |

Checkout is checked separately from session-open on purpose. An **offline-queued sale replays through checkout without ever re-opening a session**, and a Location can be switched to `No` while a session is still open — neither case would be caught by a session-time check alone.

**How an existing location got its value.** The Stage 53.1 migration backfilled every Location that had no value:

1. A location that has **demonstrably sold before** — a `POSCart` or `POSSession` exists against it — was set to **Yes**.
2. Otherwise, a location typed **Store** was set to **Yes**.
3. Everything else was set to **No**.

Rule 1 exists because of a trap worth understanding before you audit this. The Stage 17.9 migration seeded a Location row for every location code already in use with a **hardcoded Type of `Warehouse`** — so on a tenant whose shops were never re-typed by hand, *every* Location is a "Warehouse". A naive "Store sells, nothing else does" backfill would have left such a tenant with **zero sellable locations and every till refusing every sale**. Rule 1 is what makes the migration a no-op for any till that was working the day before it ran.

> **Check this after the migration, do not assume it.** Run through Setup → Core → **Location** and confirm each shop you actually trade from reads `Sellable = Yes`. The backfill is evidence-based, not clairvoyant: a **newly opened shop that has never rung a sale and was typed `Warehouse`** comes out as `No` and will refuse to open a till until you change it. That is a one-field edit, and it is the correct direction to be wrong in — a refused sale is visible and fixable in seconds, whereas a sale silently booked against head office is neither.

**Three more things to know:**

- **An unregistered location code still sells.** A location code that has no `Location` record at all is permitted. Location is still a free-text column on most doctypes (Stage 17.9 deliberately declined to convert them to Link fields), so refusing unregistered codes would break tenants that never populated the master. Only an explicit `No` — or a `Location` record that resolves to "not sellable" — refuses.
- **A blank flag is derived, not ignored.** The field is optional (making it mandatory would have rejected a save of every pre-existing Location the next time anyone edited one). A Location saved without it is **stamped on save** — `Store` → `Yes`, anything else → `No` — so in practice every row carries a real value. That is also what lets the POS picker filter on `sellable=Yes` and get exactly the set the server would accept; a picker that offered a location checkout would refuse is precisely the drift the stamp prevents.
- **An explicit value is never overwritten.** If you set `Yes` on a warehouse, it stays `Yes`.

#### B.3.0.5 What a POS receipt says about your business (Stage 53)

The receipt header is built from **two master records**, not from anything typed at the till:

- **Location** → the shop's name and code.
- **Location → Legal Entity → LegalEntity** → the company name, **GSTIN** and state.

Plus the bill number, the date and time, and the cashier's username (taken from their resolved identity at checkout, never from the browser).

**Fill in the Legal Entity to get a tax-compliant receipt.** Setup → Core → **LegalEntity**, set `name`, `gstin` (the **State** then fills itself from the GSTIN, and **State Code** shows its short code, e.g. KA) and the address lines, then set **Legal Entity** on each selling Location — a Location left without a State takes its Legal Entity's. Until you do, the receipt prints the shop name and bill details but carries **no company name and no GSTIN**.

This degrades rather than failing, deliberately: a missing Location record, a Location with no legal entity, or an entity with no GSTIN each simply leave that line off. A receipt must always print — the alternative is a cashier holding a paid sale and no paper.

Both print paths build the same header: the server-side ESC-POS/HTML rebuild (`engines/qz_payload.go`, used whenever QZ Tray and a Receipt printer are configured) and the browser fallback. If you ever see them disagree, that is a defect — report it.

#### B.3.0.6 Refresh and Reset — what each one actually clears (Stage 53.17)

These are the two buttons at the top right of the shell, and support calls go wrong when they are treated as the same button at two strengths. They are not.

**Refresh** (`sync-btn`) re-fetches labels and the registered-doctype list, then re-renders. Nothing else is touched. It is the right answer to *"I renamed a field and cannot see it"*.

**Reset** (`reset-btn`) clears, in this order:

1. Every `erp_swr_*` entry in **sessionStorage** — the stale-while-revalidate cache behind every record list and field-metadata read. This is the cache that actually makes a screen lie.
2. `erp_nav_state` in localStorage, so the sidebar rebuilds from the fresh doctype list rather than a remembered shape that may name retired screens.
3. The in-memory `state` caches (doctypes, field metadata, fetched rows, labels, permissions), re-fetched immediately afterwards.
4. The POS till's in-memory state, via `clearPOSTillState()` — called through `window` because `view-pos.js` is lazy-loaded (BLD-041) and may not be present at all.
5. **The lazy view modules themselves** — the registry is cleared *and* a `reset=<timestamp>` param is added to the next `import()`, because clearing the registry alone would re-fetch the identical URL straight out of the browser's HTTP cache.

**Tell users plainly that a browser reload is not a substitute.** F5 re-runs the app against the same sessionStorage and localStorage, so the stale copy is read straight back in. Reset is the only path that drops those copies before rebuilding.

**Three deliberate non-actions**, each of which has been asked about:

- **It does not sign the user out.** The token, role and username in localStorage are untouched. Resetting a screen must not eject a cashier mid-shift; that is what Sign Out is for.
- **It does not reset the theme.** A personal preference is not stale data.
- **It refuses to run while offline POS sales are queued.** `erp_pos_offline_queue` holds *completed sales that have not reached the server*. They are not cache, and clearing them would destroy them, so Reset blocks with a message naming the count rather than proceeding — the same posture `closePOSSessionFlow` already takes. Let them sync, then Reset.

> **Deploy note.** Lazy view modules are versioned by `VIEW_MODULE_VERSION` in `public/app.js`, which must be kept **in step with `index.html`'s `app.js?v=N`** on every release. Before Stage 53.17 the lazy modules had no cache-busting at all, so the shell could update while the screens a user actually looks at kept running the previous deploy's code. If you bump one, bump the other.

#### B.3.0.7 Stock locations — what sells, store by store (Stage 85.B)

**Inventory → Stock Locations** (Super Admin). Stock inside a store sits in named locations; this is the master that names them, says which sell and says which each store uses. It is part of the ERP, not the WMS: a client without the warehouse module has all of it.

| Location | What it is | Sells? | Physically in store? |
|---|---|---|---|
| **Main** | the selling stock | always | yes |
| **Inward** | received (goods receipt or transfer in), not yet on sale | never | yes |
| **In-Transit** | sent on a transfer, not received yet | never | **no** |
| **QC Hold** | waiting for a quality check | **your choice** | yes |
| **Damage** | damaged stock | **your choice** | yes |
| **On Hold** | held by a hold code (recall, dispute) | never | yes |
| **Purchase Return** | set aside for the vendor (warehouses) | never | yes |
| your own (*Alteration*, *Display only*, *At repair*…) | stock kept away from sale | never | **your choice** |

- **Names, store kinds, "for new stores" and "in use"** are editable on every location; **+ New Location** adds your own. A location still holding stock cannot be switched off or taken from a store (`STKLOC-0861`) — move the stock out first (**Inventory → Move Stock**).
- **Stores tab:** which locations each store uses. A store never saved here uses every location marked for new stores; Main is always on.
- **Transactions tab:** each transaction's from / to. **Goods Receipt** and **Transfer In** "to" can be **Inward** (goods are checked before they go on sale) or one of your locations; the receipt lands there in the same transaction. The rest show how they work (a sale takes Main, then any location marked sellable; a sales return goes where its QC result sends it).
- **What "sellable" changes:** the available-to-sell figure everywhere (till, online channels, sourcing, reports, the public API) counts Main plus QC Hold / Damage when you mark them sellable, and a sale that is short in Main takes the rest from them. A sale can never take stock held in your own locations or under a hold code.
- **Corrected at the Stage 85 release — available-to-sell rises where you have damaged or QC stock.** QC Hold and Damage were subtracted from the good stock's figure although they were never part of it (a store with 10 good and 2 damaged showed 8 sellable). They are now counted only if sellable. Expect higher, correct figures on the till and the channels for those items.
- **The WMS follows it:** a bin's condition Good / Damaged / QC-Hold / RTV moves the units into Main / Damage / QC Hold / Purchase Return, and the Bin Conditions screen shows the ERP names.
- **Physically in store? (Stage 107):** says whether a location's stock is on the premises. In-Transit never is - it is already left out of the store's on-hand. For your own locations choose **Not in the store** for stock that has left the building but is still yours (at a repair shop, out for alteration): a **stock count** then does not expect it on the shelf, so it is not reported short. It does not change what sells.
- **Reports:** *Stock by Location*.

#### B.3.0.8 The till's customers, tenders, salesmen, bills and advances (Stage 85.C–85.E)

- **One mobile number is one customer.** A second customer with a number already on file is refused (`CUSTOM-0133`); deleted and merged customers no longer count. Customers that already share a number (from before this rule) are listed by *Reports → CRM → Customers Sharing a Mobile Number* — merge them from the Customer master. Nothing is merged automatically.
- **Customer master fields:** Address Line 1/2, City, State (+ code), PIN, PAN, GSTIN, date of birth, anniversary, gender. An Indian GSTIN is checked with its check digit (`CUSTOM-0869`) and fills the State and the PAN; a PAN is checked (`CUSTOM-0870`).
- **Walk-in customers:** each store's first walk-in bill creates its walk-in customer (*Walk-in - <store>*, placeholder number 1111111111, 2222222222 … for the next store). It is never printed on a bill, never earns points, and every bill without a customer is billed to it. Do not edit it into a real customer.
- **Large bills** (Configuration → Point of Sale): *Buyer's address needed from*, *PAN needed above*, *Cash refused from* - amounts (a number box; anything else is refused). Blank uses your home country's rule (India: ₹50,000 to a buyer without a GSTIN, CGST Rule 46(e); PAN above ₹2,00,000 or Form 97, IT Rules r.159; cash of ₹2,00,000 or more refused, s.269ST. UK and Germany: full details from 250; UAE: AED 10,000). **0 switches a rule off.** Enforced at checkout (`POSOFF-0866 / 0867 / 0868`). The cash limit counts the same customer's cash on every paid till bill that day, at any store, not only this bill (s.269ST is per person per day); the walk-in customer is not added up, since it stands for many people. A bill with a buyer GSTIN needs no PAN or Form 97: the GSTIN carries the PAN. The till shows what Complete Sale will ask for under the total (*Cash over limit*, *Address needed*, *PAN or Form 97*, *B2B details needed*) before the cashier gets there (ask 215.4).
- **Tenders:** Setup → Masters → **POS Tender**. Name (*HDFC Card*, *Paytm*), **Kind** (Cash / Card / UPI — this decides the clearing account, 1100 / 1101 / 1102, so nothing changes in the books), *Ask for a reference*, *Opens the cash drawer*, **Stores** (codes, comma separated; blank = every store), order, status. Cash, Card and UPI are seeded. Bills keep the tender's name: *Reports → Sales → Tender-wise Sales*, and the session close lists every tender.
- **UPI QR at the till (185.1):** on each **Location**, **UPI ID for the till QR** is the store's UPI ID from BharatPe, PhonePe, Paytm or its bank (*store@ybl*). A UPI tender can carry its own **UPI ID (blank = the store's)**, for a second provider. With one set, choosing that UPI tender at the till shows a QR for the exact amount; the cashier then types the payment's UPI reference (UTR). A UPI ID must look like name@provider, and only a UPI tender takes one (`POSOFF-0878`). These providers have no public merchant API, so the ERP is not told of the payment: it is checked from the provider's statement (Finance, integrations and go-live guide, B.3.27).
- **Salesmen:** Setup → Masters → **Salesman** (name, store, phone, *Default salesman of the store*, Active / Left), and on each **Location** *Ask the salesman at the till*: Off / Optional / Mandatory (`POSOFF-0876`). *Reports → Sales → Salesman-wise Sales*.
- **Sales incentives (off until you turn them on):** Settings → Modules → **Sales incentives**. Add **Incentive Rule** records (category, from margin %, incentive %; a blank category is the fallback) and one **Incentive Profile** per salesman (salesman, store-app user name, employee). Choose the split on the sales invoice (*Salesman* or *Salesman split*, percentages add to 100; `SALESP-0740`). Each posted sale books Dr expense / Cr accrued incentives (accounts 5500 and 2400 as shipped; change under Configuration: *sales_incentives.expense_account*, *sales_incentives.accrued_account*), once per sale. A later rule change never changes past accruals. Returns (at the till or by a credit note on the return) and cancels claw back in the same split. **Run Payroll** pays the balance as *Incentive pay* for the employee linked in the profile. *Reports → Sales → Sales incentives*.
- **Tax by slab, LWF, benefits and revisions (170.22-170.25):** all off or empty until set up. **A CA must confirm the statutory figures** (slabs, rebate, standard deduction, cess, LWF amounts and months) before payroll relies on them: the FY 2025-26 slabs are seeded as ordinary rows under the Tax & Benefits page, **Tax Slab** and **Tax Regime Rule**, and can be edited or added per financial year; nothing is seeded for **LWF Rate** (state, employee and employer amounts, months such as `6,12`). Settings → HR & Payroll: **Salary TDS by tax slab** (`hr.tds_by_slab`, default off: payroll keeps the flat TDS estimate) and **Labour welfare fund on payslips** (`hr.lwf_enabled`, default off; uses the employee's *Work state*). With the slab setting on and no slabs for the year, Run Payroll stops with HRPAYR-1740. Not covered: surcharge, section 89 relief, other income, HRA exemption, and the FVU file for 24Q (the CSV is data to key in). Benefits and LWF post to **2405 Benefits Payable** and **2404 LWF Payable**. **Salary Revision** and the pay fields are readable by HR and payroll roles only, and the revision uses the normal approval rules (Settings → Approvals). Appraisal calibration needs an **Owner** on the cycle for anyone but HR to calibrate.
- **The cashier's discount** comes off the price (as a % or an amount), shared across the lines like a voucher. *Configuration → Point of Sale → Discount needs a reason above* (5% as shipped; `POSOFF-0877`). Above an Approval Rule slab for POSCart a manager approves, as before.
- **Price lists:** Setup → Sales → **Price List Version** (User Guide §8.3). A version prices nothing until it is approved; who approves is the **PriceListVersion** approval rule (**Settings → Approval Rules**; shipped *any value → Admin*). The till prices from the customer's *Contract price list*, then *Configuration → Point of Sale → Default price list*, then the item's Sale Price, then its MRP.
- **Bill numbers** are issued by the server when the sale completes, per store: **prefix + store Short Code + financial year + 6 digits**, at most 16 characters (CGST Rule 46(b)), drawn inside the sale so a failed sale leaves no gap. Give every store a Short Code of up to 4 characters (Setup → Location) for readable numbers; without one a fixed 4-character tag is used. The prefix is Prefix Configurations → **POSBILL** (*B*); the advance receipt voucher is **CA** and its refund voucher **AVR**. **Offline tills (Stage 76.10):** each till gets its own series the first time its POS screen opens online - **prefix + till number + store Short Code**, e.g. *T01JAY* (till 01, then 02 ... up to 99 per store; prefix is Prefix Configurations → **POSTILL**, one character, default *T*). A sale rung up offline is numbered from it - series + financial year + running number, 16 characters, e.g. *T01JAY2627000001* - shown on the till as **Bill ...**, and it posts under that number when a manager validates it; each till is its own row in GSTR-1 documents issued. A number already on another bill, or not from that store's till series, is refused (`POSOFF-1371`): reject the sale and bill it again online. A till with no series yet (first opened offline, or browser storage cleared) gets the store's next number when the sale is validated; clearing storage gives the till a new till number, never reuses old ones. A rejected offline sale leaves its number unused in that till's series. Each registration is in the audit log as `POS_TILL_SERIES_REGISTERED`.
- **GST (B2B) invoice:** a bill with a buyer GSTIN gets **Print GST Invoice** on the finished bill — the print transaction *Tax Invoice (GST bill from the till)*. Design its layout like any other print (Print Templates). **B2B bills from the till (ask 215.2-215.3):** the cashier types the buyer's GSTIN in the till's **GSTIN** box; the bill becomes B2B and asks the buyer's legal name, address, city, PIN, the buyer's **PO number and date**, and whether the goods are collected or shipped (then a ship-to address and state). The server refuses a B2B bill missing any of these (`POSOFF-0879` buyer, `POSOFF-0880` PO, `POSOFF-0881` ship-to; a wrong GSTIN `CUSTOM-0869`). **Place of supply:** collected at the store, the store's state (IGST Act s.10(1)(a)); shipped on the buyer's order, the buyer's GSTIN state (s.10(1)(b)) - IGST when that differs from the store's. The tax invoice prints the buyer, PO number and date, ship-to, place of supply and *Reverse charge: No* (CGST Rule 46), and GSTR-1 files the bill in B2B under the buyer's GSTIN. **Configuration → Point of Sale → B2B bill: PO number optional** (`pos.b2b.po_optional`, off): on, the PO number and date can be left blank; GSTIN, legal name and address are always needed. A customer with a GSTIN on file still makes the bill B2B as before: the GSTIN box fills from it.
- **Customer advances** (POS → Advance tab) are stored value on the profile *Customer Advance*: receipt Dr tender clearing / Cr **2640 Customer Advance Liability**; used on a later bill of the same customer only; refunded to the tender it was paid by, with a reason. The cashier chooses **Goods** (no GST on the advance — Notification 66/2017-CT) or **Services** (GST at receipt, given back when used or refunded) — **have your CA confirm which applies to your business before go-live.** *Reports → Sales → Customer Advance Register* ties to account 2640.

#### B.3.0.1 Setting up POS offers

Offers are ordinary records, not a code change: create an **Offer** (Super Admin or Store Manager) and it applies at every till on the next sale — there is nothing to deploy, restart, or push to the POS.

Fill in the name, pick the **Offer Type**, set **Applies To**, and set **Status** to Active. The form shows only the boxes your chosen type uses (215.9); a box another type uses is hidden and ignored.

| Offer Type | Fill in | Means |
|---|---|---|
| Percentage Off | Discount % | Takes that % off whatever the offer applies to |
| Flat Off | Discount amount | Takes a fixed rupee amount off (never more than the thing is worth) |
| Buy X Get Y | Buy qty, Get qty, Get % off | "Buy 2 get 1 free", or "buy 2, get the 3rd at 50% off". The **cheapest** qualifying units are the ones got. **Get % off** blank is free |
| Buy A Get B | Buy qty, Get qty, Get items, Get % off | "Buy a shirt, get a tie free": buy from what **Applies To** names (list A), get from **Get items** (list B). B units are taken cheapest first, and a unit is never both bought and got |
| Bundle Price | Bundle qty, Bundle price | "Any 3 for ₹999". The **most expensive** qualifying units are bundled first |
| Spend Slabs | Slabs | "Spend ₹2,000 get ₹200 off, ₹5,000 get 10% off": `2000=200; 5000=10%`. The highest slab the spend reaches applies, to what the offer covers |
| Quantity Slabs | Slabs | The same by units: `3=5%; 5=15%` is 5% off for 3 or 4 units and 15% for 5 or more |
| Fixed Price | Offer price | Every unit it covers sells at that price. A unit already cheaper keeps its price |
| Combo | Combo lists, Offer price | "One kurta + one dupatta for ₹1,499": one list per line (codes by commas), one unit from each list together at **Offer price**. Needs two lists or more. **Applies To** is hidden (the lists say what is covered); the dearest units are combined first |
| Gift With Purchase | Gift item, Gift qty, Buy qty | See *Gift With Purchase* in the Items, printing and stock guide |
| Bonus Points | Bonus per, Bonus points | Extra loyalty points, not a discount (below) |

In **Slabs**, steps are separated by `;` or new lines, each `from=value`; a value ending in `%` is a percentage, otherwise rupees.

**Applies To** decides which part of the sale the offer works on, and the **Scope Value** box changes to match as soon as you pick it:

| Applies To | Scope Value | The offer covers |
|---|---|---|
| Bill | (not used) | The whole sale |
| Item | Pick the item | That one SKU |
| Category | Pick from the categories already on your items | Every item whose **Category** is that value |
| Product Group | Pick a PIM **Product Group** | Every product in the group. A *Dynamic* group follows its rules, so products joining or leaving the group join or leave the offer automatically |
| SKU List | Type the SKU codes (or barcodes), separated by commas or one per line | Exactly those SKUs |
| Design List | Type the design numbers, separated by commas or one per line | Every item of those designs, so "10% off these 40 designs" is one offer |

**Get items**, **Exclude items** and each line of **Combo lists** take SKUs, barcodes and design numbers mixed.

Set up a Product Group first under **Setup → PIM → PIM Product Group** if you want to target one. If a group is later deactivated or deleted, offers pointing at it simply stop applying (they never spread to the whole bill).

Everything else is an optional condition, and they all have to be true at once:

- **Minimum Bill Amount / Minimum Qty** — the "spend ₹2000 and get…" family.
- **Coupon Code** — leave blank and the offer applies automatically; fill it in and it only applies when the cashier types that code.
- **Customer Tier** — restricts the offer to one loyalty tier. Note a walk-in with no customer on the sale has no tier, so these never apply to anonymous sales.
- **Customer Tag** — restricts the offer to customers carrying that tag (blank = everyone). A sale with no customer never matches a tagged offer.
- **Offer Type = Bonus Points** (Stage 146.2.1) — gives extra points, not a discount. **Bonus per** *Spend* (**Bonus points** per **Per ₹ spent** rupees on what the scope covers), *Unit* (per unit) or *Bill* (once per bill). Valid dates, tier, minimums and Priority work as above; *Stackable = No* ends the bonus list at that rule. Base points always apply; bonus points wait and expire like them, one ledger line per rule. Nothing changes until such an offer exists.
- **Stores** — store codes, separated by commas (blank = every store). Applies to every offer type.
- **Valid From / Valid To** — leave either blank for open-ended.
- **Days** — *Mon-Fri*, *Sat, Sun*, *Weekend*, *Weekdays* (blank = every day). **From time / To time** — the hours it runs, in the business's time zone (blank = all day); *22:00* to *02:00* runs over midnight. A day or time that does not read is refused on save.
- **Exclude items** — SKUs or design numbers that never take the offer, even a whole-bill one.
- **Uses in total / Uses per customer** — a use is a paid bill the offer took money off. A per-customer limit needs a customer on the bill, so a walk-in sale never gets such an offer. Two tills taking the very last use at the same moment can both get it.
- **Maximum Discount Cap** — the safety net on a percentage offer ("20% off, up to ₹500").
- **Priority** — lower numbers are considered first.
- **Stackable** — *Yes* lets later offers apply on top; *No* means once this one applies, nothing after it does. Use **No** for a headline offer you don't want combined with anything else.

Three behaviours worth knowing before you design a promotion:

- **A non-stackable offer stops the stack.** Combined with Priority, that's how you say "this offer instead of the others, not as well as."
- **A sale can never go below zero.** If offers add up to more than the bill, the discount is capped at the bill.
- **To switch an offer off, set Status to Inactive** — it stops applying immediately, and you keep the record and its history. Don't delete it.

**Uploading offers (215.10).** Offers list → **Bulk Import**. The **File** box picks what the CSV holds:

- **Offers**: one row per offer, one column per box (**Download Template CSV** and **Download Column Guide** give them). A row with an `id` updates that offer; a blank cell keeps what it had.
- **Item lists**: the long lists, one code per row, in three columns: `offer` (its id or name), `list` and `code` (an SKU, barcode or design number). `list` is **Applies to** (the offer's SKU or Design List), **Get** (Buy A Get B), **Exclude**, or **Combo 1**, **Combo 2** ... Each list the file names **replaces** that list on the offer; lists it does not name stay as they were. **Download Template CSV** gives an example.

**Preview (no changes written)** checks the file first: an Item lists preview shows each change as *offer · list: codes before → codes after*. **Process Import** saves. A code that is no item or design, a list the offer's type does not use, or a design in an SKU List is refused by row number. An offer with any refused row is left as it was, whole; fix its rows and upload again. Uploads need the right to change offers.

**Three kinds of discount (215.5, 215.11).** A bill keeps three discounts apart, applied in this order:

1. **Item discount**: a line sold below its list price through the line's **Override**. The cashier gives a % off, ₹ off each or a new price.
2. **Offer discount**: the offers above and any issued vouchers.
3. **Bill discount**: the cashier's % or ₹ off the whole bill.

GST is worked on what is left after all three. Each bill line stores its share of the bill discount, so the **Sales Register** (per bill), **Sales Itemwise Detail** and **Sales Discount Analysis** (per line) show **Item Discount**, **Offer Discount** and **Bill Discount** in their own columns. **Campaign Performance** and **Coupon / Voucher Usage Report** count offers and vouchers only, not the bill discount. Bills made before this change are split the same way from what they stored.

A line override is checked against the price-override approval slab with a reason code (category *Price Override*). The bill discount needs a reason above **Discount needs a reason above** (Settings > Point of Sale). The approval slab judges the cashier's own discounts together: a 9% line cut plus a 9% bill discount is judged as the 17% it is, not as two 9%s under a 10% slab. Offers are the business's own and are left out.


**Issued vouchers at the till** (Stage 73.2.10). A **Voucher** (CRM → Voucher, or a bulk CSV of codes) is a code you hand out: *Flat* or *Percentage*, with **Max Uses**, an **Expiry Date**, and optionally **Restricted to Customer**, **Valid From** and **Where it can be used** (*Any*, *POS* or *Online* — blank is Any). A cashier types it into the till's **Coupon code** box; a code no Offer claims is looked up as a voucher, applied after the offers (one voucher per bill), shared across the lines like an offer so it reaches the GST and any later return the same way, and used up only when the sale posts. The panel tells the cashier why a voucher was refused. Max Uses is enforced under a row lock, so the last use cannot be spent twice.

#### B.3.0.3 Database Schema Design — adding, editing and removing fields

**Settings → Database Schema Design.** Hover a module on the left, pick a record type, and you get its field list.

- **Add Field** opens a form: technical field name, display label, field type (Data / Number / Select / Check / Date / Link), whether it is mandatory, its options, and its position in the form. New fields land after everything already defined, so several added in a row do not collide on one position.
- **Edit** on any row opens the same form, pre-filled. Everything is editable, **including the field name**. Before this existed the only way to correct a mistake was Delete-then-Add — which throws away that field's stored value on every existing record. Use Edit.
- **Delete** removes the field from the form. As with a rename, the stored data is not erased.

**What renaming a field actually does.** The field list is a *view* over each record's stored data, not the data itself. Renaming changes which key the form reads, so values saved under the old name stop being displayed — they are not deleted, and renaming back restores them. The form warns you before you do it. This is worth knowing when a rename looks like it wiped a column: it did not.

**Two things are refused, with the reason named:** a field type the form cannot render, and renaming a field onto a name another field on that record type already uses.

**Fields the system fills in itself are shown read-only** — auto-generated document numbers, and derived companions such as **Phone Country** (which the phone engine resolves from the number itself). They display "Set automatically on save" rather than inviting a value that would be overwritten.

#### B.3.1 Status Transition Rules — how "strict" works

Out of the box, 156 record types are **strict**: for those, a status change is refused unless a rule explicitly permits it, and the error tells the user which statuses they *can* move to. Everything else is permissive — any status change is allowed unless a rule explicitly forbids it.

This matters when you edit these rules:

- **On a strict record type, deleting a rule takes the transition away.** If someone reports "I can't move this invoice to Paid any more", the first thing to check is whether its rule row still exists and is Active.
- **Adding a record type to the strict set is not a checkbox in the UI** — it's the `strict_status_transitions` flag on the record type. Ask a developer; and seed its full matrix *first*, or you will freeze existing documents in whatever status they're already in. Stage 49.5.3 added the posting record types that were still permissive — Journal Voucher, Inventory Adjustment, Purchase Return, Delivery Challan, Material Issue, Intercompany Transaction, Challan Write-off, Loyalty Point Adjustment, IRN Cancellation, IRN Manual Import, Vendor Credit Note, Payslip and Landed Cost Voucher — with the same Draft / Pending Approval / Approved / Rejected matrix; their posted statuses (Posted, Reversed, Issued, Dispatched, Applied...) have no rule on purpose, because only the document's own action may set them. **Money and stock records made strict (49.5.3).** A second pass added Loyalty Redemption Request, Price List Version, Posting Profile, Budget and Prepaid Expense Schedule (the same approval matrix; Redeemed, Superseded and Completed are set by the system only), Captured Charge (Captured → Cancelled; Billed only by invoicing), Voucher (Active, Inactive and Expired move freely; Exhausted only by use, so a used-up code cannot be saved back to Active), Subcontract Order (Draft → Cancelled; Sent and Received only by Send / Receive), and Physical Inventory, Bundle Assembly, Exchange Credit, Deferred Revenue Schedule, OMS Bill and Logistics Booking, whose every status after the first is set by their own screens and jobs. **Third pass (49.5.3).** Employee Loan, Recurring Sales Contract, POS Held Bill, POS Price Override, Service Contract, Sales Quotation, Quality Inspection, Customer Merge, Item Treatment Correction, Reprint Request, Backdated Posting Request, Period Reopen Request, GRN Excess Approval, Hold Release Request, Batch, Serial Number, Shipping Package, Gate Pass, Wave, Warehouse Task and Loading Task are strict too; where the system sets a status itself (Closed loan, Converted quotation, Merged customer, Resumed or Discarded held bill), a hand edit to that status is refused. **Fourth pass (49.5.3).** MRP Change, Certificate of Analysis, Non-Conformance Report, Maintenance Order, Fulfillment Task, Supply Proposal, NDR Case, Cross-Dock Plan, Yard Check-In and Loyalty Point Transfer are strict as well: a status moves forward one step at a time, never back or repeated, and the statuses the system sets (Released, Completed, Departed, Dispatched...) cannot be set by hand. **Approval cores, WMS and service (49.5.3, owner-approved 2026-11-12).** Password Reset Request, Supplier Submission, Appraisal and Grievance use the approval matrix (a rejection needs a reason; a reset's Failed is set only by the reset). LPN moves Open ↔ Closed → Shipped by hand. An Appointment can be set to Cancelled or No Show by hand (Cancelled also after check-in); Checked In, In Progress and Completed come only from the Yard Board and receiving. Service Ticket and Sort Slot move only through their own buttons and the sortation screens.
- **Nothing gets permanently stuck.** A document sitting in a status the record type doesn't even define (older data from before that type had a lifecycle) can always be moved back to a valid status.
- **To turn enforcement off entirely in an emergency**, one database statement does it without deleting any of your rules: `UPDATE tenant_default.doctype_meta SET strict_status_transitions = FALSE;`

Some deliberate examples of what the shipped rules block: a vendor invoice can't go straight from Draft to Paid (that would skip the 3-way match), a disposed asset can't be re-capitalised, and an approved goods receipt whose stock has already posted can only be cancelled with a reason code.

Two shipped judgement calls you may want to change: an **approved Leave** and a **selected Vendor Quote** are both treated as final. If your process needs to reverse either, add the row — it's one entry on this screen, no code change.

#### B.3.2 Prefix Configurations — the number series behind every transaction

Nobody types a document number in this system. Purchase Orders, Goods Receipts, Purchase Returns (`PRT`, and the Debit Notes they raise, `DN`), Fixed Assets raised from a goods receipt (`AST`), ASNs, RFQs, Vendor Quotes, Stock Transfers, Expense Claims, Leave, Employee Loans, Grievances, Production Orders and Attendance all draw their number from a **series** defined on this screen, at the moment the document is saved. The user sees a greyed-out box reading "Auto (PO series)" until then.

Each row is one series, and each transaction has its own: PO, GRN and Transfer numbers can all look different. The table shows the prefix, length, suffix, how often it restarts, whether the store is in the number, how many stores have their own prefix, and a **Next number looks like** example. **Edit** opens one form with every setting and a live preview:

Rows are **grouped** — Procurement, Sales, POS, Inventory, Manufacturing, Finance, HR, Products & barcodes, then any other module — and each group's heading shows how many series it holds. The drop-down beside **New Series** shows one group (or **All groups**); the choice stays for the browser tab. A series named after a record reads by its plain name: **Design** is the design-number series, **Combination ID / item** the number every new item and combination takes (`I00042`), **Item name**, **Product type** and so on.

You can also get here from the record itself: a **New** form for a numbered record type shows **Numbering for this record type** under its code, which opens this screen on that type's series (Leave opens **Leave (LV)**) with its next number previewed. A record whose code is worked out rather than numbered (HSN code) has no such link.

| Setting | What it does |
|---|---|
| **Prefix** | The leading text. `PO`, `GRN`, `TRF` — whatever your business calls it. Capital letters, digits, `/` and `-` (what you type in lower case is saved in capitals); it may not start with `0`, `/` or `-`. |
| **Separator** | What goes between the parts: **/**, **-** or **None**. With None, `PO` `BDR` `2627` `00001` prints as `POBDR262700001`. |
| **Number length (digits)** | How many digits the running number is padded to. `5` gives `00042`. Shipped lengths: purchase series (PR, RFQ, Vendor Quote, PO, ASN, GRN, Purchase Return, Debit Note) and HR series (Leave, Attendance, Expense, Loan, Grievance, Payslip) **5**; Transfers, Sales Invoice, Production Order and Fixed Asset **6**. |
| **Suffix (optional)** | Text after the number: `PO/BDR/26-27/00001/A`. The same characters as the prefix, `/` included: with **Separator** None, the year at the end and suffix `/`, the number reads `POHO00001/2627`. |
| **Restart numbering** | **Every financial year** → `PO/BDR/26-27/00001`. **Every month** → `PO/BDR/26-27-07/00001`. **Never - no year in the number** → `PO/BDR/00001`, one continuous series. |
| **Year in the number** | How the financial year prints: **26-27**, **2627**, **2026-27**, **26** (the year it starts) or **2026**. Monthly: `26-27-07`, `262707`, `2026-27-07`, and for **26** / **2026** the calendar month, `2607` / `202607` (January 2027 is `2701`, not `2601`). Shows **No year** when Restart is **Never**. Changing it mid-year carries the count on — the next PO after `PO/BDR/26-27/00041` is `PO/BDR/2627/00042`, never `00001` again. |
| **Where the year goes** | **Before the number** (as every series printed before) → `PO/BDR/26-27/00001`, or **At the end** → `PO/BDR/00001/26-27` (after the suffix, if there is one; with no separator `SMHO00012627`). Only the layout moves; the counter is still kept per year, so changing it mid-year carries the count on. |
| **Store in the number** | **Yes** prints the store's **Short Code** and numbers each store separately. **No** removes it and runs one series shared by every store. |
| **Status** | **Inactive** stops that transaction from being saved at all (`ADMINC-0030`). |
| **Store-wise code and suffix** | Every store is listed here as soon as it is created, with its **Next number**. **Code in number** replaces only that store's part of the number, for this series: give HO the code `HQ` and its ASNs read `ASN/HQ/26-27/00001` while the prefix stays `ASN`. Leave it blank to print the store's Short Code. **Suffix** gives that store its own suffix. Two stores cannot print the same code (`ADMINC-0311`). A store that was given its own prefix before this column existed shows an **Own prefix (older setting)** column; it still replaces the series prefix for that store until you clear it. |

**Which store text appears in the number.** The store's **Short Code** (Setup → Location), so a Bandra store with short code `BDR` prints `PO/BDR/...`. A store with no short code prints its Store Code. **Which store** is the document's own (Stage 76.8): a PO's billing store, a goods receipt's or a return's receiving store, a transfer's *from* store, an RFQ's deliver-to store (a vendor quote and the PO an award makes follow the RFQ) — not the store of the person typing it. So a PO raised at head office for Jayanagar carries Jayanagar's number. Only a document that names no store uses the signed-in user's store. Short codes must be unique: the Location form refuses one that another store already uses as its short code or store code (`MASTER-0312`), because two stores printing the same segment would hand out the same number.

Things to understand before you change one:

- **Restart decides two things at once, on purpose.** It sets how often the counter restarts *and* whether the number shows the period. There's no way to restart yearly while hiding the year, because that would re-issue last year's numbers — and since the number is also the document's identifier, the second document would be refused. **Never** is the only way to get a number with no year in it.
- **Store in the number works the same way.** Turning it off doesn't just hide the store; it merges the per-store counters into one. Otherwise two stores would both be handed `PO/00001`.
- **Changes apply to the next document only.** Existing documents keep the numbers they were issued. Changing a prefix, length, suffix or short code does not renumber history, and the running counter carries on (it is kept per Store Code), so no number is ever issued twice.
- **A refused save gives its number back (57.26(d)).** A number is drawn before the document is fully checked. When a record saved from its form is refused (a required field missing, a rule broken), the number goes back to the series and the next document gets it. A gap can still appear when another document drew the next number in between: a number is never issued twice or out of order. A gap is not data loss.
- **Service tickets (57.26(b)).** A service ticket takes its number from its own **TKT** series (`TKT/HO/2627/00001`), from the ticket form and from the service desk alike. Set its format here like any other series; it is listed under **Sales**.
- **Two series cannot share a prefix.** Document numbers are unique across *every* record type, so the screen refuses a prefix (or a store-wise prefix) that another series already prints (`ADMINC-0310`).
- **A deleted store does not block a save (Stage 115.2).** Every live store is checked, as listed under Store-wise code. A deleted store keeps its code reserved (no live store may print it) but issues no new number, so its number is no longer checked - before, a deleted store whose code was a Location series number (`Location/HQ/2026/000001`) refused every save with "give that store a Code in number", for a store the form does not show. The message now names the store.
- **No number may be longer than 16 characters.** GST allows an invoice, credit or debit note number of at most 16 characters (CGST Rule 46), and the rule is held for every document series. The form counts the **longest** number any store would print (the store with the longest code decides) and, when it is too long, says which store and number: `ASN/JAY/26-27/00001 is 19 characters`. Save is refused until it fits (`ADMINC-0311`). Ways to fit: a shorter prefix, the year as **2627** or **26**, fewer digits, a blank separator, or a shorter store code. Over-long series are marked in red on the list, so you can see which ones need attention. A series that already prints numbers over 16 (several shipped series made before this rule, such as `PO/HO/26-27/00001`) can still be **saved as it is**, so you can switch it off or on: the form says so at its foot, under the store table. Since Stage 130.6 those 19 series (PO, PR, GRN, ASN, RFQ, QTN, PRT, TO, TI, VCN, DN, AST, ATT, EXP, GRV, LOAN, LV, PRO, SM) ship short for a new client — the year as **2627** and as many digits as fit in 16 at its longest store code, e.g. `GRN/HO/2627/0001` — and an existing client's were changed the same way where the series had never issued a number and nobody had saved it; one already in use keeps printing as it did. Any change to its number has to fit. Barcode and GRN batch counters are not documents and are not counted, and neither is a record's code (a vendor's, an item's). A business whose home country (**Settings → Configuration → default country**) is not India gets a limit of **20** characters and no GST character rule.
- **Sales invoices and credit notes (Stage 76.9).** A sales invoice takes its number from the **SI** series and a customer credit note from **CN** (new: `CN/BDR/2627/0001`), at the invoice's store; debit notes and credit notes to vendors already use **DN** / **VCN**. A note or invoice made on the record form shows its number box greyed out and gets the number on save. One the system makes — an order's or a parcel's invoice, a recurring invoice, a warehouse charge invoice, the credit note for a cancelled order — keeps its internal reference and prints the series number. If that series is switched off, such a document is still made (a shipment never stops on it) with its internal reference as the number, and System Errors says why. A new install's **SI** prints `SI/BDR/2627/00001` (16 characters); an existing one changes only if it was never used and never edited.
- **Orders, returns, refunds, JVs, counts and the rest (Stage 76.11).** Documents the system makes as part of a flow now print a series number too, at their own store: **Sales Order** `SO` (the store that fulfils it — `SO/BDR/2627/00001`), **Sales Return** `SR` (the store it is returned to), **Refund** `RF`, **Physical Inventory** `PI`, **Gate Pass** `GP`, **Consumption Entry** `CE`, **Kit / De-kit** `KIT`, **Landed Cost Voucher** `LCV` (its goods receipt's store) and **Journal Voucher** `JV` — one company-wide series, `JV/2627/000001`, since a JV belongs to no store. Each keeps its internal reference behind the scenes (links, the ledger and retries use it); the number is what the OMS order list, order search, the Returns list, the record screens and the toasts show. Searching OMS orders by the new number works. Documents made before this keep the reference they had. The year prints as **2627** in these new series so they fit 16 characters at a two-letter store code; change any of them here like any other series.
- **Series set up ahead of their documents (Stage 76.12).** **Material Issue** `MI`, **Material Receipt** `MR`, **Delivery Challan** `DC`, **Job work out / return** `JO` / `JR`, **Store credit voucher** `CV`, **Damage** `DMG` and **Stock adjustment** `ADJ` are listed now so their format can be set before those documents arrive (Stage 74); nothing draws from them yet. Every transaction series shows its name on the list — "Sales order (SO)".
- **Only the characters GST allows.** A document number may use capital letters, digits, `/` and `-`, and may not start with `0`, `/` or `-` (the e-invoice portal's rule). Only the number's first character counts: `TOJAY00001` is fine, because the running number may start with 0. The form checks every store's number the same way and names the store whose number breaks the rule — usually because its Short Code or Store Code has a `_`, a `.` or small letters; give that store a **Code in number** to fix it. The list marks such a series *not a valid GST number*.

##### Adding a series for a record type that isn't listed yet (New Series)

**Master record codes are short, and set up the first time (Stage 76.5).** A master record — Vendor, Customer, Item, Employee, Category, a design and the rest — is numbered `V00001`, `V00002` …: a prefix and a zero-padded number, with no store, no year and no separator, never restarting. The **first** time anyone creates a record of a kind, its New form (and the inline **+ New** dialog) shows **"… code format (asked once)"** with a suggested prefix (`V` vendors, `C` customers, `I` items, `D` designs, `LE` legal entities, otherwise the name's initials — never a prefix another series already prints) and **Digits** (3–8, default 5). Saving that first record confirms the format; from then on every record of that kind takes the next number and the question is gone. A CSV import or API call that arrives first gets the suggested format. You can also set the format **here first**: saving a master's row (say Vendor, `VEN`, 4 digits) confirms it, and the first vendor is `VEN0001` (Stage 126.4; before, the first record replaced what was typed here). To change a confirmed format later, edit its row — it applies to the next record only. A master's form here offers only **Prefix**, **Separator**, **Number length** and **Suffix**: **Restart** shows Never and **Store in the number** shows No, because a record's code never prints either, and they cannot be changed (changing them restarted its count at 1 over codes already issued).
- **Barcode counters are fixed (Stage 126.4).** **Barcode (counter)** and **Receipt-date barcode (counter)** mint the digits inside every EAN barcode: only their **Status** can change. A different length breaks every barcode; a different Restart or Store would repeat barcodes already printed. The **GRN batch number** keeps the case you type its prefix in (`bt` prints `bt-261021-0001`); a store's code in it is saved in capitals.

- The server numbers every master itself, for every role: the forms no longer call the admin-only number endpoint, so a Store Manager can create a vendor.
- A save that is refused gives its number back; an import row is numbered only after it passes validation. A number already used by any record is skipped, never overwritten.
- **Currency, UOM and HSN Code** are not numbered: their code *is* the value (`INR`, `PCS`, `7113`) and is typed on the form.
- Codes issued before Stage 76 keep their long shape (`Vendor/HQ/2026/000006`) until an Admin renumbers them (below).

**Renumbering old long codes (Stage 76.6 / 93.11).** When a master list (Vendor, Customer, Item and the rest) still holds records on an old long code, an Admin sees a panel at the top of that list: *"N … records are still on an old long code"*, with each record's old and new code. Other roles never see it, and it goes away once no record of that kind is left on a long code.
1. If the code format was never set, the panel asks for it once (**Prefix**, **Digits**, with the suggestion filled in). **Show the new codes** redraws the old → new list in that format.
2. **Show what changes** lists every table and column that holds these codes, with its row count. Every record that points at a code is rewritten in the same step.
3. **Item** codes carry a warning: barcode stickers and sales-channel listings that print the old code must be redone. Tick *I will reprint the stickers and update the channel listings* first.
4. **Renumber N records** stays disabled until a backup from the last 24 hours exists (the nightly 02:00 backup; if it failed, the operator runs `deploy/backup.sh`). A server that is not production and has never recorded a backup is not held to this. Click it and confirm. The whole run is one transaction: either every code changes or none does. Codes cannot change again afterwards.
- Signed audit rows and append-only ledgers keep the old code, because rewriting them would falsify history. A till or integration that still sends an old **store** code lands on the new one. Each run is written to the audit log (`MASTER_CODES_CLEANED`).

For a record type that has no row here yet, use **New Series** (the plus-icon button) at the top of the screen:

1. Click **New Series**. The same form opens, with the **Transaction** box editable. A new series starts at 4 digits with the year as **2627** (`PO/HO/2627/0001`), inside 16 characters.
2. **Transaction** — pick it from the list that opens as you type: every transaction and record type that does not have a series yet, by name (*Purchase order (PO)*, *Customer*). Typing the name or the code in any case also works (`purchase order`, `po`). A name the system does not number asks first: *"… nothing would use this series. Pick one from the list instead - or save it anyway?"*
3. Fill in the rest of the form as for **Edit** and save. A transaction that already has a series is refused here (*"… already has a number series - close this and use its Edit button"*); there is only ever one series per transaction. After saving, the list shows the group the new series is in.

One caution: **it applies to the next record only.** Adding a series for a record type that has been auto-numbering for months does **not** renumber the existing ones, and the counter starts from 1 — so if you choose a new prefix, choose one that cannot collide with what has already been issued. If in doubt, keep the default prefix (the record type's own name, which is what the engine was already using).

#### B.3.2.1 Choosing the industry, and changing it later

**Settings → Configuration → Industry** (the first entry in the list on the left). Only the org's admin sees it — Configuration is admin-only, and other roles never see the industry at all. Choosing an industry profile (Jewelry, Food & Beverage, Clothing, Pharmaceuticals and the rest) overlays that industry's extra fields onto the standard record types — see §B.3.0.3 and User Guide §8d. It is a wide-reaching change: it rewrites field definitions across the system and replaces the screen labels with the industry's own.

The panel shows the **current** industry, **who** set it and **when**, and how many times it has been changed since. Below it, every industry is listed by name with what it covers (for example *Fashion and fine jewellery; stones, precious metals, plating, workmanship*).

**Setting it the first time:** click an industry. The panel says what will happen — its fields are added to Item and the setup lists, the labels are replaced, its common HSN codes are added, and nothing that already exists changes. Press **Set industry to …** and confirm.

**Once set, the profile is locked**: changing it needs a written reason. The lock is real server-side state, not a browser setting — clearing your browser, using another machine, or signing in as a different admin will not unlock it.

**Choosing the industry also loads its HSN codes (Stage 66).** The industry's common HSN/SAC codes are added to **Setup → Core → HSN Code**, each with a description and, where the code has a single GST rate, that rate. For example, a jewellery client gets 7113 silver and gold jewellery, 7117 imitation jewellery, coins, bullion and diamonds; a clothing client gets fabrics, garments, made-ups and footwear. Codes already on the list are left exactly as they were, so a description or rate you set is never overwritten. An override to another industry adds that industry's codes too and removes none. From then on, the **HSN Code** box on every Item suggests codes ranked for the item's name and your industry (User Guide §8.1.2). Without an industry the suggestions still work from the item name, but the *Common in your industry* group is missing. Set the industry early.

**The HSN rates you set win.** If a code on your HSN Code list carries a **Default GST Rate**, that rate is what the item picker fills, and its button reads *Set on your HSN list*. The system's own rates (GST from 22 September 2025) appear beside it. A list that still holds an old rate (a pre-September-2025 12%, say) therefore keeps suggesting it until you correct it on the list. The full list the system suggests from ships with the system (`engines/hsn_master.json`, about 290 codes across the ten industries). It is suggestions only: an Item can carry any valid 4, 6 or 8-digit code.

**To change it:**

1. In **Settings → Configuration → Industry**, click the industry you are moving to. The panel says what the change does: the new industry's fields are added, the labels are replaced, its HSN codes are added — and **nothing is deleted**: the current industry's fields and every record stay.
2. **Enter the reason for the change.** It is **mandatory** — the button refuses without one. Write why the profile is genuinely changing ("client confirmed they sell apparel, not jewellery"), because this text is what an auditor reads later.
3. Press **Change industry to …** and confirm.

The system then records **who** changed it, **when**, **why**, and increments the change counter shown on the panel.

Notes:

- **Only a Super Admin can choose or change the profile.** The lock is an extra barrier on top of that permission, not a replacement for it. Other roles see neither the panel nor the industry, and the server refuses the change if one tries it anyway.
- **(Until Stage 82 the industry was a dropdown in every user's top bar with an Override button that asked for a typed code. Both are gone.)**
- **The counter is never reset.** Repeated overrides accumulate, so a profile that has been changed five times cannot later be presented as having been set once.
- **An override is not a rollback.** Switching profiles overlays the new industry's fields; it does not remove data already captured in the old industry's fields. Treat it as a forward change, and expect to tidy up now-unused fields on **Database Schema Design** afterwards.
- **Decide the profile before go-live if you can.** The lock exists precisely because this is not a setting to experiment with on a tenant that already holds real catalog data.

#### Competitor undercut alerts

If your buyers record competitor prices (User Guide §8a), the system can tell them when one of your SKUs is being materially undercut, instead of waiting for someone to run the report.

It is **off by default** and takes two pieces of setup:

1. **Set the threshold.** Admin → Settings → Platform → **Competitor undercut alert threshold** (`market.undercut_threshold_pct`). It is `0` on a fresh system, which means "never alert" — a business that doesn't track competitor prices is never bothered by this. Set it to the percentage gap you actually care about; `10` means "tell me when someone is 10% or more below what we last sold it for". The maximum accepted is `90`, because a competitor apparently selling at 95% off is a typo in the imported data, not a pricing signal.
2. **Author the message.** Setup → OMS → **Notification Template** (under Advanced), with **Event** set to `Competitor Undercut`. This uses the same templating as every other notification — `{{sku}}`, `{{our_price}}`, `{{competitor_price}}`, `{{undercut_pct}}`, `{{platform}}` and `{{source_url}}` are substituted into the body. Delivery goes through the matching **Notification Channel Config** webhook, exactly like order and return notifications.

Points worth knowing:

- **A given observation alerts at most once.** The check runs hourly but only looks at competitor prices *recorded since the previous run*, so a competitor who stays cheap for a month produces one alert, not seven hundred. Re-import the same SKU at a new price and you'll get a fresh alert, which is the intent.
- **It never changes a price.** The worker only reads and notifies. Repricing stays a human decision — there is no code path in it that writes to a document or a setting.
- **No template configured means no alert is sent, but the attempt is still logged.** Look in **Notification Log** for `Skipped-NoTemplate` rows if you expected an alert and got nothing — that is the usual cause.
- **A SKU with no sales history is never alerted on.** The comparison needs a price of ours to compare against, and this system takes that from what you actually sold at (see the note in User Guide §8a). Items never sold appear in the report as "No price on file" and are skipped by the alert.

#### Tax-exempt, nil-rated and zero-rated goods

Most items are taxable and need a GST Rate above 0 — the system rejects a bare 0, because it cannot tell that apart from a rate nobody filled in. Items that genuinely carry no GST (fresh produce, unbranded grain, salt, books, exports) declare it instead, on the **Tax Treatment** field of the Item master: **Taxable**, **Exempt**, **Nil-Rated** or **Zero-Rated**. Users see this in User Guide §9 Step 3; what you need to know as an administrator:

- **Nothing has to be switched on.** The field appears on every Item form already, and leaving it blank means Taxable — every item that existed before this feature keeps behaving exactly as it did, including still being required to carry a positive GST Rate.
- **HSN Code stays mandatory on all four treatments.** It belongs on the invoice whatever the rate is, and GSTR-1 reports the nil/exempt table HSN-wise too.
- **Three GL accounts appear in the Chart of Accounts**: `4110` Exempt Sales Revenue, `4111` Nil-Rated Sales Revenue, `4112` Zero-Rated Sales Revenue. A sale of non-taxable goods still credits full revenue to `4100`, then moves that portion into whichever of the three applies. A tenant that never sells such goods never posts to them and will see them sitting at zero — that is expected, not a setup error.
- **Why they exist:** the **GST Return Summary** report reads `4100`'s balance as the period's *taxable* value. Without the reclass, exempt turnover would sit in `4100` and be reported as taxable — overstating GSTR-3B 3.1(a) and understating 3.1(c). The report now also shows Exempt / Nil-Rated / Zero-Rated / Total Non-Taxable, which is the split GSTR-1's nil-and-exempt table and GSTR-3B 3.1(b)/(c) are filed from. Those four figures are hidden on the screen when they are all zero.
- **Exempt and Nil-Rated are deliberately not merged**, and neither is Zero-Rated, because the returns report them in different boxes. If you are reconciling by hand, do not add them together before filing.
- **Zero-Rated here means the LUT/bond route** — exports supplied *without* payment of tax. The export-with-payment-then-claim-refund route is not modelled; the refund claim would need a workflow this system does not have.

#### Purchase Returns (return to vendor) — setup and accounting

**Procurement → Purchase Return** (User Guide §6.5) sends goods back against the GRN they arrived on. Nothing has to be configured for it to work. These are the settings you may want to change, and what posting a return does to the books.

1. **Who can use it.** The `PurchaseReturn` record type ships with full rights for HR/Admin and read/create/update for Store Manager. Role templates that grant the Procurement module cover it the same way they cover Goods Receipt. Change it under **Settings → Roles**. Posting a return needs *update* rights on `PurchaseReturn`. Location-scoped users only see and post returns for their own location.
2. **Requiring approval (optional).** To make returns go through maker-checker, open **Settings → Approval Rules** and add a rule for `PurchaseReturn`. The amount slab is matched against the return's value before GST (`total_amount`). From then on a Draft return shows **Submit for approval** instead of **Post**. An approver approves it in **Approvals** (the submitter too, if **Who can approve their own documents** allows it), and only an Approved return can be posted. Delete the rule to go back to direct posting.
3. **Numbering.** Returns draw from the `PRT` series and the Debit Notes they raise from the `DN` series (B.3.2). Both are shipped per tenant by `migrations_stage57_15_purchase_return.sql` and can be edited like any other series. A Debit Note entered by hand still takes the note number you type.
4. **What the stock does.** Accepted units leave *available* at the GRN's receiving location through the same floor-checked posting as every outbound movement. If they have already been sold, the post is refused. Units QC set aside at receipt (rejected → QC hold, damaged → damaged) leave those buckets instead. Every movement is in the stock ledger with voucher type `PurchaseReturn`. The item's moving-average cost has the returned receipt taken back out. The return does **not** touch WMS bin quantities: if the stock was put away into bins, adjust the bin as you would for any other non-WMS outbound movement.
5. **What the books do.** A Goods Receipt books Dr `1200` Inventory / Cr `2100` GRN Suspense at the PO's ex-GST rate for the accepted units. Posting a return reverses exactly that for the units returned, in two entries:
   - the return posts Dr `5150` Purchase Returns / Cr `1200` Inventory;
   - its Debit Note posts Dr `2100` / Cr `5150` through the same Post a hand-made Debit Note uses.

   `5150` nets to zero, and both what you hold and what you owe drop by the returned value. Live example: 8 accepted at ₹100 booked ₹800. Returning 3 left `2100` at Cr ₹500 and `1200` at Dr ₹500. Rejected or damaged units were never booked as owed, so returning them posts no GL entry and raises no Debit Note.
6. **The supplier's bill.** The Vendor Invoice three-way match values a GRN at its accepted units **less units on posted returns**. A bill for the units actually kept matches, and one still charging for returned units goes to MismatchHold. If the bill was paid before the return, `2100` is left in debit by the returned goods' value **including GST** (item 8): the supplier owes you that amount.
7. **If the Debit Note cannot be posted** (for example its accounting period is closed), the return still posts, because the goods have physically gone back. The Debit Note is left as a Draft, and the poster is told `PURCHA-0120` with the note number. Post it from **Financial Accounting → Debit Notes** once the cause is fixed.
8. **GST on the returned goods is reversed automatically** (since 2026-10-10). Input credit is booked to `1500` GST Input Credit when a supplier's bill is paid. When a return posts, the system checks whether the returned units were already on a bill: the GRN's Matched, Approved, Pending Approval or Paid bills now come to more than it is owed for the units kept.
   - **If they were billed,** the GST share of those units is reversed in the same posting: Dr `2100` / Cr `1500`, at the bill's own GST share (`gst_amount ÷ invoice_amount`), capped at the over-billed amount.
   - **If they were returned before any bill,** nothing is reversed. The supplier bills only the kept units, so their GST was never claimed.
   - **Not reversed:** rejected or damaged units, which were never billed, and a GRN billed in a foreign currency, whose import IGST is paid at customs.
   - **Billed but not yet paid:** the reversal posts at the return, the later payment claims the full credit, and `1500` nets to the right figure.

   Live example: a bill of ₹1,180 (₹180 GST) for 10 units at ₹100 was paid. Returning 3 reversed ₹54, leaving `1500` at ₹126 (GST on the 7 kept) and `2100` in debit by ₹354, the supplier's tax-inclusive refund. The Debit Note's own entry stays at the value before GST (₹300). The note also records `gst_amount` (₹54) and `total_with_gst` (₹354), its **Reason** ends with "GST 54.00 reversed; total with GST 354.00" (visible in Debit Notes), and the poster sees the reversed amount in the confirmation.

   **GST Return Summary:** Input Tax Credit is `1500`'s net for the period, so it falls by the reversal in the return's posting period, and Net Tax Payable rises by the same amount. There is no separate line to file by hand.

Error codes: `PURCHA-0116` (no reason), `PURCHA-0117` (more than can be returned, or a line the GRN never received), `PURCHA-0118` (no GRN, or a cancelled one whose stock never posted), `PURCHA-0119` (already on another return, or already posted), `PURCHA-0120` (Debit Note raised but not posted).


#### Return dispositions, reasons and credit-note receipts (Stage 148.28)

All off until you act. **Setup → OMS → Return Disposition Rule** (Admin or Accountant): one row per **Disposition** (Sellable, Damaged, Repairable, Wrong-Item) with **Drafts** (blank, Credit, Replace, Return to vendor, Scrap), a **Fee charge code** (a Trade Charge Code, whose GL account receives the fee) and a **Fee %**. No row, or no active charge code, means no fee. The fee is held back from the refund; revenue is still reversed whole through the 148.4 rule, and the fee is taken as GST-inclusive at the charge code's **GST rate** (148.28a): the tax part goes to output CGST and SGST (IGST when the sale was to another state) and the rest to the charge code's GL account. A code with no GST rate credits the whole fee to its account. On a Credit line the fee simply stays inside the original sale, so nothing is split. A Credit line is not refunded: a draft credit note is raised and a person posts it. Replace, Return to vendor and Scrap draft a Sales Order, a Purchase Return and an Inventory Adjustment; a line that cannot be drafted records why on the return. Scrap puts the unit in quarantine (QC hold).
Two settings in **Sales & Returns**: `sales.return_reason_required` (a new return is refused without a reason, SALESR-1315) and `sales.credit_note_needs_receipt` (a credit note on stock items posts only once the return it names is QC Complete or Closed, SALESR-1316). Both default off.
#### Item Types — what kind of item a SKU is (Stage 60.3/60.4)

Every Item has an **Item Type** (Setup → Item → Item Type). **Product** is the default: an Item saved with the field blank, or with the older value "Stock", is a Product. Item types are a list you manage under **Setup → Item Type**; seven ship with every client:

| Type | Barcode | Behaviour |
|---|---|---|
| **Product** | Client default | A normal stock item, bought and sold. |
| **Accessory** | Fixed per SKU | Sold alongside a main product; otherwise a normal stock item. |
| **Non-Asset** | Fixed per SKU | A stock item that is not a fixed asset (consumables, packing material). |
| **Free With Item** | Fixed per SKU | **Sells at zero price**, and only alongside its parent item: set **Free with item** on the Item to the main product. The till refuses the free item unless the parent is on the same bill in at least the same quantity (`POSOFF-0314`), and the Item form refuses a Free With Item SKU with no parent, or with itself as parent (`MASTER-0315`). |
| **Gift** | Fixed per SKU | **Sells at zero price** at the till. The line shows "Free (gift / free with item)" as its price source and does not count as a discount. |
| **Backflush** | Fixed per SKU | In production it is **not** issued at Material Issue. Each time finished goods are reported (partial or full completion), the BOM quantity × units finished is taken from stock at the order's location in the same posting as the finished goods. Short stock refuses the completion (`MANUFA-0143`). |
| **Fixed Asset** | Off | Bought on a PO and raised as an Asset, never sold (below). |

Each type is a record with these settings, so you can change the shipped ones or add your own: **Generate barcode at**, **Barcode per receipt date**, **Sell at zero price**, **Only with its parent item**, **Backflush**, **Fixed asset**, and **Status**. An Item picks its behaviour up from its type; only the two barcode settings can be overridden per Item. Zero price applies to the till's server-side pricing (POS quote and checkout); it cannot be overridden by a price list or a supervisor override.

**Since Stage 72 an item type is a full profile** with eight independent dimensions (commercial class, stock tracking, valuation, accounting, sellability, customer bill, POS capture, consumption) that every Item may override, six more shipped types, and Gift / Free With Item are now *Promo only* — see **§B.3.8**. The four flags above still work: where a type leaves a dimension blank, its flags decide it.

#### Fixed Asset items — bought on a PO, never sold (Stage 57.8)

An Item whose **Item Type** is **Fixed Asset** is equipment the business keeps, not stock it sells. User Guide §6.7 has the day-to-day steps. What an administrator should know:

1. **Turning an Item into a fixed asset** is the **Item Type** field on the Item (Setup → Item; blank means Product, so every existing Item is unchanged). Change it before the item is received. Stock already on hand from earlier receipts stays as stock, and that stock still cannot be sold once the type is Fixed Asset.
2. **Receiving.** When its GRN posts, the accepted units do not enter stock and nothing is booked to `1200` Inventory. Each unit becomes a **Draft** asset in HRM → Fixed Assets, numbered from the `AST` series (B.3.2; the location is the store segment), costed at the PO's rate before GST, and carrying the Item, GRN, PO, vendor, location and serial number. Units QC rejected or damaged go to QC hold / damaged as for any item. If an asset cannot be raised, the receipt still posts and an ERROR naming the GRN is written to the system error log, so the asset can be entered by hand.
3. **Accounting.** Capitalise books Dr `1400` Fixed Assets / Cr `2100` GRN Suspense, the same entry a hand-entered asset has always made. The supplier's bill is matched and paid against the GRN as usual, and paying it clears `2100`. Capitalise asks for the useful life when the asset has none, and stores it on the asset.
4. **Never sellable.** One check refuses a Fixed Asset item on every sale path: the server's sale-side tax computation, which POS quote and checkout, order and pack invoices and customer returns all use, and OMS order intake, both when an order is created and when a hold is released. The refusal is `ASSET-0273`. Picking needs an order line and stock, and a Fixed Asset item has neither.

**Code for every piece received (146.3.2).** An item type setting (**Setup → Item Type**, default **No**). Switch it **Yes** and name the stores in **Settings → Configuration → Inventory → Stores that give every piece its own code** (`inventory.piece_codes_stores`: blank = off, `all`, or store codes separated by commas). Only then does posting a goods receipt mint one serial per accepted piece, numbered from the **Piece code** series (Setup → Number series, group Products & barcodes; prefix `PC` and 8 digits by default). Codes already typed on the line are kept and only the shortfall is minted; they are saved on the receipt line so sticker reprints and re-posts never mint again. On the handheld, **RF → Receive pieces** (146.3.2b) scans or mints the codes line by line on a goods receipt received in steps, before the line is posted (`GET`/`POST /api/v1/grn-steps/{id}/piece-codes`). It needs the right to edit a Goods Receipt (GRN); each save writes a row to the receipt's change log (step **Piece codes**). A code twice, more codes than pieces, a code on another line of the receipt or already in the serial register, and a posted line are refused.

**Handheld work queue (146.3.3).** **Settings → Configuration → Warehouse → Handheld work queue** (`wms.work_queue`, default off). On: a posted goods receipt creates put-away tasks, a released wave creates pick tasks, and the RF screen shows **My next task**. Off: nothing is created and the RF screen is unchanged. Tasks already created stay on the Warehouse task list when you switch it off.

**Replenish, Move and Count tasks (146.3.3).** With the queue on: a bin below its minimum (Bin Replenishment rule) gets one **Replenish** task from the bin that can fill it, made after a pick and by the `replenish` action on the work queue (no background sweep); a **Move** task is made by the `request_move` action (from bin, to bin, SKU, quantity); starting a **Physical Inventory** leaves one **Count** task per frozen bin. Each is made once. Completing a Replenish or Move moves the stock; a bin move done by hand closes the matching task; closing or cancelling the count completes or cancels its Count tasks.

**Put-away rules (146.3.7).** Add rows under **Put-away Rule** (Inventory): an Item, a Design or a Category (only one), an optional Location, and a Zone or a Bin. Most specific wins: item, then design, then category; a rule for the exact location beats one for all locations. Home bins still come first; with no usable rule the normal strategy runs. Inactive rules are ignored.

**Blocks (146.3.14).** On a **Zone** or **Bin**, set **Block** to *No receipts* (put-away, moves and replenishment into it are refused) or *No picks* (never allocated to an order or wave). A reason is required; who and when are stored and audited (WMS_FLOW_BLOCK). A zone block covers its bins. Blank means open.

**Split put-away and fill warning (146.3.15).** `wms.putaway_split` (default off): when no single bin takes the quantity, Suggest Bin offers parts over the best bins by room, never mixing a single-item bin. `wms.putaway_fill_warn_pct` (default 0 = no warning): a put-away that takes a bin to that percent of its capacity shows a warning; the hard capacity block is unchanged.

#### Cost of goods: what there is to set (Stage 148.1)

Cost needs no setup and cannot be turned off. The rules are in the User Guide, §6.15: each item's rate is the value of its stock in hand ÷ the units in hand, company-wide, and tracked pieces and batches carry their own receipt cost.

- **Where the rates start.** Each item's rate on the day of the release is where its new rate starts. Nothing is recalculated, and no past posting or stock value changes. The first receipt after that blends over the stock in hand.
- **5195 COGS Variance** is added to every company's chart. The sold share of a bill or late charge posts there at once. Report it with cost of goods.
- **Item Type → Bill price tolerance %**: how far over the receipt's rate a bill line of that type may go. Blank: the 3-way match tolerance in **Configuration → Procurement**. Used only on bills that list **Bill Lines**.
- **Vendor → Bill weight tolerance (g)**: how many grams a bill may list over what was received, per item. Blank: weight is not checked. Set it for bullion vendors, and have the bill list **Weight (g)**.
- **Tying stock to the ledger.** **Inventory Valuation**'s control total compares its rows with GL 1200. Rounding of the rate can leave a few paise per item. Selling more than is in hand can leave more: the sale costs at the rate, and the next receipt sets a new one.

#### GST on purchases (Input Tax Credit) and on credit sales

- **Supplier bills.** When a Vendor Invoice is matched, the system works out how much of the bill is GST, using the GST on the Purchase Order it matches (CGST/SGST or IGST in the PO's proportions), and stores it on the invoice. Paying the invoice posts the goods value to `2100` GRN Suspense and the GST to `1500` GST Input Credit Account. That holds for Pay, Pay w/ TDS and an approved Override & Pay alike. A foreign-currency bill, or one matched against a PO without GST, posts the whole amount to `2100` as before.
- **RFQ award policy still needs a product decision.** The current Vendor Quote captures one `quoted_price` and lead time, but no unit, item line, tax basis or delivery terms. Selecting the winning quote records the comparison result; it must not automatically price or issue a PO. Stage 55.4 now links existing source documents in the UI, while quote-to-PO field semantics and approval policy remain open. Configure and review a PO explicitly; do not treat the linked records alone as three-way-match acceptance.
- **Procurement handoff (Stage 55.4 continuation):** an approved PO's **Receive goods** action carries that PO into GRN; the posted GRN's **Record vendor invoice** action carries its GRN/PO/vendor into the generic Draft invoice form. Operators still enter the supplier invoice number, amount and financial year and run **Match** before paying. A local partial receipt of one unit at ₹100 plus 3% GST matched a ₹103 invoice; a second ₹110 bill on that same GRN was held as overbilling. Rejected/damaged replacement versus PO-fulfillment accounting remains a product decision; do not change tolerance/closure policy on an assumption.
- **TDS** on a bill whose GST is recorded is deducted on the value **excluding** GST (CBDT Circular 23/2017). The supplier still receives the full bill less TDS.
- **Credit and online sales.** A Sales Invoice drafted for a shipped order now carries its CGST/SGST/IGST, worked out the same way package invoices are. Posting it books the tax to `2200`/`2201`/`2202` GST Output Payable and only the rest to `4100` Sales Revenue, the same split a POS sale makes. Before this, credit sales put their whole value in revenue, so their GST never reached the GST return.
- **GST Return Summary** shows **Input Tax Credit** (the period's movement on `1500`, which includes GST on expense claims) and **Net GST Payable** (output tax minus input credit; a negative figure is credit carried forward).
- These follow standard Indian GST treatment. Have your CA confirm eligibility rules (blocked credits under Section 17(5), reversals) for your business; the system records all input GST as claimable.
- **A PO's CGST + SGST / IGST is never chosen by hand (Stage 49.5.6b).** It is derived from the vendor's state and the billing store's state on every save, import and API call; the old `interstate_override` field is kept on the record but always stored false, and a client's `interstate` value is ignored. **Vendor → State** is required by rule (the Database Schema Design screen cannot make it optional; a GSTIN fills it). A PO for a vendor with no State and no GSTIN is refused (**PURCHA-4961**) on save, import, submit for approval, RFQ award, supply-proposal approval and the bulk purchase upload. Vendors and POs saved before this are not changed; before go-live, give every vendor a state (Setup → Vendors, or an import with a `state` column). A store with no state still makes intra-state POs - give every store, or its Legal Entity, a state.

#### Currency and exchange-rate setup (Stage 37 foundation)

After `db/migrations/migrations_stage37_1_currency_foundation.sql` is applied, two Finance Masters appear under Setup: **Currency** and **Exchange Rate**. INR is seeded because it was already the application's implicit functional currency; add only currencies the tenant actually trades in.

For each rate, set **From Currency**, **To Currency**, a positive Rate, the Rate Type (**Spot**, **Average**, or **Closing**), and its effective dates. Leave **Effective To** blank for an open-ended rate. Manual rates may have an optional note; an **Imported** rate must carry a Source Reference such as the provider and batch/date so it remains auditable. Generic Bulk Import can load repeated rate feeds without a separate importer.

The shared resolver chooses the most recently effective active rate whose date window includes the transaction date. If only the reverse pair exists, it safely uses the inverse; if neither exists it returns `FIN-0021` instead of inventing a value. Currency code and rate-window uniqueness are also enforced in the database.

**One rule that will otherwise cost you an afternoon: a Currency's ID must be its ISO code.** Create the US dollar with ID `USD`, not `CUR-USD` or an auto-generated value. Every document that selects a currency links to it by ID, so any other ID makes that currency unusable on invoices and orders. The system now refuses a mismatched ID at save time and tells you which one to use, but if you are importing currencies in bulk, set the ID column to the code.

#### Foreign-currency invoices, FX gain/loss, and period-end revaluation (Stage 37.1.3-37.1.5)

Set the tenant's reporting currency under **Configuration → Finance → Functional (reporting) currency** (default INR). Every ledger posting, report and approval threshold is expressed in it. Documents may be transacted in other currencies and are converted to it at their exchange rate. **Changing it does not re-translate existing postings** — it declares what they were always in — so treat it as a provisioning-time decision.

**A purchase order keeps its currency once goods are received (49.5.7).** After the first receipt against a PO, **Currency** and **Exchange Rate** on it cannot be changed (FIN-0844): raise a new PO, or correct the receipts. A posted invoice already works this way. A submit that is overtaken by a decision no longer reopens the document.

**Raising a foreign-currency invoice.** Set **Currency** on the invoice and leave **Exchange Rate** blank to have the rate resolved from your rate table *on the invoice's own date* (not today's — a back-dated invoice does not quietly pick up this morning's rate). Enter a rate explicitly to override it, which is what you want when a contract fixes the rate. The invoice keeps the amount you agreed in the foreign currency; the ledger holds its value in your functional currency.

**Settling one.** Post the invoice as usual. On settlement you may optionally supply:

- **Settlement date** — the date the money actually moved. Use it when a receipt is entered a few days late, so the rate quoted is the one that applied on the day, not today.
- **Exchange rate** — the rate the bank actually gave you. A bank confirmation beats any rate table; without it the difference just hides in your cash account.

The system books the cash at the settlement rate, clears the receivable at what the ledger was carrying, and posts the difference as a **realised** gain or loss. Four accounts are used and they are deliberately kept apart:

| Account | Name | Used for |
|---|---|---|
| 4200 | Realised FX Gain | Cash actually received/paid was worth more than carried |
| 5600 | Realised FX Loss | …worth less than carried |
| 4210 | Unrealised FX Gain | A still-open balance revalued upward at a period end |
| 5610 | Unrealised FX Loss | …revalued downward |

Realised means cash has moved and the result can never reverse. Unrealised is an opinion about an open balance that may well reverse next month. Never merge the two into one account — auditors and tax authorities treat them differently and the split cannot be recovered afterwards.

**Running a period-end revaluation.** Open foreign-currency receivables and payables are still carried at the rate they were booked at, which goes stale. To restate them:

1. **Preview first.** `GET /api/v1/finance/fx-revaluation?as_of=YYYY-MM-DD&rate_type=Closing` returns exactly what would be posted, per document, and **writes nothing**. Check the totals before committing.
2. **Commit.** `POST` the same endpoint with `{"as_of": "YYYY-MM-DD", "rate_type": "Closing"}`.

The rate type defaults to **Configuration → Finance → FX revaluation rate type** (Closing, which is what accounting standards name for balance-sheet items). Only genuinely booked balances are revalued — Approved sales invoices and Matched/Pending-Approval vendor invoices. Purchase orders and sales orders are *commitments*, not balances, and are deliberately left alone.

Running the same date twice is safe: the second run reports each document as *already revalued on that date* and posts nothing. A revaluation dated into a closed accounting period is refused, like any other posting.

**Reporting.** Three reports appear in the report catalog under Finance, with the usual CSV/PDF export and scheduling:

- **Open FX Exposure** — every open foreign balance and what it is worth at today's rate. This is the daily treasury view, and it is the same calculation the revaluation preview runs.
- **FX Gain/Loss Register** — every posting to the four accounts above, filterable to realised or unrealised. Use it to answer "where did this FX figure on the P&L come from".
- **Trial Balance in Presentation Currency** — your ledger restated into another currency at a chosen rate type.

⚠️ **The presentation trial balance applies one rate to every line.** A full IAS 21 translation uses the closing rate for balance-sheet items, the average for P&L items and historical rates for equity. This is an honest single-rate convenience translation — good for "roughly what does this look like in dollars", not a statutory consolidation. Every row carries the rate type used so it cannot be mistaken for one.

#### B.3.3 Turning on batch / lot traceability (Stage 42.1)

Batch tracking is **off for every item until you switch it on for that item**. Nothing changes for a tenant that never touches it — pick lists, receipts and stock figures behave exactly as before. Switch it on only for items where the lot genuinely matters: food, pharma, cosmetics, chemicals, anything with a shelf life or a recall obligation.

**Step 1 — configure the item.** Under **Setup → Items**, four fields appear on each item:

| Field | What it does |
| --- | --- |
| **Traceability** | `None` (default), `Batch`, `Serial`, or `Batch and Serial`. Only the two Batch modes have any effect today — `Serial` is accepted and stored, but the serial register itself is a later item (42.1.8). |
| **Shelf Life (days)** | Used to *derive* an expiry when a receipt gives a manufacture date but no expiry. Leave blank if cartons always print an expiry. |
| **Min Shelf Life on Receipt (days)** | Refuse a delivery arriving with less than this left. Your defence against a supplier clearing short-dated stock onto you. |
| **Min Shelf Life on Pick (days)** | Never allocate stock with less than this left. **This is the important one** — it's what stops goods expiring in a customer's hands. |

Two configurations are refused when you save, because both are silently unusable rather than merely odd: a negative number, and a receipt/pick minimum longer than the item's own shelf life (no delivery could ever be accepted, or no stock could ever be allocated, and nothing on a pick list would say why).

**Step 2 — there is no step 2.** Receiving clerks are prompted for a lot number automatically from then on, batches register themselves from the receipt, and pick lists switch from FIFO to FEFO for that item. See [User Guide](user-guide.md) §6.3 for what your users will see.

**What you can do that they can't.**

- **Hold or release a lot.** The `Batch` master (Setup → Batch, or via `POST /api/v1/wms/batch/status`) carries `Active` / `Quarantined` / `Blocked` / `Expired` / `Consumed`. Anything not `Active` is invisible to allocation. **Releasing a held lot back to Active requires a reason** — it's the first question a recall audit asks. `Consumed` is terminal on purpose: a fully-shipped lot never becomes Active again, a new receipt gets a new lot.
- **Run the expiry sweep.** `POST /api/v1/wms/batch/expiry-sweep` marks every past-expiry lot `Expired` and moves its remaining good stock into the existing **QC-Hold** bucket, which takes it out of `available` through the same path a damaged-goods call uses. It is **idempotent** — running it twice quarantines nothing twice — and it returns a count of what it did rather than changing things silently. It is deliberately operator-triggered rather than a background timer: it moves stock out of sellable, so someone should be able to point at a person and a time. If you want it on a schedule, call it from the existing scheduled-job path rather than expecting the server to do it on its own.
- **Correct a batch by hand.** `Batch` is an ordinary master, so bulk CSV import, field permissions and the audit trail all work on it. Note that a lot number only has to be unique **within its item** — two suppliers both shipping "LOT-001" for different SKUs is normal and allowed.

**One limitation worth knowing before you roll this out.** Turning on batch tracking for an item that *already* has stock on the shelves does not retroactively give that stock a lot — it can't, because nobody recorded one. FEFO will then only offer the stock that does carry a lot, and report the rest as short. That is the correct refusal (issuing untraceable units is what batch tracking exists to prevent), but it means **you should either switch an item over when its stock is low, or assign lots to the existing stock first** via `POST /api/v1/wms/batch/putaway`. The **Batch Stock Inquiry** report shows you exactly what has been assigned so far.

#### B.3.3.1 Barcodes generated automatically at goods receipt

**Stock availability review (Stage 55.6 partial).** Inventory and Current Stock
now expose `available_to_sell`, calculated by the reservation engine's shared
formula from base availability minus reserved, safety, blocked, QC-hold,
damaged, channel-buffer and active-hold quantities. `available` remains the
base bucket in the API for compatibility; do not use it alone to promise stock.
The browser check placed an actual Hold against a disposable stock row and
confirmed Available to sell fell while On hand did not. A later isolated
Chromium run followed Place Hold → linked Hold Release Request Draft → Submit
for Approval → a different Store Manager's Approve and confirmed available
stock returned only after approval. A transfer run exposed and fixed source
`on_hand` not decreasing at dispatch; the regression test and a second live
transfer show one unit leaving HO and entering the destination, without a
duplicate on-hand unit. Count/recount, lot/serial identity and independent
transfer-approval policy remain open Stage 55.6 gates.

Inventory's Transfer link now opens the real transfer composer with source and
item filled, rather than the generic TransferOrder record table. Its Count
link starts a scoped blind CycleCountLine with a generated session reference;
the new-line form omits system quantity, variance and lifecycle status, and
generic create validation accepts only Draft, nonnegative whole-unit counts
with no system-owned values. A Chromium run posted a zero-variance line.
Variance/recount approval and historical balance remediation remain unverified.

**Barcode policy (Stage 57.7)** — two settings under **Configuration → Inventory**:

| Setting | Choices |
|---|---|
| **Generate barcodes at** | **SKU creation** (default): a new Item saved without a barcode gets one immediately. **Goods receipt**: it gets one the first time it is received. **Off**: barcodes are only scanned, typed, or made with the **Generate** button. |
| **New barcode for each receipt date** | **No** (default): the same barcode for a SKU every time. **Yes**: each day a SKU is received it also gets that day's own barcode, printed on that receipt's stickers. |

Barcodes belong to the **SKU** — each Item, i.e. each variant. A design / Product Family has none. **Barcode** is no longer a field anyone has to type on the Item form; one you type or scan yourself is always kept, and nothing ever replaces an existing barcode.

**Receipt-date barcodes** are EAN-13s of the form `02` + `YYMMDD` + a 4-digit number for that day + check digit (up to 9,999 per day). They are listed under **Setup → Item Barcode** with the SKU, the date and the receipt(s) that used them. Scanning one anywhere — POS, pick/pack, sticker re-print — finds its SKU. One code per SKU per day: a second receipt of the same SKU on the same day reuses that day's code.

**Per item type and per item (Stage 60.3).** The two settings above are the client-wide default. Each **Item Type** (Setup → Item Type) can override them — **Generate barcode at** (Client default / SKU creation / Goods receipt / Off) and **Barcode per receipt date** (Client default / Fixed per SKU / New per receipt date) — and each **Item** can override its type with the same two fields ("Item type default" keeps the type's rule). The most specific one wins: **Item, then Item Type, then the client setting.** The shipped Accessory, Non-Asset, Free With Item, Gift and Backflush types are **Fixed per SKU**, so those SKUs keep one barcode even when the client issues a new one per receipt date; Fixed Asset issues none (assets carry an asset tag).

Receipt-time generation of the permanent barcode (below) still runs under **SKU creation** too, as the safety net for older, imported or API-created Items.

When a goods receipt is posted, every distinct SKU on it that has **no** barcode yet is issued one, as a valid **EAN-13** (correct check digit, so ordinary retail scanners read it). An item that already has a barcode is left untouched. It is idempotent: receiving the same SKU again, or the same SKU twice on one receipt, issues exactly one barcode and never changes an existing one.

What you need to know as the administrator:

- **It runs after the receipt has already posted, deliberately.** Stock movement is never held up or reversed by barcode generation. If generation fails, the receipt still stands and the item is simply left without a barcode. That failure is written to the **system error log** (§B.4) rather than shown to the receiving clerk, who cannot act on it anyway.
- **So check the log, not the user.** A clerk will not report this. If an item is mysteriously unlabelled, look for its entry in the system log, then generate the barcode by hand from the PIM screen.
- **It does not print.** Generation and printing are separate on purpose — nothing is sent to a printer by posting a receipt. Printing is the operator's explicit second step (§B.3.4, User Guide §7A.1).
- **Barcodes you have already assigned are safe.** If you imported your own barcodes, or your supplier's, receiving will not overwrite them.
- **This closed a real gap**: before it, a newly received item had no barcode, so its label had nothing to print and someone had to remember to generate one manually first.

**GRN batch numbers (Stage 61).** A fixed barcode cannot say which delivery a piece came from, so a GRN line left without a batch number gets one when the GRN is saved — before anything posts, so the GRN, its stickers, the stock ledger and the Batch record all carry the same value. A number the clerk types is always kept. What you control:

**How often (Settings → Inventory → GRN batch numbers, then Item Type, then Item).** The same precedence as the barcode rules: the Item's **GRN batch number** field wins, then its **Item Type**'s, then the client setting. "Client default" / "Item type default" (or blank) inherit, so existing records keep the client rule.

| Value | Effect |
|---|---|
| **One per SKU on each GRN** (default) | Lines of the same SKU on one GRN share a number; a number typed on one of them covers the others. |
| **One per GRN line** | Every blank line gets its own number. |
| **One per SKU per day** | Every GRN receiving the SKU at the same location on the same day shares one number; its Batch record adds up their quantities and lists every GRN. |
| **Off** | No number is filled in. |

**What it looks like (Admin → Prefix Configurations → "GRN batch number (GRNBatchSeq)").** An ordinary number series: **Prefix**, **Store in number** (the receiving store's short code), the period, the **Number** length and **Suffix**, joined by the **Separator**. **Restart numbering** here means **Every day** (the receipt date is in the number — B-261016-0001, the default), **Every month** (B-26-10-0001) or **Never** (no date — B-0001). The Next-number column previews it. Its prefix may repeat another series' prefix, since a batch number is never a record id. **Keep it short on small labels**: on a 50 × 25 label at 203 dpi the default fits with 2-dot bars, but a longer prefix (e.g. LOT) or a 5-digit number makes the bars 1 dot wide — the Sticker Studio's scan check warns when that happens; widen the barcode or shorten the format.

What else to know:

- **Two kinds of line are never numbered.** A **batch-tracked** item (Traceability = Batch) still needs the lot printed on the carton (INVENT-0115): that is a control you switched on per item, and an invented number would hide a missed entry. A **Fixed Asset** item never enters stock.
- **Each batch is registered** under **Setup → Batch** with its **Goods Receipt**, **Received On**, **Received At** (location), **Supplier** (the PO's vendor) and the received quantity.
- **Two reports** (Reports → Inventory; User Guide §6.3): **Batch Lookup & Ageing** (scan a batch, barcode or SKU → receipt date, age, age band, GRN, supplier, received and estimated left) and **Stock Ageing (by GRN batch)** (each item's stock on hand in 0-30 / 31-60 / 61-90 / 91-180 / 180+ day bands). "Left" is a **first-in-first-out estimate**: a POS sale scans the SKU's barcode and records no batch, so the item's stock on hand across all locations is laid against its newest batches first. Stock beyond everything the batches received shows as **Before batch numbers**.
- **Labels.** The built-in label prints the batch as a second Code 128 barcode with the number under it. On custom templates, drag **Batch / Lot** from the Studio's Data panel onto a barcode element (§B.3.4).
- Fixed with it: two lines of the same SKU (and lot) on one document used to write only the first line to the **stock ledger** (the second looked like a replay), so the ledger under-counted while stock on hand was right. They now write one ledger entry for the combined quantity.

#### B.3.3.2 Bulk import: Master codes and Design-based SKUs

**Stage 51.10:** on **Setup → Item → Bulk Import** there are two template buttons. **Download Template CSV** is the full Item template, unchanged, with `id` and `code`. **Download Template (SKU from Design)** has no `id` or `code` column and puts `family` first, so a user who fills it in as downloaded gets Design-based SKUs. Variation goes in `variant_option_values` (`size:S`) unless the tenant's Items carry the jewellery attribute fields. Uploading a row for a SKU that already exists updates that Item (see the last paragraph below). User Guide §8d.5 has the steps.

**An import file meets the same checks as a save (49.3.4, 49.3.5).** A row that updates a record of another store is refused for a user limited to one store (the store named is in the row's error), even when the row names the user's own store; a row that changes a posted document's amounts or un-posts it, or makes a status move the record type's rules do not allow, is refused with the same message the record form gives. Full-access roles are not limited.
Two things about CSV import that are worth knowing before you run a large load, both corrected as of Stage 51.8:

- **A Master record imported with a blank `id` now takes its `code` as its id.** Every Link field in this system resolves by *id*, so a master whose id and code disagree can never be selected by any Link field — an imported Vendor that exists but which no Purchase Order can reference. Import now applies the same id = code rule the on-screen save path does. **If you imported masters before this**, audit them with `scripts/audit_master_id_code_drift.sql` (read-only, see its header for how to run it); it reports how many rows are affected per record type, whether a fix would collide with anything, and how many transactions already point at the old ids.
- **An imported Item under a Design now gets a real SKU.** Give the file a `family` column naming the Product Family (Design), and import records the variant attributes and numbers the Combination ID exactly as the form does — since Stage 117 the next number in the **Combination ID / item** series (`I00042`), not the design plus its values. Previously this only worked on the single-record screen, so bulk-imported variants got plain sequence numbers (`Item/HQ/2026/000123`) instead of SKUs. See User Guide §8d for what the resulting codes look like.
  **You can leave the `code` column out of the file entirely** in this case — normally an Item upload with no `code` column is rejected up front, but when a `family` column is present the server is generating the code, so it is no longer demanded. That exception is deliberately this narrow: an Item file *without* a family column, and every other record type, still requires `code`.

**Re-importing a Master by code now updates it rather than duplicating it**, which follows from the first point: the code resolves to the same id, so the row is matched and updated. Keep that in mind for a repeated load — it is usually what you want, but it does mean a second upload of the same file overwrites rather than being rejected.

**Each template has a column guide (47.13.2).** Download it with `/api/v1/import/<RecordType>/template?variant=dictionary` (the file is `<RecordType>_dictionary.csv`; there is no button for it yet). It lists every column of that template in order, with its type, whether it is required, what it means, the format (dates YYYY-MM-DD, dot decimals), the allowed values, an example, the records it needs first and whether it must be unique. **An update row changes only what it fills (47.13.2a):** a blank cell or a missing column keeps the stored value, and what the system keeps on a record (GST split, BOM snapshot, payments) is never touched. To empty a field, type `<clear>` in its cell; a required field cannot be emptied. The required columns must still be in the file, but on an update their cells may stay blank. An imported purchase order gets its GST split and totals from its lines, as one saved on screen does. A cell such as `=HYPERLINK(...)` or `@SUM(...)` is kept as plain text, and an export writes it so that a spreadsheet never runs it as a formula.

**Duplicate suppliers and Duplicate items (47.13.5).** Reports → Admin. They group records that look like one business or one product - the same GSTIN, the same PAN under GSTINs of different states, phone, email or name (company-form words ignored) for suppliers; the same barcode, GTIN, or name and HSN for items (the combinations of one design share its name, so for them the values must match too) - and propose which to keep: the one with the most transactions, then a valid GSTIN (or a barcode), then the most complete, then the oldest. Merging is by hand for now: move the open documents to the kept record, then deactivate the other.

#### Store range (Settings -> Inventory -> Store range)

Off by default. **Warn** or **Block** makes a store (or channel) that has rows under *PIM -> Range* check new purchase orders for the store, transfers into it, sales there and channel orders against that range; Warn shows a message and records an *Outside assortment* item exception, Block refuses. Stores and channels with no rows are never checked, so turning it on changes nothing until range rows exist. Who may add rows: Super Admin and Store Manager can add one at a time from the Assortment list; adding a category to many stores at once (*PIM -> Range -> Add to Range*) is for administrators. Markdown plans (*PIM -> Markdowns*) need no setting: a Scheduled plan applies on its dates. Error codes PIM-9240 to PIM-9245 (range) and PIM-9250 (markdown plan) are in the error-codes guide.


### People settings (HR)

| Setting | Default | Effect |
|---|---|---|
| `hr.manager_team_scope` | Off | A manager sees leave, attendance and appraisals of direct reports (the Manager field on Employee) and approves their leave only. |
| `hr.exit_clearance_required` | Off | A leaver with an open offboarding checklist cannot have the payslip posted. |
| `hr.analytics_min_group` | 5 | A people-analytics group smaller than this is folded into one Other row. |
| `hr.certificate_reminder_days` | 30 | HR is notified once this many days before a certificate runs out. 0 turns it off. |

Notification event: **Certificate Expiring**. The **Certificate Expiry** report (HR) uses the same days unless the user types others. Holiday calendars are set per location under **Holiday Calendar**; with none, leave and attendance count as before.

## Quality: AQL sampling, supplier actions and lot trace (170.14 - 170.16)

There is nothing to switch on. Each part works only where you use it.

1. **AQL.** Open an **Inspection Plan** and pick an **AQL** (0.065 to 6.5). Inspections recorded against that plan get their sample size and accept / reject numbers from ISO 2859-1, general level II, single sampling, normal inspection, and a Pass or Fail from the defects found. Leave AQL blank and the plan behaves as before. Check the table against your copy of the standard before you rely on it for a contract: the numbers are built into the program and a different level or switching rule is not offered.
2. **Supplier actions.** Roles that can use Quality can open and work a **Supplier CAR**. The vendor **Scorecard** then shows **Defects per million** (rejected quantity over received quantity, times a million) beside **QC reject**, and the count of **Open SCARs**.
3. **Lot trace.** Work orders completed into a named output lot record which input lots went in. The record is kept as **Lot Genealogy** documents, read-only to everyone. Add the optional **Input lots** list to a production order to write the lots down; without it the trace shows the lots the stock would have been drawn from, marked *Inferred*.
