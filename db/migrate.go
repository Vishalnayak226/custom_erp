package db

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// migrationFiles embeds every .sql file in this directory into the binary, so
// `erp-server -migrate` works from a stripped release binary on a machine that
// has no checkout of the repo - the same reason the VERSION file is embedded
// (Stage 14.6-14.8).
//
//go:embed *.sql
var migrationFiles embed.FS

// Stage 30.2.2. Before this, the only thing that ever applied a migration file
// was promote.ps1's Invoke-PendingMigrations, which runs on a dev -> test ->
// live promotion and nowhere else. db/migration.sql is only used to create a
// database from scratch, so any table added to it after a database was first
// provisioned never reached that database - found live as five missing
// integration tables (500s on six endpoints, plus a background worker
// retrying against a nonexistent table every ~8 minutes).
//
// This is deliberately the smallest thing that fixes that: an ordered list, a
// ledger of what has run, one transaction per file. No dependency, no DSL, no
// down-migrations - this repo's migrations are already written to be
// idempotent and additive (CREATE TABLE IF NOT EXISTS / ADD COLUMN IF NOT
// EXISTS), which is a stronger guarantee than a rollback script would give.

// MigrationResult is one file's outcome from ApplyPendingMigrations.
type MigrationResult struct {
	File    string
	Applied bool   // false = already recorded in the ledger, skipped
	Err     error  // non-nil if this file failed; the run stops here
	Detail  string // short human-readable note for the caller to print
}

// ApplyPendingMigrations runs every embedded migration file that the ledger
// (public.schema_migrations) does not already record, in order, and records
// each one as it succeeds. It is safe to run repeatedly: an already-applied
// file is skipped, and a file that is applied is applied inside its own
// transaction, so a failure part-way through leaves that file's changes rolled
// back and its ledger row absent - re-running retries exactly that file.
//
// Ordering is numeric-aware (see compareMigrationNames), so
// migrations_stage26_4_* runs before migrations_stage26_10_*, which plain
// lexicographic sorting gets backwards.
func ApplyPendingMigrations() ([]MigrationResult, error) {
	if DB == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}

	// The ledger table itself lives in migrations_stage14b_versioning.sql, so
	// on a database that predates it (or a brand-new one) it may not exist
	// yet. Creating it up front - with the same definition that file uses -
	// removes the bootstrap ordering problem promote.ps1 works around by
	// swallowing errors.
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			migration_file VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			description TEXT
		)`); err != nil {
		return nil, fmt.Errorf("could not ensure public.schema_migrations exists: %w", err)
	}

	applied, err := appliedMigrations()
	if err != nil {
		return nil, err
	}

	names, err := migrationFileNames()
	if err != nil {
		return nil, err
	}

	results := make([]MigrationResult, 0, len(names))
	for _, name := range names {
		if applied[name] {
			results = append(results, MigrationResult{File: name, Detail: "already applied"})
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			results = append(results, MigrationResult{File: name, Err: err})
			return results, fmt.Errorf("could not read embedded migration %s: %w", name, err)
		}

		tx, err := DB.Begin()
		if err != nil {
			results = append(results, MigrationResult{File: name, Err: err})
			return results, fmt.Errorf("could not begin transaction for %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			results = append(results, MigrationResult{File: name, Err: err})
			// SQLSTATE 22P05 (untranslatable_character) here means one thing in
			// practice: the database is not UTF8, and a migration seeds a
			// character its encoding cannot represent - the ₹ in the Stage 37.1
			// currency seed is the first one to hit it. That is the exact
			// condition EnforceUTF8Encoding already warns about at startup, and
			// migrations stop dead until it is fixed, so the failure says so
			// rather than leaving someone debugging their SQL.
			if strings.Contains(err.Error(), "22P05") || strings.Contains(err.Error(), "has no equivalent in encoding") {
				return results, fmt.Errorf("migration %s failed (rolled back, ledger not updated): %w\n"+
					"       This database's encoding cannot store the character the migration seeds.\n"+
					"       Check `SHOW server_encoding` - if it is not UTF8, every migration from this\n"+
					"       one onward is blocked. Recreate the database with\n"+
					"       CREATE DATABASE ... ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0\n"+
					"       and restore into it. See db.EnforceUTF8Encoding's comment for the full background", name, err)
			}
			return results, fmt.Errorf("migration %s failed (rolled back, ledger not updated): %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO public.schema_migrations (migration_file, description) VALUES ($1, $2)
			 ON CONFLICT (migration_file) DO NOTHING`,
			name, "applied by erp-server -migrate"); err != nil {
			_ = tx.Rollback()
			results = append(results, MigrationResult{File: name, Err: err})
			return results, fmt.Errorf("could not record %s in the ledger (rolled back): %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			results = append(results, MigrationResult{File: name, Err: err})
			return results, fmt.Errorf("could not commit %s: %w", name, err)
		}
		results = append(results, MigrationResult{File: name, Applied: true, Detail: "applied"})
	}
	return results, nil
}

