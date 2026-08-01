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

	"custom_erp/db"
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
	flag.Parse()

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
		return
	}

	server.Run()
}
