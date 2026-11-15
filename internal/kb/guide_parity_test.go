package kb

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 39.18 (user, 2026-11-15): docs/guides/ stays canonical. In-app Help cannot
// open a guide, so the Knowledge Center keeps its own copy of the user guide's
// §13 first-sale walkthrough instead of a pointer. This test holds that copy to
// the guide: the same steps with the same titles in the same order, and every
// screen, field and button the article names in bold in a step (or in the
// introduction) is one the guide names in that step. A change to the guide's
// walkthrough therefore fails here until the article follows it.

var (
	parityGuideStep = regexp.MustCompile(`^### Step (\d+) [—-] (.+)$`)
	parityKBStep    = regexp.MustCompile(`^## (\d+)\. (.+)$`)
	parityBold      = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	parityArrow     = regexp.MustCompile(`\s*(→|»|->)\s*`)
	paritySpace     = regexp.MustCompile(`\s+`)
	parityTitleJunk = regexp.MustCompile(`[*(),.—-]`)
)

type walkthroughStep struct {
	number int
	title  string
	terms  map[string]bool
}

// parityTerm puts one bold phrase in a form both documents share: either
// menu-path arrow, whitespace and line breaks collapsed, no trailing
// punctuation, lower case.
func parityTerm(s string) string {
	s = strings.ReplaceAll(s, "’", "'")
	s = parityArrow.ReplaceAllString(s, " > ")
	s = paritySpace.ReplaceAllString(strings.TrimSpace(s), " ")
	return strings.ToLower(strings.TrimRight(s, ".:?"))
}

func parityTitle(s string) string {
	s = parityTitleJunk.ReplaceAllString(s, " ")
	return strings.ToLower(paritySpace.ReplaceAllString(strings.TrimSpace(s), " "))
}

// walkthroughSteps splits lines into an introduction (step 0) and numbered
// steps. A level-2 heading that is not a step ends the walkthrough.
func walkthroughSteps(t *testing.T, lines []string, step *regexp.Regexp) []walkthroughStep {
	t.Helper()
	steps := []walkthroughStep{{number: 0}}
	var body []string
	flush := func() {
		cur := &steps[len(steps)-1]
		cur.terms = map[string]bool{}
		for _, m := range parityBold.FindAllStringSubmatch(strings.Join(body, "\n"), -1) {
			cur.terms[parityTerm(m[1])] = true
		}
		body = nil
	}
	for _, line := range lines {
		if m := step.FindStringSubmatch(line); m != nil {
			flush()
			n, _ := strconv.Atoi(m[1])
			steps = append(steps, walkthroughStep{number: n, title: parityTitle(m[2])})
			continue
		}
		if strings.HasPrefix(line, "## ") && len(steps) > 1 {
			break
		}
		// A callout's "> " would read as a menu arrow inside a wrapped phrase.
		body = append(body, strings.TrimLeft(line, "> "))
	}
	flush()
	return steps
}

func parityLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

func TestFirstSaleWalkthroughFollowsUserGuide(t *testing.T) {
	root := filepath.Join("..", "..")
	guide := parityLines(t, filepath.Join(root, "docs", "guides", "user-guide-store-app-and-help.md"))
	start := -1
	for i, line := range guide {
		if strings.HasPrefix(line, "## 13. Open a Shop and Make Your First Sale") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatal("user guide §13 (the first-sale walkthrough) not found; if it moved, point this test and the KB article at its new place")
	}
	g := walkthroughSteps(t, guide[start:], parityGuideStep)

	article := parityLines(t, filepath.Join(root, "docs", "kb", "getting-started", "open-a-shop-and-make-your-first-sale.md"))
	k := walkthroughSteps(t, article, parityKBStep)

	if len(g) < 2 {
		t.Fatal("no steps found in user guide §13")
	}
	if len(k) != len(g) {
		t.Fatalf("the guide's walkthrough has %d steps and the KB article %d; bring the article in line with the guide", len(g)-1, len(k)-1)
	}
	for i := range g {
		if k[i].number != g[i].number || k[i].title != g[i].title {
			t.Errorf("step %d: guide %q, KB article %d %q", g[i].number, g[i].title, k[i].number, k[i].title)
		}
		for term := range k[i].terms {
			if !g[i].terms[term] {
				where := "step " + strconv.Itoa(g[i].number)
				if i == 0 {
					where = "the introduction"
				}
				t.Errorf("the KB article names **%s** in %s, but the guide does not; follow the guide", term, where)
			}
		}
	}
}
