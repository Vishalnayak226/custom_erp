-- ---------------------------------------------------------------------------
-- Stage 148.12: the purchase rate is kept current by the matched bill.
--
--   item_purchase_rate_history  one row per change of an item's purchase rate
--                               (Item.standard_cost): who, when, old and new
--                               (paise), and the bill that set it.
--   VendorInvoice.price_jump_reason  the reason a bill's rate jump is accepted.
--
-- Additive; loops every tenant schema; no existing row is written; re-running
-- changes nothing.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                    WHERE table_schema = schema_rec.schema_name AND table_name = 'documents') THEN
      CONTINUE;
    END IF;
    EXECUTE format($sql$
      CREATE TABLE IF NOT EXISTS %I.item_purchase_rate_history (
        id              BIGSERIAL PRIMARY KEY,
        sku             VARCHAR(200) NOT NULL,
        old_rate_paise  BIGINT NOT NULL DEFAULT 0,
        new_rate_paise  BIGINT NOT NULL DEFAULT 0,
        changed_by      VARCHAR(200) NOT NULL DEFAULT '',
        changed_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
        source_doctype  VARCHAR(100) NOT NULL DEFAULT '',
        source_id       VARCHAR(200) NOT NULL DEFAULT '',
        vendor          VARCHAR(200) NOT NULL DEFAULT '',
        reason          TEXT NOT NULL DEFAULT ''
      )
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      CREATE INDEX IF NOT EXISTS item_purchase_rate_history_sku_idx ON %I.item_purchase_rate_history (sku, changed_at DESC)
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta') THEN
      EXECUTE format($sql$
        INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT 'VendorInvoice', 'price_jump_reason', 'Reason for the rate change', 'Data', FALSE, NULL, 86
        WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = 'VendorInvoice')
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
