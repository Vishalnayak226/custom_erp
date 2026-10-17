-- Stage 65.6 (user, 2026-10-17): a free trial per client, set and ended from
-- the Control Panel.
--
-- trial_ends_at NULL means no trial (a paying client, and every client that
-- existed before this file). Set, the client works normally until that moment;
-- from then on engines.TenantIsOperational refuses it exactly as it refuses a
-- paused client - nothing is deleted, and extending or ending the trial gives
-- access back at once. Additive column on the existing registry, no new table.
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS trial_ends_at TIMESTAMPTZ;
