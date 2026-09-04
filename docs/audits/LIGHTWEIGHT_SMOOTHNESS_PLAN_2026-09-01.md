# First-principles lightweight ERP smoothness plan — 2026-09-01

**Purpose:** turn the findings in [ERP_DEEP_PERSONA_AUDIT_2026-09-01.md](ERP_DEEP_PERSONA_AUDIT_2026-09-01.md) into an ordered, measurable plan. This document designs work; it does not implement it.

**North star:** the smallest system that can complete, recover, reconcile, explain, and prove a customer's critical business journeys.

“Most lightweight” must mean low total cost, not merely a small executable. The total includes binary and RAM, database growth, browser work, external services, background CPU, deployment steps, operational skills, support burden, and the human time lost to confusing screens. A 16 MB binary with a 92 MB unbounded audit table and a broken mobile task is not lightweight in use.

## 1. Product constitution

Every new proposal must obey these rules unless measured evidence and an architecture decision record justify an exception.

1. **Correctness before speed; speed before breadth.** No new module work ships while a shared money, stock, privacy, tenant/owner, retry, or audit invariant is knowingly unsafe.
2. **Keep the modular monolith.** Default topology remains one Go application and one PostgreSQL database. A process boundary must solve a measured isolation/scaling problem, not express an org chart.
3. **PostgreSQL is the queue, search engine, lock coordinator, and reporting source until it demonstrably cannot meet a written SLO.** Use transactions, advisory locks, `SKIP LOCKED`, full-text/trigram options, views/materialized views, and partitions before adding infrastructure.
4. **No mandatory Redis, Kafka, Elasticsearch, Kubernetes, service mesh, data warehouse, Node server, or frontend framework.** Any future exception must include load evidence, failure-mode reduction, migration/rollback, and its permanent resource/operations budget.
5. **No production build dependency on the public internet.** Releases are reproducible from pinned, vendored, or prebuilt inputs. The server must start and operate without `npm`, `npx`, a CDN, or remote fonts.
6. **One authoritative command per business event.** Sale, return, receipt, allocation, shipment, payment, and journal effects each have one owner, one state machine, explicit invariants, and a transaction/idempotency design.
7. **Deny by default.** Screens, routes, records, fields, exports, jobs, and events share the same capability vocabulary. “Not in the menu” is never a security control.
8. **Do less work.** Do not fetch, serialize, transfer, render, retain hot, or recompute data the user cannot consume.
9. **Progressive disclosure.** A floor user sees the next safe action; an expert can expand details. Internal types, UUIDs, and stage numbers stay out of normal workflows.
10. **Every failure is recoverable and explainable.** The user sees what happened, what did not happen, whether retry is safe, and who can resolve it.
11. **Evidence is a feature.** Reconciliation, authorization denials, approvals, configuration versions, migrations, and restores produce reviewable evidence without leaking sensitive data.
12. **Modules earn activation.** A feature is Experimental, Preview, Production, or Certified for a supported configuration. A checked backlog item is not automatically a production claim.

## 2. Non-negotiable budgets

Record these in CI/release evidence. A budget breach blocks the release or requires an explicit signed exception with an expiry.

| Dimension | Current audit baseline | Target budget |
|---|---:|---:|
| Stripped server executable | 16.59 MiB | ≤ 25 MiB |
| Idle app working set, small tenant | ~65 MiB | ≤ 80 MiB |
| Mandatory production services | Go app + PostgreSQL | Exactly 2 until an evidence-based exception |
| Mandatory external runtime downloads | None | 0 |
| Core cold-page compressed transfer | ~287 KB | ≤ 180 KB |
| Core JS needed before first interaction | ~237 KB gzip | ≤ 120 KB gzip |
| Lazy view payload | Not separated | ≤ 40 KB gzip per ordinary view |
| p75 LCP, supported mid-tier client/Fast 4G | Not established | ≤ 1.5 s |
| p75 INP | Not established | ≤ 200 ms |
| Cached view switch p95 | Not established | ≤ 250 ms to usable state |
| Small interactive API server p95 | Mostly < 10 ms locally | ≤ 150 ms at supported production load, excluding network |
| Ordinary screen request failure | 8/30 503 in burst test | < 0.1%; 0 failures at specified normal-screen burst |
| Ordinary server request p99 | Not established | ≤ 500 ms; heavy work becomes a visible job |
| Audit/security hot storage | ~71% of demo DB | ≤ 20% of hot DB after required retention design |
| Unbounded list/report endpoints | Several | 0 |
| Accessibility | Not audited | WCAG 2.2 AA target plus real RF-device acceptance |

