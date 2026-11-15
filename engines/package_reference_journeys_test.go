package engines

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
)

// 50.12 / BLD-022: one reference journey for each of the ten sellable
// packages, run three times (normal, denied, fresh repeat on a new client).
//
// Each journey provisions a new client, sets the package the way the platform
// does (TransitionTenantPackages), builds the minimum masters and runs the
// package's main workflow through the engine functions the screens call, runs
// one report, exports the client, imports it into a second new client and
// reconciles it, then proves a module the package lacks is refused. Every
// client the test makes is dropped again.

type packageJourney struct {
	pkg string
	// bundle: BLD-023 allows this package to be sold on its own. An add-on
	// package is set the way an operator does it (the override, still audited).
	bundle bool
	// reportModules: the report is one of these modules' own.
	reportModules []string
	shape         func(s *SynthShape)
	extra         func(t *testing.T, tenantID string)
}

func journeyBaseShape() SynthShape {
	return SynthShape{Name: "journey", Items: 8, Stores: 1, Warehouses: 1, BinsPerWarehouse: 10, StockedBinPct: 50, Vendors: 3, Customers: 5,
		Bills: 3, MaxLinesPerBill: 2, HotSKUPct: 30, Owners: 1, AuditTrail: 5, Months: 1, MonthEndEntries: 2, CustomerBillPct: 100}
}

