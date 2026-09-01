-- Stage 38.4: webhook subscriptions with HMAC signing, retry/backoff and
-- DLQ - extends outbox.go/notifications.go, exactly as the backlog item
-- names it. WebhookSubscription is a plain registered doctype (the
-- ScheduledReport precedent, migrations_stage26_10_4_scheduled_reports.sql)
-- - no dedicated create/list/delete handler needed, the generic doc API
-- already covers it. Delivery itself rides Stage 38.6's new async job
-- runner (engines/jobrunner.go): engines/webhook.go's dispatchWebhooksForEvent
-- (called from processOutbox, engines/outbox.go) enqueues one
-- 'webhook_delivery' job per matching Active subscription, and the runner's
-- own retry/backoff/DeadLettered handling IS the DLQ this item asks for -
-- no second retry mechanism invented.
--
-- New module_key 'integrations': genuinely cross-cutting platform
-- infrastructure, not gated behind any one business module - registered
-- here per Stage 37.9's own lesson (register public.modules +
-- module_entitlements in the SAME migration that introduces the doctype,
-- checked proactively rather than found live afterward).
INSERT INTO public.modules (module_key, display_name, description, is_core, default_enabled) VALUES
('integrations', 'Integrations', 'Webhook subscriptions and outbound event delivery.', FALSE, TRUE)
ON CONFLICT (module_key) DO NOTHING;

INSERT INTO tenant_default.module_entitlements (module_key, enabled, granted_by, note) VALUES
('integrations', TRUE, 'system', NULL)
ON CONFLICT (module_key) DO NOTHING;

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('WebhookSubscription', 'Integrations', 'integrations', 'Master')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('WebhookSubscription', 'code', 'Subscription ID', 'Data', TRUE, NULL, 1),
('WebhookSubscription', 'name', 'Name', 'Data', TRUE, NULL, 2),
('WebhookSubscription', 'url', 'Delivery URL', 'Data', TRUE, NULL, 3),
('WebhookSubscription', 'secret', 'Signing Secret', 'Data', TRUE, NULL, 4),
('WebhookSubscription', 'event_pattern', 'Event Pattern', 'Data', TRUE, NULL, 5),
('WebhookSubscription', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin', 'WebhookSubscription', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'WebhookSubscription', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them - the same pattern every prior Stage 35-37 migration in
-- this file family uses. module_entitlements re-touched for every schema
-- including tenant_default (ON CONFLICT DO UPDATE, the 37.9 precedent).
DO $$
DECLARE
    schema_rec RECORD;
BEGIN
    FOR schema_rec IN
        SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
    LOOP
        EXECUTE format(
            'INSERT INTO %I.module_entitlements (module_key, enabled, granted_by, granted_at) VALUES '
            '(''integrations'', TRUE, ''system'', CURRENT_TIMESTAMP) '
            'ON CONFLICT (module_key) DO UPDATE SET enabled = TRUE',
            schema_rec.schema_name
        );
    END LOOP;

    FOR schema_rec IN
        SELECT schema_name FROM information_schema.schemata
        WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
    LOOP
        EXECUTE format('INSERT INTO %I.doctype_meta (name, module, module_key, document_type) VALUES
            (''WebhookSubscription'', ''Integrations'', ''integrations'', ''Master'')
            ON CONFLICT (name) DO NOTHING', schema_rec.schema_name);

        EXECUTE format('INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
            (''WebhookSubscription'', ''code'', ''Subscription ID'', ''Data'', TRUE, NULL, 1),
            (''WebhookSubscription'', ''name'', ''Name'', ''Data'', TRUE, NULL, 2),
            (''WebhookSubscription'', ''url'', ''Delivery URL'', ''Data'', TRUE, NULL, 3),
            (''WebhookSubscription'', ''secret'', ''Signing Secret'', ''Data'', TRUE, NULL, 4),
            (''WebhookSubscription'', ''event_pattern'', ''Event Pattern'', ''Data'', TRUE, NULL, 5),
            (''WebhookSubscription'', ''status'', ''Status'', ''Select'', TRUE, ''Active,Inactive'', 6)
            ON CONFLICT (doctype_name, fieldname) DO NOTHING', schema_rec.schema_name);

        EXECUTE format('INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
            (''Super Admin'', ''WebhookSubscription'', TRUE, TRUE, TRUE, TRUE),
            (''Store Manager'', ''WebhookSubscription'', TRUE, TRUE, TRUE, FALSE)
            ON CONFLICT (role, doctype_name) DO NOTHING', schema_rec.schema_name);
    END LOOP;
END $$;
