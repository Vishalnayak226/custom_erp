package db

import (
	"testing"
)

// Stage 49.7.5 tests for the migration ledger checksum. These need a live
// Postgres (unlike the rest of migrate_test.go), so they live in their own
// file with their own doc comment rather than perturbing that file's
// documented "no database needed" convention.
func TestVerifyMigrationChecksums(t *testing.T) {
	InitDB(ConnStringFromEnv())

	t.Run("a historic NULL-checksum row is backfilled from the currently embedded file, not reported as drift", func(t *testing.T) {
		if _, err := DB.Exec(`UPDATE public.schema_migrations SET checksum = NULL WHERE migration_file = 'migration.sql'`); err != nil {
			t.Fatalf("force a NULL checksum: %v", err)
		}

		findings, err := VerifyMigrationChecksums()
		if err != nil {
			t.Fatalf("VerifyMigrationChecksums: %v", err)
		}
		for _, f := range findings {
			if f.File == "migration.sql" {
				t.Fatalf("expected a NULL checksum to be silently backfilled, not reported as drift: %+v", f)
			}
		}

		var checksum *string
		if err := DB.QueryRow(`SELECT checksum FROM public.schema_migrations WHERE migration_file = 'migration.sql'`).Scan(&checksum); err != nil {
			t.Fatalf("reload checksum: %v", err)
		}
		if checksum == nil || *checksum == "" {
			t.Fatalf("expected VerifyMigrationChecksums to have backfilled migration.sql's checksum, still NULL")
		}
		body, err := migrationFiles.ReadFile("migration.sql")
		if err != nil {
			t.Fatalf("read embedded migration.sql: %v", err)
		}
		if *checksum != checksumOf(body) {
			t.Fatalf("backfilled checksum does not match the currently embedded file's own hash")
		}
	})

	t.Run("a row whose recorded checksum no longer matches the embedded file is reported as drift", func(t *testing.T) {
		const target = "migrations_stage47_1_role_templates.sql"
		var original string
		if err := DB.QueryRow(`SELECT checksum FROM public.schema_migrations WHERE migration_file = $1`, target).Scan(&original); err != nil {
			t.Fatalf("read original checksum for %s: %v (was it applied to this dev DB yet?)", target, err)
		}
		defer func() {
			if _, err := DB.Exec(`UPDATE public.schema_migrations SET checksum = $1 WHERE migration_file = $2`, original, target); err != nil {
				t.Errorf("failed to restore %s's real checksum after the test - the shared dev ledger may now be left tampered: %v", target, err)
			}
		}()

		if _, err := DB.Exec(`UPDATE public.schema_migrations SET checksum = 'deadbeef0000000000000000000000000000000000000000000000000000' WHERE migration_file = $1`, target); err != nil {
			t.Fatalf("tamper with checksum: %v", err)
		}

		findings, err := VerifyMigrationChecksums()
		if err != nil {
			t.Fatalf("VerifyMigrationChecksums: %v", err)
		}
		found := false
		for _, f := range findings {
			if f.File == target {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a tampered checksum for %s to be reported as drift, got %+v", target, findings)
		}
	})

	t.Run("a ledger row naming a file this binary no longer embeds is reported", func(t *testing.T) {
		const fake = "migrations_stage00_0_nonexistent_test_only.sql"
		if _, err := DB.Exec(
			`INSERT INTO public.schema_migrations (migration_file, description, checksum) VALUES ($1, 'test-only row', 'aaaa0000000000000000000000000000000000000000000000000000000000')`,
			fake); err != nil {
			t.Fatalf("seed fake ledger row: %v", err)
		}
		defer DB.Exec(`DELETE FROM public.schema_migrations WHERE migration_file = $1`, fake)

		findings, err := VerifyMigrationChecksums()
		if err != nil {
			t.Fatalf("VerifyMigrationChecksums: %v", err)
		}
		found := false
		for _, f := range findings {
			if f.File == fake {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a ledger row naming a file no longer embedded to be reported, got %+v", findings)
		}
	})
}
