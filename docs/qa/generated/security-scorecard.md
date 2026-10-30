# Security Scorecard

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: the security evidence files named in each row
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from the security evidence files named in each row, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

Evidence, not one number (Stage 49.17.4). Judged as of 2026-10-30, the newest dated ledger section; *attention* rows need an owner's look.

| Area | Measure | State | Evidence |
|---|---|---|---|
| Attack surface | 868 routes: integration-credential 5, none 3, public 27, session 828, static 5 | classified (every route has a capability; tests fail otherwise) | `docs/security/attack_surface.json` |
| Configuration | 44 environment flags, 14 security-critical | listed | `docs/security/attack_surface.json` |
| Dependencies | 2 modules, 2 direct | pinned by go.sum | `docs/security/attack_surface.json` |
| Dependency provenance | ledger updated 2026-10-29 | current | `docs/security/dependency-ledger.json` |
| Open risks | 11 open | no review overdue | `docs/security/risk_register.md` |
| Detections | 23 rules (7 page, 16 queue for review) | every rule fires in the drill test | `engines/security_detection.go`, `TestStage49118DetectionDrillCoversEveryRule` |
| Tenant and access negative tests | 17 test functions | run in every release (G10) | `*redteam*_test.go`, `stage49_3*_test.go`, `stage49_4_7*_test.go` |
| Restore drill | last logged 2026-08-07 | attention: last logged drill 84 days ago (monthly expected); `deploy/restore_drill.sh` now also checks ledgers and audit evidence | `docs/operations/restore_drill_log.md` |
| Security documents | 26 documents | attention: past review_by - README.md, access-control-policy.md, assurance-evidence-handling.md, customer-security-pack.md, dependency-inventory.md, human-operations-and-social-engineering.md, risk_register.md, secure-development-lifecycle.md, third-party-risk-register.md, threat_model.md, verification-standards-matrix.md, vulnerability-disclosure-lifecycle.md | `docs/security/*.md` front matter |
| Cadence: threat model review | last 2026-09-09 | current | `docs/security/threat_model.md` |
| Cadence: risk register review | last 2026-09-09 | current | `docs/security/risk_register.md` |
| Cadence: vendor review | last 2026-09-16 | current | `docs/security/third-party-risk-register.md` |
| Cadence: access and detection review | never done | attention: due every 35 days, never logged | `docs/security/assessment-register.md` |
| Cadence: tabletop exercise | never done | attention: due every 100 days, never logged | `docs/security/assessment-register.md` |
| Cadence: program review | last 2026-09-11 | current | `docs/security/secure-development-lifecycle.md` |
| Cadence: independent penetration test | never done | attention: due every 380 days, never logged | `docs/security/assessment-register.md` |
| Assessments | 6 current | attention: 0 expired, 0 external | `docs/security/assessment-register.md` |
| Security lessons | 10, each with a regression test | held by `TestStage49178EveryLessonHasALiveRegressionTest` | `docs/security/security-lessons.md` |
| Bounded state | 19 tables swept, 18 kept as evidence or fixed-size, 4 in-memory capped | every state table registered (`TestStage49182EveryStateTableIsBounded`) | `engines/bounded_state.go` |
| Access recertification | admins without 2FA, API keys to review | live only | Activity Log → Security |
| Audit and data growth | audit rows, lost audit writes, verifier failures | live only | Activity Log → Security |
| User-reported security friction | - | not measured yet | - |
