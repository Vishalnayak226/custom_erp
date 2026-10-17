-- ---------------------------------------------------------------------------
-- Stage 72.6 - Consumption Entry (the user's blueprint 9.6, FR-012): periodic
-- count-based consumption (butter paper: counted closing stock, consumption =
-- book - counted) and manual issue. Written only by
-- engines.PostConsumptionEntry through POST /api/v1/inventory/consumption, so
-- no role creates or edits one by hand; Inventory -> Consumption Entry and
-- the Report Catalog list them.
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('ConsumptionEntry', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ConsumptionEntry', 'location', 'Store', 'Link', TRUE, 'Location', 1),
        ('ConsumptionEntry', 'mode', 'Type', 'Select', TRUE, 'Periodic count,Manual issue', 2),
        ('ConsumptionEntry', 'count_reference', 'Count reference', 'Data', FALSE, NULL, 3),
        ('ConsumptionEntry', 'reason', 'Reason', 'Data', FALSE, NULL, 4),
        ('ConsumptionEntry', 'lines', 'Lines', 'JSONTable', FALSE, NULL, 5),
        ('ConsumptionEntry', 'cost_total', 'Cost (valuated items)', 'Number', FALSE, NULL, 6),
        ('ConsumptionEntry', 'posted_by', 'Posted by', 'Data', FALSE, NULL, 7),
        ('ConsumptionEntry', 'approved_by', 'Approved by', 'Data', FALSE, NULL, 8),
        ('ConsumptionEntry', 'status', 'Status', 'Select', TRUE, 'Posted', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'ConsumptionEntry', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'ConsumptionEntry', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
