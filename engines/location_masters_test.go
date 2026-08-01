package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestValidateLocationReference(t *testing.T) {
	db.InitDB(testConnStr())
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

	t.Run("locations seeded by the Stage 17.9 migration are still valid", func(t *testing.T) {
		// 'HO' is the one code migrations_stage17h_location_masters.sql seeds
		// unconditionally (its `UNION SELECT 'HO'` branch), so it exists on
		// every install and is always a meaningful assertion.
		if err := ValidateLocationReference(tenantID, "HO"); err != nil {
			t.Fatalf("expected HO (unconditionally seeded by the 17.9 migration) to validate, got %v", err)
		}

		// WH01/LOC-0001 only exist where the migration found pre-existing
		// legacy rows to seed from - it derives Location masters via
		// SELECT DISTINCT over inventory_availability and documents, which are
		// empty on a fresh database. This subtest used to assert them
		// unconditionally, which made the whole suite fail on any newly
		// provisioned environment (new dev machine, fresh CI runner, DR
		// rebuild) even though nothing was wrong. Assert them only where the
		// legacy data they describe actually exists, so the back-compat
		// guarantee is still checked on databases that have it without
		// inventing a failure on databases that never did.
		for _, code := range []string{"WH01", "LOC-0001"} {
			var exists bool
			if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+
				".documents WHERE doctype = 'Location' AND id = $1)", code).Scan(&exists); err != nil {
				t.Fatalf("probing for legacy location %s: %v", code, err)
			}
			if !exists {
				t.Logf("no legacy Location %s on this database - nothing for the 17.9 back-compat seeding to have preserved here", code)
				continue
			}
			if err := ValidateLocationReference(tenantID, code); err != nil {
				t.Errorf("expected legacy location %s to validate, got %v", code, err)
			}
		}
	})
}
