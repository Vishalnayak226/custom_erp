package engines

import (
	"errors"
	"testing"

	"custom_erp/db"
)

// Stage 64.2: a background worker's failure reaches the client's
// system_error_logs, once per hour per distinct failure.
func TestLogBackgroundJobErrorRecordsOncePerHour(t *testing.T) {
	db.InitDB(testConnStr())
	const module = "TEST_BG_642"
	clean := func() { db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE module_source = $1`, module) }
	clean()
	t.Cleanup(clean)

	count := func() int {
		var n int
		if err := db.DB.QueryRow(`SELECT count(*) FROM tenant_default.system_error_logs WHERE module_source = $1`, module).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	failure := errors.New(`relation "pinelabs_transactions" does not exist`)
	LogBackgroundJobError(module, "Reconciliation", "tenant_default", failure)
	LogBackgroundJobError(module, "Reconciliation", "tenant_default", failure)
	if n := count(); n != 1 {
		t.Fatalf("same failure twice: %d rows, want 1", n)
	}
	LogBackgroundJobError(module, "Reconciliation", "tenant_default", errors.New("a different failure"))
	if n := count(); n != 2 {
		t.Fatalf("a different failure: %d rows, want 2", n)
	}
}
