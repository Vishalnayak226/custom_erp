---
doc_id: DOC-QA-ERP-MATURITY
title: ERP independent maturity checklist
type: reference
status: draft
owner: qa-owner
approvers: [qa-owner, product-owner, security-owner, operations-owner]
audience: [engineering, QA, product, implementation, operations]
applies_to: 2026-09-16 frozen local source and synthetic fixtures
authority: source
confidentiality: internal
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# ERP independent maturity checklist

This is a repeatable third-party-style review, not certification or a claim of exhaustive testing.
Existing open implementation work remains in the [project backlog](../micro_checklist.md).
An open feature is not classified as a defect. A test location is not execution evidence.
The dated report will distinguish product defects, test infrastructure defects, environment
limits, existing work and new product decisions. Runtime fixes are outside this audit.

## Execution plan

For automatable checks, execute three rounds: baseline, fixed shuffled order, and a second
fixed shuffled order, with fresh databases. Browser checks use three device/theme contexts.
Repeat discovered failures in focused checks before confirming them. Record blocked or
inapplicable checks explicitly; never turn missing evidence into a pass. Local load is bounded
and synthetic. Real providers, production load, physical devices and expert acceptance require
their own environments and reviewers. Do not use default development database test settings.

| ID | Test family | Planned evidence / acceptance question |
|---|---|---|
| QA-01 | Source and build reproducibility | Frozen HEAD plus dirty file hashes; build, dependency integrity, toolchain parity |
| QA-02 | Static analysis | Go vet, syntax, formatting, routes and bypass inventory |
| QA-03 | Unit/component/regression | Three uncached suites; pass, fail and skip counts; statement coverage |
| QA-04 | Database integration | Real PostgreSQL, fresh migration, replay, CI ordering, constraints and transactions |
| QA-05 | Security and supply chain | Vulnerability scan, auth/session/RBAC/tenant/IDOR/input/export/security regression tests |
| QA-06 | Concurrency and idempotency | Existing parallel transaction/replay cases; race detector; evidence gaps |
| QA-07 | Domain accuracy | Stock, money, tax, UOM, rounding, posting and lifecycle invariants |
| QA-08 | Contract and compatibility | API envelopes, malformed requests, method/auth checks, browser behavior |
| QA-09 | End-to-end workflows | Login/navigation plus selected documented business tasks with isolated fixtures |
| QA-10 | Accessibility and responsive UI | Keyboard, focus, labels, reflow, target sizes, themes, motion, error/empty states |
| QA-11 | Performance and footprint | Three local samples: latency/error rate under bounded concurrency, memory, payloads |
| QA-12 | Recovery and operations | Backup/restore fidelity, restart/health, migration safety, rollback tooling |
| QA-13 | Documentation and support | Strict lint, generated drift, browser harness, task instructions and evidence boundaries |
| QA-14 | SaaS lifecycle and isolation | Tenant routing/provisioning, suspension/export, entitlement and operational boundaries |
| QA-15 | Detachable module packaging | Registry/dependency/API/data/export contracts and standalone sale requirements |
| QA-16 | Product breadth | Implemented capability inventory, existing planned modules, optional candidate domains |
| QA-17 | Internationalization | Unicode, locale/date/time/money assumptions, keyboard and long-content handling |
| QA-18 | Adversarial resilience | Negative input, upload/path/size bounds, failures/retries, fault and resource exhaustion coverage |
| QA-19 | Observability and supportability | Correlation, logs, health, audit integrity, incident/restore runbooks |
| QA-20 | Release and real-world acceptance | Cross-platform, physical hardware, external providers, soak, independent review and UAT gates |

## Standards used to frame the review

