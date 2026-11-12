package engines

import (
	"testing"
	"time"

	"custom_erp/db"
)

func holdTestSale(t *testing.T, schema, customer, sku, serial string) error {
	t.Helper()
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	lines := []saleCostLine{{SKU: sku, Qty: 1, unitPaise: 1000, serialNos: []string{serial}}}
	if _, err := settleNamedPiecesTx(tx, schema, lines, false, customer); err != nil {
		return err
	}
	return tx.Commit()
}

func holdCode(err error) string {
	if ve, ok := err.(*ValidationError); ok {
		return ve.Code
	}
	return ""
}

// Stage 148.41: a named hold on a piece. Off: nothing changes. On: the till and
// picking skip it for anyone else, the named customer's sale uses it, expiry
// frees it, an advance stays on the customer's account.
func TestStage14841PieceHold(t *testing.T) {
	tenantID, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	for _, c := range []string{"C-H1", "C-H2"} {
		insert(c, "Customer", "Active", map[string]interface{}{"name": "Cust " + c, "mobile": "99" + c})
	}
	for _, s := range []string{"SN-H1", "SN-H2", "SN-H3", "SN-H4"} {
		insert("S-"+s, "SerialNumber", "InStock", map[string]interface{}{"serial_no": s, "item": "RING-H", "location_code": "WH1", "status": "InStock", "reserved_for": ""})
	}
	tomorrow := time.Now().In(BusinessLocation()).AddDate(0, 0, 1).Format("2006-01-02")
	req := PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H1", CustomerID: "C-H1", Until: tomorrow, Note: "festival"}

	// Off by default: refused, and a sale of any piece is as before.
	if _, err := PlacePieceHold(tenantID, "u1", req); holdCode(err) != "INVENT-1325" {
		t.Fatalf("off: want INVENT-1325 got %v", err)
	}
	if err := holdTestSale(t, schema, "C-H2", "RING-H", "SN-H4"); err != nil {
		t.Fatalf("off: sale must work: %v", err)
	}
	if err := SetSetting(tenantID, SettingPieceHolds, "true", "u1"); err != nil {
		t.Fatal(err)
	}

	// Refusals.
	bad := map[string]struct {
		r    PieceHoldRequest
		code string
	}{
		"no customer":   {PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H1", Until: tomorrow}, "INVENT-1327"},
		"unknown piece": {PieceHoldRequest{SKU: "RING-H", SerialNo: "NOPE", CustomerID: "C-H1"}, "INVENT-1327"},
		"past expiry":   {PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H1", CustomerID: "C-H1", Until: "2020-01-01"}, "INVENT-1329"},
		"sold piece":    {PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H4", CustomerID: "C-H1"}, "INVENT-1327"},
	}
	for name, c := range bad {
		if _, err := PlacePieceHold(tenantID, "u1", c.r); holdCode(err) != c.code {
			t.Errorf("%s: want %s got %v", name, c.code, err)
		}
	}

	// A hold with an advance.
	req.AdvanceRupees, req.Location = 500, "WH1"
	view, err := PlacePieceHold(tenantID, "u1", req)
	if err != nil || view.CustomerID != "C-H1" || view.Until != tomorrow || view.Advance == "" {
		t.Fatalf("place: %+v %v", view, err)
	}
	if adv, err := LoadCustomerAdvance(tenantID, view.Advance); err != nil || adv == nil || adv.CustomerID != "C-H1" || adv.Balance != 500 {
		t.Fatalf("advance: %+v %v", adv, err)
	}
	if _, err := PlacePieceHold(tenantID, "u1", req); holdCode(err) != "INVENT-1328" {
		t.Errorf("held twice: %v", err)
	}
	// A piece a voucher reserved is not held over (reserved_for is the voucher's).
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"reserved_for\":\"TO-1\"}', status='Allocated' WHERE id = 'S-SN-H2'"); err != nil {
		t.Fatal(err)
	}
	if _, err := PlacePieceHold(tenantID, "u1", PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H2", CustomerID: "C-H1"}); holdCode(err) != "INVENT-1327" {
		t.Errorf("reserved piece: %v", err)
	}
	var reserved string
	_ = db.DB.QueryRow("SELECT data->>'reserved_for' FROM " + schema + ".documents WHERE id = 'S-SN-H1'").Scan(&reserved)
	if reserved != "" {
		t.Errorf("a hold must not write reserved_for: %q", reserved)
	}

	// Anyone else is skipped: the till (another customer, walk-in) and picking.
	for _, who := range []string{"C-H2", ""} {
		if err := holdTestSale(t, schema, who, "RING-H", "SN-H1"); holdCode(err) != "INVENT-1326" {
			t.Errorf("sale to %q: want INVENT-1326 got %v", who, err)
		}
	}
	insert("SO-H2", "SalesOrder", "Open", map[string]interface{}{"customer_id": "C-H2"})
	insert("SO-H1", "SalesOrder", "Open", map[string]interface{}{"customer_id": "C-H1"})
	task := &pickPackTask{id: "T-H", data: map[string]interface{}{"location_code": "WH1", "order_id": "SO-H2"}}
	if err := task.checkScannedPiece(tenantID, schema, "RING-H", "SN-H1"); err == nil {
		t.Errorf("picking for another customer's order must skip a held piece")
	}
	task.data["order_id"] = "SO-H1"
	if err := task.checkScannedPiece(tenantID, schema, "RING-H", "SN-H1"); err != nil {
		t.Errorf("picking for the holder's order: %v", err)
	}
	// An allocation to someone else's order through the status route is refused too.
	if err := TransitionSerialStatus(tenantID, "RING-H", "SN-H1", SerialAllocated, "SalesOrder", "SO-H2", "", "u1"); holdCode(err) != "INVENT-1326" {
		t.Errorf("allocate to other order: %v", err)
	}

	// The named customer's sale uses it, and clears the hold.
	if err := holdTestSale(t, schema, "C-H1", "RING-H", "SN-H1"); err != nil {
		t.Fatalf("holder's sale: %v", err)
	}
	var st, hc string
	_ = db.DB.QueryRow("SELECT status, COALESCE(data->>'hold_customer','') FROM "+schema+".documents WHERE id = 'S-SN-H1'").Scan(&st, &hc)
	if st != "Shipped" || hc != "" {
		t.Errorf("after the sale: %s hold=%q", st, hc)
	}

	// Expiry frees a piece and leaves the advance on the account.
	if _, err := PlacePieceHold(tenantID, "u1", PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H3", CustomerID: "C-H1", Until: tomorrow, AdvanceRupees: 100, Location: "WH1"}); err != nil {
		t.Fatal(err)
	}
	if n, err := ExpirePieceHolds(tenantID, "system"); err != nil || n != 0 {
		t.Fatalf("not yet due: %d %v", n, err)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET data = data || '{\"hold_until\":\"2020-01-01\"}' WHERE id = 'S-SN-H3'"); err != nil {
		t.Fatal(err)
	}
	// An expired hold no longer stops anyone, even before the job runs.
	tx, _ := db.DB.Begin()
	_, piece, _ := lockSerialTx(tx, schema, "RING-H", "SN-H3")
	_ = tx.Rollback()
	if err := pieceHoldRefusal(piece, "C-H2"); err != nil {
		t.Fatalf("expired hold must not block: %v", err)
	}
	if n, err := ExpirePieceHolds(tenantID, "system"); err != nil || n != 1 {
		t.Fatalf("expire: %d %v", n, err)
	}
	var left string
	_ = db.DB.QueryRow("SELECT COALESCE(data->>'hold_customer','') FROM " + schema + ".documents WHERE id = 'S-SN-H3'").Scan(&left)
	if left != "" {
		t.Errorf("expired hold stays: %q", left)
	}
	var bal int64
	_ = db.DB.QueryRow("SELECT COALESCE(SUM(balance_paise),0) FROM "+schema+".stored_value_instruments WHERE customer_id = 'C-H1' AND profile = $1", AdvanceProfileID).Scan(&bal)
	if bal != 60000 {
		t.Errorf("advances must stay on the account (500 + 100): %d", bal)
	}

	// Release by hand needs a held piece.
	if _, err := PlacePieceHold(tenantID, "u1", PieceHoldRequest{SKU: "RING-H", SerialNo: "SN-H3", CustomerID: "C-H2"}); err != nil {
		t.Fatal(err)
	}
	if rows, err := ListPieceHolds(tenantID); err != nil || len(rows) != 1 {
		t.Errorf("list: %v %v", rows, err)
	}
	if err := ReleasePieceHold(tenantID, "u1", "RING-H", "SN-H3", "changed mind"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := ReleasePieceHold(tenantID, "u1", "RING-H", "SN-H3", ""); holdCode(err) != "INVENT-1330" {
		t.Errorf("release twice: %v", err)
	}
	if rows, err := ListPieceHolds(tenantID); err != nil || len(rows) != 0 {
		t.Errorf("list after release: %v %v", rows, err)
	}
}
