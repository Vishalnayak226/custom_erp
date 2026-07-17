-- Stage 14.9-14.12: Dev/Test/Live Pipeline - Control-Plane Registry
--
-- public.deployments is the audit trail of every promotion promote.ps1 runs:
-- what commit went to which environment, for which tenant scope, and
-- whether the build/vet/test gate passed. tenant_scope = 'ALL' is a
-- universal release; a specific tenant_id is a per-client patch - this
-- column is the literal technical answer to "deploy to one client vs
-- everyone" for actual binary-code changes (a pure config/data fix - a
-- module entitlement, a feature flag - is already naturally per-tenant via
-- Stage 14.1's tables/APIs with zero deployment machinery needed at all;
-- this table only matters for real code changes that require a rebuild).
--
-- This does NOT need to be applied to tenant_default or any tenant schema -
-- it's an instance-level control-plane table, same tier as public.tenants
-- and public.schema_migrations.
CREATE TABLE IF NOT EXISTS public.deployments (
    id SERIAL PRIMARY KEY,
    environment VARCHAR(20) NOT NULL,       -- 'dev' | 'test' | 'live' (or a live-<client> codename later)
    tenant_scope VARCHAR(100) NOT NULL DEFAULT 'ALL',
    git_commit VARCHAR(64) NOT NULL,
    app_version VARCHAR(20),
    promoted_by VARCHAR(100),
    promoted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    build_status VARCHAR(20) NOT NULL,      -- 'passed' | 'failed' | 'rolled_back'
    notes TEXT
);
CREATE INDEX IF NOT EXISTS idx_deployments_environment ON public.deployments (environment, promoted_at DESC);
