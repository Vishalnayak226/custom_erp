# ERP deep persona audit — 2026-09-01

**Status:** read-only product, architecture, security, usability, documentation, performance, and compliance-readiness assessment. No application code, database schema, or production configuration was changed for this audit.

**Snapshot:** tests and measurements were completed against commit `cbce34cdbf78dcfed10174864fd508de04fda0b8` (`feat: close Stage 37.4`). Development was continuing in another session, so later uncommitted finance changes are outside this snapshot. Re-run the release gates against the eventual merge commit.

**Companion plan:** [LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md](LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md)

## 1. Executive verdict

This ERP has unusually broad functional coverage for a lightweight modular monolith. Its strongest qualities are the small production stack, one Go application plus PostgreSQL, very few runtime dependencies, extensive automated tests, tenant schemas, useful configuration descriptions, and real depth in several approval, accounting, inventory, OMS, PIM, and WMS flows.

It is not yet safe to position as an enterprise-ready replacement for SAP Business One, Dynamics 365, or a specialist OMS/WMS/PIM. The limiting factor is not feature count. It is control integrity at the intersections between roles, money, stock, tenants/owners, retries, and operating scale.

The present release decision is **no-go for production handling of real payroll, financial, 3PL-owner, or high-value POS data** until the seven conditional stop-ship findings below are closed and independently retested:

1. Cashier role privacy and financial-report exposure.
2. Client-controlled POS price and cost.
3. Non-atomic checkout across inventory, loyalty, tax, and finance.
4. Replayable legacy POS return flow.
5. Missing owner isolation in 3PL allocation and picking.
6. Unusable/clipped WMS mobile workflows and field misclassification.
7. Audit-chain and retention weaknesses.

The system is light at idle and fast on small data. It is not yet “butter smooth” in the product sense because mobile layout, oversized initial JavaScript, unbounded tables, audit-log growth, screen-request bursts, setup friction, and silent documentation drift dominate the user's experience. Smoothness is a correctness property as much as a latency property: a fast response that leaks payroll, posts stock twice, or returns a generic 503 is not smooth.

### Maturity scorecard

Scores are an opinionated current-state assessment, not a certification.

| Area | Score / 5 | Current reading |
|---|---:|---|
| Core architecture and dependency discipline | 4.0 | Excellent lightweight foundation; modular monolith is the right shape. |
| Finance and inventory control integrity | 2.0 | Good accounting primitives, but critical POS/return transaction boundaries remain unsafe. |
| Identity, RBAC, privacy, and segregation of duties | 1.5 | Server-side framework exists; actual Cashier grants and unguarded routes defeat it. |
| OMS | 3.0 | Broad flows and connector implementations; live credentials, failure operations, and returns depth still need proof. |
| WMS | 2.0 | Considerable desktop depth; 3PL owner isolation and mobile execution block serious deployment. |
| PIM and master data | 2.5 | Useful product functionality; stewardship, golden-record governance, legal attributes, and staged migration need depth. |
| Self-onboarding and documentation | 2.0 | Large guides exist; they do not yet make a governed, production-ready tenant self-configuring. |
| Platform and integration maturity | 2.0 | API key/OpenAPI foundation; OAuth2, webhooks, durable job runner, and sandbox remain open. |
| Performance at current demo scale | 3.5 | APIs and idle footprint are good; browser payload and audit query shape already show limits. |
| Performance/operability at enterprise scale | 1.5 | No representative-volume proof, query telemetry, distributed coordination, or enforceable SLOs. |
| Compliance evidence and auditability | 1.5 | Some good logs and controls; no complete legal control matrix or reliable immutable audit evidence. |
| Product coherence | 2.5 | Impressive breadth, but raw internal names, stage language, gaps, and inconsistent workflows expose implementation seams. |

## 2. How the audit was performed

The assessment deliberately used several different “biased eyes” and repeated the same workflows from conflicting interests: CEO, CFO/controller, SAP architect, Microsoft Dynamics architect, OMS operator, WMS floor worker, PIM/MDM steward, cashier, normal office user, advanced user, product head, integration developer, SRE, security tester, privacy/compliance lead, and a third-party developer with a long-lived-system mindset.

Evidence labels used below:

- **Live:** reproduced through a locally running HTTP server using a real signed application role.
- **Code-proven:** control flow and persistence order are explicit in source.
- **Observed:** reproduced in browser or responsive viewport.
- **Measured:** repeatable timing, size, row-count, or query-plan result from this snapshot.
- **Documentation:** a contradiction, omission, or unreachable journey in the written material.
- **Applicability:** legal or operational requirement depends on entity, turnover, geography, deployment, or contractual scope and must be decided with qualified counsel/auditors.

Checks included a full serial Go test run, build and vet, JavaScript syntax check, generated-knowledge check, binary/static footprint, authenticated and unauthenticated browser journeys, responsive/mobile views, real role-based endpoint probes, focused source tracing, database sizing and query plans, route/document coverage, setup documentation, and official-source legal research.

This was not an external penetration test, legal opinion, financial audit, hardware certification, live connector certification, or statistically valid production load test.

## 3. Stop-ship and high-risk findings

### A-01 — Cashier can read confidential HR, system, and finance data

**Priority:** P0. **Evidence:** Live + Code-proven. **Affected claims:** privacy, least privilege, segregation of duties, server-side enforcement.

The current Cashier role has a very broad generic document grant set: 61 readable document types and write-like grants on 20. It can read sensitive types such as `Payslip`, `EmployeeLoan`, `Grievance`, `ExpenseClaim`, and `MarketplaceSettlement`. No field-permission rows narrow several of those types, so absence of field rules behaves as full field visibility. The location condition also permits location-less records, which is common for HR and finance data.

