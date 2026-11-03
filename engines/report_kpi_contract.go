package engines

import (
	"fmt"
	"strings"
	"time"
)

// reportCategoryBasis is the static basis a report's figures rest on, by its
// catalogue category. Anything not listed reads documents as entered.
var reportCategoryBasis = map[string]string{
	"Finance":   KPIBasisLedger,
	"Inventory": KPIBasisStockLedger,
	"WMS":       KPIBasisStockLedger,
}

var (
	reportFromKeys = []string{"from", "date_from", "start_date", "start"}
	reportToKeys   = []string{"to", "date_to", "end_date", "end"}
	reportAsOfKeys = []string{"as_of", "as_of_date"}
)

func firstReportParam(params map[string]string, keys []string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(params[k]); v != "" {
			return v
		}
	}
	return ""
}

// BuildReportContract is the KPI contract of one report run (Stage 47.14.3):
// period and scope come from the run's own filters, AsOf is the run's time,
// basis is the report's. Confidence stays this static basis; a live
// reconciliation state is not part of it. limited/stores describe a viewer
// cut to some stores.
func BuildReportContract(def ReportDefinition, params map[string]string, limited bool, stores []string, asOf time.Time) KPIContract {
	from, to := firstReportParam(params, reportFromKeys), firstReportParam(params, reportToKeys)
	period := "All dates"
	switch {
	case from != "" && to != "":
		period = from + " to " + to
	case from != "":
		period = "From " + from
	case to != "":
		period = "Up to " + to
	case firstReportParam(params, reportAsOfKeys) != "":
		period = "As of " + firstReportParam(params, reportAsOfKeys)
	}

	scope := "All stores"
	store := ""
	for _, k := range reportStoreKeys {
		if v := strings.TrimSpace(params[k]); v != "" {
			store = v
			break
		}
	}
	switch {
	case store != "":
		scope = "Store " + store
	case limited && len(stores) == 0:
		scope = "No stores"
	case len(stores) == 1:
		scope = "Your store: " + stores[0]
	case limited:
		scope = fmt.Sprintf("Your %d stores: %s", len(stores), strings.Join(stores, ", "))
	}

	basis := reportCategoryBasis[def.Category]
	if basis == "" {
		basis = KPIBasisOperational
	}
	owner := RoleStoreManager
	switch def.Category {
	case "Finance":
		owner = roleAccountant
	case "Admin", "Exceptions":
		owner = RoleAdmin
	}
	definition := def.Label + "."
	var filters []string
	for _, p := range def.Params {
		if v := strings.TrimSpace(params[p.Key]); v != "" {
			filters = append(filters, firstNonEmpty(p.Label, p.Key)+" "+v)
		}
	}
	if len(filters) > 0 {
		definition += " Filters: " + strings.Join(filters, ", ") + "."
	}
	return KPIContract{
		Definition: definition,
		Period:     period,
		Scope:      scope,
		AsOf:       asOf,
		Basis:      basis,
		BasisNote:  kpiBasisNotes[basis],
		Owner:      owner,
		Drill:      "reports",
	}
}

// ReportStoreScopeFor exposes the store cut a report run applied to a viewer.
func ReportStoreScopeFor(tenantID, role, userID string) (StoreAccess, bool) {
	return reportStoreScope(tenantID, role, userID)
}
