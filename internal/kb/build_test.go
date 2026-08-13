package kb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArticle(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestBuildProducesNavSearchAndScreenMap(t *testing.T) {
	dir := t.TempDir()
	writeArticle(t, dir, "getting-started/alpha.md", `---
title: Alpha article
section: Getting Started
order: 1
summary: The first thing to read.
screens: [pos, oms]
last_verified: 2026-08-12
---

# Alpha article

## A section

Reserving stock for an order.
`)
	writeArticle(t, dir, "reference/beta.md", `---
title: Beta reference
section: Reference
order: 2
summary: Integration detail.
public: true
last_verified: 2026-08-12
---

# Beta reference

Idempotency keys make a retry safe.
`)

	result, err := Build(dir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.Index.ArticleCount != 2 {
		t.Fatalf("article count = %d, want 2", result.Index.ArticleCount)
	}
	// Getting Started must sort ahead of Reference regardless of walk order -
	// the reading order of the core journey is not left to the filesystem.
	if result.Index.Sections[0].Name != "Getting Started" {
		t.Fatalf("first section = %q, want Getting Started", result.Index.Sections[0].Name)
	}
	if got := result.Index.ScreenMap["pos"]; len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("screen_map[pos] = %#v, want the alpha article", got)
	}
	if _, ok := result.Files["articles/alpha.html"]; !ok {
		t.Fatalf("no rendered article for alpha; files = %v", keysOf(result.Files))
	}
	rendered := string(result.Files["articles/alpha.html"])
	if !strings.Contains(rendered, `<h2 id="a-section">`) {
		t.Fatalf("rendered article lost its anchored heading:\n%s", rendered)
	}
	if len(result.Index.Sections[0].Articles[0].Headings) == 0 {
		t.Fatal("headings were not extracted, so no article can show a table of contents")
	}

	var search SearchIndex
	if err := json.Unmarshal(result.Files["search.json"], &search); err != nil {
		t.Fatalf("search index is not valid JSON: %v", err)
	}
	if len(search.Terms["reserving"]) != 1 {
		t.Fatalf("body term missing from the search index: %#v", search.Terms["reserving"])
	}
	if len(search.Terms["the"]) != 0 {
		t.Fatal("a stopword was indexed; it matches everything and ranks nothing")
	}
}

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}

func TestBuildRejectsUnusableArticles(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":  "# Just a heading\n",
		"missing title":   "---\nsection: Reference\n---\n\n# Body\n",
		"missing section": "---\ntitle: Orphan\n---\n\n# Body\n",
		"bad order":       "---\ntitle: Bad\nsection: Reference\norder: soon\n---\n\n# Body\n",
	}
	for name, body := range cases {
		dir := t.TempDir()
		writeArticle(t, dir, "article.md", body)
		if _, err := Build(dir); err == nil {
			t.Fatalf("%s was accepted; an article that cannot be navigated to must fail the build", name)
		}
	}
}

func TestBuildRejectsDuplicateSlugs(t *testing.T) {
	dir := t.TempDir()
	header := "---\ntitle: Same name\nsection: Reference\n---\n\n# Body\n"
	writeArticle(t, dir, "a/setup.md", header)
	writeArticle(t, dir, "b/setup.md", header)
	_, err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), "slug") {
		t.Fatalf("duplicate slugs error = %v, want a slug collision refusal - a slug is a permanent link", err)
	}
}

func TestDiffDetectsStaleAndOrphanedOutput(t *testing.T) {
	source := t.TempDir()
	writeArticle(t, source, "alpha.md", "---\ntitle: Alpha\nsection: Reference\nsummary: s\nlast_verified: 2026-08-12\n---\n\n# Alpha\n")
	result, err := Build(source)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out := t.TempDir()
	if err := WriteTo(out, result); err != nil {
		t.Fatalf("write: %v", err)
	}
	if differences := Diff(out, result); len(differences) != 0 {
		t.Fatalf("fresh output reported as drifted: %v", differences)
	}

	// An edited article whose output was not regenerated.
	if err := os.WriteFile(filepath.Join(out, "articles", "alpha.html"), []byte("<p>hand edited</p>"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if differences := Diff(out, result); len(differences) != 1 || !strings.Contains(differences[0], "stale") {
		t.Fatalf("stale output differences = %v, want one stale-file report", differences)
	}

	// An article deleted from source whose HTML is still being served.
	if err := WriteTo(out, result); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(out, "articles", "ghost.html"), []byte("<p>gone</p>"), 0o644); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	differences := Diff(out, result)
	if len(differences) != 1 || !strings.Contains(differences[0], "orphaned") {
		t.Fatalf("orphan differences = %v, want one orphaned-file report", differences)
	}
	// WriteTo must clear it, or a deleted article keeps being served forever.
	if err := WriteTo(out, result); err != nil {
		t.Fatalf("rewrite after orphan: %v", err)
	}
	if differences := Diff(out, result); len(differences) != 0 {
		t.Fatalf("WriteTo left drift behind: %v", differences)
	}
}

func TestEmbeddedContentIsReadableAndPublicSubsetIsNarrower(t *testing.T) {
	index, err := ContentIndex()
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	if index.ArticleCount == 0 {
		t.Fatal("the embedded Knowledge Center is empty - run docs/kb/update-kb.ps1")
	}
	public, err := PublicIndex()
	if err != nil {
		t.Fatalf("public index: %v", err)
	}
	if public.ArticleCount == 0 {
		t.Fatal("no article is marked public: true, so an integrator has nothing to read before signing in")
	}
	if public.ArticleCount >= index.ArticleCount {
		t.Fatal("the public index is not narrower than the full one; authenticated-by-default is not being enforced")
	}
	for _, section := range public.Sections {
		for _, article := range section.Articles {
			if !article.Public {
				t.Fatalf("article %q leaked into the public index", article.Slug)
			}
		}
	}

	// Every slug in the full index must resolve to a body, or the sidebar
	// offers links that 404.
	for _, section := range index.Sections {
		for _, article := range section.Articles {
			if _, body, err := ArticleHTML(article.Slug); err != nil || strings.TrimSpace(body) == "" {
				t.Fatalf("article %q is listed but has no body: %v", article.Slug, err)
			}
		}
	}
	if _, _, err := ArticleHTML("../../../etc/passwd"); err != ErrArticleNotFound {
		t.Fatalf("path traversal error = %v, want ErrArticleNotFound - only slugs the build wrote may be read", err)
	}
}
