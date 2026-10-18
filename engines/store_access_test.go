package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 85.A: store access, role first and then user. The rules worth
// pinning are the ones a later change could quietly invert: "All" includes a
// store added afterwards; a user's own stores ADD to an inherited role; a
// claimed working store the user has lost falls back to home rather than
// staying; and a switch is refused while the user's till is open.
func TestCombineStoreAccess(t *testing.T) {
	for _, tc := range []struct {
		name           string
		role, userMode string
		userStores     []string
		roleMode       string
		roleStores     []string
		allow, deny    []string
		wantAll        bool
	}{
		{name: "super admin is every store", role: "Super Admin", userMode: StoreAccessHome, roleMode: StoreAccessHome, allow: []string{"HO", "ANY"}, wantAll: true},
		{name: "role never set is home only", role: "Cashier", userMode: StoreAccessInherit, roleMode: StoreAccessHome, allow: []string{"HO"}, deny: []string{"JAY"}},
		{name: "inherit adds the user's own stores", role: "Cashier", userMode: StoreAccessInherit, userStores: []string{"JAY"}, roleMode: StoreAccessSelected, roleStores: []string{"KOR"}, allow: []string{"HO", "JAY", "KOR"}, deny: []string{"WH"}},
		{name: "role All reaches a store added later", role: "Area Manager", userMode: StoreAccessInherit, roleMode: StoreAccessAll, allow: []string{"NEW-STORE"}, wantAll: true},
		{name: "user Home overrides a role All", role: "Area Manager", userMode: StoreAccessHome, roleMode: StoreAccessAll, allow: []string{"HO"}, deny: []string{"JAY"}},
		{name: "user Selected ignores the role list", role: "Cashier", userMode: StoreAccessSelected, userStores: []string{"JAY"}, roleMode: StoreAccessSelected, roleStores: []string{"KOR"}, allow: []string{"HO", "JAY"}, deny: []string{"KOR"}},
		{name: "user All", role: "Cashier", userMode: StoreAccessAll, roleMode: StoreAccessHome, allow: []string{"ANY"}, wantAll: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := combineStoreAccess(tc.role, "HO", tc.userMode, tc.userStores, tc.roleMode, tc.roleStores)
			if a.All != tc.wantAll {
				t.Fatalf("All = %v, want %v", a.All, tc.wantAll)
			}
			for _, s := range tc.allow {
				if !a.Allows(s) {
					t.Errorf("should allow %s", s)
				}
			}
			for _, s := range tc.deny {
				if a.Allows(s) {
					t.Errorf("should not allow %s", s)
				}
			}
			if a.Allows("") {
				t.Error("an empty store is never allowed")
			}
		})
	}
}

func TestWorkingStoreFallsBackToHome(t *testing.T) {
	live := LiveUserState{LocationCode: "HO", Access: combineStoreAccess("Cashier", "HO", StoreAccessSelected, []string{"JAY"}, StoreAccessHome, nil)}
	if got := live.WorkingStore("JAY"); got != "JAY" {
		t.Errorf("allowed claim: got %q, want JAY", got)
	}
	if got := live.WorkingStore("KOR"); got != "HO" {
		t.Errorf("a store taken away must fall back to home: got %q", got)
	}
	if got := live.WorkingStore(""); got != "HO" {
		t.Errorf("no claim: got %q, want HO", got)
	}
}

