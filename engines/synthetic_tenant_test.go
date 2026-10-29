package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// synthTestEnv points the generator's guard at the test database on purpose -
// the suite already writes to whatever TEST_DATABASE_URL names - and turns
// external side effects off the way the guard demands.
func synthTestEnv(t *testing.T) {
	t.Helper()
	db.InitDB(testConnStr())
	var name string
	if err := db.DB.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV", "")
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
	t.Setenv("ERP_SYNTH_ALLOW_DB", name)
}

// Stage 47.9.1: the generator refuses production, a run that could reach the
// outside world, and a database that does not look like a scratch one.
func TestSyntheticTenantGuard(t *testing.T) {
	synthTestEnv(t)
	if err := CheckSyntheticTarget(); err != nil {
		t.Fatalf("named scratch database refused: %v", err)
	}
	t.Setenv("ENV", "production")
	if err := CheckSyntheticTarget(); err == nil || !strings.Contains(err.Error(), "production") {
		t.Errorf("ENV=production: %v", err)
	}
	t.Setenv("ENV", "")
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "")
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	if err := CheckSyntheticTarget(); err == nil || !strings.Contains(err.Error(), "side effects") {
		t.Errorf("side effects on: %v", err)
	}
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "")
	t.Setenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS", "1")
	t.Setenv("ERP_SYNTH_ALLOW_DB", "some_other_db")
	var name string
	_ = db.DB.QueryRow(`SELECT current_database()`).Scan(&name)
	if err := CheckSyntheticTarget(); !synthScratchDB.MatchString(name) && err == nil {
		t.Errorf("database %q is not named scratch and was not allowed, yet passed", name)
	}
	for dbName, want := range map[string]bool{"custom_erp": false, "custom_erp_inspect": false, "erp_prod": false,
		"custom_erp_test": true, "erp_synth_10k": true, "bench_100k": true, "perf_lab": true, "scratch1": true} {
		if got := synthScratchDB.MatchString(dbName); got != want {
			t.Errorf("scratch(%q) = %v, want %v", dbName, got, want)
		}
	}
	if _, err := GenerateSyntheticTenant(SynthOptions{Shape: "no-such-shape", TenantID: "synth-guard"}); err == nil {
		t.Error("an unknown shape was accepted")
	}
}

// synthRun builds one synthetic tenant for a test and drops it afterwards.
func synthRun(t *testing.T, label string, opts SynthOptions) *SynthResult {
	t.Helper()
	opts.TenantID = fmt.Sprintf("synth-%s-%d", label, time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() { _ = DeleteSyntheticTenant(opts.TenantID) })
	res, err := GenerateSyntheticTenant(opts)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	return res
}

// Stage 47.9.1: one seed builds the same tenant twice - identical counts and
// checksum - and a second seed builds a different one. A tiny custom shape
// keeps the three builds quick; the named small shape is checked below.
func TestSyntheticTenantDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three tenants")
	}
	synthTestEnv(t)
	tiny := SynthShape{Name: "tiny", Items: 12, Stores: 1, Warehouses: 2, BinsPerWarehouse: 10, StockedBinPct: 30, Vendors: 2, Customers: 5,
		Bills: 12, MaxLinesPerBill: 3, HotSKUPct: 40, LargeOrders: 1, LargeOrderLines: 8, ReturnPct: 30, CustomerBillPct: 40,
		AuditTrail: 20, Owners: 2, Months: 2, MonthEndEntries: 3}
	a := synthRun(t, "a", SynthOptions{Custom: &tiny, Seed: 7})
	b := synthRun(t, "b", SynthOptions{Custom: &tiny, Seed: 7})
	c := synthRun(t, "c", SynthOptions{Custom: &tiny, Seed: 8})
	t.Logf("tiny seed 7: %v in %s, checksum %s", a.Counts, a.Elapsed.Round(time.Millisecond), a.Checksum)
	if a.Checksum != b.Checksum {
		t.Errorf("same seed, different tenants: %s vs %s\n%v\n%v", a.Checksum, b.Checksum, a.Counts, b.Counts)
	}
	for k, v := range a.Counts {
		if b.Counts[k] != v {
			t.Errorf("count %s: %d vs %d", k, v, b.Counts[k])
		}
	}
	if a.Checksum == c.Checksum {
		t.Error("a different seed built an identical tenant")
	}
	if dup, err := GenerateSyntheticTenant(SynthOptions{Custom: &tiny, Seed: 7, TenantID: a.TenantID}); err == nil || dup != nil {
		t.Error("a second build over an existing tenant was accepted")
	}
}

