package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

func TestAccountingPeriodClosesBlockPostingsAndOpenPeriodsStillWork(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".accounting_periods WHERE period_name LIKE 'TEST-PERIOD-%'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'TestPeriodDoc'")
	}
	cleanup()
	defer cleanup()

	today := time.Now().UTC()
	start := today.AddDate(0, 0, -3).Format("2006-01-02")
	end := today.AddDate(0, 0, 3).Format("2006-01-02")

	periodID, err := CreateAccountingPeriod(tenantID, "TEST-PERIOD-CLOSED", start, end, "system")
	if err != nil {
		t.Fatalf("CreateAccountingPeriod: %v", err)
	}

	// Overlapping range must be rejected.
	if _, err := CreateAccountingPeriod(tenantID, "TEST-PERIOD-OVERLAP", start, end, "system"); err == nil {
		t.Fatalf("expected overlap rejection, got nil error")
	}

	// While the period covering today is still Open, posting must succeed.
	if err := PostDoubleEntry(tenantID, "TestPeriodDoc", "OPEN-CHECK", map[string]int{"1100": 100}, map[string]int{"4100": 100}, "", ""); err != nil {
		t.Fatalf("expected posting to succeed while period is Open: %v", err)
	}

	if err := CloseAccountingPeriod(tenantID, periodID, "system"); err != nil {
		t.Fatalf("CloseAccountingPeriod: %v", err)
	}

	// A second close attempt must fail - one-way transition, no double-close.
	if err := CloseAccountingPeriod(tenantID, periodID, "system"); err == nil {
		t.Fatalf("expected second close to be rejected")
	}

	// Now that the period covering today is Closed, posting must be rejected.
	if err := PostDoubleEntry(tenantID, "TestPeriodDoc", "CLOSED-CHECK", map[string]int{"1100": 100}, map[string]int{"4100": 100}, "", ""); err == nil {
		t.Fatalf("expected posting to be rejected while period is Closed")
	}

	// 24.6: an explicit transactionDate falling inside the closed period
	// must be rejected too, even though "today" (CURRENT_DATE) is the same
	// date in this test - this exercises the $1::date branch specifically,
	// not just the CURRENT_DATE default every other call in this codebase
	// still uses.
	if err := PostDoubleEntry(tenantID, "TestPeriodDoc", "BACKDATED-CHECK", map[string]int{"1100": 100}, map[string]int{"4100": 100}, today.Format("2006-01-02"), ""); err == nil {
		t.Fatalf("expected posting with an explicit transactionDate inside the closed period to be rejected")
	}
	// And a transactionDate outside the closed period must still be allowed
	// even though a closed period exists at all.
	farFuture := today.AddDate(1, 0, 0).Format("2006-01-02")
	if err := PostDoubleEntry(tenantID, "TestPeriodDoc", "FUTURE-DATE-CHECK", map[string]int{"1100": 100}, map[string]int{"4100": 100}, farFuture, ""); err != nil {
		t.Fatalf("expected posting with a transactionDate outside the closed period to succeed: %v", err)
	}

	periods, err := ListAccountingPeriods(tenantID)
	if err != nil {
		t.Fatalf("ListAccountingPeriods: %v", err)
	}
	found := false
	for _, p := range periods {
		if p.ID == periodID {
			found = true
			if p.Status != "Closed" {
				t.Fatalf("expected listed period status Closed, got %s", p.Status)
			}
			if p.ClosedBy == nil || *p.ClosedBy != "system" {
				t.Fatalf("expected closed_by 'system', got %v", p.ClosedBy)
			}
		}
	}
	if !found {
		t.Fatalf("created period not found in list")
	}
}
