package kb

import (
	"strings"
	"testing"
)

// 210.3: lesson blocks teach one transaction - What it is, Why it matters,
// Use case, How to do it.
func TestRenderMarkdownLessonBlocks(t *testing.T) {
	what := RenderMarkdown("> [!WHAT]\n> A purchase order commits you to buy.")
	for _, want := range []string{`class="kb-admonition kb-lesson kb-lesson-what"`, "What it is", "<p>A purchase order commits you to buy.</p>"} {
		if !strings.Contains(what, want) {
			t.Fatalf("WHAT block = %q, want it to contain %q", what, want)
		}
	}

	usecase := RenderMarkdown("> [!USECASE]\n> First paragraph.\n>\n> Second paragraph.")
	if strings.Count(usecase, "<p>") != 2 {
		t.Fatalf("a bare > line must split a use case into paragraphs: %q", usecase)
	}

	// A [!HOW] with no body of its own takes the numbered procedure under it,
	// and the paragraph after the list stays outside the block.
	how := RenderMarkdown("> [!HOW]\n\n1. Open the screen.\n2. Click **Save**\n   when done.\n\nAfter the steps.")
	if !strings.Contains(how, `How to do it</p><ol><li>Open the screen.</li><li>Click <strong>Save</strong> when done.</li></ol></aside>`) {
		t.Fatalf("HOW did not take the list below it: %q", how)
	}
	if !strings.Contains(how, "</aside>\n<p>After the steps.</p>") {
		t.Fatalf("the paragraph after the list was swallowed into the HOW block: %q", how)
	}

	// The same steps written inside the quote work too, wrapped lines and all.
	quoted := RenderMarkdown("> [!HOW]\n> 1. Open the screen.\n> 2. Add the vendor\n>    GSTIN.")
	if !strings.Contains(quoted, "<ol><li>Open the screen.</li><li>Add the vendor GSTIN.</li></ol>") {
		t.Fatalf("quoted HOW list = %q", quoted)
	}

	// The GitHub kinds keep their old one-paragraph shape.
	note := RenderMarkdown("> [!NOTE]\n> Keep this.")
	if !strings.Contains(note, `class="kb-admonition kb-admonition-note"`) || strings.Contains(note, "kb-lesson") {
		t.Fatalf("NOTE rendering changed: %q", note)
	}
}

// 210.4: a snapshot carries no src - the browser fetches it through the
// authenticated help API when it scrolls into view.
func TestRenderMarkdownSnapshotHasNoSrc(t *testing.T) {
	html := RenderMarkdown("![The PO composer](../img/po-composer.webp)")
	if !strings.Contains(html, `data-kb-img="po-composer.webp"`) || !strings.Contains(html, `alt="The PO composer"`) {
		t.Fatalf("snapshot = %q", html)
	}
	if strings.Contains(html, "src=") {
		t.Fatalf("a snapshot must not carry a src the static server cannot answer: %q", html)
	}
	if strings.Contains(RenderMarkdown("![x](../img/Bad_Name.webp)"), "data-kb-img") {
		t.Fatal("a name outside the snapshot pattern was treated as a snapshot")
	}
}

const lessonArticle = `---
title: Buying
section: Module Handbooks
module: procurement
screens: [purchase-orders, grn]
screen_sections: [%s]
---

# Buying

## Raise the order

Text.

### Approve it

Text.

## Receive it

![The receipt](../img/grn-receive.webp)
`

func webp(size int) []byte {
	data := make([]byte, size)
	copy(data, "RIFF\x00\x00\x00\x00WEBP")
	return data
}

