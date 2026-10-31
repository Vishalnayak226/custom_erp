-- ---------------------------------------------------------------------------
-- Stage 49.11.2: the rest of the security event schema - where a request came
-- from and under which authorisation policy it ran.
--
--   source_ip       the request's client address as the server's trusted-proxy
--                   rule reads it (clientIP, middleware.go) - personal data,
--                   registered in engines/personal_data_register.go and
--                   cleared after 180 days by the hourly retention sweep
--   device          a short summary of the user agent ("Chrome on Windows"),
--                   never the raw string - cleared with the address
--   capability      the route capability the request needed (route_capabilities.go)
--   policy_version  a fingerprint of the compiled authorisation policy (route
--                   capabilities, role allowlists, role templates)
--
-- None of the four is covered by the row's signature: the address and device
-- must be clearable at the end of their retention without breaking the
-- evidence, and the checkpoint digests cover signatures and ids only. The
-- audit archive does not carry them either.
--
-- Additive and backward-compatible: nullable, no default, instant on a large
-- table; older rows keep NULL. Every client schema, tenant_default included.
-- The partial index keeps the retention sweep to the rows that still hold an
-- address (empty until the first request writes one).
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format('ALTER TABLE IF EXISTS %I.audit_logs
      ADD COLUMN IF NOT EXISTS source_ip VARCHAR(45),
      ADD COLUMN IF NOT EXISTS device VARCHAR(48),
      ADD COLUMN IF NOT EXISTS capability VARCHAR(80),
      ADD COLUMN IF NOT EXISTS policy_version VARCHAR(16)', schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = schema_rec.schema_name AND table_name = 'audit_logs') THEN
      EXECUTE format('CREATE INDEX IF NOT EXISTS idx_audit_logs_source_retention ON %I.audit_logs (created_at)
        WHERE source_ip IS NOT NULL OR device IS NOT NULL', schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
