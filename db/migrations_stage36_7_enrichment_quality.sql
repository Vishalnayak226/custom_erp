-- ---------------------------------------------------------------------------
-- Stage 36.7: enrichment & quality - channel-shaped content assist templates
-- (36.7.1), related products (36.7.3), UPC/EAN generation and check-digit
-- validation (36.7.4), bulk catalog translation (36.7.5), attribute-level
-- permissions audit (36.7.6, code-only - see engines/pim_bulk.go/import.go,
-- no schema change needed).
-- ---------------------------------------------------------------------------

-- 36.7.4: a dedicated document-numbering series for GenerateEANBarcode
-- (engines/pim_barcode.go) to draw from, reusing the existing
-- GenerateSequence/prefix_configs/sequence_counters machinery
-- (engines/numbering.go) rather than a second counter mechanism. An empty
-- prefix/separator and reset_frequency NEVER make GenerateSequence's own
-- formatting collapse to exactly nine zero-padded digits with nothing else
-- attached - see that function's own comment for why the shape matters.
INSERT INTO tenant_default.prefix_configs
    (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store) VALUES
('PIMBarcodeSeq', '', '', 9, 'NEVER', TRUE, FALSE)
ON CONFLICT (doc_type) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical row - the same pattern every prior
-- PIM-stage migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.prefix_configs', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.prefix_configs
        (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store)
      VALUES ('PIMBarcodeSeq', '', '', 9, 'NEVER', TRUE, FALSE)
      ON CONFLICT (doc_type) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
