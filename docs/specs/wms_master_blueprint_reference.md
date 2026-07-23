# Universal WMS Master Blueprint — Reference Notes

**Source document:** `Universal_WMS_Master_Blueprint_Developer_Doc.pdf` (24 sections, v1.0, dated 9 Jul 2026) — an industry-general, architecture-agnostic WMS design reference (patterns distilled from Odoo/ERPNext/Dynamics 365/NetSuite/OpenBoxes/SAP EWM/GS1/OWASP/NIST). Read in full 2026-07-23.

**Why this file exists:** a standalone project (`Antigravity Projects/WMS`, separate Go service on port 8081 + React/Vite frontend on port 5173, own `wms_tenant_<id>` schema convention) was started against this blueprint before this repo's own WMS work existed. That project's architecture conflicts with this repo's standing rules (no new frontend framework, no new third-party dependency, one lightweight server — see `CLAUDE.md`), so it has been retired rather than merged as code (see `project_ledger.md`'s retirement entry for the reasoning). This file is **the durable knowledge kept from it**: the blueprint's design content, reconciled against what this repo already has. Everything below is reference/checklist material for whoever builds the remaining Stage 26.3/26.5 items — it does not describe a new module to build.

**Headline finding:** this repo's own forward roadmap (Stage 26.5 "WMS Enterprise Maturity Sprint" in `micro_checklist.md`, sourced from a *different* PDF — `ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf`) already mirrors this blueprint's Phase-2/3 capability map almost item-for-item (ASN capture, QC sampling, cross-dock, LPN/carton, bin-to-bin replenishment, wave/batch picking, short-pick, cartonization, ABC cycle-count, blind-count). The gap was never "this repo doesn't know these ideas" — it's "these backlog items aren't built yet." Nothing in this file adds new Stage 26 items; it only annotates the existing ones (see `micro_checklist.md` 26.5.5/26.5.6/26.5.9/26.10.1) and preserves detail the Stage 26 one-liners don't carry.

---

## 1. Inventory state machine vs. this repo's actual states

Blueprint §6.2/§12.5 state machine, mapped to what `engines/wms.go`/`inventory.go` actually implement today:

| Blueprint state | This repo's equivalent | Status |
|---|---|---|
| EXPECTED / RECEIVED_PENDING_QC | GRN receipt flow | Exists (pre-Stage 20) |
| PUTAWAY_PENDING → AVAILABLE | `PutawayToBin` (20.17), `inventory_availability` | Exists |
| Good / QC_HOLD / DAMAGED / RTV | `bin_stock.condition` via `TransitionBinStockCondition` (20.23) | Exists — any-pair transition, not a strict linear state machine, which is a deliberate looser model than the blueprint's (see comment in `wms.go`) |
| RESERVED / PICKED / PACKED / SHIPPED | `FulfillmentTask` (`fulfillment.go`), `GenerateBinPickList` (20.18), pack/dispatch mapping (20.19) | Exists |
| Cycle count + variance → approval → post | `ReconcileCycleCount`/`PostCycleCountAdjustment` (20.20-20.22) | Exists, reuses maker-checker approval engine |
| Cross-dock staging | — | **Gap** — tracked as 26.5.3 |
| RETURN_PENDING_QC | Returns flow (pre-Stage 20/RMA) | Exists in some form; not cross-checked in this pass |

Net: this repo's model is a pragmatic subset of the blueprint's state machine, not a re-implementation of it — bins carry a *condition* (Good/Damaged/QC-Hold/RTV) as a breakdown of `inventory_availability`, rather than the blueprint's full location+status matrix. That's an intentional simplification already documented at `wms.go`'s top comment and Stage 20.17's precedent; not something to "fix" toward the blueprint's fuller model without a real reason.

## 2. Movement-type catalogue (blueprint §12.2)

The blueprint's RECEIPT_ACCEPT/PUTAWAY/RESERVE/PICK/PACK/SHIP/HOLD/ADJUST/COUNT_POST catalogue is a useful checklist when wiring 26.10.1 (see §4 below) — it's the field vocabulary a real stock ledger needs, not a new engine to build.

## 3. Barcode/label strategy (blueprint §7)

- Recommends Code 128 as the default internal symbology (item/unit/bin/carton labels), GS1-128/SSCC only when a trading partner or 3PL requires it.
- **Cross-check**: `engines/stickers.go` is documented in `ai_handover.md` as "Barcode label printing (**text, not scannable symbology**)". That's a real gap against this blueprint's baseline recommendation — worth knowing before anyone builds a receiving/picking scanner screen (26.3.4/26.5) that assumes an actual scannable barcode exists on a printed label. Not scoped or added as a new Stage 26 item here — flagging it so it isn't rediscovered from scratch when 26.3.4 is picked up.
- Reprint-audit rule (user, reason, printer, old/new print count, supervisor approval for high-value) — per `micro_checklist.md:253`, already audited and satisfied ("print history + ledger already cover this").

## 4. Stock ledger field shape (blueprint §11.4) — the concrete finding behind 26.10.1

