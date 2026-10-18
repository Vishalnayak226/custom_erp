-- ---------------------------------------------------------------------------
-- Stage 81.1 (user, 2026-10-18): "I will create variant for the client and it
-- will be set for the client but disablable by client if needed ... it will
-- stay forever for them ... All attribute value deletable and attributes
-- disableable."
--
-- Three fields on Product Attribute Definition (ProductAttributeDef):
--   used_for   - Variant (tells one sellable combination from another: the
--                Item's variant options, PO designs, prints) or Product
--                information (PIM enrichment only). Blank on the attributes a
--                client already has = offered everywhere, as before.
--   uom        - the unit a Number attribute is measured in (mm, cm, micron).
--   library_id - set when the platform operator added the attribute from the
--                industry attribute library (engines/attribute_library.json).
--                Such an attribute can be switched off but never deleted.
--
-- Additive and idempotent, replayed into every tenant schema. Records are not
-- touched: attributes reach a client only when the operator adds them.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('ProductAttributeDef', 'used_for', 'Used for', 'Select', FALSE, 'Variant,Product information', 7),
        ('ProductAttributeDef', 'uom', 'Unit', 'Data', FALSE, NULL, 8),
        ('ProductAttributeDef', 'library_id', 'From the attribute library', 'Data', FALSE, NULL, 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
