-- Stage 26.8: HR/Payroll Maturity Sprint (26.8.1-26.8.6).
-- Additive-only, extends Stage 13.13a's HR Foundation (Employee/Attendance/
-- Leave, access-link sync, payroll export) rather than replacing it.
-- 26.8.7 (full KRA/KPI appraisal cycles, training, grievance handling)
-- stays out of scope per the checklist's own [P2] note.

-- ============================================================
-- New GL accounts (26.8.3/26.8.4): Staff Loans Receivable is an Asset
-- (money owed back to the company); PF/ESI/PT Payable are Liabilities
-- alongside the existing TDS Payable (2300, reused here for TDS-on-salary
-- rather than a second TDS account - it's the same statutory liability
-- regardless of source); Salary Expense is the payroll run's debit side.
-- ============================================================
INSERT INTO tenant_default.gl_accounts (account_code, account_name, account_type) VALUES
('1600', 'Staff Loans Receivable', 'Asset'),
('2400', 'Salary Payable', 'Liability'),
('2401', 'PF Payable', 'Liability'),
('2402', 'ESI Payable', 'Liability'),
('2403', 'PT Payable', 'Liability'),
('5500', 'Salary Expense', 'Expense')
ON CONFLICT (account_code) DO NOTHING;

-- ============================================================
-- 26.8.6: Employee additive fields for offboarding (exit date, validated
-- against date_of_joining by validateEmployeeRules/HRPAYR-0156) and
-- 26.8.3's bank-details gate (HRPAYR-0155) before a payslip can be posted.
-- ============================================================
INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Employee', 'date_of_joining', 'Date of Joining', 'Date', FALSE, NULL, 9),
('Employee', 'date_of_exit', 'Date of Exit', 'Date', FALSE, NULL, 10),
('Employee', 'bank_account_no', 'Bank Account Number', 'Data', FALSE, NULL, 11),
('Employee', 'bank_ifsc', 'Bank IFSC', 'Data', FALSE, NULL, 12)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

-- ============================================================
-- 26.8.1: Shift roster / store schedule - extends Attendance (Stage
-- 13.13a) via a new Shift master + ShiftAssignment roster, checked by
-- validateAttendanceRules/HR-0269 only for employees who actually have at
-- least one ShiftAssignment row (opt-in by existing data).
-- ============================================================
INSERT INTO tenant_default.doctype_meta (name, module, module_key, document_type) VALUES
('Shift', 'HR', 'hr', 'Master'),
('ShiftAssignment', 'HR', 'hr', 'Transaction'),
('SalaryStructure', 'HR', 'hr', 'Master'),
('Payslip', 'HR', 'hr', 'Transaction'),
('EmployeeLoan', 'HR', 'hr', 'Transaction'),
('OnboardingChecklist', 'HR', 'hr', 'Transaction')
ON CONFLICT (name) DO NOTHING;

INSERT INTO tenant_default.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order) VALUES
('Shift', 'code', 'Shift Code', 'Data', TRUE, NULL, 1),
('Shift', 'name', 'Name', 'Data', TRUE, NULL, 2),
('Shift', 'start_time', 'Start Time (HH:MM)', 'Data', TRUE, NULL, 3),
('Shift', 'end_time', 'End Time (HH:MM)', 'Data', TRUE, NULL, 4),
('Shift', 'break_mins', 'Break (Minutes)', 'Number', FALSE, NULL, 5),
('Shift', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 6),

