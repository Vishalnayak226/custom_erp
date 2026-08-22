package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"github.com/lib/pq"
	"testing"
)

func TestStage357BundlesKitsAndVirtualSKUs(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='ProductBundle')`).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Skip("db/migrations_stage35_7_bundles_kits.sql has not been applied")
	}
	const componentA = "TEST-BUNDLE-A-357"
	const componentB = "TEST-BUNDLE-B-357"
	const virtualSKU = "TEST-BUNDLE-VIRTUAL-357"
	const stockedSKU = "TEST-BUNDLE-STOCKED-357"
	const fixedSKU = "TEST-BUNDLE-FIXED-357"
	const componentPriceSKU = "TEST-BUNDLE-PRICE-357"
	const orderLocation = "TEST-BUNDLE-ORDER-LOC-357"
	const kitLocation = "TEST-BUNDLE-KIT-LOC-357"
	const orderRef = "TEST-BUNDLE-ORDER-357"
	allSKUs := []string{componentA, componentB, virtualSKU, stockedSKU, fixedSKU, componentPriceSKU}

	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.inventory_reservation WHERE sku=ANY($1)`, pq.Array(allSKUs))
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.inventory_availability WHERE sku=ANY($1) AND location_code IN ($2,$3)`, pq.Array(allSKUs), orderLocation, kitLocation)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE (doctype='SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM `+schema+`.documents WHERE doctype='SalesOrder' AND data->>'channel_order_id'=$1)) OR (doctype='SalesOrder' AND data->>'channel_order_id'=$1) OR (doctype='ProductBundle' AND data->>'bundle_sku'=ANY($2)) OR (doctype='BundleAssembly' AND data->>'bundle_sku'=ANY($2)) OR (doctype='StockLedgerEntry' AND data->>'item_id'=ANY($2)) OR id=ANY($2)`, orderRef, pq.Array(allSKUs))
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, sku := range allSKUs {
		data, _ := json.Marshal(map[string]interface{}{"code": sku, "name": sku, "status": "Active"})
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents(id,doctype,data,status,created_by) VALUES($1,'Item',$2,'Active','system')`, sku, data); err != nil {
			t.Fatal(err)
		}
	}
	virtualComponents := []map[string]interface{}{{"sku": componentA, "quantity": 2}, {"sku": componentB, "quantity": 1}}
	virtualPayload := map[string]interface{}{"bundle_sku": virtualSKU, "name": "Virtual test bundle", "fulfillment_mode": "Virtual", "pricing_mode": "Parent Price", "components": virtualComponents, "status": "Active"}
	if err := validateProductBundleMasterRules("default", "BUNDLE-VIRTUAL-357", virtualPayload); err != nil {
		t.Fatal(err)
	}
	insertBundle := func(id string, payload map[string]interface{}) {
		t.Helper()
		payload["code"] = id
		data, _ := json.Marshal(payload)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents(id,doctype,data,status,created_by) VALUES($1,'ProductBundle',$2,'Active','system')`, id, data); err != nil {
			t.Fatal(err)
		}
	}
	insertBundle("BUNDLE-VIRTUAL-357", virtualPayload)
	stockedPayload := map[string]interface{}{"bundle_sku": stockedSKU, "name": "Stocked test kit", "fulfillment_mode": "Stocked", "pricing_mode": "Component Price", "components": []map[string]interface{}{{"sku": componentA, "quantity": 2, "unit_price": 80}, {"sku": componentB, "quantity": 1, "unit_price": 40}}, "status": "Active"}
	if err := validateProductBundleMasterRules("default", "BUNDLE-STOCKED-357", stockedPayload); err != nil {
		t.Fatal(err)
	}
	insertBundle("BUNDLE-STOCKED-357", stockedPayload)
	fixedPayload := map[string]interface{}{"bundle_sku": fixedSKU, "name": "Fixed-price bundle", "fulfillment_mode": "Virtual", "pricing_mode": "Fixed Price", "fixed_price": 250, "components": virtualComponents, "status": "Active"}
	if err := validateProductBundleMasterRules("default", "BUNDLE-FIXED-357", fixedPayload); err != nil {
		t.Fatal(err)
	}
	insertBundle("BUNDLE-FIXED-357", fixedPayload)
	componentPricePayload := map[string]interface{}{"bundle_sku": componentPriceSKU, "name": "Component-price bundle", "fulfillment_mode": "Virtual", "pricing_mode": "Component Price", "components": []map[string]interface{}{{"sku": componentA, "quantity": 2, "unit_price": 80}, {"sku": componentB, "quantity": 1, "unit_price": 40}}, "status": "Active"}
	if err := validateProductBundleMasterRules("default", "BUNDLE-PRICE-357", componentPricePayload); err != nil {
		t.Fatal(err)
	}
	insertBundle("BUNDLE-PRICE-357", componentPricePayload)
	if err := validateProductBundleMasterRules("default", "BUNDLE-NESTED-357", map[string]interface{}{"bundle_sku": stockedSKU, "fulfillment_mode": "Virtual", "pricing_mode": "Parent Price", "components": []map[string]interface{}{{"sku": virtualSKU, "quantity": 1}}, "status": "Active"}); err == nil {
		t.Fatal("expected nested bundle validation to fail")
	}
	priceTotal := func(lines []SalesOrderLineInput) float64 {
		t.Helper()
		total := 0.0
		for _, line := range lines {
			total += line.UnitPrice * float64(line.Qty)
		}
		return total
	}
	fixedLines, _, err := ExpandSalesOrderBundles("default", []SalesOrderLineInput{{SKU: fixedSKU, Qty: 2, UnitPrice: 999}})
	if err != nil || fmt.Sprintf("%.2f", priceTotal(fixedLines)) != "500.00" {
		t.Fatalf("fixed-price expansion=%#v err=%v", fixedLines, err)
	}
	componentPriceLines, _, err := ExpandSalesOrderBundles("default", []SalesOrderLineInput{{SKU: componentPriceSKU, Qty: 2, UnitPrice: 999}})
	if err != nil || fmt.Sprintf("%.2f", priceTotal(componentPriceLines)) != "400.00" {
		t.Fatalf("component-price expansion=%#v err=%v", componentPriceLines, err)
	}
	stockedLines, _, err := ExpandSalesOrderBundles("default", []SalesOrderLineInput{{SKU: stockedSKU, Qty: 2, UnitPrice: 210}})
	if err != nil || len(stockedLines) != 1 || stockedLines[0].SKU != stockedSKU {
		t.Fatalf("stocked kit should not explode: %#v err=%v", stockedLines, err)
	}

	seedAvailability := func(sku, location string, available, reserved, safety, blocked int) {
		t.Helper()
		_, err := db.DB.Exec(`INSERT INTO `+schema+`.inventory_availability(sku,location_code,on_hand,available,reserved,safety_stock,blocked,qc_hold,damaged,channel_buffer,hold_qty) VALUES($1,$2,$3,$3,$4,$5,$6,0,0,0,0) ON CONFLICT(sku,location_code) DO UPDATE SET on_hand=EXCLUDED.on_hand,available=EXCLUDED.available,reserved=EXCLUDED.reserved,safety_stock=EXCLUDED.safety_stock,blocked=EXCLUDED.blocked,qc_hold=0,damaged=0,channel_buffer=0,hold_qty=0`, sku, location, available, reserved, safety, blocked)
		if err != nil {
			t.Fatal(err)
		}
	}
	seedAvailability(componentA, orderLocation, 10, 2, 1, 0) // ATS 7 / 2 = 3 bundles
	seedAvailability(componentB, orderLocation, 5, 0, 0, 1)  // ATS 4 / 1 = 4 bundles
	ats, err := ComputeSellableSKUATS("default", virtualSKU, orderLocation)
	if err != nil || ats != 3 {
		t.Fatalf("derived ATS=%d err=%v, want 3", ats, err)
	}
	networkATS, err := ComputeSellableSKUATS("default", virtualSKU, "")
	if err != nil || networkATS != 3 {
		t.Fatalf("network derived ATS=%d err=%v, want 3", networkATS, err)
	}
	availability, err := GetAvailableToSell("default", virtualSKU, orderLocation)
	if err != nil || int(numericFromAny(availability["ats"])) != 3 || availability["derived_from_components"] != true {
		t.Fatalf("availability=%#v err=%v", availability, err)
	}

	orderID, err := CreateSalesOrder("default", SalesOrderInput{Channel: "Stage357", ChannelOrderID: orderRef, CustomerName: "Bundle Buyer", ShippingAddress: "1 Bundle Road 560001", PaymentStatus: "Confirmed", PreferredLocation: orderLocation, Lines: []SalesOrderLineInput{{SKU: virtualSKU, Qty: 2, UnitPrice: 300}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.DB.Query(`SELECT data FROM `+schema+`.documents WHERE doctype='SalesOrderLine' AND data->>'order_id'=$1 ORDER BY id`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	lineCount, total, qtyA, qtyB := 0, 0.0, 0, 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var line map[string]interface{}
		_ = json.Unmarshal(raw, &line)
		if line["bundle_sku"] != virtualSKU {
			t.Fatalf("component lost bundle attribution: %#v", line)
		}
		qty := int(numericFromAny(line["qty"]))
		total += numericFromAny(line["unit_price"]) * float64(qty)
		if line["sku"] == componentA {
			qtyA = qty
		}
		if line["sku"] == componentB {
			qtyB = qty
		}
		lineCount++
	}
	rows.Close()
	if lineCount != 2 || qtyA != 4 || qtyB != 2 || fmt.Sprintf("%.2f", total) != "600.00" {
		t.Fatalf("exploded lines count=%d A=%d B=%d total=%.2f", lineCount, qtyA, qtyB, total)
	}

	seedAvailability(componentA, kitLocation, 10, 0, 0, 0)
	seedAvailability(componentB, kitLocation, 10, 0, 0, 0)
	assemblyID, err := PostBundleAssembly("default", stockedSKU, kitLocation, 2, "Assemble", "manager1", "REQ-ASSEMBLE-357")
	if err != nil {
		t.Fatal(err)
	}
	replayID, err := PostBundleAssembly("default", stockedSKU, kitLocation, 2, "Assemble", "manager1", "REQ-ASSEMBLE-357")
	if err != nil || replayID != assemblyID {
		t.Fatalf("idempotent replay id=%s err=%v", replayID, err)
	}
	checkAvailable := func(sku string, want int) {
		t.Helper()
		var got int
		if err := db.DB.QueryRow(`SELECT available FROM `+schema+`.inventory_availability WHERE sku=$1 AND location_code=$2`, sku, kitLocation).Scan(&got); err != nil || got != want {
			t.Fatalf("%s available=%d err=%v want=%d", sku, got, err, want)
		}
	}
	checkAvailable(componentA, 6)
	checkAvailable(componentB, 8)
	checkAvailable(stockedSKU, 2)
	if _, err := PostBundleAssembly("default", stockedSKU, kitLocation, 1, "Disassemble", "manager1", "REQ-DISASSEMBLE-357"); err != nil {
		t.Fatal(err)
	}
	checkAvailable(componentA, 8)
	checkAvailable(componentB, 9)
	checkAvailable(stockedSKU, 1)
	if _, err := PostBundleAssembly("default", stockedSKU, kitLocation, 10, "Assemble", "manager1", "REQ-SHORT-357"); err == nil {
		t.Fatal("expected an oversized assembly to fail atomically")
	}
	checkAvailable(componentA, 8)
	checkAvailable(componentB, 9)
	checkAvailable(stockedSKU, 1)
	var failedOperationExists bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.documents WHERE doctype='BundleAssembly' AND data->>'request_key'='REQ-SHORT-357')`).Scan(&failedOperationExists); err != nil || failedOperationExists {
		t.Fatalf("failed atomic operation persisted=%v err=%v", failedOperationExists, err)
	}
}
