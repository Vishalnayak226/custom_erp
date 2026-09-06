package engines

import (
	"fmt"
	"sort"

	"custom_erp/db"
)

// Stage 47.1.5 - safe role templates, rebuilt from actual tasks (audit
// finding A-09).
//
// What shipped before this file: three roles, seeded doctype by doctype in
// db/migration.sql and eighteen later migrations, with no statement anywhere
// of what a role is FOR. The result is the finding: Cashier holds
// doctype-level read on Payslip and EmployeeLoan - not because a cashier's
// job needs a colleague's payslip, but because the row was convenient when
// the HR module was seeded. That is what "remove generic sensitive-record
// grants" means here.
//
// A template declares access by MODULE (doctype_meta.module, already the
// organising fact of this schema) plus a small list of per-doctype
// overrides, rather than by enumerating ~200 doctypes twelve times. The same
// reasoning route_capabilities.go used for 454 routes applies: a systematic
// rule that a human can check is more auditable than 2,400 individual
// judgment calls, and the overrides are exactly the places a rule is not
// good enough - which is where the real decisions live and where they are
// therefore visible.
//
// Templates are DECLARATIONS. Nothing here rewrites a tenant's roles on its
// own: applying a template to a tenant goes through 47.1.7's reviewed diff
// (role_template_migration.go), because "do not silently reset legitimate
// custom roles" is that item's whole point.

// AccessLevel is the four-value ladder role_permissions can express.
type AccessLevel int

const (
	AccessNone AccessLevel = iota
	// AccessRead is allow_read only - look, never change.
	AccessRead
	// AccessOperate adds create and update: the level a role that does the
	// work day to day needs.
	AccessOperate
	// AccessManage adds delete. Deliberately rare - deletion is the action
	// that destroys evidence, so only a role that owns the data has it.
	AccessManage
)

func (a AccessLevel) String() string {
	switch a {
	case AccessRead:
		return "read"
	case AccessOperate:
		return "operate"
	case AccessManage:
		return "manage"
	}
	return "none"
}

// Permissions expands a level into the four role_permissions booleans.
func (a AccessLevel) Permissions() (read, create, update, del bool) {
	switch a {
	case AccessRead:
		return true, false, false, false
	case AccessOperate:
		return true, true, true, false
	case AccessManage:
		return true, true, true, true
	}
	return false, false, false, false
}

// RoleTemplate is one role, described by the tasks it performs.
type RoleTemplate struct {
	Name string
	// Summary is the actual job. It is not decoration: every grant below
	// has to be justifiable by this sentence, and the SoD catalog
	// (sod_catalog.go) and the administrator preview both show it.
	Summary string
	// LegacyNames are role strings already in use in tenant data that this
	// template governs. Existing sessions and users keep working under
	// their stored name; the template is what defines their access.
	LegacyNames []string
	// Modules is doctype_meta.module -> level. Applies to every doctype in
	// that module.
	Modules map[string]AccessLevel
	// Doctypes overrides Modules for a named doctype, in either direction.
	Doctypes map[string]AccessLevel
	// Capabilities are route capabilities (route_capabilities.go) this role
	// holds beyond plain "authenticated".
	Capabilities []string
	// GlobalScopes are the scope dimensions this role is NOT confined by
	// (scope_policy.go). Absent means confined to the session's own value.
	GlobalScopes []ScopeDimension
}

