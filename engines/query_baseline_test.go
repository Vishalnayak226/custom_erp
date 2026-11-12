package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 47.9.3: a saved baseline is what later runs are compared with, and
// the summary carries the database's buffer hit / read.
func TestStage4793BaselineAndBuffers(t *testing.T) {
	db.InitDB(testConnStr())
	defer db.DB.Exec(`DELETE FROM public.ops_run_log WHERE run_type = 'query_baseline'`)
	db.DB.Exec(`DELETE FROM public.ops_run_log WHERE run_type = 'query_baseline'`)
	oldStore, oldTh := serverSlowQueries.Store, serverSlowQueries.Threshold
	defer func() { serverSlowQueries.Store, serverSlowQueries.Threshold = oldStore, oldTh }()
	serverSlowQueries.Store = NewFingerprintStore(20)
	serverSlowQueries.Threshold = time.Second
	const q = `SELECT * FROM stage4793_probe WHERE id = 7`
	serverSlowQueries.Store.Record(q, 2, 2000, 1500, 0, time.Now())

	before := SlowQueries(10)
	if before.Source == "pg_stat_statements" {
		t.Skip("pg_stat_statements is installed: this test drives the sampler's store")
	}
	if len(before.ByTotal) != 1 || before.ByTotal[0].BaselineMeanMs != nil {
		t.Fatalf("before a baseline: %+v", before.ByTotal)
	}
	if before.Baseline != nil {
		t.Fatal("a baseline is reported before one is saved")
	}
	if before.Buffers == nil || before.Buffers.Hit+before.Buffers.Read <= 0 {
		t.Fatalf("no buffer figures: %+v", before.Buffers)
	}

	if err := SaveQueryBaseline("tester"); err != nil {
		t.Fatal(err)
	}
	if err := SaveQueryBaseline("tester"); err != nil { // a second save replaces the first
		t.Fatal(err)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM public.ops_run_log WHERE run_type = 'query_baseline'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d baseline rows, want 1", n)
	}

	// The same statement later runs twice as slow.
	serverSlowQueries.Store.Record(q, 0, 2000, 1500, 0, time.Now()) // total 4000 over 2 calls = mean 2000
	serverSlowQueries.Store.Record(q, 0, 0, 3000, 0, time.Now())
	after := SlowQueries(10)
	if after.Baseline == nil || after.Baseline.SavedBy != "tester" {
		t.Fatalf("baseline: %+v", after.Baseline)
	}
	fp := after.ByTotal[0]
	if fp.BaselineMeanMs == nil || *fp.BaselineMeanMs != 1000 {
		t.Fatalf("baseline mean: %+v", fp.BaselineMeanMs)
	}
	if fp.ChangePct == nil || *fp.ChangePct != 100 {
		t.Fatalf("change: %+v", fp.ChangePct)
	}
	b, _ := json.Marshal(after)
	if strings.Contains(string(b), "id = 7") {
		t.Fatal("a literal reached the summary")
	}
}
