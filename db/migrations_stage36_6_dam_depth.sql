-- ---------------------------------------------------------------------------
-- Stage 36.6: DAM depth - asset transformations, bulk zip up/download,
-- tagging/browse UI, auto-association.
--
-- The only schema change this stage needs is one additive field:
-- ProductMedia.tags, a comma-separated list in the same shape
-- ProductContent.tags already uses. Everything else is engine behaviour
-- (engines/pim_media.go) over data that already exists:
--   - transformations (36.6.1) are generated on demand and cached to disk,
--     content-addressed by checksum+preset the same way the existing
--     thumbnail already is - no new table;
--   - bulk zip up/download (36.6.2) and filename-based auto-association
--     (36.6.4) read/write ProductMedia rows exactly like a single upload
--     already does, just looped;
--   - search/filter (36.6.3) is a new WHERE clause over the same table.
--
-- Additive and backward-compatible: a tenant that never tags an asset is
-- byte-identical to what it is today.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ProductMedia', 'tags', 'Tags (comma-separated)', 'Data', FALSE, NULL, 15)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

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
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES ('ProductMedia', 'tags', 'Tags (comma-separated)', 'Data', FALSE, NULL, 15)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
