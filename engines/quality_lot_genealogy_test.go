package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// 170.14: a work order completion records the input lots behind its output
// lot, and a trace reads that both ways.
func TestLotGenealogyTraceBothWays(t *testing.T) {
	m := newMC1481(t)
	rm := m.item(nil)
	lotIn, lotOut := m.code("RMLOT"), m.code("FGLOT")
	fg, bom := m.mc1481Bom(rm, map[string]interface{}{"tracking_mode": TrackingBatch})
	t.Cleanup(func() {
		db.DB.Exec("DELETE FROM "+m.schema+".documents WHERE doctype = 'LotGenealogy' AND data->>'output_batch' = $1", lotOut)
	})
	// The raw lot came in on a vendor receipt.
	m.doc(m.id("BATCH"), "Batch", "Active", map[string]interface{}{"item": rm, "batch_no": lotIn, "vendor": "VEND-170", "grn": "GRN-170", "received_on": "2026-01-02"})
	m.receive(rm, 4, 50)

	id := m.id("PRO")
	lots, _ := json.Marshal([]map[string]interface{}{{"sku": rm, "batch_no": lotIn, "qty": 4}})
	m.doc(id, "ProductionOrder", "Draft", map[string]interface{}{"bom_id": bom, "quantity": 2.0, "location": m.loc, "status": "Draft", "input_lots": string(lots)})
	if err := IssueProductionMaterial(m.tenant, id); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := CompleteProductionOrder(m.tenant, id, OutputPieces{BatchNo: lotOut}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	rowsFor := func(sku, lot, dir string) (made, from, recv []LotTraceRow) {
		rows, err := TraceLot(m.tenant, sku, lot, dir)
		if err != nil {
			t.Fatalf("trace %s: %v", lot, err)
		}
		for _, r := range rows {
			switch r.Relation {
			case "Made into":
				made = append(made, r)
			case "Made from":
				from = append(from, r)
			case "Received":
				recv = append(recv, r)
			}
		}
		return
	}
	// Backward from the output lot: the input lot, then its vendor receipt.
	_, from, recv := rowsFor(fg, lotOut, TraceBackward)
	if len(from) != 1 || from[0].BatchNo != lotIn || from[0].Qty != 4 || from[0].Source != "Recorded" {
		t.Fatalf("backward: %+v", from)
	}
	if len(recv) != 1 || recv[0].Party != "VEND-170" || recv[0].Document != "GRN-170" {
		t.Fatalf("vendor receipt: %+v", recv)
	}
	// Forward from the raw lot: it was made into the output lot.
	made, _, _ := rowsFor(rm, lotIn, TraceForward)
	if len(made) != 1 || made[0].BatchNo != lotOut || made[0].Sku != fg {
		t.Fatalf("forward: %+v", made)
	}
	// Replaying the record of the same completion writes nothing twice.
	count := func() (n int) {
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+m.schema+".documents WHERE doctype = 'LotGenealogy' AND data->>'output_batch' = $1", lotOut).Scan(&n)
		return
	}
	if count() != 1 {
		t.Fatalf("rows after completion: %d", count())
	}
	for i := 0; i < 2; i++ {
		recordLotGenealogy(m.tenant, id, fg, lotOut, 2, 1)
	}
	if n := count(); n > 2 {
		t.Fatalf("rows after replay: %d", n)
	}
	// An unnamed output records nothing; a missing lot or direction is refused.
	recordLotGenealogy(m.tenant, id, fg, "", 2, 9)
	if _, err := TraceLot(m.tenant, fg, "", ""); err == nil || err.(*ValidationError).Code != "MFG-1760" {
		t.Fatalf("blank lot: %v", err)
	}
	if _, err := TraceLot(m.tenant, fg, lotOut, "Sideways"); err == nil || err.(*ValidationError).Code != "MFG-1761" {
		t.Fatalf("bad direction: %v", err)
	}
}
