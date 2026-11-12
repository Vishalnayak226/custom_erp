-- Stage 148.1: cost of goods follows the stock in hand.
--
-- One new chart row (5195 COGS Variance, where the share of a bill or late charge
-- that belongs to units already sold posts) and the fields that let a bill list
-- its lines, an item type or vendor set their tolerances, and a credit note name
-- the piece it brings back. No existing row changes and no stock value is written:
-- item_cost.avg_unit_cost_paise as it stands is where the moving average starts.
DO $mig$
DECLARE
    schema_rec RECORD;
BEGIN
    FOR schema_rec IN
        SELECT schema_name FROM information_schema.schemata
        WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
    LOOP
        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
            EXECUTE format($sql$
                INSERT INTO %I.gl_accounts (account_code, account_name, account_type)
                VALUES ('5195', 'COGS Variance', 'Expense')
                ON CONFLICT DO NOTHING
            $sql$, schema_rec.schema_name);
        END IF;

        CONTINUE WHEN NOT EXISTS (SELECT 1 FROM information_schema.tables
                                  WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta');

        EXECUTE format($sql$
            INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
            SELECT d.doctype_name, d.fieldname, d.label, d.fieldtype, FALSE, d.options, d.display_order
            FROM (VALUES
                ('VendorInvoice', 'bill_lines', 'Bill Lines', 'JSONTable', '[{"key":"sku","label":"Item","type":"link","link":"Item","required":true},{"key":"qty","label":"Qty","type":"number","required":true},{"key":"rate","label":"Rate","type":"number"},{"key":"weight_g","label":"Weight (g)","type":"number"}]', 85),
                ('ItemType', 'bill_price_tolerance_percent', 'Bill price tolerance %%', 'Number', NULL, 40),
                ('Vendor', 'bill_weight_tolerance_g', 'Bill weight tolerance (g)', 'Number', NULL, 60),
                ('CreditNote', 'serial_no', 'Piece returned', 'Data', NULL, 40),
                ('CreditNote', 'return_location', 'Back to store', 'Link', 'Location', 41)
            ) AS d(doctype_name, fieldname, label, fieldtype, options, display_order)
            WHERE EXISTS (SELECT 1 FROM %I.doctype_meta m WHERE m.name = d.doctype_name)
            ON CONFLICT (doctype_name, fieldname) DO NOTHING
        $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END LOOP;
END $mig$;
