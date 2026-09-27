---
doc_id: DOC-VERIFMATRIX01
title: Version-pinned verification matrix
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.1.0; configuration-specific acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-13
review_by: 2026-10-13
supersedes: none
superseded_by: none
---

# Version-pinned verification matrix

**Stage 49.10.1** — established 2026-09-13. Companion to
[threat_model.md](threat_model.md) (what this system protects) and
[risk_register.md](risk_register.md) (where a gap below already has an open
row). Feeds [vulnerability-disclosure-lifecycle.md](vulnerability-disclosure-lifecycle.md)
§49.15.2's severity scoring and is the "adopted... standards" this document set
promises for **49.16.2**'s control crosswalk.

> **Status of this document.** The adopted standards and their chapter/entry
> lists below were fetched live (web search + fetch, 2026-09-13) rather than
> recalled from training data, specifically because getting a requirement ID
> wrong in a document whose whole purpose is "point at something real" would
> be worse than not writing it. What was **not** done: a line-by-line audit of
> every one of ASVS 5.0's ~350 requirements against this codebase. That is a
> materially larger undertaking than one session, and faking chapter-level
> confidence into requirement-level confidence would be exactly the "checklist
> closed ≠ actually verified" failure this project's own Stage 47.17 exists to
> catch. What this document gives instead: every requirement **chapter** and
> every Top-25 **weakness** mapped honestly to either a real, cited control in
> this repository, or an explicit open item with its own Stage/item number —
> never left blank, never asserted without a citation.

## 1. Adopted standards (version-pinned)

| Standard | Version pinned | Why this one |
|---|---|---|
| **OWASP Application Security Verification Standard (ASVS)** | **5.0.0** (released May 2025) — 17 chapters, ~350 requirements | Current stable release as of this writing; supersedes 4.0.3's 14-chapter structure. Chapter list below confirmed against `github.com/OWASP/ASVS` `5.0/en` directly, not recalled. |
| **CWE Top 25 Most Dangerous Software Weaknesses** | **2025 edition** (CISA/MITRE, published against CVEs disclosed June 2024–June 2025) | Most recent published edition; list confirmed against `cwe.mitre.org/top25/archive/2025/` directly. |
| **NIST SP 800-218 (Secure Software Development Framework)** | Already this project's adopted secure-development framework — see `docs/security/secure-development.md` (Stage 48, `SEC-SDLC-001`) and [secure-development-lifecycle.md](secure-development-lifecycle.md) (Stage 49.9), not re-adopted here. | Reused, not duplicated — 49.10.1 asks for a secure-development framework and this project already has one. |
| **CIS Benchmarks** | CIS Ubuntu Linux Benchmark (production confirmed live 2026-09-13: **Ubuntu 22.04.5 LTS**) and CIS PostgreSQL Benchmark (production confirmed live: **PostgreSQL 14.24**) | Matches the actual deployed OS/DB versions exactly, checked by SSH against the production droplet rather than assumed from an older note. **Exact CIS benchmark document edition/revision number is not pinned yet** — recorded honestly: confirming that number against `cisecurity.org` and downloading the actual control list is real, separate follow-up work, not done in this pass. |

**Exclusion, with rationale and owner, as 49.10.1 requires:** no mobile (OWASP MASVS) or cloud-infrastructure (CIS cloud provider benchmarks) standard is adopted — this system is D1/D2 single-box-hosted per [threat_model.md](threat_model.md) §1.1, with no mobile app and no cloud-managed infrastructure beyond a single droplet. Owner: security-owner; revisit if a deployment configuration outside D1/D2 is ever supported.

## 2. ASVS 5.0 chapter map

Mapped at chapter granularity, honestly, not at the ~350-requirement granularity (see the status note above). "Open" cites the Stage/item that owns full closure — never left as a bare gap.

