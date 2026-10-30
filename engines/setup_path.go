package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 55.3 - the setup path: the order a new client's masters have to be
// set up in, computed from what already exists.
//
// GetSetupStatus (setup_advisor.go) knows how many of each master there are;
// it does not know that a Location needs a Legal Entity first, or that Items
// need their Item Types. This orders the core prerequisites as dependencies,
// marks each Done, Next or Waiting-for, and is recomputed from live data on
// every call - so it "resumes" by construction and never deletes anything.

// SetupPathStep is one prerequisite in dependency order.
type SetupPathStep struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Doctypes  []string `json:"doctypes"`
	View      string   `json:"view,omitempty"` // a screen, when the step is not a record list
	DependsOn []string `json:"depends_on"`
	Count     int      `json:"count"`
	Done      bool     `json:"done"`
	Next      bool     `json:"next"`
	Waiting   []string `json:"waiting_for"`
	// Stage 91.3: what the step asks, in a line, and whether a client may
	// leave it (approval rules, print formats) - an optional step never
	// holds the setup open.
	Note     string `json:"note"`
	Optional bool   `json:"optional,omitempty"`
	// Stage 47.12.4: readiness is not row count (setup_readiness.go). Done
	// above stays the guided setup's progress; State is whether what was
	// entered passes its checks.
	State            string       `json:"state"`
	Severity         string       `json:"severity,omitempty"`
	Checks           []SetupCheck `json:"checks"`
	ValidatorVersion string       `json:"validator_version"`
}

// Stage 91.3 (decided 2026-10-21): the guided setup walks these in the
// listed order - company and legal entity, stores, tax, number series, users
// and roles, items, approval rules, print formats - each a plain form whose
// code is given on save.
var setupPathPlan = []SetupPathStep{
	{ID: "entity", Label: "Company and legal entity", Doctypes: []string{"LegalEntity"},
		Note: "Your company's registered name, GSTIN, address and bank details - printed on every invoice."},
	{ID: "locations", Label: "Stores and warehouses", Doctypes: []string{"Location"}, DependsOn: []string{"entity"},
		Note: "Each shop and warehouse: its name, type, state and short code. A cost centre is made for each."},
	{ID: "org", Label: "Departments and cost centres", Doctypes: []string{"Department", "CostCenter"}, DependsOn: []string{"entity"},
		Note: "How the business is split for people and costs."},
	{ID: "tax", Label: "Tax: GSTIN and HSN codes", Doctypes: []string{"HSNCode"}, DependsOn: []string{"entity"},
		Note: "Every legal entity's GSTIN, and the HSN codes your items use with their GST rate."},
	{ID: "numbering", Label: "Document numbering", View: "prefix-configs", DependsOn: []string{"locations"},
		Note: "How bills, purchase orders and receipts are numbered - prefix, store code, year."},
	{ID: "users", Label: "Users and their roles", View: "users", DependsOn: []string{"locations"},
		Note: "Everyone who signs in: their role and the stores they work at."},
	{ID: "units", Label: "Units", Doctypes: []string{"UOM"},
		Note: "Pieces, metres, kilograms... how items are counted."},
	{ID: "item-types", Label: "Item types and categories", Doctypes: []string{"ItemType", "Category"}, DependsOn: []string{"units"},
		Note: "Which kinds of goods you keep, and the categories you sell them under."},
	{ID: "items", Label: "Items", Doctypes: []string{"Item"}, DependsOn: []string{"item-types"},
		Note: "What you buy and sell - or add them on your first purchase order."},
	{ID: "parties", Label: "Vendors and customers", Doctypes: []string{"Vendor", "Customer"}, DependsOn: []string{"entity"},
		Note: "Who you buy from; customers are also added at the till."},
	{ID: "approvals", Label: "Approval rules", View: "approval-rules", Optional: true,
		Note: "Which documents need a second person's OK, and above what amount. Leave it if nothing does."},
	{ID: "printing", Label: "Print formats", Doctypes: []string{"PrintTemplate"}, Optional: true,
		Note: "Your own invoice and label layouts. The standard ones print until you change them."},
}

