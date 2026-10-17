-- ---------------------------------------------------------------------------
-- Stage 61.6 / 61.7 (user, 2026-10-16: "All above" - GRN batch numbers with the
-- same flexibility as the barcode rules).
--
-- 1. ItemType.grn_batch_number and Item.grn_batch_number - how a GRN line left
--    without a batch number gets one, overriding the client setting
--    inventory.grn_batch_number exactly the way barcode_generate_at does
--    (Item, then Item Type, then client). Blank / "Client default" /
--    "Item type default" inherit, so no existing record changes behaviour.
-- 2. The GRNBatchSeq series becomes an ordinary number series: the batch
--    number is now what GenerateSequence prints (prefix, store, period = the
--    receipt date, number, suffix, joined by the separator). Stage 61.1
--    seeded it with no prefix and no separator because the code formatted the
--    number itself; only that untouched seed is given prefix B and separator
--    "-" (B-261015-0001). A series someone has already edited is left alone.
-- 3. Batch.location - where the receiving GRN was posted.
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
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT v.* FROM (VALUES
          ('ItemType', 'grn_batch_number', 'GRN batch number', 'Select', FALSE,
           'Client default,One per SKU on each GRN,One per GRN line,One per SKU per day,Off', 10),
          ('Item', 'grn_batch_number', 'GRN batch number', 'Select', FALSE,
           'Item type default,One per SKU on each GRN,One per GRN line,One per SKU per day,Off', 44),
          ('Batch', 'location', 'Received At', 'Data', FALSE, NULL, 13)
        ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        UPDATE %I.prefix_configs SET prefix = 'B', separator = '-'
        WHERE doc_type = 'GRNBatchSeq' AND COALESCE(prefix, '') = '' AND COALESCE(separator, '') = ''
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
