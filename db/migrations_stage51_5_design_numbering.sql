-- ---------------------------------------------------------------------------
-- Stage 51.5: a friendlier auto-numbering prefix for ProductFamily, the
-- closest thing this codebase has to a real "Design ID" record (see
-- engines.PrepareItemVariantCode's own comment on why family/ProductFamily
-- plays that role). Like Vendor/Item before this (migrations_stage51_4_
-- vendor_item_numbering.sql), ProductFamily was already being auto-numbered
-- through GenerateSequence's own no-row-yet fallback (prefix = doc_type,
-- i.e. "ProductFamily/HQ/..."); this just makes the prefix nicer and the
-- series editable from the Prefix Configurations screen, without changing
-- any already-issued code's shape beyond the literal prefix text.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.prefix_configs
    (doc_type, prefix, separator, padding_width, reset_frequency, active_status, include_store) VALUES
('ProductFamily', 'Design', '/', 6, 'ANNUAL', TRUE, TRUE)
ON CONFLICT (doc_type) DO NOTHING;

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
      VALUES ('ProductFamily', 'Design', '/', 6, 'ANNUAL', TRUE, TRUE)
      ON CONFLICT (doc_type) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
