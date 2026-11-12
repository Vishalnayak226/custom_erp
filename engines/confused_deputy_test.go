package engines

// Stage 49.3.2 - no confused-deputy path. Background work acts for an explicit
// client. Two ways it used to fall back to the default client:
//
//   - a job handler read the client from its payload, and a payload with none
//     gave "" - which db.GetTenantSchema answers with tenant_default;
//   - a sweep turned a schema back into a client with schemaToTenantID, and a
//     failed lookup gave "" the same way.
//
// The runtime test proves the job runner now binds a job to the client whose
// schema holds it. The static test keeps both shapes from coming back: every
// "empty client means the default one" fallback in engines, internal/server
// and db is listed below with its reason, and no schema-to-client result is
// used without checking it.

import (
	"context"
	"custom_erp/db"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

const stage4932JobType = "test-4932-tenant"

var stage4932Seen = struct {
	sync.Mutex
	byJob map[string]string
}{byJob: map[string]string{}}

func TestStage4932JobActsForTheClientItIsQueuedFor(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, ok := jobHandlers[stage4932JobType]; !ok {
		RegisterJobHandler(stage4932JobType, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
			named, _ := job.Payload["tenant_id"].(string)
			stage4932Seen.Lock()
			stage4932Seen.byJob[job.ID] = named
			stage4932Seen.Unlock()
			return map[string]interface{}{"ok": true}, nil
		})
	}

	unnamed, err := EnqueueJob(tenantID, stage4932JobType, map[string]interface{}{"n": 1}, "")
	if err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}
	other, err := EnqueueJob(tenantID, stage4932JobType, map[string]interface{}{"tenant_id": "default"}, "")
	if err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}
	own, err := EnqueueJob(tenantID, stage4932JobType, map[string]interface{}{"tenant_id": tenantID}, "")
	if err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}
	runClaimedJobs(schema)

	stage4932Seen.Lock()
	defer stage4932Seen.Unlock()
	if got, ran := stage4932Seen.byJob[unnamed]; !ran || got != tenantID {
		t.Fatalf("a job with no client in its payload ran=%v for %q, want it run for %q (its queue's client), never the default", ran, got, tenantID)
	}
	if got, ran := stage4932Seen.byJob[own]; !ran || got != tenantID {
		t.Fatalf("a job naming its own client ran=%v for %q", ran, got)
	}
	if _, ran := stage4932Seen.byJob[other]; ran {
		t.Fatal("a job queued for this client but naming the default client was run")
	}
	var status, lastError string
	if err := db.DB.QueryRow("SELECT status, COALESCE(last_error, '') FROM "+schema+".async_jobs WHERE id = $1", other).Scan(&status, &lastError); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if status == "Succeeded" || !strings.Contains(lastError, "not run") {
		t.Fatalf("the refused job: status %s, last_error %q", status, lastError)
	}
}

func TestStage4932SchemaToTenantIDNeverMeansTheDefaultClient(t *testing.T) {
	db.InitDB(testConnStr())
	if got := schemaToTenantID("tenant_default"); got != "default" {
		t.Fatalf("schemaToTenantID(tenant_default) = %q, want default", got)
	}
	if got := schemaToTenantID("tenant_not_registered_4932"); got != "" {
		t.Fatalf("an unregistered schema resolved to %q", got)
	}
	if _, err := runAuditCheckpointJob(context.Background(), "tenant_not_registered_4932", Job{}); err == nil {
		t.Fatal("an audit checkpoint job for an unregistered schema ran (it would have checkpointed the default client)")
	}
}

// defaultClientFallbacks are the only functions allowed to read an empty (or
// unknown) client as the default one. Each acts on nothing a job, hook or
// sweep brought in, or is the open decision named in its reason.
var defaultClientFallbacks = map[string]string{
	"db/db.go:GetTenantSchema":                                              `"" and "default" are the default client's own id; an unregistered id answers ErrUnknownTenant (49.3.2a)`,
	"engines/bounded_state.go:tenantLegalHold":                              "reads the default client's legal-hold flag for its own empty id; acts on nothing",
	"engines/platform_console.go:IsPlatformTenant":                          "a predicate: with no operator named, the default client is the platform's",
	"engines/tenant_lifecycle.go:InvalidateTenantLifecycleCache":            "cache key normalisation only",
	"engines/tenant_lifecycle.go:tenantHasCachedGate":                       "cache key normalisation only",
	"internal/server/apierror.go:resolvedContext":                           "the tenant a log line is filed under before apiMiddleware resolved one",
	"internal/server/middleware.go:apiMiddleware":                           "pre-auth: a request naming no client is the default client's; a token's own claim always overrides it; the panic log",
	"internal/server/handlers_integrations_admin.go:handleGetTenantVersion": "operator screen: no client named means the operator's own; an unknown one answers 404",
	"internal/server/handlers_oauth_token.go:handleOAuthToken":              "the request-log row of a token call that names no client",
	"internal/server/middleware_public_api.go:publicAPIMiddleware":          "the request-log row of an unauthenticated public-API call that names no client",
}

// schemaToClient are the helpers that turn a schema back into a client id; ""
// or an error from them means "no client", which must never reach code that
// would read it as the default client.
var schemaToClient = map[string]bool{"schemaToTenantID": true, "tenantIDForSchema": true}

