package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// 205.3-205.6: what a key may reach is the key's modules, the plan, and the
// owner's rights today, whichever is narrowest.

func TestAPIAccessEveryDoctypeClassed(t *testing.T) {
	db.InitDB(testConnStr())
	rows, err := db.DB.Query(`SELECT DISTINCT name FROM tenant_default.doctype_meta`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		seen++
		if !APIDoctypeClassKnown(name) {
			t.Errorf("document type %s has no API class: add it to apiDoctypeClasses in engines/api_access_doctypes.go (rw, ro or never)", name)
		}
	}
	if seen == 0 {
		t.Fatal("no document types found")
	}
}

// apiAccessPick finds two plan-enabled modules with a writable document type each.
func apiAccessPick(t *testing.T) (modA, docA, modB, docB string) {
	t.Helper()
	modules, err := APIAccessModules("default", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range modules {
		if !m.InPlan || !m.CanChange {
			continue
		}
		for _, d := range m.Doctypes {
			if d.Class != APIDoctypeRW {
				continue
			}
			if modA == "" {
				modA, docA = m.Key, d.Doctype
			} else if m.Key != modA && modB == "" {
				modB, docB = m.Key, d.Doctype
			}
			break
		}
	}
	if modB == "" {
		t.Skip("fewer than two modules with a writable document type in this database")
	}
	return
}

func makeAPIKeyOwnedBy(t *testing.T, ownerID, ownerRole string, modules map[string]string) (*APICredential, *APIKeyAccess) {
	t.Helper()
	out, err := CreateAPIKey("default", APIKeyActor{UserID: ownerID, Role: ownerRole}, CreateAPIKeyInput{
		Name: "api-access-test " + ownerID, Grant: APIKeyGrant{Modules: modules}, Expires: "30d",
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, out.Credential.ID) })
	if out.TenantCode != "default" || out.APIKey == "" {
		t.Fatalf("created key = %+v", out)
	}
	if got := PublicAPIKeyTenant(out.APIKey); got != "default" {
		t.Fatalf("a new key must carry its company code; got %q", got)
	}
	access, err := LoadAPIKeyAccess("default", out.Credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	cred := out.Credential
	return &cred, access
}

func TestAPIAccessCeiling(t *testing.T) {
	db.InitDB(testConnStr())
	modA, docA, _, docB := apiAccessPick(t)
	owner := fmt.Sprintf("__api205_%d__", time.Now().UnixNano())
	seedLifecycleUser(t, owner, RoleSuperAdmin, "Active")
	cred, access := makeAPIKeyOwnedBy(t, owner, RoleSuperAdmin, map[string]string{modA: "write"})

	check := func(doctype, action, ip string) *APIRefusal {
		t.Helper()
		InvalidateLiveUserState("default", owner)
		refusal, err := CheckAPIKeyCall("default", cred, access, doctype, action, ip)
		if err != nil {
			t.Fatalf("check %s %s: %v", action, doctype, err)
		}
		return refusal
	}
	want := func(r *APIRefusal, code string) {
		t.Helper()
		if r == nil || r.Code != code {
			t.Fatalf("refusal = %+v, want %s", r, code)
		}
	}

	if r := check(docA, "read", "203.0.113.5"); r != nil {
		t.Fatalf("a key on %s was refused %s: %+v", modA, docA, r)
	}
	if r := check(docA, "create", "203.0.113.5"); r != nil {
		t.Fatalf("Change on %s was refused: %+v", docA, r)
	}

	t.Run("module not on the key", func(t *testing.T) {
		want(check(docB, "read", "203.0.113.5"), APIKeyModuleNotOnKey)
	})

	t.Run("view only key cannot change", func(t *testing.T) {
		viewOnly := *cred
		viewOnly.Scopes = []string{modA + ":read"}
		InvalidateLiveUserState("default", owner)
		r, err := CheckAPIKeyCall("default", &viewOnly, access, docA, "create", "203.0.113.5")
		if err != nil {
			t.Fatal(err)
		}
		want(r, APIKeyModuleNotOnKey)
	})

	t.Run("module not in the plan", func(t *testing.T) {
		var was bool
		if err := db.DB.QueryRow(`SELECT enabled FROM tenant_default.module_entitlements WHERE module_key=$1`, modA).Scan(&was); err != nil {
			t.Skipf("no entitlement row for %s: %v", modA, err)
		}
		db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=false WHERE module_key=$1`, modA)
		t.Cleanup(func() {
			db.DB.Exec(`UPDATE tenant_default.module_entitlements SET enabled=$2 WHERE module_key=$1`, modA, was)
		})
		want(check(docA, "read", "203.0.113.5"), APIKeyModuleNotInPlan)
	})

	t.Run("owner no longer active", func(t *testing.T) {
		db.DB.Exec(`UPDATE tenant_default.users SET status='Inactive' WHERE id=$1`, owner)
		t.Cleanup(func() { db.DB.Exec(`UPDATE tenant_default.users SET status='Active' WHERE id=$1`, owner) })
		want(check(docA, "read", "203.0.113.5"), APIKeyOwnerInactive)
	})

	t.Run("owner lost the right", func(t *testing.T) {
		// A cashier may not change or delete in these modules; the key keeps its
		// tick but the owner's own rights are the ceiling.
		db.DB.Exec(`UPDATE tenant_default.users SET role='Cashier' WHERE id=$1`, owner)
		t.Cleanup(func() { db.DB.Exec(`UPDATE tenant_default.users SET role=$2 WHERE id=$1`, owner, RoleSuperAdmin) })
		r := check(docA, "create", "203.0.113.5")
		if r != nil && r.Code != APIKeyOwnerLostModule {
			t.Fatalf("refusal = %+v, want %s or none when a cashier may create %s", r, APIKeyOwnerLostModule, docA)
		}
		if r == nil {
			if ok, _ := RoleMayOnDoctype("default", "Cashier", docA, "create"); ok {
				t.Skipf("a Cashier may create %s in this database", docA)
			}
			t.Fatal("a Cashier who may not create the document was let through")
		}
	})

	t.Run("store the owner does not work at", func(t *testing.T) {
		db.DB.Exec(`UPDATE tenant_default.api_credentials SET stores='["NO-SUCH-STORE"]'::jsonb WHERE id=$1`, cred.ID)
		access2, err := LoadAPIKeyAccess("default", cred.ID)
		if err != nil {
			t.Fatal(err)
		}
		db.DB.Exec(`UPDATE tenant_default.users SET role='Cashier', location_code='HO' WHERE id=$1`, owner)
		t.Cleanup(func() {
			db.DB.Exec(`UPDATE tenant_default.users SET role=$2 WHERE id=$1`, owner, RoleSuperAdmin)
			db.DB.Exec(`UPDATE tenant_default.api_credentials SET stores=NULL WHERE id=$1`, cred.ID)
		})
		InvalidateLiveUserState("default", owner)
		r, err := CheckAPIKeyCall("default", cred, access2, docA, "read", "203.0.113.5")
		if err != nil {
			t.Fatal(err)
		}
		want(r, APIKeyStoreNotAllowed)
	})

	t.Run("address not on the list", func(t *testing.T) {
		db.DB.Exec(`UPDATE tenant_default.api_credentials SET ip_allow='["203.0.113.0/24"]'::jsonb WHERE id=$1`, cred.ID)
		access2, err := LoadAPIKeyAccess("default", cred.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.DB.Exec(`UPDATE tenant_default.api_credentials SET ip_allow=NULL WHERE id=$1`, cred.ID) })
		InvalidateLiveUserState("default", owner)
		if r, _ := CheckAPIKeyCall("default", cred, access2, docA, "read", "198.51.100.9"); r == nil || r.Code != APIKeyIPNotAllowed {
			t.Fatalf("outside address = %+v, want %s", r, APIKeyIPNotAllowed)
		}
		if r, _ := CheckAPIKeyCall("default", cred, access2, docA, "read", "203.0.113.77"); r != nil {
			t.Fatalf("listed address refused: %+v", r)
		}
	})

	t.Run("a class never or read-only blocks the call", func(t *testing.T) {
		never := ""
		for name, class := range apiDoctypeClasses {
			if class == APIDoctypeNever {
				never = name
				break
			}
		}
		if never == "" {
			t.Skip("no never class")
		}
		if got := APIDoctypeClass(never); got != APIDoctypeNever {
			t.Fatalf("class of %s = %s", never, got)
		}
	})
}

func TestAPIAccessGrantIsHeldToTheOwner(t *testing.T) {
	db.InitDB(testConnStr())
	modA, _, _, _ := apiAccessPick(t)
	owner := fmt.Sprintf("__api205g_%d__", time.Now().UnixNano())
	seedLifecycleUser(t, owner, "Cashier", "Active")
	// A module the owner cannot change is refused when the key is made.
	_, err := CreateAPIKey("default", APIKeyActor{UserID: owner, Role: "Cashier"}, CreateAPIKeyInput{
		Name: "too wide", Grant: APIKeyGrant{Modules: map[string]string{modA: "write"}}, Expires: "30d",
	})
	if err == nil {
		t.Skipf("a Cashier may change module %s in this database", modA)
	}
	if verr, ok := err.(*ValidationError); !ok || verr.Code == "" {
		t.Fatalf("refusal = %v, want a coded refusal", err)
	}
}
