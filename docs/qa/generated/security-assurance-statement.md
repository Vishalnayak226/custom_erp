# Security Assurance Statement

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `internal/server/VERSION`, `docs/security/risk_register.md`, `docs/security/assessment-register.md`, `docs/security/security-lessons.md`
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `internal/server/VERSION`, `docs/security/risk_register.md`, `docs/security/assessment-register.md`, `docs/security/security-lessons.md`, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

What is supported, how to report a problem, what is not yet true, and how far the evidence reaches - as of 2026-10-29 (Stage 49.17.8). Not a certification.

## Supported version and configuration

- **Version 0.19.0** (`internal/server/VERSION`). Only the latest release receives security fixes; there is no long-term support line.
- **Configuration:** one Go binary and PostgreSQL behind a TLS reverse proxy (Caddy) on a Linux host, installed by `deploy/`; each client in its own database schema; the browser app served by the same binary from its own origin, loading nothing from any other. Nothing else is required: no CDN, WAF, SIEM or secrets service (49.18.5).
- **Not supported:** other databases, running without TLS, sharing one schema between clients, or browser extensions that inject scripts into the app.

## Security contact

Report through the channel in [SECURITY.md](../../../SECURITY.md): a private GitHub advisory once enabled; until then an issue containing only "I have a security report, please advise a private channel". No encryption key is published yet.

## Material known limits

- **11 open risks** in the risk register (1 critical, 6 high, 2 low, 2 medium). High and critical ones:
  - R-02 Four bootstrap credentials ship in the repository (critical)
  - R-01 Re-running `db/migration.sql` resets every rotated bootstrap password (high)
  - R-03 Connector credential key can exist only on one host, outside every backup (high)
  - R-07 Runtime, migration and backup identities are the same account (high)
  - R-08 No governed break-glass or support-access evidence path (high)
  - R-09 No artifact signing, provenance or reproducible build (high)
  - R-13 No enforced code review or branch protection on `main` (high)
- Rows marked *attention* on the [security scorecard](security-scorecard.md) - overdue reviews, drills and documents - are limits too.

## Assurance scope

- **6 current, 0 expired** assessments in the [assessment register](../../security/assessment-register.md).
  - latest internal-persona: 2026-10-26, Detection drill: every detection rule fired against seeded attack traffic (49.11.8) (counts until 2027-04-24)
- **No independent external assessment has been performed.** Everything above is this project testing itself.
- **10 security lessons** fed back, each held by a regression test that runs in every release ([security-lessons.md](../../security/security-lessons.md)).
