-- ---------------------------------------------------------------------------
-- Stage 73.2.3 - an award converts to a PO, once, with lineage both ways.
--
-- RFQ.awarded_po_id / awarded_quote_id, VendorQuote.po_id and
-- PurchaseOrder.source_rfq_id / source_quote_id / freight_amount /
-- payment_terms are written by engines.AwardRFQ under the RFQ's row lock; a
-- second award is refused. Field registration only (read-only display).
-- Additive and idempotent, replayed into every tenant schema.
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
      SELECT v.d, v.f, v.l, v.t, FALSE, v.o, v.n
        FROM (VALUES
          ('RFQ', 'awarded_po_id', 'Purchase order (awarded)', 'Link', 'PurchaseOrder', 20),
          ('RFQ', 'awarded_quote_id', 'Winning quote', 'Link', 'VendorQuote', 21),
          ('VendorQuote', 'po_id', 'Purchase order', 'Link', 'PurchaseOrder', 20),
          ('PurchaseOrder', 'source_rfq_id', 'From RFQ', 'Link', 'RFQ', 90),
          ('PurchaseOrder', 'source_quote_id', 'From quote', 'Link', 'VendorQuote', 91),
          ('PurchaseOrder', 'freight_amount', 'Freight (quoted)', 'Number', NULL, 92),
          ('PurchaseOrder', 'payment_terms', 'Payment terms', 'Data', NULL, 93)
        ) AS v(d, f, l, t, o, n)
       WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = v.d)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
