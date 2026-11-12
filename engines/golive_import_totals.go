package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Stage 47.12.7: the cutover pack's migration line. Each staged import that
// states control totals (import_declared_totals.go) is shown against what was
// loaded: per import, per column, pass or fail and the difference. An import
// with no stated totals is not listed; with none stated at all the line is out
// of scope, so a client that migrated nothing is not blocked.

const goLiveImportTotalsListed = 6

func checkCutImportTotals(c *goLiveCtx) lineResult {
	const src = "Staged imports · stated control totals vs loaded"
	rows, err := c.rawJSON(`SELECT data, status FROM %s.documents WHERE doctype = 'ImportBatch' AND deleted_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return errLine(src, err)
	}
	var bad, ok int
	var badNotes, okNotes []string
	for _, r := range rows {
		b := &ImportBatchData{}
		if json.Unmarshal(r.data, b) != nil || len(b.Declared) == 0 {
			continue
		}
		deriveImportStage(b, r.status)
		if b.Stage == ImportStageRejected || b.Stage == ImportStageExpired {
			continue
		}
		name := firstNonEmpty(b.Code, b.ID)
		if b.Stage != ImportStageCommitted && b.Stage != ImportStageReconciled {
			bad++
			badNotes = append(badNotes, fmt.Sprintf("%s: not loaded yet (%s)", name, strings.ReplaceAll(b.Stage, "_", " ")))
			continue
		}
		loaded := map[string]float64{}
		cols := b.FileTotals
		if b.Reconcile != nil && len(b.Reconcile.Columns) > 0 {
			cols = b.Reconcile.Columns
		}
		for _, col := range cols {
			loaded[col.Field] = col.Accepted
		}
		var diffs []string
		for _, d := range b.Declared {
			got := loaded[d.Field]
			if roundControl(got) != roundControl(d.Expected) {
				diffs = append(diffs, fmt.Sprintf("%s stated %v, loaded %v, difference %v", firstNonEmpty(d.Label, d.Field), d.Expected, got, roundControl(got-d.Expected)))
			} else if math.IsNaN(got) {
				diffs = append(diffs, fmt.Sprintf("%s has no loaded value", firstNonEmpty(d.Label, d.Field)))
			}
		}
		if len(diffs) > 0 {
			bad++
			badNotes = append(badNotes, fmt.Sprintf("%s: FAIL, %s", name, strings.Join(diffs, "; ")))
		} else {
			ok++
			okNotes = append(okNotes, fmt.Sprintf("%s: pass, %d column(s) tie", name, len(b.Declared)))
		}
	}
	switch {
	case bad == 0 && ok == 0:
		return notApplicable(src, "No staged import states control totals.")
	case bad > 0:
		return fail(src, "%d of %d import(s) do not tie. %s", bad, bad+ok, goLiveImportList(append(badNotes, okNotes...)))
	}
	return pass(src, "%d import(s) tie to their stated totals. %s", ok, goLiveImportList(okNotes))
}

func goLiveImportList(notes []string) string {
	if len(notes) > goLiveImportTotalsListed {
		return strings.Join(notes[:goLiveImportTotalsListed], " | ") + fmt.Sprintf(" | and %d more", len(notes)-goLiveImportTotalsListed)
	}
	return strings.Join(notes, " | ")
}

type goLiveRawRow struct {
	data   []byte
	status string
}

// rawJSON reads (data, status) rows of this client's documents.
func (c *goLiveCtx) rawJSON(q string, args ...interface{}) ([]goLiveRawRow, error) {
	rows, err := goLiveQuery(c, q, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func goLiveQuery(c *goLiveCtx, q string, args ...interface{}) ([]goLiveRawRow, error) {
	rows, err := db.DB.Query(fmt.Sprintf(q, c.schema), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []goLiveRawRow
	for rows.Next() {
		var r goLiveRawRow
		if err := rows.Scan(&r.data, &r.status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