### Data-growth budget

Binary size is nearly irrelevant after years of transactions; data dominates. Every persistent feature must submit:

- bytes per business transaction at p50 and p95;
- indexes and write amplification;
- expected daily/monthly volume by tenant size;
- hot-retention period and access frequency;
- statutory/security retention requirement;
- archive format and restore/query path;
- deletion/anonymization trigger;
- peak temporary space during migration, backup, restore, and index build.

The design objective is **one canonical business record plus the minimum independent evidence required to prove it**. Do not duplicate the same order into multiple local “read stores” by default. Derived summaries should be reproducible, bounded, and discarded/rebuilt where safe.

## 3. The critical path

Do not run these as unrelated feature tickets. They are one “trustworthy transaction foundation” program.

```text
Route/field authorization ─┐
Server price authority ────┼──> Atomic sale/return ──> Reconciliation gates
Inventory owner dimension ─┤
Audit evidence model ──────┘
          │
          └──> Mobile/RF reference journey ──> Supported pilot
```

### Gate 0 — Freeze unsafe claims and preserve evidence

Before implementation begins:

- Mark 3PL, mobile WMS, payroll privacy, POS returns, and regulated audit-trail claims as Preview/Unsupported in the support matrix.
- Create reproducible failing tests for A-01 through A-07 from the audit. A defect is not closed by changing one example response.
- Snapshot the current role grants and identify tenant migrations required; never repair only newly created tenants.
- Identify the single active route for sale and return and list every table/event it mutates.
- Disable real external side effects in development/test by default; require an explicit environment-level enable switch and visible banner.
- Add no new product dependency or service during this gate.

**Exit:** every stop-ship finding has a failing regression/abuse test, named owner, invariant, migration impact, and acceptance evidence.

### Gate 1 — Authorization and privacy foundation

Design one capability registry that binds:

`route/command → action → document/domain → allowed role capabilities → entity/location/owner scope → field policy → export/log policy`.

Plan of work:

1. Inventory all 432 registered routes and classify public, authenticated, administrative, background, integration, and sensitive-data access.
2. Make unclassified authenticated routes fail closed in tests and production.
3. Define compact business capabilities such as `finance.trial_balance.read`, `hr.payslip.read_self`, `pos.price.override`, and `system.logs.read`; do not grant hundreds of raw record types directly to floor roles.
4. Separate record visibility from field visibility and mutation permission. Missing field policy for a sensitive type must not mean “all fields”.
5. Make scope explicit. Location-less records are global only for a capability that says global; `NULL` is not an authorization wildcard.
6. Ship conservative role templates: Cashier, Store Supervisor, Warehouse Picker, Warehouse Manager, AP Clerk, AR Clerk, Accountant, HR Manager, Employee Self-Service, Auditor, Administrator, Integration.
7. Add a segregation-of-duties conflict catalog and show conflicts before save: create+approve, price override+cash refund, vendor create+payment, employee master+payroll, stock adjust+cycle approval, connector secret+event replay.
8. Generate negative API tests from the capability registry for every role/route/field/scope combination; sample positive journeys separately.
9. Provide an administrator “access preview”: choose user and record, explain allow/deny, source role, scope, and field redactions.
10. Migrate existing tenants and produce a before/after grant report for owner sign-off.

**Exit:** Cashier receives 403/field redaction on all audited sensitive routes; no unclassified route is callable; cross-location/entity/owner negative tests pass; role migration is reversible and signed.

**Footprint effect:** small metadata/code increase; no new service; lower support and breach cost.

### Gate 2 — Server-authoritative POS and one atomic return model

Define domain invariants before code shape:

