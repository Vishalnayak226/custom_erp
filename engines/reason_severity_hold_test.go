package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func severityTestCode(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

// 74.1.1: a purchase return whose reason code is High severity goes to Pending
// Approval and posts nothing; approved it posts once (a second post or a
// concurrent one does not move stock again); rejected it never posts; a Low
// code posts at once as before.
func TestStage7411HighSeverityReturnNeedsApproval(t *testing.T) {
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
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'maker')", id, doctype, raw, status)
	}
	const sku, loc, vendor, po, grn = "SV-ITEM", "SV-LOC", "SV-VEND", "SV-PO", "SV-GRN"
	lines := func(rows ...map[string]interface{}) string { raw, _ := json.Marshal(rows); return string(raw) }
	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Severity item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "Severity vendor"})
	insert(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": vendor, "gst_mode": GSTModeExclusive, "total_amount": 1000, "grand_total": 1180,
		"items": lines(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0})})
	insert(grn, "GRN", "Active", map[string]interface{}{"code": grn, "po_id": po, "location": loc,
		"received_items": lines(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10})})
	if _, err := PostGRNReceiptWithQC(tenantID, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0}}, "system", grn); err != nil {
		t.Fatalf("PostGRNReceiptWithQC: %v", err)
	}
	for code, sev := range map[string]string{"RC-SV-HI": "High", "RC-SV-LO": "Low"} {
		insert(code, "ReasonCode", "Active", map[string]interface{}{"code": code, "description": code, "category": ReasonPurchaseReturn, "severity": sev})
	}
	onHand := func() int {
		var n int
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(on_hand), 0) FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ledger := func(id string) int {
		var n int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", id).Scan(&n)
		return n
	}
	statusOf := func(id string) string {
		var s string
		db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE doctype = 'PurchaseReturn' AND id = $1", id).Scan(&s)
		return s
	}
	draft := func(id, reasonCode string, qty int) {
		t.Helper()
		p := map[string]interface{}{"grn_id": grn, "reason": "bad", "reason_code": reasonCode,
			"return_items": lines(map[string]interface{}{"sku": sku, "stock_bucket": "Accepted", "qty": qty})}
		if err := validatePurchaseReturnRules(tenantID, "", "", nil, p); err != nil {
			t.Fatalf("return %s refused: %v", id, err)
		}
		insert(id, "PurchaseReturn", "Draft", p)
	}
	if onHand() != 10 {
		t.Fatalf("starting stock: %d", onHand())
	}

	// High severity: Pending Approval, nothing moves, a second post is refused.
	draft("SV-RET-A", "RC-SV-HI", 3)
	res, err := PostPurchaseReturn(tenantID, "SV-RET-A", "maker", "HQ")
	if err != nil || res == nil || !res.PendingApproval {
		t.Fatalf("high severity post: res=%+v err=%v", res, err)
	}
	if statusOf("SV-RET-A") != "Pending Approval" || onHand() != 10 || ledger("SV-RET-A") != 0 || res.DebitNoteID != "" {
		t.Fatalf("held return moved something: status=%s on_hand=%d", statusOf("SV-RET-A"), onHand())
	}
	if _, err := PostPurchaseReturn(tenantID, "SV-RET-A", "maker", "HQ"); severityTestCode(err) != "GLOBAL-0019" {
		t.Fatalf("post while pending: want GLOBAL-0019, got %v", err)
	}

	// Approved: posts exactly once, even when posts race.
	if err := DecideApproval(tenantID, "PurchaseReturn", "SV-RET-A", "checker", RoleAdmin, "", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := PostPurchaseReturn(tenantID, "SV-RET-A", "maker", "HQ")
			if err == nil && r.PendingApproval {
				err = fmt.Errorf("approved return came back pending")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	posted := 0
	for err := range results {
		if err == nil {
			posted++
		} else if c := severityTestCode(err); c != "PURCHA-0119" && c != "GLOBAL-0019" {
			t.Fatalf("racing post: %v", err)
		}
	}
	if posted != 1 || statusOf("SV-RET-A") != "Posted" || onHand() != 7 {
		t.Fatalf("approved return: posted=%d status=%s on_hand=%d (want 1, Posted, 7)", posted, statusOf("SV-RET-A"), onHand())
	}
	if _, err := PostPurchaseReturn(tenantID, "SV-RET-A", "maker", "HQ"); severityTestCode(err) != "PURCHA-0119" || onHand() != 7 {
		t.Fatalf("replay after posting: %v on_hand=%d", err, onHand())
	}

	// Rejected: nothing ever posts.
	draft("SV-RET-B", "RC-SV-HI", 3)
	if r, err := PostPurchaseReturn(tenantID, "SV-RET-B", "maker", "HQ"); err != nil || !r.PendingApproval {
		t.Fatalf("second high return: %+v %v", r, err)
	}
	if err := DecideApproval(tenantID, "PurchaseReturn", "SV-RET-B", "checker", RoleAdmin, "", "Rejected", "no"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := PostPurchaseReturn(tenantID, "SV-RET-B", "maker", "HQ"); err == nil || statusOf("SV-RET-B") != "Rejected" || onHand() != 7 || ledger("SV-RET-B") != 0 {
		t.Fatalf("rejected return posted: err=%v status=%s on_hand=%d", err, statusOf("SV-RET-B"), onHand())
	}

	// Low severity posts at once, as before.
	draft("SV-RET-C", "RC-SV-LO", 2)
	r, err := PostPurchaseReturn(tenantID, "SV-RET-C", "maker", "HQ")
	if err != nil || r.PendingApproval || statusOf("SV-RET-C") != "Posted" || onHand() != 5 {
		t.Fatalf("low severity: res=%+v err=%v status=%s on_hand=%d", r, err, statusOf("SV-RET-C"), onHand())
	}
}

// 74.1.1: a transfer receipt that writes units off under a High-severity code
// is held; nothing moves; approval posts it exactly once; rejection posts
// nothing; a Low code posts at once.
func TestStage7411HighSeverityTransferLossNeedsApproval(t *testing.T) {
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
	const from, to, sku = "LOC-SV-A", "LOC-SV-B", "SV-TSKU"
	for _, l := range []string{from, to} {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')",
			l, fmt.Sprintf(`{"code":%q,"name":%q}`, l, l))
	}
	for c, sev := range map[string]string{"TD-SV-HI": "High", "TD-SV-LO": "Low"} {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')",
			c, fmt.Sprintf(`{"code":%q,"description":%q,"category":%q,"severity":%q}`, c, c, TransferDiscrepancyReasonCategory, sev))
	}
	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": sku, "tracking_mode": TrackingNone, "standard_cost": 10})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ITEM-SV-TSKU', 'Item', $1, 'Active', 'system')", item)
	exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, from)
	order := func(id string) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 5}})
		data, _ := json.Marshal(map[string]interface{}{"transfer_number": id, "from_warehouse": from, "to_warehouse": to, "status": "Approved", "items": string(items)})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, 'Approved', 'maker')", id, data)
		if err := DispatchTransferOrder(tenantID, id, "system"); err != nil {
			t.Fatalf("dispatch %s: %v", id, err)
		}
	}
	destOnHand := func() int {
		var n int
		db.DB.QueryRow("SELECT COALESCE(SUM(on_hand), 0) FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, to).Scan(&n)
		return n
	}
	statusOf := func(doctype, id string) string {
		var s string
		db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE doctype = $1 AND id = $2", doctype, id).Scan(&s)
		return s
	}
	events := func(id string) int {
		d, _, _ := fetchDocData(tenantID, "TransferOrder", id)
		return len(loadTransferEvents(d))
	}
	holds := func() int {
		var n int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = $1", TransferLossHoldDoctype).Scan(&n)
		return n
	}
	recv := func(id, reasonCode, key string) (*TransferMovementResult, error) {
		return ReceiveTransferOrderLines(tenantID, id, "maker", TransferReceiptRequest{ReceiptKey: key,
			Lines: []TransferReceiptLine{{Sku: sku, Qty: 3, ReasonCode: reasonCode}}}) // 2 of 5 closed as lost
	}
	for _, id := range []string{"TO-SV-1", "TO-SV-2", "TO-SV-3"} {
		order(id)
	}

	// High: held, nothing moved, a retry with the same key finds the same hold.
	res, err := recv("TO-SV-1", "TD-SV-HI", "k1")
	if err != nil || res == nil || !res.PendingApproval || res.ApprovalID == "" {
		t.Fatalf("high severity receipt: res=%+v err=%v", res, err)
	}
	if again, err := recv("TO-SV-1", "TD-SV-HI", "k1"); err != nil || again.ApprovalID != res.ApprovalID || holds() != 1 {
		t.Fatalf("retry made a second hold: %+v %v holds=%d", again, err, holds())
	}
	if destOnHand() != 0 || events("TO-SV-1") != 0 || statusOf("TransferOrder", "TO-SV-1") != "Dispatched" || statusOf(TransferLossHoldDoctype, res.ApprovalID) != "Pending Approval" {
		t.Fatalf("held receipt moved something: dest=%d events=%d status=%s", destOnHand(), events("TO-SV-1"), statusOf("TransferOrder", "TO-SV-1"))
	}

	// Approved: posts exactly once.
	if err := DecideApproval(tenantID, TransferLossHoldDoctype, res.ApprovalID, "checker", RoleAdmin, "", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if destOnHand() != 3 || events("TO-SV-1") != 1 || statusOf("TransferOrder", "TO-SV-1") != "Received" || statusOf(TransferLossHoldDoctype, res.ApprovalID) != "Posted" {
		t.Fatalf("approved receipt: dest=%d events=%d status=%s hold=%s", destOnHand(), events("TO-SV-1"), statusOf("TransferOrder", "TO-SV-1"), statusOf(TransferLossHoldDoctype, res.ApprovalID))
	}
	if err := DecideApproval(tenantID, TransferLossHoldDoctype, res.ApprovalID, "checker2", RoleAdmin, "", "Approved", "again"); err == nil {
		t.Fatal("a decided hold was approved a second time")
	}
	executeTransferLossHold(tenantID, res.ApprovalID, "checker") // a replayed hook
	if destOnHand() != 3 || events("TO-SV-1") != 1 {
		t.Fatalf("replayed approval posted again: dest=%d events=%d", destOnHand(), events("TO-SV-1"))
	}

	// Rejected: nothing posts, the transfer stays open for a corrected receipt.
	held, err := recv("TO-SV-2", "TD-SV-HI", "k2")
	if err != nil || !held.PendingApproval {
		t.Fatalf("second high receipt: %+v %v", held, err)
	}
	if err := DecideApproval(tenantID, TransferLossHoldDoctype, held.ApprovalID, "checker", RoleAdmin, "", "Rejected", "recount"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if destOnHand() != 3 || events("TO-SV-2") != 0 || statusOf("TransferOrder", "TO-SV-2") != "Dispatched" || statusOf(TransferLossHoldDoctype, held.ApprovalID) != "Rejected" {
		t.Fatalf("rejected receipt moved something: dest=%d events=%d", destOnHand(), events("TO-SV-2"))
	}

	// An explicit lost quantity under a High code is held too.
	lost, err := ReceiveTransferOrderLines(tenantID, "TO-SV-2", "maker", TransferReceiptRequest{KeepOpen: true,
		Lines: []TransferReceiptLine{{Sku: sku, Qty: 3, LostQty: 1, ReasonCode: "TD-SV-HI"}}})
	if err != nil || !lost.PendingApproval {
		t.Fatalf("lost qty under a High code: %+v %v", lost, err)
	}

	// Low: posts at once, as before.
	if r, err := recv("TO-SV-3", "TD-SV-LO", "k3"); err != nil || r.PendingApproval || events("TO-SV-3") != 1 || destOnHand() != 6 {
		t.Fatalf("low severity receipt: %+v %v events=%d dest=%d", r, err, events("TO-SV-3"), destOnHand())
	}
}
