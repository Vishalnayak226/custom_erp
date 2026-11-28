---
doc_id: DOC-PRODUCT-INVENTORY-PLANNING-PLAN
title: Inventory planning plan
type: reference
status: in-review
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md items 323.1-323.30, 324.2, 324.4, 273.2
authority: proposed-design
confidentiality: internal
last_verified: 2026-11-28
review_by: 2027-02-26
supersedes: none
superseded_by: none
---

# Inventory planning plan (ask 323, 2026-11-28)

The owner asked for a thorough plan for inventory planning and sent three sources:

- their own *ERP Inventory Planning and Deadstock Redistribution Blueprint* (8 pages, 28 Nov 2026);
- Vandeput, *Inventory Optimization: Models and Simulations* (De Gruyter, 2020);
- Hyndman and Athanasopoulos, *Forecasting: Principles and Practice* (FPP). The PDF read was the
  2nd edition, with R. The blueprint names the Python edition. The methods are the same.

The blueprint covers a jewellery retailer: 106 stores, one central warehouse, two legal entities
sharing that warehouse, one barcode per piece, and both store and online sales. This plan follows
the blueprint in nearly everything. Where it differs, the reason is something already in this
code. The client is not named here or anywhere in the product (owner's rule, item 106.3).

The rules behind every decision come from the blueprint, and both books agree with them:

- Every number a planner sees can be explained.
- Nothing moves or gets bought until a person approves it.
- Stock is moved before more is bought.
- A model replaces a simple rule only after a back-test shows it beats that rule.

## What is already built

Much of what the blueprint asks for already exists. The plan builds on these instead of adding
second versions:

| Blueprint asks for | Already in the code |
|---|---|
| Incremental stock position, separate stock states | `inventory_availability`: on hand, available, reserved, committed, in transit, blocked, QC hold, damaged, channel buffer, hold, inward |
| Firm pipeline: open PO, TO in transit, back-orders | `loadReorderInbound` (`engines/reorder_position.go`, item 146.1.4) |
| Order quantity shaping: MOQ, max, multiple | `ReorderPointConfig` per item × location; `shapeReorderQty` |
| Planned action, approval, conversion to a draft PO or TO | `SupplyProposal` (`engines/supply_proposals.go`, Stage 73.2.18): one open proposal per source, item and location; approved into a Draft PO, TO or production order |
| Pegging | `GetPeggedDemand`, report `pegged-demand-supply` |
| Move before you buy | `RebalanceSuggestions` (`engines/inventory_rebalance.go`, item 273.2), which also counts open claims on a donor |
| Design and Combination | Product family with a design number, and Item with its Combination ID and barcode, both searchable at the till (items 272.1, 272.2) |
| Unit barcode with its movements | `SerialNumber` (`engines/serial_tracking.go`): status, location, customer hold, receipt cost and GRN, movement history; transfers and the till already handle serials |
| Item lifecycle | Item stage Launch / Active / Phase-out / End of life, launch date, substitute item (`engines/pim_item_lifecycle.go`) |
| Legal entity per location | `LegalEntity` master, `Location.legal_entity` (`engines/intercompany.go`) |
| Bounded background worker, idempotency | Job runner on `async_jobs` (`engines/jobrunner.go`): leases, retries, idempotency keys, progress |
| Ageing | Reports `cat-ageing-inventory-value`, `batch-ageing`, `cat-fast-slow-moving-stock` |
| Budgets | `engines/budgeting.go` |
| Source selection, distance | Ask 324's sourcing engine (being built now): spare stock, PIN-code distance, days of cover |

## What the blueprint needs that is missing

- **The forecast is a flat average.** Demand is total sales over the last 30 days divided by 30 (`SalesVelocityAt`). There is no:
  - stockout censoring (a day with no stock counts as a day with no demand)
  - seasonality, offer uplift or new-item handling
  - back-test
  
  `CalculateWeightedVelocity`'s "prior" period overlaps the recent one.
- **Safety stock is a typed number.** It is not computed from how much demand and lead time vary.
- **Planning runs one location at a time.**
  - Rebalance picks one donor per item for one destination, so two short stores can both be promised the same donor stock.
  - Rebalance does not check legal entity or stock age.
- **The scan stops at 5,000 rows a location** (`optimizationScanRowCap`). Items a store should carry but has no stock row for are never proposed, which blocks one-for-one replacement. The config is also read with one query per item.
- **Missing records:**
  - policy levels above store × item
  - store clusters
  - a measured vendor lead time
  - a daily stock record (needed to know which days were stockouts)
  - lost sales
  - an offer calendar for planning
  - a run record with its trace
  - a stale-feed gate
- **A transfer between two legal entities is not refused, and there is no intercompany stock flow.** `IntercompanyTransaction` is financial only.
- **Supply Proposals sit under Manufacturing** in the sidebar.

## Decisions

| # | Decision (recommended) | Why |
|---|---|---|
| D1 | **Pilot one legal entity:** its warehouse and about 10 representative stores. Go national after release 1's UAT and release 2's back-test gate. | The blueprint's own rollout. Mistakes stay small and the numbers can still be compared. |
| D2 | **Grain is item × location × day.** The blueprint's Combination is an Item, whose Combination ID is the item code. Its Design is a product family with a design number. Design and category are forecast levels. A piece is chosen only at reserve, pick or transfer. No new record. | The client's till already works this way (items 272.1, 272.2). |
| D3 | **What the 10-digit barcode marks, which the owner confirms.** If one tag marks one piece, it is the serial number of a Serial-tracked item, and no new barcode model is needed. Existing stock is then put on serials by the client's own import, not by a release. If one tag marks a combination, keep the item barcode and plan by quantity: "each donor barcode once" then reads "each donor unit once". | `SerialNumber` already does what the blueprint's "barcode unit" record does. The till today shows one barcode per combination (272.2). Article 5: a release never changes client data. |
| D4 | **Legal entities stay apart.** The planning run never pairs locations of different entities. A manual cross-entity transfer warns now, and is refused once the intercompany stock flow exists. That flow waits on Finance's treatment. | Blueprint §1 and scenario 6. Refusing outright today would stop transfers the client may make now. |
| D5 | **One calculation.** Upgrade the shared reorder calculation in place, so Supply Proposals, rebalance and ask 324's sourcing and auto requests all use the same numbers. Once 324 ships, an approved transfer proposal becomes a 324 stock request, so the source still accepts it. 324's daily auto request stays off at stores the run covers. | Article 1: fix it once where every caller passes. Two engines making requests for the same shortage would double them. |
| D6 | **Reuse the records.** `SupplyProposal` stays the planned action and gains Reviewed, an override reason, pins with expiry, the trace and the run id. `ReorderPointConfig` stays the store × item level. A new `PlanningPolicy` covers only the levels above it. `inventory_availability` is the stock-position table, the job runner runs the run, and `SerialNumber` is the unit record. | Article 1: no second way to do the same thing. |
| D7 | **Forecasting in plain Go with simple methods,** chosen for each group by back-test: naive, seasonal naive, moving average, simple exponential smoothing, damped trend, Croston. ETS, ARIMA and harmonic regression are added only if the back-test shows the simple ones losing. | Article 1: no Python or R dependency. FPP ch. 3 and 12.4: simple methods are hard to beat and are the benchmark anyway. |
| D8 | **Safety stock stays typed through release 1.** In release 2 it is computed per policy, but only where the back-test shows the computed figure holds the service target with less stock. | Vandeput ch. 6. A formula is only as good as the error it is fed. |
| D9 | **Greedy allocator with one shared supply ledger** in release 3. Min-cost flow only if measured. | The blueprint's own order. Greedy is explainable line by line. |
| D10 | **No auto-approval in releases 1-3.** Every proposal is approved by a person within their approval slab. After the pilot, consider auto-approval for low-value lines with high confidence. | Blueprint §7. Trust comes from the trace first. |
| D11 | **Lost sales at the till:** an *Asked, not in stock* action in the item search when an item shows 0 here. Set per store, off by default. | Blueprint §3. Without it, a store that is always empty forecasts zero. It adds a till step, so the owner decides. |
| D12 | **Planning moves under Inventory:** a Planning Home with exception tiles, and Proposals, Policies and Runs each on their own page. Manufacturing keeps production proposals. | Article 7: one page, one job. |
| D13 | **The run is nightly,** at the client's hour after day close, with a *Run now* for one location. Back-tests run weekly at night. | First principle, applied to the server. Daily is the blueprint's cadence. |

## Order of work

| Order | Phase | Items | Done when (blueprint gate) |
|---|---|---|---|
| 1 | Owner decisions | 323.2 | The owner answers, or says "all recommended" |
| 2 | 0 Data readiness | 323.3-323.9 | The readiness check passes for the pilot entity, or each exception is explicitly excluded |
| 3 | 1 Transparent rules | 323.10-323.15 | Scenarios 1, 2, 6, 9, 10 and 11 pass. UAT passes for unit and ownership controls |
| 4 | Pilot release 1 | - | Planners review four weeks of proposals and the KPI baseline is recorded |
| 5 | 2 Forecast and launches | 323.16-323.23 | The back-test beats seasonal naive on the pilot's history. Scenarios 7 and 8 pass |
| 6 | 3 Network redistribution | 323.24-323.26 | No donor is over-allocated. Moved pieces sell better than the hold-or-exit baseline. Scenarios 3, 4 and 5 pass |
| 7 | 4 Optimization | 323.27-323.30 | The measured benefit covers the compute and the operating effort |

Phase 0 can start at once. Phase 1 starts after ask 324 ships, because it changes the calculation
324's sourcing reads and hands transfers to 324's request flow.

## Phases

### 0. Data readiness

**Why.** The blueprint's §8 rule: stop the run on a stale feed, a failed reconciliation or a
missing entity mapping, rather than produce plausible but unsafe numbers. Forecasting also needs
to know which zero-sale days had no stock, and the run needs real lead times.

**Items.**

- **323.3 Planning readiness check** (INV › Stock levels & availability). For each location and item it checks:
  - the location has a legal entity;
  - on hand agrees with the stock ledger;
  - a serial item's in-stock pieces equal its on-hand count;
  - nothing is negative;
  - the Unicommerce feed is newer than the client's limit.
  
  Failures go on an exception list, and the run holds proposals for the affected location or item (scenario 10). A test shows that QC hold, blocked and damaged stock is never available or donor stock (scenario 11).
- **323.4 Daily stock position** (INV › Stock levels & availability). A new table, `inventory_daily_position`, holds day, item, location, closing on hand, closing available and a stockout flag. A nightly job writes it. History is rebuilt once from the stock ledger by an admin-run job, never at deploy.
- **323.5 Vendor lead time, measured** (PROC › Vendors). For each vendor × item, and for each category:
  - the actual time from PO approval to GRN, with its mean and spread;
  - promised date against actual.
  
  It is a report, and the run reads it.
- **323.6 Transfer lanes** (INV › Transfers & stock movements). Lane time comes from actual dispatch-to-receipt history between two locations. Distance comes from ask 324's PIN-code distances. The cost per piece is the client's figure, with a default by distance band.
- **323.7 Cross-entity transfer guard** (INV › Transfers & stock movements). D4: the run never pairs two entities, and a manual transfer between entities warns.
- **323.8 Intercompany stock flow** (FIN › Currencies & multi-company). A transfer between entities carries its intercompany documents, and it becomes the only way to move stock across entities. Waits on Finance's treatment (owner action 3).
- **323.9 Piece barcode is the serial** (INV › Batches, serials & ageing). D3, needed only if one tag marks one piece. Check the pilot's flow end to end:
  - the receipt registers the tag;
  - the till sells it;
  - a transfer picks it;
  - a return goes to QC;
  - the piece's age is read from its receipt date.

**Done when.** The readiness check runs clean for the pilot entity, or each exception is excluded
by name, and the daily position has the pilot's history.

### 1. Transparent rules (blueprint release 1)

**Why.** Store and warehouse min-max, one-for-one, customer orders, draft TO and PO, the
explanation and planner approval, all at network scale. Today the calculation runs per location,
stops at 5,000 rows, and cannot see items a store has no stock row for.

**Items.**

- **323.10 Planning policies** (INV › Replenishment & planning). `PlanningPolicy` sets a policy for a scope.
  - **Scopes:** all, legal entity, category, product family, store cluster or location. `ReorderPointConfig` stays the store × item level.
  - **Types:** min-max, reorder point, one-for-one display minimum, order only, launch, manual/exit.
  - **Parameters:** minimum, maximum, display floor, review days, service target, lead-time override, MOQ, multiple.
  - **Effective dates:** from and to, with the document history as the change log.
  - **Clusters:** a planning cluster field on Location.
  - **Resolution:** the most specific level wins, and one bulk resolver serves the whole run.
- **323.11 Network planning run** (INV › Replenishment & planning). One job over every active location, with the idempotency key tenant + run date. It upgrades the shared calculation in place (D5).
  - **Coverage:** it pages through every row, with no 5,000 cap, and includes items a policy covers but the store has no stock row for.
  - **Projected position by due date:** only firm receipts due before arrival count, using the PO's expected date and dispatch plus lane time.
  - **Need:** `max(0, target at arrival − projected before arrival)` (blueprint §4).
  - **Order of filling:** customer orders first, then the display floor, then the policy target.
  - **Supply:** the warehouse fills stores first as transfer proposals. The rest rolls up to the warehouse and becomes purchase proposals per vendor, with MOQ, multiple, minimum order value and the reject allowance.
  - **Urgency:** a missed release date is marked urgent.
  - **Run record:** id, as-of time, policy and model versions, counts and exceptions.
- **323.12 Trace and review states** (INV › Replenishment & planning). Every proposal carries its trace: the parts of the position, the demand used, the safety stock, the policy and the level it came from, and the rule applied. The proposal then moves through these states:

  | State | What happens |
  |---|---|
  | Proposed | The run creates it |
  | Reviewed | The planner may override the quantity with a reason, or pin or exclude it with an expiry |
  | Approved | Within the approver's slab. Bulk approval only within that slab |
  | Converted | Becomes a Draft PO, Draft TO or 324 stock request, under an idempotency key |
  | Done | The target document closes |

  Approval re-runs the line and refuses it if the position has moved.
- **323.13 Planning screens under Inventory** (INV › Replenishment & planning). D12: a Planning Home with exception tiles, and Proposals, Policies and Runs each on their own page. The user and admin guides are updated with them.
- **323.14 Planning KPIs** (INV › Inventory dashboard & reports), one registered report covering:
  - **Service:** stockout days on policy items, customer orders filled on time.
  - **Inventory:** days of cover, aged value.
  - **Planning:** approval, override and reject rates.
  - **Execution:** POs and transfers on time against measured lead time.
- **323.15 Acceptance tests** (INV › Replenishment & planning). Scenarios 1, 2, 6, 9, 10 and 11 run on scratch fixtures, with units and value reconciled by entity, item and location before and after. A wrong-tenant test and a concurrent-approval test are included (Article 6).

**Done when.** The pilot's nightly run explains every line, nothing moves without approval, and
the six scenarios pass.

### 2. Forecast and launches (blueprint release 2)

**Why.** A flat 30-day average misses three things:

- stockouts, where a day with no stock looks like a day with no demand;
- seasons and offers;
- new pieces.

Jewellery demand per store × item is sparse, so the signal lives in groups.

**Items.**

- **323.16 Demand history, cleaned** (INV › Replenishment & planning). Daily demand per item × location from the same sources the velocity uses today.
  - Sale, return and cancel are kept as separate series. Demand is gross sales, so ask 274's rule on till returns still holds.
  - An online order counts once, by its source event id.
  - Stockout days from 323.4 are marked unknown, not zero.
  - Event days (323.20) and outliers (FPP 12.9) are flagged.
- **323.17 Lost sales and substitutions** (POS › Billing & till). D11. *Asked, not in stock* records the item, store, quantity and time. If the cashier picks the item's substitute from the same prompt, that is recorded as a substitution.
- **323.18 Forecast methods in Go** (INV › Replenishment & planning). Weekly baseline from daily data.
  - **Methods:**
    - mean, naive, seasonal naive (same weekday last week, same week last year), drift and moving average (FPP 3.1);
    - simple exponential smoothing and damped trend (FPP 7);
    - Croston on the sizes of non-zero demands and the gaps between them, for sparse series (FPP 12.2).
  - **Weekly season:** with a 52.18-week year, the same week last year is matched by date, not by week number (FPP 12.1).
  - **Levels:** the forecast is made where demand is dense (product family or category × cluster) and split down by recent shares. The series are added up from store × item only where those series are dense (FPP 10).
- **323.19 Back-test and model choice** (INV › Replenishment & planning).
  - **Method:** rolling origin, using only data known at each cut-off (FPP 3.4; blueprint §9).
  - **Measures:** bias and MAE per series, WAPE in aggregate, and MASE against seasonal naive, which still works on sparse series where MAPE does not.
  - **Rule:** a method is used for a group only if it beats seasonal naive there.
  - **Records:** the run stores the model version, and forecast accuracy joins 323.14.
- **323.20 Events and offers** (INV › Replenishment & planning). `PlanningEvent` holds a scope, dates and an uplift. Its source is a campaign, a POS offer or a manual entry. The uplift applies only on its dates and then expires, and event days are left out of the baseline (scenario 8).
- **323.21 New items and launches** (INV › Replenishment & planning). A Launch-stage item is forecast from its analogue, or from its product family's average, along a ramp-up profile. Launch allocation is set by cluster, and the forecast is marked low confidence (scenario 7; FPP 4.4, 4.6).
- **323.22 Forecast overrides** (INV › Replenishment & planning). A planner override by scope carries a reason, an expiry and an approver. The back-test scores overrides against the model (FPP 4.7).
- **323.23 Computed safety stock** (INV › Replenishment & planning). D8.
  - **Formula:** Ss = z·√((L+R)·σd² + σL²·d²) (Vandeput eq. 6.5).
  - **Inputs:** σd is the forecast error, not the raw demand spread. σL comes from 323.5.
  - **z:** from the policy's cycle service target. A fill-rate target is optional (Vandeput ch. 7).
  - **Display floor:** stays separate.

**Done when.** On the pilot's history, replayed only with data known at each cut-off, the chosen
methods beat seasonal naive, and planned availability improves for the same or less stock.

### 3. Network redistribution (blueprint release 3)

**Why.** Today each short store finds its own donor, so two stores can be promised the same
pieces, and aged stock has no route out. The blueprint wants one network pass before any buying.

**Items.**

- **323.24 Stock health** (INV › Batches, serials & ageing). Every item × location, and every piece by its receipt age, is graded:
  - Healthy or protected;
  - Slow or excess;
  - Dead or end of life;
  - Blocked.
  
  Thresholds are set by category and lifecycle in the policy. Dead and end-of-life stock goes to an exit list (markdown, vendor return or disposal) for the buyer, and is never transferred automatically (scenario 4).
- **323.25 Network redistribution** (INV › Replenishment & planning). Runs in the nightly run, before buying, in five steps (blueprint §5):
  1. Every store's net need.
  2. Each donor's surplus: stock above its floor and its own demand over lead time, minus open claims and open 324 requests.
  3. Feasible pairs: same legal entity, QC-cleared stock, and a lane time that arrives before the need date. Otherwise the warehouse or an urgent purchase (scenario 5).
  4. Ranking by benefit minus cost. Customer order before display floor before forecast. Slow and excess stock is given first. Lane cost comes from 323.6.
  5. Greedy allocation over one shared ledger, so each donor unit is used once (scenario 3).
  
  A location never gives and receives the same item in one run. The run uses 324's spare and distance rules rather than a second engine, and every line carries a reason code.
- **323.26 Redistribution outcome** (INV › Inventory dashboard & reports). Moved pieces' sell-through within N days, against similar pieces left in place. This is release 3's go-live gate.

**Done when.** No donor is over-allocated in any run, and moved pieces sell better than the
hold-or-exit baseline.

### 4. Optimization (blueprint release 4)

**Why.** These steps come only after the earlier phases' data shows where the money is.

**Items.**

- **323.27 Open-to-buy gate** (PROC › Purchase orders). Budget by entity × category × month (`engines/budgeting.go`). Purchase proposals are ranked by service value, and what was cut is shown. A buyer approves anything over budget.
- **323.28 Service level from cost, season buys** (INV › Replenishment & planning).
  - **Service target per item class** from holding cost against shortage cost: 1 − hR/b for an (R,S) policy (Vandeput ch. 8).
  - **One-time season buys** by the critical ratio cu/(cu+co) (ch. 11).
  - **Warehouse vs store:** the warehouse covers only its own risk period, and the stores carry the review period (ch. 10).
- **323.29 Policy simulator** (INV › Replenishment & planning). Replays an item × location's history under a candidate policy and compares service, stock and orders before the planner switches (Vandeput ch. 13). This is also the blueprint's scenario comparison.
- **323.30 Optimal allocator, only if measured** (INV › Replenishment & planning). Min-cost flow replaces greedy only when 323.26 shows greedy leaving measurable benefit.

## Not adopted

| Proposal | Instead | Why |
|---|---|---|
| Python forecasting stack (blueprint §11, FPP Python edition) | Plain Go | Article 1: no new dependency. The simple methods are a few dozen lines each |
| ETS, ARIMA, harmonic regression in release 2 | Simple methods chosen by back-test | Added only if the back-test shows the simple ones losing (FPP 12.1, 12.4) |
| Optimal hierarchical reconciliation (FPP 10.7) | Top-down from recent shares, middle-out, bottom-up where dense | Needs matrix work for little gain on sparse jewellery series |
| A separate planned-action record | `SupplyProposal` with new states | D6 |
| A separate barcode-unit record | `SerialNumber` | D3 |
| A full input snapshot per run | Each line's trace holds the inputs it used. The daily position gives the as-of state. The run keeps versions and counts | A full copy of 106 stores × every item each night is heavy and repeats what the ledger already holds |
| Gamma and kernel-density demand (Vandeput ch. 9, 12) | Normal first | Phase 4's simulator tests whether normal under-protects the low-volume classes |
| Auto-approval | Not before the pilot's KPIs | D10 |

## Owner actions

1. Approve the decisions, or say "all recommended".
2. Name the pilot entity and stores. This goes in the owner's notes, not the product. Say whether
   one 10-digit tag marks one piece or one combination.
3. Finance: the treatment for stock moving between the two entities that share the warehouse.
4. Merchandising: which items each store cluster carries, which drives one-for-one and launch
   allocation. Also age thresholds by category and service targets, or accept the defaults.
5. For phase 4: the budget by entity × category × month.

## Guardrails

- **First principle.**
  - The nightly run is one job in bounded batches, with a checkpoint per batch. Back-tests run weekly at night.
  - Local tests run per package on scratch fixtures.
  - The run's cost is measured on the pilot before national rollout, using ask 313's load method.
- **Article 5.**
  - Migrations are additive.
  - History rebuilds are admin-run jobs.
  - Serial tracking on existing stock is the client's import.
  - No release changes client data.
- **Article 6.** Converting proposals into documents is an economic command. It is tested for replay, concurrency, partial failure and reconciliation, along with wrong-tenant and wrong-entity cases.
- **Article 7.** Title-only screens, short text, one page for one job, no number spinners.
- **Shared tree.** Ask 324 is being built by another session. Nothing here edits its files, and phase 1 waits for it to ship.

## Sources

- **The blueprint:**
  - §1-§8: the design;
  - §9: the 11 acceptance scenarios, numbered here in the blueprint's order;
  - §10: the releases and their gates, which are the phases above.
- **Vandeput:**
  - ch. 1-5: policies, EOQ, order-up-to level;
  - ch. 6: safety stock with variable lead time, eq. 6.5;
  - ch. 7: fill rate;
  - ch. 8: optimal service level;
  - ch. 9: gamma demand;
  - ch. 10: multi-echelon risk periods;
  - ch. 11: newsvendor;
  - ch. 12: discrete and KDE demand;
  - ch. 13: simulation optimization.
- **FPP 2nd ed.:**
  - 3.1: simple methods;
  - 3.4: accuracy, MASE, time-series cross-validation;
  - 4.4, 4.6, 4.7: analogies, new products, judgmental adjustments;
  - 7: exponential smoothing;
  - 10: hierarchical forecasts;
  - 12.1: weekly seasonality;
  - 12.2: Croston;
  - 12.4: combinations;
  - 12.9: missing values and outliers.
