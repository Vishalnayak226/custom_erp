-- ---------------------------------------------------------------------------
-- Ask 215.7-215.9 (user, 2026-11-15): every standard offer type, the
-- conditions that limit an offer, and an offer screen that shows only the
-- chosen type's fields.
--
-- Offer.offer_type gains Buy A Get B, Spend Slabs, Quantity Slabs, Fixed
-- Price and Combo; Offer.scope gains Design List. New fields:
--   get_discount_pct  Buy X Get Y / Buy A Get B: % off the get units (blank = free)
--   get_list          Buy A Get B: the items or designs that are discounted
--   slabs             Spend / Quantity Slabs: "2000=200; 5000=10%"
--   offer_price       Fixed Price (per unit) and Combo (per combo)
--   combo_lists       Combo: one list per line, codes by commas
--   days, hour_from, hour_to, exclude_list, max_uses, max_uses_per_customer
-- engines/pos_offers.go and engines/pos_offer_types.go read them.
--
-- The existing type fields lose their "(type)" suffix, since the screen now
-- shows each field only for its own type. Every change is guarded: options
-- and labels change only while they still equal what was shipped, and a
-- re-run changes nothing. No Offer document is touched.
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
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Offer', 'get_discount_pct', 'Get %% off (blank = free)', 'Number', FALSE, NULL, 47),
        ('Offer', 'get_list', 'Get items', 'Text', FALSE, NULL, 48),
        ('Offer', 'slabs', 'Slabs', 'Text', FALSE, NULL, 49),
        ('Offer', 'offer_price', 'Offer price', 'Number', FALSE, NULL, 50),
        ('Offer', 'combo_lists', 'Combo lists', 'Text', FALSE, NULL, 51),
        ('Offer', 'days', 'Days (blank = all)', 'Data', FALSE, NULL, 52),
        ('Offer', 'hour_from', 'From time', 'Data', FALSE, NULL, 53),
        ('Offer', 'hour_to', 'To time', 'Data', FALSE, NULL, 54),
        ('Offer', 'exclude_list', 'Exclude items', 'Text', FALSE, NULL, 55),
        ('Offer', 'max_uses', 'Uses in total (blank = no limit)', 'Number', FALSE, NULL, 56),
        ('Offer', 'max_uses_per_customer', 'Uses per customer (blank = no limit)', 'Number', FALSE, NULL, 57)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'Percentage Off,Flat Off,Buy X Get Y,Buy A Get B,Bundle Price,Spend Slabs,Quantity Slabs,Fixed Price,Combo,Gift With Purchase,Bonus Points'
       WHERE doctype_name = 'Offer' AND fieldname = 'offer_type'
         AND options = 'Percentage Off,Flat Off,Buy X Get Y,Bundle Price,Gift With Purchase,Bonus Points'
    $sql$, schema_rec.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields
         SET options = 'Bill,Item,Category,Product Group,SKU List,Design List'
       WHERE doctype_name = 'Offer' AND fieldname = 'scope'
         AND options = 'Bill,Item,Category,Product Group,SKU List'
    $sql$, schema_rec.schema_name);

    -- Short labels: each field now shows only for its own type.
    EXECUTE format($sql$
      UPDATE %I.doctype_fields f
         SET label = v.new_label
        FROM (VALUES
          ('discount_pct',    'Discount %% (Percentage Off)',               'Discount %%'),
          ('discount_amount', 'Discount Amount (Flat Off)',                 'Discount amount'),
          ('buy_qty',         'Buy Qty (Buy X Get Y)',                      'Buy qty'),
          ('get_qty',         'Free Qty (Buy X Get Y)',                     'Get qty'),
          ('bundle_qty',      'Bundle Qty (Bundle Price)',                  'Bundle qty'),
          ('bundle_price',    'Bundle Price (Bundle Price)',                'Bundle price'),
          ('gift_item',       'Gift item (Gift With Purchase)',             'Gift item'),
          ('gift_qty',        'Gift qty (per bill, or per Buy Qty units)',  'Gift qty'),
          ('bonus_basis',     'Bonus per (Bonus Points)',                   'Bonus per'),
          ('stores',          'Stores (Bonus Points; blank = all)',         'Stores (blank = all)'),
          ('scope_value',     'Applies To - which item, category, group or SKUs', 'Items, SKUs or designs')
        ) AS v(fieldname, old_label, new_label)
       WHERE f.doctype_name = 'Offer' AND f.fieldname = v.fieldname AND f.label = v.old_label
    $sql$, schema_rec.schema_name);

    -- Lists of SKUs or designs can run long: 200,000 characters each
    -- (about 12,000 codes), only where no limit was set yet.
    IF EXISTS (
      SELECT 1 FROM information_schema.columns
      WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_fields' AND column_name = 'max_length')
    THEN
      EXECUTE format($sql$
        UPDATE %I.doctype_fields SET max_length = 200000
         WHERE doctype_name = 'Offer' AND max_length IS NULL
           AND fieldname IN ('scope_value', 'get_list', 'combo_lists', 'exclude_list')
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
