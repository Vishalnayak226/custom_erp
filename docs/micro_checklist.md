# In-House ERP: Micro-Checklist & Build Tracker

This checklist tracks the implementation of the In-House ERP Kernel and pluggable modules at a micro-level. Developers can mark tasks as `[/]` (In Progress) and `[x]` (Completed) to evaluate builds and trace milestones.

> **Correction pass (2026-07-12)**: a source-level audit found several items below had been marked `[x]` without a matching implementation. Those are re-opened as `[ ]` with a strikethrough note explaining what's actually missing and a pointer to `docs/hardening_roadmap.md` for the fix. Everything else was spot-checked against the current code and left as-is.
>
> **Scope note (2026-07-12)**: Stages 1-12 below track the build against the **Omnichannel Scale Add-on Blueprint's** rollout plan (kernel → single vertical pilot → omnichannel sync → fulfillment → scale test → marketplace expansion → optimization) — that's why POS/Finance/Reports read as "Completed" here despite having no user-facing screen: those stages tracked backend/API completion, not full functional build-out. The **Master Blueprint's** wider functional module list (POS billing screen, Finance/GL screen, GST/e-invoice, CRM, HR, Assets, Manufacturing, ~80-report catalog, RFQ/quote comparison, approval/maker-checker workflow engine) was never in this tracker's scope at all — see the new **Stage 13** below and the full writeup in [`docs/pdf_blueprint_gap_analysis.md`](pdf_blueprint_gap_analysis.md).

---

## 🚀 Stage 1 - Core Foundation (Completed)

- [x] **1.1 Base Schema Migrations**
  - [x] Establish `.gitignore`, project folder structure, and dynamic log badges.
  - [x] Create `doctype_meta` registry schema table to hold core document parameters.
  - [x] Create `doctype_fields` table to define database-validated field constraints.
  - [x] Initialize standard system user tables and RBAC role permission schema mapping.
  - [x] Setup `system_error_logs` schema to handle panic recovery stack traces.
  - [x] Register WMS-relevant DocTypes metadata (`Item`, `PurchaseOrder`, `ASN`, `SalesInvoice`, `TransferOrder`).
- [x] **1.2 Core Engines Core Logic**
  - [x] **Audit Engine**: Setup database triggers to log modifications (old value, new value, user, time). Enforce append-only, non-editable audits.
  - [x] **Panic Handler Middleware**: Configure route catch block to capture crashes and write stack traces to the log database.
- [x] **1.3 API Security & Gateway Foundation**
  - [x] **Gateway Rate Limiting**: Implement token bucket throttling (limit public logins to 5/min per IP, standard CRUD to 60/min per token).
  - [x] **Strict Tenant Resolution**: Enforce backend-only JWT verification mapping `tenant_id` securely (prevents IDOR leaks). Fully re-closed 2026-07-12: the "missing header → admin" fallback is fixed (Phase 1.1) and issued tokens now expire (Phase 1.4, default 24h, `JWT_EXPIRY_HOURS` overridable).
  - [x] **Prepared Parameterization**: Mandate parameterized SQL queries across all operations (blocks SQL injections). Re-closed 2026-07-12: the generic doc-list filter's query-parameter *key* is now allowlisted against a strict identifier pattern before it touches the SQL string (`hardening_roadmap.md` Phase 1.2, verified).
  - [x] **Payload Size Controls**: Enforce HTTP request size limits (max 2MB body limit) and file size/MIME type validation.
  - [x] **CSRF & CORS policies**: Setup SameSite cookies, CSRF tokens, and enforce strict, non-wildcard CORS domains in production. Re-closed 2026-07-12: CORS now uses an explicit allowlist (`hardening_roadmap.md` Phase 1.5, verified) instead of reflecting any `Origin`. The SameSite-cookie/CSRF-token half doesn't apply as written — auth here is a Bearer token in the `Authorization` header (stored client-side, never a cookie), not a cookie-based session, so classic CSRF (which relies on the browser auto-attaching cookies cross-origin) isn't the relevant threat model; the CORS allowlist is what actually prevents a malicious page from getting a browser to carry an authorized request to this API.
  - [x] **Secrets Protection**: Scan codebase for hardcoded keys and store configs in env variables. Re-closed 2026-07-12: `engines/auth.go`'s signing secret is no longer a literal — `JWT_SECRET` env var if set, otherwise an auto-generated secret persisted outside the repo (`hardening_roadmap.md` Phase 1.3, verified).
  - [x] **Object-Level Authorization**: Enforce user location, role, and document ownership checks on every fetching and editing API.
  - [x] **Environment Credentials**: Move database connection credentials to `DATABASE_URL` environment variables.
  - [x] **SSO Claim Alignment**: Enforce token claim keys consistency containing `tenant`, `role`, and `loc` properties.
