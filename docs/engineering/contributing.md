---
doc_id: DOC-ENG-CONTRIB
title: Change, migration and verification standards
type: procedure
status: draft
owner: engineering-owner
approvers: [engineering-owner, qa-owner]
audience: [developers, reviewers, release-maintainers]
applies_to: repository changes
authority: canonical
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Change, migration and verification standards

Use the existing engine/handler, error response, UI, approval and report patterns. Keep the
Go/PostgreSQL/native frontend topology and avoid dependencies without measured need and an
architecture decision. Additive migrations must be safely rerunnable and compatible with
the documented upgrade/rollback path; no destructive table rewrite is routine maintenance.

Review worktree status before edits and before staging. Shared uncommitted work belongs to
its author until reviewed; stage only explicit reviewed files. Do not blanket-add, reset,
clean, rebase or move another session's work. If generation detects concurrent edits, wait
for a stable source snapshot and rerun; do not disable the guard.

Tests should verify business invariants and meaningful failures. Use isolated fixture data,
deterministic cleanup and serialized database test packages. For economic commands test
replay, concurrency, partial failure and reconciliation; for permissions test wrong tenant,
role, scope, field and workflow state. Keep simulated external delivery off unless the
specific environment/provider verification was authorized.

Review requirements/support implications, API/schema compatibility, privacy/access, help,
operations/recovery and release notes. Use the [PR template](../../.github/pull_request_template.md)
and [release evidence template](../governance/release-acceptance.md). Version labels are not
acceptance evidence. A rollback must state application/configuration/database boundaries
and prove recovery, rather than assume reversing a binary reverses a migration.

Update the live checklist, project ledger and handover for finished work. Regenerate owned
outputs rather than hand-editing them. The [documentation standard](../governance/documentation-standard.md)
governs approval, archive, path migration and deletion separately from ordinary code fixes.
