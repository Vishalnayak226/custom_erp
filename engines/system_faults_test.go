package engines

import (
	"testing"

	"custom_erp/db"
)

// TestSystemFaultSQLLeavesOutRejections covers Stage 104.2: the Control
// Panel's Errors (24h) and the error-rate alert count a worker error, a panic
// or a 5xx answer, but not a request the user was told to correct.
func TestSystemFaultSQLLeavesOutRejections(t *testing.T) {
	db.InitDB(testConnStr())
	const marker = "STAGE104_FAULT_FILTER_TEST"
	defer db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE module_source = $1`, marker)
	for _, row := range [][2]string{
		{"Medium", "[GLOBAL-0001] Required value is missing. | POST /api/v1/doc/PurchaseOrder/PO-1"}, // rejection
		{"High", "[GOODSR-0096] Enter a damage reason for each damaged receipt line."},               // rejection, High
		{"Critical", "[GLOBAL-0302] Unexpected error occurred."},                                     // 5xx fault
		{"ERROR", "einvoice worker: provider timed out"},                                             // worker fault
		{"PANIC", "runtime error: index out of range"},                                               // panic
	} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.system_error_logs (severity, module_source, error_message) VALUES ($1, $2, $3)`, row[0], marker, row[1]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	count := func() int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.system_error_logs WHERE module_source = $1 AND `+systemFaultSQL("$2"), marker, clientErrorCodesArg()).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	old := ClientErrorCodes
	defer func() { ClientErrorCodes = old }()

	ClientErrorCodes = nil
	if n := count(); n != 5 {
		t.Fatalf("with no code list every row counts, as before: want 5, got %d", n)
	}
	ClientErrorCodes = func() []string { return []string{"GLOBAL-0001", "GOODSR-0096"} }
	if n := count(); n != 3 {
		t.Fatalf("want the 3 faults, not the 2 rejections: got %d", n)
	}
}
