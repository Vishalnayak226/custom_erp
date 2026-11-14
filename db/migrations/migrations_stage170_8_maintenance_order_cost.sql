-- ---------------------------------------------------------------------------
-- Stage 170.8: maintenance order cost. Parts (item, qty, location), labour
-- hours x rate and downtime hours are recorded on the order. With the setting
-- assets.maintenance_costing on, completing it issues the parts and posts
-- Dr 5314 Repairs & Maintenance Expense / Cr 1200 Inventory and Cr 5315
-- Maintenance Labour Absorbed. Adds the two accounts (ON CONFLICT DO NOTHING)
-- and display fields. Additive and safe to run twice. The release counts the
-- new gl_accounts rows as a data change.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default' OR schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('5314', 'Repairs & Maintenance Expense', 'Expense'),
          ('5315', 'Maintenance Labour Absorbed', 'Expense')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('MaintenanceOrder', 'labour_hours',   'Labour hours',   'Number', FALSE, NULL, 8),
        ('MaintenanceOrder', 'labour_rate',    'Labour rate per hour', 'Number', FALSE, NULL, 9),
        ('MaintenanceOrder', 'downtime_hours', 'Downtime hours', 'Number', FALSE, NULL, 10),
        ('MaintenanceOrder', 'cost_posted',    'Cost posted',    'Number', FALSE, NULL, 11)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

  END LOOP;
END $mig$;
