package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 49.2.1 / 49.2.6 - identity lifecycle sweeps.

func seedLifecycleUser(t *testing.T, id, role, status string) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, 'x', $2, $3, $4, 'HO')`, id, id+"@lifecycle.invalid", role, status); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE details LIKE $1`, "Set user "+id+" status to%")
		InvalidateLiveUserState("default", id)
	})
}

// setLifecyclePolicy turns a policy on for tenant_default and, when aged,
// makes the change look long past so only the item's own idle time decides.
func setLifecyclePolicy(t *testing.T, key, days string, aged bool) {
	t.Helper()
	if err := SetSetting("default", key, days, "lifecycle-test"); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	if aged {
		if _, err := db.DB.Exec(`UPDATE tenant_default.system_settings SET updated_at = NOW() - INTERVAL '400 days' WHERE key = $1`, key); err != nil {
			t.Fatalf("age %s: %v", key, err)
		}
		// The client's last suspend/resume is a floor too; a freshly
		// migrated database stamps it today.
		var was string
		db.DB.QueryRow(`SELECT COALESCE(lifecycle_changed_at::text, '') FROM public.tenants WHERE tenant_id = 'default'`).Scan(&was)
		db.DB.Exec(`UPDATE public.tenants SET lifecycle_changed_at = NOW() - INTERVAL '400 days' WHERE tenant_id = 'default'`)
		t.Cleanup(func() {
			db.DB.Exec(`UPDATE public.tenants SET lifecycle_changed_at = NULLIF($1, '')::timestamptz WHERE tenant_id = 'default'`, was)
		})
	}
	t.Cleanup(func() {
		_ = SetSetting("default", key, "0", "lifecycle-test")
		db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, key)
	})
}

func lifecycleStatus(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := db.DB.QueryRow(`SELECT status FROM tenant_default.users WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("status of %s: %v", id, err)
	}
	return s
}

func TestStage4921RecordSignInAndReactivation(t *testing.T) {
	db.InitDB(testConnStr())
	id := "t4921-signin-" + stage4911Suffix()
	seedLifecycleUser(t, id, RoleCashier, "Active")

	RecordSignIn("default", id)
	var signedIn bool
	db.DB.QueryRow(`SELECT last_sign_in_at > NOW() - INTERVAL '1 minute' FROM tenant_default.users WHERE id = $1`, id).Scan(&signedIn)
	if !signedIn {
		t.Fatal("RecordSignIn did not stamp last_sign_in_at")
	}

	// Already Active: not a reactivation.
	MarkUserReactivated("tenant_default", id)
	var stamped bool
	db.DB.QueryRow(`SELECT reactivated_at IS NOT NULL FROM tenant_default.users WHERE id = $1`, id).Scan(&stamped)
	if stamped {
		t.Fatal("an Active account was marked reactivated")
	}
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Inactive' WHERE id = $1`, id)
	MarkUserReactivated("tenant_default", id)
	db.DB.QueryRow(`SELECT reactivated_at IS NOT NULL FROM tenant_default.users WHERE id = $1`, id).Scan(&stamped)
	if !stamped {
		t.Fatal("an Inactive account made Active again was not marked reactivated")
	}
	// Best effort: an unknown user or blank id is a no-op, never a panic.
	RecordSignIn("default", "")
	RecordSignIn("default", "t4921-nobody-"+stage4911Suffix())
}

func TestStage4921PickDormantKeepsSuperAdminAndLastAdmin(t *testing.T) {
	cands := []DormantAccount{
		{ID: "sa", Role: RoleSuperAdmin},
		{ID: "legacy", Role: RoleLegacySuperAdmin},
		{ID: "admin-old", Role: RoleAdmin},
		{ID: "cashier", Role: RoleCashier},
		{ID: "admin-new", Role: RoleAdmin},
	}
	ids := func(in []DormantAccount) string {
		var out []string
		for _, a := range in {
			out = append(out, a.ID)
		}
		return strings.Join(out, ",")
	}
	// Two active admins: the longer-idle one goes, the other stays.
	if got := ids(pickDormantToDisable(cands, 2)); got != "admin-old,cashier" {
		t.Errorf("two admins: got %q", got)
	}
	// One active admin: never the last.
	if got := ids(pickDormantToDisable(cands, 1)); got != "cashier" {
		t.Errorf("last admin: got %q", got)
	}
	// Plenty of admins: every dormant admin goes, never a Super Admin.
	if got := ids(pickDormantToDisable(cands, 5)); got != "admin-old,cashier,admin-new" {
		t.Errorf("many admins: got %q", got)
	}
}

