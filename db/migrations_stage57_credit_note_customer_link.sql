-- ---------------------------------------------------------------------------
-- Stage 57 (found recording the finance SOP chapter, 2026-10-09): a Credit
-- Note's Customer becomes a Link to Customer.
--
-- CreditNote.customer_id was a 'Data' field, so the form rendered a bare text
-- box and the user had to know and type the customer's code - the opposite of
-- Stage 57.1 (names, not codes). DebitNote.vendor_id has always been a Link to
-- Vendor; this brings its sibling in line, the same way Stage 57 turned
-- DebitNote.reference_po into a Link to PurchaseOrder.
--
-- Not mandatory, so existing notes stay valid; only a field that is still
-- 'Data' is touched, so re-running changes nothing. Applied to every tenant.
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
      UPDATE %I.doctype_fields
      SET fieldtype = 'Link', options = 'Customer', label = 'Customer'
      WHERE doctype_name = 'CreditNote' AND fieldname = 'customer_id' AND fieldtype = 'Data'
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
