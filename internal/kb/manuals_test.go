package kb

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestManualsReuseTopicsAndKeepLinksAndAnchors(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"a.md": "---\ntitle: First\nsection: Reference\nlast_verified: 2026-09-01\n---\n# First\n## Shared\n[Here](#shared) [Next](b.md#shared) [Other](c.md) [Source](../guides/USER_GUIDE.md)\n<script>alert(1)</script>",
		"b.md": "---\ntitle: Second\nsection: Reference\n---\n# Second\n## Shared\nDifferent topic.",
		"c.md": "---\ntitle: Other\nsection: Reference\n---\n# Other\nReference.",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	selection := filepath.Join(t.TempDir(), "manuals.json")
	if err := os.WriteFile(selection, []byte(`{"schema_version":1,"manuals":[{"id":"user","title":"User <manual>","audience":"user","owner":"docs","topics":["a","b"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := BuildManuals(result, root, selection, "v1")
	if err != nil {
		t.Fatal(err)
	}
	output := string(files["user.html"])
	for _, want := range []string{`href="#a--shared"`, `href="#b--shared"`, `id="b--shared"`, `href="../kb/c.md"`, `href="../guides/USER_GUIDE.md"`, `2026-09-01`, `Draft reading and print edition`, `User &lt;manual&gt;`, `<h3 id="a--shared"`} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(output, "<script>") || strings.Count(output, "<h1>") != 1 {
		t.Fatal("manual must be inert with one top-level heading")
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("---\ntitle: First\nsection: Reference\n---\n# First\nChanged canonical content."), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	next, err := BuildManuals(changed, root, selection, "v1")
	if err != nil || !strings.Contains(string(next["user.html"]), "Changed canonical content") {
		t.Fatal("manual did not follow canonical edit", err)
	}
}

func TestCuratedRepositoryManualsHaveResolvableLocalLinks(t *testing.T) {
	root := filepath.Join("..", "..")
	source := filepath.Join(root, "docs", "kb")
	result, err := Build(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range LinkWarnings(result.Articles) {
		t.Error(warning)
	}
	files, err := BuildManuals(result, source, filepath.Join(root, "docs", "governance", "manual-selection.json"), "test")
	if err != nil {
		t.Fatal(err)
	}
	idsPattern := regexp.MustCompile(`\bid="([^"]+)"`)
	linksPattern := regexp.MustCompile(`\bhref="([^"]+)"`)
	for name, data := range files {
		ids := map[string]bool{}
		for _, match := range idsPattern.FindAllSubmatch(data, -1) {
			id := string(match[1])
			if ids[id] {
				t.Errorf("%s duplicate anchor %s", name, id)
			}
			ids[id] = true
		}
		for _, match := range linksPattern.FindAllSubmatch(data, -1) {
			target := string(match[1])
			if strings.HasPrefix(target, "#") {
				if !ids[target[1:]] {
					t.Errorf("%s unresolved internal anchor %s", name, target)
				}
			} else if strings.HasPrefix(target, "/help/") {
				t.Errorf("%s unresolved repository topic %s", name, target)
			} else if !strings.Contains(target, ":") && !strings.HasPrefix(target, "/") {
				file, _, _ := strings.Cut(target, "#")
				if _, err := os.Stat(filepath.Join(root, "docs", "user", filepath.FromSlash(file))); err != nil {
					t.Errorf("%s missing local target %s", name, file)
				}
			}
		}
	}
}

func TestManualSelectionRejectsMissingRepeatedAndUnsafeTopics(t *testing.T) {
	for _, config := range []string{
		`{"schema_version":2,"manuals":[]}`,
		`{"schema_version":1,"manuals":[{"id":"../escape","title":"T","owner":"O","audience":"A","topics":["a"]}]}`,
		`{"schema_version":1,"manuals":[{"id":"user","title":"T","owner":"O","audience":"A","topics":["missing"]}]}`,
		`{"schema_version":1,"manuals":[{"id":"user","title":"T","owner":"O","audience":"A","topics":["a","a"]}]}`,
	} {
		root := t.TempDir()
		selection := filepath.Join(root, "selection.json")
		if err := os.WriteFile(selection, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := BuildManuals(&BuildResult{Articles: []Article{{Slug: "a"}}}, root, selection, "v1"); err == nil {
			t.Fatal("accepted invalid manual selection", config)
		}
	}
}
