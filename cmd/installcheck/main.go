// Command installcheck is the install step that comes before any tenant
// exists (Stage 47.12.8): is this host fit to run the ERP, are the secrets in
// place, does the domain point here, will backups land somewhere writable.
// It prints a PASS / WARN / FAIL table with the exact next step for each row
// and exits non-zero when anything FAILs.
//
//	installcheck -env /etc/erp/erp.env -init-secrets -domain app.example.com \
//	    -backup-dir /opt/erp/backups -channel stable
//
// A hosted client (a tenant on a server that is already running) is made with
// cmd/tenantctl provision, not here. It changes nothing but the env file, and
// only by adding what is missing: it never replaces a secret that holds a
// real value.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"text/tabwriter"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("installcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	def := "/etc/erp/erp.env"
	if runtime.GOOS == "windows" {
		def = "erp.env"
	}
	c := &Config{}
	fs.StringVar(&c.EnvFile, "env", def, "Env file that holds the secrets.")
	fs.StringVar(&c.Domain, "domain", "", "Public host name, e.g. app.example.com.")
	fs.StringVar(&c.PublicIP, "public-ip", "", "This host's public address when it differs from its own interfaces (NAT, load balancer).")
	fs.StringVar(&c.DBURL, "db-url", "", "PostgreSQL URL (default: DATABASE_URL in the env file, then the environment).")
	fs.IntVar(&c.AppPort, "port", 8080, "Port the app listens on.")
	fs.StringVar(&c.BackupDir, "backup-dir", "", "Where backups are written.")
	fs.StringVar(&c.DeployDir, "deploy-dir", "deploy", "Folder holding restore_drill.sh and the other deploy scripts.")
	fs.StringVar(&c.Channel, "channel", "", "Upgrade channel to record: stable, candidate or hold.")
	fs.StringVar(&c.ReleaseBin, "release-bin", "", "Release binary to check before installing it.")
	fs.StringVar(&c.ReleaseSig, "release-sig", "", "Its signature file.")
	fs.StringVar(&c.PubKey, "pub", "", "Release public key (default release/signing.pub).")
	fs.IntVar(&c.MinDiskGB, "min-disk-gb", 10, "Smallest acceptable free disk.")
	fs.IntVar(&c.MinMemMB, "min-mem-mb", 1024, "Smallest acceptable memory.")
	fs.BoolVar(&c.InitSecret, "init-secrets", false, "Create the env file and fill in the secrets it lacks. Never replaces one that is set.")
	asJSON := fs.Bool("json", false, "Print the report as JSON.")
	strict := fs.Bool("strict", false, "Treat WARN as a failure too.")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if c.DBURL == "" {
		if lines, err := readLines(c.EnvFile); err == nil {
			if _, v, ok := envValue(lines, "DATABASE_URL"); ok {
				c.DBURL = v
			}
		}
	}
	if c.DBURL == "" {
		c.DBURL = os.Getenv("DATABASE_URL")
	}
	results := RunAll(c)
	failed := Failed(results, *strict)
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]any{"ok": !failed, "checks": results})
	} else {
		w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "CHECK\tRESULT\tDETAIL")
		for _, r := range results {
			fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, r.Status, r.Detail)
		}
		w.Flush()
		var steps bool
		for _, r := range results {
			if r.Next != "" && (r.Status == Fail || r.Status == Warn) {
				if !steps {
					fmt.Fprintln(stdout, "\nNext steps:")
					steps = true
				}
				fmt.Fprintf(stdout, "  [%s] %s: %s\n", r.Status, r.Name, r.Next)
			}
		}
		if failed {
			fmt.Fprintln(stdout, "\nNot ready: fix the FAIL rows and run it again.")
		} else {
			fmt.Fprintln(stdout, "\nReady to install.")
		}
	}
	if failed {
		return 1
	}
	return 0
}
