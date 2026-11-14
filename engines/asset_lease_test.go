package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Ask 170.5: leases (IND AS 116).

const leaseTestSchema = "tenant_default"

func cleanupLeaseTest() {
	db.InitDB(testConnStr())
	db.DB.Exec("DELETE FROM " + leaseTestSchema + ".gl_postings WHERE document_type IN ('Lease', 'Asset') AND document_id LIKE 'T170L-%'")
	db.DB.Exec("DELETE FROM " + leaseTestSchema + ".documents WHERE id LIKE 'T170L-%'")
	db.DB.Exec("DELETE FROM " + leaseTestSchema + ".accounting_periods WHERE period_name LIKE 'T170L%'")
}

func leaseGL(t *testing.T, id string) map[string][2]int64 {
	t.Helper()
	rows, err := db.DB.Query("SELECT account_code, SUM(debit), SUM(credit) FROM "+leaseTestSchema+".gl_postings WHERE document_type = 'Lease' AND document_id = $1 GROUP BY account_code", id)
	if err != nil {
		t.Fatalf("gl: %v", err)
	}
	defer rows.Close()
	out := map[string][2]int64{}
	for rows.Next() {
		var code string
		var d, c int64
		rows.Scan(&code, &d, &c)
		out[code] = [2]int64{d, c}
	}
	return out
}

func leasePostingCount(t *testing.T, id string) int {
	t.Helper()
	var n int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+leaseTestSchema+".gl_postings WHERE document_type = 'Lease' AND document_id = $1", id).Scan(&n)
	return n
}

func lessee(t *testing.T, id string, term int, pay, rate float64, timing string) {
	t.Helper()
	insertTestDoc(t, leaseTestSchema, id, "Lease", map[string]interface{}{"code": id, "lease_type": "Lessee", "vendor": "V1",
		"term_months": term, "payment_amount": pay, "discount_rate": rate, "payment_timing": timing, "start_date": "2026-01-01"})
	db.DB.Exec("UPDATE " + leaseTestSchema + ".documents SET status = 'Draft' WHERE id = '" + id + "'")
}

func TestLeaseScheduleMath(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, advance := range []bool{false, true} {
		pv, rows := BuildLeaseSchedule(12, 1000000, 12, advance, start)
		if len(rows) != 12 || pv <= 0 {
			t.Fatalf("advance=%v: pv %d rows %d", advance, pv, len(rows))
		}
		var pay, interest, dep int64
		for _, r := range rows {
			pay += r.Payment
			interest += r.Interest
			dep += r.Depreciation
		}
		if rows[11].Closing != 0 {
			t.Fatalf("advance=%v: liability not cleared: %d", advance, rows[11].Closing)
		}
		if pay != pv+interest {
			t.Fatalf("advance=%v: payments %d != pv %d + interest %d", advance, pay, pv, interest)
		}
		if dep != pv {
			t.Fatalf("advance=%v: depreciation %d != pv %d", advance, dep, pv)
		}
	}
	pvA, _ := BuildLeaseSchedule(12, 1000000, 12, false, start)
	pvB, _ := BuildLeaseSchedule(12, 1000000, 12, true, start)
	if pvB <= pvA {
		t.Fatalf("paying in advance must be worth more: %d vs %d", pvB, pvA)
	}
	if pv0, _ := BuildLeaseSchedule(12, 1000000, 0, false, start); pv0 != 12000000 {
		t.Fatalf("zero rate pv = %d", pv0)
	}
}

