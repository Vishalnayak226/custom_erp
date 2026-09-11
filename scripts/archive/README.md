---
doc_id: DOC-E388F349B2
title: Archive
type: reference
status: draft
owner: engineering-owner
approvers: [documentation-maintainer, engineering-owner]
audience: [maintainers, engineering-owner]
applies_to: source documentation; scoped release acceptance required
authority: source
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Archive

One-off scripts and artifacts from past sessions, kept for historical record rather than deleted (repo convention: don't delete, archive). None of these are referenced by any current build/deploy/test path.

- `patch.js`, `wrap_tables.js` — already-applied, one-time Node scripts that mutated `public/app.js` directly (a "Code" auto-generation field for Master doctypes, and a table-wrapper HTML fix). Their effects are already baked into the current `public/app.js`; re-running them against the current file would likely no-op or error since the patterns they match have already changed.
- `diff.txt` — a captured `git diff` of `public/app.js` from a past session, saved in a mangled UTF-16-without-proper-decode encoding. Kept as-is rather than "fixed," since it's a historical artifact, not active documentation.
