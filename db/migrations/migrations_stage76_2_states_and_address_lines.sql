-- ---------------------------------------------------------------------------
-- Stage 76.2 / 76.3 (user, 2026-10-17, on the New Vendor dialog): "State
-- mandatory ... It should dropdown. This is for all state field" and "All
-- address will be address line 1 & 2".
--
--   * Vendor.state becomes mandatory. A Vendor with a GSTIN never has to pick
--     it: engines/document_regions.go fills it from the GSTIN before the
--     mandatory check runs.
--   * Every address is Address Line 1 + Address Line 2 + City + State + PIN
--     code. The existing `address` / `shipping_address` field IS line 1 - it
--     is relabelled, not moved, so no stored value changes and every print,
--     connector and report that reads it keeps working.
--   * Location gains a State (filled from its Legal Entity when left blank),
--     and Location / Legal Entity a read-only State Code (KA) that the server
--     sets from the state.
--   * The address block is grouped at the end of each form (display order
--     20+). Only fields still at their shipped label / order are touched - a
--     tenant that already renamed or reordered one keeps its own.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields');

    -- New fields. ON CONFLICT keeps a field a tenant already has.
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Vendor',      'address_line2',          'Address Line 2',          'Data', FALSE, NULL, 21),
        ('Vendor',      'city',                   'City',                    'Data', FALSE, NULL, 22),
        ('Vendor',      'pincode',                'PIN Code',                'Data', FALSE, NULL, 24),
        ('LegalEntity', 'address',                'Address Line 1',          'Data', FALSE, NULL, 20),
        ('LegalEntity', 'address_line2',          'Address Line 2',          'Data', FALSE, NULL, 21),
        ('LegalEntity', 'city',                   'City',                    'Data', FALSE, NULL, 22),
        ('LegalEntity', 'state_code',             'State Code',              'Data', FALSE, NULL, 24),
        ('LegalEntity', 'pincode',                'PIN Code',                'Data', FALSE, NULL, 25),
        ('Location',    'address_line2',          'Address Line 2',          'Data', FALSE, NULL, 21),
        ('Location',    'state',                  'State',                   'Data', FALSE, NULL, 23),
        ('Location',    'state_code',             'State Code',              'Data', FALSE, NULL, 24),
        ('SalesOrder',  'shipping_address_line2', 'Shipping Address Line 2', 'Data', FALSE, NULL, 13),
        ('SalesOrder',  'shipping_city',          'Shipping City',           'Data', FALSE, NULL, 14),
        ('SalesOrder',  'shipping_pincode',       'Shipping PIN Code',       'Data', FALSE, NULL, 18)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    -- Line 1 is the existing field, relabelled where still at shipped text.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET label = 'Address Line 1'
       WHERE doctype_name IN ('Vendor', 'Location') AND fieldname = 'address' AND label IN ('Address', 'Location Address', 'Store Address')
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET label = 'Shipping Address Line 1'
       WHERE doctype_name = 'SalesOrder' AND fieldname = 'shipping_address' AND label = 'Shipping Address'
    $sql$, schema_rec.schema_name);

    -- Vendor State is mandatory (D3: blank, must pick - or a GSTIN fills it).
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET mandatory = TRUE
       WHERE doctype_name = 'Vendor' AND fieldname = 'state'
    $sql$, schema_rec.schema_name);

    -- Group the address block at the end, only from the shipped order.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields f SET display_order = v.new_order
        FROM (VALUES
          ('Vendor', 'address', 9, 20), ('Vendor', 'state', 10, 23),
          ('LegalEntity', 'state', 4, 23),
          ('Location', 'address', 6, 20), ('Location', 'city', 7, 22), ('Location', 'pincode', 6, 25)
        ) AS v(doctype_name, fieldname, old_order, new_order)
       WHERE f.doctype_name = v.doctype_name AND f.fieldname = v.fieldname AND f.display_order = v.old_order
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
