-- ---------------------------------------------------------------------------
-- Stage 42.2.7 - PutawayStrategy master + directed putaway. No Active
-- strategy for a location (the default, every tenant's state before this
-- migration) means SuggestPutawayBin returns no suggestion at all and the
-- Putaway screen behaves exactly as before - "falls back to today's manual
-- entry" from the plan, satisfied by simply not configuring this master.
--
-- `criteria` is a comma-separated whitelist (validatePutawayStrategyMasterRules/
-- putawayCriteriaWeights, engines/warehouse_task.go) naming which of the
-- plan's five signals (item velocity tier, zone putaway_sequence, bin
-- capacity headroom, hazmat/temperature compatibility, existing-batch
-- consolidation) SuggestPutawayBin actually weighs for that location - a
-- tenant with no Zone/hazmat data configured yet can turn those two off
-- rather than get a suggestion silently ignoring a criterion it claims to
-- use.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PutawayStrategy', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PutawayStrategy', 'code', 'Strategy Code', 'Data', TRUE, NULL, 1),
('PutawayStrategy', 'location_code', 'Location (optional - blank applies everywhere)', 'Data', FALSE, NULL, 2),
('PutawayStrategy', 'criteria', 'Criteria (comma-separated: velocity, zone_sequence, capacity, hazmat_temp, batch_consolidation)', 'Data', TRUE, NULL, 3),
('PutawayStrategy', 'notes', 'Notes', 'Data', FALSE, NULL, 4),
('PutawayStrategy', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'PutawayStrategy', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'PutawayStrategy', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'PutawayStrategy', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Item-side half of the `hazmat_temp` criterion: neither field existed
-- anywhere in this tree before. Both optional and additive - a blank
-- hazmat_class/temperature_class on every pre-existing Item is what makes
-- the hazmat_temp criterion a no-op (never blocks a suggestion) until a
-- tenant actually classifies an item, matching the same "criterion is only
-- as strict as the data behind it" posture the migration note above takes
-- for Zone.hazmat_allowed defaulting to 'Yes' on auto-created zones.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Item', 'hazmat_class', 'Hazmat Class (optional)', 'Data', FALSE, NULL, 30),
('Item', 'temperature_class', 'Temperature Class (optional)', 'Select', FALSE, 'Ambient,Chilled,Frozen,Controlled', 31)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;
