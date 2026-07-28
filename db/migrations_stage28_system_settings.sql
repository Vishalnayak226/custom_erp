-- Stage 28.1: system_settings - the per-tenant key/value configuration store
-- behind the module-by-module admin Settings screen (engines/settings_registry.go).
--
-- Every configurable value that used to be a hardcoded Go constant (loyalty
-- point expiry, reservation TTL, OTP validity, ...) declares its default in the
-- Go settings registry. A row here is only ever an *override* of that default,
-- so an empty table reproduces exactly the pre-Stage-28 behavior - this
-- migration changes nothing until an admin edits a value in the UI.
--
-- Follows the feature_flags convention exactly: created in tenant_default here,
-- and added to engines/saas.go's ProvisionTenantSchema clone list so every new
-- tenant gets its own copy. Additive and backward-compatible (CREATE TABLE IF
-- NOT EXISTS), per the repo's schema-change rule - no destructive rework.

CREATE TABLE IF NOT EXISTS tenant_default.system_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT
);
