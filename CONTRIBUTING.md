---
doc_id: DOC-ENG-CONTRIB
title: Project constitution
type: normative
status: active
owner: product-owner
approvers: [product-owner, engineering-owner]
audience: [developers, reviewers, ai-sessions, release-maintainers]
applies_to: every change to this repository - code, database, data, documents, scripts
authority: canonical
confidentiality: internal
last_verified: 2026-11-10
review_by: 2027-02-08
supersedes: none
superseded_by: none
---

# Project constitution

This file holds the rules every change to this repository must follow. That covers code, the
database schema, shipped data, documents and scripts, whether a person or an AI session makes
the change. The file is called `CONTRIBUTING.md` because that is the standard name for these
rules, and GitHub shows it to anyone who opens a pull request.

## Order of authority

1. **This constitution.**
2. **The standards it hands detail to:** the [documentation standard](docs/governance/documentation-standard.md),
   the [security policy](SECURITY.md), the [release gates](release/README.md) and the
   [brain map rules](docs/brain/README.md).
3. **`CLAUDE.md`** (local, git-ignored). It tells an AI session how to work inside these rules.
   It may add to them but never contradict them.
4. **Skills and general advice**, such as the vendored Karpathy guidelines.

When two documents disagree, the lower one is wrong: fix it so it matches the higher one.

## Article 1 - Lightweight, future-proof, solid

Every change keeps the server lightweight, the codebase future-proof and the implementation solid.

- **No frontend framework or build step.** `public/` stays vanilla JS, CSS and HTML, served
  directly from disk. No bundler, no transpiler, no npm.
- **No new third-party dependency** (Go module or JS library) unless there is truly no reasonable
  way to do it with the standard library or what is already vendored. A few extra lines of plain
  code are better than a dependency. If a dependency is truly needed, record it as an
  architecture decision first.
- **Database changes are additive and backward-compatible** (Article 5). Never rework an
  existing table destructively.
- **Reuse the existing pattern** instead of adding a second way to do the same thing:
  - the two dialog systems;
  - `apiMiddleware`'s `Resolved-*` headers;
  - the `.table-panel` / `.btn` / CSS-token vocabulary;
  - the maker-checker approval engine for any approval-gated flow;
  - `BulkImportCSV` for any line-item CSV upload;
  - `ReportDefinition` / `RegisterReport` for any report;
  - the `writeAPIError` / `writeAPIErrorGeneric` envelope for any error response.
- **Fix it once, where every caller passes.** When a fix or validation must apply everywhere,
  attach it to the shared point every caller already runs through. Examples:
  `ValidateDocument` in `engines/doctype.go`, the response writers in `apierror.go`, and
  `showApiError()` in `app.js`. Don't patch each call site one by one.

## Article 2 - Complete, smooth, documented end to end

Every module must actually work, not just be built. The guides must be complete enough that a
user can do any task by following them.

- "Done" means usable: data renders, actions work, empty and error states are clear, and nothing
  dead-ends. A page that loads without an error is not enough.
- Every feature updates the [user guide](docs/guides/user-guide.md) and the
  [admin guide](docs/guides/admin-guide.md) in the same change.
- Test the guides by following their steps literally. Fix the doc if it has drifted from the app.
  Fix the app if the doc shows a real gap.
- The bar: when the owner tests it, they should find almost nothing left to change.
- Finish the asked-for scope properly, but don't invent extra scope nobody asked for.

## Article 3 - Where things live

| Folder | Holds |
|---|---|
| `cmd/<tool>/` | One `main.go` per program: the server (`cmd/server`) and each command-line tool |
| `engines/` | Business logic, in one Go package. Do not split it. |
| `internal/server/` | HTTP routes, middleware and handlers |
| `internal/<area>/` | Other self-contained internal packages (KB, doc generation, security scans, this layout check) |
| `db/` | Database connection and migration runner; every migration file sits in `db/migrations/` |
| `public/` | The browser app: HTML, CSS and ES-module JavaScript, served as-is |
| `docs/` | All documentation. [docs/README.md](docs/README.md) maps every subfolder. |
| `deploy/` | What runs on the production host: service unit, Caddy, backup, restore and deploy scripts |
| `release/` | The three release clicks and their gates |
| `scripts/` | Developer and operations helpers still in use. A retired script is deleted; git history keeps it. |
| `extension-sdk/` | The contract for outside extension developers |
| `.github/` | CI, pull request template, CODEOWNERS |

