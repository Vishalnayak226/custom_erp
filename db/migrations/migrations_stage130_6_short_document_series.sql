-- Stage 130.6 (126.5, user decision 2026-10-22: "Shorten new + unused").
-- 19 shipped document series print as XX/<store>/26-27/00001 - 17 to 19
-- characters, against the user's rule "Never exceed 16 char in India" (CGST
-- Rule 46(b)). A series nobody has saved (confirmed_at IS NULL) and that has
-- never issued a number moves to the year as 2627 with as many digits as fit
-- in 16 for the org's longest store segment (at most 5, at least 3), e.g.
-- GRN/HO/2627/0001. A series in use, or one an admin saved, is not touched,
-- so no number already printed changes. New clients copy tenant_default.
DO $mig$
DECLARE
  schema_rec RECORD;
  cfg RECORD;
  store_len INT;
  pad INT;
  used BOOLEAN;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs' AND column_name = 'confirmed_at');
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs' AND column_name = 'year_format');

    -- The store segment a number prints: a Location's Short Code, else its code.
    store_len := 2;
    IF to_regclass(format('%I.documents', schema_rec.schema_name)) IS NOT NULL THEN
      EXECUTE format($q$
        SELECT COALESCE(MAX(length(COALESCE(NULLIF(TRIM(data->>'short_code'), ''), NULLIF(data->>'code', ''), id))), 2)
          FROM %I.documents WHERE doctype = 'Location' AND deleted_at IS NULL
      $q$, schema_rec.schema_name) INTO store_len;
    END IF;

    FOR cfg IN EXECUTE format($q$
      SELECT doc_type, prefix FROM %I.prefix_configs
       WHERE doc_type = ANY($1) AND confirmed_at IS NULL
         AND COALESCE(year_format, '') = '' AND COALESCE(include_store, TRUE)
         AND COALESCE(separator, '/') = '/' AND COALESCE(suffix, '') = ''
         AND COALESCE(year_position, '') = '' AND COALESCE(reset_frequency, 'ANNUAL') = 'ANNUAL'
         AND COALESCE(store_overrides, '{}'::jsonb) = '{}'::jsonb
    $q$, schema_rec.schema_name)
    USING ARRAY['PO','PR','GRN','ASN','RFQ','QTN','PRT','TO','TI','VCN','DN','AST','ATT','EXP','GRV','LOAN','LV','PRO','SM']
    LOOP
      used := FALSE;
      IF to_regclass(format('%I.sequence_counters', schema_rec.schema_name)) IS NOT NULL THEN
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I.sequence_counters WHERE doc_type = $1 AND current_val > 0)', schema_rec.schema_name)
          INTO used USING cfg.doc_type;
      END IF;
      CONTINUE WHEN used;
      -- PREFIX / STORE / 2627 / digits: three joiners and four year digits.
      pad := LEAST(5, 16 - length(cfg.prefix) - store_len - 7);
      CONTINUE WHEN pad < 3;
      EXECUTE format('UPDATE %I.prefix_configs SET year_format = ''FY_COMPACT'', padding_width = $2 WHERE doc_type = $1 AND confirmed_at IS NULL', schema_rec.schema_name)
        USING cfg.doc_type, pad;
    END LOOP;
  END LOOP;
END
$mig$;
