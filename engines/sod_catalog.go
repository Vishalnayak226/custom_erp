package engines

import (
	"fmt"
	"sort"

	"custom_erp/db"
)

// Stage 47.1.6 - segregation-of-duties conflict catalog and administrator
// preview (audit finding A-09).
//
// A role can be individually reasonable on every line and still be wrong as
// a whole: the danger is not "may this role read Vendor" but "may the same
// person create the vendor AND release the payment to it". This file names
// the six conflicts the item lists, detects them against a role's REAL
// effective access (its doctype grants, its route capabilities, and the
// approval_rules rows that make it an approver), and feeds the
// "why allowed / why denied" preview an administrator sees before granting.
//
// Detection is deliberately evidence-based rather than declarative: a
// conflict is reported because the role actually holds both duties in this
// tenant's data, not because a template says it might.

// Duty is one half of a conflict: the things that, if a role holds any of
// them, mean the role performs this duty.
type Duty struct {
	Name string
	// Doctypes is doctype -> the minimum level that constitutes the duty.
	Doctypes map[string]AccessLevel
	// Capabilities are route capabilities that constitute the duty.
	Capabilities []string
	// ApproverFor means "approves any of these doctypes", read from the
	// tenant's approval_rules.required_role.
	ApproverFor []string
}

// SoDConflict is one catalogued pair of duties that must not meet.
type SoDConflict struct {
	ID       string
	Label    string
	Severity string // "High" or "Medium"
	// Why states the loss that occurs when one person holds both. A
	// conflict with no stated loss is a rule nobody can argue with or
	// waive, which is how SoD catalogs become theatre.
	Why  string
	A, B Duty
	// Note records a known limit of the detector for this conflict.
	Note string
}

var sodConflicts = []SoDConflict{
	{
		ID: "create-and-approve", Label: "Creates and approves the same document", Severity: "High",
		Why: "The maker-checker control is the only thing standing between a fabricated purchase order, journal or claim and a real payment. One person holding both ends removes it entirely.",
		A: Duty{Name: "Creates transactions", Doctypes: map[string]AccessLevel{
			"PurchaseOrder": AccessOperate, "JournalVoucher": AccessOperate,
			"VendorInvoice": AccessOperate, "PaymentProposal": AccessOperate,
			"ExpenseClaim": AccessOperate, "SalesReturn": AccessOperate,
			"PhysicalInventory": AccessOperate,
		}},
		B: Duty{Name: "Approves those transactions", ApproverFor: []string{
			"PurchaseOrder", "JournalVoucher", "VendorInvoice", "PaymentProposal",
			"ExpenseClaim", "SalesReturn", "PhysicalInventory",
		}},
	},
	{
		ID: "price-override-and-cash-refund", Label: "Approves price overrides and issues refunds", Severity: "High",
		Why: "Discount the sale, then refund it to a different tender, and the difference walks out of the till with no second signature anywhere in the trail.",
		A:   Duty{Name: "Approves POS price overrides", ApproverFor: []string{"POSCart", "POSInvoice"}},
		B: Duty{Name: "Issues refunds", Doctypes: map[string]AccessLevel{
			"SalesReturn": AccessOperate, "CreditNote": AccessOperate,
		}},
	},
	{
		ID: "vendor-create-and-payment", Label: "Creates vendors and pays them", Severity: "High",
		Why: "Payment-diversion fraud in one step: add a supplier with your own bank details, raise the invoice, release the payment. This is why the Accounts Payable template holds Vendor at read only.",
		A:   Duty{Name: "Creates or edits vendors", Doctypes: map[string]AccessLevel{"Vendor": AccessOperate}},
		B: Duty{Name: "Releases payment", Doctypes: map[string]AccessLevel{
			"PaymentProposal": AccessOperate, "VendorInvoice": AccessOperate,
		}},
	},
	{
		ID: "employee-master-and-payroll", Label: "Maintains employees and runs payroll", Severity: "High",
		Why:  "A ghost employee is created, paid, and removed by the same person, and nothing outside the payroll run ever sees it.",
		A:    Duty{Name: "Maintains the employee master", Doctypes: map[string]AccessLevel{"Employee": AccessOperate}},
		B:    Duty{Name: "Runs payroll", Capabilities: []string{"hr.payroll"}},
		Note: "The HR Manager template holds both by design - a single-HR-person business has no second party. This is the conflict most likely to need a recorded waiver rather than a fix, which is exactly why it is reported rather than blocked.",
	},
	{
		ID: "stock-adjust-and-count-approval", Label: "Adjusts stock and approves the count that hides it", Severity: "High",
		Why: "Shrinkage becomes invisible: take the stock, book the adjustment, approve your own count variance.",
		A: Duty{Name: "Adjusts stock", Doctypes: map[string]AccessLevel{
			"PhysicalInventory": AccessOperate, "CycleCountLine": AccessOperate,
		}},
		B: Duty{Name: "Approves count variances", ApproverFor: []string{"PhysicalInventory", "CycleCountLine"}},
	},
	{
		ID: "connector-secret-and-event-replay", Label: "Holds a connector secret and can replay events", Severity: "Medium",
		Why:  "The webhook signing secret plus the ability to re-drive a delivery means an outbound event can be forged and made to look like the system's own retry.",
		A:    Duty{Name: "Holds connector secrets", Doctypes: map[string]AccessLevel{"WebhookSubscription": AccessOperate}},
		B:    Duty{Name: "Replays delivered events", Capabilities: []string{"jobs.default"}},
		Note: "jobs.default (POST /api/v1/jobs/{id}/retry -> ReplayJob) is not yet one of route_capabilities.go's restricted capabilities, so EVERY authenticated role satisfies duty B today. Until job replay is capability-gated, this conflict reduces in practice to 'holds WebhookSubscription create/update'. Recorded rather than quietly narrowed, because the detector's honesty is the point.",
	},
}

