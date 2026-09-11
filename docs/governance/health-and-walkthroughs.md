---
doc_id: DOC-GOV-HEALTH
title: Documentation health and sampled walkthroughs
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Documentation health and sampled walkthroughs

The monthly GitHub workflow collects the offline inventory/health report, checks deterministic generated content, runs bounded public external-link checks and retains reports for 90 days. External checks reuse results for seven days and are advisory; private, credential-bearing and query-string links require owner review without automated requests.

Review missing/stale metadata, ownership acceptance, authority, orphan/invalid links, help coverage, generated drift, unsupported capability claims, total document/KB/index bytes and migration/deletion candidates. Assign an owner, severity, action and due date in the live backlog. The large `micro_checklist.md` work register is an explicit exception to the 120 KiB reader-article budget while the standing three-tracker convention remains; its full bytes are still reported. The handover must remain at most 150 lines. Historical execution ledgers are records, not user articles.

## Quarterly sample

At least quarterly, and after a material navigation change, sample CEO/product, normal user, floor user, tenant admin, developer, SRE, implementer, QA/security and applicable auditor/legal roles. Use a real representative participant for each role; an AI/source walkthrough does not substitute for them.

| Record field | Required content |
|---|---|
| Identity/scope | Date, participant role, observer, release/configuration, source/artifact identity |
| Task | Reader question, start page, expected authoritative answer and safe recovery |
| Findability | First decision, second decision, search query if used, time and interventions |
| Execution | Device/assistive technology, expected/actual result, reconciliation or evidence reference |
| Finding | Severity, owner, backlog reference, correction and recheck date |
| Acceptance | Qualified review reference or explicit pending/blocked result |

Sample questions: supported mixed-owner scope; reverse a sale safely; resolve a rejected receipt; revoke a user; reproduce a migration; restore service; map imported masters; assess an audit or privacy claim. Save the dated result in `docs/assurance/` or the approved restricted evidence store. No quarterly human walkthrough has been claimed by creating this procedure.
