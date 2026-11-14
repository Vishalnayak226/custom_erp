---
title: Release notes (part 3)
section: Reference
order: 27
summary: Earlier releases, 2026-07-27 to 2026-08-10, continued from the release notes.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Release notes (part 3)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Release notes (part 3)

This page continues the [release notes](release-notes.md) with **27** earlier entries, 2026-07-27 to 2026-08-10.

## 2026-08-10

**Stage 41 — user-reported batch: schema editing, sticky headers, country-aware phone data, setup guidance, POS location** *(code + schema + docs)*

Five gaps reported directly by the user. **Numbered Stage 41, not 34.4** — 34.4 is the gated JSON-endpoint harvester in Stage 34, blocked behind 34.6's legal/ToS decision, so reusing that number would have read as the gate being crossed. Same reasoning §88 gives for Stage 40 not being 35. Full item detail in `micro_checklist.md` Stage 41.

**Stage 40 — user-reported batch: PO line items, field formats, the Super Admin rename, speed and motion** *(code + schema + docs)*

Six gaps reported directly by the user in one pass, built in the priority order they chose. **Numbered Stage 40, not 35** — Stages 35-39 are already allocated to the Parity Master Plan (§86), and 35.1/35.5/35.6 there mean entirely different things.

## 2026-08-09

**Stage 26.6.11 — tax-exempt / nil-rated / zero-rated goods** *(code + schema + docs)*

The last substantively-buildable item in the live checklist, and the only one still gated on a product decision. Stage 30.1.2 had made `hsn_code` **and a positive `gst_rate`** mandatory on Item — correctly, since checkout and PO creation both rejected an item lacking them, so "saveable at 0%" only ever meant "unusable later". The side effect was that genuinely untaxed goods (unbranded grain, fresh produce, salt, books, exports) became unsaveable, so a tenant selling ordinary exempt stock could not create the Item at all.

**Parity Master Plan — Stages 35-39 scoped against Unicommerce, Unbxd, SAP and Dynamics 365** *(docs, planning only)*

The user named four external benchmarks and asked for one consolidated plan to reach that level, plus a knowledge center at the end. All four were read directly — the Unicommerce OMS product page and the Uniware documentation portal (sale-order model, WMS API index, client-integration structure), the Unbxd PIM help centre's full 49-section information architecture, SAP Help/S-4HANA Cloud's line-of-business scope, and the Dynamics 365 product surface incl. Business Central — then reconciled item-by-item against real code rather than against the trackers' own claims.

## 2026-08-07

**Stage 34.1-34.3 built, the first production DR drill, and nightly backups finally switched on** *(code + schema + ops + docs)*

The user asked to clear everything actionable. That was two things: Stage 34's buildable half (34.1-34.3), and 26.11.3's restore drill. Both landed; both turned up something the checklist did not know. The drill's finding — that production had no nightly backup — was then signed off and fixed the same day (26.11.7). Item detail in `micro_checklist.md` (34.1-34.3, 26.11.3, 26.11.7).

## 2026-08-06

**Production formalisation, content assist, and go-live readiness docs** *(code + schema + ops + docs)*

The user asked to start building the whole blocked backlog, decisions first. Four decisions were taken up front (hosting: formalise the droplet; content assist: local/offline only; P2 bundles: two greenlit; credentials: all parked) and three more in a second round (no domain yet, deploy 33.2 + commit the loose docs, draft all three go-live docs). Item detail in `micro_checklist.md` (26.1.1, 26.1.3, 26.4.11, 26.10.6, 26.11.1/3/5/6, 33.2).

**Archive reconciliation + Stage 34 market-intelligence plan** *(docs)*

A planning pass, no code. Cross-referenced `docs/archive/micro_checklist_closed_stages.md` against the live tracker and turned §81's retained knowledge into a buildable Stage. Item detail in `micro_checklist.md`'s new header block, 26.6.11 and Stage 34.

## 2026-08-05

**Clearing the decision-gated backlog: MFA recovery, backups, supplier portal, smoothness** *(code + schema + docs)*

Review of open item 32.4 concluded it was a real bug already fixed by `0bee663` and already deployed — so the only genuinely unblocked item was closed before this session started. The user then approved, in one pass, every remaining item that was gated purely on a *decision* rather than on something only they could supply (credentials, hosting choice, a pen-test engagement, real UAT users, a physical printer). Five items, built and verified in sequence. Full per-item mechanics in `micro_checklist.md` (32.2, 32.3, 32.5, 26.1.6, 26.4.10).

**Retiring the OmniCore/"Buying Catalog" project, keeping its market-intelligence knowledge** *(docs)*