- [x] **1.4 Base API Endpoints**
  - [x] Implement generic CRUD handler `GET /api/v1/doc/:doctype` supporting master, transaction, and child table reads.
  - [x] Implement `GET /api/v1/doc/:doctype/:id` and `POST /api/v1/doc/:doctype` with dynamic field validation rules.
  - [x] Implement prefix config api (`GET /api/v1/prefix` & `POST /api/v1/prefix`).
  - [x] Implement labels translation api (`GET /api/v1/labels` & `POST /api/v1/labels`).
  - [x] **Dynamic GET Filters**: Support filtering JSONB document keys directly from URL query parameters (e.g., `?status=Approved`).
  - [x] **GRN Callback Hook**: Intercept `POST /api/v1/doc/GRN` to update stock levels atomically in `inventory_availability`.
- [x] **1.5 Omnichannel Scale Foundation**
  - [x] **Base Schema Migrations**: Create tables: `inventory_availability`, `inventory_reservation`, `integration_event_outbox`, `integration_event_log`, and `dead_letter_queue`.
  - [x] **Event Bus & Outbox Pattern**: Implement dynamic outbox publishing triggers and background worker polling event logs.
  - [x] **Inventory Availability Read Model**: Create calculated Available-to-Sell (ATS) inventory read model service API.
  - [x] **Reservation Service**: Implement in-memory or database-backed temporary stock reservation checks and lockouts.

---

## 🎨 Stage 2 - Dynamic Configuration (Completed)

- [x] **2.1 Core Schema Builders**
  - [x] **DocType Builder UI**: Create the admin customizer panel allowing users to add custom columns, toggle mandatory rules, and define display order.
  - [x] **Parent-Child Vocabulary Aliasing**: Configure abstract database key mappings (`parent_document_id` / `child_document_id`) to support client-customized nomenclature.
  - [x] **Numbering Engine**: Implement dynamic prefix, separator, padding width, and monthly/annual sequence resets. Enforce dynamic variant/child concatenation formulas.
  - [x] **Dynamic Label Engine**: Build case-insensitive text translation cache mapping original labels to display overlays.
- [x] **2.2 Dynamic Form Rendering Engine**
  - [x] Implement dynamic JSON meta response reader (`GET /api/v1/doc/:doctype/meta`).
  - [x] Build React/Vue component generator drawing text, number, date, select, link, attachment, table, and scan fields on the fly.
  - [x] Parse parent-child vocabulary maps to translate model references dynamically on forms and lists.
  - [x] Implement rename fields UI overriding default labels (e.g. changing "Polish" to "Fabric" or "Engine Type").
  - [x] Implement toggles to configure list view column visibility dynamically.

---

## 📦 Stage 3 - Master Packages (Completed)

- [x] **3.1 Industry Profile Preset Migrations**
  - [x] Implement **Jewelry Preset**: load Brand, Style, Size, Color, and Polish fields.
  - [x] Implement **F&B / Beverage Preset**: load Brand, Batch, Expiry, Weight, and Temperature attributes.
  - [x] Implement **Automobile Preset**: load Make, Model, Engine Type, Fuel Type, and Serial VIN fields.
  - [x] Implement **Clothing Preset**: load Brand, Style, Size (S/M/L/XL), Fabric, and Color fields.
- [x] **3.2 Master Configurations**
  - [x] Setup Organization, Location, Item (parent/variant), Vendor, Customer, Employee, Tax, and GL master schemas.
- [x] **3.3 Bulk Uploads Engine**
  - [x] Implement Excel/CSV structure verification (checks column matching before processing rows). No direct silent imports.
  - [x] Build item import validation (validates HSN codes, duplicate keys, and category defaults).
  - [x] Setup row-level error log exports returning failed rows with comments.

---

## 🛒 Stage 4 - Procurement (Completed Backend Mappings)

- [x] **4.1 Procurement Docs**
  - [x] Register `PurchaseOrder` metadata and field validations.
  - [x] Link documents to statuses and tracks cancellation states.

---

## 💎 Stage 5 - GRN and Inventory (Completed Backend Mappings)

- [x] **5.1 GRN Reconciliation**
  - [x] Build GRN callback hooks and validate incoming item counts.
  - [x] Generate barcode numbers dynamically for accepted goods.
