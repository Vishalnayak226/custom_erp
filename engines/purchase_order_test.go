package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 40.1's DB-backed half: the Location -> LegalEntity -> state walk, the
// Vendor -> GSTIN walk, and the printed PO that stitches them together.
//
// Fixture ids are all TEST35- prefixed and torn down either side of the run,
// following gst_test.go's convention - this package shares one dev database
// with every other test, so a fixture that outlives its test shows up as
// somebody else's failure.
func TestPurchaseOrderPreviewAndPrint(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		skuA      = "TEST35-ITEM-A"
		skuB      = "TEST35-ITEM-B"
		skuNoHSN  = "TEST35-ITEM-NOHSN"
		vendorKA  = "TEST35-VENDOR-KA" // Karnataka (29), by GSTIN
		vendorMH  = "TEST35-VENDOR-MH" // Maharashtra (27), by state field only
		vendorBad = "TEST35-VENDOR-NOSTATE"
		entityMH  = "TEST35-ENTITY-MH"
		locationX = "TEST35-LOC"
		poID      = "TEST35-PO-1"
	)

	ids := []string{skuA, skuB, skuNoHSN, vendorKA, vendorMH, vendorBad, entityMH, locationX, poID}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()

	insert := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')",
			id, doctype, raw); err != nil {
			t.Fatalf("insert %s %s: %v", doctype, id, err)
		}
	}

	insert(skuA, "Item", map[string]interface{}{"code": skuA, "name": "Test Tee", "hsn_code": "6109", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(skuB, "Item", map[string]interface{}{"code": skuB, "name": "Test Jeans", "hsn_code": "6203", "gst_rate": 12.0, "tax_treatment": "Taxable"})
	insert(skuNoHSN, "Item", map[string]interface{}{"code": skuNoHSN, "name": "Unclassified", "gst_rate": 5.0})
	insert(vendorKA, "Vendor", map[string]interface{}{"code": vendorKA, "name": "Karnataka Supplier", "gstin": "29AABCU9603R1ZM", "address": "Bengaluru", "contact_email": "ka@example.test"})
	insert(vendorMH, "Vendor", map[string]interface{}{"code": vendorMH, "name": "Mumbai Supplier", "state": "Maharashtra"})
	insert(vendorBad, "Vendor", map[string]interface{}{"code": vendorBad, "name": "Unregistered Supplier"})
	insert(entityMH, "LegalEntity", map[string]interface{}{"code": entityMH, "name": "Test Entity", "gstin": "27AAPFU0939F1ZV", "address": "Mumbai"})
	insert(locationX, "Location", map[string]interface{}{"code": locationX, "name": "Test Store", "type": "Store", "legal_entity": entityMH})

	itemsJSON := func(lines ...map[string]interface{}) string {
		raw, _ := json.Marshal(lines)
		return string(raw)
	}

	t.Run("interstate is derived from the vendor's GSTIN against the entity's", func(t *testing.T) {
		pos := ResolvePlaceOfSupply(tenantID, locationX, vendorKA)
		if !pos.Derived {
			t.Fatalf("expected a derived place of supply, got reason %q", pos.Reason)
		}
		if !pos.Interstate || pos.VendorStateCode != "29" || pos.BuyerStateCode != "27" {
			t.Fatalf("got %+v, want interstate 29 -> 27", pos)
		}
	})

	t.Run("a vendor with no GSTIN falls back to its state field", func(t *testing.T) {
		pos := ResolvePlaceOfSupply(tenantID, locationX, vendorMH)
		if !pos.Derived {
			t.Fatalf("expected derivation from the state field, got reason %q", pos.Reason)
		}
		if pos.Interstate {
			t.Fatalf("Maharashtra vendor + Maharashtra entity must be intra-state, got %+v", pos)
		}
	})

	t.Run("a vendor with neither is reported undetermined, not guessed as intra-state", func(t *testing.T) {
		pos := ResolvePlaceOfSupply(tenantID, locationX, vendorBad)
		if pos.Derived {
			t.Fatalf("expected no derivation, got %+v", pos)
		}
		if pos.Reason == "" {
			t.Fatal("an undetermined place of supply must say why")
		}
	})

	t.Run("Exclusive adds GST on top; Inclusive backs it out", func(t *testing.T) {
		lines := itemsJSON(map[string]interface{}{"sku": skuA, "qty": 10, "rate": 450.0})

		exc, err := PreviewPurchaseOrder(tenantID, map[string]interface{}{
			"items": lines, "vendor": vendorKA, "location": locationX, "gst_mode": GSTModeExclusive})
		if err != nil {
			t.Fatalf("exclusive preview: %v", err)
		}
		if exc.Breakdown.TaxableAmount != 4500 || exc.GrandTotal != 4725 {
			t.Fatalf("exclusive: taxable %.2f grand %.2f, want 4500 / 4725", exc.Breakdown.TaxableAmount, exc.GrandTotal)
		}
		// Karnataka vendor, Maharashtra entity - the tax must be IGST only.
		if exc.Breakdown.IGST != 225 || exc.Breakdown.CGST != 0 {
			t.Fatalf("exclusive: IGST %.2f CGST %.2f, want 225 / 0", exc.Breakdown.IGST, exc.Breakdown.CGST)
		}

		inc, err := PreviewPurchaseOrder(tenantID, map[string]interface{}{
			"items": lines, "vendor": vendorKA, "location": locationX, "gst_mode": GSTModeInclusive})
		if err != nil {
			t.Fatalf("inclusive preview: %v", err)
		}
		if inc.GrandTotal != 4500 {
			t.Fatalf("inclusive grand total = %.2f, want the gross 4500 back", inc.GrandTotal)
		}
		if inc.Breakdown.TaxableAmount >= 4500 {
			t.Fatalf("inclusive taxable = %.2f, want it backed out below the gross", inc.Breakdown.TaxableAmount)
		}
	})

	t.Run("an unclassified item flags its own line rather than failing the whole preview", func(t *testing.T) {
		p, err := PreviewPurchaseOrder(tenantID, map[string]interface{}{
			"items": itemsJSON(
				map[string]interface{}{"sku": skuA, "qty": 2, "rate": 100.0},
				map[string]interface{}{"sku": skuNoHSN, "qty": 1, "rate": 50.0},
			),
			"vendor": vendorKA, "location": locationX,
		})
		if err != nil {
			t.Fatalf("preview should not error on a bad line: %v", err)
		}
		if !p.Blocking {
			t.Fatal("expected Blocking so the screen can disable Save")
		}
		if len(p.Lines) != 2 {
			t.Fatalf("expected both lines returned, got %d", len(p.Lines))
		}
		if p.Lines[0].Error != "" {
			t.Errorf("the good line was flagged: %q", p.Lines[0].Error)
		}
		if p.Lines[1].Error == "" {
			t.Error("the unclassified line was not flagged")
		}
		// The good line must still be priced, so the maker sees what is right
		// alongside what is wrong.
		if p.Lines[0].LineTotal <= 0 {
			t.Errorf("the good line was not priced: %+v", p.Lines[0])
		}
	})

	t.Run("an explicit override beats the derivation", func(t *testing.T) {
		p, err := PreviewPurchaseOrder(tenantID, map[string]interface{}{
			"items":  itemsJSON(map[string]interface{}{"sku": skuA, "qty": 10, "rate": 450.0}),
			"vendor": vendorKA, "location": locationX,
			"interstate": false, "interstate_override": true,
		})
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		if p.Breakdown.IGST != 0 || p.Breakdown.CGST == 0 {
			t.Fatalf("override to intra-state ignored: %+v", p.Breakdown)
		}
		// The derivation is still reported, so the banner can show what the
		// masters actually say while honouring the override.
		if !p.PlaceOfSupply.Interstate {
			t.Error("the underlying derivation should still report inter-state")
		}
	})

	t.Run("ApplyPlaceOfSupply stamps the payload but respects an override", func(t *testing.T) {
		payload := map[string]interface{}{"location": locationX, "vendor": vendorKA}
		ApplyPlaceOfSupply(tenantID, payload)
		if payload["interstate"] != true {
			t.Errorf("interstate not stamped: %#v", payload["interstate"])
		}
		if payload["place_of_supply"] == "" {
			t.Error("place_of_supply summary not stamped")
		}

		overridden := map[string]interface{}{"location": locationX, "vendor": vendorKA, "interstate": false, "interstate_override": true}
		ApplyPlaceOfSupply(tenantID, overridden)
		if overridden["interstate"] != false {
			t.Errorf("override was overwritten by the derivation: %#v", overridden["interstate"])
		}
	})

	t.Run("the printed PO resolves both parties and carries the amount in words", func(t *testing.T) {
		insert(poID, "PurchaseOrder", map[string]interface{}{
			"po_number": "PO-TEST35-1", "code": "PO-TEST35-1",
			"vendor": vendorKA, "vendor_id": vendorKA,
			"location": locationX, "target_warehouse": locationX,
			"gst_mode": GSTModeExclusive,
			"items": itemsJSON(
				map[string]interface{}{"sku": skuA, "qty": 10, "rate": 450.0, "mrp": 699.0},
				map[string]interface{}{"sku": skuB, "qty": 5, "rate": 1200.0},
			),
		})

		doc, err := BuildPurchaseOrderPrint(tenantID, poID)
		if err != nil {
			t.Fatalf("print: %v", err)
		}
		if doc.PONumber != "PO-TEST35-1" {
			t.Errorf("po number = %q", doc.PONumber)
		}
		if doc.Vendor.Name != "Karnataka Supplier" || doc.Vendor.GSTIN != "29AABCU9603R1ZM" {
			t.Errorf("vendor block not resolved: %+v", doc.Vendor)
		}
		if doc.Buyer.Name != "Test Entity" || doc.Buyer.GSTIN != "27AAPFU0939F1ZV" {
			t.Errorf("buyer block not resolved from Location -> LegalEntity: %+v", doc.Buyer)
		}
		// 4500 + 6000 taxable, IGST 225 + 720.
		if doc.Breakdown.TaxableAmount != 10500 || doc.GrandTotal != 11445 {
			t.Errorf("totals = taxable %.2f grand %.2f, want 10500 / 11445", doc.Breakdown.TaxableAmount, doc.GrandTotal)
		}
		if doc.AmountInWords == "" {
			t.Error("amount in words missing from the printed copy")
		}
		// MRP is stripped by the handler, not the engine - the engine keeps it
		// because the composer needs it back when amending.
		if doc.Lines[0].MRP != 699 {
			t.Errorf("engine dropped MRP; the handler is what strips it for the vendor copy: %+v", doc.Lines[0])
		}
	})

	t.Run("a missing PO is a clean not-found, not a panic", func(t *testing.T) {
		if _, err := BuildPurchaseOrderPrint(tenantID, "TEST35-NO-SUCH-PO"); err == nil {
			t.Fatal("expected an error for a PO that does not exist")
		}
	})
}