A standalone Python microservices stack (`Antigravity Projects/Buying Catalog/OmniCore` — FastAPI + Streamlit + SQLAlchemy/SQLite + Redis/RQ + docker-compose, services `crawler`/`pims`/`oms`/`erp`/`pos`, last touched 2025-12-27) was read in full, its knowledge extracted to `docs/product/research/market-intelligence.md`, and the folder deleted. Same disposition and the same reasoning as the standalone WMS project in §(1c1c050)/`docs/product/research/wms-benchmark.md`.

**Stage 31.1.9 — One-click print wired into the last three screens** *(code)*

Stage 31.1 built the QZ Tray bridge and wired stickers plus the marketplace-document file picker; 31.1.9 was the open remainder — a Print Label button on the logistics booking, the POS receipt over ESC-POS, and invoice printing. Item-by-item mechanics in micro_checklist.md 31.1.9.

## 2026-08-04

**Stage 33 — Dialog viewport fit & responsive form layout** *(code)*

User report with a screenshot of the New Location dialog, its header cut off at the top of the frame and no Save button anywhere: *"Nothing is visible here. So, set up. Make it more 2 column or 3 if required so it will be visible. All screen and config it should auto adjust with 100% to any percent 40% or whatever. If 120% also it should get auto adjusted."* Item-by-item mechanics in micro_checklist.md Stage 33.

**Stage 32.1 — Interaction responsiveness: the sidebar hover defect** *(code)*

User report after using the deployed app: *"The interface is not at all smooth. I have to click many times. Then it is working. Hover is also not working. I have to click on arrow then it is showing permanently else it is going away."* Item-by-item mechanics in micro_checklist.md Stage 32.1.

## 2026-08-03

**Stage 31.1 — QZ Tray silent printing** *(code + schema + docs)*

User request, framed from marketplace operations: *"For Myntra and other Marketplaces OMS, I want to crack QZ print automation"*, with Myntra support's own setup mail attached, and the clarification that it should be **one-click print for all** — from the ERP as well as for the marketplace documents the channels supply. Three scope questions were put to the user first (what it drives, how QZ is authorised per PC, and what the label hardware is); the answers were one-click across both, self-signed/fully silent, and support all printer types. Item-by-item mechanics in micro_checklist.md Stage 31.1.

## 2026-08-02

**Stage 30.5.5 — Retiring `Stores`, folding its fields into `Location`** *(code + schema + docs)*

The last open item in Stage 30, and the only one that was blocked on a product decision rather than on work. `Stores` was promoted in the sidebar and documented as "your shop/warehouse locations", but it had **zero Link references and zero Go references** — nothing in the system could select one. A user following the manual created their shop somewhere no transaction could see it.

**Stage 30.5.8 — The picker consistency sweep** *(code)*

The audit item read as four unrelated chores: three screens missing a typeahead their siblings had, a 103-entry Location list with no structure, Employee picked with a `<select>` on five screens and a typeahead on a sixth, and icon-only buttons with no accessible name. They shared one cause — **there was no single place to be consistent in**. All 42 picker call sites configured `attachTypeahead` themselves, so "how a Location is chosen" was a decision made fifteen separate times, which is precisely how three screens came to be missing it without anyone noticing.

## 2026-08-01

**Stage 30.5.11 — Retiring the Dashboard landing screen** *(code + docs)*

User's call, made looking at the screen: *"Dashboard not required. Everything it is showing from config."* It was accurate — the screen held four derived stat cards (registered record types, audit-log count, active tenant, a hardcoded "Operational" pill) over nine shortcut tiles into **Database Schema Design, Dynamic Labels, Prefix Configs, Extension Hooks, Activity Log, Configuration, System Status, Tenant Entitlements** and **Tenant Usage**, every one of which the Settings module already lists. It owned no business data; it was a second front door to configuration.

**Stage 30.3/30.4/30.5 + the 29.7/29.8 strays — the manual overhaul and the UX sweep** *(code + docs)*

User request: finish the whole remaining Stage 30 manual/UX backlog in one pass, plus three items the previous session had flagged as *open but mis-filed inside the closed-stage archive*. Three genuine product decisions were taken with the user up front so the rest could run end to end without stopping: Trial Balance scoping (**mandatory as-of date**), the screenshot workflow (**scripted Playwright captures**), and the two flagged status transitions (**allow both, reason-code required**). Full item-by-item detail in **micro_checklist.md** Stage 29.7/29.8 follow-ups and Stage 30.

**Stage 30.8 — The project brain map** *(tooling + docs)*

User request: a graphify-driven diagram of the entire project, in its own folder under `docs/`, that "may behave like a brain", is appended to as the system grows, and is easy to update. Built as `docs/brain/` — `BRAIN.md` (7 Mermaid diagrams + a card per region), `brain.html` (interactive, self-contained), `brain.map.json` (the only hand-edited file) — generated by a new stdlib-only `cmd/brainmap`. Item-by-item mechanics in micro_checklist.md Stage 30.8.

