-- ---------------------------------------------------------------------------
-- Stage 75.1.4-75.1.6: off-box backups.
--
-- deploy/backup.sh now copies each encrypted dump (and its sha256 sidecar) to
-- an S3-compatible bucket (Cloudflare R2) when OFFSITE_S3_* is set, and keeps
-- an upload-only copy of media_store (scope 'media'). Each run records where
-- its copy went and whether it got there, and the Go server's backup monitor
-- (engines/alerting.go CheckBackupFreshness) alerts BACKUP_OFFSITE_STALE when
-- the newest off-box success is older than its limit.
--
--   offsite_key      the object key the dump was uploaded under ("" / NULL
--                    when no bucket is configured)
--   offsite_outcome  'Success', 'Failed', or NULL when no bucket is configured
--
-- Additive and idempotent; public schema only (backup_runs is a platform fact).
-- ---------------------------------------------------------------------------
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS offsite_key TEXT;
ALTER TABLE public.backup_runs ADD COLUMN IF NOT EXISTS offsite_outcome VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_backup_runs_offsite
    ON public.backup_runs (scope, offsite_outcome, finished_at DESC);
