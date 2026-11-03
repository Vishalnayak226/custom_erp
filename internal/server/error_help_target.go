package server

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"custom_erp/internal/kb"
)

// Stage 47.14.4: where an error code is explained in the in-app help. The
// error code reference is generated one row per code with the row id
// code-<code>; the list runs over several pages (error-code-reference,
// error-code-reference-2, ...), so the page is found by reading them once.
var (
	errorHelpOnce  sync.Once
	errorHelpSlugs map[string]string // anchor -> article slug
	errorHelpRowID = regexp.MustCompile(`<tr id="(code-[^"]+)"`)
)

func loadErrorHelp() {
	errorHelpSlugs = map[string]string{}
	for part := 1; ; part++ {
		slug := "error-code-reference"
		if part > 1 {
			slug = fmt.Sprintf("error-code-reference-%d", part)
		}
		_, html, err := kb.ArticleHTML(slug)
		if err != nil {
			return
		}
		for _, m := range errorHelpRowID.FindAllStringSubmatch(html, -1) {
			errorHelpSlugs[m[1]] = slug
		}
	}
}

// errorHelpTarget returns "<slug>#<anchor>" for a catalogue code, or "" when
// the help has no entry for it.
func errorHelpTarget(code string) string {
	errorHelpOnce.Do(loadErrorHelp)
	anchor := "code-" + kb.HeadingSlug(strings.TrimSpace(code))
	if slug, ok := errorHelpSlugs[anchor]; ok {
		return slug + "#" + anchor
	}
	return ""
}
