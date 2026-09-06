-- ---------------------------------------------------------------------------
-- Stage 49.1.5 - secure tenant provisioning and deprovisioning.
--
-- What was actually missing. ProvisionTenantSchema already generated a
-- cryptographically random one-time admin password (engines/saas.go), but
-- nothing after that point was governed:
--
--   * the one-time password never expired and nothing recorded whether it had
--     ever been rotated, so a tenant provisioned and then forgotten kept a
--     working, hand-delivered credential forever;
--   * the registry INSERT and CREATE SCHEMA ran outside the clone/seed
--     transaction, so a failure half-way through left a registry row pointing
--     at an empty schema - a tenant that resolves, authenticates and then 500s;
--   * there was no suspend state at all: the only two states were "exists" and
--     "DROP SCHEMA CASCADE", and the latter existed only for sandboxes;
--   * nothing checked backups, retention or legal hold before a deletion, and
--     nothing proved afterwards that no row, schema, hostname or live session
--     for that tenant remained.
--
-- This file is the storage for all of that. Two objects:
--
--   1. New columns on public.tenants - the lifecycle state machine, the
--      bootstrap credential's expiry/rotation record, and the retention and
--      legal-hold gates that stand in front of a purge.
--
--   2. public.tenant_lifecycle_events - append-only evidence. Deliberately
--      NOT foreign-keyed to public.tenants: a purge deletes the registry row,
--      and the evidence that the purge happened has to outlive it. This is the
--      only durable record that a purged tenant ever existed.
--
-- bootstrap_password_hash holds a copy of the bcrypt hash written into the new
-- tenant's admin row at provisioning time. It is not a new class of secret -
-- it is the same value as <schema>.users.password_hash, and it is here for one
-- reason: bcrypt salts every hash, so "the admin's stored hash is still byte
-- for byte the one we issued" is the only cheap, offline-checkable proof that
-- the one-time credential has never been rotated. It is cleared the moment
-- rotation is observed.
-- ---------------------------------------------------------------------------

ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS lifecycle_status VARCHAR(30) NOT NULL DEFAULT 'active';
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS lifecycle_reason TEXT;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS lifecycle_changed_at TIMESTAMPTZ;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS provisioned_at TIMESTAMPTZ;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS legal_hold BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS legal_hold_reason TEXT;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS retention_until TIMESTAMPTZ;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS bootstrap_password_hash TEXT;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS bootstrap_issued_at TIMESTAMPTZ;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS bootstrap_expires_at TIMESTAMPTZ;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS bootstrap_consumed_at TIMESTAMPTZ;

-- Legal values for lifecycle_status. Enforced as a CHECK rather than an enum
-- type so the additive-migration rule still holds (an enum needs ALTER TYPE,
-- which cannot run in the same transaction as its first use on some versions):
--   active                - normal operation
--   suspended             - reversible; every session, worker and login for
--                           this tenant is refused, nothing is destroyed
--   deprovision_requested - offboarding started; same refusal as suspended,
--                           plus the retention clock is running
-- A purged tenant has no row at all, by design.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'tenants_lifecycle_status_check'
    ) THEN
        ALTER TABLE public.tenants
            ADD CONSTRAINT tenants_lifecycle_status_check
            CHECK (lifecycle_status IN ('active', 'suspended', 'deprovision_requested'));
    END IF;
END
$$;

-- Existing tenants predate the column and are, by definition, live.
UPDATE public.tenants
   SET provisioned_at = COALESCE(provisioned_at, created_at, NOW()),
       lifecycle_changed_at = COALESCE(lifecycle_changed_at, created_at, NOW())
 WHERE provisioned_at IS NULL OR lifecycle_changed_at IS NULL;

CREATE TABLE IF NOT EXISTS public.tenant_lifecycle_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id VARCHAR(100) NOT NULL,
    -- provisioned | bootstrap_issued | bootstrap_consumed | bootstrap_expired |
    -- bootstrap_rotated | suspended | resumed | deprovision_requested |
    -- legal_hold_set | legal_hold_cleared | purge_refused | purged | verified
    event VARCHAR(40) NOT NULL,
    actor VARCHAR(150) NOT NULL DEFAULT 'system',
    detail TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tenant_lifecycle_events_tenant
    ON public.tenant_lifecycle_events (tenant_id, occurred_at DESC);