// roleTemplates are the twelve the item names. Administrator is the existing
// Super Admin; Store Supervisor and Cashier govern the two other roles this
// product already ships, under their existing names, so applying a template
// to an existing tenant is a reviewable change to a live role rather than
// twelve new ones appearing beside it.
var roleTemplates = []RoleTemplate{
	{
		Name:        "Administrator",
		Summary:     "Configures the system and holds every capability. The break-glass role, not a daily one.",
		LegacyNames: []string{RoleSuperAdmin, RoleLegacySuperAdmin},
		Modules:     allModules(AccessManage),
		Capabilities: []string{
			"audit.logs", "system.logs", "finance.statements",
			"assets.manage", "hr.payroll", "expenses.manage",
			"pos.price_override", "returns.exception",
		},
		GlobalScopes: []ScopeDimension{ScopeLocation, ScopeSelf, ScopeOwner, ScopeEntity},
	},
	{
		Name:        "Store Supervisor",
		Summary:     "Runs one store: opens and closes tills, approves overrides and returns, keeps stock and the team's day right.",
		LegacyNames: []string{RoleStoreManager},
		Modules: map[string]AccessLevel{
			"POS": AccessManage, "Sales": AccessOperate, "CRM": AccessOperate,
			"Inventory": AccessOperate, "OMS": AccessOperate, "Service": AccessOperate,
			"Master Data": AccessRead, "PIM": AccessRead, "Reports": AccessOperate,
			"Core": AccessRead, "HR": AccessRead, "Finance": AccessRead,
			"Procurement": AccessRead, "Quality": AccessRead, "Inbound": AccessRead,
		},
		Doctypes: map[string]AccessLevel{
			// The team's day: a supervisor records these for other people,
			// which is why they are the exceptions to HR being read-only.
			"Attendance": AccessOperate, "Leave": AccessOperate,
			"ShiftAssignment": AccessOperate, "ExpenseClaim": AccessOperate,
			// ...and these are the ones that stay read-only-and-own even
			// though HR is otherwise readable. Scope, not capability, is
			// what confines them (scope_policy.go's self dimension).
			"Payslip": AccessRead, "SalaryStructure": AccessRead,
		},
		// pos.price_override (Stage 47.2.3) is this template's own summary
		// line made enforceable - "approves overrides and returns" was true
		// on paper before there was a command to hold.
		Capabilities: []string{"finance.statements", "assets.manage", "expenses.manage", "pos.price_override", "returns.exception"},
		GlobalScopes: []ScopeDimension{ScopeOwner},
	},
	{
		Name:    "Cashier",
		Summary: "Serves customers at the till: rings up sales, takes payment, handles receipted returns, looks up stock and prices.",
		Modules: map[string]AccessLevel{
			"POS": AccessOperate, "Sales": AccessOperate, "CRM": AccessOperate,
			"Master Data": AccessRead, "Inventory": AccessRead, "PIM": AccessRead,
			"Core": AccessRead,
		},
		Doctypes: map[string]AccessLevel{
			// Self-service only. These three were the "generic sensitive
			// grants" in the shipped seed: read on every colleague's
			// Payslip and EmployeeLoan, and create/update on Grievance
			// with no scope at all. Payslip stays readable because an
			// employee should see their OWN payslip - the fix is scope
			// (self) plus field policy (47.1.3), not removing the row and
			// breaking self-service.
			"Payslip": AccessRead, "Leave": AccessOperate,
			"ExpenseClaim": AccessOperate, "Grievance": AccessOperate,
			"EmployeeLoan": AccessNone,
		},
	},
	{
		Name:    "Picker",
		Summary: "Executes warehouse tasks on an RF device: pick, put away, move, count what the system tells them to.",
		Modules: map[string]AccessLevel{
			"Inventory": AccessRead, "Master Data": AccessRead, "Core": AccessRead,
		},
		Doctypes: map[string]AccessLevel{
			"WarehouseTask": AccessOperate, "FulfillmentTask": AccessOperate,
			"TaskCompletionLog": AccessOperate, "SerialNumber": AccessOperate,
			"Batch": AccessOperate, "CycleCountLine": AccessOperate,
		},
	},
	{
		Name:    "Warehouse Manager",
		Summary: "Owns a warehouse end to end: receiving, put-away strategy, waves, counts, quality holds and the people running them.",
		Modules: map[string]AccessLevel{
			"Inventory": AccessManage, "Inbound": AccessManage, "Quality": AccessOperate,
			"Master Data": AccessOperate, "Reports": AccessOperate, "Core": AccessRead,
			"OMS": AccessOperate, "Procurement": AccessRead, "Manufacturing": AccessRead,
		},
		GlobalScopes: []ScopeDimension{ScopeOwner},
	},
	{
		Name:    "Accounts Payable",
		Summary: "Processes what the company owes: vendor invoices, three-way match, payment proposals, expense reimbursement.",
		Modules: map[string]AccessLevel{
			"Procurement": AccessOperate, "Finance": AccessRead, "Master Data": AccessRead,
			"Reports": AccessOperate, "Core": AccessRead, "Inventory": AccessRead,
		},
		Doctypes: map[string]AccessLevel{
			"VendorInvoice": AccessOperate, "PaymentProposal": AccessOperate,
			"DebitNote": AccessOperate, "ExpenseClaim": AccessOperate,
			"LandedCostVoucher": AccessOperate,
			// A payables clerk must not also create the vendor being paid -
			// that is the vendor-create + payment SoD conflict, enforced as
			// an absent grant rather than only as a warning.
			"Vendor": AccessRead,
		},
		Capabilities: []string{"finance.statements", "expenses.manage"},
		GlobalScopes: []ScopeDimension{ScopeLocation},
	},
	{
		Name:    "Accounts Receivable",
		Summary: "Processes what the company is owed: customer invoices, credit notes, collections and receipt application.",
		Modules: map[string]AccessLevel{
			"Sales": AccessOperate, "Finance": AccessRead, "CRM": AccessRead,
			"Master Data": AccessRead, "Reports": AccessOperate, "Core": AccessRead,
			"OMS": AccessRead,
		},
		Doctypes: map[string]AccessLevel{
			"CreditNote": AccessOperate, "RecurringSalesContract": AccessOperate,
		},
		Capabilities: []string{"finance.statements"},
		GlobalScopes: []ScopeDimension{ScopeLocation},
	},
	{
		Name:    "Accountant",
		Summary: "Owns the ledger: journals, period close, tax returns, fixed assets, the statements that come out of them.",
		Modules: map[string]AccessLevel{
			"Finance": AccessManage, "Reports": AccessOperate, "Master Data": AccessRead,
			"Core": AccessRead, "Sales": AccessRead, "Procurement": AccessRead,
			"Inventory": AccessRead, "Manufacturing": AccessRead,
		},
		Capabilities: []string{"finance.statements", "assets.manage", "expenses.manage"},
		GlobalScopes: []ScopeDimension{ScopeLocation},
	},
	{
		Name:    "HR Manager",
		Summary: "Owns the employee record and the payroll run: hiring, structures, attendance, payslips, grievances.",
		Modules: map[string]AccessLevel{
			"HR": AccessManage, "Master Data": AccessRead, "Reports": AccessOperate,
			"Core": AccessRead,
		},
		Capabilities: []string{"hr.payroll", "expenses.manage"},
		// Global self scope is the whole job - HR works on other people's
		// records by definition.
		GlobalScopes: []ScopeDimension{ScopeSelf, ScopeLocation},
	},
	{
		Name:    "Employee Self-Service",
		Summary: "An employee looking after their own record: their payslip, their leave, their expenses, their grievance. Nobody else's.",
		Modules: map[string]AccessLevel{
			"Core": AccessRead,
		},
		Doctypes: map[string]AccessLevel{
			"Payslip": AccessRead, "SalaryStructure": AccessRead,
			"Attendance": AccessRead, "TrainingRecord": AccessRead,
			"Appraisal": AccessRead, "OnboardingChecklist": AccessRead,
			"Leave": AccessOperate, "ExpenseClaim": AccessOperate,
			"Grievance": AccessOperate, "Employee": AccessRead,
		},
		// No global scope at all: every row this role sees is its own,
		// enforced by scope_policy.go's self dimension.
	},
	{
		Name:    "Auditor",
		Summary: "Reads to verify, and changes nothing: transactions, evidence, the audit and system logs behind them.",
		Modules: allModules(AccessRead),
		Doctypes: map[string]AccessLevel{
			// An auditor verifies that payroll was controlled, not what any
			// individual earns; the payroll register belongs to HR. Left
			// out deliberately - an "auditor sees everything" grant is how
			// the generic sensitive-record grant gets reintroduced.
			"Payslip": AccessNone, "SalaryStructure": AccessNone,
			"EmployeeLoan": AccessNone, "Grievance": AccessNone,
		},
		Capabilities: []string{"audit.logs", "system.logs", "finance.statements"},
		GlobalScopes: []ScopeDimension{ScopeLocation, ScopeOwner},
	},
	{
		Name:    "Integrator",
		Summary: "Wires this system to other systems: connectors, webhooks, import schedules, published catalogs.",
		Modules: map[string]AccessLevel{
			"Integrations": AccessManage, "PIM": AccessOperate, "OMS": AccessRead,
			"Inventory": AccessRead, "Master Data": AccessRead, "Core": AccessRead,
			"Sales": AccessRead,
		},
		GlobalScopes: []ScopeDimension{ScopeLocation},
	},
}

