-- ---------------------------------------------------------------------------
-- Stage 71.1 (user, 2026-10-17: the "Standard ERP Transaction Print Formats"
-- pack - "adopt it, discard what is not required").
--
-- Four more transactions can be printed and given designed templates: the
-- GRN's Vendor Copy, Purchase Return, Credit Note (to customer) and Sales
-- Return. PrintTemplate.transaction is a Select, so its options gain the four
-- keys. Only the option list Stage 63 shipped is replaced; one an admin has
-- already edited is left alone.
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
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        UPDATE %I.doctype_fields
           SET options = 'PurchaseOrder,VendorPO,GRN,GRNVendorCopy,PurchaseReturn,TransferOut,TransferIn,DebitNote,VendorCreditNote,CreditNote,SalesReturn'
         WHERE doctype_name = 'PrintTemplate' AND fieldname = 'transaction'
           AND options = 'PurchaseOrder,VendorPO,GRN,TransferOut,TransferIn,DebitNote,VendorCreditNote'
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
