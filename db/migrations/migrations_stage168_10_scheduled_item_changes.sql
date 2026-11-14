-- ---------------------------------------------------------------------------
-- Stage 168.10: effective-dated product changes. A ScheduledItemChange row is
-- one field of one item, its new value and the day it takes effect; a daily
-- job applies the due rows through the normal item update path. A plain
-- Master doctype, replayed into every tenant schema. Additive only: no
-- existing row is touched and nothing happens until someone schedules a change.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ScheduledItemChange', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ScheduledItemChange', 'item', 'Item', 'Link', TRUE, 'Item', 1),
('ScheduledItemChange', 'field', 'Field', 'Select', TRUE, 'sale_price,mrp,standard_cost,gst_rate,hsn_code,tax_treatment,attribute', 2),
('ScheduledItemChange', 'attribute', 'Attribute', 'Link', FALSE, 'ProductAttributeDef', 3),
('ScheduledItemChange', 'new_value', 'New value', 'Data', TRUE, NULL, 4),
('ScheduledItemChange', 'effective_date', 'Effective date', 'Date', TRUE, NULL, 5),
('ScheduledItemChange', 'status', 'Status', 'Select', TRUE, 'Scheduled,Applied,Cancelled,Failed', 6),
('ScheduledItemChange', 'failure_reason', 'Failure reason', 'Data', FALSE, NULL, 7),
('ScheduledItemChange', 'applied_at', 'Applied at', 'Data', FALSE, NULL, 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'ScheduledItemChange', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'ScheduledItemChange', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'ScheduledItemChange'
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'ScheduledItemChange'
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'ScheduledItemChange'
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END
$$;
