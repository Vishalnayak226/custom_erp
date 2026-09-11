---
doc_id: DOC-ENG-SETUP
title: Developer setup and verification
type: procedure
status: draft
owner: engineering-owner
approvers: [engineering-owner]
audience: [developers]
applies_to: isolated development checkout
authority: canonical
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# Developer setup and verification

Use an isolated checkout and development database. Read the current
[handover §6](../ai_handover.md#6-version-control--handover-status) for shared-tree state;
this procedure contains stable steps without workstation credentials.

1. Install the Go version required by `go.mod`, PostgreSQL 16-compatible tooling, Git and
   PowerShell 7 for the repository's operational wrappers. Go dependencies are in `go.mod`/
   `go.sum`. The browser application uses native assets; its optional npm bundle is not
   required for normal development.
2. Obtain approved development connection/configuration through the local secret/config
   mechanism. Set `DATABASE_URL` for the intended database; do not rely on an implicit
   connection when operating data or evidence tools. Keep real external side effects off.
3. Follow the existing migration runner or CI ordering for the full additive migration set.
   Applying only the base schema does not produce a current database. Inspect the intended
   environment before running any operator command.
4. Build and verify the change proportionally. Build the application from `./cmd/server`;
   database tests must use an isolated fixture and the serialized package order below.
5. Start the development application through the existing manager or built server with
   approved environment values. Verify the displayed environment and simulated delivery
   posture before trying any workflow.

```powershell
go build ./...
go vet ./...
go test ./... -p 1
pwsh docs/update-docs.ps1 -Group Content -Check
go run ./cmd/doclint
```

`-race` requires the supported C toolchain; CI supplies it on Linux. Documentation changes
have [focused safety checks](../test-docs-safety.ps1) that run in a temporary copy and do
not require business-data mutations. The brain also requires the ignored local graph:
refresh it explicitly with `graphify update .`; its `-Check` never refreshes it.

Record exact commands, results and unresolved failures. Do not interpret a documentation
check or a test-file link as proof that application UAT, release deployment or a provider
integration passed. [Contributing standards](contributing.md) explain review and evidence.
