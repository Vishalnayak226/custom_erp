-- ---------------------------------------------------------------------------
-- Ask 169, items 73.4.2.6 and 73.4.2.7: carrier claims and freight invoice
-- reconciliation.
--
--   * CarrierClaim: a claim for a lost, damaged or short shipment, raised
--     against its LogisticsBooking and taken Open -> Filed -> Settled, or
--     Rejected. Every move is the engine's (OMS -> Freight), so the doctype
--     is strict with no generic rule.
--   * FreightInvoiceLine: one AWB on a carrier's freight invoice, loaded with
--     the standard CSV import; reconciliation matches it to its booking and
--     sets expected, difference and status. Status is the engine's too.
--
-- Additive and safe to run twice.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name = 'tenant_default'
        OR (schema_name LIKE 'tenant\_%' ESCAPE '\')
  LOOP
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');
    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
        ('CarrierClaim',       'OMS', 'oms', 'Transaction'),
        ('FreightInvoiceLine', 'OMS', 'oms', 'Transaction')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
        ('CarrierClaim', 'code',           'Claim',             'Data',     FALSE, NULL, 1),
        ('CarrierClaim', 'booking_id',     'Logistics Booking', 'Link',     TRUE,  'LogisticsBooking', 2),
        ('CarrierClaim', 'order_id',       'Order',             'Data',     FALSE, NULL, 3),
        ('CarrierClaim', 'carrier',        'Carrier',           'Data',     FALSE, NULL, 4),
        ('CarrierClaim', 'awb',            'AWB',               'Data',     FALSE, NULL, 5),
        ('CarrierClaim', 'claim_type',     'Type',              'Select',   TRUE,  'Lost,Damaged,Short', 6),
        ('CarrierClaim', 'amount_claimed', 'Claimed',           'Currency', TRUE,  NULL, 7),
        ('CarrierClaim', 'claim_ref',      'Carrier reference', 'Data',     FALSE, NULL, 8),
        ('CarrierClaim', 'amount_settled', 'Settled',           'Currency', FALSE, NULL, 9),
        ('CarrierClaim', 'settled_on',     'Settled on',        'Date',     FALSE, NULL, 10),
        ('CarrierClaim', 'notes',          'Notes',             'Text',     FALSE, NULL, 11),
        ('CarrierClaim', 'status',         'Status',            'Select',   FALSE, 'Open,Filed,Settled,Rejected', 12),
        ('FreightInvoiceLine', 'invoice_no',      'Invoice',      'Data',     TRUE,  NULL, 1),
        ('FreightInvoiceLine', 'carrier',         'Carrier',      'Data',     TRUE,  NULL, 2),
        ('FreightInvoiceLine', 'invoice_date',    'Invoice date', 'Date',     FALSE, NULL, 3),
        ('FreightInvoiceLine', 'awb',             'AWB',          'Data',     TRUE,  NULL, 4),
        ('FreightInvoiceLine', 'charged_amount',  'Charged',      'Currency', TRUE,  NULL, 5),
        ('FreightInvoiceLine', 'booking_id',      'Booking',      'Link',     FALSE, 'LogisticsBooking', 6),
        ('FreightInvoiceLine', 'expected_amount', 'Expected',     'Currency', FALSE, NULL, 7),
        ('FreightInvoiceLine', 'difference',      'Difference',   'Currency', FALSE, NULL, 8),
        ('FreightInvoiceLine', 'status',          'Status',       'Select',   FALSE, 'Unmatched,Matched,Overcharged,Undercharged,No booking,Duplicate', 9)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_meta SET strict_status_transitions = TRUE
       WHERE name IN ('CarrierClaim', 'FreightInvoiceLine')
    $sql$, schema_rec.schema_name);
    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
       WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions');
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
        ('Super Admin',   'CarrierClaim',       TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'CarrierClaim',       TRUE, TRUE, TRUE, FALSE),
        ('Super Admin',   'FreightInvoiceLine', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'FreightInvoiceLine', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END $$;
