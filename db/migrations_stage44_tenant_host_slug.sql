-- Stage 44.1: per-tenant hostnames (<slug>.<TENANT_BASE_DOMAIN>).
--
-- Until now nothing about a request's Host header selected a tenant: tenant
-- came from X-Tenant-ID / ?tenant_id= before login and from the bearer token's
-- own claim after it (internal/server/middleware.go). One hostname therefore
-- served every tenant. This column is what lets acme.example.com resolve to
-- the "acme" tenant without changing that token-is-authoritative rule.
--
-- Additive and reversible by neglect: a tenant with a NULL host_slug simply
-- has no hostname of its own and keeps working exactly as before.
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS host_slug VARCHAR(63);

-- Backfill from tenant_id, but only where tenant_id is already a legal DNS
-- label. tenant_id is a free-text VARCHAR(100) with no character restrictions,
-- so ids containing '_' or '.' (both illegal in a hostname label, and both
-- present in this repo's own fixtures) must not be backfilled into something
-- that can never be served. Those tenants get NULL and an operator names them
-- explicitly.
UPDATE public.tenants
   SET host_slug = LOWER(tenant_id)
 WHERE host_slug IS NULL
   AND LOWER(tenant_id) ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?$'
   AND LENGTH(tenant_id) <= 63;

-- Unique on LOWER(...) rather than the raw column: hostnames are
-- case-insensitive, so "Acme" and "acme" must not both be claimable. Partial,
-- so any number of tenants may keep a NULL slug.
CREATE UNIQUE INDEX IF NOT EXISTS tenants_host_slug_lower_key
    ON public.tenants (LOWER(host_slug))
 WHERE host_slug IS NOT NULL;
