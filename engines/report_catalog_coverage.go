package engines

import (
	"reflect"
	"strings"
)

// Stage 75.3.2: where each of the catalog's 290 reports stands - built
// (registered under its catalog id, with the columns the ERP cannot fill
// named) or waiting for a module that does not exist yet. cmd/gencatalog
// -coverage writes it to docs/product/report-catalog-coverage.md.

// catalogWaits: why a catalog report is not built, by id prefix.
var catalogWaits = []struct{ prefix, why string }{
	{"cat-delivery-note", "Delivery challan documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-job-work", "Job work outward / inward documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-material-issue", "Material issue documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-material-receipt", "Material receipt documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-mi-mr", "Material issue / receipt documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-wastage", "Wastage documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-damage", "Damage documents do not exist yet - Stage 74 (74.5.1)"},
	{"cat-stock-adjustment", "The Stock Adjustment document does not exist yet - 88.5.4"},
	{"cat-receipt-note", "Receipt notes do not exist yet - Stage 74 (74.5.1)"},
	{"cat-bulk-stock-upload", "The ERP has no bulk stock upload"},
	{"cat-e-way-bill", "The ERP does not issue e-way bills (75.2 covers e-invoices only)"},
	{"cat-lead-register", "The ERP keeps no leads"},
	{"cat-opportunity-pipeline", "The ERP keeps no opportunities"},
	{"cat-customer-feedback", "The ERP keeps no customer feedback or NPS responses"},
	{"cat-recruitment", "The ERP keeps no recruitment records"},
}

// CatalogCoverageRow is one catalog report's standing.
type CatalogCoverageRow struct {
	ID, Module, Group, Name string
	Built                   bool
	Note                    string
	Columns, Filled         int
	Blank                   []string
}

var catNotHeldPtr = reflect.ValueOf(catNotHeld).Pointer()

func catHeld(f catFn) bool { return f != nil && reflect.ValueOf(f).Pointer() != catNotHeldPtr }

// CatalogCoverage lists every catalog report in the catalog's order.
func CatalogCoverage() []CatalogCoverageRow {
	out := make([]CatalogCoverageRow, 0, len(catalogSpecs))
	for _, s := range catalogSpecs {
		row := CatalogCoverageRow{ID: s.ID, Module: s.Module, Group: s.Group, Name: s.Name, Columns: len(s.Columns)}
		w, ok := catalogWiring[s.ID]
		if !ok {
			for _, x := range catalogWaits {
				if strings.HasPrefix(s.ID, x.prefix) {
					row.Note = x.why
				}
			}
			if row.Note == "" {
				row.Note = "Not built"
			}
			out = append(out, row)
			continue
		}
		row.Built = true
		for _, h := range s.Columns {
			held := false
			if f, has := w.Cols[h]; has {
				held = catHeld(f)
			} else {
				for _, src := range w.sources() {
					if f, has := src.Cols[h]; has {
						held = held || catHeld(f)
					} else if catHeld(catCommon[h]) {
						held = true
					}
				}
			}
			if held {
				row.Filled++
			} else {
				row.Blank = append(row.Blank, h)
			}
		}
		out = append(out, row)
	}
	return out
}