The quality scope uses the [ISO/IEC 25010 product quality model](https://www.iso.org/standard/78176.html).
Web accessibility checks are mapped to [WCAG 2.2](https://www.w3.org/TR/WCAG22/);
automated DOM checks cannot establish conformance. Security scope links to the project's
[verification standards matrix](../security/verification-standards-matrix.md) and its ASVS mapping.
Business process breadth is compared with the primary-source
[Dynamics business process catalog](https://learn.microsoft.com/en-us/dynamics365/guidance/business-processes/overview)
as a discovery aid, not a requirement to implement every vendor feature.

## Results

See the [dated audit and bug report](../assurance/erp-independent-audit-2026-09-16.md),
[machine results](erp-audit-2026-09-16-summary.json),
[reproduction guide](erp-audit-reproduction.md), and
[module/SaaS/UI build roadmap](../product/erp-maturity-roadmap-2026-09-16.md).
No existing open stage is closed by this audit.

**Status key:** PASS = the stated local check passed; FAIL = observed defect or failed gate;
SAMPLED = existing regressions or limited browser observations, not complete acceptance;
OPEN = existing or proposed build/decision work, excluded from defect judgment;
UNEXECUTED = required environment, time or reviewer unavailable. Repeat count `3` means
three executed iterations; `0` is deliberately not a pass. `R3` means three full regression
runs cover part of the subject, with actual failures separately identified in the report.
Use the evidence scope, not a percentage of green cells, to decide release readiness.

## Detailed mature-ERP control checklist

### Build, provenance and supply chain

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-001 | Freeze release input and record every included file hash | PASS | One capture; 987-file manifest; final integrity comparison |
| MC-002 | Compile every Go package without changing dependencies | PASS | 3; build logs |
| MC-003 | Verify downloaded module integrity | PASS | 3; `go mod verify` |
| MC-004 | Run static Go analysis | PASS | 3; `go vet ./...` |
| MC-005 | Parse application JavaScript entry points | PASS | 3 × 4 files; Node syntax checks |
| MC-006 | Build Linux release artifact within 25 MiB | PASS | 3; 18.14 MiB and identical hashes |
| MC-007 | Match and document local/CI/release toolchains | FAIL | Local 1.26.5 vs pin 1.22.12; pinned build also succeeds |
| MC-008 | Scan reachable dependency vulnerabilities | FAIL | 3 repository scans; 6 findings, 5 on server-only scan |
| MC-009 | Verify dependency ledger and workflow pin tests | PASS | R3; supply-chain package |
| MC-010 | Prove reproducible signed release provenance and offline deployment | OPEN | Full customer release evidence; 49.9 / 49.17 |

### Functional tests and verification quality

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-011 | Execute all packages uncached against isolated fixtures | FAIL | 3; 4 / 4 / 2 failing test events |
| MC-012 | Change deterministic test order | FAIL | Baseline, seeds 4802/4803 expose policy fixture coupling |
| MC-013 | Reproduce failures with controlled prerequisites | PASS | 3 per approval-rule condition; focused results |
| MC-014 | Separate product, test, environment and unfinished scope findings | PASS | Dated report classification |
| MC-015 | Measure statement coverage | SAMPLED | 1 instrumented full run; 42.9%, not acceptance threshold |
| MC-016 | Property/metamorphic tests for economic invariants | OPEN | Expand existing examples with generated invariant cases |
| MC-017 | Coverage-guided fuzzing of parsers and state transitions | UNEXECUTED | 0; no Go Fuzz functions in snapshot |
| MC-018 | Mutation testing proves critical assertions can fail | UNEXECUTED | 0; bounded campaign still needed |
| MC-019 | Pairwise role/module/device/locale configuration matrix | OPEN | Catalog defined; full combinations not exercised |
| MC-020 | Requirements-to-tests-to-runtime evidence traceability | SAMPLED | Generated references exist; 48.3 / 47.17 acceptance remains |

### Schema, migration and data correctness

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-021 | Fresh UTF-8 database installs with supported runner | FAIL | 3 baseline rounds; AUD-02 |
| MC-022 | Retrying failed migration recovers without manual intervention | FAIL | 3 retries reach same missing prerequisite |
| MC-023 | Applied schema migration replay is a no-op | PASS | 3 after explicitly recorded fixture workaround |
| MC-024 | CI migration route reaches the same valid schema | FAIL | 1 complete rehearsal reaches AUD-02; unit ordering tests also run R3 |
| MC-025 | Upgrade previously deployed schemas and roll back compatible code | UNEXECUTED | 0 real production-version matrix; deploy doubles are narrower |
| MC-026 | Detect changes to applied migration checksums | SAMPLED | R3 migration checksum tests; production ledger unexamined |
| MC-027 | Restore all table rows without accidental change | PASS | 3; 74 tables and 13,020 rows fingerprinted |
| MC-028 | Preserve Unicode across UI, database and restored data | PASS | 3 form runs plus 3 restore drills |
| MC-029 | Enforce referential/business constraints through alternate write paths | SAMPLED | R3; extend with generated adversarial combinations |
| MC-030 | Validate timezone, DST, leap-day and period-boundary policy | OPEN | Full declared locale/timezone matrix still required |

### Business and financial integrity

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-031 | Server determines sale prices/tax and records overrides | SAMPLED | R3; approval-test fixture failures separately diagnosed |
| MC-032 | Sale, stock, ledger and payment reconcile atomically | SAMPLED | R3 atomic-checkout tests; real payment journey pending |
| MC-033 | Duplicate commands have one durable business effect | SAMPLED | R3 command/idempotency tests; broader fault matrix open |
| MC-034 | Returns/refunds reconcile original economics | SAMPLED | R3 return tests; external provider uncertainty untested |
| MC-035 | Stock reservation, release and short pick remain consistent | SAMPLED | R3 inventory/fulfillment tests; volume acceptance pending |
| MC-036 | Selected single-owner warehouse guard holds | SAMPLED | R3; unsupported mixed-owner mode is excluded |
| MC-037 | Procurement receipt-to-invoice-to-payment reconciles | SAMPLED | R3 procurement/finance tests; real buyer/accountant journey pending |
| MC-038 | GL, periods, reversals, currencies and rounding are correct | SAMPLED | R3; qualified financial acceptance remains required |
| MC-039 | Manufacturing consumption, yield, scrap and costing reconcile | SAMPLED | R3 manufacturing tests; reference factory fixture pending |
| MC-040 | Payroll, tax and statutory reports match approved cases | OPEN | Country-specific expert test vectors and sign-off |
| MC-041 | Intercompany and consolidation follow approved accounting policy | OPEN | Existing engine presence is not policy acceptance |
| MC-042 | Approval transitions and user messages match economic outcome | FAIL | QA-DEF-01 test assumptions; broader UX criteria in roadmap |

### Identity, authorization and tenant isolation

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-043 | Anonymous and malformed bearer requests are rejected | PASS | 3 local HTTP rounds |
| MC-044 | Forged resolved-role headers cannot create authority | PASS | 3 local HTTP rounds |
| MC-045 | Token tenant overrides a caller-supplied tenant header | PASS | 3 equal-response-hash comparisons; intended contract |
| MC-046 | Wrong-host token and hostname lifecycle rules hold | SAMPLED | R3 hostname tests; real DNS/TLS trial excluded |
| MC-047 | User deactivation and credential version invalidate sessions | SAMPLED | R3 identity tests |
| MC-048 | MFA/recovery/password reset controls hold | SAMPLED | R3; real delivery and support recovery untested |
| MC-049 | Row, location, owner, self and field authorization hold | SAMPLED | R3 negative tests; not exhaustive object/role matrix |
| MC-050 | Shipped role templates cover shipped metadata | FAIL | 3 full + 3 focused; AUD-07 |
| MC-051 | Maker/checker and prohibited self-approval are enforced | SAMPLED | R3; remaining SoD program stays open |
| MC-052 | Complete multi-tenant/noisy-neighbor adversarial review | OPEN | 47.1 / 49.3 / 49.10 |

### Browser and API security

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-053 | Stored record text cannot execute browser script | FAIL | 3 form runs and cross-role probe; AUD-01 |
| MC-054 | Reflected/DOM/template injection is blocked across every surface | OPEN | Regression coverage needs expansion after AUD-01 |
| MC-055 | Static directories do not expose listings | PASS | 3 local HTTP rounds |
| MC-056 | Encoded traversal cannot read source files | PASS | 3 specific local HTTP probes; not exhaustive path fuzzing |
| MC-057 | Static write methods and unknown API routes reject correctly | PASS | 3 local HTTP rounds |
| MC-058 | Request/body/upload/query limits reject resource abuse safely | SAMPLED | R3 boundary tests; no disk/OOM chaos campaign |
| MC-059 | SQL/filter keys and identifier inputs reject injection | SAMPLED | R3; no claim of full DAST/pentest coverage |
| MC-060 | Session storage, CSP, CSRF and cookie model meet approved design | OPEN | Existing 47.8 / 49.4 work; XSS is concrete evidence |
| MC-061 | Route and worker attack inventory matches the executable | FAIL | 3 scans and R3 freshness failures; QA-DEF-02 |
| MC-062 | Independent authenticated security assessment and abuse review | UNEXECUTED | 0 external review; 49.10 / 49.17 |

### SaaS lifecycle and commercial boundaries

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-063 | All declared package selections converge normally | PASS | 3 × 10 selections |
| MC-064 | Failed package changes cannot report complete success | FAIL | 3 injected disable failures; AUD-05 |
| MC-065 | Configured quota rejects excess usage | PASS | 3 focused engine checks |
| MC-066 | Database failure is distinguishable from an absent quota | FAIL | 3; AUD-06 |
| MC-067 | Provision/suspend/resume/offboard/hold transitions hold | SAMPLED | R3 lifecycle tests; real operator acceptance pending |
| MC-068 | Every SKU completes a standalone customer journey | OPEN | PKG-02; normal entitlement tests are insufficient |
| MC-069 | Module disable/downgrade preserves retained data and drains jobs | OPEN | PKG-05; define read/export/re-enable contract |
| MC-070 | Subscription invoices, proration, grace and cancellation are owned | OPEN | PKG-06; commercial decision, not a declared defect |
| MC-071 | Metering, quotas and costs reconcile per tenant and period | OPEN | PKG-07/09; usage dashboard is only part of acceptance |
| MC-072 | Module data/API/event dependencies are explicit and versioned | OPEN | PKG-01/03/10; keep modular monolith unless evidence requires more |

### Integration, import/export and extensibility

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-073 | Provider timeout/retry/signature/outbox regressions execute | SAMPLED | R3 local doubles; no real provider certification |
| MC-074 | Real marketplace, payment, bank, tax and courier acceptance | UNEXECUTED | 0; provider credentials/sandbox/customer scope required |
| MC-075 | CSV/import validation and failed-row outcomes are recoverable | SAMPLED | R3; representative customer migration still open |
| MC-076 | Export enforces scope, privacy and spreadsheet-safe text | SAMPLED | R3; expand stored-markup/security variants |
| MC-077 | Large imports/exports stream within time/memory limits | OPEN | 47.9/47.10; no representative large-file trial |
| MC-078 | Public API compatibility/version/deprecation contracts hold | SAMPLED | R3 API regressions; old-client compatibility matrix open |
| MC-079 | Extensions fail safely and cannot escape approved capability scope | SAMPLED | R3; hostile extension review still required |
| MC-080 | Connector/API/worker schema changes preserve queued work | OPEN | Upgrade/fault-injection matrix; PKG-10 |

### User experience and visual quality

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-081 | Implemented screen navigation renders across three contexts | SAMPLED | 162 observations; hook-log subview requires selected record |
| MC-082 | Real generic create form saves and refreshes | PASS | 3 Vendor creations, with security/accessibility failures separately tracked |
| MC-083 | Dense and long-content tables remain usable | OPEN | Empty/default traversal does not establish dense-data usability |
| MC-084 | Empty states explain setup and provide next actions | SAMPLED | Three browser contexts; content review on selected screenshots |
| MC-085 | Error/offline/loading states preserve task context and input | OPEN | Selected error views only; full interruption matrix pending |
| MC-086 | Shared component/token hierarchy works in both themes | SAMPLED | Desktop light, mobile dark, tablet light; no full visual-diff baseline |
| MC-087 | Main role tasks need minimal navigation and no dead ends | OPEN | Observed task study required; 47.12 / 47.14 |
| MC-088 | Mobile/RF supports complete physical work | OPEN | Existing 47.6; emulator viewport is not a scanner test |
| MC-089 | Human-language outcomes distinguish draft/pending/paid/failed | OPEN | Roadmap acceptance and reference journeys |
| MC-090 | Print/export output is usable on supported devices | UNEXECUTED | 0 physical printer/PDF-layout acceptance in this audit |

### Accessibility and internationalization

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-091 | Every editable control has a programmatic name | FAIL | 3 AX-tree checks; AUD-08 |
| MC-092 | Dialog save/cancel returns useful visible focus | FAIL | 3 save observations; AUD-09 |
| MC-093 | Keyboard order, focus visibility and scroll regions are usable | SAMPLED | 3 context tab sequences; complete task audit still required |
| MC-094 | Reflow avoids page-level horizontal overflow | PASS | 54 views × 3 contexts, within sampled empty/default state |
| MC-095 | Screen reader announces structure, changes and validation | UNEXECUTED | 0 real assistive-technology sessions |
| MC-096 | Text/non-text contrast and color-independent meaning conform | UNEXECUTED | 0 complete measured contrast audit; do not infer from theme tokens |
| MC-097 | Target spacing, zoom, text spacing and high contrast work | OPEN | Full WCAG/device matrix pending |
| MC-098 | Reduced motion does not hide required state transitions | SAMPLED | Browser contexts requested reduced motion; manual assessment pending |
| MC-099 | Unicode names survive save/read/restore | PASS | 3 browser submissions and 3 restore drills |
| MC-100 | RTL, localization, long labels and locale formatting work | OPEN | Unicode storage is not full localization support |

### Performance, capacity and resource use

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-101 | Record hardware/workload/percentile/error treatment | PASS | Local fixture documented; nearest-rank percentiles |
| MC-102 | Bound interactive page size | PASS | 3 requests for 1,000,000 rows capped at 1,000 |
| MC-103 | Meet local interactive latency target | PASS | 3 rounds × concurrency 1/4/8; p95 ≤103.2 ms |
| MC-104 | Rate-limit excess demand without counting rejects as fast successes | PASS | Raw overload preserved separately; final measured rounds all HTTP 200 |
| MC-105 | Small-process memory stays within sampled budget | SAMPLED | 3 adjacent Windows samples; about 37 MiB working set |
| MC-106 | Initial frontend meets 120 KiB JS / 180 KiB cold-core targets | FAIL | 3 HTTP measurements; existing 47.9/47.10 work remains open |
| MC-107 | Representative Linux production-capacity proof | UNEXECUTED | 0 production-equivalent load runs; no capacity promise |
| MC-108 | Long soak demonstrates bounded memory/storage/queue growth | UNEXECUTED | 0 multi-hour/day soak |
| MC-109 | Measure real LCP/INP and warm-screen interaction budgets | OPEN | Script settling time is not LCP/INP acceptance |
| MC-110 | Stress, spike, saturation and noisy-neighbor recovery | OPEN | Bounded local HTTP sample is not this campaign |

### Recovery, release and operations

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-111 | Dump and restore preserve table contents | PASS | 3 independent new restore databases |
| MC-112 | Meet approved business RTO/RPO, including attachments/keys | OPEN | Small local restore timings are not approved RTO/RPO |
| MC-113 | Healthy deployment activates a matching release | PASS | 3 real-script command-double drills |
| MC-114 | Unhealthy deploy restores matching binary and frontend | FAIL | 3; AUD-03 |
| MC-115 | Restart-command error enters recovery | FAIL | 3 command-double failures; AUD-04 |
| MC-116 | Migration failure leaves a matching prior release | FAIL | 3 command-double failures; AUD-03 |
| MC-117 | Restart/shutdown/crash preserves accepted work | UNEXECUTED | Local process action rejected by automatic approval review |
| MC-118 | Point-in-time restore and corrupt/missing backup alarms | UNEXECUTED | 0 real PITR/corruption drill in this audit |
| MC-119 | Disk full, OOM, DB outage, packet loss and clock skew recover | UNEXECUTED | 0 infrastructure chaos; focused quota/entitlement faults are narrower |
| MC-120 | Cross-platform runtime and proxy/service compatibility | OPEN | Windows runtime + Linux compilation; no Linux runtime/edge acceptance |

### Audit, privacy and observability

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-121 | Signed event/checkpoint/archive regressions execute | SAMPLED | R3; one Unix permission case skipped on Windows |
| MC-122 | Retention, legal hold and offboarding have accountable decisions | OPEN | Existing 47.16 / 49.6 / 49.16 |
| MC-123 | Privacy access/correction/export/deletion respects scope | SAMPLED | R3 supported regressions; real legal acceptance excluded |
| MC-124 | Logs/errors do not disclose protected fields or credentials | SAMPLED | R3 redaction/security tests; not exhaustive production-log review |
| MC-125 | Correlation and actionable alerts support diagnosis | SAMPLED | Existing tests/runbooks; no real on-call delivery drill |
| MC-126 | Key rotation/revocation and encrypted archive recovery work operationally | OPEN | Engine regressions do not replace operator key-loss/restore rehearsal |
| MC-127 | Incident/forensic/disclosure workflows are exercised | UNEXECUTED | 0 human tabletop/external security response drill |
| MC-128 | Security and audit retention meet measured storage budgets | OPEN | 49.18; source inventory is not a growth forecast |

### Documentation, support and release acceptance

| ID | Check / acceptance | Result | Repeats / evidence or next step |
|---|---|---|---|
| MC-129 | Strict documentation health is green for reviewed source | FAIL | 3 Git-aware rounds; 8 findings; Stage 48 remains open |
| MC-130 | Generated content matches the source snapshot | FAIL | 3 initial safety gates; fixture normalization kept separate |
| MC-131 | Generator check modes are read-only and safe under failure | PASS | 3 × 13 cases on normalized isolated fixture; original freshness still fails |
| MC-132 | Manuals help users finish real tasks without hidden setup | OPEN | Three Vendor tasks are not full manual UAT; 47.15 / 48.7 |
| MC-133 | Screenshots and evidence identify exact release/role/fixture | PASS | Hashed input, fixed roles, synthetic data and browser context records |
| MC-134 | Support, installation, migration and recovery guides are executable | OPEN | Selected drills only; complete implementation journey pending |
| MC-135 | Legal/accounting/payroll/security owners approve applicable claims | UNEXECUTED | 0 external professional sign-offs; no inferred compliance |
| MC-136 | All chosen customer reference journeys meet release gates | OPEN | 47.17 / 49.17; capability maturity remains unchanged |

## Open build and decision register

The [live build and acceptance checklist](../product/erp-build-checklist.md) now maps every
MC-001–136 control to implementation or release verification, with dependencies, owners,
acceptance and a next-thread prompt. Its status reflects current work, including later fixes;
the results above remain the September 16 frozen-source audit and must not be overwritten.

Use the existing stage IDs for known work. Do not duplicate unfinished features as audit bugs.
The nine concrete defects and two test/inventory findings have reproducible evidence in the
report. Proposed new work is grouped as PKG-01–12 and PERF-01–08 in the roadmap, with a
separate candidate-domain table. Each implementation should close its own acceptance test,
not merely add a screen or mark a broad module complete.
