-- ---------------------------------------------------------------------------
-- Stage 42.4.1/42.4.2 - WaveTemplate + Wave: the first real outbound-depth
-- item. Before this migration a "wave" was nothing but a free-text
-- FulfillmentTask.wave_id tag (26.5.6) - any string two people happened to
-- type the same way. This makes a wave a real, governed object with its own
-- lifecycle (Planned -> Released -> In Progress -> Complete -> Closed,
-- engines/wms_wave.go's TransitionWaveStatus), and WaveTemplate lets that
-- object be created by a rule on a schedule instead of only by a manager
-- typing task ids into AssignTasksToWave by hand - which stays available as
-- the manual path, unchanged.
--
-- WaveTemplate's criteria are deliberately narrower than the plan's literal
-- "channel, carrier, cut-off, zone, order type, service level" list -
-- channel, zone/location and service_level are kept because real data backs
-- each (SalesOrder.channel, FulfillmentTask.location_code, SalesOrder.
-- priority), matching this Stage's "a criterion is only as strict as the
-- data behind it" posture (warehouse_cockpit.go's own bin-utilization
-- comment states this same principle first). carrier and order_type are kept
-- as fields on the template (so the plan's full vocabulary is authorable and
-- visible) but are NOT matched against - no FulfillmentTask/SalesOrder field
-- carries a carrier or order-type value before a booking exists, and
-- matching against nothing would silently match every order or none. See
-- engines/wms_wave.go's matchesWaveTemplate for the exact fields evaluated.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('WaveTemplate', 'Inventory', 'inventory', 'Master'),
('Wave', 'Inventory', 'inventory', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('WaveTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('WaveTemplate', 'location_code', 'Location (optional)', 'Link', FALSE, 'Location', 2),
('WaveTemplate', 'channel', 'Channel (optional)', 'Data', FALSE, NULL, 3),
('WaveTemplate', 'service_level', 'Service Level (optional)', 'Select', FALSE, 'Any,Normal,Expedite', 4),
('WaveTemplate', 'carrier', 'Carrier (optional, informational - not yet matched)', 'Data', FALSE, NULL, 5),
('WaveTemplate', 'order_type', 'Order Type (optional, informational - not yet matched)', 'Data', FALSE, NULL, 6),
('WaveTemplate', 'cutoff_time', 'Cutoff Time HH:MM (optional - only orders placed before this today)', 'Data', FALSE, NULL, 7),
('WaveTemplate', 'run_daily_at', 'Auto-run Daily At HH:MM (optional - blank means manual-only)', 'Data', FALSE, NULL, 8),
('WaveTemplate', 'last_auto_run_date', 'Last Auto-run Date (system-set)', 'Data', FALSE, NULL, 9),
('WaveTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10),

('Wave', 'code', 'Wave', 'Data', TRUE, NULL, 1),
('Wave', 'wave_template', 'Wave Template (optional)', 'Link', FALSE, 'WaveTemplate', 2),
('Wave', 'location_code', 'Location', 'Data', TRUE, NULL, 3),
('Wave', 'status', 'Status', 'Select', TRUE, 'Planned,Released,In Progress,Complete,Closed', 4),
('Wave', 'created_via', 'Created Via', 'Select', TRUE, 'Manual,Auto', 5),
('Wave', 'task_count', 'Task Count (set at creation)', 'Number', FALSE, NULL, 6),
('Wave', 'notes', 'Notes (optional)', 'Data', FALSE, NULL, 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin',       'WaveTemplate', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'WaveTemplate', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'WaveTemplate', TRUE, FALSE, FALSE, FALSE),
('HR/Admin',       'Wave', TRUE, TRUE, TRUE, TRUE),
('Store Manager',  'Wave', TRUE, TRUE, TRUE, FALSE),
('Cashier',        'Wave', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
SELECT
  'STR-Wave-' || v.from_status || '-' || v.to_status,
  'StatusTransitionRule',
  jsonb_build_object(
    'code', 'STR-Wave-' || v.from_status || '-' || v.to_status,
    'entity', 'Wave', 'from_status', v.from_status, 'to_status', v.to_status,
    'allowed', 'Yes', 'requires_reason_code', 'No', 'status', 'Active'
  ),
  'Active', 'system'
FROM (VALUES
  ('Planned', 'Released'), ('Released', 'In Progress'),
  ('In Progress', 'Complete'), ('Complete', 'Closed')
) AS v(from_status, to_status)
ON CONFLICT (id) DO NOTHING;
