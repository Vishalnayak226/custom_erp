-- Stage 26.8.8/26.8.9/26.8.10 (HR/Payroll Sprint P2 follow-up): KRA/KPI
-- appraisal cycles, training, grievance handling. Go-ahead given
-- 2026-07-27 for all five P2 bundles previously deferred pending a real
-- pilot customer/HR-domain process design input - built as generic,
-- tenant-configurable process frameworks (same treatment as e.g.
-- approval_rules) rather than one company's specific HR process.

-- 26.8.8: AppraisalCycle (the period + KRA/KPI template) is a plain Master;
-- Appraisal (one employee's ratings against that cycle's template) is a
-- Transaction routed through the existing approval engine for manager
-- sign-off, same "flat doctype + SubmitForApproval" shape as
-- PurchaseRequisition/JournalVoucher before it.
INSERT INTO tenant_default.doctype_meta (name, module, document_type, module_key) VALUES
('AppraisalCycle', 'HR', 'Master', 'hr'),
('Appraisal', 'HR', 'Transaction', 'hr'),
('TrainingProgram', 'HR', 'Master', 'hr'),
('TrainingRecord', 'HR', 'Transaction', 'hr'),
('Grievance', 'HR', 'Transaction', 'hr')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('AppraisalCycle', 'code', 'Cycle Name', 'Data', TRUE, NULL, 1),
('AppraisalCycle', 'period_start', 'Period Start', 'Data', TRUE, NULL, 2),
('AppraisalCycle', 'period_end', 'Period End', 'Data', TRUE, NULL, 3),
('AppraisalCycle', 'kra_template', 'KRA/KPI Template (JSON: [{"kra":"...","weight":..}])', 'Data', TRUE, NULL, 4),
('AppraisalCycle', 'status', 'Status', 'Select', TRUE, 'Draft,Active,Closed', 5)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Appraisal', 'code', 'Appraisal Number', 'Data', TRUE, NULL, 1),
('Appraisal', 'cycle_id', 'Appraisal Cycle', 'Link', TRUE, 'AppraisalCycle', 2),
('Appraisal', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 3),
('Appraisal', 'ratings_json', 'Ratings (JSON: [{"kra":"...","self_rating":..,"manager_rating":..}])', 'Data', FALSE, NULL, 4),
('Appraisal', 'overall_rating', 'Overall Rating (1-5)', 'Number', FALSE, NULL, 5),
('Appraisal', 'comments', 'Manager Comments', 'Data', FALSE, NULL, 6),
('Appraisal', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 7)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TrainingProgram', 'code', 'Program Name', 'Data', TRUE, NULL, 1),
('TrainingProgram', 'category', 'Category', 'Data', FALSE, NULL, 2),
('TrainingProgram', 'description', 'Description', 'Data', FALSE, NULL, 3),
('TrainingProgram', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 4)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('TrainingRecord', 'code', 'Record Number', 'Data', TRUE, NULL, 1),
('TrainingRecord', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('TrainingRecord', 'program_id', 'Training Program', 'Link', TRUE, 'TrainingProgram', 3),
('TrainingRecord', 'completion_date', 'Completion Date', 'Data', FALSE, NULL, 4),
('TrainingRecord', 'score', 'Score (optional)', 'Number', FALSE, NULL, 5),
('TrainingRecord', 'status', 'Status', 'Select', TRUE, 'Enrolled,Completed,Failed', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- 26.8.10: Grievance - employee-submitted (via the existing "My Requests"
-- self-service tab, 26.8.5's precedent), routed through the approval
-- engine for HR investigation/resolution sign-off instead of a new
-- case-management workflow. "Approved" here means "resolved in the
-- employee's favor/actioned"; "Rejected" means "investigated, no action" -
-- reusing the same two-outcome vocabulary the approval engine already
-- gives every other doctype, rather than inventing a third status.
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Grievance', 'code', 'Grievance Number', 'Data', TRUE, NULL, 1),
('Grievance', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('Grievance', 'category', 'Category', 'Select', TRUE, 'Harassment,Compensation,Workplace Safety,Discrimination,Other', 3),
('Grievance', 'description', 'Description', 'Data', TRUE, NULL, 4),
('Grievance', 'resolution_notes', 'Resolution Notes', 'Data', FALSE, NULL, 5),
('Grievance', 'status', 'Status', 'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- allow_update=TRUE on the submitting roles below (Store Manager for
-- Appraisal, Store Manager/Cashier for Grievance) is required, not
-- optional: POST /api/v1/approval/submit's own permission gate
-- (handleSubmitApproval, internal/server/handlers_pim_pos_finance.go)
-- checks allow_update on the doctype, not allow_create - confirmed against
-- QualityInspection's own identical grant (migrations_stage26_9_
-- manufacturing_mrp.sql). This is coarse doctype-level permission, not
-- row-level ownership (a Cashier who can submit their own Grievance can
-- technically update anyone else's) - the same accepted tradeoff Leave/
-- ExpenseClaim/QualityInspection already make, not a new gap.
INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'AppraisalCycle', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'AppraisalCycle', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'Appraisal', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Appraisal', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'TrainingProgram', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'TrainingProgram', TRUE, FALSE, FALSE, FALSE),
('HR/Admin', 'TrainingRecord', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'TrainingRecord', TRUE, TRUE, TRUE, FALSE),
('HR/Admin', 'Grievance', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Grievance', TRUE, TRUE, TRUE, FALSE),
('Cashier', 'Grievance', TRUE, TRUE, TRUE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- Appraisal sign-off routes to the employee's people-manager tier (Store
-- Manager); Grievance investigation/resolution routes to HR/Admin - the
-- same "every amount routes to one fixed role, not amount-slab-tiered"
-- shape JournalVoucher's own rule above uses (neither doctype has a
-- meaningful monetary amount to slab against).
INSERT INTO tenant_default.approval_rules (doctype, min_amount, max_amount, required_role) VALUES
('Appraisal', 0, NULL, 'Store Manager'),
('Grievance', 0, NULL, 'HR/Admin')
ON CONFLICT (doctype, min_amount) DO NOTHING;
