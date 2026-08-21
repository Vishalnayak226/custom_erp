-- ---------------------------------------------------------------------------
-- Stage 42.1.7 - Outbound lottable validation (Infor §16).
--
-- Batch.attributes (42.1.2) has always carried arbitrary lottable JSON, e.g.
-- {"country_of_origin": "IN", "grade": "A"} - what was missing was a master
-- to declare which values a customer's contract actually requires, and the
-- check that reads it (engines/traceability.go's ValidateLotForCustomer).
--
-- Same global-unique document-id gotcha as Batch/SerialNumber: a constraint's
-- natural key is (customer, item-or-blank, attribute_key), not something that
-- fits as a PRIMARY KEY-friendly id, so the id stays generated and the
-- uniqueness check lives in engines/master_data_validation.go instead, where
-- it can produce a real message.
--
-- `customer` and `item` are both Data, not Link, for the same reason Batch's
-- `item` is: the generic Link check (META-0198) resolves against
-- documents.id, and neither a Customer's nor an Item's document id is the
-- code a person types (see migrations_stage42_1_traceability.sql's note on
-- Batch.item). `item` is deliberately optional - a blank item applies the
-- rule to every SKU that customer buys, which is what lets one row express
-- "this customer requires country_of_origin = IN, full stop" instead of one
-- row per SKU in their catalog.
--
-- module_key 'master_data' matches Batch/SerialNumber: this is a constraint
-- master a Store Manager configures, not a floor-ops transaction, even though
-- the route that reads it (POST /api/v1/wms/batch/consume) sits behind
-- moduleGate("wms") like every other floor-ops endpoint.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('LottableConstraint', 'Master Data', 'master_data', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LottableConstraint', 'customer', 'Customer Code', 'Data', TRUE, NULL, 1),
('LottableConstraint', 'item', 'Item Code (SKU, optional = applies to all items)', 'Data', FALSE, NULL, 2),
('LottableConstraint', 'attribute_key', 'Lottable Attribute Key', 'Data', TRUE, NULL, 3),
('LottableConstraint', 'allowed_values', 'Allowed Values (comma-separated)', 'Data', TRUE, NULL, 4),
('LottableConstraint', 'notes', 'Notes', 'Data', FALSE, NULL, 5),
('LottableConstraint', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same split Batch uses: Store Manager configures the contract, HR/Admin can
-- delete a mistyped row, Cashier is read-only.
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'LottableConstraint', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'LottableConstraint', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'LottableConstraint', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
