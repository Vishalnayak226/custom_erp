---
doc_id: DOC-ENG-159-001
title: Repository restructure and project constitution - change record
type: record
status: draft
owner: engineering-owner
approvers: [product-owner]
audience: [product-owner, developers, ai-sessions]
applies_to: repository layout and rules from 2026-11-10 (on top of commit cbf2799, release 0.23.0)
authority: historical
confidentiality: internal
last_verified: 2026-11-11
review_by: 2027-02-09
supersedes: none
superseded_by: none
verification_scope: phase one - go build/vet, go test of internal/repolayout, doclint and brain redraw; phase two - the same plus every migration on a fresh scratch database and the whole go test suite on it; no production change
---

# Repository restructure and project constitution

The owner asked for this on 2026-11-10:

> "Many doc are present and codes are present. Code are disorganised and docs are disorganised.
> Arrange in proper std and structured way. It should look clean and proper. We will have a rule
> to it. Future any changes will be adopted according this. We should have one core rule like a
> constitution... We already have first principle add there. Give a good file name to it."

Tracker items: **159.1** (constitution), **159.2** (layout guard), **159.3** (this record), plus
the already-approved Stage 103 tidy (**103.1** root part, **103.2**, the `scripts/archive/` part
of **103.4**). The second phase (items 103.1 rest, 103.3, 103.4 rest, 103.5, 103.6 and 160.1) was
approved with "Implement all" and done on 2026-11-11: section 6.

## 1. In one paragraph

The repository now has one rule book, [CONTRIBUTING.md](../../CONTRIBUTING.md) at the root. It is
the project constitution: every change to code, data, documents or scripts must follow it,
whether a person or an AI session makes it. The old "first principle" and "second principle"
moved into it as Articles 1 and 2. A Go test, `internal/repolayout`, now fails the build when a
file is put in the wrong place or named the wrong way, so the rules are checked by a machine,
not just written down. The root folder was cleaned: build outputs went to `bin/`, private notes
went to `.local/`, and a crash dump, an unused `package.json` and some old binaries were removed.
The second phase, on 2026-11-11, renamed about 480 older files that broke the naming rules, moved
every migration into `db/migrations/`, removed nine unused functions and wrote a disk clean-up
script for the owner to run. Files other sessions were still editing kept their old names.

## 2. The constitution: CONTRIBUTING.md

**Why this name.** `CONTRIBUTING.md` at the repository root is the standard place for a
project's rules for changes. GitHub shows it to anyone who opens a pull request or an issue, and
developers look for it first. The file calls itself the project constitution.

**What it says, article by article.**

| Article | Rule |
|---|---|
| Order of authority | Constitution first, then the standards it hands detail to (documentation standard, SECURITY.md, release gates, brain map rules), then `CLAUDE.md`, then skills. When two disagree, the lower one is fixed. |
| 1. Lightweight, future-proof, solid | The old first principle: no frontend framework or build step, no new dependency without real need, additive database changes, reuse the existing patterns, fix things once at the shared choke point. |
| 2. Complete, smooth, documented | The old second principle: "done" means usable, the user and admin guides are updated in the same change, guides are tested by following them. |
| 3. Where things live | One table of folders and what each holds. The root may hold only 14 named files. Builds go in `bin/`, private notes in `.local/`, scratch files in `%TEMP%`. |
| 4. How things are named | Name a file by what it does, never by when it was built: no stage, phase or batch numbers. Go files `<module>_<feature>.go`; docs and browser files kebab-case; PowerShell kebab-case (release scripts keep `Verb-Noun`); shell and Python snake_case. |
| 5. Data and the database | New migrations only, additive and safe to run twice. A shipped migration is never edited, renamed or deleted. Migration names keep the `migrations_stage<N>_<M>_<what>.sql` form because the runner applies files in name order. A release never changes client data or resets a password. No secrets in the repository. Generated files are never hand-edited. |
| 6. Code and tests | Build, vet and `go test -p 1` pass. Money, stock and permission code gets real failure-case tests. New routes get a capability entry. Review checklist, rollback rules, release evidence. |
| 7. Screens | Title only, very short text, list first with New as its own page, no number spinners, fits any window size or zoom. These were separate owner rules; now they are in one place. |
| 8. Documentation | The three working records (checklist, ledger, handover), every ask recorded as an item, the documentation standard, brain and generated-docs redraws. |
| 9. Shared tree, git and releases | Several sessions share one tree: stage only reviewed files, never touch another session's work, commit or push only when asked, release only through the three clicks. |
| 10. Enforcement | A table of which check enforces which rule and where it runs (CI, release gates). |
| 11. Amendments | The owner approves every change to the constitution. The change updates its check and adds a line to the amendment log. |

