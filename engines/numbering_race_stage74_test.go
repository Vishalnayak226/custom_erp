package engines

import (
	"custom_erp/db"
	"sort"
	"sync"
	"testing"
)

// TestStage74_1_2FirstNumbersAtOnce is the numbering race (74.1.2): the first
// numbers of a new store / year bucket taken at the same moment all succeed,
// each a different number.
func TestStage74_1_2FirstNumbersAtOnce(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID, docType, store = "default", "TEST74RACE", "T74"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() { db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE doc_type = $1", docType) }
	cleanup()
	defer cleanup()

	const n = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	var got []string
	var errs []error
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := GenerateSequence(tenantID, docType, store, "2026-27")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			got = append(got, id)
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("%d of %d first numbers failed: %v", len(errs), n, errs[0])
	}
	sort.Strings(got)
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Fatalf("number %s issued twice", got[i])
		}
	}
	var current int
	db.DB.QueryRow("SELECT current_val FROM "+schema+".sequence_counters WHERE doc_type = $1", docType).Scan(&current)
	if current != n {
		t.Fatalf("counter = %d, want %d", current, n)
	}
}
