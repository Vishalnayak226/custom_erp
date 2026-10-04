package server

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// The manifest is a projection of actual declarations, never a second route,
// DocType, report or worker list. Optional explicit output is audit evidence.
type moduleManifestEntry struct {
	Core           bool                `json:"core"`
	Capabilities   []map[string]string `json:"capabilities"`
	Dependencies   []string            `json:"dependencies"`
	DocTypes       []string            `json:"doctypes"`
	RoleVocabulary []string            `json:"role_vocabulary"`
	Routes         []string            `json:"routes"`
	Screens        []string            `json:"screens"`
	Workers        []string            `json:"workers"`
	Reports        []string            `json:"reports"`
	Exports        []string            `json:"exports"`
}

type moduleRouteFact struct {
	Pattern string
	Modules []string
}

func moduleSource(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func moduleRouteFacts(t *testing.T) []moduleRouteFact {
	t.Helper()
	var facts []moduleRouteFact
	gates := regexp.MustCompile(`moduleGate\("([^"]+)"`)
	for _, line := range strings.Split(moduleSource(t, "routes.go"), "\n") {
		if m := routeLinePattern.FindStringSubmatch(line); m != nil {
			fact := moduleRouteFact{Pattern: m[1]}
			for _, g := range gates.FindAllStringSubmatch(line, -1) {
				fact.Modules = append(fact.Modules, g[1])
			}
			facts = append(facts, fact)
		}
	}
	if len(facts) == 0 {
		t.Fatal("route extraction found no production registrations")
	}
	return facts
}

func TestModuleManifestCatalog(t *testing.T) {
	db.InitDB(testConnStr())
	catalog, err := engines.ListModules()
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]*moduleManifestEntry{}
	for _, m := range catalog {
		manifest[m.ModuleKey] = &moduleManifestEntry{Core: m.IsCore, Dependencies: engines.ModulePrerequisites(m.ModuleKey, catalog)}
	}
	var register struct {
		SchemaVersion int `json:"schema_version"`
		Release       string
		Owner         string
		Capabilities  []struct {
			ID         string
			Owner      string
			ModuleKeys []string `json:"module_keys"`
		}
	}
	if err := json.Unmarshal([]byte(moduleSource(t, "../../docs/product/capability-register.json")), &register); err != nil {
		t.Fatal(err)
	}
	if register.SchemaVersion < 3 || register.Release == "" || register.Owner == "" {
		t.Fatal("module mapping needs schema version, release and owner")
	}
	for _, cap := range register.Capabilities {
		if cap.Owner == "" || len(cap.ModuleKeys) == 0 {
			t.Errorf("capability %s lacks owner/module mapping", cap.ID)
		}
		for _, key := range cap.ModuleKeys {
			entry, ok := manifest[key]
			if !ok {
				t.Errorf("capability %s references unknown module %s", cap.ID, key)
				continue
			}
			entry.Capabilities = append(entry.Capabilities, map[string]string{"id": cap.ID, "owner": cap.Owner, "version": register.Release})
		}
	}
	for key, entry := range manifest {
		if len(entry.Capabilities) == 0 {
			t.Errorf("module %s lacks owned/versioned capability mapping", key)
		}
	}
	meta, err := engines.GetDocTypes("default")
	if err != nil {
		t.Fatal(err)
	}
	roleVocabulary := map[string]bool{}
	for _, template := range engines.RoleTemplates() {
		for vocabulary := range template.Modules {
			roleVocabulary[vocabulary] = true
		}
	}
	for _, dt := range meta {
		name, _ := dt["name"].(string)
		key, _ := dt["module_key"].(string)
		vocabulary, _ := dt["module"].(string)
		entry, ok := manifest[key]
		if !ok {
			t.Errorf("DocType %s references unknown module %q", name, key)
			continue
		}
		if !roleVocabulary[vocabulary] {
			t.Errorf("DocType %s uses unknown role vocabulary %s", name, vocabulary)
		}
		entry.DocTypes = append(entry.DocTypes, name)
		entry.RoleVocabulary = append(entry.RoleVocabulary, vocabulary)
	}
	for _, fact := range moduleRouteFacts(t) {
		classification, ok := routeCapabilities[fact.Pattern]
		if !ok {
			t.Errorf("unclassified route %s", fact.Pattern)
			continue
		}
		namespace, _, _ := strings.Cut(classification.Capability, ".")
		if entry, known := manifest[namespace]; known && !entry.Core {
			found := false
			for _, key := range fact.Modules {
				found = found || key == namespace
			}
			if !found {
				t.Errorf("optional capability %s route %s lacks moduleGate(%q)", classification.Capability, fact.Pattern, namespace)
			}
		}
		if len(fact.Modules) == 0 { // platform/dynamic routes resolve at their own shared boundary
			key := "core"
			if e := manifest[namespace]; e != nil && e.Core {
				key = namespace
			}
			manifest[key].Routes = append(manifest[key].Routes, fact.Pattern)
		}
		for _, key := range fact.Modules {
			if entry := manifest[key]; entry != nil {
				entry.Routes = append(entry.Routes, fact.Pattern)
			} else {
				t.Errorf("route %s uses unknown gate %s", fact.Pattern, key)
			}
		}
	}
	for _, route := range publicAPIV1Routes() {
		if entry := manifest[route.ModuleKey]; entry != nil {
			entry.Routes = append(entry.Routes, route.Method+" "+route.Path)
		} else {
			t.Errorf("public route %s lacks valid module", route.Path)
		}
	}
	for _, def := range engines.ListReportDefinitions() {
		entry := manifest[def.ModuleKey]
		if entry == nil {
			t.Errorf("report %s has unknown module %s", def.ID, def.ModuleKey)
			continue
		}
		entry.Reports = append(entry.Reports, def.ID)
		entry.Exports = append(entry.Exports, def.ID)
	}
	app := moduleSource(t, "../../public/app.js")
	m := regexp.MustCompile(`(?s)const MENU_MODULE_MAP = (\{.*?\n\});`).FindStringSubmatch(app)
	if m == nil {
		t.Fatal("navigation registry extraction failed")
	}
	var navigation map[string]struct {
		Module string
		Views  []string
	}
	if err := json.Unmarshal([]byte(m[1]), &navigation); err != nil {
		t.Fatal(err)
	}
	screens := map[string]int{}
	for id, item := range navigation {
		entry := manifest[item.Module]
		if entry == nil {
			t.Errorf("navigation %s references unknown module %s", id, item.Module)
			continue
		}
		for _, view := range item.Views {
			screens[view]++
			entry.Screens = append(entry.Screens, view)
		}
	}
	dispatch := strings.Split(strings.Split(app, "async function renderViewContent(view, root) {")[1], "// Translate labels")[0]
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`view === '([^']+)'`).FindAllStringSubmatch(dispatch, -1) {
		seen[m[1]] = true
	}
	// BLD-041: most screens are no longer literal `view === '...'` branches -
	// they dispatch through the LAZY_VIEW_MODULES lookup table (native
	// import() per view module) instead, so every key declared there counts
	// as dispatched too.
	lazy := regexp.MustCompile(`(?s)const LAZY_VIEW_MODULES = (\{.*?\n\});`).FindStringSubmatch(app)
	if lazy == nil {
		t.Fatal("lazy view module registry extraction failed")
	}
	for _, m := range regexp.MustCompile(`(?m)^\s*'?([\w-]+)'?:\s*\[`).FindAllStringSubmatch(lazy[1], -1) {
		seen[m[1]] = true
	}
	for view := range seen {
		if screens[view] != 1 {
			t.Errorf("dispatched view %s has %d module declarations", view, screens[view])
		}
	}
	for view := range screens {
		if !seen[view] {
			t.Errorf("stale module view %s", view)
		}
	}
	for worker, keys := range moduleWorkerFacts(t) {
		for _, key := range keys {
			if entry := manifest[key]; entry != nil {
				entry.Workers = append(entry.Workers, worker)
			} else {
				t.Errorf("worker %s references unknown module %s", worker, key)
			}
		}
	}
	for _, entry := range manifest {
		for _, list := range []*[]string{&entry.DocTypes, &entry.RoleVocabulary, &entry.Routes, &entry.Screens, &entry.Workers, &entry.Reports, &entry.Exports} {
			sort.Strings(*list)
			*list = uniqueModuleStrings(*list)
		}
	}
	if out := os.Getenv("BLD021_EVIDENCE_DIR"); out != "" {
		if !filepath.IsAbs(out) {
			t.Fatal("BLD021_EVIDENCE_DIR must be absolute")
		}
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		body, err := json.MarshalIndent(map[string]interface{}{"schema_version": 1, "capability_schema_version": register.SchemaVersion, "version": register.Release, "owner": register.Owner, "modules": manifest}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "module-manifest.json"), append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("derived %d modules, %d capabilities, %d DocTypes, %d authenticated routes, %d public routes, %d screens, %d reports", len(manifest), len(register.Capabilities), len(meta), len(moduleRouteFacts(t)), len(publicAPIV1Routes()), len(seen), len(engines.ListReportDefinitions()))
}

