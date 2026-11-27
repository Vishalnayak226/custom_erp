package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 276.16: every screen uses one type scale - 12, 13.5, 14, 18 and 24 px, plus
// display sizes from 28 px up (big numbers, icons) - and nothing under 12 px.
// The column walk found 19 sizes, with 11 px text on 137 of 220 views. Print
// output is sized for paper and is left alone: @media print, sheets sized in
// mm, and the sticker, invoice, slip and receipt rules. So are the handheld
// screens (rf.css, big on purpose) and the public pages outside the app
// (p/, store/, f/ and the catalog share page).
var typeScale = map[float64]bool{12: true, 13.5: true, 14: true, 18: true, 24: true}

var (
	typeScaleDecl  = regexp.MustCompile(`(?:font-size:\s*|font:\s*(?:[a-z0-9-]+\s+)*?)([0-9.]+)px`)
	typeScalePrint = regexp.MustCompile(`(?i)sticker-label|invoice-sheet|tdp-|receipt|slip|print`)
	typeScaleMM    = regexp.MustCompile(`\d+mm`)
)

func TestPublicTypeScale(t *testing.T) {
	root := "../../public"
	skip := map[string]bool{"rf.css": true, "pim-catalog-share.css": true}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "p", "store", "f", "downloads":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".css") || skip[rel] {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// cssComment is client_weight_test.go's; blanked, so offsets hold.
		css := cssComment.ReplaceAllStringFunc(string(b), func(c string) string {
			return strings.Repeat(" ", len(c))
		})
		for _, bad := range offScaleSizes(css) {
			t.Errorf("%s: font size %s is off the type scale (12, 13.5, 14, 18, 24, or 28+ for display) in %q", rel, bad[0], bad[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// offScaleSizes returns [size, selector] for each screen font size off the scale.
func offScaleSizes(css string) [][2]string {
	var out [][2]string
	var stack []string
	start := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			stack = append(stack, strings.TrimSpace(css[start:i]))
			start = i + 1
		case '}':
			body := css[start:i]
			if len(stack) > 0 && !inPrint(stack) && !typeScaleMM.MatchString(body) {
				for _, m := range typeScaleDecl.FindAllStringSubmatch(body, -1) {
					v, err := strconv.ParseFloat(m[1], 64)
					if err == nil && !typeScale[v] && v < 28 {
						out = append(out, [2]string{m[1] + "px", stack[len(stack)-1]})
					}
				}
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			start = i + 1
		case ';':
			if len(stack) == 0 {
				start = i + 1 // an @import or @charset line
			}
		}
	}
	return out
}

func inPrint(stack []string) bool {
	for _, s := range stack {
		if strings.HasPrefix(s, "@media") && strings.Contains(s, "print") {
			return true
		}
	}
	return typeScalePrint.MatchString(stack[len(stack)-1])
}
