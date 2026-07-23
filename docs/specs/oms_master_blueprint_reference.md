# OMS Master Blueprint & BRD Pack — Reference Notes

**Source documents:**
- `Inhouse_OMS_Master_Blueprint.pdf` (v1.0, 5 Jul 2026, 30 sections) — a four-engine OMS design (Order Engine / Inventory Promise Engine / Allocation-Sourcing Engine / Orchestration Engine), consolidating OpenOMS/KubeRiva/OpenShip/Apache OFBiz/Saleor/UniCommerce/IBM Sterling/SAP/Dynamics/Fluent patterns. Read in full 2026-07-23.
- `Inhouse_OMS_Module_BRD_Pack.pdf` (v1.0, 5 Jul 2026, 17 module BRDs) — developer-ready requirements/process-logic/business-rules/validations/APIs/events/reports/acceptance-criteria per module. Read in full 2026-07-23.

**Why this file exists:** mirrors `docs/specs/wms_master_blueprint_reference.md`'s precedent from the same day. A standalone project (`Antigravity Projects/OMS` — a separate Go multi-tenant service plus a mock/localStorage-driven vanilla-JS frontend, its own `custom_oms` git remote) was started against these two blueprints before this repo's own order/fulfillment/marketplace work existed. That project's architecture conflicts with this repo's standing rules (no new frontend framework, no new third-party dependency, one lightweight server — see `CLAUDE.md`), so it has been retired rather than merged as code (see `project_ledger.md`'s retirement entry for the reasoning). This file is the durable knowledge kept from it: the two blueprints' design content, reconciled against what this repo already has, section by section. Everything below is reference/checklist material for whoever builds Stage 26.12 — it does not describe a new module to build in this file itself.

**Headline finding:** unlike WMS (which already had a `Stage 26.5` sprint mirroring its blueprint almost item-for-item before that retirement pass), this repo's Stage 26 roadmap had **no Order-Management phase at all** before this pass. That said, the gap is a maturity/completion gap, not a build-from-zero one: ERP already has real, working (if MVP-thin) pieces covering a meaningful slice of OMS scope — channel order ingestion with idempotency and SKU mapping, row-locked atomic reservation, a single-strategy sourcing engine, a single-stage fulfillment task, a bare logistics-booking record, a working return-anywhere flow, and a generic outbox/retry mechanism reusable as the OMS exception queue. The new `Stage 26.12 — OMS/Order Management Maturity Sprint` in `micro_checklist.md` is the first buildable backlog structure for this scope.

---

## 1. The four-engine model vs. this repo's actual engines

| Blueprint engine | Responsibility | This repo's equivalent | Status |
|---|---|---|---|
| Order Engine | Capture, validation, lifecycle, holds, cancellation | No dedicated engine — channel orders piggyback on the `POSCart` doctype (`ImportChannelOrder` creates one in `Reserved` status, `engines/sourcing.go:154`) | **Gap** — tracked 26.12.1 |
| Inventory Promise Engine | ATP, reservation, release, exposure, mismatch control | `engines/inventory.go` (`CreateReservation`, `GetAvailableToSell`, `PostInventoryLedger`) | **Partial** — reservation/concurrency real, ATP formula incomplete |
| Allocation/Sourcing Engine | Select fulfillment node by stock/SLA/cost/priority/workload | `FindBestFulfillmentNode` (`engines/sourcing.go:11`) | **Partial** — one strategy only |
| Orchestration Engine | Pick, pack, invoice, label, manifest, dispatch, return, RTO, refund | `FulfillmentTask`/`ProcessReturnAnywhere` (`engines/fulfillment.go`), `CreateLogisticsBooking` (`engines/marketplace.go`) | **Partial** — pick/pack/return exist in bare form; courier/label/manifest/RTO essentially absent |

## 2. Domain model vs. what exists

The blueprint's 7 business objects (Customer Order, Order Line, Fulfillment Order, Shipment, Invoice, Reservation, Return Order, Refund Request) mapped against live code:

- **Customer Order / Order Line** → `POSCart` — a single document with a JSONB items array, not separate order-header + order-line rows with independent status.
- **Reservation** → `inventory_reservation` table — real (`engines/inventory.go:233`), row-locked, atomic.
- **Fulfillment Order** → `FulfillmentTask` (`engines/fulfillment.go:88`) — one document per pick task, not a formal fulfillment-order-plus-lines pair.
- **Shipment** → `LogisticsBooking` (`engines/marketplace.go:17`) — a bare record (carrier, tracking number, charge), no line items, no AWB/label.
- **Invoice** → `SalesInvoice` doctype exists but has no per-line item data at all (confirmed by `engines/fulfillment.go`'s own `resolveOriginalSale` comment, lines 19-21) — it satisfies "an original bill exists," not a quantity cross-check.
- **Return Order** → `SalesReturn` doctype (used by `ProcessReturnAnywhere`) — no QC/disposition sub-record.
- **Refund Request** → not modeled as distinct from the immediate GL reversal inside `ProcessReturnAnywhere`.

(The standalone prototype's own `src/db/migrations.sql` independently confirms this is the right normalized shape — it defines `orders`/`order_lines`/`fulfillment_orders`/`fulfillment_order_lines`/`pick_tasks`/`pick_task_lines`/`pack_tasks`/`pack_task_lines`/`shipments`/`manifests`/`manifest_shipments`/`returns`/`return_lines`/`refunds`/`exception_cases`/`audit_logs` as separate tables — but the DDL itself was not ported, only the shape confirmed.)

## 3. Status architecture vs. what exists

The blueprint mandates multi-level status — Order, Order Line, Fulfillment, and Shipment tracked *separately*, because one customer order can split across fulfillment nodes, partially cancel, or partially return. It ships an explicit status-transition matrix (e.g. `Dispatched → Delivered` allowed, `Dispatched → Cancelled` not allowed, must route through RTO/return instead).

This repo today: `POSCart` carries one status field; `FulfillmentTask` carries one status field (`Pending`/`Rejected`/`Dispatched`); no transition-matrix enforcement exists at the order level (the generic doctype engine's `status` field has no state-machine gate built in). This is the single biggest structural gap for a genuine multi-node/split-shipment OMS — flagged as a design decision at 26.12.1, not guessed at here.

## 4. Inventory Promise Engine — ATP formula

Blueprint: `ATP = Physical − Reserved − Blocked − QC Hold − Damaged − Safety Stock − Channel Buffer` (7 terms), evaluated at location + barcode/design/combination/MRP level. Reservation must be atomic (no two orders reserve the same barcode).

This repo (`GetAvailableToSell`, `engines/inventory.go:255`): `ATS = Available − Reserved − Safety Stock` (3 terms) — no Blocked/QC-Hold/Damaged/Channel-Buffer buckets on `inventory_availability`. Concurrency, however, is already correct: `CreateReservation` takes a `FOR UPDATE` row lock before computing ATS and inserting the reservation (`engines/inventory.go:215-228`), and `PostInventoryLedger`'s decrement path locks the same way (`engines/inventory.go:145-168`) — the blueprint's atomicity requirement is already satisfied, just on a narrower bucket set. Tracked at 26.12.6.

The standalone prototype's own `state.js:getATP` (`records.physical − records.reserved − records.blocked − records.qcHold − records.damaged − safety − buffer`) already implements the *full* 7-term formula, just client-side and behind a soft JS-level lock (`_acquireLock()`), not a real database row lock — a materially weaker concurrency guarantee than this repo's own `FOR UPDATE` path. This confirms the 7-term shape is the right one to add (not a design guess), while the *locking* strategy should stay this repo's own, not the prototype's.

## 5. Allocation/Sourcing Engine — strategies

Blueprint strategies: Warehouse First, Nearest Node, Highest Availability, Lowest Workload, Oldest Stock First, Split Shipment, Manual Allocation — selected via a configured `allocation_rule` priority table, with an allocation exception queue when no plan is possible.

This repo (`FindBestFulfillmentNode`, `engines/sourcing.go:11`): one strategy — iterate every location, sum ATS per requested SKU, pick the location with the highest total ATS across all items; fall back to a hardcoded `"HO"` location if nothing qualifies (logged via `LogSystemError`, code `OMNI-0247` — deliberately never rejects an order for lack of stock, the same "don't reverse an already-deliberate workflow decision" precedent used elsewhere in this codebase). No pincode/distance, SLA, workload, or stock-aging input; no configurable rule table; no split-shipment; no manual-reallocation hook (`TransitionTaskStatus`'s reroute-on-reject path in `fulfillment.go` reuses this same single strategy). Tracked at 26.12.2.

## 6. Fulfillment execution — Pick/Pack

Blueprint: distinct Pick Task and Pack Task stages, scan-first validation (wrong-barcode/duplicate-scan blocked instantly), short-pick reason capture, and a hard rule that packed quantity can never exceed picked quantity.

This repo (`FulfillmentTask`, `engines/fulfillment.go:81-285`): single-stage task (`Pending` → `Rejected`/`Dispatched`) — no scan validation step, no distinct pack confirmation, no short-pick reason field; a rejection triggers a full re-route to another location via sourcing rather than a partial/short-pick record. Note for whoever builds this: Stage 20 Track B.2's separate WMS engine (`engines/wms.go`, bin-level pick lists) already has adjacent structure for warehouse-internal picking — cross-check before building a parallel pick/pack path. Tracked at 26.12.3.

## 7. Courier, Shipment and Manifest

Blueprint: serviceability check, AWB generation, label generation, manifest creation (grouped by courier/pickup-slot/location), tracking sync, RTO detection — all via a Courier Provider connector, the same "provider framework" pattern the blueprint uses for Channel/Inventory/Fulfillment/Payment/Tax providers.

This repo (`CreateLogisticsBooking`, `engines/marketplace.go:10-37`): a bare manual record — carrier name, tracking number, shipping charge, hardcoded status `"Shipped"`. No serviceability check, no AWB generation, no label, no manifest grouping, no RTO/tracking-event ingestion at all. **This is the single largest orchestration gap versus the blueprint.** Tracked at 26.12.4 — should reuse the same outbox/provider-connector pattern already proven for Shopify/BigCommerce/Magento/Unicommerce (`engines/connector_*.go`, `engines/unicommerce.go`), not a new integration mechanism.

## 8. Cancellation and Hold Management

Blueprint: order-level Hold (payment/risk/address/SKU reasons, owner, release action) plus a stage-gated cancellation matrix (allowed pre-reservation and pre-dispatch, restricted/blocked after pick/pack/dispatch, mandatory reason codes).

This repo: confirmed via a direct search (`grep -r "Hold|OrderHold|ReasonCode|reason_code|CancellationReason" engines/`) — no order-level hold mechanism and no reason-code master exist anywhere, distinct from the maker-checker *approval* engine (which gates document actions generically but is not an order-hold queue). `FulfillmentTask`'s "Rejected" transition is the closest analog today, and it unconditionally re-routes rather than checking a cancellation-stage matrix. Tracked at 26.12.1 alongside the Order Engine gap, since Hold/Cancel only make sense once there's an actual multi-level order status to hold or cancel against.

## 9. Returns, RTO, QC and Refund

Blueprint: return request → eligibility check → approval → pickup → scan receipt → QC disposition (Sellable / Damaged / Repairable / Missing / Wrong-Item / Rejected) → inventory bucket update per disposition → refund request (line-level, policy-gated, owned by Finance but requested by OMS) → close.

This repo (`ProcessReturnAnywhere`, `engines/fulfillment.go:291-443`): a real, working return-to-any-store flow — validates against the original sale's line items (`resolveOriginalSale`), blocks over-return via `sumPriorReturns`, enforces a 30-day window (`salesReturnWindowDays`), increments stock at the return location, and reverses GL (debit Sales Revenue/credit Cash, debit Inventory/credit COGS) — but it is a single-step "receive and restock" flow: no request/approval step, no QC disposition bucketing (everything becomes sellable stock unconditionally), no RTO path (courier-returned undelivered shipments), and no separate refund-request record — the GL reversal *is* the refund, posted immediately, not policy-gated on QC. Tracked at 26.12.5.

## 10. Exception, Error Recovery and Reconciliation

Blueprint: centralized `exception_case` + `retry_queue` + `dead_letter_event` tables, plus reconciliation jobs comparing OMS vs. channel/ERP/WMS/courier/payment.

This repo already has the real backbone for this, just not OMS-specific dashboards yet: `integration_event_outbox`/`integration_event_log` (`engines/outbox.go`, reused generically by `engines/unicommerce.go:295`'s `processUnicommerceOutbox` — `attempts<5` retry cap, `Pending`/`Failed`/`Dispatched` states) is the same idempotent-write-then-async-drain pattern the blueprint's provider framework calls for. Stage 26.10.5 ("Exception queues... as a dashboard widget") already plans a generic UI over this. No dedicated reconciliation-variance report (OMS vs. channel/courier) exists yet. Tracked at 26.12.7 as a dashboard/report extension, not a new mechanism.

## 11. Reports, Configuration Masters, Roles/Audit/Notifications

- **Reports**: the blueprint's Order Aging, SLA Breach, Allocation Pending, Stock Mismatch, Return Aging, Reserved Stock, and Courier Performance reports have no equivalent yet. The `ReportDefinition` framework (Stage 20 Track B.4) is the right place to add them, same pattern as the existing Stock/Sales/Vendor-Ledger/Payables reports. Tracked at 26.12.8.
- **Configuration masters**: the blueprint's `allocation_rule`, `reason_code`, and `status_transition_rule` tables have no equivalent. This repo's generic doctype-meta engine can hold these as ordinary doctypes without a new mechanism. Tracked at 26.12.9.
- **Roles/permissions/audit**: already covered generically by this repo's `role_permissions` table + `LogAuditEvent`. The blueprint's OMS-specific role list (OMS Admin, CS, Warehouse Picker/Packer, Dispatch, Store Fulfillment, Warehouse Manager, Finance, Integration Admin, Audit) is a content checklist for configuring roles once OMS screens exist, not a new mechanism.
- **Notifications**: the blueprint wants event-driven alerts (exception owner, SLA breach, approval). This repo's `alerting.go` (Stage 17.10) already does incident-level Slack/Teams alerting; per-order/customer notification templates don't exist yet — out of scope for Stage 26.12's first pass, noted here for a later phase rather than silently dropped.

## 12. Concrete algorithmic patterns worth preserving (validated by the retired prototype's working implementation)

The two blueprint PDFs describe policy/requirements in the abstract; the standalone project's own `js/services/*.js` (read in full: `order.js`, `allocation.js`, `fulfillment.js`, `return.js`, `inventory.js`, `shipping.js`) show one concrete, coherent way to implement the resulting state machine. None of this code was ported, but the *shapes* below are worth keeping as implementation guidance for whoever picks up the matching Stage 26.12 item — the same "design note, not new code" treatment the WMS retirement gave its own prototype's replenishment/wave-picking logic.

- **Order validate→reserve chain — 26.12.1**: `createOrder` duplicate-checks on `(channelId, channelOrderId)`, then chains straight into `validateOrder` → `reserveInventoryForOrder`. Validation order matters: SKU-mapping resolution first (per line, must be active + product sellable), then pincode format, then prepaid-payment-confirmed — each failure sets `Validation Hold` and raises a *specific* exception code (`SKU_MAPPING_FAILED`/`ADDR_INVALID`/`PAYMENT_PENDING`) rather than a generic error, so a hold queue can route by reason. `releaseHold` resolves the order's open exception case(s) then re-runs the same `validateOrder` chain rather than a bespoke resume path. `cancelOrder` takes a mandatory reason code, forbids cancellation once `Shipped`/`Delivered`/`Closed`/`Cancelled`, and releases reservations only for lines still `Reserved`/`Allocated`.
- **Allocation strategy selection — 26.12.2**: each strategy is a simple per-line candidate filter, not a scoring engine — `WH_FIRST` picks the first warehouse-type location with enough ATP; `NEAREST` uses `abs(orderPincode − locationPincode)` as a distance proxy (a real geo/zone lookup would be a genuine improvement, not just a port); `HIGHEST_STOCK` picks the max-ATP qualifying location. Split-shipment detection is a side effect of grouping chosen locations into a map: more than one group present *and* the channel disallows partial shipment → hold the order with an exception instead of allocating. Reallocation forbids a fixed status set (`Picked`/`Packed`/`Shipped`/`Delivered`/`Cancelled`) and rolls back to the original location's reservation if the new location's reservation attempt fails (reserve-then-verify, not verify-then-reserve).
- **Pick/Pack scan validation — 26.12.3**: a scan resolves barcode→product→SKU, then matches against the *first task line* for that SKU where picked/packed qty is still below expected — a wrong/unrelated/already-fulfilled barcode gets a specific rejection message naming the actual product it belongs to, not just "invalid scan." Short-pick computes the remaining shortfall and marks all of it short in one action (no partial-short-then-more-picking modeled). Pack completion hard-blocks on any line where packed qty doesn't exactly equal expected qty, and auto-generates an invoice record from the pack task's lines plus captured package dimensions on success.
- **Shipping/manifest — 26.12.4**: courier selection filters couriers by destination-pincode serviceability then sorts by priority — serviceability-then-priority, no cost/SLA scoring. Manifest generation groups already-AWB-assigned shipments by courier+location. Handover cascades three status updates atomically: shipment → Handed Over, its fulfillment order → Dispatched, and the parent order → Shipped only once *every* fulfillment order for that order has reached Dispatched/Closed (otherwise Partially Fulfilled) — this is the concrete split-shipment-aware order-closure rule the blueprint describes only abstractly in its status matrix.
- **Return/QC/refund — 26.12.5**: return eligibility is an allowlist on order status (Shipped/Delivered/Closed/Partially Fulfilled), not a return-window date check — the blueprint's own "within N days" rule isn't actually implemented in the prototype; note this as a gap in the prototype itself, not something to inherit uncritically. QC disposition drives inventory bucket directly: Sellable → restock to physical; anything else → damaged bucket, with no intermediate repair/investigation bucket despite the blueprint listing one. Refund amount is computed from the *original order line's* price minus discount, not the return-time price — worth preserving, since recomputing from a since-changed current price would let unrelated price changes affect a refund. Refund auto-approves only if every return line QC-passed; any single QC failure holds the *whole* refund rather than partial-refunding the passed lines — a real fork against the blueprint's own "refund should be line-level" principle; flag this explicitly rather than silently picking one when 26.12.5 is built.
- **Inventory sync — 26.12.6**: stock-snapshot ingestion validates SKU/location existence per line before applying anything (an unknown SKU/location raises a per-line exception without aborting the whole batch), and raises a reconciliation-variance exception when an incoming physical count differs from the locally-held one *before* overwriting it — reconciliation-on-ingest rather than silent overwrite.

## 13. What was deliberately NOT carried forward

- The standalone project's separate Go multi-tenant service (own `go.mod`, own port, JWT/OAuth verification middleware, canary version routing by tenant/feature-flag, a planned WebAssembly-sandboxed customer-extension runner via `wazero`, Redis-cached tenant routing) — conflicts with this repo's single-binary/schema-per-tenant/no-new-dependency rules. Read in full, not ported.
- The standalone project's 3-service architecture assumption (ERP on port 8080 / OMS client-side-only / WMS on port 8081, per its own `docs/architecture_plan.md`) — already stale, since this repo absorbed WMS directly (Stage 20 Track B.2 plus the WMS retirement above); OMS belongs in-process the same way.
- SQLite dev-mode persistence and Docker Compose multi-service orchestration (`docker-compose.yml`, `Dockerfile`) — this repo's standing policy is no containerization (Stage 14: built once, then reverted by deliberate decision).
- The mock/localStorage-driven vanilla-JS frontend (`index.html`, `js/{app,state,mockData}.js`, `js/services/{order,inventory,allocation,fulfillment,shipping,return}.js`) — a UI simulator with hardcoded seed data (products, warehouses, couriers) and a role-selector dropdown standing in for real auth; not reusable against this repo's real multi-tenant auth/RBAC and existing `public/app.js` conventions. Its *domain logic* (the ATP formula, allocation strategy names, pick/pack/QC flow shapes) is exactly what sections 1-9 above already extracted — the code itself was not ported.

## 14. Superseded standalone-project docs

The standalone project's own `docs/` (`architecture_plan.md`, `development_plan.md`, `ledger.md`, `ai_handover.md`) and `README.md` are superseded by this file plus `micro_checklist.md`'s Stage 26.12 — nothing in them describes a capability not already captured above or already covered by this repo's own Stage 20/26 work. The standalone project folder itself was deleted after this migration was verified against it file-by-file (see `project_ledger.md`'s retirement entry).
