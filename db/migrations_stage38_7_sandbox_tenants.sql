-- Stage 38.7: self-service sandbox tenant for integrators.
--
-- A sandbox tenant is a normal tenant (provisioned through the same
-- ProvisionTenantSchema every real tenant uses - engines/saas.go), flagged
-- so external side effects can be turned off for it (Stage 38.4's webhook
-- delivery job handler checks this flag and simulates delivery instead of
-- making a real HTTP call for a sandbox tenant) and so it auto-expires.
-- Additive columns on the existing public.tenants table - no new table.
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS is_sandbox BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE public.tenants ADD COLUMN IF NOT EXISTS sandbox_expires_at TIMESTAMPTZ;
