---
doc_id: DOC-ASSURANCE-RELEASE-BUDGET-20261004
title: Release artifact and cost budget evidence, 4 October 2026
type: record
status: draft
owner: engineering-owner
approvers: [engineering-owner, operations-owner]
audience: [engineering, operations, QA]
applies_to: working tree at afaf419 plus the uncommitted Stage 50.14/BLD-048 batch
authority: historical
confidentiality: internal
last_verified: 2026-10-04
review_by: 2026-11-04
supersedes: none
superseded_by: none
---

# Release artifact and cost budget evidence — 4 October 2026

BLD-048 evidence record. Covers the three reproducible release artifacts, the
measurement units every recorded number uses, and the threshold-failure tests
that now gate the canonical NFR budgets.

Reproduce with `go run ./cmd/releasebudget -root . -binary <stripped-binary>
-database-url <read-only-url> -out <report.json>`; the Go tests in
`cmd/releasebudget/main_test.go` enforce the same thresholds without needing a
built binary or a database.

## 1. Measurement units

Every metric in the emitted report is an integer **byte** count, and the report
carries this in its own `units` field so a stored artifact stays interpretable
without this page:

| Term | Definition used here |
| --- | --- |
| KiB | 1024 bytes |
| MiB | 1024 × 1024 bytes |
| gzip asset | Each HTTP resource compressed **independently** at `gzip.BestCompression`, then summed — this is what a browser actually downloads, since each file is its own response. It is *not* the size of one archive containing all of them, which would be smaller and would understate the budget. |
| raw bytes | On-disk size, uncompressed — used for the embedded KB, the search index and the per-topic cap, because those are embedded in the binary rather than served compressed. |
| stripped binary | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w"` |

`schema_version` is `1`. Statuses are `within_budget`, `over_budget`,
`not_measured` (the input was not supplied in this environment) and
`measured_no_numeric_policy` (recorded, but no approved cap exists to compare
against — see §4).

## 2. Three reproducible release artifacts

Three independent builds of `./cmd/server`, each measured separately. All three
binaries are **byte-for-byte identical** (`-trimpath` plus the stripped
ldflags make the build deterministic), and all three reports are identical:

| Round | Binary SHA-256 | Binary bytes |
| --- | --- | --- |
| 1 | `972d8b41ab11316a952dd6b83b2c2bece701ac04208736ed94f1b04f370621c6` | 16,990,368 |
| 2 | `972d8b41ab11316a952dd6b83b2c2bece701ac04208736ed94f1b04f370621c6` | 16,990,368 |
| 3 | `972d8b41ab11316a952dd6b83b2c2bece701ac04208736ed94f1b04f370621c6` | 16,990,368 |

Environment: Windows host, cross-compiled `linux/amd64`, Go toolchain as
pinned by `go.mod`; measurement database `custom_erp_t2` on loopback
`127.0.0.1:5490` (a disposable instance, not a tenant database).

## 3. Budgeted artifacts — all within limit

Hard thresholds, from NFR-COST-001 and NFR-DOC-001. Identical across all three
rounds.

| Metric | Measured | Limit | Headroom | Clause |
| --- | --- | --- | --- | --- |
| `binary_stripped_bytes` | 16,990,368 (16.2 MiB) | 26,214,400 (25 MiB) | 35.2% | NFR-COST-001 |
| `cold_core_gzip_bytes` | 151,744 (148.2 KiB) | 184,320 (180 KiB) | 17.7% | NFR-COST-001 |
| `initial_js_gzip_bytes` | 113,817 (111.2 KiB) | 122,880 (120 KiB) | 7.4% | NFR-COST-001 |
| `embedded_kb_raw_bytes` | 924,297 (902.6 KiB) | 2,097,152 (2 MiB) | 55.9% | NFR-DOC-001 |
| `kb_search_index_raw_bytes` | 150,153 (146.6 KiB) | 256,000 (250 KiB) | 41.3% | NFR-DOC-001 |
| `kb_largest_topic_raw_bytes` | 84,047 (82.1 KiB) | 122,880 (120 KiB) | 31.6% | NFR-DOC-001 |

