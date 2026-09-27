---
doc_id: DOC-ASSUREVID01
title: Assurance evidence handling
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-16
review_by: 2026-10-16
supersedes: none
superseded_by: none
---

# Assurance evidence handling

**Stage 49.16.7** — established 2026-09-16. Governs how pentest reports,
audit findings, customer security questionnaires, incident records and legal
documents are stored, shared and eventually retired — as distinct from the
*content* of a finding, which belongs in
[risk_register.md](risk_register.md), or an incident record, which belongs
to [vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md)'s
process.

## What this covers

| Document class | Example | Default classification |
|---|---|---|
| Pentest/audit report | A future external assessment under `20.5`/`26.11.1` | **Restricted** |
| Customer security questionnaire (completed) | A filled-in vendor security questionnaire for a specific prospect | **Restricted** (may name that customer) |
| Incident record | A future entry in the incident register `vulnerability-disclosure-lifecycle.md` §49.15.6 governs | **Restricted** while active, **Internal** once closed and lessons are extracted |
| Legal document | DPA, security schedule, contract security exhibit | **Restricted** — governed jointly with Stage 48.6's legal document register once it exists |
| Public summary | This project's own [customer-security-pack.md](customer-security-pack.md), [SECURITY.md](../../SECURITY.md) | **Public/Customer** — the whole point of these two documents is that they're shareable |

**This project has not yet produced a document in the first four rows** — no
pentest has been engaged, no incident has occurred, no customer questionnaire
has been completed. The policy below is written now, before the first one
exists, deliberately: retrofitting access control onto sensitive documents
after they already exist and are scattered is how they end up under-protected.

## Handling rules

1. **Restricted documents are versioned and access-logged.** For a
   single-owner project today (`.github/CODEOWNERS`), "access-logged" means:
   stored somewhere with its own access history (a private repository,
   password-managed vault, or equivalent — **not** this public repository,
   and **not** email/chat, both of which have no retention or access control
   this project manages). The moment a second person needs access, that
   access is granted by name, not by a shared credential — the same
   principle [threat_model.md](threat_model.md) and the identity-lifecycle
   items elsewhere in Stage 49.2 already apply to production credentials.
2. **Retained, with an expiry, not forever.** A restricted document has a
   named retention period tied to its purpose (a pentest report: until the
   next assessment supersedes it, plus any contractually required minimum; a
   closed incident record: per whatever statutory retention Stage 47.16
   ultimately specifies — not invented here). No restricted document is kept
   indefinitely "just in case" once its purpose has expired and no
   contractual/statutory reason to keep it remains.
3. **Shared only through an approved channel.** The same reporting channel
   [SECURITY.md](../../SECURITY.md) names for intake is the model: a named,
   deliberate channel, not an ad hoc email thread. A customer's completed
   security questionnaire is returned to that specific customer, never
   reused verbatim for a different customer without checking it doesn't leak
   the first customer's name/environment details.
4. **A public summary is a distinct document, not a redacted copy.** Per
   49.16.7's own wording, a public summary "states scope/date/expiry and
   limitations without creating an attacker roadmap" — this project's
   pattern for that is already established: [customer-security-pack.md](customer-security-pack.md)
   §7 states plainly that no independent assessment exists yet, and will be
   updated with scope/date/expiry the day one is engaged, rather than a
   redacted version of the (not yet existing) report itself being produced
   and accidentally leaving in exploit-relevant detail.
5. **A pentest/audit report is never summarized by deleting the word
   "critical."** If a finding is severe, the public summary says the
   assessment happened and was acted on (per
   [vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md)'s
   patch-construction and coordinated-release sections), not that nothing
   was found.

## What is explicitly out of scope here

- The **content** of how a vulnerability is triaged/fixed/disclosed —
  that's [vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md).
- The **legal** retention/deletion obligations for these document classes
  under DPDP/contract — that's Stage 47.16/48.6, counsel-gated, not decided
  here. This document's retention language above is deliberately generic
  ("tied to purpose," "per whatever Stage 47.16 specifies") rather than
  inventing a specific number of days/years that only a qualified reviewer
  should set.
