---
title: Inventory & Warehouse Operations
section: Module Handbooks
order: 20
summary: Receive stock against a PO, put it away, pick and pack it back out, count it, adjust it, and move it between locations — the floor-ops backbone every other WMS feature sits on top of.
audience: warehouse operator, warehouse manager, admin
last_verified: 2026-09-03
screens: [grn, rf-receiving, putaway, wave-picking, cycle-count, transfers, warehouse-cockpit, inventory, stock-locations, stock-moves, module:inventory, reports]
doc_id: DOC-79A300F5E0
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
topic_type: how-to
module: inventory-wms-operations
task: Inventory & Warehouse Operations
prerequisites: Signed-in account with permission for the described task; observe the article prerequisites
---

# Inventory & Warehouse Operations

For dock and floor tasks, use [dock appointments](appointment-calendar.md),
[yard movement](yard-board.md), [inventory holds](place-hold.md),
[put-wall sortation](sortation.md) and [loading/departure](loading-dock.md).

A warehouse in this ERP is built from a small set of primitives used
everywhere: a **Location** (a store, warehouse, or HO) holds stock at the
`inventory_availability` level (`on_hand`/`available`/`in_transit`/
`qc_hold`/`damaged`); a **Bin** inside a location optionally holds a
finer-grained breakdown of that same stock (`bin_stock`, by SKU and
**condition** — Good, Damaged, QC-Hold, RTV) — a bin is never a second
source of truth for the total, only for *where* and *what state* it's in.
Every floor action below — receiving, putaway, picking, packing, counting,
adjusting, transferring — is a controlled move of quantity between those
buckets, each one logged to the stock ledger with a voucher type and id so
it can be traced back later.

All of this lives behind the WMS product package: every route under
`/api/v1/wms/*` runs through `moduleGate("wms", ...)`, and a tenant that
hasn't purchased WMS gets refused with `SAAS-0191` on every one of them. If
your screens quietly don't work, that's the first thing to check with an
admin before assuming something is broken.

This handbook does not re-explain lot/serial capture, FEFO picking, or
expiry handling — that's
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md)'s job.
Where a screen here also carries a batch/serial field, this doc just notes
that it's there and links out.

## Receiving

### GRN Workbench (desktop, full detail)

**Setup → Procurement → GRN**, or the **GRN** sidebar item, opens the GRN
Workbench (`renderGRNWorkbenchView`) — every goods receipt in the system is
created here, and there is no separate "capture" step before it.

1. The **GRN Number** fills in automatically on save. Enter the **PO
   Reference** (required — a GRN is a Transaction doctype tied to a
   `PurchaseOrder`, `db/migrations_phase3.sql`) and click **Load Items from
   PO** to pull in its ordered lines, or type a **Receiving Location**
   yourself.
2. **Receiving Location defaults from the PO's target warehouse** if you
   leave it blank (`PrepareGRNReceipt`, Stage 30.2.1) — it's a required
   field on the document, so this default exists specifically so a clerk
   who filled in the PO doesn't have to retype where it ships to.
3. Optionally enter an **ASN Reference** and click **Load Items from ASN**
   instead — a receipt can be pre-filled from either source.
4. Per line: **SKU**, **Ordered Qty**, **Received Qty**, then the QC split —
   **Rejected Qty** (+ a **Rejection Reason**, required once rejected > 0) and
   **Damaged Qty** (+ a **Damage Reason**, required once damaged > 0). What's
   left after both is the accepted quantity that actually lands in sellable
   stock.
5. For a batch- or serial-tracked item, the line grows a **Batch / Lot**
   row or a **Serial Numbers** row automatically — see
   [Batch, Serial & Expiry Traceability](traceability-batch-serial.md).
6. For an item flagged catch-weight, an **Actual Weight** field is required;
   **L / W / H** dimensions are always optional, for any line.
7. Click **Add Line**, repeat, then **Save receipt**, and work the receipt
   through Verify, Price check and MRP and post ([Procurement](procurement.md),
   step 4 is where stock moves).

**What posting actually does** - RF Receiving's **Post Receipt**, and each
child GRN at step 4 (`PostGRNReceiptWithQC` / `PostGRNReceiptPart`,
`engines/wms_receiving.go`), per line:

