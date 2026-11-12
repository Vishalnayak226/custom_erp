-- Stage 47.16.4c (G3, approved by the user 2026-10-29): an 8-year floor on audit
-- retention. Companies (Accounts) Rules 2014, Rule 3(5): the audit trail is kept
-- for at least eight years. 2,922 days is eight years with their two leap days.
--
-- audit_retention_policy.delete_after_days (NULL = keep for ever, the shipped
-- default) may not be set below the floor. The database refuses it, so the
-- floor holds for every writer - a future settings screen, the Data Console or
-- plain SQL. NOT VALID: a policy row written before this keeps its value as
-- history (a retention rule's past value has to stay explainable), and the
-- engine reads any such value as the floor (engines/audit_archive.go,
-- currentRetentionPolicy). New rows are checked. A client made later copies the
-- constraint with the table (LIKE ... INCLUDING ALL).
--
-- The retention sweeps' one delete path, retentionDelete, applies the same
-- floor to audit_logs, audit_checkpoints and audit_archives (an archive file is
-- reached only through its audit_archives row), so nothing is deleted inside
-- the window whatever a policy says. Additive.

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.audit_retention_policy', schema_rec.schema_name)) IS NULL;
    CONTINUE WHEN EXISTS (
      SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        JOIN pg_namespace n ON n.oid = t.relnamespace
       WHERE n.nspname = schema_rec.schema_name AND t.relname = 'audit_retention_policy'
         AND c.conname = 'audit_retention_policy_delete_floor');
    EXECUTE format('ALTER TABLE %I.audit_retention_policy ADD CONSTRAINT audit_retention_policy_delete_floor '
                   || 'CHECK (delete_after_days IS NULL OR delete_after_days >= 2922) NOT VALID', schema_rec.schema_name);
  END LOOP;
END
$mig$;
