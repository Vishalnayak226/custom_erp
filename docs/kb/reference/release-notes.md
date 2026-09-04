---
title: Release notes
section: Reference
order: 25
summary: What shipped and when, generated from the build ledger so this page can't say something the ledger doesn't.
audience: everyone
last_verified: 2026-09-03
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: go run ./cmd/gendocs && go run ./cmd/genkb -->

# Release notes

Generated from `docs/project_ledger.md`'s own Stage sections - **70** entries as of this build. Each excerpt is that section's own opening paragraph, not a rewritten summary, so it reads like an engineering build log because that is what it is. For the full detail behind any entry, including what was verified and how, read the ledger itself.

## 2026-09-01

**Stage 37.11 — Role dashboards, savable layouts, digests, drill-through** *(code + schema)*

Pre-build audit of the existing exec dashboard (`renderExecDashboard`, `public/app.js`) found it fully hardcoded - 4 literal cards + a literal `sales-register` trend chart, no persistence, no per-role variation. Generalized rather than replaced: the four cards became `DefaultDashboardTiles()`'s data, not literal code.

**Stage 38.7 — Self-service sandbox tenant for integrators** *(code + schema)*

Built last of the three remaining Stage 38 items (its webhook side-effect-off hook depends on 38.4). A sandbox is a normal tenant - provisioned through the exact same `ProvisionTenantSchema` every real tenant uses - flagged via two additive columns on `public.tenants` (`is_sandbox`, `sandbox_expires_at`), not a new table.

**Stage 38.4 — Webhook subscriptions with HMAC signing, retry/backoff and DLQ** *(code + schema)*

Built on top of 38.6 (same session, built first). Extends `outbox.go` exactly as the backlog item names it: `WebhookSubscription` is a plain generic-doc-API doctype (the `ScheduledReport` precedent), and `dispatchWebhooksForEvent` (`engines/webhook.go`) is called from `processOutbox` once an outbox event is already committed - fanning out to one `webhook_delivery` async job per matching Active subscription. The job runner's own retry/backoff/DeadLettered handling IS this item's DLQ - no second retry mechanism built.

**Stage 38.6 — One general async job runner with retries, DLQ and a visibility screen** *(code + schema)*

Built first of the three remaining Stage 38 items - 38.4 and 38.7 both depend on it. The foundation `micro_checklist.md`'s own §47.11.4 already named as the target every bespoke ticker in this codebase should migrate onto incrementally, later - this stage builds the runner and its visibility screen only, no existing ticker touched. A dedicated `async_jobs` table (the `integration_event_outbox` precedent, not a generic doctype).

**Stage 37.10 — Planning depth: forecasting, reorder points, pegging, capacity** *(code + schema)*

Pre-build audit found this ~60% already built under different names: `ForecastDemand`/`CalculateSalesVelocity` (real but naive), `GetReplenishmentSuggestions`/`GetMRPSuggestions` (reorder point from call-site parameters, not persisted config), `GetProductionSchedule` (already a genuine finite-capacity scheduler, never wired to a report). Pegging was the one genuinely new piece. Every new function is a sibling of an existing one - originals untouched.

**Stage 37.9 — Quality & maintenance: inspection plans, CoA, NCR/CAPA, preventive maintenance** *(code + schema)*

Pre-build audit found all four completely absent. GRN receiving's own QC (Stage 26.5.2) is a pure quantity split with no structured per-item test list, so `PostGRNReceiptWithQC` is deliberately left untouched. `ReasonCode` is reused as-is for NCR root-cause (a new "Quality" category value).

**Stage 37.8 — Service management** *(code + schema)*

Pre-build audit found no ServiceTicket/WorkOrder/AMC concept anywhere. WarehouseTask's dispatch spine (Stage 42.2) is WMS-specific and not directly reusable as an object, but its lifecycle PATTERN - typed status enum, terminal-state guard, reason-required transitions - is what ServiceTicket's own dedicated engine functions copy, the same shape IntercompanyTransaction/LandedCostVoucher/PrepaidExpenseSchedule already use this session.

**Stage 37.7 — Projects & job costing** *(code + schema)*

