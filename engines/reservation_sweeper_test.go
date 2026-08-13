package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// The sweeper's safety property is negative: it must NOT release a live order's
// reservation. That is asserted first, because getting it wrong would release
// stock out from under real orders.
func TestReservationSweeperReleasesOnlyWhatNothingNeeds(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'inventory_reservation' AND column_name = 'order_id')`, schema).Scan(&ready); err != nil {
		t.Fatalf("inspect inventory_reservation: %v", err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_3_7_reservation_attribution.sql has not been applied to this database")
	}

	const sku = "TEST-SWEEP-SKU"
	const location = "TEST-SWEEP-LOC"
	const liveLine = "TEST-SWEEP-ORDER-L1"
	const deadLine = "TEST-SWEEP-ORDER-L2"
	const orderID = "TEST-SWEEP-ORDER"

	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1,$2)", liveLine, deadLine)
	}
	cleanup()
	defer cleanup()

	insertLine := func(id, status string) {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{
			"order_id": orderID, "sku": sku, "qty": 1, "location_code": location, "line_status": status,
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,'SalesOrderLine',$2,$3,'system')",
			id, body, status); err != nil {
			t.Fatalf("insert line %s: %v", id, err)
		}
	}
	insertLine(liveLine, "Reserved")
	insertLine(deadLine, "Cancelled")

	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1,$2,100,100,30)",
		sku, location); err != nil {
		t.Fatalf("seed availability: %v", err)
	}

	insertReservation := func(orderRef, lineRef interface{}, qty int, expiresAt time.Time) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO "+schema+`.inventory_reservation (sku, location_code, quantity, reservation_type, expires_at, order_id, line_id)
			VALUES ($1,$2,$3,'Online',$4,$5,$6)`, sku, location, qty, expiresAt, orderRef, lineRef); err != nil {
			t.Fatalf("insert reservation: %v", err)
		}
	}
	// 1. A live order's reservation, long past its nominal expiry. Must survive:
	//    the TTL is a cart-hold rule and says nothing about a confirmed order.
	insertReservation(orderID, liveLine, 10, time.Now().Add(-48*time.Hour))
	// 2. A cancelled line's reservation, nowhere near its expiry. Must go.
	insertReservation(orderID, deadLine, 5, time.Now().Add(48*time.Hour))
	// 3. An abandoned cart hold past its expiry. Must go.
	insertReservation(nil, nil, 15, time.Now().Add(-1*time.Hour))
	// 4. A live cart hold. Must survive.
	insertReservation(nil, nil, 7, time.Now().Add(1*time.Hour))

	// The sweep is schema-wide by design, and this suite shares one database
	// with every other test's fixtures, so the counts it returns are not this
	// test's to assert on. What IS this test's is exactly which of ITS four
	// reservations survived - which is the behaviour that matters anyway.
	if _, err := SweepExpiredReservations("default"); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	var surviving int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".inventory_reservation WHERE sku = $1", sku).Scan(&surviving); err != nil {
		t.Fatalf("count survivors: %v", err)
	}
	if surviving != 2 {
		t.Fatalf("%d reservations survived, want 2 - the live order line and the live cart hold", surviving)
	}
	var liveStillReserved, cancelledStillReserved bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+".inventory_reservation WHERE line_id = $1)", liveLine).Scan(&liveStillReserved); err != nil {
		t.Fatalf("check live line: %v", err)
	}
	if !liveStillReserved {
		t.Fatal("the sweeper released a LIVE order line's reservation - stock was taken from under a real order, which is worse than the leak this sweeper fixes")
	}
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+".inventory_reservation WHERE line_id = $1)", deadLine).Scan(&cancelledStillReserved); err != nil {
		t.Fatalf("check cancelled line: %v", err)
	}
	if cancelledStillReserved {
		t.Fatal("a cancelled line is still holding stock; the sweeper did not release it")
	}
	var expiredHoldRemains bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+
		".inventory_reservation WHERE sku = $1 AND order_id IS NULL AND expires_at < CURRENT_TIMESTAMP)", sku).Scan(&expiredHoldRemains); err != nil {
		t.Fatalf("check expired hold: %v", err)
	}
	if expiredHoldRemains {
		t.Fatal("an expired cart hold is still holding stock; expires_at is still being ignored")
	}

	// The availability read model must have the released quantity back.
	var reserved int
	if err := db.DB.QueryRow("SELECT reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&reserved); err != nil {
		t.Fatalf("read availability: %v", err)
	}
	if reserved != 10 {
		t.Fatalf("reserved = %d, want 10 (30 - 20 released)", reserved)
	}

	// A second sweep must not touch this test's surviving rows again - the
	// sweep has to be safe to run continuously, not just once.
	if _, err := SweepExpiredReservations("default"); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".inventory_reservation WHERE sku = $1", sku).Scan(&surviving); err != nil {
		t.Fatalf("count survivors after second sweep: %v", err)
	}
	if surviving != 2 {
		t.Fatalf("a repeat sweep changed the surviving set to %d rows; the sweep is not idempotent", surviving)
	}
	if err := db.DB.QueryRow("SELECT reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&reserved); err != nil {
		t.Fatalf("read availability after second sweep: %v", err)
	}
	if reserved != 10 {
		t.Fatalf("reserved = %d after a repeat sweep, want 10 - the release was double-counted", reserved)
	}
}

func TestCreateReservationRecordsAttribution(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'inventory_reservation' AND column_name = 'order_id')`, schema).Scan(&ready); err != nil {
		t.Fatalf("inspect inventory_reservation: %v", err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_3_7_reservation_attribution.sql has not been applied to this database")
	}

	const sku = "TEST-ATTR-SKU"
	const location = "TEST-ATTR-LOC"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1,$2,50,50,0)",
		sku, location); err != nil {
		t.Fatalf("seed availability: %v", err)
	}

	if _, err := CreateReservation("default", sku, location, 4, "Online", 600,
		ReservationAttribution{OrderID: "ATTR-ORDER", LineID: "ATTR-ORDER-L1"}); err != nil {
		t.Fatalf("attributed reservation: %v", err)
	}
	var orderID, lineID string
	if err := db.DB.QueryRow("SELECT COALESCE(order_id,''), COALESCE(line_id,'') FROM "+schema+".inventory_reservation WHERE sku = $1", sku).
		Scan(&orderID, &lineID); err != nil {
		t.Fatalf("read attribution: %v", err)
	}
	if orderID != "ATTR-ORDER" || lineID != "ATTR-ORDER-L1" {
		t.Fatalf("attribution = %q/%q, want ATTR-ORDER/ATTR-ORDER-L1", orderID, lineID)
	}

	// Releasing by line id must take THAT row, not the oldest lookalike.
	if _, err := CreateReservation("default", sku, location, 4, "Online", 600); err != nil {
		t.Fatalf("unattributed lookalike: %v", err)
	}
	if err := releaseLineReservation(schema, sku, location, 4, "ATTR-ORDER-L1"); err != nil {
		t.Fatalf("release by line: %v", err)
	}
	var attributedRemaining, totalRemaining int
	if err := db.DB.QueryRow("SELECT COUNT(*) FILTER (WHERE line_id IS NOT NULL), COUNT(*) FROM "+schema+".inventory_reservation WHERE sku = $1", sku).
		Scan(&attributedRemaining, &totalRemaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if attributedRemaining != 0 || totalRemaining != 1 {
		t.Fatalf("after release: %d attributed / %d total remain, want 0/1 - the line's own row must be the one released",
			attributedRemaining, totalRemaining)
	}
}
