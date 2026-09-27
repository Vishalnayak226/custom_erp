-- ---------------------------------------------------------------------------
-- Stage 51.4: make Vendor/Item auto-numbering editable via the existing
-- Prefix Configurations admin screen.
--
-- GenerateSequence (engines/numbering.go) already falls back to sane
-- defaults (prefix = doc_type, separator '/', padding 6, ANNUAL) when no
-- prefix_configs row exists for a doc_type - which is why Vendor/Item codes
-- were already being auto-generated through the exact same engine every
-- transaction doctype uses, just invisibly. The only real gap was that the
-- admin screen (renderPrefixConfigsView) only lists rows that already exist
-- in this table, so there was no way to see or edit Vendor/Item's
-- prefix/padding. Seeding these two rows with values identical to what the
-- fallback already produces is a pure visibility change, not a behavior
-- change - no already-issued Vendor/Item code's format shifts.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.prefix_configs
    (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store) VALUES
('Vendor', 'Vendor', '/', 6, 'ANNUAL', TRUE, TRUE),
('Item', 'Item', '/', 6, 'ANNUAL', TRUE, TRUE)
ON CONFLICT (doc_type) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - same pattern as the
-- PIMBarcodeSeq backfill in migrations_stage36_7_enrichment_quality.sql.
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
      VALUES
        ('Vendor', 'Vendor', '/', 6, 'ANNUAL', TRUE, TRUE),
        ('Item', 'Item', '/', 6, 'ANNUAL', TRUE, TRUE)
      ON CONFLICT (doc_type) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
