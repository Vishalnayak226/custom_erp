package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestValidateLocationReference(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const activeLoc = "TEST-LOC-ACTIVE"
	const inactiveLoc = "TEST-LOC-INACTIVE"
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + activeLoc + "', '" + inactiveLoc + "')")
	}
	cleanup()
	defer cleanup()

	insertLoc := func(id, status string) {
		data := map[string]interface{}{"code": id, "name": id, "type": "Warehouse", "status": status}
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, $3, 'system')", id, bytes, status); err != nil {
			t.Fatalf("insert location %s: %v", id, err)
		}
	}
	insertLoc(activeLoc, "Active")
	insertLoc(inactiveLoc, "Inactive")

	t.Run("empty location is a no-op", func(t *testing.T) {
		if err := ValidateLocationReference(tenantID, ""); err != nil {
			t.Fatalf("expected no error for empty location, got %v", err)
		}
	})

	t.Run("an unknown location is rejected", func(t *testing.T) {
		if err := ValidateLocationReference(tenantID, "TEST-LOC-DOES-NOT-EXIST"); err == nil {
			t.Fatalf("expected rejection for unknown location")
		}
	})

	t.Run("an Inactive location is rejected", func(t *testing.T) {
		if err := ValidateLocationReference(tenantID, inactiveLoc); err == nil {
			t.Fatalf("expected rejection for an Inactive location")
		}
	})

	t.Run("an Active location is accepted", func(t *testing.T) {
		if err := ValidateLocationReference(tenantID, activeLoc); err != nil {
			t.Fatalf("expected an Active location to be accepted, got %v", err)
		}
	})

	t.Run("legacy seeded locations from the Stage 17.9 migration are still valid", func(t *testing.T) {
		if err := ValidateLocationReference(tenantID, "WH01"); err != nil {
			t.Fatalf("expected WH01 (seeded from legacy data) to validate, got %v", err)
		}
		if err := ValidateLocationReference(tenantID, "LOC-0001"); err != nil {
			t.Fatalf("expected LOC-0001 (seeded from inventory_availability) to validate, got %v", err)
		}
	})
}