// allModules is the "everything at one level" shorthand, used only by
// Administrator (Manage) and Auditor (Read). Kept as a function so a new
// module added to doctype_meta cannot silently fall outside those two roles.
func allModules(level AccessLevel) map[string]AccessLevel {
	out := map[string]AccessLevel{}
	for _, module := range knownModules {
		out[module] = level
	}
	return out
}

// knownModules is doctype_meta.module's full set as shipped. The completeness
// test compares it against the live tenant schema, so a module added by a
// later migration fails the build until Administrator/Auditor are considered.
var knownModules = []string{
	"CRM", "Core", "Finance", "HR", "Inbound", "Integrations", "Inventory",
	"Manufacturing", "Master Data", "OMS", "PIM", "POS", "Procurement",
	"Quality", "Reports", "Sales", "Service",
}

// RoleTemplates returns the templates in declaration order.
func RoleTemplates() []RoleTemplate {
	out := make([]RoleTemplate, len(roleTemplates))
	copy(out, roleTemplates)
	return out
}

// RoleTemplateFor resolves a role string - canonical name or legacy name,
// case- and space-insensitively - to its template.
func RoleTemplateFor(role string) (RoleTemplate, bool) {
	key := normalizeRoleKey(role)
	for _, template := range roleTemplates {
		if normalizeRoleKey(template.Name) == key {
			return template, true
		}
		for _, legacy := range template.LegacyNames {
			if normalizeRoleKey(legacy) == key {
				return template, true
			}
		}
	}
	return RoleTemplate{}, false
}

