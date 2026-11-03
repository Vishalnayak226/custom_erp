-- ---------------------------------------------------------------------------
-- Stage 146.5.1: which picture set was last sent to Shopify for a linked
-- variant (a fingerprint), so a changed set is re-sent and an unchanged one
-- never is. Additive; empty means "not recorded yet".
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF to_regclass(format('%I.shopify_links', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($sql$
      ALTER TABLE %I.shopify_links ADD COLUMN IF NOT EXISTS images_hash VARCHAR(64) NOT NULL DEFAULT ''
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