**The repository root holds only these files:** `README.md`, `CONTRIBUTING.md`, `SECURITY.md`,
`go.mod`, `go.sum`, `Dockerfile`, `docker-compose.yml`, `railway.json`, `environments.json`,
`manage.ps1`, `promote.ps1`, `.gitignore`, `.gitattributes` and `.dockerignore`. Anything else
goes in a folder.

**Local-only folders** are git-ignored and never committed:

- `bin/`: builds, such as `bin/erp-server.exe`
- `.local/`: private notes and credentials
- `logs/`
- `backups/`
- `media_store/`
- `graphify-out/`
- `.claude/`
- `skill-observations/`
- `CLAUDE.md`
- `Open-ERP.cmd`

Scratch files go in `%TEMP%`, never in the tree.

- Data files the server embeds (master lists, word lists) sit beside the Go file that embeds them.
- Tests sit beside the code they test.
- Generated files say which tool regenerates them.

## Article 4 - How things are named

**Name a thing by what it does, never by when it was built.** Stage, phase, item, batch and wave
numbers belong in the tracker, not in file names. There are two exceptions:

- A migration's number is its version (Article 5).
- A dated record (an audit, drill, handover or anything under `docs/archive/`) puts its ISO date,
  `YYYY-MM-DD`, in the name.

| Kind | Form | Example |
|---|---|---|
| Go source and tests | `<module>_<feature>.go`, lower snake_case | `inventory_reorder.go`, `inventory_reorder_test.go` |
| Browser files in `public/` | lower kebab-case | `view-attribute-library.js` |
| Documents in `docs/` | lower kebab-case `.md` | `backup-restore.md` |
| PowerShell | kebab-case. The release clicks keep PowerShell's `Verb-Noun` form. | `update-docs.ps1`, `Invoke-Release.ps1` |
| Shell and Python | lower snake_case | `restore_drill.sh`, `gen_error_catalog.py` |
| Migrations | see Article 5 | `migrations_stage158_4_reorder_rules.sql` |

- `<module>` is the business area the file belongs to, such as `inventory`, `finance`, `pos`,
  `crm`, `wms`, `oms`, `pim` or `hr`.
- Root files whose names tools or GitHub look for keep their conventional names (`README.md`,
  `CONTRIBUTING.md`, `SECURITY.md`, `Dockerfile`, `CODEOWNERS`).
- Files named before this constitution existed are listed in
  [legacy-names.txt](internal/repolayout/testdata/legacy-names.txt). That list may only shrink.
  Never add a line to make a new file pass.
- Rename a listed file only when no other session is editing it, then delete its line in the
  same change.

## Article 5 - Data and the database

- **Schema changes only through a new migration file.** Make it additive, backward-compatible
  and safe to run twice: `CREATE ... IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`.
- **A shipped migration is never edited, renamed or deleted.** Every database records each
  migration's file name and checksum. The runner ([db/migrate.go](db/migrate.go)) reports any
  file whose content changed after it was applied. Fix a mistake with a new migration.
- **Migration files go in `db/migrations/`.** The runner embeds only that folder and records each
  file by its bare name, so a `.sql` file anywhere else is never applied.
- **Migration names:** `migrations_stage<N>_<M>_<what_it_does>.sql`, where `N.M` is the tracker
  item number. For example, item 158.4 becomes `migrations_stage158_4_reorder_rules.sql`. The
  runner applies files in natural name order, so keep the `migrations_stage` prefix. Any other
  prefix would sort before or after every existing migration and change the order on a fresh
  database.
- **A release never changes client data or resets a password a user has set.** A release that
  changes data must be confirmed with an explicit YES at the Deploy click.
- **No secrets in the repository:** no credentials, keys, customer data or workstation paths.
  Configuration comes from environment variables (see [deploy/erp.env.example](deploy/erp.env.example)).
  Private notes live in `.local/`.
- **Test data stays in tests.** Use isolated fixtures with deterministic cleanup, and never point
  a test at production.
- **Generated files are never edited by hand.** Change the source and regenerate.

## Article 6 - Code and tests

- `go build ./...`, `go vet ./...` and `go test -p 1 ./...` must pass. The `-p 1` matters:
  test packages share one database, so they run one at a time.
- Tests check business rules and real failure cases, not just that code runs.
  - An economic command (money, stock or ledger) tests replay, concurrency, partial failure
    and reconciliation.
  - A permission tests the wrong tenant, role, scope, field and workflow state.
- Simulated outside delivery stays off unless that specific provider check was authorised.
- A new HTTP route gets its capability entry, and the attack-surface inventory is regenerated
  (release gate G6).
