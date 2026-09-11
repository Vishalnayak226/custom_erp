---
doc_id: IMPL-DEL-001
title: Discovery, cutover and support handoff workbook
type: template
status: draft
owner: implementation-owner
approvers: [implementation-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Discovery, cutover and support handoff workbook

Create a dated customer execution record from this workbook; never overwrite the blank template with one customer's outcome. Link the [onboarding workbook](onboarding-workbook.md), [data migration](../data/import-and-migration.md) and [local operating procedure](operating-procedure-template.md).

| Gate | Fill in before advancing | Accountable approval |
|---|---|---|
| Discovery/scope | Reference configuration, entities, locations, owner model, users, volume, countries, processes, exclusions and measurable outcomes | Business/product owner |
| Configuration | Tenant/environment, numbering, roles/SoD, currencies/tax decisions, masters, workflows and local controls | Process/data/finance owners |
| Data migration | Source-to-target map, keys, quality rules, dry-run counts, rejects, reconciliation, cutover delta and rollback | Data owner |
| Connector/device readiness | Sandbox/provider identity, delivery mode, webhook/replay tests, scanner/printer/browser/locale results | Integration and floor owners |
| Business UAT | Real role tasks, expected outcomes, negative/recovery cases, reconciliation and unresolved severity | Customer process owner + QA |
| Training | KB/manual versions, role training attendance, observed independent task/recovery completion | Customer operations owner |
| Cutover | Freeze/delta window, backup/recovery references, migration sequence, go/no-go criteria and authority | Business + operations |
| Hypercare/handoff | Monitoring, on-call reference, incident route, support limits, open issues/owners, exit criteria and acceptance | Customer + support owners |

For every result retain date, release, fixture/environment, participant role, device, commands or actions, expected/actual evidence, findings and signatures where required. No customer has accepted this blank workbook. Mixed-owner warehouse use and physical RF acceptance retain Stage 47 gates.
