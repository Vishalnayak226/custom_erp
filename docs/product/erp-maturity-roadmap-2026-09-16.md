---
doc_id: DOC-PRODUCT-MATURITY-20260916
title: ERP module coverage, SaaS packaging and experience roadmap
type: reference
status: draft
owner: product-owner
approvers: [product-owner, engineering-owner, qa-owner, operations-owner]
audience: [product, engineering, design, implementation]
applies_to: 2026-09-16 audited source; proposed future acceptance criteria
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# ERP module coverage, SaaS packaging and experience roadmap

This is a product decision and build checklist accompanying the
[independent audit](../assurance/erp-independent-audit-2026-09-16.md).
**Existing open features are excluded from defect judgments.** “Present” below means there
is identifiable implementation and regression coverage or a rendered screen; it does not
mean complete market parity or customer acceptance. The authoritative
[capability register](capability-register.json) currently labels 12 capability groups Preview
and WMS and projects/service Experimental. Preserve those boundaries.

## Business breadth

**Build execution:** the [current build checklist](erp-build-checklist.md) turns every proposal
below into an owned work package, acceptance journey or explicit scope decision. It records
dependencies and later implementation progress without changing this dated audit baseline.
Use Stage 50 in the live TODO for priority and original Stages for their existing feature scope.

The source has 219 registered document types across 18 metadata module labels, 20 entitlement
modules, 14 product capability groups and ten package definitions. These are different
vocabularies, not interchangeable counts of finished products. The Store role-template defect
demonstrates why the relationships need one verified mapping.

| Business area | Existing implementation / observed evidence | Remaining mature-product acceptance, not a newly discovered bug |
|---|---|---|
| POS and store operations | Server-priced checkout, sessions, offers, payment state, atomic checkout tests, POS UI | Real cashier/refund journeys, hardware, offline reconciliation and country acceptance; 47.2–47.4 / 47.6 |
| Order-to-cash / OMS | Order ingestion/mutation, reservation, fulfillment, packaging, invoice, cancellation and settlement engines | Representative end-to-end channel journeys, retries and exception ownership |
| Returns and refunds | Returns engine and atomic return regressions; returns UI | Real payment-provider uncertain outcomes and reconciled stock/tax/loyalty evidence |
| Inventory | Stock availability, transfers, UOM, lot/serial tracking, counts, holds and traceability | Representative cardinality, concurrency and historical valuation reconciliation |
| WMS | Receiving, directed putaway, replenishment, waves, picking, packing, loading, yard, slotting and task engines | Physical RF/scanner/label journeys; 47.6; selected single-owner warehouse scope only |
| 3PL / warehouse billing | Owner guard, labour/VAS/storage billing and related tests | Contract/invoice reconciliation; mixed-owner allocation remains explicitly unsupported, not judged missing |
| Procurement / source-to-pay | Requisitions, RFQ, quotes, PO, GRN, vendor invoice, supplier and payment proposal paths | Supplier onboarding, approval separation and complete receipt-to-payment journey |
| PIM / catalog / DAM | Taxonomy, variants, product groups, media, content versions, imports, exports, workflows and publishing | Supplier/media recovery, large catalogs, channel validation and owned content stewardship |
| Finance / record-to-report | GL, periods, journals, AR/AP, tax, bank reconciliation, costs, budgets and reports | Qualified accountant review of balances, closing, adjustments, financial statements and audit extracts |
| Multi-currency / intercompany | Currency/FX documents and reports, intercompany engine | Consolidation policy, ownership/eliminations, currencies and jurisdiction-specific close acceptance |
| Revenue schedules | Recurring sales contracts, deferred/prepaid schedules and billing worker | Policy-specific recognition and contract amendment/cancellation acceptance; not equivalent to SaaS platform billing |
| India tax | GST/place-of-supply, TDS and related finance metadata | Real statutory provider/filing acceptance and accountable legal/tax review; no inferred legal compliance |
| Manufacturing | BOM/work order, MRP, scheduling and subcontract-related paths | Shop-floor execution, constrained planning, scrap/rework and costing acceptance on the chosen manufacturing model |
| Quality / maintenance | Quality inspections and maintenance-related implementation | Calibrations, controlled approvals, regulated validation where applicable; industry scope decision |
| CRM / loyalty | Customers, campaigns, segmentation/analytics, tiers, earning/redemption and expiry | Real consent, messaging providers, campaign economics and service handoff |
| HR / payroll | Employee, attendance/leave, payroll structures and HR UI | Country-specific statutory payroll, benefits, termination and qualified payroll-owner acceptance |
| Assets | Asset register and depreciation-related engine/UI | Acquisition-to-disposal, impairments and tax/book treatment acceptance |
| Expenses | Expense engine/UI and approval integration | Mobile receipt capture, card feeds and reimbursement provider acceptance if in scope |
| Projects and service | Project accounting and service management engines/tests | Complete quote-to-project/time-to-invoice/service SLA journeys; experimental scope remains explicit |
| Reports and analysis | Report registry, dashboards, scheduled reports, exports and finance/WMS/PIM catalogs | Reconciled metrics, lineage, dense-data usability, row/field security and report cost budgets |
| Platform / integrations | API credentials, extension hooks, webhooks, outbox, connector SDK and channel adapters | API lifecycle contracts, extension isolation, real provider certification and upgrade compatibility |
| Identity / approvals / audit | Roles, MFA/recovery, live credential state, approval engine, signed events/checkpoints and archives | Observed defects and Stages 47/49 release gates; external assurance still required |
| Knowledge / documentation | Knowledge Center, generated manuals and governance tooling | Stage 48's remaining ownership, migration and acceptance gates; no automatic closure from this audit |