Pre-build audit found no Project concept anywhere, and confirmed the codebase's own conventions point to Project as a 4th whole-posting dimension (the CostCenter/Department/Entity precedent) rather than a WIP-style running-cost ledger, since every cost-incurring doctype here posts immediately - there is no accumulation stage to attach one to.

**Stage 37.6 — Deferred revenue, prepaid amortisation, recurring billing, price-list versioning** *(code + schema)*

Pre-build audit found all four completely absent, plus a structural finding: `SalesInvoice` is lump-sum only (no `lines` field) - deferred revenue is recognised at the whole-invoice level, not per line.

**Stage 37.5 — Financial statement builder with dimensions and drill-down** *(code only)*

Pre-build audit found the codebase's own `ReportDefinition` framework explicitly rejects user-authored query/layout flexibility as an injection-risk feature outside its stated scope - so "builder" here is scoped to extending the existing hardcoded Trial Balance/P&L/Balance Sheet with dimension filters and drill-down, not a new statement-layout doctype (which would be the first violation of that stated principle). `gl_accounts` is also confirmed flat (5 basic types, never altered since Stage 1) - a full multi-section layout is a separate, larger undertaking not attempted here.

**Stage 37.4 — Budgeting, cash-flow forecast, credit limits, dunning** *(code + schema)*

Pre-build audit found all four completely absent, and one structural finding shaped the whole design: neither `SalesOrder` nor `SalesInvoice` carries a real `Customer` Link - customer identity flows as a free-text name throughout, the same convention `GetCustomerLedgerReport` already relies on. Credit-limit matching (37.4.2) inherits that name-based convention rather than adding a second, inconsistent identity model - a real Link-based rework of customer identity across these doctypes is a materially larger, separate undertaking.

**Stage 37.3 — Costing & valuation, incl. landed cost allocation** *(code + schema)*

Pre-build audit found this codebase had NO costing method anywhere - `StockLedgerEntry`/`bin_stock`/`inventory_availability` track quantity only. Two real, previously-undiscovered gaps fell out of that audit and are closed by this stage, not just the checklist's own "5 sub-items":

**Stage 37.2 — Multi-entity & intercompany: entity-scoped posting, mirrored entries, reconciliation, consolidation** *(code + schema)*

