-- Stage 34.1: CompetitorPrice - competitor price observations, fed by CSV.
--
-- The one item in Stage 34 that is buildable regardless of how 34.6 (the
-- legal/ToS gate on automated collection) lands, because its data source is
-- a buyer pasting a spreadsheet exported by hand from a marketplace seller
-- panel - a legitimate source with no ToS question attached. If 34.4's
-- JSON-endpoint harvester is ever approved, it writes into this same
-- doctype; nothing here assumes where a row came from.
--
-- Registered as a **Master** doctype deliberately, even though the rows are
-- timestamped observations rather than classic master data. The Setup
-- flyout and the generic doctype table (renderSidebarSubmenu / app.js:1854)
-- both filter on `document_type = 'Master'`, so Master is what gets the
-- generic list + "New" form + **Bulk Import** button for free. A
-- Transaction doctype would be unreachable from the UI without bespoke
-- frontend code - exactly the trap CycleCountLine hit and had to work
-- around (see app.js:6505's comment). Master costs one migration and no
-- JavaScript, which is what 34.1 asked for.
--
-- `setup_advanced` is left FALSE (the 30.5.4 default): this is business
-- data a buyer genuinely maintains, not system plumbing to file away.
--
-- Field notes:
--   our_item        Link->Item, NOT mandatory. A competitor listing that has
--                   not been matched to one of our SKUs yet is still worth
--                   recording; the 34.2 price-gap report simply skips rows
--                   that carry no link. Making it mandatory would force the
--                   buyer to solve catalog matching before they can paste a
--                   spreadsheet, which is backwards.
--   platform        Select, so the gap report can group by source without
--                   free-text spelling drift ("Amazon" vs "amazon.in").
--                   'Other' is present so an unlisted source is recordable
--                   rather than blocking the import.
--   observed_at     Date, mandatory. A competitor price with no date is not
--                   evidence of anything - 34.3's undercut worker needs to
--                   know how stale an observation is before alerting on it.
--   source_url      Provenance. Free-text; never fetched by this Stage.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('CompetitorPrice', 'PIM', 'pim', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CompetitorPrice', 'code', 'Observation Code', 'Data', TRUE, NULL, 1),
('CompetitorPrice', 'our_item', 'Our Item (SKU)', 'Link', FALSE, 'Item', 2),
('CompetitorPrice', 'competitor_product', 'Competitor Product Title', 'Data', FALSE, NULL, 3),
('CompetitorPrice', 'competitor_sku', 'Competitor SKU / ASIN', 'Data', FALSE, NULL, 4),
('CompetitorPrice', 'platform', 'Platform', 'Select', TRUE, 'Amazon,Flipkart,Myntra,Meesho,Ajio,Nykaa,Tata Cliq,JioMart,eBay,Own Website,Other', 5),
('CompetitorPrice', 'competitor_price', 'Competitor Price', 'Number', TRUE, NULL, 6),
('CompetitorPrice', 'mrp', 'Competitor MRP', 'Number', FALSE, NULL, 7),
('CompetitorPrice', 'rating', 'Rating', 'Number', FALSE, NULL, 8),
('CompetitorPrice', 'review_count', 'Review Count', 'Number', FALSE, NULL, 9),
('CompetitorPrice', 'observed_at', 'Observed On', 'Date', TRUE, NULL, 10),
('CompetitorPrice', 'source_url', 'Source URL', 'Data', FALSE, NULL, 11),
('CompetitorPrice', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Buyers/merchandisers maintain it; a Store Manager reads it (the price-gap
-- report is useful at store level) but does not edit competitor data.
-- Cashier gets nothing - this never appears at the till.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'CompetitorPrice', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'CompetitorPrice', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Backfill every already-provisioned tenant schema. New tenants inherit all
-- of this through ProvisionTenantSchema's clone of tenant_default (whose
-- table list 26.11.2 repaired). Same loop shape as
-- migrations_stage26_4_10_supplier_portal.sql.
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      VALUES ('CompetitorPrice', 'PIM', 'pim', 'Master')
      ON CONFLICT (name) DO NOTHING
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT * FROM (VALUES
        ('CompetitorPrice', 'code', 'Observation Code', 'Data', TRUE, NULL::text, 1),
        ('CompetitorPrice', 'our_item', 'Our Item (SKU)', 'Link', FALSE, 'Item', 2),
        ('CompetitorPrice', 'competitor_product', 'Competitor Product Title', 'Data', FALSE, NULL, 3),
        ('CompetitorPrice', 'competitor_sku', 'Competitor SKU / ASIN', 'Data', FALSE, NULL, 4),
        ('CompetitorPrice', 'platform', 'Platform', 'Select', TRUE, 'Amazon,Flipkart,Myntra,Meesho,Ajio,Nykaa,Tata Cliq,JioMart,eBay,Own Website,Other', 5),
        ('CompetitorPrice', 'competitor_price', 'Competitor Price', 'Number', TRUE, NULL, 6),
        ('CompetitorPrice', 'mrp', 'Competitor MRP', 'Number', FALSE, NULL, 7),
        ('CompetitorPrice', 'rating', 'Rating', 'Number', FALSE, NULL, 8),
        ('CompetitorPrice', 'review_count', 'Review Count', 'Number', FALSE, NULL, 9),
        ('CompetitorPrice', 'observed_at', 'Observed On', 'Date', TRUE, NULL, 10),
        ('CompetitorPrice', 'source_url', 'Source URL', 'Data', FALSE, NULL, 11),
        ('CompetitorPrice', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 12)
      ) AS v(doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      WHERE EXISTS (SELECT 1 FROM %I.doctype_meta WHERE name = 'CompetitorPrice')
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $f$, schema_rec.schema_name, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES
        ('HR/Admin', 'CompetitorPrice', TRUE, TRUE, TRUE, TRUE),
        ('Store Manager', 'CompetitorPrice', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
