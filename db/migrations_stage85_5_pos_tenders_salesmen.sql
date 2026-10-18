-- ---------------------------------------------------------------------------
-- Stage 85.E2 / 85.E3 (77.5, 77.6; user, 2026-10-17): the till's tenders and
-- salesmen, under the client's control.
--
-- POSTender: the ways a store takes money - "HDFC Card", "Paytm", "Cash".
-- Each is one of the three kinds the ledger knows (Cash / Card / UPI) and
-- posts to that kind's clearing account (1100 / 1101 / 1102), so the GL does
-- not change; the bill keeps the tender's own name for the tender-wise
-- report and the per-tender close. stores = the store codes that take it
-- (blank: every store). A client with no tender keeps Cash, Card and UPI.
--
-- Salesman (77.0: a separate list, not the Employee master): name, store,
-- Active / Left, one default per store. Location gains salesman_mode -
-- whether the till asks for the salesman (Off / Optional / Mandatory).
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
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('POSTender', 'Sales', 'sales', 'Master'),
        ('Salesman',  'Sales', 'sales', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('POSTender', 'name',            'Tender name',                         'Data',   TRUE,  NULL, 1),
        ('POSTender', 'kind',            'Kind (decides the clearing account)', 'Select', TRUE,  'Cash,Card,UPI', 2),
        ('POSTender', 'needs_reference', 'Ask for a reference (card / UTR)',    'Select', FALSE, 'No,Yes', 3),
        ('POSTender', 'opens_drawer',    'Opens the cash drawer',               'Select', FALSE, 'No,Yes', 4),
        ('POSTender', 'stores',          'Stores (blank = every store; codes, comma separated)', 'Data', FALSE, NULL, 5),
        ('POSTender', 'display_order',   'Order on the till',                   'Number', FALSE, NULL, 6),
        ('POSTender', 'status',          'Status',                              'Select', TRUE,  'Active,Inactive', 7),
        ('Salesman',  'name',            'Salesman name',                       'Data',   TRUE,  NULL, 1),
        ('Salesman',  'store',           'Store',                               'Link',   TRUE,  'Location', 2),
        ('Salesman',  'phone',           'Phone',                               'Data',   FALSE, NULL, 3),
        ('Salesman',  'is_default',      'Default salesman of the store',       'Select', FALSE, 'No,Yes', 4),
        ('Salesman',  'status',          'Status',                              'Select', TRUE,  'Active,Left', 5),
        ('Location',  'salesman_mode',   'Ask the salesman at the till',        'Select', FALSE, 'Off,Optional,Mandatory', 30)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('HR/Admin', 'POSTender', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'POSTender', TRUE, FALSE, FALSE, FALSE),
        ('Cashier', 'POSTender', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin', 'Salesman', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'Salesman', TRUE, TRUE, TRUE, FALSE),
        ('Cashier', 'Salesman', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT v.id, 'POSTender', jsonb_build_object('code', v.id, 'name', v.id, 'kind', v.id, 'needs_reference', v.ref,
               'opens_drawer', v.drawer, 'stores', '', 'display_order', v.ord, 'status', 'Active'), 'Active', 'system'
        FROM (VALUES ('Cash', 'No', 'Yes', 1), ('Card', 'Yes', 'No', 2), ('UPI', 'Yes', 'No', 3)) AS v(id, ref, drawer, ord)
       WHERE NOT EXISTS (SELECT 1 FROM %I.documents d WHERE d.doctype = 'POSTender' AND d.id = v.id)
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
