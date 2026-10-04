---
title: Release notes
section: Reference
order: 25
summary: What shipped and when, generated from the build ledger so this page can't say something the ledger doesn't.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Release notes
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: go run ./cmd/gendocs && go run ./cmd/genkb -->

# Release notes

Generated from `docs/project_ledger.md`'s own Stage sections - **115** entries as of this build. Each excerpt is that section's own opening paragraph, not a rewritten summary, so it reads like an engineering build log because that is what it is. For the full detail behind any entry, including what was verified and how, read the ledger itself.

## 2026-10-03

**Stage 50 — BLD-041 completed: native ESM screen loading meets both cold budgets, preserves entitlement/error/help/print paths, no bundler or npm scripts** *(code + tests + docs)*

**This supersedes the intermediate classic-script implementation and open status recorded in §180.** Expanded the split to 18 native ES modules; `public/app.js` retains the app shell, shared services, navigation and the existing access checks. A route's module is fetched only after `/api/v1/me/modules` has returned and `isMenuModuleVisible` permits it. `loadViewModule()` caches the in-flight import/result to avoid duplicate requests and adds a cache-busting retry URL after a failed import; the existing retry panel handles failure. No bundler, build output, package install or npm script is required. POS, standalone Returns and RF traceability have distinct modules, so first paint on POS does not download the other screens. Help and cross-domain reports/PIM actions use explicit lazy bridges. Shared document-table, formatting, QZ and print-sheet helpers stay in the shell to avoid a dependency waterfall when entering those screens.

## 2026-10-02

**Stage 50 — BLD-041 opened (lazy screen code and initial payload budgets): baseline measured, a classic-script lazy-loading approach decided over an ES-module rewrite, and the mechanism proven end-to-end on 4 real view groups (Manufacturing, Expenses, Fixed Assets, Stock Transfer) — small but real, verified payload reduction; item stays open** *(code + docs)*

First session to touch BLD-041. Measured the real baseline first rather than assuming: three cold-cache HTTP runs against a disposable scratch server (`Accept-Encoding: gzip`) put cold-core (index.html + styles.css + db.js + components/erp-typeahead.js + app.js + qz-print.js) at 347,586 bytes (~339.4 KiB) against the 180 KiB budget, and initial JS alone at 307,602 bytes (~300.4 KiB) against the 120 KiB budget — `app.js` alone is ~96.7% of the JS total and the only lever that moves the needle.

**Stage 50 — BLD-036's gap (2) closed for real (all ~28 remaining "Failed to load" sites individually re-verified, one genuine dead end found and fixed); gap (3) deliberately accepted as residual rather than chased a fourth time** *(code + docs)*

Picked up where the 2026-10-01 thread left off: BLD-036 had three named gaps across three prior sessions, of which gap (1) (double-submit guard coverage) was already closed that same day. Two remained: (2) ~20 of the 54 "Failed to load" sites assessed as action-triggered, non-dead-end fetches but never individually re-verified one by one, and (3) several bespoke section-refresh functions (`loadYardBoard`, `loadWarehouseCockpit`, `loadOMSOrders`, and the same shape elsewhere in WMS/Finance) still blank-then-fill instead of the doctype-table's stale-while-revalidate pattern.

## 2026-10-01

**Stage 50 — BLD-036 continued again (recoverable empty/loading/error/offline states): double-submit guard swept across 18 more bespoke composers app-wide, and a real, unrelated third-instance timezone-skew bug found and honestly flagged (not fixed) while investigating a regression-run failure — item stays open** *(code + docs)*

Picked up the prior thread's handoff verbatim: BLD-036 was left open on three named, narrower gaps (~30 bespoke composers outside the five guarded paths still unguarded; ~20 of 54 dead-end sites unaudited; the bespoke section-refresh screens still blank-then-fill). Confirmed via `git status` that the tree's uncommitted state was unchanged since that handoff. Given (1) is the most mechanical and lowest-risk of the three — a proven extraction pattern applied for a third time, not new design — worked it in full rather than splitting effort across all three.

## 2026-09-30

**Stage 50 — BLD-036 continued (recoverable empty/loading/error/offline states): double-submit guard extended to Approve/Reject and two bespoke composers, 13 more dead-end "Failed to load" screens given a real retry, stable-loading-layout genuinely assessed, and two real bugs caught by live-verifying the fixes themselves — still real progress, item stays open** *(code + docs)*

Picked up the prior same-day thread's handoff verbatim: BLD-036 was left open on three explicitly named gaps (the double-submit guard covering only two of many save paths; only 1 of ~54 "Failed to load" dead-end messages swept; "stable loading layout" not assessed at all). Confirmed via `git status` that the tree's uncommitted state was unchanged since that handoff before starting, and worked all three gaps in the same pass rather than picking one.

**Stage 50 — BLD-036 (recoverable empty/loading/error/offline states): a global API timeout added, a real duplicate-document-creation gap closed on the two highest-traffic save paths, and the Approvals dead-end load failure given a real retry — real progress, item stays open** *(code + docs)*

Picked up as the next-ready item off the prior thread's handoff (BLD-036 — READY after BLD-035's shared-form fixes, which landed 2026-09-29). Confirmed via `git status` that the tree's uncommitted state (BLD-046/BLD-043/BLD-035/documentation-hygiene, `docs/ai_handover.md` §6) was unchanged since that handoff, then started fresh, touching `public/app.js` only.

## 2026-09-29

**Stage 50 — BLD-035 (consistent forms and truthful transaction outcomes): a hardcoded "saved" response replaced with the document's real status app-wide, five silent save/submit/decide call sites wired to show it, missing Date/Currency form fields fixed, and an unrelated Stage-51.1-era edit/delete routing bug found and fixed live** *(code + docs)*

Continuing from the prior thread's priority order (BLD-035 first — DEPENDS on BLD-010/011/012, all done, and had sat READY across several sessions' handoffs without being picked up). Confirmed via `git status` that HEAD was still `afaf419` with the prior session's BLD-046/documentation-hygiene work uncommitted, then started fresh. Checked MC-031/042/082/089 before touching code — they live in `docs/qa/erp-maturity-checklist.md`, not `micro_checklist.md` (confirmed via grep first, per this file's own MC-089 sourcing note) — and found MC-042 ("approval transitions and user messages match economic outcome") already scored FAIL and MC-089 OPEN, so this was a real, acknowledged gap rather than a verify-and-close item.

**Documentation hygiene — split `docs/micro_checklist.md` down to pending-only items, moving every closed item into the archive at item/sub-group granularity for the first time** *(docs only, no code change)*

