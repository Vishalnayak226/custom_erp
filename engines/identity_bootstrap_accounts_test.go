package engines

import (
	"os"
	"testing"

	"custom_erp/db"
)

// SB-021 / 178.1: the migration closes the shipped `system` account and asks
// cashier1 to choose a password only while it still has the shipped one. A
// password anyone has set is never touched, and no hash is ever written.
func TestBootstrapAccountsMigrationClosesShippedCredentials(t *testing.T) {
	db.InitDB(testConnStr())
	const shippedCashier = "$2a$10$u2OOnj/nClI2tPmLfyTPpuePXesLvp1oOwzfK4EAmKFNxbYeJzS5u"
	type acct struct {
		status, hash string
		mustChange   bool
	}
	read := func(id string) (acct, bool) {
		var a acct
		err := db.DB.QueryRow(`SELECT status, password_hash, must_change_password FROM tenant_default.users WHERE id = $1`, id).
			Scan(&a.status, &a.hash, &a.mustChange)
		return a, err == nil
	}
	origSystem, okS := read("system")
	origCashier, okC := read("cashier1")
	if !okS || !okC {
		t.Skip("bootstrap accounts not seeded in this database")
	}
	defer func() {
		db.DB.Exec(`UPDATE tenant_default.users SET status = $2, password_hash = $3, must_change_password = $4 WHERE id = $1`,
			"system", origSystem.status, origSystem.hash, origSystem.mustChange)
		db.DB.Exec(`UPDATE tenant_default.users SET status = $2, password_hash = $3, must_change_password = $4 WHERE id = $1`,
			"cashier1", origCashier.status, origCashier.hash, origCashier.mustChange)
	}()

	sqlText, err := os.ReadFile("../db/migrations/migrations_stage178_1_bootstrap_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	migrate := func() {
		t.Helper()
		for run := 0; run < 2; run++ { // re-running changes nothing
			if _, err := db.DB.Exec(string(sqlText)); err != nil {
				t.Fatalf("migration run %d: %v", run+1, err)
			}
		}
	}

	// Still on the shipped credentials: system closes, cashier1 is asked to choose.
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Active' WHERE id = 'system'`)
	db.DB.Exec(`UPDATE tenant_default.users SET status = 'Active', password_hash = $1, must_change_password = FALSE WHERE id = 'cashier1'`, shippedCashier)
	migrate()
	sys, _ := read("system")
	cash, _ := read("cashier1")
	if sys.status != "Inactive" {
		t.Errorf("system status %q, want Inactive", sys.status)
	}
	if sys.hash != origSystem.hash {
		t.Error("the migration rewrote system's password hash")
	}
	if !cash.mustChange || cash.status != "Active" || cash.hash != shippedCashier {
		t.Errorf("cashier1 = %+v, want Active, must_change_password, hash untouched", cash)
	}

	// A password someone has set is left alone.
	db.DB.Exec(`UPDATE tenant_default.users SET password_hash = '$2a$10$someoneSetThisPasswordAlready000000000000000000000000', must_change_password = FALSE WHERE id = 'cashier1'`)
	migrate()
	if cash, _ = read("cashier1"); cash.mustChange {
		t.Error("cashier1 with a password of its own was asked to change it")
	}
}
