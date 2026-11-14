package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// inlineStyleCeiling is the number of style="..." attributes left in public/
// JS and HTML after 49.4.3 converted the static ones to classes (styles.css
// for the cold core, view-inline.css for lazy views). What remains is built
// from ${...} values at run time, set in documents written into their own
// window, or toggled by JS with style.display. The ceiling only goes down:
// a new screen uses a class, not an inline style.
const inlineStyleCeiling = 69

var inlineStyleAttr = regexp.MustCompile(`[ \t]style=["'][^"']`)

// TestPublicInlineStylesOnlyShrink is the ratchet that keeps the strict
// style-src trial (report-only in middleware.go) from growing new violations.
func TestPublicInlineStylesOnlyShrink(t *testing.T) {
	root := "../../public"
	count := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "downloads" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".js") && !strings.HasSuffix(p, ".html") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		count += len(inlineStyleAttr.FindAll(b, -1))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count > inlineStyleCeiling {
		t.Errorf("public/ has %d inline style attributes, ceiling %d - use a class (styles.css or view-inline.css) instead", count, inlineStyleCeiling)
	}
	if count < inlineStyleCeiling {
		t.Logf("public/ has %d inline style attributes; lower inlineStyleCeiling to %d", count, count)
	}
}