User asked to clean the live checklist down to pending work. Checked every live stage (20, 26, 31, 34, 38, 39, 47-52) against the file's own rule (a stage header may only be marked closed if 100% of its items are `[x]`) — none qualified, since every stage retains at least one genuinely open item (several from the 2026-09-28 session). The existing archive convention only ever moves a *whole* closed Stage, so at that granularity there was nothing new to move; asked the user how to proceed, and they chose the larger, non-standard option: pull every individual closed `[x]` item out of the still-open stages too, not just whole stages.

## 2026-09-28

**Stage 50 — BLD-046 (bounded imports, reports, exports and jobs): a real cross-worker tenant-scoping bug found and fixed, four bespoke tickers bounded, CSV import capped, one export converted to real streaming — real progress, item stays open on the "interrupted" leg** *(code + tests + docs)*

Continuing the same thread that closed BLD-034 (§172) earlier the same day. Per the priority order the prior session handed off (BLD-046 first — DEPENDS on BLD-043, done; BLD-035 next). Confirmed genuinely unstarted via `git status`/the build checklist's own `[ ]` mark before touching anything.

**Stage 50 — BLD-034 (usable dense tables): server-backed pagination/search/counts replacing a 500-row client-side cap, plus a search field-permission leak and a pre-existing refresh-persistence bug found and closed along the way** *(code + docs)*

Continuing the same thread that closed BLD-043 (§171) earlier the same day. Per the priority order the prior session handed off (BLD-034 first — DEPENDS on BLD-003/043, both done; BLD-046/035 next). Confirmed genuinely unstarted via `git status`/the build checklist's own `[ ]` mark before touching anything.

**Stage 50 — BLD-043 (realistic datasets and query plans): a real unindexed `sort=recent` cost found and fixed via measurement at three scales** *(code + docs)*

Per the priority order handed off from the 2026-09-25/27 threads (BLD-038/037), both of which turned out to already be closed and committed (`8bed150`) by the time this thread started — confirmed via `git status`/the build checklist's own `[x]` marks before touching anything. BLD-043 was the next genuinely-unstarted READY item (no DEPENDS beyond BLD-001, done).

## 2026-09-27

**Stage 50 — BLD-037 (coherent visual hierarchy and support context): environment-banner truncation, a half-empty Home stat-card, a missing correlation reference, and raw doctype identifiers leaking app-wide** *(code + docs)*

