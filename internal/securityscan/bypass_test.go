package securityscan

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Stage 49.1.4's reviewed no-bypass inventory.
//
// Every block-severity hit the scanner can find in this tree is listed here
// with the reason it is not a bypass and the control that keeps it from
// becoming one. The count is part of the entry: a file that gains a second
// hit in the same category fails this test, so "there is a reviewed reason for
// this one line" cannot silently become cover for a new one on the line below.
//
// The prose here is the deliverable, not the map. If an entry cannot be
// written as a sentence that survives being read out loud in a review, the
// right answer is to remove the code, not to word the entry more carefully.
var reviewedBypassFindings = map[string]map[string]struct {
	Count  int
	Reason string
}{
	"seeded-credential": {
		"db/migration.sql": {4, `The four bootstrap accounts (admin, cashier1, manager1, system) a fresh
			database is created with. They are a known credential in every deployment that ran this
			file, which is why engines/security_baseline.go's SB-021 refuses an ENV=production boot
			while any of them is still active. Removing them entirely needs pgcrypto (not otherwise
			used here) to generate a random password in plain SQL, so 49.1.5 owns replacing the seed
			with a one-time bootstrap credential; until then the startup gate is the control.`},
		"engines/auth.go": {1, `seedAdminPasswordHash - the same admin hash again, as the constant
			24.27's startup check compares against. A copy of a hash that is already public in
			db/migration.sql, held so the check can be made at all.`},
		"engines/security_baseline.go": {3, `The other three seeded hashes, for the same reason:
			SB-021 cannot detect a credential it does not hold. Guarded by
			TestSeededCredentialHashesCoverMigrationSQL, which fails if this set and
			db/migration.sql's ever disagree.`},
	},
	"security-disabling-flag": {
		"engines/environment.go": {1, `ERP_DISABLE_EXTERNAL_SIDE_EFFECTS - the 47.0.5 emergency kill
			switch for outbound webhook/email/ops-alert delivery. It disables side effects, never a
			security control: nothing about authentication, authorization or audit changes when it is
			set. It is announced in the startup banner and on GET /api/v1/system/environment, and
			SB-017 reports it as a finding at every production boot so it cannot be left on unnoticed.`},
		"engines/security_baseline.go": {1, `The SB-017 check itself reading that same variable.`},
	},
	"bypass-vocabulary": {
		"engines/pim_bulk.go": {1, `A comment describing a control that prevents a back door
			("keeps the PIM bulk-edit endpoint from becoming a back door to every generic doctype"),
			on the function that enforces it. The phrase is in the inventory because a scanner cannot
			tell a description of a hole from a description of the plug - a human read it and this is
			the plug.`},
	},
}

func TestNoUnreviewedBypassPattern(t *testing.T) {
	findings := cachedBypass(t)

	var blocking []BypassFinding
	for _, f := range findings {
		if f.Severity == BypassBlock {
			blocking = append(blocking, f)
		}
	}
	counts := CountByCategoryAndFile(blocking)

	var problems []string
	for category, files := range counts {
		reviewed := reviewedBypassFindings[category]
		for file, n := range files {
			entry, ok := reviewed[file]
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"  NEW      %s in %s (%d hit(s)) - remove it, convert it to governed break-glass (49.14.5), or add it to reviewedBypassFindings with the reason it is safe",
					category, file, n))
				continue
			}
			if n > entry.Count {
				problems = append(problems, fmt.Sprintf(
					"  INCREASE %s in %s: %d hit(s), %d reviewed - a new one appeared next to a reviewed one; review it on its own merits before raising the count",
					category, file, n, entry.Count))
			}
			if n < entry.Count {
				problems = append(problems, fmt.Sprintf(
					"  STALE    %s in %s: %d hit(s), %d reviewed - one was cleaned up; lower the count so the inventory keeps meaning something",
					category, file, n, entry.Count))
			}
		}
	}
	// A whole reviewed file that no longer produces any hit at all.
	for category, files := range reviewedBypassFindings {
		for file := range files {
			if counts[category] == nil || counts[category][file] == 0 {
				problems = append(problems, fmt.Sprintf(
					"  GONE     %s in %s is reviewed but no longer found - remove the entry",
					category, file))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("the no-bypass inventory (49.1.4) does not match the tree:\n%s", strings.Join(problems, "\n"))
	}
}

// Review-severity hits are reported, never enforced: they are a human reading
// list for the release gate (49.17.2), and a build that fails because someone
// wrote "for now" in a comment teaches people to stop writing comments. This
// test always passes; it exists so the list is printed by `go test -v` and
// lands in the release evidence.
func TestReviewSeverityBypassHitsAreListed(t *testing.T) {
	findings := cachedBypass(t)
	for _, f := range findings {
		if f.Severity == BypassReview {
			t.Logf("review at release: %s %s:%d  %s", f.Category, f.File, f.Line, f.Excerpt)
		}
	}
}

// The scanner's output is printed in CI logs and pasted into review notes, so
// it must never carry the credential it found.
func TestBypassFindingsAreRedacted(t *testing.T) {
	findings := cachedBypass(t)
	seen := false
	for _, f := range findings {
		if f.Category != "seeded-credential" {
			continue
		}
		seen = true
		if redactHash.MatchString(f.Excerpt) || redactPEM.MatchString(f.Excerpt) {
			t.Errorf("%s:%d reports the credential it found instead of a redaction: %s", f.File, f.Line, f.Excerpt)
		}
		if !strings.Contains(f.Excerpt, "redacted") {
			t.Errorf("%s:%d does not look redacted: %s", f.File, f.Line, f.Excerpt)
		}
	}
	if !seen {
		t.Fatal("no seeded-credential findings at all - the scanner is not reading db/migration.sql, so this test proves nothing")
	}
}

// A scanner that matches nothing passes every allowlist test. This asserts it
// can still see the thing it was written to see.
func TestBypassScannerStillDetects(t *testing.T) {
	findings := cachedBypass(t)
	categories := map[string]int{}
	for _, f := range findings {
		categories[f.Category]++
	}
	for _, want := range []string{"seeded-credential", "security-disabling-flag"} {
		if categories[want] == 0 {
			t.Errorf("the %s pattern matched nothing in a tree that is known to contain hits - the pattern has stopped working", want)
		}
	}
}
