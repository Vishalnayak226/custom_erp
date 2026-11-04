package engines

import (
	"math"
	"testing"

	"custom_erp/db"
)

// Stage 148.2 (left): an order's shipping charge goes on the first package's
// invoice only, once; the packages' invoices add up to the order's charge.
func TestStage1482PackageInvoicesBillShippingOnce(t *testing.T) {
	schema := spFixture(t)
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = data || '{"shipping_charge":118}'::jsonb WHERE id = $1`, spOrder); err != nil {
		t.Fatal(err)
	}
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatal(err)
	}
	pkg2, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{{SKU: spSKU, Qty: 3}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	inv1, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inv2, err := GenerateInvoiceForPackage("default", pkg2, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{}); again != inv1 {
		t.Fatalf("retry returned %s, want %s", again, inv1)
	}
	val := func(id, key string) float64 {
		var v float64
		_ = db.DB.QueryRow(`SELECT COALESCE((data->>'`+key+`')::numeric, 0) FROM `+schema+`.documents WHERE id = $1`, id).Scan(&v)
		return v
	}
	if val(inv1, "shipping_charge") != 118 || val(inv2, "shipping_charge") != 0 {
		t.Fatalf("shipping on first package only: %v and %v", val(inv1, "shipping_charge"), val(inv2, "shipping_charge"))
	}
	// Items: 8 x 100 + 2 x 50 split across the two; the invoices carry the 118 on top.
	if got := val(inv1, "total_amount") + val(inv2, "total_amount"); math.Abs(got-(900+118)) > 0.01 {
		t.Errorf("invoices total %v, want 1018", got)
	}
	// The shipping's GST (18% inside 118 = 18.00) is in the first invoice's tax,
	// and every invoice still adds up.
	for _, id := range []string{inv1, inv2} {
		sum := val(id, "taxable_amount") + val(id, "non_taxable") + val(id, "total_tax")
		if math.Abs(sum-val(id, "total_amount")) > 0.011 {
			t.Errorf("%s: taxable+non-taxable+tax %v != total %v", id, sum, val(id, "total_amount"))
		}
	}
	if got := val(inv1, "total_tax") + val(inv2, "total_tax"); math.Abs(got-(900-900/1.18+18)) > 0.02 {
		t.Errorf("tax across invoices %v, want about %v", got, 900-900/1.18+18)
	}
	// A cancelled first invoice frees the charge; no order charge changes nothing (below).
}

func TestStage1482PackageInvoiceWithoutShippingUnchanged(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatal(err)
	}
	inv, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	var has bool
	_ = db.DB.QueryRow(`SELECT (data->>'total_amount')::numeric, data ? 'shipping_charge' FROM `+schema+`.documents WHERE id = $1`, inv).Scan(&total, &has)
	if has || math.Abs(total-900) > 0.001 {
		t.Errorf("no shipping: total %v, has charge key %v; want 900 and none", total, has)
	}
}
