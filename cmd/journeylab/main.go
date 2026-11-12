// Command journeylab drives the seven business journeys (and a soak) over real
// HTTP, as the proper role, against a scratch server, and records evidence:
// what each step cost, and whether the books still tie afterwards.
//
//	journeylab journey -base-url http://localhost:8730 -db-url <scratch db> -out docs/qa/evidence
//	journeylab soak    -duration 30m -server-bin <server.exe> -out <dir>
//
// It refuses non-local servers and non-scratch databases unless told otherwise
// (see guard.go), and never runs against ENV=production.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
	"custom_erp/internal/releasegate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "journey":
		code = cmdJourney(os.Args[2:])
	case "soak":
		code = cmdSoak(os.Args[2:])
	case "cluster":
		code = cmdCluster(os.Args[2:])
	default:
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  journeylab journey [flags]   drive the journeys, write JSON and Markdown evidence
  journeylab soak    [flags]   repeat a light journey for a duration with disturbances
  journeylab cluster up|down   start or stop a private Postgres cluster for the soak`)
}

type commonFlags struct {
	baseURL, dbURL, allowDB, out, tenant, pgBin, serverBin string
	allowRemote                                            bool
	serverPID                                              int
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.baseURL, "base-url", "http://localhost:8730", "the scratch server (localhost only unless -allow-remote)")
	fs.StringVar(&c.dbURL, "db-url", os.Getenv("DATABASE_URL"), "the scratch database URL (default: DATABASE_URL)")
	fs.StringVar(&c.allowDB, "allow-db", "", "name a database that is not obviously a scratch one (test, scratch, bench ...)")
	fs.BoolVar(&c.allowRemote, "allow-remote", false, "allow a non-local server or database host")
	fs.StringVar(&c.out, "out", "journeylab-out", "folder for the evidence files")
	fs.StringVar(&c.tenant, "tenant", "default", "tenant id")
	fs.StringVar(&c.pgBin, "pg-bin", "", "folder holding pg_dump, pg_restore, createdb, dropdb, psql")
	fs.StringVar(&c.serverBin, "server-bin", "", "the server binary (for -verify-restore, and for soak to start it)")
	fs.IntVar(&c.serverPID, "server-pid", 0, "pid of the server under test, for CPU and memory figures (0 = not measured)")
}

// bootstrap checks the guards, opens the database and builds a Runner. The
// caller sets up the role users with Setup().
func bootstrap(c *commonFlags) (*Runner, error) {
	if err := CheckAll(c.baseURL, c.dbURL, c.allowDB, c.allowRemote); err != nil {
		return nil, err
	}
	db.InitDB(c.dbURL)
	schema, err := db.GetTenantSchema(c.tenant)
	if err != nil {
		return nil, err
	}
	return newRunner(c, db.DB, schema)
}

func newRunner(c *commonFlags, d *sql.DB, schema string) (*Runner, error) {
	cl := NewClient(c.baseURL)
	r := &Runner{Cl: cl, DB: d}
	pid := c.serverPID
	r.Cfg = Config{BaseURL: c.baseURL, DBURL: c.dbURL, Tenant: c.tenant, Schema: schema, Settle: 1100 * time.Millisecond, PGBin: c.pgBin, ServerBin: c.serverBin,
		ServerPID: func() int { return pid }}
	r.St = NewDBStats(d, schema, r.Cfg.Settle)
	// The admin session is minted, not logged in: the admin account needs a second factor.
	var id, loc string
	var cv int
	var role string
	if err := d.QueryRow(fmt.Sprintf(`SELECT id, role, COALESCE(location_code,''), COALESCE(credential_version,1) FROM %s.users WHERE username = 'admin'`, schema)).Scan(&id, &role, &loc, &cv); err != nil {
		return nil, fmt.Errorf("find the admin user: %w", err)
	}
	mint := func() string { return engines.SignToken(id, "admin", role, c.tenant, loc, cv) }
	cl.Sess["admin"] = &Session{Name: "admin", Role: role, Username: "admin", Token: mint(), Mint: mint}
	return r, nil
}

func cmdJourney(args []string) int {
	fs := flag.NewFlagSet("journey", flag.ExitOnError)
	var c commonFlags
	c.register(fs)
	only := fs.String("journeys", "J1,J2,J3,J4,J5,J6,J7", "which journeys to drive, comma separated")
	browser := fs.String("browser-evidence", "", "the browser suite's accessibility and help-usability output (JSON)")
	inject := fs.Bool("inject-mismatch", false, "fail-first: add an unbalanced ledger row before reconciliation; the run must come out red")
	recordGate := fs.String("record-gate", "", "repo root: record the run as the release gate's journey evidence (release/evidence/journey.json); only a run of all seven journeys counts")
	_ = fs.Parse(args)

	r, err := bootstrap(&c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeylab:", err)
		return 2
	}
	r.Cfg.InjectFault = *inject
	if err := r.Setup(); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab: setup:", err)
		return 2
	}
	want := map[string]bool{}
	for _, k := range strings.Split(*only, ",") {
		want[strings.TrimSpace(strings.ToUpper(k))] = true
	}
	order := []struct {
		id string
		fn func() JourneyRecord
	}{
		{"J1", r.JourneyFirstSale}, {"J2", r.JourneyP2P}, {"J3", r.JourneyO2C}, {"J4", r.JourneyWarehouse},
		{"J5", r.JourneyPeriodClose}, {"J6", r.JourneyMigration}, {"J7", r.JourneyIncident},
	}
	var js []JourneyRecord
	for _, o := range order {
		if !want[o.id] {
			continue
		}
		fmt.Printf("%s ...\n", o.id)
		rec := o.fn()
		fmt.Printf("%s %s (%.0f ms, %d steps)\n", o.id, strings.ToUpper(rec.Status), rec.WallMS, len(rec.Steps))
		for _, s := range rec.Steps {
			for _, e := range s.Errors {
				fmt.Printf("    step %q: %s\n", s.Name, e)
			}
		}
		for _, k := range rec.Recon {
			if !k.Pass && !k.Warn {
				fmt.Printf("    reconciliation FAIL: %s (%s)\n", k.Name, k.Detail)
			}
		}
		js = append(js, rec)
	}
	if *inject {
		r.RemoveInjected()
	}
	ev := NewEvidence(c.baseURL, DBNameFromURL(c.dbURL), r.St.Source, js, LoadBrowser(*browser))
	jp, mp, err := WriteEvidence(c.out, ev)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeylab: write evidence:", err)
		return 2
	}
	fmt.Printf("overall: %s\nevidence: %s\nsummary:  %s\n", strings.ToUpper(ev.Overall), jp, mp)
	if *recordGate != "" {
		if ge, ok, why := GateEvidence(ev, *inject, gitHead(*recordGate), filepath.Base(jp), time.Now()); !ok {
			fmt.Println("gate evidence: not recorded -", why)
		} else if p, err := releasegate.WriteEvidence(*recordGate, ge); err != nil {
			fmt.Fprintln(os.Stderr, "journeylab: gate evidence:", err)
			return 2
		} else {
			fmt.Printf("gate evidence: %s (%s)\n", p, ge.Result)
		}
	}
	if ev.Overall != "pass" {
		return 1
	}
	return 0
}