func uniqueModuleStrings(in []string) []string {
	out := []string{}
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

// Follow actual local worker calls to their tenant enumeration. Global
// maintenance without tenant enumeration belongs to the core process.
func moduleWorkerFacts(t *testing.T) map[string][]string {
	t.Helper()
	functions := map[string]*ast.FuncDecl{}
	paths, err := filepath.Glob("../../engines/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
				functions[fn.Name.Name] = fn
			}
		}
	}
	var resolve func(string, map[string]bool) []string
	resolve = func(name string, visited map[string]bool) []string {
		if visited[name] {
			return nil
		}
		visited[name] = true
		fn := functions[name]
		if fn == nil {
			return nil
		}
		var modules, calls []string
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if id.Name == "listTenantSchemas" {
				if len(call.Args) == 0 {
					modules = append(modules, "core")
				}
				for _, arg := range call.Args {
					lit, ok := arg.(*ast.BasicLit)
					if !ok {
						t.Errorf("worker %s has nonliteral tenant-module filter", name)
						continue
					}
					key, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					modules = append(modules, key)
				}
			} else {
				calls = append(calls, id.Name)
			}
			return true
		})
		if len(modules) > 0 {
			return modules
		}
		for _, name := range calls {
			modules = append(modules, resolve(name, visited)...)
		}
		return modules
	}
	workers := map[string][]string{}
	for _, m := range regexp.MustCompile(`engines\.(Start\w+)\(workerCtx,`).FindAllStringSubmatch(moduleSource(t, "routes.go"), -1) {
		if functions[m[1]] == nil {
			t.Errorf("worker declaration missing: %s", m[1])
		}
		keys := resolve(m[1], map[string]bool{})
		if len(keys) == 0 {
			keys = []string{"core"}
		}
		sort.Strings(keys)
		workers[m[1]] = uniqueModuleStrings(keys)
	}
	if len(workers) == 0 {
		t.Fatal("worker source extraction found nothing")
	}
	return workers
}

