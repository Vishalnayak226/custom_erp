package kb

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Item 48.7.7: the Knowledge Center and the manuals generated from it are usable by keyboard and
// screen-reader users, read as plain English, are ready for translation and are found in two
// decisions. These checks run on what ships: the embedded index and article HTML, and the
// generated manuals under docs/user/.

var (
	a11yHeading  = regexp.MustCompile(`<h([1-6])[ >]`)
	a11yImage    = regexp.MustCompile(`<img [^>]*>`)
	a11yImageAlt = regexp.MustCompile(` alt="([^"]*)"`)
	a11yLink     = regexp.MustCompile(`(?s)<a ([^>]*)>(.*?)</a>`)
	a11yTag      = regexp.MustCompile(`<[^>]+>`)
	a11yTopic    = regexp.MustCompile(`<article id="(topic-[^"]+)"`)

	// Link text a screen reader announces out of context ("links list") says nothing on its own.
	a11yVagueLinkText = map[string]bool{
		"here": true, "click here": true, "this": true, "this page": true, "this link": true,
		"link": true, "more": true, "read more": true, "see more": true, "details": true, "go": true,
	}

	// Plain English and translation: words that have a plain form, and directions that only work
	// for someone who can see the layout (WCAG 1.3.3) or that flip in a right-to-left locale.
	a11yPlainWords = []struct {
		pattern *regexp.Regexp
		plain   string
	}{
		{regexp.MustCompile(`(?i)\butili[sz](e|es|ed|ing)\b`), "use"}, // "utilisation" stays: it is a measure (bin, dock)
		{regexp.MustCompile(`(?i)\bin order to\b`), "to"},
		{regexp.MustCompile(`(?i)\bleverag(e|es|ed|ing)\b`), "use"},
		{regexp.MustCompile(`(?i)\baforementioned\b`), "this / that"},
		{regexp.MustCompile(`(?i)\b(herein|hereby|thereof|whereby|henceforth)\b`), "a plain phrase"},
		{regexp.MustCompile(`(?i)\bclick here\b`), "the name of what the link opens"},
		{regexp.MustCompile(`(?i)\b(green|red|blue|orange|yellow) (button|link|box)\b`), "the button's name"},
		{regexp.MustCompile(`(?i)\b(button|link|box|menu|panel) (on|at|to) the (left|right)\b`), "the control's name"},
	}
	// A date like 03/04/2026 reads as March or April depending on the reader's locale.
	a11yAmbiguousDate = regexp.MustCompile(`\b(0?[1-9]|[12]\d|3[01])/(0?[1-9]|1[0-2])/(\d{4}|\d{2})\b`)
)

const (
	a11yMaxSections    = 9   // the first decision: a short list of sections
	a11yMaxSummaryChar = 300 // the line under a title in the section list and search results
)

func TestKnowledgeCenterIsAccessibleAndFindable(t *testing.T) {
	index, err := ContentIndex()
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}

	// Findable in two decisions inside the product: Help lists the sections, a section lists its
	// articles. Every article is in exactly one section and the first list stays short.
	if len(index.Sections) == 0 || len(index.Sections) > a11yMaxSections {
		t.Errorf("Help offers %d sections; the first decision needs 1-%d", len(index.Sections), a11yMaxSections)
	}
	seenSlug := map[string]string{}
	seenTitle := map[string]string{}
	listed := 0
	for _, section := range index.Sections {
		for _, article := range section.Articles {
			listed++
			if prev, ok := seenSlug[article.Slug]; ok {
				t.Errorf("%s is listed under both %q and %q; an article belongs to one section", article.Slug, prev, section.Name)
			}
			seenSlug[article.Slug] = section.Name
			key := strings.ToLower(strings.TrimSpace(article.Title))
			if prev, ok := seenTitle[key]; ok {
				t.Errorf("%s and %s share the title %q; the second decision cannot tell them apart", prev, article.Slug, article.Title)
			}
			seenTitle[key] = article.Slug
			if strings.TrimSpace(article.Summary) == "" {
				t.Errorf("%s has no summary, so the section list and search show a bare title", article.Slug)
			} else if n := len([]rune(article.Summary)); n > a11yMaxSummaryChar {
				t.Errorf("%s: summary is %d characters; keep it under %d so it reads at a glance", article.Slug, n, a11yMaxSummaryChar)
			}
		}
	}
	if listed != index.ArticleCount {
		t.Errorf("the sections list %d articles but the index counts %d; an article outside every section cannot be reached by browsing", listed, index.ArticleCount)
	}
	search, err := SearchIndexJSON()
	if err != nil {
		t.Fatalf("read search index: %v", err)
	}
	for slug := range seenSlug {
		if !strings.Contains(string(search), `"slug":"`+slug+`"`) {
			t.Errorf("%s is not in the search index", slug)
		}
	}

	for slug := range seenSlug {
		_, body, err := ArticleHTML(slug)
		if err != nil {
			t.Errorf("%s: %v", slug, err)
			continue
		}
		for _, problem := range readabilityProblems(body, true) {
			t.Errorf("article %s: %s", slug, problem)
		}
	}

	// The docs portal: manuals.md opens each manual, and each manual's contents list links every
	// topic in it - pick a manual, pick a topic.
	userDir := filepath.Join("..", "..", "docs", "user")
	portal, err := os.ReadFile(filepath.Join(userDir, "manuals.md"))
	if err != nil {
		t.Fatalf("read the manuals portal: %v", err)
	}
	manuals, err := filepath.Glob(filepath.Join(userDir, "*.html"))
	if err != nil || len(manuals) == 0 {
		t.Fatalf("no generated manuals under docs/user (%v); run docs/update-docs.ps1 -Group Content", err)
	}
	for _, path := range manuals {
		name := filepath.Base(path)
		if !strings.Contains(string(portal), "]("+name+")") {
			t.Errorf("docs/user/manuals.md does not link %s", name)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		page := string(raw)
		for _, want := range []string{`<html lang="en">`, `<meta charset="utf-8">`, `<main id="manual-content">`, `href="#manual-content"`, `<nav aria-label=`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: missing %s (language for screen readers and translation, skip link, landmarks)", name, want)
			}
		}
		nav := page
		if i := strings.Index(page, `<main id="manual-content">`); i > 0 {
			nav = page[:i]
		}
		topics := a11yTopic.FindAllStringSubmatch(page, -1)
		if len(topics) == 0 {
			t.Errorf("%s: no topics", name)
		}
		for _, m := range topics {
			if !strings.Contains(nav, `href="#`+m[1]+`"`) {
				t.Errorf("%s: #%s is not in the contents list", name, m[1])
			}
		}
		main := page
		if i := strings.Index(page, `<main id="manual-content">`); i >= 0 {
			main = page[i:]
		}
		for _, problem := range readabilityProblems(main, false) {
			t.Errorf("manual %s: %s", name, problem)
		}
	}
}

