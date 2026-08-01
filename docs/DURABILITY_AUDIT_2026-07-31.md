# 20-Year Durability Audit — 2026-07-31

A full-stack survivability review: does this codebase still build, run, stay
correct and stay safe when the environment around it changes for two decades?
Every claim below was produced by running something, not by reading code alone.

**Baseline**: 168 Go files, ~46,900 LOC, 36 test files, **2** third-party
dependencies. Go toolchain 1.26.5, `go.mod` declares 1.22.12.

**Method**: the live tree was snapshotted first (concurrent sessions were
actively editing it), and a **dedicated throwaway Postgres cluster** was
provisioned on port 5435 in a scratch directory — so nothing here touched the
shared dev database.

---

## Scorecard

| Area | Verdict |
|---|---|
| Build / vet | ✅ clean |
| Cold-start provisioning (virgin DB) | ✅ **63/63 migrations apply cleanly** |
| Test determinism (repeat runs) | ✅ byte-identical across runs |
| SQL injection | ✅ no vector found |
| Supply chain / CVE | ✅ 0 reachable vulnerabilities |
| Secrets handling | ✅ no hardcoded secrets; fail-closed webhooks |
| Test self-containment | ❌ suite cannot pass on a fresh database |
| CI schema coverage | ❌ **applied 10 of 65 migrations** |
| Concurrency (document IDs) | ❌ **primary keys collided under load** |
| Race detector | ❌ never run anywhere |
| Stored XSS | ❌ exploitable |
| Money precision | ⚠️ architectural constraint (needs a decision) |

---

## What is genuinely strong

These are load-bearing properties that most systems this size fail, and they
hold here. They deserve to be stated as plainly as the defects.

1. **A virgin database provisions perfectly.** 63 migration files applied to a
   freshly `initdb`-ed cluster with `ON_ERROR_STOP=1`: **63 applied, 0 failed**.
   Being able to reconstitute the schema from zero is the single most important
   20-year property, and it works.
2. **No SQL injection surface.** Every dynamic query interpolates only the
   schema name, which is validated once at the single choke point
   (`db.GetTenantSchema` → `validSchemaNameRe`). All user values ride on `$1`/`$2`
   placeholders. There is no dynamic `ORDER BY`, no user-built `WHERE`, no string
   concatenation into SQL. This is better than most production codebases.
3. **A two-dependency supply chain.** `lib/pq` and `golang.org/x/crypto`, nothing
   else. `govulncheck` reports **0 reachable vulnerabilities** (19 exist in
   required modules, none on a called path). This is the cheapest possible
   20-year maintenance burden and it was clearly deliberate.
4. **Deterministic tests.** Repeated full-suite runs produced byte-identical
   results. The failures below are real defects, not flakes.
5. **Genuinely defensive auth/webhook posture.** Webhook verification fails
   *closed*, feature/module gates fail closed, JWT keys support real rotation,
   and secrets are never in source.

---

## Findings

### 1. CI applied 10 of 65 migrations — false-green for ~50 migrations · **FIXED**

`.github/workflows/ci.yml` enumerated migration files **by hand**. The list
stopped at `migrations_stage17_soft_delete.sql`. Everything from Stage 17c
onward — 55 files — was never applied in CI.

Proved by diffing a full run against the CI subset. Tables CI never saw:

```
public.ops_run_log                 tenant_default.accounting_periods   (GL period close)
tenant_default.bin_stock           tenant_default.payment_utr_log      (duplicate-payment guard)
tenant_default.loyalty_tier_rules  tenant_default.system_settings      (settings registry)
tenant_default.bin_stock_lpn       tenant_default.product_content_versions
tenant_default.pos_offline_heartbeats  tenant_default.tenant_limits
tenant_default.loyalty_redemption_otp_challenges
```

This is a *recurrence*: the step's own comment records the same bug at Stage 14.
A hand-maintained list has to be updated by every future migration author, so it
will drift again — that is the actual defect, not the missing files.

