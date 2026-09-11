package kb

import (
	"strings"
	"testing"
)

func TestLinkWarningsUseRenderedRoutesAndIncludeTitleAnchors(t *testing.T) {
	articles := []Article{
		{Slug: "a", SourcePath: "a.md", HTML: RenderMarkdown("# Title\n[Self](#title) [Valid](b.md#details) [Missing](absent.md) [Wrong anchor](b.md#absent) [Web](https://example.com/a.md)")},
		{Slug: "b", SourcePath: "b.md", HTML: RenderMarkdown("# Other\n## Details\nText.")},
	}
	warnings := LinkWarnings(articles)
	if len(warnings) != 2 {
		t.Fatalf("expected missing route and anchor only, got %v", warnings)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "broken help link /help/absent") || !strings.Contains(strings.Join(warnings, "\n"), "broken help anchor /help/b#absent") {
		t.Fatal(warnings)
	}
}
