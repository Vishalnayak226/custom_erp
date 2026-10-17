-- ---------------------------------------------------------------------------
-- Stage 72.1 - Item & Material Master: eight independent classification
-- dimensions (the user's blueprint, 2026-10-17: "Do not build Asset,
-- Non-Asset, Gift, Backflush, Voucher and Sellable as one mutually exclusive
-- item-type list"). engines/item_policy.go resolves them.
--
--   * ItemType (the reusable profile) gains commercial_class, stock_tracking,
--     valuation, accounting_treatment, sellability, bill_mode, capture_method,
--     consumption_method and expense_account.
--   * Item gains the same nine as overrides; "Item type default" inherits.
--   * The seven Stage 60 types get dimension values ONLY where none are set,
--     chosen to keep today's behaviour (Gift / Free With Item become Promo
--     only, which the till could not sell before this Stage anyway - it
--     refused every zero-priced line).
--   * Five new profiles: Packaging, Consumable - Periodic, Service,
--     Non-Stock Expense, Gift Card, Virtual Kit.
--   * GL: 5160 Packaging Consumption, 5170 Promotional / GWP Expense,
--     5180 Consumables Expense, 2610 Gift Card / Stored Value Liability.
--
-- Additive and idempotent, replayed into every tenant schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields') THEN
      EXECUTE format($sql$
        INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT v.* FROM (VALUES
          ('ItemType', 'commercial_class', 'Commercial class', 'Select', FALSE, 'Merchandise,Accessory,Promotional / GWP,Packaging,Consumable,Service,Stored Value,Fixed Asset,Internal / MRO,Bundle / Kit', 20),
          ('ItemType', 'stock_tracking', 'Stock tracking', 'Select', FALSE, 'Tracked,Not tracked', 21),
          ('ItemType', 'valuation', 'Valuation', 'Select', FALSE, 'Valuated,Non-valuated,Not applicable', 22),
          ('ItemType', 'accounting_treatment', 'Accounting treatment', 'Select', FALSE, 'Inventory asset,Expense,Fixed asset,Stored value liability,Service', 23),
          ('ItemType', 'sellability', 'Sellability at POS', 'Select', FALSE, 'Normal sale,Promo only,Not sellable,Stored-value issue only', 24),
          ('ItemType', 'bill_mode', 'Customer bill', 'Select', FALSE, 'Visible line,Hidden (internal),Separate receipt', 25),
          ('ItemType', 'capture_method', 'POS capture', 'Select', FALSE, 'Scan,Quick select,Auto,Manual,None', 26),
          ('ItemType', 'consumption_method', 'Consumption', 'Select', FALSE, 'Sale issue,Auto backflush,Manual issue,Periodic,Production,None', 27),
          ('ItemType', 'expense_account', 'Expense account (expensed issues)', 'Data', FALSE, NULL, 28),
          ('Item', 'commercial_class', 'Commercial class', 'Select', FALSE, 'Item type default,Merchandise,Accessory,Promotional / GWP,Packaging,Consumable,Service,Stored Value,Fixed Asset,Internal / MRO,Bundle / Kit', 60),
          ('Item', 'stock_tracking', 'Stock tracking', 'Select', FALSE, 'Item type default,Tracked,Not tracked', 61),
          ('Item', 'valuation', 'Valuation', 'Select', FALSE, 'Item type default,Valuated,Non-valuated,Not applicable', 62),
          ('Item', 'accounting_treatment', 'Accounting treatment', 'Select', FALSE, 'Item type default,Inventory asset,Expense,Fixed asset,Stored value liability,Service', 63),
          ('Item', 'sellability', 'Sellability at POS', 'Select', FALSE, 'Item type default,Normal sale,Promo only,Not sellable,Stored-value issue only', 64),
          ('Item', 'bill_mode', 'Customer bill', 'Select', FALSE, 'Item type default,Visible line,Hidden (internal),Separate receipt', 65),
          ('Item', 'capture_method', 'POS capture', 'Select', FALSE, 'Item type default,Scan,Quick select,Auto,Manual,None', 66),
          ('Item', 'consumption_method', 'Consumption', 'Select', FALSE, 'Item type default,Sale issue,Auto backflush,Manual issue,Periodic,Production,None', 67),
          ('Item', 'expense_account', 'Expense account (blank = item type''s)', 'Data', FALSE, NULL, 68)
        ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = v.doctype_name)
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name, schema_rec.schema_name);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'gl_accounts') THEN
      EXECUTE format($sql$
        INSERT INTO %I.gl_accounts (account_code, account_name, account_type) VALUES
          ('5160', 'Packaging Consumption', 'Expense'),
          ('5170', 'Promotional / GWP Expense', 'Expense'),
          ('5180', 'Consumables Expense', 'Expense'),
          ('2610', 'Gift Card / Stored Value Liability', 'Liability')
        ON CONFLICT (account_code) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;

    CONTINUE WHEN NOT EXISTS (
      SELECT 1 FROM information_schema.tables
      WHERE table_schema = schema_rec.schema_name AND table_name = 'documents');

    -- The seven Stage 60 types: dimensions only where the row has none, so a
    -- client that already set its own keeps them.
    EXECUTE format($sql$
      UPDATE %I.documents d SET data = d.data || t.dims, updated_at = CURRENT_TIMESTAMP
        FROM (VALUES
          ('Product',        '{"commercial_class":"Merchandise","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Normal sale","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"Sale issue"}'::jsonb),
          ('Accessory',      '{"commercial_class":"Accessory","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Normal sale","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"Sale issue"}'::jsonb),
          ('Non-Asset',      '{"commercial_class":"Consumable","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Normal sale","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"Sale issue"}'::jsonb),
          ('Free With Item', '{"commercial_class":"Promotional / GWP","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Promo only","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"Sale issue","expense_account":"5170"}'::jsonb),
          ('Gift',           '{"commercial_class":"Promotional / GWP","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Promo only","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"Sale issue","expense_account":"5170"}'::jsonb),
          ('Backflush',      '{"commercial_class":"Consumable","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Normal sale","bill_mode":"Visible line","capture_method":"Auto","consumption_method":"Auto backflush"}'::jsonb),
          ('Fixed Asset',    '{"commercial_class":"Fixed Asset","stock_tracking":"Not tracked","valuation":"Not applicable","accounting_treatment":"Fixed asset","sellability":"Not sellable","bill_mode":"Visible line","capture_method":"None","consumption_method":"None"}'::jsonb)
        ) AS t(code, dims)
       WHERE d.doctype = 'ItemType' AND d.id = t.code AND d.deleted_at IS NULL
         AND NOT (d.data ? 'commercial_class')
    $sql$, schema_rec.schema_name);

    -- New profiles following the blueprint's section 23 examples.
    EXECUTE format($sql$
      INSERT INTO %I.documents (id, doctype, data, status, created_by)
      SELECT t.code, 'ItemType',
             jsonb_build_object('code', t.code, 'description', t.description,
               'barcode_generate_at', 'Client default', 'barcode_per_receipt_date', 'Fixed per SKU',
               'sell_at_zero_price', 'No', 'needs_parent_item', 'No', 'backflush', 'No', 'fixed_asset', 'No',
               'status', 'Active') || t.dims,
             'Active', 'system'
        FROM (VALUES
          ('Packaging', 'Boxes, bags and wrapping used with a sale - consumed automatically by Backflush Rules, never sold',
           '{"commercial_class":"Packaging","stock_tracking":"Tracked","valuation":"Valuated","accounting_treatment":"Inventory asset","sellability":"Not sellable","bill_mode":"Hidden (internal)","capture_method":"Auto","consumption_method":"Auto backflush","expense_account":"5160"}'::jsonb),
          ('Consumable - Periodic', 'Butter paper, tape, ribbon - counted and consumed periodically, expensed at receipt',
           '{"commercial_class":"Consumable","stock_tracking":"Tracked","valuation":"Non-valuated","accounting_treatment":"Expense","sellability":"Not sellable","bill_mode":"Hidden (internal)","capture_method":"None","consumption_method":"Periodic","expense_account":"5180"}'::jsonb),
          ('Service', 'Repair, installation, freight - no stock is kept',
           '{"commercial_class":"Service","stock_tracking":"Not tracked","valuation":"Not applicable","accounting_treatment":"Service","sellability":"Normal sale","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"None"}'::jsonb),
          ('Non-Stock Expense', 'Bought and expensed straight away (office supplies) - no stock is kept',
           '{"commercial_class":"Internal / MRO","stock_tracking":"Not tracked","valuation":"Not applicable","accounting_treatment":"Expense","sellability":"Not sellable","bill_mode":"Visible line","capture_method":"None","consumption_method":"None","expense_account":"5180"}'::jsonb),
          ('Gift Card', 'Stored value - issued and redeemed through POS -> Gift Card, never sold as merchandise',
           '{"commercial_class":"Stored Value","stock_tracking":"Not tracked","valuation":"Not applicable","accounting_treatment":"Stored value liability","sellability":"Stored-value issue only","bill_mode":"Separate receipt","capture_method":"Scan","consumption_method":"None"}'::jsonb),
          ('Virtual Kit', 'Sold as one item, its components leave stock (Product Bundle, fulfilment Virtual)',
           '{"commercial_class":"Bundle / Kit","stock_tracking":"Not tracked","valuation":"Not applicable","accounting_treatment":"Inventory asset","sellability":"Normal sale","bill_mode":"Visible line","capture_method":"Scan","consumption_method":"Sale issue"}'::jsonb)
        ) AS t(code, description, dims)
      WHERE NOT EXISTS (SELECT 1 FROM %I.documents d WHERE d.id = t.code)
    $sql$, schema_rec.schema_name, schema_rec.schema_name);
  END LOOP;
END
$mig$;
