---
doc_id: ADR-001
title: Existing lightweight application topology
type: record
status: archived
owner: engineering-owner
approvers: [engineering-owner]
audience: [engineering, operations]
applies_to: existing repository topology
authority: historical
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Existing lightweight application topology

Retrospective record of a governing repository constraint, captured from the development
tree on 2026-09-07. This records existing implementation and standing project guidance; it
does not invent a historical approval signature or approve new service dependencies.

The current architecture uses one Go application, PostgreSQL with tenant schemas and native
browser assets. The [root README](../../../README.md), [server entrypoint](../../../cmd/server/main.go),
[database implementation](../../../db/db.go) and [frontend](../../../public/index.html) provide
the source evidence. Documentation tooling remains build/maintenance tooling, with embedded
help served through the existing application.

The constraint reduces deployment components and keeps authorization/economic controls in
shared code. Its consequences include coordinated application releases, explicit database
contention/capacity management and in-process worker lifecycle responsibilities. It does not
prove any particular tenant count, high availability or production resource budget.

Reconsider only when representative measurements or a concrete requirement justify a change;
record alternatives, resource cost, failure/recovery behavior and an approved migration/rollback
before adding infrastructure. See [current architecture](../current-architecture.md) and
[NFRs](../../requirements/nonfunctional-requirements.md) for current concerns and targets.
