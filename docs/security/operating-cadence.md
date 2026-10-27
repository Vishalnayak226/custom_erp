---
doc_id: DOC-SECCADENCE01
title: Security operating cadence
type: procedure
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner, operations-owner]
applies_to: source release 0.16.0 and later; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-26
review_by: 2027-01-24
supersedes: none
superseded_by: none
---

# Security operating cadence

**Stage 49.17.5.** Security is an operating habit, not a one-off pass. This
page fixes what is looked at, how often, by whom, and where the proof lands.
It does not keep "last done" dates by hand: the
[security scorecard](../qa/generated/security-scorecard.md) reads each
activity's own evidence file and marks it *overdue* when the cadence below has
passed, so a skipped review shows up in the next generated scorecard rather
than in an audit.

## The cadence

| Cadence | Activity | Owner | Evidence the scorecard reads |
|---|---|---|---|
| Continuous | Critical advisory intake: Go and module advisories, provider notices | engineering-owner | the daily patch-intake worker; `govulncheck` at every release |
| Every release | Attack-surface and control evidence: routes, sign-in levels, jobs, flags, dependencies, outbound calls | engineering-owner | release gates G6 / G6b, `docs/security/attack_surface.json` |
| Monthly (35 days) | Vulnerability and patch review | engineering-owner | `docs/security/dependency-ledger.json` `updated` |
| Monthly | Restore drill on a throwaway copy | operations-owner | newest dated heading in `docs/operations/restore_drill_log.md` |
| Monthly | Access review: admins without 2FA, API keys, emergency-control use | security-owner | Activity Log → Security (live) |
| Monthly | Detection and data-growth review: fired detections, lost audit writes, the state forecast | security-owner | Activity Log → Security (live); `cmd/stateforecast` |
| Quarterly (100 days) | Threat model review | security-owner | `docs/security/threat_model.md` `last_verified` |
| Quarterly | Risk register review | security-owner | `docs/security/risk_register.md` `last_verified` |
| Quarterly | Vendor and third-party review | security-owner | `docs/security/third-party-risk-register.md` `last_verified` |
| Quarterly | Tabletop exercise on one incident runbook section | security-owner | a `tabletop` row in [assessment-register.md](assessment-register.md) |
| Annual (380 days) | Independent penetration test and program review | security-owner | an `external` row in [assessment-register.md](assessment-register.md); `docs/security/secure-development-lifecycle.md` `last_verified` |

Faster when risk or a contract requires it: a high-risk line in a release's
G6b report, a security incident, or a material change in hosting or providers
brings the quarterly reviews forward to that week.

## What "done" means

- **A review is done when its evidence file changes**, not when someone says
  so: a threat-model review that found nothing still moves `last_verified`
  and says "no change" in the file's own history.
- **Live rows** (access, detections, data growth) are read on the Security tab;
  write the date and the one-line outcome into the monthly row of
  [assessment-register.md](assessment-register.md) as an `internal-review`.
- **Every finding becomes a tracked item**: a bug goes to the backlog with a
  regression test, and the lesson goes to
  [security-lessons.md](security-lessons.md) so the test is held to existing.

## Acceptance, read honestly

The cadence and the overdue check are built. What only people can do — run
the reviews, hold the tabletop, engage an external tester — is not, and the
scorecard says so for every activity whose evidence is older than its cadence.
