-- Stage 26.12.10: Per-order/customer notification templates, distinct from
-- alerting.go's incident-level Slack/Teams ops alerts. Scope decision made
-- with the user (2026-07-25): real generic webhook dispatch, reusing
-- alerting.go's own "POST a JSON payload to a configured webhook URL, log-
-- only no-op if unconfigured" pattern - no new SDK/dependency, and no real
-- email/SMS/WhatsApp provider credentials are needed here since the
-- receiving webhook (Zapier/Make/the tenant's own dispatch service) is
-- responsible for the actual send and for resolving customer contact
-- details, the same "payload carries identifiers, never raw PII further
-- than it has to" precedent alerting.go's own file header documents.
--
-- NotificationTemplate/NotificationChannelConfig are ordinary Master
-- doctypes (same shape as AllocationRule/ReasonCode, Stage 26.12.9).
-- NotificationLog is a Transaction doctype written only by
-- engines/notifications.go's DispatchNotification - never created via the
-- generic doc-create endpoint, same "engine writes it, humans only read it"
-- shape as SalesOrder/SalesOrderLine.
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('NotificationTemplate', 'OMS', 'oms', 'Master'),
('NotificationChannelConfig', 'OMS', 'oms', 'Master'),
('NotificationLog', 'OMS', 'oms', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('NotificationTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('NotificationTemplate', 'event', 'Event', 'Select', TRUE, 'Order Placed,Order On Hold,Order Cancelled,Return Requested,Return Approved,Return Rejected,RTO Detected,Refund Processed', 2),
('NotificationTemplate', 'channel', 'Channel', 'Select', TRUE, 'Email,SMS,WhatsApp', 3),
('NotificationTemplate', 'subject', 'Subject (Email only)', 'Data', FALSE, NULL, 4),
('NotificationTemplate', 'body_template', 'Body Template', 'Data', TRUE, NULL, 5),
('NotificationTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),
('NotificationChannelConfig', 'code', 'Config Code', 'Data', TRUE, NULL, 1),
('NotificationChannelConfig', 'channel', 'Channel', 'Select', TRUE, 'Email,SMS,WhatsApp', 2),
('NotificationChannelConfig', 'webhook_url', 'Dispatch Webhook URL', 'Data', TRUE, NULL, 3),
('NotificationChannelConfig', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4),
('NotificationLog', 'code', 'Log ID', 'Data', TRUE, NULL, 1),
('NotificationLog', 'event', 'Event', 'Data', TRUE, NULL, 2),
('NotificationLog', 'channel', 'Channel', 'Data', TRUE, NULL, 3),
('NotificationLog', 'order_id', 'Order Reference', 'Data', FALSE, NULL, 4),
('NotificationLog', 'template_id', 'Template Used', 'Data', FALSE, NULL, 5),
('NotificationLog', 'dispatch_status', 'Dispatch Status', 'Select', TRUE, 'Sent,Failed,Skipped-NoConfig,Skipped-NoTemplate', 6),
('NotificationLog', 'response_detail', 'Response Detail', 'Data', FALSE, NULL, 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- Same role shape as AllocationRule/ReasonCode (Stage 26.12.9): HR/Admin
-- full CRUD over config, Store Manager read-only. NotificationLog is
-- read-only for both (only the engine writes it).
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'NotificationTemplate', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'NotificationChannelConfig', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'NotificationLog', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'NotificationTemplate', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'NotificationChannelConfig', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'NotificationLog', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;
