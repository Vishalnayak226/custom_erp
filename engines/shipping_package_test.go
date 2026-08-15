package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 35.4's regression suite: package -> invoice -> label -> manifest, plus
// the gate pass and the cancellation credit note.
//
// The assertions are per-fixture rather than on any global count. This suite
// shares one database with every other test in the package, so a global "how
// many packages exist" would be nobody's to own - the same discipline
// reservation_sweeper_test.go settled on.

const (
	spSKU      = "TEST-PKG-SKU-A"
	spSKUB     = "TEST-PKG-SKU-B"
	spOrder    = "TEST-PKG-ORDER"
	spTask     = "TEST-PKG-TASK"
	spLocation = "TEST-PKG-LOC"
)

// spInitDB connects once for the whole file rather than once per test.
//
// db.InitDB reassigns the global pool without closing the previous one, so
// every call leaks up to dbMaxIdleConns connections for ConnMaxIdleTime. The
// package convention is one InitDB per test, which was already close enough to
// Postgres's default max_connections that adding this file's tests tipped
// `go test ./... -p 1` into "sorry, too many clients already" - a connection
// ceiling, not a logic failure. Reusing a live pool keeps this file's cost at
// one connection set instead of twelve; the existing per-test convention in
// the other files is left exactly as it is.
func spInitDB() {
	if db.DB != nil && db.DB.Ping() == nil {
		return
	}
	db.InitDB(testConnStr())
}

// spFixture builds the minimum real chain: two classified Items, a SalesOrder
// with two lines, and a FulfillmentTask already Packed. Everything is deleted
// on the way in as well as out, so a previous failed run cannot poison this one.
func spFixture(t *testing.T) string {
	t.Helper()
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM `+schema+`.doctype_meta WHERE name = 'ShippingPackage')`).Scan(&ready); err != nil {
		t.Fatalf("inspect doctype_meta: %v", err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_4_shipping_package.sql has not been applied to this database")
	}

	spCleanup(t, schema)
	t.Cleanup(func() { spCleanup(t, schema) })

	insertDoc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		data["code"] = id
		body, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')",
			id, doctype, body, status); err != nil {
			t.Fatalf("insert %s %s: %v", doctype, id, err)
		}
	}

	for _, sku := range []string{spSKU, spSKUB} {
		insertDoc(sku, "Item", "Active", map[string]interface{}{
			"name": "Package Test " + sku, "hsn_code": "6109", "gst_rate": 18.0,
			"tax_treatment": "Taxable", "sale_price": 100.0,
		})
	}
	insertDoc(spOrder, "SalesOrder", "Reserved", map[string]interface{}{
		"customer_name": "Package Test Customer", "shipping_address": "1 Test Road",
		"payment_status": "Confirmed", "order_status": "Reserved", "total_amount": 1180.0,
	})
	insertDoc(spOrder+"-L1", "SalesOrderLine", "Reserved", map[string]interface{}{
		"order_id": spOrder, "sku": spSKU, "qty": 10, "unit_price": 100.0,
		"location_code": spLocation, "line_status": "Reserved",
	})
	insertDoc(spOrder+"-L2", "SalesOrderLine", "Reserved", map[string]interface{}{
		"order_id": spOrder, "sku": spSKUB, "qty": 2, "unit_price": 50.0,
		"location_code": spLocation, "line_status": "Reserved",
	})
	// Packed, with one line short-picked: 8 of 10 picked and packed. The
	// package must carry 8, not 10 - that is the assertion the whole
	// invoice-from-pack item rests on.
	insertDoc(spTask, "FulfillmentTask", "Packed", map[string]interface{}{
		"order_id": spOrder, "location_code": spLocation, "status": "Packed",
		"items": []map[string]interface{}{
			{"sku": spSKU, "qty": 10, "picked_qty": 8, "packed_qty": 8, "short_qty": 2},
			{"sku": spSKUB, "qty": 2, "picked_qty": 2, "packed_qty": 2, "short_qty": 0},
		},
	})
	return schema
}

func spCleanup(t *testing.T, schema string) {
	t.Helper()
	// Children first, then the fixtures they point at.
	for _, stmt := range []string{
		`DELETE FROM ` + schema + `.documents WHERE doctype = 'CreditNote' AND data->>'sales_order_id' = $1`,
		`DELETE FROM ` + schema + `.documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1`,
		`DELETE FROM ` + schema + `.documents WHERE doctype = 'ShippingPackage' AND data->>'order_id' = $1`,
		`DELETE FROM ` + schema + `.documents WHERE doctype = 'LogisticsBooking' AND data->>'order_id' = $1`,
	} {
		if _, err := db.DB.Exec(stmt, spOrder); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
	if _, err := db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id IN ($1,$2,$3,$4,$5,$6)`,
		spOrder, spOrder+"-L1", spOrder+"-L2", spTask, spSKU, spSKUB); err != nil {
		t.Fatalf("cleanup fixtures: %v", err)
	}
}