**How AI sessions get it.** `CLAUDE.md` (local, not in git) now imports `CONTRIBUTING.md`, so
every Claude session loads the constitution automatically. The two principle sections that used
to be in `CLAUDE.md` were removed there, because the constitution now holds them. The third rule,
"take full control of routine execution", stays in `CLAUDE.md`: it is about how an AI session
works, not a rule for the code.

## 3. How the rules are enforced

| Check | What it catches | Where it runs |
|---|---|---|
| `go test ./internal/repolayout/` (new) | A file at the root that is not on the allowlist; a committed build (`.exe`, `.dll`, ...) or secret (`.pem`, `.key`, `*.local.*`, `.env`); a file named after a stage, phase or batch; wrong casing for Go, docs, browser, PowerShell, shell or Python files | CI and release gate G10, like every Go test |
| Existing checks | Build, vet, tests, doc links, generated docs, brain map, migration checksums | Unchanged; Article 10 lists them |

**The legacy list.** 537 files were named before the constitution existed. They are listed in
[legacy-names.txt](../../internal/repolayout/testdata/legacy-names.txt), so the test passes
today. The list can only shrink: rename a file, then delete its line. Adding a line to make a new
file pass is forbidden. The test also fails if a listed file already follows the rules, so stale
lines get removed.

| In the legacy list | Count |
|---|---|
| Go files named after a stage (`engines/` 377, `internal/server/` 124, `cmd/` 3) | 504 |
| Docs not in kebab-case (`ai_handover.md`, `BRAIN.md`, the upper-case guides, snake_case runbooks, ...) | 24 |
| Scripts (6 kebab-case Python files in `docs/qa/`, `verify_connector_live.ps1`) | 7 |
| Other (`docs/qa/stage55-shared-correctness.cjs`, `public/profiles/food_bev.json`) | 2 |

The list includes 94 files that other sessions had created but not yet released, so their
releases are not blocked. A listed file that is missing is only logged, not failed. That matters
because a release snapshot leaves held work out.

## 4. What moved or was removed

Everything removed is still in git history unless marked "never in git".

| Before | After | Why |
|---|---|---|
| `server.exe` (root) | `bin/server.exe` | Builds belong in the ignored `bin/` folder |
| `erp-server.exe`, `erp-stage34.exe`, `gendocs.exe`, `edgecheck.exe`, `releasebudget.exe` (root, about 72 MB) | deleted (never in git) | Old build outputs, approved for deletion by name in item 103.1. Rebuild any of them with `go build -o bin/<name>.exe ./cmd/<name>`. |
| `bash.exe.stackdump` (root) | deleted (never in git) | A crash dump from Git Bash |
| `DEV_CREDENTIALS.local.txt`, `RAILWAY_SECRETS.local.txt`, `RAILWAY_SETUP.local.md`, `STAGING_ACCESS.local.md` (root) | `.local/` | One ignored folder for private notes. Their contents were not opened or committed. |
| `package.json` (root) | deleted | Metadata only. Nothing read it, and its `main` pointed to a file that does not exist. The app has no npm step. |
| `scripts/archive/` (README, `diff.txt`, `patch.js`, `wrap_tables.js`) | deleted | Retired one-off scripts. Recover any of them with `git show cbf2799:scripts/archive/<file>`. |
| `docs/engineering/contributing.md` | merged into `CONTRIBUTING.md` | Its rules (patterns, shared tree, tests, review, rollback, records) are now in Articles 1, 5, 6, 8 and 9. The constitution keeps its document id, `DOC-ENG-CONTRIB`. A row in the archive's "Where did it go?" table records the move. |

## 5. Files updated to match

| File | Change |
|---|---|
| `.gitignore` | Ignores `/.local/` and `*.stackdump` |
| `.dockerignore` | Leaves `.local/` and `*.local.md` out of any image |
| `manage.ps1` | `release` builds `bin\erp-server.exe`, and `start` runs it from there |
| `promote.ps1` | Builds and starts `bin/erp-server.exe` in the test/live worktrees |
| `scripts/verify_connector_live.ps1` | Reads credentials from `.local/CONNECTOR_CREDENTIALS.local.json` by default |
| `README.md` | New folder-level structure that matches the real tree and points to the constitution. The run steps now use `go run ./cmd/server -migrate` and `bin/`, instead of the removed `migrations_phase3.sql` step. |
| `docs/README.md`, `docs/engineering/developer-setup.md` | Link to the constitution instead of the old contributing page |
| `docs/archive/readme.md` | "Where did it go?" row for the merged contributing page |
| `docs/guides/ADMIN_GUIDE.md`, `docs/guides/UAT_CHECKLIST.md`, `docs/operations/connector_live_verification.md` | New credential path (`.local/`) and build path (`bin/`) |
| `.github/pull_request_template.md` | New first line: say whether the change follows the constitution |
| `.github/CODEOWNERS` | Owner rows for `CONTRIBUTING.md` and `internal/repolayout/` |
| `docs/brain/brain.map.json` | Files `CONTRIBUTING.md` and the layout guard under Project Documentation; drops `package.json` and the removed `public/qrcode.min.js` |
| `CLAUDE.md` (local) | Imports the constitution and removes the two principles it now holds |

