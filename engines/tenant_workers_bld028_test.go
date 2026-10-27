package engines

// BLD-028 - the tenant lifecycle reaches the background workers. A suspended or
// trial-expired client is refused at sign-in; before this, ~30 workers still
// sent campaigns and dunning mail, submitted e-invoices, delivered webhooks and
// ran queued jobs in its name, because listTenantSchemas read every registry
// row. Now only maintenance (retention, audit evidence, security detection,
// operator monitors) sees every registered tenant.

import (
	"custom_erp/db"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBLD028WorkersServeOnlyOperationalTenants(t *testing.T) {
	db.InitDB(testConnStr())

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	rows := []struct {
		id, status string
		trialEnds  string // SQL expression
		served     bool
	}{
		{"bld028a" + suffix, "active", "NULL", true},
		{"bld028s" + suffix, "suspended", "NULL", false},
		{"bld028t" + suffix, "active", "NOW() - INTERVAL '1 hour'", false},
		{"bld028f" + suffix, "active", "NOW() + INTERVAL '7 days'", true},
		{"bld028d" + suffix, "deprovision_requested", "NULL", false},
	}
	for _, r := range rows {
		// Registry rows only: the lists read public.tenants, not the schema.
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO public.tenants (tenant_id, name, schema_name, lifecycle_status, trial_ends_at)
			VALUES ($1, $1, $2, $3, %s)`, r.trialEnds), r.id, "tenant_"+r.id, r.status); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
	defer func() {
		for _, r := range rows {
			db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, r.id)
		}
	}()

	toSet := func(list []string, err error) map[string]bool {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	served := toSet(listTenantSchemas())
	registered := toSet(listRegisteredTenantSchemas())
	for _, r := range rows {
		schema := "tenant_" + r.id
		if served[schema] != r.served {
			t.Errorf("%s (%s, trial %s): served=%v, want %v", r.id, r.status, r.trialEnds, served[schema], r.served)
		}
		if !registered[schema] {
			t.Errorf("%s: maintenance list must still see every registered tenant", r.id)
		}
	}
	if !served["tenant_default"] {
		t.Error("tenant_default must be served")
	}

	// Resume: the worker picks the client up on its next tick.
	db.DB.Exec(`UPDATE public.tenants SET lifecycle_status = 'active' WHERE tenant_id = $1`, rows[1].id)
	if !toSet(listTenantSchemas())["tenant_"+rows[1].id] {
		t.Error("a resumed tenant must be served again")
	}
}

// maintenanceTenantLoops is the only code allowed to iterate every registered
// tenant, suspended ones included. A new worker that sends, posts or calls a
// provider belongs on listTenantSchemas; adding it here needs a reason.
var maintenanceTenantLoops = map[string]string{
	"StartQueueSaturationMonitor":         "operator alert: a suspended client's backlog is still the operator's",
	"StartAlertMonitor":                   "operator alert on error rate",
	"QuarantineStaleExternalDispatchJobs": "safety: stops external dispatch on non-production copies",
	"StartJobRunnerRetentionSweeper":      "retention keeps async_jobs bounded",
	"runPatchIntakeCycle":                 "security patch intake",
	"StartPublicAPIRuntimeSweeper":        "retention of API runtime state",
	"StartReportExportRetentionSweeper":   "retention of export files",
	"seededCredentialFindings":            "security posture",
	"storedConnectorCredentialFindings":   "security posture",
	"EncryptStoredValueCardsOnBoot":       "encryption at rest",
	"StartIntegrationLogRetentionSweeper": "retention of integration logs",
	"StartAuditArchiveScheduler":          "audit evidence is kept through suspension and retention",
	"StartAuditEvidenceScheduler":         "audit checkpoints",
	"StartSecurityDetectionSweep":         "detection keeps watching a suspended client",
}

func TestBLD028OnlyMaintenanceSweepsSeeEveryTenant(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(src), "listRegisteredTenantSchemas(") {
			continue
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "listRegisteredTenantSchemas" {
					found[fn.Name.Name] = true
					if _, allowed := maintenanceTenantLoops[fn.Name.Name]; !allowed {
						t.Errorf("%s (%s) iterates every registered tenant, suspended ones included; use listTenantSchemas unless it is maintenance", fn.Name.Name, f)
					}
				}
				return true
			})
		}
	}
	var stale []string
	for name := range maintenanceTenantLoops {
		if !found[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("allowlisted but no longer calling listRegisteredTenantSchemas: %v", stale)
	}
}