- [x] **5.2 Stock Ledger & Returns**
  - [x] Implement append-only fast availability read models.
  - [x] Process and validate item returns.

---

## 🚚 Stage 6 - Warehouse and Transfer (Completed Sourcing & Re-routing)

- [x] **6.1 Warehouse Logistics & Transfers**
  - [x] Implement location-scoping and automated order reservations.
  - [x] Configure rule-driven picking tasks and re-routing on rejection.

---

## 💳 Stage 7 - POS and Sales (Completed Webhooks & Checkouts)

- [x] **7.1 POS Checkout & Webhooks**
  - [x] Register `POSCart` and `SalesReturn` metadata schemas.
  - [x] Build POST /api/v1/checkout to deduct stock and complete checkout.
  - [x] Build POST /api/v1/integration/shopify/order webhook with idempotency checks.

---

## 📊 Stage 8 - Finance (Completed Balanced GL Ledger)

- [x] **8.1 Accounting Engine**
  - [x] Register `GLAccount` and `GLPosting` schema tables.
  - [x] Build balanced double-entry accounting routine validating debits == credits.
- [x] **8.2 Ledger Postings**
  - [x] Automate postings for GRNs, cashier checkouts, and returns.
  - [x] Expose GET /api/v1/finance/trial-balance.

---

## 🔌 Stage 9 - Tax and Integrations

- [/] **9.1 API Channel Syncs**
  - [x] Implement Shopify product/inventory mapping, delta stocks, and order webhooks.
  - [ ] ~~Implement Unicommerce inventory sync & multi-marketplace order ingestion.~~ Re-opened: no Unicommerce integration code exists anywhere in `main.go`/`engines/`.
  - [ ] ~~Implement Pine Labs Plutus payment terminal reconciliation checkouts.~~ Re-opened: no Pine Labs integration code exists.
  - [ ] ~~Implement CleverTap customer order event log syncing.~~ Re-opened: no CleverTap integration code exists.
  - [x] Implement Marketplace settlements (Shopify/Amazon) payout reconciliation and commission bookings.
  - [x] Implement logistics dispatch tracking bookings and carrier registration.
- [/] **9.2 Error Logs Hub**
  - [x] Build Log Hub screen displaying audit trails and system panic backtraces.
  - [ ] ~~Build Log Hub screen displaying integration payloads.~~ Re-opened: the backend endpoint (`GET /api/v1/integration/logs`) exists, but the frontend Log Hub view doesn't call it — no integration-payload list is actually rendered.
  - [ ] ~~Implement `Retry` buttons for failed payloads.~~ Re-opened: `POST /api/v1/integration/retry` exists on the backend, but no button/UI wires up to it.
  - [ ] ~~Verify signature tokens on incoming external webhooks and callbacks.~~ Re-opened: no signature verification exists on the Shopify order webhook or any other callback handler today.

---

## 📈 Stage 10 - Reports and Dashboards

- [x] **10.1 Reports Engine**
  - [x] Implement replenishment suggestions reports with safety stock and lead times parameters. Shares `CalculateSalesVelocity` with demand forecasting below — the status-mismatch fix applies here too.
  - [x] Implement demand forecasting projection reports. Status-mismatch fixed 2026-07-12 (`hardening_roadmap.md` Phase 2.2) — verified with a real checkout followed by a real forecast call returning correct non-zero demand.
  - [x] Implement picking task SLA breach monitoring alerts reports.
  - [x] Implement Trial Balance GL ledger balanced summaries reports.

---

## 🧪 Stage 11 - QA and Go-Live

- [/] **11.1 Test Coverage**
  - [x] Perform concurrency scale stress-testing (100 concurrent workers, 1,000 transactions across 2,000 store nodes).
  - [x] Run UAT scripts mapping end-to-end checkouts, settlements, logistics bookings, replenishment reorders, and SLA breaches. A real HTTP-level integration test now exists (`main_test.go`, `hardening_roadmap.md` Phase 3.1) driving login → checkout → forecast through the actual handlers — proven to catch the Phase 10.1 status-mismatch class of bug by deliberately reintroducing it and confirming the test fails.
  - [x] Validate database schema integrity and run trial migrations.
  - [ ] ~~Execute security validation checklists (cross-tenant role boundaries and token verification).~~ Re-opened: the auth-bypass and SQL-injection findings above mean this hasn't actually been done to a passing bar yet. See `hardening_roadmap.md` Phase 1.

