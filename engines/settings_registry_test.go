package engines

import (
	"custom_erp/db"
	"testing"
)

// TestSettingsRegistry (Stage 28.1) proves the three properties the config
// framework rests on: value validation per type, default fallback for an unset
// key, and a set/get round-trip whose override survives a subsequent rejected
// write. Cleans the shared dev DB before and after (the documented shared-DB
// test-pollution gotcha) so it's idempotent across reruns.
func TestSettingsRegistry(t *testing.T) {
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}

	t.Run("validation", func(t *testing.T) {
		cases := []struct {
			key, val string
			wantErr  bool
		}{
			{"loyalty.point_expiry_days", "365", false},
			{"loyalty.point_expiry_days", "0", true},   // below Min 1
			{"loyalty.point_expiry_days", "abc", true}, // not an int
			{"security.loyalty_otp_validity_minutes", "999", true}, // above Max 60
			{"loyalty.recompute_tier_on_earn", "true", false},
			{"loyalty.recompute_tier_on_earn", "maybe", true},
			{"security.default_idle_timeout_minutes", "30", false},
			{"security.default_idle_timeout_minutes", "7", true}, // not an allowed option
			{"does.not.exist", "1", true},                        // unregistered key
		}
		for _, c := range cases {
			err := ValidateSetting(c.key, c.val)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidateSetting(%q,%q): wantErr=%v got err=%v", c.key, c.val, c.wantErr, err)
			}
		}
	})

	t.Run("default fallback and set/get round-trip", func(t *testing.T) {
		key := "loyalty.point_expiry_days"
		clear := func() {
			db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", key)
			settingsCacheMu.Lock()
			delete(settingsCache, settingsCacheKey(schema, key))
			settingsCacheMu.Unlock()
		}
		clear()
		defer clear()

		if got := GetSettingInt(tenantID, key); got != 365 {
			t.Fatalf("expected registered default 365 when unset, got %d", got)
		}
		if err := SetSetting(tenantID, key, "180", "test"); err != nil {
			t.Fatalf("set: %v", err)
		}
		if got := GetSettingInt(tenantID, key); got != 180 {
			t.Fatalf("expected 180 after override, got %d", got)
		}
		if got := GetSettingIntForSchema(schema, key); got != 180 {
			t.Fatalf("expected schema-scoped read to see 180, got %d", got)
		}
		if err := SetSetting(tenantID, key, "0", "test"); err == nil {
			t.Fatalf("expected below-min write to be rejected")
		}
		if got := GetSettingInt(tenantID, key); got != 180 {
			t.Fatalf("expected 180 to survive a rejected write, got %d", got)
		}
	})
}
