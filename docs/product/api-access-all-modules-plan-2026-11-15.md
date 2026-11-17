---
doc_id: DOC-PRODUCT-API-ACCESS-PLAN
title: API access for every module plan
type: reference
status: in-review
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md items 205.1-205.24
authority: proposed-design
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# API access for every module: plan (ask 205, 2026-11-15)

User, 2026-11-15, with a screenshot of **Settings › API access › New API access**: *"This is required for all possible modules. This needs to broaden. Research and Make a solid plan. Make UI more user friendly. API access should modern and functional like butter. Never failing. Easy to understand by end user. Make a documentation and well to share from here. It will be for all module and it will be controlled on module level access. If I give this module, user can expose/share those modules API only"*.

Items are in `docs/micro_checklist.md` under **CHN › Public API, webhooks & extensions** with the same ids.

## What exists today (researched 2026-11-15)

- **The screen** ([view-api-access.js](../../public/view-api-access.js)) issues an API key or an OAuth client, shows the secret once, rotates, revokes and lists recent calls. Only an Admin can use it (`requireHRAdmin` in [handlers_public_api_admin.go](../../internal/server/handlers_public_api_admin.go)).
- **The public API** (`/api/public/v1`, [routes_public_api_v1.go](../../internal/server/routes_public_api_v1.go)) has five endpoints: list items, get one item, stock for one SKU, order status, notification delivery receipt. AI access adds five more of its own behind the `ai_access` module.
- **The screen offers 8 scopes, and 4 of them unlock nothing.** `orders:write`, `pim:read`, `pim:write` and `webhooks:manage` have no endpoint behind them. A key holding only those can call nothing.
- **What to keep (it is good):** a separate public middleware (credential → scope → per-minute and daily limits → `Idempotency-Key` required on every write → traffic log), the shared error envelope, the tenant module gate, an OpenAPI spec generated from the route table, auto-revoke of unused keys, and one active key per purpose.
- **Gaps:**
  - Keys have no idea of modules.
  - Nothing caps a key at what the person who issued it may do.
  - Rotating cuts the old secret at once, so the integration is down until someone updates it.
  - The OpenAPI file is Admin-only, there is no viewer, and nothing can be shared.
  - There is no way to test a key.
  - Webhooks fire on about 10 events, none of them for documents.
  - About 280 document types in 23 modules cannot be reached.
- **Why "every module" is affordable:**
  - Every record a screen saves goes through one document engine (`/api/v1/doc/{doctype}`, `handleGenericDoc` in [handlers_core_doc_engine.go](../../internal/server/handlers_core_doc_engine.go)). It already applies role rights, store scope, field rules, approval gating, numbering, the "made only on its own page" guards and audit.
  - Every document type belongs to a module (`doctype_meta.module_key`).
  - Every internal route's capability is already named after its module (oms, wms, finance, pim, procurement and so on).

| Module | Document types | Module | Document types |
|---|---|---|---|
| Inventory | 69 | Master data / Core | 12 / 12 |
| PIM | 33 | Quality, CRM & loyalty | 8 / 8 |
| Finance | 30 | Reports | 7 |
| OMS, Sales & POS | 22 / 22 | Manufacturing | 6 |
| Procurement | 19 | Custom orders, Stickers, RFQ, Service, Sales incentives | 4 / 3 / 2 / 2 / 2 |
| HR | 17 | WMS, Expenses, Assets, Integrations | 1 each (WMS works mostly through 104 action routes) |

## How others do it (reference)

| Product | Pattern | What we take |
|---|---|---|
| Stripe restricted keys | None / Read / Write for each resource | Off / View / View & change for each module |
| GitHub fine-grained tokens | An owner, a required expiry, a level for each permission | An owner for each key, expiry presets, reminders |
| Shopify custom apps | `read_` / `write_` scopes for each resource | `<module>:read` / `<module>:write` |
| ERPNext / Frappe | `/api/resource/{doctype}`, acting with the key user's own permissions | A records API that goes through the same document engine, acting as the key's owner |

## Decisions

Recommended answers are in the second column. "All recommended approved" approves this column as written.

