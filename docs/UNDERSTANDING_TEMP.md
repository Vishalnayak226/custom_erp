# ERP Project — Current Understanding (TEMP)

> **Status: temporary review artifact.** This file exists so you can check my understanding of the codebase against reality. Every claim below was checked against actual source (not just the existing docs) as of 2026-07-11. Once you've corrected anything wrong, we'll fold the accurate parts into the real docs and delete this file.

---

## 1. What this project actually is right now

A **Go 1.22 monolith backend** (`main.go` + `engines/*.go`) backed by **PostgreSQL with schema-per-tenant isolation**, serving a **vanilla JS SPA** from `public/`. It implements a metadata-driven "DocType" pattern (similar to Frappe/ERPNext): most business objects are stored as generic `documents` rows (`id`, `doctype`, `data JSONB`, `status`) validated against a `doctype_fields` metadata table, rather than one dedicated SQL table per document type.

- **Run**: `erp-server.exe` (built via `go build -o erp-server.exe`) serves both the API and the static `public/` folder on `http://localhost:8080`.
- **DB**: Portable PostgreSQL 16.3 on port `5435`, database `custom_erp`, default schema `tenant_default`.
- **The project has visibly pivoted**: the repo root `app.js`/`db.js`/`index.html`/`styles.css` (old client-only prototype with mock in-memory data) were deleted and replaced by the Go backend + `public/` folder. **`README.md` and `package.json` were not updated** — they still describe the old `npx http-server` prototype and don't mention Go/Postgres at all. (See §5.)

## 2. Architecture, verified against code

- **Multi-tenancy**: `db.GetTenantSchema(tenantID)` maps a tenant ID to a Postgres schema (default `tenant_default`); `db.SetSearchPath(tx, schema)` scopes a transaction. Pattern is consistent but **mixed**: some queries explicitly qualify `%s.table`, others rely on `SET LOCAL search_path` and use unqualified table names — both work, but it means correctness in new code depends on remembering which pattern a given function uses.
- **DocType kernel**: `engines/doctype.go` — `doctype_meta` (registers a document type) + `doctype_fields` (field-level rules: mandatory, type, `Select` options, `Link` target). `ValidateDocument()` enforces these server-side on every generic write. This part is real and working, not just aspirational.
- **Generic CRUD**: `GET/POST/DELETE /api/v1/doc/{doctype}[/{id}]` in `main.go` — handles arbitrary doctypes uniformly, checks RBAC (`role_permissions` table) and a location filter for non-admins.
- **Numbering**: `engines/numbering.go` — `SELECT ... FOR UPDATE` row lock on `sequence_counters`, so sequence generation is safe under concurrency. Verified this is real (not just a claim) by reading the transaction logic.
- **Finance**: `engines/finance.go` — every posting goes through `PostDoubleEntry()`, which hard-fails if `sum(debits) != sum(credits)`. Checkout, GRN, returns, and marketplace settlements all route through it.
- **Inventory**: `engines/inventory.go` — `inventory_availability` read model with `on_hand / available / committed / reserved / safety_stock`; ATS = `available - reserved - safety_stock`. Reservations (`CreateReservation`) check ATS before inserting. This logic is real and matches the docs' description.
- **Omnichannel**: `engines/sourcing.go` (`FindBestFulfillmentNode`, Shopify order import with idempotency via `channel_order_mapping`), `engines/fulfillment.go` (pick tasks, reject → re-route via `FindBestFulfillmentNode` → new reservation + new task), `engines/outbox.go` (background poller every 5s).
- **Marketplace**: `engines/marketplace.go` — settlement reconciliation validates `total - commission == net` before posting GL entries; logistics booking is a simple tracking record. This module exists and matches the ledger docs claim it as "Phase 6 completed" — **note**: `implementation_plan.md` §7 lists Phase 6 as `[PENDING]` while `project_ledger.md`/`ai_handover.md` say it's `COMPLETED`. The code backs up the "completed" claim (it's built and wired to routes), so `implementation_plan.md` looks stale on this specific point.
- **Scale test**: `engines/scale.go` — real concurrent worker-pool simulation (goroutines + channels), computes p50/p95/p99 from actual measured durations, not fabricated numbers.
- **Frontend**: `public/app.js` (1042 lines) is **entirely API-driven** — every data operation goes through `fetch()`/`apiFetch()` to `/api/v1/...`. I grepped it directly to confirm this.

