-- BLD-053: which audit signing key signed each row and checkpoint.
--
-- A signed audit row that no loaded AUDIT_SIGNING_KEY verifies used to be
-- reported as tampered - so losing a key (a host rebuilt without its key file,
-- a rotation that dropped the old key) produced a wall of false tamper alarms
-- that teaches responders to ignore the real one. sig_key holds the signing
-- key's fingerprint (first 8 hex of its SHA-256 - names the key, reveals
-- nothing of it), so the verifier can say "signed by key <fp>, which is not
-- loaded: restore it" separately from "changed since it was signed".
--
-- Additive and backward-compatible: nullable, no default, instant on a large
-- table; older rows keep NULL and are verified exactly as before. Same
-- DO-block catch-up shape as Stage 50.15 for schemas provisioned earlier.

ALTER TABLE tenant_default.audit_logs ADD COLUMN IF NOT EXISTS sig_key VARCHAR(16);
ALTER TABLE tenant_default.audit_checkpoints ADD COLUMN IF NOT EXISTS sig_key VARCHAR(16);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.audit_logs ADD COLUMN IF NOT EXISTS sig_key VARCHAR(16)', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE IF EXISTS %I.audit_checkpoints ADD COLUMN IF NOT EXISTS sig_key VARCHAR(16)', schema_rec.schema_name);
  END LOOP;
END $$;
