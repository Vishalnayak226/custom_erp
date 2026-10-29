package engines

import (
	"sort"
	"strings"
)

// Stage 49.3.6 / 49.3.7 - what makes a grant dangerous, and the one diff both
// a hand-made role change and a baseline (role template) change are judged by.
//
// A grant is dangerous when holding it lets one person move money, see or
// change what colleagues earn, reset someone else's access, or hold a
// connector's secret - the losses the SoD catalog (sod_catalog.go) names in
// pairs, here named one grant at a time so a single change can be flagged
// before it is saved rather than after a second grant completes the pair.
//
// The list is deliberately short and per record type. A role that does the
// work (HR Manager and payroll, Accountant and journals) still holds these;
// the point is that gaining one is never silent: it is shown in the preview,
// recorded with the change and, for the floor roles, refused by a test.

// dangerousDoctype is one record type and the level from which holding it is
// dangerous.
type dangerousDoctype struct {
	Category string
	// Minimum is the level that is dangerous for everyone.
	Minimum AccessLevel
	// ReadWhenGlobalSelf marks a self-scoped personal record (scope_policy.go):
	// read is harmless while the role is confined to its own rows, and
	// dangerous once it sees everyone's.
	ReadWhenGlobalSelf bool
}

var dangerousDoctypes = map[string]dangerousDoctype{
	// What people earn and owe.
	"Payslip":         {Category: "payroll", Minimum: AccessOperate, ReadWhenGlobalSelf: true},
	"SalaryStructure": {Category: "payroll", Minimum: AccessOperate, ReadWhenGlobalSelf: true},
	"EmployeeLoan":    {Category: "payroll", Minimum: AccessOperate, ReadWhenGlobalSelf: true},
	// Posting to the ledger and releasing payment.
	"JournalVoucher":  {Category: "finance posting", Minimum: AccessOperate},
	"GLPost":          {Category: "finance posting", Minimum: AccessOperate},
	"PaymentProposal": {Category: "finance posting", Minimum: AccessOperate},
	"VendorInvoice":   {Category: "finance posting", Minimum: AccessOperate},
	// Changing where money is paid to.
	"BankAccount": {Category: "finance posting", Minimum: AccessOperate},
	// Taking over another person's account.
	"PasswordResetRequest": {Category: "user administration", Minimum: AccessOperate},
	// A connector's signing secret (sod_catalog's connector-secret conflict).
	"WebhookSubscription": {Category: "connector secrets", Minimum: AccessOperate},
}

// dangerousCapabilities are route capabilities (route_capabilities.go) whose
// holder reaches the same losses through a dedicated screen rather than a
// record type. "admin." covers every administrator screen - users, roles,
// settings, the Setup Workbook.
var dangerousCapabilities = []struct{ Prefix, Category string }{
	{"hr.payroll", "payroll"},
	{"system.logs", "platform"},
	{"audit.logs", "audit evidence"},
	{"admin.", "administration"},
}

// DangerousGrant is one grant a role holds that is on the dangerous list.
type DangerousGrant struct {
	Role       string `json:"role"`
	Doctype    string `json:"doctype,omitempty"`
	Capability string `json:"capability,omitempty"`
	Category   string `json:"category"`
	Level      string `json:"level,omitempty"`
	// Inherited is true when a role template reaches the record type only
	// through a whole-module grant - nobody wrote it down for this role.
	Inherited bool `json:"inherited,omitempty"`
}

// IsDangerousGrant reports whether holding level on doctype is dangerous for
// role, and the category when it is. A full-access role holds everything by
// definition and is never reported grant by grant.
func IsDangerousGrant(role, doctype string, level AccessLevel) (string, bool) {
	if HasFullAccess(role) || level == AccessNone {
		return "", false
	}
	d, ok := dangerousDoctypes[doctype]
	if !ok {
		return "", false
	}
	if level >= d.Minimum {
		return d.Category, true
	}
	if d.ReadWhenGlobalSelf && level >= AccessRead &&
		(SessionScope{Role: role}).HasGlobalScope(ScopeSelf, doctype) {
		return d.Category, true
	}
	return "", false
}

// DangerousCapabilityCategory reports whether a route capability is dangerous.
func DangerousCapabilityCategory(capability string) (string, bool) {
	for _, c := range dangerousCapabilities {
		if capability == c.Prefix || (strings.HasSuffix(c.Prefix, ".") && strings.HasPrefix(capability, c.Prefix)) {
			return c.Category, true
		}
	}
	return "", false
}

// DangerousGrantsFor lists the dangerous grants among a role's doctype grants
// and capabilities, sorted by record type then capability.
func DangerousGrantsFor(role string, grants map[string]AccessLevel, capabilities []string) []DangerousGrant {
	out := []DangerousGrant{}
	for _, doctype := range sortedKeys(grants) {
		if category, ok := IsDangerousGrant(role, doctype, grants[doctype]); ok {
			out = append(out, DangerousGrant{Role: role, Doctype: doctype, Category: category, Level: grants[doctype].String()})
		}
	}
	if HasFullAccess(role) {
		return out
	}
	caps := append([]string(nil), capabilities...)
	sort.Strings(caps)
	for _, capability := range caps {
		if category, ok := DangerousCapabilityCategory(capability); ok {
			out = append(out, DangerousGrant{Role: role, Capability: capability, Category: category})
		}
	}
	return out
}

// DangerousGained is the dangerous grants a role holds after a change that it
// did not hold, at that level or above, before it - what an owner has to see
// before signing a change off.
func DangerousGained(role string, before, after map[string]AccessLevel) []DangerousGrant {
	out := []DangerousGrant{}
	for _, g := range DangerousGrantsFor(role, after, nil) {
		if _, held := IsDangerousGrant(role, g.Doctype, before[g.Doctype]); held && before[g.Doctype] >= after[g.Doctype] {
			continue
		}
		out = append(out, g)
	}
	return out
}

// TemplateDangerousGrants lists a role template's dangerous grants as it would
// apply in this tenant, marking those it reaches only through a module grant.
func TemplateDangerousGrants(tenantID string, template RoleTemplate) ([]DangerousGrant, error) {
	grants, err := TemplateGrants(tenantID, template)
	if err != nil {
		return nil, err
	}
	out := DangerousGrantsFor(template.Name, grants, template.Capabilities)
	for i := range out {
		if out[i].Doctype == "" {
			continue
		}
		if _, explicit := template.Doctypes[out[i].Doctype]; !explicit {
			out[i].Inherited = true
		}
	}
	return out, nil
}

// GrantLevelDiff is a before/after split by direction - "3 removed, 1 widened"
// is reviewable where "12 changes" is not.
type GrantLevelDiff struct {
	Added     []GrantChange `json:"added"`
	Tightened []GrantChange `json:"tightened"`
	Loosened  []GrantChange `json:"loosened"`
	Removed   []GrantChange `json:"removed"`
}

// DiffGrantLevels compares two doctype -> level maps. A missing key and
// AccessNone are the same thing: no access.
func DiffGrantLevels(before, after map[string]AccessLevel) GrantLevelDiff {
	var diff GrantLevelDiff
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, doctype := range names {
		b, a := before[doctype], after[doctype]
		change := GrantChange{Doctype: doctype, From: b.String(), To: a.String()}
		switch {
		case a == b:
		case b == AccessNone:
			diff.Added = append(diff.Added, change)
		case a == AccessNone:
			diff.Removed = append(diff.Removed, change)
		case a < b:
			diff.Tightened = append(diff.Tightened, change)
		default:
			diff.Loosened = append(diff.Loosened, change)
		}
	}
	return diff
}
