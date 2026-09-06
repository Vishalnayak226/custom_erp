package engines

import "sort"

// Stage 47.1.3 - sensitive-field policy (audit findings A-01/A-09,
// docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md).
//
// Before this file, field-level access was purely additive-deny: the
// field_permissions table (Stage 16.7) holds one row per role/doctype/field
// that should be HIDDEN, and fieldPermissions() returns only those rows. A
// doctype with no rows at all therefore exposed every field to every role
// that passed the doctype gate - "no field rows means every field", exactly
// the model this item exists to replace. The seed data proves the
// consequence rather than merely implying it: db/migration.sql grants
// ('Cashier', 'Payslip', TRUE, ...) and ('Cashier', 'EmployeeLoan', TRUE,
// ...) doctype-level read, and no field_permissions row anywhere restricts
// Payslip, so a Cashier could read every colleague's gross pay, net pay, PF,
// ESI, PT, TDS and loan deduction through the ordinary
// GET /api/v1/doc/Payslip endpoint. Same shape for BankAccount/Vendor bank
// details and SalaryStructure (Store Manager read), and for
// WebhookSubscription.secret (Store Manager read+write).
//
// The policy here is deny-by-default for a NAMED set of sensitive fields:
// declared in code, applying to every tenant, and merged into
// fieldPermissions() itself so all five existing choke points inherit it
// with no call-site change -
//
//	read (single doc + list):  FilterFieldsForRole         (handlers_core_doc_engine.go:188/:320)
//	field meta / form render:  FilterFieldMetaForRole      (handlers_core_doc_engine.go:1282)
//	write:                     RejectRestrictedFieldWrites (handlers_core_doc_engine.go:350)
//	bulk CSV import:           RejectRestrictedFieldWrites (import.go:262)
//	PIM bulk edit:             RejectRestrictedFieldWrites (pim_bulk.go:83)
//
// - which is the same "attach it at the one shared choke point every caller
// already runs through" rule the rest of this codebase follows.
//
// Report/export output is deliberately NOT re-implemented here: reports do
// not read documents field-by-field, and the report framework already has
// its own column-level control (ReportColumn.Sensitive plus
// maskSensitiveColumns/reportFullVisibilityRoles in report_registry.go),
// which the async export path also runs through because
// StartReportExportWorker calls RunReport. PIM export templates
// (RunPIMExportTemplate) read a closed switch of catalog columns, not
// arbitrary document fields, so they cannot express a field named here.
//
// A tenant's own field_permissions rows still work and still apply; the
// policy can only TIGHTEN what they allow, never loosen it. That direction
// matters: an existing tenant row that hides a field keeps hiding it, and no
// tenant can grant a role read access to payroll or a connector secret by
// inserting a row.

// Sensitive-field categories. These are the six the item names explicitly
// ("payroll, bank, grievance, cost/margin, connector secret and personal
// data"); the string values match the capability vocabulary
// route_capabilities.go already uses so an administrator sees one naming
// scheme, not two.
const (
	SensitiveCategoryPayroll   = "hr.payroll"
	SensitiveCategoryBank      = "finance.bank"
	SensitiveCategoryGrievance = "hr.grievance"
	SensitiveCategoryCost      = "inventory.cost"
	SensitiveCategorySecret    = "integration.secret"
	SensitiveCategoryPersonal  = "privacy.personal"
)

// anySensitiveWriter is the WriteRoles sentinel for a field any role that
// already passed the doctype gate may WRITE but only a listed role may READ.
// Two real cases need it, and naming a "write-only" tier for them is honest
// rather than convenient:
//
//   - WebhookSubscription.secret - a shared secret is supposed to be
//     write-only. Store Manager holds create/update on the doctype today
//     (db seed), so denying the write would remove a working capability to
//     close a disclosure risk that denying the READ already closes.
//   - Grievance.description - an employee of any role submits their own
//     grievance through the My Requests self-service panel (app.js's
//     "Submit Grievance"). Denying that write would break the submission
//     path; denying the read is the actual privacy control.
const anySensitiveWriter = "*"

type sensitiveFieldRule struct {
	Category string
	// ReadRoles/WriteRoles list the roles allowed IN ADDITION to Super
	// Admin (IsSuperAdmin, so the legacy "HR/Admin" name resolves too).
	// An empty list means Super Admin only - the deny-by-default case.
	ReadRoles  []string
	WriteRoles []string
	// Why is the one-line reason an administrator sees in the
	// "why allowed/denied" explanation (ExplainSensitiveFields).
	Why string
}

