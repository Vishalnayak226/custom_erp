---
doc_id: OPS-RUN-001
title: Deployment, configuration and service health
type: procedure
status: draft
owner: operations-owner
approvers: [operations-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Deployment, configuration and service health

Platform operators start here; tenant administrators use the [tenant admin manual](../user/tenant-admin-manual.html). Choose the approved environment and deployment record before running commands.

## Deploy and configure

Follow the platform-specific [deployment procedure](../../deploy/README.md), with the release's source/artifact identity, migration set, credentials from the approved secret system and previous artifact/backup references. Native service deployment and optional container packaging are different configurations; record the one actually used. Do not copy a developer's trust-authentication database settings to production.

Record configuration keys and intended values by non-secret reference: database/TLS, tenant lifecycle, environment delivery mode, identity/session, backup locations/keys, outbound provider settings and alert destination. Validate required values without writing secrets to logs or evidence. Keep real provider actions disabled in development unless a scoped trial is authorized.

## Observe and respond

Check service readiness, error/latency percentiles, database connections/locks/storage, job/outbox backlog and oldest age, backup freshness and external delivery failures. Record dataset, tenant count, request mix and measurement window with every capacity claim. Use [NFRs](../requirements/nonfunctional-requirements.md) and Stage 47 as the proposed budgets; the deployment owner must approve actual SLO/RTO/RPO targets.

| Symptom | First safe action | Evidence / follow-up |
|---|---|---|
| Application unavailable | Inspect service and database health in the intended environment | Correlation/time, service logs and last deployment; follow [incident runbook](incident_runbook.md) |
| Growing latency/backlog | Inspect query/lock/pool and job age before increasing limits | Workload profile and measured bottleneck; [jobs/outbox](jobs-and-outbox.md) |
| Stale or failed backup | Verify scheduler, destination, key access and recent result | [Backup/restore procedure](backup_restore.md), then isolated drill |
| Wrong business result | Contain the affected path with authorized controls; preserve evidence | Reconcile sale/stock/GL/provider state; do not repair via ad hoc SQL |
| Unexpected external send | Verify environment/delivery controls and affected jobs | Preserve provider/command references; incident owner decides containment |

Secrets, on-call contacts and customer hostnames stay in approved configuration/contact systems. This document names accountable roles only. Record incident execution separately from the reusable procedure.
