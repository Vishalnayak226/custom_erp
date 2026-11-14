-- ---------------------------------------------------------------------------
-- Stage 170.16: supplier corrective action requests (SCAR). Opened from a
-- vendor non-conformance and taken step by step (root cause, action,
-- verification) by the quality screen; the vendor scorecard counts the open
-- ones. Additive and safe to run twice.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('SupplierCAR', 'Quality', 'quality', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('SupplierCAR', 'code',         'SCAR',            'Data',   FALSE, NULL, 1),
        ('SupplierCAR', 'vendor',       'Vendor',          'Link',   TRUE,  'Vendor', 2),
        ('SupplierCAR', 'ncr_id',       'Non-conformance', 'Data',   FALSE, NULL, 3),
        ('SupplierCAR', 'item',         'Item',            'Data',   FALSE, NULL, 4),
        ('SupplierCAR', 'batch_no',     'Lot',             'Data',   FALSE, NULL, 5),
        ('SupplierCAR', 'issue',        'Issue',           'Data',   TRUE,  NULL, 6),
        ('SupplierCAR', 'due_date',     'Reply by',        'Date',   TRUE,  NULL, 7),
        ('SupplierCAR', 'root_cause',   'Root cause',      'Data',   FALSE, NULL, 8),
        ('SupplierCAR', 'action',       'Action',          'Data',   FALSE, NULL, 9),
        ('SupplierCAR', 'verification', 'Verification',    'Data',   FALSE, NULL, 10),
        ('SupplierCAR', 'status',       'Status',          'Select', TRUE,  'Open,Root cause found,Action taken,Closed', 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'SupplierCAR', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'SupplierCAR', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
