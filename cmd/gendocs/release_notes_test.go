package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestDelinkRepoFileReferencesDropsRelativeMDLinkOnly(t *testing.T) {
	got := delinkRepoFileReferences("Full detail in [micro_checklist.md](micro_checklist.md) Stage 35.4.")
	want := "Full detail in micro_checklist.md Stage 35.4."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDelinkRepoFileReferencesLeavesExternalLinkAlone(t *testing.T) {
	text := "See the [dashboard](https://grafana.example/d/api-latency) for detail."
	if got := delinkRepoFileReferences(text); got != text {
		t.Fatalf("external link was altered: got %q", got)
	}
}

func TestDelinkRepoFileReferencesLeavesNonMDRelativeLinkAlone(t *testing.T) {
	text := "See the [report](report.csv) for detail."
	if got := delinkRepoFileReferences(text); got != text {
		t.Fatalf("non-.md relative link was altered: got %q", got)
	}
}

func TestParseLedgerEntriesDelinksExcerpt(t *testing.T) {
	source := "## 1. Example Stage (2026-09-06, code + docs)\n\n" +
		"Full item-by-item detail in [micro_checklist.md](micro_checklist.md) Stage 1.\n"
	entries := parseLedgerEntries(source)
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if want := "Full item-by-item detail in micro_checklist.md Stage 1."; entries[0].Excerpt != want {
		t.Fatalf("got %q, want %q", entries[0].Excerpt, want)
	}
}

// The release notes spill onto further pages as the ledger grows: every entry
// appears exactly once, the newest stay on the first page, and no page
// outgrows its share.
func TestReleaseNotesSplitKeepsEveryEntry(t *testing.T) {
	var ledger strings.Builder
	const total = 400
	for n := total; n >= 1; n-- {
		ledger.WriteString(fmt.Sprintf("## %d. Stage %d - entry title %d (2026-%02d-%02d, code)\n\n%s\n\n",
			n, n, n, 1+n%12, 1+n%28, strings.Repeat("Opening paragraph words. ", 12)))
	}
	pages := kbReleaseNotes("2026-10-18", ledger.String())
	if len(pages) < 2 {
		t.Fatalf("a %d-entry ledger stayed on %d page", total, len(pages))
	}
	first, ok := pages["release-notes.md"]
	if !ok || !strings.Contains(first, fmt.Sprintf("**Stage %d - entry title %d**", total, total)) {
		t.Fatal("the first page must keep its name and the newest entry")
	}
	all := ""
	for name, body := range pages {
		if len(body) > 2*kbReleaseNotesPartBytes {
			t.Errorf("%s is %d bytes", name, len(body))
		}
		all += body
	}
	for n := 1; n <= total; n++ {
		if c := strings.Count(all, fmt.Sprintf("**Stage %d - entry title %d**", n, n)); c != 1 {
			t.Errorf("entry %d appears %d times across the pages", n, c)
		}
	}
}