func moduleHTTPFixture(t *testing.T) (http.Handler, string) {
	t.Helper()
	db.InitDB(testConnStr())
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })
	id, cleanup := seedStage47User(t, engines.RoleSuperAdmin, "")
	t.Cleanup(cleanup)
	return mux, stage47Token(id, engines.RoleSuperAdmin, "")
}

func moduleRequest(t *testing.T, handler http.Handler, token, method, path string, seq int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	// Distinct local peers keep this entitlement campaign separate from the
	// rate-limit campaign; every request still executes the real limiter.
	req.RemoteAddr = fmt.Sprintf("198.18.%d.%d:12345", seq/250, seq%250+1)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestModuleManifestEveryDeclaredHTTPGate(t *testing.T) {
	handler, token := moduleHTTPFixture(t)
	before := snapshotEntitlements(t, "tenant_default")
	t.Cleanup(func() { restoreEntitlements("tenant_default", before) })
	if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=false WHERE module_key IN (SELECT module_key FROM public.modules WHERE NOT is_core)`); err != nil {
		t.Fatal(err)
	}
	params := regexp.MustCompile(`\{[^}]+\}`)
	count := 0
	for _, fact := range moduleRouteFacts(t) {
		optional := false
		for _, key := range fact.Modules {
			var core bool
			if err := db.DB.QueryRow(`SELECT is_core FROM public.modules WHERE module_key=$1`, key).Scan(&core); err != nil {
				t.Fatal(err)
			}
			optional = optional || !core
		}
		if !optional {
			continue
		}
		method, path, found := strings.Cut(fact.Pattern, " ")
		if !found {
			method = "GET"
			path = fact.Pattern
		}
		// A valid core DocType lets the declared route gate decide this case;
		// unknown and disabled DocTypes are tested separately below.
		path = strings.ReplaceAll(path, "{doctype}", "Item")
		path = params.ReplaceAllString(path, "__module_probe__")
		r := moduleRequest(t, handler, token, method, path, count)
		count++
		if r.Code != http.StatusForbidden || !strings.Contains(r.Body.String(), "SAAS-0191") {
			t.Errorf("disabled module route %s = %d %s", fact.Pattern, r.Code, r.Body.String())
		}
	}
	if count == 0 {
		t.Fatal("no module-gated routes exercised")
	}
	t.Logf("%d actual registered HTTP routes denied disabled modules", count)
	unknown := moduleRequest(t, handler, token, "GET", "/api/v1/__unknown_module__/probe", count+1)
	if unknown.Code != http.StatusNotFound {
		t.Errorf("unknown module path status=%d", unknown.Code)
	}
}

func TestModuleManifestGenericEntryPoints(t *testing.T) {
	handler, token := moduleHTTPFixture(t)
	before := snapshotEntitlements(t, "tenant_default")
	t.Cleanup(func() { restoreEntitlements("tenant_default", before) })
	meta, err := engines.GetDocTypes("default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=false WHERE module_key IN (SELECT module_key FROM public.modules WHERE NOT is_core)`); err != nil {
		t.Fatal(err)
	}
	facts := moduleRouteFacts(t)
	seq := 0
	count := 0
	for _, dt := range meta {
		key, _ := dt["module_key"].(string)
		var core bool
		if err := db.DB.QueryRow(`SELECT is_core FROM public.modules WHERE module_key=$1`, key).Scan(&core); err != nil {
			t.Fatal(err)
		}
		if core {
			continue
		}
		name := dt["name"].(string)
		for _, fact := range facts {
			if !strings.Contains(fact.Pattern, "{doctype}") {
				continue
			}
			method, path, found := strings.Cut(fact.Pattern, " ")
			if !found {
				method = "GET"
				path = fact.Pattern
			}
			path = strings.ReplaceAll(path, "{doctype}", name)
			path = strings.ReplaceAll(path, "{id}", "__module_probe__")
			r := moduleRequest(t, handler, token, method, path, seq)
			seq++
			count++
			if r.Code != 403 || !strings.Contains(r.Body.String(), "SAAS-0191") {
				t.Errorf("%s %s = %d %s", method, path, r.Code, r.Body.String())
			}
		}
	}
	r := moduleRequest(t, handler, token, "GET", "/api/v1/doc/__unknown_type__", seq)
	if r.Code != 404 {
		t.Errorf("unknown doctype status=%d body=%s", r.Code, r.Body.String())
	}
	t.Logf("%d generic DocType/metadata/import/reactivation requests denied", count)
}

