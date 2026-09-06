package main

// Stage 39.10 - release notes generated from the ledger's own Stage
// sections, so a second, hand-maintained changelog never drifts from
// docs/project_ledger.md the way any hand-kept copy of a code-owned list
// already drifts (the same reasoning kbdocs.go gives for the other three
// generated articles). The ledger is an engineering build record, not
// customer-facing prose, so this generator does not try to rewrite its
// voice - it extracts the heading and the first paragraph mechanically and
// says so, rather than pretending to summarise.

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// ledgerEntryPattern matches "## <n>. <title> (<date>, <build-type>)" - the
// shape every real Stage section in the ledger uses. Sections 1-3 (project
// genesis, architectural decisions, the phase-1-7 rollout ledger) are
// numbered headings too but carry no trailing "(date, type)", so they never
// match this pattern and are correctly excluded without special-casing them.
var ledgerEntryPattern = regexp.MustCompile(`(?m)^## \d+\.\s+(.+?)\s+\((\d{4}-\d{2}-\d{2}),\s*([^)]+)\)\s*$`)

type ledgerEntry struct {
	Title   string
	Date    string
	Kind    string
	Excerpt string
}

// parseLedgerEntries walks every match of ledgerEntryPattern and takes the
// first paragraph of the body that follows it (up to the first blank line)
// as the excerpt - the ledger's own opening context sentence(s) for that
// Stage, not a rewritten summary.
func parseLedgerEntries(source string) []ledgerEntry {
	locs := ledgerEntryPattern.FindAllStringSubmatchIndex(source, -1)
	entries := make([]ledgerEntry, 0, len(locs))
	for i, loc := range locs {
		bodyStart := loc[1]
		bodyEnd := len(source)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		body := strings.TrimSpace(source[bodyStart:bodyEnd])

		var paragraph []string
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				if len(paragraph) > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, ">") || trimmed == "---" {
				break
			}
			paragraph = append(paragraph, trimmed)
		}

		entries = append(entries, ledgerEntry{
			Title:   strings.TrimSpace(source[loc[2]:loc[3]]),
			Date:    strings.TrimSpace(source[loc[4]:loc[5]]),
			Kind:    strings.TrimSpace(source[loc[6]:loc[7]]),
			Excerpt: delinkRepoFileReferences(strings.Join(paragraph, " ")),
		})
	}
	return entries
}

// mdLink matches a Markdown inline link: `[label](target)`.
var mdLink = regexp.MustCompile(`(!?)\[([^\]\n]*)\]\(([^\s)]+)\)`)

// delinkRepoFileReferences strips the hyperlink from any scheme-less,
// host-less ".md" link copied verbatim out of the ledger's own prose - e.g.
// `[micro_checklist.md](micro_checklist.md)`, which resolves fine from
// docs/project_ledger.md's own directory.
//
// This excerpt is pasted into a Knowledge Center article instead, and
// genkb's renderer (internal/kb/markdown.go's articleURL) rewrites every
// such link into /help/<basename-slug> on the assumption it references
// another KB article. micro_checklist.md is not a KB article and has no
// runtime URL at all - docs/ is not shipped, only the embedded KB is - so
// the rendered link would 404. Keep the visible text, drop the link, rather
// than emit an href genkb cannot resolve. An http(s) link is left untouched;
// genkb only intercepts relative .md links.
func delinkRepoFileReferences(text string) string {
	return mdLink.ReplaceAllStringFunc(text, func(m string) string {
		groups := mdLink.FindStringSubmatch(m)
		label, target := groups[2], groups[3]
		u, err := url.Parse(target)
		if err != nil || u.Scheme != "" || u.Host != "" || !strings.EqualFold(path.Ext(u.Path), ".md") {
			return m
		}
		return label
	})
}

func kbReleaseNotes(stamp, source string) string {
	entries := parseLedgerEntries(source)

	var b strings.Builder
	b.WriteString(kbFrontmatter(
		"Release notes",
		"Reference",
		25,
		"What shipped and when, generated from the build ledger so this page can't say something the ledger doesn't.",
		"everyone",
		stamp,
		nil))

	b.WriteString("# Release notes\n\n")
	b.WriteString(fmt.Sprintf(
		"Generated from `docs/project_ledger.md`'s own Stage sections - **%d** entries as of this build. "+
			"Each excerpt is that section's own opening paragraph, not a rewritten summary, "+
			"so it reads like an engineering build log because that is what it is. "+
			"For the full detail behind any entry, including what was verified and how, read the ledger itself.\n\n",
		len(entries)))

	currentDate := ""
	for _, entry := range entries {
		if entry.Date != currentDate {
			currentDate = entry.Date
			b.WriteString(fmt.Sprintf("## %s\n\n", currentDate))
		}
		b.WriteString(fmt.Sprintf("**%s** *(%s)*\n\n", entry.Title, entry.Kind))
		if entry.Excerpt != "" {
			b.WriteString(entry.Excerpt + "\n\n")
		}
	}
	return b.String()
}
