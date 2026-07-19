package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestGSTEnforcement(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const skuComplete = "TEST-GST-COMPLETE"
	const skuMissingHSN = "TEST-GST-NO-HSN"
	const skuMissingRate = "TEST-GST-NO-RATE"
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + skuComplete + "', '" + skuMissingHSN + "', '" + skuMissingRate + "')")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'POSCart' AND document_id = 'TEST-GST-CART'")
	}
	cleanup()
	defer cleanup()

	insertItem := func(id string, data map[string]interface{}) {
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, bytes); err != nil {
			t.Fatalf("insert item %s: %v", id, err)
		}
	}
	insertItem(skuComplete, map[string]interface{}{"name": "Complete Item", "hsn_code": "6109", "gst_rate": 18.0})
	insertItem(skuMissingHSN, map[string]interface{}{"name": "No HSN Item", "gst_rate": 18.0})
	insertItem(skuMissingRate, map[string]interface{}{"name": "No Rate Item", "hsn_code": "6109"})

	t.Run("GetItemGSTInfo rejects missing hsn_code", func(t *testing.T) {
		if _, _, err := GetItemGSTInfo(tenantID, skuMissingHSN); err == nil {
			t.Fatalf("expected rejection for missing hsn_code")
		}
	})

	t.Run("GetItemGSTInfo rejects missing gst_rate", func(t *testing.T) {
		if _, _, err := GetItemGSTInfo(tenantID, skuMissingRate); err == nil {
			t.Fatalf("expected rejection for missing gst_rate")
		}
	})

	t.Run("GetItemGSTInfo resolves a fully-classified item", func(t *testing.T) {
		hsn, rate, err := GetItemGSTInfo(tenantID, skuComplete)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hsn != "6109" || rate != 18.0 {
			t.Fatalf("got hsn=%s rate=%v, want 6109/18.0", hsn, rate)
		}
	})

	t.Run("CalculateGST splits intrastate as CGST+SGST, interstate as IGST", func(t *testing.T) {
		intra, err := CalculateGST(1000, 18, false)
		if err != nil {
			t.Fatalf("intra: %v", err)
		}
		if intra.CGST != 90 || intra.SGST != 90 || intra.IGST != 0 || intra.TotalTax != 180 {
			t.Fatalf("intra breakdown wrong: %+v", intra)
		}
		inter, err := CalculateGST(1000, 18, true)
		if err != nil {
			t.Fatalf("inter: %v", err)
		}
		if inter.IGST != 180 || inter.CGST != 0 || inter.SGST != 0 {
			t.Fatalf("inter breakdown wrong: %+v", inter)
		}
	})

	t.Run("ComputeGSTForLines rejects a line with an incomplete item", func(t *testing.T) {
		lines := []GSTLineInput{{Sku: skuComplete, Qty: 1, UnitRate: 118}, {Sku: skuMissingHSN, Qty: 1, UnitRate: 118}}
		if _, err := ComputeGSTForLines(tenantID, lines, false); err == nil {
			t.Fatalf("expected rejection when one line's item is missing hsn_code")
		}
	})

	t.Run("ComputeGSTForLines backs the taxable amount out of a tax-inclusive rate", func(t *testing.T) {
		// unit rate 118 at 18% GST => taxable 100, tax 18, per unit.
		lines := []GSTLineInput{{Sku: skuComplete, Qty: 2, UnitRate: 118}}
		result, err := ComputeGSTForLines(tenantID, lines, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TaxableAmount < 199.9 || result.TaxableAmount > 200.1 {
			t.Fatalf("expected taxable ~200, got %v", result.TaxableAmount)
		}
		if result.TotalTax < 35.9 || result.TotalTax > 36.1 {
			t.Fatalf("expected total_tax ~36, got %v", result.TotalTax)
		}
	})

	t.Run("ComputePurchaseOrderGST treats empty items as a no-op, not an error", func(t *testing.T) {
		result, err := ComputePurchaseOrderGST(tenantID, map[string]interface{}{"items": ""})
		if err != nil || result.TotalTax != 0 {
			t.Fatalf("expected zero-value no-op, got %+v err=%v", result, err)
		}
	})

	t.Run("ComputePurchaseOrderGST rejects an item missing HSN/rate", func(t *testing.T) {
		itemsJSON, _ := json.Marshal([]map[string]interface{}{{"sku": skuMissingRate, "qty": 5, "rate": 100}})
		payload := map[string]interface{}{"items": string(itemsJSON)}
		if _, err := ComputePurchaseOrderGST(tenantID, payload); err == nil {
			t.Fatalf("expected rejection for item missing gst_rate")
		}
	})

	t.Run("ComputePurchaseOrderGST computes a breakdown for valid items", func(t *testing.T) {
		itemsJSON, _ := json.Marshal([]map[string]interface{}{{"sku": skuComplete, "qty": 10, "rate": 118}})
		payload := map[string]interface{}{"items": string(itemsJSON), "interstate": true}
		result, err := ComputePurchaseOrderGST(tenantID, payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Interstate || result.IGST <= 0 || result.CGST != 0 {
			t.Fatalf("expected interstate IGST-only breakdown, got %+v", result)
		}
	})

	t.Run("PostSalesGSTBooking posts a balanced entry matching the breakdown", func(t *testing.T) {
		breakdown, err := CalculateGST(1000, 18, false)
		if err != nil {
			t.Fatalf("CalculateGST: %v", err)
		}
		if err := PostSalesGSTBooking(tenantID, "TEST-GST-CART", breakdown); err != nil {
			t.Fatalf("PostSalesGSTBooking: %v", err)
		}
		var cgst, sgst int
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id='TEST-GST-CART' AND account_code='2200'").Scan(&cgst); err != nil {
			t.Fatalf("query cgst: %v", err)
		}
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id='TEST-GST-CART' AND account_code='2201'").Scan(&sgst); err != nil {
			t.Fatalf("query sgst: %v", err)
		}
		if cgst != 90 || sgst != 90 {
			t.Fatalf("expected CGST=90 SGST=90, got cgst=%d sgst=%d", cgst, sgst)
		}
	})
}