func TestStage4921DormantSweepDisablesOnlyIdleAccounts(t *testing.T) {
	db.InitDB(testConnStr())
	ResetLiveUserStateCache()
	s := stage4911Suffix()
	dormant, recent, revived, never, super := "t4921-dorm-"+s, "t4921-recent-"+s, "t4921-revived-"+s, "t4921-never-"+s, "t4921-super-"+s
	for _, id := range []string{dormant, recent, revived, never} {
		seedLifecycleUser(t, id, RoleCashier, "Active")
	}
	seedLifecycleUser(t, super, RoleSuperAdmin, "Active")
	all := []string{dormant, recent, revived, never, super}
	db.DB.Exec(`UPDATE tenant_default.users SET created_at = NOW() - INTERVAL '200 days' WHERE id = ANY($1::text[])`, "{"+strings.Join(all, ",")+"}")
	db.DB.Exec(`UPDATE tenant_default.users SET last_sign_in_at = NOW() - INTERVAL '200 days' WHERE id IN ($1, $2, $3)`, dormant, revived, super)
	db.DB.Exec(`UPDATE tenant_default.users SET last_sign_in_at = NOW() - INTERVAL '1 day' WHERE id = $1`, recent)
	db.DB.Exec(`UPDATE tenant_default.users SET reactivated_at = NOW() - INTERVAL '1 day' WHERE id = $1`, revived)

	// Off (the default): nothing happens.
	if n, err := sweepDormantAccounts("default", all); err != nil || n != 0 {
		t.Fatalf("policy off: disabled %d, err %v", n, err)
	}
	// Just turned on: the window starts now, so nobody is idle yet.
	setLifecyclePolicy(t, SettingDormantAccountDays, "90", false)
	if n, err := sweepDormantAccounts("default", all); err != nil || n != 0 {
		t.Fatalf("policy just turned on: disabled %d, err %v", n, err)
	}
	// On long enough, but the client was just resumed: nobody could sign in
	// while it was suspended, so the window starts at the resume.
	setLifecyclePolicy(t, SettingDormantAccountDays, "90", true)
	db.DB.Exec(`UPDATE public.tenants SET lifecycle_changed_at = NOW() WHERE tenant_id = 'default'`)
	if n, err := sweepDormantAccounts("default", all); err != nil || n != 0 {
		t.Fatalf("client just resumed: disabled %d, err %v", n, err)
	}
	// On long enough: only the idle Cashier goes.
	db.DB.Exec(`UPDATE public.tenants SET lifecycle_changed_at = NOW() - INTERVAL '400 days' WHERE tenant_id = 'default'`)
	n, err := sweepDormantAccounts("default", all)
	if err != nil || n != 1 {
		t.Fatalf("aged policy: disabled %d, err %v (want 1)", n, err)
	}
	if got := lifecycleStatus(t, dormant); got != "Inactive" {
		t.Errorf("dormant account is %s, want Inactive", got)
	}
	for _, id := range []string{recent, revived, super} {
		if got := lifecycleStatus(t, id); got != "Active" {
			t.Errorf("%s is %s, want Active", id, got)
		}
	}
	// No sign-in recorded: idle only from when sign-ins began to be recorded.
	var trackedRecently bool
	db.DB.QueryRow(`SELECT COALESCE(applied_at > NOW() - INTERVAL '90 days', FALSE) FROM public.schema_migrations WHERE migration_file = $1`, identityLifecycleMigration).Scan(&trackedRecently)
	if trackedRecently && lifecycleStatus(t, never) != "Active" {
		t.Errorf("an account with no recorded sign-in was disabled before 90 days of tracking")
	}
	// The session ends and the reason is on record, in the admin's wording.
	if _, err := ResolveLiveUserState("default", dormant); !IsUserNotActiveError(err) {
		t.Errorf("disabled account still resolves: %v", err)
	}
	var audited int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'USER_MANAGEMENT' AND status = 'USER_STATUS_CHANGED'
		AND details LIKE $1`, "Set user "+dormant+" status to Inactive - not signed in for 90 days%").Scan(&audited)
	if audited != 1 {
		t.Errorf("audit rows for the disable = %d, want 1", audited)
	}
	// Running again changes nothing.
	if n, err := sweepDormantAccounts("default", all); err != nil || n != 0 {
		t.Errorf("second run disabled %d, err %v", n, err)
	}
}

func TestStage4921PostureListsDormantAccounts(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	idle, active := "t4921-pidle-"+s, "t4921-pactive-"+s
	seedLifecycleUser(t, idle, RoleCashier, "Active")
	seedLifecycleUser(t, active, RoleCashier, "Active")
	db.DB.Exec(`UPDATE tenant_default.users SET created_at = NOW() - INTERVAL '200 days', last_sign_in_at = NOW() - INTERVAL '120 days' WHERE id = $1`, idle)
	db.DB.Exec(`UPDATE tenant_default.users SET created_at = NOW() - INTERVAL '200 days', last_sign_in_at = NOW() - INTERVAL '2 days' WHERE id = $1`, active)

	p, err := GetSecurityPosture("default")
	if err != nil {
		t.Fatal(err)
	}
	if p.DormantDays != postureDormantDays || p.DormantAutoDisable {
		t.Errorf("policy off: days %d auto %v", p.DormantDays, p.DormantAutoDisable)
	}
	listed := map[string]bool{}
	for _, a := range p.DormantAccounts {
		listed[a.Username] = true
	}
	if !listed[idle] || listed[active] {
		t.Errorf("dormant list: idle listed %v, active listed %v", listed[idle], listed[active])
	}
}

func TestStage4926UnusedAPICredentialSweep(t *testing.T) {
	db.InitDB(testConnStr())
	s := stage4911Suffix()
	scope := []string{PublicAPIScopes()[0]}
	old, err := IssueAPICredential("default", "t4926-old-"+s, scope, nil, "lifecycle-test")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := IssueAPICredential("default", "t4926-fresh-"+s, scope, nil, "lifecycle-test")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{old.Credential.ID, fresh.Credential.ID}
	t.Cleanup(func() {
		for _, id := range ids {
			db.DB.Exec(`DELETE FROM tenant_default.api_oauth_tokens WHERE credential_id = $1::uuid`, id)
			db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1::uuid`, id)
			db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action = 'API_CREDENTIAL_REVOKED' AND details LIKE $1`, "credential="+id+"%")
		}
	})
	db.DB.Exec(`UPDATE tenant_default.api_credentials SET created_at = NOW() - INTERVAL '200 days', last_used_at = NOW() - INTERVAL '100 days' WHERE id = $1::uuid`, old.Credential.ID)
	db.DB.Exec(`UPDATE tenant_default.api_credentials SET created_at = NOW() - INTERVAL '200 days', last_used_at = NOW() - INTERVAL '3 days' WHERE id = $1::uuid`, fresh.Credential.ID)

	if n, err := sweepUnusedAPICredentials("default", ids); err != nil || n != 0 {
		t.Fatalf("policy off: revoked %d, err %v", n, err)
	}
	setLifecyclePolicy(t, SettingAPICredentialUnusedDays, "30", false)
	if n, err := sweepUnusedAPICredentials("default", ids); err != nil || n != 0 {
		t.Fatalf("policy just turned on: revoked %d, err %v", n, err)
	}
	setLifecyclePolicy(t, SettingAPICredentialUnusedDays, "30", true)
	if n, err := sweepUnusedAPICredentials("default", ids); err != nil || n != 1 {
		t.Fatalf("aged policy: revoked %d, err %v (want 1)", n, err)
	}
	status := func(id string) string {
		var st string
		db.DB.QueryRow(`SELECT status FROM tenant_default.api_credentials WHERE id = $1::uuid`, id).Scan(&st)
		return st
	}
	if status(old.Credential.ID) != "Revoked" || status(fresh.Credential.ID) != "Active" {
		t.Errorf("old %s, fresh %s; want Revoked, Active", status(old.Credential.ID), status(fresh.Credential.ID))
	}
	if _, err := AuthenticateAPICredential("default", old.APIKey); err != ErrInvalidAPICredential {
		t.Errorf("revoked key still authenticates: %v", err)
	}
	var audited int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'API_CREDENTIAL_REVOKED' AND details LIKE $1`,
		"credential="+old.Credential.ID+"%not used for 30 days%").Scan(&audited)
	if audited != 1 {
		t.Errorf("audit rows for the revoke = %d, want 1", audited)
	}
}
