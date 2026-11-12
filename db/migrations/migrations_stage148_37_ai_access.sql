-- ---------------------------------------------------------------------------
-- Stage 148.37: read-only access for AI assistants (module ai_access, off by
-- default, decision D9).
--
--   ai_access_clients  binds one OAuth client (an api_credentials row) to the
--                      one user it acts as. The user, not the client, decides
--                      what every call may see: role, store scope and field
--                      rules are re-read on each call. The credential's own
--                      columns are untouched, so the shared credential scan,
--                      rotate and revoke code is unchanged.
--
-- The module is registered switched OFF in the catalog; a tenant with no
-- entitlement row reads as off, so no client sees a change until an admin
-- turns it on. No per-client row is written. Additive and idempotent, replayed
-- into every tenant schema.
-- ---------------------------------------------------------------------------
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('ai_access', 'AI assistant access', 'Read-only stock, order status and reports for an AI assistant acting as one named user; it can only leave a Draft sales order.', FALSE, FALSE)
ON CONFLICT (module_key) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'api_credentials');

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.ai_access_clients (
        credential_id UUID PRIMARY KEY,
        acting_user   VARCHAR(255) NOT NULL,
        created_by    VARCHAR(255) NOT NULL DEFAULT '',
        created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS ai_access_clients_user_idx ON %I.ai_access_clients (acting_user)
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
