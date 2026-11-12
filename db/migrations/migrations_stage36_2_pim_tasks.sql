-- ---------------------------------------------------------------------------
-- Stage 36.2: the PIM task & workflow engine.
--
-- Why this is not the approval engine. We already have maker-checker approvals,
-- and they are not tasks. An approval asks "may this saved document proceed?"
-- and is answered once, by someone with the authority, about a document that
-- already exists. A task says "someone must go and make this product better",
-- is owned by a named person, has a due date, survives being partly done, and
-- exists precisely because the product is *not* ready. Modelling one on the
-- other would break both: an approval cannot be assigned, re-assigned, or left
-- open for a week, and a task must never be able to move a document's approval
-- state. So this is a separate, additive set of doctypes, and no approval path
-- is touched.
--
-- Four doctypes, in dependency order:
--   PIMTaskTemplate       (Master)      - a reusable task definition
--   PIMWorkflowDefinition (Master)      - ordered stages, each optionally
--                                         instantiating a template
--   PIMTask               (Transaction) - one unit of work for one person
--   PIMWorkflowRun        (Transaction) - one product travelling a workflow
--
-- Everything is additive. Nothing here alters an existing doctype, column or
-- row, so a tenant that never opens the Tasks screen is byte-identical to what
-- it is today.
-- ---------------------------------------------------------------------------

INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('PIMTaskTemplate',       'PIM', 'pim', 'Master'),
('PIMWorkflowDefinition', 'PIM', 'pim', 'Master'),
('PIMTask',               'PIM', 'pim', 'Transaction'),
('PIMWorkflowRun',        'PIM', 'pim', 'Transaction')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.2.2 - PIMTaskTemplate.
--
-- A template is instantiated against a product group, producing one task per
-- resolved product. due_in_days rather than a fixed date: a template is reused
-- for months and a stored absolute date would be in the past by its second use.
-- title_pattern takes {item_code} / {item_name} / {family} so a hundred
-- generated tasks read as a hundred different jobs in an inbox rather than a
-- hundred identical rows.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMTaskTemplate', 'code', 'Template Code', 'Data', TRUE, NULL, 1),
('PIMTaskTemplate', 'name', 'Template Name', 'Data', TRUE, NULL, 2),
('PIMTaskTemplate', 'task_type', 'Task Type', 'Select', TRUE,
 'Enrichment,Imagery,Attributes,Translation,Review,Other', 3),
