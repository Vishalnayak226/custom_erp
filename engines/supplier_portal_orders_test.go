package engines

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"custom_erp/db"
)

func portalPO(f *creditFixture, id, vendor, status string, qty float64) {
	f.t.Helper()
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "ITEM-CC", "qty": qty, "rate": 77.5, "net_value": qty * 77.5, "mrp": 120}})
	f.seed(id, "PurchaseOrder", status, map[string]interface{}{
		"po_number": id, "vendor": vendor, "vendor_id": vendor, "status": status, "location": "HO",
		"items": string(items), "expected_delivery_date": "2026-12-01", "grand_total": qty * 77.5, "internal_note": "squeeze price",
	})
}

func portalLogin(f *creditFixture, userID, vendor string) *SupplierVendor {
	f.t.Helper()
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".users (id, username, password_hash, email, role, status, supplier_code) VALUES ($1,$1,'x',$2,'Supplier','Active',$3)",
		userID, userID+"@s.test", vendor); err != nil {
		f.t.Fatal(err)
	}
	v, err := ResolveSupplierVendor(f.tenantID, userID)
	if err != nil {
		f.t.Fatalf("resolve: %v", err)
	}
	return v
}

func TestSupplierPortalScopesEveryRecordType(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("SUP-A", nil)
	f.vendor("SUP-B", nil)
	portalPO(f, "PO-A1", "SUP-A", "Approved", 10)
	portalPO(f, "PO-A2", "SUP-A", "Draft", 5)
	portalPO(f, "PO-B1", "SUP-B", "Approved", 7)
	f.seed("ASN-B1", "ASN", "Expected", map[string]interface{}{"asn_number": "ASN-B1", "po_id": "PO-B1", "po_number": "PO-B1", "vendor": "SUP-B", "expected_items": `[{"sku":"ITEM-CC","qty":2}]`})
	f.seed("BILL-A", "VendorInvoice", "Matched", map[string]interface{}{"invoice_number": "INV-A", "vendor_id": "SUP-A", "po_id": "PO-A1", "invoice_amount": 1000.0, "paid_amount": 400.0,
		"match_details": "secret", "payments": []map[string]interface{}{{"reference": "UTR1", "kind": "Payment", "amount": 400.0, "date": "2026-11-02", "by": "acct1", "posting_key": "k1"}}})
	f.seed("BILL-B", "VendorInvoice", "Matched", map[string]interface{}{"invoice_number": "INV-B", "vendor_id": "SUP-B", "invoice_amount": 50.0})
	a := portalLogin(f, "supa", "SUP-A")

	orders, err := SupplierOrders(f.tenantID, a)
	if err != nil || len(orders) != 1 || orders[0]["id"] != "PO-A1" {
		t.Fatalf("orders: %v %v (draft and other vendor must be hidden)", orders, err)
	}
	if _, err := SupplierOrder(f.tenantID, a, "PO-B1"); !errors.Is(err, ErrSupplierNotFound) {
		t.Fatalf("other vendor's order must be not-found, got %v", err)
	}
	if _, err := SupplierOrder(f.tenantID, a, "PO-A2"); !errors.Is(err, ErrSupplierNotFound) {
		t.Fatalf("a draft order must be not-found, got %v", err)
	}
	if _, err := SupplierASN(f.tenantID, a, "ASN-B1"); !errors.Is(err, ErrSupplierNotFound) {
		t.Fatalf("other vendor's ASN must be not-found, got %v", err)
	}
	if asns, _ := SupplierASNs(f.tenantID, a); len(asns) != 0 {
		t.Fatalf("asns leaked: %v", asns)
	}
	if _, err := SupplierBill(f.tenantID, a, "BILL-B"); !errors.Is(err, ErrSupplierNotFound) {
		t.Fatalf("other vendor's bill must be not-found, got %v", err)
	}
	bills, _ := SupplierBills(f.tenantID, a)
	if len(bills) != 1 || bills[0]["payment_status"] != "Part paid" || bills[0]["match_status"] != "Matched" || bills[0]["outstanding_amount"] != 600.0 {
		t.Fatalf("bills: %v", bills)
	}
	pays, _ := SupplierPayments(f.tenantID, a)
	if len(pays) != 1 || pays[0]["reference"] != "UTR1" || pays[0]["amount"] != 400.0 {
		t.Fatalf("payments: %v", pays)
	}

	// Internal fields never reach a supplier.
	raw, _ := json.Marshal([]interface{}{orders, bills, pays})
	for _, banned := range []string{"77.5", "rate", "mrp", "net_value", "grand_total", "internal_note", "squeeze", "match_details", "secret", "posting_key", "acct1"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("internal field %q leaked: %s", banned, raw)
		}
	}

	// Another client sees nothing of this vendor, and a login linked to a missing vendor is refused.
	g := newCreditFixture(t)
	if _, err := db.DB.Exec("INSERT INTO " + g.schema + ".users (id, username, password_hash, email, role, status, supplier_code) VALUES ('supx','supx','x','x@s.test','Supplier','Active','SUP-A')"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveSupplierVendor(g.tenantID, "supx"); !errors.Is(err, ErrSupplierNotLinked) {
		t.Fatalf("a vendor of another client must not resolve, got %v", err)
	}
	g.vendor("SUP-A", nil)
	other, _ := ResolveSupplierVendor(g.tenantID, "supx")
	if _, err := SupplierOrder(g.tenantID, other, "PO-A1"); !errors.Is(err, ErrSupplierNotFound) {
		t.Fatalf("another client's order must be not-found, got %v", err)
	}
}

