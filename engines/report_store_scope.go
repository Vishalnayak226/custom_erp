package engines

import (
	"fmt"
	"strings"
)

// Stage 75.3.3: store scope on reports, at the choke point every run goes
// through (RunReportContext - the report screen, exports, scheduled reports,
// dashboard tiles - and the drill-down). A user whose store access is not
// "All stores" (Stage 85), in a role with no company-wide store scope, sees
// only the rows of the stores they may work at, and a run or an export whose
// filter names another store is refused. Before this, a Store Manager's
// report showed every store's figures.
//
// A report's store sits under ReportDefinition.StoreKey; left empty, the first
// column named like a store is used; "-" marks a report with no store
// dimension (masters, company figures behind finance.statements), which is
// not cut.

// reportStoreKeys are the row / filter keys a store is carried under, in the
// order they are looked for.
var reportStoreKeys = []string{"location", "location_code", "store", "store_code", "warehouse", "warehouse_id", "warehouse_code", "from_warehouse", "from_store"}

func isReportStoreKey(key string) bool {
	for _, k := range reportStoreKeys {
		if k == key {
			return true
		}
	}
	return false
}

// reportStoreKey is the row field a report's store is in, or "".
func reportStoreKey(d ReportDefinition) string {
	if d.StoreKey == "-" {
		return ""
	}
	if d.StoreKey != "" {
		return d.StoreKey
	}
	for _, k := range reportStoreKeys {
		for _, c := range d.Columns {
			if c.Key == k {
				return k
			}
		}
	}
	return ""
}

// reportStoreScope reports whether this user's report rows are cut to their
// stores, and which. An empty userID is an internal run (a tile's count, a
// control total) and is not cut; a lookup failure cuts everything (fail
// closed).
func reportStoreScope(tenantID, role, userID string) (StoreAccess, bool) {
	if strings.TrimSpace(userID) == "" || HasFullAccess(role) || roleTemplateGrantsGlobalScope(role, ScopeLocation) {
		return StoreAccess{}, false
	}
	access, err := UserStoreAccess(tenantID, userID)
	if err != nil {
		return StoreAccess{Stores: map[string]bool{}}, true
	}
	if access.All {
		return access, false
	}
	return access, true
}

// checkReportStoreParams refuses filters naming a store the user may not see.
func checkReportStoreParams(d ReportDefinition, params map[string]string, access StoreAccess) error {
	for _, p := range d.Params {
		if !isReportStoreKey(p.Key) {
			continue
		}
		for _, v := range strings.Split(params[p.Key], ",") {
			if v = strings.TrimSpace(v); v != "" && !access.Allows(v) {
				return &ValidationError{Code: "REPORT-0987", SubFor: firstNonEmpty(p.Label, p.Key), Message: fmt.Sprintf("store %s is not one of the stores you may see", v)}
			}
		}
	}
	return nil
}

// filterReportRowsByStore keeps the rows of stores the user may see. A row
// with no store is dropped too: it cannot be shown to belong to theirs.
func filterReportRowsByStore(rows []map[string]interface{}, key string, access StoreAccess) []map[string]interface{} {
	out := rows[:0:0]
	for _, r := range rows {
		if v, ok := r[key]; ok && access.Allows(strings.TrimSpace(fmt.Sprint(v))) {
			out = append(out, r)
		}
	}
	return out
}

// CheckReportStoreAccess is the up-front check an export makes before
// queueing (the run applies the same rule again).
func CheckReportStoreAccess(tenantID, reportID, role, userID string, params map[string]string) error {
	d, ok := reportRegistry[reportID]
	if !ok {
		return fmt.Errorf("unknown report %q", reportID)
	}
	if access, limited := reportStoreScope(tenantID, role, userID); limited {
		return checkReportStoreParams(d, params, access)
	}
	return nil
}
