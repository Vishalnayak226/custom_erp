package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 49.7.4: grantLeastPrivilegeSchemaAccessTx must be a silent no-op on
// every database that has not run deploy/postgres_harden.sql - which is
// every database this test suite runs against, since that script is an
// operator-run artifact, never applied automatically. This is what makes the
// grant step in ProvisionTenantSchema safe to ship unconditionally: an
// unhardened deployment (dev, CI, a production box that has not adopted the
// least-privilege roles yet) provisions tenants exactly as it always has.
func TestGrantLeastPrivilegeSchemaAccessIsNoOpWithoutTheHardenedRoles(t *testing.T) {
	db.InitDB(testConnStr())

	var exists bool
	if err := db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'erp_app')`).Scan(&exists); err != nil {
		t.Fatalf("check for erp_app role: %v", err)
	}
	if exists {
		t.Skip("this dev database has erp_app defined (deploy/postgres_harden.sql was run against it) - the no-op path this test covers does not apply here")
	}

	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	if err := grantLeastPrivilegeSchemaAccessTx(tx, "tenant_default"); err != nil {
		t.Fatalf("expected a no-op (nil error) when erp_app/erp_backup do not exist, got: %v", err)
	}
}
