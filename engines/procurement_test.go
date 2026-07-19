package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestConvertRequisitionToOrder(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const prID = "TEST-PR-1"
	var createdOrderIDs []string
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + prID + "'")
		for _, id := range createdOrderIDs {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()

	seedPR := func(status string) {
		data := map[string]interface{}{
			"id": prID, "code": prID, "description": "50 boxes of A4 paper",
			"quantity": 50, "department": "Admin", "total_amount": 15000, "status": status,
		}
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseRequisition', $2, $3, 'system')",
			prID, bytes, status); err != nil {
			t.Fatalf("seed PR: %v", err)
		}
	}

	t.Run("rejects conversion of a non-Approved requisition", func(t *testing.T) {
		cleanup()
		seedPR("Pending Approval")
		if _, err := ConvertRequisitionToOrder(tenantID, prID, "RFQ", "HO", "TEST-FY", "system"); err == nil {
			t.Fatalf("expected rejection for non-Approved requisition")
		}
	})

	t.Run("rejects an invalid target", func(t *testing.T) {
		cleanup()
		seedPR("Approved")
		if _, err := ConvertRequisitionToOrder(tenantID, prID, "SomethingElse", "HO", "TEST-FY", "system"); err == nil {
			t.Fatalf("expected rejection for invalid target")
		}
	})

	t.Run("converts an Approved requisition to a Draft RFQ carrying its details, and blocks a second conversion", func(t *testing.T) {
		cleanup()
		seedPR("Approved")

		newID, err := ConvertRequisitionToOrder(tenantID, prID, "RFQ", "HO", "TEST-FY", "system")
		if err != nil {
			t.Fatalf("ConvertRequisitionToOrder: %v", err)
		}
		createdOrderIDs = append(createdOrderIDs, newID)

		var newDataStr, newStatus string
		if err := db.DB.QueryRow("SELECT data, status FROM "+schema+".documents WHERE id=$1 AND doctype='RFQ'", newID).Scan(&newDataStr, &newStatus); err != nil {
			t.Fatalf("new RFQ not found: %v", err)
		}
		if newStatus != "Draft" {
			t.Fatalf("expected new RFQ status Draft, got %s", newStatus)
		}
		var newData map[string]interface{}
		json.Unmarshal([]byte(newDataStr), &newData)
		if newData["description"] != "50 boxes of A4 paper" || newData["quantity"].(float64) != 50 {
			t.Fatalf("expected carried-over description/quantity, got %v", newData)
		}
		if newData["source_requisition_id"] != prID {
			t.Fatalf("expected source_requisition_id to trace back to %s, got %v", prID, newData["source_requisition_id"])
		}

		var prStatus, prDataStr string
		db.DB.QueryRow("SELECT status, data FROM "+schema+".documents WHERE id=$1", prID).Scan(&prStatus, &prDataStr)
		if prStatus != "Converted" {
			t.Fatalf("expected requisition status Converted, got %s", prStatus)
		}
		var prData map[string]interface{}
		json.Unmarshal([]byte(prDataStr), &prData)
		if prData["converted_to_id"] != newID {
			t.Fatalf("expected converted_to_id=%s, got %v", newID, prData["converted_to_id"])
		}

		if _, err := ConvertRequisitionToOrder(tenantID, prID, "RFQ", "HO", "TEST-FY", "system"); err == nil {
			t.Fatalf("expected a second conversion attempt to be rejected")
		}
	})

	t.Run("converts to a Draft PurchaseOrder carrying the estimated amount", func(t *testing.T) {
		cleanup()
		seedPR("Approved")

		newID, err := ConvertRequisitionToOrder(tenantID, prID, "PurchaseOrder", "HO", "TEST-FY", "system")
		if err != nil {
			t.Fatalf("ConvertRequisitionToOrder: %v", err)
		}
		createdOrderIDs = append(createdOrderIDs, newID)

		var newDataStr string
		if err := db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id=$1 AND doctype='PurchaseOrder'", newID).Scan(&newDataStr); err != nil {
			t.Fatalf("new PO not found: %v", err)
		}
		var newData map[string]interface{}
		json.Unmarshal([]byte(newDataStr), &newData)
		if newData["total_amount"].(float64) != 15000 {
			t.Fatalf("expected carried-over total_amount=15000, got %v", newData["total_amount"])
		}
	})
}
