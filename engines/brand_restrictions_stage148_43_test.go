package engines

import (
	"strings"
	"testing"
	"time"
)

// Stage 148.43: brand-restricted dealers. Off, an order for any brand goes
// through and the customer's brand boxes are not offered. On, a dealer with a
// list and mode Block is refused a brand outside it, Warn lets it through
// with a note on the order, and dates switch the rule on and off.
func TestStage14843BrandRestrictedDealers(t *testing.T) {
	f := newCreditFixture(t)
	f.seed("BR-NIKE", "Brand", "Active", map[string]interface{}{"code": "BR-NIKE", "name": "Nike"})
	f.seed("ITEM-NK", "Item", "Active", map[string]interface{}{"code": "ITEM-NK", "name": "Shoe", "sale_price": 500.0, "hsn_code": "4819", "tax_treatment": "Exempt", "brand": "BR-NIKE"})
	f.seed("ITEM-PU", "Item", "Active", map[string]interface{}{"code": "ITEM-PU", "name": "Cap", "sale_price": 500.0, "hsn_code": "4819", "tax_treatment": "Exempt", "brand": "Puma"})
	for _, s := range []string{"ITEM-NK", "ITEM-PU"} {
		if _, err := PostInventoryLedger(f.tenantID, "HO", []interface{}{map[string]interface{}{"sku": s, "qty": 50.0}}, false); err != nil {
			t.Fatal(err)
		}
	}
	f.customer("CUST-DLR", "Dealer One", map[string]interface{}{"allowed_brands": "Nike", "brand_restriction": "Block"})
	place := func(name, sku string) (string, error) {
		return CreateSalesOrder(f.tenantID, SalesOrderInput{Channel: "Manual", CustomerName: name, ShippingAddress: "12 MG Road, Bengaluru, Karnataka 560001",
			PaymentStatus: "COD", Lines: []SalesOrderLineInput{{SKU: sku, Qty: 1, UnitPrice: 500}}})
	}

	// Off: nothing changes, and the boxes are not offered.
	if _, err := place("Dealer One", "ITEM-PU"); err != nil {
		t.Fatalf("setting off must leave orders alone: %v", err)
	}
	if meta, err := GetDocTypeMeta(f.tenantID, "Customer"); err != nil {
		t.Fatal(err)
	} else {
		for _, m := range meta {
			if m.Fieldname == "allowed_brands" || m.Fieldname == "brand_restriction" {
				t.Fatalf("with the setting off the customer form must not offer %s", m.Fieldname)
			}
		}
	}

	if err := SetSetting(f.tenantID, SettingBrandRestrictions, "true", "admin"); err != nil {
		t.Fatal(err)
	}
	meta, _ := GetDocTypeMeta(f.tenantID, "Customer")
	seen := map[string]bool{}
	for _, m := range meta {
		seen[m.Fieldname] = true
	}
	for _, k := range []string{"allowed_brands", "brand_restriction", "brand_restriction_from", "brand_restriction_to"} {
		if !seen[k] {
			t.Fatalf("with the setting on the customer form must offer %s", k)
		}
	}

	// Block: the allowed brand (spelt by master name or by code) passes, another is refused.
	if _, err := place("Dealer One", "ITEM-NK"); err != nil {
		t.Fatalf("allowed brand: %v", err)
	}
	_, err := place("Dealer One", "ITEM-PU")
	if codeOf(err) != "SALESR-1400" || !strings.Contains(err.Error(), "Puma") {
		t.Fatalf("a brand outside the list must be refused with SALESR-1400 naming it, got %v", err)
	}
	// A customer with no list, or none/blank mode, is unrestricted.
	f.customer("CUST-FREE", "Free Dealer", nil)
	f.customer("CUST-NONE", "None Dealer", map[string]interface{}{"allowed_brands": "Nike", "brand_restriction": "None"})
	for _, n := range []string{"Free Dealer", "None Dealer"} {
		if _, err := place(n, "ITEM-PU"); err != nil {
			t.Fatalf("%s is not restricted: %v", n, err)
		}
	}
	// Warn: placed, with the note on the order.
	f.customer("CUST-WARN", "Warn Dealer", map[string]interface{}{"allowed_brands": "BR-NIKE", "brand_restriction": "Warn"})
	id, err := place("Warn Dealer", "ITEM-PU")
	if err != nil {
		t.Fatalf("warn must place the order: %v", err)
	}
	if w := f.field("SalesOrder", id, "brand_warning"); !strings.Contains(w, "Puma") {
		t.Fatalf("warn leaves a note on the order, got %q", w)
	}
	// Dates: a rule that has not started, or has ended, does not apply.
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	f.customer("CUST-LATER", "Later Dealer", map[string]interface{}{"allowed_brands": "Nike", "brand_restriction": "Block", "brand_restriction_from": future})
	f.customer("CUST-ENDED", "Ended Dealer", map[string]interface{}{"allowed_brands": "Nike", "brand_restriction": "Block", "brand_restriction_to": past})
	for _, n := range []string{"Later Dealer", "Ended Dealer"} {
		if _, err := place(n, "ITEM-PU"); err != nil {
			t.Fatalf("%s is outside its dates: %v", n, err)
		}
	}
	// Turned off again: back to today.
	if err := SetSetting(f.tenantID, SettingBrandRestrictions, "false", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := place("Dealer One", "ITEM-PU"); err != nil {
		t.Fatalf("off again: %v", err)
	}
}
