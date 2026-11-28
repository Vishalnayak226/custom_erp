-- ---------------------------------------------------------------------------
-- Ask 297.4: an item box that stays fast on 10 lakh items.
--
-- Every item box (PO, GRN, transfer, POS lines) searches q_fields=@identity:
-- the item's ID, code, barcode, design and family, never its name (147.2).
-- The 47.10 trigram index joins those fields with the names, so a word that
-- is in many names but no code ("lin", "kurta") gave the planner no way to
-- see that nothing would match: it walked every item in id order looking for
-- ten. On the 1-crore scratch run that took 2.6 s for "lin"; with this index
-- and statistics on the same expression it takes 0.6 ms, a part code 0.7 ms
-- and a part barcode 114 ms.
--
-- The expression is exactly what the list endpoint builds for an Item
-- @identity search (fieldsSearchClause over engines.SearchIdentityFields);
-- TestAsk297ItemIdentityIndexMatchesThePicker fails if the two drift apart.
--
-- Additive and safe to run twice. A host without pg_trgm skips the index (the
-- statistics still help). The build takes a SHARE lock on each documents
-- table, so writes wait while it runs: 18 s on 10 lakh items, well under a
-- second at today's sizes. Writes no rows.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  opclass text;
  schema_rec RECORD;
BEGIN
  SELECT format('%I.gin_trgm_ops', n.nspname) INTO opclass
    FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
   WHERE e.extname = 'pg_trgm';
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name = 'tenant_default'
       OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
    ORDER BY (schema_name <> 'tenant_default'), schema_name
  LOOP
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;
    IF opclass IS NOT NULL THEN
      EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_documents_item_identity_trgm ON %I.documents USING gin ((coalesce(data->>'id', '') || E'\x1f' || coalesce(data->>'code', '') || E'\x1f' || coalesce(data->>'barcode', '') || E'\x1f' || coalesce(data->>'parent_product_code', '') || E'\x1f' || coalesce(data->>'family', '')) %s) WHERE doctype = 'Item' AND deleted_at IS NULL$f$,
        schema_rec.schema_name, opclass);
    END IF;
    EXECUTE format($f$CREATE STATISTICS IF NOT EXISTS %I.documents_item_identity_stats ON ((coalesce(data->>'id', '') || E'\x1f' || coalesce(data->>'code', '') || E'\x1f' || coalesce(data->>'barcode', '') || E'\x1f' || coalesce(data->>'parent_product_code', '') || E'\x1f' || coalesce(data->>'family', ''))) FROM %I.documents$f$,
      schema_rec.schema_name, schema_rec.schema_name);
    EXECUTE format('ANALYZE %I.documents', schema_rec.schema_name);
  END LOOP;
END
$mig$;
