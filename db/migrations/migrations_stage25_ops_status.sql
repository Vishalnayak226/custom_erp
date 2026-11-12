-- Stage 25.8: genuinely new API surface (outside the 187-code recount,
-- Production Mandatory tier) - DEPLOY-0204..0210 / DR-0211..0214 need
-- somewhere to attach to. public.deployments (migrations_stage14c_pipeline.sql)
-- already covers deploy history, populated by promote.ps1's Add-Deployment;
-- ops_run_log is its backup/restore/restore-drill sibling, since no
-- equivalent existed for those manage.ps1 actions. Both are control-plane
-- tables (live in dev's own custom_erp database regardless of which
-- environment the row is about), same reasoning promote.ps1's own header
-- comment already gives for public.deployments/public.schema_migrations.

CREATE TABLE IF NOT EXISTS public.ops_run_log (
    id BIGSERIAL PRIMARY KEY,
    run_type VARCHAR(30) NOT NULL,       -- 'backup' | 'restore' | 'restore_drill'
    environment VARCHAR(20) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL,         -- 'success' | 'failed'
    detail TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ops_run_log_type_finished ON public.ops_run_log (run_type, finished_at DESC);

-- SAAS-0193 (subscription limit exceeded): engines/saas.go already has
-- IsFeatureEnabled/module entitlements but no plan/limit concept at all.
-- Per-tenant, keyed rather than one fixed column per limit so a new limit
-- type never needs another migration - an unset key means "no limit
-- configured" (open), matching this codebase's established
-- unset-env-var-means-no-op convention (OPS_ALERT_WEBHOOK_URL etc.).
CREATE TABLE IF NOT EXISTS tenant_default.tenant_limits (
    tenant_id VARCHAR(100) NOT NULL,
    limit_key VARCHAR(50) NOT NULL,
    limit_value INT NOT NULL,
    PRIMARY KEY (tenant_id, limit_key)
);