Picked up the priority order the 2026-09-25 thread handed off (BLD-037 first — no DEPENDS, explicitly the lightest of the three ready items). Confirmed genuinely unstarted via `git status` before touching anything. Live-verified throughout against the existing disposable Postgres 16.3 on loopback `:5462` (reused, already fully migrated per the prior session's own note) and a fresh scratch server on `:8973`, using real Playwright/Chromium across light/dark/mobile contexts. Full per-item detail `docs/micro_checklist.md` 50.13 and the build checklist's BLD-037 entry; this is the index-style summary.

**Stage 52 — Category-based sticker/label printing module: StickerTemplate designer, GRN/Transfer-Order-driven bulk printing** *(code + tests + docs)*

New feature thread, opened directly from the user's own request for a real label-printing module for a jewellery retailer: different categories need visually different labels, printing should start from a GRN/Transfer Order rather than manual SKU entry (whole document or one line at a time), and the user wants full self-service control over the label layout via a drag-and-drop designer. See `docs/micro_checklist.md` Stage 52 for full per-item detail; this is the index-style summary. Entered Plan mode given the scope (new doctype, a generic cross-doctype transaction loader, a from-scratch drag/resize canvas UI, generalized ZPL rendering) — three parallel Explore passes grounded the design in actual file:line reads (doctype registration is entirely migration-driven; `Item.category` is free text with no master behind it; GRN prints against `accepted_qty`; `Printer.dpi` existed but was dead code) before the plan was written and approved.

## 2026-09-25

**Stage 50 — BLD-038 (accessibility and localization acceptance): closed dialog tab-order/AX-tree leak, missing error announcements, forced-colors focus gaps** *(code + docs)*

Continued the priority order the 2026-09-24 thread handed off (BLD-038 first, then BLD-037 if time allowed). Confirmed genuinely unstarted via `git status` before touching anything. Live-verified throughout against a disposable Postgres 16.3 on loopback `:5462` (already migrated, reused from the prior session per its own handover note) and a scratch server on `:8179`, using Playwright/Chromium plus raw CDP (`Accessibility.getFullAXTree`, `CSS.getMatchedStylesForNode`) for what a keyboard/AT user actually experiences, not just what the CSS/markup claims. Full per-item detail `docs/micro_checklist.md` 50.13; this is the index-style summary.

## 2026-09-24

**Stage 51.9 — Jewellery field-set correction against real client data, and the `minn` PO&Inventory.xlsx master-data migration** *(code + tests + docs)*

User supplied the client's actual business workbook and asked for the ERP's own bulk-import template (not molding the ERP to the client's format), with our own generated Design ID/SKU — see `docs/micro_checklist.md` 51.9 for full detail; this is the index-style summary.

**Stage 50 — BLD-033 (task-oriented role navigation and onboarding): a new Home landing screen, permission-derived not role-name-branched** *(code + tests + docs)*

Built on the priority order the 2026-09-23 thread handed off: BLD-033 first (READY, DEPENDS on BLD-009/021, both done), then BLD-038/BLD-037 next if time allowed. Confirmed genuinely unstarted first (`git status` showed no file evidence). Full per-item detail `docs/micro_checklist.md` 50.13, build checklist BLD-033 entry; this is the index-style summary.

## 2026-09-23

**Stage 50 — BLD-031 (providers, public API and extensions): two real bugs found and fixed via local failure campaigns** *(code + tests + docs)*

Continuation of the same Stage 50 build thread as §160-163. HEAD unchanged at `82f5517`; nothing committed. Per the next-thread prompt's priority order, verified BLD-031 genuinely showed no file evidence in this tree (no outbox/webhook/contract test files, no matching mtime cluster from the 2026-09-20 dispatch) before starting — confirmed not started, built it.

## 2026-09-22

**Stage 50 — BLD-020 (pairwise compatibility and boundary matrix) built and verified, closing Wave 2; a second real timezone-skew bug found and fixed** *(code + tests + docs)*

Same session as §160-162, immediately following BLD-019. BLD-020 formally `DEPENDS on BLD-002`, resolved earlier this session. HEAD unchanged at `82f5517`; nothing committed.

**Stage 50 — BLD-019 (targeted mutation proof) built and verified, three real test gaps found and closed** *(code + tests + docs)*

Same session as §161, immediately following. BLD-019 formally `DEPENDS on BLD-015/017`, both now done, so this was next-ready. HEAD unchanged at `82f5517`; nothing committed.

**Stage 50 — BLD-017 (economic property/metamorphic tests) built and verified, GST rounding bug found and fixed** *(code + tests + docs)*

Continuation of the same session as §160. Two other things closed first, both quick: re-asked and got the BLD-002 DECISION half from the product/QA owner (both reference candidates approved as drafted; Chromium/3-viewport/en-IN accepted as the release bar; a tenant-size dimension approved and defined as three tiers reusing the existing `tenant_limits`/`CheckTenantLimit` mechanism keyed on `max_users`; module/role subsets proposed per candidate with a stated gap — see `bld-002-test-configuration-inventory.md` §9 and the build checklist's BLD-002 entry; unblocks BLD-020). HEAD unchanged at `82f5517`; nothing committed.

## 2026-09-18

**Stage 50 — POSCart/override decision implemented, BLD-010/011 live-verified, two new focus/keyboard gaps found and fixed** *(code + tests)*

Continued §157's build thread: the two items it left open (§157's own "one new finding" and BLD-010/011's live verification). HEAD unchanged at `82f5517`; nothing committed. All verification against a fresh, disposable PostgreSQL 16.3 cluster on loopback port 5461 (deliberately new rather than reusing the 2026-09-17 session's still-running :5460 cluster, whose bootstrap role name could not be determined) plus a scratch `erp-server` on port 8462, never shared dev/production.

## 2026-09-17

**Stage 50 BLD-001–014 (Wave 0/1) built and verified, plus AUD-03/04 fixed and one new finding** *(code + tests)*

Executed the build checklist's next-thread prompt from §156: BLD-001's isolated environment, then the full BLD-003–014 release-blocker batch. HEAD unchanged at `82f5517`; nothing committed. All work verified against a disposable PostgreSQL 16.3 cluster on loopback port 5460 (four independently fresh-migrated databases across the session), never the shared dev DB or the frozen 2026-09-16 audit fixture on 5446/8178.

**Stage 50 build checklist and new-thread handoff** *(planning only)*

Prepared the detailed build and acceptance checklist at the user's request: 60 work packages in ten dependency-ordered waves, 23 business reference journeys and eight explicit candidate-domain decisions. Each work package identifies responsible roles, readiness/prerequisites, implementation scope and observable closure criteria. All 136 audit controls, nine defects, two verification findings, twelve package proposals, eight performance proposals and twelve Stage 48 parent gates are mapped. The live TODO now links the detailed queue through 50.11–50.19 and records only checklist preparation as complete (50.6b).

**Stage 50.7 (partial) — AUD-01/AUD-02/AUD-05 fixed and verified** *(code + tests)*

Three of the five release-impacting findings from §154's independent audit, each fixed at its actual root cause and verified against a live reproduction rather than by inspection alone. AUD-03/AUD-04 (deploy rollback / restart-failure recovery) are §151's active concurrent work and deliberately not touched here.

## 2026-09-16

**Stage 47.16.5 (partial) + 49.6.2 upgrade — automated payment-card-data scanner** *(code + tests)*

Closes the technical half of 47.16.5 ("automated schema/log/attachment checks prohibit full PAN/CVV/PIN/track data") by extending 49.1.4's existing no-bypass scanner (`internal/securityscan/bypass.go`) with a new `payment-card-data` category, rather than building a second scanning mechanism — the same reuse-the-choke-point reasoning every prior Stage 49 item in this tree already follows. Matches card-PAN/CVV/track-data/PIN-block field and struct-tag shapes; deliberately excludes bare `pan`/`pan_number` because `engines/field_formats.go`'s `panPattern` is India's unrelated Income Tax PAN, a real field this codebase legitimately stores in the clear — a scanner that confused the two would either miss card data hidden behind a `pan_number`-style rename or produce a false positive on every GST-related doctype.

**Stage 49.14 (partial) — secure human operations, support, devices, anti-social-engineering** *(docs only)*

Closes 49.14.1 (plain-language/non-enumerating security UX — verified as the codebase's existing consistent convention, not newly introduced) and 49.14.4 (support has no invisible backdoor — proven by 49.1.4's already-executed bypass scanner, plus `engines/admin_password_reset.go`'s guided-diagnostics-over-impersonation pattern). Parent 49.14 stays open: 49.14.2 partial (49.2.4's dual-control/reauthentication half is built, the preview/diff/typed-confirmation half stays 49.3.6's unbuilt job); 49.14.3/49.14.5/49.14.6/49.14.7/49.14.8 each honestly not built, tied to a named product/security decision or a prerequisite (47.6.6's physical hardware, 49.2.1's identity lifecycle, 49.9.7's signing key, or simply a second person existing on what is today a single-contributor project) rather than to missing effort.

**Deploy auto-rollback — deploy.ps1/remote_deploy.sh** *(tooling only)*

Prompted by the user asking why the ERP wasn't opening: it wasn't actually down (verified `app.wholeops.in` live, 200, no console errors), but the screenshot they'd captured matched exactly the environment-caveat banner text baked into the currently-running `0e79425` binary, rendered with none of its CSS applied — consistent with the documented 2026-09-13 incident (`docs/ai_handover.md` §6) where a deploy hit `security_baseline.go`'s fail-fast gate, crash-looped the box for ~90s, and was rolled back **by hand** over SSH because `deploy.ps1` had no rollback logic at all. The user's actual ask: deploys must never leave the box broken unattended — auto-rollback on failure, always.

**Stage 49.16 (partial) — customer security pack, third-party risk register, assurance evidence handling** *(docs only)*

Closes 49.16.2 (already satisfied by §149's `verification-standards-matrix.md` — no second crosswalk document built), 49.16.3, 49.16.6, 49.16.7. Parent 49.16 stays open on 49.16.1/49.16.4/49.16.5, each deliberately not built: all three explicitly require qualified counsel/business-owner input as part of the item's own acceptance bar (47.16/48.6's gate for .1, a legal/business approval for .4, a market/cost judgment call for .5) — building parallel prose ahead of that would be exactly what 49.16.1's own text warns against.

## 2026-09-13

**Stage 49.10.1 — version-pinned verification matrix** *(docs only)*

`docs/security/verification-standards-matrix.md`. Adopted OWASP ASVS 5.0.0 (17 chapters, ~350 requirements) and the 2025 CWE Top 25 — both fetched live via web search/fetch during this session rather than recalled from training data, specifically because a wrong requirement ID in a "point at something real" document is worse than not writing it. Mapped every ASVS chapter and every CWE Top-25 entry to either a real cited control already in this repository or an explicit open Stage/item number; nothing left as a bare, uncited gap.

**Stage 49.15 — vulnerability disclosure, triage, remediation and security-update lifecycle** *(docs only)*

Closes 49.15.1-49.15.5/49.15.7; 49.15.6 stays open by design. Root `SECURITY.md` (the public-facing entry point GitHub's Security tab and researchers expect) plus `docs/security/vulnerability-disclosure-lifecycle.md` (the engineering-facing companion, same rigor as `secure-development-lifecycle.md`: every claim points at a real artifact in this repository, every real gap marked `[needs decision: ...]` rather than written as prose that implies it's done).

**Stage 40.8/40.9/40.10 + Stage 44.7 — closing Stage 40 and Stage 44, and a QZ Tray CSP defect found in the process** *(code + schema + tests + prod verification)*

Closes Stage 40 (`docs/micro_checklist.md`) completely: the three items left open since the 2026-08-10 batch (real-browser verification, `customer_phone` live-verification, and a QZ silent-print builder for a Purchase Order) were all buildable now, not blocked as first thought.

## 2026-09-12

**Stage 47.7.6 — the audit archive, and the incident it caused while being built** *(code + schema + tests)*

Closes the one piece §142 left open: 47.7 (A-07/A-30) now reads **fully done**. `engines/audit_archive.go` adds the archive itself on top of §142's signed-events-plus-checkpoints model — AES-256-GCM-encrypted, gzip-compressed archive files (same key-management pattern as `channel_credentials.go`), one archive per checkpoint window (never partial — a single held row under legal hold blocks the whole window), manifest-then-delete taken literally (the file is written and self-verified by re-decrypting and re-checking its digest *before* the archive row is recorded and the source rows deleted, all inside one transaction). The subtler half: checkpoint verification is now archive-aware, redirecting to the archive file instead of the (now-empty) live table for an archived window, so a legitimate archive is never mistaken for the deletion attack checkpoints exist to catch — while still catching the archive file itself being deleted, corrupted or swapped. Five new HTTP endpoints and a daily scheduler entry round it out. Full mechanics in `micro_checklist.md`'s 47.7.6 entry.

## 2026-09-11

**Stage 49.9 — Secure development lifecycle, dependency and release-artifact supply chain** *(CI tooling + tests + docs)*

Built the CI-only assurance layer 49.9 asks for, with zero mandatory runtime dependency: a hand-maintained, test-cross-checked dependency ledger (`docs/security/dependency-ledger.json`, package `internal/supplychain`) covering direct/transitive Go modules, CI actions, CI tools and OS/runner images with owner/purpose/license/provenance/version/checksum; a release manifest generator and verifier (`cmd/releasemanifest`) producing an SBOM, artifact checksums and a provenance record, with a `-verify` mode a deployment step can run to refuse a tampered binary; every third-party GitHub Action in every workflow pinned to a full commit SHA and enforced by a generic test (`TestNoWorkflowActionIsUnpinned`) rather than a one-time fix; a new `release-artifact` CI job building hermetically from a clean, already-tested checkout; and `.github/CODEOWNERS` plus an expanded PR template giving review ownership and the 49.9.1 change-trigger a reviewable shape. Signing-key provisioning, DAST-against-a-release-candidate, and wiring `deploy.ps1` to consume the new manifest are named explicitly as `[needs decision]`/follow-on work rather than left implicit. Full accounting in `docs/security/secure-development-lifecycle.md`; new risk R-13 (no enforced branch protection) recorded in `docs/security/risk_register.md`.

**Stage 49.7 — Database, host, network and deployment hardening** *(code + schema + tests + docs)*

Built the least-privilege split `engines/tenant_lifecycle.go`'s own `db-privilege` reporter had only been able to name, not fix: `deploy/postgres_harden.sql` splits the shared `erp` role into `erp_app`(DML)/`erp_migrate`(schema owner)/`erp_backup`(SELECT), verified end-to-end against an isolated scratch database and fully cleaned up afterward. Added a SHA-256 migration-ledger checksum system (`db/migrate.go`, SB-024) and a static scanner proving every migration in the tree is additive-only. Hardened `deploy/erp.service` (capability/namespace/kernel isolation, explicit `ReadWritePaths` instead of all of `/opt`).

**Stage 49.6 — Data classification, privacy, cryptography, keys and secrets** *(code + schema + tests + docs)*

Built a data-flow classification registry extending 47.1.3's sensitive-field policy (`engines/data_classification.go`); a reusable rotation-capable AES-256-GCM keyring (`engines/secret_keyring.go`, generalizing Stage 29.8's JWT-rotation pattern) applied to connector credentials, closing risk register R-03's missing rotation path with an operator command (`tenantctl reencrypt-channel-credentials`); a telemetry redaction choke point (`engines/telemetry_redaction.go`) for Stage 49.11's future event pipeline; and a Customer-scoped data-subject request lifecycle (`engines/privacy_rights.go`, new `data_subject_requests` table, new admin API) with maker-checker approval and legal-hold refusal mirroring `PurgeTenant`'s shape.

## 2026-09-09

**Stage 47.5 + 47.7 — the two decision-blocked audit findings, closed** *(code + schema + tests)*

Both items had sat open on a *decision* rather than on effort, and both decisions were taken by the user this session from measured evidence rather than in the abstract.

**Stage 49.2.4 — Closed: recovery-destination reauthentication, and dual-control helpdesk password reset** *(code + schema + tests + docs)*

49.2.4 (recovery cannot bypass authentication) is now fully closed — the two clauses 49.2.2's session left open. Full detail is in `docs/micro_checklist.md`'s own 49.2.4 entry; this is the index.

## 2026-09-08

**Stage 49.2.2 — Password baseline, and the session revocation it unlocks for 49.2.4** *(code + schema + tests + docs)*

Stage 49 moved into Phase S1 (identity/authorization/data integrity) with 49.2 (identity, authentication, recovery, machine identity) — its first buildable sub-item, 49.2.2, is closed; the rest of 49.2 stays open. `engines/password_policy.go` + an embedded, curated `common_passwords.txt` (~300 entries, including the `Word@123`-shaped patterns this India-first ERP's own corporate password culture produces) is the one choke point `handleChangePassword`, `engines.CompletePasswordReset` and `handleCreateUser` all call now: minimum length (new `security.password_min_length` setting, default 12, following Stage 30.7's registry pattern), a denylist match, username-equality, and a cheap ascending/descending/repeated-character sequence check nothing else would catch.

## 2026-09-10

**Stage 48 — Documentation governance and canonical manuals** *(implementation and review package)*

The documentation portal now leads to owned product, process, architecture, data/API, security/legal, engineering, operations, implementation and QA drafts. User and tenant-administrator manuals are generated from canonical Knowledge Center topics. All 56 registered screens have help mappings across 49 topics; current returns, RF, warehouse-depth, asset and expense instructions replace the previously missing or stale guidance.

## 2026-09-08

**Stage 49.1.7 — Outside-in verification, and a real static-server method gap it found** *(code + tool + tests + docs)*

The last open sub-item of 49.1 needed something none of the rest of Stage 49 S0 could provide from inside this tree: a genuinely external vantage point. `cmd/edgecheck` is a new, reusable CLI — not a one-off script — that probes a deployed instance the way an anonymous internet client would: declared public routes answer as declared, `/internal/*` never reaches the edge, alternate HTTP methods and encoded/traversal paths fail safely, the app's own bind port and a handful of common service ports are unreachable directly, and (when `TENANT_BASE_DOMAIN` is live) an unknown tenant subdomain is refused rather than silently falling through. Run from the developer's own machine over the ordinary internet — not the SSH tunnel, not `127.0.0.1`, the exact distinction the 2026-08-14 TLS bring-up already learned the hard way when a firewall problem was invisible from inside the box.

## 2026-09-06

**Stage 49.1.5 — Secure tenant provisioning, offboarding and proof of removal** *(code + schema + tests + docs)*

The tenant lifecycle had exactly two states: it existed, or somebody ran `DROP SCHEMA CASCADE` by hand. Everything in between — a credential that stops working, a tenant that stops being served without being destroyed, a deletion that has to wait for a backup and a retention period, a way to show afterwards that nothing of it remains — did not exist. Provisioning itself was better than its reputation (it already minted a cryptographically random one-time password and refused to clone `tenant_default`'s users), but nothing governed what happened to that password after it was handed over.

**Stage 47.4 + 47.6 — Atomic replay-safe returns, and the RF task shell** *(code + schema + tests)*

**47.4 (A-04).** Stage 35.9 had already built the right shape — one `ReturnRequest` aggregate with a real state machine, prices resolved from the original sale — and its own entry recorded that it had no management UI. What it lacked was the four properties that make a returns model safe, and what it sat beside was a legacy instant-return path that had none of them. That path took price and cost from the caller, incremented stock with an unlocked upsert, committed, and only then posted two GL reversals with no idempotency key; its "already returned" pool came from a single `SalesReturn` document whose repeat INSERT conflicted and whose error the HTTP handler discarded, so the recorded total never advanced past the first call while stock and GL for every later call still went through. Four replayed calls of 3 against a sale of 10 returned 12.

**Stage 49 Phase S0 — Security charter, attack-surface inventory and fail-closed production baseline** *(code + docs + tests)*

The first phase of Stage 49's security program: establish what is being protected and from whom (49.0), then make the surface knowable and the production baseline fail closed (49.1). Built in parallel with the Stage 47 session and deliberately scoped to files that session was not holding — two narrow edits to `internal/server/routes.go`, everything else new.

## 2026-09-05

**Stage 47.1 — Deny-by-default authorization, privacy and segregation of duties** *(code + schema + tests)*

All eight of Stage 47.1's sub-items built: **47.1.1/47.1.2** (route-capability registry over all 461 `apiMiddleware` routes, and the five sensitive capabilities the A-01 finding named) earlier the same day, then **47.1.3-47.1.8** — a sensitive-field policy that replaces "no field rows means every field" (`engines/sensitive_fields.go`, merged into `fieldPermissions()` so read, form-meta, write, CSV import and PIM bulk edit all inherit it with no call-site change); scope semantics separated from role capability (`engines/scope_policy.go` — location strict where the doctype declares it mandatory, permissive where optional, plus the self and 3PL-owner dimensions the old inline clause had no notion of, all fail-closed); twelve task-derived role templates (`engines/role_templates.go`, which the route-capability allowlist is now *derived from* rather than duplicating); a six-conflict SoD catalog with evidence-based detection and a one-page "why allowed / why denied" administrator preview (`engines/sod_catalog.go`); a reviewed, reversible per-tenant grant migration that changes nothing until an owner approves a before/after diff and never touches a custom role (`engines/role_template_migration.go`, `db/migrations_stage47_1_role_templates.sql`); and a generated authorization contract matrix of 7,837 route×role assertions plus HTTP-level unauthenticated/deactivated/demoted/cross-location/sensitive-field checks.

## 2026-09-03

**Stage 39 — Knowledge Center content: all remaining items closed except one** *(content + code + docs)*

Closed **39.8** (drift guards over the Knowledge Center - stale `last_verified`, dangling screen/endpoint/error-code references, unmapped screens; `internal/kb/drift.go`), **39.9** (article feedback as a plain generic doctype + registered report, zero new Go handler), **39.10** (release notes generated from this ledger's own Stage headings, `cmd/gendocs/release_notes.go`), **39.14** (channel-connector + courier integration guides, 26.4.8's error dictionary inline), **39.15** (Admin & Operations section - Backup & Restore, Incident Response, adapted from `docs/operations/` rather than duplicated), and **all 9 of 39.13's remaining module handbooks** (POS, Inventory & WMS, Procurement, Security/Roles/Approvals, HR & Payroll, Manufacturing & MRP, PIM/PXM, Finance & Tax, CRM & Loyalty, OMS) - closing Stage 39.13 (module handbooks) entirely, 10 of 10 counting Traceability from 2026-08-31. Full per-item detail, verification steps and file paths are in `micro_checklist.md`'s own Stage 39 entries - this section is the index pointer, not a duplicate.

## 2026-09-01

**Stage 37.11 — Role dashboards, savable layouts, digests, drill-through** *(code + schema)*

Pre-build audit of the existing exec dashboard (`renderExecDashboard`, `public/app.js`) found it fully hardcoded - 4 literal cards + a literal `sales-register` trend chart, no persistence, no per-role variation. Generalized rather than replaced: the four cards became `DefaultDashboardTiles()`'s data, not literal code.

**Stage 38.7 — Self-service sandbox tenant for integrators** *(code + schema)*

Built last of the three remaining Stage 38 items (its webhook side-effect-off hook depends on 38.4). A sandbox is a normal tenant - provisioned through the exact same `ProvisionTenantSchema` every real tenant uses - flagged via two additive columns on `public.tenants` (`is_sandbox`, `sandbox_expires_at`), not a new table.

**Stage 38.4 — Webhook subscriptions with HMAC signing, retry/backoff and DLQ** *(code + schema)*

Built on top of 38.6 (same session, built first). Extends `outbox.go` exactly as the backlog item names it: `WebhookSubscription` is a plain generic-doc-API doctype (the `ScheduledReport` precedent), and `dispatchWebhooksForEvent` (`engines/webhook.go`) is called from `processOutbox` once an outbox event is already committed - fanning out to one `webhook_delivery` async job per matching Active subscription. The job runner's own retry/backoff/DeadLettered handling IS this item's DLQ - no second retry mechanism built.

**Stage 38.6 — One general async job runner with retries, DLQ and a visibility screen** *(code + schema)*

Built first of the three remaining Stage 38 items - 38.4 and 38.7 both depend on it. The foundation `micro_checklist.md`'s own §47.11.4 already named as the target every bespoke ticker in this codebase should migrate onto incrementally, later - this stage builds the runner and its visibility screen only, no existing ticker touched. A dedicated `async_jobs` table (the `integration_event_outbox` precedent, not a generic doctype).

**Stage 37.10 — Planning depth: forecasting, reorder points, pegging, capacity** *(code + schema)*

Pre-build audit found this ~60% already built under different names: `ForecastDemand`/`CalculateSalesVelocity` (real but naive), `GetReplenishmentSuggestions`/`GetMRPSuggestions` (reorder point from call-site parameters, not persisted config), `GetProductionSchedule` (already a genuine finite-capacity scheduler, never wired to a report). Pegging was the one genuinely new piece. Every new function is a sibling of an existing one - originals untouched.

**Stage 37.9 — Quality & maintenance: inspection plans, CoA, NCR/CAPA, preventive maintenance** *(code + schema)*

Pre-build audit found all four completely absent. GRN receiving's own QC (Stage 26.5.2) is a pure quantity split with no structured per-item test list, so `PostGRNReceiptWithQC` is deliberately left untouched. `ReasonCode` is reused as-is for NCR root-cause (a new "Quality" category value).

**Stage 37.8 — Service management** *(code + schema)*

Pre-build audit found no ServiceTicket/WorkOrder/AMC concept anywhere. WarehouseTask's dispatch spine (Stage 42.2) is WMS-specific and not directly reusable as an object, but its lifecycle PATTERN - typed status enum, terminal-state guard, reason-required transitions - is what ServiceTicket's own dedicated engine functions copy, the same shape IntercompanyTransaction/LandedCostVoucher/PrepaidExpenseSchedule already use this session.

**Stage 37.7 — Projects & job costing** *(code + schema)*

Pre-build audit found no Project concept anywhere, and confirmed the codebase's own conventions point to Project as a 4th whole-posting dimension (the CostCenter/Department/Entity precedent) rather than a WIP-style running-cost ledger, since every cost-incurring doctype here posts immediately - there is no accumulation stage to attach one to.

**Stage 37.6 — Deferred revenue, prepaid amortisation, recurring billing, price-list versioning** *(code + schema)*

Pre-build audit found all four completely absent, plus a structural finding: `SalesInvoice` is lump-sum only (no `lines` field) - deferred revenue is recognised at the whole-invoice level, not per line.

**Stage 37.5 — Financial statement builder with dimensions and drill-down** *(code only)*

Pre-build audit found the codebase's own `ReportDefinition` framework explicitly rejects user-authored query/layout flexibility as an injection-risk feature outside its stated scope - so "builder" here is scoped to extending the existing hardcoded Trial Balance/P&L/Balance Sheet with dimension filters and drill-down, not a new statement-layout doctype (which would be the first violation of that stated principle). `gl_accounts` is also confirmed flat (5 basic types, never altered since Stage 1) - a full multi-section layout is a separate, larger undertaking not attempted here.

**Stage 37.4 — Budgeting, cash-flow forecast, credit limits, dunning** *(code + schema)*

Pre-build audit found all four completely absent, and one structural finding shaped the whole design: neither `SalesOrder` nor `SalesInvoice` carries a real `Customer` Link - customer identity flows as a free-text name throughout, the same convention `GetCustomerLedgerReport` already relies on. Credit-limit matching (37.4.2) inherits that name-based convention rather than adding a second, inconsistent identity model - a real Link-based rework of customer identity across these doctypes is a materially larger, separate undertaking.

**Stage 37.3 — Costing & valuation, incl. landed cost allocation** *(code + schema)*

Pre-build audit found this codebase had NO costing method anywhere - `StockLedgerEntry`/`bin_stock`/`inventory_availability` track quantity only. Two real, previously-undiscovered gaps fell out of that audit and are closed by this stage, not just the checklist's own "5 sub-items":

**Stage 37.2 — Multi-entity & intercompany: entity-scoped posting, mirrored entries, reconciliation, consolidation** *(code + schema)*

User asked to build the remaining open Stage 37/38 backlog, starting with 37.2 (the largest ERP-core item still open after 37.1's multi-currency work closed). `LegalEntity` (Stage 17.9) already existed as a Master, linked from `Location.legal_entity`, but nothing transacted across entities — this closes that gap entirely inside one tenant's schema (confirmed via `engines/saas.go` that one schema = one tenant = one consolidation group; there is no cross-tenant consolidation model in this codebase and this stage does not invent one).

## 2026-08-31

**Documentation sync pass — archived Stages 41/42/43/46, corrected a stale cross-reference, rebuilt the executive blueprint** *(docs only)*

User asked for a full docs sync: which checklist stages are ready to archive, and which docs need building so the project is legible "at a glance" to an engineering head, a product head, a developer, and the CEO.

## 2026-08-30

**Stage 36.4/36.6/36.7 — export & syndication, DAM depth, enrichment & quality — Stage 36 closes** *(code + schema)*

Closes the rest of Stage 36 (PIM parity, Unbxd level): export & syndication depth (36.4), DAM depth (36.6), and enrichment & quality (36.7, its last five sub-items — 36.7.2 had shipped earlier). With 36.1-36.7 all now built and verified, Stage 36 is **complete** and has been moved out of `micro_checklist.md` into `docs/archive/micro_checklist_closed_stages.md`. Full item-by-item detail lived there before the move and now lives in that archive entry; this section is the index.

## 2026-08-28

**Stage 36.5 + 36.3 — declarative transform rules, and PIM import depth** *(code + schema)*

Opens Stage 36's remaining five top-level items (36.3-36.7 — import, export/syndication, transform rules, DAM depth, enrichment) with the two built in dependency order: 36.5 first (a shared value-transform seam), then 36.3 (import depth), which consumes it. Full item-by-item detail, including the scope calls made explicit before building, is in `micro_checklist.md` Stage 36.

## 2026-08-25

**Stage 35.8/35.9 — Settlement reconciliation ("UniReco") and returns depth** *(code + schema + UI)*

Closes the last two open items of Stage 35's OMS parity plan: 35.8 (5 sub-items) and 35.9 (3 sub-items). Full item detail in `micro_checklist.md` Stage 35.

## 2026-08-22

**Stage 35.7 — commercial bundles and physically stocked kits** *(code + schema + UI)*

All four Stage 35.7 items are complete. `ProductBundle` is intentionally not another manufacturing BOM: it describes a sellable commercial composition, with Virtual/Stocked fulfillment and Parent/Fixed/Component pricing. Validation pins every SKU to an active Item, forbids duplicate/self/nested components and keeps one active definition per bundle SKU.

**Stage 35.6 — channel breadth becomes an operational SDK** *(code + schema + UI)*

All seven Stage 35.6 items are code-complete. The new connector contract declares auth validation, order pull, inventory push, catalogue publish, status push, error mapping and capabilities; the scheduler and UI consume capabilities instead of platform-name conditionals. Amazon and Flipkart implement their public seller contracts, WooCommerce implements REST v3, and Myntra/Meesho/Ajio/Nykaa/Blinkit/Zepto/Swiggy Instamart use a fail-closed negotiated-path adapter because their seller contracts are private. Quick-commerce descriptors bind orders to a store location and forbid split allocation.

## 2026-08-24

**Stage 42.5.5 — Multi-owner stock segregation, closing Phase 42.5 and Stage 42** *(code + schema)*

Closes the one item §110 left open: 42.D2 ("is 3PL/multi-owner a real target?") was resolved — yes, build it, the same call already made for §112's 42.6.6-42.6.9. Phase 42.5 is now 8/8 and Stage 42 (58 items across 6 phases) is fully closed. Full item detail in `micro_checklist.md` Stage 42.

## 2026-08-22

**Stage 42.6 — Labour and billing depth** *(code + schema)*

All nine listed Phase 42.6 rows are built (the source heading calls the phase eight items, but the checklist contains 42.6.1 through 42.6.9).

**Stage 35.5 — Courier integration crosses the provider boundary** *(code + schema)*

All six Stage 35.5 items are code-complete using the parity plan's default first providers, Delhivery and Shiprocket; actually calling either account remains credential-gated. Full item detail is in `micro_checklist.md` Stage 35.

**Stage 42.5 — Inventory control depth: physical inventory, CycleClass, replenishment breadth, slotting v2, facility hierarchy/copy/cross-facility inquiry** *(code + schema)*

7 of 8 items (42.5.1-42.5.4, 42.5.6-42.5.8); 42.5.5 (multi-owner stock segregation) excluded, still gated on open decision 42.D2 (is 3PL a real target?). Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-21

**Stage 46 — Money precision: paise migration** *(code + schema)*

Closed the two items both `docs/DURABILITY_AUDIT_2026-07-31.md` and `docs/ERP_LOOPHOLES_ANALYSIS.md`'s 2026-08-20 re-evaluation still carried as genuinely open. Full item-by-item detail in `micro_checklist.md` Stage 46.

**Stage 42.4 CLOSED — Outbound depth: waves, sortation, cartonization v2, packing validation, deconsolidation, loading + Bill of Lading, pre-ship gate, and VAS/kitting off the existing BOM** *(code + schema + frontend)*

Closed all 11 items of Phase 42.4 in one pass. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-20

**Stage 42.3 CLOSED — Inbound depth: dock scheduling, yard, hold/release, configurable receipt rules, catch weight, planned cross-dock, hazmat compliance, and RF receiving** *(code + schema + frontend)*

Closed all 10 items of Phase 42.3 in one pass. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-19

**Stage 42.2 CLOSED — Zone/PutawayStrategy/AllocationStrategy masters, directed putaway, exception follow-on, the warehouse cockpit, and a real bug found in Stage 45's own render mechanism** *(code + schema + frontend)*

Asked to finish the rest of Phase 42.2 (the warehouse task spine — 4/10 done, 42.2.1-42.2.4 already closed by the prior session). Closed the remaining six items, 42.2.5-42.2.10. Full item-by-item detail in `micro_checklist.md` Stage 42.

**Stage 45 round 1 — UI/UX design-bar sweep: the render race, the clipped tab bars, and a mid-session misdiagnosis corrected** *(frontend only)*

User reported 8 issues live against `:8080` deployment screenshots (duplicate panels across three Financial Accounting screens, F&A frozen header, help icon styling, a Wholeops rebrand ask, Setup submenu visuals, PIM tab bar clipping at 90% zoom, a setup banner that shouldn't be dismissible), then asked for a broader "no one has ever seen it, brainless-intuitive" design-bar sweep and to fix everything found. Full item-by-item detail in `micro_checklist.md` Stage 45.

**Stage 42.1 CLOSED — outbound lottable validation, UOM conversion, real Code 128, and two bugs live verification caught** *(code + schema + docs)*

Asked to build through the whole open backlog. Picked up Phase 42.1 where the prior session left it (42.1.1-42.1.9 already closed) and finished the remaining three items — 42.1.7, 42.1.10, 42.1.11 — closing the phase at 11/11. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-17

**Stage 39.11/39.12/39.16/39.17 — the Knowledge Center gets its content, 6 articles → 23** *(content + code)*

Asked to start writing Stage 39's content, which was 11 open items described as "mostly content authoring". Four of them are now closed. Full item-by-item detail in `micro_checklist.md` Stage 39.

## 2026-08-16

**Stage 37.1.3-37.1.5 — multi-currency finished, and the 98.8% receivable understatement it uncovered** *(code + schema + docs)*

Asked to start building from the open parity backlog, whose first row is **Stage 37 — ERP core depth**, annotated "incl. finishing multi-currency (37.1.3-37.1.5)". That was the right place to start for a reason beyond ordering: 37.1 was the only half-built item in the list, and half-built multi-currency is not a missing feature, it is a live correctness liability. Full item-by-item detail in `micro_checklist.md` Stage 37.1. A concurrent session's Stage 42.1 work was uncommitted in this tree throughout — see the note at the end.

**Stage 42.1 — the traceability foundation: batch, FEFO and recall** *(code + schema + docs)*

Asked to start building Stage 42 (WMS parity), which had been planning-only since 2026-08-11. The plan is unusually emphatic about where to begin — Phase 42.1, "start here, nothing else first" — because grep-verified there was **no lot/batch concept anywhere in the tree**: no `batch_no`, no `expiry`, no shelf life in `engines/` or `db/`. That absence is what made FEFO, expiry blocking and recall unbuildable, and it is why this WMS could not be sold into food, pharma, cosmetics or electronics. Six of the phase's eleven items are now closed (42.1.1–42.1.6) plus the recall traceability the plan asked for as proof the model is right. Full item-by-item detail in `micro_checklist.md` Stage 42.

## 2026-08-15

**Stage 36.2 — the PIM task & workflow engine, the biggest PIM gap** *(code + schema + docs)*

Asked to start building the open parity work, top of the list being **36 — PIM parity**, whose first named item is the task/workflow engine. It was also the thing 36.1.3 explicitly deferred ("task assignment: it follows 36.2's task engine, which does not exist yet"), so it is the item that unblocks the rest of Stage 36. Full item-by-item detail in `micro_checklist.md` Stage 36.2. A concurrent session held Stage 35.4 in the same tree throughout — see the note at the end.

**Stage 35.4 — the outbound document chain, and the pack object that made it possible** *(code + schema)*

Asked to start building Stages 35/36/37. That span is ~30-45 sessions by the parity plan's own sizing, so it was worked in the plan's dependency order rather than scaffolded shallowly across three stages: R1 was already done, and **R2's head item, 35.4**, is what this section covers. (A concurrent session picked up 36.2 in the same window — see the note at the end.)

## 2026-08-14

**Stage 44 — per-tenant hostnames, and the domain that unblocked go-live** *(code + schema + proxy config + docs)*

User registered **wholeops.in** and asked to close the domain-gated items. Two things came out of it: the go-live path was unblocked, and the URL scheme they described turned out not to exist.

## 2026-08-13

**The dev cluster converted to UTF-8, closing the 20.6 defect** *(database + one test + docs)*

User asked for the three defects §96 recorded to be fixed. Two were already fixed in the tree and were re-verified rather than re-asserted: the SPA's relative asset paths (no relative `src`/`href` remains in `index.html`, no relative `fetch` in `app.js`) and the OpenAPI dynamic-schema trap (the paged envelope is inlined per endpoint, `PublicPage` is not a cached component). The third was real and outstanding.

## 2026-08-12

**Stages 35.3.7 / 36 / 37.1.2 / 38 / 39 — the platform layer: public API, Knowledge Center, multi-currency** *(code + schema + docs)*

User handed over a ranked gap list and an explicit build order, deduplicated across the two: finish 36.1.3 and 36.7.2, build 38.3/38.5/38.9 together, open the first read-only 38.1 routes, build 38.8, land 39.2-39.7 as one safe unit, then 37.1.2 — plus 35.3.7, the correctness gap they ranked first. All eight built. Item-by-item detail in `micro_checklist.md`.

**Stage 43 — Go-live blocker sweep: the buildable half of the parked items** *(code + docs)*

User handed back eight parked items from `go_live_decisions.md` — escalation contacts and the alert webhook, connector/GSP/e-way/payment-terminal credentials, Cloudflare zone access, a pen-test vendor, UAT participants and hypercare sign-off, real QZ Tray printer verification, the competitor-scraping legal call, and the data mart — with "build from this which is possible… skip which needs decision for now."

## 2026-08-11

**Stage 38/39 foundations — scoped API identities and safe Markdown rendering** *(code + one unapplied migration + docs)*

User asked to start Platform & Extensibility and the Knowledge Center while the shared tree still contained concurrent Stage 35-42 work. Both starts were therefore taken at dependency foundations that add no live surface: external credentials before public routes, and a renderer before generated static help.

**Stage 35 release train R1 — the OMS becomes a real product** *(code + schema + docs)*

User asked to work the open plan phase by phase. The open plan is the Parity Master Plan's Stages 35-39, and its own sequencing puts release train **R1 = 35.1 + 35.2 + 35.3** first, because those three items exist to clear the three structural debts everything downstream assumes are gone. All three are now closed. Full item-by-item detail in `micro_checklist.md` Stage 35.

**Stage 36.1 / 37.1 foundations — Product Groups and effective-dated currency rates** *(code + schema + docs)*

User explicitly asked to start both Stage 36 and Stage 37 while protecting the ongoing session and partially completed work. The implementation therefore begins at each stage's dependency foundation, is additive, avoids `public/app.js`, and leaves both new migrations unapplied. This request also resolves parity-plan decisions D6 and D7: multi-currency, multi-entity, Projects and Service are real build scope, to be taken in plan order rather than skipped as speculative.

**Stage 42 — WMS parity plan against SAP EWM and Infor WMS** *(planning only, no code)*

User request: review the WMS module of SAP EWM and Infor WMS thoroughly, prepare a plan, update the checklist. Both benchmark URLs were supplied. Output is `docs/specs/wms_parity_plan.md` (the source of detail) plus the Stage 42 tracker skeleton in `micro_checklist.md` — the same two-file relationship Stages 35-39 have to `parity_master_plan.md`. **No source, schema or config was touched.**

**Tracker reconciliation — 23.11 and 26.1.3** *(docs only)*

Two checklist entries mixed completed engineering decisions with nonexistent or externally gated work. **23.11 is now closed as not applicable, not implemented**: the catalog's one-row inline-grid style has no paste-into-grid product surface or roadmap item, so leaving it unchecked permanently misreported an already-settled scope decision as unfinished work. Stage 23 is fully closed and moved to the closed-stage archive.

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

A standalone Python microservices stack (`Antigravity Projects/Buying Catalog/OmniCore` — FastAPI + Streamlit + SQLAlchemy/SQLite + Redis/RQ + docker-compose, services `crawler`/`pims`/`oms`/`erp`/`pos`, last touched 2025-12-27) was read in full, its knowledge extracted to `docs/specs/market_intelligence_reference.md`, and the folder deleted. Same disposition and the same reasoning as the standalone WMS project in §(1c1c050)/`docs/specs/wms_master_blueprint_reference.md`.

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

User asked to start building open checklist items, explicitly not phase by phase. Took the whole P0 block of Stage 30 (the 2026-07-30 layman audit, `docs/UX_MANUAL_AUDIT.md`) — seven items — plus two backend items from 30.5 that fell out of the same work. Item-by-item mechanics in micro_checklist.md Stage 30.

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

