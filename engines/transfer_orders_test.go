package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestTransferOrderDispatchReceiveLifecycle(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const sku = "TEST-TRANSFER-SKU"
	const fromWH = "TEST-TRANSFER-FROM"
	const toWH = "TEST-TRANSFER-TO"
	const toID = "TEST-TO-1"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-TO-%'")
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code IN ($2, $3)", sku, fromWH, toWH)
	}
	cleanup()
	defer cleanup()

	seedOrder := func(id string, status string, items []map[string]interface{}) {
		itemsJSON, _ := json.Marshal(items)
		data := map[string]interface{}{
			"id": id, "transfer_number": id, "from_warehouse": fromWH, "to_warehouse": toWH,
			"status": status, "items": string(itemsJSON),
		}
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, $3, 'system')",
			id, bytes, status); err != nil {
			t.Fatalf("seed order %s: %v", id, err)
		}
	}

	t.Run("dispatch moves source available into in_transit", func(t *testing.T) {
		cleanup()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, fromWH); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
		seedOrder(toID, "Approved", []map[string]interface{}{{"sku": sku, "qty": 30}})

		if err := DispatchTransferOrder(tenantID, toID, "system"); err != nil {
			t.Fatalf("DispatchTransferOrder: %v", err)
		}

		var available, inTransit int
		if err := db.DB.QueryRow("SELECT available, in_transit FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, fromWH).Scan(&available, &inTransit); err != nil {
			t.Fatalf("query availability: %v", err)
		}
		if available != 70 || inTransit != 30 {
			t.Fatalf("expected available=70 in_transit=30, got available=%d in_transit=%d", available, inTransit)
		}

		if err := DispatchTransferOrder(tenantID, toID, "system"); err == nil {
			t.Fatalf("expected duplicate dispatch to be rejected")
		}
	})

	t.Run("dispatch is rejected when stock is insufficient", func(t *testing.T) {
		cleanup()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)", sku, fromWH); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
		seedOrder(toID, "Approved", []map[string]interface{}{{"sku": sku, "qty": 30}})
		if err := DispatchTransferOrder(tenantID, toID, "system"); err == nil {
			t.Fatalf("expected rejection for insufficient stock")
		}
	})

	t.Run("dispatch is rejected when order is still Draft", func(t *testing.T) {
		cleanup()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, fromWH); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
		seedOrder(toID, "Draft", []map[string]interface{}{{"sku": sku, "qty": 30}})
		if err := DispatchTransferOrder(tenantID, toID, "system"); err == nil {
			t.Fatalf("expected rejection for non-Approved order")
		}
	})

	t.Run("full receive credits destination and clears in_transit, no variance", func(t *testing.T) {
		cleanup()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, fromWH); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
		seedOrder(toID, "Approved", []map[string]interface{}{{"sku": sku, "qty": 30}})
		if err := DispatchTransferOrder(tenantID, toID, "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}

		if err := ReceiveTransferOrder(tenantID, toID, "system", []interface{}{map[string]interface{}{"sku": sku, "qty": 30}}); err != nil {
			t.Fatalf("ReceiveTransferOrder: %v", err)
		}

		var srcInTransit, destAvailable int
		if err := db.DB.QueryRow("SELECT in_transit FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, fromWH).Scan(&srcInTransit); err != nil {
			t.Fatalf("query source: %v", err)
		}
		if err := db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, toWH).Scan(&destAvailable); err != nil {
			t.Fatalf("query dest: %v", err)
		}
		if srcInTransit != 0 || destAvailable != 30 {
			t.Fatalf("expected source in_transit=0 dest available=30, got in_transit=%d available=%d", srcInTransit, destAvailable)
		}

		var dataStr string
		db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id=$1", toID).Scan(&dataStr)
		var data map[string]interface{}
		json.Unmarshal([]byte(dataStr), &data)
		if _, hasVariance := data["receive_variance"]; hasVariance {
			t.Fatalf("expected no variance recorded for a full receive, got %v", data["receive_variance"])
		}

		if err := ReceiveTransferOrder(tenantID, toID, "system", []interface{}{map[string]interface{}{"sku": sku, "qty": 30}}); err == nil {
			t.Fatalf("expected duplicate receive to be rejected")
		}
	})

	t.Run("short-receive credits only what arrived and records a variance", func(t *testing.T) {
		cleanup()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, fromWH); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
		seedOrder(toID, "Approved", []map[string]interface{}{{"sku": sku, "qty": 30}})
		if err := DispatchTransferOrder(tenantID, toID, "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}

		if err := ReceiveTransferOrder(tenantID, toID, "system", []interface{}{map[string]interface{}{"sku": sku, "qty": 20}}); err != nil {
			t.Fatalf("ReceiveTransferOrder: %v", err)
		}

		var srcInTransit, destAvailable int
		db.DB.QueryRow("SELECT in_transit FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, fromWH).Scan(&srcInTransit)
		db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku=$1 AND location_code=$2", sku, toWH).Scan(&destAvailable)
		if srcInTransit != 0 || destAvailable != 20 {
			t.Fatalf("expected source in_transit=0 dest available=20, got in_transit=%d available=%d", srcInTransit, destAvailable)
		}

		var dataStr string
		db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id=$1", toID).Scan(&dataStr)
		var data map[string]interface{}
		json.Unmarshal([]byte(dataStr), &data)
		varianceStr, _ := data["receive_variance"].(string)
		if varianceStr == "" {
			t.Fatalf("expected a recorded variance for a short receive")
		}
		var variances []map[string]interface{}
		json.Unmarshal([]byte(varianceStr), &variances)
		if len(variances) != 1 || int(variances[0]["shortfall"].(float64)) != 10 {
			t.Fatalf("expected one variance line with shortfall=10, got %v", variances)
		}
	})

	t.Run("over-receive beyond dispatched qty is rejected", func(t *testing.T) {
		cleanup()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, fromWH); err != nil {
			t.Fatalf("seed availability: %v", err)
		}
		seedOrder(toID, "Approved", []map[string]interface{}{{"sku": sku, "qty": 30}})
		if err := DispatchTransferOrder(tenantID, toID, "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if err := ReceiveTransferOrder(tenantID, toID, "system", []interface{}{map[string]interface{}{"sku": sku, "qty": 999}}); err == nil {
			t.Fatalf("expected rejection for receiving more than dispatched")
		}
	})
}
