# Parity Master Plan — OMS, PIM, ERP and the Knowledge Center

> **⚠️ STATUS UPDATE 2026-08-11: release train R1 (35.1, 35.2, 35.3) is BUILT — see `micro_checklist.md` Stage 35 and `project_ledger.md` §93.**
>
> **And §1.2 below was partly wrong when written. Verify every item against the source before building it.** Debt #1 claimed channel orders still land as `POSCart`; in fact `engines/channel_orders.go`'s `ImportChannelSalesOrder` had routed Shopify and Unicommerce onto `CreateSalesOrder` since commit `efed245` (2026-07-27). Debt in 35.1.4 claimed `DispatchNotification` was unwired; it was already called from `evaluateOrderShipmentClosure` and `RecordDeliveryEvent`. What *was* broken was worse and different: the legacy importers created **no document at all** (not a POSCart), the BigCommerce webhook discarded every order it received, and the Magento poller only counted them. This plan was reconciled against the checklist rather than against the code, so treat its "have"/"gap" claims as a starting hypothesis, not as fact.
>
> **Status: plan only, no code written.** Drafted 2026-08-09 from four external benchmarks
> (Unicommerce OMS + Uniware docs, Unbxd PIM help centre, SAP Help / S/4HANA Cloud, Microsoft
> Dynamics 365 + Business Central) reconciled line-by-line against this repo's real state as of
> commit `a410c42`. Every "have" claim below points at real code; every "gap" is something that
> genuinely does not exist yet, not something merely undocumented.
>
> This document is the **source of detail** for Stages 35–39 in
> [`docs/micro_checklist.md`](../micro_checklist.md), the same relationship
> [`oms_master_blueprint_reference.md`](oms_master_blueprint_reference.md) has to Stage 26.12.

---

## 0. Executive summary — what "this level" honestly means

The four benchmarks are not one target. They are three different products plus a documentation
standard, and only two of them are realistically matchable by this codebase.

| Benchmark | What it really is | Honest target for this repo |
|---|---|---|
| **Unicommerce OMS / Uniware** | Multichannel order+warehouse ops, 151 channel connectors, 290+ integrations, an API-first console | **Full functional parity is achievable.** The engines are largely built; what is missing is the *console*, the *channel breadth*, and the *order→invoice→settlement* chain. This is the highest-value pillar. |
| **Unbxd PIM** | Catalogue enrichment, workflow/task automation, syndication, DAM, ~20 AI enrichment apps | **Functional parity achievable except the AI app store.** We have the data model and the approval spine; we lack product groups, a task/workflow engine, catalogs, and the import/export depth (SFTP, scheduled, templated). |
| **SAP S/4HANA** | 60+ modules, decades of statutory localisation, thousands of person-years | **Parity is not a coherent goal and should not be attempted.** Chasing it would violate this repo's first principle. The useful read of SAP is as a *checklist of finance/controlling concepts we are missing* — see §4. |
| **Dynamics 365 / Business Central** | The realistic ERP peer: SMB/mid-market ERP with finance, SCM, projects, service, manufacturing, warehouse, BI | **This is the ERP benchmark to actually aim at.** Business Central is the right shape and the right size. §4 is scoped against BC, cross-checked against S/4HANA for concept coverage. |
| **Knowledge Center** | `documentation.unicommerce.com` + `help.pim.unbxd.com` | **Achievable and cheap relative to its value**, because three of its sections are already machine-generated (`cmd/gendocs`). See §6. |

**Bottom line:** the plan is five stages, ~118 items, sized at **≈52–66 build sessions ≈ 3–4 months**
at this repo's demonstrated cadence. It is sequenced so that pilot-ready OMS lands first, because
that is where the codebase is closest to the benchmark and furthest from being *usable*.

---

## 1. Where we actually stand (grounded, 2026-08-09)

Measured, not estimated:

- **162** Go files in `engines/`, **257** registered HTTP routes, **80** seeded doctypes, 9 guide docs.
- **Stage 26 sprints 26.4 (PIM), 26.5 (WMS), 26.6 (Finance), 26.7 (CRM), 26.9 (Manufacturing), 26.10 (Reports), 26.12 (OMS) are closed.**
- Everything still open in `micro_checklist.md` is **gated, not unbuilt**: credentials (20.3, 26.2.1–26.2.5), vendor engagements (26.11.1), real business users (26.11.5), a public hostname plus Cloudflare-zone access (26.1.3b), a legal decision (34.6), or a live printer (31.1.8).

That matters for how to read this plan: **the backlog is not "finish what's started". It is "the next level", and it is new scope.**

### 1.1 What is genuinely strong already

Worth naming, because the plan builds on these rather than around them:

- **DocType meta-engine** — dynamic doctypes/fields/validation with a generic CRUD choke point (`handleGenericDoc`). This is the single biggest force multiplier in the repo; several benchmark features below cost almost nothing because of it.
- **Maker-checker approval engine** — reused by PIM content, supplier submissions, refunds, payments.
- **Report framework** (`RegisterReport`/`ReportDefinition`) — a new report is a registered function, not a new endpoint. 20+ reports already registered.
- **Error catalog** — 300+ coded messages, generated into `error_catalog_generated.go` and into `ERROR_CODES.md`.
- **Multi-tenant SaaS spine** — schema-per-tenant, module entitlements (`engines/modules.go`), product packages, tenant limits.
- **Shared choke points** — `computeATS`, `ValidateDocument`, `writeAPIError`, `apiMiddleware`. New work attaches here rather than sweeping call sites.

