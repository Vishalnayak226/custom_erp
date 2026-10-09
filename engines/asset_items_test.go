package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// TestFixedAssetItem is Stage 57.8: a Fixed Asset item bought on a PO is
// received into the Fixed Assets module as Draft Assets with no stock, and
// can never be sold, ordered, invoiced or picked.
func TestFixedAssetItem(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		assetSKU = "TEST578-LAPTOP"
		stockSKU = "TEST578-STOCK"
		vendor   = "TEST578-VEND"
		poID     = "TEST578-PO"
		grnID    = "TEST578-GRN"
		location = "TEST578-LOC"
	)
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_type = 'Asset' AND document_id IN (SELECT id FROM "+schema+".documents WHERE doctype = 'Asset' AND data->>'source_grn' = $1)", grnID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Asset' AND data->>'source_grn' = $1", grnID)
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", grnID)
		for _, id := range []string{assetSKU, stockSKU, vendor, poID, grnID} {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		for _, sku := range []string{assetSKU, stockSKU} {
			db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", sku)
			db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1", sku)
		}
	}
	cleanup()
	defer cleanup()

	insert(assetSKU, "Item", "Active", map[string]interface{}{"code": assetSKU, "name": "Office Laptop", "category": "IT Equipment", "hsn_code": "8471", "gst_rate": 18.0, "tax_treatment": "Taxable", "item_type": ItemTypeFixedAsset})
	insert(stockSKU, "Item", "Active", map[string]interface{}{"code": stockSKU, "name": "Ordinary Stock", "hsn_code": "8471", "gst_rate": 18.0, "tax_treatment": "Taxable", "sale_price": 100.0})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "Laptop Vendor"})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": assetSKU, "qty": 2, "rate": 50000.0}})
	insert(poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "vendor": vendor, "gst_mode": GSTModeExclusive, "items": string(items)})
	received, _ := json.Marshal([]map[string]interface{}{{"sku": assetSKU, "qty": 2, "accepted_qty": 2}})
	insert(grnID, "GRN", "Active", map[string]interface{}{"code": grnID, "po_id": poID, "location": location, "received_items": string(received)})

	if _, err := PostGRNReceiptWithQC(tenantID, location, []interface{}{
		map[string]interface{}{"sku": assetSKU, "qty": 2.0, "accepted_qty": 2.0, "serial_numbers": []interface{}{"SN-A1", "SN-A2"}},
	}, "system", grnID); err != nil {
		t.Fatalf("PostGRNReceiptWithQC: %v", err)
	}

	t.Run("the receipt raised Draft assets and no stock", func(t *testing.T) {
		var available int
		_ = db.DB.QueryRow("SELECT COALESCE(SUM(available), 0) FROM "+schema+".inventory_availability WHERE sku = $1", assetSKU).Scan(&available)
		if available != 0 {
			t.Errorf("a fixed asset must not become stock, available = %d", available)
		}
		if ats, err := ComputeSellableSKUATS(tenantID, assetSKU, location); err == nil && ats != 0 {
			t.Errorf("available-to-sell must be 0, got %d", ats)
		}
		var glLines int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = '1200'", grnID).Scan(&glLines)
		if glLines != 0 {
			t.Errorf("a fixed asset must not be booked to 1200 Inventory at receipt, found %d line(s)", glLines)
		}
		rows, err := db.DB.Query("SELECT id, status, data FROM "+schema+".documents WHERE doctype = 'Asset' AND data->>'source_grn' = $1 ORDER BY id", grnID)
		if err != nil {
			t.Fatalf("read assets: %v", err)
		}
		defer rows.Close()
		serials := map[string]bool{}
		n := 0
		for rows.Next() {
			var id, status, raw string
			if err := rows.Scan(&id, &status, &raw); err != nil {
				t.Fatal(err)
			}
			var data map[string]interface{}
			_ = json.Unmarshal([]byte(raw), &data)
			n++
			if status != "Draft" || data["item_code"] != assetSKU || data["source_po"] != poID || data["vendor"] != vendor ||
				data["location"] != location || numFromInterface(data["cost"]) != 50000 || data["category"] != "IT Equipment" {
				t.Errorf("asset %s wrong: status=%s %+v", id, status, data)
			}
			serials[strField(data, "serial_number")] = true
		}
		if n != 2 || !serials["SN-A1"] || !serials["SN-A2"] {
			t.Errorf("want 2 Draft assets carrying SN-A1 and SN-A2, got %d %v", n, serials)
		}
	})

	t.Run("it capitalises with the useful life given at Capitalise", func(t *testing.T) {
		var assetID string
		if err := db.DB.QueryRow("SELECT id FROM "+schema+".documents WHERE doctype = 'Asset' AND data->>'source_grn' = $1 ORDER BY id LIMIT 1", grnID).Scan(&assetID); err != nil {
			t.Fatal(err)
		}
		if err := CapitalizeAsset(tenantID, assetID); err == nil {
			t.Fatal("capitalising with no useful life should be refused (ASSET-0271)")
		}
		if err := CapitalizeAssetWithLife(tenantID, assetID, 3); err != nil {
			t.Fatalf("CapitalizeAssetWithLife: %v", err)
		}
		var debit int64
		if err := db.DB.QueryRow("SELECT debit FROM "+schema+".gl_postings WHERE document_type = 'Asset' AND document_id = $1 AND account_code = '1400'", assetID).Scan(&debit); err != nil || debit != 5000000 {
			t.Errorf("want Dr 1400 5000000 paise, got %d (%v)", debit, err)
		}
	})

	isAsset0273 := func(err error) bool {
		var verr *ValidationError
		return errors.As(err, &verr) && verr.Code == "ASSET-0273"
	}

	t.Run("it cannot be sold at the POS", func(t *testing.T) {
		_, err := ResolvePOSQuote(tenantID, QuoteRequest{Location: location, Lines: []QuoteLineRequest{{Sku: assetSKU, Qty: 1, FallbackUnitPrice: 100}}})
		if !isAsset0273(err) {
			t.Errorf("POS quote: want ASSET-0273, got %v", err)
		}
	})

	t.Run("it cannot be invoiced", func(t *testing.T) {
		_, err := ComputeGSTForLines(tenantID, []GSTLineInput{{Sku: assetSKU, Qty: 1, UnitRate: 100}}, false)
		if !isAsset0273(err) {
			t.Errorf("sale-side GST (order/pack invoice, returns): want ASSET-0273, got %v", err)
		}
		if _, err := ComputeGSTForLines(tenantID, []GSTLineInput{{Sku: stockSKU, Qty: 1, UnitRate: 100}}, false); err != nil {
			t.Errorf("an ordinary stock item must still be sellable: %v", err)
		}
	})

	t.Run("it cannot be ordered, so it can never be picked", func(t *testing.T) {
		_, err := CreateSalesOrder(tenantID, SalesOrderInput{
			Channel: "Manual", ChannelOrderID: "TEST578-ORDER", CustomerName: "Test", ShippingAddress: "1 Road, 560001",
			PaymentStatus: "Confirmed", Lines: []SalesOrderLineInput{{SKU: assetSKU, Qty: 1, UnitPrice: 100}},
		})
		if !isAsset0273(err) {
			t.Errorf("OMS order: want ASSET-0273, got %v", err)
		}
		var orders int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = 'TEST578-ORDER'").Scan(&orders)
		if orders != 0 {
			t.Errorf("no order may be created for a fixed asset, found %d", orders)
		}
	})
}
