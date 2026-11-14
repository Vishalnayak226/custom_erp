package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
)

func serviceFix(t *testing.T) *wsFix {
	t.Helper()
	f := newWSFix(t, false)
	f.vendor("SV-V")
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "WS-A", "qty": 10, "rate": 100}})
	f.insert("PO-S1", "PurchaseOrder", "Approved", "system", map[string]interface{}{"code": "PO-S1", "vendor_id": "SV-V", "location": "WS-HQ",
		"order_date": BusinessToday(), "items": string(items), "total_amount": 1000.0, "grand_total": 1000.0, "po_kind": "Service", "status": "Approved"})
	f.insert("PO-G1", "PurchaseOrder", "Approved", "system", map[string]interface{}{"code": "PO-G1", "vendor_id": "SV-V", "location": "WS-HQ",
		"order_date": BusinessToday(), "items": string(items), "total_amount": 1000.0, "grand_total": 1000.0, "status": "Approved"})
	return f
}

func (f *wsFix) sheet(id string, qty float64) map[string]interface{} {
	f.t.Helper()
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": "WS-A", "qty": qty}})
	data := map[string]interface{}{"code": id, "po_id": "PO-S1", "entry_date": BusinessToday(), "lines": string(lines), "status": "Draft"}
	f.insert(id, "ServiceEntrySheet", "Draft", "system", data)
	return data
}

func stockEntries(f *wsFix) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry'`, f.schema)).Scan(&n)
	return n
}

func docStatus(t *testing.T, f *wsFix, doctype, id string) string {
	t.Helper()
	var s string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype=$1 AND id=$2`, f.schema), doctype, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServicePOIsNeverReceivedOnAGoodsReceipt(t *testing.T) {
	f := serviceFix(t)
	wantCode(t, refuseServiceGRN(f.tenantID, "PO-S1"), codeServicePO)
	if err := refuseServiceGRN(f.tenantID, "PO-G1"); err != nil {
		t.Fatalf("goods PO refused: %v", err)
	}
	wantCode(t, validatePOKind("Approved", map[string]interface{}{"po_kind": "Service"}, map[string]interface{}{}), codeServicePO)
}

func TestServiceEntryOverAcceptanceRefusedAndAcceptPostsNoStock(t *testing.T) {
	f := serviceFix(t)
	before := stockEntries(f)
	over := f.sheet("SES-1", 11)
	wantCode(t, validateServiceEntrySheetSave(f.tenantID, "SES-1", "Draft", nil, over), codeSheetSave)
	f.sheet("SES-2", 6)
	if err := AcceptServiceEntrySheet(f.tenantID, "SES-2", "mgr", "Store Manager"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if d, c := f.gl("ServiceEntrySheet", "SES-2", "2100"); d != 0 || c != 60000 {
		t.Fatalf("accrual 2100 debit %d credit %d, want 0 / 60000", d, c)
	}
	// replay: a second accept changes nothing
	wantCode(t, AcceptServiceEntrySheet(f.tenantID, "SES-2", "mgr", "Store Manager"), codeSheetAction)
	if _, c := f.gl("ServiceEntrySheet", "SES-2", "2100"); c != 60000 {
		t.Fatalf("replay changed the ledger: %d", c)
	}
	// 6 accepted of 10: 5 more is over what is left
	wantCode(t, validateServiceEntrySheetSave(f.tenantID, "SES-3", "Draft", nil, f.sheet("SES-3", 5)), codeSheetSave)
	if after := stockEntries(f); after != before {
		t.Fatalf("stock moved: %d rows became %d", before, after)
	}
}

func TestServiceEntryAcceptNeedsTheApproverRole(t *testing.T) {
	f := serviceFix(t)
	f.sheet("SES-R", 2)
	wantCode(t, AcceptServiceEntrySheet(f.tenantID, "SES-R", "cashier1", "Cashier"), codeSheetAction)
	if st := docStatus(t, f, "ServiceEntrySheet", "SES-R"); st != "Draft" {
		t.Fatalf("status %s", st)
	}
}

func TestServiceBillMatchesTheAcceptedSheet(t *testing.T) {
	f := serviceFix(t)
	f.sheet("SES-B", 4)
	if err := AcceptServiceEntrySheet(f.tenantID, "SES-B", "mgr", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	bill := func(id string, amount float64, sheet string) {
		f.insert(id, "VendorInvoice", "Draft", "system", map[string]interface{}{"code": id, "vendor_id": "SV-V", "po_id": "PO-S1",
			"service_entry_id": sheet, "invoice_amount": amount, "status": "Draft"})
	}
	bill("VI-OK", 400, "SES-B")
	m, err := MatchVendorInvoiceAs(f.tenantID, "mgr", "VI-OK", "", "", 0)
	if err != nil || !m.Matched {
		t.Fatalf("a bill for the accepted work did not match: %+v %v", m, err)
	}
	// billing 1000 for 400 of work is held
	bill("VI-BIG", 1000, "SES-B")
	if m, err = MatchVendorInvoiceAs(f.tenantID, "mgr", "VI-BIG", "", "", 0); err != nil || m.Matched || m.Status != "MismatchHold" {
		t.Fatalf("an over-bill matched: %+v %v", m, err)
	}
	// a draft sheet is not work done
	f.sheet("SES-D", 1)
	bill("VI-DRAFT", 100, "SES-D")
	if m, err = MatchVendorInvoiceAs(f.tenantID, "mgr", "VI-DRAFT", "", "", 0); err != nil || m.Matched {
		t.Fatalf("a bill on an unaccepted sheet matched: %+v %v", m, err)
	}
	// no sheet named at all
	bill("VI-NONE", 100, "")
	if _, err = MatchVendorInvoiceAs(f.tenantID, "mgr", "VI-NONE", "", "", 0); err == nil {
		t.Fatal("a service bill with no sheet was matched")
	}
}
