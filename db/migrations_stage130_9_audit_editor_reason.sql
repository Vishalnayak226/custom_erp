-- Stage 130.9 (72.D e, user decision 2026-10-22): the audit trail records who
-- edited a record, and why. documents gains updated_by (the editor of the
-- last change) and edit_reason (the reason given with it, if any); the
-- generic record save fills both. tenant_default's field-change trigger now
-- names the editor rather than the record's creator, and carries the reason.
-- Every client also gets one signed audit event per edit from the server
-- (handlers_core_doc_engine.go). Additive.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL;
    EXECUTE format('ALTER TABLE %I.documents ADD COLUMN IF NOT EXISTS updated_by TEXT', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.documents ADD COLUMN IF NOT EXISTS edit_reason TEXT', schema_rec.schema_name);
  END LOOP;
END
$mig$;

CREATE OR REPLACE FUNCTION tenant_default.log_document_changes()
RETURNS TRIGGER AS $$
DECLARE
    key TEXT;
    old_val TEXT;
    new_val TEXT;
    user_id_val VARCHAR(100);
    reason_txt TEXT;
BEGIN
    user_id_val := COALESCE(NULLIF(NEW.updated_by, ''), NEW.created_by, 'system');
    reason_txt := CASE WHEN COALESCE(NEW.edit_reason, '') <> '' THEN ' (reason: ' || NEW.edit_reason || ')' ELSE '' END;

    FOR key IN SELECT jsonb_object_keys(OLD.data)
    LOOP
        old_val := OLD.data ->> key;
        new_val := NEW.data ->> key;

        IF old_val IS DISTINCT FROM new_val THEN
            INSERT INTO tenant_default.audit_logs (user_id, action, status, details)
            VALUES (
                user_id_val,
                'UPDATE_' || NEW.doctype,
                'SUCCESS',
                'Field "' || key || '" modified from "' || old_val || '" to "' || new_val || '" for Document ID: ' || NEW.id || reason_txt
            );
        END IF;
    END LOOP;

    FOR key IN SELECT jsonb_object_keys(NEW.data)
    LOOP
        IF NOT OLD.data ? key THEN
            new_val := NEW.data ->> key;
            INSERT INTO tenant_default.audit_logs (user_id, action, status, details)
            VALUES (
                user_id_val,
                'UPDATE_' || NEW.doctype,
                'SUCCESS',
                'Field "' || key || '" added with value "' || new_val || '" for Document ID: ' || NEW.id || reason_txt
            );
        END IF;
    END LOOP;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
