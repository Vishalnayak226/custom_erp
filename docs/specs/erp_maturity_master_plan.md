# ERP Maturity Master Plan — Source Summary & Repo-Grounded Status

**Latest source document:** `ERP_Final_Leg_Maturity_Completion_Master_Plan.pdf` (10 sections + 2 appendices, dated 23 Jul 2026) — supersedes the original `ERP_Full_Fledged_Maturity_Master_Plan.pdf` (dated 19 Jul 2026) as the active maturity roadmap. This document has been rewritten in place to reflect the newer PDF; the original's phase framework/gap table is kept as history in §6 below rather than duplicated at the top.

**What this document is:** like the original, this PDF was generated from an outside review of this project's own state (ledger/checklist/blueprint docs), not an independent code audit. It is a maturity/roadmap framework layered on top of what's already tracked here — it does not replace `docs/micro_checklist.md` (authoritative task-level tracker) or `docs/project_ledger.md` (authoritative build history). Read in full and cross-checked against live repo/git state (build, vet, `go test ./... -p 1`, and file/profile existence checks) on 2026-07-23 — corrections below.

---

## 1. Phase Framework (PDF §8, "Step-by-Step Completion Roadmap")

| # | Phase | Work | Exit Criteria |
|---|---|---|---|
| 0 | Truth Freeze and Regression Fix | Freeze latest branch, reconcile docs/checklist/code, fix finance regression | One current tracker; full test suite clean |
| 1 | Production-Like Environment | Deploy test/live environments, TLS, secrets, backups, alerts, monitoring | Non-prod production-like environment ready for real UAT |
| 2 | External Credentials and Regulatory Integrations | Connector credentials, GSP/IRN/e-way sandbox, alert webhook, payment terminal sandbox | All external flows verified success/failure/retry/cancel |
| 3 | Reachability and Usability Gaps | GRN workbench, PR entry, Approval Rules UI, WMS operation screens, vendor override payment control | Every built backend capability has a usable screen or a conscious API-only reason |
| 4 | PIM/PXM Maturity Sprint | Channel packs, supplier portal, quality rules, search feed, media governance | PIM comparable to mature commerce PIM for our categories/channels |
| 5 | WMS Enterprise Maturity Sprint | ASN/dock, directed putaway, wave/batch/cluster, RF/mobile, cartonization, labor, slotting | WMS can run warehouse ops without Excel/manual workarounds |
| 6 | Finance/Tax Close Sprint | IRN/e-way, GST registers, P&L/BS/GL, payment file approvals, month-end close pack | Finance team can close a period and answer auditor questions |
| 7 | CRM/Loyalty Sprint | Segmentation, vouchers, tiers, campaigns, fraud controls, lifecycle dashboards | Marketing/store teams can run loyalty without external spreadsheets |
| 8 | HR/Payroll Sprint | Rosters, payroll, statutory, employee self-service, HR dashboards | HR can run monthly payroll and leave/attendance ops |
| 9 | Manufacturing/MRP Sprint | Multi-level BOM, routing, work centers, MRP, shop-floor, QC, costing | Manufacturing can plan/cost jobs without manual parallel sheets |
| 10 | Reports and BI Sprint | Complete report catalog, role dashboards, scheduled exports, stock-ledger wiring, drill-downs | Ops/management dashboards become decision-ready |
| 11 | Security, Scale, UAT and Go-Live | External pentest, load/soak/spike tests, DR drill, migration rehearsal, user UAT, hypercare | Signed go-live checklist and controlled pilot launch |

This is now the organizing spine for the buildable backlog — see **Stage 26** in [`micro_checklist.md`](../micro_checklist.md), numbered `26.0`–`26.11` to match these phase numbers 1:1.

The PDF's own executive verdict: strong foundation, medium-high operational maturity on the retail/jewellery reference vertical, enterprise maturity still incomplete (production deployment, real credentials, regulatory integrations, wider industry packs, external security/performance sign-off). Final leg, not a restart.

---

## 2. P0 Blockers (PDF §5), reconciled against this repo on 2026-07-23

