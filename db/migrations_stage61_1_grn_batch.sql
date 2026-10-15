-- ---------------------------------------------------------------------------
-- Stage 61.1 (user, 2026-10-15): "In each GRN, each Combination ID/SKU will
-- have a batch number" - and "after scanning I should know the batch, so I
-- will know ageing or which day it came in".
--
-- engines.PrepareGRNBatchNumbers numbers every blank GRN line B + YYMMDD + "-"
-- + the day's running number (B261015-0001). This file only adds:
--
-- 1. The GRNBatchSeq series row, so the daily counter has a home with no
--    store segment (same shape as Stage 57.7's PIMDatedBarcodeSeq). The code
--    formats the number from the counter's digits, so a missing row would
--    still produce a correct number - the row is for the Number Series screen.
-- 2. Batch.grn and Batch.received_on, which EnsureBatch now stamps on a lot a
--    GRN registers, so the Batch form shows where and when it came in. Batch
--    documents written before this keep their data; the ageing report falls
--    back to the record's creation date for them.
--
-- Additive and idempotent (ON CONFLICT DO NOTHING), replayed into every
-- tenant schema; new tenants clone tenant_default's rows.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
        VALUES ('GRNBatchSeq', '', '', 4, 'ANNUAL', TRUE, FALSE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        VALUES
          ('Batch', 'grn', 'Goods Receipt', 'Data', FALSE, NULL, 11),
          ('Batch', 'received_on', 'Received On', 'Date', FALSE, NULL, 12)
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
