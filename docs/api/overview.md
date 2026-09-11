---
doc_id: DOC-API-NAV
title: API and extension reference
type: reference
status: draft
owner: api-owner
approvers: [engineering-owner]
audience: [integrators, developers]
applies_to: curated public API and extension contracts
authority: navigation
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# API and extension reference

Use [generated OpenAPI](generated/public-v1.json) for the machine contract and the
[public API guide](../specs/public_api_v1.md) for its boundary. A scope being defined does
not mean a corresponding endpoint ships. Private application routes are not public
integration contracts.

- [Authentication](../kb/reference/public-api-authentication.md) and
  [idempotency](../kb/reference/public-api-idempotency.md) explain integration use.
- [Compatibility policy](compatibility-policy.md) defines proposed change/retirement rules.
- [Extension SDK](../../extension-sdk/README.md) documents existing hooks/contracts.
- [Capability catalog](../generated/capability-catalog.md) records support and evidence limits.

The former `docs/specs/openapi_public_v1.json` path remains a compatibility projection
generated from the same registry. Both files are checked for equality; neither is
hand-maintained. Keep the old path for the governed two-supported-release transition.

## Webhooks and asynchronous outcomes

Use the public guide's implemented endpoint inventory to select a supported operation.
Verify credentials/scopes and signature/replay requirements for that exact integration;
private application routes do not inherit the public API contract. A queued job or accepted
HTTP response is not confirmation of an external business effect. Retain the command/job
reference, observe its terminal result and reconcile provider state before retrying an
uncertain delivery. See [job/outbox recovery](../operations/jobs-and-outbox.md).

## Changes and compatibility

Review [development release notes](../product/releases/0.1.0-development.md), the generated
contract diff and the [compatibility policy](compatibility-policy.md). Existing limitations,
version enforcement gaps and required contract evidence remain explicit in the capability
register and Stage 47.11; a written policy does not claim an enforcement mechanism ships.
