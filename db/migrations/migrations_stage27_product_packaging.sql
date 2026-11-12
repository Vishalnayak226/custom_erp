-- Stage 27: Modular Product Packaging - register the two module_keys that
-- were missing before PIM/WMS/OMS/HR/etc. could be sold as independent
-- products: 'wms' and 'oms'. Same shape as the existing Stage 18 core-
-- module fix (migrations_stage18_core_module_fix.sql) - register in
-- public.modules, seed tenant_default.module_entitlements, backfill every
-- already-provisioned tenant schema (new tenants pick this up automatically
-- via engines/saas.go's ProvisionTenantSchema, which clones tenant_default's
-- rows - only pre-existing schemas need a direct backfill).
--
-- Before this migration, WMS floor-ops routes (putaway/pick-list/bin-
-- condition-transition/transfer-pack/cycle-count-reconcile,
-- internal/server/routes.go) and the OMS integration surface (BigCommerce
-- webhook, Unicommerce credentials/orders, Shopify/marketplace/fulfillment
-- routes) had no module-entitlement gate of their own at all - moduleGate
-- calls added alongside this migration (see routes.go) are what actually
-- make 'wms'/'oms' real, enforced product boundaries rather than just
-- catalog rows.
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('wms', 'Warehouse Management', 'Putaway, bin-grouped pick lists, bin condition transitions, transfer-order pack, cycle-count reconciliation', FALSE, TRUE),
('oms', 'Order Management', 'Channel order routing/webhooks (Shopify/BigCommerce/Unicommerce), marketplace settlement reconciliation, logistics booking', FALSE, TRUE)
ON CONFLICT (module_key) DO NOTHING;

INSERT INTO tenant_default.module_entitlements (module_key, enabled, granted_by, note) VALUES
('wms', TRUE, 'system', NULL),
('oms', TRUE, 'system', NULL)
ON CONFLICT (module_key) DO NOTHING;

DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.module_entitlements (module_key, enabled, granted_by, granted_at) VALUES (''wms'', TRUE, ''system'', CURRENT_TIMESTAMP), (''oms'', TRUE, ''system'', CURRENT_TIMESTAMP) ON CONFLICT (module_key) DO UPDATE SET enabled = TRUE',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