- The **accepted** quantity (received minus rejected minus damaged, or an
  explicit `accepted_qty` if the caller sends one) posts to `available`
  through the same ledger path every other receipt uses.
- **Rejected** and **damaged** quantities move `on_hand` up but land in the
  `qc_hold` and `damaged` buckets respectively, not `available` — the goods
  are physically in the building but not sellable. Each split gets its own
  stock-ledger entry (`ToStatus: "QC-Hold"` / `"Damaged"`) so the reason it
  didn't go straight to stock is traceable later.
- The receipt is costed and posted to the general ledger in the same call
  (Dr **1200 Inventory** / Cr **2100 GRN Suspense**, Stage 37.3) — a GRN is
  the one document in this flow that touches the GL directly, not just
  inventory.
- **If the stock post fails after the GRN document itself is already
  saved**, the GRN is automatically reversed to `Cancelled` (with the
  failure reason recorded on `posting_error`) rather than left as a
  "successful" receipt with no stock behind it — the PO stays open for a
  real retry instead of counting a phantom receipt against it
  (`PURCHA-0084` would otherwise block re-receiving a PO the system
  believes is already fulfilled).

The GRN list at the bottom of the screen shows every receipt's **Received /
Rejected / Damaged** totals and status (`Pending`, `Approved`, `Cancelled`).

### RF Receiving (scan-only, against an ASN)

**WMS → RF Receiving** is a second, narrower way to create the same kind of
GRN document, built for a handheld scanner rather than a keyboard-and-mouse
desk: scan or type an **ASN** number, confirm a **Receiving Location**, then
scan each carton's SKU. Each scan confirms the *entire* remaining expected
quantity for that SKU in one tap — there's no partial-qty entry, and no
reject/damage/batch/serial capture here by design; that stays on the GRN
Workbench, "the same scope boundary Infor's own RF receiving screens draw:
an RF gun confirms quantities fast, a supervisor desk handles exceptions."

Two things this screen is strict about:

- **The ASN must have a PO reference.** If it doesn't, RF Receiving refuses
  to post ("a GRN requires one") and tells you to use the GRN Workbench
  against that ASN instead.
- A scan that doesn't match an outstanding line on the ASN is rejected
  in-place ("is not an outstanding line on this ASN") rather than silently
  ignored.

**Post Receipt** here calls the exact same `POST /api/v1/doc/GRN` endpoint
the desktop Goods Receipt page uses — it's a different data-entry surface for
the same document type, not a parallel receiving mechanism. The desktop page
saves its receipt in steps (`"flow": "steps"`), so its stock moves at step 4;
RF Receiving posts at once.

### Receiving error codes

| Code | Meaning |
|---|---|
| `GOODSR-0089` | Accepted quantity exceeds received quantity. |
| `GOODSR-0090` | Rejected quantity given with no rejection reason. |
| `GOODSR-0095` | A vendor invoice was posted against a GRN that isn't completed. |
| `GRN-0253` | GRN cancellation blocked — a downstream vendor invoice or stock movement already exists against it. |
| `PURCHA-0084` | The PO this GRN references is closed; no further receipts allowed against it. |

### Reading receiving activity

**Reports → Procurement → GRN Register** (`grn-register`) lists every GRN —
ID, PO reference, line count, status, date. It has no parameters; it runs
as soon as you pick it.

## Putaway

**WMS → Putaway** (`renderPutawayView`) has three independent panels, all
against stock already `available` at a location but not yet assigned to a
bin.

### Manual putaway

Enter **Bin Code**, **SKU**, **Qty**, and click **Put Away**
(`PutawayToBin`). It refuses more than the location's *unassigned*
on-hand quantity — on-hand minus whatever is already binned across every
bin/condition for that SKU at that location — so a bin can never claim
stock the location doesn't actually have. A few more checks run before the
placement is accepted:

- The bin must be an **Active** record and not operationally `Blocked`,
  `Full`, or `Counting` (a bin frozen for a physical inventory count —
  see Cycle Counts below — is unavailable for putaway until the count
  closes).
