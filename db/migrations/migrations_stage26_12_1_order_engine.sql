-- Stage 26.12.1: Order Engine - multi-level Order/Order-Line status, an
-- order-level Hold engine, and a stage-gated cancellation matrix. Design
-- decision (confirmed with the user 2026-07-24): a new SalesOrder/
-- SalesOrderLine doctype pair, not an extension of POSCart - POSCart's
-- one-status-field-per-document shape can't carry independent per-line
-- status, which the blueprint's split-shipment support genuinely needs
-- (see docs/specs/oms_master_blueprint_reference.md §3).
--
-- Both are Transaction doctypes (not Master - they're never hand-created
-- via the generic Setup-submenu form; engines/orders.go's CreateSalesOrder
-- is the real, sole entry point, since creating an order atomically
-- reserves stock and writes N line documents, which the single-document
-- generic create endpoint can't do). Registered in doctype_meta anyway so
-- the existing generic GET /api/v1/doc/{doctype} list/detail views work
-- for free, same as FulfillmentTask/POSCart/GRN before this.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('SalesOrder', 'OMS', 'oms', 'Transaction'),
('SalesOrderLine', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('SalesOrder', 'code', 'Order Number', 'Data', TRUE, NULL, 1),
('SalesOrder', 'channel', 'Channel', 'Data', FALSE, NULL, 2),
('SalesOrder', 'channel_order_id', 'Channel Order ID', 'Data', FALSE, NULL, 3),
('SalesOrder', 'customer_name', 'Customer Name', 'Data', FALSE, NULL, 4),
('SalesOrder', 'shipping_address', 'Shipping Address', 'Data', TRUE, NULL, 5),
('SalesOrder', 'payment_status', 'Payment Status', 'Select', TRUE, 'Pending,Confirmed', 6),
('SalesOrder', 'order_status', 'Order Status', 'Select', TRUE, 'Draft,On Hold,Reserved,Partially Fulfilled,Dispatched,Shipped,Delivered,Cancelled,Closed', 7),
('SalesOrder', 'hold_reason', 'Hold Reason', 'Data', FALSE, NULL, 8),
('SalesOrder', 'hold_owner', 'Hold Owner', 'Data', FALSE, NULL, 9),
('SalesOrder', 'total_amount', 'Total Amount', 'Number', FALSE, NULL, 10),
('SalesOrderLine', 'code', 'Line ID', 'Data', TRUE, NULL, 1),
('SalesOrderLine', 'order_id', 'Order', 'Link', TRUE, 'SalesOrder', 2),
('SalesOrderLine', 'sku', 'SKU', 'Data', TRUE, NULL, 3),
('SalesOrderLine', 'qty', 'Quantity', 'Number', TRUE, NULL, 4),
('SalesOrderLine', 'unit_price', 'Unit Price', 'Number', FALSE, NULL, 5),
('SalesOrderLine', 'location_code', 'Allocated Location', 'Data', FALSE, NULL, 6),
('SalesOrderLine', 'line_status', 'Line Status', 'Select', TRUE, 'Pending,Reserved,Dispatched,Cancelled,Returned', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same role shape as the existing FulfillmentTask/POSCart Transaction
-- doctypes: HR/Admin full, Store Manager/Cashier can read/create/update
-- (day-to-day order handling) but not delete - an order is cancelled via
-- the Order Engine's own CancelOrder (mandatory reason code, stage-gated),
-- never hard-deleted.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'SalesOrder', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'SalesOrderLine', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'SalesOrder', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'SalesOrderLine', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'SalesOrder', TRUE, TRUE, FALSE, FALSE),
('Cashier', 'SalesOrderLine', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