**Fixed**: replaced with `for f in $(ls db/*.sql | sort)`, added
`-v ON_ERROR_STOP=1` (psql otherwise exits 0 on SQL errors and CI goes green
against a broken schema), and added a **schema-completeness gate** that asserts
sentinel tables from late migrations actually exist.

> Note: a threshold based on table *count* was drafted first and discarded — a
> full virgin DB has only 55 tables vs 44 for the broken subset, so any round
> number would have been wrong. Calibrating against the real database caught it.

### 2. Document-ID primary keys collide under concurrency · **FIXED**

~30 call sites minted primary keys as `fmt.Sprintf("TSK-%d", time.Now().UnixNano())`.
That is safe only if the clock actually advances between calls.

**Measured on this Windows dev box: it advances in ~520 microsecond steps** — 31
distinct values across 2,000,000 consecutive `time.Now()` calls. So ~520µs of
concurrent work shares one "nanosecond", and only **1,922 distinct IDs exist per
second**.

Measured collision rates (Poisson arrivals, i.e. traffic that clusters):

| Load | Windows (520µs tick) | Linux (~1ns clock) |
|---|---|---|
| 20 req/s | 0.33% of IDs duplicated | ~0% |
| 50 req/s | 1.20% | ~0% |
| 100 req/s | 2.68% | ~0% |
| 250 req/s | 6.41% | ~0% |

`documents.id` is the PRIMARY KEY, so a duplicate is a failed INSERT in the
user's face. This had already been observed and written off in
`ai_handover.md` as an "environmental race" in the test suite. **It is not
environmental.**

Two things matter for the 20-year question:
- Linux production is far safer, but only *because the clock happens to be
  fine-grained*. That is luck, not a designed property.
- It never protected multi-instance deployments at all — two app processes
  behind a load balancer share no counter.

**Fixed**: new `engines/docid.go` — `NewDocID(prefix)` / `NewDocIDCompact(prefix)`.
In-process, a CAS loop guarantees strictly increasing values regardless of clock
resolution; across processes, an 8-char `crypto/rand` per-process suffix keeps
instances apart. IDs stay time-sortable and fit `VARCHAR(100)`. Stdlib only, no
new dependency. All 25 remaining generator sites converted.

Locked in by `engines/docid_test.go`, which measured on this machine:

```
legacy UnixNano scheme collided on 19985/20000 sequential IDs (99.9%);
NewDocID collided on 0
```

### 3. The race detector has never been run · **FIXED (in CI)**

`-race` requires cgo; there is **no C compiler on this Windows dev box**, so it
cannot run locally, and CI never asked for it. A codebase that serves concurrent
HTTP over a shared `*sql.DB` plus several package-level caches has therefore
never been checked for data races.

**Fixed**: CI now runs `go test ./... -p 1 -race -v` with `CGO_ENABLED=1`, where
a C toolchain does exist. *This has not yet been executed — the first CI run
after this change is the real test, and it may well surface races.*

### 4. `deploy/migrate.sh` reports success while applying nothing (Windows) · **not fixed — superseded**

Running it against a virgin DB printed `[apply] migration.sql`, then
`migrations up to date.`, and **exited 0** — having applied nothing and skipped
61 files.

Two compounding causes:
1. The MSVC `psql` build does **not permute options**: with the conninfo as the
   first positional argument, `-v ON_ERROR_STOP=1 -f "$f"` are all discarded as
   "extra command-line argument ... ignored". glibc's getopt *does* permute, so
   this works on Linux — the documented deploy target.
2. Because `-f` was discarded, `psql` fell back to reading SQL **from stdin** —
   which was the `find | sort` pipe — swallowing the remaining 61 filenames. A
   classic "command inside `while read` consumes the loop's stdin" bug; `</dev/null`
   is the standard guard.

Production deploys on Linux are fine. The trap is that the project's *own dev
platform* gets a silent false green.