## 3. Dead / stale artifacts found (concrete, verified)

- **`public/db.js`** — the old mock `INITIAL_ERP_DATA` object (fake brands/styles/colors). `index.html` still `<script src="db.js">`-includes it, but a grep of `app.js` shows **zero references to `INITIAL_ERP_DATA`**. It's dead code, loaded but unused.
- **`package.json`** — `"scripts": {"start": "npx http-server -p 8080"}`. This does not start the actual system anymore (that's `erp-server.exe`, per `ai_handover.md`). Running `npm start` today would serve `public/` as static files with no backend behind it.
- **`README.md`** — describes the deleted root-level `app.js`/`db.js` prototype, not the current Go/Postgres architecture. Doesn't mention `main.go`, `engines/`, `db/`, or how to actually run the real system.

## 4. Concrete issues found while reading the code (not opinions — traced to specific lines)

These are worth a decision from you: fix now, track as known debt, or you tell me I'm misreading something.

1. **Unauthenticated requests silently get admin access.** In `apiMiddleware` (`main.go`), if no `Authorization: Bearer` header is present, the code does **not** reject the request — it sets `userID = "admin"; role = "HR/Admin"` and proceeds (comment calls it a "local dev fallback"). There's no environment check gating this — it's unconditional. Combined with RBAC being role-based (not identity-based), any request that simply omits the auth header gets full admin rights on every endpoint including `/api/v1/doc/{doctype}` deletes.
2. **SQL injection via query-parameter keys**, `handleGenericDoc` GET-list branch (`main.go`, dynamic filters loop): `query += fmt.Sprintf(" AND data->>'%s' = $%d", key, argIndex)` — the **key** from `r.URL.Query()` is spliced into the query unescaped (only the value is parameterized). A request like `GET /api/v1/doc/Brand?status'--=x` would inject through the key. This is the same endpoint the `micro_checklist.md` explicitly checks off as done ("Dynamic GET Filters ... [x]") and the security docs claim is "Prepared Parameterization ... blocks SQL injections" — so this is a real gap against the project's own stated bar.
3. **Hardcoded auth secret in source**, `engines/auth.go`: `var jwtSecret = []byte("custom_erp_super_secure_secret_key_123!")`. This directly contradicts `implementation_plan.md` §4 rule 4 ("No Secrets in Source Code") and the checklist's "Secrets Protection [x]" line.
4. **The auth token isn't JWT** despite being called that in the docs (`ai_handover.md` calls it "JWT verification", checklist says "SSO Claim Alignment"). It's a custom scheme: `base64(claims) + "." + hex(HMAC-SHA256(claims))`. Functionally similar (signed, tamper-evident) but **no expiry claim** — a signed token is valid forever once issued; there's no `exp` check anywhere in `ParseToken`.
5. **Permissive CORS reflects any Origin with credentials allowed**: `apiMiddleware` does `w.Header().Set("Access-Control-Allow-Origin", origin)` (verbatim reflection of the request's `Origin` header) plus `Access-Control-Allow-Credentials: true`. This is stricter-looking than a wildcard but is a known anti-pattern — it effectively allows any origin to make credentialed requests. Contradicts `framework_architecture.md` §8's stated rule of non-wildcard, allow-listed CORS.
6. **All four seeded default users share the identical bcrypt hash** (`db/migration.sql`, `INSERT INTO tenant_default.users`): `admin`, `cashier1`, `manager1`, `system` all get `$2a$10$7Z2u3n5b...`. Likely a placeholder for a shared dev password, but worth a deliberate decision before this schema ever seeds a non-dev environment.
7. **Checkout doesn't floor-check stock before decrementing.** `handleCheckout` → `PostInventoryLedger` does an unconditional upsert (`available = available + qtyVal` where `qtyVal` is negative) with no `GREATEST(0, ...)` floor, unlike `TransitionTaskStatus`'s dispatch path which does clamp at zero. It's possible to oversell at the direct-checkout endpoint without a preceding reservation (the reservation flow does check ATS; direct checkout does not). This may be intentional (checkout assumes a prior reservation), but nothing enforces that assumption at the API layer.
8. **`StartOutboxWorker` hardcodes `schema := "tenant_default"`.** The background poller that pushes Shopify delta-sync events only ever processes the default tenant's outbox — any other tenant's `integration_event_outbox` rows are never dispatched. Fine for a single-tenant pilot, a real gap for the multi-tenant story the rest of the docs describe.

## 5. Docs vs. built reality — the gap I'd flag most

The `docs/` folder mixes **built reality** (project_ledger.md, ai_handover.md, most of implementation_plan.md's phase log) with **forward-looking specification** (pos_architecture.md, most of modules_overview.md, industry_plugs.md's Pharma/Metal/Construction/Medical/Semiconductor/Agriculture sections). Concretely, **none of the following described features exist in code yet** — they're spec only:

- Offline-first POS (IndexedDB/PouchDB catalog cache, cash opening/closing sessions, KOT, split-bills) — `pos_architecture.md` in full. Actual checkout (`handleCheckout`) is a single synchronous online endpoint with no offline queue, no cash session model.
- RFQ, PR (Purchase Requisition), 3-way match, RTV, sticker printing, GST e-invoice/IRN, HR/Assets/Expenses modules — all described in `modules_overview.md`, none seeded in `migration.sql` or referenced in `main.go` routes.
- Industry profiles beyond the 4 that exist: only `jewelry.json`, `food_bev.json`, `auto.json`, `clothing.json` exist in `public/profiles/`, matching the 4 options hardcoded in `handleGetIndustries`. Pharma, Metal & Steel, Construction, Medical Devices, Semiconductors, Agriculture (all detailed in `industry_plugs.md` §2) have no corresponding JSON profile or code path.
- Multi-legal-entity / multi-currency / GST jurisdiction tax engine (`framework_architecture.md` §7.2) — no `legal_entity_id`, `currency`, or tax-rule tables in the schema.
- Reports engine, Log Hub UI, IP-protection build steps (stripped binaries, obfuscated JS) — all explicitly marked `[ ]` incomplete in `micro_checklist.md` Stages 9.2/10/12, and I found nothing in code that contradicts that (no report endpoints, no `-ldflags="-s -w"` build script found).

None of this is a problem by itself — it's normal for planning docs to run ahead of code. Flagging it because a future AI session reading `modules_overview.md` or `pos_architecture.md` in isolation could easily assume these are built when they aren't.

## 6. What's genuinely solid

Worth saying explicitly, not just gaps: the double-entry finance invariant, the row-locked sequence numbering, the ATS reservation math, the outbox/idempotency pattern for external orders, and the scale-test harness are all real, checked-against-source, and match their descriptions in `project_ledger.md`/`ai_handover.md`. The core DocType metadata engine (register a doctype, define fields, validate, render dynamic forms) is also real end-to-end, front to back.

---

## Open questions for you

1. Item 1 (auth bypass on missing header) and item 2 (SQL injection via filter keys) look like the two that matter most — want me to fix those now, or just track them?
2. Should I update `README.md` and `package.json` to reflect the Go/Postgres reality, or are those intentionally being left as-is for now?
3. Is `public/db.js` safe to delete, or is something else still planning to depend on it?
4. Once you've corrected anything above, tell me which sections should become permanent (probably folded into `ai_handover.md`/`project_ledger.md`) vs. discarded, and I'll delete this file.
