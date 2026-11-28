package securityscan

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
)

// Item 322.1 - no query splices a value into SQL text.
//
// Parameterized queries ($1, $2, ...) are this codebase's rule, and until this
// scanner nothing enforced it: a new query that wrote a user's value into the
// SQL text would pass every test. The scanner finds the shape that does that, a
// hole inside a single-quoted SQL string literal:
//
//   - a fmt.Sprintf / fmt.Fprintf verb inside quotes, in a format that reads as SQL:
//     fmt.Sprintf(`... WHERE code = '%s'`, code)
//   - a non-literal operand of a + chain that lands inside quotes:
//     "... WHERE code = '" + code + "'"
//
// Every hit in the tree is listed in sql_splice_test.go with the reason it is
// safe (most are JSON key names written as Go constants). A new hit, or one
// more in a listed file, fails the build.
//
// Not reported: an unquoted %s. The code uses it for the schema (validated
// once in db.GetTenantSchema), table and column names, $n placeholders and
// SQL fragments built in code. Telling those from a raw value needs data flow,
// which a parser does not have, so that part stays with review. A value
// written unquoted is still a syntax error for any text that is not a number,
// which is why the quoted shape is the one injection actually takes.
//
// Also not seen: a quote written in a string with no SQL in it, such as
// "'" + v + "'" passed on to be placed in a query later.

// SQLSplice is one hole inside a quoted SQL literal.
type SQLSplice struct {
	File    string
	Line    int
	Func    string
	Excerpt string
}

// sqlShape decides whether a string reads as SQL. Case-sensitive on purpose:
// this code writes SQL keywords in capitals, and messages in sentence case.
var sqlShape = regexp.MustCompile(`\b(SELECT|INSERT INTO|UPDATE|DELETE FROM|WHERE|VALUES|ON CONFLICT|JOIN|COALESCE|FILTER|AND|OR)\b|\bIN \(|->>|::(text|numeric|date|jsonb|int|bigint|timestamp)`)

// ScanSQLSplices parses every non-test Go file under root and returns each
// hole, sorted by file then line.
func ScanSQLSplices(root string) ([]SQLSplice, error) {
	var out []SQLSplice
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if skippedDirs[fi.Name()] || fi.Name() == "testdata" || fi.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		hits, err := sqlSplicesInSource(filepath.ToSlash(rel), src)
		if err != nil {
			return nil // a file that does not parse fails go build, not this scan
		}
		out = append(out, hits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// CountSQLSplicesByFile is the shape the reviewed list is kept in.
func CountSQLSplicesByFile(hits []SQLSplice) map[string]int {
	counts := map[string]int{}
	for _, h := range hits {
		counts[h.File]++
	}
	return counts
}

func sqlSplicesInSource(rel string, src []byte) ([]SQLSplice, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	var out []SQLSplice
	report := func(line int, fn string) {
		excerpt := ""
		if line-1 < len(lines) {
			excerpt = strings.TrimSpace(lines[line-1])
			if len(excerpt) > 160 {
				excerpt = excerpt[:160] + "..."
			}
		}
		out = append(out, SQLSplice{File: rel, Line: line, Func: fn, Excerpt: excerpt})
	}

	for _, decl := range file.Decls {
		fn := "-"
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				fn = receiverName(fd.Recv.List[0].Type) + "." + fn
			}
		}
		inChain := map[ast.Node]bool{} // + nodes already read as part of a longer chain
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				format, ok := formatArg(x)
				if !ok {
					return true
				}
				text, ok := constString(format)
				if !ok || !sqlShape.MatchString(text) {
					return true
				}
				// A raw string's text is its source, so a hole's line is the
				// literal's line plus the newlines before it.
				start := fset.Position(format.Pos()).Line
				lit, single := format.(*ast.BasicLit)
				raw := single && strings.HasPrefix(lit.Value, "`")
				for _, h := range quotedHoles(formatParts(text)) {
					line := start
					if raw {
						line += strings.Count(text[:h.off], "\n")
					}
					report(line, fn)
				}
			case *ast.BinaryExpr:
				if x.Op != token.ADD || inChain[x] {
					return true
				}
				operands := flattenAdd(x, inChain)
				var parts []splicePart
				var literal strings.Builder
				hasLiteral := false
				for _, op := range operands {
					if s, ok := constString(op); ok {
						parts = append(parts, splicePart{text: s})
						literal.WriteString(s)
						hasLiteral = true
						continue
					}
					parts = append(parts, splicePart{hole: true, line: fset.Position(op.Pos()).Line})
				}
				if !hasLiteral || !sqlShape.MatchString(literal.String()) {
					return true
				}
				for _, h := range quotedHoles(parts) {
					report(h.line, fn)
				}
			}
			return true
		})
	}
	return out, nil
}