// SoDConflicts returns the catalog.
func SoDConflicts() []SoDConflict {
	out := make([]SoDConflict, len(sodConflicts))
	copy(out, sodConflicts)
	return out
}

// EffectiveAccess is what a role can actually do in one tenant: the three
// independent inputs a conflict is evaluated against.
type EffectiveAccess struct {
	Role         string
	Grants       map[string]AccessLevel
	Capabilities []string
	// ApproverFor is doctype -> true, from approval_rules.required_role.
	ApproverFor map[string]bool
}

func (e EffectiveAccess) holds(duty Duty) (bool, string) {
	for doctype, minimum := range duty.Doctypes {
		if e.Grants[doctype] >= minimum && minimum != AccessNone {
			return true, fmt.Sprintf("%s: %s", doctype, e.Grants[doctype])
		}
	}
	for _, capability := range duty.Capabilities {
		for _, held := range e.Capabilities {
			if held == capability {
				return true, "capability: " + capability
			}
		}
	}
	for _, doctype := range duty.ApproverFor {
		if e.ApproverFor[doctype] {
			return true, "approves: " + doctype
		}
	}
	return false, ""
}

// SoDFinding is one detected conflict, with the evidence for both halves so
// an administrator can see WHY rather than only THAT.
type SoDFinding struct {
	ConflictID string `json:"conflict_id"`
	Label      string `json:"label"`
	Severity   string `json:"severity"`
	Why        string `json:"why"`
	EvidenceA  string `json:"evidence_a"`
	EvidenceB  string `json:"evidence_b"`
	Note       string `json:"note,omitempty"`
}

// DetectSoDConflicts returns every catalogued conflict the access satisfies
// both halves of, worst first.
func DetectSoDConflicts(access EffectiveAccess) []SoDFinding {
	var out []SoDFinding
	for _, conflict := range sodConflicts {
		heldA, evidenceA := access.holds(conflict.A)
		if !heldA {
			continue
		}
		heldB, evidenceB := access.holds(conflict.B)
		if !heldB {
			continue
		}
		out = append(out, SoDFinding{
			ConflictID: conflict.ID, Label: conflict.Label, Severity: conflict.Severity,
			Why: conflict.Why, EvidenceA: evidenceA, EvidenceB: evidenceB, Note: conflict.Note,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Severity == "High" && out[j].Severity != "High"
	})
	return out
}

// ApproverDoctypesForRole reads which doctypes a role is an approver for.
// Super Admin approves everything (DecideApproval treats it as the catch-all
// approver regardless of approval_rules), so it is reported as such rather
// than as an empty set that would hide its conflicts.
func ApproverDoctypesForRole(tenantID, role string) (map[string]bool, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	if IsSuperAdmin(role) {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT doctype FROM %s.approval_rules`, schema))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var doctype string
			if err := rows.Scan(&doctype); err != nil {
				return nil, err
			}
			out[doctype] = true
		}
		return out, rows.Err()
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT DISTINCT doctype FROM %s.approval_rules WHERE required_role = $1`, schema), role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var doctype string
		if err := rows.Scan(&doctype); err != nil {
			return nil, err
		}
		out[doctype] = true
	}
	return out, rows.Err()
}