// GetSetupPath returns the steps with live counts. A multi-doctype step is
// done when every doctype in it has an active record.
func GetSetupPath(tenantID string) ([]SetupPathStep, error) {
	status, err := GetSetupStatus(tenantID)
	if err != nil {
		return nil, err
	}
	active := map[string]int{}
	for _, s := range status {
		active[s.Doctype] = s.Active
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	series, _ := ListNumberSeries(tenantID)
	var users, rules, noGSTIN int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users WHERE COALESCE(status, 'Active') = 'Active' AND %s`, schema, NotSupportUserSQL("users"))).Scan(&users)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.approval_rules`, schema)).Scan(&rules)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'LegalEntity' AND deleted_at IS NULL
		AND COALESCE(status, 'Active') = 'Active' AND COALESCE(data->>'gstin', '') = ''`, schema)).Scan(&noGSTIN)

	readiness := &setupReadiness{tenantID: tenantID, schema: schema, country: TenantPhoneCountry(tenantID)}

	steps := make([]SetupPathStep, len(setupPathPlan))
	done := map[string]bool{}
	for i, plan := range setupPathPlan {
		step := plan
		step.Waiting = []string{}
		if step.DependsOn == nil {
			step.DependsOn = []string{}
		}
		switch step.ID {
		case "numbering":
			step.Count = len(series)
			step.Done = step.Count > 0
		case "users":
			// The first administrator exists from provisioning; a second
			// active user is what "people can work" means.
			step.Count = users
			step.Done = users > 1
		case "approvals":
			step.Count = rules
			step.Done = rules > 0
		case "tax":
			// 91.3: HSN codes in use, and no active legal entity without its GSTIN.
			step.Count = active["HSNCode"]
			step.Done = step.Count > 0 && active["LegalEntity"] > 0 && noGSTIN == 0
		default:
			step.Done = true
			for _, dt := range step.Doctypes {
				step.Count += active[dt]
				if active[dt] == 0 {
					step.Done = false
				}
			}
		}
		// Stage 47.12.4: what the step's records are held to.
		switch step.ID {
		case "entity":
			step.Checks = readiness.legalEntityChecks()
		case "locations":
			step.Checks = readiness.locationChecks()
		case "tax":
			// 47.16.4d: and a warning when a GSTIN's sales pass the
			// e-invoice turnover while its IRP profile is off.
			step.Checks = append(readiness.taxChecks(active["HSNCode"]), readiness.einvoiceTurnoverChecks()...)
		case "numbering":
			step.Checks = readiness.numberingChecks(series)
		case "users":
			step.Checks = []SetupCheck{setupExists("users-second", "Someone besides the first administrator can sign in", users-1, "user")}
		case "approvals":
			step.Checks = []SetupCheck{setupExists("approvals-rule", "At least one approval rule", rules, "approval rule")}
		case "items":
			step.Checks = readiness.itemChecks(active["Item"])
		default:
			step.Checks = countChecks(step, active)
		}
		step.ValidatorVersion = SetupValidatorVersion
		step.State, step.Severity = setupStepState(step, readiness.pending(step.Doctypes))
		done[step.ID] = step.Done
		steps[i] = step
	}
	if readiness.err != nil {
		return nil, readiness.err
	}
	nextMarked := false
	for i := range steps {
		for _, dep := range steps[i].DependsOn {
			if !done[dep] {
				steps[i].Waiting = append(steps[i].Waiting, dep)
			}
		}
		if !nextMarked && !steps[i].Done && !steps[i].Optional && len(steps[i].Waiting) == 0 {
			steps[i].Next = true
			nextMarked = true
		}
	}
	return steps, nil
}
