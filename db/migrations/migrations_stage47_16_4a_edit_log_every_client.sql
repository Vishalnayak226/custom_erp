-- Stage 47.16.4a (G1, approved by the user 2026-10-29): the edit log on every
-- client. Companies (Accounts) Rules 2014, Rule 3(1): every change to a record
-- is logged with its old and new values, the editor and the date.
--
-- Until now only tenant_default had it: tenant_default.log_document_changes()
-- wrote straight into tenant_default.audit_logs, so it could not be attached to
-- any other schema, and a client made by ProvisionTenantSchema got only the
-- server's signed EDIT_<doctype> event, which names the changed fields but not
-- their values.
--
-- public.erp_log_document_edit() writes into the audit_logs of the schema whose
-- documents row changed (TG_TABLE_SCHEMA), so one function serves every client.
-- One row per save, not one per field (the 49.18 weight budget):
--   action      UPDATE_<doctype>
--   user_id     the editor (updated_by), else the record's creator, else 'system'
--   entity_type / entity_id   the record, so the indexed entity lookup finds it
--   details     Fields changed: {"<field>": {"new": <json>, "old": <json>}, ...}
--               for Document ID: <id>[ (reason: <edit_reason>)]
-- Only fields inside `data` count, as before; a save that changes none of them
-- (a status-column or soft-delete update) writes nothing, and the WHEN clause
-- skips the call entirely.
--
-- The row is unsigned and dated by the column default on the session clock, as
-- the trigger rows before it were: VerifyAuditEvidence counts it as unsigned
-- (never as tampered), the next checkpoint seals it, and the forensic pack
-- windows unsigned rows on the session clock.
--
-- Attached here to tenant_default and every client that exists now.
-- db.SyncTenantSchemaTx attaches it to a client made later (ProvisionTenantSchema
-- runs it) and to any client that has lost it, after every -migrate.
--
-- Additive: tenant_default.log_document_changes() stays defined (unattached);
-- rows already written keep their per-field shape and are still read.
-- updated_by / edit_reason / entity_type / entity_id are added where missing
-- because a fresh install runs this file before the Stage 130.9 migration that
-- first adds the two documents columns, and the function reads them.

CREATE OR REPLACE FUNCTION public.erp_log_document_edit() RETURNS trigger
LANGUAGE plpgsql AS $fn$
DECLARE
    changes jsonb;
    reason  text;
BEGIN
    IF jsonb_typeof(OLD.data) = 'object' AND jsonb_typeof(NEW.data) = 'object' THEN
        SELECT jsonb_object_agg(k, jsonb_build_object('old', OLD.data -> k, 'new', NEW.data -> k))
          INTO changes
          FROM (SELECT jsonb_object_keys(OLD.data) UNION SELECT jsonb_object_keys(NEW.data)) AS keys(k)
         WHERE (OLD.data -> k) IS DISTINCT FROM (NEW.data -> k);
    ELSIF OLD.data IS DISTINCT FROM NEW.data THEN
        changes := jsonb_build_object('data', jsonb_build_object('old', OLD.data, 'new', NEW.data));
    END IF;
    IF changes IS NULL THEN
        RETURN NULL;
    END IF;
    reason := NULLIF(btrim(COALESCE(NEW.edit_reason, '')), '');
    EXECUTE format('INSERT INTO %I.audit_logs (user_id, action, status, details, entity_type, entity_id) VALUES ($1, $2, $3, $4, $5, $6)', TG_TABLE_SCHEMA)
      USING COALESCE(NULLIF(NEW.updated_by, ''), NEW.created_by, 'system'),
            'UPDATE_' || NEW.doctype,
            'SUCCESS',
            'Fields changed: ' || changes::text || ' for Document ID: ' || NEW.id
                || COALESCE(' (reason: ' || reason || ')', ''),
            NEW.doctype,
            NEW.id;
    RETURN NULL;
END
$fn$;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL
               OR to_regclass(format('%I.audit_logs', schema_rec.schema_name)) IS NULL;
    EXECUTE format('ALTER TABLE %I.documents ADD COLUMN IF NOT EXISTS updated_by TEXT, ADD COLUMN IF NOT EXISTS edit_reason TEXT', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.audit_logs ADD COLUMN IF NOT EXISTS entity_type VARCHAR(100), ADD COLUMN IF NOT EXISTS entity_id VARCHAR(200)', schema_rec.schema_name);
    -- tenant_default's trigger of the same name pointed at its per-field
    -- function; every other schema had none.
    EXECUTE format('DROP TRIGGER IF EXISTS trg_log_document_changes ON %I.documents', schema_rec.schema_name);
    EXECUTE format('CREATE TRIGGER trg_log_document_changes AFTER UPDATE ON %I.documents FOR EACH ROW '
                   || 'WHEN (OLD.data IS DISTINCT FROM NEW.data) EXECUTE FUNCTION public.erp_log_document_edit()', schema_rec.schema_name);
  END LOOP;
END
$mig$;
