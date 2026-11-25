---
doc_id: DOC-HANDOVER
title: Current developer handover
type: reference
status: active
owner: engineering-owner
approvers: [engineering-owner]
audience: [developers, maintainers]
applies_to: shared development tree; verify state before editing
authority: canonical
confidentiality: internal
last_verified: 2026-11-12
review_by: 2026-12-12
supersedes: historical handover captured 2026-09-10
superseded_by: none
---

# Current developer handover

Start with this page, then use the [live checklist](micro_checklist.md) for open gates and the
[project ledger](project_ledger.md) for dated evidence. Historical entries are context, not a
substitute for checking the current source and worktree.

## Current repository state

- The tree is shared and usually dirty: §6 below names each session's uncommitted work. Keep
  edits you do not recognise.
- The portable Postgres runs on 5490, not the 5435 the `db` package defaults to: always pass
  `TEST_DATABASE_URL` and check the listener before trusting a suite result.
- Build, verification and safety notes for this tree (test database, browser checks, release
  budgets, CSP, alerts, the no-commit rule) are in
  [developer setup](engineering/developer-setup.md#notes-for-this-tree).
- The 2026-10-04 notes that stood here are kept in the
  [archived entries](archive/handovers/ai-handover-2026-10-26.md#repository-state-notes-2026-10-04).

## Durable references

- [Build checklist](product/erp-build-checklist.md) — item scope, dependencies and Done bars.
- [Developer setup](engineering/developer-setup.md) and [service operations](operations/service-operations.md).
- [2026-10-03 prior handover snapshot](archive/handovers/ai-handover-2026-10-03.txt) — the previous
  long-form handoff; follow the live sources above where it is stale.

## 6. Version Control
- **2026-11-25 release 0.29.0 (9db21d3) deployed to app and minn (ask 265, ledger §432).** It carries every uncommitted ask from 241 to 264 and 266, except PrintBridge 0.3.3's three downloads, which `release/hold.txt` keeps back (249.1).
  - Deploy applied migrations 248.2, 251.2 and 263.4 and installed the new `erp.service`; both hosts answer 0.29.0 (9db21d3).
  - **Each client runs Settings › Data Healer › Printer names once** (263). Until then, printing reads either key.
  - **Records commit after the deploy also carries 265.9:** Help search indexes the release-notes pages by title and summary only (`internal/kb/build.go`), since the ledger had pushed `search.json` over 250 KiB; `Update-RecordsDocs.ps1` regenerates from every staged file. Index now 212,437 bytes.
  - **ci.yml on that commit (ac92e5f) failed only the password-reset timing check** (one cold call decided a 2-sample median). 265.10 warms each path once and takes 3 samples (`internal/server/timing_test.go`), pushed as c5efedf. Its run then passed every step but gitleaks, which read the depth-1 checkout's whole tree as new and flagged 25 test fixtures; 265.11 checks out with `fetch-depth: 0` (`.github/workflows/ci.yml`), pushed as fd902e4, whose run 45532338819 is main's first green ci.yml since 2026-07-17. A red ci.yml on main is now a real finding.
  - **NOT COMMITTED, ships next release:** 265.8 (G13 retries a refused push) in `release/scripts/Invoke-Release.ps1` and `release/README.md`.
  - **Left for the owner:** erp.env has no `OPS_ALERT_WEBHOOK_URL`; no restore-drill record on the server yet (`deploy/restore_drill.sh`).
  - **Other sessions' dirty files:** line-ending-only noise in six files (extension-hooks-checklist, guide-org-cycle, saas_test, state_forecast, handlers_access_governance, rf.css), plus one hash in `docs/generated/guides-manifest.json` (backlog-journey-map) from a docs regen. They are not this ask's, so leave them.
- **2026-11-25 ask 266 — current ERP improvement plan ready (266.1 Done, ledger §431); planning only, the plan shipped in 0.29.0.** New `docs/product/erp-improvement-plan-2026-11-25.md`: source-confirmed Returns first-200/filter-state gaps and typed exchange-code friction; existing 264.4 FX settlement decision; eight proposed packages and coverage of all 25 modules. Artifact budget check passes: cold core 182,660/184,320, initial JS 113,817/122,880 bytes. HEAD at review start `7d59685` (0.29.0); deployed versions were not checked. No app/database/server changes or new live acceptance; preserve the existing dirty files. Implementation awaits the user's next instruction; do not treat the proposals as built or duplicate existing acceptance owners.
- **2026-11-24 PrintBridge 0.3.3 refuses a print to a printer that is off or unplugged (ask 249, ledger §430): 249.1 Built, not verified. Guides shipped in 0.29.0; the three downloads stay held and uncommitted.**
  - **PrintBridge repo (not committed):** `agent/internal/print/ready_windows.go` + test (new), `printer.go` (`ReadyChecker`), `winspool_windows.go` (status consts at package level), `agent/internal/agent/handlers.go` (`BuildVersion` 0.3.3, check in `print`), `print_ready_test.go` (new), `docs/PROTOCOL.md`.
  - **ERP:** `public/downloads/printbridge-*` (0.3.3, signed), **listed in `release/hold.txt`** until a plugged-in USB printer prints (LD0801 round, 251.3); then remove those lines. Guides: silent-printing-setup, user guide §7B.1, admin guide items-printing.
  - Agent 0.3.3 is installed on this PC. The scratch server on :8190 is an older build (old `/api/v1/print/qz/*` routes); the check mapped the new route names to it.
- Older §6 entries (up to the 0.29.0 release on 2026-11-25) are kept verbatim, newest first, in [archive/handovers/ai-handover-2026-10-26.md](archive/handovers/ai-handover-2026-10-26.md) (moved 2026-11-12, Stage 134.9, 2026-11-14, item 186.1, 2026-11-24, items 264.2 and 265.3, and 2026-11-25, ask 265, so this page fits its 150-line budget).