// TestShippingPackageFromPackedTask covers 35.4.1's creation contract: packed
// quantities only, and idempotent.
func TestShippingPackageFromPackedTask(t *testing.T) {
	schema := spFixture(t)

	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("CreateShippingPackageFromTask: %v", err)
	}
	p, err := loadShippingPackage(schema, pkgID)
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	if p.Status != "Draft" {
		t.Errorf("new package status = %q, want Draft", p.Status)
	}
	if p.OrderID != spOrder || p.LocationCode != spLocation {
		t.Errorf("package did not inherit order/location: %+v", p)
	}
	got := map[string]int{}
	for _, it := range p.Items {
		got[it.SKU] = it.Qty
	}
	// The short-picked line is the point: 8 packed of 10 ordered.
	if got[spSKU] != 8 {
		t.Errorf("package holds %d of %s, want the packed 8 (not the ordered 10)", got[spSKU], spSKU)
	}
	if got[spSKUB] != 2 {
		t.Errorf("package holds %d of %s, want 2", got[spSKUB], spSKUB)
	}

	again, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("second CreateShippingPackageFromTask: %v", err)
	}
	if again != pkgID {
		t.Errorf("creation is not idempotent: got %s then %s - a double-tapped pack button would produce two parcels", pkgID, again)
	}
}

// TestShippingPackageSplitRefusals pins the two refusals that keep a split from
// silently losing or duplicating stock.
func TestShippingPackageSplitRefusals(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{
		{SKU: spSKU, Qty: 8}, {SKU: spSKUB, Qty: 2},
	}, "tester"); err == nil {
		t.Error("splitting every unit out was allowed - that leaves an empty package that still looks shippable")
	}

	if _, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{{SKU: spSKU, Qty: 9}}, "tester"); err == nil {
		t.Error("moving more than the package holds was allowed")
	}

	// Two rows for the same SKU must be summed before validation, or each
	// passes on its own and the split moves 10 out of a package holding 8.
	if _, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{
		{SKU: spSKU, Qty: 5}, {SKU: spSKU, Qty: 5},
	}, "tester"); err == nil {
		t.Error("duplicate split rows were not summed - 5+5 was accepted against a holding of 8")
	}

	if _, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{{SKU: "NOT-IN-BOX", Qty: 1}}, "tester"); err == nil {
		t.Error("splitting out a SKU the package does not hold was allowed")
	}

	// After every refusal the source must be untouched.
	p, err := loadShippingPackage(schema, pkgID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	total := 0
	for _, it := range p.Items {
		total += it.Qty
	}
	if total != 10 {
		t.Errorf("source package holds %d units after four refused splits, want the original 10", total)
	}
}

// TestShippingPackageSplitConserves is the positive case: contents add up.
func TestShippingPackageSplitConserves(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	newID, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{{SKU: spSKU, Qty: 3}}, "tester")
	if err != nil {
		t.Fatalf("SplitShippingPackage: %v", err)
	}

	sum := map[string]int{}
	for _, id := range []string{pkgID, newID} {
		p, err := loadShippingPackage(schema, id)
		if err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
		for _, it := range p.Items {
			sum[it.SKU] += it.Qty
		}
	}
	if sum[spSKU] != 8 || sum[spSKUB] != 2 {
		t.Errorf("split did not conserve contents: got %v, want map[%s:8 %s:2]", sum, spSKU, spSKUB)
	}
	target, _ := loadShippingPackage(schema, newID)
	if target.SplitFrom != pkgID {
		t.Errorf("split package's lineage = %q, want %q", target.SplitFrom, pkgID)
	}
	if target.OrderID != spOrder {
		t.Errorf("split package lost its order link: %q", target.OrderID)
	}
}

