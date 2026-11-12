-- ---------------------------------------------------------------------------
-- Stage 42.4.8/42.4.9 - Loading: LoadingTask against a DockDoor (42.3.1) +
-- Trailer (42.3.4), scan-verified carton-to-trailer, feeding the existing
-- Manifest (engines/marketplace.go's GenerateManifest/HandoverManifest,
-- untouched by this migration or its engine file). Pallet exchange counters
-- live on LoadingTask itself, per the plan's own item text. The Bill of
-- Lading (42.4.9) is not a stored doctype - it is assembled from a completed
-- LoadingTask plus its linked Manifest/bookings and rendered through the
-- existing browser print-sheet path (public/app.js's renderPrintSheet
-- pattern, the same one stickers/labels already use), so it needs no new
-- table and no new renderer, matching the plan's explicit instruction.
--
-- Two additive fields go on the pre-existing ShippingPackage doctype
-- (loaded_at, loading_task_id) rather than a new junction table - the same
-- "attach to the existing record, don't invent a second ledger" choice this
-- Stage made for hold_qty on inventory_availability and catch-weight fields
-- on the GRN receipt line.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('LoadingTask', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('LoadingTask', 'code', 'Loading Task', 'Data', TRUE, NULL, 1),
('LoadingTask', 'dock_door', 'Dock Door', 'Data', TRUE, NULL, 2),
('LoadingTask', 'trailer_no', 'Trailer', 'Data', TRUE, NULL, 3),
('LoadingTask', 'manifest_id', 'Manifest (optional)', 'Link', FALSE, 'Manifest', 4),
('LoadingTask', 'expected_carton_count', 'Expected Carton Count (optional)', 'Number', FALSE, NULL, 5),
('LoadingTask', 'scanned_carton_count', 'Scanned Carton Count (system-set)', 'Number', FALSE, NULL, 6),
('LoadingTask', 'pallet_exchange_out', 'Pallets Given To Carrier (optional)', 'Number', FALSE, NULL, 7),
('LoadingTask', 'pallet_exchange_in', 'Pallets Received Back (optional)', 'Number', FALSE, NULL, 8),
('LoadingTask', 'status', 'Status', 'Select', TRUE, 'Planned,Loading,Loaded,Departed', 9),
('LoadingTask', 'loaded_by', 'Loaded By (optional)', 'Data', FALSE, NULL, 10),
('LoadingTask', 'notes', 'Notes (optional)', 'Data', FALSE, NULL, 11),

('ShippingPackage', 'loaded_at', 'Loaded At (system-set)', 'Data', FALSE, NULL, 14),
('ShippingPackage', 'loading_task_id', 'Loading Task (system-set)', 'Link', FALSE, 'LoadingTask', 15)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'LoadingTask', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'LoadingTask', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'LoadingTask', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-LoadingTask-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code', 'STR-LoadingTask-' || v.from_status || '-' || v.to_status,
    'entity', 'LoadingTask', 'from_status', v.from_status, 'to_status', v.to_status,
    'allowed', 'Yes', 'requires_reason_code', 'No', 'status', 'Active'
  ),
  'Active', 'system'
FROM (VALUES
  ('Planned', 'Loading'), ('Loading', 'Loaded'), ('Loaded', 'Departed')
) AS v(from_status, to_status)
ON CONFLICT (id) DO NOTHING;
