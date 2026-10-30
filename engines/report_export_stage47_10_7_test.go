package engines

import (
	"context"
	"custom_erp/db"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 47.10.7 (adversarial journey #8, report half): what the screen shows for a
// report - its rows, its control-total strip - and what the export job writes
// to CSV for the same report agree, and still agree after writes land
// between the two.
//
// As-of boundary, stated: a report run (the screen's, the export job's) is as
// of its own query; the control-total strip's summary and cut-off come from
// one snapshot taken right after that run. An export queued after a write
// includes the write, so it can show more than a screen drawn before it -
// the screen's own re-run then agrees with the export. There is no shared
// snapshot across the two, and nothing claims one.
func TestStage47107ReportExportTotalsAgree(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	tag := fmt.Sprintf("T47107-%d", time.Now().UnixNano()%1000000)
	var jobIDs []string
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'PurchaseOrder' AND id LIKE $1", tag+"%")
		for _, id := range jobIDs {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ReportExportJob' AND id = $1", id)
		}
	}
	cleanup()
	defer cleanup()

	// Approved purchase orders spread over the report's age buckets.
	seed := func(n int, amount float64, ageDays int) {
		t.Helper()
		id := fmt.Sprintf("%s-%03d", tag, n)
		raw, _ := json.Marshal(map[string]interface{}{"id": id, "supplier": "T47107", "total_amount": amount})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, 'PurchaseOrder', $2, 'Approved', 'system', CURRENT_TIMESTAMP - make_interval(days => $3))", id, raw, ageDays); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	var seeded float64
	for i := 1; i <= 12; i++ {
		amount := float64(1000*i) + 0.25
		seeded += amount
		seed(i, amount, []int{3, 40, 75, 120}[i%4])
	}

	type figures struct {
		rows   int
		count  float64
		amount float64
	}
	screen := func() (figures, *ReportControlTotals) {
		t.Helper()
		_, rows, _, ct, err := RunReportWithControlTotals(context.Background(), tenantID, "po-commitments", RoleSuperAdmin, "system", nil)
		if err != nil || ct == nil {
			t.Fatalf("screen run: %v (controls %v)", err, ct)
		}
		return figures{rows: len(rows), count: sumRowField(rows, "count"), amount: sumRowField(rows, "amount")}, ct
	}
	export := func() figures {
		t.Helper()
		jobID, err := CreateReportExportJob(tenantID, "po-commitments", RoleSuperAdmin, nil, "system")
		if err != nil {
			t.Fatalf("queue export: %v", err)
		}
		jobIDs = append(jobIDs, jobID)
		processReportExportJobs(context.Background(), schema)
		status, csvBytes, code, err := GetReportExportJob(tenantID, jobID)
		if err != nil || status != "Completed" {
			t.Fatalf("export job %s: status %q code %q err %v", jobID, status, code, err)
		}
		records, err := csv.NewReader(strings.NewReader(string(csvBytes))).ReadAll()
		if err != nil || len(records) == 0 {
			t.Fatalf("export CSV: %v (%d records)", err, len(records))
		}
		col := map[string]int{}
		for i, h := range records[0] {
			col[h] = i
		}
		for _, want := range []string{"Age Bucket", "PO Count", "Committed Amount"} {
			if _, ok := col[want]; !ok {
				t.Fatalf("export header %v lacks %q - the CSV must carry the report's own columns", records[0], want)
			}
		}
		f := figures{rows: len(records) - 1}
		for _, rec := range records[1:] {
			n, _ := strconv.ParseFloat(rec[col["PO Count"]], 64)
			a, _ := strconv.ParseFloat(rec[col["Committed Amount"]], 64)
			f.count += n
			f.amount += a
		}
		return f
	}
	agree := func(what string, s figures, ct *ReportControlTotals, e figures) {
		t.Helper()
		if s.rows != e.rows || !near(s.count, e.count) || !near(s.amount, e.amount) {
			t.Fatalf("%s: screen rows %d count %.0f amount %.2f; export rows %d count %.0f amount %.2f - they must agree", what, s.rows, s.count, s.amount, e.rows, e.count, e.amount)
		}
		if !near(ct.DetailTotal, e.amount) || !near(ct.SummaryTotal, e.amount) || ct.CutOff == "" {
			t.Fatalf("%s: control strip detail %.2f summary %.2f (cut-off %q) vs export amount %.2f", what, ct.DetailTotal, ct.SummaryTotal, ct.CutOff, e.amount)
		}
	}

	s1, ct1 := screen()
	if s1.count < 12 || s1.amount < seeded-0.01 {
		t.Fatalf("the seeded orders are not in the report: %+v", s1)
	}
	agree("quiet", s1, ct1, export())

	// Writes land between the screen's run and the export: the export is as
	// of its own query, so it carries them; the screen agrees once re-run.
	s2, _ := screen()
	var added float64
	for i := 13; i <= 17; i++ {
		amount := float64(500 * i)
		added += amount
		seed(i, amount, 10)
	}
	e2 := export()
	if !near(e2.count, s2.count+5) || !near(e2.amount, s2.amount+added) {
		t.Fatalf("export after 5 writes: count %.0f amount %.2f; screen before them count %.0f amount %.2f + %.2f - the export is as of its own query and must carry the writes", e2.count, e2.amount, s2.count, s2.amount, added)
	}
	s3, ct3 := screen()
	agree("after writes", s3, ct3, e2)
}
