package engines

import (
	"testing"
	"time"

	"custom_erp/db"
)

// TestPatchIntakeQueuesFaultsNotRefusals covers 206.4: the daily fix queue
// took only ERROR/PANIC rows, so a 500 (GLOBAL-0302, logged "Critical") never
// reached it. It now takes every fault by the System Errors filter, and still
// no refusal the user can correct.
func TestPatchIntakeQueuesFaultsNotRefusals(t *testing.T) {
	db.InitDB(testConnStr())
	saved := ClientErrorCodes
	ClientErrorCodes = func() []string { return []string{"GLOBAL-0009"} }
	defer func() { ClientErrorCodes = saved }()

	var marker string
	if err := db.DB.QueryRow(`SELECT 'TEST206-' || left(gen_random_uuid()::text, 8)`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		db.DB.Exec(`DELETE FROM tenant_default.system_error_logs WHERE module_source = $1`, marker)
		db.DB.Exec(`DELETE FROM public.patch_proposals WHERE module_source = $1`, marker)
	}
	cleanup()
	defer cleanup()

	// A narrow window: intake reads every row since, so an older test's rows
	// would otherwise become proposals nobody cleans up.
	since := time.Now().Add(-2 * time.Second)
	fault := "[GLOBAL-0302] boom | GET /api/v1/test206"
	for _, msg := range []string{fault, fault, "[GLOBAL-0009] Session expired | GET /api/v1/test206"} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.system_error_logs (severity, module_source, error_message) VALUES ('Critical', $1, $2)`, marker, msg); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := intakeSchemaErrors("tenant_default", "default", since, nil); err != nil {
		t.Fatalf("intake: %v", err)
	}
	rows, err := db.DB.Query(`SELECT signature, occurrence_count FROM public.patch_proposals WHERE module_source = $1`, marker)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var sig string
		var n int
		if err := rows.Scan(&sig, &n); err != nil {
			t.Fatal(err)
		}
		got[sig] = n
	}
	if len(got) != 1 || got[fault] != 2 {
		t.Fatalf("want one proposal for the Critical fault (2 occurrences) and none for the refusal, got %v", got)
	}
}
