-- ---------------------------------------------------------------------------
-- Stage 88.5.5 (D14, user 2026-10-18): the MRP Change document.
--
-- Inventory ▸ MRP Change changes items' MRP from an effective date and keeps
-- the old and new MRP on the document, so its list is each item's MRP
-- history (engines/inventory_documents.go). Item-level, not batch-level
-- (batch MRP is 94.6). Engine-written: create on MRPChange means "may change
-- MRP"; the generic record API only reads it.
--
-- Series MRP: MRP/2627/0001, no store - an MRP is the item's everywhere.
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
      VALUES ('MRPChange', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('MRPChange', 'code',           'MRP change no', 'Data',      TRUE,  NULL, 1),
        ('MRPChange', 'effective_date', 'Effective from', 'Date',     TRUE,  NULL, 2),
        ('MRPChange', 'reason',         'Reason',        'Data',      FALSE, NULL, 3),
        ('MRPChange', 'items',          'Items',         'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"old_mrp","label":"Old MRP","type":"number"},{"key":"new_mrp","label":"New MRP","type":"number","required":true}]', 4),
        ('MRPChange', 'status',         'Status',        'Select',    TRUE,  'Draft,Scheduled,Applied,Cancelled', 5)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('HR/Admin', 'MRPChange', TRUE, TRUE, FALSE, FALSE),
               ('Store Manager', 'MRPChange', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format('ALTER TABLE %I.prefix_configs ADD COLUMN IF NOT EXISTS year_format VARCHAR(12) NOT NULL DEFAULT ''''', schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store, year_format)
        SELECT 'MRP', 'MRP', '/', 4, 'ANNUAL', TRUE, FALSE, 'FY_COMPACT'
        WHERE NOT EXISTS (SELECT 1 FROM %1$I.prefix_configs WHERE doc_type = 'MRP' OR UPPER(prefix) = 'MRP')
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
