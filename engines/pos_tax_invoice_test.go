package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 85.D2: a till sale to a GSTIN buyer prints as a tax invoice - the
// server bill number, the buyer's GSTIN and place of supply, the GST-inclusive
// price split back into taxable value and tax, and an HSN-wise summary.
func TestPOSTaxInvoicePrint(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"S85E-STORE", "S85E-CUST", "S85E-ITEM-A", "S85E-ITEM-B", "S85E-CART", "S85E-CART-OPEN"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	put := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	put("S85E-STORE", "Location", "Active", map[string]interface{}{"code": "S85E-STORE", "name": "S85E Store", "type": "Store"})
	put("S85E-CUST", "Customer", "Active", map[string]interface{}{"name": "Acme Traders", "phone": "8000000086", "gstin": "29AABCU9603R1ZJ",
		"address": "1 MG Road", "city": "Bengaluru", "state": "Karnataka", "state_code": "29"})
	put("S85E-ITEM-A", "Item", "Active", map[string]interface{}{"code": "S85E-ITEM-A", "name": "Shirt", "hsn_code": "6205", "gst_rate": 12})
	put("S85E-ITEM-B", "Item", "Active", map[string]interface{}{"code": "S85E-ITEM-B", "name": "Trouser", "hsn_code": "6203", "gst_rate": 12})
	cart := map[string]interface{}{"location": "S85E-STORE", "customer_id": "S85E-CUST", "payment_mode": "Card", "bill_number": "BS85E2627000001",
		"gst_breakdown": map[string]interface{}{"interstate": false},
		"items": []map[string]interface{}{
			{"sku": "S85E-ITEM-A", "qty": 2, "sale_price": 1120, "net_total": 2240},
			{"sku": "S85E-ITEM-B", "qty": 1, "sale_price": 2240, "net_total": 2240},
		}}
	put("S85E-CART", "POSCart", "Paid", cart)
	put("S85E-CART-OPEN", "POSCart", "Draft", cart)

	d, err := BuildPrintDocument(tenantID, PrintTxnPOSTaxInvoice, "S85E-CART", "", false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if d.Number != "BS85E2627000001" || d.Title != "Tax Invoice" || d.PartyTitle != "Buyer" {
		t.Errorf("header: number %q title %q party %q", d.Number, d.Title, d.PartyTitle)
	}
	// Over the counter the place of supply is the store's state, not the buyer's.
	if d.Party.GSTIN != "29AABCU9603R1ZJ" || d.Fields["supply_type"] != "B2B" || d.Fields["place_of_supply"] == "Karnataka (29)" {
		t.Errorf("buyer: gstin %q supply %v place %v", d.Party.GSTIN, d.Fields["supply_type"], d.Fields["place_of_supply"])
	}
	if len(d.TaxSummary) != 2 || d.TaxSummary[0].HSN != "6203" || d.TaxSummary[1].HSN != "6205" {
		t.Fatalf("HSN-wise summary: %+v", d.TaxSummary)
	}
	// 2240 incl. 12% = 2000 taxable + 240 tax, CGST 120 + SGST 120 per HSN.
	for _, r := range d.TaxSummary {
		if r.Taxable != 2000 || r.CGST != 120 || r.SGST != 120 || r.IGST != 0 {
			t.Errorf("HSN %s: %+v", r.HSN, r)
		}
	}
	if d.Totals["grand_total"] != 4480 {
		t.Errorf("total %v, want 4480", d.Totals["grand_total"])
	}
	if _, err := BuildPrintDocument(tenantID, PrintTxnPOSTaxInvoice, "S85E-CART-OPEN", "", false); err == nil {
		t.Error("an unpaid sale has no tax invoice")
	}
}