| # | Chapter | Disposition | Evidence / open item |
|---|---|---|---|
| V1 | Encoding and Sanitization | **Partial** | KB Markdown renderer (Stage 39.1) escapes raw HTML and allowlists URL schemes. General app-wide contextual encoding/CSP-without-unsafe-inline is **open** — 47.8.2/49.4.3. |
| V2 | Validation and Business Logic | **Partial** | `ValidateDocument` (`engines/doctype.go`) is the one shared structural-validation choke point per this project's First Principle. Business-logic abuse detection (threshold splitting, rapid void/refund) is **open** — 49.5.4. |
| V3 | Web Frontend Security | **Partial** | No frontend framework/bundler (vanilla JS/CSS/HTML per this project's First Principle) removes an entire class of frontend supply-chain risk by construction. A CSP exists (Stage 31.1) but strict nonce/hash CSP without `unsafe-inline` is **open** — 47.8.2/49.4.3. |
| V4 | API and Web Service | **Built and tested** | `internal/server/route_capabilities.go` + `route_capabilities_test.go` + `authorization_contract_test.go` (cited in [secure-development-lifecycle.md](secure-development-lifecycle.md) §49.9.5) classify and test every route. |
| V5 | File Handling | **Open** | Upload allowlist/quota/archive-bomb defense is 49.8.1, not yet built. `BulkImportCSV` (existing pattern, per this project's First Principle) is the reuse point once it lands. |
| V6 | Authentication | **Built** | `engines/security_baseline.go` (SB-* startup gate), MFA enrollment/recovery (documented in the `security-approvals` KB handbook, Stage 39.13.11), `engines/password_reset.go`/password policy. Login-abuse defense (spray/stuffing detection) is **open** — 49.2.5. |
| V7 | Session Management | **Built** | `JWT_SECRET`/`JWT_SECRET_<n>` zero-downtime rotation keyring (`engines/secret_keyring.go`, generalized from Stage 29.8), `engines.ResolveLiveUserState` cuts off sessions in flight on suspend/deprovision (cited in [README.md](README.md)). A formal session-topology ADR (cookie vs. bearer, idle/absolute lifetime) is **open** — 49.4.1. |
| V8 | Authorization | **Built, not yet closed end-to-end** | Route-capability classification (V4 above) plus the maker-checker approval engine and `FilterFieldsForRole`/`RejectRestrictedFieldWrites` (Stage 39.13.11). "One authorization contract" spanning object/field/action/tenant/scope/workflow-state everywhere is the larger **open** item — 49.3.1. |
| V9 | Self-contained Tokens | **Built** | Same `engines/secret_keyring.go` keyring as V7 — JWT signing key rotation, every configured key (plus legacy bare `NAME`) still decrypts what it wrote. |
| V10 | OAuth and OIDC | **Deliberately not built** | Stage 38.2's opaque API-key branch is built; OAuth2 client-credentials (38.2d/49.2.7) is explicitly deferred until a real integration needs standards-based token exchange — not fabricated to look done. |
| V11 | Cryptography | **Built** | `engines/secret_keyring.go` (AES-256-GCM, connector credentials + backups), bcrypt for password hashes (cited in [README.md](README.md)'s tenant-lifecycle section). |
| V12 | Secure Communication | **Built (at the deployment layer)** | Caddy terminates TLS with on-demand per-tenant issuance (Stage 44); `TRUST_PROXY`/`TRUSTED_PROXY_CIDRS` now matches Caddy's `trusted_proxies` exactly as of the SB-012 fix applied 2026-09-13 (this document's own sibling fix). |
| V13 | Configuration | **Built** | `engines/security_baseline.go`'s SB-* startup validator is exactly this chapter's subject — refuses a production boot on a known-insecure baseline, which is precisely how SB-012 was caught. |
| V14 | Data Protection | **Built** | `engines/data_classification.go` (Stage 49.6.1) + `engines/sensitive_fields.go` (47.1.3) classification registry; `engines/privacy_rights.go` (49.6.8) for the Customer access/export/erasure lifecycle — all cited in [README.md](README.md). |
| V15 | Secure Coding and Architecture | **Built** | This repository's own `CLAUDE.md` First/Second/Third principles, [threat_model.md](threat_model.md), [secure-development-lifecycle.md](secure-development-lifecycle.md), `.github/CODEOWNERS`. |
| V16 | Security Logging and Error Handling | **Built** | `engines/audit_evidence.go` (signed events + checkpoints, Stage 47.7) and `engines/audit_archive.go` (47.7.6); `TestBaselineFindingsNeverEchoConfiguredValues` (cited in [README.md](README.md)) for the "never leak the value" rule this chapter asks for. |
| V17 | WebRTC | **Not applicable** | This system has no real-time media/peer-connection feature anywhere in its architecture. Owner: security-owner; revisit only if a WebRTC-based feature is ever proposed. |

## 3. CWE Top 25 (2025) disposition

| Rank | CWE | Weakness | Disposition |
|---|---|---|---|
| 5, 7, 8, 11, 13, 14, 16 | 787, 416, 125, 120, 476, 121, 122 | Out-of-bounds write/read, use-after-free, classic/stack/heap buffer overflow, NULL pointer dereference | **Structurally low-risk by language choice.** This codebase is Go throughout — no manual memory management, no raw pointer arithmetic in application code. Not claimed as "impossible" (Go's runtime and any cgo/unsafe use would need their own review — this codebase uses neither in the production binary, confirmed by this project's own "no mandatory runtime dependency" discipline in [secure-development-lifecycle.md](secure-development-lifecycle.md)), but these 7 of the Top 25 are not a meaningful attack surface here the way they would be in C/C++. |
| 1 | 79 | Cross-Site Scripting | **Partial** — see ASVS V1/V3 above. KB renderer closed; general app-wide closure is 47.8/49.4.3, open. |
| 2 | 89 | SQL Injection | **Convention-level defense, not yet audited as a dedicated sweep.** This codebase's established convention is parameterized queries via Go's `database/sql`; a systematic grep-and-review confirming zero string-concatenated SQL exists nowhere is real follow-up work, not claimed as done here. |
| 3 | 352 | CSRF | **Needs the session-topology decision first.** If session auth is bearer-token (`Authorization` header, no ambient browser cookie), CSRF's classic exploit path largely does not apply — this needs confirming against 49.4.1's still-open session-topology ADR before this row can honestly be closed either way. |
| 4, 17 | 862, 863 | Missing / Incorrect Authorization | **Built, strong existing coverage.** `TestNoUnauthenticatedRouteOutsideTheReviewedSet` and the route-capability classification (ASVS V4/V8 above) directly target this. Full object/field/action negative-matrix testing is 49.3.4, open. |
| 6 | 22 | Path Traversal | **Partial.** `TestStaticFileServerNeverListsADirectory` (cited in [README.md](README.md)) covers directory listing; upload-path traversal defense is 49.8.1, open. |
| 9, 23 | 78, 77 | OS / Command Injection | **Not audited this session.** No `os/exec` call sites were identified in this session's review, but a dedicated grep-based sweep confirming that is a cheap, discrete follow-up, not claimed as done here. |
| 10 | 94 | Code Injection | **Low risk by construction.** Go is compiled, with no `eval`-equivalent dynamic code execution path in this codebase's architecture. |
| 12 | 434 | Unrestricted File Upload | **Open** — 49.8.1. |
| 15 | 502 | Deserialization of Untrusted Data | **Low residual risk.** Go's `encoding/json` has no known gadget-chain deserialization class comparable to Java/PHP object deserialization. The residual risk is unbounded body/array/JSON-depth size, tracked as 49.4.4, open. |
| 18, 19 | 20, 284 | Improper Input Validation / Access Control | **Partial** — see ASVS V2/V8 above; full closure is 49.3/49.4.6, open. |
| 20 | 200 | Exposure of Sensitive Information | **Built.** `engines/sensitive_fields.go`, `engines/data_classification.go`, `engines/telemetry_redaction.go` (all cited in [README.md](README.md)) are exactly this. |
| 21 | 306 | Missing Authentication for Critical Function | **Built.** `TestNoUnauthenticatedRouteOutsideTheReviewedSet` (cited in [README.md](README.md)) is this CWE's direct regression test. |
| 22 | 918 | SSRF | **Open** — 49.8.4 (outbound webhook DNS-rebinding/SSRF defense), not yet built. |
| 24 | 639 | Authorization Bypass Through User-Controlled Key | **Open** — 49.3.4 (IDOR/BOLA negative-test matrix), not yet built. |
| 25 | 770 | Allocation of Resources Without Limits | **Open** — the whole 47.9–47.11/49.13 admission-control/performance-budget program, not yet built. |

## 4. What this document is not

It is not an ASVS Level 1/2/3 conformance claim, not a penetration-test substitute, and not a certification. It is the version-pinned baseline
[49.16.2](../micro_checklist.md)'s control crosswalk cites, and the thing
[risk_register.md](risk_register.md) rows should reference by chapter/CWE ID
going forward instead of inventing ad hoc framework language per row.
