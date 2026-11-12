-- ---------------------------------------------------------------------------
-- Stage 85.C (user, 2026-10-17 and 2026-10-18): the customer at the till.
--
-- "Customer name + number, address, DOB, DOA, gender, PAN ... There will be
-- gst bill also" - and "Phone number is unque. Ask Always."
--
-- Customer gains the address block in Stage 76's shape (Address Line 1 is the
-- new `address` field, then Line 2, City, State, State Code, PIN code),
-- anniversary, gender, PAN and GSTIN. A Customer with a `state` and a `gstin`
-- field gets the existing GSTIN -> State fill and mismatch check
-- (engines/document_regions.go) with no code of its own.
--
-- walk_in / walk_in_store mark each store's default customer (77.0: one per
-- store, a placeholder number never printed). Engine-written; shown on the
-- master so an administrator can see which record it is.
--
-- All fields optional and additive: no existing customer changes.
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
        ('Customer', 'gender',              'Gender',              'Select', FALSE, 'Female,Male,Other', 13),
        ('Customer', 'date_of_anniversary', 'Anniversary',         'Date',   FALSE, NULL, 14),
        ('Customer', 'address',             'Address Line 1',      'Data',   FALSE, NULL, 20),
        ('Customer', 'address_line2',       'Address Line 2',      'Data',   FALSE, NULL, 21),
        ('Customer', 'city',                'City',                'Data',   FALSE, NULL, 22),
        ('Customer', 'state',               'State',               'Data',   FALSE, NULL, 23),
        ('Customer', 'state_code',          'State Code',          'Data',   FALSE, NULL, 24),
        ('Customer', 'pincode',             'PIN Code',            'Data',   FALSE, NULL, 25),
        ('Customer', 'pan',                 'PAN',                 'Data',   FALSE, NULL, 30),
        ('Customer', 'gstin',               'GSTIN',               'Data',   FALSE, NULL, 31),
        ('Customer', 'walk_in',             'Store walk-in customer', 'Select', FALSE, 'Yes,No', 70),
        ('Customer', 'walk_in_store',       'Walk-in for store',   'Data',   FALSE, NULL, 71)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
