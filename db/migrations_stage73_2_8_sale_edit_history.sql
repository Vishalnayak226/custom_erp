-- ---------------------------------------------------------------------------
-- Stage 73.2.8 - post-sale edit history.
--
--   * DocumentEditHistory: one record per edit of an issued sale (a sales
--     invoice or order past Draft, a paid till bill) - the document, its
--     status then, who changed it, the reason they gave, and each changed
--     field's before and after value. Written only by the save itself
--     (engines/sale_edit_history.go), so it is read-only to every role; the
--     Post-Sale Edit History report lists it.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('DocumentEditHistory', 'Sales', 'Transaction', 'sales')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('DocumentEditHistory', 'document_type', 'Document', 'Data', TRUE, NULL, 1),
        ('DocumentEditHistory', 'document_id', 'Number', 'Data', TRUE, NULL, 2),
        ('DocumentEditHistory', 'document_status', 'Status then', 'Data', FALSE, NULL, 3),
        ('DocumentEditHistory', 'edited_by', 'Changed by', 'Data', TRUE, NULL, 4),
        ('DocumentEditHistory', 'reason', 'Reason', 'Data', TRUE, NULL, 5),
        ('DocumentEditHistory', 'edited_at', 'When', 'Data', TRUE, NULL, 6)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'DocumentEditHistory', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'DocumentEditHistory', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
