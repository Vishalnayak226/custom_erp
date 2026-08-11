-- Stage 26.6.11: tax-exempt / nil-rated / zero-rated goods.
--
-- Stage 30.1.2 made hsn_code and a POSITIVE gst_rate mandatory on Item, on the
-- reasoning that GetItemGSTInfo rejected an item without them at both checkout
-- and PO creation, so "saveable with 0%" only ever meant "unusable later". That
-- closed a real hole but created another: an item genuinely taxed at 0% -
-- unbranded grain, fresh produce, books, exports - became unsaveable, so a
-- tenant selling ordinary exempt goods could not create the Item at all.
--
-- The fix is NOT to let a bare 0 through: 0 is indistinguishable from "not
-- filled in yet", which is exactly the hole 30.1.2 closed. Instead the item
-- declares its treatment explicitly, and the 0 rate is only accepted once that
-- declaration has been made.
--
--   Taxable     - the ordinary case. gst_rate must be > 0 (30.1.2's rule).
--   Exempt      - exempt by notification (e.g. fresh produce). Rate must be 0.
--   Nil-Rated   - tariff rate is genuinely 0%. Rate must be 0.
--   Zero-Rated  - exports / SEZ supplies under LUT. Rate must be 0.
--
-- Exempt and Nil-Rated are kept apart rather than merged because GSTR-1's
-- nil/exempt/non-GST table reports them in separate columns, and Zero-Rated is
-- separate again because GSTR-3B puts it in 3.1(b) while exempt+nil go to
-- 3.1(c). Merging any two would make the return un-fileable without a manual
-- re-split.
--
-- Deliberately mandatory = FALSE: a blank/absent tax_treatment is read as
-- 'Taxable' by engines/master_data_validation.go and engines/gst.go, so every
-- Item that exists today keeps validating exactly as it does now, and every
-- existing CSV import template keeps working. Nothing is relaxed by that
-- default - a 0 rate is still rejected unless a non-taxable treatment was
-- explicitly chosen.
--
-- HSN stays mandatory for all four treatments (30.1.2's guarantee is untouched):
-- HSN is required on the invoice regardless of rate for most turnover slabs,
-- and GSTR-1's nil/exempt table is itself reported HSN-wise.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'tax_treatment', 'Tax Treatment', 'Select', FALSE, 'Taxable,Exempt,Nil-Rated,Zero-Rated', 8)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Slot it next to HSN Code (7) rather than at the end of the form: it gates
-- what gst_rate is allowed to be, so a user who meets it after "Variant
-- Options" has already filled in the field it governs. Display metadata only -
-- no document data is touched.
UPDATE tenant_default.doctype_fields SET display_order = 9  WHERE doctype_name = 'Item' AND fieldname = 'gst_rate';
UPDATE tenant_default.doctype_fields SET display_order = 10 WHERE doctype_name = 'Item' AND fieldname = 'family';
UPDATE tenant_default.doctype_fields SET display_order = 11 WHERE doctype_name = 'Item' AND fieldname = 'parent_product_code';
UPDATE tenant_default.doctype_fields SET display_order = 12 WHERE doctype_name = 'Item' AND fieldname = 'variant_option_values';

-- Non-taxable turnover needs somewhere to land that is not 4100. GetGSTReturnSummary
-- reads 4100's net balance as the period's TAXABLE value - correct today only
-- because PostSalesGSTBooking moves the tax portion out of 4100 and every sale
-- is taxable. An exempt sale moves nothing, so its full value would have been
-- reported as taxable turnover: a wrong GSTR-3B 3.1(a), understating exempt
-- supplies and overstating taxable ones. PostExemptSalesReclass now moves each
-- non-taxable line's value out of 4100 into the matching account below, so the
-- summary can report all four buckets from the GL alone, the same way it
-- already reports the three tax accounts.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('4110', 'Exempt Sales Revenue', 'Revenue'),
('4111', 'Nil-Rated Sales Revenue', 'Revenue'),
('4112', 'Zero-Rated Sales Revenue', 'Revenue')
ON CONFLICT (account_code) DO NOTHING;

-- Backfill every already-provisioned tenant schema. New tenants inherit both
-- the field and the accounts from tenant_default at provisioning time
-- (ProvisionTenantSchema clones doctype_fields and gl_accounts), so only
-- pre-existing schemas need the direct write - same pattern as
-- db/migrations_stage30_2_5_loyalty_redemption_account.sql.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) '
      'VALUES (''Item'', ''tax_treatment'', ''Tax Treatment'', ''Select'', FALSE, ''Taxable,Exempt,Nil-Rated,Zero-Rated'', 8) '
      'ON CONFLICT (doctype_name, fieldname) DO NOTHING',
      schema_rec.schema_name
    );
    EXECUTE format(
      'UPDATE %I.doctype_fields SET display_order = CASE fieldname '
      '  WHEN ''gst_rate'' THEN 9 WHEN ''family'' THEN 10 '
      '  WHEN ''parent_product_code'' THEN 11 WHEN ''variant_option_values'' THEN 12 END '
      'WHERE doctype_name = ''Item'' '
      '  AND fieldname IN (''gst_rate'', ''family'', ''parent_product_code'', ''variant_option_values'')',
      schema_rec.schema_name
    );
    EXECUTE format(
      'INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES '
      '(''4110'', ''Exempt Sales Revenue'', ''Revenue''), '
      '(''4111'', ''Nil-Rated Sales Revenue'', ''Revenue''), '
      '(''4112'', ''Zero-Rated Sales Revenue'', ''Revenue'') '
      'ON CONFLICT (account_code) DO NOTHING',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
