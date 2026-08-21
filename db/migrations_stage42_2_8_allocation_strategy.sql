-- ---------------------------------------------------------------------------
-- Stage 42.2.8 - AllocationStrategy master, lifting GenerateWavePickList's
-- (via AllocateFromStock, 42.1.5) hard-coded FIFO default out of code.
--
-- Deliberately does NOT let a configured strategy override FEFO for a
-- batch-tracked item - ResolveAllocationStrategy's own 42.1.5 comment is
-- explicit that "an item declared batch-tracked is allocated by expiry
-- regardless," a correctness gate for food/pharma/cosmetics recall, not a
-- preference a warehouse should be able to configure away. AllocationStrategy
-- therefore only ever applies to a non-batch-tracked item choosing among
-- FIFO (unchanged default)/LIFO/NearestBin/FewestPicks/CleanLocation -
-- FEFO is deliberately not one of `strategy`'s allowed values, since it is
-- already the correct, non-optional behaviour for the one case it applies to.
--
-- `item` optional (blank = applies to every non-batch-tracked item, the
-- same "blank is the catch-all" shape LottableConstraint/TaskDispatchStrategy
-- already use) rather than location-scoped: rotation rule is a property of
-- the SKU/business (how fast it turns, whether slot-clearing matters), not
-- of which warehouse it sits in.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('AllocationStrategy', 'Inventory', 'inventory', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('AllocationStrategy', 'code', 'Strategy Code', 'Data', TRUE, NULL, 1),
('AllocationStrategy', 'item', 'Item Code (optional - blank applies to every non-batch-tracked item)', 'Data', FALSE, NULL, 2),
('AllocationStrategy', 'strategy', 'Strategy', 'Select', TRUE, 'FIFO,LIFO,NearestBin,FewestPicks,CleanLocation', 3),
('AllocationStrategy', 'notes', 'Notes', 'Data', FALSE, NULL, 4),
('AllocationStrategy', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'AllocationStrategy', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'AllocationStrategy', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'AllocationStrategy', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
