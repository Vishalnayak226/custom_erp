-- Stage 35.8: Settlement/payment reconciliation, the "UniReco" gap.
-- MarketplaceSettlementLine is CSV-imported through the existing generic
-- POST /api/v1/import/{doctype} + BulkImportCSV path (engines/import.go) -
-- no new import endpoint needed, just doctype_meta/doctype_fields/
-- role_permissions like any other bulk-importable master. Matching and GL
-- posting is engines/settlement_reconciliation.go.

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('MarketplaceSettlementLine', 'Finance', 'finance', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('MarketplaceSettlementLine', 'channel', 'Channel', 'Data', TRUE, NULL, 1),
('MarketplaceSettlementLine', 'channel_order_id', 'Channel Order ID', 'Data', TRUE, NULL, 2),
('MarketplaceSettlementLine', 'settlement_batch_id', 'Settlement Batch ID', 'Data', TRUE, NULL, 3),
('MarketplaceSettlementLine', 'settlement_date', 'Settlement Date', 'Data', TRUE, NULL, 4),
('MarketplaceSettlementLine', 'gross_amount', 'Gross Amount', 'Number', TRUE, NULL, 5),
('MarketplaceSettlementLine', 'commission', 'Commission', 'Number', FALSE, NULL, 6),
('MarketplaceSettlementLine', 'shipping_fee', 'Shipping Fee', 'Number', FALSE, NULL, 7),
('MarketplaceSettlementLine', 'other_fee', 'Other Fee', 'Number', FALSE, NULL, 8),
('MarketplaceSettlementLine', 'tds', 'TDS Deducted', 'Number', FALSE, NULL, 9),
('MarketplaceSettlementLine', 'tcs', 'TCS Deducted', 'Number', FALSE, NULL, 10),
('MarketplaceSettlementLine', 'net_payout', 'Net Payout', 'Number', TRUE, NULL, 11),
('MarketplaceSettlementLine', 'notes', 'Notes', 'Text', FALSE, NULL, 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin', 'MarketplaceSettlementLine', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'MarketplaceSettlementLine', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET allow_read=EXCLUDED.allow_read, allow_create=EXCLUDED.allow_create, allow_update=EXCLUDED.allow_update, allow_delete=EXCLUDED.allow_delete;

-- New Chart of Accounts entries the settlement GL split needs: the two tax
-- credits a marketplace deducts at source (recoverable, hence Asset not
-- Expense), the non-commission fee bucket, and the variance write-off
-- expense used when a held Variance/Disputed line is closed out.
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1310', 'TDS Receivable', 'Asset'),
('1320', 'TCS Receivable (GST)', 'Asset'),
('5210', 'Marketplace Shipping & Other Fees', 'Expense'),
('5270', 'Settlement Variance Written Off', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_settlement_line_channel_order
ON tenant_default.documents ((data->>'channel'), (data->>'channel_order_id'))
WHERE doctype = 'MarketplaceSettlementLine' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_settlement_line_match_status
ON tenant_default.documents ((data->>'match_status'))
WHERE doctype = 'MarketplaceSettlementLine' AND deleted_at IS NULL;

-- ReasonCode.category gains 'Settlement' - append-only, same precedent
-- migrations_stage26_5_wms_enterprise.sql set for 'Cycle Count Variance'.
-- RaiseSettlementDispute/WriteOffSettlementVariance (engines/settlement_reconciliation.go)
-- require a ReasonCode of this category, same requireActiveReasonCode gate
-- Return/Cancellation/Hold already use.
UPDATE tenant_default.doctype_fields
SET options = options || ',Settlement'
WHERE doctype_name = 'ReasonCode' AND fieldname = 'category'
  AND options NOT LIKE '%Settlement%';

DO $$
DECLARE
  schema_rec RECORD;
  doctype_list text[] := ARRAY['MarketplaceSettlementLine'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN CONTINUE; END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta(name,module,module_key,document_type)
      SELECT name,module,module_key,document_type FROM tenant_default.doctype_meta WHERE name=ANY($1)
      ON CONFLICT(name) DO UPDATE SET module=EXCLUDED.module,module_key=EXCLUDED.module_key,document_type=EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING doctype_list;
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields(doctype_name,fieldname,label,fieldtype,mandatory,options,display_order)
      SELECT doctype_name,fieldname,label,fieldtype,mandatory,options,display_order FROM tenant_default.doctype_fields WHERE doctype_name=ANY($1)
      ON CONFLICT(doctype_name,fieldname) DO UPDATE SET label=EXCLUDED.label,fieldtype=EXCLUDED.fieldtype,mandatory=EXCLUDED.mandatory,options=EXCLUDED.options,display_order=EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING doctype_list;
    EXECUTE format($f$
      INSERT INTO %I.role_permissions(role,doctype_name,allow_read,allow_create,allow_update,allow_delete)
      SELECT role,doctype_name,allow_read,allow_create,allow_update,allow_delete FROM tenant_default.role_permissions WHERE doctype_name=ANY($1)
      ON CONFLICT(role,doctype_name) DO UPDATE SET allow_read=EXCLUDED.allow_read,allow_create=EXCLUDED.allow_create,allow_update=EXCLUDED.allow_update,allow_delete=EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING doctype_list;
    EXECUTE format($f$
      INSERT INTO %I.gl_accounts(account_code,account_name,account_type)
      SELECT account_code,account_name,account_type FROM tenant_default.gl_accounts WHERE account_code IN ('1310','1320','5210','5270')
      ON CONFLICT(account_code) DO NOTHING
    $f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_settlement_line_channel_order ON %I.documents ((data->>'channel'),(data->>'channel_order_id')) WHERE doctype='MarketplaceSettlementLine' AND deleted_at IS NULL$f$, schema_rec.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS idx_settlement_line_match_status ON %I.documents ((data->>'match_status')) WHERE doctype='MarketplaceSettlementLine' AND deleted_at IS NULL$f$, schema_rec.schema_name);
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NOT NULL THEN
      EXECUTE format($f$UPDATE %I.doctype_fields SET options = options || ',Settlement' WHERE doctype_name = 'ReasonCode' AND fieldname = 'category' AND options NOT LIKE '%%Settlement%%'$f$, schema_rec.schema_name);
    END IF;
  END LOOP;
END $$;
