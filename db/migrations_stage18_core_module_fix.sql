-- Stage 18 bug fix (found live while verifying the Stage 18 Location
-- typeahead, docs/micro_checklist.md Stage 18): Location/LegalEntity/
-- Department/CostCenter (Stage 17.9, db/migrations_stage17h_location_masters.sql)
-- were registered in doctype_meta with module_key = 'core', but no 'core'
-- module was ever added to the public.modules catalog or granted in any
-- tenant's module_entitlements. engines.IsModuleEnabled fails closed on a
-- missing entitlement row (by design, for modules that legitimately aren't
-- provisioned yet), so every read/write to these four doctypes through the
-- generic GET/POST /api/v1/doc/{doctype} endpoint has been returning 403
-- "Module 'core' is disabled for this tenant" for every role, on every
-- tenant, since Stage 17.9 shipped. ValidateLocationReference (called
-- directly from PurchaseOrder/TransferOrder creation) bypasses this gate
-- entirely, which is why Stage 17.9's own verification never caught it.
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('core', 'Core Masters', 'Location / Legal Entity / Department / Cost Center organizational masters', TRUE, TRUE)
ON CONFLICT (module_key) DO NOTHING;

-- Backfill every already-provisioned tenant schema (new tenants pick this
-- up automatically going forward: engines/saas.go's ProvisionTenant seeds a
-- new tenant's module_entitlements by copying tenant_default's rows, so
-- inserting into tenant_default here is enough for anything provisioned
-- from now on - only schemas that already existed before this fix need a
-- direct backfill).
DO $$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    EXECUTE format(
      'INSERT INTO %I.module_entitlements (module_key, enabled, granted_by, granted_at) VALUES (''core'', TRUE, ''system'', CURRENT_TIMESTAMP) ON CONFLICT (module_key) DO UPDATE SET enabled = TRUE',
      schema_rec.schema_name
    );
  END LOOP;
END $$;
