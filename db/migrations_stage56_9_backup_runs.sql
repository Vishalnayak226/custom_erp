-- ---------------------------------------------------------------------------
-- Stage 56.9: backup monitoring reads a database record, not a folder.
--
-- engines/alerting.go's CheckBackupFreshness used to stat the newest
-- custom_erp_*.dump.enc in BACKUP_DIR and treat a missing directory as "not a
-- machine that takes backups" - silence. On a PaaS the service checking the
-- folder and the job writing it do not share a disk, so monitoring switched
-- itself off. deploy/backup.sh now writes one row per run here, and the
-- monitor reads the newest successful whole-database row instead.
--
-- public schema only: a backup is of the whole database (or one tenant
-- schema, recorded with scope 'tenant'), so it is a platform fact, not a
-- per-tenant one, and no tenant_* schema needs a copy.
--
-- Additive and idempotent: CREATE TABLE IF NOT EXISTS for a fresh database,
-- ADD COLUMN IF NOT EXISTS so a partially created table converges.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.backup_runs (
    id BIGSERIAL PRIMARY KEY,
    finished_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 'database' = whole-database nightly; 'tenant' = an on-demand tenant_* export
-- (26.1.6), recorded for the trail but never counted as the nightly.
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS scope VARCHAR(20) NOT NULL DEFAULT 'database';
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS tenant_schema VARCHAR(100);
-- 'Success' or 'Failed'. Only Success satisfies the freshness check.
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS outcome VARCHAR(20) NOT NULL DEFAULT 'Success';
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS file_name VARCHAR(255);
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS size_bytes BIGINT;
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS duration_ms BIGINT;
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS sha256 VARCHAR(64);
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS detail TEXT;
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS host VARCHAR(255);
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_backup_runs_latest
    ON public.backup_runs (scope, outcome, finished_at DESC);
