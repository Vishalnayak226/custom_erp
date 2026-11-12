-- Stage 26.12.4: Courier/Shipment/Manifest - extends LogisticsBooking (a
-- bare carrier/tracking/charge record since Stage 13) into a real Shipment
-- engine: serviceability check, AWB assignment, manifest grouping, handover
-- cascade, tracking sync, RTO detection. Per the checklist item's own scope
-- note, this is the internal AWB/manifest engine only - a real courier API
-- is a separate, later, credentials-gated item (same "code-complete,
-- credentials pending" pattern as 26.2.1-26.2.5's channel connectors).

-- CourierServiceArea (Master, module_key='oms') is the "Courier Provider
-- connector" config table the blueprint calls for, in its internal-only
-- form: which couriers service which pincode prefixes, and in what
-- priority order - a real geo/zone lookup would be a genuine improvement,
-- not just a port, same caveat already noted for 26.12.2's Nearest-Pincode
-- allocation strategy.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('CourierServiceArea', 'OMS', 'oms', 'Master'),
('Manifest', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('CourierServiceArea', 'code', 'Rule Code', 'Data', TRUE, NULL, 1),
('CourierServiceArea', 'courier', 'Courier', 'Data', TRUE, NULL, 2),
('CourierServiceArea', 'pincode_prefix', 'Pincode Prefix (blank = all)', 'Data', FALSE, NULL, 3),
('CourierServiceArea', 'priority', 'Priority (lower preferred)', 'Number', TRUE, NULL, 4),
('CourierServiceArea', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5),
('Manifest', 'code', 'Manifest ID', 'Data', TRUE, NULL, 1),
('Manifest', 'courier', 'Courier', 'Data', TRUE, NULL, 2),
('Manifest', 'location_code', 'Location', 'Data', TRUE, NULL, 3),
('Manifest', 'shipment_count', 'Shipment Count', 'Number', FALSE, NULL, 4),
('Manifest', 'status', 'Status', 'Select', TRUE, 'Open,Handed Over', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'CourierServiceArea', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Manifest', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'CourierServiceArea', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Manifest', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'Manifest', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- LogisticsBooking (pre-existing since Stage 13, module_key='inventory' -
-- left as-is, only the new OMS endpoints below are oms-gated) gets the
-- additive fields the real Shipment engine needs: a link to the
-- FulfillmentTask it ships (optional - a manual booking with no task link
-- stays outside manifest grouping and the SalesOrder closure cascade,
-- same "deliberately out of scope" boundary 26.12.1 drew around legacy
-- channel-webhook orders), the destination pincode the serviceability
-- check and Nearest-Pincode-style matching runs against, the AWB number
-- and manifest it was grouped into once assigned, and an RTO reason.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LogisticsBooking', 'fulfillment_task_id', 'Fulfillment Task', 'Link', FALSE, 'FulfillmentTask', 7),
('LogisticsBooking', 'destination_pincode', 'Destination Pincode', 'Data', FALSE, NULL, 8),
('LogisticsBooking', 'awb_number', 'AWB Number', 'Data', FALSE, NULL, 9),
('LogisticsBooking', 'manifest_id', 'Manifest', 'Link', FALSE, 'Manifest', 10),
('LogisticsBooking', 'rto_reason', 'RTO Reason', 'Data', FALSE, NULL, 11)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Widen the LogisticsBooking status lifecycle from the old 3-value
-- (Shipped,In-Transit,Delivered - "Shipped" was set unconditionally at
-- creation, before anything had actually shipped) to the real Shipment
-- engine's stages. Existing rows keep whatever value they already have -
-- this only widens the Select dropdown's own option list, same "additive,
-- not destructive" shape as every other options-list update in this repo.
UPDATE tenant_default.doctype_fields
SET options = 'AWB Assigned,Manifested,Handed Over,In-Transit,Delivered,RTO'
WHERE doctype_name = 'LogisticsBooking' AND fieldname = 'status';
