-- ---------------------------------------------------------------------------
-- Stage 146.2.3: items that cannot be paid with points.
--
-- Two fields, values live in the document data (no new column):
--   Item.loyalty_redemption       - blank = follow the product group;
--                                   "Allowed" / "Not allowed" decides the item.
--   PIMProductGroup.no_points_redemption - ticked = every product in the
--                                   group is not payable with points.
-- Existing items and groups are untouched (blank / unticked = as before).
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'loyalty_redemption', 'Pay with points', 'Select', FALSE, 'Allowed,Not allowed', 93),
('PIMProductGroup', 'no_points_redemption', 'Not payable with points', 'Check', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT * FROM (VALUES
        ('Item', 'loyalty_redemption', 'Pay with points', 'Select', FALSE, 'Allowed,Not allowed'::text, 93),
        ('PIMProductGroup', 'no_points_redemption', 'Not payable with points', 'Check', FALSE, NULL::text, 11)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING$f$,
      schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
