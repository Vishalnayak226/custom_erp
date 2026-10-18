package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// Stage 73.1.8 on a provisioned throwaway client: configurable recount
// triggers (units / percent / value) send a first count back for a blind
// recount instead of to an approver, an uncounted recount line is not a
// count of zero, and an approved variance posts once, in one transaction,
// and only when Approved.
func TestCycleCountRecountTriggersStage7318(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('CycleCountLine', 0, NULL, 'Store Manager') ON CONFLICT (doctype, min_amount) DO NOTHING")
	exec("INSERT INTO " + schema + ".documents (id, doctype, data, status, created_by) VALUES ('RC-CC-THEFT', 'ReasonCode', '{\"code\":\"RC-CC-THEFT\",\"category\":\"Cycle Count Variance\"}', 'Active', 'system')")
	const loc = "LOC-CC-1"
	stock := func(sku string, qty int, cost float64) {
		data, _ := json.Marshal(map[string]interface{}{"code": sku, "name": sku, "standard_cost": cost})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, data)
		exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, loc, qty)
	}
	line := func(id, session, sku string, counted int) {
		data, _ := json.Marshal(map[string]interface{}{"count_session": session, "location": loc, "sku": sku, "counted_qty": counted, "status": "Draft"})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Draft', 'counter')", id, data)
	}
	status := func(id string) (string, map[string]interface{}) {
		var st, raw string
		db.DB.QueryRow("SELECT status, data FROM "+schema+".documents WHERE id = $1", id).Scan(&st, &raw)
		var d map[string]interface{}
		json.Unmarshal([]byte(raw), &d)
		return st, d
	}
	onHand := func(sku string) (n int) {
		db.DB.QueryRow("SELECT on_hand FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&n)
		return
	}

	t.Run("unit trigger: big miss recounted blind, small miss approved, posted once", func(t *testing.T) {
		if err := SetSetting(tenantID, RecountTriggerQtySetting, "5", "system"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		stock("CC-SMALL", 12, 10)
		stock("CC-BIG", 20, 10)
		stock("CC-EXACT", 7, 10)
		line("CCL-S", "SESS-1", "CC-SMALL", 10)
		line("CCL-B", "SESS-1", "CC-BIG", 0)
		line("CCL-E", "SESS-1", "CC-EXACT", 7)

		res, err := ReconcileCycleCountDetailed(tenantID, "SESS-1", "counter", "Store Manager")
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if res.Posted != 1 || res.PendingApproval != 1 || res.RecountRequested != 1 || len(res.Recounts) != 1 {
			t.Fatalf("want 1 posted / 1 pending / 1 recount, got %+v", res)
		}
		if st, d := status("CCL-B"); st != "Recount Requested" || d["recount_trigger"] == nil {
			t.Fatalf("big miss: %s %v", st, d["recount_trigger"])
		}
		recountID := res.Recounts[0].NewLineID
		if _, d := status(recountID); d["counted_qty"] != nil || d["system_qty"] != nil {
			t.Fatalf("the recount line must be blind, got %v", d)
		}

		// Not counted yet: waiting, never read as zero.
		res, err = ReconcileCycleCountDetailed(tenantID, "SESS-1", "counter", "Store Manager")
		if err != nil || res.AwaitingCount != 1 || res.PendingApproval != 0 || res.RecountRequested != 0 {
			t.Fatalf("second reconcile before the recount: %v %+v", err, res)
		}

		if err := SubmitRecountValue(tenantID, recountID, 18, "counter2"); err != nil {
			t.Fatalf("SubmitRecountValue: %v", err)
		}
		res, err = ReconcileCycleCountDetailed(tenantID, "SESS-1", "counter2", "Store Manager")
		if err != nil || res.PendingApproval != 1 || res.RecountRequested != 0 {
			t.Fatalf("recount reconcile: %v %+v", err, res)
		}
		st, d := status(recountID)
		if st != "Pending Approval" || numFromInterface(d["variance"]) != -2 || numFromInterface(d["first_count_qty"]) != 0 {
			t.Fatalf("recount line: %s %v", st, d)
		}

		// Not approved yet: posting is refused.
		if err := PostCycleCountAdjustment(tenantID, recountID, "system"); err == nil {
			t.Fatalf("a Pending Approval line must not post")
		}
		if err := SetCycleCountVarianceReason(tenantID, recountID, "RC-CC-THEFT", "manager"); err != nil {
			t.Fatalf("reason: %v", err)
		}
		if err := DecideApproval(tenantID, "CycleCountLine", recountID, "manager", "Store Manager", loc, "Approved", ""); err != nil {
			t.Fatalf("approve: %v", err)
		}
		// Two posts at once (a double click, a retry): exactly one lands.
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			wg.Add(1)
			go func(i int) { defer wg.Done(); errs[i] = PostCycleCountAdjustment(tenantID, recountID, "manager") }(i)
		}
		wg.Wait()
		ok := 0
		for _, e := range errs {
			if e == nil {
				ok++
			}
		}
		if ok != 1 {
			t.Fatalf("want exactly one successful post, got %d (%v)", ok, errs)
		}
		if n := onHand("CC-BIG"); n != 18 {
			t.Fatalf("CC-BIG on hand %d, want 18", n)
		}
		if st, _ := status(recountID); st != "Posted" {
			t.Fatalf("recount line %s, want Posted", st)
		}
		var ledger int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", recountID).Scan(&ledger)
		if ledger != 1 {
			t.Fatalf("want one ledger line for the posted variance, got %d", ledger)
		}
	})

	t.Run("percent and value triggers", func(t *testing.T) {
		SetSetting(tenantID, RecountTriggerQtySetting, "0", "system")
		if err := SetSetting(tenantID, RecountTriggerPctSetting, "50", "system"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		stock("CC-P40", 10, 1)
		stock("CC-P60", 10, 1)
		line("CCL-P40", "SESS-2", "CC-P40", 6) // 40%
		line("CCL-P60", "SESS-2", "CC-P60", 4) // 60%
		res, err := ReconcileCycleCountDetailed(tenantID, "SESS-2", "counter", "Store Manager")
		if err != nil || res.PendingApproval != 1 || res.RecountRequested != 1 || res.Recounts[0].Sku != "CC-P60" {
			t.Fatalf("percent trigger: %v %+v", err, res)
		}

		SetSetting(tenantID, RecountTriggerPctSetting, "0", "system")
		if err := SetSetting(tenantID, RecountTriggerValueSetting, "500", "system"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		stock("CC-V400", 50, 100)
		stock("CC-V600", 50, 100)
		line("CCL-V400", "SESS-3", "CC-V400", 46) // 4 x 100 = 400
		line("CCL-V600", "SESS-3", "CC-V600", 56) // 6 x 100 = 600
		res, err = ReconcileCycleCountDetailed(tenantID, "SESS-3", "counter", "Store Manager")
		if err != nil || res.PendingApproval != 1 || res.RecountRequested != 1 || res.Recounts[0].Sku != "CC-V600" {
			t.Fatalf("value trigger: %v %+v", err, res)
		}
		fmt.Println("value trigger said:", res.Recounts[0].Trigger)
	})
}