('ShiftAssignment', 'code', 'Assignment Code', 'Data', TRUE, NULL, 1),
('ShiftAssignment', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('ShiftAssignment', 'shift_id', 'Shift', 'Link', TRUE, 'Shift', 3),
('ShiftAssignment', 'date', 'Date', 'Date', TRUE, NULL, 4),
('ShiftAssignment', 'location', 'Location', 'Data', FALSE, NULL, 5),
('ShiftAssignment', 'status', 'Status', 'Select', TRUE, 'Assigned,Cancelled', 6),

-- 26.8.2: Salary structure + statutory deduction calc - a real payroll
-- *processing* engine (CalculateSalaryComponents/RunPayroll,
-- engines/hr_payroll.go), a deliberate sibling to the existing payroll
-- *export* (GetPayrollExport, unchanged), same pattern Stage 20.28's
-- vendor-TDS used alongside the plain no-TDS pay function.
('SalaryStructure', 'code', 'Structure Code', 'Data', TRUE, NULL, 1),
('SalaryStructure', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('SalaryStructure', 'basic', 'Basic', 'Number', TRUE, NULL, 3),
('SalaryStructure', 'hra', 'HRA', 'Number', FALSE, NULL, 4),
('SalaryStructure', 'other_allowances', 'Other Allowances', 'Number', FALSE, NULL, 5),
('SalaryStructure', 'pf_percent', 'PF % of Basic', 'Number', FALSE, NULL, 6),
('SalaryStructure', 'esi_percent', 'ESI % of Gross', 'Number', FALSE, NULL, 7),
('SalaryStructure', 'pt_amount', 'Professional Tax (Flat)', 'Number', FALSE, NULL, 8),
('SalaryStructure', 'effective_from', 'Effective From', 'Date', TRUE, NULL, 9),
('SalaryStructure', 'status', 'Status', 'Select', TRUE, 'Active,Inactive', 10),

-- 26.8.3: Payslip generation + payroll-to-GL posting - RunPayroll writes
-- these directly (not via the generic doc-create endpoint, since it also
-- has to read attendance/salary-structure/loan state atomically), so this
-- registration is for the free generic list/detail view only.
('Payslip', 'code', 'Payslip Number', 'Data', TRUE, NULL, 1),
('Payslip', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('Payslip', 'period_from', 'Period From', 'Date', TRUE, NULL, 3),
('Payslip', 'period_to', 'Period To', 'Date', TRUE, NULL, 4),
('Payslip', 'gross_pay', 'Gross Pay', 'Number', TRUE, NULL, 5),
('Payslip', 'pf_deduction', 'PF Deduction', 'Number', FALSE, NULL, 6),
('Payslip', 'esi_deduction', 'ESI Deduction', 'Number', FALSE, NULL, 7),
('Payslip', 'pt_deduction', 'PT Deduction', 'Number', FALSE, NULL, 8),
('Payslip', 'tds_deduction', 'TDS Deduction', 'Number', FALSE, NULL, 9),
('Payslip', 'loan_deduction', 'Loan Deduction', 'Number', FALSE, NULL, 10),
('Payslip', 'net_pay', 'Net Pay', 'Number', TRUE, NULL, 11),
('Payslip', 'status', 'Status', 'Select', TRUE, 'Draft,Posted', 12),

-- 26.8.4: Loans/advances against salary - deducted in the payroll run
-- (RunPayroll/activeLoanDeductionForEmployee), disbursed via
-- DisburseEmployeeLoan (Dr Staff Loans Receivable / Cr Cash-Bank).
('EmployeeLoan', 'code', 'Loan Code', 'Data', TRUE, NULL, 1),
('EmployeeLoan', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('EmployeeLoan', 'principal_amount', 'Principal Amount', 'Number', TRUE, NULL, 3),
('EmployeeLoan', 'monthly_deduction', 'Monthly Deduction', 'Number', TRUE, NULL, 4),
('EmployeeLoan', 'outstanding_balance', 'Outstanding Balance', 'Number', FALSE, NULL, 5),
('EmployeeLoan', 'start_date', 'Start Date', 'Date', FALSE, NULL, 6),
('EmployeeLoan', 'status', 'Status', 'Select', TRUE, 'Draft,Active,Closed', 7),

-- 26.8.6: Onboarding/offboarding checklist + document locker - extends the
-- Employee master with a checklist doctype (items/documents are JSON, same
-- "Data field holding JSON" convention as BOM.components/GRN.received_items).
('OnboardingChecklist', 'code', 'Checklist Code', 'Data', TRUE, NULL, 1),
('OnboardingChecklist', 'employee_id', 'Employee', 'Link', TRUE, 'Employee', 2),
('OnboardingChecklist', 'checklist_type', 'Type', 'Select', TRUE, 'Onboarding,Offboarding', 3),
('OnboardingChecklist', 'items', 'Checklist Items JSON ([{task, done}])', 'Data', FALSE, NULL, 4),
('OnboardingChecklist', 'documents', 'Document Locker JSON ([{name, url}])', 'Data', FALSE, NULL, 5),
('OnboardingChecklist', 'status', 'Status', 'Select', TRUE, 'In Progress,Completed', 6)
ON CONFLICT (doctype_name, fieldname) DO NOTHING;

INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
('HR/Admin', 'Shift', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'ShiftAssignment', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'SalaryStructure', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'Payslip', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'EmployeeLoan', TRUE, TRUE, TRUE, TRUE),
('HR/Admin', 'OnboardingChecklist', TRUE, TRUE, TRUE, TRUE),
('Store Manager', 'Shift', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'ShiftAssignment', TRUE, TRUE, TRUE, FALSE),
('Store Manager', 'SalaryStructure', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'Payslip', TRUE, FALSE, FALSE, FALSE),
('Store Manager', 'EmployeeLoan', TRUE, TRUE, FALSE, FALSE),
('Store Manager', 'OnboardingChecklist', TRUE, TRUE, TRUE, FALSE),
-- 26.8.5 self-service: the Cashier role is this codebase's ordinary
-- non-manager employee login - it can already create ExpenseClaim but not
-- Leave, a real pre-existing gap self-service needs closed. Read-only on
-- Payslip/EmployeeLoan/ShiftAssignment so an employee can see their own pay
-- history/loan balance/roster (self-service screen filters to their own
-- employee_id; this permission row is what makes the underlying read legal
-- at all, same as every other Cashier read-only row in this table).
('Cashier', 'Leave', TRUE, TRUE, FALSE, FALSE),
('Cashier', 'Payslip', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'EmployeeLoan', TRUE, FALSE, FALSE, FALSE),
('Cashier', 'ShiftAssignment', TRUE, FALSE, FALSE, FALSE)
ON CONFLICT (role, doctype_name) DO NOTHING;

-- 26.8.2/26.8.3: a default "SALARY" TDSSection so RunPayroll's TDS-on-salary
-- estimate (CalculateTDS, engines/tds.go - already built for vendor TDS,
-- reused here rather than a second progressive-slab tax engine) has
-- something to look up out of the box. Monthly-equivalent threshold/rate -
-- illustrative statutory defaults, same "admin can retune via the generic
-- doctype-table edit" precedent the existing 194C/194J/194Q rows set.
INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
('SALARY', 'TDSSection', '{"id":"SALARY","code":"SALARY","section_code":"SALARY","description":"TDS on Salary (monthly-equivalent estimate)","threshold_amount":20000,"rate_percent":10,"status":"Active"}', 'Active', 'system')
ON CONFLICT (id) DO NOTHING;