// StoredRoleGrants reads a role's ACTUAL doctype grants from the tenant's
// role_permissions table - what the role can do right now, as opposed to
// what its template says it should.
func StoredRoleGrants(tenantID, role string) (map[string]AccessLevel, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT doctype_name, allow_read, allow_create, allow_update, allow_delete
		 FROM %s.role_permissions WHERE role = $1`, schema), role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AccessLevel{}
	for rows.Next() {
		var doctype string
		var read, create, update, del bool
		if err := rows.Scan(&doctype, &read, &create, &update, &del); err != nil {
			return nil, err
		}
		out[doctype] = levelFor(read, create, update, del)
	}
	return out, rows.Err()
}

func levelFor(read, create, update, del bool) AccessLevel {
	switch {
	case del:
		return AccessManage
	case create || update:
		return AccessOperate
	case read:
		return AccessRead
	}
	return AccessNone
}

// AccessExplanation is the administrator-facing "why allowed / why denied"
// preview for one role, optionally focused on one doctype.
type AccessExplanation struct {
	Role          string                 `json:"role"`
	Template      string                 `json:"template,omitempty"`
	Summary       string                 `json:"summary,omitempty"`
	Capabilities  []string               `json:"capabilities"`
	GlobalScopes  []string               `json:"global_scopes"`
	Doctype       string                 `json:"doctype,omitempty"`
	StoredLevel   string                 `json:"stored_level,omitempty"`
	TemplateLevel string                 `json:"template_level,omitempty"`
	ScopeReasons  []string               `json:"scope_reasons,omitempty"`
	Fields        []SensitiveFieldAccess `json:"sensitive_fields,omitempty"`
	Conflicts     []SoDFinding           `json:"sod_conflicts"`
}

// ExplainAccess assembles the preview from every layer that decides an
// answer - template, stored grants, scope, sensitive fields and SoD - so an
// administrator sees one page instead of inferring the outcome from five.
func ExplainAccess(tenantID, role, doctype string) (AccessExplanation, error) {
	out := AccessExplanation{Role: role, Doctype: doctype, Capabilities: []string{}, GlobalScopes: []string{}}

	stored, err := StoredRoleGrants(tenantID, role)
	if err != nil {
		return out, err
	}
	approver, err := ApproverDoctypesForRole(tenantID, role)
	if err != nil {
		return out, err
	}

	template, hasTemplate := RoleTemplateFor(role)
	if hasTemplate {
		out.Template = template.Name
		out.Summary = template.Summary
		out.Capabilities = append(out.Capabilities, template.Capabilities...)
		for _, dimension := range template.GlobalScopes {
			out.GlobalScopes = append(out.GlobalScopes, string(dimension))
		}
		if doctype != "" {
			grants, err := TemplateGrants(tenantID, template)
			if err != nil {
				return out, err
			}
			out.TemplateLevel = grants[doctype].String()
		}
	}

	if doctype != "" {
		out.StoredLevel = stored[doctype].String()
		out.Fields = ExplainSensitiveFields(role, doctype)
		constraints, known := ScopeConstraints(doctype, SessionScope{Role: role})
		if !known {
			out.ScopeReasons = append(out.ScopeReasons,
				"not in the scope registry - the legacy location filter applies")
		}
		for _, c := range constraints {
			reason := fmt.Sprintf("confined to this session's %s", c.Dimension)
			if c.AllowMissing {
				reason += " (rows with no value are visible - the field is optional on this doctype)"
			} else {
				reason += " (rows with no value are denied - the field is mandatory on this doctype)"
			}
			out.ScopeReasons = append(out.ScopeReasons, reason)
		}
		if known && len(constraints) == 0 {
			out.ScopeReasons = append(out.ScopeReasons, "no scope constraints - this role sees every row")
		}
	}

	out.Conflicts = DetectSoDConflicts(EffectiveAccess{
		Role: role, Grants: stored, Capabilities: out.Capabilities, ApproverFor: approver,
	})
	if out.Conflicts == nil {
		out.Conflicts = []SoDFinding{}
	}
	return out, nil
}
