-- 24.36: the offline POS queue (Stage 20.13) lived only in localStorage -
-- nothing about it was ever visible to the server until a sale actually
-- synced, so a cashier clearing browser storage before reconnecting left
-- zero trace anywhere: the sale, and the client-side "refuse to close with
-- a pending queue" guard, both simply vanished. This can't be made fully
-- watertight (a device that goes offline and is wiped before a single
-- online moment ever recurs has, by construction, never told the server
-- anything) - but it closes the much more common case: any connectivity
-- window between queueing and wiping now leaves a server-side trace.
--
-- pos_offline_heartbeats is a plain working table (not a browsable
-- doctype, same shape as any other engine-internal state) - the frontend
-- best-effort beacons its current queued cart_numbers here whenever it has
-- a network path; ClosePOSSession diffs the latest beacon against what
-- actually synced and, on a mismatch, writes a POSOfflineQueueGap record -
-- that part IS a browsable doctype, same "engine writes it, HR/Admin and
-- Store Manager review it" shape as POSOfflineSyncVariance.
CREATE TABLE IF NOT EXISTS tenant_default.pos_offline_heartbeats (
    session_id VARCHAR(100) PRIMARY KEY,
    cashier VARCHAR(100) NOT NULL,
    location VARCHAR(100) NOT NULL,
    cart_numbers JSONB NOT NULL DEFAULT '[]',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('POSOfflineQueueGap', 'Sales', 'sales', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSOfflineQueueGap', 'session_id', 'POS Session', 'Data', TRUE, NULL, 1),
('POSOfflineQueueGap', 'cashier', 'Cashier', 'Data', TRUE, NULL, 2),
('POSOfflineQueueGap', 'location', 'Location', 'Data', TRUE, NULL, 3),
('POSOfflineQueueGap', 'missing_cart_numbers', 'Missing Cart Numbers', 'Data', TRUE, NULL, 4),
('POSOfflineQueueGap', 'missing_count', 'Missing Count', 'Number', TRUE, NULL, 5),
('POSOfflineQueueGap', 'status', 'Status', 'Select', TRUE, 'Open,Reviewed', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSOfflineQueueGap', TRUE, FALSE, TRUE, FALSE),
('Store Manager', 'POSOfflineQueueGap', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
