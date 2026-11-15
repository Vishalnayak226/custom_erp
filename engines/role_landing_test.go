package engines

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.14.5: per-role landing, and the maturity labels behind the sidebar.

func TestRoleLandingRoundTripAndTemplateFallback(t *testing.T) {
	db.InitDB(testConnStr())
	const role = "QA Landing Role"
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.role_landing WHERE role = $1`, role) })
	if v, _ := ResolveRoleLanding("default", role); v != "" {
		t.Fatalf("an unknown role must have no landing, got %q", v)
	}
	if v, fromTpl := ResolveRoleLanding("default", RoleCashier); v != "pos" || !fromTpl {
		t.Fatalf("Cashier should land on pos from its template, got %q %v", v, fromTpl)
	}
	if err := SetRoleLanding("default", role, "finance", "admin"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, fromTpl := ResolveRoleLanding("default", role); v != "finance" || fromTpl {
		t.Fatalf("stored landing not used: %q %v", v, fromTpl)
	}
	if err := SetRoleLanding("default", RoleCashier, "reports", "admin"); err != nil {
		t.Fatalf("set cashier: %v", err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.role_landing WHERE role = $1`, RoleCashier) })
	if v, _ := ResolveRoleLanding("default", RoleCashier); v != "reports" {
		t.Fatalf("a stored choice must beat the template, got %q", v)
	}
	if err := SetRoleLanding("default", role, "", "admin"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if v, _ := ResolveRoleLanding("default", role); v != "" {
		t.Fatalf("clearing should restore the default, got %q", v)
	}
}

// A row saved under another spelling of the role is replaced, not left to win.
func TestRoleLandingSaveReplacesAnotherSpelling(t *testing.T) {
	db.InitDB(testConnStr())
	const role = "QA Landing Case"
	clean := func() { db.DB.Exec(`DELETE FROM tenant_default.role_landing WHERE lower(role) = lower($1)`, role) }
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.role_landing (role, view, updated_by, updated_at) VALUES ($1, 'reports', 'x', now())`, strings.ToLower(role)); err != nil {
		t.Fatal(err)
	}
	if err := SetRoleLanding("default", role, "finance", "admin"); err != nil {
		t.Fatal(err)
	}
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.role_landing WHERE lower(role) = lower($1)`, role).Scan(&n)
	if v, _ := ResolveRoleLanding("default", role); v != "finance" || n != 1 {
		t.Fatalf("landing %q with %d rows, want finance and one row", v, n)
	}
}

func TestRoleLandingRefusals(t *testing.T) {
	db.InitDB(testConnStr())
	if c := codeOf(SetRoleLanding("default", " ", "pos", "admin")); c != "GLOBAL-9140" {
		t.Fatalf("no role: %q", c)
	}
	if c := codeOf(SetRoleLanding("default", "QA Landing Role", "not-a-screen", "admin")); c != "GLOBAL-9141" {
		t.Fatalf("bad screen: %q", c)
	}
}

func TestNavMaturityLabelsOnlyUnfinishedEntries(t *testing.T) {
	modules, menus := NavMaturity()
	for k, lvl := range modules {
		if lvl == MaturityProduction || lvl == MaturityCertified || lvl == "" {
			t.Errorf("module %s labelled %q: only levels below Production are labels", k, lvl)
		}
	}
	for id, lvl := range menus {
		if !strings.HasPrefix(id, "menu-") || lvl == MaturityProduction || lvl == MaturityCertified {
			t.Errorf("menu %s labelled %q", id, lvl)
		}
	}
	if len(modules)+len(menus) == 0 {
		t.Fatal("no entry is labelled: the register has Preview capabilities, so something must be")
	}
}

func TestCapabilityMenusPointAtRealCapabilitiesAndMenus(t *testing.T) {
	html, err := os.ReadFile("../public/index.html")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range CapabilityMaturityRegistry() {
		have[c.Key] = true
	}
	for key, ids := range capabilityMenus {
		if !have[key] {
			t.Errorf("capabilityMenus names %q, which is not in the register", key)
		}
		for _, id := range ids {
			if !regexp.MustCompile(`id="` + regexp.QuoteMeta(id) + `"`).Match(html) {
				t.Errorf("menu %s is not in public/index.html", id)
			}
		}
	}
}

func TestNavForRoleHidesUnfinishedOnlyFromNonAdmins(t *testing.T) {
	db.InitDB(testConnStr())
	t.Cleanup(func() { SetSetting("default", SettingKeyHideUnfinished, "false", "test") })
	if err := SetSetting("default", SettingKeyHideUnfinished, "false", "test"); err != nil {
		t.Fatal(err)
	}
	if NavForRole("default", RoleCashier).Hide {
		t.Fatal("hiding is off by default")
	}
	if err := SetSetting("default", SettingKeyHideUnfinished, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if !NavForRole("default", RoleCashier).Hide {
		t.Fatal("a cashier must have unfinished entries hidden when the setting is on")
	}
	if NavForRole("default", RoleSuperAdmin).Hide {
		t.Fatal("an administrator always sees every entry")
	}
}

// 202.2: short codes are on unless the company turns them off, and turning
// them back on restores them.
func TestNavForRoleShortCodesSwitch(t *testing.T) {
	db.InitDB(testConnStr())
	t.Cleanup(func() { SetSetting("default", SettingKeyShortCodes, "true", "test") })
	if settingsRegistry[SettingKeyShortCodes].Default != "true" {
		t.Fatal("short codes are on by default")
	}
	for _, c := range []struct {
		value string
		off   bool
	}{{"false", true}, {"true", false}} {
		if err := SetSetting("default", SettingKeyShortCodes, c.value, "test"); err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{RoleCashier, RoleSuperAdmin} {
			if got := NavForRole("default", role).NoCodes; got != c.off {
				t.Errorf("setting %s, role %s: no_codes = %v, want %v", c.value, role, got, c.off)
			}
		}
	}
}
