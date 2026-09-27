---
doc_id: DOC-PRODUCT-BLD-002-INVENTORY
title: BLD-002 reference-configuration inventory (READY half)
type: reference
status: draft
owner: product-owner
approvers: [product-owner, qa-owner]
audience: [product, engineering, QA]
applies_to: docs/product/erp-build-checklist.md BLD-002
authority: source
confidentiality: internal
last_verified: 2026-09-18
review_by: 2026-10-18
supersedes: none
superseded_by: none
---

# BLD-002 reference-configuration inventory (READY half)

[BLD-002](erp-build-checklist.md) reads: "Name proposed reference customers, country,
supported browsers/devices, tenant size, enabled modules, user roles and economic
outcomes. Reconcile open Stage 20/26/31/34/35–39/47–49 tasks into those outcomes
without discarding IDs or converting historical audit passes into current assurance."
It is split **READY for inventory; DECISION for customer scope**. This document is the
READY half only — a factual compilation of what this codebase already supports/proposes,
with citations. It makes no product decision and approves nothing. The **DECISION half —
which of the candidates below (if any) becomes the actual reference customer scope for
Wave 8 (BLD-057, JRN-01–23, DEC-01–08)** — is explicitly left for the product/QA owner,
per the build checklist's own note. Do not read anything below as approved scope.

## 1. Existing draft reference-customer candidates

Two candidates are already drafted (status: **draft**, not approved) — this document does
not invent them, it surfaces what's already proposed so the decision has a real starting
point instead of a blank page:

| ID | Description | Country | Maturity | Source |
|---|---|---|---|---|
| `REF-RETAIL-IN` | India retail (single/multiple stores) | India | Preview evaluation | `docs/product/vision.md:24-32`, `docs/product/capability-register.json:6-26` |
| `REF-WAREHOUSE-IN` | India warehouse/wholesale distribution, one stock owner per warehouse | India | Preview evaluation | same |

Both are also rendered in `docs/generated/capability-catalog.md:11-14`. `docs/product/vision.md`
itself is `status: draft` and is 48.2.1's own open item (needs Product Head/CEO approval) —
the candidates above are proposals, not commitments. No non-India candidate is proposed
anywhere in the docs tree.

## 2. Country / localization inventory

- **Only India has real tax-compliance logic.** `engines/gst.go` implements CGST/SGST/IGST/HSN
  (`CalculateGST`, `ComputeGSTForLines`). No VAT or other-country tax engine exists.
- **Phone/locale support is generic across 54 countries** (`engines/phone.go:137-192`,
  `phoneCountryRules`; confirmed in `docs/guides/ADMIN_GUIDE.md:163`). `DefaultPhoneCountry = "IN"`.
- Tenant home country is a per-tenant setting (`SettingKeyDefaultCountry`,
  `engines/settings_definitions.go:446,457-474`), populated from the same 54-country table.
- **Currency is generic ISO-4217** (`engines/currency.go:15`), multi-currency documents and FX
  exist (`engines/currency_documents.go`, `currency_fx.go`); reporting currency defaults to INR
  but is tenant-configurable (`docs/guides/ADMIN_GUIDE.md:287`).
- **Reading:** a non-India reference customer is technically possible (generic phone/currency),
  but would run with NO tax-compliance engine at all (GST is India-only) — any non-India
  candidate needs an explicit decision on whether "no tax engine" is acceptable for that
  journey's acceptance bar, not just a country-name swap.

## 3. Supported browsers / devices inventory

- **No approved minimum-browser-version matrix exists.** `docs/qa/control-and-device-matrix.md`
  is a **draft/template**, not a filled-in supported list (QA-MAT-001).