// sensitiveFields is doctype -> fieldname -> rule.
//
// Field names were taken from the actual doctype_fields seeds in db/*.sql
// (extracted mechanically, then reviewed one by one - unlike
// route_capabilities.go's 454 routes, this is a small enough set to justify
// individual review, and each entry below carries a real access decision).
// A field does NOT have to be a declared doctype_fields row to be listed:
// documents are JSONB, so Item.cost_price - which Stage 16.7's own seed
// restricts for Cashier but which no doctype_fields row declares - is
// covered here too.
var sensitiveFields = map[string]map[string]sensitiveFieldRule{
	// --- Payroll (Super Admin only, read and write) -------------------
	// Matches route_capabilities.go's "hr.payroll" capability exactly, so
	// the run/post/disburse ROUTES and the payslip DATA agree. A-09's
	// finding is precisely this pair being out of step.
	"Payslip": {
		"gross_pay":      payrollRule("Payslip amounts identify an individual's pay"),
		"net_pay":        payrollRule("Payslip amounts identify an individual's pay"),
		"pf_deduction":   payrollRule("Statutory deduction on an individual's payslip"),
		"esi_deduction":  payrollRule("Statutory deduction on an individual's payslip"),
		"pt_deduction":   payrollRule("Statutory deduction on an individual's payslip"),
		"tds_deduction":  payrollRule("Statutory deduction on an individual's payslip"),
		"loan_deduction": payrollRule("Reveals an individual's outstanding salary advance"),
	},
	"SalaryStructure": {
		"basic":            payrollRule("Salary component - individual compensation"),
		"hra":              payrollRule("Salary component - individual compensation"),
		"other_allowances": payrollRule("Salary component - individual compensation"),
		"pf_percent":       payrollRule("Salary component - individual compensation"),
		"esi_percent":      payrollRule("Salary component - individual compensation"),
		"pt_amount":        payrollRule("Salary component - individual compensation"),
	},
	"EmployeeLoan": {
		"principal_amount":    payrollRule("Salary advance - individual financial position"),
		"monthly_deduction":   payrollRule("Salary advance - individual financial position"),
		"outstanding_balance": payrollRule("Salary advance - individual financial position"),
	},

	// --- Bank details (Super Admin only, read and write) --------------
	// Store Manager holds read-only on BankAccount/Vendor/Employee today
	// and no create/update on any of them, so this removes disclosure
	// without removing a single working action.
	"BankAccount": {
		"account_number": bankRule("Company bank account number"),
		"ifsc_code":      bankRule("Company bank account routing detail"),
	},
	"Vendor": {
		"bank_account_number": bankRule("Vendor payment account - payment-diversion fraud target"),
		"bank_ifsc":           bankRule("Vendor payment account - payment-diversion fraud target"),
	},
	"Employee": {
		"bank_account_no": {Category: SensitiveCategoryPayroll, Why: "Employee salary account - payroll-grade personal data"},
		"bank_ifsc":       {Category: SensitiveCategoryPayroll, Why: "Employee salary account - payroll-grade personal data"},
	},

	// --- Grievance (read Super Admin only; description writable by the
	// submitting employee, whatever their role) -----------------------
	"Grievance": {
		"description": {
			Category:   SensitiveCategoryGrievance,
			WriteRoles: []string{anySensitiveWriter},
			Why:        "Grievance content - readable only by the role that adjudicates it; any employee may submit their own",
		},
		"resolution_notes": {
			Category: SensitiveCategoryGrievance,
			Why:      "HR's adjudication of a grievance",
		},
	},

	// --- Cost / margin (Super Admin + Store Manager) ------------------
	// Stage 16.7's demonstration seed already hid Item.cost_price from
	// Cashier for one tenant; this makes it structural and
	// tenant-independent, and extends the same rule to the other
	// cost-bearing masters.
	// Stage 47.2.2 adds standard_cost: it is the server-side COGS fallback the
	// client's cost_price was replaced with, so it reveals exactly the margin
	// cost_price did and must be hidden from the same roles.
	"Item":            {"cost_price": costRule("Purchase cost - reveals margin on every sale"), "standard_cost": costRule("Standard cost - reveals margin on every sale")},
	"Asset":           {"cost": costRule("Asset acquisition cost")},
	"BOM":             {"standard_cost": costRule("Standard cost - reveals manufacturing margin")},
	"ProductionOrder": {"actual_cost": costRule("Actual production cost - reveals manufacturing margin")},
	"WorkCenter":      {"cost_per_hour": costRule("Internal costing rate")},

	// --- Connector secrets (Super Admin read; see anySensitiveWriter) --
	"WebhookSubscription": {
		"secret": {
			Category:   SensitiveCategorySecret,
			WriteRoles: []string{anySensitiveWriter},
			Why:        "Webhook signing secret - write-only: a holder can forge signed deliveries",
		},
	},
	"RoboticsIntegrationCredential": {
		"api_key": {Category: SensitiveCategorySecret, Why: "Robotics integration credential"},
	},
	"PIMCatalog": {
		"share_token_hash": {Category: SensitiveCategorySecret, Why: "Catalog share token - system-generated, never hand-entered"},
	},
	"PIMImportSchedule": {
		"hook_token_hash": {Category: SensitiveCategorySecret, Why: "Import hook token - system-generated, never hand-entered"},
	},

	// --- Personal data (Super Admin + Store Manager) ------------------
	// Deliberately narrow. Customer phone/email/name are POS-operational
	// (a Cashier looks a customer up by phone every transaction) and are
	// NOT restricted here; date of birth is not needed to complete a sale
	// and no screen in app.js reads or writes it today, so restricting it
	// costs nothing and closes a real DPDP-relevant exposure.
	"Customer": {
		"date_of_birth": {
			Category:   SensitiveCategoryPersonal,
			ReadRoles:  []string{RoleStoreManager},
			WriteRoles: []string{RoleStoreManager},
			Why:        "Customer date of birth - personal data with no POS-operational need",
		},
	},
}

