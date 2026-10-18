-- Checklist 84.7 (found at the 2026-10-18 release): retire `Stores` again on
-- a database built from zero.
--
-- migrations_stage30_5_5_retire_stores.sql retired the Stores master and
-- folded its fields into Location. But migrations_stores_master_fields.sql
-- sorts after it ("stores" > "stage") and re-inserts the doctype_meta row, so
-- every database migrated from empty - CI, a new install - ends up with a
-- Stores doctype in module 'Store', which no role template or module
-- manifest knows (TestKnownModulesMatchTheTenantSchema,
-- TestAdministratorAndAuditorCoverEveryShippedDoctype,
-- TestModuleManifestCatalog). Databases migrated in the original order
-- (production, the dev DB) never got it back, so this is a no-op there.
--
-- This file sorts after migrations_stores_master_fields.sql, so it runs last.
-- It repeats 30.5.5's two steps with the same safety: any Stores record moves
-- to Location first, and the doctype is removed only when nothing is left to
-- lose (doctype_fields and role_permissions cascade from it).

UPDATE tenant_default.documents
SET doctype = 'Location',
    data = data || jsonb_build_object('type', COALESCE(data->>'type', 'Store')),
    updated_at = CURRENT_TIMESTAMP
WHERE doctype = 'Stores';

DELETE FROM tenant_default.doctype_meta
WHERE name = 'Stores'
  AND NOT EXISTS (SELECT 1 FROM tenant_default.documents WHERE doctype = 'Stores');
