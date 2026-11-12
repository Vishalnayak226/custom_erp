package engines

import (
	"fmt"
	"testing"

	"custom_erp/db"
)

// 163.1: a new client starts with the starter reason codes the migrations seed
// into every existing client (created by 'system'), and with none of the codes
// another client made itself.
func TestNewClientGetsTheStarterReasonCodes(t *testing.T) {
	db.InitDB(testConnStr())

	// A code someone made in the template must not travel to a new client.
	const own = "TEST-163-1-OWN-CODE"
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'ReasonCode', '{"code":"TEST-163-1-OWN-CODE","category":"Hold"}', 'Active', 'admin')
		ON CONFLICT (id) DO NOTHING`, own); err != nil {
		t.Fatalf("seed own code: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, own)

	var want int
	if err := db.DB.QueryRow(`SELECT count(*) FROM tenant_default.documents WHERE doctype = 'ReasonCode' AND created_by = 'system'`).Scan(&want); err != nil {
		t.Fatalf("count template codes: %v", err)
	}
	if want == 0 {
		t.Fatal("tenant_default has no starter reason codes - the 74.1.1 migrations did not run")
	}

	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	var got, foreign int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FILTER (WHERE created_by = 'system'), count(*) FILTER (WHERE id = $1)
		FROM %s.documents WHERE doctype = 'ReasonCode'`, schemaName), own).Scan(&got, &foreign); err != nil {
		t.Fatalf("count new client's codes: %v", err)
	}
	if got != want {
		t.Errorf("new client has %d starter reason codes, want %d (all of tenant_default's)", got, want)
	}
	if foreign != 0 {
		t.Error("a code made by a user in tenant_default was copied to the new client")
	}

	// The codes are usable: one from each 74.1.1 seed is there.
	for _, id := range []string{"GRN-DMG-TRANSIT", "HOLD-CREDIT"} {
		var category string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'category' FROM %s.documents WHERE doctype = 'ReasonCode' AND id = $1`, schemaName), id).Scan(&category); err != nil || category == "" {
			t.Errorf("starter code %s missing in the new client (category %q, %v)", id, category, err)
		}
	}
}
