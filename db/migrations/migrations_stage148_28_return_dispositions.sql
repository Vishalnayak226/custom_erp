-- ---------------------------------------------------------------------------
-- Stage 148.28 (builder h): returns drive the next document.
--
-- ReturnDispositionRule: one row per QC disposition (Sellable, Damaged, ...) saying
-- what that disposition drafts (credit note, replacement order, return to vendor,
-- scrap to quarantine) and which charge code carries its restocking fee. With no
-- row on file a return behaves exactly as it always has.
-- ReturnRequest gains the RMA no., the reason and what was drafted; CreditNote links
-- back to its return; RefundRequest shows the fee kept.
--
-- Metadata only, replayed into every tenant schema. No document or posting row is
-- written or changed.
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
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('ReturnDispositionRule', 'OMS', 'oms', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT d.doctype_name, d.fieldname, d.label, d.fieldtype, d.mandatory, d.options, d.display_order
      FROM (VALUES
        ('ReturnDispositionRule', 'disposition', 'Disposition', 'Select', TRUE, 'Sellable,Damaged,Repairable,Wrong-Item', 1),
        ('ReturnDispositionRule', 'action', 'Drafts', 'Select', FALSE, ',Credit,Replace,Return to vendor,Scrap', 2),
        ('ReturnDispositionRule', 'charge_code', 'Fee charge code', 'Link', FALSE, 'TradeChargeCode', 3),
        ('ReturnDispositionRule', 'fee_percent', 'Fee %%', 'Number', FALSE, NULL, 4),
        ('ReturnDispositionRule', 'status', 'Status', 'Select', FALSE, 'Active,Inactive', 5),
        ('ReturnRequest', 'rma_no', 'RMA no.', 'Data', FALSE, NULL, 40),
        ('ReturnRequest', 'reason_code', 'Reason code', 'Link', FALSE, 'ReasonCode', 41),
        ('ReturnRequest', 'reason', 'Reason', 'Data', FALSE, NULL, 42),
        ('CreditNote', 'return_request_id', 'Return', 'Link', FALSE, 'ReturnRequest', 42),
        ('RefundRequest', 'restocking_fee', 'Restocking fee', 'Number', FALSE, NULL, 40)
      ) AS d(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = d.doctype_name)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Super Admin', 'ReturnDispositionRule', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'ReturnDispositionRule', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'ReturnDispositionRule', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'ReturnDispositionRule', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
