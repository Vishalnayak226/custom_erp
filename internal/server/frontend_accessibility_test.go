package server

// Stage 47.6.3 / 47.6.4 - operator-width responsiveness and WCAG 2.2 AA,
// guarded by a test rather than by a one-time sweep (audit finding A-06/A-39).
//
// Why this exists as a test at all: an accessibility pass that is only ever
// performed is an accessibility pass that lasts until the next screen. This
// repo already answers that problem the same way three times over -
// route_capabilities_test.go parses routes.go, scope_policy_test.go parses the
// migration seeds, and the Stage 49 manifest test parses the whole surface -
// so this parses public/styles.css and public/index.html and fails the build
// when an invariant is dropped.
//
// WHAT THIS DOES NOT CLAIM. It checks the criteria that are decidable from the
// static assets: colour contrast of the design tokens, a global focus
// indicator, minimum target sizes, the reflow/drawer structure, and labelling
// in the application shell. Full WCAG 2.2 AA conformance across ~40
// dynamically-rendered screens needs axe-core driving a real browser over each
// one, which is 47.6.4's remaining work and is recorded as open in
// docs/micro_checklist.md. A green test here means the foundation holds, not
// that the product is certified.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func readPublicAsset(t *testing.T, name string) string {
	t.Helper()
	// The test binary runs in internal/server; the assets are two levels up.
	path := filepath.Join("..", "..", "public", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	return string(body)
}

// --- contrast (WCAG 2.1 SC 1.4.3, carried forward into 2.2) ----------------

type rgb struct{ r, g, b float64 }

func parseHex(hex string) (rgb, bool) {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return rgb{}, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return rgb{}, false
	}
	return rgb{
		r: float64((v >> 16) & 0xff),
		g: float64((v >> 8) & 0xff),
		b: float64(v & 0xff),
	}, true
}

