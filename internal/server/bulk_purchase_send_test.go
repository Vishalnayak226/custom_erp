package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// 320.1: with Send to, one upload makes the PO, receives the bill and takes
// its stock on to the store that sells it - through the business's approval
// rule - and the same file again finishes a transfer that waited and never
// makes a second one.
func TestBulkPurchaseSendsToStore(t *testing.T) {
	c := swTestCaller(t)
	probe, err := newSWRun(c, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := probe.resolveLink("Location", "HO"); err != nil || id == "" {
		t.Skip("the test database has no store HO")
	}
	storeHasState(t, "HO")
	sfx := fmt.Sprint(time.Now().UnixNano() % 1e9)
	vendor, category, dest := "BPS Vendor "+sfx, "BPSCat"+sfx, "BPS-"+sfx
	schema, _ := db.GetTenantSchema("default")
	exec := func(q string, args ...interface{}) {
		if _, err := db.DB.Exec(fmt.Sprintf(q, schema), args...); err != nil {
			t.Logf("cleanup: %v", err)
		}
	}

	// The store the stock goes on to: same state and legal entity as HO.
	loc, _ := json.Marshal(map[string]interface{}{"id": dest, "code": dest, "name": "BPS Store " + sfx, "type": "Store",
		"state": "Maharashtra", "state_code": "MH", "status": "Active", "sellable": "Yes", "legal_entity": "LE-TEST-MH"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')`, schema), dest, loc); err != nil {
		t.Fatal(err)
	}
	limit := engines.GetSettingString("default", engines.TransferDirectApprovalLimitSetting)
	if err := engines.SetSetting("default", engines.TransferDirectApprovalLimitSetting, "0", "system"); err != nil {
		t.Fatal(err)
	}

	var res *bpResult
	var grns []string
	t.Cleanup(func() {
		for _, g := range grns {
			exec(`DELETE FROM %[1]s.gl_postings WHERE document_id IN (SELECT id FROM %[1]s.documents WHERE doctype = 'TransferOrder' AND data->>'source_grn' = $1)`, g)
			exec(`DELETE FROM %[1]s.approval_log WHERE document_id IN (SELECT id FROM %[1]s.documents WHERE doctype = 'TransferOrder' AND data->>'source_grn' = $1)`, g)
			exec(`DELETE FROM %[1]s.documents d USING %[1]s.documents t WHERE t.doctype = 'TransferOrder' AND t.data->>'source_grn' = $1
				AND d.id <> t.id AND (d.data->>'transfer_order_id' = t.id OR d.data->>'voucher_id' LIKE t.id || '%%')`, g)
			exec(`DELETE FROM %[1]s.documents WHERE doctype = 'TransferOrder' AND data->>'source_grn' = $1`, g)
		}
		_ = engines.SetSetting("default", engines.TransferDirectApprovalLimitSetting, limit, "system")
		bpTestCleanup(schema, category, []string{vendor}, res)
		exec(`DELETE FROM %[1]s.documents WHERE doctype = 'Location' AND id = $1`, dest)
	})

	file := swBook(t, "", map[string][][]string{
		"Vendors": {{"Check", "Vendor Name *", "State *", "City", "GSTIN", "Phone", "Email"},
			{"", vendor, "Maharashtra", "Mumbai", "", "", ""}},
		"Purchase": {bpTestPurchaseHead,
			{"", "BPS-" + sfx, vendor, "HO", "", "Earring", category, "7117", "3", "Red", "", "2", "90", "349", "", "s-1", "", "", ""},
			{"", "BPS-" + sfx, vendor, "HO", "", "Earring", category, "7117", "3", "Blue", "", "3", "90", "349", "", "s-1", "", "", ""},
		},
		"Bills": {{"Check", "Vendor Invoice No *", "Vendor *", "Invoice Date *", "Invoice Total (₹) *"},
			{"", "s-1", vendor, "46345", "450"}},
	}, "Vendors", "Purchase", "Bills")
	upload := func() *bpResult {
		t.Helper()
		run, err := newBPRun(c, true)
		if err != nil {
			t.Fatal(err)
		}
		run.sendTo, run.res.SendTo = dest, dest
		vendors, lines, bills, attrs, err := bpRead(file, engines.ClientDateOrder(c.tenantID))
		if err != nil {
			t.Fatal(err)
		}
		out := run.process(vendors, lines, bills, attrs)
		if len(out.Bills) != 1 || out.Bills[0].Result != "done" || out.Bills[0].GRNID == "" {
			for _, b := range out.Bills {
				t.Logf("bill %s: %s %s %s", b.Invoice, b.Action, b.Result, b.Message)
			}
			t.Fatalf("the bill was not received: totals %+v", out.Totals)
		}
		return out
	}

	res = upload()
	bill := res.Bills[0]
	grns = append(grns, bill.GRNID)
	if bill.TOID == "" || !bill.PostedNow {
		t.Fatalf("no transfer was made, or the bill is not marked posted now (320.2): %+v", bill)
	}
	switch bill.TOResult {
	case "waiting":
		// Direct approval is off, so the transfer waits for a person; the
		// screen says so and counts it as waiting.
		if bill.TOStatus != "Pending Approval" || res.Totals.Waiting != 1 {
			t.Fatalf("the waiting transfer: status %q, totals %+v - %s", bill.TOStatus, res.Totals, bill.Message)
		}
		if err := engines.SetSetting("default", engines.TransferDirectApprovalLimitSetting, "100000000", "system"); err != nil {
			t.Fatal(err)
		}
		if _, err := engines.ApproveTransferOrderDirect("default", bill.TOID, "admin", "Super Admin", ""); err != nil {
			t.Fatalf("approve the transfer: %v", err)
		}
	case "done":
		// No approval rule for transfers here: submitting approved it.
	default:
		t.Fatalf("the transfer: result %q, status %q - %s", bill.TOResult, bill.TOStatus, bill.Message)
	}

	again := upload()
	b2 := again.Bills[0]
	if b2.PostedNow {
		t.Errorf("a bill posted by an earlier upload offers its stickers again")
	}
	if b2.TOID != bill.TOID || b2.TOResult != "done" || b2.TOStatus != "Received" || again.Totals.Sent != 1 {
		t.Fatalf("the same file again: transfer %s (first %s), %s %s, totals %+v - %s", b2.TOID, bill.TOID, b2.TOResult, b2.TOStatus, again.Totals, b2.Message)
	}
	var atDest, atHO int
	for _, po := range res.POs {
		for _, l := range po.Lines {
			var n, h int
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(available), 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), l.SKU, dest).Scan(&n)
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(available), 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = 'HO'`, schema), l.SKU).Scan(&h)
			atDest += n
			atHO += h
		}
	}
	if atDest != 5 || atHO != 0 {
		t.Errorf("stock after the transfer: %d at the store (want 5), %d left at HO (want 0)", atDest, atHO)
	}

	third := upload()
	var count int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND data->>'source_grn' = $1`, schema), bill.GRNID).Scan(&count)
	if third.Bills[0].TOID != bill.TOID || third.Bills[0].TOResult != "done" || count != 1 {
		t.Errorf("a third upload: transfer %s %s, %d transfers for the receipt (want the same one, done, 1)", third.Bills[0].TOID, third.Bills[0].TOResult, count)
	}
}