| ID | PDF item | Status as actually found in this repo |
|---|---|---|
| P0-1 | Fix finance trial-balance regression (test expects 9000, gets 9500) | **Reproduced, but root cause is different from what the PDF assumes.** Ran `go test ./... -p 1` fresh on 2026-07-23: `TestEngines/FinanceDoubleEntryAndPOS` still fails with the *exact* 9000-vs-9500 numbers the PDF quotes — but the trial balance itself reports `balanced:true`, `total_debits == total_credits == 9500`. The ledger is internally consistent; the test's own fixture expectation is 500 short. Combined with every prior session's finding that this reproduces identically via `git stash` across unrelated commits (see [[erp-test-suite-race-gotcha]]), this is not a broken double-entry posting path — it is leftover fixture debris in the one shared, persistent `custom_erp` dev DB that every `go test` run (and every dev/throwaway-port session) writes to, with no per-run isolation (no dedicated test schema, no truncate-before-test). **Correct P0-1 action: give the finance test its own isolated schema or an explicit truncate/reset of the GL fixture rows before asserting exact totals — not a code fix to `engines/finance.go`.** Re-run once isolated to confirm no real posting bug hides underneath (very likely none, given `balanced:true`).
| P0-1b | (new, found during this reconciliation) `TestEngines/DocTypeValidationAndAuth` also still fails | Same shared-DB root cause, different symptom: the `Brand` doctype has accumulated a `fefo_enabled` mandatory field from earlier Agriculture industry-profile testing on the same DB (confirmed 2026-07-22, still present 2026-07-23). Same fix category as P0-1 — this is why Phase 0's exit criterion should be "isolate or reset test fixtures," not "patch two unrelated engines."
| P0-2 | Production environment | **Confirmed open.** No real host/DNS/TLS/secrets-vault exists; dev is portable Go+Postgres on this machine only. Needs the user's hosting decision. |
| P0-3 | Real alert contacts | **Confirmed open, code-complete.** `engines/alerting.go` + `docs/operations/incident_runbook.md` (Stage 17.10) work against a mock webhook; only real contacts/URL missing. |
| P0-4 | Live connector verification | **Confirmed open, code-complete.** Shopify/BigCommerce/Magento connectors + `scripts/verify_connector_live.ps1` (Stage 16, 17.11) exist; blocked only on real non-production store credentials. |
| P0-5 | Regulatory (GSP/IRN/e-way) credentials | **Confirmed open** (Stage 20.30/20.31) — no GSP sandbox account exists yet. |
| P0-6 | Role permission decisions | **Partially done, partially open.** The *mechanism* is already built and live: sidebar visibility derives automatically from `role_permissions` (Stage 22.6, `GET /api/v1/me/permissions`), so "role-filtered sidebar aligns with API RBAC" is structurally guaranteed, not a future task. What remains open is a **business sign-off on the exact permission values** per role (Store Manager/Cashier/WH/Finance/Product/HR-Admin/Developer-Admin) — a decision for module owners, not a code gap. |
| P0-7 | End-to-end pilot data pack | **Confirmed open.** No realistic 30-day UAT dataset exists; only ad hoc test-session fixtures (cleaned up after each stage per this repo's own verification discipline). A synthetic representative pack is buildable now; a truly business-real one needs the user's actual product/vendor/customer lists. |

**Also found, not in the PDF:** the PDF's Current-State snapshot (§3) and its Industry Packs final-leg table (§6.10) both claim only "Jewellery, F&B, Automobile, Clothing are real" and ask for "Pharma, Medical Device, Steel, Construction, Agriculture, Semiconductor, Logistics/Transportation" as new work. **This is stale.** `public/profiles/` already has 10 real profile files: `jewelry`, `food_bev`, `auto`, `clothing`, `pharma`, `metal`, `construction`, `medical`, `semiconductor`, `agriculture` (Stage 12.1, 2026-07-21 — the profile files existed even earlier; only the backend allowlist was missing them, since fixed). Of the PDF's 7 requested packs, 6 already map onto existing profiles (Pharma, Medical Device→`medical`, Steel→`metal`, Construction, Agriculture, Semiconductor) — **only Logistics/Transportation is a genuinely new pack.** Don't re-propose the other 6 as new work.

---

## 3. Module-by-Module Final Leg (PDF §6) — condensed

The PDF gives a "Current / Final Leg Work / Benchmark Target" table for 10 module groups: PIM/PXM (§6.1), WMS/Inventory (§6.2), POS/Omnichannel (§6.3), Finance/Tax/Compliance (§6.4), Procurement/Vendor/GRN (§6.5), CRM/Loyalty (§6.6), HR/Payroll (§6.7), Manufacturing/MRP (§6.8), Reports/Analytics (§6.9), SaaS Ops/Extension/Industry Packs (§6.10). Rather than reproduce all ~90 rows here, each has been broken into numbered, build-sized items as **Stage 26** (`26.4`–`26.10`) in `micro_checklist.md`, one sub-track per module, following the same Track-A/Track-B split Stage 20 established. The full PDF stays the source of record for *why* each item matters (benchmark rationale, market-leader comparison) — the checklist stays the source of record for *what's actually done*.

**Scope-tiering note (applies across every module):** several "Final Leg Work" cells describe genuinely large, possibly over-scoped capabilities for this platform's stage — full RF/voice picking, warehouse robotics/conveyor APIs, 3PL client billing portals, AI-assisted PIM content, full appraisal/performance-management HR. These are flagged in Stage 26 as `[P2 — tier/scope decision needed, do not build speculatively]` rather than silently built or silently dropped, per this repo's standing practice of surfacing product decisions explicitly instead of guessing.

---

## 4. Market-Leader Benchmark Gaps (PDF §7)

Read this section as *pattern inheritance*, not screen-by-screen cloning (the PDF says the same): the goal is configurable workflows, auditability, extensibility, channel readiness, strong reporting, and safe async integration behavior — patterns this codebase already applies elsewhere (outbox/event pattern, maker-checker approval engine, generic report registry) — applied more deeply to PIM, WMS, Finance, POS, Manufacturing, and CRM/Loyalty specifically. No new architectural pattern is implied; Stage 26's module tracks reuse the existing choke points (approval engine, `BulkImportCSV`, `ReportDefinition`/`RegisterReport`, `writeAPIError`) per `CLAUDE.md`'s first principle.

**Minimum Pilot vs Full Market-Grade (PDF §7.2):** the PDF distinguishes 5 release levels (internal technical pilot → business pilot → company rollout → SaaS external customer release → market-grade platform). None of these gates can be *claimed* by an AI session alone — they require the user's own infra/credential/UAT sign-off — but Stage 26 is scoped so the **business pilot** gate (retail/jewellery vertical, PIM→PO→GRN→WMS→Transfer→POS/OMS→Return→Finance→Reports) becomes achievable purely through Track B (buildable-now) work plus the Track A user-inputs already tracked since Stage 20.

---

## 5. Final Go-Live and Maturity Sign-Off Checklist (PDF §9)

Kept here verbatim as a reference gate list (architecture / security / finance / inventory-WMS / POS / PIM / integrations / reports / performance / UAT / operations / documentation) — reuse the PDF's Appendix E-equivalent role as a **pre-pilot regression checklist**, the same way the original plan's Appendix E (ERP Loophole Prevention Matrix) was flagged for reuse. Don't attempt to "pass" this checklist item-by-item until Stage 26 Phases 0-10 are closed and Phase 11 (external security/performance/UAT) has real engagement — attempting it earlier just produces false sign-offs.

---

## 6. History: the original (2026-07-19) plan

The original `ERP_Full_Fledged_Maturity_Master_Plan.pdf` used a 10-phase (0-9) framework: Source Freeze → Production Infra Readiness → User-First Data Entry → Core Pilot Flow Hardening → Functional Depth Completion → Omnichannel/Integration Certification → Security/Performance/Resilience → SaaS Ops/Support → Pilot Go-Live → Commercial Maturity Expansion. Its P0/P1 gap table drove **Stage 20** (`micro_checklist.md`), which closed nearly all of Track B (POS/WMS/Finance maturity, Reports engine) between 2026-07-20 and 2026-07-23 — only Track A (user-input-blocked items) and 20.30/20.31 (GSP-blocked) remain open, and those same items are carried forward unchanged into the new plan's Phase 1/2/11 above (no re-numbering needed, just re-homed under the new phase names). The new (2026-07-23) PDF is a deepening of the same plan, not a divergent one — same reference vertical, same don't-rewrite-the-kernel stance, same "final leg not a restart" verdict — with much more module-level granularity (§6.1-6.10) and an explicit market-benchmark section (§7) that the original didn't have.

**Appendices worth reusing verbatim (from either PDF):** the original's Appendix E (ERP Loophole Prevention Matrix) and Appendix B (UAT Scenario Pack) remain useful pre-pilot QA tools — several of the matrix's required controls already exist here (re-approval-on-edit, duplicate-invoice DB constraint, row-locked barcode scans, location-scoped stock checks, period-close control, webhook HMAC verification, `moduleGate`). The new PDF's Appendix A (a 10-item immediate-action list for the engineering head) and Appendix B (benchmark references — Akeneo/Pimcore/Unbxd for PIM, Infor/SAP EWM/Manhattan for WMS, SAP/Dynamics for finance) are folded into §1/§3/§4 above rather than repeated as a separate list.

---

## 7. Source notes

This document summarizes and reconciles the 2026-07-23 PDF above; it supersedes its own 2026-07-19 content (kept as §6 history) and does not affect `docs/specs/pdf_blueprint_gap_analysis.md` (still the historical record of the original 2026-07-12 gap baseline) or `docs/micro_checklist.md` (still the current source of truth for what's built at task level). See `docs/ai_handover.md` §6 for the handover-snapshot pointer to this update.
