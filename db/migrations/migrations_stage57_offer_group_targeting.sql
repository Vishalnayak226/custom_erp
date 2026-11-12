-- ---------------------------------------------------------------------------
-- Stage 57.10 (user decision 2026-10-07): offers can target a PIM Product
-- Group or a list of SKUs, besides one Item or one Category.
--
-- Offer.scope gains two choices; engines/pos_offers.go resolves them
-- (ResolvePIMProductGroupItemCodes for a group, a comma/line-separated list
-- for SKU List). scope_value stays the same field - only what it holds
-- changes with the scope.
--
-- Additive and idempotent: only a scope field still carrying exactly the
-- shipped options is widened, so a tenant that customised the list keeps its
-- own, and a re-run changes nothing. Existing offers are untouched.
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
      SET options = 'Bill,Item,Category,Product Group,SKU List'
      WHERE doctype_name = 'Offer' AND fieldname = 'scope' AND options = 'Bill,Item,Category'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