func TestStoreAccessSettingsAndSwitch(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		user  = "s85-cashier"
		role  = "S85 Test Role"
		home  = "S85-HOME"
		other = "S85-OTHER"
		third = "S85-THIRD"
	)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S85-%' OR (doctype = 'POSSession' AND data->>'cashier' = '" + user + "')")
		db.DB.Exec("DELETE FROM "+schema+".users WHERE id = $1", user)
		db.DB.Exec("DELETE FROM "+schema+".role_store_access WHERE role = $1", role)
		ResetLiveUserStateCache()
	}
	cleanup()
	defer cleanup()

	for _, id := range []string{home, other, third} {
		raw, _ := json.Marshal(map[string]interface{}{"code": id, "name": id + " Store", "type": "Store", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatalf("location %s: %v", id, err)
		}
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".users (id, username, password_hash, role, status, location_code) VALUES ($1, $1, 'x', $2, 'Active', $3)", user, role, home); err != nil {
		t.Fatalf("user: %v", err)
	}

	access := func() StoreAccess {
		InvalidateLiveUserState(tenantID, user)
		a, err := UserStoreAccess(tenantID, user)
		if err != nil {
			t.Fatalf("access: %v", err)
		}
		return a
	}
	if a := access(); a.All || !a.Allows(home) || a.Allows(other) {
		t.Fatalf("a role never set is home only, got %+v", a)
	}

	// Role first: Selected gives OTHER.
	if err := SetRoleStoreAccess(tenantID, role, "Selected", []string{other}, "test"); err != nil {
		t.Fatalf("role selected: %v", err)
	}
	if a := access(); !a.Allows(other) || a.Allows(third) {
		t.Fatalf("role Selected should give OTHER only, got %+v", a)
	}
	// Then the user: an extra store on top of the role.
	if err := SetUserStoreAccess(tenantID, user, "Inherit", []string{third}, "test"); err != nil {
		t.Fatalf("user inherit: %v", err)
	}
	if a := access(); !a.Allows(other) || !a.Allows(third) {
		t.Fatalf("inherit + extra should give OTHER and THIRD, got %+v", a)
	}
	// Role All: a store created afterwards is allowed with no edit.
	if err := SetRoleStoreAccess(tenantID, role, "All", nil, "test"); err != nil {
		t.Fatalf("role all: %v", err)
	}
	if a := access(); !a.All || !a.Allows("S85-NOT-YET-CREATED") {
		t.Fatalf("role All should allow any store, got %+v", a)
	}

	// Coded refusals.
	var ve *ValidationError
	if err := SetRoleStoreAccess(tenantID, "Super Admin", "Home", nil, "test"); !errors.As(err, &ve) || ve.Code != "STORE-0853" {
		t.Errorf("narrowing Super Admin: want STORE-0853, got %v", err)
	}
	if err := SetRoleStoreAccess(tenantID, role, "Selected", []string{"S85-NO-SUCH-STORE"}, "test"); !errors.As(err, &ve) || ve.Code != "STORE-0852" {
		t.Errorf("unknown store: want STORE-0852, got %v", err)
	}
	if err := SetRoleStoreAccess(tenantID, role, "Selected", nil, "test"); !errors.As(err, &ve) || ve.Code != "STORE-0851" {
		t.Errorf("Selected with no store: want STORE-0851, got %v", err)
	}

	// Switching: allowed, then refused while a till session is open here.
	if err := SetUserStoreAccess(tenantID, user, "Selected", []string{other}, "test"); err != nil {
		t.Fatalf("user selected: %v", err)
	}
	if got, err := SwitchWorkingStore(tenantID, user, user, home, other); err != nil || got != other {
		t.Fatalf("switch to OTHER: got %q, %v", got, err)
	}
	if _, err := SwitchWorkingStore(tenantID, user, user, other, third); !errors.As(err, &ve) || ve.Code != "STORE-0850" {
		t.Errorf("switch to a store not given: want STORE-0850, got %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"location": other, "cashier": user, "status": "Open"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('S85-SESSION', 'POSSession', $1, 'Open', $2)", raw, user); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := SwitchWorkingStore(tenantID, user, user, other, home); !errors.As(err, &ve) || ve.Code != "STORE-0854" {
		t.Errorf("switch with the till open: want STORE-0854, got %v", err)
	}

	stores, err := StoresFor(tenantID, access())
	if err != nil {
		t.Fatalf("stores: %v", err)
	}
	names := map[string]string{}
	for _, s := range stores {
		names[s.Code] = s.Name
	}
	if names[home] != home+" Store" || names[other] != other+" Store" || names[third] != "" {
		t.Errorf("StoresFor should list HOME and OTHER by name only, got %v", names)
	}
}
