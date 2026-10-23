package server

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEngineErrorCodesAreCatalogued (132.10 f): an engine ValidationError or a
// handler writeAPIError naming a code with no catalog row reaches the user as
// "Unexpected error occurred" (GLOBAL-0302) instead of its reason. FIN-0021
// and ASN-0271 did; this keeps the next one out.
func TestEngineErrorCodesAreCatalogued(t *testing.T) {
	patterns := map[string]*regexp.Regexp{
		filepath.Join("..", "..", "engines"): regexp.MustCompile(`Code:\s*"([A-Z]+-[0-9]{4})"`),
		".":                                  regexp.MustCompile(`writeAPIError(?:Detail)?\(w, r, "([A-Z]+-[0-9]{4})"`),
	}
	missing := map[string]string{}
	seen := 0
	for dir, re := range patterns {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				seen++
				if _, ok := errorCatalog[m[1]]; !ok {
					missing[m[1]] = filepath.Base(f)
				}
			}
		}
	}
	if seen < 300 {
		t.Fatalf("scanned only %d error-code uses - the source paths are wrong", seen)
	}
	if len(missing) > 0 {
		codes := make([]string, 0, len(missing))
		for code, file := range missing {
			codes = append(codes, code+" ("+file+")")
		}
		sort.Strings(codes)
		t.Fatalf("error codes with no catalog row - add them to error_catalog_extensions.go: %s", strings.Join(codes, ", "))
	}
}