func TestModuleManifestPublicCredentialRoutes(t *testing.T) {
	handler, _ := moduleHTTPFixture(t)
	before := snapshotEntitlements(t, "tenant_default")
	t.Cleanup(func() { restoreEntitlements("tenant_default", before) })
	issued, err := engines.IssueAPICredential("default", "BLD-021 boundary fixture", []string{"items:read", "inventory:read", "orders:read"}, nil, "system")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id=$1`, issued.Credential.ID)
		if err != nil {
			t.Error(err)
		}
	})
	params := regexp.MustCompile(`\{[^}]+\}`)
	for i, route := range publicAPIV1Routes() {
		path := params.ReplaceAllString(route.Path, "__module_probe__")
		for _, enabled := range []bool{false, true} {
			if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$1 WHERE module_key=$2`, enabled, route.ModuleKey); err != nil {
				t.Fatal(err)
			}
			r := moduleRequest(t, handler, issued.APIKey, route.Method, path, 2000+i)
			if !enabled && (r.Code != 403 || !strings.Contains(r.Body.String(), "SAAS-0191")) {
				t.Errorf("disabled public route %s: %d %s", route.Path, r.Code, r.Body.String())
			}
			if enabled && (r.Code == 401 || r.Code == 403 || r.Code >= 500) {
				t.Errorf("enabled public route rejected: %s %d %s", route.Path, r.Code, r.Body.String())
			}
			if enabled && route.Path == "/api/public/v1/items" && r.Code != 200 {
				t.Errorf("enabled item list: %d %s", r.Code, r.Body.String())
			}
		}
	}
	if r := moduleRequest(t, handler, issued.APIKey, "GET", "/api/public/v1/__unknown_module__", 2010); r.Code != 404 {
		t.Errorf("unknown public path: %d", r.Code)
	}
}