`WriteStockLedgerEntry` (`engines/inventory.go:49`) today only carries `item_id`, `warehouse_id`, `qty`, `voucher_type`, `voucher_id`. The blueprint's ledger design (§11.4/§11.5, and the retired WMS prototype's `stock_ledger` table matched it) additionally carries:

- `idempotency_key` — dedupes a retried scan/API call instead of double-posting
- `from_location_id` / `to_location_id`, `from_status` / `to_status` — what actually moved, not just a net qty delta
- `user_id` / `device_id` — who/what performed it

`micro_checklist.md`'s 26.10.1 already says the Stock Ledger report (Stage 20.40) was deferred because the ledger is "dead code" — this is *why* it wouldn't be enough to just start calling `WriteStockLedgerEntry` everywhere: without the fields above, the resulting report couldn't show location movement or dedupe a retried write. When 26.10.1 is picked up, these are additive columns on the existing `StockLedgerEntry` doctype, not a new engine — consistent with this repo's "additive, backward-compatible" schema rule.

## 5. RBAC role matrix (blueprint §9.1)

Receiver / QC user / Putaway user / Picker / Packer / Shipping user / Inventory controller / Warehouse manager / Admin / Auditor-Finance, each with an explicit allowed/blocked column. This repo's role-permission model already exists and is enforced generically (`role_permissions` table, self-service `GET /api/v1/me/permissions`, Stage 22.6) — the blueprint's matrix is useful as a **content checklist** for whoever configures WMS-specific role permissions (e.g. confirming a Picker role can't release a QC hold, a Putaway user can't change receipt quantity), not a new mechanism to build.

## 6. KPIs (blueprint §18.3)

Inventory accuracy, dock-to-stock time, order cycle time, pick/pack accuracy, task ageing, short-pick rate, adjustment value/rate, hold ageing, integration latency. These map onto the existing `ReportDefinition` framework (Stage 20.39's role/column masking) the same way 26.10.3's "role-based executive dashboards" already plans to use it — a content list for those report definitions, not a new reporting mechanism.

## 7. SOP topics (blueprint §21)

Receiving, Putaway, Picking, Packing, Shipping, Returns, Cycle-count, Adjustment, Barcode-reprint, Outage, Integration-failure, and Security SOPs. Cross-check against `docs/guides/ADMIN_SOP.md`/`USER_SOP.md` before writing new ones — some of this may already be covered there; this pass didn't re-audit those files line by line.

## 8. Critical UAT scenarios (blueprint §19.2) — concrete test cases for Stage 26.5/26.3.4

Worth lifting verbatim as a test-case source when that work is actually built (unit + UAT, per this repo's own testing discipline):

- Inbound exact / short / excess / damaged receipt
- Duplicate barcode scan (receipt/pick/pack) → second scan rejected or idempotent
- Wrong-bin putaway → blocked with a clear message
- Pick wrong item / wrong lot / expired lot → blocked
- Short pick → reason captured, reallocation/exception triggered
- Pack missing item / ship unpacked order → blocked
- Return restock → QC then putaway/available
- Cycle count variance → approval required before adjustment (already true here, 20.22)
- Concurrent pick on the same barcode → only one succeeds
- Integration failure mid-flow → outbox retries, error visible (this repo's `outbox.go` already has this shape)
- Unauthorized/direct API stock mutation attempt → rejected unless a valid role/approval/document backs it

## 9. Hard rules (blueprint §22, "Ten Commandments") — already satisfied, not new obligations

No direct quantity edit; every stock change through a transaction/ledger; no bin-level-free available stock; scanner-first (n/a — this repo has no scanner UI yet, tracked as the 26.3.4 gap); idempotency on writes; ERP/OMS never mutate WMS stock directly; visible integration failures; immutable audit logs; modular monolith over premature microservices; don't build advanced optimization before reliable core. This repo's existing conventions (`ValidateDocument` choke point, maker-checker approval engine, `LogAuditEvent`, single Go binary, `bin_stock` as a breakdown not a second source of truth) already satisfy essentially all of these — listed here so a future reviewer can confirm against this table rather than re-deriving it.

## 10. What was deliberately NOT carried forward

- The blueprint's recommended tech stack (§15.5: React/Vue frontend, Java Spring Boot/.NET/Node backend, separate PWA scanner client, Redis/Kafka queue) — conflicts with this repo's single-Go-binary, vanilla-JS, stdlib-only rule. Not adopted.
- The retired WMS prototype's separate-service architecture (own `go.mod`, `wms_tenant_<id>` schema-per-tenant, JWT auth with a **hardcoded secret** in `api/middleware/auth.go`) — not reusable as-is and not something to reproduce; this repo's existing `tenant_default`-per-tenant + session auth conventions stay authoritative.
- The prototype's Go domain logic (command-pattern inventory engine, wave-picking S-shape sort, bin-to-bin replenishment generator, random-sample cycle-count generator) — read in full, none copied; the *approach* each validates is noted inline on the relevant `micro_checklist.md` item (26.5.5, 26.5.6, 26.5.9) instead, phrased to fit this repo's existing per-function engine style rather than a generic command dispatcher.