// formatArg returns the format argument of fmt.Sprintf / fmt.Fprintf.
func formatArg(call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Sprintf":
		if len(call.Args) > 0 {
			return call.Args[0], true
		}
	case "Fprintf":
		if len(call.Args) > 1 {
			return call.Args[1], true
		}
	}
	return nil, false
}

// constString evaluates a string literal, or a + of string literals.
func constString(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return constString(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok := constString(x.X)
		if !ok {
			return "", false
		}
		r, ok := constString(x.Y)
		return l + r, ok
	}
	return "", false
}

// flattenAdd lists the operands of a left-nested + chain and marks its inner
// + nodes so they are not read again as chains of their own.
func flattenAdd(e *ast.BinaryExpr, seen map[ast.Node]bool) []ast.Expr {
	var out []ast.Expr
	var walk func(ast.Expr)
	walk = func(x ast.Expr) {
		if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			seen[b] = true
			walk(b.X)
			walk(b.Y)
			return
		}
		out = append(out, x)
	}
	walk(e)
	return out
}

func receiverName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return receiverName(x.X)
	}
	return "?"
}

// splicePart is literal SQL text or a hole a value is written into. A format
// hole keeps its byte offset in the format; a + chain hole keeps its line.
type splicePart struct {
	text string
	hole bool
	off  int
	line int
}

// fmtVerb matches one fmt verb after its '%': flags, an optional argument
// index, width, precision, then the verb letter.
var fmtVerb = regexp.MustCompile(`^[+\-# 0]*(\[\d+\])?(\d+|\*)?(\.(\d+|\*)?)?(\[\d+\])?[a-zA-Z]`)

// numberVerbs print numbers or booleans, never a quote. go vet (a release
// gate) rejects them with a string argument, so INTERVAL '%d days' is not a
// splice. %v, %s, %q and %c can all print a quote and stay holes.
const numberVerbs = "bdoOxXeEfFgGt"

// formatParts splits a format into literal text and verb holes; %% is text.
func formatParts(format string) []splicePart {
	var parts []splicePart
	var text strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			text.WriteByte(format[i])
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			text.WriteByte('%')
			i++
			continue
		}
		m := fmtVerb.FindString(format[i+1:])
		if m == "" {
			text.WriteByte('%')
			continue
		}
		if strings.IndexByte(numberVerbs, m[len(m)-1]) >= 0 {
			text.WriteByte('0')
			i += len(m)
			continue
		}
		parts = append(parts, splicePart{text: text.String()}, splicePart{hole: true, off: i})
		text.Reset()
		i += len(m)
	}
	return append(parts, splicePart{text: text.String()})
}

// quotedHoles returns the holes that fall inside a single-quoted SQL literal.
// Two quotes in a row inside a literal are an escaped quote, not its end (gofmt
// turns two quote marks in a doc comment into a curly quote, hence the words).
// A -- or /* */ comment
// is skipped, so "the wave's tasks" in a comment opens no literal.
func quotedHoles(parts []splicePart) []splicePart {
	const (
		code = iota
		quoted
		lineComment
		blockComment
	)
	state := code
	var out []splicePart
	for _, p := range parts {
		if p.hole {
			if state == quoted {
				out = append(out, p)
			}
			continue
		}
		s := p.text
		for i := 0; i < len(s); i++ {
			next := byte(0)
			if i+1 < len(s) {
				next = s[i+1]
			}
			switch state {
			case code:
				switch {
				case s[i] == '\'':
					state = quoted
				case s[i] == '-' && next == '-':
					state, i = lineComment, i+1
				case s[i] == '/' && next == '*':
					state, i = blockComment, i+1
				}
			case quoted:
				if s[i] == '\'' {
					if next == '\'' {
						i++
					} else {
						state = code
					}
				}
			case lineComment:
				if s[i] == '\n' {
					state = code
				}
			case blockComment:
				if s[i] == '*' && next == '/' {
					state, i = code, i+1
				}
			}
		}
	}
	return out
}
