package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestApplyPackageSelectionReportsUndisabledModules is Stage 50/AUD-05: a
// module that refuses to disable on every retry pass used to make
// ApplyPackageSelection return nil anyway - "with PIM enabled, apply the
// HR-only package while a fixture trigger rejects disabling PIM... returns
// nil and PIM stays enabled." The real code path that can make a disable
// permanently fail is SetModuleEntitlement's dependentsOf check
// (engines/modules.go), but this codebase's only entry in
// moduleDependencies ("rfq" needs "procurement") is always bundled with its
// prerequisite in every real ProductPackage, so an ordinary client can
// never reach the case where one is wanted and the other isn't - the same
// honest limitation the audit itself recorded ("no claim is made that an
// ordinary client can install the fault-injection trigger"). This test
// reproduces the failure the same way the audit did: a Postgres trigger
// that unconditionally refuses to disable one specific module, standing in
// for any real-world write failure (a stricter grant, a constraint, a
// transient error) SetModuleEntitlement's own retry-across-passes design
// already has to tolerate.
func TestApplyPackageSelectionReportsUndisabledModules(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	// Snapshot every current entitlement row so this test can restore the
	// tenant to exactly its prior state afterward, regardless of pass/fail.
	type row struct {
		moduleKey string
		enabled   bool
	}
	var before []row
	rows, err := db.DB.Query("SELECT module_key, enabled FROM " + schema + ".module_entitlements")
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.moduleKey, &r.enabled); err != nil {
			rows.Close()
			t.Fatalf("snapshot scan: %v", err)
		}
		before = append(before, r)
	}
	rows.Close()

	restore := func() {
		for _, r := range before {
			_, _ = db.DB.Exec(
				"UPDATE "+schema+".module_entitlements SET enabled = $1 WHERE module_key = $2",
				r.enabled, r.moduleKey)
		}
	}
	defer restore()

	if err := SetModuleEntitlement(tenantID, "pim", true, "test-aud05"); err != nil {
		t.Fatalf("enable pim before the test: %v", err)
	}

	const triggerName = "aud05_block_pim_disable"
	const funcName = "aud05_block_pim_disable_fn"
	installTrigger := `
		CREATE OR REPLACE FUNCTION ` + schema + `.` + funcName + `() RETURNS trigger AS $$
		BEGIN
			IF NEW.module_key = 'pim' AND NEW.enabled = false THEN
				RAISE EXCEPTION 'aud05 fixture: refusing to disable pim';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER ` + triggerName + `
			BEFORE UPDATE ON ` + schema + `.module_entitlements
			FOR EACH ROW EXECUTE FUNCTION ` + schema + `.` + funcName + `();`
	if _, err := db.DB.Exec(installTrigger); err != nil {
		t.Fatalf("install fixture trigger: %v", err)
	}
	defer func() {
		_, _ = db.DB.Exec("DROP TRIGGER IF EXISTS " + triggerName + " ON " + schema + ".module_entitlements")
		_, _ = db.DB.Exec("DROP FUNCTION IF EXISTS " + schema + "." + funcName + "()")
	}()

	// "hr" wants only hr+reports, so pim is one of the modules
	// ApplyPackageSelection will try (and, per the trigger, permanently
	// fail) to disable.
	err = ApplyPackageSelection(tenantID, []string{"hr"}, "test-aud05")
	if err == nil {
		t.Fatal("expected ApplyPackageSelection to report the undisableable module, got nil error")
	}
	if !strings.Contains(err.Error(), "pim") {
		t.Errorf("expected the error to name the module that could not be disabled (pim), got: %v", err)
	}

	stillEnabled, err := IsModuleEnabled(tenantID, "pim")
	if err != nil {
		t.Fatalf("IsModuleEnabled: %v", err)
	}
	if !stillEnabled {
		t.Error("pim was reported as disabled despite the trigger refusing every write - state and error now disagree")
	}
}
