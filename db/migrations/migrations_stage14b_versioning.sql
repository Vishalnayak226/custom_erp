-- Stage 14.6-14.8: Application Versioning + Per-Tenant Version Stamping
--
-- app_version/pinned_version on public.tenants are a point-in-time compat/
-- audit record of which build last touched a tenant's schema (stamped by
-- engines.ProvisionTenantSchema at provision time, surfaced via
-- GET /api/v1/admin/tenant/version) - not a live per-request runtime
-- dispatch. One running process can only ever serve one binary version
-- regardless of which tenant is asking; true per-tenant *runtime* version
-- pinning only becomes real once a tenant is split into its own physical
-- instance (Stage 14.9-14.12's dev/test/live pipeline), at which point
-- pinned_version becomes the control plane's authoritative sync target.

ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS app_version VARCHAR(20);
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS pinned_version VARCHAR(20);

-- schema_migrations is a ledger of which migration FILES have been applied
-- to *this* instance - needed by the promotion tooling (Stage 14.9-14.12)
-- to know what still needs to run when promoting dev -> test -> live.
-- Keyed by filename rather than app semver, since several migration files
-- can land within one unreleased version before a version bump - filename
-- is the actually-unambiguous unit of "has this been applied here yet".
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    migration_file VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    description TEXT
);

-- Bootstrap: record everything already known to have been applied to any
-- instance that runs this file, so the ledger starts accurate rather than
-- empty. ON CONFLICT DO NOTHING makes re-running this file harmless.
INSERT INTO public.schema_migrations (migration_file, description) VALUES
('migration.sql', 'Base schema - Stage 1 kernel through Stage 13 Master Blueprint scope'),
('migrations_phase3.sql', 'Phase 2/3 transactional metadata seed'),
('migrations_stage14a_modules.sql', 'Stage 14.1-14.5: module registry + per-tenant module entitlements'),
('migrations_stage14b_versioning.sql', 'Stage 14.6-14.8: application versioning + per-tenant version stamping')
ON CONFLICT (migration_file) DO NOTHING;
