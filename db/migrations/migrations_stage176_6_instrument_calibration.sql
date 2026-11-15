-- ---------------------------------------------------------------------------
-- Ask 176 item 176.6: instrument calibration. An Instrument (gauge, scale,
-- meter) has a calibration interval in days; a CalibrationRecord against it
-- stores its next due date (engines/quality_calibration.go sets it: the
-- calibration date plus the interval on a Pass, the calibration date itself on
-- a Fail). The "Instruments due for calibration" report lists what is overdue.
-- Additive and idempotent, replayed into every tenant schema.
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
        ('Instrument',        'Quality', 'quality', 'Master'),
        ('CalibrationRecord', 'Quality', 'quality', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Instrument', 'code',                      'Code',                   'Data',   TRUE,  NULL, 1),
        ('Instrument', 'name',                      'Name',                   'Data',   TRUE,  NULL, 2),
        ('Instrument', 'serial_no',                 'Serial number',          'Data',   FALSE, NULL, 3),
        ('Instrument', 'kept_at',                   'Kept at',                'Data',   FALSE, NULL, 4),
        ('Instrument', 'calibration_interval_days', 'Calibrate every (days)', 'Number', TRUE,  NULL, 5),
        ('Instrument', 'status',                    'Status',                 'Select', TRUE,  'Active,Out of service,Retired', 6),
        ('CalibrationRecord', 'code',           'Record number',  'Data',   TRUE,  NULL, 1),
        ('CalibrationRecord', 'instrument',     'Instrument',     'Link',   TRUE,  'Instrument', 2),
        ('CalibrationRecord', 'calibrated_on',  'Calibrated on',  'Date',   TRUE,  NULL, 3),
        ('CalibrationRecord', 'result',         'Result',         'Select', TRUE,  'Pass,Fail', 4),
        ('CalibrationRecord', 'certificate_no', 'Certificate',    'Data',   FALSE, NULL, 5),
        ('CalibrationRecord', 'calibrated_by',  'Calibrated by',  'Data',   FALSE, NULL, 6),
        ('CalibrationRecord', 'next_due_on',    'Next due',       'Date',   FALSE, NULL, 7),
        ('CalibrationRecord', 'notes',          'Notes',          'Text',   FALSE, NULL, 8),
        ('CalibrationRecord', 'status',         'Status',         'Select', TRUE,  'Draft,Recorded', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'Instrument',        TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'Instrument',        TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin',      'Instrument',        TRUE, TRUE, TRUE, FALSE),
        ('Super Admin',   'CalibrationRecord', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'CalibrationRecord', TRUE, TRUE, TRUE, FALSE),
        ('HR/Admin',      'CalibrationRecord', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
