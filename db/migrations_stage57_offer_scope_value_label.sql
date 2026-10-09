-- ---------------------------------------------------------------------------
-- Stage 57.10 follow-up (found recording the offers SOP chapter, 2026-10-09):
-- Offer.scope_value's label still read "Item SKU / Category (blank for whole
-- bill)" after Product Group and SKU List became targets. The form now
-- changes the box itself with Applies To (wireOfferScopeValue), so the label
-- says what it is in general terms.
--
-- Only a label still carrying the shipped text is renamed, so a tenant's own
-- wording is kept and a re-run changes nothing. Every tenant.
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
      SET label = 'Applies To - which item, category, group or SKUs'
      WHERE doctype_name = 'Offer' AND fieldname = 'scope_value'
        AND label = 'Item SKU / Category (blank for whole bill)'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
