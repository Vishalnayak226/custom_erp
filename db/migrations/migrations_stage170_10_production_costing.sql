-- ---------------------------------------------------------------------------
-- Stage 170.10-170.13: operation confirmations (qty, time, operator), scrap /
-- rework and WIP accounting, by-product cost share.
--
-- Additive only: one new table, three new chart rows, two doctype field rows and
-- the by-product editor gaining a "Cost share %" column. Every new posting is
-- behind a setting that defaults OFF, so no existing order changes.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.production_confirmations (
        id            BIGSERIAL PRIMARY KEY,
        order_id      VARCHAR(100) NOT NULL,
        seq           INT NOT NULL,
        good_qty      NUMERIC(18,4) NOT NULL DEFAULT 0,
        scrap_qty     NUMERIC(18,4) NOT NULL DEFAULT 0,
        minutes       NUMERIC(12,2) NOT NULL DEFAULT 0,
        operator_kind VARCHAR(20) NOT NULL DEFAULT 'User',
        operator_id   VARCHAR(100) NOT NULL DEFAULT '',
        confirmed_by  VARCHAR(100) NOT NULL DEFAULT '',
        confirmed_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
      )$sql$, schema_rec.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS production_confirmations_order_idx ON %I.production_confirmations (order_id, seq)', schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('1250', 'Work in Progress', 'Asset'),
          ('5360', 'Production Scrap Loss', 'Expense'),
          ('5370', 'Production Variance', 'Expense')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT d.doctype_name, d.fieldname, d.label, d.fieldtype, FALSE, d.options, d.display_order
        FROM (VALUES
          ('ProductionOrder', 'rework_of', 'Rework of', 'Link', 'ProductionOrder', 20)
        ) AS d(doctype_name, fieldname, label, fieldtype, options, display_order)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = d.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    -- The by-product editor gets a cost share column (only while it is still the stock editor).
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = '[{"key":"sku","label":"By-Product SKU","type":"link","link":"Item","required":true},{"key":"qty_per_unit","label":"Qty per Unit","type":"number","required":true},{"key":"cost_share_percent","label":"Cost share %%","type":"number"}]'
       WHERE doctype_name = 'BOM' AND fieldname = 'by_products'
         AND options NOT LIKE '%%cost_share_percent%%'
    $sql$, schema_rec.schema_name);

    -- Posting-profile events for the production postings (appended to the list).
    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = options || ',production_scrap,production_wip_issue,production_wip_finish,production_variance'
       WHERE doctype_name = 'PostingProfile' AND fieldname = 'event'
         AND options NOT LIKE '%%production_scrap%%'
    $sql$, schema_rec.schema_name);
  END LOOP;
END $mig$;
