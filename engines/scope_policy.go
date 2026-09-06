package engines

import (
	"fmt"
	"sort"
)

// Stage 47.1.4 - scope semantics, enforced separately from role capability
// (audit finding A-09).
//
// Before this file, handleGenericDoc had exactly one scope dimension
// (location), expressed inline as:
//
//	AND (COALESCE(data->>'location', data->>'location_code') = $n
//	     OR COALESCE(data->>'location', data->>'location_code') IS NULL)
//
// for every non-Super-Admin, on every doctype. That single clause conflated
// three genuinely different situations, which is what this item exists to
// separate:
//
//  1. The doctype has no location concept at all (MarketplaceSettlement,
//     LogisticsBooking). NULL is correct and the rows must stay visible -
//     the original comment says so, and it is right.
//  2. The doctype declares location as OPTIONAL (Printer, Zone,
//     ShiftAssignment, TaskCompletionLog...). Location is an attribute, not
//     the row's scope; a row without one is "not applicable", so it stays
//     visible, but a row WITH one is still filtered.
//  3. The doctype declares location as MANDATORY (POSSession, GRN,
//     PurchaseOrder, Wave, WarehouseTask...). Here a missing location is an
//     anomaly, not a global record - and the old clause handed it to every
//     role at every location. That is the "a NULL location is not global
//     visibility" case, and it is now denied unless the role holds an
//     explicit global-scope capability.
//
// The same separation is applied to two dimensions the old clause had no
// notion of at all:
//
//   - self (employee): an employee's own HR records. db/migration.sql grants
//     ('Cashier', 'Payslip', TRUE, ...) and ('Cashier', 'EmployeeLoan',
//     TRUE, ...) doctype-level read, so before this a Cashier could list
//     every colleague's payslip. Field-level redaction (47.1.3) hides the
//     amounts; this hides the rows.
//   - owner (3PL inventory owner): owner-scoped doctypes now need an
//     explicit global-owner capability. Per-session owner scope itself is
//     47.5's work (sessions carry no owner today); this establishes the
//     dimension, the capability and the enforcement path so 47.5 replaces a
//     default rather than inventing a mechanism.
//
// Deliberately NOT a dimension yet: legal entity. The evidence is that the
// data model has no legal-entity scope to enforce - Location.legal_entity is
// optional and populated on 1 of 112 rows in the development tenant,
// JournalVoucher.entity is optional and populated on 0 of 199, and
// StatusTransitionRule.entity - the only mandatory "entity" field in the
// whole schema - does not mean a legal entity at all (its own Select options
// are 'Order,OrderLine,FulfillmentOrder,Shipment'; the stored values are
// doctype names like "ExpenseClaim"). Scoping on it would be wrong, not
// merely strict. Entity becomes a real dimension when 47.12.2/47.12.3 build
// the legal-entity -> registration -> branch/location graph; ScopeDimension
// already names it so that work extends this file instead of forking it.

type ScopeDimension string

const (
	ScopeLocation ScopeDimension = "location"
	ScopeSelf     ScopeDimension = "self"
	ScopeOwner    ScopeDimension = "owner"
	ScopeEntity   ScopeDimension = "entity"
)

// Global-scope capabilities. A role holding one sees every row of that
// dimension; a role without it is confined to its own session's value.
// Named in the same "area.thing" vocabulary as route_capabilities.go's
// capabilities so an administrator reads one scheme.
const (
	CapabilityGlobalLocation = "scope.location.global"
	CapabilityGlobalSelf     = "scope.self.global"
	CapabilityGlobalOwner    = "scope.owner.global"
	CapabilityGlobalEntity   = "scope.entity.global"
)

// DoctypeScope declares which dimensions a doctype is scoped by and the
// JSONB field each lives in.
type DoctypeScope struct {
	// LocationFields are checked with COALESCE in order - this codebase
	// uses two names ("location", and FulfillmentTask-style
	// "location_code") and a doctype declares exactly one of them, but
	// COALESCE keeps a single code path.
	LocationFields []string
	// LocationMandatory is true when the doctype_fields seed marks the
	// location field mandatory. Only then is a missing location an
	// anomaly to deny rather than an inapplicable attribute to allow.
	LocationMandatory bool
	// SelfField holds the Employee code a row belongs to.
	SelfField string
	// OwnerField holds the inventory owner a row belongs to (47.5).
	OwnerField string
}

// Scoped reports whether any dimension applies.
func (s DoctypeScope) Scoped() bool {
	return len(s.LocationFields) > 0 || s.SelfField != "" || s.OwnerField != ""
}

func locScope(field string, mandatory bool) DoctypeScope {
	return DoctypeScope{LocationFields: []string{field}, LocationMandatory: mandatory}
}

