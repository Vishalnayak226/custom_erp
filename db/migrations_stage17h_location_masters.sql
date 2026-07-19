-- Stage 17.9: Location/LegalEntity/Department/CostCenter masters.
-- Decision confirmed 2026-07-19: retain existing free-text location fields
-- as-is (do NOT migrate every doctype's location column to a Link field -
-- far higher risk, touches PurchaseOrder/GRN/TransferOrder/Employee/Asset/
-- ExpenseClaim/ProductionOrder all at once); instead seed a Location row
-- for every code already in use and validate new writes going forward.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('Location', 'Core', 'Master', 'core'),
('LegalEntity', 'Core', 'Master', 'core'),
('Department', 'Core', 'Master', 'core'),
('CostCenter', 'Core', 'Master', 'core')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Location', 'code', 'Location Code', 'Data', TRUE, NULL, 1),
('Location', 'name', 'Location Name', 'Data', TRUE, NULL, 2),
('Location', 'type', 'Type', 'Select', TRUE, 'Store,Warehouse,HO', 3),
('Location', 'legal_entity', 'Legal Entity', 'Link', FALSE, 'LegalEntity', 4),
('Location', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),
('LegalEntity', 'code', 'Entity Code', 'Data', TRUE, NULL, 1),
('LegalEntity', 'name', 'Entity Name', 'Data', TRUE, NULL, 2),
('LegalEntity', 'gstin', 'GSTIN', 'Data', FALSE, NULL, 3),
('LegalEntity', 'state', 'State', 'Data', FALSE, NULL, 4),
('LegalEntity', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),
('Department', 'code', 'Department Code', 'Data', TRUE, NULL, 1),
('Department', 'name', 'Department Name', 'Data', TRUE, NULL, 2),
('Department', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3),
('CostCenter', 'code', 'Cost Center Code', 'Data', TRUE, NULL, 1),
('CostCenter', 'name', 'Cost Center Name', 'Data', TRUE, NULL, 2),
('CostCenter', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 3)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Location', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'LegalEntity', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Department', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'CostCenter', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Location', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'LegalEntity', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Department', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'CostCenter', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Seed one Active Location per code already in real use, so validating new
-- writes against this master never breaks existing legacy data: every
-- distinct inventory_availability.location_code (includes the Phase 5
-- scale-test LOC-0001..0100 range) plus the free-text 'location'/
-- 'location_code'/'target_warehouse'/'from_warehouse'/'to_warehouse'
-- values already present on documents, plus 'HO' (referenced pervasively
-- in role/location scoping even though it holds no inventory row itself).
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT code, 'Location', jsonb_build_object('code', code, 'name', code, 'type', 'Warehouse', 'status', 'Active'), 'Active', 'system'
FROM (
    SELECT DISTINCT location_code AS code FROM tenant_default.inventory_availability
    UNION
    SELECT DISTINCT data->>'location' FROM tenant_default.documents WHERE data->>'location' IS NOT NULL AND data->>'location' != ''
    UNION
    SELECT DISTINCT data->>'location_code' FROM tenant_default.documents WHERE data->>'location_code' IS NOT NULL AND data->>'location_code' != ''
    UNION
    SELECT DISTINCT data->>'target_warehouse' FROM tenant_default.documents WHERE data->>'target_warehouse' IS NOT NULL AND data->>'target_warehouse' != ''
    UNION
    SELECT DISTINCT data->>'from_warehouse' FROM tenant_default.documents WHERE data->>'from_warehouse' IS NOT NULL AND data->>'from_warehouse' != ''
    UNION
    SELECT DISTINCT data->>'to_warehouse' FROM tenant_default.documents WHERE data->>'to_warehouse' IS NOT NULL AND data->>'to_warehouse' != ''
    UNION
    SELECT 'HO'
) seeds
WHERE code IS NOT NULL AND code != ''
ON CONFLICT (id) DO NOTHING;
