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
// JS and HTML. 49.4.3 turned the static ones into classes (styles.css for the
// cold core, view-inline.css for lazy views) and the run-time ones into
// data-css="...", which app.js applyDataCss sets through the CSSOM - allowed
// by a strict style-src, where a style attribute in markup is refused. The two
// left are the PDF and dashboard rasterisers (print-pdf.js, view-dashboards.js):
// they build an SVG image as a string, and an image is its own document that
// the page's policy does not reach. The ceiling only goes down.
const inlineStyleCeiling = 2

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

// 49.4.3: a stylesheet made at run time is a constructed sheet (app.js
// adoptCss), never a <style> element: a strict style-src refuses the element
// however it was made. The rasterisers' <style> sits inside an SVG image
// string (the line strips "</style" from the embedded CSS) and is the only
// exception. The shell must also keep the one observer that applies data-css.
func TestPublicStylesGoThroughTheCSSOM(t *testing.T) {
	root := "../../public"
	styleElement := regexp.MustCompile(`createElement\(['"]style['"]\)|<style[ >]`)
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
		for i, line := range strings.Split(string(b), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") || strings.HasPrefix(code, "*") || strings.HasPrefix(code, "/*") {
				continue // a comment that names the element
			}
			if styleElement.MatchString(line) && !strings.Contains(line, `css.replace(/<\/style/gi`) {
				t.Errorf("%s:%d makes a <style> element; use adoptCss(id, css) - a strict style-src refuses it", p, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := os.ReadFile(filepath.Join(root, "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"function applyDataCss(", "function adoptCss(", "for (const n of r.addedNodes) if (n.nodeType === 1) applyDataCss(n);"} {
		if !strings.Contains(string(app), want) {
			t.Errorf("public/app.js lost %q; data-css and run-time stylesheets would stop applying", want)
		}
	}
}
