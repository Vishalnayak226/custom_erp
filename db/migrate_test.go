package db

import (
	"sort"
	"strings"
	"testing"
)

// TestMigrationOrdering covers Stage 30.2.2's ordering rule. These are pure
// string comparisons - no database needed, so this test runs in CI even when
// Postgres isn't reachable.
func TestMigrationOrdering(t *testing.T) {
	t.Run("digit runs compare numerically, not as text", func(t *testing.T) {
		for _, tc := range []struct{ a, b string }{
			// The two orderings plain lexicographic sorting gets backwards.
			{"migrations_stage26_4_pim_maturity.sql", "migrations_stage26_10_1_stock_ledger.sql"},
			{"migrations_stage9_x.sql", "migrations_stage30_1_2_item_tax_mandatory.sql"},
			// And the ones it happens to get right, which must not regress.
			{"migration.sql", "migrations_phase3.sql"},
			{"migrations_stage14a_modules.sql", "migrations_stage14b_versioning.sql"},
			{"migrations_stage29_8_status_transition_map.sql", "migrations_stage30_1_2_item_tax_mandatory.sql"},
		} {
			if got := compareMigrationNames(tc.a, tc.b); got >= 0 {
				t.Errorf("expected %q to sort before %q, got %d", tc.a, tc.b, got)
			}
			if got := compareMigrationNames(tc.b, tc.a); got <= 0 {
				t.Errorf("expected %q to sort after %q, got %d", tc.b, tc.a, got)
			}
		}
	})

	t.Run("comparison is a consistent total order", func(t *testing.T) {
		names, err := migrationFileNames()
		if err != nil {
			t.Fatalf("migrationFileNames: %v", err)
		}
		if len(names) < 10 {
			t.Fatalf("expected the db/*.sql files to be embedded, got %d", len(names))
		}
		if compareMigrationNames("migrations_stage17_soft_delete.sql", "migrations_stage17_soft_delete.sql") != 0 {
			t.Fatalf("a name must compare equal to itself")
		}
		if !sort.SliceIsSorted(names, func(i, j int) bool { return compareMigrationNames(names[i], names[j]) < 0 }) {
			t.Fatalf("migrationFileNames returned an unsorted list")
		}
	})

	t.Run("the base schema always runs first", func(t *testing.T) {
		names, err := migrationFileNames()
		if err != nil {
			t.Fatalf("migrationFileNames: %v", err)
		}
		if names[0] != "migration.sql" {
			t.Fatalf("first migration is %q, want migration.sql - every other file builds on it", names[0])
		}
	})

	t.Run("every embedded file is a .sql file and is readable", func(t *testing.T) {
		names, err := migrationFileNames()
		if err != nil {
			t.Fatalf("migrationFileNames: %v", err)
		}
		for _, name := range names {
			if !strings.HasSuffix(name, ".sql") {
				t.Errorf("non-SQL file embedded as a migration: %s", name)
			}
			body, err := migrationFiles.ReadFile(name)
			if err != nil {
				t.Errorf("embedded migration %s is unreadable: %v", name, err)
				continue
			}
			if len(body) == 0 {
				t.Errorf("embedded migration %s is empty", name)
			}
			// Each file is executed as one statement batch inside a single
			// transaction, so a psql meta-command (\c, \i) or a statement
			// Postgres refuses to run transactionally would fail the whole
			// run. Catch that here rather than during a production promotion.
			for _, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, `\`) {
					t.Errorf("%s contains a psql meta-command (%q) - the Go runner executes files with database/sql, which cannot run those", name, trimmed)
				}
				if strings.Contains(strings.ToUpper(trimmed), "CONCURRENTLY") && !strings.HasPrefix(trimmed, "--") {
					t.Errorf("%s uses CONCURRENTLY, which cannot run inside the runner's per-file transaction", name)
				}
			}
		}
	})

	t.Run("the five tables Stage 30.2.2 found missing are shipped as a migration", func(t *testing.T) {
		body, err := migrationFiles.ReadFile("migrations_stage30_2_2_integration_tables_catchup.sql")
		if err != nil {
			t.Fatalf("catch-up migration is missing: %v", err)
		}
		for _, table := range []string{
			"pinelabs_credentials", "pinelabs_transactions",
			"unicommerce_credentials", "unicommerce_inventory_sync", "unicommerce_order_mapping",
		} {
			if !strings.Contains(string(body), table) {
				t.Errorf("catch-up migration does not create %s", table)
			}
		}
	})
}
