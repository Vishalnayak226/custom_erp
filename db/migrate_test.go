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
			// Stage 50/AUD-02: an unnumbered "foundation" file for stage N.M
			// must sort before a numbered N.M.K sub-item of it, even though
			// plain byte comparison of a digit against the sub-item's own
			// leading letter says otherwise (every digit is < every letter
			// in ASCII, so the old rule got this backwards regardless of
			// which file actually depends on the other). This is the exact
			// pair a genuinely fresh `-migrate` failed against
			// ("relation tenant_default.audit_checkpoints does not exist");
			// live-verified end to end against a real empty scratch database
			// after this fix - all 161 embedded migrations applied cleanly,
			// re-running was a correct no-op, `go build`/`go vet` clean - see
			// project_ledger.md for the full session record. Not turned into
			// a permanent CREATE DATABASE integration test here: this
			// package has no existing pattern for owning a scratch
			// database's lifecycle inside `go test`, and inventing one is a
			// separate, larger piece of test infrastructure.
			{"migrations_stage47_7_audit_evidence.sql", "migrations_stage47_7_6_audit_archive.sql"},
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

	t.Run("no migration file contains destructive, non-additive DDL", func(t *testing.T) {
		// Stage 49.7.5: "reviewed checksumed additive migrations" - this
		// codebase's own convention (stated in this file's package doc
		// comment above) is that a migration, once shipped, is never edited
		// or reversed, only added to (CREATE TABLE IF NOT EXISTS, ADD COLUMN
		// IF NOT EXISTS). A DROP of a table/column/schema/database, or a
		// TRUNCATE, destroys data or history a rollback cannot restore and a
		// tenant-scoped restore cannot selectively undo. As of this test, no
		// migration file uses any of these - if one legitimately needs to,
		// add a narrow, named exception here with the reason, the same
		// pattern internal/securityscan/bypass_test.go uses for its own
		// reviewed exceptions, rather than loosening the check itself.
		destructive := []string{"DROP TABLE", "DROP COLUMN", "DROP SCHEMA", "DROP DATABASE", "TRUNCATE"}
		names, err := migrationFileNames()
		if err != nil {
			t.Fatalf("migrationFileNames: %v", err)
		}
		for _, name := range names {
			body, err := migrationFiles.ReadFile(name)
			if err != nil {
				t.Errorf("read %s: %v", name, err)
				continue
			}
			for _, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "--") {
					continue // a comment mentioning the word is not a statement
				}
				upper := strings.ToUpper(trimmed)
				for _, pattern := range destructive {
					if strings.Contains(upper, pattern) {
						t.Errorf("%s contains %q outside a comment - migrations in this codebase must be additive-only (see this test's doc comment for the reviewed-exception pattern): %q", name, pattern, trimmed)
					}
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