- A quote is derived from item, price list/contract, customer, location, channel, date/time, quantity, currency, tax basis, and eligible promotions.
- The client selects inputs; it does not assert authoritative price, tax, cost, margin, discount entitlement, or available stock.
- A posted sale is exactly-once by tenant and idempotency key.
- A retry returns the original outcome or safely continues a durable state machine; it never repeats a mutation.
- Inventory, stock ledger, COGS, revenue, tax, tender/receivable, loyalty, and audit evidence reconcile to one sale identity.
- A return references immutable original lines and tender facts. Cumulative accepted quantity cannot exceed eligible quantity under concurrency.
- Refund override, no-receipt return, exchange, goodwill, and damaged disposition are explicit commands with capabilities and reasons.

Preferred lightweight transaction shape:

1. Begin one PostgreSQL transaction.
2. Insert/lock the command idempotency row before any mutation.
3. Lock the cart/order and affected availability/return-quantity rows in deterministic order.
4. Resolve price/tax/cost on the server and record rule versions.
5. Validate approval/override capability and thresholds.
6. Apply inventory and accounting mutations.
7. Write business event/outbox and audit evidence in the same transaction.
8. Commit once; external receipt/notification/payment follow-up consumes the outbox.

If a payment provider requires an external round trip, use a small durable state machine (`Initiated → Authorized → Posted` or `Failed/Voided`) with provider idempotency, not a database transaction held across the network. Inventory reservation expiry and payment reversal must be explicit.

Required tests: tampered client payload, zero-price without discount, stale quote, price change during checkout, two-tab checkout, response loss, failure after every mutation boundary, partial/multi-tender return, repeated/concurrent return, tax reversal, loyalty failure, and reconciliation totals.

**Exit:** forced failures leave no unexplained stock/finance difference; 100 retries produce one business outcome; cost never comes from or appears to an unauthorized POS client; legacy return route is removed or returns a hard deprecation error.

**Footprint effect:** consolidating duplicate paths should reduce code and test/support surface.

### Gate 3 — Owner-safe WMS or explicit de-scope

If 3PL remains a product claim, owner becomes a first-class required dimension on every relevant domain object and query:

- inbound appointment/ASN/receipt and receipt line;
- LPN, lot/batch, serial, bin balance, availability and stock ledger;
- sales/transfer/return order and allocation demand;
- wave, task, pick, pack, ship, adjustment, hold, cycle count;
- value-added service, storage/activity billing, owner statement and reconciliation.

Database uniqueness and foreign-key rules should prevent ownerless or mismatched transitions where domain rules permit. Allocation must never use an owner-agnostic fallback. Cross-dock, substitution, commingled stock, and ownership transfer require explicit policies and evidence.

If this cannot be completed now, enforce one owner per warehouse and label 3PL/mixed-owner use unsupported. Honest de-scoping is safer and lighter than partial isolation.

**Exit:** adversarial two-owner lifecycle and reconciliation pass at API/database/UI levels, or the product prevents configuration that implies 3PL isolation.

### Gate 4 — Audit evidence and bounded retention

First choose the threat/evidence model with the statutory auditor and security lead. A practical lightweight model is:

- append-only audit event with tenant, actor/session, command, entity, business key, before/after digest or constrained diff, timestamp source, request/idempotency correlation, software release, and policy version;
- serialized per-tenant/per-shard chain or independently signed events plus periodic signed checkpoint;
- chain/checkpoint row written in the same transaction as the protected business mutation where possible;
- database role/trigger controls that prevent normal application update/delete;
- scheduled verifier with alert and evidence of last successful run;
- explicit legacy seal/checkpoint for rows that cannot be credibly backfilled;
- time/tenant partitions or archive segments based on measured access and retention;
- compressed, encrypted, integrity-checked archive with indexed manifest and tested restore/export.

Add indexes from real access patterns (`tenant`, `created_at`, business entity/key, actor, correlation), not every possible filter. Keep a short hot window sized to investigations; retain legally required evidence in a cheaper sealed form. Do not archive by deleting before verification and manifest commit. Do not claim WORM/immutability unless the storage and administrative model supports it.

**Exit:** concurrent-write, tamper, checkpoint, archive, restore, legal-hold, and verification drills pass; newest/range/entity queries meet SLO at target volume; policy covers statutory retention and privacy erasure.

### Gate 5 — A real mobile/RF reference journey

Do not shrink the desktop application. Build a thin role/task shell using the existing Go/JS stack:

- full viewport with no permanent desktop sidebar;
- explicit scanner/text/number/date/phone input metadata;
- one instruction, one primary scan/action, one recovery action;
- human label plus optional code; never a naked UUID;
- focus automatically returns to scanner field;
- distinct sound/vibration/color plus text for success, duplicate, wrong item, hold, and offline;
- local queue only for commands explicitly safe offline, each with device command ID and visible sync state;
- supervisor handoff without sharing credentials;
- accessible target size, contrast, zoom/reflow, keyboard, and screen-reader semantics;
- bounded payload and no full inventory download.

Start with one end-to-end path: receive → identify LPN/lot/serial → put away → allocate → pick → stage → load. Prove it on the actual supported Android/RF hardware, scanner mode, printer, label, glove, light, noise, and weak-Wi-Fi conditions.

**Exit:** trained floor users complete the reference journey without developer help, no horizontal clipping, no alphanumeric corruption, safe reconnect/retry, and agreed time/error targets.

## 4. Measurement before optimization

### Performance laboratory

Add test-only tooling, not production services:

1. Deterministic tenant generator for small, 10k, 100k, and 1m-scale business shapes, including skew: hot SKU, long audit history, many empty locations, large orders, partial returns, and multi-owner stock.
2. A small Go HTTP workload suite for authenticated business journeys and concurrency/race scenarios. Keep third-party load tools optional outside the production build.
3. Browser journey budgets using a pinned test browser in CI: transfer bytes, DOM nodes, long tasks, LCP, INP, layout shift, accessibility scan, HTTP/console/page errors, and screenshots.
4. PostgreSQL query evidence. Enable `pg_stat_statements` on benchmark/staging and representative production where allowed; otherwise use bounded slow-query logging. Record fingerprints, calls, total time, p95 proxy, rows, and buffers without parameters containing personal data.
5. Resource sampling: executable/assets, RSS/heap, goroutines, open DB connections, CPU/request, DB/table/index size, WAL rate, archive rate, and backup size/duration.
6. Soak and restart tests: 24-hour representative job/outbox workload, expired tokens, unique limiter keys, stale jobs, app kill, DB restart, and clock/time-zone boundaries.

### Reference journeys and SLOs

Measure complete user outcomes rather than isolated endpoints:

| Journey | Start | End | Core correctness measure |
|---|---|---|---|
| First sale | Empty supported tenant | Closed till and reconciled sale | Zero manual database work; price/tax/stock/GL agree. |
| Procure-to-pay | Approved demand | Vendor liability/payment ready | Receipt, match, valuation, tax, and GL reconcile. |
| Order-to-cash | Imported/created order | Settlement reconciled | Exactly-once channel/stock/shipment/refund effects. |
| Warehouse execution | ASN | Loaded shipment | Owner/lot/serial correct; retries and device loss safe. |
| Period close | Open period | Signed statements | All subledgers reconcile; exceptions owned; period locks hold. |
| Master migration | Source package | Signed control totals | Rejects explained; rerun safe; transformed lineage retained. |
| Incident recovery | Alert | Restored and reconciled service | RTO/RPO achieved; tokens/secrets/evidence handled. |

For each journey collect task time, error/retry count, API/DB latency, query count, bytes, CPU, memory, data growth, accessibility failures, and user confidence. A change is an optimization only if the journey improves without weakening an invariant.

## 5. Browser and API smoothness work

### 5.1 Split by role and view with native modules

Keep vanilla JavaScript, but turn the monolithic script into native ES modules:

- a small shell: session, navigation, capability map, API client, errors, overlays, common controls;
- domain/view modules loaded with `import()` only when authorized and opened;
- role-scoped navigation so a Cashier never downloads HR/finance-admin view logic;
- shared formatting/validation based on explicit field metadata;
- event delegation instead of inline handlers;
- no build step required for development or production.

Serve content-hashed or versioned immutable assets. Precompressed `.gz`/Brotli artifacts are acceptable only if the entire shipped static directory remains within its release budget; they trade a few hundred KB of disk for lower CPU/transfer and add no service. Keep source maps outside public production assets unless access-controlled.

### 5.2 Make screen startup one controlled plan