- If the bin has a configured **capacity** (max qty / max weight / max
  volume), the putaway is refused if it would push the bin over any of
  them.
- A hazmat-classified item can only go into a bin whose zone has opted in
  to hazmat storage.

**Suggest Bin** (fill in **SKU**, **Qty**, and **Location**, click
**Suggest Bin**) calls the directed-putaway strategy
(`GET /api/v1/wms/putaway/suggest-bin`) and fills the Bin Code field for
you when it finds one — a convenience, never a gate; "no suggestion" is
shown as plain text and you can still enter a bin manually.

### Cross-dock staging

Two more panels on the same screen skip shelving entirely when an outbound
order is already waiting on the exact SKU you're about to put away:

- **Cross-Dock Staging** — **Check Opportunity** looks for an open
  transfer/sale already demanding this SKU at this location; if matched,
  **Stage for Cross-Dock** moves it straight to outbound staging instead of
  a shelf bin.
- **Planned Cross-Dock / Flow-Through / Transship** — the same idea, but
  staged against a **Cross-Dock Plan** raised ahead of receipt (**WMS →
  Cross-Dock Plans**) rather than scanned for live demand.

## Picking & Packing

Order-fulfillment picking and packing in this app is two layers: a
**bin-level pick list** that tells a picker where to walk, and the
**pick and pack scans** on the `FulfillmentTask` itself (OMS → Pick / Pack)
that actually drive the order forward.

### Bin-grouped pick list

The bin-level pick list (used by WMS picking; OMS → Pick prints the task's
own **Pick list** template) is a read-only view (`GET /api/v1/wms/pick-list?task_id=...`,
`GenerateBinPickList`) — the bins/zones/aisles/racks to walk, in
walk-route order, for that one task's items. A SKU with insufficient binned
stock shows a **Shortfall** rather than failing the whole list, so a picker
still gets a usable list for what's actually available right now. If a
task's items were never put away into bins at all, the list says so and
tells you to pick from general stock instead — that's expected, not an
error.

### Wave / Batch Picking

**WMS → Wave / Batch Picking** consolidates several open fulfillment tasks
into one walk instead of one per order:

1. **Make a wave** — choose the **Location**, tick the orders under
   *Orders ready to wave*, choose **Into** (*A new wave* or an open one) and
   click **Put N selected into the wave**.
2. **Release it** — under *Open waves*, click **Release**. Then **Pick list**
   shows the consolidated, walk-route-sorted pick list and, per order, what
   could be allocated and what is short. **Pick on a phone** opens Mobile
   Picking with that wave.

User Guide §7.4 has the full wave → pick → pack → load flow.

For a batch-tracked item, allocation here is **FEFO**, and the pick list
shows the batch/expiry to take — see
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md).

**Mobile Picking** (**WMS → Mobile Picking**) is a phone-friendly,
one-item-at-a-time reader for the exact same wave pick list — choose the
**Location**, then the **Wave** from the list (User Guide §5.1), and it steps
through the list as big-text cards, with optional voice
readout (`speechSynthesis`) and voice "next"/"confirm" control
(`SpeechRecognition`, where the browser supports it).

> [!NOTE]
> **Confirm & Next records the line on the server but takes nothing out of
> a bin.** Each **Confirm & Next** on Mobile Picking is saved with who
> picked it (`POST /api/v1/wms/wave/pick-confirm`), so reopening the wave
> starts at the first line still to go. After the last line, **Mark N
> order(s) packed** moves the complete orders to Packed. A short order is
> left as it is — finish it on **OMS → Pick** (User Guide §7.4).

### What actually moves the order forward

The order itself advances on **OMS → Pick / Pack / Ship**, by scans
(User Guide §9A.9 and §9A.10):

1. **OMS → Pick** → **Pick** opens the scan panel. Scan each unit's barcode
   (or type its code and press Enter). Each scan counts one unit against the
   task (`POST /api/v1/wms/pick-scan`, `ScanPickItem`).
2. If the rest of a line cannot be found, click **Short-pick** and give a
   reason (`POST /api/v1/wms/short-pick`, `ShortPickLine`). Its reserved
   stock is released.
