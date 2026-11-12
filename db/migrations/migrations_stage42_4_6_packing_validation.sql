-- ---------------------------------------------------------------------------
-- Stage 42.4.6 - Packing validation templates + blind packing.
-- PackingValidationTemplate is a configurable pre-pack checklist
-- (engines/wms_pack_station.go's EvaluatePackingValidation is the choke
-- point handlePackComplete now runs through, additively - see that file's
-- header for exactly what "additive" means here); blind_pack hides the
-- expected qty from the packer's confirm screen so they count independently
-- rather than confirming a number they were just shown.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PackingValidationTemplate', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PackingValidationTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('PackingValidationTemplate', 'applies_to', 'Applies To', 'Select', TRUE, 'All,Customer,Item', 2),
('PackingValidationTemplate', 'applies_to_value', 'Applies To Value (Customer code or SKU, required unless All)', 'Data', FALSE, NULL, 3),
('PackingValidationTemplate', 'require_weight_check', 'Require Weight Check', 'Select', TRUE, 'Yes,No', 4),
('PackingValidationTemplate', 'require_documents', 'Require Documents Present', 'Select', TRUE, 'Yes,No', 5),
('PackingValidationTemplate', 'blind_pack', 'Blind Pack (hide expected qty from packer)', 'Select', TRUE, 'Yes,No', 6),
('PackingValidationTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 7),

-- Captured by CompletePackTaskWithValidation (engines/wms_pack_station.go)
-- when a matched template's require_weight_check is Yes - additive, visible
-- read-only on the existing FulfillmentTask record.
('FulfillmentTask', 'packed_weight_kg', 'Packed Weight kg (optional, system-set)', 'Number', FALSE, NULL, 21)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'PackingValidationTemplate', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'PackingValidationTemplate', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'PackingValidationTemplate', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