**Left as they are, on purpose.**

- The migration files keep their names. Every database records them by name and checksum, so
  renaming one would make it look new or changed.
- `db/migration.sql` and `engines/auth.go` still mention `DEV_CREDENTIALS.local.txt` by name in a
  comment. The name is still right. Editing a shipped migration is forbidden, and `engines/` is
  being changed by other sessions right now.
- `engines/` stays one Go package. Splitting it would be a large, risky import change with no
  benefit at runtime.
- `CLAUDE.md` and `Open-ERP.cmd` stay local and git-ignored, as before. The constitution is the
  tracked copy of the rules, so a fresh clone now has them.
- `docs/sop-video/` and `backups/` were not touched.

## 6. Phase two - done on 2026-11-11

The owner approved all of it on 2026-11-11 ("Implement all"). Other sessions kept working in this
tree the whole time, so anything they were still editing was left alone. Those files are listed
below and wait for their own release.

| Step | Item | What was done |
|---|---|---|
| Migration folder | 103.3 (a) | All 388 migrations moved from `db/` into `db/migrations/`, with names and contents unchanged. `db/migrate.go` embeds only that folder and records each file by its bare name, so no database re-runs anything. The layout guard now rejects a `.sql` file in the `db/` root. Articles 3 and 5 say so; see the amendment log entry for 2026-11-11. |
| Data files | 103.3 (b) | Not done, on purpose. Article 3 says a data file the server embeds sits beside the Go file that embeds it, which is where `attribute_library.json`, `hsn_master.json` and `common_passwords.txt` already are. |
| Stage-named code | 103.3 (c) | 454 Go files renamed to `<module>_<feature>.go`, and their lines deleted from the legacy list. 50 Go files from the Stage 148 work another session is still building (42 of them not yet committed) keep their names until that work is released. |
| Docs and scripts | 160.1 | 29 renamed to kebab-case (Python to snake_case), for example `docs/guides/USER_GUIDE.md` became `docs/guides/user-guide.md`, and `docs/brain/BRAIN.md` became `docs/brain/brain-map.md`. Every link in the tree was rewritten, and each move has a row in the archive's "Where did it go?" table. The three working records (`micro_checklist.md`, `project_ledger.md`, `ai_handover.md`) keep their names, because the release script, doclint and every session's instructions name them. |
| Dead code | 103.4 | Nine unused functions removed, 13 kept for wiring, 19 not reviewed because another session was editing their files. See the table below. |
| Git housekeeping | 103.5 | The 2026-07-26 stash and the local `dashboard-removal` branch were saved to `.local/git-archive-2026-11-11/` (git bundles and patches) and then removed. `origin/main-local` and `origin/dashboard-removal` are still on GitHub, because deleting them is a push. When the owner says so: `git push origin --delete main-local dashboard-removal`. |
| Free disk space | 103.1 | `.local/cleanup-2026-11-11.ps1`, for the owner to run. On its own it only lists what it would remove; `-Apply` removes it. The dry run on 2026-11-11 found about 4.9 GB: 87 old graph snapshots (1.0 GB), 97 worktrees nobody had used for a day (3.9 GB; the uncommitted edits in 92 of them are saved as patches first), the old deploy binary and 24 MB of Stage 48 scratch (zipped first). |

Every old and new path is listed in [the rename record](../archive/renamed-files-2026-11-11.md).
Tracker items and ledger sections written before 2026-11-11 still use the old names; look them up
there.

**Generated pages.** The documentation generator keeps a manifest of every page it writes, and it
refuses to run when a page it wrote earlier has gone missing. The old paths (`BRAIN.md`, the
upper-case guides) were marked as retired in `docs/generated/brain-manifest.json` and
`guides-manifest.json`, and all pages were regenerated under their new names.

**Line endings.** Renamed and regenerated files keep the LF line endings git has for them, so
each diff shows only the real change.

### Dead code: what happened to each function

`deadcode -test ./...` found 41 functions that nothing in the program or its tests reaches.