// TestInvoiceFromPack is 35.4.2 - the item 26.12.3 deferred.
func TestInvoiceFromPack(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	invoiceID, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatalf("GenerateInvoiceForPackage: %v", err)
	}

	var dataStr, status string
	if err := db.DB.QueryRow(`SELECT data, status FROM `+schema+`.documents WHERE id = $1 AND doctype = 'SalesInvoice'`, invoiceID).
		Scan(&dataStr, &status); err != nil {
		t.Fatalf("read invoice: %v", err)
	}
	if status != "Draft" {
		t.Errorf("invoice status = %q, want Draft - pack automation must never post to the GL by itself", status)
	}
	var inv map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &inv); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}

	// 8 x 100 + 2 x 50 = 900 gross, tax-inclusive at 18%.
	if got := numFromInterface(inv["total_amount"]); got != 900 {
		t.Errorf("invoice total = %v, want 900 (8x100 packed + 2x50, NOT the ordered 10x100)", got)
	}
	// Intra-state by default, so CGST+SGST and no IGST.
	cgst, sgst, igst := numFromInterface(inv["cgst"]), numFromInterface(inv["sgst"]), numFromInterface(inv["igst"])
	if igst != 0 {
		t.Errorf("igst = %v on a default intra-state invoice, want 0", igst)
	}
	if cgst <= 0 || cgst != sgst {
		t.Errorf("cgst/sgst = %v/%v, want an equal positive split", cgst, sgst)
	}

	// The invoice must add up exactly, in both directions. The shared GST
	// breakdown does not (its CGST+SGST came to 137.30 against a TotalTax of
	// 137.28, and its taxable value was stored unrounded);
	// reconcileInvoiceAmounts is what makes the document itself consistent, so
	// these two are the assertions that pin it.
	taxable := numFromInterface(inv["taxable_amount"])
	totalTax := numFromInterface(inv["total_tax"])
	nonTaxable := numFromInterface(inv["non_taxable"])
	if cgst+sgst+igst != totalTax {
		t.Errorf("tax components %v+%v+%v do not sum to total_tax %v", cgst, sgst, igst, totalTax)
	}
	if taxable+nonTaxable+totalTax != 900 {
		t.Errorf("taxable %v + non-taxable %v + tax %v does not reconcile to the 900 total", taxable, nonTaxable, totalTax)
	}
	// Rounded to paise, not carrying float noise onto a tax document.
	if taxable != round2(taxable) || totalTax != round2(totalTax) {
		t.Errorf("invoice amounts are not rounded to paise: taxable=%v tax=%v", taxable, totalTax)
	}
	if inv["shipping_package_id"] != pkgID {
		t.Errorf("invoice does not reference its package: %v", inv["shipping_package_id"])
	}

	p, _ := loadShippingPackage(schema, pkgID)
	if p.Status != "Invoiced" || p.SalesInvoiceID != invoiceID {
		t.Errorf("package after invoicing = %s/%s, want Invoiced/%s", p.Status, p.SalesInvoiceID, invoiceID)
	}

	if again, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{}); err != nil || again != invoiceID {
		t.Errorf("invoicing is not idempotent: got %s, %v", again, err)
	}

	// Contents are frozen once a tax document exists against them.
	w := 5.0
	if err := UpdateShippingPackage("default", pkgID, ShippingPackageUpdate{WeightKg: &w}, "tester"); err == nil {
		t.Error("an Invoiced package accepted an edit - its invoice would silently become wrong")
	}
	if _, err := SplitShippingPackage("default", pkgID, []ShippingPackageLine{{SKU: spSKU, Qty: 1}}, "tester"); err == nil {
		t.Error("an Invoiced package accepted a split")
	}
}