// Stage 47.9.1 / 47.18.2: the named small shape has the skew it promises, the
// real engines' invariants hold on it, and its footprint is measured.
func TestSyntheticTenantSmallShape(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a tenant of 60 bills")
	}
	synthTestEnv(t)
	a := synthRun(t, "small", SynthOptions{Shape: "small", Seed: 7, Measure: true})
	t.Logf("small seed 7: %v in %s, checksum %s", a.Counts, a.Elapsed.Round(time.Millisecond), a.Checksum)

	s, _ := SynthShapeNamed("small")
	schema := a.Schema
	one := func(q string, args ...interface{}) int64 {
		t.Helper()
		var n int64
		if err := db.DB.QueryRow(fmt.Sprintf(q, schema), args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if a.Counts["bills"] != int64(s.Bills) {
		t.Errorf("bills = %d, want %d", a.Counts["bills"], s.Bills)
	}
	var sandbox bool
	_ = db.DB.QueryRow(`SELECT is_sandbox FROM public.tenants WHERE tenant_id = $1`, a.TenantID).Scan(&sandbox)
	if !sandbox {
		t.Error("a synthetic tenant must be a sandbox, so nothing is ever sent for it")
	}

	// Hot SKU: the most-sold line by a wide margin.
	lines := one(`SELECT COUNT(*) FROM %s.documents d, jsonb_array_elements(d.data->'items') l WHERE d.doctype = 'POSCart' AND d.status = 'Paid'`)
	hotLines := one(`SELECT COUNT(*) FROM %s.documents d, jsonb_array_elements(d.data->'items') l WHERE d.doctype = 'POSCart' AND d.status = 'Paid' AND l->>'sku' = $1`, a.HotSKU)
	runnerUp := one(`SELECT COALESCE(MAX(n), 0) FROM (SELECT COUNT(*) n FROM %s.documents d, jsonb_array_elements(d.data->'items') l
		WHERE d.doctype = 'POSCart' AND d.status = 'Paid' AND l->>'sku' <> $1 GROUP BY l->>'sku') x`, a.HotSKU)
	if hotLines*100 < lines*20 || hotLines < 3*runnerUp {
		t.Errorf("hot SKU %s: %d of %d lines, next SKU %d - not skewed", a.HotSKU, hotLines, lines, runnerUp)
	}
	// Large orders.
	if got := one(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND jsonb_array_length(data->'items') = $1`, s.LargeOrderLines); got != int64(s.LargeOrders) {
		t.Errorf("large orders = %d, want %d", got, s.LargeOrders)
	}
	// Partial returns: some, each smaller than its bill.
	returns := one(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ReturnRequest'`)
	if returns == 0 {
		t.Error("no partial returns")
	}
	if full := one(`SELECT COUNT(*) FROM %s.documents r JOIN %[1]s.documents c ON c.id = r.data->>'original_order_id' AND c.doctype = 'POSCart'
		WHERE r.doctype = 'ReturnRequest' AND (SELECT SUM((l->>'qty')::int) FROM jsonb_array_elements(c.data->'items') l) <= 1`); full != 0 {
		t.Errorf("%d return(s) took back the whole bill", full)
	}
	// Most bins empty; owners one per warehouse.
	bins := one(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Bin'`)
	stockedBins := one(`SELECT COUNT(DISTINCT bin_code) FROM %s.bin_stock WHERE qty > 0`)
	if bins != int64(s.Warehouses*s.BinsPerWarehouse) || stockedBins == 0 || stockedBins*100 > bins*int64(s.StockedBinPct) {
		t.Errorf("bins %d, stocked %d - want %d bins, at most %d%% stocked", bins, stockedBins, s.Warehouses*s.BinsPerWarehouse, s.StockedBinPct)
	}
	if owners := one(`SELECT COUNT(DISTINCT owner_id) FROM %s.bin_stock_owner`); owners != int64(s.Owners) {
		t.Errorf("owners = %d, want %d", owners, s.Owners)
	}
	if mixed := one(`SELECT COUNT(*) FROM (SELECT location_code FROM %s.bin_stock_owner GROUP BY 1 HAVING COUNT(DISTINCT owner_id) > 1) x`); mixed != 0 {
		t.Errorf("%d warehouse(s) hold two owners", mixed)
	}
	// Long audit history and month-end bursts.
	if got := one(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'ITEM_PRICE_CHANGE' AND details LIKE $1`, "%"+a.HotSKU+"%"); got != int64(s.AuditTrail) {
		t.Errorf("hot SKU audit trail = %d, want %d", got, s.AuditTrail)
	}
	if got := one(`SELECT COUNT(DISTINCT document_id) FROM %s.gl_postings WHERE document_type = 'MonthEndClose'`); got != int64(s.Months*s.MonthEndEntries) {
		t.Errorf("month-end postings = %d, want %d", got, s.Months*s.MonthEndEntries)
	}
	// The invariants the real engines keep.
	if neg := one(`SELECT COUNT(*) FROM %s.inventory_availability WHERE available < 0 OR on_hand < 0`); neg != 0 {
		t.Errorf("%d negative stock row(s)", neg)
	}
	if diff := one(`SELECT COALESCE(SUM(debit) - SUM(credit), 0) FROM %s.gl_postings`); diff != 0 {
		t.Errorf("the ledger is out of balance by %d paise", diff)
	}

	// The footprint measured on the same run (47.18.2).
	kinds := map[string]SynthFootprint{}
	for _, f := range a.Footprint {
		kinds[f.Kind] = f
		t.Logf("footprint %-22s n=%-4d rows=%.1f heap p50=%d p95=%d factor=%.2f", f.Kind, f.Count, f.RowsMean, f.HeapP50, f.HeapP95, f.Factor)
	}
	for _, k := range []string{"sale", "return", "receipt", "audit", "month_end_posting", "master:Item"} {
		if f, ok := kinds[k]; !ok || f.HeapP50 <= 0 || f.HeapP95 < f.HeapP50 || f.Factor < 1 {
			t.Errorf("footprint %s missing or empty: %+v", k, f)
		}
	}
	if kinds["sale"].HeapP50 <= kinds["audit"].HeapP50 {
		t.Errorf("a sale (%d bytes) should outweigh one audit row (%d)", kinds["sale"].HeapP50, kinds["audit"].HeapP50)
	}
}

func TestSyntheticShapesAreConsistent(t *testing.T) {
	for _, name := range SynthShapeNames() {
		s, _ := SynthShapeNamed(name)
		if err := s.check(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if percentileInt([]int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 50) != 5 || percentileInt([]int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 95) != 10 || percentileInt(nil, 50) != 0 {
		t.Error("percentileInt is not nearest-rank")
	}
	if g := synthGSTIN("29", 17); !ValidGSTINChecksum(g) || !gstinPattern.MatchString(g) {
		t.Errorf("synthetic GSTIN %s fails the real rules", g)
	}
}