// RolesWithCapability lists every role string that holds a capability -
// canonical template names AND their legacy names, so an existing
// "Store Manager" session is judged by the Store Supervisor template it
// actually governs. This is the single source of truth the HTTP layer's
// capability allowlist is built from, so a template edit and a route check
// can never disagree.
func RolesWithCapability(capability string) []string {
	var out []string
	for _, template := range roleTemplates {
		for _, held := range template.Capabilities {
			if held != capability {
				continue
			}
			out = append(out, template.Name)
			out = append(out, template.LegacyNames...)
			break
		}
	}
	sort.Strings(out)
	return out
}

// CapabilitiesForRole is RolesWithCapability read the other way: what this one
// role holds. Stage 47.2.3 needs it so the POS screen can decide whether to
// offer a "price override" button from the same source of truth
// checkRouteCapability enforces with, rather than the frontend hardcoding a
// role name that a template edit would silently invalidate. An unrecognised
// (tenant-custom) role holds nothing, which is the same fail-closed answer the
// route check gives it.
func CapabilitiesForRole(role string) []string {
	if IsSuperAdmin(role) {
		// Administrator's template is the authority for what "everything"
		// currently means, so this stays correct as capabilities are added.
		if template, ok := RoleTemplateFor(RoleSuperAdmin); ok {
			out := append([]string(nil), template.Capabilities...)
			sort.Strings(out)
			return out
		}
	}
	template, ok := RoleTemplateFor(role)
	if !ok {
		return []string{}
	}
	out := append([]string(nil), template.Capabilities...)
	sort.Strings(out)
	return out
}

// RoleHasCapability is CapabilitiesForRole reduced to the one question a
// handler usually has.
//
// Stage 47.4.5 needs it because the route-capability registry gates whole
// ROUTES, and the two return exceptions are two ACTIONS on a route that must
// stay open to ordinary returns - splitting them onto their own route would
// duplicate the handler and give the two paths two places to drift apart.
// Checking here keeps one code path and still reads the same templates
// checkRouteCapability enforces with.
func RoleHasCapability(role, capability string) bool {
	for _, held := range CapabilitiesForRole(role) {
		if held == capability {
			return true
		}
	}
	return false
}

// TemplateGrants expands a template into doctype -> level for one tenant,
// resolving module grants against that tenant's own doctype_meta (a tenant
// can have doctypes this checkout's migrations never seeded - the Doctype
// Builder creates them at runtime).
func TemplateGrants(tenantID string, template RoleTemplate) (map[string]AccessLevel, error) {
	modules, err := doctypeModules(tenantID)
	if err != nil {
		return nil, err
	}
	grants := map[string]AccessLevel{}
	for doctype, module := range modules {
		if level, ok := template.Modules[module]; ok && level != AccessNone {
			grants[doctype] = level
		}
	}
	for doctype, level := range template.Doctypes {
		// An override to AccessNone removes the module's grant - that is
		// how "Auditor reads everything except payroll" is expressed.
		if _, exists := modules[doctype]; !exists {
			continue
		}
		if level == AccessNone {
			delete(grants, doctype)
			continue
		}
		grants[doctype] = level
	}
	return grants, nil
}

// doctypeModules reads doctype -> module for a tenant.
func doctypeModules(tenantID string) (map[string]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT name, module FROM %s.doctype_meta`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, module string
		if err := rows.Scan(&name, &module); err != nil {
			return nil, err
		}
		out[name] = module
	}
	return out, rows.Err()
}