func TestStage4932NoSilentDefaultClient(t *testing.T) {
	type source struct{ dir, prefix string }
	sources := []source{{".", "engines"}, {"../internal/server", "internal/server"}, {"../db", "db"}}
	fset := token.NewFileSet()
	found := map[string]bool{}
	var problems []string
	for _, src := range sources {
		files, _ := filepath.Glob(filepath.Join(src.dir, "*.go"))
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(body)
			if !strings.Contains(text, `"default"`) && !strings.Contains(text, `"tenant_default"`) &&
				!strings.Contains(text, "schemaToTenantID(") && !strings.Contains(text, "tenantIDForSchema(") {
				continue
			}
			file, err := parser.ParseFile(fset, path, body, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			rel := src.prefix + "/" + filepath.Base(path)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				key := rel + ":" + fn.Name.Name
				if fallsBackToDefaultClient(fn.Body) {
					found[key] = true
					if _, allowed := defaultClientFallbacks[key]; !allowed {
						problems = append(problems, key+" reads an empty client as the default one - make the caller carry its client, or add it to defaultClientFallbacks with the reason it is safe")
					}
				}
				if fn.Name.Name == "schemaToTenantID" {
					continue // the helper itself
				}
				problems = append(problems, uncheckedSchemaToClient(fset, key, fn.Body)...)
			}
		}
	}
	var stale []string
	for key := range defaultClientFallbacks {
		if !found[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		problems = append(problems, fmt.Sprintf("listed in defaultClientFallbacks but no longer falling back: %v", stale))
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// fallsBackToDefaultClient reports an assignment of "default" to a variable
// named for a tenant, or a "tenant_default" result returned from a branch.
func fallsBackToDefaultClient(body *ast.BlockStmt) bool {
	hit := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				if i < len(s.Rhs) && isTenantName(lhs) && isStringLit(s.Rhs[i], "default") {
					hit = true
				}
			}
		case *ast.ReturnStmt:
			if len(s.Results) > 0 && isStringLit(s.Results[0], "tenant_default") {
				hit = true
			}
		}
		return !hit
	})
	return hit
}

// uncheckedSchemaToClient finds schema-to-client results that are passed on
// inline, assigned and never compared with "", or whose error is discarded.
func uncheckedSchemaToClient(fset *token.FileSet, key string, body *ast.BlockStmt) []string {
	var out []string
	checked := map[string]bool{} // identifiers compared with ""
	ast.Inspect(body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BinaryExpr); ok && (b.Op == token.EQL || b.Op == token.NEQ) {
			for _, side := range [][2]ast.Expr{{b.X, b.Y}, {b.Y, b.X}} {
				if id, ok := side[0].(*ast.Ident); ok && isStringLit(side[1], "") {
					checked[id.Name] = true
				}
			}
		}
		return true
	})
	isHelperCall := func(e ast.Expr) (string, bool) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || !schemaToClient[id.Name] {
			return "", false
		}
		return id.Name, true
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.CallExpr:
			for _, arg := range s.Args {
				if name, ok := isHelperCall(arg); ok {
					out = append(out, fmt.Sprintf("%s (%s): %s(...) is passed on unchecked - \"\" would mean the default client", key, fset.Position(arg.Pos()), name))
				}
			}
		case *ast.AssignStmt:
			if len(s.Rhs) != 1 {
				return true
			}
			name, ok := isHelperCall(s.Rhs[0])
			if !ok {
				return true
			}
			pos := fset.Position(s.Pos())
			id, ok := s.Lhs[0].(*ast.Ident)
			if !ok || checked[id.Name] {
				return true // the id itself is compared with ""
			}
			if name == "tenantIDForSchema" && len(s.Lhs) == 2 {
				if errID, ok := s.Lhs[1].(*ast.Ident); ok && errID.Name != "_" {
					return true // its error is kept
				}
			}
			out = append(out, fmt.Sprintf("%s (%s): %s from %s is used unchecked - \"\" would mean the default client", key, pos, id.Name, name))
		}
		return true
	})
	return out
}

func isTenantName(e ast.Expr) bool {
	var name string
	switch v := e.(type) {
	case *ast.Ident:
		name = v.Name
	case *ast.SelectorExpr:
		name = v.Sel.Name
	default:
		return false
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "tenant") && !strings.Contains(lower, "schema")
}

func isStringLit(e ast.Expr, want string) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == fmt.Sprintf("%q", want)
}

// 49.3.2a (user, 2026-10-28: "Fix it"): an unregistered client id is never the
// default client - not in the registry lookup every caller goes through.
func TestStage4932aUnknownClientIDIsRefused(t *testing.T) {
	db.InitDB(testConnStr())
	for _, id := range []string{"", "default"} {
		if schema, err := db.GetTenantSchema(id); err != nil || schema != "tenant_default" {
			t.Errorf("GetTenantSchema(%q) = %q, %v; want tenant_default", id, schema, err)
		}
	}
	for _, id := range []string{"t4932a-no-such-client", "tenant_default", "Default"} {
		if schema, err := db.GetTenantSchema(id); err != db.ErrUnknownTenant || schema != "" {
			t.Errorf("GetTenantSchema(%q) = %q, %v; want ErrUnknownTenant", id, schema, err)
		}
	}
}
