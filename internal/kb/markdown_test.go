package kb

import (
	"strings"
	"testing"
)

func TestRenderMarkdownVocabularyAndEscaping(t *testing.T) {
	source := `# Getting Started

Use **safe defaults**, *read the hint*, and open [Setup](/help/setup.html).

## Steps
## Steps

1. Sign in
2. Choose a tenant

- One
- Two with ` + "`inline <code>`" + `

| Field | Meaning |
|---|---|
| status | Current state |

> [!WARNING]
> Do not paste secrets into screenshots.

![Dashboard](/help/img/dashboard.png)

[unsafe](javascript:alert(1))

[also unsafe](data:text/html,bad)

` + "```go" + `
fmt.Println("<unsafe>")
` + "```" + `

<script>alert("raw html")</script>
`

	html := RenderMarkdown(source)
	wants := []string{
		`<h1 id="getting-started">Getting Started`,
		`<strong>safe defaults</strong>`,
		`<em>read the hint</em>`,
		`<a href="/help/setup.html">Setup</a>`,
		`<h2 id="steps-2">Steps`,
		`<ol><li>Sign in</li><li>Choose a tenant</li></ol>`,
		`<ul><li>One</li><li>Two with <code>inline &lt;code&gt;</code></li></ul>`,
		`<table><thead><tr><th>Field</th><th>Meaning</th></tr></thead>`,
		`kb-admonition-warning`,
		`<img src="/help/img/dashboard.png" alt="Dashboard" loading="lazy">`,
		`<a href="#">unsafe</a>`,
		`<a href="#">also unsafe</a>`,
		`<pre><code class="language-go">fmt.Println(&#34;&lt;unsafe&gt;&#34;)</code></pre>`,
		`&lt;script&gt;alert(&#34;raw html&#34;)&lt;/script&gt;`,
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "javascript:") || strings.Contains(html, "data:text") || strings.Contains(html, "<script>") {
		t.Fatalf("unsafe content survived rendering:\n%s", html)
	}
}

func TestRenderMarkdownHandlesUnclosedAndMalformedInput(t *testing.T) {
	html := RenderMarkdown("A **broken marker and invalid byte \xff")
	if !strings.Contains(html, "<p>") || !strings.Contains(html, "**broken") || !strings.Contains(html, "&#xfffd;") {
		t.Fatalf("malformed inline input was lost: %q", html)
	}
	if got := RenderMarkdown("```text\n<value>"); !strings.Contains(got, "&lt;value&gt;") {
		t.Fatalf("unclosed code fence was not escaped: %q", got)
	}
}

// A cross-reference between two articles is written the way Markdown files
// reference each other on disk, so the sources stay readable outside the app.
// The renderer is what turns that into the URL the shell actually serves - and
// an article link that 404s is worse than no link, so pin every case.
func TestRenderMarkdownRewritesArticleLinks(t *testing.T) {
	cases := []struct{ source, want string }{
		{"[a](first-order.md)", `<a href="/help/first-order">a</a>`},
		{"[a](../troubleshooting/error-codes.md)", `<a href="/help/error-codes">a</a>`},
		{"[a](Error-Codes.MD)", `<a href="/help/error-codes">a</a>`},
		{"[a](glossary.md#stock)", `<a href="/help/glossary#stock">a</a>`},
		// Left alone: another project's file, an in-page anchor, a non-article
		// path, and a link that is already in the served shape.
		{"[a](https://example.com/readme.md)", `<a href="https://example.com/readme.md">a</a>`},
		{"[a](#reading-an-error-code)", `<a href="#reading-an-error-code">a</a>`},
		{"[a](/help/setup.html)", `<a href="/help/setup.html">a</a>`},
		{"[a](mailto:support@example.com)", `<a href="mailto:support@example.com">a</a>`},
	}
	for _, testCase := range cases {
		if got := RenderMarkdown(testCase.source); !strings.Contains(got, testCase.want) {
			t.Errorf("RenderMarkdown(%q) = %q, want it to contain %q", testCase.source, got, testCase.want)
		}
	}
	// The rewrite must not become a way to smuggle a scheme past safeURL.
	if got := RenderMarkdown("[a](javascript:alert(1).md)"); strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe scheme survived the article-link rewrite: %q", got)
	}
}