**Stage 30.7 — Config sweep ("nothing hardcoded") + POS offer engine** *(code)*

Two requests the user framed as separate and which were built as such: every module's operational config surfaced on the Configuration page and *properly wired* ("if I change it should immediately change everywhere"), and offer configuration for POS, set up in the ERP and reflected at the till. Two scope questions were put to the user first — which offer families to support (answer: all four) and what to do about platform safety limits (answer: expose them, but do it properly) — and the build followed those answers. Item-by-item mechanics in micro_checklist.md Stage 30.7.

**Stage 30.6 — Server-issued document numbers** *(code)*

User request, made from the Purchase Orders screen: *"PO number will be auto created. I will create po sequence. Same of GRN and all other transaction."* Two scope questions were put to the user before building (how wide, and what shape the numbers take); both were answered, and the work followed those answers. Item-by-item mechanics in micro_checklist.md Stage 30.6.

## 2026-07-31

**Stage 30.1/30.2 — the layman audit's P0 block, closed** *(code)*

User asked to start building open checklist items, explicitly not phase by phase. Took the whole P0 block of Stage 30 (the 2026-07-30 layman audit, `docs/archive/audits/ux-manual-audit.md`) — seven items — plus two backend items from 30.5 that fell out of the same work. Item-by-item mechanics in micro_checklist.md Stage 30.

## 2026-07-29

**Stage 29.9 — Final SaaS module/menu naming pass** *(code + docs)*

User asked, ahead of a SaaS relaunch, whether reusing standard ERP nomenclature carries trademark/replication risk — confirmed no (generic functional terms like "Sales Order"/"Chart of Accounts" are industry-standard, not vendor IP; this repo's own Stage 21.11 ERPNext-style renames are the same category of naming). User then picked final names from a SAP/Microsoft/Oracle/NetSuite/Infor/Odoo/ERPNext comparison and asked for them applied everywhere. Item-by-item mechanics in micro_checklist.md Stage 29.9.

**Stage 29.8 — the last two loophole items closed: status-transition map + JWT session staleness** *(code)*

Both remaining items in `ERP_LOOPHOLES_ANALYSIS.md` were blocked on a decision rather than on work — one marked `[needs design decision]`, the other deferred by standing policy. The user asked to be asked; they chose opt-in-strict enforcement scoped to transactional doctypes **and** masters, a middleware live-state re-check over a jti denylist, and multi-key signing rotation. Item mechanics in micro_checklist.md Stage 29.8.

**Stage 29.7 — QC exhaustive-report follow-ups: gl_postings reporting index + production sslmode docs** *(code + docs)*

User asked for the two non-blocking observations in `QC_EXHAUSTIVE_REPORT.md` (O1 index, O2 sslmode) to be fixed, plus any other open items. Item-by-item mechanics in micro_checklist.md Stage 29.7.

## 2026-07-28

**Stage 28.5 — UI defect batch: dialog legibility, flyout reliability, global search** *(code)*

Four defects reported by the user in one session, all fixed in `public/app.js` + `public/styles.css` — no Go, no schema change, no new dependency. Item-by-item mechanics in micro_checklist.md Stage 28.5.

## 2026-07-27

**Stage 29 — OMS operations follow-up** *(code)*

User explicitly authorized the previously identified OMS follow-ups. Added a unified Order Management workbench over the existing generic document API; no duplicate reporting endpoint or table was introduced. Shopify and Unicommerce intake now normalize into `CreateSalesOrder`, preserving their legacy mapping tables while sharing the SalesOrder engine's SKU mapping, validation, allocation, reservation, hold, and idempotency behavior. Shipment handover now creates a single idempotent Draft SalesInvoice only after all fulfillment tasks ship, and tracking delivery dispatches the existing `Order Delivered` notification; invoice posting/settlement remain explicit finance actions to avoid implicit GL writes. Focused and full serialized tests pass; the first full run exposed a transient existing WMS-slotting fixture flake, whose isolated and subsequent full reruns passed. Stage 23.11 was reviewed again and correctly remains a no-op because no paste-grid product surface exists.

**Stage 28 — Configurability, Theming & Deployment** *(code, UNCOMMITTED as of this note)*

User request (one batch): every operational config should live in the admin UI, module by module — "nothing hardcoded"; a dark/light/system theme toggle; user-controllable report columns saved as profiles (one Universal/shared, one Personal); and Caddy as the reverse proxy for automatic Let's Encrypt TLS. Approved plan built all four in sequence, verifying + checkpointing after each. Full per-item mechanics in micro_checklist.md Stage 28.