// TestInvoiceFromPackInterstate checks the override actually switches the tax
// split, since that is the only lever an operator has for a bill-to/ship-to.
func TestInvoiceFromPackInterstate(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	interstate := true
	invoiceID, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{Interstate: &interstate})
	if err != nil {
		t.Fatalf("GenerateInvoiceForPackage: %v", err)
	}
	var dataStr string
	if err := db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id = $1`, invoiceID).Scan(&dataStr); err != nil {
		t.Fatalf("read invoice: %v", err)
	}
	var inv map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &inv)
	if numFromInterface(inv["igst"]) <= 0 {
		t.Errorf("interstate override produced no IGST: %v", inv)
	}
	if numFromInterface(inv["cgst"]) != 0 || numFromInterface(inv["sgst"]) != 0 {
		t.Errorf("interstate invoice also charged CGST/SGST - never both: %v", inv)
	}
	if basis, _ := inv["gst_basis"].(string); !strings.Contains(basis, "override") {
		t.Errorf("gst_basis = %q, want it to record that an override was used", basis)
	}
}

// TestLabelAndManifestOrdering is 35.4.3, and the backward-compatibility
// boundary that goes with it.
func TestLabelAndManifestOrdering(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// No explicit package id - the booking must find the task's package itself,
	// or the rule would only bind callers who already knew about packages.
	bookingID, err := CreateLogisticsBooking("default", spOrder, spTask, "TestCourier", "", "560001", 40)
	if err != nil {
		t.Fatalf("CreateLogisticsBooking: %v", err)
	}
	_, bData, _, err := fetchLogisticsBooking("default", bookingID)
	if err != nil {
		t.Fatalf("fetch booking: %v", err)
	}
	if bData["shipping_package_id"] != pkgID {
		t.Fatalf("booking did not auto-link its task's package: %v", bData["shipping_package_id"])
	}

	if _, err := GenerateShippingLabel("default", bookingID); err == nil {
		t.Error("a label printed before the invoice - the parcel could move with no tax document")
	}

	// An un-labelled packaged booking must not be manifested either.
	if _, _, err := GenerateManifest("default", "TestCourier", spLocation); err == nil {
		t.Error("an un-labelled packaged booking was manifested")
	}

	if _, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{}); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	label, err := GenerateShippingLabel("default", bookingID)
	if err != nil {
		t.Fatalf("label after invoicing was refused: %v", err)
	}
	if !strings.Contains(label, "AWB") {
		t.Errorf("label does not carry an AWB: %q", label)
	}

	manifestID, count, err := GenerateManifest("default", "TestCourier", spLocation)
	if err != nil {
		t.Fatalf("GenerateManifest after label: %v", err)
	}
	if count != 1 {
		t.Errorf("manifest grouped %d shipments, want 1", count)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, manifestID)
	})

	// Handover closes the package lifecycle.
	if err := HandoverManifest("default", manifestID, "tester"); err != nil {
		t.Fatalf("HandoverManifest: %v", err)
	}
	p, _ := loadShippingPackage(schema, pkgID)
	if p.Status != "Shipped" {
		t.Errorf("package after handover = %q, want Shipped", p.Status)
	}
}

// TestLegacyBookingUnaffectedByOrderingRule is the other half of 35.4.3: a
// booking with no package is exactly as labellable as it was before.
func TestLegacyBookingUnaffectedByOrderingRule(t *testing.T) {
	schema := spFixture(t)
	// No task, so no package can be inferred - the pre-35.4 shape.
	bookingID, err := CreateLogisticsBooking("default", spOrder, "", "TestCourier", "TRKLEGACY", "560001", 25)
	if err != nil {
		t.Fatalf("CreateLogisticsBooking: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, bookingID) })

	if _, err := GenerateShippingLabel("default", bookingID); err != nil {
		t.Errorf("a legacy booking with no package was refused a label: %v", err)
	}
}

// TestOrderShippedDoesNotDoubleInvoice guards the widening in
// CreateSalesInvoiceFromOrder. Without it, every package-invoiced order would
// be invoiced a second time for its full value the moment it reached Shipped.
func TestOrderShippedDoesNotDoubleInvoice(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	packInvoice, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatalf("invoice: %v", err)
	}

	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = jsonb_set(data, '{order_status}', '"Shipped"'), status = 'Shipped' WHERE id = $1`, spOrder); err != nil {
		t.Fatalf("mark shipped: %v", err)
	}
	got, err := CreateSalesInvoiceFromOrder("default", spOrder, "tester")
	if err != nil {
		t.Fatalf("CreateSalesInvoiceFromOrder: %v", err)
	}
	if got != packInvoice {
		t.Errorf("order-level invoicing returned %s, want the existing package invoice %s", got, packInvoice)
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype = 'SalesInvoice' AND data->>'sales_order_id' = $1 AND deleted_at IS NULL`, spOrder).Scan(&n); err != nil {
		t.Fatalf("count invoices: %v", err)
	}
	if n != 1 {
		t.Errorf("order carries %d invoices after shipping, want exactly 1 - the customer would be billed twice", n)
	}
}

// TestCancellationCreditNote is 35.4.5.
func TestCancellationCreditNote(t *testing.T) {
	schema := spFixture(t)
	pkgID, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	invoiceID, err := GenerateInvoiceForPackage("default", pkgID, "tester", PackInvoiceOptions{})
	if err != nil {
		t.Fatalf("invoice: %v", err)
	}

	// A Draft invoice was never posted, so it is voided, not credited.
	notes, err := IssueCancellationCreditNotes("default", spOrder, "RC-TEST-CANCEL", "tester")
	if err != nil {
		t.Fatalf("IssueCancellationCreditNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("credited an unposted Draft invoice (%v) - there was nothing recognised to reverse", notes)
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE id = $1`, invoiceID).Scan(&status); err != nil {
		t.Fatalf("read invoice: %v", err)
	}
	if status != "Cancelled" {
		t.Errorf("draft invoice status = %q after cancellation, want Cancelled", status)
	}

	// Now the real case: a posted invoice must produce a credit note.
	posted := "TEST-PKG-POSTED-INV"
	body, _ := json.Marshal(map[string]interface{}{
		"code": posted, "sales_order_id": spOrder, "customer": "Package Test Customer",
		"total_amount": 900, "status": "Approved",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,'SalesInvoice',$2,'Approved','system')", posted, body); err != nil {
		t.Fatalf("seed posted invoice: %v", err)
	}

	notes, err = IssueCancellationCreditNotes("default", spOrder, "RC-TEST-CANCEL", "tester")
	if err != nil {
		t.Fatalf("IssueCancellationCreditNotes (posted): %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("got %d credit notes for one posted invoice, want 1", len(notes))
	}
	var noteData string
	if err := db.DB.QueryRow(`SELECT data FROM `+schema+`.documents WHERE id = $1 AND doctype = 'CreditNote'`, notes[0]).Scan(&noteData); err != nil {
		t.Fatalf("read credit note: %v", err)
	}
	var note map[string]interface{}
	_ = json.Unmarshal([]byte(noteData), &note)
	if numFromInterface(note["amount"]) != 900 {
		t.Errorf("credit note amount = %v, want the invoiced 900", note["amount"])
	}
	if note["sales_invoice_id"] != posted {
		t.Errorf("credit note does not reference its invoice: %v", note["sales_invoice_id"])
	}

	again, err := IssueCancellationCreditNotes("default", spOrder, "RC-TEST-CANCEL", "tester")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(again) != 1 || again[0] != notes[0] {
		t.Errorf("credit is not idempotent: %v then %v - a retried cancellation would double the credit", notes, again)
	}
}