- Return minimal session identity, capabilities, localization/version, and initial route data; defer everything else.
- Cache stable metadata with ETag/version. Invalidate by schema/config release, not time guesses.
- Coalesce identical in-flight requests and cancel stale view requests with `AbortController`.
- Define per-view request budgets (ordinary view ≤ 4 initial API calls unless justified).
- Replace the generic 503 burst failure with bounded client scheduling, server pool backpressure, and a specific retryable 429/503 envelope with `Retry-After` where overload is real.
- Reserve capacity for authentication/health/command completion so heavy reports cannot starve transaction finalization.

Do not build one enormous bootstrap response. The goal is fewer necessary bytes and round trips, not mechanically one call.

### 5.3 Bound every collection

- Server-side filtering, sorting, authorization, and pagination for every list.
- Prefer keyset/cursor pagination for high-write/high-volume event and transaction tables; offset is acceptable for small stable masters.
- Search in SQL before limit. Use normalized columns and B-tree prefix indexes first, PostgreSQL FTS/`pg_trgm` only where measured need justifies them.
- Return explicit total/estimated count only where users need it; exact counts on huge sets can be more expensive than the task.
- Virtualize or incrementally render long client tables while preserving keyboard and screen-reader behavior.
- Exports and heavy reports become visible cancellable jobs with snapshot/as-of semantics and expiry, using the PostgreSQL job runner.

### 5.4 Make state obvious

Every view has distinct loading, empty, filtered-empty, permission-denied, validation, transient failure, and terminal failure states. Error text follows:

`What happened → what was saved/not saved → safe next action → reference ID/help`.

Use local business dates in an unambiguous format selected by tenant locale, while APIs and stored timestamps retain standard representations. KPIs always state period, scope, refresh time, definition, owner, and action. Hide engineering stages and record-type internals from normal users.

## 6. Lightweight job runner and integration platform

Stage 38.6 should establish one reusable PostgreSQL-backed job/outbox mechanism before more custom tickers are added.

Minimum job fields: tenant, type/version, payload reference, status, priority, scheduled time, lease owner/expiry, attempt/max, idempotency key, progress, last safe error code, created/started/finished/expiry, correlation, and cancel request. Workers claim with `FOR UPDATE SKIP LOCKED`, use short leases/heartbeats, and make each handler idempotent. PostgreSQL advisory locks can own singleton schedules. Retention deletes payload/results by policy while retaining minimum outcome evidence.

Then order platform work as follows:

1. **38.6 durable job runner:** consolidate reports, schedules, outbox retries, expiry sweeps, connector sync, and archive work.
2. **38.4 webhooks:** signed versioned events, subscription capability scopes, delivery log, exponential backoff/jitter, disable threshold, secret rotation, replay, and idempotency guidance.
3. **38.7 sandbox tenant:** seeded synthetic data, external side effects off, reset/expiry, representative roles, API/webhook playground, no copying production personal data.
4. **38.2d OAuth2:** only supported grants, short tokens, scoped clients, rotation/revocation, audit, rate policy, and tenant-admin consent. Avoid implementing a general identity provider when a standards-compliant deployment integration is safer; document supported topology.

The integration developer needs a versioned public contract, pagination/idempotency/error semantics, changelog/deprecation window, sample payloads, webhook replay, and compatibility tests. Do not expose internal generic routes as a substitute for API design.

## 7. Self-setup and master-data plan

### 7.1 Scope-led onboarding

Replace the flat setup list with a small dependency graph rendered as a guided checklist:

1. Business profile: industry, countries, legal entities, registrations, channels, locations, warehouses, headcount/roles, inventory tracking, accounting basis.
2. Recommended scope: modules and controls; unsupported combinations are clearly blocked.
3. Organization graph: legal entity → branch/location → warehouse/store → dimensions.
4. Fiscal/tax/accounting: calendar/periods, chart template, currency, registrations, invoice series, banks/cash, opening date.
5. People/access: named users, conservative role templates, scopes, approval thresholds, SoD conflicts, emergency admin.
6. Master migration: ordered packages and reconciliation.
7. Integrations/devices: sandbox connection tests and evidence.
8. Rehearsal: synthetic reference transactions and deliberate failure recovery.
9. Cutover: backup/restore proof, opening counts/balances, exceptions, sign-offs, side-effect enablement.

