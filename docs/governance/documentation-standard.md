---
doc_id: DOC-GOV-001
title: Documentation standard
type: normative
status: draft
owner: documentation-maintainer
approvers: [product-owner, engineering-owner]
audience: [authors, reviewers, maintainers]
applies_to: repository documentation
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-06
review_by: 2026-10-06
supersedes: none
superseded_by: none
---

# Documentation standard

This is the Stage 48 governance draft. The tooling described here is implemented;
business, legal, UAT and release claims still require their accountable reviewers.
Existing paths remain valid during the transition. See the
[authority matrix](authority-matrix.md) and [document register](document-register.json).

## Types and lifecycle

Use `normative` for requirements/policies, `procedure` for repeatable tasks,
`reference` for facts/navigation, `template` for a blank reusable form, `record`
for an executed observation/decision, and `generated` for a reproducible projection.
Images and generator scripts are registered reference assets with their owning family.

Living documents progress through `draft → in-review → approved → active → deprecated
→ superseded → archived`. Approval requires a named accountable reviewer and review
evidence. A draft must never be presented as a currently supported capability.
Records retain the date, source revision, scope and execution outcome; their lifecycle
is `archived` once captured, without implying the captured system passed.
Generated output is a `projection`, never independent approval or assurance.

Historical evidence is preserved. Corrections are separate dated addenda with links;
do not rewrite a prior audit finding to describe today's implementation. A short
current-authority banner may be added outside the original record body.

## Metadata

New living Markdown uses the scalar and inline-list front matter demonstrated above.
Required fields are `doc_id`, `title`, `type`, `status`, `owner`, `approvers`, `audience`,
`applies_to`, `authority`, `confidentiality`, `last_verified`, `review_by`, `supersedes`
and `superseded_by`. IDs survive moves. `none` is explicit; it is not approval.
Generated files declare their sources through the output manifests. Records additionally
name capture date, release/commit, environment and evidence scope when known; unknown
historical facts must be labelled unknown. Git supplies version history.

Legacy files are classified by [register-policy.json](register-policy.json). These are
provisional role assignments, not accepted personal ownership. A `rebuild` disposition
queues missing metadata/content review; it does not certify the old content. Add actual
front matter when rebuilding each family. Do not insert governance headers into generated
files: change their source or manifest instead.

## Ownership, review and approval

| Family | Accountable owner / approver | Authors and consulted roles | Review |
|---|---|---|---|
| Vision, BRD, PRD, support, roadmap | Product owner | BA, process owners, engineering, QA | Each release; quarterly |
| Module/process requirements | Process owner | BA, module engineer, users, QA | Each workflow change |
| Architecture and ADRs | Engineering owner | Architect, module/data/security owners | Each material design change |
| Data dictionary and MDM | Data owner | Stewards, module engineers, privacy | Each schema/import change |
| Security and privacy | Security owner | Engineering, privacy, operations | Monthly; each control change |
| Legal or compliance claims | Qualified legal/control owner | Product, privacy, security | Before use; applicability change |
| Tests and executed evidence | QA owner | Testers, security, real business users | Each release or execution |
| Runbooks and deployment | Operations owner | SRE, engineering, support | Quarterly; after incidents |
| Help and manuals | Documentation maintainer + module owner | Users, support, QA | Each affected release |

The owner is responsible for scheduling review and selecting qualified reviewers.
Authors implement changes; consulted roles verify their domain; affected readers are
informed through release notes. Developers and AI may draft and report test observations.
They cannot self-approve business outcomes, legal applicability, customer UAT, compliance,
certification or Production support. Approval evidence is the last step after a concrete
reviewable draft exists. Routine reversible tooling fixes need no additional approval.

## Style, paths and confidentiality

Use one H1, task language, meaningful link labels and repository-relative file links.
New paths use lowercase kebab-case; legacy names remain until a controlled move.
ISO dates belong in immutable record filenames. Never embed workstation usernames,
machine-specific absolute paths, credentials or private contact details. Setup examples
use environment variables and explain any local development configuration explicitly.

