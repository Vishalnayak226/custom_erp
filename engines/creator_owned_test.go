package engines

import "testing"

// 276.2: every role keeps its own list and report columns, and only an admin
// or a Store Manager sets (or changes) the default for everyone.
func TestColumnProfileWriteAllowed(t *testing.T) {
	mine := map[string]interface{}{"scope": "Personal", "owner": "cashier1"}
	theirs := map[string]interface{}{"scope": "Personal", "owner": "manager1"}
	everyone := map[string]interface{}{"scope": "Universal", "owner": "manager1"}
	everyoneAsMine := map[string]interface{}{"scope": "Universal", "owner": "cashier1"}
	noOwner := map[string]interface{}{"scope": "Personal"}

	cases := []struct {
		name, role, user string
		record           map[string]interface{}
		want             bool
	}{
		{"own personal", "Cashier", "cashier1", mine, true},
		{"a new record before it is sent", "Cashier", "cashier1", nil, true},
		{"someone else's personal", "Cashier", "cashier1", theirs, false},
		{"the default for everyone", "Cashier", "cashier1", everyone, false},
		{"a default for everyone under their own name", "Cashier", "cashier1", everyoneAsMine, false},
		{"no owner", "Cashier", "cashier1", noOwner, false},
		{"no username on the session", "Cashier", "", noOwner, false},
		{"a role with no grant row", "Accountant", "acc1", map[string]interface{}{"scope": "Personal", "owner": "acc1"}, true},
		{"store manager sets the default", RoleStoreManager, "manager1", everyone, true},
		{"store manager changes anyone's", RoleStoreManager, "manager1", mine, true},
		{"full access sets the default", "Super Admin", "admin", everyone, true},
	}
	for _, c := range cases {
		if got := ColumnProfileWriteAllowed(c.role, c.user, c.record); got != c.want {
			t.Errorf("%s: ColumnProfileWriteAllowed(%q, %q) = %v, want %v", c.name, c.role, c.user, got, c.want)
		}
	}
	if !ColumnProfileManager("Super Admin") || !ColumnProfileManager(RoleStoreManager) || ColumnProfileManager("Cashier") {
		t.Error("ColumnProfileManager: full access and Store Manager manage column profiles; a Cashier does not")
	}
}
