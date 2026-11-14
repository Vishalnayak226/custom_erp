-- ---------------------------------------------------------------------------
-- Stage 170.9: meter-based maintenance. AssetMeterReading holds an asset's
-- readings (date, reading, unit); a MaintenanceSchedule can carry a meter
-- interval as well as days. Additive and safe to run twice; no existing row is
-- touched.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('AssetMeterReading', 'Finance', 'assets', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('AssetMeterReading', 'asset',        'Asset',   'Link',   TRUE, 'Asset', 1),
        ('AssetMeterReading', 'reading_date', 'Date',    'Date',   TRUE, NULL, 2),
        ('AssetMeterReading', 'reading',      'Reading', 'Number', TRUE, NULL, 3),
        ('AssetMeterReading', 'unit',         'Unit',    'Data',   TRUE, NULL, 4),
        ('MaintenanceSchedule', 'interval_meter', 'Interval (meter)', 'Number', FALSE, NULL, 7),
        ('MaintenanceSchedule', 'meter_baseline', 'Reading at last service', 'Number', FALSE, NULL, 8)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin', 'AssetMeterReading', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin',    'AssetMeterReading', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'AssetMeterReading', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

  END LOOP;
END $mig$;
