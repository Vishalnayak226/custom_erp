package engines

import (
	"fmt"
	"strings"
	"time"
)

// Stage 47.14.3 - the KPI contract. "327 SLA breaches" without context
// cannot ship: a figure on a dashboard says what it counts, over which
// period, for whose stores, when it was read, how far it can be trusted,
// which role answers for it and where a click takes you.
//
// Every dataset a dashboard widget reads (dashboard_datasets.go and the ones
// other modules register) declares the two parts that are not in its query:
// the accountable role and the basis of its figures. RunDatasetQuery builds
// the rest from the query itself and puts the whole contract on the result,
// so every number, chart and table carries it. TestStage47143 fails when a
// dataset ships without its entry here.

// KPI bases: how far a dataset's figures are reconciled.
const (
	// KPIBasisLedger: posted double-entry rows - every posting balances, and
	// the trial balance proves the books agree.
	KPIBasisLedger = "Posted ledger"
	// KPIBasisStockLedger: posted stock movements and the balances they
	// keep, which the stock book vs ledger check reconciles.
	KPIBasisStockLedger = "Stock ledger"
	// KPIBasisOperational: documents as people entered them (bills, orders,
	// receipts, claims, customers, messages) - true to the records, not
	// reconciled to the books.
	KPIBasisOperational = "Operational records"
)

// kpiBasisNotes is what each basis means, in a line, for the contract.
var kpiBasisNotes = map[string]string{
	KPIBasisLedger:      "Posted double-entry rows; every posting balances and the trial balance ties them to the books.",
	KPIBasisStockLedger: "Posted stock movements and the balances they keep; checked by the stock book vs ledger reconciliation.",
	KPIBasisOperational: "Documents as they were entered; not reconciled to the books.",
}

// datasetContract is what a dataset declares for its figures.
type datasetContract struct {
	Owner string // the role accountable for the figures
	Basis string // one of the KPIBasis* constants
}

const roleAccountant = "Accountant"

// datasetContracts is every built-in dataset's declaration, by key.
var datasetContracts = map[string]datasetContract{
	"sales_lines":     {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"pos_bills":       {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"stock_on_hand":   {Owner: RoleStoreManager, Basis: KPIBasisStockLedger},
	"stock_moves":     {Owner: RoleStoreManager, Basis: KPIBasisStockLedger},
	"purchase_orders": {Owner: RoleAdmin, Basis: KPIBasisOperational},
	"goods_receipts":  {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"transfers":       {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"customers":       {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"loyalty":         {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"online_orders":   {Owner: RoleAdmin, Basis: KPIBasisOperational},
	"expenses":        {Owner: roleAccountant, Basis: KPIBasisOperational},
	"ledger":          {Owner: roleAccountant, Basis: KPIBasisLedger},
	"crm_messages":    {Owner: RoleAdmin, Basis: KPIBasisOperational},
	"crm_consent":     {Owner: RoleAdmin, Basis: KPIBasisOperational},
	"crm_feedback":    {Owner: RoleStoreManager, Basis: KPIBasisOperational},
	"crm_pipeline":    {Owner: RoleAdmin, Basis: KPIBasisOperational},
}

// KPIContract travels with every dataset result.
type KPIContract struct {
	// Definition: what is counted - the dataset, each total, and the filters.
	Definition string `json:"definition"`
	// Period: the dates the figures cover, "All dates", or "Now" for a
	// snapshot such as stock on hand.
	Period string `json:"period"`
	// Scope: whose rows - every store, one store, or the viewer's own stores.
	Scope string `json:"scope"`
	// AsOf: when the figures were read (results are kept 60 seconds).
	AsOf time.Time `json:"as_of"`
	// Basis and BasisNote: how far the figures are reconciled.
	Basis     string `json:"basis"`
	BasisNote string `json:"basis_note"`
	// Owner: the role that answers for the figures.
	Owner string `json:"owner"`
	// Drill: the screen a click opens, and the record type behind each row.
	Drill       string `json:"drill"`
	DrillRecord string `json:"drill_record,omitempty"`
}

// datasetFilterOps reads a filter operator in words.
var datasetFilterOps = map[string]string{
	"eq": "is", "ne": "is not", "in": "is one of", "contains": "contains", "gte": "is at least", "lte": "is at most",
}

// buildKPIContract fills the contract for one query of ds. stores is the
// viewer's store list when they are limited to some, else nil.
func buildKPIContract(ds DashboardDataset, q DatasetQuery, limited bool, stores []string, asOf time.Time) KPIContract {
	label := func(key string) string {
		if f, ok := ds.field(key); ok {
			return f.Label
		}
		return key
	}
	parts := []string{strings.TrimSuffix(ds.About, ".") + "."}
	var totals []string
	for _, m := range q.Measures {
		if m.Field == "" {
			totals = append(totals, "number of rows")
			continue
		}
		totals = append(totals, datasetMeasureLabel(m.Agg, label(m.Field)))
	}
	if len(totals) > 0 {
		parts = append(parts, "Shows "+strings.Join(totals, ", ")+".")
	}
	if len(q.GroupBy) > 0 {
		var by []string
		for _, g := range q.GroupBy {
			by = append(by, label(g))
		}
		parts = append(parts, "By "+strings.Join(by, " and ")+".")
	}
	for _, f := range q.Filters {
		op := datasetFilterOps[f.Op]
		if op == "" {
			op = f.Op
		}
		parts = append(parts, fmt.Sprintf("Only where %s %s %s.", label(f.Field), op, strings.ReplaceAll(f.Value, ",", ", ")))
	}

	period := "All dates"
	switch {
	case ds.DateField == "":
		period = "Now (a snapshot)"
	case q.DateFrom != "" && q.DateTo != "":
		period = q.DateFrom + " to " + q.DateTo
	case q.DateFrom != "":
		period = "From " + q.DateFrom
	case q.DateTo != "":
		period = "Up to " + q.DateTo
	}

	scope := "All stores"
	switch {
	case q.Store != "":
		scope = "Store " + q.Store
	case limited && len(stores) == 0:
		scope = "No stores"
	case len(stores) == 1:
		scope = "Your store: " + stores[0]
	case limited:
		scope = fmt.Sprintf("Your %d stores: %s", len(stores), strings.Join(stores, ", "))
	}

	c := datasetContracts[ds.Key]
	drill := ds.List
	if drill == "" {
		drill = ds.Doctype
	}
	record := ""
	if ds.DocField != "" {
		record = ds.Doctype
	}
	return KPIContract{
		Definition:  strings.Join(parts, " "),
		Period:      period,
		Scope:       scope,
		AsOf:        asOf,
		Basis:       c.Basis,
		BasisNote:   kpiBasisNotes[c.Basis],
		Owner:       c.Owner,
		Drill:       drill,
		DrillRecord: record,
	}
}
