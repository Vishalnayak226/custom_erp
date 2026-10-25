# Security Scorecard

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: the security evidence files named in each row
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from the security evidence files named in each row, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

Evidence, not one number (Stage 49.17.4). Judged as of 2026-10-26, the newest dated ledger section; *attention* rows need an owner's look.

| Area | Measure | State | Evidence |
|---|---|---|---|
| Attack surface | 846 routes: integration-credential 5, none 3, public 27, session 806, static 5 | classified (every route has a capability; tests fail otherwise) | `docs/security/attack_surface.json` |
| Configuration | 41 environment flags, 14 security-critical | listed | `docs/security/attack_surface.json` |
| Dependencies | 2 modules, 2 direct | pinned by go.sum | `docs/security/attack_surface.json` |
| Dependency provenance | ledger updated 2026-09-17 | current | `docs/security/dependency-ledger.json` |
| Open risks | 11 open | no review overdue | `docs/security/risk_register.md` |
| Detections | 12 rules (6 page, 6 queue for review) | every rule fires in the drill test | `engines/security_detection.go`, `TestStage49118DetectionDrillCoversEveryRule` |
| Tenant and access negative tests | 9 test functions | run in every release (G10) | `*redteam*_test.go`, `stage49_3*_test.go`, `stage49_4_7*_test.go` |
| Restore drill | last logged 2026-08-07 | attention: last logged drill 80 days ago (monthly expected); `deploy/restore_drill.sh` now also checks ledgers and audit evidence | `docs/operations/restore_drill_log.md` |
| Security documents | 15 documents | attention: past review_by - README.md, access-control-policy.md, assurance-evidence-handling.md, customer-security-pack.md, dependency-inventory.md, human-operations-and-social-engineering.md, risk_register.md, secure-development-lifecycle.md, third-party-risk-register.md, threat_model.md, verification-standards-matrix.md, vulnerability-disclosure-lifecycle.md | `docs/security/*.md` front matter |
| Access recertification | admins without 2FA, API keys to review | live only | Activity Log → Security |
| Audit and data growth | audit rows, lost audit writes, verifier failures | live only | Activity Log → Security |
| User-reported security friction | - | not measured yet | - |