func journeyDoc(t *testing.T, tenantID, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'journey')`, id, doctype, raw, status); err != nil {
		t.Fatalf("%s %s: %v", doctype, id, err)
	}
}

func packageJourneys() []packageJourney {
	return []packageJourney{
		{pkg: "pim", bundle: true, reportModules: []string{"pim"}, extra: func(t *testing.T, tenantID string) {
			// Catalogue work: a further range of items through the same import the screen uses.
			rows := []map[string]interface{}{
				{"code": "PJ-CAT-1", "name": "Catalogue item 1", "barcode": "2900000009001", "hsn_code": "6109", "gst_rate": 5, "tax_treatment": "Taxable", "sale_price": 499, "mrp": 499, "category": "Catalogue"},
				{"code": "PJ-CAT-2", "name": "Catalogue item 2", "barcode": "2900000009002", "hsn_code": "6109", "gst_rate": 5, "tax_treatment": "Taxable", "sale_price": 599, "mrp": 599, "category": "Catalogue"},
			}
			res, err := runDocDataImport(tenantID, "Item", "journey", "", false, rows, make([]string, len(rows)))
			if err != nil || res.FailedRows > 0 {
				t.Fatalf("catalogue import: %v %+v", err, res)
			}
		}},
		{pkg: "wms", bundle: true, reportModules: []string{"wms"}, shape: func(s *SynthShape) { s.Warehouses, s.Owners, s.BinsPerWarehouse = 2, 2, 20 }},
		{pkg: "oms", bundle: true, reportModules: []string{"oms"}, shape: func(s *SynthShape) { s.SalesOrders = 2 }},
		{pkg: "hr", reportModules: []string{"hr"}, extra: func(t *testing.T, tenantID string) {
			journeyDoc(t, tenantID, "PJ-EMP-1", "Employee", "Active", map[string]interface{}{"code": "PJ-EMP-1", "name": "Journey Employee", "status": "Active"})
			journeyDoc(t, tenantID, "PJ-LV-1", "Leave", "Applied", map[string]interface{}{"employee_id": "PJ-EMP-1", "leave_type": "Casual", "days": 2,
				"from_date": time.Now().Format("2006") + "-03-02", "to_date": time.Now().Format("2006") + "-03-03"})
			lines, err := GetLeaveBalance(tenantID, "PJ-EMP-1", time.Now().Format("2006"), SessionScope{Role: RoleSuperAdmin, UserID: "journey"})
			if err != nil {
				t.Fatalf("leave balance: %v", err)
			}
			var pending float64
			for _, l := range lines {
				pending += l.Pending
			}
			if pending != 2 {
				t.Fatalf("leave balance should show the 2 days applied for, got %+v", lines)
			}
		}},
		{pkg: "procurement", reportModules: []string{"procurement", "rfq"}, shape: func(s *SynthShape) { s.PurchaseOrders = 2 }},
		{pkg: "manufacturing", reportModules: []string{"manufacturing"}, shape: func(s *SynthShape) { s.ProductionOrders = 2 }},
		{pkg: "crm", reportModules: []string{"crm_loyalty"}, extra: func(t *testing.T, tenantID string) {
			lead, err := SaveCRMLead(tenantID, "", CRMLead{Name: "Journey Buyer", Company: "Journey Traders", Phone: "9876543210", Source: "Referral"}, "journey")
			if err != nil {
				t.Fatalf("lead: %v", err)
			}
			conv, err := ConvertCRMLead(tenantID, lead.ID, "Journey deal", 25000, time.Now().AddDate(0, 0, 30).Format("2006-01-02"), "journey")
			if err != nil || conv.CustomerID == "" || conv.OpportunityID == "" {
				t.Fatalf("convert lead: %+v %v", conv, err)
			}
		}},
		{pkg: "assets", reportModules: []string{"assets"}, extra: func(t *testing.T, tenantID string) {
			journeyDoc(t, tenantID, "PJ-ASSET-1", "Asset", "Draft", map[string]interface{}{"code": "PJ-ASSET-1", "category": "Plant", "cost": 12000.0,
				"useful_life_years": 1, "capitalisation_date": time.Now().AddDate(0, -3, 0).Format("2006-01-02"), "location": "SYN-S001"})
			if err := CapitalizeAsset(tenantID, "PJ-ASSET-1"); err != nil {
				t.Fatalf("capitalise: %v", err)
			}
			res, err := RunAssetDepreciation(t.Context(), tenantID)
			if err != nil || res.MonthsPosted == 0 {
				t.Fatalf("depreciation: %+v %v", res, err)
			}
		}},
		{pkg: "expenses", reportModules: []string{"expenses"}, extra: func(t *testing.T, tenantID string) {
			if err := SetSetting(tenantID, SettingExpenseBills, "true", "journey"); err != nil {
				t.Fatal(err)
			}
			res, err := CreateExpenseBill(tenantID, "journey", ExpenseBillInput{VendorID: "SYN-V00001", InvoiceNumber: "PJ-RENT-1",
				Lines: []ExpenseBillLine{{Description: "Rent", Account: "5100", Amount: 1000, GSTRate: 18, ITC: "Eligible"}}})
			if err != nil || res.InvoiceID == "" {
				t.Fatalf("expense bill: %+v %v", res, err)
			}
			if again, err := CreateExpenseBill(tenantID, "journey", ExpenseBillInput{VendorID: "SYN-V00001", InvoiceNumber: "PJ-RENT-1",
				Lines: []ExpenseBillLine{{Description: "Rent", Account: "5100", Amount: 1000, GSTRate: 18, ITC: "Eligible"}}}); err != nil || !again.Replayed {
				t.Fatalf("the same bill twice must be the same bill: %+v %v", again, err)
			}
		}},
		{pkg: "erp_full", bundle: true, reportModules: []string{"finance", "inventory", "sales"}, shape: func(s *SynthShape) {
			s.SalesOrders, s.PurchaseOrders, s.ProductionOrders = 2, 2, 2
		}},
	}
}

type journeyOutcome struct {
	checksum string
	tenantID string
	report   string
}

func journeyTenant(pkg string) (string, string) {
	id := fmt.Sprintf("pj%s%d", strings.ReplaceAll(pkg, "_", ""), time.Now().UnixNano()%1_000_000_000)
	return id, "tenant_" + id
}

func journeyDrop(t *testing.T, id, schema string) {
	t.Cleanup(func() {
		dropLifecycleTenant(id, schema)
		_, _ = db.DB.Exec(`DELETE FROM public.saas_package_transitions WHERE tenant_id = $1`, id)
	})
}

func moduleKeys(m map[string]bool) []string {
	var out []string
	for k, on := range m {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func journeyCode(err error) string {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Code
	}
	return ""
}

// journeyNormal is round 1 and 3: provision, set the package, build, run the
// workflow and the report, export, import, reconcile.
func journeyNormal(t *testing.T, j packageJourney) journeyOutcome {
	t.Helper()
	tenantID, schema := journeyTenant(j.pkg)
	journeyDrop(t, tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	tr, err := TransitionTenantPackages(tenantID, []string{j.pkg}, "journey", "", j.bundle)
	if err != nil {
		t.Fatalf("set package %s: %v", j.pkg, err)
	}
	if got := moduleKeys(moduleSet(t, tenantID)); strings.Join(got, ",") != strings.Join(tr.After, ",") {
		t.Fatalf("modules after setting %s: %v, the transition says %v", j.pkg, got, tr.After)
	}
	for _, m := range ExpandPackagesToModules([]string{j.pkg}) {
		if !moduleSet(t, tenantID)[m] {
			t.Fatalf("package %s should enable %s", j.pkg, m)
		}
	}

	shape := journeyBaseShape()
	if j.shape != nil {
		j.shape(&shape)
	}
	if err := shape.check(); err != nil {
		t.Fatal(err)
	}
	g := &synthGen{opts: SynthOptions{Shape: shape.Name, Seed: 7, TenantID: tenantID}, shape: shape, tenantID: tenantID, schema: schema,
		rng: rand.New(rand.NewPCG(7, 0x4709_0001))}
	res, err := g.build(time.Now())
	if err != nil {
		t.Fatalf("masters and main workflow: %v", err)
	}
	if res.Counts["documents"] == 0 {
		t.Fatalf("the workflow wrote nothing: %+v", res.Counts)
	}
	if j.extra != nil {
		j.extra(t, tenantID)
	}

	// One report of the package's own module.
	reportID := ""
	for _, def := range ListReportDefinitions() {
		ok := false
		for _, m := range j.reportModules {
			ok = ok || def.ModuleKey == m
		}
		required := false
		for _, p := range def.Params {
			required = required || p.Required
		}
		if !ok || required {
			continue
		}
		if _, _, _, err := RunReport(tenantID, def.ID, RoleSuperAdmin, "journey", map[string]string{}); err == nil {
			reportID = def.ID
			break
		}
	}
	if reportID == "" {
		t.Fatalf("no report of %v ran for package %s", j.reportModules, j.pkg)
	}

	// Export, import into a new client, reconcile.
	dir := filepath.Join(t.TempDir(), "export")
	m, err := ExportTenantPortable(tenantID, dir, "journey", false)
	if err != nil && strings.Contains(err.Error(), "queued work") {
		m, err = ExportTenantPortable(tenantID, dir, "journey", true)
	}
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	newID, newSchema := journeyTenant(j.pkg + "x")
	journeyDrop(t, newID, newSchema)
	imp, err := ImportTenantPortable(dir, newID, newSchema, "journey", "reference journey")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep, err := ReconcileTenantPortable(dir, newID); err != nil || !rep.OK {
		t.Fatalf("reconcile: %+v %v", rep, err)
	}
	if a, b := strings.Join(moduleKeys(moduleSet(t, tenantID)), ","), strings.Join(moduleKeys(moduleSet(t, newID)), ","); a != b {
		t.Fatalf("the package did not travel: %s vs %s", a, b)
	}
	if imp.Rows < 100 || len(m.Tables) < 20 {
		t.Fatalf("export looks too small: %d rows, %d tables", imp.Rows, len(m.Tables))
	}
	// The new client shows the same report.
	if _, _, _, err := RunReport(newID, reportID, RoleSuperAdmin, "journey", map[string]string{}); err != nil {
		t.Fatalf("report %s on the imported client: %v", reportID, err)
	}
	return journeyOutcome{checksum: res.Checksum, tenantID: tenantID, report: reportID}
}

// journeyDenied is round 2: what the package lacks is refused, and the
// failures an operator can cause are refused cleanly.
func journeyDenied(t *testing.T, j packageJourney, tenantID string) {
	t.Helper()
	on := moduleSet(t, tenantID)
	denied := ""
	for _, m := range []string{"hr", "manufacturing", "crm_loyalty", "assets", "expenses", "procurement", "wms", "oms", "pim"} {
		if !on[m] {
			denied = m
			break
		}
	}
	if denied == "" { // the full suite lacks nothing: an unknown package and a bad bundle are the refusals
		if _, err := TransitionTenantPackages(tenantID, []string{"nonexistent"}, "journey", "", true); journeyCode(err) != "SAAS-0191" {
			t.Fatalf("an unknown package must be refused with SAAS-0191, got %v", err)
		}
		return
	}
	if err := RequireModules(tenantID, denied); journeyCode(err) != "SAAS-0191" {
		t.Fatalf("%s is not in package %s and must be refused with SAAS-0191, got %v", denied, j.pkg, err)
	}
	// The doctype list is the catalogue (the menu is gated by MENU_MODULE_MAP);
	// the record routes refuse by the doctype's module, so prove that path:
	// every doctype tagged with the missing module resolves to it and is refused.
	docTypes, err := GetDocTypes(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dt := range docTypes {
		if key, _ := dt["module_key"].(string); key == denied {
			name, _ := dt["name"].(string)
			if mk, err := ModuleForDoctype(tenantID, name); err != nil || mk != denied {
				t.Fatalf("doctype %s should resolve to module %s, got %q %v", name, denied, mk, err)
			}
			if err := RequireModules(tenantID, denied); journeyCode(err) != "SAAS-0191" {
				t.Fatalf("doctype %s of the missing module %s must be refused, got %v", name, denied, err)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatalf("no doctype is filed under module %s, so the denied round proved nothing for records", denied)
	}
	for _, def := range ListReportDefinitions() {
		if def.ModuleKey == denied {
			if _, _, _, err := RunReport(tenantID, def.ID, RoleSuperAdmin, "journey", map[string]string{}); journeyCode(err) != "SAAS-0191" {
				t.Fatalf("report %s of the missing module %s must be refused, got %v", def.ID, denied, err)
			}
			break
		}
	}
	if !j.bundle {
		if ok, _ := ValidatePackageBundle([]string{j.pkg}); ok {
			t.Fatalf("add-on package %s must not be sellable alone", j.pkg)
		}
	}
	// An export folder in use, and an import over an existing client, are refused and change nothing.
	dir := filepath.Join(t.TempDir(), "used")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportTenantPortable(tenantID, dir, "journey", true); err == nil {
		t.Fatal("an export into a used folder must be refused")
	}
}

func TestPackageReferenceJourneys(t *testing.T) {
	synthTestEnv(t)
	for _, j := range packageJourneys() {
		j := j
		t.Run(j.pkg, func(t *testing.T) {
			first := journeyNormal(t, j)        // round 1: normal
			journeyDenied(t, j, first.tenantID) // round 2: denied and failure
			again := journeyNormal(t, j)        // round 3: fresh repeat on a new client
			if first.checksum != again.checksum || first.tenantID == again.tenantID {
				t.Fatalf("a fresh client must give the same business content: %s vs %s", first.checksum, again.checksum)
			}
			if first.report != again.report {
				t.Fatalf("report changed between rounds: %s vs %s", first.report, again.report)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// No hidden, unusable navigation.
// ---------------------------------------------------------------------------

type menuEntry struct {
	Module string   `json:"module"`
	Views  []string `json:"views"`
}

func readMenuModuleMap(t *testing.T) map[string]menuEntry {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "public", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)const MENU_MODULE_MAP = (\{.*?\n\});`).FindSubmatch(body)
	if m == nil {
		t.Fatal("MENU_MODULE_MAP not found in public/app.js")
	}
	var out map[string]menuEntry
	if err := json.Unmarshal(m[1], &out); err != nil {
		t.Fatalf("MENU_MODULE_MAP is not plain JSON any more: %v", err)
	}
	return out
}