func TestBuildScreenSectionsAndMenuModules(t *testing.T) {
	dir := t.TempDir()
	writeArticle(t, dir, "module-handbooks/buying.md", strings.Replace(lessonArticle, "%s", "purchase-orders=raise-the-order, purchase-orders=approve-it, GRN=receive-it", 1))
	writeArticle(t, dir, "module-handbooks/odd.md", "---\ntitle: Odd\nsection: Module Handbooks\nmodule: astrology\n---\n\n# Odd\n")
	// Expenses shares `finance` with the ledger handbooks but sits in the HRM menu.
	writeArticle(t, dir, "module-handbooks/expenses.md", "---\ntitle: Expenses\nsection: Module Handbooks\nmodule: finance\n---\n\n# Expenses\n")
	writeArticle(t, dir, "module-handbooks/ledger.md", "---\ntitle: Ledger\nsection: Module Handbooks\nmodule: finance\n---\n\n# Ledger\n")
	writeArticle(t, dir, "img/grn-receive.webp", string(webp(64)))

	result, err := Build(dir)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := strings.Join(result.Index.ScreenSections["purchase-orders"], ","); got != "buying#raise-the-order,buying#approve-it" {
		t.Fatalf("screen_sections[purchase-orders] = %q", got)
	}
	if got := result.Index.ScreenSections["grn"]; len(got) != 1 || got[0] != "buying#receive-it" {
		t.Fatalf("screen ids are case-folded like screens: - screen_sections[grn] = %v", got)
	}
	modules := map[string]string{}
	for _, article := range result.Index.Sections[0].Articles {
		modules[article.Slug] = article.MenuModule
	}
	if modules["buying"] != "Procurement" || modules["odd"] != "Other" {
		t.Fatalf("menu modules = %v, want buying under Procurement and odd under Other", modules)
	}
	if modules["expenses"] != "HRM" || modules["ledger"] != "Financial Accounting" {
		t.Fatalf("menu modules = %v, want expenses filed by its slug under HRM and ledger by its module under Financial Accounting", modules)
	}
	if order := result.Index.MenuModules; len(order) == 0 || order[0] != "POS" || order[len(order)-1] != "Other" {
		t.Fatalf("menu_modules = %v, want the app's menu order ending in Other", order)
	}
	warned := false
	for _, warning := range result.Warnings {
		warned = warned || strings.Contains(warning, `"astrology"`)
	}
	if !warned {
		t.Fatal("an unfiled module must be named in a warning, not silently filed under Other")
	}
	if len(result.Files["img/grn-receive.webp"]) != 64 {
		t.Fatal("the snapshot was not carried into the generated content")
	}
}

func TestBuildRejectsBadScreenSectionsAndSnapshots(t *testing.T) {
	cases := map[string]struct {
		sections string
		image    []byte
		name     string
		want     string
	}{
		"no equals":         {"purchase-orders", webp(64), "grn-receive.webp", "must be screen=anchor"},
		"unlisted screen":   {"pos=raise-the-order", webp(64), "grn-receive.webp", "not in this article's screens"},
		"missing heading":   {"grn=nowhere", webp(64), "grn-receive.webp", "not a heading"},
		"over the cap":      {"grn=receive-it", webp(MaxSnapshotBytes + 1), "grn-receive.webp", "snapshot cap"},
		"not webp":          {"grn=receive-it", []byte("\x89PNG\r\n\x1a\n........"), "grn-receive.webp", "not a WebP"},
		"orphan snapshot":   {"grn=receive-it", webp(64), "grn-other.webp", "no article shows"},
		"bad snapshot name": {"grn=receive-it", webp(64), "GRN.webp", "kebab-case"},
	}
	for name, testCase := range cases {
		dir := t.TempDir()
		writeArticle(t, dir, "module-handbooks/buying.md", strings.Replace(lessonArticle, "%s", testCase.sections, 1))
		writeArticle(t, dir, "img/"+testCase.name, string(testCase.image))
		if testCase.name != "grn-receive.webp" && testCase.want == "no article shows" {
			writeArticle(t, dir, "img/grn-receive.webp", string(webp(64)))
		}
		_, err := Build(dir)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("%s: build error = %v, want one containing %q", name, err, testCase.want)
		}
	}

	dir := t.TempDir()
	writeArticle(t, dir, "module-handbooks/buying.md", strings.Replace(lessonArticle, "%s", "grn=receive-it", 1))
	if _, err := Build(dir); err == nil || !strings.Contains(err.Error(), "not in docs/kb/img/") {
		t.Fatalf("a snapshot the article shows but nobody added must fail the build, got %v", err)
	}
}
