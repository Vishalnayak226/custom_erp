package engines

import (
	"context"
	"custom_erp/db"
	"errors"
	"slices"
	"testing"
)

func TestModuleBoundaryReportExport(t *testing.T) {
	db.InitDB(testConnStr())
	var previous bool
	if err := db.DB.QueryRow(`SELECT enabled FROM tenant_default.module_entitlements WHERE module_key='hr'`).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$1 WHERE module_key='hr'`, previous)
	})
	if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=false WHERE module_key='hr'`); err != nil {
		t.Fatal(err)
	}
	job, err := CreateReportExportJob("default", "attendance-summary", RoleSuperAdmin, map[string]string{}, "system")
	if job != "" {
		defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype='ReportExportJob' AND id=$1`, job)
	}
	if err == nil {
		t.Fatal("disabled HR module accepted attendance export")
	}
}

func TestModuleManifestEngineBoundaries(t *testing.T) {
	db.InitDB(testConnStr())
	catalog, err := ListModules()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, m := range catalog {
		known[m.ModuleKey] = true
	}
	for _, pkg := range ListProductPackages() {
		for _, key := range ExpandPackagesToModules([]string{pkg.PackageKey}) {
			if !known[key] {
				t.Errorf("package %s references unknown module %s", pkg.PackageKey, key)
			}
		}
	}
	for _, m := range catalog {
		for _, key := range ModulePrerequisites(m.ModuleKey, catalog) {
			if !known[key] {
				t.Errorf("module %s requires unknown %s", m.ModuleKey, key)
			}
		}
	}
	for _, def := range ListReportDefinitions() {
		if !known[def.ModuleKey] {
			t.Errorf("report %s category %s resolves unknown module %s", def.ID, def.Category, def.ModuleKey)
		}
	}
	for _, key := range []string{"", "__unknown_module__"} {
		if err := RequireModules("default", key); err == nil {
			t.Errorf("allowed unknown module %q", key)
		}
		for _, enabled := range []bool{true, false} {
			if err := SetModuleEntitlement("default", key, enabled, "system"); err == nil {
				t.Errorf("accepted unknown entitlement %q", key)
			}
		}
		eligible, err := listTenantSchemas(key)
		if err != nil {
			t.Fatal(err)
		}
		if len(eligible) != 0 {
			t.Errorf("unknown module %q admitted workers: %v", key, eligible)
		}
	}
	if err := ApplyPackageSelection("default", []string{"__unknown_package__"}, "system"); err == nil {
		t.Error("accepted unknown package")
	}
	for _, m := range catalog {
		t.Run(m.ModuleKey, func(t *testing.T) {
			var before bool
			if err := db.DB.QueryRow(`SELECT enabled FROM tenant_default.module_entitlements WHERE module_key=$1`, m.ModuleKey).Scan(&before); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$1 WHERE module_key=$2`, before, m.ModuleKey); err != nil {
					t.Error(err)
				}
			})
			for _, enabled := range []bool{false, true} {
				if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$1 WHERE module_key=$2`, enabled, m.ModuleKey); err != nil {
					t.Fatal(err)
				}
				eligible, err := listTenantSchemas(m.ModuleKey)
				if err != nil {
					t.Fatal(err)
				}
				if slices.Contains(eligible, "tenant_default") != enabled {
					t.Errorf("worker eligibility does not match %s=%v: %v", m.ModuleKey, enabled, eligible)
				}
			}
		})
	}
}

func TestModuleBoundaryReportLifecycle(t *testing.T) {
	db.InitDB(testConnStr())
	for _, key := range []string{"hr", "reports"} {
		var previous bool
		if err := db.DB.QueryRow(`SELECT enabled FROM tenant_default.module_entitlements WHERE module_key=$1`, key).Scan(&previous); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$1 WHERE module_key=$2`, previous, key); err != nil {
				t.Error(err)
			}
		})
	}
	if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=true WHERE module_key IN ('hr','reports')`); err != nil {
		t.Fatal(err)
	}
	job, err := CreateReportExportJob("default", "attendance-summary", RoleSuperAdmin, map[string]string{}, "system")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype='ReportExportJob' AND id=$1`, job)
	})
	if _, err := db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=false WHERE module_key='hr'`); err != nil {
		t.Fatal(err)
	}
	assertDenied := func(err error) {
		t.Helper()
		var v *ValidationError
		if !errors.As(err, &v) || v.Code != "SAAS-0191" {
			t.Errorf("expected module denial, got %v", err)
		}
	}
	_, _, _, err = RunReport("default", "attendance-summary", RoleSuperAdmin, "system", map[string]string{})
	assertDenied(err)
	_, err = RunReportDrillDown("default", "attendance-summary", RoleSuperAdmin, "x", map[string]string{})
	assertDenied(err)
	_, _, _, err = GetReportExportJob("default", job)
	assertDenied(err)
	processReportExportJobs(context.Background(), "tenant_default")
	var status, csv string
	if err := db.DB.QueryRow(`SELECT status,COALESCE(data->>'csv','') FROM tenant_default.documents WHERE doctype='ReportExportJob' AND id=$1`, job).Scan(&status, &csv); err != nil {
		t.Fatal(err)
	}
	if status != "Failed" || csv != "" {
		t.Fatalf("disabled module worker produced export: status=%s csv_bytes=%d", status, len(csv))
	}
}

func TestModuleBoundaryMissingEntitlement(t *testing.T) {
	db.InitDB(testConnStr())
	// Preserve the entire fixture row, including attribution and timestamps.
	var original string
	if err := db.DB.QueryRow(`SELECT row_to_json(e)::text FROM tenant_default.module_entitlements e WHERE module_key='hr'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	_, err := db.DB.Exec(`DELETE FROM tenant_default.module_entitlements WHERE module_key='hr'`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := db.DB.Exec(`INSERT INTO tenant_default.module_entitlements SELECT * FROM json_populate_record(NULL::tenant_default.module_entitlements,$1::json)`, original)
		if err != nil {
			t.Error(err)
		}
	})
	items, err := ListModuleEntitlements("default")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ModuleKey == "hr" && item.Enabled {
			t.Fatal("missing entitlement advertised as enabled while runtime denies it")
		}
	}
}
