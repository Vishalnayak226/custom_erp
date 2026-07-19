# PDF Blueprint Gap Analysis

> **Status: superseded snapshot.** This analysis was written 2026-07-12 and its "Thin" verdict on the business-user-facing layer (§1) drove the Stage 13 build plan in `docs/micro_checklist.md`. Most of what §3+ below lists as missing — POS screen, Finance/GL screen, GST calc, approval/maker-checker engine, MFA, security headers, RFQ/vendor quotes, sticker printing, HR, Fixed Assets, Expense Management, CRM/Loyalty (scoped MVP), Manufacturing (scoped MVP), and per-API-type rate limiting — has since been built (Stage 13.1 through 13.15, including the 13.13a-e split). Treat everything below as a historical record of what was true on 2026-07-12, not current state; **[docs/micro_checklist.md](../micro_checklist.md)** is the current source of truth for what's built.

**Date:** 2026-07-12
**Source documents:** `C:\Users\ABCD\Downloads\MyBusiness\IT Solution\ERP\PDF\`
1. `Inhouse_ERP_Master_Blueprint_Generic.pdf` (MB) — 28 pages, 24 sections, jewellery/retail vertical detail
2. `Generic_Inhouse_ERP_Platform_Approach_Blueprint.pdf` (AB) — 19 pages, 21 sections, multi-tenant SaaS/kernel framing
3. `Inhouse_ERP_Functionality_Blueprint_One_For_All.pdf` (FB) — condensed cross-industry functional summary
4. `Inhouse_ERP_Omnichannel_Scale_Addon_Blueprint.pdf` (OB) — event-driven, 1-to-2000-store scale add-on
5. `Inhouse_ERP_Web_Security_Developer_Checklist.pdf` (SEC-V1) — superseded by V2
6. `Inhouse_ERP_Web_Security_V2_Developer_Checklist.pdf` (SEC-V2) — authoritative security checklist

**Methodology:** MB and AB were extracted by a background research agent that hit an account session-limit mid-run; it had already read both documents in full (cross-validated with two extraction modes) and only lost its final wrap-up paragraph, so treat MB/AB coverage as complete. FB and OB were read directly by the main agent, in full, using `pdftotext -raw`. SEC-V1/V2 were extracted in full by a second background agent that completed normally. Every "Built" / "Missing" claim below was checked directly against the live source (`main.go`, `engines/*.go`, `db/migration.sql`, `public/*`), not against memory or prior doc claims — see inline file:line references. Where a claim couldn't be verified against source (e.g. whether a doctype was created later via the runtime DocType Builder rather than migration seed data), it's marked **[unverified]** rather than asserted either way.

---

## 1. Headline Answer

**No, the system described across these five functional blueprints is not fully built.** But "not fully built" undersells what exists — the honest picture has three distinct layers, and they're at very different levels of completion:

| Layer | State |
|---|---|
| **Kernel / architecture** (DocType-metadata engine, RBAC, multi-tenancy, numbering, audit, rate limiting, event outbox) | **Strong.** Matches or in places exceeds AB's spec (e.g. schema-per-tenant instead of row-level `tenant_id`, which AB itself calls the stronger option). |
| **Omnichannel backend** (inventory availability/reservation, fulfillment routing, GL double-entry, marketplace settlement, demand forecasting) | **Substantially built.** This is the single most-developed layer, and it's sophisticated — ATS reservation math, re-routing on pick rejection, balanced double-entry postings, scale-tested to 2,000 simulated stores. |
| **Business-user-facing ERP** (the modules in MB's functional scope: POS billing screen, Finance/GL screen, GST/e-invoice, CRM, HR, Assets, Expense, Manufacturing, full report catalog, approval/maker-checker workflows) | **Thin.** The sidebar exposes 13 menu items; most of MB's ~18 functional modules have no screen at all, and some have real backend logic sitting completely unreachable behind zero UI. |

The reason for this shape isn't neglect — it's traceable. `docs/ai_handover.md` §6 and `docs/micro_checklist.md`'s Stage 1–12 structure show the build was executed against the **Omnichannel Scale Add-on Blueprint's rollout plan** (OB §19: Foundation → Single Vertical Pilot → Omnichannel Pilot → Store Fulfillment → Scale Test → Marketplace Expansion → Advanced Optimization — nearly identical wording to the "Stage" headers in `micro_checklist.md`). That plan is about proving the **architecture** scales safely from 1 to 2,000 stores. It was never a plan to cover MB's full **functional module list** (POS UI, Finance UI, GST, CRM, HR, Assets, Manufacturing, the ~80-report catalog). Those modules aren't "in progress and behind schedule" — they were never in the tracked build scope to begin with. That's the most important distinction in this whole analysis: two of the six PDFs were the actual build target; the other three (MB's full functional breadth, FB's module coverage table, most of SEC-V2) describe a wider system that hasn't been started.

---

## 2. What's Solidly Built (verified against source)

**Architecture / kernel** — matches AB §4–§6 closely:
- Go backend, PostgreSQL, **schema-per-tenant** isolation (`db.GetTenantSchema` + `db.SetSearchPath` used consistently across every engine file checked)
- DocType/metadata engine: `doctype_meta` + `doctype_fields` tables, generic `documents` table with JSONB `data`, dynamic field validation, admin DocType Builder UI (`public/index.html` "DocType Builder" menu item, `main.go:265-269`)
- RBAC via `role_permissions` table, checked per-doctype (allow_read/create/update/delete)
- Numbering Engine (`engines/numbering.go`): prefix/separator/padding/reset-frequency — matches MB §6.1/AB §10.1 format exactly
- Dynamic Label Engine (`engines/labels.go`) — matches MB §14.4/AB's Label Engine
- Audit logging (`audit_logs` table, `engines/logs.go`)
- Bulk import with template + row-level error export (`engines/import.go`, matches MB §15.1/§15.2)
- Real login flow, bcrypt passwords, HMAC-signed tokens with configurable expiry (`JWT_EXPIRY_HOURS`, verified in `engines/auth.go:23-28,74,123` — **this closes a gap `docs/ai_handover.md` still claims is open, see §7 below**)
- CORS allowlist, per-endpoint rate limiting (`main.go:132-142`: 5/min on `/login`, 60/min elsewhere, returns HTTP 429 — matches SEC-V1 §4's literal spec almost verbatim), SQL-injection-safe filter-key allowlisting

**Omnichannel/scale backend** — matches OB closely, this is the deepest layer:
- Inventory Availability + Reservation engine (`inventory_availability`, `inventory_reservation` tables; `engines/inventory.go`) implementing OB §4's exact ATS formula
- Order/fulfillment routing (`engines/sourcing.go` `FindBestFulfillmentNode`, `engines/fulfillment.go` task transitions + re-routing on rejection) — matches OB §10's rule-driven fulfillment concept (stock-availability-based node selection; OB's richer factor list — SLA, store capacity, ageing, margin, manual override — is not implemented, only stock-availability is)
- Event outbox pattern (`integration_event_outbox`, `integration_event_log` tables, `engines/outbox.go` background poller) — matches OB §5's "no lost event" outbox requirement
- GL double-entry accounting engine (`engines/finance.go` `PostDoubleEntry`, enforces debits == credits) with automated postings from GRN, checkout, returns, marketplace settlement
- Marketplace settlement reconciliation + logistics booking (`engines/marketplace.go`)
- Demand forecasting / replenishment suggestions / SLA-breach monitoring (`engines/optimization.go`)
- Shopify product-map + order webhook with idempotency (`engines/sourcing.go` `ImportChannelOrder`, checks `channel_order_mapping` before processing)
- SaaS tenant provisioning with unique per-tenant admin credentials, feature flags (`engines/saas.go` — **also closes a gap `docs/ai_handover.md` still claims is open, see §7**)
- Concurrent scale-test harness (`engines/scale.go`), actually run at 100 workers / 1,000 transactions / 2,000 simulated store nodes with p50/p95/p99 latency capture

This is genuinely more sophisticated than a typical "MVP ERP" — the reservation math, re-routing logic, and outbox pattern are the kind of thing most in-house ERPs never get to.

---

## 3. The Central Gap: Backend Built, No Front Door

This is the single highest-leverage finding. The sidebar (`public/index.html`) has exactly **13 menu items**: Dashboard, Master Definition, Vendors, Stores, Purchase Orders, Inventory, Transfers, Users, Roles, Prefix Configs, Dynamic Labels, DocType Builder, Log Hub.

The API surface (`main.go:222-285`) has **20+ route groups**. Cross-referencing them:

| Backend capability (route exists, tested, working) | Frontend screen? |
|---|---|
| POS checkout (`POST /api/v1/checkout`), POSCart/SalesReturn doctypes | **None.** No cashier billing screen, no barcode-scan-to-sell UI anywhere. |
| Finance trial balance (`GET /api/v1/finance/trial-balance`), GL postings | **None.** No Finance/GL screen. |
| Inventory availability + reservation (`GET /api/v1/availability`, `POST /api/v1/reserve`) | **None.** Not surfaced on the Inventory screen or anywhere else. |
| Fulfillment task transitions + returns (`POST /api/v1/fulfillment/task/transition`, `/return`) | **None.** No pick/pack/dispatch workbench. |
| Marketplace settlement + logistics booking (`POST /api/v1/marketplace/*`) | **None.** |
| Optimization: replenishment suggestions, SLA breaches, demand forecast (`GET /api/v1/optimization/*`) | **None.** |
| Integration logs + retry (`GET /api/v1/integration/logs`, `POST /api/v1/integration/retry`) | **Backend only** — already tracked as a known gap in `micro_checklist.md` Stage 9.2: Log Hub's UI shows audit trails and panic traces but never calls these two endpoints. |
| Industry switch (`GET/POST /api/v1/admin/industry`) | **None** found in the sidebar/screens (only the DocType-level effect is visible). |
| Scale test (`POST /api/v1/admin/scale-test`) | Correctly headless — this is a dev/ops tool, not meant to have a UI. |

Everything in the first six rows is real, tested, working backend logic that a business user cannot reach today. Closing this gap is almost pure frontend work against already-correct APIs — likely the best return on effort of anything in this document.

---

## 4. Confirmed Missing — Never Built, Not Just Incomplete

Cross-referencing MB §4 (masters), MB §Recommended Module Coverage / FB §4, against the DocType seed list in `db/migration.sql` and the sidebar:

| Area | Spec source | Status |
|---|---|---|
| **Approval / Workflow Engine (maker-checker)** | MB §3 &amp; AB §5 — both name this as one of the ~11-13 "build first" common engines | **Confirmed absent.** No `approval_log` table, no workflow/approval code anywhere in `engines/` or `main.go` — the only hit for "approval"/"workflow" in the whole repo is a mention in `docs/implementation_plan.md` (a planning doc, not code). Documents carry a flat `status` field (e.g. `Draft,Approved,Closed`) with no approver, no amount-slab routing, no maker-checker segregation. This is the most structurally consequential gap: PO approval, vendor bank-change approval, payment approval, and price-change approval (all explicitly required by MB/SEC-V2) all depend on this engine existing. |
| **Vendor / Customer as real masters** | MB §4.5 (dedicated Vendor/Customer/Supplier masters with GSTIN/bank/loyalty fields) | **Confirmed absent (verified 2026-07-13 against the live DB).** `db/migration.sql` seeds no `Vendor` or `Customer` doctype — `vendor` and `customer` only exist as free-text `Data` fields on `PurchaseOrder`/`SalesInvoice` (`db/migration.sql:333,350`). The sidebar's "Vendors" menu item raised the possibility one was added later via the runtime DocType Builder, but `SELECT name FROM tenant_default.doctype_meta WHERE name ILIKE '%vendor%' OR name ILIKE '%customer%' OR name ILIKE '%supplier%'` against the live dev DB returns zero rows — no such doctype exists there either. The "Vendors" sidebar screen is reading/writing the free-text field, not a real master. |
| **GST / Tax Engine** (HSN-driven GST calc, IRN e-invoice, e-way bill) | MB §12.4, named as a first-class engine in MB §3 | **Confirmed absent.** No HSN/GST-rate calculation code, no IRN/e-invoice integration, no e-way bill logic anywhere in `engines/`. |
| **CRM / Loyalty, HR, Expense, Fixed Assets, Manufacturing/Job-Work** | MB §16 (HR/Expense/Assets), MB/FB module coverage tables | **Confirmed absent.** No doctypes, no engines, no menu items for any of these five modules. |
| **Reports module** | MB §17.1 names ~80 specific reports across 15 categories | **~4 of ~80 exist.** Only replenishment suggestions, demand forecast, SLA breaches, and trial balance (`engines/optimization.go`, `engines/finance.go`) — all API-only, no report-browsing UI, no filters/export/drilldown framework described in MB §17.1/§19.2. |
| **RFQ / Vendor Quote / Quote Comparison** | MB §8.3 | **Confirmed absent.** Procurement goes straight to `PurchaseOrder`; no RFQ or quote-comparison doctype/screen. |
| **Sticker/Barcode printing** | MB §15.3/AB §11.9 | **Confirmed absent.** No print-template, printer-mapping, or print-queue code. |
| **Remaining industry packages** | AB §9 table names 7-8 packages (Jewellery, Apparel, Pharma, F&amp;B, Automobile, Construction, Steel) | Only 4 exist (`public/profiles/jewelry.json`, `food_bev.json`, `auto.json`, `clothing.json` — confirmed via directory listing). Pharma, Construction, Steel/Metal have no profile file. Already tracked in `micro_checklist.md` Stage 12.1. |
| **Backup / Disaster Recovery** | SEC-V2 §14 (entire new domain: automated backup, encryption, monthly restore test, RPO/RTO, runbook) | **Confirmed absent** — already correctly tracked as re-opened in `micro_checklist.md` Stage 12.2. No duplicate tracking needed, just cross-referencing here since SEC-V2 gives it the most detail of any source document. |

---

## 5. Security Gap — SEC-V2 Checklist vs Verified Code

`docs/hardening_roadmap.md` closed 17 real security/correctness issues this session (auth bypass, SQL injection, CORS wildcard, JWT secret/expiry, stock floor-check race, tenant provisioning credential reuse, and more — all independently verified live). Cross-referencing what's *left* against SEC-V2 specifically (the authoritative, more detailed checklist — see the extraction's own V1-vs-V2 diff for what V2 added/dropped):

| SEC-V2 requirement | Status | Evidence |
|---|---|---|
| MFA mandatory for admin/finance/IT/super users/production support (§12) | **Confirmed absent** | No `MFA`/`TOTP`/`otp` hits anywhere in `*.go` |
| Security headers: HSTS, CSP, X-Frame-Options/frame-ancestors, X-Content-Type-Options (§8) | **Confirmed absent** | No matches anywhere in `*.go` — `apiMiddleware` sets CORS headers but no security headers |
| Object-level authorization / IDOR checks with the 4 specific risk scenarios in §4 | **Partial** | RBAC + tenant-schema isolation blocks cross-tenant reads structurally (schema-per-tenant is actually a stronger mechanism than SEC-V2 assumes), but there's no per-document ownership/location check independent of role — e.g. nothing stops a store-scoped user from reading another store's document by ID if their role otherwise allows the doctype |
| Webhook signature + timestamp validation (§5) | **Confirmed absent** | Already tracked in `micro_checklist.md` Stage 9.2 — no signature check on the Shopify order webhook |
| Finance maker-checker (§11: vendor bank change approval, payment exception approval, duplicate UTR blocking, posted GST/TDS field locking) | **Confirmed absent** | Depends entirely on the missing Approval/Workflow Engine (§4 above) |
| Audit log immutability ("append-only, non-editable from UI") (§9) | **Claimed done** | `micro_checklist.md` Stage 1.2 claims DB triggers enforce this; not independently re-verified in this pass — worth a direct trigger check before relying on it for a compliance claim |
| Per-API-type rate limiting (§5's 9-row table: distinct limits for search/report/export/bulk-upload/webhook/GST APIs) | **Partial** | Current limiter is 2-tier (5/min login, 60/min everything else) — matches the *mechanism* SEC-V1 asks for (429 + exact-ish message) but not SEC-V2's per-API-type granularity |
| Sensitive column masking (mobile/email/bank/cost price) on export (§10) | **[unverified]** — no report/export framework exists yet to check against (see §4 above) |
| Backup, DR, RPO/RTO (§14) | **Confirmed absent** — see §4 above |
| CSRF | **Not applicable, correctly** | Auth is a Bearer token in `Authorization`, never a cookie — `micro_checklist.md` Stage 1.3 already documents this reasoning correctly |

---

## 6. Spec-Internal Contradictions Worth Knowing About

These aren't code bugs — they're places where MB and AB (both nominally describing the same system) disagree with each other. Worth knowing so nobody "fixes" the code to match one spec and breaks alignment with the other:

- **Document status enum differs.** MB §6.2 has 9 statuses including a standalone "On Hold" and separate "Fully Processed"/"Closed". AB §6 collapses this to a shorter linear chain with no "On Hold" and a combined "Completed/Closed". The actual code's status values (e.g. PurchaseOrder's `Draft,Approved,Closed`) are closer to AB's shorter model but match neither exactly.
- **Integration log schema differs.** MB §13.1 uses `status: Success/Failed/Retrying/Ignored/Duplicate` + separate `error_code`/`error_message`. AB §14 uses `status: Pending/Success/Failed/Retried/Cancelled` + a combined `error_summary`, and adds `tenant_id`/`severity` fields MB doesn't have. Worth checking which one `integration_event_log`'s actual schema follows before building the Log Hub UI against it.
- **Error response envelope differs.** MB §18.3 has no `correlation_id` in its example; AB's Appendix A adds `correlation_id` to both success and error shapes. If correlation-ID-based tracing is wanted (SEC-V1 explicitly asks for it, SEC-V2 drops it — another V1/V2 divergence), AB's envelope is the one to standardize on.

---

## 7. Doc Corrections Made This Pass

Two claims in `docs/ai_handover.md` §6 were checked against source and found stale (already fixed earlier this session, doc just wasn't updated to say so):
- "Token expiry (Phase 1.4) is still open" → **false**, `tokenTTL()`/`JWT_EXPIRY_HOURS`/`exp` claim verified present in `engines/auth.go`
- "Provisioning currently clones the shared placeholder password hash" → **false**, `generateRandomPassword()` verified present in `engines/saas.go`, unique bcrypt hash per tenant

Both corrected in place (see `ai_handover.md` diff). One claim was checked and found **still accurate, not stale**: the `prompt()` migration gap (DocType Builder, Prefix Config edit) — 6 raw `prompt()` calls confirmed still present in `public/app.js:804-806,963-966` despite the "Replace native alert, confirm, and prompt boxes" commit; that commit covered `alert`/`confirm` only. Left as-is, no change needed.

---

## 8. Rough Completion Estimate

Caveat: these are rough, category-level estimates for planning purposes, not a precise metric.

| Category | Estimate | Basis |
|---|---|---|
| Kernel/architecture (AB's scope) | ~75-80% | Core engines built; MFA, security headers, maker-checker are the main holes |
| Omnichannel/scale backend (OB's scope) | ~70% | Very strong for a first pass; missing dead-letter-queue UI, reconciliation-snapshot table, richer allocation-rule factors |
| Functional module breadth (MB's full scope) | ~20-25% | Master data/procurement/inventory/transfers/POS-backend/finance-backend exist; POS/Finance/GST/CRM/HR/Assets/Manufacturing/Reports UI mostly don't |
| Security (SEC-V2's scope) | ~55% | 17 real fixes landed this session; MFA, headers, maker-checker, per-API-type limits, backup/DR remain |

**If the question is "can this run a real jewellery retail business end-to-end today?"** — not yet: there's no POS screen to actually sell something and no Finance screen to see the books, even though both have working APIs underneath.

---

## 9. Recommended Phased Plan

Ordered by risk/effort ratio, not by PDF section order. This is a proposal for the user to prioritize, sequence, or descope — not a commitment to build.

### Phase A — Cheap, high-risk-reduction security items
1. Add security response headers (HSTS, CSP, X-Frame-Options, X-Content-Type-Options) in `apiMiddleware` — a small, self-contained middleware change.
2. Add webhook signature + timestamp validation to the Shopify order webhook (`engines/sourcing.go` `ImportChannelOrder` / its `main.go` handler).
3. Wire `IsFeatureEnabled` into at least the routes it's meant to gate (currently set but never checked — `micro_checklist.md` Stage 12.1).
4. MFA for HR/Admin and Finance-equivalent roles — biggest lift in this phase, but explicitly "mandatory" per SEC-V2 §12.

### Phase B — Frontend for logic that already exists (highest ROI)
5. POS billing screen against the existing `/api/v1/checkout` + availability/reservation APIs.
6. Finance/GL screen against `/api/v1/finance/trial-balance` + GL posting history.
7. Wire the Log Hub screen to the already-built `/api/v1/integration/logs` and `/api/v1/integration/retry` endpoints (re-opened item in `micro_checklist.md` Stage 9.2 — this is specifically a UI wiring task, backend is done).
8. Fulfillment/reservation workbench (pick/pack/dispatch) against `/api/v1/fulfillment/*`.
9. Marketplace settlement screen against `/api/v1/marketplace/*`.

### Phase C — The structural gap: Approval/Workflow Engine
10. Build the maker-checker/approval engine (`approval_log` table, amount-slab + role + location routing, re-approval-on-edit triggers) — named as a "build first" engine in both MB and AB, and a hard dependency for PO approval, vendor bank-change approval, and SEC-V2's entire finance maker-checker domain (§11).

### Phase D — Functional module breadth
11. Dedicated Vendor/Customer masters (replacing the free-text fields) with GSTIN/bank/loyalty fields per MB §4.5 — confirmed absent by the live-DB check in §4 above, so this is a clean build, not a migration of existing data.
12. GST/Tax engine — HSN-driven rate calc at minimum; IRN/e-way as a stretch.
13. Expand the report catalog beyond the current 4, prioritizing the ones MB marks as operationally critical (Current Stock, Sales Register, Vendor Ledger, Payables Ageing).
14. CRM/Loyalty, HR, Expense, Assets, Manufacturing — lowest priority; scope with the user before starting since these are large modules with no existing backend scaffolding at all.

### Phase E — Remaining ops/scale hardening
15. Backup/DR automation (already tracked, `micro_checklist.md` Stage 12.2).
16. Remaining industry profile packages (Pharma, Construction, Steel).
17. Per-API-type rate limiting granularity (search/report/export/bulk-upload distinct from generic 60/min).
18. Sticker/barcode printing module.

---

## 10. Cross-References

- Security/correctness fixes already closed this session: [`docs/operations/hardening_roadmap.md`](../operations/hardening_roadmap.md)
- Granular build tracker (Stage 1-12, matches OB's rollout plan): [`docs/micro_checklist.md`](../micro_checklist.md)
- Developer setup + handover notes: [`docs/ai_handover.md`](../ai_handover.md)
- Historical build log: [`docs/project_ledger.md`](../project_ledger.md)
