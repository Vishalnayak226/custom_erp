-- Stage 49.2.1: identity lifecycle - when each account last signed in.
--
-- last_sign_in_at is stamped when a full session is issued (password, or the
-- authenticator step after it, or a PIN sign-in on a store phone). The
-- dormant-account sweep (engines/identity_lifecycle.go) reads it, and only
-- when a client turns security.dormant_account_days on - off by default.
-- reactivated_at is stamped when an Inactive account is made Active again,
-- so a re-enabled account gets a fresh dormant window instead of being
-- disabled again on the next sweep.
--
-- Additive and backward-compatible: nullable, no default, no backfill; every
-- existing row keeps NULL. Same DO-block catch-up shape as BLD-053 for client
-- schemas provisioned earlier.

ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS last_sign_in_at TIMESTAMPTZ;
ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS reactivated_at TIMESTAMPTZ;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.users ADD COLUMN IF NOT EXISTS last_sign_in_at TIMESTAMPTZ', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.users ADD COLUMN IF NOT EXISTS reactivated_at TIMESTAMPTZ', schema_rec.schema_name);
  END LOOP;
END $$;
