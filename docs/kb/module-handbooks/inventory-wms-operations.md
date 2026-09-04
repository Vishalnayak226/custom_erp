---
title: Inventory & Warehouse Operations
section: Module Handbooks
order: 20
summary: Receive stock against a PO, put it away, pick and pack it back out, count it, adjust it, and move it between locations — the floor-ops backbone every other WMS feature sits on top of.
audience: warehouse operator, warehouse manager, admin
last_verified: 2026-09-03
screens: [grn, rf-receiving, putaway, wave-picking, cycle-count, transfers, warehouse-cockpit, doctype-table, reports]
---

# Inventory & Warehouse Operations

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
7. Click **Add Line**, repeat, then **Post Receipt**.

**What Post Receipt actually does** (`PostGRNReceiptWithQC`,
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
the desktop Workbench uses — it's a different data-entry surface for the
same document type, not a parallel receiving mechanism.

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
**bin-level pick list** that tells a picker where to walk, and a
**coarse status workflow** on the `FulfillmentTask` itself that actually
drives the order forward. They are deliberately not the same mechanism.

### Bin-grouped pick list

From **Fulfillment**, a Pending or Picking task's **View Pick List** button
opens a read-only modal (`GET /api/v1/wms/pick-list?task_id=...`,
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

1. **Tag tasks into a wave** — enter a Wave ID and a comma-separated list
   of Task IDs, click **Tag Tasks**.
2. **Generate the wave pick list** — enter the same Wave ID, click
   **Generate Pick List**. This returns a consolidated, walk-route-sorted
   pick list across every tagged task, plus a **Per-Order Allocation**
   table (how much of each SKU each task actually got, and any shortfall).

For a batch-tracked item, allocation here is **FEFO**, and the pick list
shows the batch/expiry to take — see
[Batch, Serial & Expiry Traceability](traceability-batch-serial.md).

**Mobile Picking** (**WMS → Mobile Picking**) is a phone-friendly,
one-item-at-a-time reader for the exact same wave pick list — enter a Wave
ID, and it steps through the list as big-text cards, with optional voice
readout (`speechSynthesis`) and voice "next"/"confirm" control
(`SpeechRecognition`, where the browser supports it).

> [!NOTE]
> **Confirm & Next only advances the card locally — it does not record a
> pick against the server.** Both **Previous**/**Next** on the desktop wave
> list and **Confirm & Next** on Mobile Picking are navigation only; nothing
> here calls back to the server to mark a line picked. The pick list is a
> *reference* for where to walk and what to take, not the record of what
> was actually taken.

### What actually moves the order forward

The order itself advances on the **Fulfillment** screen, by a coarse status
transition (`POST /api/v1/fulfillment/task/transition`):
**Pending → Picking → Packed → Dispatched** (or **Rejected** from Pending or
Picking). "Start Picking", "Mark Packed" and "Dispatch" are plain status
changes — they don't take a quantity or a bin, and don't call the pick list
or any scan endpoint underneath.

The only genuinely quantity-carrying, wired pack action anywhere in WMS is
packing a **Transfer Order** into boxes — see Transfers below.

> [!WARNING]
> **Task-level pick/pack scanning is real and tested, but has no screen.**
> `POST /api/v1/wms/pick-scan` (`ScanPickItem`) and
> `POST /api/v1/wms/pack-scan` (`ScanPackItem`) exist specifically to record
> a scanned SKU against a `FulfillmentTask`'s picked/packed quantity,
> `POST /api/v1/wms/short-pick` (`ShortPickLine`) records a short pick with a
> reason code, and `POST /api/v1/wms/pack-complete`
> (`handlePackCompleteValidated`, a pre-pack checklist wrapped around
> `CompletePackTask`) is meant to be the real close-out of a pack task. All
> four are role-open, module-gated routes with tested engine functions behind
> them — confirmed by grep, none of `pick-scan`, `pack-scan`, `short-pick`, or
> `pack-complete` appears anywhere in `public/app.js`. What the app actually
> uses instead is the coarse Pending→Picking→Packed→Dispatched transition
> above plus the read-only pick list. If you need genuine scan-by-scan
> pick/pack tracking against a `FulfillmentTask` today, it has to be driven by
> a direct API call, not by clicking through the app.

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

> [!WARNING]
> **Physical Inventory (a full/annual stock take) is a real, tested engine
> with zero UI.** `StartPhysicalInventory`, `SubmitPhysicalInventoryCount`,
> `ReconcilePhysicalInventory`, `ClosePhysicalInventory`, and
> `CancelPhysicalInventory` (`engines/wms_physical_inventory.go`) are a
> governed way to count *every* SKU at a location (or a zone within it) —
> freezing the bins involved (`bin_status = 'Counting'`, which also blocks
> putaway and picking allocation against them for the duration) and creating
> one `CycleCountLine` per SKU under a `PhysicalInventory` header, reusing
> the exact same reconcile/post/approve mechanics described above rather than
> a second counting system. All five routes exist
> (`POST /api/v1/wms/physical-inventory/{start,submit-count,reconcile,close,
> cancel}`), all are module-gated the same as every other WMS route, and none
> of them — nor the string `PhysicalInventory` — appears anywhere in
> `public/app.js`. Today, running a full stock take (as opposed to an ongoing
> ABC-sampled cycle count) means calling these directly, not clicking through
> a screen.

## Stock Adjustments

There is no free-form "adjust this SKU's quantity" screen in this app —
that's deliberate, not an oversight. Every quantity-level adjustment goes
through the Cycle Count reconciliation/approval/reason-code path above
(`PostCycleCountAdjustment`), so a quantity correction always carries a
count session, a reviewer (for anything non-zero), and a root-cause reason
code. If you need to correct a quantity, count it — enter the real quantity
as a `CycleCountLine` against the current session and reconcile.

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

## Transfers (Stock Transfer)

[USER_GUIDE.md §7](../../guides/USER_GUIDE.md) covers the basic Draft →
Approved → Dispatch → Receive click-through. This section goes deeper into
what actually happens at each step and where it can go wrong.

**WMS → Stock Transfer** (`renderTransfersView`) drives a `TransferOrder`
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
5. **Receive** — prompts for the quantity actually received, per line,
   defaulting to the full dispatched quantity. Confirmed quantity leaves
   the source's `in_transit` (whatever left, left, however much of it
   arrives) and only what's confirmed received is added to the
   destination's `available`.

> [!WARNING]
> **A genuine short-receive on a Transfer Order cannot be completed through
> this screen.** `ReceiveTransferOrder` requires a shortage/damage
> **reason** on any line where received quantity is less than dispatched
> (`TRN-0259`) — but `receiveTransferOrder` in `public/app.js` only ever
> prompts for a quantity per line, sends `{sku, qty}` with no `reason`
> field, and has no follow-up prompt to supply one after a `TRN-0259`
> rejection. In practice: entering the full dispatched quantity works, and
> entering anything less always fails with no way to proceed from this
> dialog. This contradicts USER_GUIDE.md §7's own description ("entering
> the lower number records that shortage rather than hiding it") — as of
> this writing that only happens via a direct API call that includes a
> `reason` per short line, not by clicking through the Receive prompt.

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
required."** Enter one when prompted — the dialog asks for a reason
automatically whenever a line's received quantity is less than what was
dispatched (`TRN-0259`).

**"Source and destination location cannot be the same" (`STOCKT-0111`).**
Self-explanatory — pick two different locations for a transfer.

**Wave pick list is empty for a wave I just tagged tasks into.** Nothing in
that wave's tasks is currently stored in a bin — putaway hasn't happened
yet for that stock, or it's being picked from general (non-binned) stock
instead. That's a valid state, not an error.

## What is not here yet

**Task-level pick/pack scanning has no screen**, even though the server
functions behind it (`ScanPickItem`, `ScanPackItem`, `ShortPickLine`,
`CompletePackTask`) are real and tested — see the warning under Picking &
Packing above. What the app drives today is a coarse task-status transition
plus a read-only pick list, not scan-by-scan quantity tracking against a
`FulfillmentTask`.

**Full/annual Physical Inventory has no screen** — see the warning under
Cycle Counts. The ABC-sampled cycle count workflow is fully wired; counting
every SKU at a location under a governed, bin-freezing session is API-only
today.


**There is no free-form stock-adjustment screen**, and that's by design —
every quantity correction goes through Cycle Count's count/reconcile/
approve/reason-code path, never a direct "set this SKU's quantity to X."