User asked to build the remaining open Stage 37/38 backlog, starting with 37.2 (the largest ERP-core item still open after 37.1's multi-currency work closed). `LegalEntity` (Stage 17.9) already existed as a Master, linked from `Location.legal_entity`, but nothing transacted across entities — this closes that gap entirely inside one tenant's schema (confirmed via `engines/saas.go` that one schema = one tenant = one consolidation group; there is no cross-tenant consolidation model in this codebase and this stage does not invent one).

## 2026-08-31

**Documentation sync pass — archived Stages 41/42/43/46, corrected a stale cross-reference, rebuilt the executive blueprint** *(docs only)*

User asked for a full docs sync: which checklist stages are ready to archive, and which docs need building so the project is legible "at a glance" to an engineering head, a product head, a developer, and the CEO.

## 2026-08-30

**Stage 36.4/36.6/36.7 — export & syndication, DAM depth, enrichment & quality — Stage 36 closes** *(code + schema)*

Closes the rest of Stage 36 (PIM parity, Unbxd level): export & syndication depth (36.4), DAM depth (36.6), and enrichment & quality (36.7, its last five sub-items — 36.7.2 had shipped earlier). With 36.1-36.7 all now built and verified, Stage 36 is **complete** and has been moved out of `micro_checklist.md` into `docs/archive/micro_checklist_closed_stages.md`. Full item-by-item detail lived there before the move and now lives in that archive entry; this section is the index.

## 2026-08-28

**Stage 36.5 + 36.3 — declarative transform rules, and PIM import depth** *(code + schema)*

Opens Stage 36's remaining five top-level items (36.3-36.7 — import, export/syndication, transform rules, DAM depth, enrichment) with the two built in dependency order: 36.5 first (a shared value-transform seam), then 36.3 (import depth), which consumes it. Full item-by-item detail, including the scope calls made explicit before building, is in `micro_checklist.md` Stage 36.

## 2026-08-25

**Stage 35.8/35.9 — Settlement reconciliation ("UniReco") and returns depth** *(code + schema + UI)*

Closes the last two open items of Stage 35's OMS parity plan: 35.8 (5 sub-items) and 35.9 (3 sub-items). Full item detail in `micro_checklist.md` Stage 35.

## 2026-08-22

**Stage 35.7 — commercial bundles and physically stocked kits** *(code + schema + UI)*

All four Stage 35.7 items are complete. `ProductBundle` is intentionally not another manufacturing BOM: it describes a sellable commercial composition, with Virtual/Stocked fulfillment and Parent/Fixed/Component pricing. Validation pins every SKU to an active Item, forbids duplicate/self/nested components and keeps one active definition per bundle SKU.

**Stage 35.6 — channel breadth becomes an operational SDK** *(code + schema + UI)*

All seven Stage 35.6 items are code-complete. The new connector contract declares auth validation, order pull, inventory push, catalogue publish, status push, error mapping and capabilities; the scheduler and UI consume capabilities instead of platform-name conditionals. Amazon and Flipkart implement their public seller contracts, WooCommerce implements REST v3, and Myntra/Meesho/Ajio/Nykaa/Blinkit/Zepto/Swiggy Instamart use a fail-closed negotiated-path adapter because their seller contracts are private. Quick-commerce descriptors bind orders to a store location and forbid split allocation.

## 2026-08-24

**Stage 42.5.5 — Multi-owner stock segregation, closing Phase 42.5 and Stage 42** *(code + schema)*

Closes the one item §110 left open: 42.D2 ("is 3PL/multi-owner a real target?") was resolved — yes, build it, the same call already made for §112's 42.6.6-42.6.9. Phase 42.5 is now 8/8 and Stage 42 (58 items across 6 phases) is fully closed. Full item detail in `micro_checklist.md` Stage 42.

## 2026-08-22

**Stage 42.6 — Labour and billing depth** *(code + schema)*

All nine listed Phase 42.6 rows are built (the source heading calls the phase eight items, but the checklist contains 42.6.1 through 42.6.9).

**Stage 35.5 — Courier integration crosses the provider boundary** *(code + schema)*

All six Stage 35.5 items are code-complete using the parity plan's default first providers, Delhivery and Shiprocket; actually calling either account remains credential-gated. Full item detail is in `micro_checklist.md` Stage 35.

**Stage 42.5 — Inventory control depth: physical inventory, CycleClass, replenishment breadth, slotting v2, facility hierarchy/copy/cross-facility inquiry** *(code + schema)*

7 of 8 items (42.5.1-42.5.4, 42.5.6-42.5.8); 42.5.5 (multi-owner stock segregation) excluded, still gated on open decision 42.D2 (is 3PL a real target?). Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-21

**Stage 46 — Money precision: paise migration** *(code + schema)*

Closed the two items both `docs/DURABILITY_AUDIT_2026-07-31.md` and `docs/ERP_LOOPHOLES_ANALYSIS.md`'s 2026-08-20 re-evaluation still carried as genuinely open. Full item-by-item detail in `micro_checklist.md` Stage 46.

**Stage 42.4 CLOSED — Outbound depth: waves, sortation, cartonization v2, packing validation, deconsolidation, loading + Bill of Lading, pre-ship gate, and VAS/kitting off the existing BOM** *(code + schema + frontend)*

Closed all 11 items of Phase 42.4 in one pass. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-20

**Stage 42.3 CLOSED — Inbound depth: dock scheduling, yard, hold/release, configurable receipt rules, catch weight, planned cross-dock, hazmat compliance, and RF receiving** *(code + schema + frontend)*

Closed all 10 items of Phase 42.3 in one pass. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-19

**Stage 42.2 CLOSED — Zone/PutawayStrategy/AllocationStrategy masters, directed putaway, exception follow-on, the warehouse cockpit, and a real bug found in Stage 45's own render mechanism** *(code + schema + frontend)*

Asked to finish the rest of Phase 42.2 (the warehouse task spine — 4/10 done, 42.2.1-42.2.4 already closed by the prior session). Closed the remaining six items, 42.2.5-42.2.10. Full item-by-item detail in `micro_checklist.md` Stage 42.

**Stage 45 round 1 — UI/UX design-bar sweep: the render race, the clipped tab bars, and a mid-session misdiagnosis corrected** *(frontend only)*

User reported 8 issues live against `:8080` deployment screenshots (duplicate panels across three Financial Accounting screens, F&A frozen header, help icon styling, a Wholeops rebrand ask, Setup submenu visuals, PIM tab bar clipping at 90% zoom, a setup banner that shouldn't be dismissible), then asked for a broader "no one has ever seen it, brainless-intuitive" design-bar sweep and to fix everything found. Full item-by-item detail in `micro_checklist.md` Stage 45.

**Stage 42.1 CLOSED — outbound lottable validation, UOM conversion, real Code 128, and two bugs live verification caught** *(code + schema + docs)*

Asked to build through the whole open backlog. Picked up Phase 42.1 where the prior session left it (42.1.1-42.1.9 already closed) and finished the remaining three items — 42.1.7, 42.1.10, 42.1.11 — closing the phase at 11/11. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-17

**Stage 39.11/39.12/39.16/39.17 — the Knowledge Center gets its content, 6 articles → 23** *(content + code)*

Asked to start writing Stage 39's content, which was 11 open items described as "mostly content authoring". Four of them are now closed. Full item-by-item detail in `micro_checklist.md` Stage 39.

## 2026-08-16

**Stage 37.1.3-37.1.5 — multi-currency finished, and the 98.8% receivable understatement it uncovered** *(code + schema + docs)*

Asked to start building from the open parity backlog, whose first row is **Stage 37 — ERP core depth**, annotated "incl. finishing multi-currency (37.1.3-37.1.5)". That was the right place to start for a reason beyond ordering: 37.1 was the only half-built item in the list, and half-built multi-currency is not a missing feature, it is a live correctness liability. Full item-by-item detail in `micro_checklist.md` Stage 37.1. A concurrent session's Stage 42.1 work was uncommitted in this tree throughout — see the note at the end.

**Stage 42.1 — the traceability foundation: batch, FEFO and recall** *(code + schema + docs)*

Asked to start building Stage 42 (WMS parity), which had been planning-only since 2026-08-11. The plan is unusually emphatic about where to begin — Phase 42.1, "start here, nothing else first" — because grep-verified there was **no lot/batch concept anywhere in the tree**: no `batch_no`, no `expiry`, no shelf life in `engines/` or `db/`. That absence is what made FEFO, expiry blocking and recall unbuildable, and it is why this WMS could not be sold into food, pharma, cosmetics or electronics. Six of the phase's eleven items are now closed (42.1.1–42.1.6) plus the recall traceability the plan asked for as proof the model is right. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-15

**Stage 36.2 — the PIM task & workflow engine, the biggest PIM gap** *(code + schema + docs)*

Asked to start building the open parity work, top of the list being **36 — PIM parity**, whose first named item is the task/workflow engine. It was also the thing 36.1.3 explicitly deferred ("task assignment: it follows 36.2's task engine, which does not exist yet"), so it is the item that unblocks the rest of Stage 36. Full item-by-item detail in `micro_checklist.md` Stage 36.2. A concurrent session held Stage 35.4 in the same tree throughout — see the note at the end.

**Stage 35.4 — the outbound document chain, and the pack object that made it possible** *(code + schema)*

Asked to start building Stages 35/36/37. That span is ~30-45 sessions by the parity plan's own sizing, so it was worked in the plan's dependency order rather than scaffolded shallowly across three stages: R1 was already done, and **R2's head item, 35.4**, is what this section covers. (A concurrent session picked up 36.2 in the same window — see the note at the end.)

## 2026-08-14

**Stage 44 — per-tenant hostnames, and the domain that unblocked go-live** *(code + schema + proxy config + docs)*

User registered **wholeops.in** and asked to close the domain-gated items. Two things came out of it: the go-live path was unblocked, and the URL scheme they described turned out not to exist.

## 2026-08-13

**The dev cluster converted to UTF-8, closing the 20.6 defect** *(database + one test + docs)*

User asked for the three defects §96 recorded to be fixed. Two were already fixed in the tree and were re-verified rather than re-asserted: the SPA's relative asset paths (no relative `src`/`href` remains in `index.html`, no relative `fetch` in `app.js`) and the OpenAPI dynamic-schema trap (the paged envelope is inlined per endpoint, `PublicPage` is not a cached component). The third was real and outstanding.

## 2026-08-12

**Stages 35.3.7 / 36 / 37.1.2 / 38 / 39 — the platform layer: public API, Knowledge Center, multi-currency** *(code + schema + docs)*

User handed over a ranked gap list and an explicit build order, deduplicated across the two: finish 36.1.3 and 36.7.2, build 38.3/38.5/38.9 together, open the first read-only 38.1 routes, build 38.8, land 39.2-39.7 as one safe unit, then 37.1.2 — plus 35.3.7, the correctness gap they ranked first. All eight built. Item-by-item detail in `micro_checklist.md`.

**Stage 43 — Go-live blocker sweep: the buildable half of the parked items** *(code + docs)*

User handed back eight parked items from `go_live_decisions.md` — escalation contacts and the alert webhook, connector/GSP/e-way/payment-terminal credentials, Cloudflare zone access, a pen-test vendor, UAT participants and hypercare sign-off, real QZ Tray printer verification, the competitor-scraping legal call, and the data mart — with "build from this which is possible… skip which needs decision for now."

## 2026-08-11

**Stage 38/39 foundations — scoped API identities and safe Markdown rendering** *(code + one unapplied migration + docs)*

User asked to start Platform & Extensibility and the Knowledge Center while the shared tree still contained concurrent Stage 35-42 work. Both starts were therefore taken at dependency foundations that add no live surface: external credentials before public routes, and a renderer before generated static help.

**Stage 35 release train R1 — the OMS becomes a real product** *(code + schema + docs)*

User asked to work the open plan phase by phase. The open plan is the Parity Master Plan's Stages 35-39, and its own sequencing puts release train **R1 = 35.1 + 35.2 + 35.3** first, because those three items exist to clear the three structural debts everything downstream assumes are gone. All three are now closed. Full item-by-item detail in `micro_checklist.md` Stage 35.

**Stage 36.1 / 37.1 foundations — Product Groups and effective-dated currency rates** *(code + schema + docs)*

User explicitly asked to start both Stage 36 and Stage 37 while protecting the ongoing session and partially completed work. The implementation therefore begins at each stage's dependency foundation, is additive, avoids `public/app.js`, and leaves both new migrations unapplied. This request also resolves parity-plan decisions D6 and D7: multi-currency, multi-entity, Projects and Service are real build scope, to be taken in plan order rather than skipped as speculative.

**Stage 42 — WMS parity plan against SAP EWM and Infor WMS** *(planning only, no code)*

User request: review the WMS module of SAP EWM and Infor WMS thoroughly, prepare a plan, update the checklist. Both benchmark URLs were supplied. Output is `docs/specs/wms_parity_plan.md` (the source of detail) plus the Stage 42 tracker skeleton in `micro_checklist.md` — the same two-file relationship Stages 35-39 have to `parity_master_plan.md`. **No source, schema or config was touched.**

**Tracker reconciliation — 23.11 and 26.1.3** *(docs only)*

Two checklist entries mixed completed engineering decisions with nonexistent or externally gated work. **23.11 is now closed as not applicable, not implemented**: the catalog's one-row inline-grid style has no paste-into-grid product surface or roadmap item, so leaving it unchecked permanently misreported an already-settled scope decision as unfinished work. Stage 23 is fully closed and moved to the closed-stage archive.

## 2026-08-10

**Stage 41 — user-reported batch: schema editing, sticky headers, country-aware phone data, setup guidance, POS location** *(code + schema + docs)*

Five gaps reported directly by the user. **Numbered Stage 41, not 34.4** — 34.4 is the gated JSON-endpoint harvester in Stage 34, blocked behind 34.6's legal/ToS decision, so reusing that number would have read as the gate being crossed. Same reasoning §88 gives for Stage 40 not being 35. Full item detail in `micro_checklist.md` Stage 41.

**Stage 40 — user-reported batch: PO line items, field formats, the Super Admin rename, speed and motion** *(code + schema + docs)*

Six gaps reported directly by the user in one pass, built in the priority order they chose. **Numbered Stage 40, not 35** — Stages 35-39 are already allocated to the Parity Master Plan (§86), and 35.1/35.5/35.6 there mean entirely different things.

## 2026-08-09

**Stage 26.6.11 — tax-exempt / nil-rated / zero-rated goods** *(code + schema + docs)*

The last substantively-buildable item in the live checklist, and the only one still gated on a product decision. Stage 30.1.2 had made `hsn_code` **and a positive `gst_rate`** mandatory on Item — correctly, since checkout and PO creation both rejected an item lacking them, so "saveable at 0%" only ever meant "unusable later". The side effect was that genuinely untaxed goods (unbranded grain, fresh produce, salt, books, exports) became unsaveable, so a tenant selling ordinary exempt stock could not create the Item at all.

**Parity Master Plan — Stages 35-39 scoped against Unicommerce, Unbxd, SAP and Dynamics 365** *(docs, planning only)*

The user named four external benchmarks and asked for one consolidated plan to reach that level, plus a knowledge center at the end. All four were read directly — the Unicommerce OMS product page and the Uniware documentation portal (sale-order model, WMS API index, client-integration structure), the Unbxd PIM help centre's full 49-section information architecture, SAP Help/S-4HANA Cloud's line-of-business scope, and the Dynamics 365 product surface incl. Business Central — then reconciled item-by-item against real code rather than against the trackers' own claims.

## 2026-08-07

**Stage 34.1-34.3 built, the first production DR drill, and nightly backups finally switched on** *(code + schema + ops + docs)*

The user asked to clear everything actionable. That was two things: Stage 34's buildable half (34.1-34.3), and 26.11.3's restore drill. Both landed; both turned up something the checklist did not know. The drill's finding — that production had no nightly backup — was then signed off and fixed the same day (26.11.7). Item detail in `micro_checklist.md` (34.1-34.3, 26.11.3, 26.11.7).

## 2026-08-06

**Production formalisation, content assist, and go-live readiness docs** *(code + schema + ops + docs)*

The user asked to start building the whole blocked backlog, decisions first. Four decisions were taken up front (hosting: formalise the droplet; content assist: local/offline only; P2 bundles: two greenlit; credentials: all parked) and three more in a second round (no domain yet, deploy 33.2 + commit the loose docs, draft all three go-live docs). Item detail in `micro_checklist.md` (26.1.1, 26.1.3, 26.4.11, 26.10.6, 26.11.1/3/5/6, 33.2).

**Archive reconciliation + Stage 34 market-intelligence plan** *(docs)*

A planning pass, no code. Cross-referenced `docs/archive/micro_checklist_closed_stages.md` against the live tracker and turned §81's retained knowledge into a buildable Stage. Item detail in `micro_checklist.md`'s new header block, 26.6.11 and Stage 34.

## 2026-08-05

**Clearing the decision-gated backlog: MFA recovery, backups, supplier portal, smoothness** *(code + schema + docs)*

Review of open item 32.4 concluded it was a real bug already fixed by `0bee663` and already deployed — so the only genuinely unblocked item was closed before this session started. The user then approved, in one pass, every remaining item that was gated purely on a *decision* rather than on something only they could supply (credentials, hosting choice, a pen-test engagement, real UAT users, a physical printer). Five items, built and verified in sequence. Full per-item mechanics in `micro_checklist.md` (32.2, 32.3, 32.5, 26.1.6, 26.4.10).

**Retiring the OmniCore/"Buying Catalog" project, keeping its market-intelligence knowledge** *(docs)*

A standalone Python microservices stack (`Antigravity Projects/Buying Catalog/OmniCore` — FastAPI + Streamlit + SQLAlchemy/SQLite + Redis/RQ + docker-compose, services `crawler`/`pims`/`oms`/`erp`/`pos`, last touched 2025-12-27) was read in full, its knowledge extracted to `docs/specs/market_intelligence_reference.md`, and the folder deleted. Same disposition and the same reasoning as the standalone WMS project in §(1c1c050)/`docs/specs/wms_master_blueprint_reference.md`.

**Stage 31.1.9 — One-click print wired into the last three screens** *(code)*

Stage 31.1 built the QZ Tray bridge and wired stickers plus the marketplace-document file picker; 31.1.9 was the open remainder — a Print Label button on the logistics booking, the POS receipt over ESC-POS, and invoice printing. Item-by-item mechanics in micro_checklist.md 31.1.9.

## 2026-08-04

**Stage 33 — Dialog viewport fit & responsive form layout** *(code)*

User report with a screenshot of the New Location dialog, its header cut off at the top of the frame and no Save button anywhere: *"Nothing is visible here. So, set up. Make it more 2 column or 3 if required so it will be visible. All screen and config it should auto adjust with 100% to any percent 40% or whatever. If 120% also it should get auto adjusted."* Item-by-item mechanics in micro_checklist.md Stage 33.

**Stage 32.1 — Interaction responsiveness: the sidebar hover defect** *(code)*

User report after using the deployed app: *"The interface is not at all smooth. I have to click many times. Then it is working. Hover is also not working. I have to click on arrow then it is showing permanently else it is going away."* Item-by-item mechanics in micro_checklist.md Stage 32.1.

## 2026-08-03

**Stage 31.1 — QZ Tray silent printing** *(code + schema + docs)*

User request, framed from marketplace operations: *"For Myntra and other Marketplaces OMS, I want to crack QZ print automation"*, with Myntra support's own setup mail attached, and the clarification that it should be **one-click print for all** — from the ERP as well as for the marketplace documents the channels supply. Three scope questions were put to the user first (what it drives, how QZ is authorised per PC, and what the label hardware is); the answers were one-click across both, self-signed/fully silent, and support all printer types. Item-by-item mechanics in micro_checklist.md Stage 31.1.

## 2026-08-02

**Stage 30.5.5 — Retiring `Stores`, folding its fields into `Location`** *(code + schema + docs)*

The last open item in Stage 30, and the only one that was blocked on a product decision rather than on work. `Stores` was promoted in the sidebar and documented as "your shop/warehouse locations", but it had **zero Link references and zero Go references** — nothing in the system could select one. A user following the manual created their shop somewhere no transaction could see it.

**Stage 30.5.8 — The picker consistency sweep** *(code)*

The audit item read as four unrelated chores: three screens missing a typeahead their siblings had, a 103-entry Location list with no structure, Employee picked with a `<select>` on five screens and a typeahead on a sixth, and icon-only buttons with no accessible name. They shared one cause — **there was no single place to be consistent in**. All 42 picker call sites configured `attachTypeahead` themselves, so "how a Location is chosen" was a decision made fifteen separate times, which is precisely how three screens came to be missing it without anyone noticing.

## 2026-08-01

**Stage 30.5.11 — Retiring the Dashboard landing screen** *(code + docs)*

User's call, made looking at the screen: *"Dashboard not required. Everything it is showing from config."* It was accurate — the screen held four derived stat cards (registered record types, audit-log count, active tenant, a hardcoded "Operational" pill) over nine shortcut tiles into **Database Schema Design, Dynamic Labels, Prefix Configs, Extension Hooks, Activity Log, Configuration, System Status, Tenant Entitlements** and **Tenant Usage**, every one of which the Settings module already lists. It owned no business data; it was a second front door to configuration.

**Stage 30.3/30.4/30.5 + the 29.7/29.8 strays — the manual overhaul and the UX sweep** *(code + docs)*

User request: finish the whole remaining Stage 30 manual/UX backlog in one pass, plus three items the previous session had flagged as *open but mis-filed inside the closed-stage archive*. Three genuine product decisions were taken with the user up front so the rest could run end to end without stopping: Trial Balance scoping (**mandatory as-of date**), the screenshot workflow (**scripted Playwright captures**), and the two flagged status transitions (**allow both, reason-code required**). Full item-by-item detail in **[micro_checklist.md](micro_checklist.md)** Stage 29.7/29.8 follow-ups and Stage 30.

**Stage 30.8 — The project brain map** *(tooling + docs)*

User request: a graphify-driven diagram of the entire project, in its own folder under `docs/`, that "may behave like a brain", is appended to as the system grows, and is easy to update. Built as `docs/brain/` — `BRAIN.md` (7 Mermaid diagrams + a card per region), `brain.html` (interactive, self-contained), `brain.map.json` (the only hand-edited file) — generated by a new stdlib-only `cmd/brainmap`. Item-by-item mechanics in micro_checklist.md Stage 30.8.

**Stage 30.7 — Config sweep ("nothing hardcoded") + POS offer engine** *(code)*

Two requests the user framed as separate and which were built as such: every module's operational config surfaced on the Configuration page and *properly wired* ("if I change it should immediately change everywhere"), and offer configuration for POS, set up in the ERP and reflected at the till. Two scope questions were put to the user first — which offer families to support (answer: all four) and what to do about platform safety limits (answer: expose them, but do it properly) — and the build followed those answers. Item-by-item mechanics in micro_checklist.md Stage 30.7.

**Stage 30.6 — Server-issued document numbers** *(code)*

User request, made from the Purchase Orders screen: *"PO number will be auto created. I will create po sequence. Same of GRN and all other transaction."* Two scope questions were put to the user before building (how wide, and what shape the numbers take); both were answered, and the work followed those answers. Item-by-item mechanics in micro_checklist.md Stage 30.6.

## 2026-07-31

**Stage 30.1/30.2 — the layman audit's P0 block, closed** *(code)*

User asked to start building open checklist items, explicitly not phase by phase. Took the whole P0 block of Stage 30 (the 2026-07-30 layman audit, `docs/UX_MANUAL_AUDIT.md`) — seven items — plus two backend items from 30.5 that fell out of the same work. Item-by-item mechanics in micro_checklist.md Stage 30.

## 2026-07-29

**Stage 29.9 — Final SaaS module/menu naming pass** *(code + docs)*

User asked, ahead of a SaaS relaunch, whether reusing standard ERP nomenclature carries trademark/replication risk — confirmed no (generic functional terms like "Sales Order"/"Chart of Accounts" are industry-standard, not vendor IP; this repo's own Stage 21.11 ERPNext-style renames are the same category of naming). User then picked final names from a SAP/Microsoft/Oracle/NetSuite/Infor/Odoo/ERPNext comparison and asked for them applied everywhere. Item-by-item mechanics in micro_checklist.md Stage 29.9.

**Stage 29.8 — the last two loophole items closed: status-transition map + JWT session staleness** *(code)*

Both remaining items in `ERP_LOOPHOLES_ANALYSIS.md` were blocked on a decision rather than on work — one marked `[needs design decision]`, the other deferred by standing policy. The user asked to be asked; they chose opt-in-strict enforcement scoped to transactional doctypes **and** masters, a middleware live-state re-check over a jti denylist, and multi-key signing rotation. Item mechanics in micro_checklist.md Stage 29.8.

**Stage 29.7 — QC exhaustive-report follow-ups: gl_postings reporting index + production sslmode docs** *(code + docs)*

User asked for the two non-blocking observations in `QC_EXHAUSTIVE_REPORT.md` (O1 index, O2 sslmode) to be fixed, plus any other open items. Item-by-item mechanics in micro_checklist.md Stage 29.7.

## 2026-07-28

**Stage 28.5 — UI defect batch: dialog legibility, flyout reliability, global search** *(code)*

Four defects reported by the user in one session, all fixed in `public/app.js` + `public/styles.css` — no Go, no schema change, no new dependency. Item-by-item mechanics in micro_checklist.md Stage 28.5.

## 2026-07-27

**Stage 29 — OMS operations follow-up** *(code)*

User explicitly authorized the previously identified OMS follow-ups. Added a unified Order Management workbench over the existing generic document API; no duplicate reporting endpoint or table was introduced. Shopify and Unicommerce intake now normalize into `CreateSalesOrder`, preserving their legacy mapping tables while sharing the SalesOrder engine's SKU mapping, validation, allocation, reservation, hold, and idempotency behavior. Shipment handover now creates a single idempotent Draft SalesInvoice only after all fulfillment tasks ship, and tracking delivery dispatches the existing `Order Delivered` notification; invoice posting/settlement remain explicit finance actions to avoid implicit GL writes. Focused and full serialized tests pass; the first full run exposed a transient existing WMS-slotting fixture flake, whose isolated and subsequent full reruns passed. Stage 23.11 was reviewed again and correctly remains a no-op because no paste-grid product surface exists.

**Stage 28 — Configurability, Theming & Deployment** *(code, UNCOMMITTED as of this note)*

User request (one batch): every operational config should live in the admin UI, module by module — "nothing hardcoded"; a dark/light/system theme toggle; user-controllable report columns saved as profiles (one Universal/shared, one Personal); and Caddy as the reverse proxy for automatic Let's Encrypt TLS. Approved plan built all four in sequence, verifying + checkpointing after each. Full per-item mechanics in micro_checklist.md Stage 28.

