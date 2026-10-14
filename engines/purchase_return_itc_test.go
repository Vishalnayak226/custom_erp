package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestPurchaseReturnReversesClaimedInputGST is the follow-up to 57.15 (model
// approved 2026-10-10): input GST (1500) already claimed on returned goods is
// reversed when the return posts, and goods returned before billing - never
// claimed - reverse nothing. Every case ends with the same credit for the 7
// units kept: 7 x 100 x 18% = 126.
func TestPurchaseReturnReversesClaimedInputGST(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	today := time.Now().Format("2006-01-02")

	// setup builds one PO (10 x 100 + 18% GST) and a GRN accepting all 10,
	// under its own ids, and returns helpers bound to them.
	type fixture struct {
		vendor, poID, grnID, ret string
		insert              func(id, doctype, status string, data map[string]interface{})
		returnThree         func() *PurchaseReturnPostResult
		invoice             func(id string, amount, gst float64) // insert + match
		pay                 func(id string)
		net                 func(account string, ids ...string) int64
	}
	setup := func(t *testing.T, prefix string) *fixture {
		f := &fixture{vendor: prefix + "-VEND", poID: prefix + "-PO", grnID: prefix + "-GRN", ret: prefix + "-RET"}
		sku, location := prefix+"-ITEM", prefix+"-LOC"
		var created []string
		f.insert = func(id, doctype, status string, data map[string]interface{}) {
			raw, _ := json.Marshal(data)
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
				t.Fatalf("insert %s: %v", id, err)
			}
			created = append(created, id)
		}
		cleanup := func() {
			for _, id := range append(created, sku, f.vendor, f.poID, f.grnID) {
				db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
				db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", id)
				db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
			}
			db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", sku)
			db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		}
		cleanup()
		t.Cleanup(cleanup)

		lines := func(rows ...map[string]interface{}) string { raw, _ := json.Marshal(rows); return string(raw) }
		f.insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "ITC Return Item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
		f.insert(f.vendor, "Vendor", "Active", map[string]interface{}{"code": f.vendor, "name": "ITC Return Vendor"})
		f.insert(f.poID, "PurchaseOrder", "Approved", map[string]interface{}{
			"code": f.poID, "vendor": f.vendor, "gst_mode": GSTModeExclusive, "total_amount": 1000, "grand_total": 1180,
			"items": lines(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0}),
		})
		f.insert(f.grnID, "GRN", "Active", map[string]interface{}{"code": f.grnID, "po_id": f.poID, "location": location,
			"received_items": lines(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10})})
		if _, err := PostGRNReceiptWithQC(tenantID, location, []interface{}{
			map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0},
		}, "system", f.grnID); err != nil {
			t.Fatalf("PostGRNReceiptWithQC: %v", err)
		}

		f.returnThree = func() *PurchaseReturnPostResult {
			retID := f.ret
			p := map[string]interface{}{"grn_id": f.grnID, "reason": "wrong colour",
				"return_items": lines(map[string]interface{}{"sku": sku, "stock_bucket": "Accepted", "qty": 3})}
			if err := validatePurchaseReturnRules(tenantID, "", "", nil, p); err != nil {
				t.Fatalf("return refused: %v", err)
			}
			f.insert(retID, "PurchaseReturn", "Draft", p)
			res, err := PostPurchaseReturn(tenantID, retID, "system", "HQ")
			if err != nil {
				t.Fatalf("PostPurchaseReturn: %v", err)
			}
			if res.DebitNoteID != "" {
				created = append(created, res.DebitNoteID)
			}
			return res
		}
		f.invoice = func(id string, amount, gst float64) {
			f.insert(id, "VendorInvoice", "Draft", map[string]interface{}{"invoice_number": id, "vendor_id": f.vendor,
				"po_id": f.poID, "grn_id": f.grnID, "invoice_amount": amount, "gst_amount": gst, "status": "Draft"})
			m, err := MatchVendorInvoice(tenantID, id, "", "", 2.0)
			if err != nil || !m.Matched {
				t.Fatalf("invoice %s of %v must match: err=%v reasons=%v", id, amount, err, m.Reasons)
			}
		}
		f.pay = func(id string) {
			if _, pending, err := PayVendorInvoice(tenantID, id, "system", "HR/Admin", ""); err != nil || pending {
				t.Fatalf("PayVendorInvoice %s: err=%v pending=%v", id, err, pending)
			}
		}
		f.net = func(account string, ids ...string) int64 {
			var n int64
			for _, id := range ids {
				var v int64
				if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit - credit), 0) FROM "+schema+".gl_postings WHERE account_code = $1 AND document_id = $2",
					account, id).Scan(&v); err != nil {
					t.Fatalf("sum %s: %v", account, err)
				}
				n += v
			}
			return n
		}
		return f
	}
	itcInSummary := func(t *testing.T) float64 {
		s, err := GetGSTReturnSummary(tenantID, today, today)
		if err != nil {
			t.Fatalf("GetGSTReturnSummary: %v", err)
		}
		return s.InputTaxCredit
	}

	t.Run("paid before return: the claimed GST on the returned goods is reversed", func(t *testing.T) {
		f := setup(t, "TESTITC-PAID")
		inv := "TESTITC-PAID-INV"
		f.invoice(inv, 1180, 180)
		f.pay(inv)
		if got := f.net("1500", inv); got != 18000 {
			t.Fatalf("payment should claim 180.00 input GST, got %d paise", got)
		}

		before := itcInSummary(t)
		res := f.returnThree()
		ret := f.ret
		// 3 returned x 100 x 1.18 = 354 tax-inclusive; its GST share at the
		// invoice's 180/1180 is 54.00.
		if res.GSTReversed != 54 {
			t.Errorf("GST reversed: want 54, got %v", res.GSTReversed)
		}
		if got := f.net("1500", ret); got != -5400 {
			t.Errorf("return must credit 1500 by 5400 paise, got %d", got)
		}
		if got := f.net("1500", inv, ret); got != 12600 {
			t.Errorf("1500 after the return: want 12600 (GST on the 7 kept), got %d", got)
		}
		// 2100: GRN Cr 1000, payment Dr 1000, debit note Dr 300, reversal Dr 54
		// -> Dr 354, exactly the tax-inclusive refund the vendor now owes.
		if got := f.net("2100", f.grnID, inv, ret, res.DebitNoteID); got != 35400 {
			t.Errorf("2100 should carry the vendor's 354.00 refund as a debit, got %d", got)
		}
		dn, _, err := fetchDocData(tenantID, "DebitNote", res.DebitNoteID)
		if err != nil {
			t.Fatalf("debit note: %v", err)
		}
		if numFromInterface(dn["amount"]) != 300 || numFromInterface(dn["gst_amount"]) != 54 || numFromInterface(dn["total_with_gst"]) != 354 {
			t.Errorf("debit note must show 300 + 54 GST = 354, got %+v", dn)
		}
		if reason, _ := dn["reason"].(string); !strings.HasSuffix(reason, "GST 54.00 reversed; total with GST 354.00") {
			t.Errorf("the notes list shows Reason, so it must state the GST refund, got %q", reason)
		}
		if drop := before - itcInSummary(t); drop < 53.99 || drop > 54.01 {
			t.Errorf("GST Return Summary input tax credit must fall by 54.00 in the return's period, fell by %.2f", drop)
		}
	})

	t.Run("returned before billing: nothing was claimed, nothing is reversed", func(t *testing.T) {
		f := setup(t, "TESTITC-EARLY")
		res := f.returnThree()
		ret := f.ret
		if res.GSTReversed != 0 || f.net("1500", ret) != 0 {
			t.Fatalf("a return before any invoice must not touch 1500, reversed %v / %d paise", res.GSTReversed, f.net("1500", ret))
		}
		// The vendor bills only the 7 kept: 7 x 118 = 826, GST 126.
		inv := "TESTITC-EARLY-INV"
		f.invoice(inv, 826, 126)
		f.pay(inv)
		if got := f.net("1500", inv, ret); got != 12600 {
			t.Errorf("1500 must end at 12600 (GST on the 7 kept), got %d", got)
		}
	})

	t.Run("billed but unpaid: reversed now, nets against the later claim", func(t *testing.T) {
		f := setup(t, "TESTITC-BILLED")
		inv := "TESTITC-BILLED-INV"
		f.invoice(inv, 1180, 180)
		res := f.returnThree()
		ret := f.ret
		if res.GSTReversed != 54 {
			t.Fatalf("GST reversed: want 54, got %v", res.GSTReversed)
		}
		if _, _, err := PayVendorInvoice(tenantID, inv, "system", "HR/Admin", "paying the matched bill; return settled by debit note"); err != nil {
			t.Fatalf("PayVendorInvoice: %v", err)
		}
		if got := f.net("1500", inv, ret); got != 12600 {
			t.Errorf("1500 must net to 12600 once the bill is paid, got %d", got)
		}
	})
}
