-- ---------------------------------------------------------------------------
-- Stage 93 (user, 2026-10-18): "Can it create auto cost center once I create
-- a store. A std cost center name should create automatically. It should be
-- editable. Not this lengthy name." Then: "It should ask Mandatory field and
-- auto linked ... Make it Mandatory those field to fill ... People should be
-- mapped. I want to mapped from day one automatically."
--
-- Location.cost_center: the location's cost center, a Link the location form
-- shows and lets the user change. A location saved with it blank gets one
-- (engines/location_cost_center.go); existing locations are linked or given
-- one by `erp-server -migrate` after the schema sync (93.2); and a GL posting
-- with no cost center of its own takes its location's (93.3). Not mandatory
-- in the metadata on purpose: the server fills it, so a CSV of stores needs
-- no cost center column.
--
-- 93.7 Location.legal_entity, 93.8 LegalEntity.gstin and 93.9
-- Employee.department become mandatory - checked from each record's next
-- save; nothing stored changes.
--
-- 93.9 users.department: the person's department, beside the home store the
-- user row already carries (location_code). Nullable; picked on the Users
-- screen, or taken from the linked Employee when HR is on.
--
-- Additive. New field and column into tenant_default (SyncAllTenantSchemas,
-- Stage 64.1, copies missing ones to every client after migrate); the two
-- mandatory flags are existing rows, which the sync never changes, so they
-- are set in every schema here.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'cost_center', 'Cost Center', 'Link', FALSE, 'CostCenter', 14)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

ALTER TABLE tenant_default.users ADD COLUMN IF NOT EXISTS department VARCHAR(100);

-- 93.11: each old long code a master was cleaned to (engines/master_recode.go).
-- Kept so history that still shows the old code can be traced, and so a store
-- code cached by a till or an integration still resolves.
CREATE TABLE IF NOT EXISTS tenant_default.master_code_changes (
  id          BIGSERIAL    PRIMARY KEY,
  doc_type    VARCHAR(60)  NOT NULL,
  old_code    VARCHAR(200) NOT NULL,
  new_code    VARCHAR(60)  NOT NULL,
  changed_by  VARCHAR(100),
  changed_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS master_code_changes_old_uq ON tenant_default.master_code_changes (doc_type, old_code);

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
      UPDATE %I.doctype_fields SET mandatory = TRUE
       WHERE (doctype_name, fieldname) IN (('Location', 'legal_entity'), ('LegalEntity', 'gstin'), ('Employee', 'department'))
         AND mandatory IS DISTINCT FROM TRUE
    $sql$, schema_rec.schema_name);
  END LOOP;
END
$mig$;
