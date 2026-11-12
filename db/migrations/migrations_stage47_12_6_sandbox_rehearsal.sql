-- Stage 47.12.6: the synthetic, resettable sandbox, finished.
--
-- Additive only; nothing here touches a client's rows.
--
-- 1. public.sandbox_effect_log: what a sandbox client's outside calls WOULD
--    have done. Every outside call a sandbox client attempts is stopped at the
--    one outside-calls gate (engines/outside_calls.go) and one row is written
--    here instead - the kind of call, the code place that asked, and a short
--    detail. A rehearsal reads from it; so does the check that asks whether a
--    connector was rehearsed before a client switches it on for real.
--    Cascades away with its tenant.
-- 2. A sandbox is made a sandbox when it is created, never later: once a
--    tenant is registered as a client (more than an hour ago) is_sandbox can
--    not be switched on. Resetting a sandbox empties it; this keeps the flag
--    from ever pointing that at a real client. Switching it off, and every
--    insert, are unchanged.
CREATE TABLE IF NOT EXISTS public.sandbox_effect_log (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   VARCHAR(100) NOT NULL REFERENCES public.tenants(tenant_id) ON DELETE CASCADE,
    kind        VARCHAR(30)  NOT NULL,
    effect      VARCHAR(120) NOT NULL,
    detail      TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sandbox_effect_log_tenant ON public.sandbox_effect_log (tenant_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_sandbox_effect_log_kind ON public.sandbox_effect_log (kind, created_at DESC);

CREATE OR REPLACE FUNCTION public.tenants_sandbox_flag_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.is_sandbox AND NOT OLD.is_sandbox
       AND (OLD.provisioned_at IS NULL OR OLD.provisioned_at < CURRENT_TIMESTAMP - INTERVAL '1 hour') THEN
        RAISE EXCEPTION 'tenant % is already a client: only a tenant created as a sandbox can be a sandbox', OLD.tenant_id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tenants_sandbox_flag_guard ON public.tenants;
CREATE TRIGGER tenants_sandbox_flag_guard
    BEFORE UPDATE OF is_sandbox ON public.tenants
    FOR EACH ROW EXECUTE FUNCTION public.tenants_sandbox_flag_guard();
