---
doc_id: DOC-PROD-002
title: Outcome roadmap
type: reference
status: approved
owner: product-owner
approvers: [product-owner, engineering-owner]
audience: [business-owners, product, engineering]
applies_to: the two reference configurations
authority: canonical
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# Outcome roadmap

The [live checklist](../micro_checklist.md) owns implementation tasks. This roadmap owns
outcomes and order; the [capability register](capability-register.json) owns scoped maturity.

The [ERP maturity build checklist](erp-build-checklist.md) supplies Stage 50's detailed
execution and acceptance queue: demonstrated defects first, then reliable verification,
sellable modules, usable UI, measured operating budgets and approved customer journeys.
It preserves the original Stage IDs and separates later domain decisions from defects.

| Order | Outcome | Gate |
|---|---|---|
| 1 | Trust sale, return and authorization outcomes | Built in Stage 47.1-47.4; signed tenant evidence stays with 47.17; preserve adversarial regressions |
| 2 | Keep stock owned and floor work usable | 47.5 single-owner guard built; physical-device 47.6.6 evidence still open; mixed-owner (Stage 59) is built but outside the reference evaluation |
| 3 | Defend audit evidence and release scope | 47.7.6 archive/restore, Stage 49 and qualified applicability review |
| 4 | Let users set up, learn and recover | 47.13-47.15, Stage 39 help and Stage 48 manuals/implementation guides |
| 5 | Grow performance and breadth with verified demand | Stage 47 measured budgets, remaining domain/37/38 depth for approved references |

Old parity/maturity/market files remain inputs; the tracker, not those files, lists what is open. Before retiring one, map each still-open
requirement and decision to an outcome and live work item. Completed history remains retained.
That mapping and retirement are not yet complete.

## Release communication

Each customer release needs a reviewed note identifying release/configuration, changed
behavior, migration/action required, known limits, compatibility, rollback and evidence.
Ledger-derived KB notes remain engineering excerpts, not a product-reviewed changelog or
release approval. [Release acceptance](../governance/release-acceptance.md) defines the record.

## Approval

Approved by the product owner on 2026-11-15 (ask 193) and checked against the product as it stands that day. Approval fixes the text. It is not a support claim: capability maturity stays in the [capability register](capability-register.json), where nothing is Production or Certified.
