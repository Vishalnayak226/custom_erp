package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Stage 139.3: the short codes typed in the Ctrl+K box
// (public/shortcut-codes.json). Fixed for every client, so each must open a
// screen that exists, name a record type that is seeded, and be in both help
// documents - a code the guide does not list is a code nobody learns. Since
// 202.1 every sidebar screen must have one.
func TestStage139ShortCodes(t *testing.T) {
	read := func(parts ...string) string {
		body, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
		if err != nil {
			t.Fatalf("%s: %v", filepath.Join(parts...), err)
		}
		return string(body)
	}
	var file struct {
		Codes []struct {
			Code, Name, Area, Menu, Setup, Doctype, Next string
		} `json:"codes"`
	}
	if err := json.Unmarshal([]byte(read("public", "shortcut-codes.json")), &file); err != nil {
		t.Fatalf("shortcut-codes.json: %v", err)
	}
	if len(file.Codes) == 0 {
		t.Fatal("shortcut-codes.json lists no codes")
	}
	index := read("public", "index.html")
	var seeds strings.Builder
	sqlFiles, _ := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*.sql"))
	for _, f := range sqlFiles {
		body, _ := os.ReadFile(f)
		seeds.Write(body)
	}
	// The user guide is an index plus part files (user-guide-*.md); read them all.
	var guideParts strings.Builder
	guideFiles, _ := filepath.Glob(filepath.Join("..", "..", "docs", "guides", "user-guide*.md"))
	for _, f := range guideFiles {
		body, _ := os.ReadFile(f)
		guideParts.Write(body)
	}
	guide := guideParts.String()
	article := read("docs", "kb", "getting-started", "keyboard-shortcuts.md")

	format := regexp.MustCompile(`^[A-Z]{2,4}$`)
	codes := map[string]bool{}
	menus := map[string]bool{}
	for _, c := range file.Codes {
		codes[c.Code] = true
		menus[c.Menu] = true
	}
	// 202.1: every screen in the sidebar has a code. A group heading
	// (menu-item-group) only opens its group, so it has none.
	entry := regexp.MustCompile(`<a\b[^>]*\bclass="([^"]*)"[^>]*\bid="(menu-[a-z0-9-]+)"`)
	screens := 0
	for _, m := range entry.FindAllStringSubmatch(index, -1) {
		if strings.Contains(" "+m[1]+" ", " menu-item-group ") {
			continue
		}
		screens++
		if !menus[m[2]] {
			t.Errorf("sidebar entry %s has no code in public/shortcut-codes.json", m[2])
		}
	}
	if screens < 100 {
		t.Errorf("found %d sidebar entries; the pattern no longer matches public/index.html", screens)
	}
	seen := map[string]bool{}
	for _, c := range file.Codes {
		if !format.MatchString(c.Code) {
			t.Errorf("%q: a code is 2-4 capital letters", c.Code)
		}
		if seen[c.Code] {
			t.Errorf("%s is listed twice", c.Code)
		}
		seen[c.Code] = true
		if c.Name == "" || c.Area == "" {
			t.Errorf("%s needs a name and an area", c.Code)
		}
		if (c.Menu == "") == (c.Setup == "") {
			t.Errorf("%s needs exactly one of menu (a sidebar entry) or setup (a record list)", c.Code)
		}
		if c.Menu != "" && !strings.Contains(index, `id="`+c.Menu+`"`) {
			t.Errorf("%s opens %s, which public/index.html has no sidebar entry for", c.Code, c.Menu)
		}
		for _, dt := range []string{c.Doctype, c.Setup} {
			if dt != "" && !strings.Contains(seeds.String(), "'"+dt+"'") {
				t.Errorf("%s names record type %s, which no db/migrations/*.sql seeds", c.Code, dt)
			}
		}
		if c.Next != "" && !codes[c.Next] {
			t.Errorf("%s offers next step %s, which is not a code", c.Code, c.Next)
		}
		for doc, body := range map[string]string{"docs/guides/user-guide*.md": guide, "docs/kb/getting-started/keyboard-shortcuts.md": article} {
			if !strings.Contains(body, "| `"+c.Code+"` |") {
				t.Errorf("%s does not list code %s", doc, c.Code)
			}
		}
	}
}
