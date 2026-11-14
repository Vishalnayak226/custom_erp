-- ---------------------------------------------------------------------------
-- Stage 34.4: the Myntra market-price harvester writes ordinary CompetitorPrice
-- rows; four optional columns say where a row came from. Additive: no existing
-- row is touched, and nothing is written until an admin turns the harvester on
-- (setting platform.market_harvest_myntra, off by default).
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CompetitorPrice', 'brand', 'Brand', 'Data', FALSE, NULL, 13),
('CompetitorPrice', 'keyword', 'Search Keyword', 'Data', FALSE, NULL, 14),
('CompetitorPrice', 'discount_pct', 'Discount %', 'Number', FALSE, NULL, 15),
('CompetitorPrice', 'harvest_source', 'Collected By', 'Data', FALSE, NULL, 16)
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
       WHERE doctype_name = 'CompetitorPrice'
         AND fieldname IN ('brand', 'keyword', 'discount_pct', 'harvest_source')
         AND EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'CompetitorPrice')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END $$;
