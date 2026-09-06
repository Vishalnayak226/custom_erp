-- ---------------------------------------------------------------------------
-- Stage 47.6 - RF/mobile task shell (audit finding A-06/A-39).
--
-- One doctype only. The four traceability operations 47.6.7 names
-- (batch putaway, batch consume, expiry sweep, serial status transition)
-- already have real, tested, registered endpoints and need no schema at all -
-- what they lacked was any caller, which is a frontend gap, not a data one.
--
-- FloorAssistRequest exists for 47.6.2's "one recovery/supervisor action
-- reachable without sharing the operator's own credentials (supervisor
-- override token/approval, never a password handoff)". The wrong answers,
-- both of which were considered and rejected:
--
--   * File it as a Grievance. Grievance is an HR complaint record. Putting
--     "I cannot scan this bin" into somebody's HR file is both incorrect and
--     quietly harmful to that person.
--   * Ask the supervisor to type their password into the operator's session.
--     That is exactly the password handoff the item forbids, and it is how
--     shared logins start on a warehouse floor.
--
-- So: a small record the operator raises from their own session, which a
-- supervisor sees on THEIR own device through the ordinary document screen.
-- No new endpoint - the generic doctype API already serves it.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('FloorAssistRequest', 'Inventory', 'Transaction', 'wms')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('FloorAssistRequest', 'task', 'Task', 'Data', TRUE, NULL, 1),
('FloorAssistRequest', 'request', 'What the operator needs', 'Data', TRUE, NULL, 2),
-- Optional: an operator with no location on their session (a relief worker, a
-- contractor) must still be able to ask for help, so this is not the row's
-- scope. See engines/scope_policy.go on why an optional location is an
-- attribute rather than a scope.
('FloorAssistRequest', 'location', 'Location (optional)', 'Data', FALSE, NULL, 3),
('FloorAssistRequest', 'status', 'Status', 'Select', TRUE, 'Open,Acknowledged,Resolved', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- The operator raises it; the supervisor answers it. Cashier is included
-- because a till is a floor position too and the same "I need a supervisor
-- here" need exists there - it is the one doctype on this list any role may
-- create, and it holds nothing sensitive by construction.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'FloorAssistRequest', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'FloorAssistRequest', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'FloorAssistRequest', TRUE, TRUE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_floor_assist_open
  ON tenant_default.documents ((data->>'status'))
  WHERE doctype = 'FloorAssistRequest' AND deleted_at IS NULL;
