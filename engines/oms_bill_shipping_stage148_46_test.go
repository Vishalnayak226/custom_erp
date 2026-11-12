package engines

import (
	"encoding/json"
	"math"
	"testing"

	"custom_erp/db"
)

// Stage 148.46: the OMS bill carries the order's shipping once, on the first
// package; the ERP bill raised from it matches it and posts it.
func TestStage14846OMSBillCarriesShippingAndTheERPBillMatches(t *testing.T) {
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
	bill1, _, err := GenerateOMSBillForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bill2, _, err := GenerateOMSBillForPackage("default", pkg2, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	val := func(id, key string) float64 {
		var v float64
		_ = db.DB.QueryRow(`SELECT COALESCE((data->>'`+key+`')::numeric, 0) FROM `+schema+`.documents WHERE id = $1`, id).Scan(&v)
		return v
	}
	if val(bill1, "shipping_charge") != 118 || val(bill2, "shipping_charge") != 0 {
		t.Fatalf("shipping on the first OMS bill only: %v and %v", val(bill1, "shipping_charge"), val(bill2, "shipping_charge"))
	}
	if got := val(bill1, "total_amount") + val(bill2, "total_amount"); math.Abs(got-1018) > 0.01 {
		t.Fatalf("OMS bills total %v, want 1018 (900 goods + 118 shipping)", got)
	}
	sum := val(bill1, "taxable_amount") + val(bill1, "non_taxable") + val(bill1, "total_tax")
	if math.Abs(sum-val(bill1, "total_amount")) > 0.011 {
		t.Errorf("first OMS bill: taxable+non-taxable+tax %v != total %v", sum, val(bill1, "total_amount"))
	}
	// Printed total: the lines the print is built from add up to the bill total, shipping included.
	var raw string
	_ = db.DB.QueryRow(`SELECT data::text FROM `+schema+`.documents WHERE id = $1`, bill1).Scan(&raw)
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &data)
	var printed int64
	hasShipLine := false
	for _, l := range salesInvoiceLines("default", schema, data) {
		printed += l.Gross
		hasShipLine = hasShipLine || l.Charge != ""
	}
	if !hasShipLine || math.Abs(PaiseToRupees(printed)-val(bill1, "total_amount")) > 0.011 {
		t.Errorf("printed lines %v (shipping line %v), bill total %v", PaiseToRupees(printed), hasShipLine, val(bill1, "total_amount"))
	}

	inv1, err := RaiseERPBillForPackage("default", pkgID, "tester")
	if err != nil {
		t.Fatalf("raise ERP bill: %v", err)
	}
	inv2, err := RaiseERPBillForPackage("default", pkg2, "tester")
	if err != nil {
		t.Fatalf("raise ERP bill 2: %v", err)
	}
	for _, c := range [][2]string{{bill1, inv1}, {bill2, inv2}} {
		if math.Abs(val(c[0], "total_amount")-val(c[1], "total_amount")) > 0.001 || val(c[0], "shipping_charge") != val(c[1], "shipping_charge") {
			t.Errorf("ERP bill %s (total %v, shipping %v) differs from OMS bill %s (total %v, shipping %v)",
				c[1], val(c[1], "total_amount"), val(c[1], "shipping_charge"), c[0], val(c[0], "total_amount"), val(c[0], "shipping_charge"))
		}
	}
	// What posted: the receivable is the printed total, the shipping is not counted twice.
	j1 := stage1484Posted(t, schema, "SalesInvoice", inv1)
	t.Logf("ERP bill 1 journal: %v", j1)
	var debit int64
	for k, v := range j1 {
		if len(k) > 2 && k[:2] == "D:" && k != "D:5100" {
			debit += v
		}
	}
	if math.Abs(PaiseToRupees(debit)-val(bill1, "total_amount")) > 0.011 {
		t.Errorf("ERP bill 1 receivable %v, OMS bill total %v", PaiseToRupees(debit), val(bill1, "total_amount"))
	}
}

// An order with no shipping charge, and a bill already raised, are as before.
func TestStage14846OMSBillWithoutShippingUnchanged(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatal(err)
	}
	bill, _, err := GenerateOMSBillForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	var has bool
	_ = db.DB.QueryRow(`SELECT (data->>'total_amount')::numeric, data ? 'shipping_charge' FROM `+schema+`.documents WHERE id = $1`, bill).Scan(&total, &has)
	if has || math.Abs(total-900) > 0.001 {
		t.Errorf("no shipping: total %v, has charge key %v; want 900 and none", total, has)
	}
}
