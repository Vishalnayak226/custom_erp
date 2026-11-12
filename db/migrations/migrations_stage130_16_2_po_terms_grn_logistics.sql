-- Stage 130.16 (74.3.2, BRD 4.10, DEC-19): PO commercial terms and GRN
-- logistics. A Payment Term master (with the usual terms shipped); the PO
-- gains its payment term, delivery terms, who pays the freight, the vendor
-- contact and the quote's validity; the GRN gains the vendor's challan and
-- the transport (LR / AWB, vehicle, transporter, packages). Additive: new
-- doctype, new fields, shipped rows; nothing existing changes.
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
        ('PaymentTerm', 'Procurement', 'procurement', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('PaymentTerm', 'code',        'Code',        'Data',   TRUE,  NULL, 1),
        ('PaymentTerm', 'name',        'Name',        'Data',   TRUE,  NULL, 2),
        ('PaymentTerm', 'net_days',    'Net days',    'Number', TRUE,  NULL, 3),
        ('PaymentTerm', 'description', 'Description', 'Data',   FALSE, NULL, 4),
        ('PurchaseOrder', 'payment_term',      'Payment term',        'Link',   FALSE, 'PaymentTerm', 100),
        ('PurchaseOrder', 'delivery_terms',    'Delivery terms',      'Select', FALSE, 'Door delivery,Ex-works (EXW),Free carrier (FCA),Free on board (FOB),Cost insurance freight (CIF),Delivered at place (DAP),Delivered duty paid (DDP)', 101),
        ('PurchaseOrder', 'freight_by',        'Freight paid by',     'Select', FALSE, 'Vendor,Buyer', 102),
        ('PurchaseOrder', 'vendor_contact',    'Vendor contact',      'Data',   FALSE, NULL, 103),
        ('PurchaseOrder', 'quote_valid_until', 'Quote valid until',   'Date',   FALSE, NULL, 104),
        ('GRN', 'vendor_challan_no',   'Vendor challan no',   'Data',   FALSE, NULL, 100),
        ('GRN', 'vendor_challan_date', 'Vendor challan date', 'Date',   FALSE, NULL, 101),
        ('GRN', 'lr_awb_no',           'LR / AWB no',         'Data',   FALSE, NULL, 102),
        ('GRN', 'vehicle_no',          'Vehicle no',          'Data',   FALSE, NULL, 103),
        ('GRN', 'transporter',         'Transporter',         'Data',   FALSE, NULL, 104),
        ('GRN', 'package_count',       'Packages',            'Number', FALSE, NULL, 105)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    IF EXISTS (SELECT 1 FROM information_schema.tables
                WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
          ('HR/Admin', 'PaymentTerm', TRUE, TRUE, TRUE, FALSE),
          ('Store Manager', 'PaymentTerm', TRUE, FALSE, FALSE, FALSE),
          ('Accountant', 'PaymentTerm', TRUE, TRUE, TRUE, FALSE)
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');
    EXECUTE format($sql$
      INSERT INTO %1$I.documents (id, doctype, data, status, created_by)
      SELECT v.id, 'PaymentTerm', jsonb_build_object('code', v.id, 'name', v.name, 'net_days', v.days, 'description', v.description, 'status', 'Active'), 'Active', 'system'
      FROM (VALUES
        ('ADVANCE', 'Advance before dispatch', 0, 'Paid in full before the goods leave the vendor.'),
        ('NET0', 'Due on receipt', 0, 'Paid when the goods and bill arrive.'),
        ('NET15', 'Net 15 days', 15, 'Paid 15 days after the bill.'),
        ('NET30', 'Net 30 days', 30, 'Paid 30 days after the bill.'),
        ('NET45', 'Net 45 days', 45, 'Paid 45 days after the bill.'),
        ('NET60', 'Net 60 days', 60, 'Paid 60 days after the bill.')
      ) AS v(id, name, days, description)
      ON CONFLICT DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
