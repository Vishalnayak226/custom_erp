-- Stage 30.1.2 (docs/UX_MANUAL_AUDIT.md A2): hsn_code / gst_rate were seeded
-- mandatory = FALSE on Item, so the New Item form showed no required marker on
-- either - yet BOTH POS checkout and PurchaseOrder creation hard-reject a line
-- whose Item is missing them (engines/gst.go GetItemGSTInfo, the Stage 17.5
-- gate). A first-time user could therefore create a product that looked
-- complete and saved cleanly, and only discover at the till that it can be
-- neither sold nor purchased.
--
-- Flipping the two metadata rows to mandatory makes the form show the required
-- marker and makes engines/doctype.go's generic ValidateDocument refuse the
-- save up front - the same choke point every Item writer already runs through
-- (generic form, API, bulk import), rather than a new check per call site.
-- engines/master_data_validation.go's validateItemMasterRules carries the
-- matching value rule (a positive rate), so the master layer now rejects
-- exactly what the transaction layer would have rejected later.
--
-- Additive and reversible: this only updates two doctype_fields rows, no table
-- or column is touched, and existing Item documents are left exactly as they
-- are (they simply have to gain an HSN the next time someone edits them -
-- which is the point, since they cannot be transacted until they do).
UPDATE tenant_default.doctype_fields
   SET mandatory = TRUE
 WHERE doctype_name = 'Item' AND fieldname IN ('hsn_code', 'gst_rate');

-- Backfill every already-provisioned tenant schema. New tenants inherit this
-- automatically (ProvisionTenantSchema clones tenant_default's doctype_fields),
-- so only pre-existing schemas need the direct update - same pattern as
-- db/migrations_stage18_core_module_fix.sql.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'UPDATE %I.doctype_fields SET mandatory = TRUE WHERE doctype_name = ''Item'' AND fieldname IN (''hsn_code'', ''gst_rate'')',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
