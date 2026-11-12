-- ---------------------------------------------------------------------------
-- Stage 49.2.1 (joiner / mover / leaver approval), ask 164.
--
-- migrations_stage49_2_1c seeded the AccessChangeRequest record type and its
-- approval rule into tenant_default only. New clients copy it from there, but
-- an existing client that turns security.identity_change_approval On would
-- have every joiner and mover change refused ("no approval rule configured").
-- This replays the same seed rows into every tenant schema.
--
-- Additive and idempotent: each row is written only where it is missing. Seed
-- rows for the record type only; no client data, and the setting stays Off.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN (
      SELECT COUNT(*) FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name
         AND table_name IN ('doctype_meta', 'doctype_fields', 'role_permissions', 'approval_rules')) < 4;

    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_meta (name, module, module_key, document_type)
      SELECT 'AccessChangeRequest', 'HR', 'hr', 'Transaction'
       WHERE NOT EXISTS (SELECT 1 FROM %1$I.doctype_meta WHERE name = 'AccessChangeRequest')
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT v.* FROM (VALUES
        ('AccessChangeRequest', 'name', 'Request', 'Data', FALSE, NULL, 1),
        ('AccessChangeRequest', 'kind', 'Kind', 'Select', TRUE, 'Joiner,Mover,Leaver', 2),
        ('AccessChangeRequest', 'target_user_id', 'Target User', 'Link', TRUE, 'User', 3),
        ('AccessChangeRequest', 'target_username', 'Target Username', 'Data', TRUE, NULL, 4),
        ('AccessChangeRequest', 'detail', 'What changes', 'Data', FALSE, NULL, 5),
        ('AccessChangeRequest', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Failed', 6)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
       WHERE NOT EXISTS (SELECT 1 FROM %1$I.doctype_fields f
                          WHERE f.doctype_name = v.doctype_name AND f.fieldname = v.fieldname)
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT 'HR/Admin', 'AccessChangeRequest', TRUE, TRUE, FALSE, FALSE
       WHERE NOT EXISTS (SELECT 1 FROM %1$I.role_permissions
                          WHERE role = 'HR/Admin' AND doctype_name = 'AccessChangeRequest')
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role)
      SELECT 'AccessChangeRequest', 0, NULL, 'Super Admin'
       WHERE NOT EXISTS (SELECT 1 FROM %1$I.approval_rules WHERE doctype = 'AccessChangeRequest')
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
