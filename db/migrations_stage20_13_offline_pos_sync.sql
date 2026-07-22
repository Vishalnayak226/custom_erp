-- Stage 20.13: offline-first POS queue.
-- Decisions (user, 2026-07-22): offline window is one shift, tied to the
-- cashier's own open POSSession (enforced client-side - see app.js, the
-- server has no visibility into a client-side queue that hasn't synced
-- yet); a sale that syncs after stock changed offline always posts (goods
-- already left the store) and lets inventory go negative, flagged here for
-- a manager to review/reconcile rather than silently absorbed or rejected.
--
-- POSOfflineSyncVariance is written only by engines/pos_checkout.go's
-- recordOfflineSyncVariance - same "no role gets a generic create grant,
-- every write goes through dedicated engine code" pattern as POSSession/
-- PaymentProposal (see those migrations' own comments for why). Registered
-- so Store Manager/HR-Admin get a real review screen for free via the
-- existing generic doctype-table view - no new frontend screen code needed,
-- just a sidebar entry point (index.html/app.js).
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('POSOfflineSyncVariance', 'Sales', 'Transaction', 'sales')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('POSOfflineSyncVariance', 'cart_number', 'Cart Number', 'Data', TRUE, NULL, 1),
('POSOfflineSyncVariance', 'sku', 'SKU', 'Data', TRUE, NULL, 2),
('POSOfflineSyncVariance', 'location', 'Location', 'Data', TRUE, NULL, 3),
('POSOfflineSyncVariance', 'shortfall_qty', 'Shortfall Qty', 'Number', TRUE, NULL, 4),
('POSOfflineSyncVariance', 'resulting_available', 'Resulting Available (negative)', 'Number', TRUE, NULL, 5),
('POSOfflineSyncVariance', 'status', 'Status', 'Select', TRUE, 'Open,Reviewed', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Read-only for review; HR/Admin can also flip status to Reviewed once
-- reconciled (e.g. against the next GRN) - same update grant shape as any
-- other review-only doctype, no create/delete for anyone.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'POSOfflineSyncVariance', TRUE, FALSE, TRUE, FALSE),
('Store Manager', 'POSOfflineSyncVariance', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