// TestGatePassLifecycle is 35.4.4.
func TestGatePassLifecycle(t *testing.T) {
	schema := spFixture(t)

	id, err := CreateGatePass("default", GatePassInput{
		LocationCode: spLocation, Carrier: "TestCourier", DriverName: "Test Driver",
	}, "tester")
	if err != nil {
		t.Fatalf("CreateGatePass: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, id) })

	// Issuing without a vehicle must be refused - a departure record with no
	// vehicle on it cannot be reconciled against anything later.
	if err := IssueGatePass("default", id, "tester"); err == nil {
		t.Error("a gate pass was issued with no vehicle number")
	}

	vehicle := "KA-01-TEST-9999"
	if err := UpdateGatePass("default", id, GatePassInput{VehicleNumber: vehicle}, "tester"); err != nil {
		t.Fatalf("UpdateGatePass: %v", err)
	}
	if err := IssueGatePass("default", id, "tester"); err != nil {
		t.Fatalf("IssueGatePass after setting a vehicle: %v", err)
	}
	data, status, err := loadGatePass(schema, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if status != "Issued" {
		t.Errorf("status = %q, want Issued", status)
	}
	if data["issued_at"] == nil || data["issued_at"] == "" {
		t.Error("issued_at was not stamped")
	}

	if err := CompleteGatePass("default", id, "tester"); err != nil {
		t.Fatalf("CompleteGatePass: %v", err)
	}
	if _, status, _ = loadGatePass(schema, id); status != "Completed" {
		t.Errorf("status = %q, want Completed", status)
	}
	// Completed is terminal for amendments.
	if err := UpdateGatePass("default", id, GatePassInput{DriverName: "Someone Else"}, "tester"); err == nil {
		t.Error("a Completed gate pass was amended - that rewrites what left the building")
	}

	found, err := SearchGatePasses("default", spLocation, "TEST-9999", "", "")
	if err != nil {
		t.Fatalf("SearchGatePasses: %v", err)
	}
	if len(found) == 0 {
		t.Error("gate pass search by partial vehicle number found nothing")
	}
}

// TestGatePassFromManifestTakesItsCounts checks the transcription guard: the
// desk cannot type a package count that disagrees with the manifest.
func TestGatePassFromManifestTakesItsCounts(t *testing.T) {
	schema := spFixture(t)
	manifestID := "TEST-PKG-MANIFEST"
	body, _ := json.Marshal(map[string]interface{}{
		"code": manifestID, "courier": "ManifestCourier", "location_code": "MANIFEST-LOC",
		"shipment_count": 7, "status": "Open",
	})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,'Manifest',$2,'Open','system')", manifestID, body); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, manifestID) })

	id, err := CreateGatePass("default", GatePassInput{
		ManifestID: manifestID, LocationCode: "WRONG-LOC", Carrier: "WrongCourier",
	}, "tester")
	if err != nil {
		t.Fatalf("CreateGatePass: %v", err)
	}
	t.Cleanup(func() { _, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, id) })

	data, _, err := loadGatePass(schema, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if data["location_code"] != "MANIFEST-LOC" || data["carrier"] != "ManifestCourier" {
		t.Errorf("gate pass kept the caller's values instead of the manifest's: %v", data)
	}
	if numFromInterface(data["package_count"]) != 7 {
		t.Errorf("package_count = %v, want the manifest's 7", data["package_count"])
	}

	if _, err := CreateGatePass("default", GatePassInput{ManifestID: "NO-SUCH-MANIFEST", LocationCode: "X"}, "tester"); err == nil {
		t.Error("a gate pass referenced a manifest that does not exist")
	}
}

// TestShippingPackageRequiresPackedTask pins the entry guard.
func TestShippingPackageRequiresPackedTask(t *testing.T) {
	schema := spFixture(t)
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = jsonb_set(data, '{status}', '"Pending"'), status = 'Pending' WHERE id = $1`, spTask); err != nil {
		t.Fatalf("set task pending: %v", err)
	}
	_, err := CreateShippingPackageFromTask("default", spTask, "tester")
	if err == nil {
		t.Fatal("a package was created from a task that has not finished packing")
	}
	if !strings.Contains(err.Error(), "Pending") {
		t.Errorf("error should name the blocking status, got: %v", err)
	}
	if _, err := CreateShippingPackageFromTask("default", "NO-SUCH-TASK", "tester"); err == nil {
		t.Error("a package was created from a task that does not exist")
	}
}