func TestLeaseLesseeActivateAndMonthlyPostingsAreIdempotent(t *testing.T) {
	cleanupLeaseTest()
	t.Cleanup(cleanupLeaseTest)
	lessee(t, "T170L-1", 3, 10000, 12, "Arrears")
	if _, err := ActivateLease("default", "T170L-1", "t"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	gl := leaseGL(t, "T170L-1")
	pv := gl["1420"][0]
	if pv <= 0 || gl["2450"][1] != pv {
		t.Fatalf("opening entry must be Dr ROU / Cr liability at PV: %v", gl)
	}
	if _, err := ActivateLease("default", "T170L-1", "t"); err == nil {
		t.Fatal("a second activation must be refused")
	}
	if leasePostingCount(t, "T170L-1") != 2 {
		t.Fatalf("activation posted twice")
	}
	// Mid February: only January has ended.
	if n, err := RunLeasePostings("default", leaseTestSchema, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)); err != nil || n < 1 {
		t.Fatalf("run 1: %d %v", n, err)
	}
	after1 := leasePostingCount(t, "T170L-1")
	if n, _ := RunLeasePostings("default", leaseTestSchema, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)); n != 0 {
		t.Fatalf("a replay posted %d periods", n)
	}
	if leasePostingCount(t, "T170L-1") != after1 {
		t.Fatal("a replay added postings")
	}
	RunLeasePostings("default", leaseTestSchema, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	gl = leaseGL(t, "T170L-1")
	if gl["2450"][0] != gl["2450"][1] {
		t.Fatalf("liability must be nought at the end: %v", gl["2450"])
	}
	if gl["1420"][0] != gl["1420"][1] {
		t.Fatalf("ROU must be fully depreciated: %v", gl["1420"])
	}
	if gl["5311"][0] != pv {
		t.Fatalf("depreciation %d != pv %d", gl["5311"][0], pv)
	}
	if gl["2100"][1] != 3*RupeesToPaise(10000) {
		t.Fatalf("payments credited to 2100 = %d", gl["2100"][1])
	}
	_, status, _ := fetchDocData("default", "Lease", "T170L-1")
	if status != "Closed" {
		t.Fatalf("status = %s", status)
	}
}

func TestLeasePostingWaitsOutAClosedMonth(t *testing.T) {
	cleanupLeaseTest()
	t.Cleanup(cleanupLeaseTest)
	lessee(t, "T170L-2", 2, 5000, 10, "Arrears")
	if _, err := ActivateLease("default", "T170L-2", "t"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO " + leaseTestSchema + ".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ('T170L Jan 2026', '2026-01-01', '2026-01-31', 'Closed', 'test')"); err != nil {
		t.Fatalf("period: %v", err)
	}
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	RunLeasePostings("default", leaseTestSchema, now)
	if leasePostingCount(t, "T170L-2") != 2 {
		t.Fatalf("a closed month must post nothing, got %d rows", leasePostingCount(t, "T170L-2"))
	}
	db.DB.Exec("UPDATE " + leaseTestSchema + ".accounting_periods SET status = 'Open' WHERE period_name = 'T170L Jan 2026'")
	if n, err := RunLeasePostings("default", leaseTestSchema, now); err != nil || n != 2 {
		t.Fatalf("after reopening: %d %v", n, err)
	}
}

func TestLeaseLessorPostsRentAndRefusesASecondLease(t *testing.T) {
	cleanupLeaseTest()
	t.Cleanup(cleanupLeaseTest)
	capitalisedTestAsset(t, leaseTestSchema, "T170L-A1", 120000, 5, "2025-01-01")
	for _, id := range []string{"T170L-3", "T170L-4"} {
		insertTestDoc(t, leaseTestSchema, id, "Lease", map[string]interface{}{"code": id, "lease_type": "Lessor", "customer": "C1", "asset": "T170L-A1",
			"term_months": 2, "payment_amount": 3000, "discount_rate": 0, "start_date": "2026-01-01"})
		db.DB.Exec("UPDATE " + leaseTestSchema + ".documents SET status = 'Draft' WHERE id = '" + id + "'")
	}
	if _, err := ActivateLease("default", "T170L-3", "t"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := ActivateLease("default", "T170L-4", "t"); err == nil {
		t.Fatal("a second Active lessor lease on one asset must be refused")
	}
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	RunLeasePostings("default", leaseTestSchema, now)
	RunLeasePostings("default", leaseTestSchema, now)
	gl := leaseGL(t, "T170L-3")
	if gl["4310"][1] != 2*RupeesToPaise(3000) || gl["1300"][0] != gl["4310"][1] {
		t.Fatalf("rent: %v", gl)
	}
}
