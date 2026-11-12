-- ---------------------------------------------------------------------------
-- Stage 40.1: Purchase Order line items, GST treatment, and place of supply.
--
-- Three gaps this closes, all reported from live use:
--
--   1. There was no item selection on a PO at all. public/app.js's
--      createDraftPurchaseOrder literally posted items: '[]' and asked the
--      maker for one header-level "Total Amount" instead, so a PO recorded
--      what it cost but never what was being bought. GRN receipt, the GST
--      engine and the vendor's own copy all need the lines.
--
--   2. Purchase prices are quoted tax-EXCLUSIVE by most vendors, but
--      engines/gst.go's ComputeGSTForLines backs tax out of the gross
--      (correct for this codebase's MRP-style sale_price fields, wrong for a
--      PO). Which convention applies is a business decision, so it becomes
--      configuration - a tenant default (procurement.po_gst_mode) that each
--      PO can override - rather than a hardcoded assumption.
--
--   3. Interstate vs intra-state was a checkbox the maker ticked by hand, on
--      a screen that already knows both addresses. It is now derived from the
--      buying entity's state and the vendor's state, with the checkbox kept
--      only as an explicit override.
--
-- Additive and idempotent throughout (the ON CONFLICT DO NOTHING / UPDATE ...
-- WHERE convention every doctype_fields migration here uses). No data
-- backfill is needed: `items` keeps its existing storage exactly - a
-- serialised JSON string in a text column - so every existing reader
-- (ComputePurchaseOrderGST, the GRN receipt path) reads what it always read.
-- This is a rendering + validation change, the same shape as Stage 30.5.3.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- 1. PurchaseOrder.items -> JSONTable, so the generic form and the bespoke PO
--    screen both render a real add-line editor instead of a JSON text box.
--
-- Keys are read off the Go struct that consumes them (engines/gst.go's
-- ComputePurchaseOrderGST: sku/qty/rate), so the editor cannot emit a shape
-- the engine will not understand. `mrp` is the one new key and is optional -
-- see 30.5.3's column-spec grammar for the field meanings.
--
-- HSN and GST rate are deliberately NOT columns here. Both already live on
-- the Item master and are resolved per line by GetItemTaxInfo at save time;
-- letting someone type a different rate on the PO would create a second
-- source of truth for tax classification. The PO screen shows them read-only
-- next to each line instead.
-- ---------------------------------------------------------------------------
UPDATE tenant_default.doctype_fields
   SET fieldtype = 'JSONTable',
       label     = 'Items',
       options   = '[{"key":"sku","label":"Item","type":"link","link":"Item","required":true},
                     {"key":"qty","label":"Qty","type":"number","required":true},
                     {"key":"rate","label":"Purchase Price","type":"number","required":true},
                     {"key":"mrp","label":"MRP","type":"number"}]'
 WHERE doctype_name = 'PurchaseOrder' AND fieldname = 'items';

-- ---------------------------------------------------------------------------
-- 2. PurchaseOrder.gst_mode - whether this PO's line rates already include
--    GST. Optional: an unset value falls back to the tenant setting
--    procurement.po_gst_mode (engines/settings_definitions.go), which itself
--    defaults to Exclusive. Every historical PO therefore keeps behaving
--    exactly as it did, because nothing reads this field until it is set.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PurchaseOrder', 'gst_mode', 'GST Treatment', 'Select', FALSE, 'Exclusive,Inclusive', 20)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3. Place of supply on the PO.
--
--   interstate            - the effective flag the GST split uses.
--   interstate_override   - set when a human deliberately overrode the
--                           derived value, so the derivation does not silently
--                           undo their choice on the next save.
--   place_of_supply       - the two state codes actually compared, stored for
--                           the audit trail and for the printed PO.
--
-- All optional. engines/gst_place_of_supply.go fills them at save time.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PurchaseOrder', 'interstate', 'Inter-state Supply', 'Check', FALSE, NULL, 21),
('PurchaseOrder', 'interstate_override', 'Supply Type Overridden', 'Check', FALSE, NULL, 22),
('PurchaseOrder', 'place_of_supply', 'Place of Supply', 'Data', FALSE, NULL, 23),
-- grand_total is the tax-inclusive payable, derived from the lines at save
-- time. total_amount stays the taxable value it has always been, because
-- engines.PostDoubleEntry and every existing procurement report read it as
-- such - this is the figure the vendor actually bills, registered so it shows
-- up in the record list and in exports rather than being an invisible key.
('PurchaseOrder', 'grand_total', 'Grand Total (incl. GST)', 'Number', FALSE, NULL, 24)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 4. Vendor.state / Vendor.address.
--
-- The vendor half of "decide interstate from the two addresses" had nowhere
-- to come from: Vendor carried a GSTIN but no state, and no address at all.
--
-- `state` is optional because a GSTIN already encodes the state in its first
-- two digits, which is what StateCodeFromGSTIN reads - this field is the
-- fallback for an unregistered/composition vendor with no GSTIN, and the
-- override when the two disagree. `address` is what the printed PO puts in
-- the "To" block; it was previously not recorded anywhere.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Vendor', 'address', 'Address', 'Data', FALSE, NULL, 9),
('Vendor', 'state', 'State', 'Data', FALSE, NULL, 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 5. Backfill every already-provisioned tenant schema. Copies tenant_default's
--    rows rather than repeating the specs, so the two cannot drift - the same
--    loop db/migrations_stage30_5_3_json_line_editors.sql uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_fields', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    -- New fields: insert whatever tenant_default has that this schema lacks.
    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT d.doctype_name, d.fieldname, d.label, d.fieldtype, d.mandatory, d.options, d.display_order
        FROM tenant_default.doctype_fields d
       WHERE (d.doctype_name = 'PurchaseOrder' AND d.fieldname IN ('gst_mode', 'interstate', 'interstate_override', 'place_of_supply', 'grand_total'))
          OR (d.doctype_name = 'Vendor' AND d.fieldname IN ('address', 'state'))
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name);

    -- Changed field: PurchaseOrder.items' new fieldtype/label/options.
    EXECUTE format($f$
      UPDATE %I.doctype_fields t
         SET fieldtype = d.fieldtype, label = d.label, options = d.options
        FROM tenant_default.doctype_fields d
       WHERE d.doctype_name = t.doctype_name AND d.fieldname = t.fieldname
         AND d.doctype_name = 'PurchaseOrder' AND d.fieldname = 'items'
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