### 1.2 The three structural debts that block everything downstream

These are not new features. They are existing decisions that have to be undone before parity work
is worth doing, and they are therefore the first items in the plan.

1. **Channel orders still land as `POSCart`, not `SalesOrder`.** Stage 26.12.1 deliberately left
   `ImportChannelOrder` and the Shopify/BigCommerce/Unicommerce webhooks unrewired. The whole OMS
   engine layer — allocation, hold, split, returns, courier cascade — therefore sits *beside* the
   real order flow rather than under it. Nothing else in Stage 35 matters until this is fixed.
2. **There is no OMS console.** 26.12.1 shipped the Order Engine with "no dedicated frontend screen
   yet"; 26.12.2's exception queue is "the hold-reason-filtered list view". Unicommerce's entire
   product proposition is the dashboard — pending / SLA-breached / unverified / failed. We have the
   data and none of the screen.
3. **The order → shipment → invoice chain is not wired for `SalesOrder`.** 26.12.3 explicitly
   deferred invoice generation from a completed pack task. `engines/sales_invoice.go` and
   `engines/order_invoice.go` exist but are not fed by the OMS path.

---

## 2. Stage 35 — OMS parity (Unicommerce / Uniware level)

**Benchmark decomposition.** From the product page and the Uniware API docs, Unicommerce's OMS is
seven things: channel breadth, an order console, an order-mutation API surface (hold/unhold at order
*and* item level, edit, switch facility, set priority, split), a shipping-package/invoice/manifest/
gate-pass document chain, courier integration with real AWBs and labels, returns incl. reverse
pickup and exchange, and settlement reconciliation (UniReco).

**Size: ≈16–20 sessions.** This is the pillar to do first.

### 35.1 — Order truth: retire the POSCart channel path *(L, 2–3 sessions, blocks everything)*
- **35.1.1** Rewire `ImportChannelOrder` (`engines/channel_orders.go`) to create `SalesOrder`/`SalesOrderLine` via `CreateSalesOrder`, keeping idempotency on `(channel, channel_order_id)` which already exists.
- **35.1.2** Rewire the Shopify / BigCommerce / Magento / Unicommerce webhook handlers (`engines/connector_*.go`) onto the same path.
- **35.1.3** Backfill/adapter decision for existing `POSCart` channel rows — recommend a read-only view rather than a data migration; POSCart stays the *in-store* cart, `SalesOrder` becomes the only channel-order truth.
- **35.1.4** Wire `DispatchNotification` into `HandoverManifest`/`RecordDeliveryEvent` — the two trigger points 26.12.10 deliberately skipped because of a concurrent session.
- **35.1.5** Regression gate: the full existing OMS test suite must pass against orders created through the *channel* path, not just the direct API.

### 35.2 — The OMS Console *(L, 3–4 sessions, the visible product)*
Mirrors Unicommerce's "unified dashboard": pending orders, SLA-breached orders, unverified & failed orders.
- **35.2.1** Order list screen — cross-channel, faceted (channel / status / hold reason / location / date / SLA), with saved views. Reuse the `.table-panel` vocabulary; no new table implementation.
- **35.2.2** Order detail screen — lines with per-line status, allocation plan, reservations, fulfillment tasks, shipments, returns, refunds, notification log, and the full audit trail on one page.
- **35.2.3** Action bar wired to the existing engines: hold / release / cancel / reallocate / prioritise, each already reason-code gated.
- **35.2.4** Four live tiles driven by the existing reports (`oms-exception-queue`, `sla-breach`, `allocation-pending`, `oms-reconciliation-variance`) — registered reports, not a new widget mechanism.
- **35.2.5** Bulk actions from the list (bulk hold/release/cancel/allocate) via the existing bulk-edit-bar and `BulkDecideApproval` looping precedent.
- **35.2.6** Global order search (channel order id / AWB / customer phone / SKU) — one indexed lookup endpoint, not a search engine.

### 35.3 — Order mutation surface parity *(M, 2 sessions)*
Uniware exposes all of these; we have order-level hold only.
- **35.3.1** **Item-level hold/unhold** — `SalesOrderLine.status` already supports independent per-line state (that was the reason for the doctype split).
- **35.3.2** **Order edit** — billing/shipping address, contact, custom fields, with an edit window and re-validation through the *same* `validateOrderChain` (never a bespoke resume path — the 26.12.1 precedent).
- **35.3.3** **Switch facility** for unpicked lines — re-runs `ResolveAllocationPlan` scoped to pending lines only.
- **35.3.4** **Set priority / expedite** flag, honoured by pick-list generation ordering.
- **35.3.5** **Order split** into independent fulfilments — the allocation engine already supports per-line locations; this exposes it as an explicit user action.
- **35.3.6** Every mutation gated by `StatusTransitionRule` (26.12.9) so the matrix is configurable, not hardcoded.

