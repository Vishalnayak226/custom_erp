package engines

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 47.14.3: no dashboard figure ships without its contract. Every
// dataset declares its accountable role and basis; every ready-made widget's
// result carries a definition, period, scope, as-of time, basis, owner and
// drill; and every default home tile's report says what it answers.
func TestStage47143EveryKPICarriesItsContract(t *testing.T) {
	// The declarations - checked without a database, so a new dataset fails
	// here the moment it is registered without them.
	for _, ds := range dashboardDatasets {
		c, ok := datasetContracts[ds.Key]
		switch {
		case !ok:
			t.Errorf("dataset %s has no KPI contract entry (dashboard_kpi_contract.go)", ds.Key)
		case c.Owner == "":
			t.Errorf("dataset %s names no accountable role", ds.Key)
		case kpiBasisNotes[c.Basis] == "":
			t.Errorf("dataset %s basis %q is not one of the KPI bases", ds.Key, c.Basis)
		}
		if strings.TrimSpace(ds.About) == "" || (ds.List == "" && ds.Doctype == "") {
			t.Errorf("dataset %s needs a definition (About) and somewhere to drill", ds.Key)
		}
	}
	for key := range datasetContracts {
		if _, ok := DashboardDatasetByKey(key); !ok {
			t.Errorf("contract entry %s names no dataset", key)
		}
	}
	for _, tile := range DefaultDashboardTiles() {
		if _, ok := reportRegistry[tile.ReportID]; !ok {
			t.Errorf("default tile %s runs no registered report", tile.ReportID)
			continue
		}
		if q := reportQuestions[tile.ReportID]; q.Question == "" || q.Description == "" {
			t.Errorf("default tile %s (%s) does not say what it answers", tile.Title, tile.ReportID)
		}
	}

	// The contract on real results: every ready-made widget, with its
	// dashboard's filters, as an administrator.
	db.InitDB(testConnStr())
	const tenantID = "default"
	admin := DatasetViewer{Role: "Admin", UserID: "admin"}
	now := time.Now()
	for _, d := range ReadyMadeDashboards() {
		for _, w := range d.Widgets {
			if w.Query == nil {
				continue
			}
			q := ApplyDashboardFilters(*w.Query, d.Filters, w.OwnFilters, now)
			res, err := RunDatasetQuery(tenantID, admin, q)
			if err != nil {
				t.Errorf("%s / %s: %v", d.Name, w.Title, err)
				continue
			}
			c := res.Contract
			if c.Definition == "" || c.Period == "" || c.Scope == "" || c.AsOf.IsZero() || c.Basis == "" ||
				c.BasisNote == "" || c.Owner == "" || c.Drill == "" {
				t.Errorf("%s / %s: incomplete contract %+v", d.Name, w.Title, c)
			}
			if !c.AsOf.Equal(res.CachedAt) {
				t.Errorf("%s / %s: as-of %v is not when the figures were read (%v)", d.Name, w.Title, c.AsOf, res.CachedAt)
			}
		}
	}

	run := func(q DatasetQuery) KPIContract {
		t.Helper()
		res, err := RunDatasetQuery(tenantID, admin, q)
		if err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
		return res.Contract
	}
	// A month-to-date sales total says its dates, every store, and that it is
	// operational - not the books.
	from, to := ResolveDashboardRange(DashboardFilters{Range: "mtd"}, now)
	c := run(DatasetQuery{Dataset: "pos_bills", Measures: []DatasetMeasure{dsSum("value")}, DateFrom: from, DateTo: to})
	if c.Period != from+" to "+to || c.Scope != "All stores" || c.Basis != KPIBasisOperational || c.Owner != RoleStoreManager ||
		!strings.Contains(c.Definition, "Bill value") || c.Drill != "pos" || c.DrillRecord != "POSCart" {
		t.Errorf("month-to-date sales contract: %+v", c)
	}
	// "Owed by customers" names its account filter and rests on the ledger.
	c = run(DatasetQuery{Dataset: "ledger", Measures: []DatasetMeasure{dsSum("net")},
		Filters: []DatasetFilter{{Field: "account_code", Op: "eq", Value: "1300"}}})
	if c.Basis != KPIBasisLedger || c.Owner != roleAccountant || c.Period != "All dates" ||
		!strings.Contains(c.Definition, "Only where Account code is 1300") {
		t.Errorf("receivables contract: %+v", c)
	}
	// Stock on hand is a snapshot, and one store's figure says which store.
	c = run(DatasetQuery{Dataset: "stock_on_hand", Measures: []DatasetMeasure{countRows}, Store: "HO"})
	if c.Period != "Now (a snapshot)" || c.Scope != "Store HO" || c.Basis != KPIBasisStockLedger ||
		!strings.Contains(c.Definition, "number of rows") {
		t.Errorf("stock snapshot contract: %+v", c)
	}
}

// TestStage47143ContractReachesTheScreen: the keys view-dashboards.js reads
// off a dataset result (kpiContractLines) are on the wire under "contract",
// so every tile can show its period, scope, as-of, basis, owner and drill.
func TestStage47143ContractReachesTheScreen(t *testing.T) {
	ds, ok := DashboardDatasetByKey("pos_bills")
	if !ok {
		t.Fatal("pos_bills dataset is not registered")
	}
	asOf := time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
	res := DatasetResult{Contract: buildKPIContract(ds, DatasetQuery{Dataset: "pos_bills", DateFrom: "2026-10-01", DateTo: "2026-10-31"}, false, nil, asOf)}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	var contract map[string]interface{}
	if err := json.Unmarshal(wire["contract"], &contract); err != nil {
		t.Fatalf("no contract on the wire: %s", raw)
	}
	for _, key := range []string{"definition", "period", "scope", "as_of", "basis", "basis_note", "owner", "drill"} {
		if v, _ := contract[key].(string); strings.TrimSpace(v) == "" {
			t.Errorf("contract.%s is empty on the wire: %s", key, wire["contract"])
		}
	}
	if contract["as_of"] != "2026-10-29T09:30:00Z" {
		t.Errorf("as_of %v is not the RFC 3339 instant the screen turns into \"N min ago\"", contract["as_of"])
	}
}
