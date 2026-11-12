package engines

import (
	"encoding/json"
	"fmt"
	"testing"

	"custom_erp/db"
)

// Stage 146.2.10 - an online order earns points when it is delivered; they
// wait out the return window and a return inside it takes them back.
func TestStage146210OnlineOrderEarnsPendingPoints(t *testing.T) {
	e := newCRMTestEnv(t)
	if !GetSettingBool(e.tenantID, "loyalty.online_order_points") {
		t.Fatal("loyalty.online_order_points must default to on")
	}
	e.set("loyalty.rupees_per_point", "100")
	e.set("sales.return_window_days", "7")
	cust, phone := e.customer("Meera Iyer", nil)
	walk, _ := e.customer("Walk In", map[string]interface{}{"walk_in": "Yes"})

	seq := 0
	order := func(customerField map[string]interface{}, total float64, bookings int) (string, []string) {
		seq++
		id := fmt.Sprintf("SO-T146210-%s-%d", e.tenantID[len(e.tenantID)-4:], seq)
		d := map[string]interface{}{"channel": "Shopify", "total_amount": total, "order_status": "Shipped", "customer_name": "x"}
		for k, v := range customerField {
			d[k] = v
		}
		raw, _ := json.Marshal(d)
		e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrder', $2, 'Shipped', 'test')`, id, raw)
		var bs []string
		for i := 0; i < bookings; i++ {
			b := fmt.Sprintf("%s-B%d", id, i)
			br, _ := json.Marshal(map[string]interface{}{"order_id": id, "status": "In-Transit", "tracking_number": "T"})
			e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'LogisticsBooking', $2, 'In-Transit', 'test')`, b, br)
			bs = append(bs, b)
		}
		return id, bs
	}
	deliver := func(b string) {
		t.Helper()
		if err := RecordDeliveryEvent(e.tenantID, b, "Delivered", "test"); err != nil {
			t.Fatal(err)
		}
	}
	earned := func(id string) int {
		return e.count(`SELECT COALESCE(SUM(points), 0) FROM {s}.loyalty_point_ledger WHERE reference_doctype = 'SalesOrder' AND reference_id = $1 AND transaction_type = 'Earn'`, id)
	}

	// Two parcels: nothing until the last one is delivered; then once only.
	so, bs := order(map[string]interface{}{"customer_id": cust}, 1000, 2)
	deliver(bs[0])
	if n := earned(so); n != 0 {
		t.Fatalf("earned %d with one parcel still on the way", n)
	}
	deliver(bs[1])
	if n := earned(so); n != 10 {
		t.Fatalf("earned %d, want 10 (1000 at 100 a point)", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.loyalty_point_ledger WHERE reference_doctype = 'SalesOrder' AND reference_id = $1 AND pending_until > NOW() + interval '6 days' AND pending_until < NOW() + interval '8 days' AND expires_at IS NOT NULL`, so); n != 1 {
		t.Errorf("the row must wait out the 7-day return window and carry an expiry (rows matching: %d)", n)
	}
	if bal, _ := GetLoyaltyBalance(e.tenantID, cust); bal != 0 {
		t.Errorf("pending points must not be usable yet, balance %d", bal)
	}
	if ctx, _ := TillCustomerContextFor(e.tenantID, cust); ctx.Pending != 10 {
		t.Errorf("till pending %d, want 10", ctx.Pending)
	}
	if err := loyaltyOnlineOrderDelivered(e.tenantID, so); err != nil || earned(so) != 10 {
		t.Errorf("a replay must not pay twice: err %v, earned %d", err, earned(so))
	}

	// A return inside the window takes the points back in proportion, pending with them.
	refund := func(id string, net float64) string {
		rid := "RF-" + id
		raw, _ := json.Marshal(map[string]interface{}{"original_order_id": so, "status": "Processed", "net_amount": net, "points_value": 0})
		e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'RefundRequest', $2, 'Processed', 'test')`, rid, raw)
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		n, err := reverseOnlineOrderPointsTx(tx, e.schema, rid, so)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		_ = n
		return rid
	}
	refund("1", 500)
	if n := e.count(`SELECT COALESCE(SUM(points), 0) FROM {s}.loyalty_point_ledger WHERE reference_doctype = 'RefundRequest' AND transaction_type = 'Reversal' AND customer_id = $1`, cust); n != 5 {
		t.Errorf("half returned: reversed %d, want 5", n)
	}
	if ctx, _ := TillCustomerContextFor(e.tenantID, cust); ctx.Pending != 5 {
		t.Errorf("pending after a half return %d, want 5", ctx.Pending)
	}
	refund("2", 1000)
	if ctx, _ := TillCustomerContextFor(e.tenantID, cust); ctx.Pending != 0 {
		t.Errorf("pending after the whole order came back %d, want 0", ctx.Pending)
	}

	// Past the window the points of a kept order become usable.
	so2, bs2 := order(map[string]interface{}{"customer_phone": phone}, 2000, 1)
	deliver(bs2[0])
	if n := earned(so2); n != 20 {
		t.Fatalf("a customer found by phone earned %d, want 20", n)
	}
	e.exec(`UPDATE {s}.loyalty_point_ledger SET pending_until = NOW() - interval '1 minute' WHERE reference_id = $1`, so2)
	if bal, _ := GetLoyaltyBalance(e.tenantID, cust); bal != 20 {
		t.Errorf("after the window the balance is %d, want 20", bal)
	}

	// Not paid: walk-in, setting off, module off.
	soW, bw := order(map[string]interface{}{"customer_id": walk}, 1000, 1)
	deliver(bw[0])
	if n := earned(soW); n != 0 {
		t.Errorf("walk-in earned %d", n)
	}
	e.set("loyalty.online_order_points", "false")
	soOff, bo := order(map[string]interface{}{"customer_id": cust}, 1000, 1)
	deliver(bo[0])
	if n := earned(soOff); n != 0 {
		t.Errorf("setting off but earned %d", n)
	}
	e.set("loyalty.online_order_points", "true")
	e.exec(`UPDATE {s}.module_entitlements SET enabled = false WHERE module_key = 'crm_loyalty'`)
	soMod, bm := order(map[string]interface{}{"customer_id": cust}, 1000, 1)
	deliver(bm[0])
	if n := earned(soMod); n != 0 {
		t.Errorf("loyalty module off but earned %d", n)
	}
}