// BaselineMigrations records every embedded migration file as applied WITHOUT
// running any of it, and returns how many rows it added.
//
// This exists for the situation this runner was introduced into: a database
// whose migrations were applied by hand, one psql invocation at a time, long
// before there was a ledger to record them in - so the ledger names 13 of 68
// files even though all 68 have really been applied. Running them all again
// to "catch up" is the wrong answer there: this repo's migrations are
// idempotent in shape (CREATE TABLE IF NOT EXISTS, ON CONFLICT DO NOTHING),
// but db/migration.sql also seeds baseline rows - including the default admin
// credential - and re-seeding those on a database someone has since hardened
// is a genuinely bad outcome.
//
// So: verify the schema really is current, baseline once, and let every
// migration from then on go through ApplyPendingMigrations. On a database
// created after this runner existed, baselining is never needed.
func BaselineMigrations() (int, error) {
	if DB == nil {
		return 0, fmt.Errorf("database connection is not initialized")
	}
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			migration_file VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			description TEXT
		)`); err != nil {
		return 0, fmt.Errorf("could not ensure public.schema_migrations exists: %w", err)
	}
	names, err := migrationFileNames()
	if err != nil {
		return 0, err
	}
	recorded := 0
	for _, name := range names {
		res, err := DB.Exec(
			`INSERT INTO public.schema_migrations (migration_file, description) VALUES ($1, $2)
			 ON CONFLICT (migration_file) DO NOTHING`,
			name, "baselined - assumed already applied before the runner existed")
		if err != nil {
			return recorded, fmt.Errorf("could not baseline %s: %w", name, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			recorded++
		}
	}
	return recorded, nil
}

// PendingMigrations lists the migration files that ApplyPendingMigrations
// would run, without running them - what a "is this database up to date?"
// check needs.
func PendingMigrations() ([]string, error) {
	applied, err := appliedMigrations()
	if err != nil {
		return nil, err
	}
	names, err := migrationFileNames()
	if err != nil {
		return nil, err
	}
	var pending []string
	for _, name := range names {
		if !applied[name] {
			pending = append(pending, name)
		}
	}
	return pending, nil
}

func appliedMigrations() (map[string]bool, error) {
	applied := map[string]bool{}
	rows, err := DB.Query(`SELECT migration_file FROM public.schema_migrations`)
	if err != nil {
		// A database that has never had the ledger created yet simply has
		// nothing applied on record - not an error worth failing the caller
		// over, since ApplyPendingMigrations creates the table before it asks.
		return applied, nil
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

func migrationFileNames() ([]string, error) {
	entries, err := migrationFiles.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Slice(names, func(i, j int) bool { return compareMigrationNames(names[i], names[j]) < 0 })
	return names, nil
}

// compareMigrationNames orders migration filenames the way a human reads them:
// digit runs compare numerically, everything else compares as text. Plain
// lexicographic order puts migrations_stage26_10_1_stock_ledger.sql before
// migrations_stage26_4_pim_maturity.sql, and migrations_stage9_*.sql after
// migrations_stage30_*.sql - both wrong.
//
// db/migration.sql sorts first regardless (it is the base schema every other
// file builds on), which happens to fall out of the rules above anyway since
// "migration." < "migrations" - it is asserted by the runner's test rather
// than relied on by accident.
func compareMigrationNames(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			// Leading zeros are irrelevant to the value, so parse rather than
			// compare the digit runs as text.
			na, _ := strconv.Atoi(a[si:i])
			nb, _ := strconv.Atoi(b[sj:j])
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if a[i] != b[j] {
			if a[i] < b[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(a)-i < len(b)-j:
		return -1
	case len(a)-i > len(b)-j:
		return 1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