func TestSupplierASNOnOwnOpenOrder(t *testing.T) {
	f := newCreditFixture(t)
	f.vendor("SUP-A", nil)
	f.vendor("SUP-B", nil)
	portalPO(f, "PO-A1", "SUP-A", "Approved", 10)
	portalPO(f, "PO-B1", "SUP-B", "Approved", 10)
	portalPO(f, "PO-A3", "SUP-A", "Closed", 10)
	a := portalLogin(f, "supa", "SUP-A")
	send := func(po string, qty float64) (map[string]interface{}, error) {
		return SendSupplierASN(f.tenantID, "supa", a, SupplierASNInput{POID: po, ExpectedDate: "2026-12-05", Carrier: "VRL", Lines: []ASNQL{{Sku: "ITEM-CC", Qty: qty}}})
	}
	if _, err := send("PO-B1", 1); !errors.Is(err, ErrSupplierNotFound) {
		t.Fatalf("a notice on another vendor's order must be not-found, got %v", err)
	}
	if _, err := send("PO-A3", 1); codeOf(err) != SupplierPOClosedCode {
		t.Fatalf("a closed order must be refused with %s, got %v", SupplierPOClosedCode, err)
	}
	if _, err := send("PO-A1", 11); codeOf(err) != SupplierASNQtyCode {
		t.Fatalf("over the open quantity must be refused with %s, got %v", SupplierASNQtyCode, err)
	}
	if _, err := SendSupplierASN(f.tenantID, "supa", a, SupplierASNInput{POID: "PO-A1", Lines: []ASNQL{{Sku: "NOPE", Qty: 1}}}); codeOf(err) != "ASN-0271" {
		t.Fatalf("an item not on the order must be refused, got %v", err)
	}
	if _, err := SendSupplierASN(f.tenantID, "supa", a, SupplierASNInput{POID: "PO-A1", Lines: []ASNQL{{Sku: "ITEM-CC", Qty: 0}}}); codeOf(err) != SupplierASNLinesCode {
		t.Fatalf("a zero quantity must be refused, got %v", err)
	}
	asn, err := send("PO-A1", 6)
	if err != nil {
		t.Fatalf("a valid notice: %v", err)
	}
	// 6 of 10 are now on a notice, so another 6 is over what is open, and 4 is fine.
	if _, err := send("PO-A1", 6); codeOf(err) != SupplierASNQtyCode {
		t.Fatalf("the second notice must see the first one's quantity, got %v", err)
	}
	if _, err := send("PO-A1", 4); err != nil {
		t.Fatalf("the remaining 4: %v", err)
	}
	if list, _ := SupplierASNs(f.tenantID, a); len(list) != 2 {
		t.Fatalf("both notices must be listed, got %v", list)
	}
	one, err := SupplierASN(f.tenantID, a, strField(asn, "id"))
	if err != nil || one["status"] != "Expected" {
		t.Fatalf("own notice: %v %v", one, err)
	}
}
