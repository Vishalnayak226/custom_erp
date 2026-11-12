-- Stage 47.12.4 - the guided setup's evidence trail.
--
-- One row each time a setup step's state changes (Draft / Invalid / Awaiting
-- Approval / Ready / Waived), and one for each note somebody adds (with an
-- optional attachment reference). Append-only: a trigger refuses UPDATE and
-- DELETE, so the history can be read as it happened. A data reset keeps it
-- (it is evidence); a sanctioned purge may delete.
--
-- Additive: one new table and its guard function. Same DO-block catch-up
-- shape as Stage 49.3.6 for schemas provisioned earlier; new clients get the
-- trigger from the schema sync (db/tenant_schema_sync.go guardTriggers).
CREATE TABLE IF NOT EXISTS tenant_default.setup_step_history (
    id BIGSERIAL PRIMARY KEY,
    step_id VARCHAR(100) NOT NULL,
    kind VARCHAR(10) NOT NULL DEFAULT 'State',
    from_state VARCHAR(30) NOT NULL DEFAULT '',
    to_state VARCHAR(30) NOT NULL DEFAULT '',
    severity VARCHAR(20) NOT NULL DEFAULT '',
    validator_version VARCHAR(50) NOT NULL DEFAULT '',
    failing JSONB NOT NULL DEFAULT '[]',
    note TEXT NOT NULL DEFAULT '',
    attachment TEXT NOT NULL DEFAULT '',
    changed_by VARCHAR(100) NOT NULL DEFAULT 'system',
    changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_setup_step_history_step
  ON tenant_default.setup_step_history (step_id, id DESC);

CREATE OR REPLACE FUNCTION public.setup_step_history_guard() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
  IF TG_OP = 'DELETE' AND COALESCE(current_setting('erp.sanctioned_write', true), '') = 'purge' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'setup_step_history is append-only: a history row cannot be %', CASE TG_OP WHEN 'DELETE' THEN 'deleted' ELSE 'changed' END
    USING ERRCODE = 'check_violation';
END
$fn$;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF schema_rec.schema_name <> 'tenant_default' THEN
      EXECUTE format('CREATE TABLE IF NOT EXISTS %I.setup_step_history (LIKE tenant_default.setup_step_history INCLUDING ALL)', schema_rec.schema_name);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = schema_rec.schema_name AND c.relname = 'setup_step_history' AND t.tgname = 'setup_step_history_guard') THEN
      EXECUTE format($sql$
        CREATE TRIGGER setup_step_history_guard BEFORE UPDATE OR DELETE ON %I.setup_step_history
        FOR EACH ROW EXECUTE FUNCTION public.setup_step_history_guard()
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
