-- Stage 28.2: per-user theme preference (light / dark / system), following the
-- exact convention of idle_timeout_minutes (migrations_stage21_user_profile.sql)
-- - a per-user column on the users table, surfaced via GET /api/v1/me and set
-- via PUT /api/v1/me. 'system' means "follow the OS" (the CSS media query).
--
-- Additive and backward-compatible (ADD COLUMN IF NOT EXISTS with a DEFAULT);
-- new tenants inherit it through the users-table clone in ProvisionTenantSchema.

ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS theme_preference TEXT NOT NULL DEFAULT 'system';