---

## 🚀 Stage 12 - Multi-Industry Scale

- [/] **12.1 Multi-Tenant SaaS Operations**
  - [x] Deploy automatic tenant provisioning workflows. Each new tenant now gets a unique, freshly generated admin credential (2026-07-12, `hardening_roadmap.md` Phase 1.6, verified) rather than a cloned shared hash. Restricted to `HR/Admin` callers only (Phase 1.7, verified).
  - [x] Setup feature flag controls per tenant. *(The setter is wired to an API route; `IsFeatureEnabled` isn't called from any request handler yet, so flags don't gate anything in practice — tracked as its own actionable item at Stage 13.2.)*
  - [ ] ~~Load remaining industry templates (Pharma, Metal, Construction, etc.).~~ Re-opened: only the original 4 profiles exist (`jewelry`, `food_bev`, `auto`, `clothing`) in `public/profiles/`; Pharma/Metal/Construction/Medical/Semiconductor/Agriculture from `industry_plugs.md` have no corresponding profile file.
- [/] **12.2 Intellectual Property & Binary Safety**
  - [x] Obfuscate, minify, and bundle frontend SPA scripts. Sourcemap gap closed 2026-07-12 (`hardening_roadmap.md` Phase 4.2) — no more `--sourcemap` flag, and the previously-committed `.map` was untracked and gitignored along with the rest of `public/dist/`.
  - [x] Strip debug tables and symbols from release Go binaries (`go build -ldflags="-s -w"`). Closed 2026-07-12 — `.\manage.ps1 release` does this now (`hardening_roadmap.md` Phase 4.1), measured 9,446 KB → 6,602 KB.
  - [ ] ~~Setup automated backups, encryption, and monthly recovery test drills.~~ Re-opened: no backup, encryption, or recovery automation exists anywhere in the codebase today.

---

## 🧩 Stage 13 - Master Blueprint Functional Scope (Not Started)

Added 2026-07-12 after a full gap analysis against all 6 spec PDFs (`docs/pdf_blueprint_gap_analysis.md`). Unlike Stages 1-12, these items were never scoped into a build phase before now — they're new to this tracker, not re-opened.

**Reorganized 2026-07-13** into Phases A-E to mirror `docs/pdf_blueprint_gap_analysis.md` §9's recommended risk/effort-ordered sequencing (a prioritization proposal for the user, not a commitment to build). Two items that phased plan calls out are already tracked elsewhere and aren't duplicated here — see the cross-references inline. Also split out two items that were previously bundled into one line and, on reflection, have very different effort/priority: security response headers (cheap, high ROI) vs. MFA (biggest lift in Phase A) were one checklist line; feature-flag route gating wasn't tracked as its own actionable item at all, only as a footnote on Stage 12.1's `[x]` line.

