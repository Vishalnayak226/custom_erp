-- Stage 130.16 (74.3.1, BRD 4.1, DEC-01/02): the Delivery Challan (DC
-- series) and the Challan Write-off. inventory_availability gains the "with
-- party" pool: returnable goods out on a challan - still owned and in
-- on_hand, out of available, not in the store. Engine-written documents (their
-- routes validate and post); the record API reads them. Additive.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'inventory_availability') THEN
      EXECUTE format('ALTER TABLE %I.inventory_availability ADD COLUMN IF NOT EXISTS with_party INT NOT NULL DEFAULT 0', schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('DeliveryChallan', 'Inventory', 'inventory', 'Transaction'),
        ('ChallanWriteOff', 'Inventory', 'inventory', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('DeliveryChallan', 'code',            'Challan no',        'Data',      TRUE,  NULL, 1),
        ('DeliveryChallan', 'purpose',         'Purpose',           'Select',    TRUE,  'Job work,Repair,Exhibition,Sample,Returnable - other,Non-returnable,Other', 2),
        ('DeliveryChallan', 'location',        'From store',        'Link',      TRUE,  'Location', 3),
        ('DeliveryChallan', 'party_type',      'Party type',        'Select',    FALSE, 'Vendor,Customer,Job worker,Other', 4),
        ('DeliveryChallan', 'party_name',      'Consignee',         'Data',      TRUE,  NULL, 5),
        ('DeliveryChallan', 'party_gstin',     'Consignee GSTIN',   'Data',      FALSE, NULL, 6),
        ('DeliveryChallan', 'party_address',   'Consignee address', 'Data',      FALSE, NULL, 7),
        ('DeliveryChallan', 'party_state',     'Consignee state',   'Data',      FALSE, NULL, 8),
        ('DeliveryChallan', 'expected_return', 'Expected back',     'Date',      FALSE, NULL, 9),
        ('DeliveryChallan', 'items',           'Items',             'JSONTable', TRUE,
         '[{"key":"sku","label":"Item","type":"data","required":true},{"key":"qty","label":"Qty","type":"number","required":true},{"key":"batch_no","label":"Lot","type":"data"},{"key":"declared_value","label":"Value","type":"number"},{"key":"expected_return","label":"Expected back","type":"data"}]', 10),
        ('DeliveryChallan', 'amount',          'Declared value',    'Number',    FALSE, NULL, 11),
        ('DeliveryChallan', 'ewb_required',    'E-way bill',        'Data',      FALSE, NULL, 12),
        ('DeliveryChallan', 'transport_mode',  'Transport mode',    'Data',      FALSE, NULL, 13),
        ('DeliveryChallan', 'lr_awb_no',       'LR / AWB no',       'Data',      FALSE, NULL, 14),
        ('DeliveryChallan', 'vehicle_no',      'Vehicle no',        'Data',      FALSE, NULL, 15),
        ('DeliveryChallan', 'transporter',     'Transporter',       'Data',      FALSE, NULL, 16),
        ('DeliveryChallan', 'status',          'Status',            'Select',    TRUE,  'Draft,Pending Approval,Approved,Rejected,Dispatched,Partly Returned,Returned,Closed,Cancelled', 17),
        ('ChallanWriteOff', 'code',            'Write-off no',      'Data',      TRUE,  NULL, 1),
        ('ChallanWriteOff', 'challan_number',  'Challan',           'Data',      TRUE,  NULL, 2),
        ('ChallanWriteOff', 'location',        'Store',             'Link',      TRUE,  'Location', 3),
        ('ChallanWriteOff', 'sku',             'Item',              'Data',      TRUE,  NULL, 4),
        ('ChallanWriteOff', 'qty',             'Qty',               'Number',    TRUE,  NULL, 5),
        ('ChallanWriteOff', 'kind',            'What happened',     'Select',    TRUE,  'Lost,Kept by party', 6),
        ('ChallanWriteOff', 'reason',          'Reason',            'Data',      TRUE,  NULL, 7),
        ('ChallanWriteOff', 'amount',          'Value',             'Number',    FALSE, NULL, 8),
        ('ChallanWriteOff', 'status',          'Status',            'Select',    TRUE,  'Draft,Pending Approval,Approved,Rejected,Posted', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('HR/Admin', 'DeliveryChallan', TRUE, TRUE, TRUE, FALSE),
          ('Store Manager', 'DeliveryChallan', TRUE, TRUE, TRUE, FALSE),
          ('Accountant', 'DeliveryChallan', TRUE, FALSE, FALSE, FALSE),
          ('HR/Admin', 'ChallanWriteOff', TRUE, TRUE, FALSE, FALSE),
          ('Store Manager', 'ChallanWriteOff', TRUE, TRUE, FALSE, FALSE),
          ('Accountant', 'ChallanWriteOff', TRUE, FALSE, FALSE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
