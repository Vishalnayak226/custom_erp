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