3. When every line is picked or short-picked, **Go to Pack** opens the same
   task on **OMS → Pack**. **Pack** opens the scan panel: scan each picked
   unit into the box (`POST /api/v1/wms/pack-scan`, `ScanPackItem`).
4. Enter the box **weight**, tick **Bill and documents are in the box** if
   your business checks that, and click **Pack, bill and label**. This runs
   the pre-pack checklist and closes the pack task
   (`POST /api/v1/wms/pack-complete`), then makes the package, the OMS bill
   and the courier booking.

**Reject** (from Pending or Picking) uses the plain status transition
(`POST /api/v1/fulfillment/task/transition`), as does Mobile Picking's **Mark
N order(s) packed**.

### Beyond picking: sortation and loading

Two further outbound stages exist as their own screens for a warehouse that
sorts into carrier/route slots and manages dock scheduling before dispatch —
**WMS → Sortation** (provision/assign/confirm/clear slots) and **WMS →
Loading Dock** (create a loading task, scan cartons onto it, complete, and
generate a Bill of Lading). Both are wired end-to-end but are a layer above
the picking/packing basics this handbook focuses on.

## Cycle Counts

**WMS → Cycle Count** is a five-panel screen, and each panel is a distinct
step of the same workflow — this is also the *only* mechanism in the app
for adjusting stock quantities (see Stock Adjustments below).

**1. Enter counted quantities.** Count lines are a `CycleCountLine`
document (fields: **Count Session**, **Location**, **Bin** (optional),
**SKU**, **Counted Qty** — all except Bin required), entered the same way
any other bulk line-item load happens in this app: **Manage Count Lines**
opens the generic doctype table for `CycleCountLine`, where **Bulk Import**
takes a CSV. There's no bespoke count-entry form.

**2. Reconcile a session.** Enter the **Count Session** value your lines
share and click **Reconcile Session**
(`POST /api/v1/wms/cycle-count/reconcile`, `ReconcileCycleCount`). Every
not-yet-processed line in that session is compared against the system's
current `on_hand`:

- **Zero variance posts immediately** — nothing to approve, nothing at
  risk.
- **Any non-zero variance routes to the maker-checker approval engine**
  instead of touching inventory unreviewed — the same engine every other
  approval-gated document in this ERP uses. The result badge shows how many
  posted with no variance and how many are now pending approval.

