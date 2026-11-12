package engines

import (
	"custom_erp/db"
	"errors"
	"testing"
)

// Stage 132.13 (n): every registered report that is not a catalog report (those run in
// TestCatalogReportsRun) must at least execute. The Clienteling Tasks report failed for
// every client with "subquery uses ungrouped column t.store" and no test ran it; Guide
// found it on app. A deliberate refusal (a ValidationError - too many rows, a missing
// required input) is fine; a database error is not.
func TestEveryRegisteredReportRuns(t *testing.T) {
	db.InitDB(testConnStr())
	catalog := map[string]bool{}
	for _, spec := range catalogSpecs {
		catalog[spec.ID] = true
	}
	ran := 0
	for _, id := range reportRegistryOrder {
		def, ok := reportRegistry[id]
		if !ok || catalog[id] || def.Run == nil {
			continue
		}
		params := map[string]string{}
		needsInput := false // a report about one customer / SKU / account cannot run without it
		for _, p := range def.Params {
			if p.Required && p.Type != "date" {
				needsInput = true
			}
			if p.Type == "date" {
				switch p.Key {
				case "end", "to", "to_date", "end_date", "as_of", "until":
					params[p.Key] = "2026-12-31"
				default:
					params[p.Key] = "2026-01-01"
				}
			}
		}
		if needsInput {
			continue
		}
		ran++
		if _, err := def.Run("default", params); err != nil {
			var ve *ValidationError
			if errors.As(err, &ve) {
				continue
			}
			t.Errorf("%s %v: %v", id, params, err)
		}
	}
	if ran == 0 {
		t.Fatal("no registered report outside the catalog")
	}
	t.Logf("%d registered reports outside the catalog ran", ran)
}

// Stage 132.13 (o): a total named after the price it is taken at is summed when rows are
// grouped; a price, rate, percentage, date or number column is not.
func TestCatalogValueAtPriceIsSummed(t *testing.T) {
	for h, want := range map[string]bool{
		"Stock Value at Purchase Price": true, "Stock Value at Selling Price": true, "On Hand Qty": true, "Net Amount": true,
		"Purchase Price": false, "MRP": false, "GST Rate": false, "Margin %": false, "Invoice No": false, "Bill Date": false,
	} {
		if got := catNumericHeader(h); got != want {
			t.Errorf("catNumericHeader(%q) = %v, want %v", h, got, want)
		}
	}
}