// doctypeScopes is derived mechanically from the doctype_fields seeds in
// db/*.sql: every doctype declaring a location/location_code, employee_id or
// owner_id field, with LocationMandatory taken from that seed row's own
// mandatory column. scope_policy_test.go re-parses those seeds and fails the
// build if the two ever disagree - the same self-checking shape
// route_capabilities_test.go uses against routes.go, and the reason this map
// can be trusted without re-reading 45 migrations by hand.
//
// A doctype absent from this map is NOT unscoped: ScopeForDoctype falls back
// to the pre-47.1.4 permissive location clause for it, so a doctype created
// at runtime through the Doctype Builder keeps exactly today's behavior
// instead of silently losing its location filter.
var doctypeScopes = map[string]DoctypeScope{
	// --- location mandatory: a missing value is an anomaly, deny --------
	"ASN":                    locScope("location", true),
	"Asset":                  locScope("location", true),
	"Bin":                    locScope("location", true),
	"BundleAssembly":         locScope("location_code", true),
	"CycleCountLine":         locScope("location", true),
	"FulfillmentTask":        locScope("location_code", true),
	"GRN":                    locScope("location", true),
	"GatePass":               locScope("location_code", true),
	"Hold":                   locScope("location_code", true),
	"Manifest":               locScope("location_code", true),
	"POSCart":                locScope("location", true),
	"POSOfflineQueueGap":     locScope("location", true),
	"POSOfflineSyncVariance": locScope("location", true),
	"POSPriceOverride":       locScope("location", true),
	"POSProfile":             locScope("location", true),
	"POSSession":             locScope("location", true),
	"PhysicalInventory":      locScope("location", true),
	"ProductionOrder":        locScope("location", true),
	"PurchaseOrder":          locScope("location", true),
	"ReorderPointConfig":     locScope("location_code", true),
	"SalesInvoice":           locScope("location", true),
	"SubcontractOrder":       locScope("location", true),
	"WarehouseTask":          locScope("location_code", true),
	"Wave":                   locScope("location_code", true),

	// --- location optional: an attribute, not the row's scope -----------
	"Attendance":     {LocationFields: []string{"location"}, SelfField: "employee_id"},
	"CapturedCharge": {LocationFields: []string{"location_code"}, OwnerField: "owner_id"},
	"Channel":        locScope("location_code", false),
	// ChargeCode and Bin also declare owner_id, but as OPTIONAL ("Owner
	// (3PL Client, optional)") - same rule as location: an optional owner
	// is an attribute, not the row's scope, so neither is owner-scoped.
	"ChargeCode": locScope("location_code", false),
	"DockDoor":   locScope("location", false),
	"Employee":   locScope("location", false),
	// Stage 47.6.2: an operator with no location on their session (a relief
	// worker, a contractor) must still be able to ask for a supervisor, so an
	// absent location cannot be what hides the request. Optional = an
	// attribute, not the row's scope - the same rule every entry in this block
	// follows.
	"FloorAssistRequest":    locScope("location", false),
	"PackStation":           locScope("location_code", false),
	"PreShipValidationRule": locScope("location_code", false),
	"Printer":               locScope("location", false),
	"PutawayStrategy":       locScope("location_code", false),
	"SalesOrderLine":        locScope("location_code", false),
	"SerialNumber":          locScope("location_code", false),
	"ShippingPackage":       locScope("location_code", false),
	"SortStation":           locScope("location_code", false),
	"TaskCompletionLog":     locScope("location_code", false),
	"TaskDispatchStrategy":  locScope("location_code", false),
	"WaveTemplate":          locScope("location_code", false),
	"Zone":                  locScope("location", false),

	// --- location mandatory AND owner-scoped (3PL storage billing) ------
	"StorageBalanceSnapshot": {LocationFields: []string{"location_code"}, LocationMandatory: true, OwnerField: "owner_id"},
	"StorageBillingRate":     {LocationFields: []string{"location_code"}, LocationMandatory: true, OwnerField: "owner_id"},

	// --- owner only -----------------------------------------------------
	"ChargeContract": {OwnerField: "owner_id"},

	// --- self (employee) ------------------------------------------------
	// ExpenseClaim and ShiftAssignment also declare a location; both are
	// listed with it so one map holds every dimension a doctype has.
	"Appraisal":           {SelfField: "employee_id"},
	"EmployeeLoan":        {SelfField: "employee_id"},
	"ExpenseClaim":        {LocationFields: []string{"location"}, LocationMandatory: true, SelfField: "employee_id"},
	"Grievance":           {SelfField: "employee_id"},
	"Leave":               {SelfField: "employee_id"},
	"OnboardingChecklist": {SelfField: "employee_id"},
	"Payslip":             {SelfField: "employee_id"},
	"SalaryStructure":     {SelfField: "employee_id"},
	"ShiftAssignment":     {LocationFields: []string{"location"}, SelfField: "employee_id"},
	"TrainingRecord":      {SelfField: "employee_id"},
}

