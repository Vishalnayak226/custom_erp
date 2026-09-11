---
doc_id: DOC-SUPCHAIN0004
title: Dependency inventory and verification
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

# Dependency inventory and verification

**Stage 49.9.4** — established 2026-09-06. Companion to
[dependency-ledger.json](dependency-ledger.json) (the data) and
[secure-development-lifecycle.md](secure-development-lifecycle.md) (the
overall SDLC policy this is one piece of).

## What this covers, and why it is one ledger

49.9.4 asks for direct, transitive, **tool** and **OS** dependencies in one
inventory, each with an owner, purpose, license, provenance note, version and
checksum. Splitting those into separate documents (one for Go modules, one
for CI tooling) is how a supply-chain inventory quietly stops covering half
of the actual supply chain — the CI runner, the actions it trusts and the
container images it builds from are just as much a dependency as
`github.com/lib/pq`, and are exactly the surface Stage 49.0's persona P7
(developer/CI account compromise) and M-11 (ship a back door) target. So
[dependency-ledger.json](dependency-ledger.json) is one file, one schema,
covering all four categories:

| Type | What's in it today |
|---|---|
| `go_module_direct` | None — this project has zero direct third-party Go dependencies. |
| `go_module_indirect` | `github.com/lib/pq`, `golang.org/x/crypto` — the only two entries in `go.mod`. |
| `go_toolchain` | The Go compiler/stdlib itself (`go.mod`'s `go` directive). |
| `ci_action` | Every third-party GitHub Action referenced from `.github/workflows/*.yml`. |
| `ci_tool` | `govulncheck`, installed via `go install ...@<pinned version>` rather than vendored. |
| `os_runner` | The GitHub-hosted runner image (`ubuntu-24.04`). |
| `os_container_image` | The CI Postgres service container and the (dormant) Dockerfile's base images. |

## What is enforced automatically, and what is not

`internal/supplychain`'s tests run as part of the ordinary `go test ./...`
gate on every push (see also `.github/workflows/ci.yml`'s explicit
"Dependency and workflow supply-chain checks" step, which reruns the two
gate tests by name so they're visible in the CI log without reading a full
`go test -v` transcript):

| Test | Fails when |
|---|---|
| `TestDependencyLedgerCoversGoModules` | A Go module in `go.mod` has no ledger entry, the wrong direct/indirect type, a version that doesn't match the ledger, an empty owner/purpose/license/provenance/checksum field, or a checksum that disagrees with `go.sum`'s content hash for that exact version. |
| `TestNoWorkflowActionIsUnpinned` | Any `uses: owner/repo@ref` line in any `.github/workflows/*.yml` file has a ref that is not a full 40-character commit SHA — applies to every action in every workflow, not just the ones reviewed for Stage 49.9. |
| `TestDependencyLedgerCoversWorkflowActions` | A GitHub Action is referenced in a workflow with no matching `ci_action` ledger entry, independent of whether it happens to be pinned. |
| `TestLedgerEntriesAreWellFormed` | A ledger entry has an unrecognised `type`, a duplicate `name`, or an empty version/owner/purpose/provenance. |

What is **not** automatically enforced, and why:

- **License compatibility.** Nothing in this repository checks that a
  dependency's declared license is compatible with how the product is
  distributed — that is a legal judgement, not a parseable fact, and adding
  a license-scanning dependency to check it would violate the "no new
  dependency" rule this project holds itself to. The ledger records each
  license by hand; a human reviews it when the entry is added or changed.
- **OS container image digests.** `postgres:16`, `golang:1.22-bookworm` and
  `gcr.io/distroless/static-debian12` are recorded with floating tags, not
  digests — see the `note` field on each entry in the ledger for why
  (ephemeral CI-only fixture, or a dormant build path, in both cases lower
  practical risk than the runner image or Go toolchain that actually execute
  reachable code). This is a real, deliberately-accepted residual gap, not
  an oversight — tracked so it reads as a decision, not silence.
- **Toolchain match between CI and an operator's laptop.** CI's
  `actions/setup-go` and `deploy.ps1`'s local `go-portable` install are
  pinned independently; nothing today fails if they drift apart between a
  CI run and a manual `deploy.ps1` build. `cmd/releasemanifest` records
  `go_version` (the actual compiler that produced a given build,
  `runtime.Version()`) in every release manifest specifically so a drift
  would be visible in the evidence trail after the fact, even though nothing
  blocks it before the fact.

## Reviewing a new dependency

CLAUDE.md's standing rule is no new third-party Go module or JS library
"unless there is genuinely no reasonable way to do it with the stdlib" —
that bar is deliberately high, and this checklist is what to do on the rare
occasion it's cleared, or when a new CI-only tool/action is added:

1. **Source and maintainer.** Is it the project's own canonical repository
   (not a fork claiming to be a mirror)? Who maintains it — a named
   individual, a company, a language team? When was its last release?
2. **Update surface.** What does adding it let it reach — does it run at
   build time only (a CI tool/action) or does its code ship in the
   production binary (a Go module)? A build-time-only tool that turns out to
   be compromised affects the build; a runtime module that turns out to be
   compromised affects every deployed tenant.
3. **Vulnerability history.** Run `govulncheck` against a build that
   includes it; check the module/action's own issue tracker for open
   security reports.
4. **License.** Record it verbatim, from the dependency's own `LICENSE` file
   or repository metadata — not from memory or a marketing page.
5. **Add a ledger entry** with every field filled — an empty field fails
   `TestLedgerEntriesAreWellFormed` and an entry missing entirely fails
   `TestDependencyLedgerCoversGoModules` / `TestDependencyLedgerCoversWorkflowActions`.
6. **Pin it.** A Go module is already pinned by `go.sum`'s content hash. A
   GitHub Action must be pinned to the full commit SHA behind the release
   tag you intend to use (`git ls-remote https://github.com/<owner>/<repo>
   refs/tags/<tag>` gives it) — `TestNoWorkflowActionIsUnpinned` refuses an
   unpinned one regardless of whether anyone remembered this step.

## Regenerating / checking

The ledger is hand-maintained data, like `risk_register.md` — there is
nothing to "regenerate" for it. To check it against the current tree:

```sh
go test ./internal/supplychain/... -v
```

The release manifest (SBOM + checksums + provenance for one build) **is**
generated, by `cmd/releasemanifest` — see
[secure-development-lifecycle.md](secure-development-lifecycle.md) §49.9.6–49.9.7
for what it produces and where it plugs into CI.
