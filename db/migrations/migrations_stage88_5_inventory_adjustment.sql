-- ---------------------------------------------------------------------------
-- Stage 88.5.4 (D13, user 2026-10-18): the stock adjustment document.
--
-- Inventory ▸ Stock Adjustment adds or takes off stock with a reason on every
-- line, goes through the approval rules (a rule for InventoryAdjustment holds
-- it as Pending Approval; none posts it at once), and posts the stock ledger
-- and the GL at the moving-average cost (engines/inventory_documents.go).
-- It is 74.2.3's InventoryAdjustment, numbered from the ADJ series Stage 76.12
-- already created ("ADJ Inventory / stock adjustment"); 74.2.3 extends it.
--
-- 5190 Stock Adjustment is the other side of every posting: a gain credits
-- it, a loss debits it. Engine-written: create on InventoryAdjustment means
-- "may adjust stock"; the generic record API only reads it.
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
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
        VALUES ('5190', 'Stock Adjustment', 'Expense')
        ON CONFLICT DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('InventoryAdjustment', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('InventoryAdjustment', 'code',           'Adjustment no',   'Data',      TRUE,  NULL, 1),
        ('InventoryAdjustment', 'location',       'Store',           'Link',      TRUE,  'Location', 2),
        ('InventoryAdjustment', 'note',           'Note',            'Data',      FALSE, NULL, 3),
        ('InventoryAdjustment', 'items',          'Items',           'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"qty","label":"Change (+/-)","type":"number","required":true},{"key":"reason_code","label":"Reason","type":"data","required":true},{"key":"value","label":"Value","type":"number"}]', 4),
        ('InventoryAdjustment', 'increase_value', 'Value added',     'Number',    FALSE, NULL, 5),
        ('InventoryAdjustment', 'decrease_value', 'Value taken off', 'Number',    FALSE, NULL, 6),
        ('InventoryAdjustment', 'amount',         'Value moved',     'Number',    FALSE, NULL, 7),
        ('InventoryAdjustment', 'status',         'Status',          'Select',    TRUE,  'Draft,Pending Approval,Approved,Posted,Rejected', 8)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        VALUES ('HR/Admin', 'InventoryAdjustment', TRUE, TRUE, FALSE, FALSE),
               ('Store Manager', 'InventoryAdjustment', TRUE, TRUE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
