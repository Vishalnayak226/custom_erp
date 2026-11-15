package engines

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStage4986EveryJobHandlerWatchesItsContext (49.8.6): the worker can walk
// away from a handler that ignores its context, but the goroutine it leaves
// behind keeps running. So every handler registered with RegisterJobHandler
// must name its ctx (pass it on or select on it) - a handler with `_` or an
// unused ctx is a handler nothing can stop. The test parses every non-test
// file in engines/ and checks the body of each registered handler.
func TestStage4986EveryJobHandlerWatchesItsContext(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
	var parsed []*ast.File
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, af)
		for _, d := range af.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	usesCtx := func(ft *ast.FuncType, body *ast.BlockStmt) bool {
		if ft.Params == nil || len(ft.Params.List) == 0 || len(ft.Params.List[0].Names) == 0 {
			return false
		}
		name := ft.Params.List[0].Names[0].Name
		if name == "_" || body == nil {
			return false
		}
		found := false
		ast.Inspect(body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name {
				found = true
			}
			return !found
		})
		return found
	}
	checked := 0
	for _, af := range parsed {
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "RegisterJobHandler" || len(call.Args) != 2 {
				return true
			}
			checked++
			pos := fset.Position(call.Pos())
			switch h := call.Args[1].(type) {
			case *ast.FuncLit:
				if !usesCtx(h.Type, h.Body) {
					t.Errorf("%s: job handler ignores its context", pos)
				}
			case *ast.Ident:
				fd := funcs[h.Name]
				if fd == nil || !usesCtx(fd.Type, fd.Body) {
					t.Errorf("%s: job handler %s ignores its context", pos, h.Name)
				}
			}
			return true
		})
	}
	if checked < 8 {
		t.Fatalf("only %d registered handlers found; the scan is broken", checked)
	}
}