A signed Cashier request returned HTTP 200 from the trial balance, audit log, system log, sales register, vendor ledger, asset register, employee loan, expense claim, allocation strategy, and gate-pass endpoints. The system log response was about 35 KB and can contain operational error detail. These are real server responses, not hidden-menu observations.

**Why it matters:** payroll privacy, financial confidentiality, fraud reconnaissance, and internal error detail are exposed to a front-line account. UI trimming cannot compensate for reachable HTTP routes.

**Required closure:** deny-by-default route capabilities; rebuild role templates from business tasks; field-level policies for sensitive records; explicit location/entity scope; prohibit `IS NULL` scope broadening unless the role has a global capability; automated negative authorization tests for every route and sensitive field; migration that removes unsafe grants from existing tenants. Treat authorization as `(role capability) AND (entity/location/owner scope) AND (field policy)`, not a sidebar decision.

### A-02 — POS trusts the browser for sale price and cost price

**Priority:** P0. **Evidence:** Code-proven + Observed.

The POS exposes editable Sale Price and Cost Price. Checkout accepts those client values and rejects only negative numbers. Tax, offers, and journal effects use the submitted sale price. Approval checks rely on submitted discount percentage, so a cashier can send a zero or abnormally low price with `discount_pct = 0` and avoid discount approval. Cost is also visible to the cashier, leaking margin information. Where no receipt history exists, moving-average COGS can fall back to the submitted cost.

**Required closure:** resolve price list, contract/customer rules, tax-inclusive/exclusive basis, currency, cost, and promotion server-side from item/location/date/customer context. Make every override an explicit permissioned command with reason, threshold, approver, and immutable before/after evidence. Never accept cost from a POS client. Test tampered JSON, not only the screen.

### A-03 — Failed checkout can leave stock posted and can deduct it again on retry

**Priority:** P0. **Evidence:** Code-proven.

Checkout posts the inventory movement in a committed transaction, then separately redeems loyalty and posts finance/tax/reclassification entries. If a later step fails, the cart is marked failed while the inventory change remains. A failed cart can be retried. Ledger-row idempotency is checked too late to protect the already committed availability mutation, so a retry can decrement availability again while suppressing only the duplicate ledger row.

**Required closure:** one database transaction and one idempotency key for the complete financial/inventory outcome, or a formally designed reservation/saga with compensating entries and durable state transitions. Idempotency must protect the mutation, not only its evidence row. Add forced-failure tests after each boundary plus two-client concurrency and retry tests.

### A-04 — Legacy POS return is replayable and can separate stock from finance

**Priority:** P0. **Evidence:** Code-proven + Observed route wiring.

The active POS “Process Return” path uses the legacy fulfillment return endpoint. The browser submits editable sale/cost values. The engine checks original bill, quantity, and return window but does not re-resolve original transaction prices. Inventory is incremented before separate finance postings. There is no per-return idempotency key. The evidence ID is deterministic per original order, but duplicate insert failure is ignored after stock and finance work. Repeating a partial return can therefore keep replaying the same remaining quantity while the recorded evidence remains stale; concurrent attempts are worse.

**Required closure:** remove the legacy path and use one authoritative return aggregate; price/refund from immutable original lines and tender records; lock cumulative returned quantity; require idempotency key; make stock, refund liability/tender, tax reversal, and evidence atomic; handle exchanges and partial tenders explicitly. Add replay, concurrency, duplicate-click, disconnect, and finance-failure tests.

### A-05 — 3PL stock ownership is not enforced during allocation/picking

**Priority:** P0 if the product is sold or piloted as multi-client/3PL. **Evidence:** Code-proven.

Owner subledger and billing concepts exist, but allocation/picking explicitly do not filter stock by owner; sales orders do not carry the required owner dimension. Two clients using the same SKU/location can therefore compete for or consume each other's stock.

**Required closure:** make owner a mandatory inventory dimension from inbound receipt through LPN/bin/lot/serial, availability, allocation, pick, pack, ship, return, adjustment, cycle count, billing, and reconciliation. Add database constraints where possible, cross-owner negative tests, and owner-level stock-to-GL/subledger reconciliation. Until then, remove 3PL isolation claims and disallow mixed-owner warehouses.

### A-06 — Core WMS mobile workflows are not operable on a phone-sized device

**Priority:** P0 for WMS/RF deployment. **Evidence:** Observed.

At 390 × 844, the sidebar remains roughly 270 px wide and the working area collapses to about 120 px. POS, RF Receiving, Mobile Picking, and OMS content clip or overflow while body overflow is hidden. More seriously, the Wave ID field is inferred as a phone field because its identifier contains “mobile”; the formatter invokes a telephone keyboard, strips non-digits, limits input like an Indian phone number, and shows a phone hint. Alphanumeric wave IDs cannot be entered reliably.

**Required closure:** task-specific mobile shells with one primary action per step; explicit metadata for semantic input types instead of substring inference; barcode/scanner keyboard behavior; large-glove target sizing; offline/reconnect and duplicate-scan rules; physical Android RF-device testing in portrait/landscape and poor Wi-Fi. Do not call a page “mobile” because it fits in a route name.

### A-07 — The audit log is neither fully protected nor scale-ready

**Priority:** P0 for regulated/financial audit claims; otherwise P1. **Evidence:** Code-proven + Measured.

