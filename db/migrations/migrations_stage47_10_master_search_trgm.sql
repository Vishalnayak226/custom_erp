-- Stage 47.10 (ask 192, user 2026-11-15: "Build all recommended"): search on
-- a very large master. The 1m-row run found the list search a sequential
-- scan (23-36 s on a million Items). This adds a pg_trgm GIN index over one
-- expression joining the masters' identity fields, which the list endpoint
-- uses as a pre-filter (internal/server/handlers_core_list_search.go,
-- masterSearchPrefilter). The expression below must stay exactly the one
-- masterSearchIndexExpr builds; TestStage47MasterSearchUsesTrigramIndex fails
-- if the two drift apart.
--
-- Additive and safe to run twice. A host without the pg_trgm extension (or a
-- role that may not create it) skips the index with a NOTICE; the search
-- still works, only slower. The build takes a SHARE lock on each documents
-- table, so writes wait while it runs - seconds on today's tenants.

DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'Stage 47.10: pg_trgm not available (%), master search index skipped', SQLERRM;
END $$;

DO $$
DECLARE
  opclass text;
  schema_rec RECORD;
BEGIN
  SELECT format('%I.gin_trgm_ops', n.nspname) INTO opclass
    FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
   WHERE e.extname = 'pg_trgm';
  IF opclass IS NULL THEN
    RETURN;
  END IF;
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name = 'tenant_default'
       OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
    ORDER BY (schema_name <> 'tenant_default'), schema_name
  LOOP
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_master_search_trgm ON %I.documents USING gin ((coalesce(data->>'id', '') || E'\x1f' || coalesce(data->>'code', '') || E'\x1f' || coalesce(data->>'name', '') || E'\x1f' || coalesce(data->>'item_name', '') || E'\x1f' || coalesce(data->>'barcode', '') || E'\x1f' || coalesce(data->>'parent_product_code', '') || E'\x1f' || coalesce(data->>'family', '') || E'\x1f' || coalesce(data->>'phone', '') || E'\x1f' || coalesce(data->>'email', '') || E'\x1f' || coalesce(data->>'pan', '') || E'\x1f' || coalesce(data->>'gstin', '') || E'\x1f' || coalesce(data->>'contact_phone', '') || E'\x1f' || coalesce(data->>'contact_email', '')) %s) WHERE doctype IN ('Item', 'Customer', 'Vendor') AND deleted_at IS NULL$f$,
      schema_rec.schema_name, opclass);
  END LOOP;
END $$;
