package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 49.3.5 (rest): posted records stay posted. (a) every reversal / cancel
// path on a posted document refuses a reversal of a document that is not
// posted, and a second reversal; (b) an import update row cannot rewrite or
// un-post a Posted record.

func stage4935Seed(t *testing.T, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	if data == nil {
		data = map[string]interface{}{}
	}
	data["status"] = status
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')`, id, doctype, raw, status); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) })
}

func TestStage4935ReversalPathsRefuseDraftsAndSecondReversals(t *testing.T) {
	db.InitDB(testConnStr())
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())
	glRows := func(doc string) (n int) {
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.gl_postings WHERE document_id = $1`, doc).Scan(&n)
		return
	}
	cases := []struct {
		name     string
		doctype  string
		status   string
		data     map[string]interface{}
		reverse  func(id string) error
		wantNoop bool // an idempotent cancel of an already-cancelled document: no error, nothing posted again
	}{
		{"journal voucher: a draft", "JournalVoucher", "Draft", map[string]interface{}{"lines": []interface{}{}},
			func(id string) error { _, err := ReverseJournalVoucher("default", id, "t"); return err }, false},
		{"journal voucher: already reversed", "JournalVoucher", "Reversed", map[string]interface{}{"lines": []interface{}{}},
			func(id string) error { _, err := ReverseJournalVoucher("default", id, "t"); return err }, false},
		{"stock adjustment: a draft", InventoryAdjustmentDoctype, "Draft", nil,
			func(id string) error {
				_, _, err := ReverseInventoryAdjustment("default", id, "why", "t", RoleStoreManager)
				return err
			}, false},
		{"stock adjustment: reversed once already", InventoryAdjustmentDoctype, "Posted", map[string]interface{}{"reversed_by": "ADJ-X"},
			func(id string) error {
				_, _, err := ReverseInventoryAdjustment("default", id, "why", "t", RoleStoreManager)
				return err
			}, false},
		{"stock adjustment: a reversal itself", InventoryAdjustmentDoctype, "Posted", map[string]interface{}{"reversal_of": "ADJ-X"},
			func(id string) error {
				_, _, err := ReverseInventoryAdjustment("default", id, "why", "t", RoleStoreManager)
				return err
			}, false},
		{"sales invoice: a paid one", "SalesInvoice", "Paid", nil,
			func(id string) error { return CancelSalesInvoice("default", id, "t", "why") }, false},
		{"sales invoice: cancelled twice", "SalesInvoice", "Cancelled", nil,
			func(id string) error { return CancelSalesInvoice("default", id, "t", "why") }, true},
		{"delivery challan: cancelled twice", DeliveryChallanDoctype, "Cancelled", nil,
			func(id string) error { return CancelDeliveryChallan("default", id, "t", "why") }, false},
		{"delivery challan: goods out", DeliveryChallanDoctype, "Dispatched", nil,
			func(id string) error { return CancelDeliveryChallan("default", id, "t", "why") }, false},
		{"transfer order: cancelled twice", "TransferOrder", "Cancelled", nil,
			func(id string) error { return CancelTransferOrder("default", id, "t", "CAN-CUSTOMER", "") }, false},
		{"transfer order: dispatched", "TransferOrder", "In Transit", nil,
			func(id string) error { return CancelTransferOrder("default", id, "t", "CAN-CUSTOMER", "") }, false},
		{"shipping package: shipped", "ShippingPackage", "Shipped", nil,
			func(id string) error { return CancelShippingPackage("default", id, "CAN-CUSTOMER", "t") }, false},
		{"shipping package: cancelled twice", "ShippingPackage", "Cancelled", nil,
			func(id string) error { return CancelShippingPackage("default", id, "CAN-CUSTOMER", "t") }, true},
	}
	for i, c := range cases {
		id := fmt.Sprintf("S4935-%d-%s", i, sfx)
		data := map[string]interface{}{}
		for k, v := range c.data {
			data[k] = v
		}
		if c.doctype == "SalesInvoice" || c.doctype == "TransferOrder" {
			data["order_status"] = c.status
		}
		stage4935Seed(t, id, c.doctype, c.status, data)
		before := glRows(id)
		err := c.reverse(id)
		switch {
		case c.wantNoop && err != nil:
			t.Errorf("%s: a second cancel should be a no-op, got %v", c.name, err)
		case !c.wantNoop && err == nil:
			t.Errorf("%s: the reversal was accepted", c.name)
		}
		if after := glRows(id); after != before {
			t.Errorf("%s: %d ledger lines before, %d after - nothing may post", c.name, before, after)
		}
		var status string
		db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, id).Scan(&status)
		if status != c.status {
			t.Errorf("%s: the document moved from %s to %s", c.name, c.status, status)
		}
	}

	// An order that is already cancelled cannot be cancelled again.
	orderID := "S4935-ORD-" + sfx
	stage4935Seed(t, orderID, "SalesOrder", "Cancelled", map[string]interface{}{"order_status": "Cancelled"})
	if err := CancelOrder("default", orderID, "CAN-CUSTOMER"); err == nil || !strings.Contains(err.Error(), "cannot be cancelled") {
		t.Errorf("a cancelled order cancelled again: %v", err)
	}
}

// An import update row meets the same guards as a record-form save: it cannot
// change a posted document's money, un-post it, or make a move the doctype's
// rules refuse; a harmless edit of the same record still goes in.
func TestStage4935ImportCannotRewritePostedRecords(t *testing.T) {
	db.InitDB(testConnStr())
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())
	vendor := "S4935-V-" + sfx
	vraw, _ := json.Marshal(map[string]interface{}{"code": vendor, "name": "Posted import fixture"})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Vendor', $2, 'Active', 'system')`, vendor, vraw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, vendor) })
	mk := func(tag string) string {
		id := "S4935-DN-" + tag + "-" + sfx
		stage4935Seed(t, id, "DebitNote", "Posted", map[string]interface{}{"note_number": id, "vendor_id": vendor, "amount": 100, "reason": "damaged"})
		return id
	}
	money, unpost, note := mk("M"), mk("U"), mk("N")
	nl := "\n"
	csv := "id,note_number,vendor_id,amount,status,reason" + nl +
		money + "," + money + "," + vendor + ",999,," + nl + // changes the posted money
		unpost + "," + unpost + "," + vendor + ",,Draft," + nl + // un-posts
		note + "," + note + "," + vendor + ",,,edited note" + nl // harmless
	res, err := BulkImportCSV("default", "DebitNote", strings.NewReader(csv), "system", RoleSuperAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.SuccessRows != 1 || res.FailedRows != 2 {
		t.Fatalf("want only the note edit imported, got %d ok / %d failed: %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
	for _, e := range res.Errors {
		if !strings.Contains(fmt.Sprint(e), "posted to the accounts") {
			t.Errorf("a refused row should carry the posted-document message, got %v", e)
		}
	}
	read := func(id, field string) (v string) {
		db.DB.QueryRow(`SELECT COALESCE(data->>$2, '') FROM tenant_default.documents WHERE id = $1`, id, field).Scan(&v)
		return
	}
	if read(money, "amount") != "100" || read(unpost, "status") != "Posted" {
		t.Fatalf("a refused row was applied: amount %s, status %s", read(money, "amount"), read(unpost, "status"))
	}
	var col string
	db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, unpost).Scan(&col)
	if col != "Posted" {
		t.Fatalf("the status column moved to %s", col)
	}
	if read(note, "reason") != "edited note" {
		t.Fatalf("the harmless edit was not imported: %q", read(note, "reason"))
	}
}

// A strict doctype's import update meets its transition rules, as a save does.
func TestStage4935ImportUpdateMeetsTransitionRules(t *testing.T) {
	db.InitDB(testConnStr())
	var strict bool
	db.DB.QueryRow(`SELECT strict_status_transitions FROM tenant_default.doctype_meta WHERE name = 'Voucher'`).Scan(&strict)
	if !strict {
		t.Skip("Voucher is not strict here (Stage 49.5.3 migration)")
	}
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())
	id := "S4935-VCH-" + sfx
	stage4935Seed(t, id, "Voucher", "Exhausted", map[string]interface{}{"code": id})
	res, err := BulkImportCSV("default", "Voucher", strings.NewReader("id,code,discount_type,discount_value,status\n"+id+","+id+",Percentage,10,Active\n"), "system", RoleSuperAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.FailedRows != 1 || res.SuccessRows != 0 {
		t.Fatalf("a used-up voucher set back to Active by a file: %d ok / %d failed %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
	var s string
	db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, id).Scan(&s)
	if s != "Exhausted" {
		t.Fatalf("status is now %s", s)
	}
}
