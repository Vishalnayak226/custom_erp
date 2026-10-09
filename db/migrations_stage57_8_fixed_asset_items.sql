-- ---------------------------------------------------------------------------
-- Stage 57.8 (user decision 2026-10-06, model approved 2026-10-09):
-- non-sellable asset records live in the Fixed Assets module, never in POS,
-- sales, OMS or sellable stock.
--
--   * Item.item_type: Stock (blank means Stock, so every existing Item is
--     unchanged) or Fixed Asset. A Fixed Asset item is bought on a PO like
--     any Item, but its GRN raises Draft Assets instead of stock
--     (engines/asset_items.go), and every sale-side path refuses it
--     (ASSET-0273).
--   * Asset gains item_code / source_grn / source_po, so an asset raised from
--     a receipt says what it was bought as and where it came from.
--   * The AST number series those assets draw from. A hand-entered asset
--     still takes the Asset Number typed on the Fixed Assets screen.
--
-- Additive and idempotent, replayed into every tenant schema that carries the
-- document model. New tenants copy these rows from tenant_default.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Item', 'item_type', 'Item Type (Stock or Fixed Asset)', 'Select', FALSE, 'Stock,Fixed Asset', 40),
        ('Asset', 'item_code', 'Bought As Item', 'Link', FALSE, 'Item', 14),
        ('Asset', 'source_grn', 'Goods Receipt (GRN)', 'Link', FALSE, 'GRN', 15),
        ('Asset', 'source_po', 'Purchase Order', 'Link', FALSE, 'PurchaseOrder', 16)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'prefix_configs') THEN
      EXECUTE format($sql$
        INSERT INTO %I.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency, include_store)
        VALUES ('AST', 'AST', '/', 6, 'ANNUAL', TRUE)
        ON CONFLICT (doc_type) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