// personalViewDoctypes declare a mandatory "owner" field holding a USERNAME,
// not an employee code: DashboardLayout, OMSSavedView, ReportColumnProfile
// and ReportFilterPreset. They are deliberately NOT self-scoped here, and
// the omission is a decision rather than an oversight - each already has
// purpose-built scoping in its own engine (ListDashboardLayouts,
// DeleteDashboardLayout, the OMS console's saved views) implementing a
// private/shared distinction that a blanket "only your own rows" filter
// would silently break. Recorded so a later pass does not "fix" the gap.
var personalViewDoctypes = map[string]bool{
	"DashboardLayout": true, "OMSSavedView": true,
	"ReportColumnProfile": true, "ReportFilterPreset": true,
}

// globalScopeGrants is capability -> roles holding it, beyond Super Admin
// (which holds every global scope by definition - IsSuperAdmin).
//
// Every entry is evidence-based, taken from the shipped role_permissions
// seed: a role that already holds create/update on a doctype is acting on
// other people's records as its job, and confining it to its own rows would
// remove a working capability rather than close a leak. A role holding only
// READ on a personal HR doctype is the A-09 case and gets no exemption.
var globalScopeGrants = map[string][]string{
	// 3PL owner: no session carries an owner yet (47.5). Granting the two
	// roles with a live 3PL billing UI path keeps those screens working
	// while the dimension exists and is enforced for everyone else; 47.5
	// replaces this grant with real per-session owner scope.
	CapabilityGlobalOwner: {RoleStoreManager},
}

// selfScopeExemptions is role -> doctypes where the role legitimately works
// on other employees' records. Derived from the shipped seed: each doctype
// listed is one where that role holds allow_create or allow_update.
//
// What is NOT listed is the point of the item. Store Manager holds
// read-only on Payslip and SalaryStructure, and Cashier read-only on Payslip
// and EmployeeLoan and ShiftAssignment - so after this, none of them can
// list another employee's payslip, salary structure, salary advance or
// shift roster through GET /api/v1/doc/<doctype>.
var selfScopeExemptions = map[string]map[string]bool{
	RoleStoreManager: {
		"Appraisal": true, "Attendance": true, "EmployeeLoan": true,
		"ExpenseClaim": true, "Grievance": true, "Leave": true,
		"OnboardingChecklist": true, "ShiftAssignment": true,
		"TrainingRecord": true,
	},
	// Cashier deliberately has NO exemption, even though the shipped seed
	// grants it create/update on Leave, ExpenseClaim and Grievance. Those
	// grants exist for self-service submission, not for reading colleagues'
	// requests, and public/app.js proves it: renderMyRequestsTab fetches
	// the whole of /api/v1/doc/Leave, /ExpenseClaim and /Grievance and then
	// filters them CLIENT-SIDE on `l.employee_id === employee.code` - so
	// every one of a colleague's leave reasons, expense amounts and
	// grievance descriptions was already on the wire, one devtools tab
	// away, before this. Server-side self scope closes that and leaves the
	// page rendering exactly the same rows.
}

// ScopeForDoctype returns the declared scope, and whether the doctype is
// known to the registry at all. An unknown doctype (one built at runtime
// through the Doctype Builder) is reported as unknown so the caller can keep
// the pre-47.1.4 permissive behavior instead of dropping its location filter.
func ScopeForDoctype(doctype string) (DoctypeScope, bool) {
	scope, ok := doctypeScopes[doctype]
	return scope, ok
}

// ScopedDoctypes lists every doctype the registry covers, sorted.
func ScopedDoctypes() []string {
	out := make([]string, 0, len(doctypeScopes))
	for doctype := range doctypeScopes {
		out = append(out, doctype)
	}
	sort.Strings(out)
	return out
}

// SessionScope is the scope a request's session actually has, resolved once
// per request from the token and the tenant's own records.
type SessionScope struct {
	Role         string
	UserID       string
	LocationCode string
	// EmployeeCode is the Employee this user is linked to, "" if none.
	// Resolved lazily by ResolveSessionScope, and "" fails CLOSED: a user
	// with no Employee record sees none of the self-scoped rows rather
	// than all of them.
	EmployeeCode string
}

// HasGlobalScope reports whether the role sees every row of a dimension.
func (s SessionScope) HasGlobalScope(dimension ScopeDimension, doctype string) bool {
	if IsSuperAdmin(s.Role) {
		return true
	}
	switch dimension {
	case ScopeSelf:
		return selfScopeExemptions[CanonicalRole(s.Role)][doctype]
	case ScopeLocation:
		return roleHasGlobalGrant(CapabilityGlobalLocation, s.Role)
	case ScopeOwner:
		return roleHasGlobalGrant(CapabilityGlobalOwner, s.Role)
	case ScopeEntity:
		return roleHasGlobalGrant(CapabilityGlobalEntity, s.Role)
	}
	return false
}