**3. Variance root-cause + posting.** A non-zero-variance line cannot post
even after approval until it has a **Reason Code** — enter the **Line ID**
and a **Reason Code** (a `ReasonCode` record) and click **Set Variance
Reason**, then **Retry Post**
(`POST /api/v1/wms/cycle-count/post-adjustment`, which is really a *retry*
of `PostCycleCountAdjustment` — the same function the approval decision
itself calls on Approve, exposed again here for the case where the reason
wasn't set yet when it was first approved). Posting is idempotent against
double-clicking — a line already `Posted` refuses to post twice.

**4. Blind recount.** If a count result looks wrong before trusting it
enough to post, **Request Recount** creates a fresh line for the same
session/location/bin/SKU with **no counted or system quantity carried
over** — the second counter is blind to both the first count and the
system's own number. Enter its value with **Submit Recount Value**.

**5. ABC cycle-count planner.** Enter a **Location** and click **Get Plan**
(`GET /api/v1/wms/cycle-count/abc-plan`, `GetABCCycleCountPlan`) to see
every SKU on hand there classified into an **A/B/C velocity tier** (top 20%
of SKUs by 30-day sales velocity are A, next 30% are B, the rest C — a SKU
with no sales velocity lands in C, so nothing goes unclassified) with
whether it's **due** for its tier's recount interval. Interval defaults
(**Settings → Configuration**, module *Warehouse*): **A = 30 days**
(`wms.cycle_count_tier_a_interval_days`), **B = 60 days**, **C = 90 days**.

### What actually posts to inventory

`PostCycleCountAdjustment` applies the signed variance to *both* `on_hand`
and `available` at once — a physical count correction, not a
sale/hold/reservation — and writes a `CycleCount`-voucher stock ledger
entry so the correction is traceable to the line and reason code that
justified it.

### Full counts (Physical Inventory)

A **full count** (physical inventory) counts *every* SKU at a location (or a
zone within it) — `StartPhysicalInventory`, `SubmitPhysicalInventoryCount`,
`ReconcilePhysicalInventory`, `ClosePhysicalInventory` and
`CancelPhysicalInventory` (`engines/wms_physical_inventory.go`). It freezes
the bins involved (`bin_status = 'Counting'`, which also blocks putaway and
picking allocation against them for the duration) and creates one
`CycleCountLine` per SKU under a `PhysicalInventory` header, reusing the
exact same reconcile/post/approve mechanics described above rather than a
second counting system.

**Counting has a screen — the Store app on a phone** (User Guide §10A.9):

1. Open the Store app and tap **Count**.
2. An open full count for your store is listed under **Full counts at this
   store**. Tap it.
3. Scan every piece (fix a number with **−** / **+**), then send the count.
   Several people can count the same full count on their own phones; each
   phone's numbers are added together.

> [!WARNING]
> **Starting, reconciling, closing and cancelling a full count still have no
> screen** (checked by a search of `public/`, 2026-10-24). No screen calls
> the `start`, `reconcile`, `close` or `cancel` POST actions under
> `/api/v1/wms/physical-inventory/`. The warehouse team has to call these
> directly; the Store app only counts a full count that is already open.

## Stock Adjustments

There are two ways to correct a quantity:

- **Count it** — enter the real quantity as a `CycleCountLine` against the
  current session and reconcile. This goes through the Cycle Count
  reconciliation/approval/reason-code path above
  (`PostCycleCountAdjustment`).
- **Inventory → Stock Adjustment** — when no other document explains the
  difference (damaged or expired goods, shrinkage, stock found). Click
  **+ New Stock Adjustment**, choose the **Store** and **Loss class**, add
  each item with its **Change** (**-3** takes three off, **4** adds four) and
  a **Reason** per line, then **Save adjustment**. If an approval rule exists
  for *InventoryAdjustment*, it waits as **Pending Approval**; otherwise it
  posts at once (User Guide §5.5).

**Condition-level moves** are the other kind of "adjustment" — moving stock
between **Good**, **Damaged**, **QC-Hold**, and **RTV** within the same bin,
without changing the total quantity on hand. **WMS → Bin Conditions**
(`renderBinConditionsView`) does this: enter **Bin Code**, **SKU**, **Qty**,
**From Condition**, **To Condition**, click **Move**
(`POST /api/v1/wms/condition-transition`, `TransitionBinStockCondition`).

- Moving stock **out of Good** makes it unsellable (`available` decreases);
  moving **into Good** makes it sellable again (`available` increases).
  Any other pair (e.g. `Damaged` → `RTV`) doesn't touch `available` at all.
- `on_hand` never changes here — the stock never left the building, only
  its condition did.
- Any transition pair is allowed; which moves make operational sense (e.g.
  "damaged goods go to RTV, not straight back to Good") is a judgment call
  left to the operator, not hard-coded.

This is the same `bin_stock` condition bucket the GRN QC split (Rejected/
Damaged quantities) and the batch expiry sweep's quarantine move both write
into — see
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md) for how
an expired lot ends up in `QC-Hold` automatically.

## Stock locations inside a store (ERP, not WMS)

Every store's stock also sits in named **stock locations**: **Main** (the
selling floor), **Inward**, **In-Transit**, **QC Hold**, **Damage**, **On
Hold**, **Purchase Return**, and any the business adds (an *Alteration* rack,
say). They belong to the ERP, so a client without WMS has them too. WMS reads
them: a bin's condition Good / Damaged / QC-Hold / RTV is the store's Main /
Damage / QC Hold / Purchase Return, and changing a bin's condition moves the
units into that location.

- **Stock → Stock Locations** (Super Admin) has three tabs. **Locations**:
  each one's name, whether it sells, which kinds of store use it, and whether
  new stores get it. **Stores**: which locations each store uses.
  **Transactions**: where a Goods Receipt or Transfer In lands (e.g. Inward)
  and where a damage move goes. Main always sells; QC Hold and Damage sell
  only when you mark them; the others never do. A location still holding
  stock cannot be switched off (`STKLOC-0861`).
