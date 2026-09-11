---
doc_id: DATA-LIFE-001
title: Data lifecycle and retention decisions
type: normative
status: draft
owner: data-owner
approvers: [privacy-owner, legal-owner, security-owner, operations-owner]
audience: [data, privacy, security, operations, implementation]
applies_to: qualified tenant and jurisdiction-specific policy
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Data lifecycle and retention decisions

This draft defines the decision record, not legal retention durations or compliance.
Qualified owners must approve applicability before automated erasure/retention is enabled.
Stages 47.7, 47.16 and 49.6 retain the implementation and legal-control gaps.

Each category records purpose, system of record, personal/financial/authentication/audit
classification, tenant/entity/location/owner scope, permitted roles, export/masking,
retention trigger/duration, legal hold, deletion/anonymization method and evidence owner.
Include primary rows, JSON copies, files, imports, search indexes, jobs, logs, audit and backups.

Legal hold must take precedence over scheduled deletion where applicable, with authorized
creation/release and evidence. Erasure cannot silently corrupt required economic or audit
history: identify approved anonymization or retained statutory evidence and communicate
the exact limits. Backup expiry/restore behavior must not reintroduce supposedly erased
active data without the approved restoration controls.

Before any lifecycle action, preview affected objects/counts, evaluate dependencies/holds,
record authorization and prove the postcondition across relevant stores. Tenant suspension
and offboarding tools are [separate lifecycle controls](../../cmd/tenantctl/main.go), not
permission to infer retention periods or purge arbitrary evidence.
