-- ---------------------------------------------------------------------------
-- Stage 47.16.4b (G2 of docs/security/gst-companies-act-controls.md): ledger
-- rows the database will not change.
--
-- A trigger on every client's gl_postings refuses:
--   * DELETE, unless the transaction names itself a sanctioned purge
--     (SET LOCAL erp.sanctioned_write = 'purge': a client's data reset,
--     engines/tenant_reset.go). A sandbox reset TRUNCATEs, which a row
--     trigger does not see, and a purged client's schema is dropped.
--   * UPDATE of any column except the bank-reconciliation match mark
--     (matched_statement_line_id). A master renumber (engines/master_recode.go,
--     erp.sanctioned_write = 'recode') may also carry a renumbered code onto the
--     reference columns: document_id, cost_center, department, entity, project.
--     Money, accounts, dates, currency and keys never change: a posting is
--     corrected by a reversing entry.
--
-- Deliberately not named "append_only": a master renumber skips tables with
-- such a trigger entirely (recodeTargets), and the ledger's references must
-- follow it. New clients get the trigger from the client schema sync
-- (db/tenant_schema_sync.go guardTriggers), as LIKE ... INCLUDING ALL copies
-- none.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION public.gl_postings_guard() RETURNS trigger
LANGUAGE plpgsql AS $fn$
DECLARE
  sanction TEXT := COALESCE(current_setting('erp.sanctioned_write', true), '');
  allowed  TEXT[] := ARRAY['matched_statement_line_id'];
  changed  TEXT[];
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF sanction = 'purge' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'gl_postings is append-only: posting % cannot be deleted - post a reversing entry', OLD.posting_id
      USING ERRCODE = 'check_violation';
  END IF;
  IF sanction = 'recode' THEN
    allowed := allowed || ARRAY['document_id', 'cost_center', 'department', 'entity', 'project'];
  END IF;
  SELECT array_agg(n.key ORDER BY n.key) INTO changed
    FROM jsonb_each(to_jsonb(NEW)) n JOIN jsonb_each(to_jsonb(OLD)) o ON o.key = n.key
   WHERE n.value IS DISTINCT FROM o.value AND NOT (n.key = ANY(allowed));
  IF changed IS NULL THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'gl_postings is append-only: % of posting % cannot change - post a reversing entry', array_to_string(changed, ', '), OLD.posting_id
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
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_postings');
    IF NOT EXISTS (SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = schema_rec.schema_name AND c.relname = 'gl_postings' AND t.tgname = 'gl_postings_guard') THEN
      EXECUTE format($sql$
        CREATE TRIGGER gl_postings_guard BEFORE UPDATE OR DELETE ON %I.gl_postings
        FOR EACH ROW EXECUTE FUNCTION public.gl_postings_guard()
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