func TestModuleManifestReportHTTPAndCatalogs(t *testing.T) {
	handler, token := moduleHTTPFixture(t)
	before := snapshotEntitlements(t, "tenant_default")
	t.Cleanup(func() { restoreEntitlements("tenant_default", before) })
	if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=true`); err != nil {
		t.Fatal(err)
	}
	job, err := engines.CreateReportExportJob("default", "attendance-summary", engines.RoleSuperAdmin, map[string]string{}, "system")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype='ReportExportJob' AND id=$1`, job)
	})
	// Model an already completed export to check that download revalidates
	// entitlement, rather than leaking the bytes saved before a disable.
	if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET status='Completed', data=data||'{"csv":"private attendance"}'::jsonb WHERE id=$1 AND doctype='ReportExportJob'`, job); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$1 WHERE module_key='hr'`, enabled); err != nil {
			t.Fatal(err)
		}
		for _, endpoint := range []struct{ path, match string }{{"/api/v1/reports/catalog", "attendance-summary"}, {"/api/v1/meta/doctypes", "Employee"}} {
			r := moduleRequest(t, handler, token, "GET", endpoint.path, 2100)
			if r.Code != 200 || strings.Contains(r.Body.String(), endpoint.match) != enabled {
				t.Errorf("catalog %s enabled=%v: %d %s", endpoint.path, enabled, r.Code, r.Body.String())
			}
		}
		for _, path := range []string{"/api/v1/doc/Employee", "/api/v1/reports/run/attendance-summary", "/api/v1/reports/export/" + job + "?download=1"} {
			r := moduleRequest(t, handler, token, "GET", path, 2101)
			if enabled && r.Code != 200 {
				t.Errorf("enabled %s: %d %s", path, r.Code, r.Body.String())
			}
			if !enabled && (r.Code != 403 || !strings.Contains(r.Body.String(), "SAAS-0191")) {
				t.Errorf("disabled %s: %d %s", path, r.Code, r.Body.String())
			}
		}
	}
	for i, path := range []string{"/api/v1/doc/ReportExportJob", "/api/v1/doc/ReportExportJob/" + job} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			r := moduleRequest(t, handler, token, method, path, 2200+i)
			if r.Code != 403 {
				t.Errorf("generic export %s %s returned %d", method, path, r.Code)
			}
		}
	}
	request := httptest.NewRequest("POST", "/api/v1/reports/export", strings.NewReader(`{"report_id":"attendance-summary"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.RemoteAddr = "198.18.50.1:12345"
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, request)
	if r.Code != 403 || !strings.Contains(r.Body.String(), "SAAS-0191") {
		t.Errorf("disabled HTTP export creation: %d %s", r.Code, r.Body.String())
	}
}
