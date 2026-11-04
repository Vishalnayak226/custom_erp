-- ---------------------------------------------------------------------------
-- Stage 148.8 (builder h): charge codes that cost themselves.
--
-- TradeChargeCode is the one master for buying and selling (GL, GST, payee, into
-- cost, basis, scope, repeat). The name is not ChargeCode because that doctype
-- is already the warehouse labour billing code of Stage 42.6, a different thing.
-- TradeChargeRule puts a code on lines by vendor group x item group.
-- TradeChargeEntry is written by the engine: one row per charge a receipt
-- or a transfer carried, with each line's share or the reason it has none.
-- Vendor.freight_terms says who bears freight and insurance. The charges column
-- on PurchaseOrder, GRN and TransferOrder holds the rows a person typed.
--
-- Metadata only, replayed into every tenant schema. No document, posting or
-- master row is written or changed: with no charge code on file every receipt
-- posts exactly what it always has.
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
      VALUES
        ('TradeChargeCode', 'Procurement', 'procurement', 'Master'),
        ('TradeChargeRule', 'Procurement', 'procurement', 'Master'),
        ('TradeChargeEntry', 'Procurement', 'procurement', 'Master')
      ON CONFLICT (name) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('TradeChargeCode', 'code', 'Code', 'Data', TRUE, NULL, 1),
        ('TradeChargeCode', 'charge_name', 'Name', 'Data', TRUE, NULL, 2),
        ('TradeChargeCode', 'used_for', 'Used for', 'Select', FALSE, 'Buying,Selling,Both', 3),
        ('TradeChargeCode', 'charge_kind', 'Kind', 'Select', FALSE, ',Freight,Insurance', 4),
        ('TradeChargeCode', 'payee', 'Paid to', 'Select', FALSE, ',Vendor,Third party', 5),
        ('TradeChargeCode', 'carrier', 'Carrier', 'Link', FALSE, 'Vendor', 6),
        ('TradeChargeCode', 'clearing_account', 'Clearing account', 'Data', FALSE, NULL, 7),
        ('TradeChargeCode', 'into_cost', 'Into item cost', 'Select', FALSE, ',Yes,No', 8),
        ('TradeChargeCode', 'gl_account', 'GL account', 'Data', FALSE, NULL, 9),
        ('TradeChargeCode', 'gst_rate', 'GST rate %%', 'Number', FALSE, NULL, 10),
        ('TradeChargeCode', 'basis', 'Split by', 'Select', FALSE, ',Value,Qty,Weight', 11),
        ('TradeChargeCode', 'scope', 'Lines', 'Select', FALSE, ',All lines,Stocked lines only', 12),
        ('TradeChargeCode', 'repeat_on', 'Repeat', 'Select', FALSE, ',Once,Every receipt', 13),
        ('TradeChargeCode', 'status', 'Status', 'Select', FALSE, 'Active,Inactive', 14),

        ('TradeChargeRule', 'rule_name', 'Name', 'Data', TRUE, NULL, 1),
        ('TradeChargeRule', 'vendor_group', 'Vendor group', 'Data', FALSE, NULL, 2),
        ('TradeChargeRule', 'item_group', 'Item group', 'Data', FALSE, NULL, 3),
        ('TradeChargeRule', 'charge_code', 'Charge code', 'Link', TRUE, 'TradeChargeCode', 4),
        ('TradeChargeRule', 'method', 'Method', 'Select', TRUE, 'Percent,Per unit,Fixed', 5),
        ('TradeChargeRule', 'rate', 'Rate', 'Number', TRUE, NULL, 6),
        ('TradeChargeRule', 'status', 'Status', 'Select', FALSE, 'Active,Inactive', 7),

        ('TradeChargeEntry', 'code', 'Entry', 'Data', FALSE, NULL, 1),
        ('TradeChargeEntry', 'source_id', 'Receipt', 'Data', FALSE, NULL, 2),
        ('TradeChargeEntry', 'charge_label', 'Charge', 'Data', FALSE, NULL, 3),
        ('TradeChargeEntry', 'amount', 'Amount', 'Number', FALSE, NULL, 4),
        ('TradeChargeEntry', 'status', 'Status', 'Select', FALSE, 'Estimated,Posted,Trued up,Reversed,Skipped', 5),
        ('TradeChargeEntry', 'vendor_id', 'Party', 'Data', FALSE, NULL, 6),
        ('TradeChargeEntry', 'payee', 'Paid to', 'Data', FALSE, NULL, 7),
        ('TradeChargeEntry', 'open_amount', 'Awaiting bill', 'Number', FALSE, NULL, 8),
        ('TradeChargeEntry', 'charge_code', 'Charge code', 'Data', FALSE, NULL, 9),
        ('TradeChargeEntry', 'location', 'Location', 'Data', FALSE, NULL, 10),
        ('TradeChargeEntry', 'note', 'Lines', 'Data', FALSE, NULL, 11),

        ('Vendor', 'freight_terms', 'Freight terms', 'Select', FALSE, ',FOB,CIF', 86),
        ('PurchaseOrder', 'charges', 'Charges', 'JSONTable', FALSE,
         '[{"key":"charge_code","label":"Charge","type":"link","link":"TradeChargeCode","required":true},{"key":"amount","label":"Amount","type":"number","required":true},{"key":"only_lines","label":"Only SKUs","type":"data"},{"key":"exclude_lines","label":"Leave out SKUs","type":"data"}]', 110),
        ('GRN', 'charges', 'Charges', 'JSONTable', FALSE,
         '[{"key":"charge_code","label":"Charge","type":"link","link":"TradeChargeCode","required":true},{"key":"amount","label":"Amount","type":"number","required":true},{"key":"only_lines","label":"Only SKUs","type":"data"},{"key":"exclude_lines","label":"Leave out SKUs","type":"data"}]', 110),
        ('TransferOrder', 'charges', 'Charges', 'JSONTable', FALSE,
         '[{"key":"charge_code","label":"Charge","type":"link","link":"TradeChargeCode","required":true},{"key":"amount","label":"Amount","type":"number","required":true},{"key":"only_lines","label":"Only SKUs","type":"data"},{"key":"exclude_lines","label":"Leave out SKUs","type":"data"}]', 110)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('Super Admin', 'TradeChargeCode', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'TradeChargeCode', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'TradeChargeCode', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'TradeChargeCode', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'TradeChargeRule', TRUE, TRUE, TRUE, TRUE),
        ('HR/Admin', 'TradeChargeRule', TRUE, TRUE, TRUE, FALSE),
        ('Accountant', 'TradeChargeRule', TRUE, TRUE, TRUE, FALSE),
        ('Store Manager', 'TradeChargeRule', TRUE, FALSE, FALSE, FALSE),
        ('Super Admin', 'TradeChargeEntry', TRUE, FALSE, FALSE, FALSE),
        ('HR/Admin', 'TradeChargeEntry', TRUE, FALSE, FALSE, FALSE),
        ('Accountant', 'TradeChargeEntry', TRUE, FALSE, FALSE, FALSE),
        ('Store Manager', 'TradeChargeEntry', TRUE, FALSE, FALSE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
