package engines

import (
	"custom_erp/db"
	"testing"
)

// TestNewClientRequiresARehearsal (162.19): a client provisioned from now on
// starts with platform.require_rehearsal On, saved as its own setting; the
// registered default, what every existing client that never chose reads,
// stays Off, so no live client moves.
func TestNewClientRequiresARehearsal(t *testing.T) {
	db.InitDB(testConnStr())
	if d := settingsRegistry[SettingRequireRehearsal].Default; d != "false" {
		t.Fatalf("the registered default is %q - every existing client would change", d)
	}
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !GetSettingBool(tenantID, SettingRequireRehearsal) {
		t.Fatal("a new client does not require a sandbox rehearsal")
	}
	// The client can still turn it off.
	if err := SetSetting(tenantID, SettingRequireRehearsal, "false", "admin"); err != nil {
		t.Fatal(err)
	}
	if GetSettingBool(tenantID, SettingRequireRehearsal) {
		t.Error("turning the rehearsal off did not stick")
	}
}
