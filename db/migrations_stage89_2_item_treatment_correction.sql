-- ---------------------------------------------------------------------------
-- Stage 89.8 (user, 2026-10-18): an item's treatment can be corrected after it
-- has stock - by an admin, after a preview of the impact, a reason and the
-- item code typed (engines/item_treatment_correction.go).
--
-- ItemTreatmentCorrection is the record each correction leaves: who asked,
-- the reason, the preview as confirmed and what was posted. It is written by
-- the engine only (no role creates one through the record API). With "A second
-- admin approves item treatment corrections" on, a request waits as Pending
-- Approval and is applied when another admin approves it; the approval rule
-- below routes it to Super Admin, and the approval engine refuses an approval
-- by the admin who asked (maker-checker).
--
-- Additive and idempotent; tenant_default first, then every other tenant.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('ItemTreatmentCorrection', 'Inventory', 'Transaction', 'inventory')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ItemTreatmentCorrection', 'item', 'Item / Item Type', 'Data', TRUE, NULL, 1),
('ItemTreatmentCorrection', 'item_name', 'Name', 'Data', FALSE, NULL, 2),
('ItemTreatmentCorrection', 'target', 'Corrects', 'Data', FALSE, NULL, 3),
('ItemTreatmentCorrection', 'change_summary', 'Change', 'Data', FALSE, NULL, 4),
('ItemTreatmentCorrection', 'from_kind', 'From', 'Data', FALSE, NULL, 5),
('ItemTreatmentCorrection', 'to_kind', 'To', 'Data', FALSE, NULL, 6),
('ItemTreatmentCorrection', 'stock_value', 'Stock value moved', 'Number', FALSE, NULL, 7),
('ItemTreatmentCorrection', 'reason', 'Reason', 'Text', TRUE, NULL, 8),
('ItemTreatmentCorrection', 'requested_by', 'Asked by', 'Data', FALSE, NULL, 9),
('ItemTreatmentCorrection', 'applied_by', 'Applied by', 'Data', FALSE, NULL, 10),
('ItemTreatmentCorrection', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Applied', 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ItemTreatmentCorrection', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('ItemTreatmentCorrection', 0, NULL, 'Super Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;

DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
    WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'doctype_meta') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_meta (name, module, document_type, module_key)
        SELECT name, module, document_type, module_key FROM tenant_default.doctype_meta WHERE name = 'ItemTreatmentCorrection'
        ON CONFLICT (name) DO NOTHING
      $sql$, schema_rec.schema_name);
      EXECUTE format($sql$
        INSERT INTO %1$I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
        SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = 'ItemTreatmentCorrection'
        ON CONFLICT (doctype_name, fieldname) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'role_permissions') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
        SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = 'ItemTreatmentCorrection'
        ON CONFLICT (role, doctype_name) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = schema_rec.schema_name AND table_name = 'approval_rules') THEN
      EXECUTE format($sql$
        INSERT INTO %1$I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT doctype, min_amount, max_amount, required_role FROM tenant_default.approval_rules WHERE doctype = 'ItemTreatmentCorrection'
        ON CONFLICT (doctype, min_amount) DO NOTHING
      $sql$, schema_rec.schema_name);
    END IF;
  END LOOP;
END
$mig$;