About 240,249 of 267,146 audit rows (roughly 90%) have no checksum and are skipped by verification. New checksum creation reads the previous checksum without serializing concurrent writers; two events can share a parent even though the verifier expects a single chain. The audit table is 92 MB of a 130 MB database, has no `created_at` index, and the newest-50 query required a parallel sequential scan of the entire table (about 118 ms and 10,289 buffers). There is no general archive/partition/retention policy or scheduled chain verification.

**Required closure:** decide the evidentiary model first. Options include serialized per-tenant chains, independently signed events with periodic Merkle checkpoints, or an append-only external evidence sink. Backfill or clearly seal a migration checkpoint for legacy rows; prevent direct-update bypass; schedule verification and alerting; index access patterns; partition by tenant/time only after measuring; archive compressed, immutable evidence according to a documented retention schedule. A hash alone is not immutability.

### A-08 — XSS blast radius includes 24-hour bearer tokens

**Priority:** P1, elevated to P0 for internet-exposed production until independently tested. **Evidence:** Code-proven.

Bearer tokens are kept in `localStorage`; logout removes the browser copy but there is no effective per-token revocation. The default lifetime is 24 hours. The content-security policy permits inline scripts/styles, the UI contains many inline event handlers, and a large amount of dynamic HTML construction. Any successful XSS can therefore steal a useful token and reach every route the role can reach. Live role rechecks help after account demotion but do not revoke a stolen active token.

**Required closure:** eliminate inline handlers via event delegation, encode/sanitize every insertion, introduce a nonce/hash CSP without `unsafe-inline`, move to short-lived access tokens with rotation/revocation or secure same-site HTTP-only session cookies as the deployment model permits, and commission an external application/API penetration test. OAuth2 (Stage 38.2d) does not itself repair DOM XSS or unsafe authorization.

## 4. Cross-cutting loophole register

| ID | Priority | Loophole or weakness | Evidence | Practical consequence |
|---|---|---|---|---|
| A-09 | P1 | Report and log routes lack role-capability checks | Live/code | Hidden pages remain directly callable; docs overstate server enforcement. |
| A-10 | P1 | In-process rate limiter stores request histories per key without global pruning | Code | Memory can grow with unique invalid tokens/IP fingerprints; replicas disagree. |
| A-11 | P1 | Per-tenant concurrency cap returns generic 503 during a normal burst | Measured | 30 simultaneous setup-status calls produced 22 successes and 8 `GLOBAL-0010` failures. |
| A-12 | P1 | Search applies SQL limit/offset before in-memory text filtering | Code | A valid match beyond the fetched page can disappear from search results. |
| A-13 | P1 | Current-stock report reads all availability rows and filters/renders client-side | Code | Memory, transfer, DOM work, and lock/query cost grow without a bound. |
| A-14 | P1 | Generic list sizes can reach 500 and many tables render every row | Code/observed | Long tasks become janky; keyboard/focus state is fragile; mobile is worse. |
| A-15 | P1 | Background workers live inside every application process | Code | Multi-replica deployment can duplicate dispatch/schedules unless every job is independently safe. |
| A-16 | P1 | Startup immediately drains stale outbox/test rows | Observed | A restarted environment can perform old side effects before an operator assesses it. |
| A-17 | P1 | Durable general job runner and distributed work ownership remain open (38.6) | Backlog/code | Heavy work, retries, cancellation, visibility, and replica safety are fragmented. |
| A-18 | P1 | OAuth2, webhooks, and customer sandbox remain open | Backlog | Partners lack modern delegated auth, push delivery, replay tools, and safe experimentation. |
| A-19 | P1 | Setup status counts active master rows, not readiness or correctness | Code/docs | Green counts can mask missing tax, dependency, approval, reconciliation, or legal setup. |
| A-20 | P1 | Legitimate one-person setup collides with maker-checker | Docs/behavior | The first-sale journey requires two accounts; self-impersonation defeats the control. |
| A-21 | P1 | No complete production onboarding path from legal entity to reconciled opening balances | Docs | A novice can create records but cannot confidently prove a safe go-live. |
| A-22 | P1 | Master-data stewardship lacks a complete golden-record/change-governance journey | Product/docs | Duplicates, ownership, survivorship, effective dates, and downstream blast radius are unclear. |
| A-23 | P1 | Real connector/GSP/payment-terminal/printer credentials are unverified | Backlog/ops | “Built” integrations may fail on authentication, rate limits, schema drift, or physical devices. |
| A-24 | P1 | Batch/serial backend depth lacks equivalent operator UI wiring | Code/backlog | Traceability capability exists but warehouse users cannot complete all documented transitions. |
| A-25 | P1 | Tracker cites a new Stage 42.7 item that does not exist | Documentation | A known traceability UI gap is orphaned and can be forgotten. |
| A-26 | P1 | Ten module handbooks remain incomplete; 14 observed views lack KB mapping | Documentation | Help at the point of work is inconsistent, especially for WMS and operations screens. |
| A-27 | P1 | Screenshot test reports success on HTTP failures and overlay-obscured pages | Code/observed | A visually broken or unauthorized page can produce a green evidence pack. |
| A-28 | P1 | No representative 10k/100k/1m-volume performance proof | Test gap | Current sub-10-ms APIs do not predict month-end, large inventory, or long-retention behavior. |
| A-29 | P1 | `pg_stat_statements` is not available in the tested environment | Measured | Slow-query prioritization depends on anecdotes instead of workload evidence. |
| A-30 | P1 | Legal retention, erasure, legal hold, and export rules are not one data-lifecycle policy | Docs/legal | Privacy deletion and statutory retention can conflict or be handled inconsistently. |
| A-31 | P2 | One 1 MB JavaScript file supplies almost the whole UI | Measured | Parsing/compilation and change invalidation tax every role for modules they never use. |
| A-32 | P2 | Optional `npx -y esbuild` guidance conflicts with the no-build design | Docs | Reproducibility and air-gapped installs can gain an implicit network/toolchain dependency. |
| A-33 | P2 | Reports expose internal stage identifiers and raw engineering concepts | Observed | Users see implementation status instead of business language and actions. |
| A-34 | P2 | Dashboard KPI “327 SLA breaches” lacks timeframe, owner, and action | Observed | An alarming number is not operationally useful and erodes trust. |
| A-35 | P2 | Raw UUIDs and internal type names appear in operational setup and GRN views | Observed | Novices cannot recognize records; support errors and wrong selection increase. |
| A-36 | P2 | Date display mixes `09/01/2026` and `2026-09-01` | Observed | Day/month ambiguity can affect finance and expiry decisions. |
| A-37 | P2 | Purchase-order sample state is internally confusing | Observed | “Pending Approval”, “No lines”, taxable amount, and blank grand total undermine confidence. |
| A-38 | P2 | Cost price is visible in cashier workflow | Observed/code | Commercial margin data is unnecessarily disclosed even if price tampering is fixed. |
| A-39 | P2 | Small click/tap controls and inconsistent labeling remain | Observed | Floor use, motor accessibility, and error recovery are harder than desktop happy-path tests show. |
| A-40 | P2 | Empty charts and failure/empty states are not consistently distinguished | Observed | Users cannot tell “zero business” from “data failed to load”. |
| A-41 | P2 | View-to-KB mapping and docs freshness are not semantic contract tests | Docs/code | Generated files can be current while instructions are behaviorally wrong. |
| A-42 | P2 | Error envelopes are standardized, but floor users still encounter generic codes | Observed/design | A user who only knows basic English cannot decide whether to retry, ask approval, or call support. |
| A-43 | P2 | Industry breadth can activate unfinished controls | Product/backlog | A multi-industry menu can imply maturity the underlying module has not earned. |
| A-44 | P2 | No formal extension compatibility/version policy despite extension hooks | Product/code | Partner customizations can silently break as metadata/routes evolve. |
| A-45 | P2 | No explicit supportability budget per feature/module | Product/ops | Feature breadth increases runbooks, migrations, docs, permissions, and support load faster than binary size. |

