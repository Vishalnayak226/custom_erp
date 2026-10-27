package engines

// BLD-044 - the background worker inventory, held to the code. Every worker
// internal/server/routes.go starts is listed here with the module that owns it;
// the test reads each Start function and checks that a module-owned worker asks
// listTenantSchemas for its module (so a client without that module costs it
// nothing), and that a worker visiting every tenant runs on one server only.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// workerOwner names the module whose tenants a worker serves; "" is core work
// every tenant needs. A new worker fails the test until it is listed here.
var workerOwner = map[string]string{
	"StartOutboxWorker":                   "",
	"StartPublishQueueWorker":             "pim",
	"StartChannelSyncWorker":              "oms",
	"StartMagentoPollWorker":              "oms",
	"StartPatchIntakeWorker":              "",
	"StartAlertMonitor":                   "",
	"StartQueueSaturationMonitor":         "",
	"StartSecurityDetectionSweep":         "",
	"StartBackupFreshnessMonitor":         "",
	"StartUnicommerceWorker":              "oms",
	"StartPineLabsReconciliationWorker":   "",
	"StartCleverTapWorker":                "crm_loyalty",
	"StartReportExportWorker":             "reports",
	"StartReportExportRetentionSweeper":   "reports",
	"StartScheduledReportWorker":          "reports",
	"StartWaveAutoCreationWorker":         "wms",
	"StartRecurringJournalWorker":         "",
	"StartPIMImportScheduleWorker":        "pim",
	"StartPIMExportScheduleWorker":        "pim",
	"StartLoyaltyExpiryWorker":            "crm_loyalty",
	"StartDunningWorker":                  "",
	"StartPOAutoCancelWorker":             "",
	"StartAmortizationWorker":             "",
	"StartRecurringBillingWorker":         "",
	"StartEInvoiceWorker":                 "",
	"StartMaintenanceSchedulingWorker":    "quality",
	"StartDashboardDigestWorker":          "reports",
	"StartJobRunnerWorker":                "",
	"StartJobRunnerRetentionSweeper":      "",
	"StartIntegrationLogRetentionSweeper": "",
	"StartAuditEvidenceScheduler":         "",
	"StartAuditArchiveScheduler":          "",
	"StartAssetDepreciationScheduler":     "assets",
	"StartCampaignWorker":                 "crm_loyalty",
	"StartCRMWorker":                      "crm_loyalty",
	"StartCompetitorUndercutWorker":       "",
	"StartReservationSweeper":             "",
	"StartPublicAPIRuntimeSweeper":        "",
	"StartWebPushAlertWorker":             "",
	"StartDayLimitsWorker":                "",
	"StartItemTaxChangeWorker":            "",
}

// claimSafeWorkers may run on every server: they claim each unit of work
// with a row lock, so more servers share the queue instead of repeating it.
var claimSafeWorkers = map[string]string{
	"StartJobRunnerWorker": "claims jobs with FOR UPDATE SKIP LOCKED leases",
}

type workerFacts struct {
	interval   string
	leader     bool     // calls IsSchedulerLeader
	tenantKeys []string // module keys passed to a tenant list; nil = no tenant loop
	loops      bool
}

func TestBLD044WorkerInventoryMatchesTheCode(t *testing.T) {
	routes, err := os.ReadFile(filepath.Join("..", "internal", "server", "routes.go"))
	if err != nil {
		t.Fatal(err)
	}
	started := map[string]string{}
	for _, m := range regexp.MustCompile(`engines\.(Start[A-Za-z]+)\(workerCtx,\s*([^)]*)\)`).FindAllStringSubmatch(string(routes), -1) {
		started[m[1]] = m[2]
	}
	if len(started) < 30 {
		t.Fatalf("found only %d workers in routes.go; the pattern no longer matches", len(started))
	}

	// Collect every function body in the package, so a Start function's
	// helpers (one level down) are read too.
	fset := token.NewFileSet()
	bodies := map[string]*ast.BlockStmt{}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil && fn.Recv == nil {
				bodies[fn.Name.Name] = fn.Body
			}
		}
	}
	read := func(name string) workerFacts {
		var w workerFacts
		visit := func(body *ast.BlockStmt, follow bool, callees map[string]bool) {
			ast.Inspect(body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				switch id.Name {
				case "IsSchedulerLeader":
					w.leader = true
				case "listTenantSchemas", "listRegisteredTenantSchemas":
					w.loops = true
					for _, a := range call.Args {
						if lit, ok := a.(*ast.BasicLit); ok {
							s, _ := strconv.Unquote(lit.Value)
							w.tenantKeys = append(w.tenantKeys, s)
						}
					}
				default:
					if follow {
						callees[id.Name] = true
					}
				}
				return true
			})
		}
		callees := map[string]bool{}
		if b := bodies[name]; b != nil {
			visit(b, true, callees)
		}
		for c := range callees {
			if b := bodies[c]; b != nil && !strings.HasPrefix(c, "Start") {
				visit(b, false, nil)
			}
		}
		return w
	}

	names := make([]string, 0, len(started))
	for n := range started {
		names = append(names, n)
	}
	sort.Strings(names)
	var table strings.Builder
	for _, name := range names {
		owner, listed := workerOwner[name]
		if !listed {
			t.Errorf("%s is started in routes.go but not in workerOwner - list it with the module it serves", name)
			continue
		}
		if bodies[name] == nil {
			t.Errorf("%s: no such function in engines", name)
			continue
		}
		w := read(name)
		w.interval = started[name]
		if owner != "" && !containsString(w.tenantKeys, owner) {
			t.Errorf("%s serves module %q but lists tenants with keys %v - a client without the module would still cost it work", name, owner, w.tenantKeys)
		}
		if _, shared := claimSafeWorkers[name]; w.loops && !w.leader && !shared {
			t.Errorf("%s visits every tenant but is not gated by IsSchedulerLeader - two servers would both do the work", name)
		}
		table.WriteString(name + " every " + w.interval + " module=" + owner + " leader=" + strconv.FormatBool(w.leader) + " keys=" + strings.Join(w.tenantKeys, ",") + "\n")
	}
	for name := range workerOwner {
		if _, ok := started[name]; !ok {
			t.Errorf("%s is in workerOwner but routes.go no longer starts it", name)
		}
	}
	t.Log("\n" + table.String())
}
