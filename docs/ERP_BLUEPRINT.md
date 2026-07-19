# Custom ERP — Full Project Blueprint

**Purpose of this document**: a single, self-contained snapshot of this project meant to travel outside the repository — hand it to an outside reviewer (including an AI like ChatGPT) with no other context, and they should be able to form an informed opinion on where this project stands. It is written from five perspectives in turn — CEO, Product Head, CTO/Architect, Developer, and Project Tech Lead — because those are the five questions this project actually gets asked, and each one wants a different cut of the same facts.

Everything below is a synthesis of documents already in this repository, cited by path. This document does not introduce new facts — it exists to make the existing facts legible to someone who hasn't read the other ~15 documents. **Snapshot date: 2026-07-19.**

---

## 1. Executive Summary (the CEO question: "what have we built and is it working?")

This is a custom-built, multi-tenant Enterprise Resource Planning (ERP) system, written from scratch in Go with a PostgreSQL backend, targeting small-to-mid-size retail/distribution businesses (jewelry, F&B, apparel, and similar verticals) that need POS, inventory, procurement, finance/GL, and omnichannel (Shopify/BigCommerce/Magento) sync in one system, without paying for or being locked into a large incumbent platform (SAP, Netsuite, Odoo Enterprise).

**Current maturity**: the system is well past prototype. It has a working login/auth system, role-based access control, a metadata-driven document engine, double-entry GL accounting, POS checkout, procurement (requisition → RFQ → PO → GRN → vendor invoice → payment, three-way matched), inventory with real-time availability, HR/payroll export, fixed assets, expense management, a maker-checker approval workflow engine, TOTP MFA, a Product Information Management (PIM) module with real Shopify/BigCommerce/Magento connectors, backup/restore tooling, a multi-environment deployment pipeline (dev/test/live), and now (this session) an alerting/incident-response system. All of this is backed by an automated test suite and a CI pipeline (build + vet + test + vulnerability scan + secrets scan) that runs on every push.

**What's genuinely not done yet**: two Stage-17 items are code-complete and verified as far as they can be without inputs only a human can supply (real escalation contacts for the incident runbook; real non-production Shopify/BigCommerce/Magento store credentials to run a live connector test) — see §5. Beyond that, one new backlog item was just opened (dropdown/autosuggest UX across data-entry forms — currently many master-data fields are free text, which the user flagged as a real usability risk) and is not yet started. Several older design documents in this repo describe a much larger vision (multi-industry configurator with 10+ industry profiles, offline-first POS, a full ~80-report catalog) than what's actually built (4 industry profiles wired in, synchronous-only POS, ~5 core reports) — this gap is tracked and explicit, not hidden (see §2 and §5).

**Biggest open risks, plainly stated**:
1. **No production deployment has happened yet.** The deployment pipeline (`promote.ps1`, `manage.ps1 -Env`) and backup/restore tooling exist and are drilled, but everything to date has run in a local/dev PostgreSQL instance on the developer's machine — going live means standing up real infrastructure for the first time.
2. **Single-developer-adjacent build process.** Most of this codebase was built by AI coding agents (Claude Code sessions) working from a human's direction, with the human reviewing and directing at a high level. This is both a strength (very fast iteration, systematic documentation) and a risk worth naming honestly for an outside reviewer (see §6 for how build quality was actually controlled).
3. **Concurrent-session risk.** More than one AI/human session has edited this repo in the same window before. The project has adopted explicit git-hygiene conventions to manage this (see §6), but it's a real operational habit that has to keep being followed, not a solved problem.
4. **No real-world transaction volume yet.** Everything has been tested with synthetic/small data and unit+integration tests, never a real business's live order volume.

---

## 2. Product Scope (the Product Head question: "what does it actually do, module by module?")

