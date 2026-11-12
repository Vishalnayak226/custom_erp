-- ---------------------------------------------------------------------------
-- Stage 49.10.6b (4): a client's data reset or purge needs a second person.
--
-- The request is a ClientDataRemoval record in the operator's own client
-- (engines/client_removal_approval.go): one admin asks (tenantctl
-- request-removal), another approves it in Approvals through the ordinary
-- maker-checker engine, and only then do tenantctl reset-data / purge run
-- with -request <id>.
--
-- This adds the record type only (documents.doctype references it). The
-- client schema sync copies it to every client after migrate, as it does any
-- record type tenant_default gains. No fields, no permissions: the request is
-- written by tenantctl and decided in Approvals, never edited on a screen.
-- The approval rule (any value -> Super Admin) is put in place by the first
-- request, in the operator's client only.
-- Additive and idempotent.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ClientDataRemoval', 'Core', 'core', 'Transaction')
ON CONFLICT (name) DO NOTHING;
