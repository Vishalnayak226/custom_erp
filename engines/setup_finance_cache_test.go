package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 164.2: the setup path's Finance checks are kept per client for a
// short time; a go-live record, a currency or bank account drops them at once,
// and the TTL bounds anything else.
func TestSetupFinanceChecksAreCachedAndInvalidated(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	defer InvalidateSetupFinanceChecks(schema)
	InvalidateSetupFinanceChecks(schema)

	first, err := cachedSetupFinanceChecks(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first["entity"]) == 0 || len(first["parties"]) == 0 {
		t.Fatalf("the cache holds no finance checks: %+v", first)
	}
	// A second read inside the TTL is the same copy, without running the lines.
	second, _ := cachedSetupFinanceChecks(tenantID)
	setupFinanceCacheMu.Lock()
	e := setupFinanceCache[schema]
	setupFinanceCacheMu.Unlock()
	if &second["entity"][0] != &first["entity"][0] || e.loadedAt.IsZero() {
		t.Fatalf("a read inside the TTL ran the lines again")
	}

	// Staleness: a currency saved through the record path shows at once.
	passed := func(id string) bool {
		m, err := cachedSetupFinanceChecks(tenantID)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range m["entity"] {
			if c.ID == "finance-"+id {
				return c.Passed
			}
		}
		t.Fatalf("no finance-%s check", id)
		return false
	}
	if _, err := db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Currency'"); err != nil {
		t.Fatal(err)
	}
	InvalidateSetupFinanceChecks(schema)
	if passed("fin-currency") {
		t.Fatal("no active currency yet, the check should fail")
	}
	if _, err := db.DB.Exec("INSERT INTO " + schema + `.documents (id, doctype, data, status, created_by) VALUES ('CUR-INR', 'Currency', '{"code":"INR"}', 'Active', 'system')`); err != nil {
		t.Fatal(err)
	}
	if passed("fin-currency") {
		t.Fatal("a change with no invalidation should still read the cached copy inside the TTL")
	}
	InvalidateSetupFinanceChecks(schema)
	if !passed("fin-currency") {
		t.Fatal("after invalidation the new currency must show")
	}

	// The TTL is the backstop: an entry older than it is rebuilt.
	if _, err := db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'CUR-INR'"); err != nil {
		t.Fatal(err)
	}
	setupFinanceCacheMu.Lock()
	e = setupFinanceCache[schema]
	e.loadedAt = time.Now().Add(-2 * setupFinanceCacheTTL)
	setupFinanceCache[schema] = e
	setupFinanceCacheMu.Unlock()
	if passed("fin-currency") {
		t.Fatal("an entry past its TTL must be rebuilt from the live books")
	}

	// A go-live record drops the client's copy.
	if err := insertGoLiveRecord(schema, "Evidence", "finance", "fin-opening", "n", "", "", map[string]string{}, "tester"); err != nil {
		t.Fatal(err)
	}
	setupFinanceCacheMu.Lock()
	_, still := setupFinanceCache[schema]
	setupFinanceCacheMu.Unlock()
	if still {
		t.Fatal("a go-live record must drop the cached checks")
	}
}