Legend: **BUILT** = implemented, tested, and (where the item's own history calls for it) live-verified against a running instance. **SPEC** = designed/documented but not implemented. **PARTIAL** = a real, working subset of a larger designed scope, with the gap stated explicitly in this codebase's own docs (not discovered by this snapshot — every PARTIAL item below already carries this caveat in its source doc).

| Module | Status | Notes |
|---|---|---|
| Core document engine (metadata-driven DocTypes, generic CRUD API) | **BUILT** | `GET/POST/PUT/DELETE /api/v1/doc/:doctype`, field validation against `doctype_fields`. Modeled on ERPNext/Frappe's DocType pattern — see `docs/architecture/framework_architecture.md`. |
| Auth, RBAC, MFA | **BUILT** | JWT bearer tokens, per-role permissions, TOTP MFA (RFC 6238) required for HR/Admin roles, account lockout on repeated failed logins. |
| Multi-tenancy | **BUILT** | Schema-per-tenant in one PostgreSQL instance; tenant resolved from JWT claim, never trusted from a client-supplied header alone. |
| POS / Checkout | **PARTIAL** | Synchronous checkout screen works end-to-end (cart → GST calc → GL posting → loyalty points). `docs/specs/pos_architecture.md`'s offline-first/cash-drawer-session/KOT design is spec-only. |
| Finance / General Ledger | **BUILT** | Balanced double-entry postings, accounting-period close control, GST (CGST/SGST/IGST) calc and enforcement at PO creation and checkout. |
| Procurement | **BUILT** | Full chain: Purchase Requisition → RFQ/vendor quote comparison → Purchase Order → GRN → three-way-matched Vendor Invoice → Payment, GL-posted at each real money-movement step. |
| Inventory / Fulfillment | **BUILT** | Barcoded stock counts, Available-to-Sell read model (Available − Reserved − Safety Stock − Channel Holds), store picking tasks, transfer-order dispatch/receive, Return Anywhere. |
| Reports | **PARTIAL** | 5 prioritized reports built (Current Stock, Sales Register, Vendor Ledger, Payables Ageing, plus PIM dashboard). `docs/specs/modules_overview.md`'s ~80-report catalog is spec-only. |
| HR / Payroll | **PARTIAL** | Employee/Attendance/Leave records, payroll export, employee↔user access-link sync. Full payroll processing/statutory compliance is out of scope. |
| Fixed Assets | **BUILT** | Capitalize → straight-line depreciate → transfer → dispose lifecycle, asset register. |
| Expense Management | **BUILT** | Claim → verify → pay, reuses the approval engine, posts GL. |
| CRM / Loyalty | **PARTIAL** | Append-only points ledger (earn/redeem), wired into POS checkout. No campaigns, segmentation, or vouchers. |
| Manufacturing | **PARTIAL** | Single-level BOM, linear Production Order (issue → receive). No routing/work centers, MRP, or QC gates. |
| Multi-industry configurator | **PARTIAL** | 4 of 10+ designed industry profiles actually wired into code (Jewelry, F&B, Auto, Clothing) — `docs/specs/industry_plugs.md`. |
| PIM (Product Information Management) | **BUILT** | Family/Attribute framework, approval-gated content, completeness scoring, media library, CSV import/export, channel-publish queue, dashboard/bulk-edit/field-permissions. |
| Channel connectors (Shopify/BigCommerce/Magento) | **BUILT, unit-tested; live-store run pending real credentials** | Real API integrations (not stubs), each verified against a fake HTTP server standing in for the platform. Never yet run against an actual Shopify/BigCommerce/Magento store — see §5. |
| Deployment pipeline | **BUILT** | `promote.ps1` (git-worktree checkout → build → migrate → restart, with a red-build gate), 3-environment model (dev/test/live), rollback. Explicitly **not** containerized — Docker was built once then reverted by deliberate decision; no Docker dependency is the standing policy. |
| Backup/restore | **BUILT, drilled** | `manage.ps1 backup`/`restore`, SHA-256 sidecars, a documented restore drill actually performed and timed. |
| Incident response / alerting | **BUILT, live-verified; awaiting real contacts** | Panic recovery, failed-backup, and sustained-error-rate alerts to a Slack/Teams webhook; a full runbook. Verified against a mock webhook receiver — see §5. |
| 3rd-party extension framework | **BUILT** | Scoped, read-only, HMAC-signed extension tokens; a standalone SDK contract (`extension-sdk/`) meant to ship as its own repo to 3rd-party developers. |
| Data-entry UX (dropdowns/autosuggest) | **NOT STARTED** | New backlog item (Stage 18) — many master-data fields are currently free text where they should resolve to an existing record. Flagged as a real usability risk before this reaches non-technical end users. |

---

## 3. Architecture (the CTO/Architect question: "how is this actually built, and does it hold up?")

**Stack**: Go 1.22 (single static binary, no runtime dependency), PostgreSQL 16 (schema-per-tenant), a hand-written vanilla-JS SPA frontend (no framework) served as static files. Deliberately minimal-footprint: the whole rationale (`docs/architecture/architecture_evaluation.md`) is that a Go binary idles at ~10-15MB RAM vs. ~80-150MB for a Python/Node equivalent, which matters at the target scale (many small-margin tenants, not a handful of enterprise accounts).

**Core pattern — metadata-driven document engine**: rather than hand-coding a form/table/API per business object, most DocTypes (master records and transactions alike) go through one generic `documents` table per tenant schema (`id`, `doctype`, `data JSONB`, `status`, audit columns) plus a `doctype_meta`/`doctype_fields` registry that defines what fields each DocType has and validates against it server-side. This is explicitly modeled on ERPNext/Frappe's DocType pattern, Odoo's pluggable-app model, and Nocobase's dynamic schema approach — see `docs/architecture/framework_architecture.md` for the full rationale and the platforms it draws from.

**Multi-tenancy**: hybrid schema-per-tenant in one shared PostgreSQL instance. Every request resolves a tenant from the verified JWT (never a client-supplied header taken at face value — this was a real bug, fixed, see §6), then every query runs against that tenant's own PostgreSQL schema. Tenant A's connection can't accidentally read or write Tenant B's data because the schema boundary is enforced at the SQL layer, not just the application layer.

**Event-driven omnichannel sync**: user-facing transactions never make a synchronous outbound HTTP call to an external system (Shopify, a payment gateway, a GST API). Instead they write to an `integration_event_outbox` table in the same DB transaction as the business write, and a background poller drains it asynchronously. This means a slow or down 3rd-party API can never make a checkout or PO hang.

**Security posture**: JWT bearer auth with expiry, TOTP MFA for privileged roles, per-role RBAC enforced server-side on every doc operation, parameterized SQL everywhere (no string-built queries), a 2MB request body cap, per-category rate limiting (login/bulk-upload/report/webhook/search each get their own budget so one hot endpoint can't starve another), CORS allowlist (not a wildcard), HMAC-verified inbound webhooks, AES-256-GCM encrypted-at-rest channel credentials, and CSV formula-injection sanitization on every import/export path. A dedicated hardening pass (`docs/operations/hardening_roadmap.md`) closed a specific list of found issues and is kept as a historical record.

**Build-quality gate**: every change goes through `go build ./...`, `go vet ./...`, `go test ./... -p 1`, a `govulncheck` vulnerability scan, and a `gitleaks` secrets scan in CI (`.github/workflows/ci.yml`) on every push. Locally, the convention this project has followed since Stage 13 is: build one item at a time, write a test for it, then **live-verify it against a real running throwaway instance** before considering it done — not just "tests pass," but "I actually ran the server and hit the endpoint and looked at the database row." This is recorded, item by item, in `docs/micro_checklist.md`.

**Deployment model**: three environments (dev/test/live), each its own git worktree + PostgreSQL database + port, promoted forward via `promote.ps1` (refuses to promote a dirty tree or a failing build/test run, records every promotion in a `deployments` table, supports one-command rollback to the last known-good commit). No containerization — a deliberate, explicit decision (built once, reverted on request) to keep the deployment surface to "one static binary + one Postgres instance," matching the low-footprint goal above.

---

## 4. Build History & Velocity (the Developer question: "how did we get here, and how fast are we actually moving?")

This is an index, not a re-derivation — full detail for every stage lives in `docs/project_ledger.md`, and full per-item scope/verification detail lives in `docs/micro_checklist.md`. Summary of the arc:

- **Phases 1-7**: core foundation, single-vertical pilot, omnichannel sync groundwork, store fulfillment, concurrency scale testing, marketplace expansion, optimization engines.
- **2026-07-12, real login flow**: the app went from "no real authentication" to a proper login system in one focused pass — this is called out specifically because it's the kind of gap that's easy to not notice is missing until someone looks for it.
- **Stage 13 (2026-07-12 → 2026-07-17)**: closed the largest gap found in this project's history — a self-audit (`docs/specs/pdf_blueprint_gap_analysis.md`) found that the backend/API layer was strong but there was almost no actual user-facing screen for a business user to do their job. Built POS/Finance/Fulfillment/Marketplace screens, the maker-checker approval engine, MFA, vendor/customer masters, the report catalog, RFQ/quote comparison, HR, Fixed Assets, Expense Management, CRM/Loyalty, Manufacturing, per-endpoint rate limiting, and barcode/sticker printing — one item at a time, each live-verified before commit.
- **Stage 14 (2026-07-18)**: the deployment pipeline itself, module governance/feature flags, patch/bug-intake automation, 3rd-party extension isolation, further security hardening, Go toolchain upgrade.
- **Stage 15-16 (2026-07-18 → 2026-07-19)**: PIM foundation and V2 (approval-gated content, completeness scoring, media library, channel-publish queue), then real Shopify/BigCommerce/Magento connectors plus the remaining PIM gaps (dashboard, bulk edit, field permissions).
- **Stage 17 (2026-07-18 → 2026-07-19)**: a controlled post-PIM execution queue — soft delete, CSV injection protection, backup/restore, accounting-period control, GST enforcement, transfer orders, purchase requisitions, vendor invoice 3-way match, location/entity masters, and (this session) runbook/alerting plus connector-verification tooling.

**Velocity read**: roughly a week of wall-clock time (2026-07-12 → 2026-07-19) covered what the ledger above shows — that pace is real but is a direct consequence of AI-agent-assisted development working from a human's direction and a disciplined "build → test → live-verify → document" loop, not a claim about how fast a human team alone would move. Worth stating plainly to an outside reviewer rather than leaving implicit.

---

## 5. Known Gaps, Risks, and Exactly What's Blocked On What

Pulled directly from `docs/micro_checklist.md`'s currently-open items (`[ ]`), quoted rather than paraphrased where it matters:

1. **17.10 Runbook and alerting** — code, tests, and a full incident runbook are built and live-verified end-to-end (a real panic was triggered, logged, and alerted to a mock webhook receiver during this session). What's left is not implementation: `[needs user input: escalation contacts + OPS_ALERT_WEBHOOK_URL]` — the user has chosen to fill these in directly (real names/contacts and a real Slack/Teams webhook URL) rather than share them in an AI chat session, for sound secret-hygiene reasons.
2. **17.11 Live connector verification** — a verification script and procedure are built and ready to run. What's left: `[needs user input: non-production Shopify/BigCommerce/Magento credentials]` — the user has opted to drop a local, gitignored credentials file themselves once ready, rather than paste real API tokens into a chat transcript.
3. **Stage 18 (new, not started)** — data-entry UX audit: many master/transaction forms currently accept free text where a dropdown or type-ahead against an existing master record would prevent bad data. Flagged by the user as "without this, this will be the most difficult platform to handle" — i.e., a real go-live blocker for non-technical users, not a cosmetic nice-to-have. Not yet scoped into implementation-sized items (see `docs/micro_checklist.md` Stage 18 for the placeholder breakdown).
4. **Spec-vs-built gap, stated openly**: several early design documents (`docs/specs/`) describe considerably more than what exists today — a 10+ industry configurator (4 built), offline-first POS (synchronous-only built), an ~80-report catalog (~5 built), full payroll/statutory compliance (export-only built). Every one of these gaps is already flagged with a status banner in its own source document — this is not new information, it's collected here for visibility.
5. **No production run yet.** Everything above has been verified in a dev/test environment on the developer's own machine. Going live for the first time is itself a milestone this project hasn't crossed.

---

## 6. How Build Quality Was Actually Controlled

Worth stating explicitly for a reviewer assessing trustworthiness of the above, not just its completeness:

- **Live-verification discipline**: starting Stage 13, the working convention has been to spin up a real throwaway server instance and actually exercise a new feature (hit the endpoint, read the resulting database row) before marking it done — not just "the unit test is green." This is recorded per-item in `docs/micro_checklist.md`.
- **Real bugs found and fixed along the way**, not glossed over: a tenant-ID trust bug (missing-header silently granted admin), a location-filter bug that hid records from non-admin users on two different doctypes, a TOCTOU double-decision race in the approval engine (found by a concurrent session, fixed with a row lock), a GRN that never actually posted stock, a re-approval-on-edit bypass, a negative-quantity return exploit, a timezone bug in account-lockout timing. Each is a real defect that was caught and closed, listed here as evidence the process finds and fixes things rather than only ever reporting success.
- **Concurrent-session discipline**: this repo has been edited by more than one AI session (and the user directly) in overlapping windows before. The adopted convention is: always run `git status`/`git diff` before staging anything, and stage only the specific files actually reviewed — never a blanket `git add -A` — specifically because a shared working tree can silently carry another session's in-progress work into a commit that didn't intend to include it.
- **Documentation-as-you-go, not after the fact**: three documents (`docs/micro_checklist.md`, `docs/project_ledger.md`, `docs/ai_handover.md`) are kept in sync after every unit of work, by standing convention in this repo's own `CLAUDE.md` — not written retroactively at the end of a project.

---

## 7. What We'd Want Outside Feedback On

Since this document's stated purpose is to go into an outside review (including an AI reviewer) for a second opinion, here's what would actually be useful feedback, rather than a generic "how does this look":

1. Is the metadata-driven DocType approach (§3) the right long-term bet for a system that needs to support several different industry verticals, or does it risk becoming a bottleneck as industry-specific logic accumulates inside a generic JSONB column?
2. Given no production deployment has happened yet (§1, §5), what's the highest-risk gap to close *before* a first real customer goes live — is it the data-entry UX item (Stage 18), the spec-vs-built gaps in §5, or something not on this list at all?
3. Is "AI-agent-assisted development with human direction and a live-verification discipline" (§4, §6) a build process an outside technical reviewer would trust for a system handling real money movement (GL postings, vendor payments) — and if not, what would change that?
4. Are there red flags in the multi-tenancy or security posture (§3) that warrant a dedicated external security review before go-live, beyond the internal hardening pass already done?
