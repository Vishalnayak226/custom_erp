-- ---------------------------------------------------------------------------
-- Stage 55.0F (functional sweep): ProductBundle.components was added as a
-- JSONTable with no column spec, so OMS -> Manage bundles showed a line editor
-- with no columns and a bundle could not be given a component from the form.
-- The spec matches engines/bundles.go BundleComponent (sku, quantity,
-- unit_price, price_weight). Only an empty spec is filled - a client's own
-- spec is never overwritten.
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
      SET options = '[{"key":"sku","label":"Component SKU","type":"link","link":"Item","required":true},{"key":"quantity","label":"Quantity","type":"number","required":true},{"key":"unit_price","label":"Unit price (Component Price mode)","type":"number"},{"key":"price_weight","label":"Price weight","type":"number"}]'
      WHERE doctype_name = 'ProductBundle' AND fieldname = 'components'
        AND fieldtype = 'JSONTable' AND COALESCE(options, '') = ''
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