// HeadingSlug is exported so a generator can predict the anchor it links to.
// If it ever disagrees with the anchor the renderer actually emits, every
// generated table of contents silently stops resolving.
func TestHeadingSlugMatchesRenderedAnchor(t *testing.T) {
	for _, heading := range []string{
		"Data Import / Excel Upload",
		"Goods Receipt / GRN",
		"Finance & Accounting",
		"Getting Started",
	} {
		want := `id="` + HeadingSlug(heading) + `"`
		if got := RenderMarkdown("## " + heading); !strings.Contains(got, want) {
			t.Errorf("HeadingSlug(%q) does not match the rendered anchor: %q", heading, got)
		}
	}
}

// Stage 57 (user QA): a generated article's "do not edit" banner is an HTML
// comment, and was printed on the page as escaped text.
func TestRenderMarkdownDropsHTMLComments(t *testing.T) {
	source := "# Title\n\n<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.\nRegenerate: go run ./cmd/gendocs -->\n\nBody text.\n\n<!-- one-line note -->\n\nMore."
	out := RenderMarkdown(source)
	for _, leaked := range []string{"GENERATED", "Regenerate", "one-line note", "&lt;!--"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("comment text %q leaked into rendered HTML:\n%s", leaked, out)
		}
	}
	if !strings.Contains(out, "Body text.") || !strings.Contains(out, "More.") {
		t.Fatalf("content around the comments was lost:\n%s", out)
	}
	// An unterminated comment must not swallow the rest of the article.
	if out := RenderMarkdown("<!-- stray\n\nStill here."); !strings.Contains(out, "Still here.") {
		t.Fatalf("unterminated comment swallowed following content:\n%s", out)
	}
}

// 135.15 (user QA): a bullet wrapped onto an indented second line showed that
// line as a separate paragraph under the list, and split the list in two. A
// wrapped line - indented or "lazy" - belongs to the item above it, and an
// item indented under another is a nested list, as in CommonMark.
func TestRenderMarkdownKeepsWrappedLinesInListItems(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"indented bullet continuation",
			"- **Ready-made** dashboards for **Sales**,\n  **Customers** and **Money**.\n- Every figure",
			"<ul><li><strong>Ready-made</strong> dashboards for <strong>Sales</strong>, <strong>Customers</strong> and <strong>Money</strong>.</li><li>Every figure</li></ul>\n"},
		{"numbered continuation, indented and lazy",
			"1. Step one\n   continues.\n10. Step two\nlazy line.",
			"<ol><li>Step one continues.</li><li>Step two lazy line.</li></ol>\n"},
		{"nested list keeps the outer numbering",
			"1. Decide:\n   - Maker\n     wrapped.\n   - Role\n2. Approve",
			"<ol><li>Decide:<ul><li>Maker wrapped.</li><li>Role</li></ul></li><li>Approve</li></ol>\n"},
		{"a blank line still ends the list",
			"- Item\n\nParagraph after.",
			"<ul><li>Item</li></ul>\n<p>Paragraph after.</p>\n"},
		{"a fence still ends the list",
			"- Item\n```\ncode\n```",
			"<ul><li>Item</li></ul>\n<pre><code>code</code></pre>\n"},
		{"a heading still ends the list",
			"- Item\n## Next",
			"<ul><li>Item</li></ul>\n<h2 id=\"next\">Next <a class=\"kb-anchor\" href=\"#next\" aria-label=\"Copy link to this section\">#</a></h2>\n"},
		{"the other kind at the same depth is a new list",
			"- a\n1. b",
			"<ul><li>a</li></ul>\n<ol><li>b</li></ol>\n"},
	}
	for _, testCase := range cases {
		if got := RenderMarkdown(testCase.source); got != testCase.want {
			t.Errorf("%s:\n got %q\nwant %q", testCase.name, got, testCase.want)
		}
	}
}
