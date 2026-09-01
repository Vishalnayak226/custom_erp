-- ---------------------------------------------------------------------------
-- Stage 37.8: Service management.
--
-- Pre-build audit found no ServiceTicket/WorkOrder/AMC/warranty concept
-- anywhere. The generic WarehouseTask dispatch spine (Stage 42.2) is
-- WMS-specific (hardcoded bin/batch/qty/uom fields, zone/location semantics)
-- and not directly reusable as an object - but its LIFECYCLE PATTERN (typed
-- status enum, terminal-state guard, reason-required transitions, a
-- source-doc back-reference) is exactly what ServiceTicket's own dedicated
-- engine functions copy below, the same way every other Stage 37 doctype
-- this session (IntercompanyTransaction, LandedCostVoucher,
-- PrepaidExpenseSchedule) enforces its own valid transitions explicitly in
-- Go rather than adopting Stage 29.8's opt-in StatusTransitionRule map
-- (that mechanism governs the GENERIC document API path specifically -
-- ServiceTicket's status moves through dedicated engine functions, the
-- same category as those three).
--
-- "Technician assignment" reuses WarehouseTask.AssignedTo's own convention
-- exactly: a bare username string, no Employee-link enforcement - that
-- object doesn't validate one either.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('ServiceTicket', 'Service', 'service', 'Transaction'),
('ServiceContract', 'Service', 'service', 'Master')
ON CONFLICT (name) DO NOTHING;

-- 37.8.1/37.8.2: ServiceTicket. asset/service_contract_id are optional Links
-- - a ticket for an unregistered asset or with no AMC coverage is still a
-- real, valid ticket. respond_by_date/resolve_by_date are dates (this
-- codebase's existing date-only SLA convention, e.g. Stage 37.4.4's dunning
-- thresholds), not timestamps - simpler, and consistent with every other
-- due-date-shaped field this session added (SalesInvoice.due_date etc).
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ServiceTicket', 'code', 'Ticket Number', 'Data', TRUE, NULL, 1),
('ServiceTicket', 'customer', 'Customer', 'Data', TRUE, NULL, 2),
('ServiceTicket', 'asset', 'Asset (optional)', 'Link', FALSE, 'Asset', 3),
('ServiceTicket', 'description', 'Description', 'Data', TRUE, NULL, 4),
('ServiceTicket', 'priority', 'Priority', 'Select', TRUE, 'Low,Medium,High,Critical', 5),
('ServiceTicket', 'assigned_to', 'Assigned To (technician)', 'Data', FALSE, NULL, 6),
('ServiceTicket', 'respond_by_date', 'Respond By', 'Date', FALSE, NULL, 7),
('ServiceTicket', 'resolve_by_date', 'Resolve By', 'Date', FALSE, NULL, 8),
('ServiceTicket', 'resolution_notes', 'Resolution Notes', 'Data', FALSE, NULL, 9),
('ServiceTicket', 'cancellation_reason', 'Cancellation Reason', 'Data', FALSE, NULL, 10),
('ServiceTicket', 'service_contract_id', 'Service Contract (optional)', 'Link', FALSE, 'ServiceContract', 11),
('ServiceTicket', 'status', 'Status', 'Select', TRUE, 'Draft,Assigned,InProgress,Resolved,Closed,Cancelled', 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 37.8.3: ServiceContract (AMC). One asset per contract, a stated scope
-- decision - multi-asset coverage would need a JSONTable line-item list,
-- a real but separate extension. recurring_sales_contract_id is an optional
-- reference to Stage 37.6's RecurringSalesContract for the billing leg,
-- rather than extending that doctype's own shape.
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('ServiceContract', 'code', 'Contract Number', 'Data', TRUE, NULL, 1),
('ServiceContract', 'customer', 'Customer', 'Data', TRUE, NULL, 2),
('ServiceContract', 'asset', 'Asset (optional)', 'Link', FALSE, 'Asset', 3),
('ServiceContract', 'start_date', 'Start Date', 'Date', TRUE, NULL, 4),
('ServiceContract', 'end_date', 'End Date', 'Date', TRUE, NULL, 5),
('ServiceContract', 'visits_included', 'Visits Included', 'Number', TRUE, NULL, 6),
('ServiceContract', 'visits_used', 'Visits Used (system-managed)', 'Number', FALSE, NULL, 7),
('ServiceContract', 'recurring_sales_contract_id', 'Billing Contract (optional)', 'Link', FALSE, 'RecurringSalesContract', 8),
('ServiceContract', 'status', 'Status', 'Select', TRUE, 'Active,Expired,Cancelled', 9)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'ServiceTicket', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ServiceTicket', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'ServiceContract', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'ServiceContract', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows - the same pattern every prior
-- Stage 35-37 migration in this file family uses.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  meta_doctypes TEXT[] := ARRAY['ServiceTicket', 'ServiceContract'];
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata
     WHERE schema_name LIKE 'tenant\_%' ESCAPE '\' AND schema_name <> 'tenant_default'
  LOOP
    IF to_regclass(format('%I.doctype_meta', schema_rec.schema_name)) IS NULL THEN
      CONTINUE;
    END IF;

    EXECUTE format($f$
      INSERT INTO %I.doctype_meta (name, module, module_key, document_type)
      SELECT name, module, module_key, document_type
        FROM tenant_default.doctype_meta WHERE name = ANY($1)
      ON CONFLICT (name) DO UPDATE SET
        module = EXCLUDED.module, module_key = EXCLUDED.module_key, document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING meta_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = ANY($1)
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label, fieldtype = EXCLUDED.fieldtype, mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options, display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING meta_doctypes;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = ANY($1)
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read, allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update, allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING meta_doctypes;
  END LOOP;
END $$;