These domains cover the principal ERP process families. There is no evidence-based reason
to declare the whole HR, finance, manufacturing or service module “missing.” The next question
is depth and coherent task completion, not adding another menu label. The primary-source
[business process catalog](https://learn.microsoft.com/en-us/dynamics365/guidance/business-processes/overview)
is useful for discovering process boundaries; its full vendor scope is not a mandatory backlog.

## SaaS and independently sellable modules

The existing architecture is a useful base: one Go service, PostgreSQL tenant schemas,
tenant lifecycle/hostnames, per-module entitlements, role and field permissions, tenant usage
screens, tenant export tooling and named product URLs. Normal entitlement selection matched
all ten package definitions in three rounds. That proves configuration convergence under
normal database behavior; it does not prove each product's complete customer journey.

| Package | Current additional optional modules | URL |
|---|---|---|
| PIM | pim, stickers, reports | `/pims` |
| WMS | wms, stickers, reports | `/wms` |
| OMS | oms, reports | `/oms` |
| HR | hr, reports | `/hr` |
| Procurement | procurement, rfq, reports | `/procurement` |
| Manufacturing | manufacturing, reports | `/manufacturing` |
| CRM | crm_loyalty, reports | `/crm` |
| Assets | assets, reports | `/assets` |
| Expenses | expenses, reports | `/expenses` |
| Full ERP | Twelve optional modules declared in `ProductPackages` | `/erp` |

Five entitlement modules are always on: `core`, `master_data`, `inventory`, `sales`, `finance`.
All packages share the binary, database model and frontend assets. This supports selling
entitled functionality today; it is not proof of independently deployable binaries or arbitrary
module removal. `service`, `quality` and `integrations` are optional catalog modules without
their own package, and are not in the current full-suite optional-module list. Decide the
commercial meaning of “full” and whether these need SKUs; do not silently change contracts.

Recommended build acceptance, after the reported defects are resolved:

- [ ] **PKG-01 — one module manifest:** explicit module key, capability IDs, routes, screens,
  document types, permissions, required core services, optional dependencies, workers and exports;
  generate consistency tests so metadata names such as Store cannot drift silently.
- [ ] **PKG-02 — standalone reference journey per SKU:** start from a newly provisioned tenant,
  create required masters, complete the module's main economic workflow, report/export it,
  back up/restore it and resolve an error without enabling a hidden paid dependency.
- [ ] **PKG-03 — dependency contracts:** document shared Item/Customer/Vendor/Location identities,
  posting interfaces, event versions and ownership. The current optional dependency registry
  only declares RFQ → Procurement; do not assume this is a complete business dependency model.
- [ ] **PKG-04 — package change integrity:** AUD-05 remediation, dry-run impact preview,
  explicit partial-failure/retry semantics, concurrent-change control and attributable audit history.
- [ ] **PKG-05 — downgrade without data loss:** revoke writes/access as defined, preserve required
  retention/export, drain or cancel relevant jobs, and support a verified re-enable path.
- [ ] **PKG-06 — commercial subscriptions:** decide whether external billing or a small internal
  adapter owns subscriptions, invoices, renewals, proration, grace periods and payment failures.
  Existing tenant plans/usage and customer recurring contracts are not evidence of that complete service.
- [ ] **PKG-07 — metering contract:** identify billable units, deduplication, period boundaries,
  corrections, quota behavior under database failure, tenant visibility and cost attribution.
- [ ] **PKG-08 — tenant lifecycle acceptance:** provision, bootstrap/MFA, hostname, suspend,
  resume, export, legal hold, retention and offboarding across API/UI/jobs and package combinations.
- [ ] **PKG-09 — isolation under contention:** noisy-neighbor CPU/query/queue/storage controls,
  large-tenant pagination and fairness, with measured per-tenant cost and failure containment.
- [ ] **PKG-10 — release compatibility:** additive migrations, API/event deprecation windows,
  per-package smoke fixtures and a supported rollback contract across schema versions.
- [ ] **PKG-11 — portable customer handoff:** versioned export manifests for business records,
  attachments, settings, audit evidence and ID mappings; independently verify import/reconciliation.
- [ ] **PKG-12 — deployment choice:** keep a modular monolith by default. Consider a separate
  deployment only for an actual regulatory, isolation or scaling requirement with measured cost.

## UI target: excellent daily work, not decorative complexity

The review observed coherent colors/components, helpful setup warnings and responsive page
containers. Desktop/mobile screenshots alone do not establish usability. Prioritize the
following as acceptance criteria for the existing 47.6, 47.10, 47.12, 47.14 and 47.15 work:

| Priority | Experience change | Concrete acceptance |
|---|---|---|
| First | Safe, accessible shared fields and dialogs | Every editable control has an accessible name; required/errors are announced; text remains text; focus enters and returns correctly |
| First | Clear transaction outcomes | Paid, awaiting approval, failed and retry-safe are visibly distinct; a 200 HTTP response never implies completed economics |
| First | Task-focused start pages | Role sees today's exceptions, next action and recent work; entitlement/role hides unusable destinations; main journey starts within two navigation decisions |
| Next | Predictable tables | Server pagination/filter/sort, visible result counts, sticky context where useful, keyboard-scrollable regions, stable row actions and clear bulk-selection scope |
| Next | Consistent forms | Shared labels, units, date/money precision, required/optional conventions, understandable validation and saved/draft state; no raw JSON editor as the default business workflow |
| Next | Useful empty/error/loading states | Explain why there is no data and provide a permitted next action; preserve user input after recoverable failures; keep layout stable |
| Next | Keyboard and command navigation | Discoverable search, visible focus, working Escape/Enter semantics, no hidden-modal tab stops or hover-only required controls |
| Next | Mobile/RF task shell | One clear task per view; tested touch target spacing; scanner focus maintained; network loss and resubmission show a recoverable state |
| Next | Clear support context | Compact environment/status indicator with details available on demand; readable plain-language errors with an optional support reference |
| Next | Consistent visual hierarchy | Shared spacing/type/contrast tokens; align action placement, panel density and state badges across modules; verify both themes and long content |
| Acceptance | Representative task study | Observe actual cashier, buyer, warehouse operator, accountant and tenant administrator completing tasks; record time, errors, recoveries and assistance |
| Acceptance | Accessible device matrix | WCAG 2.2 criteria selected explicitly; keyboard, screen reader, zoom/reflow, contrast, reduced motion and interruption tests on supported browsers/devices |

Do not introduce a new frontend framework merely to get these outcomes. Reuse the present
components and shared rendering choke points, and test the resulting behavior.

## Lightweight server and frontend targets

The measured Linux artifact is 18.14 MiB, and the local Windows server's working set was about
37 MiB after the sample workload. That is promising for a modular monolith. The initial JavaScript
response exceeds its own budget, and the small local load test is not a capacity certificate.

- [ ] **PERF-01:** meet the existing 120 KiB gzip initial-JS and 180 KiB cold-core budgets by
  separating the shell from lazily loaded screen code; use native browser modules or existing
  tooling, with no mandatory runtime bundler/download service.
- [ ] **PERF-02:** run the approved Linux hardware/data/tenant matrix and record p50/p95/p99,
  non-successes, CPU, resident/private memory, connections, locks, queue lag and storage growth.
- [ ] **PERF-03:** add bounded representative datasets for normal/large tenants and hot histories;
  inspect query plans and indexes before caching. Include tenant/module/role in any cache key.
- [ ] **PERF-04:** verify disabled-package workers do no unnecessary database/provider work.
  The source inventory has 30 registered workers; measure idle polling before changing topology.
- [ ] **PERF-05:** measure initial boot, warm navigation and long sessions on a constrained device
  and realistic network; report LCP/INP rather than navigation-script sleep time.
- [ ] **PERF-06:** bound imports, exports, reports and background jobs; stream large payloads,
  cancel abandoned work and demonstrate fair per-tenant concurrency.
- [ ] **PERF-07:** run a multi-hour/day soak and leak trend, then a recovery drill with load.
  Three adjacent memory samples cannot establish leak freedom.
- [ ] **PERF-08:** enforce artifact, JS, KB, dependency and storage budgets in CI using the existing
  47.9 / 47.18 / 49.18 requirements. Avoid adding Redis, a queue broker or services without evidence.

## Candidate domains requiring a scope decision

These are discovery questions, not assertions that the repository is defective. Verify existing
coverage with a named customer process before opening a build item:

| Candidate | Scope decision before building |
|---|---|
| Advanced treasury / financial consolidation | Do target customers need cash pooling, hedging, multi-GAAP consolidation or statutory group reporting beyond existing finance/intercompany paths? |
| Advanced planning / demand and supply collaboration | Is MRP and present scheduling enough, or is a constrained multi-site S&OP/APS journey required? |
| Engineering / PLM | Are engineering change orders, revision-controlled drawings and product lifecycle approvals required beyond BOM and PIM versioning? |
| Contract / enterprise records / e-signature | Is a legally accepted signing/records process required beyond existing documents, contracts, approvals and audit evidence? |
| Enterprise talent / learning / benefits | Which hire-to-retire depth is required beyond current people/payroll workflows, and for which country? |
| Field service / dealer / customer portals | Are dispatch, offline technicians, warranty claims or external self-service required beyond the experimental service/project scope? |
| EHS / sustainability / regulated industry | Only add incident, emissions, regulated quality or validation modules for a named market with an accountable subject expert |
| SaaS operator commerce | Choose the subscription/metering/billing owner and provider model described above; keep it separate from customer-business recurring invoices |

The implementation order is: repair demonstrated defects and test reliability, complete one
reference customer journey, prove standalone package and operating budgets, then add agreed
domain depth. Existing open Stages 35–39 and 47–49 retain their original scope and status.
