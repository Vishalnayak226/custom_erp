package webasset

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripJSDropsOnlyWholeLineComments(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"line comment", "// note\nlet a = 1;\n", "\nlet a = 1;\n"},
		{"indented", "if (x) {\n    // why\n    y();\n}\n", "if (x) {\n\n    y();\n}\n"},
		{"comment after code stays", "a(); // keep\n", "a(); // keep\n"},
		{"block comment stays", "/* keep */\n// drop\n", "/* keep */\n\n"},
		{"source map directive stays", "//# sourceMappingURL=a.map\n//@ x\n", "//# sourceMappingURL=a.map\n//@ x\n"},
		{"crlf", "// a\r\nb();\r\n", "\nb();\r\n"},
		{"last line without newline", "a();\n// end", "a();\n"},
		{"template text is never stripped", "const h = `\n// not a comment\n${x}\n  // nor this\n`;\n// drop\n",
			"const h = `\n// not a comment\n${x}\n  // nor this\n`;\n\n"},
		{"comment inside ${} is a comment", "const h = `${\n// note\nx}`;\n", "const h = `${\n\nx}`;\n"},
		{"nested template", "t = `a${b ? `c\n// keep\n` : 'd'}e`;\n// drop\n", "t = `a${b ? `c\n// keep\n` : 'd'}e`;\n\n"},
		{"strings with slashes and quotes", "s = 'http://x' + \"//y\" + 'it\\'s';\n// drop\n", "s = 'http://x' + \"//y\" + 'it\\'s';\n\n"},
		{"string continues after backslash", "s = 'a\\\n// still text';\n", "s = 'a\\\n// still text';\n"},
		{"regex with backtick and quote", "r = /[`'\"/]+/g;\n// drop\nq = `\n// keep\n`;\n", "r = /[`'\"/]+/g;\n\nq = `\n// keep\n`;\n"},
		{"regex after return", "function f(s) {\n  return /`/.test(s);\n}\n// drop\n", "function f(s) {\n  return /`/.test(s);\n}\n\n"},
		{"division is not a regex", "x = a / b / c;\n// drop\ny = (d) / 2;\n", "x = a / b / c;\n\ny = (d) / 2;\n"},
		{"apostrophe in a comment", "// don't\nx();\nlet y = 1; // isn't\n", "\nx();\nlet y = 1; // isn't\n"},
		{"comment between a block and a regex", "}\n// drop\n/x/.test(s);\n", ""}, // unbalanced: refused, see below
	}
	for _, c := range cases {
		got, err := StripJS([]byte(c.in))
		if c.want == "" {
			if err == nil {
				t.Errorf("%s: want a refusal, got %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestStripJSRefusesWhatItCannotRead(t *testing.T) {
	for name, in := range map[string]string{
		"unclosed template":  "a = `x\n// y\n",
		"unclosed string":    "a = 'x;\n",
		"unclosed regex":     "a = /x\n",
		"unbalanced bracket": "f(a]\n",
		"unclosed block":     "/* x\n",
		"open at the end":    "if (x) {\n",
	} {
		if out, err := StripJS([]byte(in)); err == nil {
			t.Errorf("%s: want a refusal, got %q", name, out)
		}
	}
}

func TestStripCSS(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"line comment", "/* head */\na { color: red; }\n", "\na { color: red; }\n"},
		{"multi-line keeps newlines", "/* one\n two */\nb{}\n", "\n\nb{}\n"},
		{"after a declaration", "a { color: red; /* why */ }\n", "a { color: red;  }\n"},
		{"between two characters stays empty", "a/**/.b{}\nc/* x */d{}\n", "a/**/.b{}\nc/**/d{}\n"},
		{"strings keep their text", "a::before { content: \"/* no */\"; }\n", "a::before { content: \"/* no */\"; }\n"},
		{"unquoted url keeps its text", "a { background: url(http://x/*.png); }\n", "a { background: url(http://x/*.png); }\n"},
		{"quoted url", "a { background: url( '/*.png' ); } /* x */\n", "a { background: url( '/*.png' ); } \n"},
	}
	for _, c := range cases {
		got, err := StripCSS([]byte(c.in))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	if _, err := StripCSS([]byte("a{}/* open")); err == nil {
		t.Error("unclosed comment: want a refusal")
	}
}

func gzipLen(b []byte) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	w.Write(b)
	w.Close()
	return buf.Len()
}

// TestEveryShippedFileStrips holds every script and stylesheet in public/ to
// three things: the scanner reads it to the end (no file is quietly sent with
// its comments), its line count is unchanged, and in a script the only change
// is a // comment line emptied.
func TestEveryShippedFileStrips(t *testing.T) {
	root := filepath.Join("..", "..", "public")
	var files, before, after int
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !Strippable(p) {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out, err := Strip(p, raw)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		files++
		before += gzipLen(raw)
		after += gzipLen(out)
		in, got := strings.Split(string(raw), "\n"), strings.Split(string(out), "\n")
		if len(in) != len(got) {
			t.Errorf("%s: %d lines became %d", p, len(in), len(got))
			return nil
		}
		if strings.HasSuffix(p, ".css") {
			return nil
		}
		for i := range in {
			if in[i] == got[i] {
				continue
			}
			if got[i] != "" || !strings.HasPrefix(strings.TrimLeft(in[i], " \t"), "//") {
				t.Errorf("%s:%d changed beyond a comment line: %q -> %q", p, i+1, in[i], got[i])
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 {
		t.Fatalf("only %d scripts and stylesheets found under %s", files, root)
	}
	t.Logf("%d files, gzip %d -> %d bytes (%d%% less)", files, before, after, 100*(before-after)/before)
}
