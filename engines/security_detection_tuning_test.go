package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 49.5.4 - abuse thresholds that follow the client's volume or a
// per-role setting. Off = today's fixed thresholds.

func tuningSetting(t *testing.T, key, value string) {
	t.Helper()
	if err := SetSetting("default", key, value, "t49-5-4"); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() {
		if SetSetting("default", key, "", "t49-5-4") != nil {
			_ = SetSetting("default", key, "0", "t49-5-4") // the select settings' Off
		}
		db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, key)
	})
}

func TestStage4954TuningOffKeepsFixedThresholds(t *testing.T) {
	db.InitDB(testConnStr())
	now := time.Now()
	tn := loadDetectionTuning("tenant_default", now)
	for _, r := range SecurityDetectionRules() {
		if got := tn.limit(r, "anyone", now); got != r.Threshold {
			t.Errorf("%s: limit %d with tuning off, want %d", r.Key, got, r.Threshold)
		}
	}
}

func TestStage4954RoleAndVolumeScale(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	user := "t4954r" + s
	doc, cleanup := stage4954Fixture(t, user, "T4954Dt"+s)
	t.Cleanup(cleanup)
	now := time.Now()
	rule := stage4954Rule(t, "access_denied_burst")
	other := stage4954Rule(t, "privilege_change_burst")

	tuningSetting(t, SettingDetectionRoleScale, "Cashier=200, bogus, Manager=5")
	tn := loadDetectionTuning("tenant_default", now)
	if got := tn.limit(rule, user, now); got != rule.Threshold*2 {
		t.Errorf("Cashier=200: limit %d, want %d", got, rule.Threshold*2)
	}
	if got := tn.limit(rule, "nobody-"+s, now); got != rule.Threshold {
		t.Errorf("unknown actor: limit %d, want %d", got, rule.Threshold)
	}
	tuningSetting(t, SettingDetectionRoleScale, "cashier=50%")
	tn = loadDetectionTuning("tenant_default", now)
	if got := tn.limit(rule, user, now); got != rule.Threshold/2 {
		t.Errorf("Cashier=50: limit %d, want %d", got, rule.Threshold/2)
	}
	tuningSetting(t, SettingDetectionRoleScale, "")

	// 28 documents over the 14-day window is 2 a day; level 1 a day = 2x.
	for i := 0; i < 28; i++ {
		doc(fmt.Sprintf("T4954-%s-%d", s, i), "T4954Dt"+s, map[string]interface{}{})
	}
	tuningSetting(t, SettingDetectionVolumeDocsPerDay, "0")
	if got := loadDetectionTuning("tenant_default", now).limit(rule, user, now); got != rule.Threshold {
		t.Errorf("volume off: limit %d, want %d", got, rule.Threshold)
	}
	tuningSetting(t, SettingDetectionVolumeDocsPerDay, "250")
	tn = loadDetectionTuning("tenant_default", now)
	if got := tn.limit(rule, user, now); got != rule.Threshold {
		t.Errorf("a quiet client at level 250: limit %d, want %d", got, rule.Threshold)
	}
	tn = loadDetectionTuning("tenant_default", now)
	tn.docsPerDay = 1 // the smallest offered level is 250 a day; 28 documents in 14 days is 2 a day
	if got := tn.limit(rule, user, now); got < rule.Threshold*2 {
		t.Errorf("busy client: limit %d, want at least %d", got, rule.Threshold*2)
	}
	if got := tn.limit(other, user, now); got != other.Threshold {
		t.Errorf("a rule outside the burst list scaled: %d, want %d", got, other.Threshold)
	}
}

func TestStage4954ScaledLimitDecidesTheAlert(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	user := "t4954a" + s
	_, cleanup := stage4954Fixture(t, user, "T4954Dt"+s)
	t.Cleanup(func() {
		cleanup()
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = 'SECURITY_ALERT' AND details LIKE $1`, "%"+user+"%")
	})
	rule := stage4954Rule(t, "access_denied_burst")
	for i := 0; i < rule.Threshold; i++ {
		LogAuditEvent("default", user, "GLOBAL-0011", "REFUSED", "t4954 refused")
	}
	mine := func(fired []SecurityDetection) bool {
		for _, f := range fired {
			if f.Rule == rule.Key && strings.Contains(f.Subject, user) {
				return true
			}
		}
		return false
	}
	tuningSetting(t, SettingDetectionRoleScale, "Cashier=200")
	fired, err := RunSecurityDetections("default", time.Now())
	if err != nil || mine(fired) {
		t.Fatalf("Cashier=200: the alert fired at the old limit (err %v)", err)
	}
	tuningSetting(t, SettingDetectionRoleScale, "")
	fired, err = RunSecurityDetections("default", time.Now())
	if err != nil || !mine(fired) {
		t.Fatalf("setting cleared: the alert did not fire at the fixed limit (err %v)", err)
	}
}
