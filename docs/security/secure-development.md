---
doc_id: SEC-SDLC-001
title: Secure development and dependency evidence
type: procedure
status: draft
owner: security-owner
approvers: [security-owner]
audience: [engineering, operations, implementation, QA, domain reviewers]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
---

# Secure development and dependency evidence

This workflow tailors [NIST SP 800-218 SSDF 1.1](https://csrc.nist.gov/pubs/sp/800/218/final), the February 2022 final publication, to this repository. It is a practice mapping, not certification or a statement that every practice has passed.

| Practice area | Repository procedure | Evidence for a release |
|---|---|---|
| Prepare the organization | Named requirement/control owners; threat model, risk register and scoped acceptance | Reviewed risks, responsibilities, supported configuration and exceptions |
| Protect the software | Reviewed changes, least-privilege build/release access, secret handling and artifact identity | Source revision, review reference, build inputs and artifact hash |
| Produce well-secured software | Existing controls, negative-path tests, dependency review, static/vulnerability checks | Exact build/vet/test/scan commands, versions, findings and disposition |
| Respond to vulnerabilities | Severity/owner/expiry, repair and regression test, customer impact and release response | Issue, mitigation, patched artifact, verification and communication approval |

## Before a dependency or release change

Review `go.mod`/`go.sum`, browser assets and any optional package lock. Prefer existing code and the standard library. Record purpose, license, maintainer/source, version, transitive impact, vulnerabilities, update path and removal plan. A dependency exception needs a concrete measured benefit and accountable engineering/security review.

Run the repository CI checks, including `go build ./...`, `go vet ./...`, the appropriate tests and the existing vulnerability scan. The CI vulnerability tool currently installs `@latest`; record the actual scanner version and database time in release evidence because results are time-dependent. Do not interpret a clean scan as proof of absence of vulnerabilities.

## Bill of materials and artifact provenance

From the exact release checkout capture `go list -m -json all` and `go version -m <built-server>` into the approved release evidence directory. Include native/optional browser libraries and packaged assets, source revision, toolchain/OS/architecture, build flags, lockfile hashes and final artifact hashes. This inventory is an input to an SBOM; it is not represented as SPDX/CycloneDX certification. If a customer requires one of those formats, generate and validate that format in the release pipeline before delivery.

The release approver records unresolved findings, exception owner/expiry, mitigation and support impact. Preserve signed provenance and private vulnerability detail outside the public docs tree, with non-sensitive evidence references in the [release acceptance record](../governance/release-acceptance.md). Recheck when a dependency advisory, build input or supported configuration changes.