Classes are `public`, `customer`, `internal`, `restricted` and `secret-never-in-docs`.
The last class is prohibited in committed documentation. Restricted evidence belongs
in the approved evidence store; this register may link its non-sensitive identifier.
Do not print matched secret values in lint diagnostics.

## Generation and offline validation

```powershell
pwsh docs/update-docs.ps1                 # all declared groups; existing local graph required
pwsh docs/update-docs.ps1 -Check          # compare only, including failure paths
pwsh docs/update-docs.ps1 -Group Content  # guides + OpenAPI + generated KB sources + embedded KB
pwsh docs/update-docs.ps1 -Group Content -Check
go run ./cmd/doclint                     # warning mode; no writes and no network
go run ./cmd/doclint -strict             # nonzero for any remaining finding
go run ./cmd/doclint -fail-on broken-link,broken-anchor,kb-link,generated-drift,capability-evidence
```

The guides/KB/brain compatibility wrappers use the same transaction. Brain checks never
run graphify. Refresh the ignored local graph explicitly before a write using
`graphify update .`. CI checks the Content group because a clean checkout has no graph
snapshot. Full clean-checkout graph reproducibility remains a later gate.

Generation uses the explicit date in `generation.json`, never wall-clock time. That date
records a registry verification pass, not a claim that a tenant or release was accepted.
Each output manifest records sources, generator/version, release/scope/schema/tenant and
SHA-256 over UTF-8 with LF. Only checkout line endings are normalized during comparison.
Missing/stale/hand-edited files fail checks. Orphans block publication pending explicit
retirement. No routine docs command connects to a database.

All generators finish before publication; concurrent source/output edits abort it. A
copy failure restores prior file bytes/timestamps. This protects ordinary local failures;
it is not crash-atomic across multiple files. If a process or machine dies mid-copy, rerun
`-Check` and regenerate. No repository files are deleted automatically.

Database permission exports require explicit output root, environment and tenant schema
and are dated assurance snapshots. The legacy committed matrix is historical; route,
scope, field and workflow controls mean grant rows alone are not effective authorization.

## Budgets and migration gates

Ordinary articles: 120 KiB raw; KB search index: 250 KiB; embedded KB: 2 MiB; handover:
150 lines. Report current excesses honestly in warning mode. Generated catalog exceptions
require an explicit owner decision, never an automatic increase. The full docs tree is
excluded from production; only intentionally embedded help is shipped.

Before moving a family: settle concurrent edits, capture a recoverable approved baseline,
map inbound links and unique content, review replacements, update generators/help/portal,
then validate the family. Preserve deprecated-path stubs for two supported releases.
Before deleting an explicit file: prove replacement approval, content/evidence retention,
link migration, transition expiry and release/migration record. No bulk deletion. Audits,
UAT, incidents, restore drills, decisions and required legal versions are retained.

Stage 48.0/48.1 implement the first foundation. Later Stage 48 gates own support approval,
requirements rebuild, full migration, live walkthroughs and final strict enforcement.

The Content pipeline also validates and generates the capability catalog and requirement
traceability from `docs/product/capability-register.json`. It rejects unsupported maturity
values, missing references and structurally incomplete Production/Certified claims. Qualified
reviewers still verify approval authenticity and results. Search output is compact JSON;
publication enforces its 250 KiB budget and the combined embedded KB 2 MiB budget.

## Curated manuals and screenshots

The [manual portal](../user/manuals.md) links reading/print editions generated
from canonical KB topics through `manual-selection.json`. The content pipeline
stages and hashes both manuals; no additional manual assets enter the server.
Internal topic links and heading IDs are namespaced for the combined edition.
Older guides remain transitional inputs until unique-content parity is reviewed.

Use the [screenshot procedure](screenshot-capture.md) and repaired capture harness
before recapturing any living manual asset. A synthetic Chromium test validates
the harness failure gates; human role/device/content review still controls
acceptance of the resulting set.