### Phase A — Cheap, high-risk-reduction security
- [x] **13.1 Security response headers** (HSTS, CSP, `X-Frame-Options`/`frame-ancestors`, `X-Content-Type-Options`) — **DONE (2026-07-13).** Added `securityHeaders` middleware in `main.go`, wrapping the entire mux (static files + API alike) so it applies uniformly rather than depending on every route remembering `apiMiddleware`. CSP allows `'unsafe-inline'` for script-src/style-src (the UI uses `onclick="..."` attribute handlers throughout — 21 occurrences — and CSP treats those as inline script; refactoring them to `addEventListener` is a separate frontend task, not part of this change) and explicitly allow-lists `fonts.googleapis.com`/`fonts.gstatic.com`, the only external resource the app loads (`public/styles.css`'s Google Fonts `@import`). Verified live: built and ran a throwaway instance on a separate port (8099, never touched the live 8080 server), confirmed all five headers present on both a static asset response and an API response, confirmed real login still succeeds (200) and `app.js`/`styles.css` still load (200) with CSP active. Not verified in an actual browser (no browser-automation tool available in this session) — the CSP's `connect-src`/`img-src`/`font-src` allow-lists were derived from grepping every `fetch(`/image/font reference in the codebase, not just asserted.
- [x] **13.2 Wire `IsFeatureEnabled` into the routes it's meant to gate** — **DONE (2026-07-13).** Added a `featureGate(featureName, handler)` wrapper in `main.go` (composes inside `apiMiddleware`, since it reads `Resolved-Tenant-ID`) and applied it by matching each of the 3 seeded flags to the route group its name describes: `oms_integration` → Shopify product/order webhooks + marketplace settlement/logistics booking; `wms_integration` → fulfillment task transition/return; `advanced_forecasting` → replenishment suggestions/SLA breaches/demand forecast. All 3 flags default `TRUE` in `db/migration.sql`, so no existing tenant's behavior changes unless they explicitly disable one via the already-working `POST /api/v1/admin/tenant/feature-flag` endpoint - this makes the toggle actually take effect rather than adding new restrictions. Fails closed (blocks) if the flag can't be positively confirmed enabled, matching `IsFeatureEnabled`'s own default for an unregistered flag. Verified live end-to-end against a throwaway instance (port 8099, live 8080 server untouched): `sla-breaches` returns 200 with the flag at its default enabled state, disabling `advanced_forecasting` via the real API immediately turns it into a 403 with a clear error message, re-enabling restores 200, and an unrelated route (`/api/v1/availability`) is unaffected throughout. Left the live dev DB's flag states exactly as found (ended back at `enabled=true`).
- [x] **13.3 MFA** for HR/Admin, Finance-equivalent, and production-support roles — **DONE (2026-07-13).** TOTP (RFC 6238), implemented from stdlib only (`crypto/hmac`+`crypto/sha1`+`encoding/base32`, `engines/mfa.go`) rather than adding a dependency, matching the project's stated lightweight philosophy. This codebase has no distinct Finance/IT role - `HR/Admin` is the only privileged role, so it stands in for SEC-V2's whole "admin/finance/IT/super users" group (`RequiresMFA`, `engines/mfa.go`). `POST /api/v1/login` now routes an MFA-required role into enrollment (first time, `mfa_enabled=false`) or a TOTP challenge (subsequent logins) instead of ever handing out a full session token directly - both via a new narrowly-scoped, short-lived `SignPurposeToken` (`engines/auth.go`, excludes role/loc, 5-10min TTL) that only unlocks the matching new endpoint: `POST /api/v1/auth/mfa/enroll` (issue a pending secret), `POST /api/v1/auth/mfa/activate` (confirm + activate + issue the real session token), `POST /api/v1/auth/mfa/verify` (challenge an already-enrolled account). Both new endpoints are rate-limited at the same strict 5/min tier as `/login` (brute-forcing a 6-digit code). Added `mfa_secret`/`mfa_enabled` columns to `users` (`db/migration.sql`, applied to both live tenant schemas). Also built the frontend enrollment/challenge screens (`public/index.html`, `public/app.js`) - a backend-only MFA mandate would have broken the only admin login path with no way to complete it, so this wasn't optional polish. No QR code image (would need a client-side QR-encoding library, another dependency, and CSP already blocks external ones) - shows the manual-entry secret instead, which every authenticator app also accepts.
  - Caught and fixed a real bug during verification: `apiMiddleware` unconditionally re-resolves `tenantID` from the bearer token's `tenant` claim, but the first version of `SignPurposeToken` didn't include one - silently blanking tenant scoping for the entire MFA flow. Fixed by including `tenantID` in the purpose token too; verified the resulting session token carries the correct tenant afterward.
  - Verified live end-to-end against throwaway instances (never the live 8080 server) using disposable test users (deleted after, real seed accounts' MFA state confirmed untouched): non-MFA role (`cashier1`) still logs in single-step; fresh HR/Admin login returns `mfa_enrollment_required`; enroll returns a secret; activate rejects a wrong code (401) and accepts the real computed code (200, issues a working session token usable on a real protected route); a second login for the now-enrolled user returns `mfa_required` (challenge, not enrollment) and verify behaves the same way; a purpose-token can't be used against the wrong MFA endpoint (403). Also simulated the frontend's exact request sequence (same headers/body shapes `app.js` sends) end-to-end and confirmed every response field name lines up with what the UI reads. Not checked in an actual browser - no browser-automation tool available in this session.
  - `go build`/`vet`/`test ./...` all clean; `main_test.go`'s integration test now completes real MFA enrollment as part of login (it uses an HR/Admin test user), so it also doubles as a regression test for this flow.
- *(Webhook signature + timestamp validation — already tracked as a re-opened item, Stage 9.2.)*

### Phase B — Frontend for logic that already exists (highest ROI)
- [x] **13.4 POS billing screen** — **DONE (2026-07-13).** New "POS / Billing" sidebar screen (`public/index.html`/`public/app.js`) wired directly against the already-working `GET /api/v1/availability` and `POST /api/v1/checkout` APIs - no new backend code needed, confirming the gap analysis's call that this was "almost pure frontend work against already-correct APIs." Flow: enter a location code, scan/type a SKU (Enter submits, matching how USB/Bluetooth barcode scanners emulate keyboard input - no camera-scanning library needed), which looks up live availability and adds a cart line; qty/sale price/cost price are editable inline (no price-list/item-master pricing exists yet - flagged as a separate, not-yet-scoped gap, not part of this item); "Complete Sale" posts the whole cart to checkout in one call, matching how `handleCheckout` already expects to receive it. Kept independent of the generic DocType-table view since a cart is built up client-side line by line, not a plain CRUD record. No new dependency, no new backend endpoint - purely additive UI.
  - Verified live end-to-end against a throwaway instance (port 8099, live 8080 server untouched): logged in as `cashier1` (POS's real operator role), seeded real inventory, confirmed the availability lookup response shape matches exactly what `addSKUToPOSCart` reads (`ats`), submitted a checkout with the exact JSON body `submitPOSCheckout` sends, confirmed a 200 with the same fields `showCustomAlert` reads (`cart_number`, `sale_total`), and confirmed stock actually decremented in the DB (30 -> 25 for a qty-5 sale). Test inventory/document rows cleaned up after. `node --check public/app.js` clean. Not checked in an actual rendered browser - no browser-automation tool available in this session; verified via the exact request/response contract instead.
- [x] **13.5 Finance/GL screen** — **DONE (2026-07-13).** New "Finance / GL" sidebar screen, a read-only trial balance view against `GET /api/v1/finance/trial-balance`: summary tiles for total debits/credits and a green/red balanced-status indicator, plus the full per-account table. No new backend code needed. Verified live against a throwaway instance (port 8099) that the real endpoint's response field names (`account_code`, `account_name`, `account_type`, `debit`, `credit`, `total_debits`, `total_credits`, `status`, `balanced`) match exactly what the view reads.
- *(Log Hub wiring to `/api/v1/integration/logs` + `/retry` — already tracked as a re-opened item, Stage 9.2.)*
- [ ] **13.6 Fulfillment/reservation workbench** — task transitions and re-routing work via API; no pick/pack/dispatch UI.
- [ ] **13.7 Marketplace settlement screen** — settlement reconciliation works via API; no UI.

### Phase C — The structural gap
- [ ] **13.8 Approval / Workflow Engine (maker-checker)** — named as a "build first" common engine in both the Master Blueprint and Approach Blueprint. No `approval_log` table or approval/workflow code exists anywhere. Blocks PO approval, vendor bank-change approval, payment approval, and the entire finance maker-checker domain in the security checklist (SEC-V2 §11).

### Phase D — Functional module breadth
- [ ] **13.9 Dedicated Vendor/Customer masters** — confirmed absent 2026-07-13 by a live-DB check (`SELECT name FROM tenant_default.doctype_meta WHERE name ILIKE '%vendor%' OR ...` returns zero rows); currently only free-text fields on `PurchaseOrder`/`SalesInvoice`. Needs GSTIN/bank/loyalty fields per MB §4.5.
- [ ] **13.10 GST/Tax engine** — no HSN-driven GST calculation, IRN e-invoice, or e-way bill logic anywhere.
- [ ] **13.11 Report catalog expansion** — ~4 of the ~80 reports named in the Master Blueprint exist (replenishment, forecast, SLA breach, trial balance), all API-only with no report-browsing UI. Prioritize Current Stock, Sales Register, Vendor Ledger, Payables Ageing first.
- [ ] **13.12 RFQ / Vendor Quote / Quote Comparison** — procurement goes straight to PurchaseOrder today. Not explicitly phased in the gap analysis's §9 plan; grouped here as functional breadth.
- [ ] **13.13 CRM/Loyalty, HR, Expense, Fixed Assets, Manufacturing/Job-Work** — no doctypes, engines, or screens for any of these five modules. Largest, lowest-priority item — scope with the user before starting.

### Phase E — Remaining ops/scale hardening
- *(Backup/DR automation — already tracked as a re-opened item, Stage 12.2.)*
- *(Remaining industry profiles: Pharma, Construction, Steel — already tracked as a re-opened item, Stage 12.1.)*
- [ ] **13.14 Per-API-type rate limiting granularity** — SEC-V2 §5 specs distinct limits per API type (search/report/export/bulk-upload/webhook/GST); current limiter is a flat 2-tier 5/min-login + 60/min-everything-else.
- [ ] **13.15 Sticker/barcode printing module** — no print-template or printer-mapping code.
