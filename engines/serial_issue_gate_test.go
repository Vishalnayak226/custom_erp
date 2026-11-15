package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// 103.4.1: the serial gate runs wherever a lot's stock is issued, and an
// issue of one unit is replay-safe, race-safe and leaves nothing half-done
// when the stock side refuses.
func TestSerialIssueGateAndLotIssue(t *testing.T) {
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
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	const loc, bin, plain, sku = "LOC-SG", "SG-BIN", "SG-PLAIN", "SG-BS"
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", loc, fmt.Sprintf(`{"code":%q,"name":%q}`, loc, loc))
	for sk, mode := range map[string]string{plain: "None", sku: TrackingBatchAndSerial} {
		data, _ := json.Marshal(map[string]interface{}{"code": sk, "name": sk, "tracking_mode": mode, "standard_cost": 10})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sk, data)
	}
	if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: "LOT-1", Item: sku, ExpiryDate: isoDaysFromNow(100)}, 0, "system"); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')", "BIN-"+bin, fmt.Sprintf(`{"bin_code":%q,"location":%q,"status":"Active"}`, bin, loc))
	exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $3, 5, 'Good')", bin, sku, loc)
	exec("INSERT INTO "+schema+".bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty) VALUES ($1, $2, 'LOT-1', 'Good', $3, 5)", bin, sku, loc)
	for _, sn := range []string{"S-1", "S-2", "S-3", "S-4", "S-5"} {
		if _, err := RegisterSerial(tenantID, SerialInfo{SerialNo: sn, Item: sku, BatchNo: "LOT-1"}, loc, "GRN", "GRN-SG", "system"); err != nil {
			t.Fatalf("RegisterSerial %s: %v", sn, err)
		}
	}
	lotQty := func() int {
		var q int
		db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock_batch WHERE bin_code = $1 AND sku = $2 AND batch_no = 'LOT-1'", bin, sku).Scan(&q)
		return q
	}
	status := func(sn string) string {
		u, _ := GetSerial(tenantID, sku, sn)
		if u == nil {
			return ""
		}
		return u.Status
	}

	t.Run("gate", func(t *testing.T) {
		if err := ValidateSerialForIssue(tenantID, plain, ""); err != nil {
			t.Errorf("an untracked item needs no serial: %v", err)
		}
		if c := code(ValidateSerialForIssue(tenantID, sku, "")); c != "INVENT-0115" {
			t.Errorf("blank serial on a serial item: want INVENT-0115, got %q", c)
		}
		if c := code(ValidateSerialForIssue(tenantID, sku, "NOPE")); c != "INVENT-0115" {
			t.Errorf("unregistered serial: want INVENT-0115, got %q", c)
		}
		if err := TransitionSerialStatus(tenantID, sku, "S-5", SerialScrapped, "WO", "WO-1", "damaged", "system"); err != nil {
			t.Fatal(err)
		}
		if c := code(ValidateSerialForIssue(tenantID, sku, "S-5")); c != "INVENT-0104" {
			t.Errorf("scrapped serial: want INVENT-0104, got %q", c)
		}
		if c := code(IssueSerialFromLot(tenantID, bin, sku, "LOT-1", "S-5", "Good", "", "V-0", "system")); c != "INVENT-0104" || lotQty() != 5 {
			t.Errorf("a scrapped unit must not issue (code %q, lot qty %d)", c, lotQty())
		}
	})

	t.Run("issue, replay, wrong lot, failed consume", func(t *testing.T) {
		if err := IssueSerialFromLot(tenantID, bin, sku, "LOT-1", "S-1", "Good", "", "V-1", "system"); err != nil {
			t.Fatalf("issue: %v", err)
		}
		if st := status("S-1"); st != SerialAllocated || lotQty() != 4 {
			t.Fatalf("after issue: status %s, lot qty %d (want Allocated, 4)", st, lotQty())
		}
		if c := code(IssueSerialFromLot(tenantID, bin, sku, "LOT-1", "S-1", "Good", "", "V-1", "system")); c != "INVENT-0104" || lotQty() != 4 {
			t.Errorf("replay must be refused without a second consume (code %q, lot qty %d)", c, lotQty())
		}
		if c := code(IssueSerialFromLot(tenantID, bin, sku, "LOT-9", "S-2", "Good", "", "V-2", "system")); c != "INVENT-0104" || status("S-2") != SerialInStock {
			t.Errorf("a unit of another lot must be refused and left alone (code %q, status %s)", c, status("S-2"))
		}
		// Partial failure: the stock side refuses (no such bin) - the unit goes back.
		if err := IssueSerialFromLot(tenantID, "NO-BIN", sku, "LOT-1", "S-2", "Good", "", "V-2", "system"); err == nil {
			t.Fatal("a consume from a bin that holds nothing must fail")
		}
		if st := status("S-2"); st != SerialInStock || lotQty() != 4 {
			t.Errorf("after a failed consume S-2 must be InStock again (status %s, lot qty %d)", st, lotQty())
		}
		if u, _ := GetSerial(tenantID, sku, "S-2"); u == nil || u.ReservedFor != "" {
			t.Errorf("a handed-back unit keeps no reservation: %+v", u)
		}
	})

	t.Run("concurrent issues of one unit", func(t *testing.T) {
		before := lotQty()
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if err := IssueSerialFromLot(tenantID, bin, sku, "LOT-1", "S-3", "Good", "", fmt.Sprintf("V-3-%d", i), "system"); err == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()
		if wins != 1 || lotQty() != before-1 || status("S-3") != SerialAllocated {
			t.Errorf("exactly one issue must win: wins %d, lot qty %d (was %d), status %s", wins, lotQty(), before, status("S-3"))
		}
	})
}
