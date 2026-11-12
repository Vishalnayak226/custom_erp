-- Stage 148.43: brand-restricted dealers. Four boxes on the customer and a
-- note on the order. Fields only, no business rows; the screens show them
-- only while the setting sales.brand_restrictions is on (off by default).
-- Additive and safe to re-run; tenant_default first, then every other tenant.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Customer', 'allowed_brands', 'Allowed brands', 'Text', FALSE, NULL, 81),
('Customer', 'brand_restriction', 'Brand rule', 'Select', FALSE, 'None,Warn,Block', 82),
('Customer', 'brand_restriction_from', 'Rule from', 'Date', FALSE, NULL, 83),
('Customer', 'brand_restriction_to', 'Rule to', 'Date', FALSE, NULL, 84),
('SalesOrder', 'brand_warning', 'Brand note', 'Data', FALSE, NULL, 91)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
        WHERE (doctype_name, fieldname) IN (('Customer', 'allowed_brands'), ('Customer', 'brand_restriction'),
                                            ('Customer', 'brand_restriction_from'), ('Customer', 'brand_restriction_to'),
                                            ('SalesOrder', 'brand_warning'))
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
