-- Stage 47.12.1 / 2 / 3 / 7 and 49.17.3 - scope, setup map, finance start,
-- cutover pack and go-live gate share one append-only record table.
--
-- A row is one thing a person did: saved the scope answers (kind Scope, the
-- answers in data), attached evidence to a check, waived a check with a reason
-- and an expiry, put a setup-map node out of or back into scope, accepted the
-- opening reconciliation, or signed a pack off. The latest row of a kind for a
-- check is the current one; nothing is updated or deleted (a trigger refuses),
-- so the trail reads as it happened. A data reset keeps it.
--
-- Additive: one new table and its guard. Same catch-up shape as
-- Stage 47.12.4 (setup_step_history); new clients get the trigger from the
-- schema sync (db/tenant_schema_sync.go guardTriggers).
CREATE TABLE IF NOT EXISTS tenant_default.golive_records (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(20) NOT NULL,
    pack VARCHAR(20) NOT NULL DEFAULT '',
    line_id VARCHAR(100) NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    reference TEXT NOT NULL DEFAULT '',
    expires_on DATE,
    data JSONB NOT NULL DEFAULT '{}',
    created_by VARCHAR(100) NOT NULL DEFAULT 'system',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_golive_records_line
  ON tenant_default.golive_records (line_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_golive_records_kind
  ON tenant_default.golive_records (kind, id DESC);

CREATE OR REPLACE FUNCTION public.golive_records_guard() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
  IF TG_OP = 'DELETE' AND COALESCE(current_setting('erp.sanctioned_write', true), '') = 'purge' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'golive_records is append-only: a record cannot be %', CASE TG_OP WHEN 'DELETE' THEN 'deleted' ELSE 'changed' END
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
      EXECUTE format('CREATE TABLE IF NOT EXISTS %I.golive_records (LIKE tenant_default.golive_records INCLUDING ALL)', schema_rec.schema_name);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = schema_rec.schema_name AND c.relname = 'golive_records' AND t.tgname = 'golive_records_guard') THEN
      EXECUTE format($sql$
        CREATE TRIGGER golive_records_guard BEFORE UPDATE OR DELETE ON %I.golive_records
        FOR EACH ROW EXECUTE FUNCTION public.golive_records_guard()
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
