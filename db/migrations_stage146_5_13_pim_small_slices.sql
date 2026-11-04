-- Stage 146.5.13: two optional fields. Channel.item_url_template (the address
-- of an item's page on that channel; {item_code} and {external_id} are
-- replaced) feeds the "Live on" links; ChannelCategoryMap.product_type is the
-- Amazon product type for that category. Both optional, new rows only.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Channel', 'item_url_template', 'Item Page Address', 'Data', FALSE, NULL, 7),
('ChannelCategoryMap', 'product_type', 'Amazon Product Type', 'Data', FALSE, NULL, 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields
       WHERE (doctype_name, fieldname) IN (('Channel','item_url_template'), ('ChannelCategoryMap','product_type'))
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