Each readiness node is more than a row count. It has dependencies, validator version, status (`Not started / Draft / Invalid / Awaiting approval / Ready / Waived`), evidence, responsible person, and blocking severity. Readiness is calculated from control outcomes, not vanity completion percentage.

### 7.2 Bootstrap governance for a one-person business

Offer explicit profiles:

- **Controlled business:** distinct maker/checker required for configured high-risk actions.
- **Micro-business owner:** named owner may approve specified actions only below configured thresholds after accepting a documented risk profile; all self-approved events appear in an immutable daily/weekly exception report shareable with an accountant.
- **Implementation mode:** synthetic/sandbox only; broad setup rights expire automatically and cannot post real side effects.

Never advise one person to create two identities. High-risk capabilities such as payroll, bank beneficiary/payment, price override/refund, stock write-off, and audit/security administration may still require an external checker or later independent review.

### 7.3 Master-data migration and stewardship

Use PostgreSQL staging tables and metadata definitions—no separate ETL server by default.

Pipeline:

`Upload → malware/type/size check → parse → normalize → transform(versioned) → validate → duplicate match → preview impact → approve → idempotent commit → reconcile → retain lineage/result → expire raw file`.

Every template states required fields, business definitions, examples, dependencies, locale/date/decimal/UOM rules, enumerations, uniqueness, update key, and destructive-update behavior. Control totals include rows, quantities, values, opening balances, and rejects. CSV formula injection and spreadsheet round-trip are tested.

Stewardship metadata should cover system of record per attribute, owner/steward, quality rule, completeness, confidence/provenance, effective dates, approval, survivorship rule, market/channel/language variant, legal attribute, and downstream subscribers. Duplicate merge needs reversible references and an audit trail. Product/legal attributes need configurable country/channel packs rather than hardcoded one-market fields.

### 7.4 Help at the point of work

Complete Stage 39 in this order:

1. 39.8 documentation drift guard and route/view/role/help manifest.
2. POS, WMS/RF, security/access, finance close, and returns handbooks because they carry the highest operational/control risk.
3. Integration and administrator handbooks.
4. Remaining module handbooks ordered by enabled production scope.
5. Repair the orphaned Stage 42.7 reference and track traceability UI explicitly.

Each help page is versioned to the product release and includes purpose, prerequisites, role, exact task, expected evidence, failure/recovery, “do not do this”, glossary, and escalation. The browser smoke test must fail on HTTP errors, JavaScript errors, missing help mapping, obscuring overlays, selector drift, and unauthorized success.

## 8. Product maturity and backlog order

### Outcome-based maturity levels

| Level | Meaning | Required evidence |
|---|---|---|
| Experimental | Engineering exploration | No production claim; synthetic data only. |
| Preview | Usable with known limits | Supported scope, documented gaps, telemetry, rollback, named pilot. |
| Production | Safe for stated configuration | Reference journey, controls, negative tests, migration, docs, SLO, backup/restore, user acceptance. |
| Certified | Externally validated configuration | Relevant legal/auditor/security/device/partner evidence and expiry/revalidation date. |

Status is per configuration, not globally per module. For example, “single-owner WMS on supported Android device” can mature before “mixed-owner 3PL WMS”.

### Backlog sequence after Gates 0–5

1. **37.5 financial statement builder and close evidence.** It turns ledgers into controller-consumable outcomes and exposes reconciliation quality.
2. **37.6 deferred revenue and statutory accounting depth** where target customers require it.
3. **38.6/38.4/38.7 platform reliability** as specified above; OAuth2 when partner/SSO topology is decided.
4. **39.8 and risk-critical handbooks**, then integration/admin documentation.
5. **35/36 depth hardening:** one returns implementation, connector certification, settlement exception operations, product/legal-attribute governance, master-data migration cockpit.
6. **37.7 projects/job costing and 37.8 service management** only if the selected reference industries need them.
7. **37.9 quality/maintenance and 37.10 planning depth** for manufacturing/asset-heavy reference customers, with shop-floor/device trials.
8. **37.11 role dashboards last**, fed only by reconciled, permissioned definitions with owner/action/drill-down.

Do not develop all industries simultaneously. Select at most two reference configurations for the next production level—for example, single-store/multi-store India retail and single-owner wholesale distribution. Publish unsupported combinations. This reduces code, test matrix, documentation, legal surface, and support load while improving credibility.