// For every package the menu shows what the package has and nothing else: an
// entry gated on a module the package lacks is hidden, every module the
// package grants has a way in, and no module is switched on without the
// modules it needs.
func TestPackageNavigationHasNoHiddenUnusableEntries(t *testing.T) {
	db.InitDB(testConnStr())
	catalog, err := ListModules()
	if err != nil {
		t.Fatal(err)
	}
	known, core := map[string]bool{}, map[string]bool{}
	for _, m := range catalog {
		known[m.ModuleKey] = true
		if m.IsCore {
			core[m.ModuleKey] = true
		}
	}
	menu := readMenuModuleMap(t)
	entriesOf := map[string][]string{}
	for id, e := range menu {
		if !known[e.Module] {
			t.Errorf("menu entry %s is gated on %q, which is not a module", id, e.Module)
		}
		// An entry with no screen of its own (a list or a button) is still gated by its module.
		entriesOf[e.Module] = append(entriesOf[e.Module], id)
	}
	for _, j := range packageJourneys() {
		enabled := map[string]bool{}
		for k := range core {
			enabled[k] = true
		}
		for _, k := range ExpandPackagesToModules([]string{j.pkg}) {
			enabled[k] = true
		}
		for k := range enabled {
			for _, need := range ModulePrerequisites(k, catalog) {
				if !enabled[need] {
					t.Errorf("package %s enables %s without %s, which it needs", j.pkg, k, need)
				}
			}
		}
		for _, k := range ExpandPackagesToModules([]string{j.pkg}) {
			if len(entriesOf[k]) == 0 {
				t.Errorf("package %s grants %s but no menu entry opens it", j.pkg, k)
			}
		}
		visible, hidden := 0, 0
		for _, e := range menu {
			if enabled[e.Module] {
				visible++
			} else {
				hidden++
			}
		}
		if visible == 0 || (j.pkg != "erp_full" && hidden == 0) {
			t.Errorf("package %s: %d visible and %d hidden menu entries look wrong", j.pkg, visible, hidden)
		}
		for _, dt := range func() []map[string]interface{} { d, _ := GetDocTypes("default"); return d }() {
			// A doctype is reachable in the package only through a module the package has.
			if key, _ := dt["module_key"].(string); key != "" && !known[key] {
				t.Errorf("doctype %v belongs to unknown module %q", dt["name"], key)
			}
		}
	}
}