## 5. The same ERP through different eyes

### CEO / business owner

The CEO sees breadth and a low infrastructure bill, which are genuine advantages. The problem is trust. A dashboard that shows 327 SLA breaches without period, monetary impact, accountable owner, or a “take action” path creates anxiety rather than control. The CEO cannot yet answer four basic questions from the product: Is cash correct? Is stock owned by the right party? Who can see payroll? Are today’s exceptions getting smaller?

The CEO journey should be a ten-minute daily cockpit: cash and working capital, sales/margin with confidence timestamp, fulfillment risk, compliance exceptions, data-quality exceptions, and named owners. Role dashboards (37.11) should come after the underlying authorization and reconciliation controls, not before them.

### CFO / controller / statutory auditor

The finance primitives are promising: double-entry checks, approvals, audit events, budgets, cash forecasting, credit limits, and dunning now exist. But a controller will reject a system where a Cashier can fetch the trial balance, where a return can move stock without the matching financial reversal, or where most historic audit rows are outside the protected chain. Financial statement builder (37.5), deferred revenue (37.6), close orchestration, opening-balance migration, subledger-to-GL reconciliation, period locks, and auditable override reports are the maturity path.

### SAP architect

The SAP bias asks whether scope, organizational structure, configuration, master data, roles, testing, and production promotion are governed as one implementation lifecycle. The current setup presents many raw record types but little dependency-guided sequencing. SAP Central Business Configuration explicitly structures scope and organizational configuration and promotes configuration across landscapes; the lesson is not to imitate SAP’s weight, but to copy its implementation discipline. See SAP’s official [scope/org configuration overview](https://help.sap.com/docs/SAP_S4HANA_CLOUD/b249d650b15e4b3d9fc2077ee921abd0/3cb6987b7d174e38893e0d9a77ea102e.html) and [organizational units guidance](https://help.sap.com/docs/CENTRAL_BUSINESS_CONFIGURATION/55c9333eed324cd284f6c4e5dab8462f/00ad51c162c94563bfa5c69cf3dfa556.html).

The product needs a small, opinionated implementation layer: scope questionnaire, organization graph, dependencies, configuration transport/export, readiness gates, and role-template validation. It does not need SAP’s infrastructure footprint.

### Microsoft Dynamics architect

The Dynamics bias asks whether data can be staged, validated, packaged, promoted, replayed, and reconciled, and whether a user can learn a process in-product. Microsoft documents process catalogs, data entities/packages, task recording, and a deliberate test strategy. Useful patterns are the [business-process catalog](https://learn.microsoft.com/en-us/dynamics365/guidance/business-processes/about), [data packages/entities](https://learn.microsoft.com/en-us/dynamics365/fin-ops-core/dev-itpro/data-entities/data-entities-data-packages), [task recorder](https://learn.microsoft.com/en-us/dynamics365/fin-ops-core/dev-itpro/user-interface/task-recorder), and [implementation test strategy](https://learn.microsoft.com/en-us/dynamics365/guidance/implementation-guide/testing-strategy).

The current ERP has imports and guides, but not yet one migration cockpit showing staging errors, dependency order, transformation version, record counts, control totals, rejects, rerun idempotency, and sign-off. The lightweight answer is metadata and PostgreSQL staging tables, not another platform.

### OMS operator

The operator can see substantial order, fulfillment, connector, bundle, settlement, and return concepts. An enterprise OMS operator will immediately test oversell under concurrency, channel-order idempotency, partial cancel/ship/refund, stale webhook retries, connector backoff, listing conflict, bundle component availability, fee/tax settlement differences, and return disposition. Some depth exists, but the live-credential and failure-operation evidence is incomplete. The legacy POS return weakness also shows that parallel return implementations are dangerous.

### WMS manager and 3PL operator

The desktop surface is broad: receiving, LPN, replenishment, wave, picking, sortation, loading, yard, cycle counting, and conditions. The manager’s decisive tests fail: owner-safe allocation and usable RF mobile execution. The manager also needs task ageing, reason-coded exceptions, scanner/device management, printer fallback, labour productivity without surveillance abuse, and stock-to-owner billing reconciliation. Microsoft’s WMS mobile guidance is a useful interaction reference, including [mobile app configuration](https://learn.microsoft.com/en-us/dynamics365/supply-chain/warehousing/install-configure-warehouse-management-app) and [in-flow data inquiry/detours](https://learn.microsoft.com/en-us/dynamics365/supply-chain/warehousing/warehouse-app-data-inquiry).

### Warehouse floor worker who knows basic English

This user needs “Scan box”, “Put in BIN-A12”, “Wrong item”, and “Ask supervisor”—not `BinReplenishmentRule`, UUIDs, or a philosophy about LPN source of truth. They work with gloves, noise, weak Wi-Fi, damaged labels, repeated scans, interruptions, and shared devices. The current mobile layout and Wave ID phone formatting are hard blockers. Each task needs pictures/icons plus plain verbs, local-language-ready labels, audio/vibration feedback, and a visible recovery route that never requires understanding an internal record type.

### Cashier / store manager

The cashier flow is fast to reach but overpowered. Cost visibility and client-controlled price make a compromised account commercially dangerous. Returns need original receipt/tender-driven choices, cumulative quantity protection, supervisor reason codes, and explicit offline/duplicate-click handling. A store manager needs exception queues—not the ability to repair accounting manually.

### PIM manager / master-data steward

The PIM has useful product grouping, import/export, transformation, asset, and enrichment direction. The steward asks harder questions: Which system is authoritative per attribute? Who may override supplier content? Which value won survivorship and why? Are effective dates and market/language variants supported? Can a duplicate be merged without breaking orders? Which channels reject an attribute? Which packaged-product declarations are mandatory? There is not yet a complete stewardship and data-quality cockpit.

### Normal office user

The normal user can navigate many desktop pages, but sees too much internal vocabulary, inconsistent dates, ambiguous empty states, and long setup lists. They need saved views, predictable filters, human record labels, undo/recovery where safe, and contextual help tied to the exact state. Breadth makes discoverability more important than adding another screen.

### Advanced user / analyst

The advanced user wants bulk actions, keyboard efficiency, configurable views, exports with stable schemas, audit history, saved filters, cross-entity comparison, and documented API limits. The current generic framework is a good basis, but in-memory filtering, large client tables, incomplete public API surface, and no sandbox limit serious automation.

### Product head

The product currently optimizes for closing numbered stages. Customers optimize for complete outcomes. “Create PO” is not an outcome; “buy, receive, match, pay, reconcile, close, and prove it” is. The roadmap should stop measuring module breadth and start measuring reference journeys, control coverage, time-to-first-correct-transaction, exception recovery, and retained performance. Internal Stage identifiers must not appear in customer-facing UI.

### Third-party developer with a 20-year maintenance bias

The developer likes the simple stack and dislikes the transactional seams. The key risks are not Go or PostgreSQL; they are multiple implementations of the same business event, mutation idempotency applied after the mutation, generic permission semantics, process-local coordination presented as platform control, and missing compatibility contracts. The architecture should remain a modular monolith while domain commands gain explicit invariants, single ownership, atomic boundaries, versioned events, and contract tests.

### SRE / operations lead

Idle footprint is good, but operability is immature. Starting the server dispatched stale test outbox work immediately. Multiple embedded tickers need ownership rules. Rate/concurrency state is process-local. There is no representative workload history or top-query telemetry. The SRE wants release ID, tenant-safe metrics, queue depth/age, failure reasons, slow query fingerprints, backup age, restore proof, worker lease owner, and a kill switch for external side effects—without logging secrets or personal data.

### Security tester / malicious insider

The shortest profitable paths are a low-privilege token calling finance/log routes, tampered POS JSON, repeated returns, cross-owner allocation, XSS token theft, location-null scope expansion, and concurrent/retry races. This tester also probes export endpoints, document attachments, formula/CSV injection, connector secrets, report parameters, file content type, webhook signatures, password-reset enumeration, token replay, and tenant/schema confusion. These should become automated abuse cases, not one-off review notes.

### Privacy lead / legal reviewer

The privacy lead sees employee, payroll, customer, grievance, device, audit, and transaction data without one data inventory, purpose/retention map, data-subject workflow, breach evidence pack, processor register, or legal-hold mechanism. The biggest immediate privacy defect is excessive role access. The answer is not indiscriminate deletion: accounting and security records can have mandatory retention. Each field/table/event category needs purpose, lawful basis/notice mapping, residency, retention trigger, access class, export/erasure behavior, and hold override.

## 6. Industry lenses

| Industry/use | Current promise | Decisive missing proof or control |
|---|---|---|
| Single-store retail/POS | Strongest near-term fit after hardening | Server-authoritative pricing, atomic checkout/returns, device/printer/payment certification, offline policy. |
| Multi-store retail | Plausible | Entity/location roles, price/promotion governance, inter-store transfer reconciliation, retained-scale proof. |
| Wholesale/distribution | Plausible but incomplete | Credit/dunning are promising; statements, close, landed cost/valuation depth, customer-specific price controls, EDI/webhooks. |
| Marketplace/D2C OMS | Broad prototype | Live connector certification, settlement exception operations, high-volume idempotency, returns disposition/refund truth. |
| 3PL WMS | Not ready | Owner isolation across the entire stock lifecycle, mobile/RF operability, client billing/reconciliation. |
| Manufacturing | Early/intermediate | Planning depth (37.10), quality/maintenance (37.9), shop-floor usability, lot genealogy UI, costing variance, finite capacity. |
| Project/service business | Not ready as primary ERP | Projects/job costing (37.7), service management (37.8), deferred revenue (37.6), resource/time/billing controls. |
| Regulated/traceability-heavy goods | Not ready for assurance claims | Complete batch/serial UI, recall drill, immutable audit evidence, quality controls, validated retention/export. |
| India packaged consumer goods | Partial | Product declaration completeness, channel display validation, legal-metrology change control, country-of-origin evidence. |
| Multi-country enterprise | Not established | Localization, tax engines, data residency, consolidation, currency/revaluation, legal-entity controls, local compliance packs. |

## 7. Can one person set up and learn it alone?

### Short answer

**A technical evaluator can start it from the repository and create demo data. A normal business owner cannot yet independently configure a governed, legally ready, reconciled production tenant from the product and its current documentation.**

The current Admin Guide begins from a developer-style Windows workstation with specific Go/PostgreSQL paths, Git/repository access, and PowerShell. It is a useful operator/developer manual, not a customer installer or tenant onboarding wizard. The first-sale guide covers useful transactional records but not the full control foundation.

### Where the solo journey breaks

| Journey step | Current state | What a self-setup product must supply |
|---|---|---|
| Install or subscribe | Repository/operator instructions | Signed installer/container or hosted tenant, prerequisites check, secrets generation, TLS/domain, backup destination, upgrade channel. |
| Choose business scope | Module list and setup screens | Five-minute industry/size/channel/warehouse questionnaire with reversible recommended scope. |
| Define organization | Raw entity/location/master records | Visual legal-entity → registration → branch → warehouse/store → cost/profit-center graph with constraints. |
| Legal/tax/fiscal setup | Pieces exist across forms | Country-specific checklist, GST registrations, invoice series, fiscal year/periods, currency, tax tests, evidence and accountable sign-off. |
| Finance foundation | Record creation possible | Chart template, bank/cash, dimensions, opening balances, subledger reconciliation, trial balance acceptance, period-lock policy. |
| Master migration | Imports exist | Ordered templates, staging, transformations, duplicate match, referential validation, control totals, rejects, rerun, reconciliation. |
| Roles and approvals | Many grants and maker-checker | Safe role templates, conflict/SoD analysis, branch/entity scope preview, negative access test, bootstrap governance. |
| First transaction | Ten-step first-sale guide | A guided rehearsal with synthetic data and a “why blocked” explanation at every dependency. |
| Integrations/devices | Operations notes and open real credentials | Sandbox, test connection, capability/permission check, sample event, retry/replay, printer/scanner/payment certification. |
| Go-live | No single readiness contract | Evidence-based readiness score, open exceptions, backups/restores, cutover freeze, opening reconciliation, signed owner. |
| Learn at work | 24 KB articles and large guides | Exact-screen contextual help, role task paths, searchable glossary, state-aware recovery, short floor instructions. |

The maker-checker conflict deserves explicit design. The documentation says an administrator creates the first account, while the first-sale journey requires another account because self-approval is correctly refused. A legitimate one-person business should not create a fake second identity. Offer an explicit micro-business governance profile with documented risk acceptance, transaction thresholds, delayed owner review, and immutable exception reporting—or require an external accountant/implementation checker. Never silently bypass maker-checker.

### Documentation coverage and truthfulness

The main guides are substantial, and the generated KB is fresh (24 articles passed its generation check). That is a good base. However:

- The User Guide was last verified against an older product stage.
- Only a small subset of the planned module handbooks is complete.
- Fourteen observed view identifiers had no direct KB mapping: Appointment Calendar, Assets, Expenses, Extension Hook Log, Help, HR, Loading Dock, Manufacturing, Place Hold, RF Receiving, RFQ, Sortation, Warehouse Cockpit, and Yard Board.
- The UAT documentation says role-filtered UI is backed by server enforcement, contradicted by live Cashier HTTP tests.
- The screenshot collector can label unauthorized/error pages successful and can capture later pages under an open Setup overlay.
- The tracker points to missing Stage 42.7 work that is not actually tracked.

Docs must become executable product contracts: every route/view has an owner, role, help article, last verified release, happy path, negative path, and automated link/selector/auth/error check. Generation freshness alone does not prove semantic accuracy.

## 8. Performance and footprint baseline

Measurements are local development results, useful as a baseline but not a production SLO claim.

| Measure | Result | Interpretation |
|---|---:|---|
| Stripped Windows server binary | 16.59 MiB | Excellent; preserve this advantage. |
| Running server working set | ~65 MiB | Good idle/small-demo footprint. |
| Database | 130 MB | Small overall, but audit log already dominates it. |
| Audit log | 267,146 rows / 92 MB | About 71% of DB; retention/query design is urgent. |
| Browser cold compressed transfer | ~287 KB | Respectable, but too much is one role-agnostic JS payload. |
| `app.js` | 1,000,423 raw / ~237 KB gzip | Primary cold-load/parse/change-invalidation target. |
| Local small API p95 | mostly < 10 ms | Database/API foundation is fast at demo scale. |
| Boot batch (7 concurrent calls) | p50 ~13 ms, max ~94 ms | Fine locally; still unnecessary request fan-out. |
| 30 concurrent setup-status calls | 22 × 200, 8 × 503 | Current request control degrades abruptly. |
| Newest audit-log rows | ~118 ms query plan | Missing time index and full-table scan are visible early warnings. |
| Full serial Go tests | 140.5 s, green | Strong automated base; engines dominate runtime. |
| Go build / vet | 8.3 s / 2.7 s | Healthy developer loop on this machine. |

### What is actually slowing the product

1. **Interaction correctness:** clipped mobile flows, wrong input semantics, overlays, ambiguous state, and generic errors create retries and hesitation.
2. **Too much initial UI:** every role pays to download/parse a 1 MB script containing modules it may never open.
3. **Unbounded work:** stock and generic lists can query, transfer, and render far more rows than a human can use.
4. **Poorly shaped retained data:** the audit hot table dominates storage and lacks the index required by its common newest-first access.
5. **Request fan-out plus hard caps:** normal screens can create bursts that hit a fixed tenant limit and return 503 instead of scheduling/coalescing work.
6. **No production workload evidence:** without query fingerprints and realistic volumes, optimization risks targeting attractive microbenchmarks instead of customer pain.

The detailed budgets, order of work, and lightweight implementation constraints are in the companion plan.

## 9. Legal and compliance-readiness review (India-first)

This section is a product control checklist, **not legal advice**. Applicability depends on the customer, transaction type, turnover, industry, geography, deployment model, and contracts. Obtain Indian counsel, statutory auditor, GST practitioner, and security/privacy review before claiming compliance.

### Privacy and breach response

India’s Digital Personal Data Protection Act creates duties around processor contracts, accuracy where decisions/disclosures depend on data, reasonable safeguards, breach notification, and erasure when the purpose ends unless retention is legally necessary. The official [DPDP Act, 2023](https://www.meity.gov.in/static/uploads/2024/02/Digital-Personal-Data-Protection-Act-2023.pdf) and [DPDP Rules, 2025](https://www.meity.gov.in/static/uploads/2025/11/53450e6e5dc0bfa85ebd78686cadad39.pdf) should be the source of truth. The Rules were published on 13 November 2025 with staged commencement; as of this audit date, several substantive rules have not yet commenced, but engineering and data-lifecycle preparation should not wait. MeitY also publishes an [explanatory note](https://www.meity.gov.in/writereaddata/files/Explanatory-Note-DPDP-Rules-2025.pdf), which is guidance rather than a substitute for the law.

Product gaps: excessive Cashier access, no complete data inventory/purpose/retention map, weak token-revocation story, unclear processor/subprocessor evidence, no end-to-end data-principal request workflow, and no tested breach evidence/export pack.

### Cyber incident logs and reporting

CERT-In’s [2022 directions](https://www.cert-in.org.in/PDF/CERT-In_Directions_70B_28.04.2022.pdf) require covered entities to keep specified ICT logs for 180 days within India and report specified incidents within six hours. The official [FAQ](https://www.cert-in.org.in/PDF/FAQs_on_CyberSecurityDirections_May2022.pdf) explains that an initial report may use available information. Applicability and exact log set must be reviewed for each deployment.

Product gaps: no unified security-log inventory, residency/clock evidence, protected export, incident classification timer, contact/approval workflow, or tested six-hour response pack. Do not solve this by retaining every business payload in hot logs; minimize personal data and separate security evidence from application diagnostics.

### GST records, invoices, and e-invoicing

The official [CGST Act](https://cbic-gst.gov.in/pdf/CGST-Act-Updated-01082021.pdf), [accounts and records rules](https://cbic-gst.gov.in/accnt-record-rules.html), and [invoice rules](https://cbic-gst.gov.in/gst-invoice-rules.html) are the baseline. Records commonly require long retention and reproducible audit trail/interlinkages; proceedings can extend the period. E-invoice applicability and reporting windows change by turnover and notification. The government-authorized IRP summarizes the [₹5 crore mandate threshold](https://einvoice6.gst.gov.in/content/einvoice-mandate/) and [30-day reporting restriction for AATO ₹10 crore and above from 1 April 2025](https://einvoice6.gst.gov.in/content/revised-time-limit-for-e-invoice-reporting-for-businesses-with-aato-of-%E2%82%B910-crores-above/).

Product gaps: live GSP/IRP verification remains external; retention/lock/legal-hold policy is not consolidated; invoice mutation/cancellation and audit evidence need statutory-auditor sign-off; opening/migration reconciliation is not a governed journey.

### Accounting software audit trail

The Companies Act/rules may require covered companies using accounting software to preserve an edit log/audit trail with date and prevent disabling it. Use the official [India Code Companies Act page](https://www.indiacode.nic.in/handle/123456789/2114?locale=en) and current MCA rules/notifications with a statutory auditor. The current checksum gaps, concurrency model, direct database mutation possibility, and lack of disablement/verification evidence mean the ERP should not claim compliant audit-trail operation yet.

### Packaged goods, e-commerce, and PIM

Legal Metrology declarations can include manufacturer/packer/importer, country of origin, generic name, net quantity, dates, MRP/unit sale price, and consumer-care details depending on the product and channel. See the Department of Consumer Affairs’ official [Legal Metrology FAQ](https://consumeraffairs.nic.in/sites/default/files/file-uploads/latestnews/LM_FAQs.pdf) and [packaged commodity summary](https://consumeraffairs.nic.in/sites/default/files/14-July-2024.pdf). Consumer/e-commerce obligations must also be reviewed from the department’s [Consumer Protection rules index](https://consumeraffairs.nic.in/acts-and-rules/consumer-protection/consumer-protection).

Product gap: mandatory attributes, market/channel validation, approval evidence, version/effective date, artwork-to-attribute consistency, and country-of-origin provenance need a configurable compliance pack rather than free-form enrichment alone.

### Payment/card data

RBI’s [payment-data storage FAQ](https://systemhealth.rbi.org.in/Scripts/FAQDisplay.aspx_Id%3D130%281%29.html) and [card-on-file restrictions](https://www.rbi.org.in/scripts/NotificationUser.aspx?Id=12345) matter depending on whether the ERP/operator is in the payment ecosystem. The inspected design integrates a payment terminal and does not need raw card credentials. Preserve that boundary: store provider token/reference, masked display, status, amount, and reconciliation evidence—never full card number, CVV, PIN, or magnetic-track data. Validate PCI DSS scope contractually with the payment provider/acquirer.

### Accessibility

Accessibility obligations depend on who supplies/operates the service, but it is also a product-quality requirement. Review the Department of Empowerment of Persons with Disabilities’ [Acts and Rules](https://depwd.gov.in/en/document-category/acts/) and the government’s [GIGW accessibility focus](https://guidelines.india.gov.in/focus-areas/). The observed mobile clipping, input misclassification, small controls, inconsistent labels, and ambiguous state require a real WCAG 2.1/2.2 AA audit with keyboard, screen reader, zoom/reflow, contrast, error association, focus management, and physical device testing. A tap-size heuristic alone is not certification.

### Retention versus erasure: the required design

Do not create one global “delete customer” action or one global seven-year retention constant. Build a policy matrix by record category:

`purpose → data fields → controller/processor → legal basis/notice → access class → retention trigger and period → legal hold → export → erasure/anonymization → evidence owner`.

Separate immutable statutory transaction evidence from mutable contact/preferences and from short-lived telemetry. Where law requires retention, restrict and retain only what is required; where it does not, erase/anonymize on schedule. Keep the policy version that governed each decision.

## 10. Adversarial test journeys to institutionalize

These should become repeatable release tests. Each needs expected database, API, UI, audit, and reconciliation outcomes.

1. Cashier calls every registered route and requests every sensitive field; expected result is a deny matrix, not merely a hidden menu.
2. Cashier sends price `0.01`, cost `0`, discount `0`, alternate tax, and changed item after price quote.
3. Checkout fails after stock post, after loyalty, after tax, and after each journal step; retry simultaneously from two tabs.
4. Return the same line partially, repeatedly, concurrently, with changed price/cost/tender and lost responses.
5. Two 3PL owners share SKU/location/lot; allocate, pick, adjust, return, cycle count, and invoice simultaneously.
6. Scan alphanumeric wave/LPN/lot/serial on physical RF devices with weak Wi-Fi, repeated scan, disconnect, and app restart.
7. Write concurrent audit events, alter historic rows, restore a backup, cross a retention partition, and verify end-to-end evidence.
8. Search for a record outside the first page; compare UI count/export/API and repeat during concurrent writes.
9. Open the largest stock, audit, report, order, and item datasets at 10k, 100k, and 1m representative rows.
10. Start two app replicas; prove only one schedule/outbox job owns a lease and prove safe takeover after kill.
11. Restart with stale outbox records and external side effects disabled; inspect before replay, then replay safely.
12. Import dirty master data: duplicate GSTIN/SKU/barcode, broken references, cyclic BOM, invalid UOM, locale decimals, formula CSV, and partial rerun.
13. Use only keyboard and screen reader at 200%/400% zoom; complete receiving, picking, sale, approval, return, and error recovery.
14. Disable network at every connector/payment/printer boundary and prove the operator sees authoritative state and safe next action.
15. Perform backup, point-in-time restore where supported, reconciliation, secret rotation, token revocation, and evidence export under a timed incident drill.

## 11. Release evidence that is still missing

- Independent application/API penetration test and remediation retest.
- Signed role/field/SoD matrix and negative authorization suite.
- Financial controller sign-off on POS, returns, valuation, period close, reconciliation, and audit trail.
- Live GST/GSP/IRP, courier, channel, payment terminal, QZ/printer, scanner, and label certification for supported combinations.
- 3PL owner-isolation proof at every inventory transition.
- Representative-volume performance and soak tests with production-like skew and retention.
- Multi-replica worker/failover proof or an explicit single-replica support boundary.
- Accessibility audit and real RF/mobile field trial.
- Backup/restore plus business reconciliation under the exact production topology.
- Legal control matrix reviewed by qualified India counsel, privacy lead, GST practitioner, and statutory auditor as applicable.
- Pilot acceptance signed by normal users and floor users—not only developers/administrators.

## 12. What is already worth protecting

The correct response is not a rewrite. Protect these choices:

- Go modular monolith and PostgreSQL as the default production topology.
- One deployable binary and no mandatory frontend runtime/toolchain.
- Metadata-driven common behavior where domain invariants do not get diluted.
- Strong automated Go test base and standardized error catalog.
- Tenant schema isolation, maker-checker intent, double-entry intent, idempotency intent, and detailed operational docs.
- No raw card-data requirement and limited third-party runtime dependencies.

The next step is to make the narrow core trustworthy and effortless, then let modules earn activation through reference-journey gates. The companion [lightweight smoothness plan](LIGHTWEIGHT_SMOOTHNESS_PLAN_2026-09-01.md) gives that order without adding infrastructure by default.
