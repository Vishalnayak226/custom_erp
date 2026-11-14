-- ---------------------------------------------------------------------------
-- Stage 170.41: one data-quality queue. DataQualityIssue is written by the
-- scan (engines/data_quality_queue.go), never by hand: record type, record,
-- problem, found at, status (Open / Fixed / Ignored with a reason). It sits
-- beside ItemException (Stage 72.5) and leaves it as it was. Metadata only,
-- replayed into every tenant schema; no existing row is touched.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL;
    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('DataQualityIssue', 'Master Data', 'master_data', 'Master')
      ON CONFLICT (name) DO NOTHING$f$, schema_rec.schema_name);
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('DataQualityIssue', 'record_doctype',  'Record type',   'Data',   FALSE, NULL, 1),
        ('DataQualityIssue', 'record_id',       'Record',        'Data',   FALSE, NULL, 2),
        ('DataQualityIssue', 'problem_code',    'Check',         'Data',   FALSE, NULL, 3),
        ('DataQualityIssue', 'problem',         'Problem',       'Data',   FALSE, NULL, 4),
        ('DataQualityIssue', 'found_at',        'Found at',      'Data',   FALSE, NULL, 5),
        ('DataQualityIssue', 'status',          'Status',        'Select', TRUE,  'Open,Fixed,Ignored', 6),
        ('DataQualityIssue', 'ignore_reason',   'Reason ignored','Data',   FALSE, NULL, 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$, schema_rec.schema_name);
    CONTINUE WHEN to_regclass(format('%I.role_permissions', schema_rec.schema_name)) IS NULL;
    EXECUTE format($f$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'DataQualityIssue', TRUE, FALSE, TRUE, FALSE),
        ('HR/Admin',      'DataQualityIssue', TRUE, FALSE, TRUE, FALSE),
        ('Store Manager', 'DataQualityIssue', TRUE, FALSE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING$f$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
