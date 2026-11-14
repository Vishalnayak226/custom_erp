-- ---------------------------------------------------------------------------
-- Stage 170.14: backward lot trace. LotGenealogy rows (one per output lot and
-- input lot of a work order completion) are written by the work order
-- completion only; people can read them but not edit them. ProductionOrder
-- gains an optional `input_lots` list (item, lot, qty) a planner may record.
-- Additive and safe to run twice: no existing row is touched.
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
        ('LotGenealogy', 'Quality', 'quality', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('LotGenealogy', 'work_order',   'Work order',  'Data',   FALSE, NULL, 1),
        ('LotGenealogy', 'completion',   'Completion',  'Number', FALSE, NULL, 2),
        ('LotGenealogy', 'output_sku',   'Output item', 'Data',   FALSE, NULL, 3),
        ('LotGenealogy', 'output_batch', 'Output lot',  'Data',   FALSE, NULL, 4),
        ('LotGenealogy', 'output_qty',   'Output qty',  'Number', FALSE, NULL, 5),
        ('LotGenealogy', 'input_sku',    'Input item',  'Data',   FALSE, NULL, 6),
        ('LotGenealogy', 'input_batch',  'Input lot',   'Data',   FALSE, NULL, 7),
        ('LotGenealogy', 'input_qty',    'Input qty',   'Number', FALSE, NULL, 8),
        ('LotGenealogy', 'source',       'Lot source',  'Select', FALSE, 'Recorded,Inferred', 9),
        ('LotGenealogy', 'status',       'Status',      'Select', TRUE,  'Recorded', 10),
        ('ProductionOrder', 'input_lots', 'Input lots', 'JSONTable', FALSE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"batch_no","label":"Lot","type":"data","required":true},{"key":"qty","label":"Qty","type":"number"}]', 120)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'LotGenealogy', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'LotGenealogy', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
