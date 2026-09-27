package engines

import (
	"custom_erp/db"
	"testing"
)

// TestBLD020LeapDayAccountingPeriodBoundary closes the leap-day/timezone
// clause of BLD-020 (MC-030, previously OPEN, zero prior test coverage -
// confirmed by grep before writing this). India does not observe DST, so
// this codebase's real timezone risk is UTC-vs-IST comparison correctness
// (see loyalty_redemption_security.go's fix, same session), not a genuine
// DST transition; the leap-day risk is specifically accounting-period date
// arithmetic around Feb 29, which this test exercises against real leap
// (2024) and non-leap (2023) calendar years rather than assuming Postgres's
// native DATE type gets it right.
func TestBLD020LeapDayAccountingPeriodBoundary(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".accounting_periods WHERE period_name LIKE 'TEST-BLD020-LEAP-%'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'TestBLD020LeapDoc'")
	}
	cleanup()
	defer cleanup()

	// 2024 is a real leap year: Feb has 29 days. A period spanning the leap
	// day itself must accept a transaction dated exactly on it while Open,
	// and refuse one dated on it once Closed.
	periodID, err := CreateAccountingPeriod(tenantID, "TEST-BLD020-LEAP-2024", "2024-02-25", "2024-02-29", "system")
	if err != nil {
		t.Fatalf("CreateAccountingPeriod (leap year period): %v", err)
	}
	if err := PostDoubleEntry(tenantID, "TestBLD020LeapDoc", "LEAP-DAY-POST",
		map[string]int64{"1100": 100}, map[string]int64{"4100": 100}, "2024-02-29", "TESTBLD020:LEAP-DAY-POST"); err != nil {
		t.Fatalf("counterexample: posting dated exactly on the leap day (2024-02-29) inside its own open period was refused: %v", err)
	}
	if err := CloseAccountingPeriod(tenantID, periodID, "system"); err != nil {
		t.Fatalf("CloseAccountingPeriod: %v", err)
	}
	if err := PostDoubleEntry(tenantID, "TestBLD020LeapDoc", "LEAP-DAY-POST-CLOSED",
		map[string]int64{"1100": 100}, map[string]int64{"4100": 100}, "2024-02-29", "TESTBLD020:LEAP-DAY-POST-CLOSED"); err == nil {
		t.Fatalf("counterexample: posting dated on the leap day inside a now-CLOSED period was accepted")
	}

	// 2023 is a real non-leap year: Feb has only 28 days. A period ending
	// "2023-02-28" must not be treated as if it covered a nonexistent
	// "2023-02-29", and a transaction dated 2023-03-01 - one day past a
	// CLOSED period - must fall outside it and be allowed through (no period
	// covers it => not blocked), proving the boundary lands exactly at
	// 02-28, not off by one into a leap-only date.
	period2, err := CreateAccountingPeriod(tenantID, "TEST-BLD020-LEAP-2023", "2023-02-25", "2023-02-28", "system")
	if err != nil {
		t.Fatalf("CreateAccountingPeriod (non-leap year period): %v", err)
	}
	if err := CloseAccountingPeriod(tenantID, period2, "system"); err != nil {
		t.Fatalf("CloseAccountingPeriod (non-leap): %v", err)
	}
	if err := PostDoubleEntry(tenantID, "TestBLD020LeapDoc", "NON-LEAP-BOUNDARY",
		map[string]int64{"1100": 100}, map[string]int64{"4100": 100}, "2023-03-01", "TESTBLD020:NON-LEAP-BOUNDARY"); err != nil {
		t.Fatalf("counterexample: a transaction dated the day after a closed non-leap-year period (2023-03-01, outside 2023-02-25..28) was refused: %v", err)
	}
}