('PIMTaskTemplate', 'title_pattern', 'Title Pattern', 'Data', TRUE, NULL, 4),
('PIMTaskTemplate', 'default_assignee', 'Default Assignee', 'Data', FALSE, NULL, 5),
('PIMTaskTemplate', 'default_role', 'Default Role', 'Data', FALSE, NULL, 6),
('PIMTaskTemplate', 'due_in_days', 'Due In (days)', 'Number', FALSE, NULL, 7),
('PIMTaskTemplate', 'priority', 'Priority', 'Select', FALSE, 'Low,Normal,High', 8),
('PIMTaskTemplate', 'instructions', 'Instructions', 'Data', FALSE, NULL, 9),
('PIMTaskTemplate', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.2.3 - PIMWorkflowDefinition.
--
-- The stages are a JSONTable, not a second table, for the same reason the rest
-- of this codebase uses JSONTable for line items: they are only ever read as
-- part of their parent, never queried across definitions.
--
-- Deliberately table-driven and NOT a scripting runtime. entry_condition and
-- exit_condition are picked from a closed vocabulary the engine understands
-- (engines/pim_workflow.go, pimWorkflowConditions) with one operand in
-- *_value. That is enough to express "don't start enrichment until the product
-- has a family" and "don't leave imagery until it has a main image", which is
-- what stage conditions are actually for. It cannot express arbitrary logic,
-- which is the point: a workflow definition is authored by a category manager
-- in a form, and a form that accepts code is a remote-execution surface.
--
-- parallel_group: stages sharing a non-empty value are entered together and
-- must all satisfy their exit before the run advances past the group. Blank
-- means the stage is its own group, i.e. plain sequential behaviour.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMWorkflowDefinition', 'code', 'Workflow Code', 'Data', TRUE, NULL, 1),
('PIMWorkflowDefinition', 'name', 'Workflow Name', 'Data', TRUE, NULL, 2),
('PIMWorkflowDefinition', 'description', 'Description', 'Data', FALSE, NULL, 3),
('PIMWorkflowDefinition', 'stages', 'Stages', 'JSONTable', TRUE,
 '[{"key":"stage_code","label":"Stage Code","type":"text","required":true},
   {"key":"label","label":"Stage Name","type":"text","required":true},
   {"key":"sequence","label":"Sequence","type":"number","required":true},
   {"key":"parallel_group","label":"Parallel Group","type":"text"},
   {"key":"assignee","label":"Assignee","type":"text"},
   {"key":"assignee_role","label":"Assignee Role","type":"text"},
   {"key":"task_template","label":"Task Template","type":"link","link":"PIMTaskTemplate"},
   {"key":"due_in_days","label":"Due In (days)","type":"number"},
   {"key":"entry_condition","label":"Entry Condition","type":"text"},
   {"key":"entry_value","label":"Entry Value","type":"text"},
   {"key":"exit_condition","label":"Exit Condition","type":"text"},
   {"key":"exit_value","label":"Exit Value","type":"text"}]', 4),
('PIMWorkflowDefinition', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.2.1 - PIMTask.
--
-- `status` is the document status column (see handlers_core_doc_engine.go,
-- which copies data->>'status' into documents.status), so the task's own state
-- machine is the one the generic list, RBAC and audit trail already understand
-- - no parallel task_status field that only this module knows to read.
--
-- scope_type/scope_ref record what the task was *asked about*; item_code is the
-- concrete product it acts on. A group-scoped instantiation writes both: the
-- group in scope_ref, so "why do I have this?" is answerable months later, and
-- the product in item_code, so the task is actionable on its own.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMTask', 'code', 'Task ID', 'Data', TRUE, NULL, 1),
('PIMTask', 'title', 'Title', 'Data', TRUE, NULL, 2),
('PIMTask', 'task_type', 'Task Type', 'Select', FALSE,
 'Enrichment,Imagery,Attributes,Translation,Review,Other', 3),
('PIMTask', 'scope_type', 'Scope', 'Select', TRUE, 'Product,Product Group,Attribute Set', 4),
('PIMTask', 'scope_ref', 'Scope Reference', 'Data', TRUE, NULL, 5),
('PIMTask', 'item_code', 'Item', 'Link', FALSE, 'Item', 6),
('PIMTask', 'assignee', 'Assignee', 'Data', FALSE, NULL, 7),
('PIMTask', 'assignee_role', 'Assignee Role', 'Data', FALSE, NULL, 8),
('PIMTask', 'due_date', 'Due Date', 'Date', FALSE, NULL, 9),
('PIMTask', 'priority', 'Priority', 'Select', FALSE, 'Low,Normal,High', 10),
('PIMTask', 'instructions', 'Instructions', 'Data', FALSE, NULL, 11),
('PIMTask', 'comments', 'Comments', 'JSONTable', FALSE,
 '[{"key":"at","label":"At","type":"text","required":true},
   {"key":"author","label":"Author","type":"text","required":true},
   {"key":"comment","label":"Comment","type":"text","required":true}]', 12),
('PIMTask', 'template', 'Created From Template', 'Data', FALSE, NULL, 13),
('PIMTask', 'workflow_run', 'Workflow Run', 'Data', FALSE, NULL, 14),
('PIMTask', 'stage', 'Workflow Stage', 'Data', FALSE, NULL, 15),
('PIMTask', 'completed_at', 'Completed At', 'Data', FALSE, NULL, 16),
('PIMTask', 'completed_by', 'Completed By', 'Data', FALSE, NULL, 17),
('PIMTask', 'status', 'Status', 'Select', TRUE,
 'Open,In Progress,Blocked,Done,Cancelled', 18)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 36.2.4 - PIMWorkflowRun.
--
-- One run per product per workflow. `activity` is the run's own append-only
-- log, kept on the run rather than in the global audit log because it is read
-- constantly (every time someone opens the run) and the questions it answers -
-- which stage, when, who paused it, why it did not advance - are only ever
-- asked about one run at a time. The global audit log still receives the
-- coarse events; this is the detail beneath them.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.doctype_fields
    (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('PIMWorkflowRun', 'code', 'Run ID', 'Data', TRUE, NULL, 1),
('PIMWorkflowRun', 'workflow', 'Workflow', 'Link', TRUE, 'PIMWorkflowDefinition', 2),
('PIMWorkflowRun', 'item_code', 'Item', 'Link', TRUE, 'Item', 3),
('PIMWorkflowRun', 'current_stage', 'Current Stage', 'Data', FALSE, NULL, 4),
('PIMWorkflowRun', 'current_group', 'Current Parallel Group', 'Data', FALSE, NULL, 5),
('PIMWorkflowRun', 'started_at', 'Started At', 'Data', FALSE, NULL, 6),
('PIMWorkflowRun', 'completed_at', 'Completed At', 'Data', FALSE, NULL, 7),
('PIMWorkflowRun', 'blocked_reason', 'Blocked Reason', 'Data', FALSE, NULL, 8),
('PIMWorkflowRun', 'activity', 'Activity Log', 'JSONTable', FALSE,
 '[{"key":"at","label":"At","type":"text","required":true},
   {"key":"actor","label":"Actor","type":"text","required":true},
   {"key":"event","label":"Event","type":"text","required":true},
   {"key":"detail","label":"Detail","type":"text"}]', 9),
('PIMWorkflowRun', 'status', 'Status', 'Select', TRUE,
 'Running,Paused,Completed,Cancelled', 10)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Permissions.
--
-- 'Super Admin', not 'HR/Admin': on an already-migrated database stage40_3 has
-- renamed that role away, so inserting it here would create a row no session
-- can ever match. Same reasoning, and the same choice, as
-- migrations_stage35_2_oms_console.sql.
--
-- Store Manager gets full task rights but cannot delete a workflow definition:
-- deleting a definition out from under running instances is an authoring
-- action, not an operating one.
-- ---------------------------------------------------------------------------
INSERT INTO tenant_default.role_permissions
    (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('Super Admin',   'PIMTaskTemplate',       TRUE, TRUE, TRUE, TRUE),
('Super Admin',   'PIMWorkflowDefinition', TRUE, TRUE, TRUE, TRUE),
('Super Admin',   'PIMTask',               TRUE, TRUE, TRUE, TRUE),
('Super Admin',   'PIMWorkflowRun',        TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMTaskTemplate',       TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'PIMWorkflowDefinition', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'PIMTask',               TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'PIMWorkflowRun',        TRUE, TRUE, TRUE, FALSE),
-- A cashier never authors a workflow, but a task inbox is only useful if the
-- people doing catalogue work can see and progress their own tasks.
('Cashier',       'PIMTask',               TRUE, FALSE, TRUE, FALSE),
('Cashier',       'PIMWorkflowRun',        TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO UPDATE SET
    allow_read   = EXCLUDED.allow_read,
    allow_create = EXCLUDED.allow_create,
    allow_update = EXCLUDED.allow_update,
    allow_delete = EXCLUDED.allow_delete;

-- ---------------------------------------------------------------------------
-- Uniqueness and indexes.
--
-- Template and workflow codes are operator-facing references quoted in a
-- workflow definition's stage rows, so they must resolve to exactly one row.
-- The documents table's primary key does not cover JSON data, hence explicit
-- partial unique indexes - the same treatment PIMProductGroup.code gets.
--
-- The remaining indexes serve the three hot reads: the My Work inbox
-- (assignee + status), a product's own task list (item_code), and the workflow
-- advance path, which looks up a run's tasks by run id. All partial and
-- doctype-scoped: `documents` holds every doctype in the system, so an
-- unqualified index on data->>'item_code' would carry a row for every
-- attribute value and media record as well as the tasks it is meant for.
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_task_template_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMTaskTemplate' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_workflow_code_unique
    ON tenant_default.documents (UPPER(data->>'code'))
    WHERE doctype = 'PIMWorkflowDefinition' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_pim_task_assignee
    ON tenant_default.documents ((data->>'assignee'))
    WHERE doctype = 'PIMTask' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_pim_task_item
    ON tenant_default.documents ((data->>'item_code'))
    WHERE doctype = 'PIMTask' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_documents_pim_task_run
    ON tenant_default.documents ((data->>'workflow_run'))
    WHERE doctype = 'PIMTask' AND deleted_at IS NULL;

-- One run per (workflow, product) while it is still live. Two concurrent runs
-- of the same workflow over the same product would both create that stage's
-- tasks and both try to advance it, so the second is refused at the database
-- rather than left to a read-then-write race in the engine. Completed and
-- cancelled runs are excluded, so the same product can travel the same
-- workflow again next season.
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_workflow_run_live_unique
    ON tenant_default.documents ((data->>'workflow'), (data->>'item_code'))
    WHERE doctype = 'PIMWorkflowRun' AND deleted_at IS NULL
      AND status IN ('Running', 'Paused');

CREATE INDEX IF NOT EXISTS idx_documents_pim_workflow_run_item
    ON tenant_default.documents ((data->>'item_code'))
    WHERE doctype = 'PIMWorkflowRun' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Existing tenant schemas are independent copies of tenant_default metadata,
-- so backfill them from the canonical rows. A tenant provisioned before Stage
-- 36.2 must receive the same doctypes, fields, permissions and indexes as a
-- tenant provisioned after it, or the module simply would not exist for them.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
  schema_rec RECORD;
  doctype_list TEXT[] := ARRAY['PIMTaskTemplate', 'PIMWorkflowDefinition', 'PIMTask', 'PIMWorkflowRun'];
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
        module = EXCLUDED.module,
        module_key = EXCLUDED.module_key,
        document_type = EXCLUDED.document_type
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      INSERT INTO %I.doctype_fields
        (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      SELECT doctype_name, fieldname, label, fieldtype, mandatory, options, display_order
        FROM tenant_default.doctype_fields WHERE doctype_name = ANY($1)
      ON CONFLICT (doctype_name, fieldname) DO UPDATE SET
        label = EXCLUDED.label,
        fieldtype = EXCLUDED.fieldtype,
        mandatory = EXCLUDED.mandatory,
        options = EXCLUDED.options,
        display_order = EXCLUDED.display_order
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      INSERT INTO %I.role_permissions
        (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      SELECT role, doctype_name, allow_read, allow_create, allow_update, allow_delete
        FROM tenant_default.role_permissions WHERE doctype_name = ANY($1)
      ON CONFLICT (role, doctype_name) DO UPDATE SET
        allow_read = EXCLUDED.allow_read,
        allow_create = EXCLUDED.allow_create,
        allow_update = EXCLUDED.allow_update,
        allow_delete = EXCLUDED.allow_delete
    $f$, schema_rec.schema_name) USING doctype_list;

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_task_template_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMTaskTemplate' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_workflow_code_unique
        ON %I.documents (UPPER(data->>'code'))
       WHERE doctype = 'PIMWorkflowDefinition' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE INDEX IF NOT EXISTS idx_documents_pim_task_assignee
        ON %I.documents ((data->>'assignee'))
       WHERE doctype = 'PIMTask' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE INDEX IF NOT EXISTS idx_documents_pim_task_item
        ON %I.documents ((data->>'item_code'))
       WHERE doctype = 'PIMTask' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE INDEX IF NOT EXISTS idx_documents_pim_task_run
        ON %I.documents ((data->>'workflow_run'))
       WHERE doctype = 'PIMTask' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_pim_workflow_run_live_unique
        ON %I.documents ((data->>'workflow'), (data->>'item_code'))
       WHERE doctype = 'PIMWorkflowRun' AND deleted_at IS NULL
         AND status IN ('Running', 'Paused')
    $f$, schema_rec.schema_name);

    EXECUTE format($f$
      CREATE INDEX IF NOT EXISTS idx_documents_pim_workflow_run_item
        ON %I.documents ((data->>'item_code'))
       WHERE doctype = 'PIMWorkflowRun' AND deleted_at IS NULL
    $f$, schema_rec.schema_name);
  END LOOP;
END $$;