func payrollRule(why string) sensitiveFieldRule {
	return sensitiveFieldRule{Category: SensitiveCategoryPayroll, Why: why}
}

func bankRule(why string) sensitiveFieldRule {
	return sensitiveFieldRule{Category: SensitiveCategoryBank, Why: why}
}

func costRule(why string) sensitiveFieldRule {
	return sensitiveFieldRule{
		Category:   SensitiveCategoryCost,
		ReadRoles:  []string{RoleStoreManager},
		WriteRoles: []string{RoleStoreManager},
		Why:        why,
	}
}

// RoleMaySeeCost reports whether a role is allowed to see cost/margin figures.
//
// The field-permission machinery in this file covers stored document fields,
// which is the right shape for every doctype read. Stage 47.2.2 needs the same
// decision for a COMPUTED figure that belongs to no document - the cost_total
// a checkout response used to return to whoever rang the sale up - so this
// exposes the one rule (Super Admin, plus whoever costRule names) rather than
// letting the POS handler invent a second, drifting answer to the same
// question.
func RoleMaySeeCost(role string) bool {
	if IsSuperAdmin(role) {
		return true
	}
	return roleListAllows(costRule("").ReadRoles, role)
}

// roleListAllows reports whether role appears in a rule's allow list. Super
// Admin is handled by the caller (IsSuperAdmin), never listed here.
func roleListAllows(allowed []string, role string) bool {
	for _, r := range allowed {
		if r == anySensitiveWriter {
			return true
		}
		if normalizeRoleKey(r) == normalizeRoleKey(role) {
			return true
		}
	}
	return false
}

// sensitiveFieldPolicy returns the deny entries this policy adds for a
// role/doctype pair: one FieldPermission per sensitive field the role may
// not fully use. A Super Admin gets nil (nothing to add).
//
// Read and write are decided independently, so a write-only secret and a
// read-only cost field both express correctly.
func sensitiveFieldPolicy(role, doctype string) map[string]FieldPermission {
	rules, ok := sensitiveFields[doctype]
	if !ok || IsSuperAdmin(role) {
		return nil
	}
	out := make(map[string]FieldPermission, len(rules))
	for field, rule := range rules {
		read := roleListAllows(rule.ReadRoles, role)
		write := roleListAllows(rule.WriteRoles, role)
		if read && write {
			continue
		}
		out[field] = FieldPermission{AllowRead: read, AllowWrite: write}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SensitiveFieldNames lists the fields this policy governs on a doctype,
// sorted, regardless of role. Returns nil for a doctype with none.
func SensitiveFieldNames(doctype string) []string {
	rules, ok := sensitiveFields[doctype]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(rules))
	for field := range rules {
		names = append(names, field)
	}
	sort.Strings(names)
	return names
}

// SensitiveFieldDoctypes lists every doctype the policy covers, sorted.
func SensitiveFieldDoctypes() []string {
	names := make([]string, 0, len(sensitiveFields))
	for doctype := range sensitiveFields {
		names = append(names, doctype)
	}
	sort.Strings(names)
	return names
}

// SensitiveFieldAccess is one row of the administrator-facing
// "why allowed/denied" explanation.
type SensitiveFieldAccess struct {
	Doctype   string `json:"doctype"`
	Field     string `json:"field"`
	Category  string `json:"category"`
	CanRead   bool   `json:"can_read"`
	CanWrite  bool   `json:"can_write"`
	Reason    string `json:"reason"`
	Sensitive bool   `json:"sensitive"`
}

// ExplainSensitiveFields answers "what may this role see on this doctype,
// and why" for every sensitive field on it. Super Admin sees everything and
// the reason still says why the field is sensitive, rather than the list
// coming back empty and looking like the doctype has no sensitive fields.
func ExplainSensitiveFields(role, doctype string) []SensitiveFieldAccess {
	rules, ok := sensitiveFields[doctype]
	if !ok {
		return nil
	}
	superAdmin := IsSuperAdmin(role)
	out := make([]SensitiveFieldAccess, 0, len(rules))
	for _, field := range SensitiveFieldNames(doctype) {
		rule := rules[field]
		access := SensitiveFieldAccess{
			Doctype: doctype, Field: field, Category: rule.Category,
			Reason: rule.Why, Sensitive: true,
		}
		if superAdmin {
			access.CanRead, access.CanWrite = true, true
		} else {
			access.CanRead = roleListAllows(rule.ReadRoles, role)
			access.CanWrite = roleListAllows(rule.WriteRoles, role)
		}
		out = append(out, access)
	}
	return out
}
