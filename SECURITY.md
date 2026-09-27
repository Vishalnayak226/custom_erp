---
doc_id: DOC-SECURITYMD001
title: Security Policy
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner, external researchers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-13
review_by: 2026-10-13
supersedes: none
superseded_by: none
---

# Security Policy

This repository is a live, single-tenant-per-schema ERP. Full engineering detail —
intake, severity, remediation SLOs, patch construction, coordinated release and the
emergency-update drill — lives in
[docs/security/vulnerability-disclosure-lifecycle.md](docs/security/vulnerability-disclosure-lifecycle.md).
This file is the short, public-facing entry point GitHub and researchers expect at
this path.

## Supported versions

There is no tagged release line yet (`git tag` is empty) — this project ships as one
continuously-deployed line from `main`. **The only supported version is the commit
currently deployed to production**, recorded in
[docs/ai_handover.md](docs/ai_handover.md) §6. Once `v*` tags exist (the
release-artifact pipeline in
[docs/security/secure-development-lifecycle.md](docs/security/secure-development-lifecycle.md)
§49.9.6 already builds from one), this section gets a real supported-versions table
instead of a one-line placeholder.

## Reporting a vulnerability

**Preferred: GitHub private vulnerability reporting.**
[Report a vulnerability](https://github.com/Vishalnayak226/custom_erp/security/advisories/new)
opens a private advisory visible only to the repository owner — no public issue, no
public commit history leak. **Status: not yet enabled on this repository**
(`gh api repos/Vishalnayak226/custom_erp/private-vulnerability-reporting` returns
`"enabled": false` as of 2026-09-13) — **[needs decision: org owner]** to turn on in
Settings → Security → "Private vulnerability reporting". The link above will 404
until then.

**Fallback until that is enabled:** open a GitHub issue containing *only* "I have a
security report, please advise a private channel" — no technical detail, no proof of
concept — and the owner (`@Vishalnayak226`) will follow up with a private channel.
Do not put exploit details, credentials, or real customer data in a public issue.

**Encrypted contact option:** not yet provisioned — no PGP key or equivalent exists
for this project today. **[needs decision: security-owner]** if a report ever needs
one; recorded here as an open gap rather than a fabricated key.

## What to include

- The affected endpoint, screen, command, or file, and the exact request/steps that
  reproduce it.
- Impact: what an attacker gains (data read/write, cross-tenant access, privilege
  escalation, availability) — see
  [docs/security/threat_model.md](docs/security/threat_model.md) §3 for this
  project's own crown-jewel/trust-boundary language if it helps frame severity.
- Whether you tested against a synthetic/sandbox tenant or something else (see Safe
  Harbor below — this matters for how the report is triaged).
- A proof of concept is welcome but not required; a clear enough description to
  reproduce is enough to open a report.

## Safe harbor

Security research against this project is welcome under these bounds:

- **Test only against your own account/tenant and synthetic data.** This codebase
  already has a hard boundary for this: `engines/saas.go`'s sandbox-tenant
  classifier refuses direct production-data operations, and every engine test in
  this repository runs against scratch schemas, never a copy of production data —
  the same isolation this policy asks researchers to respect from the outside.
- **No denial-of-service testing**, no automated scanning beyond a low, reasonable
  rate, no destructive operations (data deletion, corruption) against anything but
  your own sandbox tenant.
- **No social engineering** of the maintainer, contributors, or any real customer.
- **No accessing, retaining, or exfiltrating another tenant's or customer's real
  data**, even if a bug makes it reachable — stop and report instead.
- Reports made in good faith within these bounds will not result in legal action
  from this project.

## Response targets and what happens next

Acknowledgement, severity assessment, remediation SLOs and the coordinated-release
process are the full subject of
[docs/security/vulnerability-disclosure-lifecycle.md](docs/security/vulnerability-disclosure-lifecycle.md)
(Stage 49.15.2–49.15.7). In short: every report is acknowledged, reproduced in an
isolated sandbox before anything else, and tracked in
[docs/security/risk_register.md](docs/security/risk_register.md) alongside this
project's own internally-found risks — one register, not two.

## Confidentiality

Reporter identity and report content are kept confidential, shared only with
whoever is needed to fix the issue, and never published with exploit-enabling
detail before a fix ships. Credit is given in the eventual advisory if the
reporter wants it, and withheld if they don't.