## 9. Legal and compliance by design

Create a versioned control matrix with these columns:

`Jurisdiction / rule / applicability condition / customer responsibility / product responsibility / configuration / preventive control / detective control / evidence / retention / owner / counsel-auditor review / last validation / expiry`.

Initial packs:

- India privacy/DPDP: notice/configuration, purpose/data inventory, processors, access, request handling, safeguards, breach workflow, retention/erasure/legal hold.
- CERT-In: clock/log inventory, 180-day applicable security evidence in India, incident classification and six-hour workflow where applicable.
- GST: registration/invoice/e-invoice applicability, invoice immutability/cancellation, record retention, IRP/GSP evidence, reconciliation.
- Companies Act accounting audit trail: event coverage, edit/delete policy, disablement protection, verification, evidence export, auditor sign-off.
- Legal Metrology/e-commerce: category/channel declarations, country of origin, MRP/unit price, quantity/date/consumer care, approval and effective version.
- Payments: token/reference only, no prohibited authentication/card data, provider/acquirer responsibility, PCI scope evidence.
- Accessibility: WCAG audit and procurement/customer-specific obligations.

Compliance controls must be configuration packs plus evidence, not a universal “compliant” badge. Rules change; every pack has effective dates and review expiry. Legal retention must be reconciled with privacy minimization rather than allowing either to overwrite the other.

## 10. Test architecture

### Layer 1 — Domain invariants

Fast deterministic tests for price/tax/cost authority, balanced journal, non-negative/reserved availability policy, owner/lot/serial identity, cumulative returns, period lock, approvals, and state transitions.

### Layer 2 — Transaction and concurrency

Real PostgreSQL tests that kill/fail after each boundary, retry command IDs, run two sessions, deadlock in opposite order, and verify final business reconciliation. These tests are essential for sale, return, receipt, allocation, shipment, payment, import, and job claim.

### Layer 3 — Generated authorization contracts

For every registered route and command: unauthenticated, each role capability, wrong entity/location/owner, self vs other employee, allowed/denied fields, export, and inactive/demoted/revoked token. Fail the build on an unclassified route.

### Layer 4 — API compatibility and abuse

Schema/version, pagination, idempotency, rate/backpressure, malformed/oversized input, file types, CSV injection, XSS payloads, webhook signatures/replay, connector secrets, and tenant confusion.

### Layer 5 — Browser task contracts

Role-authenticated journeys on desktop and supported mobile/RF sizes; no HTTP ≥ 400 unless expected, no console/page error, no hidden overlay, no horizontal clipping, deterministic loading/empty/error state, help link present, focus/keyboard/label checks, and performance budgets.

### Layer 6 — Volume, soak, failure, and recovery

Representative data sizes; month-end/report peaks; long audit retention; two replicas; worker takeover; stale outbox; DB restart; backup/restore; disk-near-full; secret rotation; external connector outage; low bandwidth and device restart.

### Layer 7 — Human and external validation

Cashier, accountant, warehouse floor worker, admin, integration developer, and owner complete role journeys without developer coaching. Independent security, accessibility, statutory/accounting, tax, device, and integration specialists validate only the claims relevant to the supported configuration.

## 11. Release gates

A production release cannot pass on build/tests alone. The evidence pack must show:

- exact commit/release and migration set;
- clean build, vet, serial/parallel/race-relevant tests as defined by repo policy;
- zero open P0 and signed P1 exceptions with expiry;
- route/role/field/scope authorization diff;
- money/stock/subledger/GL reconciliation for reference journeys;
- browser/accessibility/performance budgets;
- schema/data growth and migration temporary-space estimate;
- backup and restore/reconciliation result;
- job/outbox backlog and retry test;
- docs/help manifest and semantic journey result;
- dependency/license/vulnerability record;
- supported configuration and known-limits statement;
- external validations required for the claim;
- rollback/roll-forward and incident contacts.

Canary/tenant rollout should be configuration-driven and reversible. Schema migrations should be expand/backfill/verify/switch/contract where necessary, with explicit free-space checks. Do not retain duplicate old/new columns or tables indefinitely; set cleanup gates after verified rollback windows.

## 12. Server-space impact ledger

Every epic fills this table before approval and after measurement:

| Budget item | Before | Expected delta | Measured delta | Limit | Retention/cleanup | Decision |
|---|---:|---:|---:|---:|---|---|
| Binary |  |  |  | 25 MiB | old release policy |  |
| Static assets, raw + compressed |  |  |  | project release budget | content-hash cleanup |  |
| Idle/peak RSS |  |  |  | 80 MiB idle | N/A |  |
| DB heap/index per 1k transactions |  |  |  | feature budget | archive/delete |  |
| WAL per 1k transactions |  |  |  | deployment budget | backup retention |  |
| Hot logs/audit |  |  |  | ≤ 20% hot DB | partition/archive |  |
| Job/outbox rows and payload |  |  |  | bounded age/count | outcome compaction |  |
| Backup size/time |  |  |  | RPO/RTO budget | rotation |  |
| New process/service/dependency |  |  |  | zero by default | removal plan |  |
| Browser boot bytes/CPU |  |  |  | 180/120 KB budgets | cache/version |  |

Space-saving priorities are:

1. Bound and archive audit/log/job/outbox data according to legal policy.
2. Eliminate duplicate business implementations and redundant retained payloads.
3. Paginate/query on demand rather than caching entire datasets in browser/server memory.
4. Split UI for cache efficiency without adding a production toolchain.
5. Measure index value; remove unused/redundant indexes only after workload evidence.
6. Stream exports/backups and avoid loading whole files/results into memory.
7. Expire sandbox tenants, raw imports, generated reports, and temporary artifacts automatically after policy-defined windows.

## 13. Delivery waves and decision points

Calendar estimates depend on the number of engineers and the amount of production data already in use. Use gates, not dates, to prevent false completion.

| Wave | Scope | Relative effort | Decision point |
|---|---|---:|---|
| W0 | Reproduce/freeze claims, capability inventory, mutation maps, support matrix | S | Are all critical invariants and active paths known? |
| W1 | Authorization/privacy migration and generated negative tests | L | Can least-privilege roles complete only their reference tasks? |
| W2 | Authoritative pricing, atomic sale, unified atomic return | XL | Do failure/retry/reconciliation tests prove exactly one outcome? |
| W3 | 3PL owner dimension or enforced de-scope | XL | Is mixed-owner WMS a chosen reference product? |
| W4 | Audit model, migration seal, verifier, bounded retention | L–XL | What evidence model did auditor/security/legal owners approve? |
| W5 | Mobile/RF shell and physical reference journey | L | Which exact devices/scanners/printers are supported? |
| W6 | Measurement harness, bounded queries, JS/view split, request shaping | L | Do representative journeys meet budgets without new services? |
| W7 | Onboarding, migration cockpit, role templates, readiness and docs contracts | XL | Can target owner go from empty tenant to signed rehearsal alone? |
| W8 | Platform/jobs/webhooks/sandbox/OAuth and selected backlog depth | XL | Which two reference configurations earn Production status? |

Some work can run concurrently only after shared invariants are settled: browser module splitting can accompany audit design; docs contracts can accompany authorization inventory; performance fixture creation can accompany domain tests. Do not implement sale, return, and owner-allocation changes independently without a common inventory/accounting transaction model.

## 14. Definition of “ultra butter smooth”

The goal is reached only when all of these are true for the declared supported configurations:

- A normal owner can configure, rehearse, reconcile, and go live with contextual guidance and no developer/database intervention.
- A floor user with basic English can complete the physical task on supported hardware, recover from common mistakes, and always know the next action.
- A low-privilege user cannot discover or invoke unauthorized routes, fields, records, exports, or logs.
- Double click, retry, disconnect, restart, and concurrent work produce exactly one explainable money/stock outcome.
- Every quantity and amount can be reconciled to its business event, owner, accounting evidence, and audit checkpoint.
- The common screen becomes usable within the performance budgets on supported devices and normal network conditions.
- Database, logs, jobs, reports, sandboxes, and assets have bounded growth and tested cleanup/retention.
- One app plus PostgreSQL remains sufficient at the published supported load; any exception is evidence-driven.
- Documentation is found from the exact task/state and is automatically tested against the release.
- Product claims name the supported configuration and evidence rather than saying a numbered stage is complete.

That combination—not maximum feature count—is how this ERP can become both exceptionally light and exceptionally mature.
