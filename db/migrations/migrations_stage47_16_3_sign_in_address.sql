-- Stage 47.16.3: a sign-in row carries the client address.
--
-- users.last_sign_in_at (Stage 49.2.1) says when, never from where. This adds
-- the address the server resolved for the request (the trusted-proxy rule, never
-- a raw header). It is personal data: registered in engines/personal_data_register.go
-- and cleared 180 days after that sign-in by the hourly retention sweep.
--
-- Additive: nullable, no default, no backfill; every existing row keeps NULL.

ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS last_sign_in_ip VARCHAR(45);

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.users ADD COLUMN IF NOT EXISTS last_sign_in_ip VARCHAR(45)', schema_rec.schema_name);
  END LOOP;
END $$;
