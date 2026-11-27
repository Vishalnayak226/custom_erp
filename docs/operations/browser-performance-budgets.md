---
doc_id: DOC-OPS-PERF-BUDGETS
title: Browser performance budgets
type: procedure
status: draft
owner: operations-owner
approvers: [documentation-maintainer, operations-owner]
audience: [maintainers, operations-owner]
applies_to: local scratch server only; never production
authority: canonical
confidentiality: internal
last_verified: 2026-11-27
review_by: 2026-12-26
supersedes: none
superseded_by: none
verification_scope: harness run on a scratch database; budgets are targets
---

# Browser performance budgets

`docs/qa/perf-budget.cjs` loads the app in a real Edge/Chromium browser on declared device and
network profiles and fails (exit 1) when a budget in `docs/qa/perf-budgets.json` is missed. It
adds no dependency and never runs against a live site.

## Budgets

| Measure | Budget |
| --- | --- |
| LCP, p75 over runs, cold load | 1500 ms |
| INP (Event Timing), p75 over runs | 200 ms |
| Cached view switch, p95 | 250 ms |
| Server time per endpoint, p95 / p99 | 150 ms / 500 ms |
| CLS, HTTP errors, console errors, page errors, horizontal overflow, controls without a name, fields without a label | 0.1, then 0 |
| Tab size after the session: JS heap after a garbage collection, and every node the tab still holds | 12 MB, 10000 nodes |

The tab size (297.7) is read at the end of the session through every main screen. Its ceiling
is about twice the first measurement (2026-11-27: 5.8 MB and 6436 nodes on a desktop, 5.4 MB
and 5295 on `phone-2g`), so a screen that keeps what it no longer shows fails the gate. The
caches a feature needs stay: the POS offline queue, the session, saved filters, drafts, lookups
and loaded screens. The item-code lookup cache holds at most 1000 codes.

The `phone-2g` profile (297.6: 256 kbps down, 128 up, 400 ms latency, 4x slower CPU, a 360x740
touch screen) has its own targets, each from a fixed rule rather than a measurement:

| Measure on `phone-2g` | Budget | Rule |
| --- | --- | --- |
| LCP, cold load | 5000 ms | a first visit on 2G within 5 seconds |
| LCP, revisit with a warm cache | 2500 ms | Core Web Vitals "good" |
| Bytes on a revisit | 32768 | one second of a 256 kbps line |
| Cached view switch, p95 | 1000 ms | the 1-second limit for an unbroken flow |
| INP | 200 ms | the default |

Measured 2026-11-27 (3 runs, scratch DB): cold LCP 4160-4168 ms, INP 64-80 ms, 211 KB on a
first visit and 22 KB on a revisit.

Cold and cached bytes, requests, FCP, long tasks and DOM nodes are measured and reported on
every run. A budget is a target and is never edited to make a run pass. A known miss is recorded
under `ratchet` in the budgets file as a ceiling the measured value must not exceed while the
fix is pending; `--ratchet` makes a known miss a regression gate, and the default run fails on
every miss.

## Run it

1. Start the app on a scratch database and port (never production). The development banner
   ("SIMULATED ENVIRONMENT") can be the largest paint; the result reports LCP with and without it.
2. Mint at least `runs x profiles + 4` tokens, one per line, for one user
   (`go run ./cmd/minttoken manager1 "Store Manager"`). A token allows 60 calls a minute, so
   the harness paces itself at 46 and uses a token per run.
3. Run:

```powershell
node docs/qa/perf-budget.cjs --base http://127.0.0.1:8740 --tokens tokens.txt `
  --channel msedge --profiles desktop-cable,throttled-4x,phone-low,phone-2g --runs 3 --out perf.json
```

Add `--ratchet` for the release script's regression gate. The same run as a Guide suite:
`pwsh Guide.ps1 -Suite perf -Roles manager1 -Repo <worktree> -WorkingTree -Port <free> -Db <scratch>`.

Add `--record-gate <repo root>` to record the result as the release gate's `browser_budget` evidence. A full run only: PASS records pass, a miss records fail (cover a known miss with a P2 exception), an error or a partial run (`--no-server`, fewer profiles) records nothing.

A release with tests on GitHub runs this itself: `release.yml`'s evidence job starts the
candidate, mints the tokens and runs `--ratchet --record-gate .` with Playwright's Chromium
(47.9.4). The laptop run is then only needed to check a fix. See
[the release-candidate gate](release-candidate-gate.md).

## Output contract

The last stdout line is `PERF-BUDGET RESULT: PASS|MISS|KNOWN|ERROR misses=<n> known=<n> out=<file>`,
preceded by one `MISS scope.key = value (budget n)` line per miss. Exit code: 0 all budgets
met (with `--ratchet`, every miss at or under its ceiling), 1 a budget missed or a metric
could not be measured, 2 the harness could not run. `--out` writes the full JSON
(`verdict`, `misses`, `known`, `profiles`, `server`, `env`).

## Limits

Server time is measured over loopback from the client because the server sends no
`Server-Timing` header. INP is a lab proxy over scripted interactions. Accessibility checks are
the harness's own (names, labels, alt text, duplicate ids, contrast); there is no axe-core, so
focus order, keyboard traps, ARIA validity and screen-reader output are not covered.