| Decision | Functions | Why |
|---|---|---|
| Removed | `GenerateBarcode`, `GetStockBalance` | The Phase 3 originals. Barcodes and stock balances now come from the item and stock-ledger engines. |
| Removed | `ApplyPIMTransformRule` | A shortcut to `ApplyPIMTransformRuleCtx`, which both callers already use. Its description moved onto that function. |
| Removed | `settlementOptionsFromRequest` | The receivable and payable settlement handlers read the same two fields themselves. |
| Removed | `IsCurrencyBearingDoctype`, `thumbnailStorePath`, `sortBatchStockByExpiry`, `ReadyMadeKeys`, `DatasetKeys` | Small helpers nothing used. |
| Removed (2026-11-12) | `MarkOverridesConsumed` | Planned for wiring, but the checkout already retires an approved override inside the sale's own transaction, and a test proves it. A second copy outside that transaction would add nothing. |
| Keep and wire | `SealLegacyAuditRows` | Seals the old audit rows that have no checksum (47.7.4). No screen or command runs it. |
| Keep and wire | `GetDimensionAwareSlottingSuggestions` | The better slotting suggestion, with a bin capacity check (42.5.4). The screen still uses the older one. |
| Keep and check | `ListPIMCatalogs` | The Catalogs list without the share-token hash. The screen does not use it, so check what the screen does show. |
| Keep and check | `WriteStockLedgerLines` | Writes stock-ledger entries after a posting. Nothing calls it. |
| Keep | `UpdateJobProgress` | Kept on purpose for a future long-running background job; its comment says so. |
| Keep | `ConditionOperatorsFor`, `ReportNumber` | Added in the last two weeks, probably for work still in progress. |
| Keep | `catResolved`, `catMasterOf`, `catStoreOf`, `catStoreCodeOf`, `catCountLines` | The report catalog, item 75.3, still being built. |
| Not reviewed | 19 functions: the CleverTap, Unicommerce, courier and channel-credential connectors, costing, finance, serial tracking, wholesale, the personal-data register, the security baseline, tenant lifecycle, image drive, the document-engine handlers, `cmd/brainmap` and `cmd/journeylab` | Another session was editing those files. Review them after that work is released. |

## 7. How to work under the new rules

- **New file?** Put it in the folder Article 3 names, and name it by what it does:
  `engines/inventory_reorder.go`, not `engines/stage160_items.go`. Tests sit beside the code.
- **New migration?** Use `db/migrations/migrations_stage<N>_<M>_<what_it_does>.sql`, where `N.M` is the
  tracker item. Make it additive and safe to run twice. Never edit a migration that has shipped.
- **Build the server:** `.\manage.ps1 release`, or `go build -o bin/erp-server.exe ./cmd/server`.
  Then `.\manage.ps1 start`.
- **Dev login:** credentials are in `.local/DEV_CREDENTIALS.local.txt`.
- **Scratch or temporary files:** put them in `%TEMP%`, never in the repository.
- **Rename an old file:** first check that no other session is editing it, then rename it,
  update what refers to it, and delete its line from `legacy-names.txt`.
- **Change a rule:** edit `CONTRIBUTING.md` with the owner's approval, update the check that
  enforces it, and add a row to its amendment log (Article 11).

## 8. Verification

- `go build ./...` and `go vet ./...` pass on the whole module.
- `go test ./internal/repolayout/` passes. Both tests run: the layout check on the real tree, and
  a table of names the rules must accept or reject. The table pins the patterns so they cannot
  quietly loosen.
- `go build -o bin/x.exe` creates a missing `bin/` folder by itself. This was tested in a scratch
  module, so `promote.ps1` needs no extra step.
- Every file edited for this change keeps the line endings it has in git, so its diff shows only
  the real edit.
- The doclint link check and the brain map redraw were run after the edits; see the ledger entry
  for this ask.

**Phase two, 2026-11-11.** Run on the working tree as it was, with the other sessions' unfinished
work in it:

- `go build ./...` and `go vet ./...` pass.
- A new, empty scratch database took all 388 migrations from `db/migrations/` with no warning.
  This is what a fresh install does, so the move cannot have skipped or reordered one.
- The whole test suite, `go test -p 1 ./...`, ran against that scratch database. Every package passes. The `engines` package runs for about 45 minutes, so it needs the 70-minute timeout the release gives it.
- The layout test passes with the renamed files included.
- doclint finds no broken link. Its other findings are review dates and the document register,
  which the release rewrites.
- The brain map and the generated pages match the tree.
- The browser smoke run the release uses (Guide, suite `smoke`) went through the app built from
  this tree:
  - signed in as Super Admin and as Cashier;
  - raised and approved a purchase order, received and posted the goods;
  - sold one on the POS and found the bill in the Sales Register;
  - took it back as a customer return and closed the till.

  The one problem it raised was a till already left open in the copied dev data, not the change.
