---
doc_id: SEC-RLS-REVIEW-001
title: Row-level security review
type: reference
status: draft
owner: security-owner
approvers: [security-owner, tenant-owner]
audience: [security, engineering]
applies_to: tenant isolation in the PostgreSQL topology (schema per client)
authority: proposed-policy
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-05-15
supersedes: none
superseded_by: none
verification_scope: read from the code on 2026-11-15; no production database was inspected
---

# Row-level security review (Stage 49.3.3)

Question: would PostgreSQL row-level security (RLS) add a boundary that is worth its cost on top of how clients are separated today?

## How clients are separated today

- **One schema per client.** Business tables live in the client's own schema; the shared ones (`public.tenants` and similar) are always schema-qualified.
- **One place picks the schema.** `db.GetTenantSchema` (`db/db.go`) checks the name against `^[a-zA-Z_][a-zA-Z0-9_]*$` and fails closed for an unknown client. Every caller goes through it.
- **A fixed search path per transaction.** `db.SetSearchPath` runs `SET LOCAL search_path` for the client's schema and `public`, so it ends with the transaction and cannot leak to the next request on a pooled connection.
- **Live-state check.** A purged, suspended or deprovisioned client is refused in `apiMiddleware` for session and extension tokens alike (49.1.5, 49.3.3).

## What RLS would add here

RLS filters rows inside one table by a policy. This design has no table shared by two clients, so there is no row to filter between them. RLS would only help if a query ran against the wrong schema, and that is the case the schema check and the fixed search path already close.

| Option | Gain | Cost | Recommendation |
|---|---|---|---|
| RLS on every client table | a second guard if a schema name were ever wrong | a policy per table per client; schema sync (`db/tenant_schema_sync.go`) must copy it; a session variable on every connection; a slower plan on the busiest tables | **No.** The cost lands on every query for a case already closed. |
| RLS on the shared `public` tables only | stops a client-context query reading another client's registry row | few tables, small cost | **Later**, if a shared table starts holding client rows. Today `public.tenants` is read by platform code only. |
| Separate database roles (below) | a stolen application connection cannot change the schema or read backups | deployment work, no code change | **Yes.** This is the boundary that adds protection. |

## Least-privilege database roles (the owner's deployment step)

Three roles, none a superuser:

1. **Application role.** `SELECT/INSERT/UPDATE/DELETE` on client schemas; no `CREATE`, no `DROP`, no role or extension rights.
2. **Migration role.** Used only by the server's `-migrate` run and `tenantctl`; may create and alter objects.
3. **Backup role.** Read-only on all schemas; cannot write.

The server today uses one `DATABASE_URL`. Splitting it into an application URL and a migration URL is a small code change and a host change together, so it is listed for the owner to schedule with the next deploy. Until then this review treats the single role as a known gap in the risk register.

## Decision needed

Approve "no RLS on client tables, roles on the host". Recommended answer: approve.
