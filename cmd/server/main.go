// Command server is the ERP's actual entrypoint. All request handling,
// middleware, and route registration live in internal/server - this file is
// deliberately just a launcher (Stage 19 folder restructuring, 2026-07-19),
// plus the one-shot maintenance modes below that need a database but not a
// running HTTP server.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
	"custom_erp/internal/server"
)

func main() {
	// Stage 30.2.2: db/migration.sql only ever builds a database from
	// scratch, so anything added to it afterwards never reached an existing
	// one - five integration tables were missing in exactly that way, causing
	// 500s on six endpoints. These two flags are the supported way to close
	// that drift; see db/migrate.go.
	migrate := flag.Bool("migrate", false, "apply pending database migrations, then exit")
	migrateStatus := flag.Bool("migrate-status", false, "list migrations not yet applied to this database, then exit")
	migrateBaseline := flag.Bool("migrate-baseline", false, "record all migrations as applied WITHOUT running them (for a database whose migrations were applied by hand before this runner existed), then exit")
	// Stage 90: remote_deploy.sh runs this once the new build is healthy, so
	// the Control Panel's Releases table and every client's version follow
	// the release. -version/-commit/-at record an earlier build instead
	// (a backfill), which leaves the clients alone.
	recordRelease := flag.Bool("record-release", false, "record this build in the release ledger (public.deployments) and stamp every unpinned client with its version, then exit")
	releaseBy := flag.String("by", "", "with -record-release: who released it")
	releaseNotes := flag.String("notes", "", "with -record-release: a note for the ledger")
	releaseVersion := flag.String("version", "", "with -record-release: record this version instead of the running build's (backfill)")
	releaseCommit := flag.String("commit", "", "with -record-release: record this commit instead of the running build's (backfill)")
	releaseAt := flag.String("at", "", "with -record-release: when it was released, RFC 3339 (backfill; default now)")
	flag.Parse()

	if *recordRelease {
		db.InitDB(db.ConnStringFromEnv())
		version, commit, _ := server.BuildIdentity()
		backfill := *releaseVersion != "" || *releaseCommit != ""
		if *releaseVersion != "" {
			version = *releaseVersion
		}
		if *releaseCommit != "" {
			commit = *releaseCommit
		}
		rec := engines.ReleaseRecord{Environment: os.Getenv("ENV"), Version: version, Commit: commit,
			By: *releaseBy, Notes: *releaseNotes, StampClients: !backfill}
		if *releaseAt != "" {
			at, err := time.Parse(time.RFC3339, *releaseAt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "-at must be RFC 3339: %v\n", err)
				os.Exit(1)
			}
			rec.At = at
		}
		stamped, err := engines.RecordRelease(rec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "release not recorded: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Release %s (%s) recorded; %d client(s) now show it.\n", version, commit, stamped)
		return
	}

	if *migrate || *migrateStatus || *migrateBaseline {
		db.InitDB(db.ConnStringFromEnv())

		if *migrateBaseline {
			recorded, err := db.BaselineMigrations()
			if err != nil {
				fmt.Fprintf(os.Stderr, "baseline failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Baselined %d migration(s) as already applied. Nothing was executed.\n", recorded)
			return
		}

		if *migrateStatus {
			pending, err := db.PendingMigrations()
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not read migration status: %v\n", err)
				os.Exit(1)
			}
			if len(pending) == 0 {
				fmt.Println("Database is up to date - no pending migrations.")
				return
			}
			fmt.Printf("%d pending migration(s):\n", len(pending))
			for _, name := range pending {
				fmt.Printf("  %s\n", name)
			}
			return
		}

		results, err := db.ApplyPendingMigrations()
		applied := 0
		for _, r := range results {
			if r.Applied {
				applied++
				fmt.Printf("  [apply] %s\n", r.File)
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Applied %d migration(s); %d already up to date.\n", applied, len(results)-applied)

		// Stage 64.1: most migrations change only tenant_default, so bring
		// every client schema up to it after each run (db/tenant_schema_sync.go).
		reports, err := db.SyncAllTenantSchemas()
		for _, r := range reports {
			if r.Changed() {
				fmt.Printf("  [sync] %s: %s\n", r.Schema, r.Summary())
			}
			for _, w := range r.Warnings {
				fmt.Printf("  [sync-warn] %s: %s\n", r.Schema, w)
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "client schema sync failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Client schemas in step with tenant_default (%d checked).\n", len(reports))

		// Stage 93.2: every location that has never had a cost center is
		// linked to a same-name one or given its own - once, idempotent.
		ccDone, err := engines.BackfillLocationCostCentersAllTenants()
		for tenant, n := range ccDone {
			fmt.Printf("  [cost-center] %s: %d location(s) linked or given a cost center\n", tenant, n)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "location cost center backfill failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	server.Run()
}
