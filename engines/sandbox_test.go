package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

func TestStage387SandboxTenants(t *testing.T) {
	db.InitDB(testConnStr())

	t.Run("ProvisionSandboxTenant creates a real, flagged, correctly-expiring tenant; Is* helpers agree", func(t *testing.T) {
		tenantID, schemaName, adminPassword, err := ProvisionSandboxTenant("0.1.0-test", 5)
		if err != nil {
			t.Fatalf("ProvisionSandboxTenant: %v", err)
		}
		defer func() {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE")
			db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
		}()

		if adminPassword == "" {
			t.Fatalf("expected a non-empty one-time admin password")
		}
		isSandbox, err := IsSandboxTenant(tenantID)
		if err != nil || !isSandbox {
			t.Fatalf("expected IsSandboxTenant to report true, got %v, err=%v", isSandbox, err)
		}
		isSandboxBySchema, err := IsSandboxSchema(schemaName)
		if err != nil || !isSandboxBySchema {
			t.Fatalf("expected IsSandboxSchema to report true, got %v, err=%v", isSandboxBySchema, err)
		}
		expired, err := IsSandboxExpired(tenantID)
		if err != nil || expired {
			t.Fatalf("expected a freshly provisioned 5-day sandbox to not be expired yet, got expired=%v err=%v", expired, err)
		}

		var expiresAt time.Time
		if err := db.DB.QueryRow("SELECT sandbox_expires_at FROM public.tenants WHERE tenant_id = $1", tenantID).Scan(&expiresAt); err != nil {
			t.Fatalf("reload sandbox_expires_at: %v", err)
		}
		daysOut := time.Until(expiresAt).Hours() / 24
		if daysOut < 4.9 || daysOut > 5.1 {
			t.Fatalf("expected sandbox_expires_at to land ~5 days out, got %.2f days", daysOut)
		}

		// A real, unflagged tenant must never report as a sandbox or as expired.
		notSandbox, err := IsSandboxTenant("default")
		if err != nil || notSandbox {
			t.Fatalf("expected the real default tenant to never report as a sandbox, got %v, err=%v", notSandbox, err)
		}
		notExpired, err := IsSandboxExpired("default")
		if err != nil || notExpired {
			t.Fatalf("expected the real default tenant to never report as sandbox-expired, got %v, err=%v", notExpired, err)
		}
	})

	t.Run("IsSandboxExpired reports true once the expiry has passed", func(t *testing.T) {
		tenantID, schemaName, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
		if err != nil {
			t.Fatalf("ProvisionSandboxTenant: %v", err)
		}
		defer func() {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE")
			db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
		}()
		if _, err := db.DB.Exec("UPDATE public.tenants SET sandbox_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 hour' WHERE tenant_id = $1", tenantID); err != nil {
			t.Fatalf("force expiry: %v", err)
		}
		expired, err := IsSandboxExpired(tenantID)
		if err != nil || !expired {
			t.Fatalf("expected an expiry in the past to report expired=true, got %v, err=%v", expired, err)
		}
	})

	t.Run("ResetSandboxTenant refuses a non-sandbox tenant, truncates business data and extends expiry for a real one", func(t *testing.T) {
		if err := ResetSandboxTenant("default", 0); err == nil {
			t.Fatalf("expected resetting a real, non-sandbox tenant to be refused")
		}

		tenantID, schemaName, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
		if err != nil {
			t.Fatalf("ProvisionSandboxTenant: %v", err)
		}
		defer func() {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE")
			db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
		}()

		// Seed a document into the sandbox's own schema - resetting must wipe it.
		if _, err := db.DB.Exec("INSERT INTO " + schemaName + ".documents (id, doctype, data, status, created_by) VALUES ('TEST3870-DOC', 'Item', '{}', 'Active', 'admin')"); err != nil {
			t.Fatalf("seed a document into the sandbox: %v", err)
		}
		if _, err := db.DB.Exec("UPDATE public.tenants SET sandbox_expires_at = CURRENT_TIMESTAMP + INTERVAL '1 hour' WHERE tenant_id = $1", tenantID); err != nil {
			t.Fatalf("shorten expiry before reset: %v", err)
		}

		if err := ResetSandboxTenant(tenantID, 9); err != nil {
			t.Fatalf("ResetSandboxTenant: %v", err)
		}

		var docCount int
		if err := db.DB.QueryRow("SELECT count(*) FROM " + schemaName + ".documents").Scan(&docCount); err != nil {
			t.Fatalf("count documents after reset: %v", err)
		}
		if docCount != 0 {
			t.Fatalf("expected reset to truncate the sandbox's documents table, found %d rows", docCount)
		}
		// The sandbox's own admin user must survive a reset - login stays working.
		var userCount int
		if err := db.DB.QueryRow("SELECT count(*) FROM " + schemaName + ".users WHERE username = 'admin'").Scan(&userCount); err != nil {
			t.Fatalf("count users after reset: %v", err)
		}
		if userCount != 1 {
			t.Fatalf("expected the sandbox's admin user to survive a reset, found %d", userCount)
		}
		var expiresAt time.Time
		if err := db.DB.QueryRow("SELECT sandbox_expires_at FROM public.tenants WHERE tenant_id = $1", tenantID).Scan(&expiresAt); err != nil {
			t.Fatalf("reload sandbox_expires_at after reset: %v", err)
		}
		daysOut := time.Until(expiresAt).Hours() / 24
		if daysOut < 8.9 || daysOut > 9.1 {
			t.Fatalf("expected reset to push sandbox_expires_at out to ~9 days, got %.2f days", daysOut)
		}
	})

	t.Run("DeleteSandboxTenant refuses a non-sandbox tenant and drops both the schema and the registry row for a real one", func(t *testing.T) {
		if err := DeleteSandboxTenant("default"); err == nil {
			t.Fatalf("expected deleting a real, non-sandbox tenant to be refused")
		}

		tenantID, schemaName, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
		if err != nil {
			t.Fatalf("ProvisionSandboxTenant: %v", err)
		}
		if err := DeleteSandboxTenant(tenantID); err != nil {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE")
			db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
			t.Fatalf("DeleteSandboxTenant: %v", err)
		}

		var regCount int
		db.DB.QueryRow("SELECT count(*) FROM public.tenants WHERE tenant_id = $1", tenantID).Scan(&regCount)
		if regCount != 0 {
			t.Fatalf("expected the tenant registry row to be gone after delete")
		}
		var schemaCount int
		db.DB.QueryRow("SELECT count(*) FROM information_schema.schemata WHERE schema_name = $1", schemaName).Scan(&schemaCount)
		if schemaCount != 0 {
			t.Fatalf("expected the sandbox's own schema to be dropped after delete")
		}
	})

	t.Run("ListSandboxTenants includes a provisioned sandbox and excludes real tenants", func(t *testing.T) {
		tenantID, schemaName, _, err := ProvisionSandboxTenant("0.1.0-test", 5)
		if err != nil {
			t.Fatalf("ProvisionSandboxTenant: %v", err)
		}
		defer func() {
			db.DB.Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE")
			db.DB.Exec("DELETE FROM public.tenants WHERE tenant_id = $1", tenantID)
		}()

		list, err := ListSandboxTenants()
		if err != nil {
			t.Fatalf("ListSandboxTenants: %v", err)
		}
		found, foundDefault := false, false
		for _, s := range list {
			if s["tenant_id"] == tenantID {
				found = true
			}
			if s["tenant_id"] == "default" {
				foundDefault = true
			}
		}
		if !found {
			t.Fatalf("expected the provisioned sandbox to appear in ListSandboxTenants, got %+v", list)
		}
		if foundDefault {
			t.Fatalf("expected the real default tenant to never appear in ListSandboxTenants")
		}
	})
}
