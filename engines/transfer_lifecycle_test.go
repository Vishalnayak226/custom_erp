package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// Stage 73.1.7 on a provisioned throwaway client: FEFO lots and serial units
// travel with the transfer, receipts split over deliveries, discrepancies
// need reason codes, a retried receipt posts once, cancel and return to
// source, and D04's direct approval below the client's limit.
func TestTransferLifecycleStage7317(t *testing.T) {
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
	const from, to = "LOC-TF-A", "LOC-TF-B"
	for _, loc := range []string{from, to} {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')",
			loc, fmt.Sprintf(`{"code":%q,"name":%q}`, loc, loc))
	}
	reason := func(code, category string) {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')",
			code, fmt.Sprintf(`{"code":%q,"description":%q,"category":%q}`, code, code, category))
	}
	reason("RC-TF-DMG", TransferDiscrepancyReasonCategory)
	reason("RC-TF-CXL", "Cancellation")
	item := func(sku, mode string, cost float64) {
		data, _ := json.Marshal(map[string]interface{}{"code": sku, "name": sku, "tracking_mode": mode, "standard_cost": cost})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, data)
	}
	stock := func(sku, loc string, qty int) {
		exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", sku, loc, qty)
	}
	binLot := func(bin, sku, batch string, qty int) {
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system') ON CONFLICT (id) DO NOTHING",
			"BIN-"+bin, fmt.Sprintf(`{"bin_code":%q,"location":%q,"status":"Active"}`, bin, from))
		exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $3, $4, 'Good') ON CONFLICT (bin_code, sku, condition) DO UPDATE SET qty = "+schema+".bin_stock.qty + EXCLUDED.qty",
			bin, sku, from, qty)
		exec("INSERT INTO "+schema+".bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty) VALUES ($1, $2, $3, 'Good', $4, $5)", bin, sku, batch, from, qty)
	}
	order := func(id string, status string, lines []map[string]interface{}) {
		items, _ := json.Marshal(lines)
		data, _ := json.Marshal(map[string]interface{}{"transfer_number": id, "from_warehouse": from, "to_warehouse": to, "status": status, "items": string(items)})
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, $3, 'maker')", id, data, status)
	}
	avail := func(sku, loc string) (onHand, available, inTransit, damaged int) {
		t.Helper()
		db.DB.QueryRow("SELECT on_hand, available, in_transit, damaged FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).
			Scan(&onHand, &available, &inTransit, &damaged)
		return
	}
	doc := func(id string) (map[string]interface{}, string) {
		t.Helper()
		data, status, err := fetchDocData(tenantID, "TransferOrder", id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return data, status
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}

	t.Run("batch lots leave FEFO, arrive over two deliveries, discrepancies coded, retry posts once", func(t *testing.T) {
		const sku = "TF-LOT"
		item(sku, TrackingBatch, 10)
		for _, b := range []struct{ no, expiry string }{{"L-LATE", isoDaysFromNow(200)}, {"L-SOON", isoDaysFromNow(40)}} {
			if _, err := EnsureBatch(tenantID, BatchInfo{BatchNo: b.no, Item: sku, ExpiryDate: b.expiry}, 0, "system"); err != nil {
				t.Fatalf("EnsureBatch %s: %v", b.no, err)
			}
		}
		stock(sku, from, 20)
		binLot("TF-BIN-1", sku, "L-LATE", 10)
		binLot("TF-BIN-2", sku, "L-SOON", 6)
		order("TO-TF-LOT", "Approved", []map[string]interface{}{{"sku": sku, "qty": 8}})

		if err := DispatchTransferOrder(tenantID, "TO-TF-LOT", "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		data, _ := doc("TO-TF-LOT")
		var sent []transferLine
		json.Unmarshal([]byte(data["dispatched_items"].(string)), &sent)
		if len(sent) != 1 || len(sent[0].Lots) != 2 || sent[0].Lots[0].BatchNo != "L-SOON" || sent[0].Lots[0].Qty != 6 || sent[0].Lots[1].BatchNo != "L-LATE" || sent[0].Lots[1].Qty != 2 {
			t.Fatalf("want FEFO L-SOON x6 then L-LATE x2, got %+v", sent)
		}
		var soonLeft, lateLeft, bin1 int
		db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock_batch WHERE sku = $1 AND batch_no = 'L-SOON'", sku).Scan(&soonLeft)
		db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock_batch WHERE sku = $1 AND batch_no = 'L-LATE'", sku).Scan(&lateLeft)
		db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE sku = $1 AND bin_code = 'TF-BIN-1'", sku).Scan(&bin1)
		if soonLeft != 0 || lateLeft != 8 || bin1 != 8 {
			t.Fatalf("bins after dispatch: L-SOON %d (want 0), L-LATE %d (want 8), bin1 %d (want 8)", soonLeft, lateLeft, bin1)
		}

		// Delivery 1: 4 good of the soon lot, the rest stays in transit.
		res, err := ReceiveTransferOrderLines(tenantID, "TO-TF-LOT", "system", TransferReceiptRequest{
			KeepOpen: true, ReceiptKey: "rk-1",
			Lines: []TransferReceiptLine{{Sku: sku, BatchNo: "L-SOON", Qty: 4}},
		})
		if err != nil || res.Status != "Partially Received" {
			t.Fatalf("receipt 1: %v %+v", err, res)
		}
		// The same receipt again (a retry) changes nothing.
		again, err := ReceiveTransferOrderLines(tenantID, "TO-TF-LOT", "system", TransferReceiptRequest{
			KeepOpen: true, ReceiptKey: "rk-1",
			Lines: []TransferReceiptLine{{Sku: sku, BatchNo: "L-SOON", Qty: 4}},
		})
		if err != nil || !again.Replayed {
			t.Fatalf("retried receipt should replay: %v %+v", err, again)
		}
		if oh, av, _, _ := avail(sku, to); oh != 4 || av != 4 {
			t.Fatalf("destination after receipt 1 (+retry): on_hand %d available %d, want 4/4", oh, av)
		}
		// A discrepancy without a reason code is refused.
		_, err = ReceiveTransferOrderLines(tenantID, "TO-TF-LOT", "system", TransferReceiptRequest{
			Lines: []TransferReceiptLine{{Sku: sku, Qty: 2, DamagedQty: 1, LostQty: 1}},
		})
		if code(err) != "STOCKT-0484" {
			t.Fatalf("discrepancy without reason: want STOCKT-0484, got %v", err)
		}
		// More than is in transit is refused.
		_, err = ReceiveTransferOrderLines(tenantID, "TO-TF-LOT", "system", TransferReceiptRequest{
			KeepOpen: true, Lines: []TransferReceiptLine{{Sku: sku, Qty: 5}},
		})
		if code(err) != "STOCKT-0485" {
			t.Fatalf("over-receipt: want STOCKT-0485, got %v", err)
		}
		// Delivery 2: 2 good, 1 damaged, 1 lost, and 1 extra of L-LATE - closes it.
		res, err = ReceiveTransferOrderLines(tenantID, "TO-TF-LOT", "system", TransferReceiptRequest{
			ReceiptKey: "rk-2",
			Lines:      []TransferReceiptLine{{Sku: sku, Qty: 2, DamagedQty: 1, LostQty: 1, ExcessQty: 1, BatchNo: "", ReasonCode: "RC-TF-DMG"}},
		})
		if code(err) != "STOCKT-0482" {
			t.Fatalf("excess of a batch item without a lot: want STOCKT-0482, got %v", err)
		}
		res, err = ReceiveTransferOrderLines(tenantID, "TO-TF-LOT", "system", TransferReceiptRequest{
			ReceiptKey: "rk-2",
			Lines: []TransferReceiptLine{
				{Sku: sku, Qty: 2, DamagedQty: 1, LostQty: 1, ReasonCode: "RC-TF-DMG"},
				{Sku: sku, BatchNo: "L-LATE", ExcessQty: 1, ReasonCode: "RC-TF-DMG"},
			},
		})
		if err != nil || res.Status != "Received" {
			t.Fatalf("receipt 2: %v %+v", err, res)
		}
		b := res.Balance[0]
		if b.Dispatched != 8 || b.Received != 6 || b.Damaged != 1 || b.Lost != 1 || b.Excess != 1 || b.Open() != 0 {
			t.Fatalf("balance %+v", b)
		}
		srcOH, srcAv, srcTransit, _ := avail(sku, from)
		dstOH, dstAv, _, dstDmg := avail(sku, to)
		if srcOH != 12 || srcAv != 12 || srcTransit != 0 {
			t.Fatalf("source on_hand/available/in_transit %d/%d/%d, want 12/12/0", srcOH, srcAv, srcTransit)
		}
		// 6 good + 1 damaged + 1 excess on hand; damaged kept out of available.
		if dstOH != 8 || dstAv != 7 || dstDmg != 1 {
			t.Fatalf("destination on_hand/available/damaged %d/%d/%d, want 8/7/1", dstOH, dstAv, dstDmg)
		}
		var ledgerLots int
		db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'TO-TF-LOT' AND COALESCE(data->>'batch_no','') <> ''").Scan(&ledgerLots)
		if ledgerLots < 4 {
			t.Fatalf("expected lot-stamped ledger lines for the transfer, got %d", ledgerLots)
		}
	})

	t.Run("serial units: dispatch allocates, one received, one returned to source", func(t *testing.T) {
		const sku = "TF-SER"
		item(sku, TrackingSerial, 100)
		stock(sku, from, 3)
		for _, sn := range []string{"SN-1", "SN-2", "SN-3"} {
			if _, err := RegisterSerial(tenantID, SerialInfo{SerialNo: sn, Item: sku}, from, "GRN", "GRN-TF", "system"); err != nil {
				t.Fatalf("RegisterSerial %s: %v", sn, err)
			}
		}
		order("TO-TF-SER-BAD", "Approved", []map[string]interface{}{{"sku": sku, "qty": 2, "serials": []string{"SN-1"}}})
		if err := DispatchTransferOrder(tenantID, "TO-TF-SER-BAD", "system"); code(err) != "STOCKT-0483" {
			t.Fatalf("serial count mismatch: want STOCKT-0483, got %v", err)
		}
		order("TO-TF-SER", "Approved", []map[string]interface{}{{"sku": sku, "qty": 2, "serials": []string{"SN-1", "SN-2"}}})
		if err := DispatchTransferOrder(tenantID, "TO-TF-SER", "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if u, _ := GetSerial(tenantID, sku, "SN-1"); u == nil || u.Status != SerialAllocated || u.ReservedFor != "TO-TF-SER" {
			t.Fatalf("SN-1 after dispatch: %+v", u)
		}
		if _, err := ReceiveTransferOrderLines(tenantID, "TO-TF-SER", "system", TransferReceiptRequest{
			KeepOpen: true, Lines: []TransferReceiptLine{{Sku: sku, Serials: []string{"SN-3"}}},
		}); code(err) != "STOCKT-0483" {
			t.Fatalf("receiving a unit not on the transfer: want STOCKT-0483, got %v", err)
		}
		res, err := ReceiveTransferOrderLines(tenantID, "TO-TF-SER", "system", TransferReceiptRequest{
			KeepOpen: true, Lines: []TransferReceiptLine{{Sku: sku, Serials: []string{"SN-1"}}},
		})
		if err != nil || res.Status != "Partially Received" {
			t.Fatalf("receive SN-1: %v %+v", err, res)
		}
		if _, err := ReturnTransferToSource(tenantID, "TO-TF-SER", "system", "", "", "", []TransferReturnLine{{Sku: sku, Serials: []string{"SN-2"}}}); code(err) != "STOCKT-0484" {
			t.Fatalf("return without reason: want STOCKT-0484, got %v", err)
		}
		res, err = ReturnTransferToSource(tenantID, "TO-TF-SER", "system", "RC-TF-DMG", "refused at the door", "ret-1", []TransferReturnLine{{Sku: sku, Serials: []string{"SN-2"}}})
		if err != nil || res.Status != "Received" {
			t.Fatalf("return SN-2: %v %+v", err, res)
		}
		u1, _ := GetSerial(tenantID, sku, "SN-1")
		u2, _ := GetSerial(tenantID, sku, "SN-2")
		if u1.Status != SerialInStock || u1.LocationCode != to || u2.Status != SerialInStock || u2.LocationCode != from {
			t.Fatalf("SN-1 %+v (want InStock at %s), SN-2 %+v (want InStock at %s)", u1, to, u2, from)
		}
		if oh, av, it, _ := avail(sku, from); oh != 2 || av != 2 || it != 0 {
			t.Fatalf("source after return: %d/%d/%d, want 2/2/0", oh, av, it)
		}
	})

	t.Run("cancel only before dispatch, with a cancellation reason", func(t *testing.T) {
		const sku = "TF-CXL"
		item(sku, "", 5)
		stock(sku, from, 10)
		order("TO-TF-CXL", "Approved", []map[string]interface{}{{"sku": sku, "qty": 1}})
		if err := CancelTransferOrder(tenantID, "TO-TF-CXL", "system", "RC-TF-DMG", ""); code(err) != "STOCKT-0484" {
			t.Fatalf("wrong reason category: want STOCKT-0484, got %v", err)
		}
		if err := CancelTransferOrder(tenantID, "TO-TF-CXL", "system", "RC-TF-CXL", "not needed"); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if _, st := doc("TO-TF-CXL"); st != "Cancelled" {
			t.Fatalf("status %s, want Cancelled", st)
		}
		if err := DispatchTransferOrder(tenantID, "TO-TF-CXL", "system"); err == nil {
			t.Fatalf("a cancelled transfer must not dispatch")
		}
		order("TO-TF-CXL2", "Approved", []map[string]interface{}{{"sku": sku, "qty": 1}})
		if err := DispatchTransferOrder(tenantID, "TO-TF-CXL2", "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if err := CancelTransferOrder(tenantID, "TO-TF-CXL2", "system", "RC-TF-CXL", ""); code(err) != "STOCKT-0486" {
			t.Fatalf("cancel after dispatch: want STOCKT-0486, got %v", err)
		}
		res, err := ReturnTransferToSource(tenantID, "TO-TF-CXL2", "system", "RC-TF-DMG", "", "", []TransferReturnLine{{Sku: sku, Qty: 1}})
		if err != nil || res.Status != "Returned" {
			t.Fatalf("return everything: %v %+v", err, res)
		}
		if oh, av, it, _ := avail(sku, from); oh != 10 || av != 10 || it != 0 {
			t.Fatalf("source after full return: %d/%d/%d, want 10/10/0", oh, av, it)
		}
	})

	t.Run("D04: maker-checker by default, direct approval only below the limit", func(t *testing.T) {
		const sku = "TF-APR"
		item(sku, "", 10) // ₹10 standard cost
		exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('TransferOrder', 0, NULL, 'Store Manager') ON CONFLICT (doctype, min_amount) DO NOTHING")
		order("TO-TF-APR", "Draft", []map[string]interface{}{{"sku": sku, "qty": 5}}) // ₹50

		if _, err := ApproveTransferOrderDirect(tenantID, "TO-TF-APR", "maker", "Store Manager", from); code(err) != "STOCKT-0481" {
			t.Fatalf("limit 0 must refuse direct approval, got %v", err)
		}
		if err := SetSetting(tenantID, TransferDirectApprovalLimitSetting, "50", "system"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if _, err := ApproveTransferOrderDirect(tenantID, "TO-TF-APR", "maker", "Store Manager", from); code(err) != "STOCKT-0481" {
			t.Fatalf("value equal to the limit must refuse, got %v", err)
		}
		if err := SetSetting(tenantID, TransferDirectApprovalLimitSetting, "1000", "system"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if _, err := ApproveTransferOrderDirect(tenantID, "TO-TF-APR", "maker", "Cashier", from); code(err) != "STOCKT-0481" {
			t.Fatalf("a cashier must not approve directly, got %v", err)
		}
		if _, err := ApproveTransferOrderDirect(tenantID, "TO-TF-APR", "maker", "Store Manager", "LOC-ELSEWHERE"); code(err) != "STOCKT-0481" {
			t.Fatalf("a manager of another store must not approve directly, got %v", err)
		}
		v, err := ApproveTransferOrderDirect(tenantID, "TO-TF-APR", "maker", "Store Manager", from)
		if err != nil || v.Value != 50 {
			t.Fatalf("direct approval below the limit: %v (value %v)", err, v.Value)
		}
		if _, st := doc("TO-TF-APR"); st != "Approved" {
			t.Fatalf("status %s, want Approved", st)
		}

		// Above the limit: submit, and the creator cannot approve it.
		order("TO-TF-APR2", "Draft", []map[string]interface{}{{"sku": sku, "qty": 500}}) // ₹5000
		if _, err := ApproveTransferOrderDirect(tenantID, "TO-TF-APR2", "maker", "Store Manager", from); code(err) != "STOCKT-0481" {
			t.Fatalf("above the limit must refuse direct approval, got %v", err)
		}
		if err := SubmitTransferOrderForApproval(tenantID, "TO-TF-APR2", "maker", "Store Manager"); err != nil {
			t.Fatalf("submit: %v", err)
		}
		data, st := doc("TO-TF-APR2")
		if st != "Pending Approval" || numFromInterface(data["total_amount"]) != 5000 {
			t.Fatalf("after submit: %s, total_amount %v", st, data["total_amount"])
		}
		withSelfApproval(t, tenantID, SelfApprovalNobody) // Stage 92: the submitter may not decide
		if err := DecideApproval(tenantID, "TransferOrder", "TO-TF-APR2", "maker", "Store Manager", from, "Approved", ""); err == nil {
			t.Fatalf("the maker must not approve their own transfer")
		}
		if err := DecideApproval(tenantID, "TransferOrder", "TO-TF-APR2", "checker", "Store Manager", from, "Approved", ""); err != nil {
			t.Fatalf("checker approval: %v", err)
		}
		if _, st := doc("TO-TF-APR2"); st != "Approved" {
			t.Fatalf("status %s, want Approved", st)
		}
	})
}
