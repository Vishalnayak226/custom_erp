package main

// tenantctl - Stage 49.1.5's operator entry point for the tenant lifecycle:
// provision, inspect, suspend, resume, deprovision, purge, and prove that a
// purged tenant left nothing behind.
//
// Why a CLI and not an admin route. Creating and destroying tenants is
// platform-level authority, not tenant-level authority: there is no role
// inside the product that should be able to reach it, and putting it behind a
// route would mean putting an authenticated, internet-reachable path in front
// of DROP SCHEMA. Host-shell access to the database box is already an
// unpreventable privilege (whoever has it could run the SQL by hand), so the
// job here is the same one cmd/reset_mfa took: make the privileged action
// scoped, refusable, dry-run by default, and permanently evidenced.
//
// Everything destructive requires -yes; without it the command prints exactly
// what it would do and changes nothing. Every state change requires -reason
// and records it, with the actor, in public.tenant_lifecycle_events.

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

const usage = `tenantctl - tenant provisioning and deprovisioning (Stage 49.1.5)

Usage: tenantctl <command> [flags]

Inspect
  list                                     every registered tenant and its lifecycle state
  show      -tenant <id>                   one tenant in full, including bootstrap state
  events    -tenant <id> [-limit N]        the tenant's evidence trail
  verify    -tenant <id> [-schema <name>]  prove nothing of this tenant remains
  db-privilege [-schema <name>]            least-privilege posture of the app's database role

Provision
  provision -tenant <id> [-schema <name>] [-app-version <v>] -reason <text> -actor <who> -yes
  rotate-bootstrap -tenant <id> [-user admin] -reason <text> -actor <who> -yes

Lifecycle
  suspend   -tenant <id> -reason <text> -actor <who> -yes
  resume    -tenant <id> -reason <text> -actor <who> -yes
  deprovision -tenant <id> -reason <text> -actor <who> [-retention-days N] -yes
  legal-hold  -tenant <id> -on|-off -reason <text> -actor <who> -yes
  purge     -tenant <id> -backup-ref <where the data was preserved> -actor <who> -yes

DATABASE_URL selects the database; without it the same local default the rest
of the tooling uses applies. Nothing destructive happens without -yes.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	}

	db.InitDB(db.ConnStringFromEnv())

	var err error
	switch command {
	case "list":
		err = cmdList(args)
	case "show":
		err = cmdShow(args)
	case "events":
		err = cmdEvents(args)
	case "verify":
		err = cmdVerify(args)
	case "db-privilege":
		err = cmdDBPrivilege(args)
	case "provision":
		err = cmdProvision(args)
	case "rotate-bootstrap":
		err = cmdRotateBootstrap(args)
	case "suspend":
		err = cmdTransition(args, "suspend")
	case "resume":
		err = cmdTransition(args, "resume")
	case "deprovision":
		err = cmdDeprovision(args)
	case "legal-hold":
		err = cmdLegalHold(args)
	case "purge":
		err = cmdPurge(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "tenantctl %s: %v\n", command, err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Inspect
// ---------------------------------------------------------------------------

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	tenants, err := engines.ListTenantLifecycles()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TENANT\tSCHEMA\tSTATUS\tBOOTSTRAP\tLEGAL HOLD\tRETENTION UNTIL")
	for _, t := range tenants {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.TenantID, t.SchemaName, statusLabel(t), bootstrapLabel(t), yesNo(t.LegalHold), stamp(t.RetentionUntil))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\n%d tenant(s).\n", len(tenants))
	return nil
}

func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	t, err := engines.GetTenantLifecycle(*tenant)
	if err != nil {
		return err
	}
	fmt.Printf("tenant            %s\n", t.TenantID)
	fmt.Printf("schema            %s\n", t.SchemaName)
	fmt.Printf("status            %s\n", statusLabel(t))
	if t.Reason != "" {
		fmt.Printf("status reason     %s\n", t.Reason)
	}
	fmt.Printf("sandbox           %s\n", yesNo(t.IsSandbox))
	fmt.Printf("provisioned       %s\n", stamp(t.ProvisionedAt))
	fmt.Printf("status changed    %s\n", stamp(t.ChangedAt))
	fmt.Printf("legal hold        %s", yesNo(t.LegalHold))
	if t.LegalHold && t.LegalHoldReason != "" {
		fmt.Printf(" (%s)", t.LegalHoldReason)
	}
	fmt.Println()
	fmt.Printf("retention until   %s\n", stamp(t.RetentionUntil))
	fmt.Printf("bootstrap         %s\n", bootstrapLabel(t))
	fmt.Printf("  issued          %s\n", stamp(t.BootstrapIssuedAt))
	fmt.Printf("  expires         %s\n", stamp(t.BootstrapExpiresAt))
	fmt.Printf("  rotated         %s\n", stamp(t.BootstrapConsumedAt))
	return nil
}

func cmdEvents(args []string) error {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required). Works for purged tenants too - the trail outlives the registry row.")
	limit := fs.Int("limit", 50, "How many events to show, newest first.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	events, err := engines.TenantLifecycleEvents(*tenant, *limit)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Printf("No lifecycle events recorded for %s.\n", *tenant)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tEVENT\tACTOR\tDETAIL")
	for _, e := range events {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.OccurredAt.Format(time.RFC3339), e.Event, e.Actor, e.Detail)
	}
	return w.Flush()
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	schema := fs.String("schema", "", "Schema name to check when the registry row is already gone (purged tenants).")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	rep, err := engines.VerifyTenantResidue(*tenant, *schema)
	if err != nil {
		return err
	}
	printResidue(rep)
	if !rep.Clean {
		return fmt.Errorf("residue remains for %s", *tenant)
	}
	return nil
}

func cmdDBPrivilege(args []string) error {
	fs := flag.NewFlagSet("db-privilege", flag.ExitOnError)
	schema := fs.String("schema", "", "Optional schema whose owner should be reported.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := engines.TenantDatabasePrivilegeReport(*schema)
	if err != nil {
		return err
	}
	fmt.Printf("connected role    %s\n", p.ConnectedRole)
	fmt.Printf("superuser         %s\n", yesNo(p.IsSuperuser))
	fmt.Printf("createrole        %s\n", yesNo(p.CanCreateRole))
	fmt.Printf("createdb          %s\n", yesNo(p.CanCreateDB))
	if *schema != "" {
		owner := p.SchemaOwner
		if owner == "" {
			owner = "(schema does not exist)"
		}
		fmt.Printf("owner of %-8s %s\n", *schema, owner)
	}
	if len(p.Findings) == 0 {
		fmt.Println("\nNo least-privilege findings.")
		return nil
	}
	fmt.Println()
	for _, f := range p.Findings {
		fmt.Printf("FINDING  %s\n", f)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Provision
// ---------------------------------------------------------------------------

func cmdProvision(args []string) error {
	fs := flag.NewFlagSet("provision", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id to create (required).")
	schema := fs.String("schema", "", "Schema name. Defaults to tenant_<id> with non-identifier characters replaced by _.")
	appVersion := fs.String("app-version", "", "Application version to stamp on the registry row.")
	reason, actor, confirm := commonFlags(fs, "Why this tenant is being provisioned")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	if *reason == "" {
		return fmt.Errorf("-reason is required and is recorded in the tenant's evidence trail")
	}
	schemaName := *schema
	if schemaName == "" {
		schemaName = defaultSchemaName(*tenant)
	}

	if !*confirm {
		fmt.Printf("DRY RUN - would provision tenant %q into schema %q.\n", *tenant, schemaName)
		fmt.Println("Re-run with -yes to actually create it.")
		return nil
	}

	password, err := engines.ProvisionTenantSchema(*tenant, schemaName, *appVersion)
	if err != nil {
		return err
	}
	if err := engines.RecordTenantLifecycleEvent(*tenant, engines.TenantEventProvisioned, *actor, *reason); err != nil {
		fmt.Fprintf(os.Stderr, "warning: tenant created but the operator reason could not be recorded: %v\n", err)
	}

	life, err := engines.GetTenantLifecycle(*tenant)
	if err != nil {
		return err
	}
	fmt.Printf("Provisioned %s (schema %s).\n\n", *tenant, schemaName)
	fmt.Printf("  one-time admin password : %s\n", password)
	fmt.Printf("  username                : admin\n")
	fmt.Printf("  valid until             : %s\n", stamp(life.BootstrapExpiresAt))
	fmt.Println()
	fmt.Println("This password is shown once and is stored only as a hash. It is refused")
	fmt.Println("after the expiry above unless it has been rotated; reissue one with")
	fmt.Println("`tenantctl rotate-bootstrap`. The admin role is MFA-mandatory, so the")
	fmt.Println("first login goes straight into TOTP enrollment before any session is issued.")
	return nil
}

func cmdRotateBootstrap(args []string) error {
	fs := flag.NewFlagSet("rotate-bootstrap", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	user := fs.String("user", "admin", "Username whose credential is reissued.")
	reason, actor, confirm := commonFlags(fs, "Why a new one-time credential is needed, e.g. an incident or handover reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	if *reason == "" {
		return fmt.Errorf("-reason is required and is recorded in the tenant's evidence trail")
	}
	if !*confirm {
		fmt.Printf("DRY RUN - would issue a new one-time password for %s in tenant %s, invalidating the current one.\n", *user, *tenant)
		fmt.Println("Re-run with -yes to actually rotate it.")
		return nil
	}
	password, err := engines.RotateTenantBootstrapCredential(*tenant, *user, *actor, *reason)
	if err != nil {
		return err
	}
	life, err := engines.GetTenantLifecycle(*tenant)
	if err != nil {
		return err
	}
	fmt.Printf("Reissued the one-time credential for %s in %s.\n\n", *user, *tenant)
	fmt.Printf("  one-time password : %s\n", password)
	fmt.Printf("  valid until       : %s\n", stamp(life.BootstrapExpiresAt))
	return nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func cmdTransition(args []string, kind string) error {
	fs := flag.NewFlagSet(kind, flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	reason, actor, confirm := commonFlags(fs, "Why this tenant is being "+kind+"ed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	if *reason == "" {
		return fmt.Errorf("-reason is required and is recorded in the tenant's evidence trail")
	}
	if !*confirm {
		switch kind {
		case "suspend":
			fmt.Printf("DRY RUN - would suspend %s: every login and every live session for it stops. Nothing is deleted.\n", *tenant)
		case "resume":
			fmt.Printf("DRY RUN - would return %s to service.\n", *tenant)
		}
		fmt.Println("Re-run with -yes to apply it.")
		return nil
	}
	var err error
	if kind == "suspend" {
		err = engines.SuspendTenant(*tenant, *actor, *reason)
	} else {
		err = engines.ResumeTenant(*tenant, *actor, *reason)
	}
	if err != nil {
		return err
	}
	if kind == "suspend" {
		fmt.Printf("%s is now suspended. Its logins and live sessions stop within AUTH_STATE_CACHE_SECONDS.\n", *tenant)
	} else {
		fmt.Printf("%s is active again.\n", *tenant)
	}
	return nil
}

func cmdDeprovision(args []string) error {
	fs := flag.NewFlagSet("deprovision", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	retentionDays := fs.Int("retention-days", engines.UseDefaultRetention,
		"Days the data must be kept before a purge is permitted. -1 uses the policy default (TENANT_RETENTION_DAYS, else 30). 0 means an agreed zero-retention offboarding.")
	reason, actor, confirm := commonFlags(fs, "Why this tenant is being offboarded, e.g. a contract or ticket reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	if *reason == "" {
		return fmt.Errorf("-reason is required and is recorded in the tenant's evidence trail")
	}
	if !*confirm {
		fmt.Printf("DRY RUN - would start deprovisioning %s: logins and live sessions stop immediately and the\n", *tenant)
		fmt.Println("retention clock starts. No data is deleted by this step; `purge` is a separate act.")
		fmt.Println("Re-run with -yes to apply it.")
		return nil
	}
	until, err := engines.RequestTenantDeprovision(*tenant, *actor, *reason, *retentionDays)
	if err != nil {
		return err
	}
	fmt.Printf("%s is deprovisioning. Purge is not permitted before %s.\n", *tenant, until.Format(time.RFC3339))
	return nil
}

func cmdLegalHold(args []string) error {
	fs := flag.NewFlagSet("legal-hold", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	on := fs.Bool("on", false, "Place a hold: no purge of this tenant is permitted regardless of retention.")
	off := fs.Bool("off", false, "Lift the hold.")
	reason, actor, confirm := commonFlags(fs, "The matter this hold is for, or why it is being lifted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	if *on == *off {
		return fmt.Errorf("pass exactly one of -on or -off")
	}
	if *reason == "" {
		return fmt.Errorf("-reason is required and is recorded in the tenant's evidence trail")
	}
	if !*confirm {
		state := "lift the legal hold on"
		if *on {
			state = "place a legal hold on"
		}
		fmt.Printf("DRY RUN - would %s %s.\nRe-run with -yes to apply it.\n", state, *tenant)
		return nil
	}
	if err := engines.SetTenantLegalHold(*tenant, *on, *actor, *reason); err != nil {
		return err
	}
	if *on {
		fmt.Printf("Legal hold placed on %s. No purge will be permitted until it is lifted.\n", *tenant)
	} else {
		fmt.Printf("Legal hold lifted on %s.\n", *tenant)
	}
	return nil
}

func cmdPurge(args []string) error {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	tenant := fs.String("tenant", "", "Tenant id (required).")
	backupRef := fs.String("backup-ref", "",
		"Where this tenant's data has been preserved - a dump filename, backup id or archive location (required, recorded permanently).")
	reason, actor, confirm := commonFlags(fs, "Not used by purge; the backup reference and the deprovisioning reason are the record")
	_ = reason
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("-tenant is required")
	}
	if strings.TrimSpace(*backupRef) == "" {
		return fmt.Errorf("-backup-ref is required: a purge must record where the tenant's data was preserved")
	}

	life, err := engines.GetTenantLifecycle(*tenant)
	if err != nil {
		return err
	}
	if !*confirm {
		fmt.Printf("DRY RUN - would PERMANENTLY drop schema %s and delete the registry row for %s.\n", life.SchemaName, *tenant)
		fmt.Printf("  current status  : %s\n", life.Status)
		fmt.Printf("  legal hold      : %s\n", yesNo(life.LegalHold))
		fmt.Printf("  retention until : %s\n", stamp(life.RetentionUntil))
		fmt.Println("Re-run with -yes to destroy it. This cannot be undone from here - only from the backup above.")
		return nil
	}

	rep, err := engines.PurgeTenant(*tenant, *actor, *backupRef)
	if err != nil {
		return err
	}
	fmt.Printf("Purged %s.\n\n", *tenant)
	printResidue(rep)
	if !rep.Clean {
		return fmt.Errorf("purge completed but residue remains - see above")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shared
// ---------------------------------------------------------------------------

func commonFlags(fs *flag.FlagSet, reasonHelp string) (reason, actor *string, confirm *bool) {
	reason = fs.String("reason", "", reasonHelp+" (required - recorded in the evidence trail).")
	actor = fs.String("actor", defaultActor(), "Who is performing this, recorded in the evidence trail.")
	confirm = fs.Bool("yes", false, "Actually perform the action. Without this the command only prints what it would do.")
	return reason, actor, confirm
}

func defaultActor() string {
	for _, key := range []string{"TENANTCTL_ACTOR", "USERNAME", "USER"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return "operator"
}

// defaultSchemaName mirrors the tenant_<id> convention the rest of the product
// uses, with anything that is not a legal identifier character replaced so the
// result is always something ProvisionTenantSchema will accept.
func defaultSchemaName(tenantID string) string {
	var b strings.Builder
	b.WriteString("tenant_")
	for _, c := range strings.ToLower(tenantID) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func printResidue(rep engines.TenantResidueReport) {
	fmt.Printf("residue check for %s (schema %s)\n", rep.TenantID, rep.SchemaName)
	fmt.Printf("  registry row      %s\n", presentAbsent(rep.RegistryRow))
	fmt.Printf("  schema            %s", presentAbsent(rep.SchemaExists))
	if rep.SchemaExists {
		fmt.Printf(" (%d table(s))", rep.SchemaTables)
	}
	fmt.Println()
	fmt.Printf("  hostname mapping  %s\n", presentAbsentString(rep.HostSlug))
	if len(rep.PublicRows) == 0 {
		fmt.Printf("  public rows       absent\n")
	} else {
		for table, n := range rep.PublicRows {
			fmt.Printf("  public.%-12s %d row(s) STILL PRESENT\n", table, n)
		}
	}
	fmt.Printf("  cached session    %s\n", presentAbsent(rep.CachedState))
	if rep.Clean {
		fmt.Println("  => CLEAN: no route, worker, hostname, session or row can still address this tenant.")
	} else {
		fmt.Println("  => RESIDUE REMAINS: this tenant is still addressable in at least one way.")
	}
}

func statusLabel(t engines.TenantLifecycle) string {
	if t.LegalHold {
		return t.Status + " +hold"
	}
	return t.Status
}

func bootstrapLabel(t engines.TenantLifecycle) string {
	switch {
	case t.BootstrapExpired:
		return "EXPIRED (unrotated)"
	case t.BootstrapOutstanding:
		return "outstanding"
	case t.BootstrapConsumedAt != nil:
		return "rotated"
	default:
		return "-"
	}
}

func stamp(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func presentAbsent(b bool) string {
	if b {
		return "PRESENT"
	}
	return "absent"
}

func presentAbsentString(s string) string {
	if s == "" {
		return "absent"
	}
	return "PRESENT (" + s + ")"
}