// readabilityProblems applies the per-page rules: images carry alt text, every link says where it goes,
// headings do not skip a level (an article also opens with its one h1), and the text avoids the
// words and directions listed above.
func readabilityProblems(page string, article bool) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	for _, img := range a11yImage.FindAllString(page, -1) {
		m := a11yImageAlt.FindStringSubmatch(img)
		if m == nil || strings.TrimSpace(html.UnescapeString(m[1])) == "" {
			add("an image has no alt text: %s", img)
		}
	}
	for _, m := range a11yLink.FindAllStringSubmatch(page, -1) {
		label := strings.TrimSpace(html.UnescapeString(a11yTag.ReplaceAllString(m[2], "")))
		if strings.Contains(m[1], `aria-label="`) {
			continue
		}
		if label == "" {
			add("a link has no text: <a %s>", m[1])
		} else if a11yVagueLinkText[strings.ToLower(strings.TrimRight(label, ".:"))] {
			add("link text %q says nothing out of context", label)
		}
	}
	levels := a11yHeading.FindAllStringSubmatch(page, -1)
	if article {
		h1 := 0
		for _, l := range levels {
			if l[1] == "1" {
				h1++
			}
		}
		if h1 != 1 || len(levels) == 0 || levels[0][1] != "1" {
			add("must open with its one h1 title (found %d h1)", h1)
		}
	}
	prev := 0
	for _, l := range levels {
		n, _ := strconv.Atoi(l[1])
		if prev > 0 && n > prev+1 {
			add("heading jumps from h%d to h%d; a screen-reader outline loses the level between", prev, n)
		}
		prev = n
	}
	text := html.UnescapeString(a11yTag.ReplaceAllString(stripCode(page), " "))
	for _, w := range a11yPlainWords {
		if m := w.pattern.FindString(text); m != "" {
			add("%q - write %s", m, w.plain)
		}
	}
	if m := a11yAmbiguousDate.FindString(text); m != "" {
		add("date %q reads differently by locale; write 2026-11-15 or 15 November 2026", m)
	}
	return out
}

// stripCode drops <pre> and <code> content: commands, paths and code samples are quoted
// literally and are not prose.
func stripCode(page string) string {
	for _, tag := range []string{"pre", "code"} {
		re := regexp.MustCompile(`(?s)<` + tag + `[ >].*?</` + tag + `>`)
		page = re.ReplaceAllString(page, " ")
	}
	return page
}

// Each rule must fire on a page that breaks it, or a passing run proves nothing.
func TestReadabilityRulesCatchEachProblem(t *testing.T) {
	bad := `<h2>No title</h2><h4>Skipped a level</h4>` +
		`<p><img src="a.png" alt=""> <a href="/help/x">click here</a> <a href="/help/y"></a>` +
		` In order to save, press the green button on 03/04/2026.</p>` +
		`<pre>in order to (a quoted command is not prose)</pre>`
	got := strings.Join(readabilityProblems(bad, true), "\n")
	for _, want := range []string{
		"no alt text", `link text "click here"`, "a link has no text", "one h1 title",
		"from h2 to h4", `"In order to"`, `"click here" - write`, `"green button"`, `date "03/04/2026"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rule not caught: %s\ngot:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"in order to"`) {
		t.Errorf("code inside <pre> was read as prose:\n%s", got)
	}
	good := `<h1 id="a">Title <a class="kb-anchor" href="#a" aria-label="Copy link to this section">#</a></h1>` +
		`<h2>Steps</h2><p><img src="a.png" alt="The till"> Open the <a href="/help/pos-operations">POS handbook</a>` +
		` on 2026-11-15. Bin utilisation is a measure.</p><h3>Detail</h3><h2>Next</h2>`
	if p := readabilityProblems(good, true); len(p) != 0 {
		t.Errorf("a readable page was refused: %v", p)
	}
}
