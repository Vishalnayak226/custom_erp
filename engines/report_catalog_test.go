package engines

import (
	"context"
	"custom_erp/db"
	"strings"
	"testing"
	"time"
)

// Stage 75.3.5-75.3.7: every wired catalog report runs on a real tenant -
// with no filters, with a date range and with a store - and every row it
// returns carries exactly the catalog's columns. (Their labels are held to
// the spec by TestReportColumnsMatchCatalogSpec.)
func TestCatalogReportsRun(t *testing.T) {
	db.InitDB(testConnStr())
	wired := 0
	for _, spec := range catalogSpecs {
		def, ok := reportRegistry[spec.ID]
		if !ok {
			continue
		}
		wired++
		if def.Category != spec.Module || def.ModuleKey == "" {
			t.Errorf("%s: category %q / module %q, want the catalog's module %q and a module key", spec.ID, def.Category, def.ModuleKey, spec.Module)
		}
		for _, p := range []map[string]string{{}, {"from": "2026-01-01", "to": "2026-12-31"}, {"location": "HQ"}} {
			rows, err := def.Run("default", p)
			if err != nil {
				t.Errorf("%s %v: %v", spec.ID, p, err)
				continue
			}
			for i, row := range rows {
				if len(row) != len(def.Columns)+1 { // + _store
					t.Errorf("%s row %d: %d keys, want %d columns + _store", spec.ID, i, len(row), len(def.Columns))
					break
				}
				for _, c := range def.Columns {
					if _, has := row[c.Key]; !has {
						t.Errorf("%s row %d: no %q", spec.ID, i, c.Key)
					}
				}
			}
		}
	}
	if wired == 0 {
		t.Fatal("no catalog report is wired")
	}
	t.Logf("%d of %d catalog reports wired", wired, len(catalogSpecs))
}

// The catalog's money reports tie to the ledger (73.2.19): each control total
// computes, unfiltered and for a date range.
func TestCatalogControlTotalsCompute(t *testing.T) {
	db.InitDB(testConnStr())
	n := 0
	for _, spec := range catalogSpecs {
		if !HasReportControlTotals(spec.ID) {
			continue
		}
		n++
		for _, p := range []map[string]string{{}, {"from": "2026-01-01", "to": "2026-12-31"}} {
			_, _, _, totals, err := RunReportWithControlTotals(context.Background(), "default", spec.ID, RoleSuperAdmin, "admin", p)
			if err != nil || totals == nil {
				t.Errorf("%s %v: %v (totals %v)", spec.ID, p, err, totals)
				continue
			}
			for _, note := range totals.Notes {
				if strings.HasPrefix(note, "Control totals could not be computed") {
					t.Errorf("%s %v: %s", spec.ID, p, note)
				}
			}
		}
	}
	if n < 6 {
		t.Errorf("want control totals on at least 6 catalog reports, got %d", n)
	}
}

// Every wired report id is a catalog id, and no catalog column key repeats
// within a report (keys come from the catalog's headers).
func TestCatalogWiringIsCatalog(t *testing.T) {
	ids := map[string]bool{}
	for _, s := range catalogSpecs {
		ids[s.ID] = true
		seen := map[string]bool{}
		for _, h := range s.Columns {
			if seen[catKey(h)] {
				t.Errorf("%s: two columns make the key %q", s.ID, catKey(h))
			}
			seen[catKey(h)] = true
		}
	}
	for id := range catalogWiring {
		if !ids[id] || !strings.HasPrefix(id, "cat-") {
			t.Errorf("%s is wired but is not a catalog report", id)
		}
	}
}

// TestCatDayIsTheBusinessDay covers 214.4: a stamp is dated on the business's
// calendar, not sliced from its UTC text, while a plain date stays as it is.
func TestCatDayIsTheBusinessDay(t *testing.T) {
	if BusinessLocation().String() != "Asia/Kolkata" && BusinessLocation().String() != "IST" {
		t.Skip("written for the default India zone")
	}
	for in, want := range map[string]string{
		"2026-11-15T20:00:00Z":      "2026-11-16", // 01:30 IST on the 16th
		"2026-11-15T20:00:00.123Z":  "2026-11-16",
		"2026-11-15 20:00:00+00":    "2026-11-16",
		"2026-11-16T01:00:00+05:30": "2026-11-16",
		"2026-11-15T17:00:00Z":      "2026-11-15", // 22:30 IST
		"2026-11-16":                "2026-11-16",
		"2026-11-16T00:00:00":       "2026-11-16", // no zone: already local
		"":                          "",
	} {
		if got := catDay(in); got != want {
			t.Errorf("catDay(%q) = %q, want %q", in, got, want)
		}
	}
	utc := time.Date(2026, 11, 15, 19, 0, 0, 0, time.UTC)
	if got := catDay(utc); got != "2026-11-16" {
		t.Errorf("catDay(time 19:00 UTC) = %q, want 2026-11-16", got)
	}
	if got := catStamp(utc); got != "2026-11-16 00:30" {
		t.Errorf("catStamp(19:00 UTC) = %q, want 2026-11-16 00:30", got)
	}
}