- **What has actually been machine-verified** (Chromium via Playwright only — no Firefox/WebKit
  found anywhere in this repo's tooling):
  - `docs/qa/audit-browser.cjs:31-32` — three contexts: `desktop-light` (1440×900),
    `mobile-dark` (390×844), `tablet-keyboard` (768×1024), locale `en-IN`, reduced motion.
  - `docs/qa/audit-ui-tasks.cjs:10` and this session's own BLD-010/011 live-verification pass
    reused the same shape (1440×900 / 390×844 / 1440×900).
  - `docs/product/erp-build-checklist.md:423` (BLD-038) names "three meaningful contexts" as
    the target acceptance bar — i.e. this IS the intended methodology, not a stopgap.
- **Physical/RF hardware (scanners, printers, real QZ Tray) is explicitly unverifiable from a
  dev machine** — `docs/micro_checklist.md` 47.6.6 and 31.1.8 both stay open on exactly this,
  needing a real warehouse/POS bench.
- **Reading:** "supported browsers" today, as a matter of fact rather than policy, is
  "Chromium, three viewport/theme contexts, en-IN locale." No evidence this repo has ever
  tested Safari/Firefox/WebKit or a non-en-IN locale end-to-end. If a reference customer needs
  broader browser support as an acceptance criterion, that is new verification scope, not
  something already covered.

## 4. Tenant size inventory

- **No Starter/Growth/Enterprise or small/medium/large tier concept exists anywhere in code or
  docs.** Searched `engines/saas.go`, `docs/product/`, `project_ledger.md` — none found.
- The only "package" concept is **module-based, not size-based**: `engines/modules.go:58-72`,
  `ProductPackages` — exactly 10 entries (`pim`, `wms`, `oms`, `hr`, `procurement`,
  `manufacturing`, `crm`, `assets`, `expenses`, `erp_full`), plus 5 always-on modules (`core`,
  `master_data`, `inventory`, `sales`, `finance`). Confirmed in
  `docs/product/erp-maturity-roadmap-2026-09-16.md:35-36,80-97`.
- "Tenant size" appears only as a **capacity-planning/forecast dimension** (47.18.2, 49.18.7 —
  "daily/monthly volume by tenant size"), never as a commercial or acceptance tier.
- Aspirational positioning language ("SMB/mid-market", benchmarked against Dynamics 365
  Business Central and Infor WMS 11.5) exists in `docs/specs/parity_master_plan.md` and
  `docs/specs/wms_parity_plan.md` — not a shipped tier, not a test configuration.
- Twelve open SaaS-packaging decisions (`PKG-01..12`) exist, including PKG-06 (commercial
  subscription model) — none define a size tier; all are open decisions, not built features.
- **Reading:** "tenant size" cannot be named as a reference-customer dimension today beyond
  "which of the 10 module packages + 5 always-on modules are entitled." A real
  small/medium/large distinction (data volume, seat count, location count) would need its own
  decision and is not something this inventory can resolve by citation alone.

## 5. Enabled-modules inventory

Three overlapping but distinct module taxonomies exist in this codebase — a reference-customer
definition needs to say which one(s) it means:

| Taxonomy | Count | What it gates | Source |
|---|---|---|---|
| Entitlement modules (`public.modules`) | 20 | Machine-enforced `module_key` package gate | `docs/product/erp-maturity-roadmap-2026-09-16.md:35-36`; assembled across `db/migration.sql`, `migrations_stage14a_modules.sql`, `stage18_core_module_fix.sql`, `stage27_product_packaging.sql`, `stage37_9_quality_maintenance.sql`, `stage38_4_webhook_subscriptions.sql` |
| Doctype-metadata module labels | 18 | Role-template `AccessLevel` grants | `engines/role_templates.go:300-304` (`knownModules`) |
| Product-capability groups | 14 | Business capability register / maturity claims | `docs/product/capability-register.json`, `docs/generated/capability-catalog.md:16-33` — all currently "Preview" or "Experimental," none "Production"/"Certified" |

219 registered document types total (`docs/product/erp-maturity-roadmap-2026-09-16.md:35`).
Both existing draft reference candidates (§1) map to all 14 capability groups, unfiltered — a
real per-customer module subset (matching one of the 10 `ProductPackages`) has not been drafted
for either candidate yet.

## 6. User-roles inventory

- **Current declarative role-template system** (Stage 47.1.5 rebuild), exactly **12 templates**
  (`engines/role_templates.go:104-306`): Administrator, Store Supervisor, Cashier, Picker,
  Warehouse Manager, Accounts Payable, Accounts Receivable, Accountant, HR Manager, Employee
  Self-Service, Auditor, Integrator. Each declares module-level `AccessLevel`
  (None/Read/Operate/Manage), doctype overrides and named capabilities.
- **Legacy role names remain accepted** via a `LegacyNames` mapping (e.g. `RoleSuperAdmin` →
  Administrator, `RoleStoreManager` → Store Supervisor) so existing tenant data keeps working —
  `RoleTemplateFor` resolves both old and new names.
- `docs/security/verification-standards-matrix.md` and Stage 49.3 already treat "every baseline
  role" as the unit of authorization testing.
- **Reading:** 12 is the canonical current role set for any pairwise role/device/locale matrix
  (BLD-020) built against these candidates; legacy 3-4-role naming (Super Admin/Store
  Manager/Cashier/Supplier, `docs/guides/PERMISSION_MATRIX.md`) is the pre-rebuild set and
  should not be re-cited as current without noting the mapping.

## 7. Reconciliation of Stage 20/26/31/34/35–39/47–49 open items

Full per-item detail (IDs, one-line descriptions, exact `micro_checklist.md` line numbers) is
preserved in the research transcript behind this document and in `micro_checklist.md` itself —
this section classifies rather than duplicates it, per this repo's index-not-copy convention.
No ID below is discarded, resolved, or reinterpreted as closed by this document.

**Stages contributing nothing actionable:** 35 (OMS parity), 36 (PIM parity, archived), 37 (ERP
core depth) are fully closed — zero open items in any of the three.

**External-input-blocked (credentials, vendor engagement, physical hardware) — not blocked on
any product decision, just waiting on an external party or asset:**
- Stage 20: 20.1/20.2/20.3/20.4/20.5 (escalation contacts, ops webhook, connector sandbox creds,
  hosting decision, external reviewer), 20.30/20.31 (GSP e-invoice/e-way bill creds)
- Stage 26: 26.1.3b (Cloudflare WAF activation), 26.2.1–26.2.5 (duplicates of the above plus
  payment-terminal sandbox creds), 26.6.9 (blocked on 26.2.3/26.2.4), 26.11.1/26.11.5/26.11.6
  (pentest vendor, business UAT users, hypercare sign-off — all have drafted plans waiting on
  people, not code)
- Stage 31: 31.1.8 (physical QZ Tray/printer bench)
- Stage 47: 47.6.6 (device/scanner/printer certification)
- Stage 49: 49.7.2/49.7.3/49.7.7/49.7.8 (need a real host, not a decision)

**Decision-gated (need a product/legal/business call before any build work is possible) — the
ones most relevant to reconcile against a reference-customer decision:**
- Stage 26.10.6: data-mart/read-replica — already decided "not justified" as of 2026-08-06,
  revisit thresholds recorded; not a live blocker.
- Stage 34: 34.4/34.5 gated on 34.6 (legal/ToS position on competitor-price scraping) — a real
  build-or-skip decision, independent of reference-customer scope.
- Stage 38: 38.2/38.2d (OAuth2 client-credentials) — deferred by design "unless an integration
  needs it"; only relevant if a reference customer's journey specifically requires it.
- Stage 39: 39.18 (migrate 5 legacy guide docs into `docs/kb/`) — deliberately deferred as its
  own reviewed unit; unrelated to reference-customer scope.
- SB-021 (bootstrap password rotation for `cashier1`) — production-deploy blocker, not a
  reference-customer scoping question, already tracked separately in the handover.
- Stage 47: 47.8.3 (session/token topology), 47.12.5 (bootstrap governance), 47.16.x (all
  counsel/GST-practitioner-gated), several residual clauses inside otherwise-built 47.6/47.9/
  47.15 items.
- Stage 48: 48.2.1 (vision.md approval — directly gates whether §1's two candidates can become
  real scope), 48.6.6 (legal document register).
- Stage 49: 49.0.6 (risk-acceptance authority), 49.6.3/49.6.4/49.6.7/49.6.8 (encryption/telemetry/
  consent-withdrawal calls), 49.14.3/.5/.6/.7/.8 (floor posture, break-glass, device baseline,
  training, personnel controls), 49.16.1/.4/.5 (all counsel/business-owner gated).

**Real, undecided-on, buildable engineering work** (the bulk of Stages 47/48/49 — roughly 70,
35 and 95 open sub-items respectively, grouped by sub-theme in the research transcript): this is
genuine backlog, not blocked on anything in this document. It is NOT reconciled into either
reference-customer candidate's journey list yet — that reconciliation is explicitly the
DECISION half's job once a candidate is chosen, since "which of this backlog matters" depends
on which customer/country/module-set is actually being tested against.

## 8. What the DECISION half still needs to resolve

This document does not choose any of the following — it only makes the choice concrete:

1. Approve, adjust, or replace the two drafted candidates (`REF-RETAIL-IN`, `REF-WAREHOUSE-IN`)
   as the actual BLD-057/JRN-01–23 reference scope, or explicitly decide to run without a named
   reference customer for this pass.
2. Decide whether "Chromium, three viewport contexts, en-IN locale" (the only thing ever
   actually verified) is the accepted supported-browser/device bar for this release, or whether
   broader coverage (Safari/Firefox, other locales) is required before BLD-038/BLD-020 close.
3. Decide whether a tenant-size dimension (beyond the existing 10 module packages) is needed at
   all for this pass, or whether module-package selection alone is sufficient.
4. For each candidate approved, name its enabled-module subset (which of the 10 `ProductPackages`
   + 5 always-on modules) and its user-role subset (which of the 12 templates), rather than
   testing all 219 doctypes/14 capability groups unfiltered.
5. Reconcile which of the "real, undecided-on, buildable engineering work" items from §7 actually
   feed the chosen candidate's journeys (JRN-01–23) versus which stay backlog outside this pass's
   scope — owner, prerequisite and exclusion rationale per BLD-002's own Done bar.

Sources: `docs/product/erp-build-checklist.md` (BLD-002, 48.2/48.3, MC-019/020/135/136),
`docs/product/vision.md`, `docs/product/capability-register.json`,
`docs/generated/capability-catalog.md`, `docs/qa/control-and-device-matrix.md`,
`docs/qa/erp-maturity-checklist.md`, `docs/micro_checklist.md` Stages 20/26/31/34/35–39/47–49.

## 9. Decisions recorded (2026-09-22, product/QA owner)

The five §8 calls are resolved as follows. This closes BLD-002's DECISION half; §1-8 above stay
as the research record they always were (not rewritten to match the decision retroactively).

1. **Reference customers: both candidates approved as drafted, no changes.** `REF-RETAIL-IN`
   (India retail, single/multiple stores) and `REF-WAREHOUSE-IN` (India warehouse/wholesale
   distribution, one stock owner per warehouse) are the BLD-057/JRN-01-23 reference scope for
   this pass. They remain `docs/product/vision.md`'s own **draft** status pending 48.2.1's
   separate Product Head/CEO sign-off on the vision document itself — approving them as the test
   configuration here does not substitute for that sign-off, which stays open.
2. **Browser/device bar: Chromium, three viewport contexts (1440×900/390×844/768×1024), en-IN
   locale is the accepted release bar.** No Safari/Firefox or additional-locale coverage is
   required before BLD-020/BLD-038 close this pass. Broader coverage remains real, named backlog
   (not silently dropped) if a future release needs it — record it against BLD-038 rather than
   re-opening this decision.
3. **Tenant-size dimension: approved, added on top of module-package selection.** No commercial
   tier exists or is created by this decision — PKG-06/BLD-026 (subscription/commercial model)
   stays a separate, still-open decision. For **test-configuration purposes only**, three size
   tiers are defined, reusing the existing per-tenant `tenant_limits`/`CheckTenantLimit` mechanism
   (`engines/tenant_limits.go`) rather than inventing a parallel one:

   | Tier | `max_users` (the one limit key actually wired to a check today, `handlers_admin_identity.go:195`) | Descriptive secondary sizing (not yet enforced by any limit key — for test-dataset sizing only) |
   |---|---|---|
   | Small | 1-5 | REF-RETAIL-IN: 1 store. REF-WAREHOUSE-IN: 1 warehouse, <10 bins. |
   | Mid | 6-25 | REF-RETAIL-IN: 2-5 stores. REF-WAREHOUSE-IN: 2-5 warehouses, 10-200 bins. |
   | Large | 26+ | REF-RETAIL-IN: 6+ stores. REF-WAREHOUSE-IN: 6+ warehouses, 200+ bins. |

   Only `max_users` is actually enforceable today (no `limit_key` for location/warehouse/bin
   count exists yet — that would be new backlog, not assumed here). BLD-020's "three meaningful
   context rounds" should use these three tiers as its size axis for each candidate: 3 tiers ×
   2 candidates = 6 base configurations before combining with role/browser/locale.
4. **Module/role subsets: proposed here for review, not yet re-confirmed by the owner.** Per
   §5/§6's inventories (10 `ProductPackages`, 5 always-on modules, 12 role templates):

   | Candidate | Enabled optional packages | Always-on (all candidates) | Role subset tested |
   |---|---|---|---|
   | `REF-RETAIL-IN` | `pim` (product catalog), `oms` (omnichannel orders), `crm` (loyalty/CRM) | `core`, `master_data`, `inventory`, `sales`, `finance` | Administrator, Store Supervisor, Cashier, Accountant, Auditor |
   | `REF-WAREHOUSE-IN` | `wms` (core to the candidate), `procurement` (inbound), `oms` (outbound/wholesale orders) | `core`, `master_data`, `inventory`, `sales`, `finance` | Administrator, Warehouse Manager, Picker, Accounts Payable, Auditor |

   This replaces the current unfiltered "all 14 capability groups" testing surface (§5) with a
   named subset per candidate — BLD-022's ten package journeys still separately cover `hr`,
   `manufacturing`, `assets`, `expenses` and `erp_full` on their own terms; those packages are
   correctly **not** claimed by either reference customer. **Known gap, stated rather than
   hidden**: `Accounts Receivable`, `HR Manager`, `Employee Self-Service` and `Integrator` (4 of
   the 12 role templates) are not exercised by either candidate's role subset — real backlog for
   BLD-020/BLD-022 to pick up with a third candidate or an explicit role-only pass, not silently
   dropped.
5. **Backlog reconciliation into JRN-01-23: unblocked by decisions 1-4, not attempted in this
   pass.** §7's "real, undecided-on, buildable engineering work" bucket (~200 sub-items across
   Stages 47-49) can now be filtered against the two approved candidates' actual module/role
   scope from decision 4 — that filtering is itself a substantial, separate piece of work
   (BLD-002/BLD-022's own remaining job), not something this decision-recording pass does as a
   side effect. Left honestly open rather than rushed.

Sources for the tier/subset proposal: `engines/tenant_limits.go`, `internal/server/handlers_admin_identity.go:195`,
`engines/modules.go:58-72`, `engines/role_templates.go:104-306`, this document's §1/§4/§5/§6.