| # | Decision | Recommended | Alternatives |
|---|---|---|---|
| D1 | How the API reaches every module | **Three routes in:** *Records* (every document type, through the same document engine as the screens), *Actions* (an approved list of screen actions for each module) and *Reports* (every catalogue report), plus webhooks for document events | B: hand-write endpoints module by module (today's way, never finished). C: open the whole internal API to keys (unsafe, and screen changes would break integrations) |
| D2 | What a key may reach | **The owner model.** Every key has an owner. On each call it gets the ticked modules ∩ the company's enabled modules ∩ the owner's *current* rights (role, stores, field rules). Take a module away from a person and their keys lose it on the next call | The key gets its own fixed role (a service account): more setup, and it drifts from how people's access is managed |
| D3 | Who can create keys | **A new right, *Share module API*** (`api.share`), which an Admin gives to roles. Its holder can share only the modules they use: View needs read and Change needs write. Admins can share any enabled module and see every key. Holders see their own keys. After the release only Admin has the right | Admin only (today), which does not meet "if I give this module, user can share it" |
| D4 | How fine-grained | **Off / View / View & change for each module.** A collapsed *Fine-tune* narrows a module to chosen document types | Document types only (too many choices to make). Modules only (no least-privilege option) |
| D5 | Approvals and deletes by key | **Keys create, update and submit drafts. Approval steps stay human** unless *Can approve* is ticked in Fine-tune (with a warning). Keys delete drafts only, as on the screens | Allow approval by default, which defeats maker-checker |
| D6 | The owner leaves | **The deactivate-user dialog lists their keys and asks for a new owner.** An Admin can *Take over* any key. A key whose owner is inactive is refused with a clear code, and the owner and Admins are alerted (fails closed, never silently) | Keep running as the departed person (unsafe) |
| D7 | Old keys and scopes | **They keep working unchanged** (compatibility policy). The new screen shows module scopes only. `items:read`, `orders:read`, `inventory:read`, `notifications:write` and `webhooks:manage` stay valid on old keys. `pim:read`/`pim:write` become real module scopes | Force a reissue, which breaks live integrations |
| D8 | Rotation | **An overlap.** The old secret keeps working for 24 h by default (Now / 1 h / 24 h / 7 days). The status shows *Old key ends <time>* | Instant cut (today): the integration is down until it is updated |
| D9 | Expiry | **Presets of 30 days, 90 days, 1 year (default), a date, or Never.** Reminders 30, 7 and 1 day before, to the owner and the Admins. The existing unused-key auto-revoke stays | Never by default |
| D10 | The company inside the key | **New keys carry the company code**, so `X-Tenant-ID` becomes optional for them. Old keys still send it | Keep the header mandatory, one more thing to get wrong |
| D11 | Sharing the docs | **A *Share docs* button on each key** gives a read-only link to that key's modules and the company's real fields, custom fields included. No secret is in it. The link ends when the key is revoked, lasts at most 90 days and can be withdrawn. Also *Download OpenAPI* and a *Postman collection* | A public docs page (would show a company's custom fields to anyone). Files only |
| D12 | Handing the key to a developer | **An optional *Send to developer* link**, used once and valid for 48 h. The key is created when the developer opens the link, is shown only to them and is never stored. The Admin sees *Claimed on …* | The Admin copies the key into an email, where it stays |
| D13 | Docs viewer | **Our own small viewer in plain JS** that reads the generated spec (Article 1) | Swagger UI or Redoc, a new JS dependency |
| D14 | IP allow list | **Optional for each key** (*Only from these addresses*). Empty means anywhere | None |

## Order of work

| Phase | What | Items | Size | Needs |
|---|---|---|---|---|
| 1 | Foundation: module scopes, owner ceiling, document-type list | 205.3-205.6 | M | D1-D4, D6, D7 |
| 2 | Records API for every module | 205.7-205.9 | M | 1 |
| 3 | The screen | 205.10-205.13 | L | 1, then 2 for Test connection |
| 4 | Actions, reports and webhooks for each module | 205.14-205.16 | L | 1, 2 |
| 5 | Docs and sharing | 205.17-205.19 | M | 2, 4 |
| 6 | Never failing | 205.20-205.22 | M | 1, 3 |
| 7 | Verify, guides, release | 205.23-205.24 | M | all |

Each phase is usable on its own. After phases 2 and 3, every module can be reached and the screen works. Later phases add depth.

## Phase 1 - Foundation

**Why:** everything else rests on module scopes and the owner ceiling. Without them, opening more of the API only opens more risk.

**Your actions:** approve D1-D4, D6 and D7. After the release, say which roles get *Share module API*.

- **205.3 Module scopes.**
  - `<module>:read` and `<module>:write` are built from the module catalogue, so a new module gets its scopes with no extra code.
  - The scope list answers with each module's name and one line on what it covers. For the person asking, it also says whether the module is in the plan and whether they can give View or Change.
  - Old scope names are still accepted and mapped.
- **205.4 Owner and ceiling.**
  - Additive migration: `api_credentials` gains an owner, allowed document types, an IP list and the rotation overlap end.
  - Issuing checks the issuer's own rights.
  - Every call re-reads the owner (active, role, stores) and intersects.
  - Each refusal has its own code and a plain fix: owner inactive, module not in the plan, module not on the key, or the owner lost the module.
- **205.5 The *Share module API* right** (`api.share`).
  - Holders issue, list, rotate and revoke their own keys. Admins handle everyone's.
  - The route capabilities are updated to match.
- **205.6 Document-type exposure list.**
  - Each of the ~280 document types is classed *read and write*, *read only* or *never*.
  - *Never* is for internal ones: password resets, client data removal, shortcuts, saved views, export jobs and edit history.
  - A test fails the build when a new document type has no class.

**Done when:**
- An Admin and a module holder each issue a key, and the holder cannot tick a module they lack.
- Removing the module from the holder's role refuses the key's next call with the plain code.
- Tests for wrong tenant, module, role, store and owner state pass.

## Phase 2 - Records API for every module

**Why:** one mechanism reaches all 23 modules, and every module added later, with exactly the screens' checks.

- **205.7 `/api/public/v1/records/{doctype}`.**
  - Calls: list (limit, cursor, filters, search, `changed_since`), get, create, update, submit and cancel.
  - Each call goes back into `handleGenericDoc` as the key's owner. Rights, store scope, field rules, approvals, numbering, the "made only on its own page" guards and audit are the screens' own.
  - The audit shows *API: <key name> (owner)*.
- **205.8 `changed_since` on the shared document list**, so an integration syncs only what changed. The screens may use it too, and the default order is unchanged.
- **205.9 `/records/{doctype}/schema` and `/me`.**
  - The schema endpoint returns the company's real fields, custom fields included: type, required, options and read-only.
  - `/me` returns the key's name, owner and modules, and the limits left today. It drives *Test connection*.

**Done when:**
- A key with View on Procurement lists the POs changed since yesterday, reads one, and is refused on create.
- With View & change it creates a draft PO that shows on the PO screen with the right number.
- A retry with the same `Idempotency-Key` makes one PO, not two.

## Phase 3 - The screen

**Why:** *"user friendly, easy to understand by end user"*. The screen rules apply: title only, very short text, list first, New on its own page, no spinners, fits any window size and zoom. The words do the explaining: plain option names, and a ⓘ link to the guide.

- **205.10 The list.**
  - Each row: name, modules as chips, status and last use (*3 min ago*), plus calls and errors today.
  - Statuses: *Active · Expires in 5 d · Owner inactive · Rotating · Revoked · Expired*.
  - Filters: *Mine* or *All* (All is Admin only), and status.
- **205.11 The *New API access* page: one page in three short blocks.**
  1. Name, plus a quick start that pre-ticks modules: *Online store sync · Accounting export · Reporting (view only) · Warehouse device · Custom*.
  2. The module grid: Off / View / View & change for each module. A module that cannot be given is greyed with its reason (*Not in plan*, *Not in your access*). *Fine-tune* narrows a module to chosen document types.
  3. Security: sign-in type (*Secret key* / *App login (OAuth)*), expiry presets, stores, and an optional IP list.

  A one-line summary sits above **Create**: *Shopify sync · View Inventory · Change OMS · All stores · Until 15 Nov 2027*.
- **205.12 The *Created* page (shown once).**
  - The key with **Copy** and **Download .env**.
  - Ready snippets in curl, JavaScript and Python with the real address filled in.
  - **Test connection**, **Share docs** and **Send to developer**.
  - *I've saved the key* must be ticked before leaving.
- **205.13 A detail page for each key.**
  - Overview.
  - Modules: narrow any time. Widen within the owner's rights, which is audited.
  - Activity: calls with an *Errors only* filter. Each error row shows its fix.
  - Limits.
  - Rotate, with the overlap choice. Revoke. Take over (Admin).

```
API access › New API access
┌──────────────────────────────────────────────────────────────────────────┐
│ Name [Shopify sync            ]   Start from [Online store sync ▾]       │
│                                                                          │
│ Modules                      Off     View    View & change               │
│  Inventory                    ○       ●        ○                         │
│  OMS                          ○       ○        ●                         │
│  Procurement                  ●       ○        ○                         │
│  Finance                      ●       ○        ○   ⓘ Not in your access  │
│  HR                           —  Not in plan                             │
│  ▸ Fine-tune                                                             │
│                                                                          │
│ Sign-in [Secret key ▾]  Expires [1 year ▾]  Stores [All ▾]  IPs [Any]    │
│                                                                          │
│ Shopify sync · View Inventory · Change OMS · All stores · Until 15/11/27 │
│                                                          [ Create ]      │
└──────────────────────────────────────────────────────────────────────────┘
```

**Done when:**
- Guide's *fit* and *act* walks pass on the list, New, Created and detail pages.
- A user who has never seen the screen creates a working key without opening the docs.

## Phase 4 - Actions, reports and webhooks for each module

**Why:** some modules work mostly through actions rather than documents (WMS has 104 action routes and one document type, and OMS and POS are similar), and an integration needs to be told about changes, not only to ask.

- **205.14 Actions.**
  - A reviewed list maps existing screen actions to `/api/public/v1/actions/<module>/<action>`. Each call goes back into the existing handler as the owner.
  - Each entry names its module and View or Change, and has its own test.
  - An action joins the list only when an outside system has a real use for it. Money and period actions (period close, payroll run, payment execute) stay on the screens in v1.
  - First wave, each one confirmed against its handler during the build:

  | Module | Actions (existing route) |
  |---|---|
  | Inventory | move stock (`stock-locations/move`), post an adjustment, transfer submit / dispatch / receive, challan dispatch |
  | Procurement | send a PO, GRN receive / post (`grn-steps/{id}/receive`, `post-all`), match a vendor bill |
  | OMS | create an order (`/orders`), cancel, hold / release, return create / receive, courier tracking, invoice a package |
  | WMS | putaway, pick scan, pack complete, loading complete, cycle count submit, robotics event |
  | Sales & POS | quote, validate an offline sale, gift card balance, stored-value action |
  | Finance | journal voucher, receipt, bank statement import, post / cancel a sales invoice, e-invoice run |
  | CRM & loyalty | validate / redeem a voucher, loyalty redeem, redemption start / verify |
  | PIM | publish, media upload, barcode generate, bulk edit |
  | Manufacturing | issue material, confirm an operation, complete |
  | Stickers | print a label |
  | HR | none in the first wave; Employee, Attendance and Leave go through Records |
- **205.15 Reports.** List the reports the key's modules cover and run one with filters, as paged JSON or CSV. This uses the report catalogue (`ReportDefinition` / `RegisterReport`), not a second engine.
- **205.16 Webhooks for every module.**
  - Created, updated, submitted, cancelled and approved events for each document type, published once from the document engine.
  - A subscription belongs to a key and receives only its modules' events.
  - Deliveries are signed and retried with backoff.
  - A delivery log with **Resend** is on the key's detail page.

**Done when:**
- A WMS-only key confirms a putaway, and the bin shows it on the WMS screen.
- A Finance report runs by API and matches the screen's totals.
- Submitting a PO on the screen delivers `procurement.PurchaseOrder.submitted` to a subscribed test endpoint once, signed.

## Phase 5 - Docs and sharing

**Why:** *"Make a documentation and well to share from here."*

- **205.17 A spec for each company and each key.** OpenAPI 3.1 built from the route table, the document schemas, actions, reports and events, filtered to the key's modules. The existing generator ([openapi_public_v1.go](../../internal/server/openapi_public_v1.go)) is extended rather than replaced.
- **205.18 The docs viewer** (Settings › API access › **Docs**).
  - Grouped by module.
  - Each endpoint: its purpose in one line, parameters, an example request and response, snippets to copy, and its errors with their fixes.
  - A *Getting started in 5 minutes* page and a plain-language glossary.
- **205.19 Sharing** from each key: **Share docs** (D11), **Download OpenAPI / Postman** and **Send to developer** (D12).

**Done when:** a developer with only the shared links makes a first successful call in under 10 minutes. Guide follows the docs literally to prove it.

## Phase 6 - Never failing

**Why:** *"functional like butter, never failing"*. Most integration outages are expired or rotated keys and silent refusals, not server faults.

- **205.20 Rotation overlap (D8) and expiry reminders (D9)**, sent in-app and by email, plus a *Needs attention* tile on Home.
- **205.21 A failure alert with a one-click fix.** When a key is refused repeatedly (about 20 times in 10 minutes), the owner sees *Shopify sync is failing: needs View on Inventory* with an **Add** button, offered only if the owner holds that right.
- **205.22 Clear errors.**
  - Every refusal code has a plain cause and fix in the error catalogue.
  - The response carries `fix` and `docs_url`.
  - A server error on the public API raises an ops alert.

**Done when:**
- Rotating a live key causes zero failed calls in a test loop.
- Reminders arrive for a key about to expire.
- A forced refusal produces the alert, and its one-click fix works.

## Phase 7 - Verify, guides, release

- **205.23 Tests** (Article 6):
  - wrong tenant, module, role, store, field and workflow state
  - owner inactive
  - replay
  - rotation under concurrent calls
  - rate limit
  - spec completeness (every exposed route documented)
  - document-type class completeness

  Each new route also gets its capability entry, and the attack surface is regenerated (release gate G6).
- **205.24 Guides and the live check.**
  - The admin guide's API access section is rewritten.
  - The user guide gets *Share your module's API*.
  - KB articles: getting started, sign-in, records, actions, reports, webhooks, errors and retries, limits.
  - [public-api-v1.md](../api/public-api-v1.md), the error codes and UAT 10B are updated.
  - A live walk on app after the release.

## Interfaces (build contract, ask 224)

D1-D14 were approved as recommended on 2026-11-16 (ask 224). The screen and the server are built against these routes. All screen routes run through `apiMiddleware`; all key routes run through `publicAPIMiddleware`. The old `/api/v1/admin/api-credentials` routes and the five v1 endpoints stay as they are (D7).

**Screen routes** (capability `api.share`; Admin and Super Admin always pass; a holder acts on their own keys only):

| Route | Does |
|---|---|
| `GET /api/v1/api-access/modules` | Modules for the asker: `key, name, summary, in_plan, can_view, can_change, reason` (*Not in plan* / *Not in your access*), `doctypes[] {doctype, label, class: rw\|ro}` |
| `GET /api/v1/api-access/keys?scope=mine\|all&status=` | Rows: `id, name, key_prefix, auth_mode, modules {key: read\|write}, doctypes {module: [doctype]}, can_approve, owner_id, owner_name, owner_active, status (active\|expiring\|owner_inactive\|rotating\|revoked\|expired\|pending_claim), expires_at, old_key_ends_at, last_used_at, calls_today, errors_today, stores[], ip_allow[], legacy_scopes[]` |
| `POST /api/v1/api-access/keys` | Body: `name, quick_start, modules, doctypes, can_approve, auth_mode (api_key\|oauth_client), expires (30d\|90d\|1y\|never\|YYYY-MM-DD), stores[] (empty = all), ip_allow[], deliver (show\|developer_link)`. Answer: `credential, api_key` (shown once), `base_url, tenant_code`; with `developer_link`: `developer_link {url, expires_at}` and no key |
| `GET /api/v1/api-access/keys/{id}` | One row plus `limits` |
| `PUT /api/v1/api-access/keys/{id}/modules` | `modules, doctypes, can_approve`; widening is held to the owner's rights and audited |
| `POST /api/v1/api-access/keys/{id}/rotate` | `overlap: now\|1h\|24h\|7d` (default 24h). Answer as create |
| `DELETE /api/v1/api-access/keys/{id}` | Revoke |
| `POST /api/v1/api-access/keys/{id}/take-over` | Admin only; the asker becomes the owner |
| `GET /api/v1/api-access/keys/{id}/activity?errors_only=1&limit=` | Calls: `at, method, path, status, outcome, code, fix` |
| `GET\|POST /api/v1/api-access/keys/{id}/share-docs`, `DELETE .../share-docs` | Docs link `{url, expires_at}` (`days` <= 90); withdraw |
| `GET /api/v1/api-access/keys/{id}/openapi.json`, `.../postman.json` | The key's spec and Postman collection |
| `GET /api/v1/api-access/openapi.json` | The company's spec, filtered to the asker's modules (the Docs viewer) |
| `GET\|POST /api/v1/api-access/keys/{id}/webhooks`, `DELETE .../webhooks/{sub}`, `GET .../webhooks/deliveries`, `POST .../webhooks/deliveries/{d}/resend` | Document-event subscriptions for the key |
| `GET /api/v1/api-access/attention` | Keys needing attention (expiring, rotating, failing with the missing module) for the Home tile |
| `POST /api/v1/api-access/keys/{id}/fix` | `{module, level}`: the one-click fix from a failure alert |

**Key routes** (`/api/public/v1`): `GET /me`; `GET|POST /records/{doctype}`; `GET|PUT /records/{doctype}/{id}`; `POST /records/{doctype}/{id}/submit`, `.../cancel`; `GET /records/{doctype}/schema`; `GET /records/{doctype}?changed_since=&cursor=&limit=&q=&<field>=`; `POST /actions/{module}/{action}`; `GET /reports`, `GET /reports/{id}?format=json|csv&page=`. Unauthenticated: `GET /api/public/v1/docs/{token}` (shared spec, no secret), `POST /api/public/v1/developer-links/{token}/claim` (creates and shows the key once). Pages: `public/api-docs.html` (shared docs) and `public/api-claim.html` (developer link).

**Refusal codes** (error catalogue, each with a cause and a fix, response carries `fix` and `docs_url`): owner inactive, module not in plan, module not on key, owner lost the module, document type not exposed, document type not on key, approval not allowed for keys, IP not allowed, key expired, old secret ended.

## Not adopted

| Idea | Why not |
|---|---|
| Open the whole internal `/api/v1` to keys | The screens' API changes often. That would break integrations and skip the reviewed surface |
| GraphQL | A new dependency and a second query model. REST plus OpenAPI is what ERP integrators expect |
| Swagger UI / Redoc | A new JS dependency (Article 1). Our own viewer is small |
| Store the secret to show it again | *Shown once* stays. The developer link creates the key when it is opened instead |
| Merge AI access into this | AI access is its own read-only module with its own rules. It can become a quick start later |
| Per-field scopes on the key | Field rules already come from the owner's role |
| Test (sandbox) keys | Needs a sandbox company for each client. Later, if asked |

## Guardrails

- A key never does more than its owner can do on the screens, and only in ticked modules the company has enabled.
- One engine: no second save path and no second validation (Article 1, "fix it once").
- Migrations are additive only. Old keys, old scopes and the five v1 endpoints keep working ([compatibility policy](../api/compatibility-policy.md)).
- Every exposed route is classified in the route capabilities, and the attack surface is regenerated (G6).
- Screens: title only, short text, list first, no spinners, fits any window size.
- Build and test only what changed, one heavy job at a time (first principle).
