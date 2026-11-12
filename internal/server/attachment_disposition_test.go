package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStage4948AttachmentDispositionIsSafe(t *testing.T) {
	cases := map[string]string{
		"report.csv":                 `attachment; filename="report.csv"`,
		"a\"b; x=y.csv":              `attachment; filename="a_b_ x_y.csv"`,
		"x\r\nSet-Cookie: a=b":       `attachment; filename="x__Set-Cookie_ a_b"`,
		"../../etc/passwd":           `attachment; filename="_.._etc_passwd"`,
		"":                           `attachment; filename="download"`,
		"..":                         `attachment; filename="download"`,
		"Sales (Q1) 2026-11-05.xlsx": `attachment; filename="Sales (Q1) 2026-11-05.xlsx"`,
	}
	for in, want := range cases {
		if got := attachmentDisposition(in); got != want {
			t.Errorf("%q: got %s want %s", in, got, want)
		}
	}
	if got := attachmentDisposition(strings.Repeat("a", 500)); len(got) > 160 {
		t.Errorf("name not capped: %d", len(got))
	}
}

// No handler builds a downloaded file name into the header by hand.
func TestStage4948NoHandBuiltContentDisposition(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"Content-Disposition"`) && strings.Contains(line, "Set(") &&
				strings.Contains(line, "filename") && !strings.Contains(line, "attachmentDisposition(") {
				t.Errorf("%s:%d builds filename by hand: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
