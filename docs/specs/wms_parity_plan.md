# WMS Parity Plan — SAP EWM and Infor WMS as the benchmark

> **Status: plan only, no code written.** Drafted 2026-08-11 from two external benchmarks the user
> named — [SAP Extended Warehouse Management](https://help.sap.com/docs/SAP_EXTENDED_WAREHOUSE_MANAGEMENT/0e21d7e7a45c418cbbe15a723c08dfd2/f0123302094d473ba4f517670e8a93c9.html)
> and [Infor WMS 11.5.x (Supply Chain Execution)](https://docs.infor.com/wms/11.5.x/en-us/sceolh/default.html)
> — reconciled line-by-line against this repo's real WMS as of commit `a410c42`.
>
> Every "have" below points at real code in this tree; every "gap" is something that genuinely does
> not exist, verified by grep, not something merely undocumented.
>
> This document is the **source of detail** for **Stage 42** in
> [`docs/micro_checklist.md`](../micro_checklist.md) — the same relationship
> [`parity_master_plan.md`](parity_master_plan.md) has to Stages 35–39 and
> [`oms_master_blueprint_reference.md`](oms_master_blueprint_reference.md) has to Stage 26.12.
>
> It **supersedes nothing**. [`wms_master_blueprint_reference.md`](wms_master_blueprint_reference.md)
> stays valid — it annotates the *closed* Stage 26.5 items against a generic blueprint. This file is
> the *next level* against two named commercial products, and it is new scope.

---

## Sourcing caveat (read this before trusting a SAP row)

The two benchmarks were not equally retrievable and the plan should be read with that in mind.

- **Infor WMS — fully retrieved.** The 11.5.x online help TOC was fetched and parsed in full: 28
  top-level sections from Workstation/RF UI through Billing, Labor Management, Multi-Facility and
  System Administration. Every Infor citation below (`Infor §n`) points at a real section of that
  TOC.
- **SAP EWM — not retrievable programmatically.** `help.sap.com` serves a ~1 KB JavaScript shell to
  any non-browser client, and its content API (`help.sap.com/http.svc/deliverable`,
  `.../pagecontent`) answers `Access denied` / `Internal Server Error` to unauthenticated callers.
  Four fetch strategies and the 9.5 Master Guide PDF were all tried. The EWM scope below is
  therefore reconstructed from **(a)** EWM application-help node titles that surfaced with content
  in indexed search results on `help.sap.com` (Warehouse Order Creation, Storage Control,
  Cross-Docking, Opportunistic Cross-Docking, Labor Demand Planning, Slotting, Wave Management,
  Physical Inventory, Yard Management, Warehouse Worker roles), and **(b)** EWM's established
  component structure.

  **What that means practically:** the SAP column is directionally right and every EWM concept named
  below is a real EWM concept — but it is *not* a verbatim TOC read the way the Infor column is.
  Before anyone builds a Stage 42 item whose *only* justification is a SAP row, open the live page
  in a browser and confirm. Where Infor and SAP agree (which is most of the high-priority gaps), the
  Infor citation alone is enough to act on.

---

## 0. Executive summary

**The honest headline: this repo's WMS is a good mid-market WMS with a serious hole in its middle.**

Stage 20 Track B.2 and Stage 26.5 (16 items, all closed 2026-07-25/27) built a genuinely broad
feature surface — putaway, cross-dock, LPN, wave picking, cartonization, ABC cycle counting, blind
recounts, slotting, labour productivity, 3PL billing, a robotics endpoint, even a voice-picking
screen. Measured against the *feature checklists* of SAP EWM and Infor WMS, coverage is respectable.

But two things are missing that both benchmarks treat as the foundation everything else sits on:

1. **There is no lot/batch or serial traceability, and no unit-of-measure conversion.** Verified by
   grep: no `batch_no`, no `serial` on stock, no `expiry`/`shelf_life`, no `uom_conversion` anywhere
   in `engines/` or `db/`. The only serial number in the tree is `Asset.serial_number`
   (`db/migration.sql:781`). Infor spends three whole sections on this (§5 Lottable Validation /
   Shelf Life, §13 Serial Number Management, §26 Lot Attribute Masking); SAP EWM makes batch and
   serial-number profiles warehouse-level configuration. **Without it, this WMS cannot be sold into
   food, pharma, cosmetics, electronics, or any regulated category** — and FEFO, expiry blocking,
   recall traceability and lottable pick constraints are all unbuildable.
2. **There is no generic Warehouse Task object.** Both products are architecturally *task systems*:
   SAP EWM's Warehouse Task + Warehouse Order (created by Warehouse Order Creation Rules, dispatched
   by queue/resource); Infor's Task Manager + Task Dispatch Strategy (§16, §18). This repo instead
   has a set of parallel, unrelated actions — `PutawayToBin`, `ExecuteBinReplenishment`,
   `ScanPickItem`, `PostCycleCountAdjustment` — each with its own screen and no shared queue,
   priority, assignment, ageing or dispatch. That is why labour productivity could only be
   instrumented on three of them (26.5.13's own documented gap) and why there is no warehouse
   cockpit.

Everything in Phase 42.1 and 42.2 below exists to close those two holes. **Phases 42.3–42.6 are not
worth starting first**, because most of their items either need a batch/serial-aware stock record or
need a task object to hang off.

| | Benchmark | Honest target for this repo |
|---|---|---|
| **Infor WMS 11.5** | Mid-market/3PL WMS + full labour management + full billing engine | **This is the benchmark to actually aim at.** Right shape, right size, right customer. Roughly 70% of its TOC is achievable here; the labour-standards and billing-engine depth are the expensive tails. |
| **SAP EWM** | Tier-1 WMS incl. automated-equipment control, decades of process depth | **Feature-list parity is not a coherent goal and is not being attempted** — the same call `parity_master_plan.md` §0 made about S/4HANA. SAP is read here as a *concept checklist*: task/order architecture, storage control, exception codes, VAS, and the process-step vocabulary. MFS/PLC control is explicitly out (§10). |

**Sizing: 6 phases, 58 items, ≈34–44 build sessions (~2–2.5 months at this repo's demonstrated
cadence).** Sequenced foundation-first.

---

## 1. What this repo actually has today (grounded)

Measured, not estimated. 12 WMS engine files (~3,300 lines), 3 handler files, 33 WMS routes
(`internal/server/routes.go:244-276`), 9 dedicated UI screens.

| Capability | Where it lives | Notes |
|---|---|---|
| Bin master + bin_stock | `db/migrations_stage20b_wms_maturity.sql`, `engines/wms.go` | `Bin`: `bin_code`, `location`, `zone`, `aisle`, `rack`, later `bin_type` (PickFace/Reserve) and `owner_id`. `bin_stock` is a *breakdown* of `inventory_availability`, never a second source of truth. |
| Bin conditions | `TransitionBinStockCondition` (`engines/wms.go`) | Good / Damaged / QC-Hold / RTV, any-pair transition. |
| Putaway | `PutawayToBin` (`engines/wms.go`), `renderPutawayView` ([app.js:6639](../../public/app.js#L6639)) | Manual bin entry — no directed putaway. |
| ASN + GRN with QC split | `engines/wms_receiving.go` (`PostGRNReceiptWithQC`), `renderASNView` ([app.js:9450](../../public/app.js#L9450)), `renderGRNWorkbenchView` ([app.js:9054](../../public/app.js#L9054)) | Accepted → `available`, rejected → `qc_hold`, damaged → `damaged`. |
| Cross-dock (opportunistic) | `CheckCrossDockOpportunity`, `CrossDockPutaway` (`engines/wms_putaway_ext.go`) | Stages into a synthetic `XDOCK-<location>` bin. |
| LPN / carton / pallet | `AssignToLPN`, `GetLPNContents` (`engines/wms_putaway_ext.go`), `renderLPNView` ([app.js:6868](../../public/app.js#L6868)) | `bin_stock_lpn` as a further breakdown. |
| Bin-to-bin replenishment | `GetBinReplenishmentSuggestions`, `ExecuteBinReplenishment` (`engines/wms_putaway_ext.go`) | Min/max via `BinReplenishmentRule`. |
| Wave / batch picking | `AssignTasksToWave`, `GenerateWavePickList` (`engines/wms_picking.go`), `renderWavePickingView` ([app.js:7057](../../public/app.js#L7057)) | FIFO by `bin_stock.updated_at`, S-shape sort zone→aisle→rack. Manual task assignment. |
| Pick/pack scan + short pick | `ScanPickItem`, `ScanPackItem`, `ShortPickLine`, `CompletePackTask` (`engines/fulfillment_pickpack.go`) | Short pick requires a `ReasonCode`; blocks pack completion. |
| Cartonization | `SuggestCartonization` (`engines/wms_pack_count.go`) | First-fit-decreasing on **qty capacity only** — no dimensions, no weight. |
| Cycle count | `ReconcileCycleCount`, `PostCycleCountAdjustment` (`engines/wms.go`); `GetABCCycleCountPlan`, `RequestRecount`, `SubmitRecountValue`, `SetCycleCountVarianceReason` (`engines/wms_pack_count.go`); `renderCycleCountView` ([app.js:7304](../../public/app.js#L7304)) | Maker-checker gated; blind recount; mandatory variance reason before posting. |
| Slotting | `GetSlottingSuggestions` (`engines/wms_slotting.go`) | PickFace↔Reserve suggestion driven by 26.5.9's velocity tiers. Read-only. |
| Labour productivity | `GetLaborProductivity` (`engines/wms_productivity.go`), `TaskCompletionLog` | Tasks/hour per user per type, from 3 instrumented actions only. |
| 3PL storage billing | `GetStorageBillingReport` (`engines/wms_3pl_billing.go`) | Storage (snapshot × rate × days) + handling. Read-only report, no invoice. |
| Robotics/scale inbound | `engines/wms_robotics.go`, `POST /api/v1/wms/robotics/event` | API-key auth; `putaway`/`pick`/`weight_confirm`. |
| Mobile / voice picking | `renderMobilePickingView` ([app.js:7122](../../public/app.js#L7122)) | Reuses the wave pick-list endpoint; `SpeechSynthesis`/`SpeechRecognition`, feature-detected. |
| Downstream chain | `engines/returns.go`, `db/migrations_stage26_12_4_shipment_manifest.sql` | Returns/RMA and courier `Manifest` exist (Stage 26.12), but at the *courier* level, not the warehouse loading level. |

Plus the cross-cutting spine every Stage 42 item should attach to rather than reinvent: the DocType
meta-engine, the maker-checker approval engine, `RegisterReport`/`ReportDefinition`, the
`writeAPIError` envelope + error catalog, `moduleGate("wms", ...)`, and `ValidateDocument` as the
validation choke point.

---

## 2. The gap table

Grouped by the phase that closes it. **A** = both benchmarks require it, **I** = Infor only,
**S** = SAP only (treat per the sourcing caveat).

### Blocking foundations

| # | Gap | Src | Why it blocks |
|---|---|---|---|
| G1 | **No lot/batch tracking.** No `Batch` doctype, no batch on receipt/putaway/pick/ship. (`Batch` appears once, at `db/migrations_stage14a_modules.sql:86`, as a module mapping for a doctype that was never created.) | A — Infor §5/§16/§26; SAP batch management | FEFO, recall, expiry blocking, lottable pick constraints, regulated categories |
| G2 | **No serial-number tracking.** Infor devotes §13 entirely to it (receipt, pick, inquiry, adjustment, discrepancy resolution). | A | Electronics/high-value; warranty; unit-level trace |
| G3 | **No shelf life / expiry / date codes.** | A — Infor §5, §26 "Date Code Days" | FEFO allocation, expiry quarantine, "clean location" logic |
| G4 | **No UOM conversion.** No `conversion_factor` anywhere. | A — Infor §15 | Eaches/case/pallet picking, real cartonization, catch weight, billing by unit |
| G5 | **No generic Warehouse Task object or dispatch queue.** | A — Infor §16 Task Dispatch Strategy, §18 Task Manager; SAP WT/WO + WOCR | Task ageing, priority, assignment, warehouse cockpit, complete labour capture |
| G6 | **`Bin.zone` is free text, not a master.** No storage-type/zone rules (capacity, temperature, hazmat, putaway/removal strategy). | A — Infor §15 Zones/Locations; SAP storage type/section/bin type | Directed putaway, zone-based allocation, capacity checks |

### Process depth

| # | Gap | Src | Note |
|---|---|---|---|
| G7 | No directed putaway / configurable **putaway strategy** — putaway is a manual bin entry | A — Infor §16 | |
| G8 | **Allocation strategy is hard-coded** FIFO-by-`updated_at` inside `GenerateWavePickList` | A — Infor §16 Allocation Strategy | Should be configurable master data |
| G9 | No **configurable validation rules** at WMS transitions (receipt, post-allocation, post-pick, pre-ship, packing) | I — Infor §17 (an entire family) | `ValidateDocument` is doctype-level, not transition-level |
| G10 | No **wave templates / auto-wave creation / wave release lifecycle** — waves are manually assembled | A — Infor §3; SAP Wave Management | |
| G11 | No **sortation / put-wall / sorting-station** model | I — Infor §8 | |
| G12 | No **pack station, pack templates, packing validation templates, blind packing** | I — Infor §10, §17 | Cartonization is a suggestion only |
| G13 | No **deconsolidation** step | S | |
| G14 | No **loading step, Bill of Lading, pallet exchange** | A — Infor §10-11 | `Manifest` is courier-level |
| G15 | No **dock appointment scheduling** (no dock-door master, no calendar) | A — Infor §12; SAP DAS | Zero coverage — grep found nothing |
| G16 | No **yard / trailer management** (check-in→check-out) | A — Infor §12; SAP Yard Mgmt | |
| G17 | No **VAS / kitting / production staging** as warehouse work | A — Infor §25; SAP VAS orders | Manufacturing BOM exists but is not warehouse-side work |
| G18 | No **physical inventory (full/annual count)** — cycle count only | A — Infor §6 | |
| G19 | No **hold codes / hold-release workflow** as a first-class object | A — Infor §3/§5/§6 | Bin `condition` is the nearest thing, but no code master, reason or approval |
| G20 | Replenishment is **min/max only** — no demand-driven, wave-triggered or dynamic-pick-face replenishment | A — Infor §7 (4 types) | |
| G21 | **Cross-dock is opportunistic only** — no planned cross-dock, flow-through or transship | A — Infor §9; SAP 4 CD types | |
| G22 | No **catch weight / dimensional capture** at receipt | I — Infor §5 | |
| G23 | No **exception-code catalogue** tied to process steps with follow-on actions | S | Only short-pick + variance reason codes exist |
| G24 | No **resource / resource group / queue / RF menu** model | S | One mobile screen, no resource model |
| G25 | No **warehouse monitor / cockpit** — a single operational console | A — SAP Warehouse Management Monitor; Infor charts | Nine separate screens, no roll-up |
| G26 | **Slotting has no unslotting** and no capacity/dimension-driven re-slot run | I — Infor §26 | |
| G27 | No **cycle classes** as configurable count-frequency master data | I — Infor §15 | ABC is computed, not configurable |
| G28 | No **multi-owner stock segregation** — `owner_id` is on `Bin`, not on stock | I — Infor §14 Owners | Blocks true 3PL |
| G29 | No **facility hierarchy** (Company/Division/DC/Warehouse) or **facility copy** | I — Infor §23 | `Location` is flat: `code`/`name`/`type`/`legal_entity`/`status` |
| G30 | **Labour management is one metric.** Missing standards, operations, elements, allowances, travel sections, shifts, schedules, planning, and the 6 report families | A — Infor §18-20; SAP LM + Labor Demand Planning | |
| G31 | **3PL billing is one report.** Missing charge codes, rate groups, accessorials, event monitor, invoice batching/generation, contracts, taxes, minimums, markups | I — Infor §21-22 | |
| G32 | No **compliance fields**: COOL, bio-terrorism, GTIN, hazmat | I — Infor §24; SAP dangerous goods | |
| G33 | **Labels print text, not scannable symbology** (`engines/stickers.go`) | A | Already flagged in `wms_master_blueprint_reference.md` §3 and still true. Blocks any real scanner flow. |

---

## 3. Phase 42.1 — Traceability foundation (11 items, ≈8–10 sessions)

**Closes G1–G4, G33. Nothing else in Stage 42 should start before this.** Every item is additive
schema on existing tables, per this repo's standing rule.

- **42.1.1** `Item` tracking flags — `tracking_mode` (None / Batch / Serial / Batch+Serial),
  `shelf_life_days`, `min_shelf_life_on_receipt_days`, `min_shelf_life_on_pick_days`. One field group
  on the existing `Item` doctype; drives every gate below.
- **42.1.2** `Batch` master doctype — `batch_no`, `item`, `mfg_date`, `expiry_date`, `supplier_batch`,
  `status`, `attributes` (JSON, for Infor's "lottable" 1–12 concept). Global-unique document-id
  gotcha applies (see `erp-project-understanding` memory).
- **42.1.3** Batch on the stock record — additive `batch_no` on `bin_stock` and on the ledger write.
  `bin_stock` stays a breakdown of `inventory_availability`; batch is a *further* breakdown, exactly
  the precedent `bin_stock_lpn` set in 26.5.4.
- **42.1.4** Batch capture at receipt — GRN Workbench + ASN line gain batch/mfg/expiry, gated by
  42.1.1's `tracking_mode`. Reuses `PostGRNReceiptWithQC`'s existing accepted/rejected/damaged split.
- **42.1.5** **FEFO allocation** — `GenerateWavePickList`'s FIFO-by-`updated_at` becomes strategy-driven:
  FEFO where the item is batch-tracked with an expiry, FIFO otherwise. This is the single highest-value
  line of code in the phase.
- **42.1.6** Expiry gates — block picking/shipping stock inside `min_shelf_life_on_pick_days`; a
  near-expiry report and an auto-quarantine transition into the existing `qc_hold` bucket.
- **42.1.7** Outbound lottable validation — per-customer/per-order batch-attribute constraints
  enforced at allocation (Infor §16 "Outbound Lottable Validation").
- **42.1.8** `SerialNumber` register — `serial_no`, `item`, `batch_no`, `status`
  (InStock/Allocated/Shipped/Returned/Scrapped), `current_bin`, `owner`. Capture at receipt,
  consume at pick, verify at pack.
- **42.1.9** Serial inquiry + full movement history — one screen answering "where is this unit now and
  everywhere it has been". Registered as a report, not a bespoke endpoint.
- **42.1.10** UOM conversion — `UOM` master + `UOMConversion` (`item`, `from_uom`, `to_uom`, `factor`).
  Wire into cartonization, pick UoM, and 3PL billing units. Deliberately *not* wired into pricing in
  this phase.
- **42.1.11** Scannable barcode symbology — replace `engines/stickers.go`'s text rendering with real
  Code 128 (GS1-128/SSCC where a partner requires it). Per the first principle, hand-rolled Code 128
  encoding (~120 lines, well-specified) rather than a new dependency. **Prerequisite for every scanner
  flow in 42.3/42.4.**

**Recall traceability** (forward: batch → every order shipped; backward: order → batch → supplier)
falls out of 42.1.3 + 42.1.8 as two report definitions, and should be built in this phase as proof
the model is right.

---

## 4. Phase 42.2 — The warehouse task spine (10 items, ≈7–9 sessions)

**Closes G5, G6, G7, G8, G23, G24, G25.** This is the architectural phase: it converts a set of
parallel actions into a task system, which is what both benchmarks actually are.

- **42.2.1** `WarehouseTask` doctype — `task_type` (Putaway/Pick/Replenish/Count/Move/VAS/Load/Unload),
  `status`, `priority`, `location`, `from_bin`/`to_bin`, `item`, `batch_no`, `qty`, `uom`, `assigned_to`,
  `queue`, `wave_id`, `source_doc`, timestamps. **One object every floor action emits into.**
- **42.2.2** Retrofit the five existing actions to emit/close a `WarehouseTask` —
  `PutawayToBin`, `ExecuteBinReplenishment`, `ScanPickItem`, `CrossDockPutaway`,
  `PostCycleCountAdjustment`. Additive: each keeps working exactly as today, and also writes a task.
  This closes 26.5.13's documented "picking isn't instrumented" gap as a side effect, without the
  signature change that pass rejected.
- **42.2.3** Task queue + dispatch — `GetNextTask(user, location)` honouring priority, queue
  eligibility, zone and task type. The RF/mobile equivalent of a work list.
- **42.2.4** `TaskDispatchStrategy` master — ordering rules (priority → ageing → proximity → type)
  as configurable data rather than code.
- **42.2.5** `Zone` master promoted from free text — `zone_code`, `location`, `zone_type`,
  `temperature_class`, `hazmat_allowed`, `putaway_sequence`, `pick_sequence`. `Bin.zone` becomes a
  `Link`. Migration must keep existing free-text values working (auto-create a `Zone` row per
  distinct existing value).
- **42.2.6** Bin capacity + attributes — `max_qty`, `max_weight`, `max_volume`, `bin_status`
  (Available/Blocked/Full/Counting). Enforce on putaway.
- **42.2.7** `PutawayStrategy` master + **directed putaway** — suggest the bin instead of asking for
  it. Rule inputs: item velocity tier (reuse `GetABCCycleCountPlan`), zone sequence, bin capacity,
  hazmat/temperature compatibility, existing-batch consolidation. Falls back to today's manual entry.
- **42.2.8** `AllocationStrategy` master — lifts the hard-coded FIFO out of `GenerateWavePickList`.
  Options: FIFO / FEFO / LIFO / nearest-bin / fewest-picks / clean-location-first.
- **42.2.9** Exception-code catalogue — extend the existing `ReasonCode` master with a WMS exception
  category tied to process step, with a follow-on action (reallocate / hold / create count task /
  notify). Reuses the choke points 26.5.10 already established.
- **42.2.10** **Warehouse cockpit** — one screen: open tasks by type/age, exception queue, wave
  status, inbound due today, dock/appointment strip (populated in 42.3), pick/pack throughput, bin
  utilisation. This is the screen a warehouse manager actually lives in, and its absence is the most
  visible difference from both products.

**Deliberate non-goal in this phase:** SAP's Warehouse Order (the grouping of tasks into an
operator's work parcel) is *not* being modelled separately. `WarehouseTask.wave_id` +
`assigned_to` covers the practical need at this repo's scale; a separate WO object can be added later
if a real pilot demands it.

---

## 5. Phase 42.3 — Inbound depth (10 items, ≈6–8 sessions)

**Closes G15, G16, G19, G21, G22, G32, plus receipt-side G9.**

- **42.3.1** `DockDoor` master — per location, with door type (Inbound/Outbound/Both), equipment and
  service windows.
- **42.3.2** `Appointment` doctype + scheduling — carrier, ASN/PO reference, dock door, slot,
  status. Validate against door capacity and service windows.
- **42.3.3** Appointment calendar screen — day/week grid per door. Reuses the existing table-panel
  and dialog vocabulary; no calendar library.
- **42.3.4** Yard check-in / check-out — `Trailer` master + a yard-status log (Arrived → At Door →
  Unloading → Departed). **Infor's 3D yard view is explicitly out of scope** (§10).
- **42.3.5** `HoldCode` master + hold/release workflow — apply a hold at receipt, batch, bin or
  inventory level; release is approval-gated through the existing maker-checker engine. Held stock is
  excluded from allocation at the `computeATS` choke point, not at each call site.
- **42.3.6** Configurable receipt validation rules (Infor §17) — per-supplier/per-item required
  fields, tolerance checks, batch/expiry mandatory, over-receipt tolerance. Attaches to
  `ValidateDocument`.
- **42.3.7** Catch weight + dimensional capture at receipt — actual weight/dims per LPN or line,
  feeding cartonization (42.4.4) and billing (42.6.9).
- **42.3.8** Planned cross-dock + flow-through — extends 26.5.3's opportunistic engine with a
  *planned* variant driven by a flagged inbound line, and a transship/flow-through path that never
  touches stock.
- **42.3.9** Compliance fields — country of origin (COOL), GTIN on `Item`, hazmat class on `Item` +
  `Zone`, with a putaway compatibility check. Bio-terrorism-act record retention is a report, not a
  new store.
- **42.3.10** RF receiving screen — scan-driven receipt against ASN using 42.1.11's real barcodes:
  scan ASN → scan item → scan/enter batch+expiry → scan LPN → scan bin. Mirrors the mobile-picking
  screen's pattern; no new framework.

---

## 6. Phase 42.4 — Outbound depth (11 items, ≈7–9 sessions)

**Closes G10–G14, G17, plus outbound G9.**

- **42.4.1** `WaveTemplate` master + rule-based wave creation — criteria (channel, carrier, cut-off,
  zone, order type, service level) and a scheduled auto-wave run. Replaces manual `AssignTasksToWave`
  as the default path; manual stays available.
- **42.4.2** Wave lifecycle — Planned → Released → In Progress → Complete → Closed, with release as
  the point tasks become dispatchable (42.2.3). Wave monitor panel on the cockpit.
- **42.4.3** Sortation / put-wall — `SortStation` + `SortSlot` masters; batch-pick to a slot per order,
  then confirm. Closes the "batch pick with no sortation" hole in 26.5.6.
- **42.4.4** Cartonization v2 — dimensional and weight-aware, using 42.1.10's UoM and 42.3.7's
  captured dims. Today's `SuggestCartonization` is qty-capacity-only first-fit-decreasing.
- **42.4.5** `PackStation` + `PackTemplate` masters — per-station configuration and per-item/customer
  packing rules (carton type, dunnage, documents, labels).
- **42.4.6** Packing validation templates + blind packing — configurable pre-pack checks; blind mode
  hides expected qty so the packer counts independently.
- **42.4.7** Deconsolidation step — split a received/inbound HU across outbound destinations before
  packing.
- **42.4.8** Loading — `LoadingTask` against a dock door + trailer, scan-verified carton-to-trailer,
  with a load-complete gate. Feeds the existing `Manifest`.
- **42.4.9** Bill of Lading — generated from a completed load; PDF via the existing print path, not a
  new renderer. Pallet exchange counters on the load.
- **42.4.10** Pre-ship validation rules (Infor §17) — final gate before dispatch: all cartons packed,
  serials verified, documents present, hold-free.
- **42.4.11** VAS / kitting / production staging — `VASOrder` as a `WarehouseTask` type
  (label / re-pack / assemble / kit), consuming components and producing a finished LPN. Reuses the
  existing manufacturing BOM as the component source rather than defining a second BOM.

---

## 7. Phase 42.5 — Inventory control depth (8 items, ≈5–6 sessions)

**Closes G18, G20, G26, G27, G28, G29.**

- **42.5.1** Physical inventory (full/annual count) — a `PhysicalInventory` document that freezes a
  location or zone, generates count tasks for every bin (not a sample), and reconciles through the
  *existing* maker-checker path rather than a parallel one.
- **42.5.2** `CycleClass` master — configurable count frequency per class, replacing 26.5.9's
  hard-coded 20/30/50 Pareto split as the *default* rather than the only option.
- **42.5.3** Replenishment breadth — add demand-driven (open allocation vs pick-face qty),
  wave-triggered (on wave release), and dynamic-pick-face replenishment alongside today's min/max.
- **42.5.4** Slotting v2 — unslotting, capacity/dimension-driven re-slot runs, and a "clean location"
  preference (consolidate partial bins). Extends `GetSlottingSuggestions`, still read-only-by-default.
- **42.5.5** Multi-owner stock segregation — `owner_id` moves from `Bin` onto the stock record, so one
  bin can hold two owners' stock. **This is the real 3PL enabler**; 26.5.15's billing is currently
  approximating around its absence.
- **42.5.6** Facility hierarchy — `Location.parent` + `location_level`
  (Company/Division/DC/Warehouse), with roll-up inventory inquiry.
- **42.5.7** Facility copy — clone a location's WMS configuration (zones, bins, strategies, rules) to
  a new one. Cheap given the DocType engine; high value at onboarding.
- **42.5.8** Cross-facility inventory inquiry + inter-facility transfer visibility — extends the
  existing `TransferOrder` with in-transit stock as a real bucket.

---

## 8. Phase 42.6 — Labour and billing depth (8 items, ≈6–8 sessions)

**Closes G30, G31.** Lowest priority, highest per-item cost — and the phase most likely to be cut.
Both benchmarks treat these as separate licensed modules for a reason.

- **42.6.1** `LaborStandard` master — engineered time per operation, built from `Element` +
  `Allowance` + `TravelSection` records rather than a single flat number.
- **42.6.2** Operations/elements catalogue — the task-component breakdown standards are computed from.
- **42.6.3** Shift + schedule masters — `Shift`, `WeeklySchedule`, `UserWorkSchedule`.
- **42.6.4** Labour planning — forecast headcount per department from open task volume × standards.
- **42.6.5** The labour report family — enterprise productivity, facility performance, task
  productivity, user performance, standards audit, labour cost. Six `RegisterReport` definitions on
  the framework that already exists; this item is cheap *because* 42.2.2 instrumented every task.
- **42.6.6** `ChargeCode` / `RateGroup` / `ChargeGroup` masters + markups, discounts, minimums.
- **42.6.7** Event monitor — transaction types that trigger a billable charge, replacing today's
  report-time computation with real captured charge records.
- **42.6.8** Invoice generation — batch, generate and post a real invoice document from captured
  charges, reusing the existing sales-invoice document rather than a parallel one. Closes
  26.5.15's "read-only report, no invoice/GL document auto-created" carve-out.
- **42.6.9** Accessorial + storage billing v2 — real historical balance averaging (the documented
  approximation in 26.5.15 exists only because there is no historical `bin_stock` ledger; 42.2.2's
  task log plus a daily snapshot fixes it), plus per-owner handling split, contracts and tax levels.

---

## 9. Sizing and sequencing

| Phase | Items | Sessions | Gate |
|---|---|---|---|
| **42.1** Traceability foundation | 11 | 8–10 | none — start here |
| **42.2** Warehouse task spine | 10 | 7–9 | needs 42.1.3 (batch on stock) for task batch fields |
| **42.3** Inbound depth | 10 | 6–8 | needs 42.1.11 (barcodes), 42.2.1 (tasks) |
| **42.4** Outbound depth | 11 | 7–9 | needs 42.2 in full |
| **42.5** Inventory control depth | 8 | 5–6 | needs 42.2.5/42.2.6 (zone, capacity) |
| **42.6** Labour and billing depth | 8 | 6–8 | needs 42.2.2 (task instrumentation) |
| **Total** | **58** | **34–44** | ≈2–2.5 months at this repo's cadence |

**Recommended release trains:**

- **W1 — "Regulated-ready"** = 42.1 in full. The single largest commercial unlock: without it this
  WMS cannot be sold into food/pharma/cosmetics at all. Ship this even if nothing else in Stage 42
  gets built.
- **W2 — "Task-driven warehouse"** = 42.2 in full. Turns nine disconnected screens into one system
  with a cockpit. Biggest *perceived* quality jump for an operator.
- **W3 — "Dock to stock"** = 42.3 + 42.4.8–42.4.10. Closes the two ends nobody has touched: the yard
  and the truck.
- **W4 — "Fulfilment depth"** = 42.4.1–42.4.7, 42.4.11. Wave/sortation/pack-station maturity.
- **W5 — "Control"** = 42.5.
- **W6 — "3PL commercial"** = 42.6. **Only worth building against a real 3PL prospect** — same
  reasoning 26.5.11 originally applied to the P2 items.

**Interaction with Stages 35–39.** Stage 42 is largely disjoint from the Parity Master Plan: that
plan is OMS/PIM/ERP-console work, this is warehouse-floor work. Two real touchpoints:
35.1 (channel orders → `SalesOrder`) should land **before** 42.4.1's wave templates, so waves are
built from real sales orders rather than `POSCart`; and 35.7 (bundles/kits) overlaps 42.4.11 (VAS
kitting) — build 35.7's data model first and have 42.4.11 consume it, not duplicate it.

---

## 10. What we are explicitly NOT building

Named here so it is a decision on the record, not an omission rediscovered later.

- **SAP MFS (Material Flow System)** — direct PLC/conveyor/ASRS telegram control. Requires
  real-time messaging, a hardware lab and a certified integration per vendor. 26.5.16's generic
  robotics event endpoint is the correct ceiling for this repo. Same call `parity_master_plan.md` §0
  made about S/4HANA breadth.
- **Infor's 3D yard view** — a visualisation toy relative to its cost; 42.3.4's yard status list
  delivers the operational value.
- **SAP's RF/ITSmobile framework, presentation-device and resource-group model** — SAP-platform
  constructs, not warehouse requirements. 42.2.3's task queue plus the existing mobile screen covers
  the actual need.
- **Full engineered time-study tooling** (motion capture, MOST/MTM tables) behind 42.6.1 — the
  standards *master* is in scope; the industrial-engineering toolchain that populates it is not.
- **EDI/trading-partner mapping depth** (X12 940/943/944/945/947, EDIFACT) — the existing connector
  and outbox patterns are the right place if a customer needs it; not speculative scope.
- **Separate Warehouse Order object** — see 42.2's non-goal note.
- **Voice-picking vendor integration** (Vocollect/Honeywell) — 26.5.14's browser-native
  `SpeechSynthesis`/`SpeechRecognition` screen stands; a vendor SDK is a new dependency and needs a
  contracted customer.

---

## 11. Open decisions for the user

These change what gets built and cannot be resolved from the code.

1. **Is any regulated category (food / pharma / cosmetics / electronics) a real target?** If yes,
   42.1 is not optional and should start immediately. If no, 42.1 could in principle be deferred —
   but the recommendation is still to build it, because it is also the prerequisite for FEFO,
   recall, and serial warranty, and retrofitting traceability into a live stock ledger later is far
   more expensive than adding it now.
2. **Is 3PL / multi-owner a real target, or is this a single-owner warehouse product?** This decides
   whether 42.5.5 and the whole of 42.6.6–42.6.9 are in or out — roughly 6 items and 5 sessions.
3. **Batch *and* serial, or batch only?** Serial (42.1.8–42.1.9) is ~2 sessions and only pays off for
   high-value/serialised goods.
4. **Physical scanner hardware in play?** 42.1.11 (real symbology) and 42.3.10 (RF receiving) assume
   a real scanner exists at some point. If everything stays keyboard-driven, both drop in priority.
5. **Wave auto-creation on a schedule** implies a scheduler running per tenant. The repo has
   `scheduled_reports.go` as the precedent — confirm reusing it rather than adding a second timer.
6. **Does `Zone` becoming a master (42.2.5) risk existing tenant data?** The migration auto-creates a
   `Zone` per distinct existing free-text value. Confirm that is acceptable versus a manual mapping
   pass on the live droplet.

---

## 12. Verification notes for whoever builds this

- Every phase ends with the same bar the closed Stage 26.5 items met: `go build` / `go vet` /
  `go test ./... -p 1` clean, a real-Postgres engine test, **and** a live browser pass through every
  new/changed screen with zero console errors. Note the known shared-DB GL-totals false-failure
  (see the `erp-test-suite-race-gotcha` memory) — it is fixture debris, not a regression from this work.
- `wms_master_blueprint_reference.md` §8 lists 11 critical UAT scenarios that were written for
  exactly this kind of work. Batch/serial adds four more worth lifting: pick an expired batch →
  blocked; ship a batch below `min_shelf_life_on_pick_days` → blocked; duplicate serial receipt →
  rejected; recall query returns every order containing a batch.
- Per `CLAUDE.md`, run `graphify update .` and `pwsh docs/brain/update-brain.ps1` after each phase —
  42.1 and 42.2 both add engine files and will otherwise leave the brain map stale.