- Every change considers:
  - requirements and support scope
  - API and schema compatibility
  - privacy and access
  - help text
  - operations and recovery
  - release notes

  The [pull request template](.github/pull_request_template.md) asks for each.
- A rollback plan says what reverts the application, what reverts configuration and what reverts
  the database. Reverting a binary does not reverse a migration. A version label alone is not
  acceptance evidence; see the [release acceptance template](docs/governance/release-acceptance.md).

## Article 7 - Screens

These are the product owner's rules for every screen, dialog and panel:

- **Title only.** No subtitle, and no how-to text under a title.
- **Very short text.** Summaries are a few words on one line ("CRM · Store app"), never sentences.
- **List first.** A module opens on its list; **New** opens its own page.
- **No number spinners**, anywhere. Number fields stay `type=number` with the spinner hidden.
- **Fits any window size and any browser zoom.**

## Article 8 - Documentation

- Three working records track the project:
  - the [micro checklist](docs/micro_checklist.md): what is still to do;
  - the [project ledger](docs/project_ledger.md): what was built;
  - the [AI handover](docs/ai_handover.md): the state of the working tree.
- Every request from the owner becomes its own checklist item, with a status, before work
  starts. Update all three records when a unit of work finishes.
- The [documentation standard](docs/governance/documentation-standard.md) governs front matter,
  names, moves, archiving and deletion. When a file moves, add a row to the archive's
  "Where did it go?" table.
- After adding, moving or deleting files:
  - redraw the code map with `pwsh docs/brain/update-brain.ps1`;
  - regenerate the generated pages with `pwsh docs/update-docs.ps1`.

## Article 9 - Shared tree, git and releases

- Several sessions share one working tree.
  - Run `git status` and `git diff` before editing and again before staging.
  - Stage only files you reviewed, by name. Never use `git add -A` or `git add .`.
  - Never reset, clean, stash, rebase or move another session's uncommitted work.
  - If a generator reports concurrent edits, wait for the tree to settle and run it again.
    Never disable that guard.
- Move or rename a file only when no other session is editing it.
- Commit or push only when the owner asks. Never force-push `main`.
- **Releases go only through the three clicks in [release/](release/README.md).**
  - Work that must not ship yet is listed in `release/hold.txt`.
  - If a release lets a problem through, that problem becomes a new gate in
    `release/scripts/Invoke-Release.ps1` and a new row in its README, in the same session.
- Delete a file only with the owner's approval of that file by name. Dated or superseded
  material moves to `docs/archive/` instead.

## Article 10 - How these rules are enforced

| Rule | Checked by | Runs in |
|---|---|---|
| Root allowlist, no stage-named files, name casing, no committed builds or secrets | `go test ./internal/repolayout/` | CI, release G10 |
| Build, vet, tests | `go build`, `go vet`, `go test -p 1 ./...` | CI, release G8 and G10 |
| Doc links, front matter, review dates | `go run ./cmd/doclint -strict` | CI, release G7 |
| Generated pages not hand-edited | `pwsh docs/update-docs.ps1 -Check` | CI, release G7 |
| Every file filed in the code map | `pwsh docs/brain/update-brain.ps1 -Check` | release G7 |
| Shipped migrations unchanged | checksum check in [db/migrate.go](db/migrate.go) | every server start with `-migrate` |
| Data-changing releases confirmed | Deploy click (explicit YES) | every deploy |

Some rules cannot be checked by a machine, such as reusing a pattern or keeping text short.
Those are checked in review against this file.

## Article 11 - Changing this constitution

- The product owner approves every amendment.
- Make the amendment in its own change: edit this file, update the check that enforces it,
  and add a row to the log below.
- Then bring lower documents (`CLAUDE.md`, the guides and the standards) into line. Never
  change a lower document first and leave this file behind.

## Amendment log

| Date | Change |
|---|---|
| 2026-11-10 | Adopted. Articles 1 and 2 moved here from `CLAUDE.md`. `docs/engineering/contributing.md` was merged in and removed. The layout guard (`internal/repolayout`) was added. What changed and why is recorded in [the restructure record](docs/engineering/repository-restructure-2026-11-10.md). |
| 2026-11-11 | Article 3 and Article 5: every migration now sits in `db/migrations/`, and the layout guard rejects a `.sql` file in the `db/` root. Stage-named Go files, docs and scripts were renamed to Article 4 names; the old-to-new list is in [the rename record](docs/archive/renamed-files-2026-11-11.md). Approved by the owner ("Implement all", items 103.3 and 160.1). |
