---
doc_id: DOC-SUPCHAIN0001
title: Secure development lifecycle, dependency and release-artifact supply chain
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [engineering, operations, security-owner]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-11
review_by: 2026-10-11
supersedes: none
superseded_by: none
---

# Secure development lifecycle, dependency and release-artifact supply chain

**Stage 49.9** — established 2026-09-06. Companion to
[threat_model.md](threat_model.md) (persona P7 "developer/CI account
compromise", misuse case M-11 "ship a back door", crown jewel #9 "release
artifact and migrations") and [risk_register.md](risk_register.md) (R-09).

Not a duplicate of [secure-development.md](secure-development.md) (Stage 48,
`SEC-SDLC-001`): that document maps this repository's practice to NIST SP
800-218 at the policy level and is the one to cite for SSDF language. This
document is the engineering build of the tooling it points at — the ledger,
the manifest generator, the pinned CI actions and the release-artifact job
that make its "record purpose, license, maintainer/source, version,
transitive impact" line and its "CI vulnerability tool currently installs
`@latest`" gap (now fixed — see §49.9.4) actually true of the tree, not just
asked for.

> **Status of this document.** Written the same way threat_model.md was:
> every claim points at something in this repository — a file, a test, a
> workflow step — not at a plan. Sections §49.9.2 and parts of §49.9.5/§49.9.8
> describe GitHub repository *settings* this build session cannot apply
> itself; those are marked **[needs decision: org owner]** rather than
> written as prose that implies they're done. This is not a certification —
> the same domain approvals threat_model.md names (security, infrastructure,
> product) still apply before this is treated as an accepted control set.

## 0. What "before runtime" buys, and what it doesn't

Every control in this document runs at build time, in CI, or as a developer
process — never in the request path of a running tenant. That is the
"lightweight security rule" this whole Stage answers to: none of it can be
defeated by adding load to a checkout, and none of it costs the production
binary a single import (`internal/supplychain`, like `internal/securityscan`
before it, is imported only by its own tests and by a `cmd/` tool).

What it buys: a compromised dependency registry, a compromised contributor
account, a hostile pull request, or one compromised CI job cannot **silently**
turn into a trusted-looking production release — each of those paths now
either fails a test, fails a pinned-hash comparison, or produces a
mismatched checksum an operator can check before trusting an artifact.

What it does not buy, honestly: this repository's actual deploy path
(`deploy/deploy.ps1`) builds on an operator's own machine and ships over
SSH — it does not yet consume or verify the CI-produced release manifest
below. Closing that gap fully means changing what an operator actually runs
at deploy time, which is a deployment-behavior change this session did not
make (see §49.9.7's last paragraph). What exists today is the mechanism and
the CI-side half of the trace; wiring the deploy side to check it is real,
scoped, follow-on work.

---

## 49.9.1 — Security requirements and change trigger

**Mechanism, not yet a hard gate.** `.github/pull_request_template.md`'s
"Security / privacy / permissions" line now asks explicitly for the fields
49.9.1 names — affected crown jewels/trust boundaries (cite
[threat_model.md](threat_model.md) §2/§3), abuse cases (cite §5 or add one),
capabilities/fields/scopes touched, data classification/retention, secrets
or egress introduced, failure/recovery behavior, and what test proves it —
with an explicit "why not" for a change that touches none of them. This is
a template, not a required check: GitHub does not enforce that a PR
description's checkboxes are actually filled in, only that the template is
what a contributor sees. **[needs decision: org owner]** to add a literal
required status check (e.g. a PR-description-linter Action) if a stronger
gate than "the template asks" is wanted — deliberately not added here,
since a linter that can be satisfied with "N/A" everywhere is closer to
theater than to the control 49.9.1 describes, and a real one needs a
human-reviewed rubric, not a keyword match.

## 49.9.2 — Review ownership

**Built:** [`.github/CODEOWNERS`](../../.github/CODEOWNERS) — maps
auth/tenant, finance/inventory, migrations, deploy/CI and the security
program itself to a named owner, with an explicit note (in the file itself)
that it currently names the repository's one real contributor rather than a
fabricated team, and what to do when a second one joins.

**[needs decision: org owner]** — CODEOWNERS enforces nothing until branch
protection turns it on. Apply, on `main`, in GitHub's Settings > Branches:

- Require a pull request before merging, with **"Require review from Code
  Owners"** checked.
- Require status checks to pass before merging — at minimum the
  `build-and-test` job from `.github/workflows/ci.yml` (which now also runs
  `internal/supplychain`'s dependency/action-pinning gates and `govulncheck`
  as part of the same job).
- Restrict who can push directly to `main` (no direct pushes, not even by an
  admin, if "include administrators" is enabled).
- A signed, auditable emergency-exception path: GitHub's own audit log
  records any admin override of the above; there is no additional mechanism
  in this repository to add on top of that, and none is proposed — a second,
  home-grown override log would just be a second thing to keep honest.
- **Prompt leaver revocation** is an organization-membership action (removing
  repository/org access when someone leaves), not a file in this repository —
  recorded here as a process the org owner runs, with no code-side control
  possible.

## 49.9.3 — Secret-safe development

**Built and already running:** `gitleaks/gitleaks-action` in
`.github/workflows/ci.yml`, pinned to a full commit SHA (Stage 49.9.8), scans
every push and pull request. `deploy/erp.env.example` ships only placeholder
values, and `engines/security_baseline.go`'s SB-004 refuses a production boot
on the exact placeholder shape it ships (risk register R-06). Test data is
already isolated per the project's existing convention — every engine test
runs against `tenant_default`/scratch schemas seeded by the test itself, not
a copy of production data; there is no production data anywhere in this
repository or its CI to leak in the first place, which is a stronger
position than most codebases start from.

**Documented, not code-enforced (process, not a gate):**

- **No production secret/data in an issue, chat, screenshot or test.** This
  is a human practice. The concrete cases this repository's own history has
  already had to reason about — the sandbox-tenant classifier
  (`engines/saas.go`, Stage 38.7) blocking direct SQL against production, and
  the fact this session's own worktree has no access to the production
  droplet — are the closest things to an enforced version of this that
  exist; the general rule (don't paste a real customer's data into a bug
  report) has no automated check and none is proposed, because there is no
  reliable way to detect "this JSON blob is a real customer's" versus
  synthetic test fixture without false positives that would themselves leak
  the thing they're checking.
- **Masked CI output.** GitHub Actions automatically masks any value equal to
  a configured secret in its logs; this repository has no step that echoes a
  secret value deliberately, and `engines/security_baseline.go`'s own
  findings are tested (`TestBaselineFindingsNeverEchoConfiguredValues`) to
  never include the configured value itself — the same discipline applied to
  what CI's own log lines might print.
- **Restricted fork/PR secret exposure.** `pull_request` (not
  `pull_request_target`) triggers this workflow, which is the safe default —
  a fork's PR runs with a read-only, repo-scoped token and none of this
  repository's real secrets (there are none configured for CI today; the
  `GITHUB_TOKEN` gitleaks-action receives is GitHub's own ephemeral,
  read-only-by-default token, not a deploy credential). **[needs decision:
  org owner]** if any real secret (e.g. a future container registry
  credential) is ever added to repository secrets: confirm it is not exposed
  to workflows triggered by `pull_request` from a fork, per GitHub's own
  fork-PR secret model.
- **Rotate-not-just-delete.** Already the documented procedure elsewhere in
  this repository (`deploy/README.md`'s rotation guidance, the 29.8 signing-key
  rotation keyring cited in threat_model.md crown jewel #1) — 49.9.3 does not
  change it, just cross-references it here so the SDLC document doesn't
  silently omit secret rotation.

## 49.9.4 — Dependency minimization and verification

Covered in full in [dependency-inventory.md](dependency-inventory.md) and
[dependency-ledger.json](dependency-ledger.json). Summary: one ledger across
direct (none)/transitive (2 Go modules)/tool (govulncheck)/OS
(runner + container images) dependencies, each with owner/purpose/license/
provenance/version/checksum; `internal/supplychain`'s tests fail the build if
a module is bumped or added without a matching ledger entry, or if any
workflow references an unpinned GitHub Action. `go mod verify` runs in CI
before every build. `govulncheck` is pinned to `v1.7.0` rather than
`@latest` (the vulnerability *database* it queries is still fetched fresh
each run — the tool version is pinned, the analysis stays current).

## 49.9.5 — Security test layers

| Layer | Cadence | Where |
|---|---|---|
| Route/auth/validation/invariant tests | Every push and PR | `go test ./... -p 1 -race -v` in `build-and-test` — includes `internal/server/authorization_contract_test.go`, `internal/server/route_capabilities_test.go`, the Stage 47 red-team tests, and now `internal/supplychain`'s ledger/pinning gates. |
| Race detector | Every push and PR | `-race` on the same run (Linux CI only — see the existing comment in ci.yml; this dev tree is on Windows, where `-race` needs cgo the local box doesn't have). |
| Static analysis | Every push and PR | `go vet ./...`; `go build ./...` itself catches the rest Go's type system can. |
| Dependency vulnerability scan | Every push and PR | `govulncheck ./...`, pinned tool version (49.9.4). |
| Secret scan | Every push and PR | `gitleaks-action`, pinned (49.9.3). |
| Dependency/action-pinning gate | Every push and PR | `internal/supplychain` tests, called out by name in a dedicated CI step (49.9.4/49.9.8). |
| Fuzz tests | Not yet applicable | No `func Fuzz*` exists in this codebase today. Not fabricated for this Stage — a fuzz target is only worth writing against a real parser/decoder boundary (e.g. a future binary import format), and inventing one now would test nothing real. Recorded here as an explicit "none exist," not a silent gap. |
| Representative DAST / deployment scan against an isolated release candidate | **[needs decision]** — not built | Requires a running isolated instance of the release candidate (the binary the new `release-artifact` CI job now produces is the right candidate to point this at) plus DAST tooling (e.g. an OWASP ZAP baseline scan). Standing this up needs a disposable Postgres + app instance inside CI or a short-lived cloud sandbox, which is materially more infrastructure than "small Go tooling," and doing it half-built (a scan with no real findings triage process behind it) would produce false assurance rather than real coverage. Left as a scoped follow-up, not attempted this session. |
| False-positive suppression | Owner/reason/expiry required | Already the pattern in this codebase, not newly invented: `internal/securityscan/bypass_test.go`'s `reviewedBypassFindings` and this document's own `[needs decision]` markers carry an owner and a reason; `risk_register.md`'s `Review` column is the expiry mechanism. Any govulncheck/gitleaks suppression added in future must follow the same shape — an inline `//nolint`-style comment with no reason and no review date is exactly what 49.9.5 asks not to allow, and none exists in this codebase today. |

## 49.9.6 — Hermetic / reproducible release

**Built:** the `release-artifact` job in `.github/workflows/ci.yml` (runs
after `build-and-test` passes, on a `v*` tag push or manual dispatch):

- `go mod verify`, then `CGO_ENABLED=0 GOOS=linux GOARCH=amd64
  GOFLAGS=-mod=readonly go build -trimpath -ldflags="-s -w -X ...gitCommit=
  -X ...buildTime="` — no cgo, no network-fetched runtime asset, `-mod=readonly`
  refuses to build if go.mod/go.sum don't already describe every import,
  `-trimpath` removes the build machine's absolute file paths from the
  binary (removing one common source of non-reproducibility).
- Locked toolchain: `actions/setup-go` pinned to an exact patch (`1.22.12`,
  matching `go.mod`'s `go` directive exactly) via a pinned action commit.
- Locked modules: `go.sum` already pins every module's content hash; `go mod
  verify` checks it.

**Documented, not eliminated — the exact nondeterminism (49.9.6's own
escape hatch):** the embedded `buildTime` ldflag is wall-clock at build time
by design (it's operational metadata — "when was this built" — not a
content input), so two builds of the identical commit will differ in that
one field and therefore in overall binary bytes. `cmd/releasemanifest`
records `go_version` (`runtime.Version()`) and every dependency/toolchain
identity precisely so an independent rebuild can be compared field-by-field
(commit, toolchain, dependency checksums, migration/static-asset hashes) even
though the binary's raw bytes will not match byte-for-byte because of the
timestamp. Comparing two independent builds' *manifests* rather than their
raw binary hashes is the documented alternative 49.9.6 explicitly allows
("compare independent build hashes or document the exact nondeterminism") —
this project takes the second option, and names why.

- Migration and static-asset identity: `cmd/releasemanifest` hashes every
  `db/*.sql` file (in the same filename-sorted order CI's own "Apply database
  schema" step uses) into one `migration_set` digest, and every file under
  `public/` into one `static_asset_set` digest — so "what schema and what
  frontend shipped with this binary" is part of the same evidence record,
  not a separate claim.

## 49.9.7 — Artifact provenance and signing

**Built:** `cmd/releasemanifest` (package `internal/supplychain`) produces,
for one build:

- A minimal SBOM: the dependency ledger's rows (module/action/tool/OS,
  version, checksum, license, owner), carried into the manifest verbatim.
- Checksums: SHA-256 of the built binary (`build/erp-server.sha256` in CI),
  plus the migration-set and static-asset-set digests above.
- A provenance record: commit, branch, builder identity
  (`github-actions:<workflow>/<run id>` in CI — traceable back to the exact
  run in GitHub's own UI — or `local:<whatever the caller passes>` for a
  manual build), the Go toolchain version that actually compiled it, and a
  generation timestamp.
- `supplychain.VerifyArtifact(manifest, name, path)` recomputes a candidate
  file's SHA-256 and refuses (returns `ok=false` with a stated reason) on
  any mismatch — the mechanism a deployment step runs before trusting a
  binary. `cmd/releasemanifest -verify` is the CLI form of the same check.

**[needs decision: infrastructure/security]** — **signing itself is not
built**, because there is no key to sign with and provisioning one is an
infrastructure decision this session cannot make on its own (a real signing
key needs a place to live that is not this repository, this CI log, or this
build workspace — a cloud KMS, an HSM, or at minimum a GitHub Environment
secret scoped to the `release-artifact` job with required-reviewer
protection). What's built is exactly what a signing step would sign: the
manifest's own bytes, or its SHA-256, are a stable, deterministic input a
`cosign sign-blob` / `minisign -S` / `gpg --detach-sign` step can cover in
one added CI step, once a key exists — the comment left in
`.github/workflows/ci.yml` right after the manifest-generation step names
exactly where that step goes.

**Deployment verification is real for transfer integrity, not yet for CI
provenance.** `supplychain.VerifyArtifact` can be (and is designed to be)
run by a deploy step to refuse an altered binary — but `deploy/deploy.ps1`
today builds locally on the operator's machine and ships directly over SSH;
it does not fetch or verify a CI-built artifact at all, so there is nothing
yet for `VerifyArtifact` to check against in the real deploy path. Making
that true end-to-end means changing what `deploy.ps1` actually does (fetch
the CI-produced artifact + manifest instead of building locally, verify
before shipping) — a deployment-behavior change with its own testing needs
against the live droplet, which this session deliberately did not make
unreviewed. Recorded as the natural next step, not attempted here.

## 49.9.8 — CI/runner compromise containment

| Control | Status |
|---|---|
| Minimal token permissions | **Built.** `permissions: contents: read` at the workflow's top level in `.github/workflows/ci.yml`; no job requests more. |
| Pinned immutable third-party actions | **Built and tested.** Every action in every workflow is pinned to a full commit SHA; `TestNoWorkflowActionIsUnpinned` fails the build on any future unpinned one, in any workflow file, not just the ones that existed at Stage 49.9. |
| Isolated ephemeral runners/jobs | **Already true, not new.** GitHub-hosted runners are single-use VMs torn down after each job; nothing in this repository runs a persistent self-hosted runner. |
| Protected environments (approval gate for production deployment) | **[needs decision: org owner].** No GitHub Environment exists in this repository yet. `release-artifact` deliberately does not deploy anything — it only builds and uploads a reviewable artifact — so there is nothing today that a protected-environment approval gate would sit in front of. If/when an automated deploy step is added (see 49.9.7's last paragraph), it should run as a job targeting a GitHub Environment with required reviewers, exactly the gate 49.9.8 asks for. |
| No untrusted code with production secrets | **True by construction.** No workflow in this repository uses `pull_request_target` (which would run a fork's code with base-branch secrets); the one workflow that exists uses `pull_request`, and there are no production secrets in repository/CI configuration today for a compromised job to reach. |
| Post-job credential/artifact cleanup | **Already true, not new.** GitHub-hosted runner VMs are destroyed after each job; nothing persists between runs by design. The `release-artifact` job's own workspace (including `build/`) dies with the runner. |

## 49.9.9 — Source-to-production trace

**Built, for the CI half.** One `release_manifest.json` per tagged build
links: the reviewed commit (`GITHUB_SHA`) → the pinned toolchain/dependency
ledger (embedded verbatim) → the tests that ran before this job could even
start (`release-artifact` has `needs: build-and-test`, which already
includes the full `go test ./... -race`, `govulncheck`, `gitleaks` and
`internal/supplychain` gates — a failure in any of them means the manifest
job never runs) → the artifact's own checksum → the migration set and
static asset set it was built with. What it does not yet reach: **exceptions**
(this document's own `[needs decision]` markers and `risk_register.md`'s open
rows are the closest existing analogue, not yet linked by ID into the
manifest itself) and **deployment/rollback/monitoring evidence** (that lives
on the droplet — `deploy/deploy.ps1`'s own `DEPLOY-OK` marker check,
`docs/ai_handover.md` §6's commit-hash record — not in a CI artifact, because
CI does not perform the actual deploy). Closing that last link — the
manifest naming, or being named by, the eventual deploy-time record — falls
out naturally once 49.9.7's deploy-side verification step exists; until
then, an operator reconstructs the full trace by hand from: this manifest
(CI side) + `docs/ai_handover.md` §6 + `deploy/deploy.ps1`'s own output
(deploy side). That reconstruction is possible today, not automatic.

---

## Acceptance, read honestly

The Stage 49.9 acceptance line is: *"compromise of a dependency registry,
contributor account, untrusted pull request or one CI job cannot silently
publish a trusted production release; operators can verify exactly what
source/config/migrations produced every running artifact; production gains
no mandatory runtime dependency from the assurance tooling."*

- **No mandatory runtime dependency:** true. `internal/supplychain` is
  imported only by its own tests and by `cmd/releasemanifest`; nothing in
  `cmd/server` or any package it imports references it.
- **Cannot silently publish a trusted release:** true for what "publish"
  means in this repository today — there is no automated deploy, so the
  worst a compromised dependency/PR/CI job can do is produce a
  build/manifest artifact sitting in a GitHub Actions run for a human to
  review before it's ever copied anywhere. It becomes false the day an
  automated deploy step is added without also adding the protected-environment
  approval gate and the deploy-side verification described in 49.9.7/49.9.8 —
  which is exactly why both are called out here as prerequisites for that
  future step, not optional polish.
- **Operators can verify what produced every running artifact:** true for
  any build made through `release-artifact` from this point forward; not
  retroactively true for whatever is running in production today, which was
  built by hand before this manifest existed. The next deploy through
  `deploy.ps1` should be the one where an operator additionally runs
  `cmd/releasemanifest` locally and keeps the resulting manifest next to
  `docs/ai_handover.md`'s commit-hash record, closing that gap for that one
  deploy even before the tooling is wired together automatically.
