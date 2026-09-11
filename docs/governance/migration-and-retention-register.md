---
doc_id: DOC-GOV-MIG
title: Controlled migration and retention register
type: reference
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

# Controlled migration and retention register

The recoverable pre-migration snapshot was captured on 2026-09-09 from HEAD `fed51b4` plus 889 verified working-tree files. The detached local worktree `stage48-baseline-20260909` and [SHA-256 manifest](../assurance/stage48-baseline-2026-09-09.json) preserve that state. It includes concurrent Stage 49 work and is not an approved commit or release. Review and approve the concrete baseline before committing migration batches.

## Reviewed batch plan

| Batch | Family / destination | Ready material and remaining approval |
|---|---|---|
| 1 | Governance/product/requirements | Canonical drafts and projections exist; historical BRD/PRD/blueprint retained with authority banners; product/process approval pending |
| 2 | Architecture/data/API/security | Current views, decision register, dictionary and security drafts; legacy designs remain historical; engineering/control approval pending |
| 3 | Engineering/operations/implementation/QA | Owned procedures/workbooks and audience portal; accountable operational review and actual deployment evidence pending |
| 4 | User/KB/manuals/assets | Canonical task sources, generated manuals and safe screenshot harness; legacy parity, physical captures and human task review pending |
| 5 | Assurance/legal/project/generated/archive | Lifecycle register and evidence templates; legal template move and baseline/history commit approval pending |

For each approved move use an explicit file list and `git mv`, update its real inbound links/generator paths, add a published-path stub where required and record predecessor/replacement, release, batch commit and validation. Retain stubs for at least two supported releases; elapsed calendar time alone is not enough. Run content checks, doclint, relevant application tests/help smoke and brain refresh. Rollback must identify that batch's reviewed commit. Never stage concurrent work as part of a documentation move.

## Deletion register

**No repository document or evidence deletion is approved or performed by this register.** Potential candidates are reproducible duplicate projections, replaced screenshot sets, temporary staging files, empty directories, expired stubs or byte-identical duplicates. None becomes eligible merely because it is old.

| Required per-file proof | Record before deletion |
|---|---|
| Identity and recovery | Exact path, predecessor hash/commit, retained archive reference |
| Unique content | Section/control-to-canonical mapping and reviewer |
| Replacement | Approved effective replacement and both supported transition releases |
| Consumers | Markdown/code/generator/KB/published inbound references updated or redirected |
| Evidence/retention | Qualified legal/operational hold review and required evidence retained |
| Validation | Clean staged/checkout checks, release-note/migration record and approval |

Delete only the individually approved paths after these fields are complete. Audits, signed acceptance, incidents, restore drills, governing decisions and historical ledgers are retained records. The existing server packaging excludes the full docs tree; only the explicitly embedded KB belongs in the application, bounded by 2 MiB raw and 250 KiB search index.

## Concrete move review

The [24-path review table](migration-review.md) and its hashed plan describe all five proposed batches. The isolated combined preview is reviewable; approval and per-batch commits remain pending.