- **Stock → Move Stock** moves an item between two locations of one store:
  **+ New Move**, then the item, quantity, From, To and a reason. Each move is
  a **Stock Move** document (series `SM`) and moves the stock in the same
  step. Stock Move records cannot be made any other way (`STKLOC-0865`), and a
  move larger than the From location holds is refused (`STKLOC-0864`).
- **What can be sold** (the till, the online channels, sourcing, reports and
  the public API) is Main, plus QC Hold / Damage when they are marked to
  sell. **Reports → Stock by Location** shows every store location by
  location.

## Transfers (Stock Movement)

The basic transfer workflow is Draft →
Approved → Dispatch → Receive click-through. This section goes deeper into
what actually happens at each step and where it can go wrong.

**Inventory → Stock Movement** (`renderTransfersView`) drives a `TransferOrder`
through five possible states:

1. **Draft** — add **From Warehouse**, **To Warehouse**, and one or more
   **SKU + Qty** lines, then **Create Transfer**.
2. **Mark Approved.**
3. **Pack** *(optional)* — once Approved, either **Pack** (prompts for a
   **Box ID** per line, grouping lines that share a box) or **Pack
   (Suggested Cartons)** (`POST /api/v1/wms/cartonization/suggest`, a
   first-fit-decreasing box-fill suggestion for a given carton type, which
   you confirm before it packs) — both call
   `POST /api/v1/wms/transfer/pack` and move the order to **Packed**. This
   step is a confirmation, not a gate: **Dispatch** stays available directly
   from **Approved** too, with or without packing first.
4. **Dispatch** — moves each line's quantity out of the source location's
   `available` into `in_transit`. Refuses if requested quantity exceeds
   what's actually available (`STOCKT-0112`).
5. **Receive** — click **Receive** and enter, per item, what arrived:
   **Good**, **Damaged**, **Lost** and **Extra** (User Guide §7, step 6).
   - Damaged, lost and extra units need a **reason** (reason codes of type
     *Transfer Discrepancy*; **Add a reason code** opens the master list if
     there are none yet).
   - With **Units not entered here stay in transit** ticked (the default),
     the rest waits for the next delivery and the transfer shows **Partially
     Received**. Untick it to close the transfer now: what is not entered is
     recorded as lost, with the reason.
   - Click **Record Receipt**. Only what is received is added to the
     destination's stock; damaged units are kept out of sellable stock.

A short receive is therefore done on this screen: enter the lower quantity,
choose the reason, and either leave the rest in transit or close it as lost.

### Transfer error codes

| Code | Meaning |
|---|---|
| `STOCKT-0111` | Source and destination location are the same. |
| `STOCKT-0112` | Dispatch quantity exceeds what's actually available at the source. |
| `STOCKT-0113` | Attempted to close a transfer before its receipt is complete. |
| `TRN-0259` | Received quantity doesn't match dispatched quantity, and no shortage/damage reason was given — the Receive dialog prompts for one automatically on a short line. |

## Warehouse Cockpit

**WMS → Warehouse Cockpit** (`GET /api/v1/wms/cockpit?location_code=...`)
is the one-screen operational dashboard for a location — enter a Location
and **Refresh**:

| Section | Shows |
|---|---|
| Open Tasks by Type / Age | Count and oldest age per task type/status. |
| Exception Queue | Tasks stuck at a process step with a flagged follow-on action needed. |
| Wave Status | Task counts per wave status. |
| Wave Monitor | Every active wave with an **Advance** button that steps it Planned → Released → In Progress → Complete → Closed (`POST /api/v1/wms/wave/transition`), plus a **Run Template Now** field to fire a Wave Template on demand. |
| Inbound Due Today | ASNs expected today, with status. |
| Today's Throughput | Tasks/hour per user per task type — the same signal behind the **Labor Productivity** report. |
| Bin Utilisation | % used per bin, for any bin with a configured capacity — red at ≥90%, amber at ≥70%. |

## Reports

