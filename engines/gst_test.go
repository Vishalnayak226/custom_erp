package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestGSTEnforcement(t *testing.T) {
	db.InitDB(testConnStr())
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

// TestExemptGoodsCanBeSold covers Stage 26.6.11 end to end at the engine
// level: an item declared Exempt/Nil-Rated/Zero-Rated resolves, prices, and
// books without ever being taxed - and, critically, its turnover does NOT
// land in the taxable bucket, which is the figure GSTR-3B 3.1(a) is filed
// from.
func TestExemptGoodsCanBeSold(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		skuTaxable   = "TEST-TT-TAXABLE"
		skuExempt    = "TEST-TT-EXEMPT"
		skuNilRated  = "TEST-TT-NIL"
		skuZeroRated = "TEST-TT-ZERO"
		skuBadValue  = "TEST-TT-BADVALUE"
		cartID       = "TEST-TT-CART"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + skuTaxable + "', '" + skuExempt + "', '" + skuNilRated + "', '" + skuZeroRated + "', '" + skuBadValue + "')")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'POSCart' AND document_id = '" + cartID + "'")
	}
	cleanup()
	defer cleanup()

	insertItem := func(id string, data map[string]interface{}) {
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", id, bytes); err != nil {
			t.Fatalf("insert item %s: %v", id, err)
		}
	}
	insertItem(skuTaxable, map[string]interface{}{"name": "Taxable Tee", "hsn_code": "6109", "gst_rate": 18.0})
	insertItem(skuExempt, map[string]interface{}{"name": "Loose Rice", "hsn_code": "1006", "tax_treatment": "Exempt", "gst_rate": 0})
	insertItem(skuNilRated, map[string]interface{}{"name": "Table Salt", "hsn_code": "2501", "tax_treatment": "Nil-Rated"})
	insertItem(skuZeroRated, map[string]interface{}{"name": "Export Tee", "hsn_code": "6109", "tax_treatment": "Zero-Rated", "gst_rate": 0})
	insertItem(skuBadValue, map[string]interface{}{"name": "Typo Item", "hsn_code": "6109", "tax_treatment": "Exmept", "gst_rate": 0})

	t.Run("a non-taxable item resolves at 0% instead of being rejected", func(t *testing.T) {
		for sku, want := range map[string]string{
			skuExempt:    TaxTreatmentExempt,
			skuNilRated:  TaxTreatmentNilRated,
			skuZeroRated: TaxTreatmentZeroRated,
		} {
			info, err := GetItemTaxInfo(tenantID, sku)
			if err != nil {
				t.Fatalf("%s must resolve, got: %v", sku, err)
			}
			if info.Treatment != want || info.Taxable() || info.GSTRate != 0 {
				t.Fatalf("%s resolved wrong: %+v", sku, info)
			}
			if info.HSNCode == "" {
				t.Fatalf("%s lost its HSN - it is still required on every treatment", sku)
			}
		}
	})

	t.Run("an unrecognized treatment is rejected, not silently taxed", func(t *testing.T) {
		if _, err := GetItemTaxInfo(tenantID, skuBadValue); err == nil {
			t.Fatalf("expected a typo'd tax_treatment to be rejected at transaction time")
		}
	})

	t.Run("a taxable item is unaffected", func(t *testing.T) {
		info, err := GetItemTaxInfo(tenantID, skuTaxable)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !info.Taxable() || info.GSTRate != 18.0 || info.Treatment != TaxTreatmentTaxable {
			t.Fatalf("an item with no tax_treatment must still read as Taxable at its stated rate: %+v", info)
		}
	})

	t.Run("a wholly exempt cart charges no tax and reports no taxable value", func(t *testing.T) {
		lines := []GSTLineInput{{Sku: skuExempt, Qty: 3, UnitRate: 50}}
		result, err := ComputeGSTForLines(tenantID, lines, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TotalTax != 0 || result.CGST != 0 || result.SGST != 0 || result.IGST != 0 {
			t.Fatalf("exempt goods must attract no tax: %+v", result)
		}
		if result.TaxableAmount != 0 {
			t.Fatalf("exempt turnover must not land in TaxableAmount (GSTR-3B 3.1(a)): %+v", result)
		}
		if result.ExemptAmount != 150 || result.TotalAmount != 150 {
			t.Fatalf("expected 150 exempt / 150 total, got %+v", result)
		}
		if result.GSTRate != 0 {
			t.Fatalf("a wholly exempt cart's effective rate is 0, got %v", result.GSTRate)
		}
	})

	t.Run("a mixed cart keeps each treatment in its own bucket", func(t *testing.T) {
		// 1 x 118 taxable at 18% => taxable 100, tax 18. Then 100 exempt,
		// 20 nil-rated and 30 zero-rated alongside it.
		lines := []GSTLineInput{
			{Sku: skuTaxable, Qty: 1, UnitRate: 118},
			{Sku: skuExempt, Qty: 2, UnitRate: 50},
			{Sku: skuNilRated, Qty: 1, UnitRate: 20},
			{Sku: skuZeroRated, Qty: 1, UnitRate: 30},
		}
		result, err := ComputeGSTForLines(tenantID, lines, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.TaxableAmount < 99.9 || result.TaxableAmount > 100.1 {
			t.Fatalf("expected taxable ~100 (the taxable line only), got %v", result.TaxableAmount)
		}
		if result.TotalTax < 17.9 || result.TotalTax > 18.1 {
			t.Fatalf("expected tax ~18, got %v", result.TotalTax)
		}
		if result.ExemptAmount != 100 || result.NilRatedAmount != 20 || result.ZeroRatedAmount != 30 {
			t.Fatalf("buckets wrong: exempt=%v nil=%v zero=%v", result.ExemptAmount, result.NilRatedAmount, result.ZeroRatedAmount)
		}
		if result.NonTaxableAmount() != 150 {
			t.Fatalf("expected 150 non-taxable, got %v", result.NonTaxableAmount())
		}
		// The customer pays for everything: 118 + 100 + 20 + 30.
		if result.TotalAmount < 267.9 || result.TotalAmount > 268.1 {
			t.Fatalf("expected total ~268, got %v", result.TotalAmount)
		}
		// The blended rate is the rate the taxable half actually bore (18%),
		// not one diluted by goods that were never in scope.
		if result.GSTRate < 17.9 || result.GSTRate > 18.1 {
			t.Fatalf("expected blended rate ~18, got %v", result.GSTRate)
		}
	})

	t.Run("PostExemptSalesReclass moves non-taxable turnover out of 4100", func(t *testing.T) {
		breakdown := GSTBreakdown{ExemptAmount: 100, NilRatedAmount: 20, ZeroRatedAmount: 30}
		if err := PostExemptSalesReclass(tenantID, cartID, breakdown); err != nil {
			t.Fatalf("PostExemptSalesReclass: %v", err)
		}
		balance := func(account string) int {
			var v int
			if err := db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) - COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_id=$1 AND account_code=$2", cartID, account).Scan(&v); err != nil {
				t.Fatalf("query %s: %v", account, err)
			}
			return v
		}
		if got := balance("4110"); got != 100 {
			t.Fatalf("expected 100 credited to Exempt Sales (4110), got %d", got)
		}
		if got := balance("4111"); got != 20 {
			t.Fatalf("expected 20 credited to Nil-Rated Sales (4111), got %d", got)
		}
		if got := balance("4112"); got != 30 {
			t.Fatalf("expected 30 credited to Zero-Rated Sales (4112), got %d", got)
		}
		// The whole point: 4100 is left holding only taxable revenue, which is
		// what GetGSTReturnSummary reports as the period's taxable value.
		if got := balance("4100"); got != -150 {
			t.Fatalf("expected 150 moved out of Sales Revenue (4100), got net %d", got)
		}
	})

	// The save path a real Item write runs through is ValidateDocument (generic
	// metadata: mandatory fields, Select options) and THEN
	// ValidateMasterDataRules (business rules) - handlers_core_doc_engine.go
	// calls them in that order. This exercises the same pair against the real
	// migrated doctype_fields rows, which is what proves the migration's
	// Select options and the Go-side treatment vocabulary actually agree.
	t.Run("the real save path accepts an Exempt item and rejects a typo'd one", func(t *testing.T) {
		save := func(payload map[string]interface{}) error {
			if err := ValidateDocument(tenantID, "Item", payload); err != nil {
				return err
			}
			return ValidateMasterDataRules(tenantID, "TEST-TT-SAVEPATH", "Item", payload)
		}
		exempt := map[string]interface{}{
			"code": "TEST-TT-SAVEPATH", "name": "Save Path Rice", "barcode": "TESTTT9001",
			"hsn_code": "1006", "tax_treatment": TaxTreatmentExempt, "gst_rate": 0,
		}
		if err := save(exempt); err != nil {
			t.Fatalf("an Exempt item at 0%% must save through the real path: %v", err)
		}

		// The generic Select check is what catches this one - before any
		// business rule runs - because "Exmept" is not in doctype_fields.options.
		typo := map[string]interface{}{
			"code": "TEST-TT-SAVEPATH", "name": "Save Path Typo", "barcode": "TESTTT9002",
			"hsn_code": "1006", "tax_treatment": "Exmept", "gst_rate": 0,
		}
		if err := save(typo); err == nil {
			t.Fatalf("a tax_treatment outside the Select options must be rejected")
		}
	})

	t.Run("a wholly taxable cart posts no reclass at all", func(t *testing.T) {
		if err := PostExemptSalesReclass(tenantID, "TEST-TT-NOOP", GSTBreakdown{TaxableAmount: 100, TotalTax: 18}); err != nil {
			t.Fatalf("expected a silent no-op, got: %v", err)
		}
		var rows int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings WHERE document_id='TEST-TT-NOOP'").Scan(&rows); err != nil {
			t.Fatalf("count: %v", err)
		}
		if rows != 0 {
			t.Fatalf("expected no postings for an all-taxable cart, got %d", rows)
		}
	})
}