// relativeLuminance is the WCAG formula, verbatim.
func relativeLuminance(c rgb) float64 {
	channel := func(v float64) float64 {
		s := v / 255.0
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

func contrastRatio(fg, bg rgb) float64 {
	l1, l2 := relativeLuminance(fg), relativeLuminance(bg)
	if l2 > l1 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

var tokenDeclRe = regexp.MustCompile(`(--[a-z0-9-]+)\s*:\s*(#[0-9a-fA-F]{3,6})\s*;`)

// tokenBlock pulls the token values out of one `:root`-ish declaration block.
// Blocks are located by their opening selector text so the light block and the
// explicit-dark block are read separately - a token that is only correct in one
// theme is exactly the kind of regression this catches.
func tokenBlock(t *testing.T, css, startMarker string) map[string]rgb {
	t.Helper()
	i := strings.Index(css, startMarker)
	if i < 0 {
		t.Fatalf("could not find the %q token block in styles.css", startMarker)
	}
	end := strings.Index(css[i:], "\n}")
	if end < 0 {
		t.Fatalf("token block %q is not closed", startMarker)
	}
	out := map[string]rgb{}
	for _, m := range tokenDeclRe.FindAllStringSubmatch(css[i:i+end], -1) {
		if c, ok := parseHex(m[2]); ok {
			out[m[1]] = c
		}
	}
	if len(out) == 0 {
		t.Fatalf("token block %q yielded no colours", startMarker)
	}
	return out
}

// TestDesignTokenContrastMeetsAA checks every foreground/background pairing the
// status vocabulary actually uses, in BOTH themes.
//
// These pairings are not hypothetical: they are what the RF status line, the
// POS price cells and every toast render with, and an operator reads them on a
// device in a warehouse aisle - which is the worst viewing condition this
// product has.
func TestDesignTokenContrastMeetsAA(t *testing.T) {
	css := readPublicAsset(t, "styles.css")

	// All three blocks, not two. "system" is the default setting - no
	// data-theme attribute is stamped at all - so a token that is correct in
	// the explicit dark block and wrong in the media block is wrong for every
	// user who never touched the theme toggle, which is most of them.
	themes := map[string]map[string]rgb{
		"light":       tokenBlock(t, css, ":root {"),
		"dark":        tokenBlock(t, css, `:root[data-theme="dark"] {`),
		"system-dark": tokenBlock(t, css, `:root:not([data-theme="light"]) {`),
	}

	// fg -> bg pairs that real rules put together. Normal-size text, so the
	// AA threshold is 4.5:1 (SC 1.4.3).
	pairs := []struct{ fg, bg string }{
		{"--text-main", "--panel-bg"},
		{"--text-main", "--bg-color"},
		{"--text-muted", "--panel-bg"},
		{"--danger-strong", "--danger-soft-bg"},
		{"--warning-strong", "--warning-soft-bg"},
		{"--success-strong", "--success-soft-bg"},
		{"--on-primary", "--primary-color"},
	}

	for theme, tokens := range themes {
		for _, p := range pairs {
			fg, okFg := tokens[p.fg]
			bg, okBg := tokens[p.bg]
			if !okFg || !okBg {
				// A token missing from a theme block is itself the bug the
				// stylesheet's own "no literal colours below the token blocks"
				// rule exists to prevent.
				t.Errorf("[%s] %s on %s: one of the tokens is not defined in this theme (fg=%v bg=%v)", theme, p.fg, p.bg, okFg, okBg)
				continue
			}
			if ratio := contrastRatio(fg, bg); ratio < 4.5 {
				t.Errorf("[%s] %s on %s has contrast %.2f:1, below the WCAG AA minimum of 4.5:1 for normal text. "+
					"An operator reads this on a device in an aisle; adjust the token, not the place it is used.",
					theme, p.fg, p.bg, ratio)
			}
		}
	}
}

// --- structural criteria ---------------------------------------------------

// TestOperatorWidthLayoutInvariants asserts the 47.6.3 structure exists: below
// the operator breakpoint the sidebar must leave the layout flow, and a wide
// table must scroll inside its own panel rather than pushing the page sideways.
//
// Asserted against the stylesheet rather than a rendered browser because that
// is what makes it cheap enough to run on every build - and because the
// regression this guards against is somebody deleting a rule, which is exactly
// what a source assertion catches.
func TestOperatorWidthLayoutInvariants(t *testing.T) {
	css := readPublicAsset(t, "styles.css")

	breakpoint := "@media (max-width: 820px) {"
	i := strings.Index(css, breakpoint)
	if i < 0 {
		t.Fatal("the operator-width breakpoint (max-width: 820px) is gone from styles.css. " +
			"Without it the 270px sidebar takes 69% of a 390px device and the application gets 120px - audit A-06/A-39.")
	}
	// Read to the end of the media block by brace balance, so a rule added
	// inside it later is still covered.
	block := balancedBlock(css[i+len(breakpoint)-1:])

	required := []struct{ needle, why string }{
		{"transform: translateX(-100%)", "the sidebar must go off-canvas at operator widths, not merely narrow"},
		{"overflow-x: auto", "a wide table must scroll inside .table-panel; without it the PAGE scrolls sideways and columns are unreachable"},
		{"flex-wrap: wrap", "filter/action rows must reflow rather than overflow"},
		{"min-height: 44px", "the controls an operator drives on a device need a real touch target"},
	}
	for _, r := range required {
		if !strings.Contains(block, r.needle) {
			t.Errorf("the operator breakpoint no longer contains %q - %s", r.needle, r.why)
		}
	}

	// SC 1.4.10 Reflow: 320 CSS px is the criterion's own threshold.
	if !strings.Contains(css, "@media (max-width: 400px) {") {
		t.Error("the narrow-reflow block (max-width: 400px) is gone; WCAG 2.2 SC 1.4.10 requires the page to remain usable without 2D scrolling down to 320 CSS px")
	}
}

// TestGlobalFocusAndTargetSizeInvariants covers the two WCAG 2.2 AA criteria
// that are decidable from the stylesheet alone.
func TestGlobalFocusAndTargetSizeInvariants(t *testing.T) {
	css := readPublicAsset(t, "styles.css")

	// SC 2.4.7 Focus Visible - and specifically a GLOBAL rule. Before Stage
	// 47.6.4 there were focus rings on about eight components out of the whole
	// application, so keyboard and scanner-driven navigation was invisible
	// everywhere else.
	if !strings.Contains(css, ":where(a, button, input, select, textarea, summary, [tabindex]):focus-visible") {
		t.Error("the global :focus-visible rule is gone. A focus ring on a handful of components is not SC 2.4.7 - " +
			"an operator driving this with a ring scanner has nothing else to tell them where they are.")
	}
	// SC 2.4.11 Focus Not Obscured (Minimum), new in WCAG 2.2: the ring must
	// not be hidden behind a sticky header.
	// Anchored on the GLOBAL selector, not on the first ":focus-visible {" in
	// the file - there are component-level focus rules far above it, and an
	// earlier draft of this assertion matched one of those and reported a
	// missing z-index that was never missing.
	focusRule := sliceAfter(css, ":where(a, button, input, select, textarea, summary, [tabindex]):focus-visible", 400)
	if !strings.Contains(focusRule, "z-index") {
		t.Error("the global focus rule no longer lifts the focused control above sticky headers (SC 2.4.11 Focus Not Obscured)")
	}

	// SC 2.5.8 Target Size (Minimum) is 24x24 CSS px at Level AA.
	if !strings.Contains(css, "min-width: 24px") || !strings.Contains(css, "min-height: 24px") {
		t.Error("the 24x24 minimum target size for .action-btn/.btn/.icon-btn is gone (WCAG 2.2 SC 2.5.8, Level AA)")
	}
}

// TestApplicationShellInputsAreLabelled checks SC 1.3.1/3.3.2 for the static
// shell. A placeholder is deliberately NOT accepted as a label: it disappears
// the moment the field has content, which is precisely when a returning
// operator needs to know what the field is.
func TestApplicationShellInputsAreLabelled(t *testing.T) {
	html := readPublicAsset(t, "index.html")

	labelFor := map[string]bool{}
	for _, m := range regexp.MustCompile(`<label[^>]*\sfor="([^"]+)"`).FindAllStringSubmatch(html, -1) {
		labelFor[m[1]] = true
	}

	inputRe := regexp.MustCompile(`(?s)<(input|select|textarea)\b[^>]*>`)
	idRe := regexp.MustCompile(`\sid="([^"]+)"`)
	typeRe := regexp.MustCompile(`\stype="([^"]+)"`)

	// A control WRAPPED in a <label> is implicitly associated and needs no
	// for= at all - that is valid HTML and valid WCAG, and an earlier draft of
	// this test reported two correctly-labelled checkboxes as failures because
	// it only looked for explicit association.
	wrappedByLabel := func(start int) bool {
		open := strings.LastIndex(html[:start], "<label")
		if open < 0 {
			return false
		}
		return !strings.Contains(html[open:start], "</label>")
	}

	var unlabelled []string
	for _, loc := range inputRe.FindAllStringIndex(html, -1) {
		tag := html[loc[0]:loc[1]]
		typ := ""
		if m := typeRe.FindStringSubmatch(tag); m != nil {
			typ = strings.ToLower(m[1])
		}
		// Hidden fields carry no visible affordance, and submit buttons carry
		// their own accessible name in `value`.
		if typ == "hidden" || typ == "submit" || typ == "button" {
			continue
		}
		if strings.Contains(tag, "aria-label=") || strings.Contains(tag, "aria-labelledby=") {
			continue
		}
		if wrappedByLabel(loc[0]) {
			continue
		}
		id := ""
		if m := idRe.FindStringSubmatch(tag); m != nil {
			id = m[1]
		}
		if id != "" && labelFor[id] {
			continue
		}
		name := id
		if name == "" {
			name = strings.TrimSpace(tag)
			if len(name) > 70 {
				name = name[:70] + "..."
			}
		}
		unlabelled = append(unlabelled, name)
	}

	if len(unlabelled) > 0 {
		t.Errorf("%d control(s) in the application shell have no <label for>, aria-label or aria-labelledby "+
			"(WCAG SC 1.3.1 / 3.3.2 - a placeholder does not count, it vanishes as soon as the field has content):\n  %s",
			len(unlabelled), strings.Join(unlabelled, "\n  "))
	}
}

// TestRFOutcomeVocabularyIsComplete guards 47.6.4's own list. The item names
// six outcomes because an operator has to tell them apart without reading, and
// collapsing any two of them back into a single "error" is the regression.
func TestRFOutcomeVocabularyIsComplete(t *testing.T) {
	js := readPublicAsset(t, "app.js")
	css := readPublicAsset(t, "styles.css")

	for _, outcome := range []string{"ok", "duplicate", "wrong_item", "owner_mismatch", "hold", "error", "offline"} {
		if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(outcome) + `:\s*\{\s*vibrate:`).MatchString(js) {
			t.Errorf("the RF outcome %q has no entry in RF_OUTCOMES; it would fall through with no sound or vibration at all", outcome)
		}
		if !strings.Contains(css, ".rf-status-"+outcome+" ") && !strings.Contains(css, ".rf-status-"+outcome+" {") {
			t.Errorf("the RF outcome %q has no .rf-status-%s style; colour and border weight are two of its four signal channels", outcome, outcome)
		}
	}

	// Each outcome must be distinguishable without colour, so the tones must
	// actually differ from one another.
	toneRe := regexp.MustCompile(`tone:\s*(\d+)`)
	seen := map[string]string{}
	entryRe := regexp.MustCompile(`(?m)^\s*([a-z_]+):\s*\{[^}]*\}`)
	for _, m := range entryRe.FindAllStringSubmatch(sliceAfter(js, "const RF_OUTCOMES = {", 1400), -1) {
		tm := toneRe.FindStringSubmatch(m[0])
		if tm == nil {
			continue
		}
		if prev, dup := seen[tm[1]]; dup {
			t.Errorf("outcomes %q and %q share the tone %sHz; an operator cannot tell them apart without looking", prev, m[1], tm[1])
		}
		seen[tm[1]] = m[1]
	}
}

// --- small parsing helpers -------------------------------------------------

// balancedBlock returns the text of a brace-balanced block starting at the
// first "{" of s.
func balancedBlock(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

// sliceAfter returns up to n bytes following marker, or "" when absent.
func sliceAfter(s, marker string, n int) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	end := i + n
	if end > len(s) {
		end = len(s)
	}
	return s[i:end]
}

var _ = fmt.Sprintf // keep fmt imported for future assertions without churn