### 35.4 — Shipping package, invoice and gate pass *(M, 2 sessions)*
The Uniware document chain we are missing a link in.
- **35.4.1** New `ShippingPackage` entity between `FulfillmentTask` and `LogisticsBooking` — create / modify / **split** before invoicing, matching Uniware's model.
- **35.4.2** **Invoice generation from a completed pack task** — closes 26.12.3's deferred item; feeds `engines/sales_invoice.go`, with GST already computed by `engines/gst.go`.
- **35.4.3** Invoice → label → manifest ordering enforced (Uniware's hard rule: no label before invoice).
- **35.4.4** **Gate pass** for outbound movement — create / complete / update / discard / search, reusing the doctype engine.
- **35.4.5** Credit note on cancellation-after-invoice.

### 35.5 — Real courier integration *(M–L, 2–3 sessions, partly credential-gated)*
Today: placeholder AWB, plain-text label, config-table serviceability.
- **35.5.1** Courier adapter interface + the same "code-complete, credentials pending" pattern as 26.2.1–26.2.5. First adapters: **Delhivery, Shiprocket, Xpressbees, Blue Dart, Ecom Express**.
- **35.5.2** Real AWB allocation, pickup scheduling and cancellation against the adapter.
- **35.5.3** **Real shipping label** — Code128/QR symbology and a PDF, replacing `GenerateShippingLabel`'s plain text. `public/qrcode.min.js` and the QZ print path (Stage 31) already exist; this is the piece that makes them useful.
- **35.5.4** Tracking webhook ingestion (courier → us) instead of only `RecordDeliveryEvent` being called manually.
- **35.5.5** Rate-shopping / cheapest-serviceable selection across adapters, extending `CheckCourierServiceability`'s priority model.
- **35.5.6** NDR (non-delivery report) handling and re-attempt workflow — the gap between `RecordDeliveryEvent` and `RecordRTO`.

### 35.6 — Channel breadth *(L, 3–4 sessions, credential-gated per channel)*
Unicommerce ships 151 connectors. Parity on *count* is not the goal; parity on *the connectors an Indian omnichannel seller needs* is.
- **35.6.1** **Connector SDK** — formalise `engines/connector.go` into a documented interface (auth, order pull, inventory push, catalogue push, status push, error mapping) so a new channel is a file, not a project. This is the item that makes the rest cheap.
- **35.6.2** Marketplace adapters, priority order: **Amazon SP-API, Flipkart, Myntra PPMP, Meesho, Ajio, Nykaa**.
- **35.6.3** Quick-commerce adapters: **Blinkit, Zepto, Swiggy Instamart** (dark-store semantics: location = store, tight SLA, no split).
- **35.6.4** Webstore adapters: **WooCommerce** (Shopify/BigCommerce/Magento already exist).
- **35.6.5** **Inventory sync-back job** — push ATS to every connected channel on a schedule with an oversell guard; `channel_buffer` (26.12.6) exists as a column with nothing writing to it.
- **35.6.6** Per-channel SKU mapping master + unmapped-SKU exception queue (today `SKU_MAPPING_FAILED` is a hold reason with no management screen).
- **35.6.7** Connector health dashboard: last sync, lag, failure rate, per channel.

### 35.7 — Bundles, kits and virtual SKUs *(M, 1–2 sessions)*
Unicommerce: "bundle management with dynamic SKU combinations". Not present today.
- **35.7.1** `ProductBundle` definition (component SKUs + quantities + pricing mode).
- **35.7.2** Bundle ATS derived from component ATS through `computeATS` — never a second stock source.
- **35.7.3** Explosion at order creation; picking against components.
- **35.7.4** Kitting/de-kitting as a warehouse operation posting to the stock ledger.

### 35.8 — Settlement & payment reconciliation (the "UniReco" gap) *(M–L, 2–3 sessions)*
We have `bank_reconciliation.go` and `payment_file.go`; we have no *marketplace settlement* reconciliation.
- **35.8.1** Settlement/payout file ingestion per channel (CSV first, API where the adapter allows).
- **35.8.2** Three-way match: order value ↔ expected commission/fees/TDS ↔ actual payout.
- **35.8.3** Variance classification (short-payment, excess fee, missing payout, RTO not credited) with an exception queue.
- **35.8.4** GL posting of commission/fee/TDS through the existing `tds.go` and GL mapping.
- **35.8.5** Channel profitability report (net realisation per order/SKU/channel after all deductions).

### 35.9 — Returns depth *(S–M, 1 session)*
- **35.9.1** Reverse-pickup scheduling against the courier adapter (today's return flow assumes the item arrives).
- **35.9.2** **Exchange / alternate item** flow — Uniware models it; our `ReturnRequest` only refunds.
- **35.9.3** Return-to-origin vs return-to-nearest-node routing decision.

---

## 3. Stage 36 — PIM parity (Unbxd level)

**Benchmark decomposition.** Unbxd's help centre has 49 top-level sections. Stripping the ~20 AI
enrichment apps and the per-platform integration guides, the functional core is: organisation &
attribute-level permissions, imports (scheduled/SFTP/templated/hook), products & product groups,
certified products, transformation scripts, attributes & categories, **tasks**, **workflows**,
readiness reports, **catalogs**, DAM, exports (scheduled/templated/SFTP/email), and channel integration.

We have roughly 60% of that from Stage 26.4. **Size: ≈12–15 sessions.**

### 36.1 — Product groups & segmentation *(M, 1–2 sessions)*
**Started 2026-08-11.** 36.1.1 and 36.1.2 are built as an additive `PIMProductGroup`
Master with static Item rows or typed dynamic filters. A shared resolver re-evaluates dynamic groups
from current data and feeds the Report Catalog's **PIM Product Group Readiness** report. That report
completes the readiness-report portion of 36.1.3; bulk actions, task assignment and export consumers
remain open. The migration is intentionally not applied during the concurrent build session.

- **36.1.1** **Static product groups** — a saved, hand-picked set.
- **36.1.2** **Dynamic product groups** — a saved filter that re-evaluates (completeness < X, missing attribute Y, family Z). Reuse the report-definition filter vocabulary rather than inventing a query language.
- **36.1.3** Group as the unit for bulk actions, task assignment, readiness reporting and export.

### 36.2 — Task & workflow engine *(L, 3 sessions — the biggest PIM gap)*
Unbxd's Tasks (5 subsections) + Workflows (8 subsections). We have *approvals*, which are not tasks.
- **36.2.1** `PIMTask` — assignee, due date, scope (product / group / attribute set), status, comments.
- **36.2.2** **Task templates** — a reusable definition instantiated against a group.
- **36.2.3** **Workflow definition** — ordered stages, per-stage assignee role, entry/exit conditions, parallel branches. Deliberately a *declarative table-driven* engine, not a scripting runtime.
- **36.2.4** Pause / resume / cancel a running workflow, with an activity log.
- **36.2.5** Bulk workflow actions + bulk task assignment.
- **36.2.6** **Assign a task directly from a readiness report row** — the specific Unbxd affordance that makes readiness reports actionable rather than informational.
- **36.2.7** My-Work inbox screen (tasks assigned to me across products).

### 36.3 — Import depth *(M, 2 sessions)*
- **36.3.1** **Scheduled imports** with a run history — extend `scheduled_reports.go`'s scheduler rather than a second one.
- **36.3.2** **SFTP source** (stdlib-only constraint applies — if SFTP cannot be done without a dependency, ship **FTPS/HTTPS pull + a watched local drop directory** instead and say so; do not add `golang.org/x/crypto/ssh` casually).
- **36.3.3** **Import templates** — named, saved column mappings, so a recurring feed is one click.
- **36.3.4** **Import hook / API import** endpoint for push-based feeds.
- **36.3.5** Variant-aware import (parent + variant options in one file).
- **36.3.6** Import preview, row-level error report and partial-commit semantics, on top of `BulkImportCSV`.

### 36.4 — Export & syndication depth *(M, 2 sessions)*
- **36.4.1** **Custom export templates** (choose columns, order, headers, per-channel format) + a headerless mode.
- **36.4.2** **Scheduled exports** with email/webhook delivery.
- **36.4.3** Parent-product / variant-collapsed export shapes.
- **36.4.4** **Catalogs** — a shareable, tokenised, read-only real-time catalogue link (Unbxd's "Catalogs"). Reuse the supplier-portal limited-role precedent for scoping; do **not** build a second auth system.
- **36.4.5** Bulk channel download (pull a channel's current state back for diffing) — deepens 26.4.7's diff preview from "last payload we sent" toward "what the channel actually holds", for channels whose adapter supports a read.

### 36.5 — Data transformation *(M, 1–2 sessions)*
Unbxd ships a script editor. We must not ship an arbitrary code runtime in a multi-tenant server.
- **36.5.1** **Declarative transformation rules** — a table of (source attribute, operation, target attribute) with a fixed operation vocabulary (trim, case, concat, split, regex-extract, lookup-map, unit-convert, default-if-empty). Safe by construction, no sandbox needed, no dependency.
- **36.5.2** Dry-run preview against N sample products before commit.
- **36.5.3** Rule sets attachable to an import template or run as a bulk action.
- **36.5.4** **Explicitly rejected**: a JavaScript/Lua scripting engine. It is a new dependency, a sandbox-escape surface and a support burden. Documented here so the decision is not re-litigated.

### 36.6 — DAM depth *(S–M, 1 session)*
- **36.6.1** Asset transformations beyond the 26.4.4 thumbnail: **WebP conversion**, resize presets, crop. Stdlib `image` covers resize/crop; WebP encoding does not exist in stdlib — if it needs a dependency, ship AVIF-free JPEG/PNG presets and state the limit.
- **36.6.2** Bulk asset upload/download (zip in, zip out) with SKU-from-filename matching.
- **36.6.3** Asset tagging, search and filtering (metadata exists; the browse UI does not).
- **36.6.4** Asset → product auto-association by filename convention.

### 36.7 — Enrichment & quality *(M, 1–2 sessions)*
- **36.7.1** Extend `pim_content_assist.go`'s local template library to the channel-specific shapes Unbxd's apps produce (marketplace title formats, bullet points, meta description) — same deterministic, auditable, no-network design; the 26.4.11 governance answers carry over unchanged.
- **36.7.2** **Wire the Assist button into the PIM Workbench** — 26.4.11 shipped the endpoint with nothing calling it. Small, and it is the difference between built and usable.
- **36.7.3** **Related products** (Unbxd "Find Duplicates and Related Products") — we have duplicate detection; relatedness by shared family/attribute overlap.
- **36.7.4** **UPC/EAN generation and check-digit validation**.
- **36.7.5** **Catalog translation** — locale overrides already exist (26.4.1); this is the bulk workflow to populate them, translation source left as a gated decision.
- **36.7.6** Attribute-level permissions audit — Unbxd has per-attribute role permissions; verify `engines/field_permissions.go` covers PIM attributes and close the gap if not.

---

## 4. Stage 37 — ERP core depth (Business Central level, SAP-informed)

**Framing.** Business Central is the peer. SAP is used only as a concept checklist. Items are ordered
by "an SMB/mid-market customer will notice this is missing" rather than by SAP module number.

**Size: ≈16–22 sessions.** This is the longest stage and the most safely deferrable — it is depth on
things that already work, not absence.

### 37.1 — Multi-currency *(L, 2–3 sessions)*
The most commonly noticed gap for any customer with an import supplier or an export customer.
**Started 2026-08-11.** 37.1.1 is built as additive `Currency` and `ExchangeRate` Finance
Masters, with effective windows, Spot/Average/Closing types, manual/imported provenance, uniqueness
guards, an INR seed, and a shared direct/inverse resolver. The migration is intentionally not applied,
and no existing financial document or posting path changes until 37.1.2.

- **37.1.1** Currency master + exchange-rate table with effective dating and a manual/imported rate source.
- **37.1.2** Transaction currency vs functional currency on every financial document; store both.
- **37.1.3** Realised FX gain/loss on settlement; unrealised on revaluation.
- **37.1.4** Period-end revaluation run posting to a configured FX account.
- **37.1.5** Multi-currency reporting (report in any currency at a chosen rate type).

### 37.2 — Multi-entity & intercompany *(L, 2–3 sessions)*
`LegalEntity` exists as a master (Stage 17.9) but nothing transacts across entities.
- **37.2.1** Entity-scoped chart of accounts and posting.
- **37.2.2** Intercompany transactions with automatic mirrored entries.
- **37.2.3** Intercompany reconciliation report.
- **37.2.4** Consolidation with elimination entries.
- **37.2.5** Entity-level access control layered onto the existing RBAC.

### 37.3 — Costing & valuation *(L, 2–3 sessions)*
- **37.3.1** Explicit valuation-method choice per item: **standard / moving average / FIFO** (today's behaviour must be documented first, then made configurable).
- **37.3.2** **Landed cost** — allocate freight, duty, insurance and clearing across a GRN's lines by value/weight/quantity. A real gap for any importer, and one BC has.
- **37.3.3** Purchase price variance and inventory revaluation postings.
- **37.3.4** Batch/serial-level valuation where batch tracking already exists.
- **37.3.5** Cost roll-up for manufactured items through the existing BOM/MRP engine.

### 37.4 — Budgeting, forecasting & credit *(M–L, 2 sessions)*
- **37.4.1** GL budgets by account × cost centre × period, with actual-vs-budget variance reporting.
- **37.4.2** Budget checking on requisition/PO approval (soft warn / hard block, configurable).
- **37.4.3** **Cash-flow forecast** from open payables, receivables, POs and sales orders.
- **37.4.4** **Customer credit limits** with an order-hold on breach — routes through the existing hold engine, so it is a new `hold_reason`, not a new mechanism.
- **37.4.5** **Dunning / collections** — ageing-driven reminder schedule reusing the notification dispatcher.

### 37.5 — Financial statement builder *(M, 1–2 sessions)*
- **37.5.1** Statement definition layer (P&L, Balance Sheet, Cash Flow) as configurable row/column definitions over the COA, not hardcoded reports.
- **37.5.2** Comparative periods, YTD, and dimension filtering (cost centre / entity / location already exist as dimensions).
- **37.5.3** Drill-down from statement line → GL → source document.
- **37.5.4** Schedule III / Ind-AS presentation preset for India, given the existing GST/TDS localisation.

### 37.6 — Revenue, subscriptions & deferrals *(M, 1–2 sessions)*
- **37.6.1** Deferred revenue schedules with periodic recognition posting.
- **37.6.2** Prepaid expense amortisation (the mirror case).
- **37.6.3** Recurring/subscription billing runs — also directly useful to this product's own SaaS billing.
- **37.6.4** Contract/price-list versioning with effective dates.

### 37.7 — Projects & job costing *(M–L, 2 sessions)*
BC "Projects" / D365 Project Operations. Entirely absent today.
- **37.7.1** Project/job master with WBS tasks and budgets.
- **37.7.2** Time and expense capture against a project (`engines/expense.go` and `hr.go` already exist to hang this on).
- **37.7.3** Project cost/revenue posting, WIP, and percentage-of-completion.
- **37.7.4** Project profitability and resource-utilisation reports.

### 37.8 — Service management *(M, 1–2 sessions)*
BC "Service"; D365 Field Service. Absent today.
- **37.8.1** Service item / installed-base register tied to the sold serial number.
- **37.8.2** Service contracts and warranties with expiry tracking.
- **37.8.3** Service order → repair → parts consumption → invoice, reusing the existing order and inventory engines.
- **37.8.4** Field-technician scheduling (calendar assignment; no mobile app in scope).

### 37.9 — Quality & maintenance *(M, 1–2 sessions)*
SAP QM/PM concepts, scaled down.
- **37.9.1** Inspection plans and characteristics attached to a GRN or production order — formalises the ad-hoc QC already in 26.5.2 and 26.12.5.
- **37.9.2** Certificate of analysis / inspection certificate output.
- **37.9.3** Non-conformance report with disposition and CAPA link.
- **37.9.4** Preventive-maintenance schedules on fixed assets (`engines/assets.go` is financial-only today).

### 37.10 — Planning depth *(M, 1–2 sessions)*
- **37.10.1** Statistical demand forecasting (moving average / seasonal naive — stdlib maths, no ML dependency) feeding the existing MRP.
- **37.10.2** Reorder-point and min/max planning per item×location.
- **37.10.3** Supply/demand pegging view: what this PO covers, what this order is waiting on.
- **37.10.4** Capacity planning against work centres in `manufacturing_scheduling.go`.

### 37.11 — Analytics & role dashboards *(M, 1–2 sessions)*
- **37.11.1** Role-based landing dashboards (CFO / warehouse manager / category manager / CS lead) composed from registered reports.
- **37.11.2** User-savable dashboard layouts.
- **37.11.3** Scheduled dashboard/report email digests (extends `scheduled_reports.go`).
- **37.11.4** Drill-through everywhere: every dashboard number reaches its source rows.
- **37.11.5** **Deliberately deferred:** the 26.10.6 data mart / read replica. Still gated on a *measured* bottleneck.

---

## 5. Stage 38 — Platform & extensibility

**Why this is its own stage.** `documentation.unicommerce.com` is, in substance, an API portal —
their product *is* the integration surface. We have 257 routes behind session auth and no external
API story at all. Every pillar above (channel adapters, PIM feeds, ERP integrations) leans on this,
and the Knowledge Center's API reference has nothing to document without it.

**Size: ≈8–10 sessions.**

**Started 2026-08-11, credentials before exposure.** The public-v1 compatibility boundary and
allowlisted scope catalog now live in `docs/specs/public_api_v1.md`. The opaque API-key branch of
38.2 is built with per-tenant digest-only storage, expiry, issue/list/atomic rotation/revocation and
audit events. Its additive migration remains unapplied. No public business route accepts these keys
yet: per-credential limits (38.3) and idempotency for mutations (38.5) land before exposure. OAuth2
client credentials remain open if a real integrator needs token exchange rather than opaque keys.

- **38.1** **Public API v1** — a deliberately curated, versioned, stable subset of the internal routes (not all 257 exposed by default), with a documented compatibility policy.
- **38.2** **API credentials** — per-tenant API keys / OAuth2 client credentials with **scopes**, rotation, and revocation. Distinct from user sessions; must not reuse a human login.
- **38.3** **Rate limiting and quotas** per credential, layered on the existing middleware and `tenant_limits.go`.
- **38.4** **Webhook subscriptions** — outbound events as a first-class subscription model (event type, target URL, secret, HMAC signature, retry with backoff, DLQ). Extends `outbox.go` + `notifications.go` rather than a third mechanism.
- **38.5** **Idempotency keys** on every mutating public endpoint (the pattern already exists for channel-order replay; generalise it).
- **38.6** **General async job runner** — one queue with retries, backoff, DLQ and a visibility screen. Today scheduled reports, outbox retries and connector syncs each have their own timing story.
- **38.7** **Sandbox tenant** — a self-service throwaway tenant so an integrator can build without touching production data.
- **38.8** **OpenAPI generation from `routes.go`** — machine-generated, so it cannot drift. This is the input to the Knowledge Center's API reference (§6.4).
- **38.9** **Audit/observability for API traffic** — per-credential call log, error rates, latency, feeding the connector health dashboard (35.6.7).

---

## 6. Stage 39 — The Knowledge Center

**Target:** the standard set by `documentation.unicommerce.com` (structured API + integration portal)
and `help.pim.unbxd.com` (deep task-oriented help with 49 sections and hundreds of sub-articles).

**Design constraints, from this repo's first principle:** no static-site generator, no bundler, no
docs framework, no external search service. Vanilla HTML/CSS/JS served from `public/`, generated by
a Go program in `cmd/`, exactly like `cmd/gendocs` already does for three files.

**Size: ≈7–9 sessions.** Cheaper than it looks, because the generated half already exists.

### 6.1 Architecture
```
docs/kb/**/*.md          hand-written articles, YAML frontmatter
                         (title, slug, module, roles, tags, order, last_verified)
        │
        ├── cmd/genkb ──▶ %TEMP% ──▶ public/help/      static HTML, one file per article
        │                            public/help/index.json   prebuilt search index
        │
        └── generated sections (no hand-written source):
              error codes      ◀── internal/server error catalog   (exists)
              report catalog   ◀── engines report registry         (exists)
              permission matrix◀── role_permissions                (exists)
              API reference    ◀── OpenAPI from routes.go          (38.8)
              doctype/field ref◀── doctype_meta
              settings ref     ◀── settings_registry.go
              module matrix    ◀── engines/modules.go
```
The `%TEMP%`-then-copy step is mandatory on this machine — Controlled Folder Access blocks a fresh Go
binary from writing under `Documents\`, the same trap `update-brain.ps1` and `update-guides.ps1`
already work around.

### 6.2 Items — infrastructure
- **39.1** **Minimal Markdown renderer in Go** (headings, lists, tables, code, links, emphasis, images, admonitions). **Built 2026-08-11** in `internal/kb`: stdlib only, raw HTML escaped, safe URL-scheme allowlist, stable duplicate-safe heading anchors, malformed-input tests. Rejected alternative: a JS Markdown library in `public/` — it would push rendering and index-building to the client and make the search index impossible to prebuild.
- **39.2** **`cmd/genkb`** — walks `docs/kb/`, renders to `public/help/`, builds the nav tree from frontmatter, emits `index.json`.
- **39.3** **Client-side search** — a prebuilt inverted index (token → article ids + title weight) queried by ~100 lines of vanilla JS. No Lunr, no Algolia.
- **39.4** **`/help` route + shell** — sidebar nav, breadcrumb, prev/next, copy-link anchors, dark/light following the existing CSS tokens.
- **39.5** **In-app contextual help** — a `?` affordance per screen mapping screen id → article slug, opening in a side drawer built on the *existing* dialog system (not a third dialog mechanism).
- **39.6** **Access model** — authenticated by default, with a per-article `public: true` frontmatter flag for the subset that may be served unauthenticated (integration/API docs, which is exactly Unicommerce's split).
- **39.7** **`update-kb.ps1` with `-Check`** — non-zero exit when generated output is stale or an article is unreachable from the nav, mirroring `update-brain.ps1`. Wire into the build gate.
- **39.8** **Drift guards** — fail the check when an article's `last_verified` is older than N months, when a referenced screen/endpoint/error code no longer exists, or when a screen has no mapped article.
- **39.9** **"Was this helpful?" feedback** captured to a doctype, surfaced as a registered report.
- **39.10** **Release notes / changelog** section generated from the ledger's stage sections.

### 6.3 Items — content (the actual writing)
Structured after the two benchmarks. Existing `USER_GUIDE.md`, `ADMIN_GUIDE.md`, `USER_SOP.md`,
`ADMIN_SOP.md` and `QZ_PRINTING_SETUP.md` are **migrated into this tree, not duplicated beside it**.
- **39.11** **Getting Started** — first login, tenant setup, module entitlements, industry profile, first order end-to-end.
- **39.12** **Role journeys** — one guided path per role (cashier, store manager, warehouse operator, category manager, finance, admin), the affordance both benchmarks lead with.
- **39.13** **Module handbooks** — one section per module (OMS, PIM, WMS, Procurement, Finance, Manufacturing, CRM, HR, POS, Reports), each with concept → setup → daily tasks → troubleshooting, at the article granularity Unbxd uses (their Products section alone has 23 sub-articles; that is the depth bar).
- **39.14** **Integration guides** — one per connector and per courier, with credential setup, field mappings, and the error dictionary (26.4.8) inline.
- **39.15** **Admin & operations** — backups, DR drill, migrations, monitoring, hypercare, incident runbook. Pull from `docs/operations/`.
- **39.16** **FAQ + glossary + abbreviations** — Unicommerce's docs open with exactly this ("Using the Document", "Notes for the Reader", "Abbreviations", country/state/currency codes); it is genuinely useful for an API consumer.
- **39.17** **Troubleshooting index keyed by error code** — every one of the 300+ catalog codes reachable from the message the user actually saw. This is the single highest-value content item, and it is half-generated already.

---

## 7. Sequencing and release trains

Dependency-aware, not numbering order. Each train is independently shippable.

| Train | Contents | Sessions | Why here |
|---|---|---|---|
| **R1 — Make OMS real** | 35.1, 35.2, 35.3 | 7–9 | Undoes the POSCart debt and gives the product a face. Nothing else compounds until this lands. |
| **R2 — Close the document chain** | 35.4, 35.7, 35.9, 36.7.2 | 5–6 | Order→invoice→gate pass; bundles; exchanges. Small items with visible customer impact. |
| **R3 — Platform** | 38.1–38.9 | 8–10 | Pulled early *because* R4's connectors and R6's API reference both depend on it. |
| **R4 — Channels & couriers** | 35.5, 35.6, 35.8 | 8–10 | Largely credential-gated; build adapters code-complete and light them up as credentials arrive (the proven 26.2.x pattern). |
| **R5 — PIM to Unbxd level** | 36.1–36.6 | 10–13 | Independent of R1–R4; can run concurrently in a separate session on disjoint files. |
| **R6 — Knowledge Center** | 39.1–39.17 | 7–9 | Last by the user's own framing, and correctly so: it documents everything above. Start §6.2 infrastructure earlier if a session is blocked elsewhere. |
| **R7 — ERP depth** | 37.1–37.11 | 16–22 | Deliberately last. It is depth on working modules, so every item is deferrable without leaving anything broken. Pull 37.1 (multi-currency) and 37.3.2 (landed cost) forward if a real customer needs them. |

**Total ≈ 61–79 sessions.** At this repo's demonstrated cadence (Stage 26.12's 10 items across two
days; Stage 13's larger scope in about a week) that is **3–4 months** of focused work, not a year.

**Parallelisation note.** R5 (PIM) touches `engines/pim_*.go`; R1/R2/R4 touch `engines/orders.go`,
`sourcing.go`, `marketplace.go`, `returns.go`. Those sets are disjoint, so the concurrent-session
pattern that worked for 26.12.4/26.12.5 applies. R3 and R7 both touch shared middleware and GL, so
they should not run concurrently with each other.

---

## 8. Decisions needed from the user before the relevant train starts

Not blocking R1. Listed so they are not discovered mid-build.

| # | Decision | Blocks | Default if no answer |
|---|---|---|---|
| D1 | Which marketplaces matter first, and can we get seller credentials? | R4 (35.6.2/35.6.3) | Build Amazon + Flipkart adapters code-complete, credentials pending |
| D2 | Which courier accounts exist? | R4 (35.5.1) | Delhivery + Shiprocket first |
| D3 | Is SFTP worth one dependency (`golang.org/x/crypto/ssh`), or is a watched drop-directory enough? | R5 (36.3.2) | Drop directory + HTTPS pull; no dependency |
| D4 | WebP: accept a dependency, or ship JPEG/PNG presets only? | R5 (36.6.1) | JPEG/PNG only, limit documented |
| D5 | Is the Knowledge Center public (marketing/SEO value) or authenticated-only? | R6 (39.6) | Authenticated, with a public API-docs subset |
| D6 | Multi-currency and multi-entity — real near-term requirement, or speculative? | R7 (37.1/37.2) | **Decided 2026-08-11: build both**, in dependency order; 37.1.1 has started |
| D7 | Does any target customer need Projects or Service management? | R7 (37.7/37.8) | **Decided 2026-08-11: build both**, in plan order |
| D8 | Still local-only for AI content assist, or allow a bring-your-own-key provider? | R5 (36.7.1) | Local-only, per the 26.4.11 decision |

---

## 9. Explicitly out of scope (so it is not re-litigated)

- **SAP module-count parity.** 60+ modules with decades of statutory localisation. Not a goal; §4 takes the concepts, not the surface.
- **A scripting runtime for PIM transformations** (36.5.4) — declarative rules instead.
- **A headless browser or HTML-parsing dependency** for competitor scraping (Stage 34.5's existing recommendation stands).
- **A frontend framework or build step** for the console or the Knowledge Center. Vanilla, served from `http.Dir`, as today.
- **A search engine** (Elastic/OpenSearch) for either order search or KB search. Indexed Postgres lookups and a prebuilt JSON index respectively.
- **A data mart / read replica** until a bottleneck is measured (26.10.6's standing position).
- **A separate mobile app.** Responsive screens only; the QZ/scan flows already work on a handheld browser.
- **An AI enrichment app store.** 26.4.11's local-template decision holds until D8 says otherwise.

---

## 10. Source notes

External benchmarks read 2026-08-09:
[Unicommerce Multichannel OMS](https://unicommerce.com/products/multichannel-order-management-system/) ·
[Unicommerce/Uniware documentation portal](https://documentation.unicommerce.com/) (incl.
[Sale Order Management](https://documentation.unicommerce.com/docs/saleorder-overview.html),
[Warehouse Management](https://documentation.unicommerce.com/docs/wms-overview.html),
[Using the Uniware APIs](https://documentation.unicommerce.com/docs/using-the-uniware-apis.html)) ·
[Unbxd PIM help documentation](https://help.pim.unbxd.com/help-documentation/) ·
[SAP Help Portal / S/4HANA Cloud](https://help.sap.com/docs/r/product/SAP_S4HANA_CLOUD) and the
[S/4HANA Cloud Public Edition feature scope description](https://help.sap.com/doc/7c9e0bbbd1664c2581b2038a1c7ae4b3) ·
[Microsoft Dynamics 365](https://www.microsoft.com/en-us/dynamics-365/) and
[Business Central](https://www.microsoft.com/en-us/dynamics-365/products/business-central).

Repo state reconciled against: `docs/micro_checklist.md` (Stages 20, 23, 26, 31, 34),
`docs/specs/oms_master_blueprint_reference.md`, `docs/specs/erp_maturity_master_plan.md`,
`docs/specs/modules_overview.md`, `engines/` (162 files), `internal/server/routes.go` (257 routes),
`db/migration.sql` (80 doctypes), `docs/guides/` (9 documents).
