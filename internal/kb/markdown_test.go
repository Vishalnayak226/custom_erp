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