func roleHasGlobalGrant(capability, role string) bool {
	for _, r := range globalScopeGrants[capability] {
		if normalizeRoleKey(r) == normalizeRoleKey(role) {
			return true
		}
	}
	return false
}

// ScopeConstraint is one required match on a document. Fields are COALESCEd
// in order (an empty string value never matches, which is the fail-closed
// case for a session whose scope value could not be resolved).
type ScopeConstraint struct {
	Dimension ScopeDimension
	Fields    []string
	Value     string
	// AllowMissing keeps a row whose scope field is absent/empty. True only
	// for a dimension the doctype declares as optional, where a missing
	// value means "not applicable" rather than "belongs to everyone".
	AllowMissing bool
}

// ScopeConstraints returns everything a session must match to see a row of
// this doctype. An empty result means the doctype is unscoped for this
// session (Super Admin, or a doctype with no dimensions).
//
// The second return value is false when the doctype is not in the registry;
// the caller keeps its legacy behavior for that case.
func ScopeConstraints(doctype string, session SessionScope) ([]ScopeConstraint, bool) {
	scope, known := ScopeForDoctype(doctype)
	if !known {
		return nil, false
	}
	var out []ScopeConstraint
	if len(scope.LocationFields) > 0 && !session.HasGlobalScope(ScopeLocation, doctype) {
		out = append(out, ScopeConstraint{
			Dimension: ScopeLocation, Fields: scope.LocationFields,
			Value: session.LocationCode, AllowMissing: !scope.LocationMandatory,
		})
	}
	if scope.SelfField != "" && !session.HasGlobalScope(ScopeSelf, doctype) {
		out = append(out, ScopeConstraint{
			Dimension: ScopeSelf, Fields: []string{scope.SelfField},
			Value: session.EmployeeCode,
		})
	}
	if scope.OwnerField != "" && !session.HasGlobalScope(ScopeOwner, doctype) {
		// No session carries an owner yet (47.5), so Value is "" and this
		// denies every owner-scoped row to a role without the global-owner
		// capability - deny-by-default, which is the intended direction.
		out = append(out, ScopeConstraint{
			Dimension: ScopeOwner, Fields: []string{scope.OwnerField},
		})
	}
	return out, true
}

// DocumentMatchesScope applies the constraints to one already-loaded
// document - the object-level half, for a single-document GET. Returns the
// dimension that failed, or "" when the document is in scope.
func DocumentMatchesScope(constraints []ScopeConstraint, data map[string]interface{}) ScopeDimension {
	for _, c := range constraints {
		value := ""
		for _, field := range c.Fields {
			if raw, ok := data[field]; ok {
				if s := fmt.Sprintf("%v", raw); s != "" && s != "<nil>" {
					value = s
					break
				}
			}
		}
		if value == "" {
			if c.AllowMissing {
				continue
			}
			return c.Dimension
		}
		if value != c.Value {
			return c.Dimension
		}
	}
	return ""
}

// ResolveSessionScope fills in the parts of a session's scope that need a
// query. Only the self dimension does, and only for a doctype that declares
// it, so an ordinary request pays nothing.
func ResolveSessionScope(tenantID, doctype string, session SessionScope) (SessionScope, error) {
	scope, known := ScopeForDoctype(doctype)
	if !known || scope.SelfField == "" || session.HasGlobalScope(ScopeSelf, doctype) {
		return session, nil
	}
	code, err := EmployeeCodeForUser(tenantID, session.UserID)
	if err != nil {
		return session, err
	}
	session.EmployeeCode = code
	return session, nil
}

// EmployeeCodeForUser returns the Employee code linked to an ERP user, or ""
// when the user has no Employee record. The link is the Employee doctype's
// own user_id field ("Linked ERP User ID" in db/migration.sql) - there is no
// employee column on the users table.
//
// "" is not an error: plenty of accounts (an integration user, a store's
// shared till login) are legitimately not employees. It fails closed at the
// caller - such a session matches no self-scoped row at all.
func EmployeeCodeForUser(tenantID, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	// Reuses GetMyEmployeeRecord rather than issuing a second, subtly
	// different lookup: that function is what GET /api/v1/hr/my-employee
	// already answers with, and public/app.js's My Requests page keys its
	// rows on exactly `employee.code || employee.id`. Matching it here is
	// what makes the server-side self filter agree with the page's own
	// (now redundant) client-side filter instead of quietly disagreeing.
	employee, err := GetMyEmployeeRecord(tenantID, userID)
	if err != nil || employee == nil {
		return "", err
	}
	if code, ok := employee["code"].(string); ok && code != "" {
		return code, nil
	}
	if id, ok := employee["id"].(string); ok {
		return id, nil
	}
	return "", nil
}
