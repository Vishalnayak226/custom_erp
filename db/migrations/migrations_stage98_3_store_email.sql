-- ---------------------------------------------------------------------------
-- Stage 98.3 (user, 2026-10-19: send the PO, GRN, transfer and sales bill to
-- the other party; decision D2).
--
-- A store's email. A document sent from a store goes out with this as its
-- Reply-To, so the vendor's answer reaches the store; a transfer sent to a
-- store is addressed to it. Optional - with none, the email still goes, with
-- no Reply-To, and a transfer's Send asks for the address.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('Location', 'email', 'Store Email', 'Data', FALSE, NULL, 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
