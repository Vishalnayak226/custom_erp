package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 49.3.1: the authorization contract (docs/security/access-control-policy.md,
// "Where each check runs") names, for every surface, the function that decides
// and the tests that prove it. It stays true only while those names exist: a
// renamed or deleted check or test must fail here, not leave the policy
// pointing at nothing.
func TestStage4931ContractNamesLiveCode(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "docs", "security", "access-control-policy.md"))
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]bool{}
	decl := regexp.MustCompile(`(?m)^func (?:\([^)]*\) )?([A-Za-z_][A-Za-z0-9_]*)\(`)
	for _, dir := range []string{"engines", "internal", "db", "cmd"} {
		_ = filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".go") {
				b, _ := os.ReadFile(path)
				for _, m := range decl.FindAllSubmatch(b, -1) {
					funcs[string(m[1])] = true
				}
			}
			return nil
		})
	}
	named := regexp.MustCompile("`([A-Za-z][A-Za-z0-9]*)`")
	rows := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "| A") {
			continue // contract rows are "| A1 | ..."
		}
		rows++
		cells := strings.Split(line, "|")
		if len(cells) < 6 || !strings.Contains(cells[4], "`Test") {
			t.Errorf("contract row names no test: %s", line)
		}
		for _, m := range named.FindAllStringSubmatch(line, -1) {
			if !funcs[m[1]] {
				t.Errorf("the contract names %s, which no Go file defines", m[1])
			}
		}
	}
	if rows < 12 {
		t.Fatalf("read only %d contract rows; the table moved or lost its rows", rows)
	}
}
