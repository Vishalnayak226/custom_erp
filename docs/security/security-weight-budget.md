---
doc_id: DOC-SECWEIGHT001
title: Security weight, performance and storage budget
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner, engineering-owner]
audience: [maintainers, security-owner, operations-owner]
applies_to: source release 0.16.0 and later; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-26
review_by: 2027-01-24
supersedes: none
superseded_by: none
---

# Security weight, performance and storage budget

**Stage 49.18.** Maximum security must stay ultra-light: one Go binary,
PostgreSQL, the vanilla-JS client and the operating system's own facilities.
This page is the ledger of what the controls cost, the rules that keep that
cost bounded, and the test that holds each rule. Artifact sizes (binary,
static files, KB, database, archives) are measured by `cmd/releasebudget` at
release gate G4c and are not repeated here.

## 49.18.1 What the controls cost

Measured 2026-10-26 on the development machine (Windows, local PostgreSQL 16);
the tests log fresh numbers on every run and fail on the budgets.

| Control | Measured | Budget | Test |
|---|---|---|---|
| Authenticated read through `securityHeaders` + `apiMiddleware` | p50 1.8-2.7 ms, p95 2.4-3.3 ms, p99 3.2-4.4 ms | p95 adds ≤ 25 ms | `TestStage49181AuthenticatedRequestCost` |
| Database statements per authenticated read, counted on the request's own goroutine | 3: sandbox expiry, doctype → module, module entitlement - none cached | ≤ 3 | same |
| One signed audit row | p50 1.7 ms, p95 2.9 ms to write; ~445 bytes (~435 KiB per 1,000 events before indexes) | p95 ≤ 50 ms; ≤ 1.5 KiB a row | `TestStage49181AuditAndPasswordCost` |
| One password guess (bcrypt cost 10) | ~136 ms - the same for an offline attacker | ≥ 10 ms | same |
| Idle server, one client, all modules on | 180 database transactions a minute, 11 pooled connections | measured (BLD-044) | live measurement, see BLD-044 |
| Same, worker modules switched off | 122 a minute (-32%) | measured | same |

The three per-request statements are the next lever: the sandbox expiry reads the same
`public.tenants` row the tenant gate already caches, and the doctype → module and module
entitlement answers change rarely. Caching them with invalidation on change is a freshness
decision on an authorization path (49.18.6), so it is recorded here, not done silently.

## 49.18.2 Bounded state everywhere

Every store of security or operational state - login, rate and abuse counters,
sessions and revocation, idempotency and replay keys, tokens, jobs, logs,
alerts, audit and evidence - is listed in `SecurityStateStores()`
(`engines/bounded_state.go`) with what one row is keyed by, what bounds it, the
function that enforces the bound, and what a legal hold does to it.
`TestStage49182EveryStateTableIsBounded` fails when a table matching the
state-table pattern exists without an entry, or names a cleanup nothing calls.

Retention sweeps delete only through `retentionDelete`: 5,000 rows a statement,
at most 200 statements a sweep (a large backlog drains over several hourly
ticks instead of one long transaction or WAL burst), and **nothing while the
client is under legal hold** - before 49.18.2 the sweeps ignored the hold.
Sweeps now cover the CleverTap send log, the PIM channel publish log and the
system error log (`platform.error_log_retention_days`, default 180), which had
no bound at all.

## 49.18.3 One event, one piece of evidence

An action is recorded once. A security detection over many audit rows writes
one bounded `SECURITY_ALERT` row that points at its evidence - the actions, the
window, up to three correlation ids - and copies none of it; jobs and outbox
events carry the same correlation id as the request that queued them; the
forensic pack and the investigation trail read the source rows through those
references. `TestStage49183DetectionReferencesItsEvidenceInsteadOfCopyingIt`
drives twenty times a rule's threshold and holds the alert to one row under
2 KiB with no copied content.

## 49.18.4 No client bloat

The browser app loads nothing from another origin: no remote script,
stylesheet or font, no analytics SDK, no CAPTCHA bundle, no CDN include, and
the CSP is `'self'` for scripts, styles and fonts.
`TestStage49184BrowserAppLoadsNothingFromAnotherOrigin` and
`TestStage49184CSPKeepsScriptsStylesAndFontsSameOrigin` keep it so; security
UI reuses the existing lazy screen modules (the Security tab is one).

## 49.18.5 No infrastructure reflex

Fix the root cause in authorization, transactions, query bounds or
configuration first. A CDN, WAF, managed secrets service, SIEM or EDR may be an
optional deployment defence when a threat or contract warrants it, but:

- the application's security never depends on one: every control on this page
  and in [threat_model.md](threat_model.md) works on the minimum topology;
- the minimum secure production topology stays **Go + PostgreSQL + the
  existing reverse proxy and OS facilities**, as the
  [assurance statement](../qa/generated/security-assurance-statement.md) says;
- adding a required service, dependency or outbound call is a high-risk line
  in the release's G6b security-impact report and needs an evidence-based,
  approved exception recorded in [risk_register.md](risk_register.md).

## 49.18.6 Security cost cannot be "optimized away"

`TestStage49186SecurityCostIsNotOptimizedAway` reads the source: password
hashing never below `bcrypt.DefaultCost`, `InsecureSkipVerify` never set, and
every in-memory cache reviewed for what its key carries - tenant, user, store
scope - so a speed-up cannot serve one tenant's answer to another. A new cache
fails the test until it is reviewed. Optimise shared parsing, queries, indexes
and serialization instead, and measure the attacker's cost as well as the
legitimate user's (the password row above).

## 49.18.7 Multi-year forecast and cleanup drill

`cmd/stateforecast` (read-only session; safe against production) measures each
client's state tables from its own last 30 days and projects one, three and
five years: a swept table levels off at its retention window, evidence kept on
purpose keeps growing, and the totals show when the disk needs attention.

```
DATABASE_URL=postgres://... go run ./cmd/stateforecast         # every client
DATABASE_URL=postgres://... go run ./cmd/stateforecast minn    # one client
```

The cleanup drill is `TestStage49187RetentionDeletesInBatchesAndHonoursLegalHold`:
11,000 expired rows and 300 current ones; under legal hold nothing goes, with
the hold lifted the expired rows go in three batches and the current ones stay.
Batched `DELETE`s take row locks only, so inserts carry on during a sweep.

## Acceptance, read honestly

Built and tested on the development machine. Not yet measured: the same
numbers on the production host under representative load (BLD-042 needs that
host), and the forecast on production's real data - run `cmd/stateforecast`
there and keep the output with the monthly data-growth review.
