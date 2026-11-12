package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func seedGoLiveBatch(t *testing.T, schema, id, stage, status string, declared, accepted float64) {
	t.Helper()
	b := ImportBatchData{ID: id, Code: id, Stage: stage, DoctypeName: "JournalVoucher",
		Declared:   []ImportDeclaredTotal{{Field: "amount", Label: "Amount", Expected: declared}},
		FileTotals: []ImportColumnTotal{{Field: "amount", Label: "Amount", Read: declared, Accepted: accepted}},
		Reconcile:  &ImportBatchReconcile{Columns: []ImportColumnTotal{{Field: "amount", Label: "Amount", Read: declared, Accepted: accepted}}}}
	raw, _ := json.Marshal(b)
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'ImportBatch', $2, $3, 'test')`, id, string(raw), status); err != nil {
		t.Fatal(err)
	}
}

// 47.12.7: the cutover pack shows stated control totals against the loaded
// values per import, with pass / fail and the difference.
func TestStage47127CutoverShowsDeclaredTotalsAgainstLoaded(t *testing.T) {
	tenantID, schema, done := goLiveTestTenant(t)
	defer done()
	line := func() *GoLiveLine {
		p, err := GetGoLivePack(tenantID, PackCutover)
		if err != nil {
			t.Fatal(err)
		}
		return goLiveLineByIDIn(p, "cut-import-totals")
	}
	if l := line(); l.State != LineNotApplic {
		t.Fatalf("no import states totals, the line must not block: %+v", l)
	}
	seedGoLiveBatch(t, schema, "OB-TIES", ImportStageReconciled, "Reconciled", 125000.5, 125000.5)
	if l := line(); l.State != LinePass || !strings.Contains(l.Detail, "OB-TIES: pass") {
		t.Fatalf("a tying import passes: %+v", l)
	}
	seedGoLiveBatch(t, schema, "OB-OFF", ImportStageCommitted, "Committed", 1000, 940)
	l := line()
	if l.State != LineFail || !strings.Contains(l.Detail, "OB-OFF: FAIL") || !strings.Contains(l.Detail, "difference -60") || !strings.Contains(l.Detail, "OB-TIES: pass") {
		t.Fatalf("a short import fails with the difference: %+v", l)
	}
	p, _ := GetGoLivePack(tenantID, PackCutover)
	if p.CanSign {
		t.Fatal("a failing migration line must stop the sign-off")
	}
	seedGoLiveBatch(t, schema, "OB-WAIT", ImportStageApproved, "Approved", 5, 0)
	db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE id = 'OB-OFF'`)
	if l := line(); l.State != LineFail || !strings.Contains(l.Detail, "OB-WAIT: not loaded yet") {
		t.Fatalf("an import not loaded yet is open: %+v", l)
	}
	seedGoLiveBatch(t, schema, "OB-REJ", ImportStageRejected, "Rejected", 9, 0)
	if l := line(); strings.Contains(l.Detail, "OB-REJ") {
		t.Fatalf("a rejected import is not part of the cutover: %+v", l)
	}
}
