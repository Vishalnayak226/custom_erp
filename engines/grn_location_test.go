package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestPrepareGRNReceiptDefaultsLocation covers Stage 30.2.1. The defect: a GRN
// created anywhere other than the bespoke Workbench screen could not supply a
// receiving location at all (it wasn't a declared field), so it saved with
// HTTP 200, counted against its PO, and posted zero stock.
func TestPrepareGRNReceiptDefaultsLocation(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const (
		poWithWarehouse = "TEST-GRNLOC-PO-1"
		poLocationOnly  = "TEST-GRNLOC-PO-2"
		poNeither       = "TEST-GRNLOC-PO-3"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2, $3)", poWithWarehouse, poLocationOnly, poNeither)
	}
	cleanup()
	defer cleanup()

	insertPO := func(id string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseOrder', $2, 'Approved', 'system')", id, raw); err != nil {
			t.Fatalf("insert PO %s: %v", id, err)
		}
	}
	insertPO(poWithWarehouse, map[string]interface{}{"po_number": poWithWarehouse, "target_warehouse": "WH-MAIN", "location": "HO"})
	insertPO(poLocationOnly, map[string]interface{}{"po_number": poLocationOnly, "location": "HO"})
	insertPO(poNeither, map[string]interface{}{"po_number": poNeither})

	t.Run("defaults from the PO's target warehouse", func(t *testing.T) {
		payload := map[string]interface{}{"po_id": poWithWarehouse, "received_items": "[]"}
		if err := PrepareGRNReceipt(tenantID, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if payload["location"] != "WH-MAIN" {
			t.Fatalf("location = %v, want WH-MAIN", payload["location"])
		}
	})

	t.Run("falls back to the PO's location when it has no warehouse", func(t *testing.T) {
		payload := map[string]interface{}{"po_id": poLocationOnly}
		if err := PrepareGRNReceipt(tenantID, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if payload["location"] != "HO" {
			t.Fatalf("location = %v, want HO", payload["location"])
		}
	})

	t.Run("never overwrites a location the caller supplied", func(t *testing.T) {
		payload := map[string]interface{}{"po_id": poWithWarehouse, "location": "STORE-07"}
		if err := PrepareGRNReceipt(tenantID, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if payload["location"] != "STORE-07" {
			t.Fatalf("caller's location was overwritten: %v", payload["location"])
		}
	})

	t.Run("leaves location unset when the PO has nowhere to receive into", func(t *testing.T) {
		// Deliberately not an error here: the mandatory-field check in
		// ValidateDocument reports it, naming the field, which is a better
		// message than anything this function could invent.
		payload := map[string]interface{}{"po_id": poNeither}
		if err := PrepareGRNReceipt(tenantID, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := payload["location"]; ok && v != "" {
			t.Fatalf("expected no location, got %v", v)
		}
	})

	t.Run("an unknown PO is left to the Link check, not a bespoke error", func(t *testing.T) {
		payload := map[string]interface{}{"po_id": "TEST-GRNLOC-NO-SUCH-PO"}
		if err := PrepareGRNReceipt(tenantID, payload); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("location is now a declared mandatory GRN field", func(t *testing.T) {
		// The other half of the fix: without this row the generic form never
		// renders the field, so no non-Workbench caller can post stock.
		var mandatory bool
		err := db.DB.QueryRow("SELECT mandatory FROM " + schema + ".doctype_fields WHERE doctype_name = 'GRN' AND fieldname = 'location'").Scan(&mandatory)
		if err != nil {
			t.Fatalf("GRN.location is not declared as a field (run db/migrations_stage30_2_1_grn_location.sql): %v", err)
		}
		if !mandatory {
			t.Fatalf("GRN.location is declared but not mandatory - a receipt with no location would still post zero stock")
		}
	})
}