| Report | Answers |
|---|---|
| **GRN Register** (`grn-register`) | Every GRN — PO reference, line count, status, date. |
| **Labor Productivity** (`labor-productivity`) | Tasks and tasks/hour per user per task type, over a date range. |
| **Slotting / Re-Slotting Suggestions** (`slotting-suggestions`) | Which SKUs at a location should move bins, and why, ranked by velocity tier. |
| **3PL Storage & Handling Billing** (`3pl-storage-billing`) | Storage and handling charges per owner/location for a billing period. |

Batch/serial-specific reports (Near-Expiry Watchlist, Batch Stock Inquiry,
Batch Movement History, Serial Number Inquiry, Serial Movement History) are
covered in
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md) rather
than duplicated here.

## Troubleshooting

**A WMS screen just doesn't load, or every action 403s.** Check that the
WMS product package is actually enabled for this tenant — every
`/api/v1/wms/*` route refuses with `SAAS-0191` if it isn't, regardless of
the user's own role.

**"Accepted quantity cannot be greater than received quantity" on a GRN
line (`GOODSR-0089`).** The line's accepted qty (received minus rejected
minus damaged, unless you set it explicitly) exceeds what was received.
Check the rejected/damaged split.

**"Rejection reason is required for rejected quantity" (`GOODSR-0090`).**
Any line with **Rejected Qty > 0** needs a **Rejection Reason** — the same
applies to **Damaged Qty** and its own reason field.

**A GRN shows status `Cancelled` right after posting.** The stock post
itself failed after the document saved, and the system reversed it to
`Cancelled` automatically rather than leaving a "successful" receipt with
no stock behind it — check `posting_error` on the document (or the system
error log) for why, fix it, and receive again; the PO stays open for a
retry.

**RF Receiving won't post: "ASN has no PO reference."** RF Receiving only
creates a GRN, and a GRN requires a PO. Use the GRN Workbench against this
ASN instead, or attach a PO to the ASN first.

**Putaway refuses with "exceeds unassigned on-hand stock."** The bin (or
bins) already holding this SKU/location combination already account for
everything currently `available` there — there's nothing left to place. A
mis-scanned bin earlier is the usual cause; check `bin_stock` for the SKU
across all bins at the location.

**A cycle-count line won't post even after approval.** It needs a
**Variance Reason Code** first — every non-zero-variance line requires one
before `PostCycleCountAdjustment` will run, whether that's its first post
attempt or a retry. Set one on panel 3, then Retry Post.

**A Transfer Order receive is refused with "a shortage/damage reason is
required."** Choose a **Reason** in the Receive dialog — it is needed for
any damaged, lost or extra units, and for a short line when **Units not
entered here stay in transit** is unticked (`TRN-0259`).

**"Source and destination location cannot be the same" (`STOCKT-0111`).**
Self-explanatory — pick two different locations for a transfer.

**Wave pick list is empty for a wave I just put orders into.** Nothing in
that wave's tasks is currently stored in a bin — putaway hasn't happened
yet for that stock, or it's being picked from general (non-binned) stock
instead. That's a valid state, not an error.

## What is not here yet

**Starting, reconciling and closing a full count (Physical Inventory) has no
screen** — see the warning under Cycle Counts. The ABC-sampled cycle count
workflow is fully wired, and the Store app counts an open full count; but
opening, reconciling, closing or cancelling one is still a direct API call
(checked by a search of `public/`, 2026-10-24).

## Stock ownership per warehouse

The current warehouse configuration uses **one stock owner per warehouse**. An unassigned
warehouse adopts its first owner through an owner-stock write. A second owner is refused;
reassignment is also refused while the incumbent still has stock. If an ownership refusal
appears, stop the receipt/change and ask the warehouse administrator to check the location,
owner assignment and existing stock. Do not relabel stock to bypass the refusal.

Tenant administrators must retain the default `wms.stock_ownership_mode = single_owner`
for this configuration. The explicitly named `mixed_unsupported` mode does not isolate
allocation or picking by owner and is outside supported operation. Check your locally
approved warehouse procedure and acceptance record before changing configuration. Follow
the [access and approval guidance](security-approvals.md) and ask the warehouse administrator
to confirm the owner assignment and reconciliation responsibilities.