The largest single KB topic is `error-code-reference.html`. The per-topic cap is
measured as the **maximum** article, not the mean: the clause is per topic, so
an average would let one oversized article hide behind the other 48.

### Why the frontend numbers differ from the BLD-041 figures

BLD-041 recorded 139.3 KiB cold-core and 100.3 KiB initial JS over the startup
set `index.html`, `styles.css`, `db.js`, `components/erp-typeahead.js`,
`app.js`, `qz-print.js`. The budget tool measures a deliberately **larger**
profile — it adds `theme-boot.js` and `view-pos.js`:

- `theme-boot.js` is a real blocking startup script in `index.html`.
- `view-pos.js` is the first authorized transaction screen's lazy module.
  Excluding the first chunk a signed-in user actually loads would understate
  the startup cost that the budget exists to bound.

So 148.2 KiB vs 139.3 KiB and 111.2 KiB vs 100.3 KiB are the same tree measured
over a wider, more conservative set — not a regression. Both profiles are
within budget; the stricter-to-pass of the two is the one enforced.

## 4. Cost surfaces recorded without invented limits

DB growth, logs, audit archives and backups are required BLD-048 cost
surfaces. They are reported as measurements, **not** gates: NFR-DATA-001
assigns accountable owners for growth and retention but approves no numeric
capacity cap, and inventing one here would be a fabricated limit presented as
policy. `TestGrowthAndRetentionCostsAreCaptured` asserts both that each surface
is reported and that none of them carries a `limit`.

| Surface | Measured (disposable instance) |
| --- | --- |
| `postgres_database_bytes` | 35,615,203 |
| `tenant_relation_bytes` | 11,214,848 |
| `system_error_log_relation_bytes` | 516,096 |
| `audit_archive_relation_bytes` | 3,211,264 |
| `application_log_files_bytes` | 1,810,927 |
| `backup_artifacts_bytes` | 5,496,929 |
| `retention_days.whole_database_backups` | 14 (`deploy/backup.sh` default) |

These values describe a scratch database seeded by test runs. They are a
baseline for the measurement mechanism, **not** a production growth
observation; a production figure needs a stated workload and window per the
NFR evidence rules.

**Open, and owned elsewhere:** approved numeric caps for these six surfaces are
a data/operations owner decision, tracked under NFR-DATA-001 and BLD-049/053.
Until they exist, growth is observable but not enforceable.

## 5. Threshold-failure tests

`go test ./cmd/releasebudget/` — ten tests, all passing. The budget-gating ones:

- `TestCommittedArtifactsAreWithinCanonicalBudgets` — runs the real measurement
  against the real tree and fails if any artifact crosses its NFR limit. This
  is what makes a frontend or KB regression break `go test ./...`, not only CI.
- `TestEveryCanonicalBudgetCarriesAHardThreshold` — fails if a budget silently
  becomes observation-only, and asserts the binary metric is
  absent-but-declared rather than missing when no artifact is supplied.
- `TestEveryCanonicalBudgetFailsOneByteOverItsLimit` and
  `TestBinaryBudgetFailsOverTwentyFiveMiB` — prove each threshold actually
  bites at exactly one byte over, and that equal-to-limit passes.
- `TestGrowthAndRetentionCostsAreCaptured` — §4's honesty check.
- `TestReportDocumentsItsMeasurementUnits` — every metric is byte-unit and the
  units string documents KiB/MiB/gzip.
- `TestMeasurementIsReproducibleAcrossRuns` — committed-tree metrics are
  identical run to run.

CI enforces the same thresholds twice: the test job builds a stripped binary
and runs the tool against the live CI database, and the release job records
`build/release_budget_report.json` as a retained 90-day artifact.

## 6. No new infrastructure

No Redis, message broker, or other service was added. The tool is stdlib-only
apart from the `lib/pq` driver already vendored for the server, and its
database read is a single read-only `pg_*` size query.
