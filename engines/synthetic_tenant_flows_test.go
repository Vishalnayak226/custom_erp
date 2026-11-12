package engines

import (
	"custom_erp/db"
	"testing"
)

func flowsShape() SynthShape {
	s, _ := SynthShapeNamed("small")
	s.Name = "flows-test"
	s.Bills = 80
	s.LargeOrders = 1
	s.LargeOrderLines = 10
	s.PurchaseOrders = 4
	s.SalesOrders = 5
	s.ProductionOrders = 3
	s.HistoryDays = 200
	return s
}

// Item 47.9.1: the purchasing, online-order and production flows run through
// the real engines and are counted, and the history is backdated across the
// span asked for while the content checksum stays as it was.
func TestSyntheticTenantFlowsAndBackdating(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a tenant")
	}
	synthTestEnv(t)
	shape := flowsShape()
	res := synthRun(t, "flows", SynthOptions{Seed: 7, Custom: &shape})
	for key, want := range map[string]int64{"purchase_orders": 4, "grns": 8, "sales_orders": 5, "production_orders": 3} {
		if res.Counts[key] != want {
			t.Errorf("%s = %d, want %d", key, res.Counts[key], want)
		}
	}
	var posted, completed int64
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + res.Schema + `.documents WHERE doctype = 'GRN' AND data->>'grn_step' = 'Posted'`).Scan(&posted)
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + res.Schema + `.documents WHERE doctype = 'ProductionOrder' AND status = 'Completed'`).Scan(&completed)
	if posted != 4 {
		t.Errorf("posted GRNs = %d, want 4", posted)
	}
	if completed != 3 {
		t.Errorf("completed production orders = %d, want 3", completed)
	}
	var oldest, newest string
	var future int64
	if err := db.DB.QueryRow(`SELECT (now()-MIN(created_at))::text, (now()-MAX(created_at))::text,
		COUNT(*) FILTER (WHERE created_at > now()) FROM `+res.Schema+`.documents`).Scan(&oldest, &newest, &future); err != nil {
		t.Fatal(err)
	}
	if future != 0 {
		t.Errorf("%d documents are dated in the future", future)
	}
	var spreadDays float64
	_ = db.DB.QueryRow(`SELECT EXTRACT(EPOCH FROM (MAX(created_at)-MIN(created_at)))/86400 FROM ` + res.Schema + `.documents`).Scan(&spreadDays)
	if spreadDays < 190 || spreadDays > 201 {
		t.Errorf("history spans %.1f days (oldest %s ago, newest %s ago), want about 200", spreadDays, oldest, newest)
	}
	// Backdating changes dates only: the same shape without it hashes the same.
	plain := shape
	plain.HistoryDays = 0
	again := synthRun(t, "flowsplain", SynthOptions{Seed: 7, Custom: &plain})
	if again.Checksum != res.Checksum {
		t.Errorf("backdating changed the content checksum: %s vs %s", res.Checksum, again.Checksum)
	}
}

// Item 47.9.1: four store-sharded workers build the tenant one worker builds -
// same counts, same checksum - and Measure, which reads transaction ids,
// refuses to run beside them.
func TestSyntheticTenantParallelLoaderMatchesSerial(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two tenants")
	}
	synthTestEnv(t)
	shape := flowsShape()
	shape.Stores = 4
	shape.HistoryDays = 0
	serial := synthRun(t, "serial", SynthOptions{Seed: 9, Custom: &shape})
	par := synthRun(t, "par4", SynthOptions{Seed: 9, Custom: &shape, Workers: 4})
	if serial.Checksum != par.Checksum {
		t.Fatalf("parallel checksum %s differs from serial %s", par.Checksum, serial.Checksum)
	}
	for k, v := range serial.Counts {
		if k == "audit_logs" || k == "audit_actions" {
			continue // customer-update rows depend on store interleaving
		}
		if par.Counts[k] != v {
			t.Errorf("count %s: serial %d, parallel %d", k, v, par.Counts[k])
		}
	}
	if serial.Counts["bills"] != int64(shape.Bills) {
		t.Errorf("bills = %d, want %d", serial.Counts["bills"], shape.Bills)
	}
	if _, err := GenerateSyntheticTenant(SynthOptions{Shape: "small", TenantID: "synth-measure-workers", Measure: true, Workers: 2}); err == nil {
		t.Error("Measure with Workers > 1 was accepted")
	}
}
