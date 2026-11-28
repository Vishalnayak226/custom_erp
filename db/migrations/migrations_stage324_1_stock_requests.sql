-- ---------------------------------------------------------------------------
-- Items 324.1 and 324.4 (ask 324, 2026-11-28: a store or warehouse asks
-- another location for stock; the source accepts, packs and dispatches; or
-- the request is raised by itself).
--
-- A request is a transfer order, so no table is added. Two pieces of
-- metadata only:
--   - "Stock Request" joins the Reason Code categories, so a client can keep
--     its own request reasons beside the built-in ones (none are seeded);
--   - a location gets "Auto stock requests" (No / Yes), off unless switched
--     on, for the daily request made from its reorder rules.
--
-- Nothing in client data changes. Additive and idempotent, replayed into
-- every tenant schema.
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
      UPDATE %1$I.doctype_fields
         SET options = COALESCE(NULLIF(options, '') || ',', '') || 'Stock Request'
       WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
         AND position('Stock Request' in COALESCE(options, '')) = 0$sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Location', 'auto_request_stock', 'Auto Stock Requests', 'Select', FALSE, 'No,Yes', 46)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
