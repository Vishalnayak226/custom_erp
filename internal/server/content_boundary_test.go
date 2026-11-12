package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Stage 49.4.3: static content-boundary check over public/. The enforced CSP
// is script-src 'self' (no inline script, no eval); this keeps new code from
// quietly depending on what the browser would refuse, or on a sink that
// executes strings. Vendored libraries are skipped.

var (
	inlineScriptRe  = regexp.MustCompile(`(?is)<script\b[^>]*>\s*[^\s<]`)
	inlineHandlerRe = regexp.MustCompile(`(?i)\son[a-z]+\s*=\s*["']`)
	jsEvalRe        = regexp.MustCompile(`(^|[^.\w$])(eval\s*\(|new\s+Function\s*\()|document\.write(ln)?\s*\(|\bsrcdoc\s*=|setTimeout\s*\(\s*["'` + "`" + `]|setInterval\s*\(\s*["'` + "`" + `]`)
)

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// Print popups: a fresh about:blank window written once with escaped data
// (cfgEsc/esc) for the user to print. Known and reviewed (49.4.3 audit); a new
// document.write elsewhere fails the check.
var documentWriteAllowed = map[string]bool{"view-bin-tools.js": true, "view-pos-extras.js": true}

func TestPublicContentBoundary(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "public", "*"))
	if len(files) == 0 {
		t.Fatal("public/ not found")
	}
	for _, f := range files {
		base := filepath.Base(f)
		if strings.Contains(base, ".min.") || strings.HasPrefix(base, "vendor") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := string(b)
		switch strings.ToLower(filepath.Ext(f)) {
		case ".html":
			s = htmlCommentRe.ReplaceAllString(s, "")
			if inlineScriptRe.MatchString(s) {
				t.Errorf("%s has an inline <script>: the CSP refuses it; use an external file", base)
			}
			if inlineHandlerRe.MatchString(s) {
				t.Errorf("%s has an inline on*= handler: bind a listener or use data-act", base)
			}
		case ".js":
			for i, line := range strings.Split(s, "\n") {
				tl := strings.TrimSpace(line)
				if strings.HasPrefix(tl, "//") || strings.HasPrefix(tl, "*") {
					continue
				}
				if documentWriteAllowed[base] && strings.Contains(line, "w.document.write(") {
					continue
				}
				if jsEvalRe.MatchString(line) {
					t.Errorf("%s:%d runs a string as code: %s", base, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}

// A served user file must never run script: SVG goes out as a download and
// every file response carries a sandboxing CSP.
func TestAttachmentServedHeadersSource(t *testing.T) {
	b, err := os.ReadFile("handlers_attachments.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, need := range []string{`"svg"`, `"sandbox; default-src 'none'`, `"X-Content-Type-Options", "nosniff"`, `"Cache-Control", "private, no-store"`} {
		if !strings.Contains(s, need) {
			t.Errorf("attachment serving lost %s", need)
		}
	}
}
