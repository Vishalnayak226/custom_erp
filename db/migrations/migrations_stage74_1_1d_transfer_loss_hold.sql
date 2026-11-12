-- ---------------------------------------------------------------------------
-- Stage 74.1.1 (BRD 4.9, FR-RSN-03): a transfer receipt that books damaged,
-- lost or short units under a reason code of severity High waits for approval
-- as a Transfer Loss Hold; on approval the receipt posts as entered. The
-- engine writes these documents, the record API only reads them. Additive:
-- one record type, its fields and read access. No client data changes.
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
        ('TransferLossHold', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('TransferLossHold', 'code',              'Hold no',        'Data',   TRUE,  NULL, 1),
        ('TransferLossHold', 'transfer_order_id', 'Transfer',       'Data',   TRUE,  NULL, 2),
        ('TransferLossHold', 'from_warehouse',    'From',           'Link',   FALSE, 'Location', 3),
        ('TransferLossHold', 'to_warehouse',      'To',             'Link',   FALSE, 'Location', 4),
        ('TransferLossHold', 'reason_codes',      'Reason codes',   'Data',   FALSE, NULL, 5),
        ('TransferLossHold', 'amount',            'Loss value',     'Number', FALSE, NULL, 6),
        ('TransferLossHold', 'status',            'Status',         'Select', TRUE,  'Draft,Pending Approval,Approved,Rejected,Posted', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('HR/Admin', 'TransferLossHold', TRUE, TRUE, FALSE, FALSE),
          ('Store Manager', 'TransferLossHold', TRUE, TRUE, FALSE, FALSE),
          ('Accountant', 'TransferLossHold', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
