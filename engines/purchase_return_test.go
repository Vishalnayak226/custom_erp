package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// TestPurchaseReturn is Stage 57.15: a return against a GRN takes accepted
// and QC-rejected stock back out, raises and posts a debit note for the
// accepted value, and the GRN's Dr 1200 / Cr 2100 is reversed exactly.
func TestPurchaseReturn(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		sku      = "TEST5715-ITEM"
		vendor   = "TEST5715-VEND"
		poID     = "TEST5715-PO"
		grnID    = "TEST5715-GRN"
		cxlGRN   = "TEST5715-GRN-CXL"
		ret1     = "TEST5715-RET1"
		ret2     = "TEST5715-RET2"
		location = "TEST5715-LOC"
	)
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(rows ...map[string]interface{}) string {
		raw, _ := json.Marshal(rows)
		return string(raw)
	}
	var debitNotes []string
	cleanup := func() {
		for _, id := range append([]string{sku, vendor, poID, grnID, cxlGRN, ret1, ret2}, debitNotes...) {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	}
	cleanup()
	defer cleanup()

	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Return Test Item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "Return Test Vendor"})
	insert(poID, "PurchaseOrder", "Approved", map[string]interface{}{
		"code": poID, "vendor": vendor, "gst_mode": GSTModeExclusive, "total_amount": 1000, "grand_total": 1180,
		"items": lines(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0}),
	})
	received := []map[string]interface{}{{"sku": sku, "qty": 10, "accepted_qty": 8, "rejected_qty": 2, "rejection_reason": "scratched"}}
	insert(grnID, "GRN", "Active", map[string]interface{}{"code": grnID, "po_id": poID, "location": location, "received_items": lines(received...)})
	insert(cxlGRN, "GRN", "Cancelled", map[string]interface{}{"code": cxlGRN, "po_id": poID, "location": location, "received_items": lines(received...)})
	if _, err := PostGRNReceiptWithQC(tenantID, location, []interface{}{
		map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 8.0, "rejected_qty": 2.0, "rejection_reason": "scratched"},
	}, "system", grnID); err != nil {
		t.Fatalf("PostGRNReceiptWithQC: %v", err)
	}

	code := func(err error) string {
		var verr *ValidationError
		if errors.As(err, &verr) {
			return verr.Code
		}
		return ""
	}
	payload := func(grn, reason string, rows ...map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"grn_id": grn, "reason": reason, "return_items": lines(rows...)}
	}
	accepted3 := map[string]interface{}{"sku": sku, "stock_bucket": "Accepted", "qty": 3}
	rejected2 := map[string]interface{}{"sku": sku, "stock_bucket": "Rejected", "qty": "2"}

	t.Run("the reserved codes guard a save", func(t *testing.T) {
		cases := []struct {
			name string
			p    map[string]interface{}
			want string
		}{
			{"no reason", payload(grnID, " ", accepted3), "PURCHA-0116"},
			{"more than accepted", payload(grnID, "wrong size", map[string]interface{}{"sku": sku, "qty": 9}), "PURCHA-0117"},
			{"a bucket the GRN never received", payload(grnID, "wrong size", map[string]interface{}{"sku": sku, "stock_bucket": "Damaged", "qty": 1}), "PURCHA-0117"},
			{"no GRN", payload("", "wrong size", accepted3), "PURCHA-0118"},
			{"a GRN whose stock never posted", payload(cxlGRN, "wrong size", accepted3), "PURCHA-0118"},
		}
		for _, tc := range cases {
			if err := validatePurchaseReturnRules(tenantID, "", "", nil, tc.p); code(err) != tc.want {
				t.Errorf("%s: want %s, got %v", tc.name, tc.want, err)
			}
		}
		claimPosted := payload(grnID, "wrong size", accepted3)
		claimPosted["status"] = "Posted"
		if err := validatePurchaseReturnRules(tenantID, "", "", nil, claimPosted); code(err) != "GLOBAL-0019" {
			t.Errorf("claiming Posted through the doc API: want GLOBAL-0019, got %v", err)
		}
	})

	p := payload(grnID, "wrong size", accepted3, rejected2)
	if err := validatePurchaseReturnRules(tenantID, "", "", nil, p); err != nil {
		t.Fatalf("valid return refused: %v", err)
	}
	if p["status"] != "Draft" || p["vendor_id"] != vendor || p["location"] != location || p["total_amount"] != 300.0 {
		t.Fatalf("derived fields wrong: status=%v vendor=%v location=%v total=%v", p["status"], p["vendor_id"], p["location"], p["total_amount"])
	}
	insert(ret1, "PurchaseReturn", "Draft", p)

	// While ret1 is open, a second return cannot claim the same rejected qty.
	if err := validatePurchaseReturnRules(tenantID, "", "", nil, payload(grnID, "again", map[string]interface{}{"sku": sku, "stock_bucket": "Rejected", "qty": 1})); code(err) != "PURCHA-0119" {
		t.Errorf("rejected qty already on an open return: want PURCHA-0119, got %v", err)
	}

	res, err := PostPurchaseReturn(tenantID, ret1, "system", "HQ")
	if err != nil {
		t.Fatalf("PostPurchaseReturn: %v", err)
	}
	if res.DebitNoteID != "" {
		debitNotes = append(debitNotes, res.DebitNoteID)
	}
	if res.ReturnValue != 300 || !res.DebitNotePosted || res.DebitNoteID == "" {
		t.Fatalf("unexpected result %+v", res)
	}

	var available, qcHold, onHand int
	if err := db.DB.QueryRow("SELECT available, qc_hold, on_hand FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&available, &qcHold, &onHand); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if available != 5 || qcHold != 0 || onHand != 5 {
		t.Errorf("stock after return: want available 5, qc_hold 0, on_hand 5; got %d, %d, %d", available, qcHold, onHand)
	}

	dn, dnStatus, err := fetchDocData(tenantID, "DebitNote", res.DebitNoteID)
	if err != nil {
		t.Fatalf("debit note %s not found: %v", res.DebitNoteID, err)
	}
	if dnStatus != "Posted" || dn["vendor_id"] != vendor || numFromInterface(dn["amount"]) != 300 || dn["source_doc_id"] != ret1 {
		t.Errorf("debit note wrong: status=%s %+v", dnStatus, dn)
	}

	// The GRN costed its 8 accepted units: Cr 2100 / Dr 1200 800. The return
	// and its note take 300
	// back off each (the 2 rejected were never costed), and 5150 nets to zero.
	net := func(account string) int64 {
		var n int64
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit - credit), 0) FROM "+schema+".gl_postings WHERE account_code = $1 AND document_id IN ($2, $3, $4)",
			account, grnID, ret1, res.DebitNoteID).Scan(&n); err != nil {
			t.Fatalf("sum %s: %v", account, err)
		}
		return n
	}
	if got := net("2100"); got != -50000 {
		t.Errorf("2100 owed to the vendor: want Cr 50000 paise, got %d", got)
	}
	if got := net("1200"); got != 50000 {
		t.Errorf("1200 inventory: want Dr 50000 paise, got %d", got)
	}
	if got := net("5150"); got != 0 {
		t.Errorf("5150 should net to zero, got %d", got)
	}

	if _, err := PostPurchaseReturn(tenantID, ret1, "system", "HQ"); code(err) != "PURCHA-0119" {
		t.Errorf("posting twice: want PURCHA-0119, got %v", err)
	}
	if err := validatePurchaseReturnRules(tenantID, ret1, "Posted", map[string]interface{}{}, payload(grnID, "edit", accepted3)); code(err) != "GLOBAL-0019" {
		t.Errorf("editing a posted return: want GLOBAL-0019, got %v", err)
	}
	// 8 accepted, 3 returned: 5 left, so 6 is refused and 5 is fine.
	if err := validatePurchaseReturnRules(tenantID, "", "", nil, payload(grnID, "rest", map[string]interface{}{"sku": sku, "qty": 6})); code(err) != "PURCHA-0117" {
		t.Errorf("returning more than is left: want PURCHA-0117, got %v", err)
	}
	if err := validatePurchaseReturnRules(tenantID, ret2, "", nil, payload(grnID, "rest", map[string]interface{}{"sku": sku, "qty": 5})); err != nil {
		t.Errorf("returning exactly what is left was refused: %v", err)
	}

	// AP three-way match nets the returned units out: the vendor's bill for
	// the 5 kept (5 x 100 x 1.18 = 590) matches; one still billing all 8
	// accepted (944) holds.
	for _, tc := range []struct {
		id      string
		amount  float64
		matched bool
	}{{"TEST5715-INV-NET", 590, true}, {"TEST5715-INV-GROSS", 944, false}} {
		debitNotes = append(debitNotes, tc.id)
		insert(tc.id, "VendorInvoice", "Draft", map[string]interface{}{
			"invoice_number": tc.id, "vendor_id": vendor, "po_id": poID, "grn_id": grnID, "invoice_amount": tc.amount, "status": "Draft",
		})
		m, err := MatchVendorInvoice(tenantID, tc.id, "", "", 2.0)
		if err != nil {
			t.Fatalf("MatchVendorInvoice %s: %v", tc.id, err)
		}
		if m.Matched != tc.matched {
			t.Errorf("invoice of %v after the return: matched=%v, want %v (%v)", tc.amount, m.Matched, tc.matched, m.Reasons)
		}
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", tc.id)
	}
}
