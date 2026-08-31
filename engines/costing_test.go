package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Each subtest below uses its OWN sku/po/grn ids and its own cleanup - Go
// runs t.Run subtests in declaration order by default, and an earlier
// subtest's receipt would otherwise silently change a later subtest's
// weighted-average starting point if they shared one item, exactly the kind
// of cross-subtest coupling this file's own first draft got wrong.
func TestStage373CostingValuation(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	insert := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw)
	}
	itemsJSON := func(lines ...map[string]interface{}) string {
		raw, _ := json.Marshal(lines)
		return string(raw)
	}
	cleanupIDs := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_type IN ('GRN','LandedCostVoucher') AND document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", id)
		}
	}

	t.Run("a GRN receipt records a real moving-average cost and posts Dr 1200 / Cr 2100", func(t *testing.T) {
		const sku, poID, grnID, location = "TEST373A-ITEM", "TEST373A-PO", "TEST373A-GRN", "TEST373A-LOC"
		ids := []string{sku, poID, grnID}
		cleanupIDs(ids...)
		defer cleanupIDs(ids...)

		insert(sku, "Item", map[string]interface{}{"code": sku, "name": "Test Costed Item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
		insert(poID, "PurchaseOrder", map[string]interface{}{
			"code": poID, "gst_mode": GSTModeExclusive,
			"items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0}),
		})
		insert(grnID, "GRN", map[string]interface{}{
			"code": grnID, "po_id": poID, "location": location,
			"received_items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10}),
		})

		items := []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, location, items, "system", grnID); err != nil {
			t.Fatalf("PostGRNReceiptWithQC: %v", err)
		}

		unitCostPaise, hasCost, err := GetItemUnitCost(tenantID, sku)
		if err != nil {
			t.Fatalf("GetItemUnitCost: %v", err)
		}
		if !hasCost {
			t.Fatalf("expected the item to have a recorded cost after receipt")
		}
		if unitCostPaise != 10000 {
			t.Fatalf("expected unit cost 10000 paise (Rs 100, ex-GST), got %d", unitCostPaise)
		}

		var debit, credit int
		if err := db.DB.QueryRow("SELECT debit, credit FROM "+schema+".gl_postings WHERE document_type='GRN' AND document_id=$1 AND account_code='1200'", grnID).Scan(&debit, &credit); err != nil {
			t.Fatalf("query 1200 posting: %v", err)
		}
		if debit != 100000 || credit != 0 {
			t.Fatalf("expected 1200 Dr 100000 paise (Rs 1000), got debit=%d credit=%d", debit, credit)
		}
		if err := db.DB.QueryRow("SELECT debit, credit FROM "+schema+".gl_postings WHERE document_type='GRN' AND document_id=$1 AND account_code='2100'", grnID).Scan(&debit, &credit); err != nil {
			t.Fatalf("query 2100 posting: %v", err)
		}
		if credit != 100000 || debit != 0 {
			t.Fatalf("expected 2100 Cr 100000 paise, got debit=%d credit=%d", debit, credit)
		}
	})

	t.Run("a second receipt at a different rate blends into a real weighted average", func(t *testing.T) {
		const sku, poID1, poID2, grnID1, grnID2, location = "TEST373B-ITEM", "TEST373B-PO1", "TEST373B-PO2", "TEST373B-GRN1", "TEST373B-GRN2", "TEST373B-LOC"
		ids := []string{sku, poID1, poID2, grnID1, grnID2}
		cleanupIDs(ids...)
		defer cleanupIDs(ids...)

		insert(sku, "Item", map[string]interface{}{"code": sku, "name": "Test Blended Item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
		insert(poID1, "PurchaseOrder", map[string]interface{}{"code": poID1, "gst_mode": GSTModeExclusive, "items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0})})
		insert(grnID1, "GRN", map[string]interface{}{"code": grnID1, "po_id": poID1, "location": location, "received_items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10})})
		items1 := []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, location, items1, "system", grnID1); err != nil {
			t.Fatalf("PostGRNReceiptWithQC (first receipt): %v", err)
		}

		insert(poID2, "PurchaseOrder", map[string]interface{}{"code": poID2, "gst_mode": GSTModeExclusive, "items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "rate": 200.0})})
		insert(grnID2, "GRN", map[string]interface{}{"code": grnID2, "po_id": poID2, "location": location, "received_items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10})})
		items2 := []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, location, items2, "system", grnID2); err != nil {
			t.Fatalf("PostGRNReceiptWithQC (second receipt): %v", err)
		}

		// (10*100 + 10*200) / 20 = 150 rupees = 15000 paise.
		blended, _, err := GetItemUnitCost(tenantID, sku)
		if err != nil {
			t.Fatalf("GetItemUnitCost: %v", err)
		}
		if blended != 15000 {
			t.Fatalf("expected blended average 15000 paise (Rs 150), got %d", blended)
		}
	})

	t.Run("ResolveCOGSUnitCostPaise prefers the real cost and falls back for an uncosted item", func(t *testing.T) {
		const sku, poID, grnID, location = "TEST373C-ITEM", "TEST373C-PO", "TEST373C-GRN", "TEST373C-LOC"
		const neverReceivedSku = "TEST373C-NEVER-RECEIVED"
		ids := []string{sku, poID, grnID, neverReceivedSku}
		cleanupIDs(ids...)
		defer cleanupIDs(ids...)

		insert(sku, "Item", map[string]interface{}{"code": sku, "name": "Test COGS Item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
		insert(poID, "PurchaseOrder", map[string]interface{}{"code": poID, "gst_mode": GSTModeExclusive, "items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 5, "rate": 80.0})})
		insert(grnID, "GRN", map[string]interface{}{"code": grnID, "po_id": poID, "location": location, "received_items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 5, "accepted_qty": 5})})
		items := []interface{}{map[string]interface{}{"sku": sku, "qty": 5.0, "accepted_qty": 5.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, location, items, "system", grnID); err != nil {
			t.Fatalf("PostGRNReceiptWithQC: %v", err)
		}

		realCostPaise, _, _ := GetItemUnitCost(tenantID, sku)
		if got := ResolveCOGSUnitCostPaise(tenantID, sku, 999); got != realCostPaise {
			t.Fatalf("expected the real cost %d to win over the fallback, got %d", realCostPaise, got)
		}
		if got := ResolveCOGSUnitCostPaise(tenantID, neverReceivedSku, 42); got != RupeesToPaise(42) {
			t.Fatalf("expected an uncosted item to fall back to the caller's own figure, got %d", got)
		}
	})

	t.Run("a landed cost voucher tops up the average, posts Dr 1200 / Cr 2110 once, and the valuation report reflects it", func(t *testing.T) {
		const sku, poID, grnID, location = "TEST373D-ITEM", "TEST373D-PO", "TEST373D-GRN", "TEST373D-LOC"
		ids := []string{sku, poID, grnID}
		cleanupIDs(ids...)
		defer cleanupIDs(ids...)

		insert(sku, "Item", map[string]interface{}{"code": sku, "name": "Test Landed Cost Item", "hsn_code": "1234", "gst_rate": 18.0, "tax_treatment": "Taxable"})
		insert(poID, "PurchaseOrder", map[string]interface{}{"code": poID, "gst_mode": GSTModeExclusive, "items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0})})
		insert(grnID, "GRN", map[string]interface{}{"code": grnID, "po_id": poID, "location": location, "received_items": itemsJSON(map[string]interface{}{"sku": sku, "qty": 10, "accepted_qty": 10})})
		items := []interface{}{map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 10.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, location, items, "system", grnID); err != nil {
			t.Fatalf("PostGRNReceiptWithQC: %v", err)
		}

		voucherID, err := CreateLandedCostVoucher(tenantID, grnID, []map[string]interface{}{
			{"charge_type": "Freight", "amount": 200.0},
		}, "system")
		if err != nil {
			t.Fatalf("CreateLandedCostVoucher: %v", err)
		}
		if err := ApplyLandedCostVoucher(tenantID, voucherID, "system"); err != nil {
			t.Fatalf("ApplyLandedCostVoucher: %v", err)
		}

		// (1000 + 200) / 10 = 120 rupees = 12000 paise.
		got, _, err := GetItemUnitCost(tenantID, sku)
		if err != nil {
			t.Fatalf("GetItemUnitCost: %v", err)
		}
		if got != 12000 {
			t.Fatalf("expected 12000 paise (Rs 120) after the Rs 200 landed cost top-up, got %d", got)
		}

		var debit, credit int
		if err := db.DB.QueryRow("SELECT debit, credit FROM "+schema+".gl_postings WHERE document_type='LandedCostVoucher' AND document_id=$1 AND account_code='1200'", voucherID).Scan(&debit, &credit); err != nil {
			t.Fatalf("query 1200 posting: %v", err)
		}
		if debit != 20000 {
			t.Fatalf("expected 1200 Dr 20000 paise (Rs 200), got debit=%d", debit)
		}
		if err := db.DB.QueryRow("SELECT debit, credit FROM "+schema+".gl_postings WHERE document_type='LandedCostVoucher' AND document_id=$1 AND account_code='2110'", voucherID).Scan(&debit, &credit); err != nil {
			t.Fatalf("query 2110 posting: %v", err)
		}
		if credit != 20000 {
			t.Fatalf("expected 2110 Cr 20000 paise, got credit=%d", credit)
		}

		if err := ApplyLandedCostVoucher(tenantID, voucherID, "system"); err == nil {
			t.Fatalf("expected re-applying an already-Applied voucher to be refused")
		}

		rows, err := GetInventoryValuation(tenantID)
		if err != nil {
			t.Fatalf("GetInventoryValuation: %v", err)
		}
		found := false
		for _, r := range rows {
			if r["sku"] != sku {
				continue
			}
			found = true
			if costed, _ := r["costed"].(bool); !costed {
				t.Fatalf("expected %s to be marked costed, got %+v", sku, r)
			}
			if qty, _ := r["qty_on_hand"].(int64); qty != 10 {
				t.Fatalf("expected qty_on_hand=10, got %v", r["qty_on_hand"])
			}
			if val, _ := r["total_value"].(float64); val != 1200 {
				t.Fatalf("expected total_value=1200 (10 units x Rs 120), got %v", val)
			}
		}
		if !found {
			t.Fatalf("expected GetInventoryValuation to include %s, got %+v", sku, rows)
		}
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + voucherID + "'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'LandedCostVoucher' AND document_id = '" + voucherID + "'")
	})

	t.Run("CreateLandedCostVoucher and the generic-API choke point both validate the GRN reference", func(t *testing.T) {
		const grnID = "TEST373E-GRN"
		cleanupIDs(grnID)
		defer cleanupIDs(grnID)
		insert(grnID, "GRN", map[string]interface{}{"code": grnID, "po_id": "", "location": "TEST373E-LOC", "received_items": "[]"})

		if _, err := CreateLandedCostVoucher(tenantID, "", []map[string]interface{}{{"charge_type": "Freight", "amount": 100.0}}, "system"); err == nil {
			t.Fatalf("expected a blank grn_reference to be rejected")
		}
		if _, err := CreateLandedCostVoucher(tenantID, "TEST373E-NO-SUCH-GRN", []map[string]interface{}{{"charge_type": "Freight", "amount": 100.0}}, "system"); err == nil {
			t.Fatalf("expected an unregistered GRN to be rejected")
		}
		if _, err := CreateLandedCostVoucher(tenantID, grnID, nil, "system"); err == nil {
			t.Fatalf("expected zero charge lines to be rejected")
		}
		if _, err := CreateLandedCostVoucher(tenantID, grnID, []map[string]interface{}{{"charge_type": "Freight", "amount": 0.0}}, "system"); err == nil {
			t.Fatalf("expected a non-positive charge amount to be rejected")
		}

		if err := ValidateLandedCostVoucherDocument(tenantID, map[string]interface{}{"grn_reference": "TEST373E-NO-SUCH-GRN"}); err == nil {
			t.Fatalf("expected the generic-API choke point to reject an unregistered GRN too")
		}
		if err := ValidateLandedCostVoucherDocument(tenantID, map[string]interface{}{"grn_reference": grnID}); err != nil {
			t.Fatalf("expected a real GRN reference to be accepted, got %v", err)
		}
	})
}