**Not fixed here**: a concurrent session is replacing this path with an embedded
Go runner (`db/migrate.go`) with a ledger, per-file transactions and
numeric-aware ordering. That supersedes the shell script; fixing it in parallel
would have collided with their work.

### 5. Test suite cannot pass on a fresh database · **FIXED**

`TestValidateLocationReference` asserts that locations `WH01` and `LOC-0001`
validate. **`WH01` appears in no migration at all** — it is accumulated dev-database
residue. The Stage 17.9 migration seeds Location masters via
`SELECT DISTINCT ... FROM inventory_availability`, which on a virgin DB is empty,
so only `HO` is created.

Consequence: a new developer, a fresh CI runner, or a disaster-recovery rebuild
all get a red suite from a correct codebase.

> **A hypothesis I disproved rather than reported.** My first read was that this
> was a *product* bug — locations with inventory but no master. I provisioned a
> second truly virgin database to check, and found 1 Location, 0 inventory rows,
> and **zero** codes with inventory but no master. The migration is internally
> consistent. This is a test-portability defect only.

**Fixed**: the subtest now asserts `HO` — the one code the 17.9 migration seeds
*unconditionally* (its `UNION SELECT 'HO'` branch), so it exists on every install
and is always a meaningful check. `WH01`/`LOC-0001` are probed first and asserted
only where they exist, so the back-compat guarantee is still verified on
databases that have legacy data without inventing a failure on ones that never
did. Seeding them outright was rejected: it would have made the assertion
vacuous, since the whole point is to verify what the *migration* seeded.

**Verified both ways**: green twice on a pristine database, and the legacy
assertion still executes on a database that has the legacy rows.

### 6. Stored XSS in the frontend · **NOT FIXED — needs your decision**

`public/app.js` contains **283 `innerHTML` assignments** and **no HTML-escaping
helper exists anywhere in the file**. User data is interpolated raw:

```js
tr.innerHTML = `
  <td style="font-weight:600;">${line.sku}</td>
  ...
  <td><button onclick="removeSKUFromPOSCart('${line.sku}')">Remove</button></td>`;
```

`line.sku` lands in markup *and* inside a JS string literal in an `onclick`.
There is **no charset validation on Item code/SKU** anywhere in the backend —
the only sanitiser is `sanitizeCSVCell`, which addresses spreadsheet formula
injection, not HTML.

The CSP is `script-src 'self' 'unsafe-inline'`, so injected inline script
**executes**. `frame-ancestors 'none'` and `X-Frame-Options` don't help here.

**Exploit path**: any user with Item-create rights stores a payload as a SKU; it
then executes in the browser of every user who views that item — including
HR/Admin. That is privilege escalation to full session takeover, and in a
multi-tenant SaaS it is serious.

Not fixed unilaterally because the honest fix is a choice between:
- **(a)** add an `escapeHtml()` helper and apply it at ~283 sites (thorough, invasive), or
- **(b)** validate Item code/SKU to a safe charset server-side (a few lines, closes
  the main vector immediately, leaves other fields exposed), or
- **(c)** both, then drop `'unsafe-inline'` — which requires refactoring the 21+
  `onclick=` attributes to `addEventListener` first.

**Recommendation: (b) now as a fast tourniquet, then (a), then (c).**

### 7. Money is 32-bit integer rupees — sub-rupee amounts are truncated · **NOT FIXED — needs your decision**

`gl_postings.debit` / `.credit` are **`INT`** (32-bit, scale 0). The General
Ledger cannot represent fractional currency at all, and `PostSalesFinanceBooking`
takes `salePrice int, costPrice int`.

To be fair to the code: this is **not sloppiness**. `PostSalesGSTBooking`
explicitly truncates each component *before* summing so that debits always equal
credits, with a comment explaining why. **The books always balance.**

But the consequence is real and systematic. A ₹999 sale at 18% GST:

```
CGST 89.91 -> 89     SGST 89.91 -> 89     posted tax 178 vs actual 179.82
```

₹1.82 of output-tax liability disappears on that one sale. The error is always
*downward* (floor, never round), so it accumulates in one direction forever. For
Indian GST compliance, filed returns must reconcile to the books.

Two separate issues:
- **Precision**: whole rupees cannot express GST. The standard fix is storing
  **minor units (paise) as BIGINT**.
- **Range**: `INT` caps a single posting at 2,147,483,647. In whole rupees that
  is ₹214 crore — reachable for a large enterprise. In paise it would overflow at
  ₹2.14 crore, so a paise migration **must** also widen to `BIGINT`.

One cheap intermediate exists: `int(math.Round(x))` instead of `int(x)` turns a
systematic −0.5-per-component bias into an unbiased ±0.5. It does not restore
precision.

**Not changed unilaterally — this alters financial figures and is your call.**
Flagging rather than acting is deliberate. Note this is the single hardest thing
in this report to change later: money representation gets more expensive to
migrate every year it stays.

### 8. `govulncheck` was silently broken locally · **FIXED**

The installed binary was built with Go 1.25 against a 1.26.5 toolchain and could
not load packages. It emitted a tooling-mismatch message that reads like noise.
Rebuilt against the current toolchain; it now runs and reports **0 reachable
vulnerabilities**.

Worth noting how this failed: a security gate that cannot run looks very similar
to a security gate that found nothing.

### 9. Lower-severity / decay risks

- **`golang.org/x/crypto` is v0.25.0; current is v0.54.0** (~2 years stale).
  Nothing reachable is vulnerable *today*. Decay risk, not live exposure.
- **`go.mod` says Go 1.22.12, CI pins `1.22`, local toolchain is 1.26.5.** Three
  different versions; CI is not testing what developers run.
- **`hr_payroll.go:134`** still builds a payslip code from `time.Now().Unix()`
  (whole seconds) plus employee ID. Collision needs the same employee twice in
  one second — low risk, left alone.
- **Test DB connection string was hardcoded in ~40 places.** A concurrent
  session is centralising this behind `testConnStr()`.
- **Windows CRLF**: 48–56 files fail `gofmt -l` purely on line endings.
  Pre-existing; `.gitattributes` pins LF only for `deploy/`.

---

## Verification performed

- `go build ./...`, `go vet ./...` — clean, before and after every change.
- Full suite run **5×** total (2× on the frozen snapshot, 3× on the live tree),
  `-p 1`, against the isolated cluster. **Byte-identical results each time** — no
  flakiness, no order dependence.
- Cold-start provisioning on two independently created virgin databases.
- ID-collision experiment: clock-resolution measurement, tight-loop concurrency
  at 2/4/8/16/64 goroutines, and a Poisson-arrival traffic model.
- `govulncheck ./...` after rebuilding the scanner.
- Static sweeps for SQL injection, dynamic identifiers, timezone handling,
  money precision, and XSS sinks.

**Not performed** (stated so the gaps aren't mistaken for passes):
- `-race` never executed — no C toolchain on this machine. Now wired into CI, but
  its first run has not happened.
- No live browser XSS proof-of-concept; finding 6 rests on code reading plus the
  confirmed absence of both an escaper and input validation.
- No sustained load/soak test, so no goroutine- or memory-leak measurement.
- Backup/restore and disaster-recovery drills were not exercised.

## Recommended order of work

1. **Validate Item code/SKU charset server-side** — finding 6, tourniquet, hours.
2. **Decide the money representation** — finding 7. Hardest to defer; gets worse yearly.
3. **Watch the first `-race` CI run** — finding 3. It may surface real races.
4. **Make the test suite self-contained** — finding 5, so a fresh environment can go green.
5. Escape the `innerHTML` sites, then retire `'unsafe-inline'` — finding 6, (a) and (c).
6. Refresh `x/crypto`; align `go.mod` / CI / local Go versions.
