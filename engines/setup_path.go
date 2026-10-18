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
}

var setupPathPlan = []SetupPathStep{
	{ID: "entity", Label: "Legal entity", Doctypes: []string{"LegalEntity"}},
	{ID: "locations", Label: "Stores and warehouses", Doctypes: []string{"Location"}, DependsOn: []string{"entity"}},
	{ID: "org", Label: "Departments and cost centres", Doctypes: []string{"Department", "CostCenter"}, DependsOn: []string{"entity"}},
	{ID: "units", Label: "Units and HSN codes", Doctypes: []string{"UOM", "HSNCode"}},
	{ID: "item-types", Label: "Item types and categories", Doctypes: []string{"ItemType", "Category"}, DependsOn: []string{"units"}},
	{ID: "items", Label: "Items", Doctypes: []string{"Item"}, DependsOn: []string{"item-types"}},
	{ID: "parties", Label: "Vendors and customers", Doctypes: []string{"Vendor", "Customer"}, DependsOn: []string{"entity"}},
	{ID: "numbering", Label: "Document numbering", View: "prefix-configs", DependsOn: []string{"locations"}},
	{ID: "users", Label: "Users and their roles", View: "users", DependsOn: []string{"locations"}},
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
	var users int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users WHERE COALESCE(status, 'Active') = 'Active'`, schema)).Scan(&users)

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
		default:
			step.Done = true
			for _, dt := range step.Doctypes {
				step.Count += active[dt]
				if active[dt] == 0 {
					step.Done = false
				}
			}
		}
		done[step.ID] = step.Done
		steps[i] = step
	}
	nextMarked := false
	for i := range steps {
		for _, dep := range steps[i].DependsOn {
			if !done[dep] {
				steps[i].Waiting = append(steps[i].Waiting, dep)
			}
		}
		if !nextMarked && !steps[i].Done && len(steps[i].Waiting) == 0 {
			steps[i].Next = true
			nextMarked = true
		}
	}
	return steps, nil
}
