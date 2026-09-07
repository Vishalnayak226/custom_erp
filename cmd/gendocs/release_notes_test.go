package main

import "testing"

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
