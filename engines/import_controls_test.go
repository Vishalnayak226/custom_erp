package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestStage47133ImportControlTotals: a file with two bad rows reports both
// rejects, the accepted sums equal what was posted, a preview writes nothing
// but shows the same totals, the import job keeps them, and running the same
// file again is reported as a rerun (all updates, nothing new).
func TestStage47133ImportControlTotals(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var jobID string
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSCostingGap' AND id LIKE 'N20D-CTL-%'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ImportJob' AND id = $1", jobID)
	}
	cleanup()
	defer cleanup()

	file := "id,cart_number,sku,qty,sale_value,status\n" +
		"N20D-CTL-1,C1,SKU1,2,100.50,Open\n" +
		"N20D-CTL-2,C1,SKU2,3,200.25,Open\n" +
		"N20D-CTL-3,C1,SKU3,abc,50,Open\n" + // qty is not a number
		"N20D-CTL-4,C1,SKU4,5,10,Bogus\n" // status not allowed
	run := func(dryRun bool) *ImportResult {
		t.Helper()
		res, err := BulkImportCSV("default", "POSCostingGap", strings.NewReader(file), "system", "", dryRun)
		if err != nil {
			t.Fatalf("BulkImportCSV(dryRun=%v): %v", dryRun, err)
		}
		if res.Controls == nil {
			t.Fatalf("no control_totals on the result")
		}
		return res
	}
	column := func(c *ImportControlTotals, field string) ImportColumnTotal {
		for _, col := range c.Columns {
			if col.Field == field {
				return col
			}
		}
		t.Fatalf("no control total for %s in %+v", field, c.Columns)
		return ImportColumnTotal{}
	}
	checkSums := func(c *ImportControlTotals) {
		t.Helper()
		// qty: "abc" is not a number, so the file holds 2+3+5.
		if q := column(c, "qty"); q.Read != 10 || q.Accepted != 5 || q.Rejected != 5 {
			t.Errorf("qty totals %+v, want read 10, accepted 5, rejected 5", q)
		}
		if v := column(c, "sale_value"); v.Read != 360.75 || v.Accepted != 300.75 || v.Rejected != 60 {
			t.Errorf("sale_value totals %+v, want read 360.75, accepted 300.75, rejected 60", v)
		}
	}

	preview := run(true)
	if c := preview.Controls; c.RowsRead != 4 || c.RowsAccepted != 2 || c.RowsRejected != 2 || !c.Balanced {
		t.Fatalf("preview controls %+v (errors %+v), want 4 read / 2 accepted / 2 rejected, balanced", c, preview.Errors)
	}
	checkSums(preview.Controls)
	var n int
	db.DB.QueryRow("SELECT count(*) FROM " + schema + ".documents WHERE doctype = 'POSCostingGap' AND id LIKE 'N20D-CTL-%'").Scan(&n)
	if n != 0 {
		t.Fatalf("the preview wrote %d row(s)", n)
	}

	first := run(false)
	c := first.Controls
	if c.RowsRead != 4 || c.RowsAccepted != 2 || c.RowsRejected != 2 || c.Created != 2 || c.Updated != 0 || c.Rerun || !c.Balanced {
		t.Fatalf("import controls %+v, want 4 / 2 / 2, 2 new, not a rerun, balanced", c)
	}
	if len(first.Errors) != 2 || first.Errors[0].RowNumber != 4 || first.Errors[1].RowNumber != 5 {
		t.Errorf("rejects %+v, want rows 4 and 5 named", first.Errors)
	}
	checkSums(c)

	// What was posted is exactly what the accepted totals say.
	var rows int
	var qty, value float64
	if err := db.DB.QueryRow(`SELECT count(*), COALESCE(SUM((data->>'qty')::numeric), 0), COALESCE(SUM((data->>'sale_value')::numeric), 0)
		FROM `+schema+`.documents WHERE doctype = 'POSCostingGap' AND id LIKE 'N20D-CTL-%'`).Scan(&rows, &qty, &value); err != nil {
		t.Fatal(err)
	}
	if rows != c.RowsAccepted || qty != column(c, "qty").Accepted || value != column(c, "sale_value").Accepted {
		t.Errorf("posted %d rows, qty %v, value %v; controls say %d, %v, %v", rows, qty, value, c.RowsAccepted, column(c, "qty").Accepted, column(c, "sale_value").Accepted)
	}

	jobID, err = RecordImportJob("default", "POSCostingGap", first, "system")
	if err != nil {
		t.Fatalf("RecordImportJob: %v", err)
	}
	var kept string
	db.DB.QueryRow("SELECT COALESCE(data->'control_totals'->>'rows_rejected', '') FROM "+schema+".documents WHERE id = $1", jobID).Scan(&kept)
	if kept != "2" {
		t.Errorf("import job keeps rows_rejected %q, want 2", kept)
	}

	again := run(false)
	if c := again.Controls; c.Created != 0 || c.Updated != 2 || !c.Rerun || !c.Balanced {
		t.Errorf("rerun controls %+v, want 0 new, 2 updated, rerun, balanced", c)
	}
}

// TestStage47133UnbalancedTotalsAreNeverSilent: counts that do not add up
// add an error line instead of passing quietly.
func TestStage47133UnbalancedTotalsAreNeverSilent(t *testing.T) {
	res := &ImportResult{TotalRows: 3, SuccessRows: 1, FailedRows: 1, CreatedIDs: []string{"a"}}
	finishImportControls(res)
	if res.Controls.Balanced {
		t.Fatal("3 read, 1 accepted, 1 rejected was reported as balanced")
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Message, "do not balance") {
		t.Errorf("errors %+v, want one 'do not balance' line", res.Errors)
	}
	ok := &ImportResult{TotalRows: 2, SuccessRows: 1, FailedRows: 1, UpdatedIDs: []string{"a"}}
	finishImportControls(ok)
	if !ok.Controls.Balanced || !ok.Controls.Rerun || len(ok.Errors) != 0 {
		t.Errorf("balanced all-update run: %+v, errors %+v", ok.Controls, ok.Errors)
	}
}
