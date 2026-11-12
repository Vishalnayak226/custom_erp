-- Stage 146.3.2 / 146.3.3 - codes per piece minted at GRN, and the handheld
-- work queue. Additive only, safe to re-run.
--   * ItemType.piece_codes (No / Yes): an item type that opts its items into a
--     code per received piece (engines/piece_codes_stage146.go). The store
--     switch is the setting inventory.piece_codes_stores (default blank = off).
--   * PieceCode number series (PC00000001...): one counter for the client.
--   * WarehouseTask fields the work queue reads (split_from, locked_at live in
--     the document JSON; no new table).
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
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT 'ItemType', 'piece_codes', 'Code for every piece received', 'Select', FALSE, 'No,Yes', 12
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'ItemType')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    CONTINUE WHEN to_regclass(format('%I.prefix_configs', schema_rec.schema_name)) IS NULL;
    EXECUTE format($sql$
      INSERT INTO %I.prefix_configs
        (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
      VALUES ('PieceCode', 'PC', '', 8, 'NEVER', TRUE, FALSE)
      ON CONFLICT (doc_type) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
