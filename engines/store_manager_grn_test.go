package engines

import (
	"os"
	"testing"

	"custom_erp/db"
)

// Stage 140.14.3: a Store Manager can list GRNs, which Purchase Return needs.
// The migration adds read only (never create, update or delete), leaves a row
// an administrator already set alone, and is safe to run again.
func TestStage140143StoreManagerReadsGRN(t *testing.T) {
	_, schema, cleanup, _ := gateEntryTestTenant(t)
	defer cleanup()
	sqlText, err := os.ReadFile("../db/migrations/migrations_stage140_14_3_store_manager_grn.sql")
	if err != nil {
		t.Fatal(err)
	}
	read := func(sch string) (bool, bool, bool) {
		var r, c, u bool
		err := db.DB.QueryRow(`SELECT allow_read, allow_create, allow_update FROM `+sch+`.role_permissions
			WHERE role = 'Store Manager' AND doctype_name = 'GRN'`).Scan(&r, &c, &u)
		if err != nil {
			return false, false, false
		}
		return r, c, u
	}
	db.DB.Exec(`DELETE FROM ` + schema + `.role_permissions WHERE role = 'Store Manager' AND doctype_name = 'GRN'`)
	if r, _, _ := read(schema); r {
		t.Fatal("fixture: the row should be gone before the migration")
	}
	for i := 0; i < 2; i++ {
		if _, err := db.DB.Exec(string(sqlText)); err != nil {
			t.Fatal(err)
		}
	}
	if r, c, u := read(schema); !r || c || u {
		t.Fatalf("Store Manager GRN after migration: read=%v create=%v update=%v, want read only", r, c, u)
	}
	// An administrator's own choice is kept.
	db.DB.Exec(`UPDATE ` + schema + `.role_permissions SET allow_read = FALSE WHERE role = 'Store Manager' AND doctype_name = 'GRN'`)
	if _, err := db.DB.Exec(string(sqlText)); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := read(schema); r {
		t.Fatal("the migration overwrote a row an administrator had set")
	}
}
