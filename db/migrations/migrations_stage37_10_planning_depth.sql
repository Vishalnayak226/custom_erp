-- ---------------------------------------------------------------------------
-- Stage 37.10: Planning depth - forecasting, reorder points, pegging,
-- capacity.
--
-- Pre-build audit found this ~60% already built under different names/
-- scopes: ForecastDemand/CalculateSalesVelocity (engines/optimization.go)
-- is a real, if naive (flat 30-day average), forecast; GetReplenishmentSuggestions/
-- GetMRPSuggestions already compute a reorder point, just from CALL-SITE
-- lead-time/safety-stock parameters rather than a persisted per-item
-- config; GetProductionSchedule (engines/manufacturing_scheduling.go) is
-- already a genuine finite-capacity scheduler, just never wired to a report.
-- Pegging (linking a specific open SalesOrderLine's demand to the specific
-- supply that will cover it) is the one genuinely new piece.
--
-- ReorderPointConfig is the only new doctype - a per-(item, location)
-- override so a planner is no longer forced to apply one blanket lead-time/
-- safety-stock pair across an entire location's SKU catalogue. Existing
-- callers of GetReplenishmentSuggestions/GetMRPSuggestions are UNCHANGED -
-- new sibling functions consult this config, the originals are untouched.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ReorderPointConfig', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ReorderPointConfig', 'code', 'Config Code', 'Data', TRUE, NULL, 1),
('ReorderPointConfig', 'item', 'Item (SKU)', 'Data', TRUE, NULL, 2),
('ReorderPointConfig', 'location_code', 'Location', 'Data', TRUE, NULL, 3),
('ReorderPointConfig', 'lead_time_days', 'Lead Time (days)', 'Number', TRUE, NULL, 4),
('ReorderPointConfig', 'safety_stock_qty', 'Safety Stock Qty', 'Number', TRUE, NULL, 5),
('ReorderPointConfig', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_reorder_point_config_item_location
    ON tenant_default.documents (UPPER(data->>'item'), UPPER(data->>'location_code'))
    WHERE doctype = 'ReorderPointConfig' AND deleted_at IS NULL;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ReorderPointConfig', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ReorderPointConfig', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = 'ReorderPointConfig'
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'ReorderPointConfig'
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'ReorderPointConfig'
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_reorder_point_config_item_location
        ON %I.documents (UPPER(data->>'item'), UPPER(data->>'location_code'))
       WHERE doctype = 'ReorderPointConfig' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
