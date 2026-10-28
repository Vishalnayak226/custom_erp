package engines

// Stage 49.18.6 - security cost cannot be "optimized away". Three guards over
// the source, so a speed-up that weakens a control fails review instead of
// shipping: password hashing never drops below bcrypt's default cost, TLS
// verification is never switched off, and every in-memory cache has been
// reviewed for what it is keyed by - a cache that could serve one tenant's,
// user's or store scope's answer to another is the classic optimization leak.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// reviewedCaches is every package-level cache and what its key carries. A new
// cache fails the test until it is listed here with that answer.
var reviewedCaches = map[string]string{
	"authStateCache":         "tenant + user",
	"tenantGateCache":        "tenant",
	"datasetCache":           "tenant + store scope + money visibility + query",
	"itemTypeCache":          "tenant",
	"settingsCache":          "tenant schema + setting key",
	"atsPolicyCache":         "tenant schema",
	"crmZoneCache":           "time-zone name only; holds no tenant data",
	"crmJourneyVersionCache": "tenant schema + journey + version (versions never change)",
	"imageDriveCache":        "tenant",
	"doctypeModuleCache":     "tenant schema + doctype; module mapping changes only in a migration (49.18.8)",
	"tenantHostCache":        "hostname -> tenant; the tenant is the answer, re-checked by the session",
}

func securitySourceFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{"*.go", filepath.Join("..", "internal", "server", "*.go"), filepath.Join("..", "cmd", "*", "*.go")} {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			if !strings.HasSuffix(f, "_test.go") {
				out = append(out, f)
			}
		}
	}
	return out
}

func TestStage49186SecurityCostIsNotOptimizedAway(t *testing.T) {
	fset := token.NewFileSet()
	caches := map[string]string{}
	for _, f := range securitySourceFiles(t) {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		// Package-level caches.
		for _, d := range file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if strings.HasSuffix(name.Name, "Cache") {
						caches[name.Name] = f
					}
				}
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "GenerateFromPassword" || len(x.Args) != 2 {
					return true
				}
				cost, ok := x.Args[1].(*ast.SelectorExpr)
				if !ok || cost.Sel.Name != "DefaultCost" {
					t.Errorf("%s: bcrypt.GenerateFromPassword with cost %s - use bcrypt.DefaultCost or higher, never a cheaper hash for speed", fset.Position(x.Pos()), exprString(x.Args[1]))
				}
			case *ast.KeyValueExpr:
				if k, ok := x.Key.(*ast.Ident); ok && k.Name == "InsecureSkipVerify" {
					if v, ok := x.Value.(*ast.Ident); !ok || v.Name != "false" {
						t.Errorf("%s: InsecureSkipVerify must not be set - TLS verification is not optional", fset.Position(x.Pos()))
					}
				}
			}
			return true
		})
	}
	var unreviewed []string
	for name, f := range caches {
		if _, ok := reviewedCaches[name]; !ok {
			unreviewed = append(unreviewed, name+" ("+f+")")
		}
	}
	sort.Strings(unreviewed)
	for _, u := range unreviewed {
		t.Errorf("cache %s is not reviewed - add it to reviewedCaches with what its key carries; it must never serve one tenant, user or scope's answer to another", u)
	}
	for name := range reviewedCaches {
		if _, ok := caches[name]; !ok {
			t.Errorf("reviewedCaches lists %s, which no longer exists", name)
		}
	}
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Value
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	}
	return "an expression"
}
