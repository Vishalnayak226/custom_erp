-- Stage 21: self-service User Profile screen (change password, view
-- role/employee link, set a personal auto-logout/idle-timeout preference).
-- Additive-only, matches db/migrations_stage14f_security.sql's pattern.
--
-- idle_timeout_minutes is a per-user client-side inactivity timer (separate
-- from the server-side JWT session TTL in engines/auth.go's tokenTTL() -
-- that one is a hard session expiry, this is "sign me out sooner than that
-- if I've walked away"). Defaults to 30 minutes for every existing user.
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS idle_timeout_minutes INT NOT NULL DEFAULT 30;
