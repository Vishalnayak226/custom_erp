-- Stage 35.5: real courier adapters, provider AWBs/pickups/cancellation,
-- signed tracking webhooks, rate shopping and NDR re-attempt workflow.
-- Provider credentials reuse the encrypted channel_credentials store under
-- courier:<provider>; no secret is added to a document or migration.

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('NDRCase', 'OMS', 'oms', 'Transaction'),
('CourierTrackingEvent', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('NDRCase', 'code', 'NDR Case', 'Data', TRUE, NULL, 1),
('NDRCase', 'booking_id', 'Logistics Booking', 'Link', TRUE, 'LogisticsBooking', 2),
('NDRCase', 'reason', 'Failure Reason', 'Data', TRUE, NULL, 3),
('NDRCase', 'attempt_count', 'Attempt Count', 'Number', TRUE, NULL, 4),
('NDRCase', 'reattempt_at', 'Re-attempt At', 'Datetime', FALSE, NULL, 5),
('NDRCase', 'resolution_note', 'Resolution Note', 'Text', FALSE, NULL, 6),
('NDRCase', 'status', 'Status', 'Select', TRUE, 'Open,Reattempt Scheduled,Closed,RTO', 7),
('CourierTrackingEvent', 'provider', 'Provider', 'Data', TRUE, NULL, 1),
('CourierTrackingEvent', 'event_id', 'Provider Event ID', 'Data', TRUE, NULL, 2),
('CourierTrackingEvent', 'awb', 'AWB', 'Data', TRUE, NULL, 3),
('CourierTrackingEvent', 'booking_id', 'Logistics Booking', 'Link', TRUE, 'LogisticsBooking', 4),
('CourierTrackingEvent', 'status', 'Provider Status', 'Data', TRUE, NULL, 5),
('CourierTrackingEvent', 'normalized_status', 'ERP Status', 'Data', TRUE, NULL, 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LogisticsBooking', 'provider_awb', 'Provider AWB', 'Data', FALSE, NULL, 12),
('LogisticsBooking', 'remote_shipment_id', 'Provider Shipment ID', 'Data', FALSE, NULL, 13),
('LogisticsBooking', 'awb_allocated_at', 'AWB Allocated At', 'Datetime', FALSE, NULL, 14),
('LogisticsBooking', 'pickup_reference', 'Pickup Reference', 'Data', FALSE, NULL, 15),
('LogisticsBooking', 'pickup_scheduled_at', 'Pickup Scheduled At', 'Datetime', FALSE, NULL, 16),
('LogisticsBooking', 'courier_cancelled_at', 'Courier Cancelled At', 'Datetime', FALSE, NULL, 17),
('LogisticsBooking', 'provider_charge_paise', 'Provider Charge (Paise)', 'Number', FALSE, NULL, 18)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin', 'NDRCase', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'NDRCase', TRUE, TRUE, TRUE, FALSE),
('Super Admin', 'CourierTrackingEvent', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'CourierTrackingEvent', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
allow_read=EXCLUDED.allow_read, allow_create=EXCLUDED.allow_create,
allow_update=EXCLUDED.allow_update, allow_delete=EXCLUDED.allow_delete;

CREATE UNIQUE INDEX IF NOT EXISTS idx_courier_tracking_event_provider_event
ON tenant_default.documents ((data->>'provider'), (data->>'event_id'))
WHERE doctype='CourierTrackingEvent' AND deleted_at IS NULL AND COALESCE(data->>'event_id','') <> '';

CREATE INDEX IF NOT EXISTS idx_ndr_case_booking_status
ON tenant_default.documents ((data->>'booking_id'), status)
WHERE doctype='NDRCase' AND deleted_at IS NULL;
