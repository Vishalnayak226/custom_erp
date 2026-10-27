---
doc_id: DOC-SECASSESS001
title: Security assessment register
type: record
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner, customers]
applies_to: source release 0.16.0 and later
authority: source
confidentiality: internal
last_verified: 2026-10-26
review_by: 2027-01-24
supersedes: none
superseded_by: none
---

# Security assessment register

**Stage 49.17.7.** Every challenge of this system's security, by whom, over
what, and until when it counts. Evidence expires: an internal review counts
for **180 days**, an external one for **365 days**, and either stops counting
earlier after a material change (a release whose G6b report has a high-risk
line, a new provider or hosting change). The
[security scorecard](../qa/generated/security-scorecard.md) reads this table
and marks expired rows; the [assurance statement](../qa/generated/security-assurance-statement.md)
quotes the current ones.

Kinds: `internal-persona` (an attacker, insider, other-tenant or business-abuse
viewpoint), `internal-review` (a scheduled review from the
[operating cadence](operating-cadence.md)), `tabletop` (an incident rehearsed
with the people who would handle it), `external` (a qualified outside
specialist). Only `external` rows are independent; the rest are this project
testing itself and are labelled as such.

## Register

| Date | Kind | Viewpoint and scope | By | Evidence | Expires |
|---|---|---|---|---|---|
| 2026-09-01 | internal-persona | Deep persona audit: CEO, admin, operator, auditor viewpoints over the whole ERP | development sessions | `docs/archive/audits/erp-deep-persona-audit-2026-09-01.md` | 2027-02-28 |
| 2026-09-08 | internal-persona | Outside-in attacker: unauthenticated surface, headers, TLS, exposed routes | development sessions | `docs/security/outside_in_verification_2026-09-08.md` | 2027-03-07 |
| 2026-09-16 | internal-persona | Maturity audit: 136 controls incl. security, tenancy and recovery | development sessions | `docs/archive/audits/erp-independent-audit-2026-09-16.md` | 2027-03-15 |
| 2026-09-23 | internal-persona | Provider and extension abuse: bad signatures, duplicate and out-of-order webhooks, hostile hook targets (BLD-031) | development sessions | `engines/bld031_provider_extension_contract_test.go` | 2027-03-22 |
| 2026-10-25 | internal-persona | Attacker and insider: MFA bypass, reset races, purged-tenant tokens, existence disclosure, formula injection (Stages 47 / 49) | development sessions | ledger §293 | 2027-04-23 |
| 2026-10-26 | internal-persona | Detection drill: every detection rule fired against seeded attack traffic (49.11.8) | development sessions | `TestStage49118DetectionDrillCoversEveryRule` | 2027-04-24 |

No `tabletop` and no `external` assessment has been performed. The customer
security pack and the assurance statement say so rather than imply otherwise.

## Rotation

Internal persona reviews rotate so each viewpoint is revisited at least twice
a year: attacker (outside-in and authenticated), insider (privileged misuse,
separation of duties), other tenant (isolation), business abuse (money, stock,
approvals). External specialists are engaged for application / API /
multi-tenancy first, then hosting, finance and fraud, privacy, devices and
supply chain as contracts or risk require — each is a decision for the
security owner, recorded here when made.
