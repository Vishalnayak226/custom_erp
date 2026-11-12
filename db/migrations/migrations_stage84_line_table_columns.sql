-- ---------------------------------------------------------------------------
-- Stage 84 / 73.C.7: four line tables were added as JSONTable with no column
-- spec, so the generic record form showed a line editor with no columns:
-- ConsumptionEntry.lines (engines/consumption_entry.go), RFQ.lines,
-- RFQ.vendor_invitations and VendorQuote.lines (engines/rfq_sourcing.go
-- RFQLine / RFQInvitation / VendorQuoteLine). The keys match what those
-- engines store. Totals and flags the engines write (amount, tax, not_quoted,
-- invited_at ...) have no column on purpose: the line editor keeps keys it has
-- no column for (public/app.js renderJSONLineEditor), so they are not lost.
-- Only an empty spec is filled - a client's own spec is never overwritten.
--
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
      UPDATE %I.doctype_fields AS f
      SET options = spec.options
      FROM (VALUES
        ('RFQ', 'lines', '[{"key":"line_no","label":"Line","type":"number"},{"key":"sku","label":"Item","type":"link","link":"Item"},{"key":"description","label":"Description","type":"text"},{"key":"uom","label":"UOM","type":"text"},{"key":"qty","label":"Qty","type":"number","required":true}]'),
        ('RFQ', 'vendor_invitations', '[{"key":"vendor","label":"Vendor","type":"link","link":"Vendor","required":true},{"key":"status","label":"Status","type":"text"},{"key":"due_date","label":"Reply by","type":"text"},{"key":"decline_reason","label":"Decline reason","type":"text"},{"key":"quote_id","label":"Quote","type":"link","link":"VendorQuote"}]'),
        ('VendorQuote', 'lines', '[{"key":"line_no","label":"Line","type":"number"},{"key":"sku","label":"Item","type":"link","link":"Item"},{"key":"description","label":"Description","type":"text"},{"key":"uom","label":"Quoted unit","type":"text"},{"key":"qty","label":"Qty","type":"number"},{"key":"unit_price","label":"Unit price","type":"number"},{"key":"tax_rate","label":"Tax %%","type":"number"},{"key":"lead_time_days","label":"Lead days","type":"number"}]'),
        ('ConsumptionEntry', 'lines', '[{"key":"sku","label":"Item","type":"link","link":"Item","required":true},{"key":"name","label":"Item name","type":"text"},{"key":"book_qty","label":"Book qty","type":"number"},{"key":"counted_qty","label":"Counted qty","type":"number"},{"key":"consumed_qty","label":"Consumed qty","type":"number"},{"key":"unit_cost","label":"Unit cost","type":"number"},{"key":"cost","label":"Cost","type":"number"},{"key":"expense_account","label":"Expense account","type":"text"}]')
      ) AS spec(doctype_name, fieldname, options)
      WHERE f.doctype_name = spec.doctype_name AND f.fieldname = spec.fieldname
        AND f.fieldtype = 'JSONTable' AND COALESCE(f.options, '') = ''
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
