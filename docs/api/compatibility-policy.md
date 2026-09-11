---
doc_id: DOC-API-COMPAT
title: API and extension compatibility policy
type: normative
status: draft
owner: api-owner
approvers: [engineering-owner, product-owner]
audience: [integrators, extension-authors, engineering]
applies_to: explicitly supported public API and extension versions
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-07
review_by: 2026-10-07
supersedes: none
superseded_by: none
---

# API and extension compatibility policy

Treat public API, webhook/event and extension hook contracts as versioned boundaries.
Document authentication/scope, input/output, required fields, idempotency, retry/order,
signature verification, errors/limits and support window. Internal routes/types do not
become public contracts by appearing in generated code or a hook implementation.

Before a change, classify its effect on existing clients and persisted jobs/events. Adding
an optional field is not automatically harmless if parsers reject unknown fields; changing
types, defaults, authorization, enum interpretation or delivery order needs contract tests
and communication. Version breaking changes and provide migration/recovery guidance.

Deprecation needs named owner, affected versions/consumers, approved transition window,
replacement, telemetry or direct evidence of usage, client guidance and retirement record.
No silent removal is authorized by this draft. Product/API owners choose the support window
for each approved release rather than inheriting an invented universal duration.

Extensions must retain explicit tenant/actor/scope and use approved transaction/outbox
boundaries. Test supported host/hook versions, refusal paths, timeout/retry, idempotency and
uninstall/upgrade effects. See the [SDK](../../extension-sdk/README.md),
[API source routes](../../internal/server/routes_public_api_v1.go) and
[public API runtime tests](../../engines/public_api_runtime_test.go).
